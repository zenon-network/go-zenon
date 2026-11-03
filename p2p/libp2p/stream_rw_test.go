package libp2p

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"runtime"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/rlp"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/protocol"

	"github.com/zenon-network/go-zenon/p2p"
)

// mockStream implements network.Stream for testing.
type mockStream struct {
	readBuf  *bytes.Buffer
	writeBuf *bytes.Buffer
}

func newMockStream() *mockStream {
	return &mockStream{
		readBuf:  &bytes.Buffer{},
		writeBuf: &bytes.Buffer{},
	}
}

func (m *mockStream) Read(p []byte) (int, error)         { return m.readBuf.Read(p) }
func (m *mockStream) Write(p []byte) (int, error)        { return m.writeBuf.Write(p) }
func (m *mockStream) Close() error                       { return nil }
func (m *mockStream) Reset() error                       { return nil }
func (m *mockStream) CloseRead() error                   { return nil }
func (m *mockStream) CloseWrite() error                  { return nil }
func (m *mockStream) SetDeadline(t time.Time) error      { return nil }
func (m *mockStream) SetReadDeadline(t time.Time) error  { return nil }
func (m *mockStream) SetWriteDeadline(t time.Time) error { return nil }
func (m *mockStream) ID() string                         { return "mock" }
func (m *mockStream) Protocol() protocol.ID              { return "" }
func (m *mockStream) SetProtocol(id protocol.ID) error   { return nil }
func (m *mockStream) Stat() network.Stats                { return network.Stats{} }
func (m *mockStream) Conn() network.Conn                 { return nil }
func (m *mockStream) Scope() network.StreamScope         { return nil }

// buildRawMessage builds the 8-byte header + RLP payload manually.
func buildRawMessage(code uint64, payload interface{}) ([]byte, error) {
	encoded, err := rlp.EncodeToBytes(payload)
	if err != nil {
		return nil, err
	}
	var header [8]byte
	binary.BigEndian.PutUint32(header[0:4], uint32(code))
	binary.BigEndian.PutUint32(header[4:8], uint32(len(encoded)))
	return append(header[:], encoded...), nil
}

func TestStreamRW_ReadMsg(t *testing.T) {
	ms := newMockStream()
	rw := NewStreamRW(ms)

	// Write a raw message into the read buffer
	payload := []string{"hello", "world"}
	raw, err := buildRawMessage(42, payload)
	if err != nil {
		t.Fatal(err)
	}
	ms.readBuf.Write(raw)

	msg, err := rw.ReadMsg()
	if err != nil {
		t.Fatal(err)
	}
	if msg.Code != 42 {
		t.Fatalf("expected code 42, got %d", msg.Code)
	}

	var decoded []string
	if err := msg.Decode(&decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded) != 2 || decoded[0] != "hello" || decoded[1] != "world" {
		t.Fatalf("unexpected payload: %v", decoded)
	}
}

func TestStreamRW_WriteMsg(t *testing.T) {
	ms := newMockStream()
	rw := NewStreamRW(ms)

	payloadData := []string{"foo", "bar"}
	size, r, err := rlp.EncodeToReader(payloadData)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}

	msg := p2p.Msg{
		Code:    10,
		Size:    uint32(size),
		Payload: bytes.NewReader(payload),
	}

	if err := rw.WriteMsg(msg); err != nil {
		t.Fatal(err)
	}

	written := ms.writeBuf.Bytes()
	if len(written) < 8 {
		t.Fatalf("written too short: %d bytes", len(written))
	}

	code := binary.BigEndian.Uint32(written[0:4])
	length := binary.BigEndian.Uint32(written[4:8])

	if code != 10 {
		t.Fatalf("expected code 10, got %d", code)
	}
	if length != uint32(len(payload)) {
		t.Fatalf("expected length %d, got %d", len(payload), length)
	}

	var decoded []string
	if err := rlp.DecodeBytes(written[8:], &decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded) != 2 || decoded[0] != "foo" || decoded[1] != "bar" {
		t.Fatalf("unexpected payload: %v", decoded)
	}
}

func TestStreamRW_WriteMsg_RejectsSizeLargerThanPayload(t *testing.T) {
	ms := newMockStream()
	rw := NewStreamRW(ms)

	msg := p2p.Msg{
		Code:    1,
		Size:    100, // claims 100 bytes
		Payload: bytes.NewReader([]byte("short")),
	}

	err := rw.WriteMsg(msg)
	if err == nil {
		t.Fatal("expected error for msg.Size larger than actual payload")
	}
	if ms.writeBuf.Len() != 0 {
		t.Fatalf("expected nothing written on size mismatch, got %d bytes", ms.writeBuf.Len())
	}
}

func TestStreamRW_WriteMsg_RejectsSizeSmallerThanPayload(t *testing.T) {
	ms := newMockStream()
	rw := NewStreamRW(ms)

	msg := p2p.Msg{
		Code:    1,
		Size:    2, // claims fewer bytes than actually provided
		Payload: bytes.NewReader([]byte("longer than declared")),
	}

	err := rw.WriteMsg(msg)
	if err == nil {
		t.Fatal("expected error for msg.Size smaller than actual payload")
	}
	if ms.writeBuf.Len() != 0 {
		t.Fatalf("expected nothing written on size mismatch, got %d bytes", ms.writeBuf.Len())
	}
}

func TestStreamRW_RoundTrip(t *testing.T) {
	// Use a net.Pipe so WriteMsg output feeds into ReadMsg
	c1, c2 := net.Pipe()
	stream1 := &netConnStream{conn: c1}
	stream2 := &netConnStream{conn: c2}
	rw1 := NewStreamRW(stream1)
	rw2 := NewStreamRW(stream2)

	payloadData := struct {
		A uint64
		B string
	}{A: 99, B: "test"}

	go func() {
		size, reader, err := rlp.EncodeToReader(payloadData)
		if err != nil {
			c1.Close()
			return
		}
		payload, _ := io.ReadAll(reader)
		msg := p2p.Msg{
			Code:    7,
			Size:    uint32(size),
			Payload: bytes.NewReader(payload),
		}
		rw1.WriteMsg(msg)
	}()

	msg, err := rw2.ReadMsg()
	if err != nil {
		t.Fatal(err)
	}
	if msg.Code != 7 {
		t.Fatalf("expected code 7, got %d", msg.Code)
	}

	var decoded struct {
		A uint64
		B string
	}
	if err := msg.Decode(&decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.A != 99 || decoded.B != "test" {
		t.Fatalf("unexpected: %+v", decoded)
	}
}

func TestStreamRW_EmptyPayload(t *testing.T) {
	ms := newMockStream()
	rw := NewStreamRW(ms)

	var header [8]byte
	binary.BigEndian.PutUint32(header[0:4], 5)
	binary.BigEndian.PutUint32(header[4:8], 0)
	ms.readBuf.Write(header[:])

	msg, err := rw.ReadMsg()
	if err != nil {
		t.Fatal(err)
	}
	if msg.Code != 5 {
		t.Fatalf("expected code 5, got %d", msg.Code)
	}
	if msg.Size != 0 {
		t.Fatalf("expected size 0, got %d", msg.Size)
	}
}

func TestStreamRW_RejectsOversizedMessage(t *testing.T) {
	ms := newMockStream()
	rw := NewStreamRW(ms)

	// Write a header claiming 4GB payload
	var header [8]byte
	binary.BigEndian.PutUint32(header[0:4], 1)
	binary.BigEndian.PutUint32(header[4:8], 0xFFFFFFFF) // 4GB
	ms.readBuf.Write(header[:])

	_, err := rw.ReadMsg()
	if err == nil {
		t.Fatal("expected error for oversized message")
	}
	if !bytes.Contains([]byte(err.Error()), []byte("too large")) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestStreamRW_LargePayload(t *testing.T) {
	ms := newMockStream()
	rw := NewStreamRW(ms)

	// Use a string slice that RLP handles cleanly
	bigSlice := make([]string, 1000)
	for i := range bigSlice {
		bigSlice[i] = "abcdefghijklmnop" // 16 bytes each
	}

	raw, err := buildRawMessage(1, bigSlice)
	if err != nil {
		t.Fatal(err)
	}
	ms.readBuf.Write(raw)

	msg, err := rw.ReadMsg()
	if err != nil {
		t.Fatal(err)
	}
	if msg.Code != 1 {
		t.Fatalf("expected code 1, got %d", msg.Code)
	}

	var decoded []string
	if err := msg.Decode(&decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded) != len(bigSlice) {
		t.Fatalf("expected %d items, got %d", len(bigSlice), len(decoded))
	}
	if decoded[0] != bigSlice[0] || decoded[999] != bigSlice[999] {
		t.Fatal("payload content mismatch")
	}
}

// netConnStream wraps a net.Conn as a network.Stream for testing.
type netConnStream struct {
	conn net.Conn
}

func (s *netConnStream) Read(p []byte) (int, error)         { return s.conn.Read(p) }
func (s *netConnStream) Write(p []byte) (int, error)        { return s.conn.Write(p) }
func (s *netConnStream) Close() error                       { return s.conn.Close() }
func (s *netConnStream) Reset() error                       { return s.conn.Close() }
func (s *netConnStream) CloseRead() error                   { return nil }
func (s *netConnStream) CloseWrite() error                  { return nil }
func (s *netConnStream) SetDeadline(t time.Time) error      { return s.conn.SetDeadline(t) }
func (s *netConnStream) SetReadDeadline(t time.Time) error  { return s.conn.SetReadDeadline(t) }
func (s *netConnStream) SetWriteDeadline(t time.Time) error { return s.conn.SetWriteDeadline(t) }
func (s *netConnStream) ID() string                         { return "net-conn" }
func (s *netConnStream) Protocol() protocol.ID              { return "" }
func (s *netConnStream) SetProtocol(id protocol.ID) error   { return nil }
func (s *netConnStream) Stat() network.Stats                { return network.Stats{} }
func (s *netConnStream) Conn() network.Conn                 { return nil }
func (s *netConnStream) Scope() network.StreamScope         { return nil }

// A header claiming a large payload must not make ReadMsg reserve that
// payload before it arrives. Each read below gets a header claiming
// maxMessageSize followed by at most 200 KiB and then the end of the
// stream; the bytes allocated per read must stay far below the claim.
func TestStreamRW_ReadMsgAllocatesForReceivedBytesOnly(t *testing.T) {
	for _, sent := range []int{0, 200 * 1024} {
		t.Run(fmt.Sprintf("sent_%d", sent), func(t *testing.T) {
			var header [8]byte
			binary.BigEndian.PutUint32(header[0:4], 1)
			binary.BigEndian.PutUint32(header[4:8], maxMessageSize)
			frame := append(header[:], make([]byte, sent)...)

			const reads = 16
			streams := make([]*mockStream, reads)
			for i := range streams {
				streams[i] = newMockStream()
				streams[i].readBuf.Write(frame)
			}
			wantErr := io.ErrUnexpectedEOF
			if sent == 0 {
				wantErr = io.EOF
			}

			var before, after runtime.MemStats
			runtime.ReadMemStats(&before)
			for _, ms := range streams {
				if _, err := NewStreamRW(ms).ReadMsg(); err != wantErr {
					t.Fatalf("ReadMsg error = %v, want %v", err, wantErr)
				}
			}
			runtime.ReadMemStats(&after)

			perRead := (after.TotalAlloc - before.TotalAlloc) / reads
			if perRead > 1024*1024 {
				t.Fatalf("ReadMsg allocated %d bytes per read for a header claiming %d bytes with %d sent; want at most 1 MiB", perRead, maxMessageSize, sent)
			}
		})
	}
}

// One byte over maxMessageSize is still rejected from the header alone,
// with the same error as before, and nothing after the header is read.
func TestStreamRW_RejectsOneByteOverMax(t *testing.T) {
	ms := newMockStream()
	var header [8]byte
	binary.BigEndian.PutUint32(header[0:4], 1)
	binary.BigEndian.PutUint32(header[4:8], maxMessageSize+1)
	ms.readBuf.Write(header[:])
	sentinel := []byte("payload that must stay unread")
	ms.readBuf.Write(sentinel)

	_, err := NewStreamRW(ms).ReadMsg()
	want := fmt.Sprintf("message too large: %d bytes (max %d)", maxMessageSize+1, maxMessageSize)
	if err == nil || err.Error() != want {
		t.Fatalf("ReadMsg error = %v, want %q", err, want)
	}
	if ms.readBuf.Len() != len(sentinel) {
		t.Fatalf("ReadMsg consumed %d bytes past the header of a rejected frame", len(sentinel)-ms.readBuf.Len())
	}
}

// Payloads from 0 bytes to maxMessageSize, around the first buffer
// boundaries and at the limit, arrive intact when the stream hands them
// over in short reads, and the payload buffer is exactly the payload's
// size.
func TestStreamRW_ReadMsgPayloadSizes(t *testing.T) {
	sizes := []int{
		0, 1,
		initialPayloadBuffer - 1, initialPayloadBuffer, initialPayloadBuffer + 1,
		2*initialPayloadBuffer + 7, 3*initialPayloadBuffer + 7,
		maxMessageSize - 1, maxMessageSize,
	}
	for _, size := range sizes {
		t.Run(fmt.Sprintf("size_%d", size), func(t *testing.T) {
			payload := make([]byte, size)
			for i := range payload {
				payload[i] = byte(i * 31)
			}
			var header [8]byte
			binary.BigEndian.PutUint32(header[0:4], 7)
			binary.BigEndian.PutUint32(header[4:8], uint32(size))
			ms := newMockStream()
			ms.readBuf.Write(header[:])
			ms.readBuf.Write(payload)
			rw := NewStreamRW(&shortReadStream{mockStream: ms, max: 4093})

			msg, err := rw.ReadMsg()
			if err != nil {
				t.Fatal(err)
			}
			if msg.Code != 7 || msg.Size != uint32(size) {
				t.Fatalf("code=%d size=%d, want 7 and %d", msg.Code, msg.Size, size)
			}
			pr := msg.Payload.(*payloadReader)
			if len(pr.data) != size || cap(pr.data) != size {
				t.Fatalf("payload len=%d cap=%d, want both %d", len(pr.data), cap(pr.data), size)
			}
			if !bytes.Equal(pr.data, payload) {
				t.Fatal("payload content mismatch")
			}
		})
	}
}

// A stream that ends partway through a payload, past the first buffer,
// reports io.ErrUnexpectedEOF as it did when the payload was read in one
// piece.
func TestStreamRW_ReadMsgTruncatedPayload(t *testing.T) {
	var header [8]byte
	binary.BigEndian.PutUint32(header[0:4], 1)
	binary.BigEndian.PutUint32(header[4:8], 3*initialPayloadBuffer)
	ms := newMockStream()
	ms.readBuf.Write(header[:])
	ms.readBuf.Write(make([]byte, 2*initialPayloadBuffer))

	if _, err := NewStreamRW(ms).ReadMsg(); err != io.ErrUnexpectedEOF {
		t.Fatalf("ReadMsg error = %v, want %v", err, io.ErrUnexpectedEOF)
	}
}

// shortReadStream returns at most max bytes per Read.
type shortReadStream struct {
	*mockStream
	max int
}

func (s *shortReadStream) Read(p []byte) (int, error) {
	if len(p) > s.max {
		p = p[:s.max]
	}
	return s.mockStream.Read(p)
}
