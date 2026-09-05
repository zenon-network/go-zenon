package legacy

import (
	"bytes"
	"crypto/rand"
	"io"
	"runtime"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/rlp"
	"golang.org/x/crypto/sha3"

	"github.com/zenon-network/go-zenon/p2p"
)

// countingConn is the wire between a frame writer and reader. It records
// how many bytes the reader asked for after the 32-byte header, so a test
// can tell whether the reader tried to read a frame body at all.
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
func writeHeader(w *rlpxFrameRW, fsize uint32) {
	headbuf := make([]byte, 32)
	putInt24(fsize, headbuf)
	copy(headbuf[3:], zeroHeader)
	w.enc.XORKeyStream(headbuf[:16], headbuf[:16])
	copy(headbuf[16:], updateMAC(w.egressMAC, w.macCipher, headbuf[:16]))
	w.conn.Write(headbuf)
}

func totalAlloc() uint64 {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return m.TotalAlloc
}

// A header announcing more content than any message can carry is rejected
// on the strength of the header alone: no body is read and no buffer of
// the announced size is allocated.
func TestReadMsgRejectsOversizedFrameBeforeReadingBody(t *testing.T) {
	for _, fsize := range []uint32{maxFrameSize + 1, maxUint24} {
		writer, reader, wire := newFramePair()
		writeHeader(writer, fsize)

		before := totalAlloc()
		_, err := reader.ReadMsg()
		allocated := totalAlloc() - before

		if err == nil || !strings.Contains(err.Error(), "frame") {
			t.Fatalf("fsize %d: expected a frame size error, got %v", fsize, err)
		}
		if wire.bodyRequested() {
			t.Fatalf("fsize %d: the body was requested after an oversized header", fsize)
		}
		if allocated > 1<<20 {
			t.Fatalf("fsize %d: %d bytes allocated while handling the header", fsize, allocated)
		}
	}
}

// The largest message the protocol layer accepts still passes the
// transport, and one byte more does not.
func TestReadMsgFrameLimitMatchesLargestMessage(t *testing.T) {
	ptype, _ := rlp.EncodeToBytes(uint64(0x10))
	largest := maxFrameSize - uint32(len(ptype))

	writer, reader, _ := newFramePair()
	payload := make([]byte, largest)
	rand.Read(payload)
	if err := writer.WriteMsg(p2p.Msg{Code: 0x10, Size: largest, Payload: bytes.NewReader(payload)}); err != nil {
		t.Fatal(err)
	}
	msg, err := reader.ReadMsg()
	if err != nil {
		t.Fatalf("frame at the limit rejected: %v", err)
	}
	if msg.Size != largest {
		t.Fatalf("size %d, want %d", msg.Size, largest)
	}
	got, _ := io.ReadAll(msg.Payload)
	if !bytes.Equal(got, payload) {
		t.Fatal("payload mismatch")
	}

	writer, reader, wire := newFramePair()
	payload = make([]byte, largest+1)
	if err := writer.WriteMsg(p2p.Msg{Code: 0x10, Size: largest + 1, Payload: bytes.NewReader(payload)}); err != nil {
		t.Fatal(err)
	}
	if _, err := reader.ReadMsg(); err == nil || !strings.Contains(err.Error(), "frame") {
		t.Fatalf("frame one byte over the limit accepted: %v", err)
	}
	if wire.bodyRequested() {
		t.Fatal("the body was requested for a frame over the limit")
	}
}

// The existing controls are unchanged: a header with a bad MAC is rejected
// before anything else, and an ordinary frame round-trips.
func TestReadMsgHeaderControls(t *testing.T) {
	writer, reader, wire := newFramePair()
	writeHeader(writer, 64)
	wire.Bytes()[20] ^= 0xff
	if _, err := reader.ReadMsg(); err == nil || !strings.Contains(err.Error(), "bad header MAC") {
		t.Fatalf("tampered header accepted: %v", err)
	}
	if wire.bodyRequested() {
		t.Fatal("the body was requested after a bad header MAC")
	}

	writer, reader, _ = newFramePair()
	body := []byte("hello")
	if err := writer.WriteMsg(p2p.Msg{Code: 7, Size: uint32(len(body)), Payload: bytes.NewReader(body)}); err != nil {
		t.Fatal(err)
	}
	msg, err := reader.ReadMsg()
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(msg.Payload)
	if msg.Code != 7 || string(got) != "hello" {
		t.Fatalf("round trip: code %d payload %q", msg.Code, got)
	}
}
