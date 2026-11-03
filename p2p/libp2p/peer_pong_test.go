package libp2p

import (
	"encoding/binary"
	"errors"
	"io"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/protocol"

	"github.com/zenon-network/go-zenon/p2p"
	"github.com/zenon-network/go-zenon/p2p/discover"
)

const pongTestTimeout = 5 * time.Second

// heldStream is a network.Stream whose reads come from a channel of raw
// frames and whose writes announce the message code of every frame
// header on entry and then wait for a token, fail with an injected error,
// or, after close, fail at once. The payload write that follows a header
// passes straight through, so one token releases one whole message.
type heldStream struct {
	frames   chan []byte
	entered  chan uint64   // message code of every frame as its header enters
	allow    chan struct{} // one token lets one waiting header write complete
	closed   chan struct{}
	once     sync.Once
	writeErr atomic.Value // error returned by Write when set

	rmu     sync.Mutex
	pending []byte // unread bytes of the frame being delivered

	wmu       sync.Mutex
	remaining int // payload bytes still expected for the current frame

	wdeadline atomic.Value  // time.Time; a held write fails when it passes
	wdset     chan struct{} // signalled on every SetWriteDeadline
}

// writeDeadline returns a channel that fires at the current write
// deadline, or nil (never fires) when none is set. A held write re-reads
// the deadline whenever it is set again, as a yamux stream does, so a
// deadline moved while a write is held applies to that write.
func (s *heldStream) writeDeadline() <-chan time.Time {
	d, ok := s.wdeadline.Load().(time.Time)
	if !ok || d.IsZero() {
		return nil
	}
	return time.After(time.Until(d))
}

func newHeldStream() *heldStream {
	return &heldStream{
		frames:  make(chan []byte),
		entered: make(chan uint64, 1024),
		allow:   make(chan struct{}),
		closed:  make(chan struct{}),
		wdset:   make(chan struct{}, 1),
	}
}

func (s *heldStream) Read(p []byte) (int, error) {
	s.rmu.Lock()
	defer s.rmu.Unlock()
	for len(s.pending) == 0 {
		select {
		case frame := <-s.frames:
			s.pending = frame
		case <-s.closed:
			return 0, io.EOF
		}
	}
	n := copy(p, s.pending)
	s.pending = s.pending[n:]
	return n, nil
}

func (s *heldStream) Write(p []byte) (int, error) {
	s.wmu.Lock()
	defer s.wmu.Unlock()
	select {
	case <-s.closed:
		return 0, io.ErrClosedPipe
	default:
	}
	if s.remaining > 0 {
		// payload of a frame whose header was already released
		s.remaining -= len(p)
		return len(p), nil
	}
	if len(p) < 8 {
		return 0, errors.New("heldStream: write shorter than a frame header")
	}
	s.remaining = int(binary.BigEndian.Uint32(p[4:8]))
	s.entered <- uint64(binary.BigEndian.Uint32(p[0:4]))
	if err, ok := s.writeErr.Load().(error); ok && err != nil {
		s.remaining = 0
		return 0, err
	}
	for {
		select {
		case <-s.allow:
			return len(p), nil
		case <-s.closed:
			s.remaining = 0
			return 0, io.ErrClosedPipe
		case <-s.writeDeadline():
			s.remaining = 0
			return 0, errors.New("heldStream: write deadline exceeded")
		case <-s.wdset:
		}
	}
}

func (s *heldStream) Close() error                       { s.once.Do(func() { close(s.closed) }); return nil }
func (s *heldStream) Reset() error                       { return s.Close() }
func (s *heldStream) CloseRead() error                   { return nil }
func (s *heldStream) CloseWrite() error                  { return nil }
func (s *heldStream) SetDeadline(t time.Time) error      { return nil }
func (s *heldStream) SetReadDeadline(t time.Time) error  { return nil }
func (s *heldStream) SetWriteDeadline(t time.Time) error { return s.setWriteDeadline(t) }
func (s *heldStream) ID() string                         { return "held" }
func (s *heldStream) Protocol() protocol.ID              { return "" }
func (s *heldStream) SetProtocol(id protocol.ID) error   { return nil }
func (s *heldStream) Stat() network.Stats                { return network.Stats{} }
func (s *heldStream) Conn() network.Conn                 { return nil }
func (s *heldStream) Scope() network.StreamScope         { return nil }

func (s *heldStream) setWriteDeadline(t time.Time) error {
	s.wdeadline.Store(t)
	select {
	case s.wdset <- struct{}{}:
	default:
	}
	return nil
}

// deliver hands a base-protocol message with an empty list payload to the
// read loop. Because the read loop handles each message before reading
// the next, a return from deliver means every message delivered before
// this one has been handled completely.
func (s *heldStream) deliver(tb testing.TB, code uint64) {
	tb.Helper()
	frame, err := buildRawMessage(code, []interface{}{})
	if err != nil {
		tb.Fatal(err)
	}
	select {
	case s.frames <- frame:
	case <-time.After(pongTestTimeout):
		tb.Fatalf("read loop did not accept message %d", code)
	}
}

// release lets the header write currently waiting in the stream complete.
func (s *heldStream) release(tb testing.TB) {
	tb.Helper()
	select {
	case s.allow <- struct{}{}:
	case <-time.After(pongTestTimeout):
		tb.Fatal("no write was waiting to be released")
	}
}

func expectEntered(tb testing.TB, entered <-chan uint64, want uint64) {
	tb.Helper()
	select {
	case got := <-entered:
		if got != want {
			tb.Fatalf("write of message %d entered the stream, want %d", got, want)
		}
	case <-time.After(pongTestTimeout):
		tb.Fatalf("no write of message %d entered the stream", want)
	}
}

func expectNothingEntered(tb testing.TB, entered <-chan uint64) {
	tb.Helper()
	select {
	case got := <-entered:
		tb.Fatalf("unexpected write of message %d entered the stream", got)
	default:
	}
}

// runPongTestPeer starts a peer over the held stream and returns a channel
// that yields run's disconnect reason. The peer's periodic ping ticks come
// from a channel the test never fires, so only the test's own messages
// reach the stream.
func runPongTestPeer(t *testing.T, s *heldStream) (*Peer, <-chan p2p.DiscReason) {
	t.Helper()
	return runPongTestPeerWithTicks(t, s, make(chan time.Time))
}

// runPongTestPeerWithTicks is runPongTestPeer with the test supplying the
// periodic ping ticks.
func runPongTestPeerWithTicks(t *testing.T, s *heldStream, pingC <-chan time.Time) (*Peer, <-chan p2p.DiscReason) {
	t.Helper()
	p := newPeerFromStream(NewStreamRW(s), s, discover.NodeID{1}, nil, "test", nil)
	p.pingC = pingC
	done := make(chan p2p.DiscReason, 1)
	stopped := make(chan struct{})
	go func() {
		done <- p.run()
		close(stopped)
	}()
	t.Cleanup(func() {
		// Close the stream first: the read loop ends with EOF and any
		// write still held in the stream fails at once, so run does not
		// sit in its disconnect write until discWriteTimeout.
		s.Close()
		go p.Disconnect(p2p.DiscQuitting)
		select {
		case <-stopped:
		case <-time.After(pongTestTimeout):
			t.Error("peer did not stop")
		}
	})
	return p, done
}

func expectPeerReason(t *testing.T, done <-chan p2p.DiscReason, want p2p.DiscReason) {
	t.Helper()
	select {
	case got := <-done:
		if got != want {
			t.Fatalf("run returned %v, want %v", got, want)
		}
	case <-time.After(pongTestTimeout):
		t.Fatal("run did not return")
	}
}

// Pings arriving while the stream cannot accept a write must not each
// start a writer. The peer's goroutine count after a flood of pings
// behind a held pong must be what it was before the flood, within the
// handful the runtime itself may add, and once writes go through exactly
// one more pong is written for the whole flood.
func TestPingFloodDoesNotSpawnWriters(t *testing.T) {
	s := newHeldStream()
	_, _ = runPongTestPeer(t, s)

	s.deliver(t, pingMsg)
	expectEntered(t, s.entered, pongMsg) // this pong is now held in the stream

	before := runtime.NumGoroutine()
	const pings = 200
	for i := 0; i < pings; i++ {
		s.deliver(t, pingMsg)
	}
	s.deliver(t, getPeersMsg) // barrier: every ping above has been handled
	grew := runtime.NumGoroutine() - before
	if grew > 4 {
		t.Fatalf("%d goroutines appeared for %d pings behind a held write; pongs must not each get a writer", grew, pings)
	}
	expectNothingEntered(t, s.entered)

	s.release(t) // the held pong completes
	expectEntered(t, s.entered, pongMsg)
	s.release(t) // the one coalesced pong completes
	time.Sleep(20 * time.Millisecond)
	expectNothingEntered(t, s.entered)

	// A later ping is answered by a fresh pong.
	s.deliver(t, pingMsg)
	expectEntered(t, s.entered, pongMsg)
	s.release(t)
}

// A periodic ping tick that fires while a pong is held and another is
// pending is written by the same single writer, in either order, and does
// not lose or duplicate the pending pong.
func TestPingTickWhilePongPending(t *testing.T) {
	s := newHeldStream()
	pingC := make(chan time.Time, 1)
	p, _ := runPongTestPeerWithTicks(t, s, pingC)

	s.deliver(t, pingMsg)
	expectEntered(t, s.entered, pongMsg) // held
	s.deliver(t, pingMsg)
	s.deliver(t, pingMsg)
	s.deliver(t, getPeersMsg) // barrier
	if got := len(p.pongPending); got != 1 {
		t.Fatalf("%d pongs pending, want 1", got)
	}

	pingC <- time.Now() // the keepalive tick fires while the pong write is held
	expectNothingEntered(t, s.entered)

	s.release(t) // the held pong completes
	var wrote []uint64
	for i := 0; i < 2; i++ {
		select {
		case code := <-s.entered:
			wrote = append(wrote, code)
		case <-time.After(pongTestTimeout):
			t.Fatalf("only %v entered the stream after the held pong, want a ping and a pong", wrote)
		}
		s.release(t)
	}
	pingThenPong := wrote[0] == pingMsg && wrote[1] == pongMsg
	pongThenPing := wrote[0] == pongMsg && wrote[1] == pingMsg
	if !pingThenPong && !pongThenPing {
		t.Fatalf("writes after the held pong were %v, want one ping and one pong", wrote)
	}
	time.Sleep(20 * time.Millisecond)
	expectNothingEntered(t, s.entered)
	if got := len(p.pongPending); got != 0 {
		t.Fatalf("%d pongs still pending, want 0", got)
	}
	if got := len(pingC); got != 0 {
		t.Fatalf("%d ticks still pending, want 0", got)
	}

	// The peer is still live: a later ping is answered.
	s.deliver(t, pingMsg)
	expectEntered(t, s.entered, pongMsg)
	s.release(t)
}

// A failing pong write ends the peer with the same reason a failing ping
// write produces instead of being ignored.
func TestPongWriteErrorEndsPeer(t *testing.T) {
	s := newHeldStream()
	writeErr := errors.New("write failed")
	s.writeErr.Store(writeErr)
	_, done := runPongTestPeer(t, s)

	s.deliver(t, pingMsg)
	expectEntered(t, s.entered, pongMsg)
	expectPeerReason(t, done, p2p.DiscReasonForError(writeErr))
}
