package server

import (
	"net"
	"sync"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/log"
)

// stopTestTimeout bounds every wait in these tests. A wait that reaches it
// means the server left a codec open or a goroutine blocked.
const stopTestTimeout = 5 * time.Second

type stopProbe struct{}

func (stopProbe) Ping() string { return "pong" }

func newStopTestServer(t *testing.T) *Server {
	t.Helper()
	srv := NewServer()
	if err := srv.RegisterName("probe", stopProbe{}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.Stop)
	return srv
}

// serveTestCodec starts ServeCodec on a fresh in-memory connection and
// returns the server-side codec and a channel closed when ServeCodec returns.
func serveTestCodec(t *testing.T, srv *Server) (ServerCodec, <-chan struct{}) {
	t.Helper()
	p1, p2 := net.Pipe()
	t.Cleanup(func() { _ = p2.Close() })
	codec := NewCodec(p1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		srv.ServeCodec(codec, 0)
	}()
	return codec, done
}

func awaitClose[T any](t *testing.T, ch <-chan T, what string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(stopTestTimeout):
		t.Fatalf("%s did not happen within %v", what, stopTestTimeout)
	}
}

func assertNoCodecs(t *testing.T, srv *Server) {
	t.Helper()
	if n := srv.codecs.Cardinality(); n != 0 {
		t.Fatalf("%d codec(s) still registered after ServeCodec returned", n)
	}
}

// A codec that passes the running check before Stop begins must still be
// closed by Stop, even when it is registered only after Stop has scanned the
// codec set. The seam holds admission between the check and the registration
// while Stop runs to completion (or as far as it can get) on another goroutine.
func TestStopClosesCodecAdmittedDuringShutdown(t *testing.T) {
	srv := newStopTestServer(t)

	reached := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseSeam := func() { releaseOnce.Do(func() { close(release) }) }
	// Registered after the server's Stop cleanup, so it runs first: a
	// failure before the release must not leave Stop waiting on the seam.
	t.Cleanup(releaseSeam)
	srv.beforeRegister = func() {
		close(reached)
		<-release
	}

	codec, done := serveTestCodec(t, srv)
	awaitClose(t, reached, "admission reaching the seam")

	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		srv.Stop()
	}()
	// Give Stop a second to finish while admission is held. It can only
	// finish here if nothing orders it against the registration, which is the
	// defect; a correct server keeps it waiting until the codec is registered,
	// so the hold expires and the release below lets it in. On the defective
	// server the hold is only an upper bound on Stop's scheduling: once Stop
	// has returned, the codec registers after its scan on every run.
	select {
	case <-stopped:
	case <-time.After(time.Second):
	}
	releaseSeam()

	awaitClose(t, stopped, "Stop returning")
	awaitClose(t, codec.closed(), "the codec being closed by Stop")
	awaitClose(t, done, "ServeCodec returning")
	assertNoCodecs(t, srv)
}

// A codec offered after Stop has returned is closed at once and never served.
func TestServeCodecAfterStopClosesCodec(t *testing.T) {
	srv := newStopTestServer(t)
	srv.Stop()

	codec, done := serveTestCodec(t, srv)
	awaitClose(t, done, "ServeCodec returning")
	awaitClose(t, codec.closed(), "the codec being closed")
	assertNoCodecs(t, srv)
}

// A codec registered before Stop is closed by it. This is the case the
// shutdown paths in node/ rely on.
func TestStopClosesRegisteredCodecs(t *testing.T) {
	srv := newStopTestServer(t)

	registered := make(chan struct{})
	srv.beforeRegister = func() { close(registered) }
	codec, done := serveTestCodec(t, srv)
	awaitClose(t, registered, "admission reaching the seam")
	waitRegistered(t, srv, codec)

	srv.Stop()
	awaitClose(t, codec.closed(), "the codec being closed by Stop")
	awaitClose(t, done, "ServeCodec returning")
	assertNoCodecs(t, srv)
}

// reentrantConn is a Conn whose Close calls Stop on the server serving it,
// the way an embedding application's connection type might.
type reentrantConn struct {
	net.Conn
	srv *Server
}

func (c reentrantConn) Close() error {
	c.srv.Stop()
	return c.Conn.Close()
}

func waitRegistered(t *testing.T, srv *Server, codec ServerCodec) {
	t.Helper()
	deadline := time.Now().Add(stopTestTimeout)
	for !srv.codecs.Contains(codec) {
		if time.Now().After(deadline) {
			t.Fatal("codec was not registered")
		}
		time.Sleep(time.Millisecond)
	}
}

// Stop must not hold its lock while it closes codecs: a codec's close runs
// caller-supplied code that may call Stop again. The server here has no Stop
// cleanup, so a Stop that never returns fails the test instead of hanging it.
func TestStopMayBeReenteredFromCodecClose(t *testing.T) {
	srv := NewServer()
	if err := srv.RegisterName("probe", stopProbe{}); err != nil {
		t.Fatal(err)
	}
	p1, p2 := net.Pipe()
	// p1 is normally closed by the codec, but the deadlock this test looks
	// for stops reentrantConn.Close before it reaches the pipe.
	t.Cleanup(func() { _ = p1.Close() })
	t.Cleanup(func() { _ = p2.Close() })
	codec := NewCodec(reentrantConn{Conn: p1, srv: srv})
	done := make(chan struct{})
	go func() {
		defer close(done)
		srv.ServeCodec(codec, 0)
	}()
	waitRegistered(t, srv, codec)

	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		srv.Stop()
	}()
	awaitClose(t, stopped, "Stop returning")
	awaitClose(t, codec.closed(), "the codec being closed by Stop")
	awaitClose(t, done, "ServeCodec returning")
	assertNoCodecs(t, srv)
}

// gatedCloseConn blocks Close until gate is closed, so a Stop that is closing
// the codec over it can be held inside its close loop. entered is closed the
// first time Close is reached, so a test can wait until the hold is in effect.
type gatedCloseConn struct {
	net.Conn
	gate    <-chan struct{}
	entered chan struct{}
	once    *sync.Once
}

func (c gatedCloseConn) Close() error {
	c.once.Do(func() { close(c.entered) })
	<-c.gate
	return c.Conn.Close()
}

// Only the Stop call that moves the server to stopped waits for its closes to
// be issued. A second call, here concurrent with the first, returns at the
// flag while the first is still inside its close loop and the connection is
// still open. The first call is held there by a Conn whose Close blocks; the
// codec's closed channel fires before that, so the far end of the pipe is
// what shows the close has not completed.
func TestSecondStopReturnsBeforeFirstHasClosedCodecs(t *testing.T) {
	srv := NewServer()
	if err := srv.RegisterName("probe", stopProbe{}); err != nil {
		t.Fatal(err)
	}
	p1, p2 := net.Pipe()
	gate := make(chan struct{})
	var openGate sync.Once
	release := func() { openGate.Do(func() { close(gate) }) }
	// Registered after the pipe cleanups, so it runs first: a failure before
	// the release must not leave the first Stop waiting on the gate.
	t.Cleanup(func() { _ = p2.Close() })
	t.Cleanup(release)
	connClosed := make(chan struct{})
	go func() {
		defer close(connClosed)
		_, _ = p2.Read(make([]byte, 1))
	}()
	entered := make(chan struct{})
	codec := NewCodec(gatedCloseConn{Conn: p1, gate: gate, entered: entered, once: new(sync.Once)})
	done := make(chan struct{})
	go func() {
		defer close(done)
		srv.ServeCodec(codec, 0)
	}()
	waitRegistered(t, srv, codec)

	first := make(chan struct{})
	go func() {
		defer close(first)
		srv.Stop()
	}()
	// The first call is inside the gated Close, so it has released the lock
	// and is held in its close loop.
	awaitClose(t, entered, "the first Stop reaching the codec's Close")

	second := make(chan struct{})
	go func() {
		defer close(second)
		srv.Stop()
	}()
	awaitClose(t, second, "the second Stop returning")
	select {
	case <-first:
		t.Fatal("first Stop returned while its codec close was held")
	case <-connClosed:
		t.Fatal("connection was closed while the first Stop was held in its close loop")
	default:
	}

	release()
	awaitClose(t, first, "the first Stop returning")
	awaitClose(t, connClosed, "the connection being closed by the first Stop")
	awaitClose(t, codec.closed(), "the codec being closed by the first Stop")
	awaitClose(t, done, "ServeCodec returning")
	assertNoCodecs(t, srv)
}

// halter is a service whose one method stops the server serving the call.
type halter struct{ srv *Server }

func (h halter) Halt() bool {
	h.srv.Stop()
	return true
}

// Stop called from a method running on a tracked codec closes that codec and
// returns; the call fails on the client because its connection is gone.
func TestStopFromRegisteredMethod(t *testing.T) {
	srv := newStopTestServer(t)
	if err := srv.RegisterName("halt", halter{srv}); err != nil {
		t.Fatal(err)
	}
	client := DialInProc(srv)
	t.Cleanup(client.Close)

	var ok bool
	called := make(chan error, 1)
	go func() { called <- client.Call(&ok, "halt.halt") }()
	select {
	case err := <-called:
		if err == nil {
			t.Fatal("call succeeded on a connection the server closed")
		}
	case <-time.After(stopTestTimeout):
		t.Fatalf("call did not return within %v", stopTestTimeout)
	}
	deadline := time.Now().Add(stopTestTimeout)
	for srv.codecs.Cardinality() != 0 {
		if time.Now().After(deadline) {
			t.Fatal("codec still registered after Stop from a method")
		}
		time.Sleep(time.Millisecond)
	}
}

// Stop logs, and the root logger runs whatever handler the embedding
// application installed, so the log call must not run under Stop's lock
// either. The handler here calls Stop back.
func TestStopMayBeReenteredFromLogHandler(t *testing.T) {
	srv := NewServer()
	if err := srv.RegisterName("probe", stopProbe{}); err != nil {
		t.Fatal(err)
	}
	previous := log.Root().GetHandler()
	t.Cleanup(func() { log.Root().SetHandler(previous) })
	log.Root().SetHandler(log.FuncHandler(func(r *log.Record) error {
		srv.Stop()
		return nil
	}))

	codec, done := serveTestCodec(t, srv)
	waitRegistered(t, srv, codec)

	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		srv.Stop()
	}()
	awaitClose(t, stopped, "Stop returning")
	awaitClose(t, codec.closed(), "the codec being closed by Stop")
	awaitClose(t, done, "ServeCodec returning")
	assertNoCodecs(t, srv)
}
