package legacy

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"io"
	"net"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/rlp"
	"golang.org/x/crypto/sha3"

	"github.com/zenon-network/go-zenon/p2p"
	"github.com/zenon-network/go-zenon/p2p/discover"
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
// by the other. Both sides have raiseFrameLimit called so that the
// steady-state bound (maxFrameSize) applies, matching post-handshake
// operation.
func newFramePair() (writer, reader *rlpxFrameRW, wire *countingConn) {
	wire = new(countingConn)
	s := secrets{
		AES:        crypto.Keccak256(),
		MAC:        crypto.Keccak256(),
		EgressMAC:  sha3.NewLegacyKeccak256(),
		IngressMAC: sha3.NewLegacyKeccak256(),
	}
	writer = newRLPXFrameRW(wire, s)
	reader = newRLPXFrameRW(wire, s)
	writer.raiseFrameLimit()
	reader.raiseFrameLimit()
	return writer, reader, wire
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

			if c.accepted {
				payload := payloadOf(c.size)
				writeMsg(t, writer, c.code, payload)
				readMsg(t, reader, c.code, payload)
				return
			}
			// For frames the writer now rejects (issue #108's write-path
			// bound), write only the header so the reader's size check is
			// what the case exercises.
			writeHeader(t, writer, frame)
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

// --- Handshake-phase dynamic bound tests (issue #108) ---

// testSecrets returns random AES and MAC keys for testing.
func testSecrets(t *testing.T) (aesKey, macKey []byte) {
	t.Helper()
	aesKey = make([]byte, 32)
	macKey = make([]byte, 32)
	if _, err := rand.Read(aesKey); err != nil {
		t.Fatal(err)
	}
	if _, err := rand.Read(macKey); err != nil {
		t.Fatal(err)
	}
	return aesKey, macKey
}

// newHandshakePair returns two rlpx transports connected by a pipe, with
// the encryption handshake already completed on both sides. The caller
// drives the protocol handshake.
func newHandshakePair(t *testing.T) (initiator, responder *rlpx) {
	t.Helper()
	c1, c2 := net.Pipe()
	prv1, _ := crypto.GenerateKey()
	prv2, _ := crypto.GenerateKey()

	initiator = newRLPX(c1).(*rlpx)
	responder = newRLPX(c2).(*rlpx)

	node2 := &discover.Node{
		ID:  discover.PubkeyID(&prv2.PublicKey),
		IP:  net.ParseIP("127.0.0.1"),
		UDP: 30304,
		TCP: 30304,
	}

	errCh := make(chan error, 2)
	go func() {
		_, err := initiator.doEncHandshake(prv1, node2)
		errCh <- err
	}()
	go func() {
		_, err := responder.doEncHandshake(prv2, nil)
		errCh <- err
	}()
	for i := 0; i < 2; i++ {
		if err := <-errCh; err != nil {
			t.Fatalf("enc handshake: %v", err)
		}
	}
	return initiator, responder
}

// protoHandshakeMsg returns a valid protocol handshake for the given key.
func protoHandshakeMsg(prv *ecdsa.PrivateKey) *protoHandshake {
	return &protoHandshake{
		Version: baseProtocolVersion,
		Name:    "test",
		ID:      discover.PubkeyID(&prv.PublicKey),
	}
}

// TestHandshakeBoundRejectsOversizedHeader verifies that during the
// handshake phase (before raiseFrameLimit), an authenticated frame header
// announcing more than baseProtocolMaxMsgSize is rejected before any body
// read.
func TestHandshakeBoundRejectsOversizedHeader(t *testing.T) {
	initiator, responder := newHandshakePair(t)
	defer initiator.fd.Close()
	defer responder.fd.Close()

	// The responder's frame RW is still in handshake phase. Send an
	// authenticated oversized header from the initiator.
	go func() {
		_ = writeOversizedFrame(initiator.rw, baseProtocolMaxMsgSize+100)
	}()

	_, err := responder.ReadMsg()
	if err == nil {
		t.Fatal("expected ReadMsg to reject oversized frame during handshake phase")
	}
	if !strings.Contains(err.Error(), "frame size") {
		t.Fatalf("expected frame size error, got: %v", err)
	}
}

// TestProtoHandshakePromotesFrameLimit verifies the production transition:
// after doProtoHandshake succeeds on both sides, a frame larger than the
// handshake bound (2 KiB) but within the steady-state bound is accepted.
func TestProtoHandshakePromotesFrameLimit(t *testing.T) {
	prv1, _ := crypto.GenerateKey()
	prv2, _ := crypto.GenerateKey()
	initiator, responder := newHandshakePair(t)
	defer initiator.fd.Close()
	defer responder.fd.Close()

	// Run the protocol handshake on both sides concurrently.
	errCh := make(chan error, 2)
	go func() {
		_, err := initiator.doProtoHandshake(protoHandshakeMsg(prv1))
		errCh <- err
	}()
	go func() {
		_, err := responder.doProtoHandshake(protoHandshakeMsg(prv2))
		errCh <- err
	}()
	for i := 0; i < 2; i++ {
		if err := <-errCh; err != nil {
			t.Fatalf("proto handshake: %v", err)
		}
	}

	// Simulate what setupConn does: promote the frame limit after the
	// handshake and identity checks succeed.
	initiator.raiseFrameLimit()
	responder.raiseFrameLimit()

	// Now a frame larger than 2 KiB but smaller than 10 MiB must succeed.
	payload := payloadOf(4096) // 4 KiB > 2 KiB handshake bound
	msg := p2p.Msg{
		Code:    0x10,
		Size:    uint32(len(payload)),
		Payload: bytesReader(payload),
	}

	writeErr := make(chan error, 1)
	go func() {
		writeErr <- initiator.WriteMsg(msg)
	}()

	got, err := responder.ReadMsg()
	if err != nil {
		t.Fatalf("ReadMsg failed after proto handshake: %v", err)
	}
	if got.Code != 0x10 {
		t.Errorf("expected code 0x10, got 0x%x", got.Code)
	}
	gotPayload, _ := io.ReadAll(got.Payload)
	if !bytes.Equal(gotPayload, payload) {
		t.Error("payload mismatch")
	}
	if err := <-writeErr; err != nil {
		t.Errorf("WriteMsg failed after proto handshake: %v", err)
	}
}

// TestHandshakeFailureNoPromotion verifies that when the protocol handshake
// fails (remote sends disconnect), the frame limit is not promoted.
func TestHandshakeFailureNoPromotion(t *testing.T) {
	prv1, _ := crypto.GenerateKey()
	initiator, responder := newHandshakePair(t)
	defer initiator.fd.Close()
	defer responder.fd.Close()

	// The initiator sends a disconnect instead of a handshake.
	go func() {
		initiator.fd.SetWriteDeadline(time.Now().Add(5 * time.Second))
		p2p.SendItems(initiator.rw, discMsg, p2p.DiscTooManyPeers)
	}()

	_, err := responder.doProtoHandshake(protoHandshakeMsg(prv1))
	if err == nil {
		t.Fatal("expected proto handshake to fail on disconnect")
	}

	// Clear the handshake deadline so the frame RW operations below are
	// not cut off by the pipe's earlier SetDeadline.
	responder.fd.SetReadDeadline(time.Time{})
	initiator.fd.SetWriteDeadline(time.Time{})

	// The responder's frame RW must still be in handshake phase: an
	// oversized header is still rejected.
	go func() {
		_ = writeOversizedFrame(initiator.rw, baseProtocolMaxMsgSize+100)
	}()
	_, err = responder.ReadMsg()
	if err == nil {
		t.Fatal("expected ReadMsg to reject oversized frame after failed handshake")
	}
	if !strings.Contains(err.Error(), "frame size") {
		t.Fatalf("expected frame size error, got: %v", err)
	}
}

// TestSetupConnPromotesFrameLimit exercises the production transition: a
// real client completes both handshakes against a real listening Server,
// which must promote the frame limit in setupConn before admitting the
// peer. If it does, a 4 KiB ping after admission is answered with pong; if
// the promotion is missing, the server's read loop rejects the frame and
// drops the peer. This is the test that pins the raiseFrameLimit call in
// setupConn — removing it leaves the unit suite green but fails here.
func TestSetupConnPromotesFrameLimit(t *testing.T) {
	srvKey, _ := crypto.GenerateKey()
	cliKey, _ := crypto.GenerateKey()
	added := make(chan *Peer, 1)
	srv := &Server{
		PrivateKey:  srvKey,
		MaxPeers:    10,
		ListenAddr:  "127.0.0.1:0",
		NoDial:      true,
		newPeerHook: func(p *Peer) { added <- p },
	}
	if err := srv.Start(); err != nil {
		t.Fatal(err)
	}
	defer srv.Stop()

	fd, err := net.Dial("tcp", srv.listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer fd.Close()
	addr := srv.listener.Addr().(*net.TCPAddr)
	dest := &discover.Node{ID: discover.PubkeyID(&srvKey.PublicKey), IP: addr.IP, TCP: uint16(addr.Port), UDP: uint16(addr.Port)}
	cli := newRLPX(fd).(*rlpx)
	if _, err := cli.doEncHandshake(cliKey, dest); err != nil {
		t.Fatal(err)
	}
	if _, err := cli.doProtoHandshake(&protoHandshake{Version: baseProtocolVersion, Name: "probe", ID: discover.PubkeyID(&cliKey.PublicKey)}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-added:
	case <-time.After(3 * time.Second):
		t.Fatal("peer not admitted")
	}
	cli.raiseFrameLimit() // client side only, so it can write 4 KiB
	fd.SetDeadline(time.Now().Add(5 * time.Second))
	big := payloadOf(4096)
	if err := cli.WriteMsg(p2p.Msg{Code: pingMsg, Size: uint32(len(big)), Payload: bytesReader(big)}); err != nil {
		t.Fatal(err)
	}
	for {
		msg, err := cli.ReadMsg()
		if err != nil {
			t.Fatalf("server dropped us after 4 KiB frame: %v", err)
		}
		switch msg.Code {
		case pongMsg:
			return
		case discMsg:
			var r [1]p2p.DiscReason
			rlp.Decode(msg.Payload, &r)
			t.Fatalf("server disconnected after 4 KiB frame: %v", r[0])
		default:
			msg.Discard()
		}
	}
}

// TestHandshakeBoundWriteRejectsOversized verifies that during the
// handshake phase, WriteMsg rejects frames larger than
// baseProtocolMaxMsgSize.
func TestHandshakeBoundWriteRejectsOversized(t *testing.T) {
	_, responder := newHandshakePair(t)
	defer responder.fd.Close()

	largePayload := make([]byte, baseProtocolMaxMsgSize+100)
	msg := p2p.Msg{
		Code:    0x10,
		Size:    uint32(len(largePayload)),
		Payload: bytesReader(largePayload),
	}
	err := responder.WriteMsg(msg)
	if err == nil {
		t.Error("expected WriteMsg to reject oversized frame during handshake phase")
	}
}

// newHandshakePhasePair returns a writer and reader over one wire that
// share the same secrets, both still in the handshake phase (neither has
// raiseFrameLimit called), so the handshakeFrameSize bound applies.
func newHandshakePhasePair() (writer, reader *rlpxFrameRW, wire *countingConn) {
	wire = new(countingConn)
	s := secrets{
		AES:        crypto.Keccak256(),
		MAC:        crypto.Keccak256(),
		EgressMAC:  sha3.NewLegacyKeccak256(),
		IngressMAC: sha3.NewLegacyKeccak256(),
	}
	return newRLPXFrameRW(wire, s), newRLPXFrameRW(wire, s), wire
}

// handshakeOfSize returns a protoHandshake whose RLP encoding is exactly n
// bytes, by padding the Name field.
func handshakeOfSize(t *testing.T, n int) *protoHandshake {
	t.Helper()
	key, _ := crypto.GenerateKey()
	hs := &protoHandshake{Version: baseProtocolVersion, ID: discover.PubkeyID(&key.PublicKey)}
	for pad := n; pad >= 0; pad-- {
		hs.Name = strings.Repeat("a", pad)
		b, err := rlp.EncodeToBytes(hs)
		if err != nil {
			t.Fatal(err)
		}
		if len(b) == n {
			return hs
		}
		if len(b) < n {
			break
		}
	}
	t.Fatalf("could not build handshake of %d bytes", n)
	return nil
}

// The handshake frame bound is the payload bound plus the 9-byte code
// allowance, so a handshake whose RLP encoding is exactly
// baseProtocolMaxMsgSize bytes fits its frame and is accepted. This is the
// boundary case issue #108's spec calls out: the frame bound must not be
// tighter than the message-level check it sits behind.
func TestHandshakeMaxSizePayloadAccepted(t *testing.T) {
	writer, reader, _ := newHandshakePhasePair()

	// Let the writer emit the maximal handshake; the reader stays in the
	// handshake phase, which is what we exercise.
	writer.raiseFrameLimit()
	hs := handshakeOfSize(t, baseProtocolMaxMsgSize)
	if err := p2p.Send(writer, handshakeMsg, hs); err != nil {
		t.Fatalf("maximal handshake not writable: %v", err)
	}
	if _, err := readProtocolHandshake(reader, &protoHandshake{Version: baseProtocolVersion}); err != nil {
		t.Fatalf("%d-byte handshake rejected in handshake phase: %v", baseProtocolMaxMsgSize, err)
	}
}

// A handshake one byte over the payload bound still fits the frame behind
// a short code, so the frame bound passes it through and the protocol
// layer's own check in readProtocolHandshake must reject it with
// "message too big". The payload-level check owns 2 KiB+1.
func TestHandshakeOverSizePayloadRejectedByProtocol(t *testing.T) {
	writer, reader, _ := newHandshakePhasePair()
	writer.raiseFrameLimit()
	hs := handshakeOfSize(t, baseProtocolMaxMsgSize+1)
	if err := p2p.Send(writer, handshakeMsg, hs); err != nil {
		t.Fatalf("over-size handshake not writable: %v", err)
	}
	_, err := readProtocolHandshake(reader, &protoHandshake{Version: baseProtocolVersion})
	if err == nil || !strings.Contains(err.Error(), "message too big") {
		t.Fatalf("expected \"message too big\", got: %v", err)
	}
}

// TestWriteOverflowRejected verifies that the write-path frame-size
// calculation is overflow-safe: a maximum uint32 Size does not wrap and is
// rejected. After the rejection, a valid round-trip on the same frame pair
// still works.
func TestWriteOverflowRejected(t *testing.T) {
	writer, reader, wire := newFramePair()

	// Maximum uint32 with a 1-byte code: under uint32 math
	// uint32(len(ptype)) + msg.Size wraps to 0, slipping past any
	// uint24 bound computed after the addition. The uint64 calculation
	// must reject it. No large allocation is needed — the Size field is
	// set directly and the guard runs before any buffer is sized.
	for _, size := range []uint32{^uint32(0), ^uint32(0) - 1} {
		msg := p2p.Msg{
			Code:    0x10,
			Size:    size,
			Payload: bytesReader(nil),
		}
		err := writer.WriteMsg(msg)
		if err == nil {
			t.Fatalf("expected WriteMsg to reject Size 0x%x", size)
		}
	}

	// After the rejected writes, a valid round-trip must still work.
	wire.nextFrame()
	payload := []byte("hello")
	writeMsg(t, writer, 7, payload)
	readMsg(t, reader, 7, payload)
}

// TestFrameRWWriteBoundAfterRaise verifies that even after raiseFrameLimit,
// WriteMsg still rejects frames above the steady-state maxFrameSize.
func TestFrameRWWriteBoundAfterRaise(t *testing.T) {
	aesKey, macKey := testSecrets(t)
	c1, _ := net.Pipe()
	defer c1.Close()

	rw := newRLPXFrameRW(c1, secrets{
		AES:        aesKey,
		MAC:        macKey,
		EgressMAC:  sha256.New(),
		IngressMAC: sha256.New(),
	})
	rw.raiseFrameLimit()

	// A frame larger than maxFrameSize (10 MiB + 9) must be rejected.
	// We don't actually allocate 10 MiB — just set the Size field.
	msg := p2p.Msg{
		Code:    0x10,
		Size:    maxFrameSize + 1,
		Payload: bytesReader(nil),
	}
	err := rw.WriteMsg(msg)
	if err == nil {
		t.Error("expected WriteMsg to reject frame above maxFrameSize")
	}
}

// writeOversizedFrame crafts and writes a complete encrypted frame with the
// given fsize (but a minimal body), using the writer's cipher and MAC.
// The reader should reject the frame after parsing the header, without
// reading the body. It returns a write error instead of calling t.Error so
// it is safe to invoke from a goroutine that may outlive the test.
func writeOversizedFrame(writer *rlpxFrameRW, fsize uint32) error {
	// Build the plaintext header.
	headbuf := make([]byte, 32)
	putInt24(fsize, headbuf)
	copy(headbuf[3:], zeroHeader)

	// Encrypt the first 16 bytes.
	writer.enc.XORKeyStream(headbuf[:16], headbuf[:16])

	// Compute the header MAC.
	mac := updateMAC(writer.egressMAC, writer.macCipher, headbuf[:16])
	copy(headbuf[16:], mac)

	if _, err := writer.conn.Write(headbuf); err != nil {
		return err
	}
	// Do NOT write the body — ReadMsg should reject based on the header alone.
	return nil
}

// bytesReader returns an io.Reader over a byte slice.
func bytesReader(b []byte) io.Reader {
	return &sliceReader{b: b, i: 0}
}

type sliceReader struct {
	b []byte
	i int
}

func (r *sliceReader) Read(p []byte) (n int, err error) {
	if r.i >= len(r.b) {
		return 0, io.EOF
	}
	n = copy(p, r.b[r.i:])
	r.i += n
	return n, nil
}
