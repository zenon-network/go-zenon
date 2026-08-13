package libp2p

import (
	"encoding/binary"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/rlp"
	"github.com/libp2p/go-libp2p/core/network"

	"github.com/zenon-network/go-zenon/p2p"
)

// maxMessageSize is the maximum payload size allowed in a single message.
// This prevents a malicious peer from causing an OOM by declaring a huge payload.
const maxMessageSize = 10 * 1024 * 1024 // 10MB, matching ProtocolMaxMsgSize

// StreamRW wraps a libp2p bidirectional stream to implement p2p.MsgReadWriter.
//
// Wire format (replaces RLPX framing):
//
//	[4 bytes: message code, big-endian uint32]
//	[4 bytes: payload length, big-endian uint32]
//	[N bytes: RLP payload]
//
// No encryption at this layer — libp2p Noise handles transport security.
type StreamRW struct {
	stream network.Stream
	rmu    sync.Mutex
	wmu    sync.Mutex
}

// NewStreamRW creates a new StreamRW wrapping the given libp2p stream.
func NewStreamRW(s network.Stream) *StreamRW {
	return &StreamRW{stream: s}
}

// ReadMsg reads a message from the stream, rejecting a frame whose
// declared size exceeds maxMessageSize before allocating a payload
// buffer for it.
func (rw *StreamRW) ReadMsg() (p2p.Msg, error) {
	return rw.readMsg(maxMessageSize, frameReadTimeout)
}

// ReadMsgLimited reads a message from the stream like ReadMsg, but
// rejects a frame whose declared size exceeds maxSize before allocating
// a payload buffer for it. Used on the pre-adoption handshake path,
// where the remote hasn't been authenticated yet, so the cap can be
// tighter than the post-handshake maxMessageSize.
func (rw *StreamRW) ReadMsgLimited(maxSize uint32) (p2p.Msg, error) {
	return rw.readMsg(maxSize, frameReadTimeout)
}

// ReadMsgLimitedTimeout is ReadMsgLimited with an explicit read
// deadline, rather than the steady-state frameReadTimeout. Used for the
// pre-adoption handshake read, which should time out much sooner than a
// live peer's frame reads so a stalled, unauthenticated remote can't
// hold a pending-handshake slot for the full steady-state timeout.
func (rw *StreamRW) ReadMsgLimitedTimeout(maxSize uint32, timeout time.Duration) (p2p.Msg, error) {
	return rw.readMsg(maxSize, timeout)
}

func (rw *StreamRW) readMsg(maxSize uint32, timeout time.Duration) (p2p.Msg, error) {
	rw.rmu.Lock()
	defer rw.rmu.Unlock()

	rw.stream.SetReadDeadline(time.Now().Add(timeout))
	defer rw.stream.SetReadDeadline(time.Time{})

	// Read 8-byte header
	var header [8]byte
	if _, err := io.ReadFull(rw.stream, header[:]); err != nil {
		return p2p.Msg{}, err
	}

	code := uint64(binary.BigEndian.Uint32(header[0:4]))
	size := binary.BigEndian.Uint32(header[4:8])

	// Reject messages exceeding the maximum size before allocating memory
	if size > maxSize {
		return p2p.Msg{}, fmt.Errorf("message too large: %d bytes (max %d)", size, maxSize)
	}

	// Read payload
	payload := make([]byte, size)
	if size > 0 {
		if _, err := io.ReadFull(rw.stream, payload); err != nil {
			return p2p.Msg{}, err
		}
	}

	return p2p.Msg{
		Code:    code,
		Size:    size,
		Payload: &payloadReader{data: payload},
	}, nil
}

// WriteMsg writes a message to the stream.
func (rw *StreamRW) WriteMsg(msg p2p.Msg) error {
	rw.wmu.Lock()
	defer rw.wmu.Unlock()

	rw.stream.SetWriteDeadline(time.Now().Add(frameWriteTimeout))
	defer rw.stream.SetWriteDeadline(time.Time{})

	// Encode payload. Bound the read at maxMessageSize+1 so an oversized
	// or unbounded Payload reader can't force an unbounded allocation
	// here — ReadMsg enforces the same cap on the receiving side.
	payload, err := io.ReadAll(io.LimitReader(msg.Payload, int64(maxMessageSize)+1))
	if err != nil {
		return fmt.Errorf("read payload: %w", err)
	}
	if len(payload) > maxMessageSize {
		return fmt.Errorf("message too large: payload exceeds %d bytes", maxMessageSize)
	}
	if msg.Size != uint32(len(payload)) {
		return fmt.Errorf("declared message size %d does not match payload length %d", msg.Size, len(payload))
	}

	// Build 8-byte header
	var header [8]byte
	binary.BigEndian.PutUint32(header[0:4], uint32(msg.Code))
	binary.BigEndian.PutUint32(header[4:8], uint32(len(payload)))

	// Write header + payload, looping to handle short writes
	if err := writeFull(rw.stream, header[:]); err != nil {
		return err
	}
	if len(payload) > 0 {
		if err := writeFull(rw.stream, payload); err != nil {
			return err
		}
	}
	return nil
}

// writeFull writes all bytes from buf to w, looping on short writes.
func writeFull(w io.Writer, buf []byte) error {
	for len(buf) > 0 {
		n, err := w.Write(buf)
		if err != nil {
			return err
		}
		buf = buf[n:]
	}
	return nil
}

// Close closes the underlying stream.
func (rw *StreamRW) Close() error {
	return rw.stream.Close()
}

// payloadReader is a simple io.Reader over a byte slice.
type payloadReader struct {
	data []byte
	pos  int
}

func (r *payloadReader) Read(buf []byte) (int, error) {
	if r.pos >= len(r.data) {
		return 0, io.EOF
	}
	n := copy(buf, r.data[r.pos:])
	r.pos += n
	return n, nil
}

// RlpEncode encodes data as RLP and returns the bytes and size.
func RlpEncode(data interface{}) ([]byte, uint32, error) {
	size, r, err := rlp.EncodeToReader(data)
	if err != nil {
		return nil, 0, err
	}
	payload, err := io.ReadAll(r)
	if err != nil {
		return nil, 0, err
	}
	return payload, uint32(size), nil
}
