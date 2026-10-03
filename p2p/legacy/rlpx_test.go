package legacy

import (
	"bytes"
	"errors"
	"io"
	"runtime"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/rlp"
	"golang.org/x/crypto/sha3"

	"github.com/zenon-network/go-zenon/p2p"
)

// Message codes at both ends of the RLP encoding range. The code shares
// the frame with the payload, so its encoded length decides how much of
// the frame bound is left for the payload.
const (
	shortCode = uint64(0x10)    // one RLP byte
	longCode  = uint64(1) << 56 // nine RLP bytes, the most a uint64 takes
)

// countingConn is the wire between a frame writer and reader. It records
// how many bytes the reader asked for after the 32-byte header, so a test
// can tell whether the reader tried to read a frame body at all. ReadMsg
// fetches the header with one io.ReadFull of exactly 32 bytes, so any read
// after 32 bytes have been delivered is a body read.
type countingConn struct {
	bytes.Buffer
	read      int
	bodyReads int
}

func (c *countingConn) Read(p []byte) (int, error) {
	if c.read >= 32 {
		c.bodyReads++
	}
	n, err := c.Buffer.Read(p)
	c.read += n
	return n, err
}

func (c *countingConn) bodyRequested() bool { return c.bodyReads > 0 }

// nextFrame restarts the header accounting, so that after earlier frames
// have been read bodyRequested refers to the frame read next.
func (c *countingConn) nextFrame() { c.read, c.bodyReads = 0, 0 }

// newFramePair returns a writer and a reader over one wire that share the
// same secrets, so frames written by one are decrypted and authenticated
// by the other.
func newFramePair() (writer, reader *rlpxFrameRW, wire *countingConn) {
	wire = new(countingConn)
	s := secrets{
		AES:        crypto.Keccak256(),
		MAC:        crypto.Keccak256(),
		EgressMAC:  sha3.NewLegacyKeccak256(),
		IngressMAC: sha3.NewLegacyKeccak256(),
	}
	return newRLPXFrameRW(wire, s), newRLPXFrameRW(wire, s), wire
}

// writeHeader sends an authenticated frame header announcing fsize bytes of
// content and nothing else, the way WriteMsg starts a frame.
func writeHeader(t *testing.T, w *rlpxFrameRW, fsize uint32) {
	t.Helper()
	headbuf := make([]byte, 32)
	putInt24(fsize, headbuf)
	copy(headbuf[3:], zeroHeader)
	w.enc.XORKeyStream(headbuf[:16], headbuf[:16])
	copy(headbuf[16:], updateMAC(w.egressMAC, w.macCipher, headbuf[:16]))
	if _, err := w.conn.Write(headbuf); err != nil {
		t.Fatal(err)
	}
}

func writeMsg(t *testing.T, w *rlpxFrameRW, code uint64, payload []byte) {
	t.Helper()
	msg := p2p.Msg{Code: code, Size: uint32(len(payload)), Payload: bytes.NewReader(payload)}
	if err := w.WriteMsg(msg); err != nil {
		t.Fatal(err)
	}
}

// readMsg reads one frame and checks that it carries the given code and
// payload.
func readMsg(t *testing.T, r *rlpxFrameRW, code uint64, payload []byte) {
	t.Helper()
	msg, err := r.ReadMsg()
	if err != nil {
		t.Fatalf("frame rejected: %v", err)
	}
	if msg.Code != code || msg.Size != uint32(len(payload)) {
		t.Fatalf("code %d size %d, want code %d size %d", msg.Code, msg.Size, code, len(payload))
	}
	got, err := io.ReadAll(msg.Payload)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatal("payload mismatch")
	}
}

// payloadOf returns n bytes in a position-dependent pattern, so a round
// trip that drops or shifts bytes does not compare equal.
func payloadOf(n uint32) []byte {
	p := make([]byte, n)
	for i := range p {
		p[i] = byte(i*7 + i>>8)
	}
	return p
}

func encodedLen(t *testing.T, code uint64) uint32 {
	t.Helper()
	b, err := rlp.EncodeToBytes(code)
	if err != nil {
		t.Fatal(err)
	}
	return uint32(len(b))
}

func totalAlloc() uint64 {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return m.TotalAlloc
}

// The frame bound is the payload bound plus the longest message code.
func TestFrameBoundLeavesRoomForTheLongestCode(t *testing.T) {
	if n := encodedLen(t, shortCode); n != 1 {
		t.Fatalf("short code encodes to %d bytes, want 1", n)
	}
	if n := encodedLen(t, longCode); n != 9 {
		t.Fatalf("long code encodes to %d bytes, want 9", n)
	}
	if n := encodedLen(t, ^uint64(0)); n != 9 {
		t.Fatalf("largest code encodes to %d bytes, want 9", n)
	}
	if maxFrameSize != maxMessageSize+9 {
		t.Fatalf("maxFrameSize %d, want maxMessageSize + 9 = %d", maxFrameSize, maxMessageSize+9)
	}
}

// A header announcing more content than any message can carry is rejected
// on the strength of the header alone: no body is read and no buffer of
// the announced size is allocated.
func TestReadMsgRejectsOversizedFrameBeforeReadingBody(t *testing.T) {
	for _, fsize := range []uint32{maxFrameSize + 1, maxUint24} {
		writer, reader, wire := newFramePair()
		writeHeader(t, writer, fsize)

		before := totalAlloc()
		_, err := reader.ReadMsg()
		allocated := totalAlloc() - before

		if err == nil || !strings.Contains(err.Error(), "frame size") {
			t.Fatalf("fsize %d: expected a frame size error, got %v", fsize, err)
		}
		// The wire is the direct guard: a reader that sized a buffer by
		// the header would go on to fill it from the wire.
		if wire.bodyRequested() {
			t.Fatalf("fsize %d: the body was requested after an oversized header", fsize)
		}
		// Process-wide allocation is the indirect one; the threshold is
		// far below the announced sizes (10 to 16 MiB) and far above
		// what handling a header costs.
		if allocated > 1<<20 {
			t.Fatalf("fsize %d: %d bytes allocated while handling the header", fsize, allocated)
		}
	}
}

// The transport bound is on the frame, code and payload together; the
// payload bound is the protocol layer's. Every payload up to the payload
// bound fits the frame whatever its code, so the transport must accept it.
// A payload one byte over the payload bound still fits the frame behind a
// short code, and the protocol layer's own check has to reject it; behind
// the longest code it does not fit, and the transport rejects it before
// reading the body.
func TestFrameBoundAgainstPayloadBound(t *testing.T) {
	cases := []struct {
		name     string
		code     uint64
		size     uint32
		accepted bool
	}{
		{"short code, largest payload", shortCode, maxMessageSize, true},
		{"long code, largest payload", longCode, maxMessageSize, true},
		{"short code, one byte over the payload bound", shortCode, maxMessageSize + 1, true},
		{"long code, one byte over the payload bound", longCode, maxMessageSize + 1, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			frame := encodedLen(t, c.code) + c.size
			if fits := frame <= maxFrameSize; fits != c.accepted {
				t.Fatalf("frame of %d bytes fits the bound: %v, case expects %v", frame, fits, c.accepted)
			}

			writer, reader, wire := newFramePair()
			payload := payloadOf(c.size)
			writeMsg(t, writer, c.code, payload)

			if c.accepted {
				readMsg(t, reader, c.code, payload)
				return
			}
			if _, err := reader.ReadMsg(); err == nil || !strings.Contains(err.Error(), "frame size") {
				t.Fatalf("frame of %d bytes accepted: %v", frame, err)
			}
			if wire.bodyRequested() {
				t.Fatal("the body was requested for a frame over the bound")
			}
		})
	}
}

// Header authentication comes before the size check: a tampered header is
// rejected for its MAC whatever length it announces, and no body is read.
func TestReadMsgHeaderMACPrecedesSizeCheck(t *testing.T) {
	for _, fsize := range []uint32{64, maxFrameSize + 1, maxUint24} {
		writer, reader, wire := newFramePair()
		writeHeader(t, writer, fsize)
		wire.Bytes()[20] ^= 0xff // inside the header MAC
		if _, err := reader.ReadMsg(); err == nil || !strings.Contains(err.Error(), "bad header MAC") {
			t.Fatalf("fsize %d: tampered header accepted: %v", fsize, err)
		}
		if wire.bodyRequested() {
			t.Fatalf("fsize %d: the body was requested after a bad header MAC", fsize)
		}
	}
}

// The body controls are unchanged: a frame cut short in its header, body
// or MAC, a frame whose content was altered and a frame whose MAC was
// altered are each rejected.
func TestReadMsgBodyControls(t *testing.T) {
	body := []byte("hello")
	// One byte of code and five of payload pad to 16, so the wire holds
	// 32 header bytes, 16 body bytes and 16 MAC bytes.
	const wireLen = 32 + 16 + 16

	writer, reader, wire := newFramePair()
	writeMsg(t, writer, 7, body)
	if wire.Len() != wireLen {
		t.Fatalf("wire holds %d bytes, want %d", wire.Len(), wireLen)
	}
	wire.Truncate(32 + 8)
	if _, err := reader.ReadMsg(); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("truncated frame: got %v, want %v", err, io.ErrUnexpectedEOF)
	}

	writer, reader, wire = newFramePair()
	writeMsg(t, writer, 7, body)
	wire.Truncate(16)
	if _, err := reader.ReadMsg(); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("truncated header: got %v, want %v", err, io.ErrUnexpectedEOF)
	}

	writer, reader, wire = newFramePair()
	writeMsg(t, writer, 7, body)
	wire.Truncate(32 + 16)
	if _, err := reader.ReadMsg(); !errors.Is(err, io.EOF) {
		t.Fatalf("missing frame MAC: got %v, want %v", err, io.EOF)
	}

	writer, reader, wire = newFramePair()
	writeMsg(t, writer, 7, body)
	wire.Bytes()[32] ^= 0xff // first byte of the encrypted content
	if _, err := reader.ReadMsg(); err == nil || !strings.Contains(err.Error(), "bad frame MAC") {
		t.Fatalf("altered content accepted: %v", err)
	}

	writer, reader, wire = newFramePair()
	writeMsg(t, writer, 7, body)
	wire.Bytes()[wireLen-1] ^= 0xff // last byte of the frame MAC
	if _, err := reader.ReadMsg(); err == nil || !strings.Contains(err.Error(), "bad frame MAC") {
		t.Fatalf("altered frame MAC accepted: %v", err)
	}
}

// Frames round-trip in sequence over one connection, with the MAC state
// carried from one to the next, and the bound applies to a later frame
// just as it does to the first.
func TestReadMsgConsecutiveFrames(t *testing.T) {
	writer, reader, wire := newFramePair()
	frames := []struct {
		code    uint64
		payload []byte
	}{
		{7, []byte("one")},
		{shortCode, payloadOf(16)}, // exactly one padded block with the code
		{longCode, payloadOf(1000)},
		{pingMsg, nil},
	}
	for _, f := range frames {
		writeMsg(t, writer, f.code, f.payload)
	}
	for _, f := range frames {
		readMsg(t, reader, f.code, f.payload)
	}

	writeHeader(t, writer, maxFrameSize+1)
	wire.nextFrame()
	if _, err := reader.ReadMsg(); err == nil || !strings.Contains(err.Error(), "frame size") {
		t.Fatalf("oversized header after valid frames accepted: %v", err)
	}
	if wire.bodyRequested() {
		t.Fatal("the body was requested after an oversized header")
	}
}
