package server

import (
	"net"
	"sync"
	"testing"
	"time"
)

// --- helpers (self-contained; names prefixed to avoid collision with #112) ---

const issue116TestTimeout = 5 * time.Second

type issue116Probe struct{}

func (issue116Probe) Ping() string { return "pong" }

// issue116StopConn is a Conn whose Close calls Stop on the given server,
// the way an embedding application's connection type might.
type issue116StopConn struct {
	net.Conn
	srv *Server
}

func (c issue116StopConn) Close() error {
	c.srv.Stop()
	return c.Conn.Close()
}

func issue116Await(t *testing.T, ch <-chan interface{}, what string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(issue116TestTimeout):
		t.Fatalf("%s did not happen within %v", what, issue116TestTimeout)
	}
}

func issue116AwaitDone(t *testing.T, ch <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(issue116TestTimeout):
		t.Fatalf("%s did not happen within %v", what, issue116TestTimeout)
	}
}

func issue116WaitRegistered(t *testing.T, srv *Server, codec ServerCodec) {
	t.Helper()
	deadline := time.Now().Add(issue116TestTimeout)
	for !srv.codecs.Contains(codec) {
		if time.Now().After(deadline) {
			t.Fatal("codec was not registered")
		}
		time.Sleep(time.Millisecond)
	}
}

// --- tests ---

// TestPeerCloseReentrantStopNoDeadlock reproduces issue #116: when the peer
// closes the connection, the read error path calls codec.close(), which calls
// conn.Close(). If conn.Close() calls Server.Stop(), Stop iterates all codecs
// and calls close() on the same codec whose Once is currently running —
// deadlock. The fix moves conn.Close() outside the Once and guards it with a
// separate mutex, so the re-entrant close returns immediately.
func TestPeerCloseReentrantStopNoDeadlock(t *testing.T) {
	srv := NewServer()
	if err := srv.RegisterName("probe", issue116Probe{}); err != nil {
		t.Fatal(err)
	}

	p1, p2 := net.Pipe()
	t.Cleanup(func() { _ = p1.Close() })
	t.Cleanup(func() { _ = p2.Close() })

	codec := NewCodec(issue116StopConn{Conn: p1, srv: srv})
	done := make(chan struct{})
	go func() {
		defer close(done)
		srv.ServeCodec(codec, 0)
	}()
	issue116WaitRegistered(t, srv, codec)

	// Peer-initiated close: closing p2 causes the server's read loop to get
	// an error, which triggers codec.close() → conn.Close() → Stop() →
	// codec.close() (re-entrant). Without the fix this deadlocks on the
	// codec's sync.Once.
	_ = p2.Close()

	issue116Await(t, codec.closed(), "the codec being closed after peer close")
	issue116AwaitDone(t, done, "ServeCodec returning after peer close")

	if n := srv.codecs.Cardinality(); n != 0 {
		t.Fatalf("%d codec(s) still registered after ServeCodec returned", n)
	}
}

// TestCodecCloseIdempotent verifies that calling close() multiple times is
// safe and does not panic or deadlock.
func TestCodecCloseIdempotent(t *testing.T) {
	p1, p2 := net.Pipe()
	defer p1.Close()
	defer p2.Close()

	codec := NewCodec(p1).(*jsonCodec)

	var wg sync.WaitGroup
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			codec.close()
		}()
	}
	wg.Wait()

	select {
	case <-codec.closed():
	case <-time.After(issue116TestTimeout):
		t.Fatal("codec.closed() did not fire")
	}
}

// TestPeerCloseNormalConn verifies that a normal (non-reentrant) connection
// still works correctly after the fix: peer close → codec closed →
// ServeCodec returns.
func TestPeerCloseNormalConn(t *testing.T) {
	srv := NewServer()
	if err := srv.RegisterName("probe", issue116Probe{}); err != nil {
		t.Fatal(err)
	}

	p1, p2 := net.Pipe()
	t.Cleanup(func() { _ = p1.Close() })
	t.Cleanup(func() { _ = p2.Close() })

	codec := NewCodec(p1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		srv.ServeCodec(codec, 0)
	}()
	issue116WaitRegistered(t, srv, codec)

	_ = p2.Close()

	issue116Await(t, codec.closed(), "the codec being closed after peer close")
	issue116AwaitDone(t, done, "ServeCodec returning after peer close")

	if n := srv.codecs.Cardinality(); n != 0 {
		t.Fatalf("%d codec(s) still registered after ServeCodec returned", n)
	}
}
