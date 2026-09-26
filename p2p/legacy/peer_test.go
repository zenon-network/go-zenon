package legacy

import (
	"bytes"
	"crypto/ecdsa"
	"errors"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zenon-network/go-zenon/p2p"
	"github.com/zenon-network/go-zenon/p2p/discover"
)

const testTimeout = 5 * time.Second

// heldTransport is a peer transport whose reads come from a channel and
// whose writes announce their message code on entry and then wait for a
// token, fail with an injected error, or, after close, fail at once. It
// counts how many writers are inside WriteMsg at the same time.
type heldTransport struct {
	reads    chan p2p.Msg
	entered  chan uint64   // message code of every write as it enters
	allow    chan struct{} // one token lets one waiting write complete
	closed   chan struct{}
	once     sync.Once
	writeErr atomic.Value // error returned by WriteMsg when set

	inside int32 // writers currently inside WriteMsg
	maxIn  int32
}

func newHeldTransport() *heldTransport {
	return &heldTransport{
		reads:   make(chan p2p.Msg),
		entered: make(chan uint64, 1024),
		allow:   make(chan struct{}),
		closed:  make(chan struct{}),
	}
}

func (t *heldTransport) doEncHandshake(*ecdsa.PrivateKey, *discover.Node) (discover.NodeID, error) {
	return discover.NodeID{}, nil
}
func (t *heldTransport) doProtoHandshake(*protoHandshake) (*protoHandshake, error) { return nil, nil }

func (t *heldTransport) ReadMsg() (p2p.Msg, error) {
	select {
	case msg := <-t.reads:
		return msg, nil
	case <-t.closed:
		return p2p.Msg{}, io.EOF
	}
}

func (t *heldTransport) WriteMsg(msg p2p.Msg) error {
	n := atomic.AddInt32(&t.inside, 1)
	defer atomic.AddInt32(&t.inside, -1)
	for {
		old := atomic.LoadInt32(&t.maxIn)
		if n <= old || atomic.CompareAndSwapInt32(&t.maxIn, old, n) {
			break
		}
	}
	t.entered <- msg.Code
	if err, ok := t.writeErr.Load().(error); ok && err != nil {
		return err
	}
	select {
	case <-t.allow:
		return nil
	case <-t.closed:
		return io.ErrClosedPipe
	}
}

func (t *heldTransport) close(error) { t.once.Do(func() { close(t.closed) }) }

// release lets the write currently waiting in the transport complete.
func (t *heldTransport) release(tb testing.TB) {
	tb.Helper()
	select {
	case t.allow <- struct{}{}:
	case <-time.After(testTimeout):
		tb.Fatal("no write was waiting to be released")
	}
}

// deliver hands a message to the read loop. Because the read loop handles
// each message before reading the next, a return from deliver means every
// message delivered before this one has been handled completely.
func (t *heldTransport) deliver(tb testing.TB, code uint64) {
	tb.Helper()
	select {
	case t.reads <- p2p.Msg{Code: code, Payload: bytes.NewReader(nil)}:
	case <-time.After(testTimeout):
		tb.Fatalf("read loop did not accept message %d", code)
	}
}

// expectEntry waits for the next write to enter the transport and checks
// its message code.
func expectEntry(tb testing.TB, entered <-chan uint64, want uint64) {
	tb.Helper()
	select {
	case got := <-entered:
		if got != want {
			tb.Fatalf("write of message %d entered the transport, want %d", got, want)
		}
	case <-time.After(testTimeout):
		tb.Fatalf("no write of message %d entered the transport", want)
	}
}

func expectNoEntry(tb testing.TB, entered <-chan uint64) {
	tb.Helper()
	select {
	case got := <-entered:
		tb.Fatalf("unexpected write of message %d entered the transport", got)
	default:
	}
}

// runTestPeer starts a peer over the transport and returns a channel that
// yields run's disconnect reason. pingC supplies the periodic ping ticks;
// a nil pingC means the test wants none, so the real ticker is replaced
// by a channel that never fires.
func runTestPeer(t *testing.T, tr transport, pingC <-chan time.Time, caps []p2p.Cap, protocols []p2p.Protocol) (*Peer, <-chan p2p.DiscReason) {
	t.Helper()
	if pingC == nil {
		pingC = make(chan time.Time)
	}
	fd, other := net.Pipe()
	t.Cleanup(func() { _ = fd.Close(); _ = other.Close() })
	c := &conn{fd: fd, transport: tr, id: discover.NodeID{1}, name: "test", caps: caps}
	p := newPeer(c, protocols)
	p.pingC = pingC
	done := make(chan p2p.DiscReason, 1)
	stopped := make(chan struct{})
	go func() {
		done <- p.run()
		close(stopped)
	}()
	t.Cleanup(func() {
		// Disconnect blocks while run is past its loop but not yet
		// closed, which a failed shutdown assertion can leave behind;
		// send it from a goroutine so cleanup itself stays bounded.
		go p.Disconnect(p2p.DiscQuitting)
		select {
		case <-stopped:
		case <-time.After(testTimeout):
			t.Error("peer did not stop")
		}
	})
	return p, done
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(testTimeout)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(time.Millisecond)
	}
}

func expectReason(t *testing.T, done <-chan p2p.DiscReason, want p2p.DiscReason) {
	t.Helper()
	select {
	case got := <-done:
		if got != want {
			t.Fatalf("run returned %v, want %v", got, want)
		}
	case <-time.After(testTimeout):
		t.Fatal("run did not return")
	}
}

func expectRunning(t *testing.T, done <-chan p2p.DiscReason) {
	t.Helper()
	select {
	case got := <-done:
		t.Fatalf("run returned %v early", got)
	default:
	}
}

// Pings arriving while the transport cannot accept a write must not each
// start a writer: one pong is in flight, at most one more is pending, and
// the rest are absorbed by that pending pong. Once writes go through, the
// pending pong is written, nothing else is, and later pings are answered
// again.
//
// Every step is a barrier: deliver returns only after the read loop has
// handled all earlier messages, entered reports each write as it starts,
// and release hands exactly one write its completion. The pending slot is
// inspected directly.
func TestPingsAreAnsweredByOneWriter(t *testing.T) {
	tr := newHeldTransport()
	p, _ := runTestPeer(t, tr, nil, nil, nil)

	tr.deliver(t, pingMsg)
	expectEntry(t, tr.entered, pongMsg)

	const pings = 200
	for i := 0; i < pings; i++ {
		tr.deliver(t, pingMsg)
	}
	tr.deliver(t, getPeersMsg) // barrier: every ping above has been handled

	if got := atomic.LoadInt32(&tr.maxIn); got != 1 {
		t.Fatalf("%d writers entered the transport at once for %d pings", got, pings+1)
	}
	expectNoEntry(t, tr.entered)
	if got := len(p.pongPending); got != 1 {
		t.Fatalf("%d pongs pending for %d pings behind a held write, want 1", got, pings)
	}

	tr.release(t) // the pong in flight completes
	expectEntry(t, tr.entered, pongMsg)
	if got := len(p.pongPending); got != 0 {
		t.Fatalf("%d pongs still pending after the coalesced pong started, want 0", got)
	}
	tr.release(t) // the coalesced pong completes
	waitFor(t, "the writer to leave the transport", func() bool { return atomic.LoadInt32(&tr.inside) == 0 })
	expectNoEntry(t, tr.entered)

	// A later ping is answered by a fresh pong.
	tr.deliver(t, pingMsg)
	expectEntry(t, tr.entered, pongMsg)
	tr.release(t)
	waitFor(t, "the writer to leave the transport", func() bool { return atomic.LoadInt32(&tr.inside) == 0 })
	expectNoEntry(t, tr.entered)
	if got := atomic.LoadInt32(&tr.maxIn); got != 1 {
		t.Fatalf("%d writers entered the transport at once", got)
	}
}

// A periodic ping tick that fires while a pong is held and another is
// pending is written by the same single writer, in either order, and does
// not lose or duplicate the pending pong.
func TestPingTickWhilePongIsPending(t *testing.T) {
	tr := newHeldTransport()
	pingC := make(chan time.Time, 1)
	p, _ := runTestPeer(t, tr, pingC, nil, nil)

	tr.deliver(t, pingMsg)
	expectEntry(t, tr.entered, pongMsg)
	tr.deliver(t, pingMsg)
	tr.deliver(t, pingMsg)
	tr.deliver(t, getPeersMsg) // barrier
	if got := len(p.pongPending); got != 1 {
		t.Fatalf("%d pongs pending, want 1", got)
	}

	pingC <- time.Now() // the keepalive tick fires while the pong write is held
	expectNoEntry(t, tr.entered)

	tr.release(t) // the held pong completes
	var wrote []uint64
	for i := 0; i < 2; i++ {
		select {
		case code := <-tr.entered:
			wrote = append(wrote, code)
		case <-time.After(testTimeout):
			t.Fatalf("only %v entered the transport after the held pong, want a ping and a pong", wrote)
		}
		tr.release(t)
	}
	pingThenPong := wrote[0] == pingMsg && wrote[1] == pongMsg
	pongThenPing := wrote[0] == pongMsg && wrote[1] == pingMsg
	if !pingThenPong && !pongThenPing {
		t.Fatalf("writes after the held pong were %v, want one ping and one pong", wrote)
	}
	waitFor(t, "the writer to leave the transport", func() bool { return atomic.LoadInt32(&tr.inside) == 0 })
	expectNoEntry(t, tr.entered)
	if got := len(p.pongPending); got != 0 {
		t.Fatalf("%d pongs still pending, want 0", got)
	}
	if got := len(pingC); got != 0 {
		t.Fatalf("%d ticks still pending, want 0", got)
	}
	if got := atomic.LoadInt32(&tr.maxIn); got != 1 {
		t.Fatalf("%d writers entered the transport at once", got)
	}

	// The peer is still live: a later ping is answered.
	tr.deliver(t, pingMsg)
	expectEntry(t, tr.entered, pongMsg)
	tr.release(t)
}

// A failing pong write ends the peer with the same reason a failing ping
// write produces: both reach run through the protocol-error channel.
func TestPongWriteErrorClosesPeer(t *testing.T) {
	tr := newHeldTransport()
	writeErr := errors.New("write failed")
	tr.writeErr.Store(writeErr)
	_, done := runTestPeer(t, tr, nil, nil, nil)

	tr.deliver(t, pingMsg)
	expectEntry(t, tr.entered, pongMsg)
	expectReason(t, done, p2p.DiscReasonForError(writeErr))
}

// Disconnecting with a pong stuck in a transport whose close fails the
// stuck write at once: run returns the requested reason and the writer
// leaves the transport. This covers the peer's cleanup, not the timing of
// rlpx.close, which waits for the write lock; see
// TestDisconnectWaitsForWritesAheadOfClose for that.
func TestDisconnectWithPendingPong(t *testing.T) {
	tr := newHeldTransport()
	p, done := runTestPeer(t, tr, nil, nil, nil)

	tr.deliver(t, pingMsg)
	expectEntry(t, tr.entered, pongMsg)
	tr.deliver(t, pingMsg)
	tr.deliver(t, getPeersMsg) // barrier: a second pong is pending

	p.Disconnect(p2p.DiscQuitting)
	expectReason(t, done, p2p.DiscQuitting)
	waitFor(t, "writers to leave the transport", func() bool { return atomic.LoadInt32(&tr.inside) == 0 })
	expectNoEntry(t, tr.entered)
}

// fifoLock is a mutex that hands off to waiters in arrival order and
// reports how many acquisitions have been requested, so a test can fix
// and observe the order of the writers queued on it.
type fifoLock struct {
	mu      sync.Mutex
	cond    *sync.Cond
	next    uint64 // tickets issued
	serving uint64 // ticket currently holding the lock
}

func newFifoLock() *fifoLock {
	l := &fifoLock{}
	l.cond = sync.NewCond(&l.mu)
	return l
}

func (l *fifoLock) Lock() {
	l.mu.Lock()
	ticket := l.next
	l.next++
	for l.serving != ticket {
		l.cond.Wait()
	}
	l.mu.Unlock()
}

func (l *fifoLock) Unlock() {
	l.mu.Lock()
	l.serving++
	l.cond.Broadcast()
	l.mu.Unlock()
}

func (l *fifoLock) requested() uint64 {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.next
}

var errWriteTimeout = errors.New("write deadline exceeded")

// lockedTransport models the shutdown-relevant part of rlpx: WriteMsg and
// close take the same write lock, a write holding the lock ends only when
// its deadline expires (the test expires it), and close cannot close the
// connection until it holds the lock. The lock is handed off in arrival
// order so the test can force the writers to acquire it before close;
// sync.Mutex gives no such guarantee, so this is one valid schedule, not
// a model of which waiter wins in production.
type lockedTransport struct {
	reads   chan p2p.Msg
	lock    *fifoLock
	entered chan uint64   // code of each write once it holds the lock
	expire  chan struct{} // one receive ends the write holding the lock with a timeout
	closing chan struct{} // closed when close is called
	closed  chan struct{} // closed when close holds the lock
	once    sync.Once
}

func newLockedTransport() *lockedTransport {
	return &lockedTransport{
		reads:   make(chan p2p.Msg),
		lock:    newFifoLock(),
		entered: make(chan uint64, 16),
		expire:  make(chan struct{}),
		closing: make(chan struct{}),
		closed:  make(chan struct{}),
	}
}

func (t *lockedTransport) doEncHandshake(*ecdsa.PrivateKey, *discover.Node) (discover.NodeID, error) {
	return discover.NodeID{}, nil
}
func (t *lockedTransport) doProtoHandshake(*protoHandshake) (*protoHandshake, error) {
	return nil, nil
}

func (t *lockedTransport) ReadMsg() (p2p.Msg, error) {
	select {
	case msg := <-t.reads:
		return msg, nil
	case <-t.closed:
		return p2p.Msg{}, io.EOF
	}
}

func (t *lockedTransport) WriteMsg(msg p2p.Msg) error {
	t.lock.Lock()
	defer t.lock.Unlock()
	t.entered <- msg.Code
	<-t.expire
	return errWriteTimeout
}

func (t *lockedTransport) close(error) {
	t.once.Do(func() {
		close(t.closing)
		t.lock.Lock()
		defer t.lock.Unlock()
		close(t.closed)
	})
}

func (t *lockedTransport) deliver(tb testing.TB, code uint64) {
	tb.Helper()
	select {
	case t.reads <- p2p.Msg{Code: code, Payload: bytes.NewReader(nil)}:
	case <-time.After(testTimeout):
		tb.Fatalf("read loop did not accept message %d", code)
	}
}

// expireWrite ends the write that holds the lock as if its deadline had
// passed.
func (t *lockedTransport) expireWrite(tb testing.TB) {
	tb.Helper()
	select {
	case t.expire <- struct{}{}:
	case <-time.After(testTimeout):
		tb.Fatal("no write was holding the lock")
	}
}

// With a transport whose close needs the write lock, as rlpx does,
// Disconnect returns from run only after every write that acquires the
// lock before close has ended, each by its own deadline: here a pong
// holding the lock and a protocol write that is made to acquire it next.
// This is the pre-existing shutdown bound; the change routes pongs through
// the keepalive writer and adds no writer of its own.
func TestDisconnectWaitsForWritesAheadOfClose(t *testing.T) {
	tr := newLockedTransport()
	startWrite := make(chan struct{})
	proto := p2p.Protocol{
		Name:    "tst",
		Version: 1,
		Length:  1,
		Run: func(_ p2p.Peer, rw p2p.MsgReadWriter) error {
			<-startWrite
			return p2p.Send(rw, 0, "payload")
		},
	}
	p, done := runTestPeer(t, tr, nil, []p2p.Cap{proto.Cap()}, []p2p.Protocol{proto})

	// A pong holds the write lock.
	tr.deliver(t, pingMsg)
	expectEntry(t, tr.entered, pongMsg)

	// A protocol write queues behind it.
	close(startWrite)
	waitFor(t, "the protocol write to queue on the lock", func() bool { return tr.lock.requested() == 2 })

	// close is made to acquire the lock after both.
	p.Disconnect(p2p.DiscQuitting)
	select {
	case <-tr.closing:
	case <-time.After(testTimeout):
		t.Fatal("run did not call close after Disconnect")
	}
	waitFor(t, "close to queue on the lock", func() bool { return tr.lock.requested() == 3 })
	expectRunning(t, done)

	tr.expireWrite(t)                              // the pong's deadline passes
	expectEntry(t, tr.entered, baseProtocolLength) // the protocol write holds the lock
	expectRunning(t, done)

	tr.expireWrite(t) // the protocol write's deadline passes; close takes the lock
	expectReason(t, done, p2p.DiscQuitting)
	select {
	case <-tr.closed:
	default:
		t.Fatal("run returned before the transport was closed")
	}
	expectNoEntry(t, tr.entered)
}
