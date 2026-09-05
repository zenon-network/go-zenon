package legacy

import (
	"bytes"
	"crypto/ecdsa"
	"errors"
	"io"
	"net"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zenon-network/go-zenon/p2p"
	"github.com/zenon-network/go-zenon/p2p/discover"
)

// heldTransport is a peer transport whose reads come from a channel and
// whose writes block on a gate until released, fail with an injected error,
// or, after close, fail at once. It counts writers by message code and how
// many are inside WriteMsg at the same time.
type heldTransport struct {
	reads    chan p2p.Msg
	gate     chan struct{}
	closed   chan struct{}
	once     sync.Once
	writeErr atomic.Value // error returned by WriteMsg when set

	inside  int32 // writers currently inside WriteMsg
	maxIn   int32
	written map[uint64]int32
	mu      sync.Mutex
}

func newHeldTransport() *heldTransport {
	return &heldTransport{
		reads:   make(chan p2p.Msg),
		gate:    make(chan struct{}),
		closed:  make(chan struct{}),
		written: make(map[uint64]int32),
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
	if err, ok := t.writeErr.Load().(error); ok && err != nil {
		return err
	}
	select {
	case <-t.gate:
	case <-t.closed:
		return io.ErrClosedPipe
	}
	t.mu.Lock()
	t.written[msg.Code]++
	t.mu.Unlock()
	return nil
}

func (t *heldTransport) close(error) { t.once.Do(func() { close(t.closed) }) }

func (t *heldTransport) release() { close(t.gate) }

func (t *heldTransport) count(code uint64) int32 {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.written[code]
}

func (t *heldTransport) ping() {
	t.reads <- p2p.Msg{Code: pingMsg, Payload: bytes.NewReader(nil)}
}

// runTestPeer starts a peer over the transport and returns a channel that
// yields run's disconnect reason.
func runTestPeer(t *testing.T, tr *heldTransport) (*Peer, <-chan p2p.DiscReason) {
	t.Helper()
	fd, other := net.Pipe()
	t.Cleanup(func() { fd.Close(); other.Close() })
	c := &conn{fd: fd, transport: tr, id: discover.NodeID{1}, name: "test"}
	p := newPeer(c, nil)
	done := make(chan p2p.DiscReason, 1)
	stopped := make(chan struct{})
	go func() {
		done <- p.run()
		close(stopped)
	}()
	t.Cleanup(func() {
		p.Disconnect(p2p.DiscQuitting)
		select {
		case <-stopped:
		case <-time.After(5 * time.Second):
			t.Error("peer did not stop")
		}
	})
	return p, done
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(time.Millisecond)
	}
}

// Pings arriving while the transport cannot accept a write must not each
// start a writer: one pong is in flight, at most one more is pending, and
// the rest are absorbed. Once writes go through, the pending pong is sent
// and later pings are answered again.
func TestPingsAreAnsweredByOneWriter(t *testing.T) {
	tr := newHeldTransport()
	before := runtime.NumGoroutine()
	_, _ = runTestPeer(t, tr)

	const pings = 200
	for i := 0; i < pings; i++ {
		tr.ping()
	}
	// Every ping has been consumed by the read loop; give any per-ping
	// work a moment to show up.
	time.Sleep(100 * time.Millisecond)

	if got := atomic.LoadInt32(&tr.maxIn); got > 1 {
		t.Fatalf("%d writers entered the transport at once for %d pings", got, pings)
	}
	if grew := runtime.NumGoroutine() - before; grew > 8 {
		t.Fatalf("goroutines grew by %d for %d pings", grew, pings)
	}

	tr.release()
	waitFor(t, "pending pong", func() bool { return tr.count(pongMsg) >= 1 })
	time.Sleep(50 * time.Millisecond)
	if got := tr.count(pongMsg); got > 2 {
		t.Fatalf("%d pongs written for %d pings while the writer was held", got, pings)
	}

	sent := tr.count(pongMsg)
	tr.ping()
	waitFor(t, "pong for a later ping", func() bool { return tr.count(pongMsg) == sent+1 })
}

// A failing pong write ends the peer like a failing ping write does.
func TestPongWriteErrorClosesPeer(t *testing.T) {
	tr := newHeldTransport()
	tr.writeErr.Store(errors.New("write failed"))
	_, done := runTestPeer(t, tr)

	tr.ping()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("peer kept running after the pong write failed")
	}
}

// Disconnecting with a pong stuck in the transport must not hang: closing
// the transport fails the write and the writer stops.
func TestDisconnectWithPendingPong(t *testing.T) {
	tr := newHeldTransport()
	p, done := runTestPeer(t, tr)

	tr.ping()
	tr.ping()
	waitFor(t, "a writer to block", func() bool { return atomic.LoadInt32(&tr.inside) >= 1 })

	p.Disconnect(p2p.DiscQuitting)
	select {
	case reason := <-done:
		if reason != p2p.DiscQuitting {
			t.Fatalf("reason %v", reason)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("peer did not stop with a pending pong")
	}
	waitFor(t, "writers to leave the transport", func() bool { return atomic.LoadInt32(&tr.inside) == 0 })
}
