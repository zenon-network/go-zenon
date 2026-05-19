package p2p

import (
	"encoding/binary"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/rlp"
	"github.com/libp2p/go-libp2p/core/network"
)

// maxMessageSize is the maximum payload size allowed in a single message.
// This prevents a malicious peer from causing an OOM by declaring a huge payload.
const maxMessageSize = 10 * 1024 * 1024 // 10MB, matching ProtocolMaxMsgSize

// StreamRW wraps a libp2p bidirectional stream to implement MsgReadWriter.
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

// ReadMsg reads a message from the stream.
func (rw *StreamRW) ReadMsg() (Msg, error) {
	rw.rmu.Lock()
	defer rw.rmu.Unlock()

	rw.stream.SetReadDeadline(time.Now().Add(frameReadTimeout))
	defer rw.stream.SetReadDeadline(time.Time{})

	// Read 8-byte header
	var header [8]byte
	if _, err := io.ReadFull(rw.stream, header[:]); err != nil {
		return Msg{}, err
	}

	code := uint64(binary.BigEndian.Uint32(header[0:4]))
	size := binary.BigEndian.Uint32(header[4:8])

	// Reject messages exceeding the maximum size before allocating memory
	if size > maxMessageSize {
		return Msg{}, fmt.Errorf("message too large: %d bytes (max %d)", size, maxMessageSize)
	}

	// Read payload
	payload := make([]byte, size)
	if size > 0 {
		if _, err := io.ReadFull(rw.stream, payload); err != nil {
			return Msg{}, err
		}
	}

	return Msg{
		Code:    code,
		Size:    size,
		Payload: &payloadReader{data: payload},
	}, nil
}

// WriteMsg writes a message to the stream.
func (rw *StreamRW) WriteMsg(msg Msg) error {
	rw.wmu.Lock()
	defer rw.wmu.Unlock()

	rw.stream.SetWriteDeadline(time.Now().Add(frameWriteTimeout))
	defer rw.stream.SetWriteDeadline(time.Time{})

	// Encode payload
	payload, err := io.ReadAll(msg.Payload)
	if err != nil {
		return fmt.Errorf("read payload: %w", err)
	}

	// Build 8-byte header
	var header [8]byte
	binary.BigEndian.PutUint32(header[0:4], uint32(msg.Code))
	binary.BigEndian.PutUint32(header[4:8], uint32(len(payload)))

	// Write header + payload
	if _, err := rw.stream.Write(header[:]); err != nil {
		return err
	}
	if _, err := rw.stream.Write(payload); err != nil {
		return err
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
