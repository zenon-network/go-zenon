package server

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const teardownTestTimeout = 5 * time.Second

// scriptedConn is the server side of a net.Pipe whose writes a test can fail
// or hold. The test reads and writes the other end.
type scriptedConn struct {
	net.Conn
	mu      sync.Mutex
	failErr error
	gate    chan struct{}
	writes  int
	closes  atomic.Int32
}

func newScriptedConn(t *testing.T) (*scriptedConn, net.Conn) {
	t.Helper()
	server, client := net.Pipe()
	c := &scriptedConn{Conn: server}
	t.Cleanup(func() { server.Close(); client.Close() })
	return c, client
}

func (c *scriptedConn) Write(p []byte) (int, error) {
	c.mu.Lock()
	err, gate := c.failErr, c.gate
	c.writes++
	c.mu.Unlock()
	if err != nil {
		return 0, err
	}
	if gate != nil {
		<-gate
	}
	return c.Conn.Write(p)
}

func (c *scriptedConn) Close() error {
	c.closes.Add(1)
	return c.Conn.Close()
}

func (c *scriptedConn) failWrites(err error) {
	c.mu.Lock()
	c.failErr = err
	c.mu.Unlock()
}

func (c *scriptedConn) gateWrites() (release func()) {
	gate := make(chan struct{})
	c.mu.Lock()
	c.gate = gate
	c.mu.Unlock()
	var once sync.Once
	return func() { once.Do(func() { close(gate) }) }
}

func (c *scriptedConn) closeCount() int { return int(c.closes.Load()) }

func (c *scriptedConn) writeCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.writes
}

func awaitClosed(t *testing.T, ch <-chan interface{}, what string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(teardownTestTimeout):
		t.Fatalf("%s did not happen within %v", what, teardownTestTimeout)
	}
}

var errTransport = errors.New("transport: write failed")

// A terminal write error tears the codec down: closed() fires, the
// connection is closed exactly once, and later writes are refused with the
// recorded error without touching the connection. Fails if writeJSON stops
// aborting on a transport error, or if a refused write still reaches Write.
func TestTerminalWriteErrorTearsDownCodec(t *testing.T) {
	conn, _ := newScriptedConn(t)
	codec := NewCodec(conn).(*jsonCodec)
	conn.failWrites(errTransport)

	err := codec.writeJSON(context.Background(), &jsonrpcMessage{Version: vsn, Method: "x"})
	if !errors.Is(err, errTransport) {
		t.Fatalf("first write: got %v, want %v", err, errTransport)
	}
	awaitClosed(t, codec.closed(), "closed() after a terminal write error")
	if got := conn.closeCount(); got != 1 {
		t.Fatalf("connection closed %d times, want 1", got)
	}
	writes := conn.writeCount()
	err = codec.writeJSON(context.Background(), &jsonrpcMessage{Version: vsn, Method: "y"})
	if !errors.Is(err, errTransport) {
		t.Fatalf("write after teardown: got %v, want the recorded error %v", err, errTransport)
	}
	if conn.writeCount() != writes {
		t.Fatal("a write after teardown reached the connection")
	}
	codec.close() // the lifecycle's close after an abort is a no-op
	if got := conn.closeCount(); got != 1 {
		t.Fatalf("connection closed %d times after close(), want 1", got)
	}
}

// An error produced while encoding the payload, before any transport I/O,
// is not a transport failure: the codec stays open and the next write works.
// Fails if writeJSON aborts on every error.
func TestMarshalErrorDoesNotTearDownCodec(t *testing.T) {
	conn, client := newScriptedConn(t)
	codec := NewCodec(conn).(*jsonCodec)

	// A channel cannot be marshalled: json.Encoder fails before writing.
	err := codec.writeJSON(context.Background(), map[string]interface{}{"c": make(chan int)})
	var unsupported *json.UnsupportedTypeError
	if !errors.As(err, &unsupported) {
		t.Fatalf("got %v, want *json.UnsupportedTypeError", err)
	}
	select {
	case <-codec.closed():
		t.Fatal("codec was closed by a marshalling error")
	default:
	}
	if conn.closeCount() != 0 {
		t.Fatal("connection was closed by a marshalling error")
	}

	// The connection still carries the next message.
	done := make(chan error, 1)
	go func() { done <- codec.writeJSON(context.Background(), &jsonrpcMessage{Version: vsn, Method: "ok"}) }()
	var msg jsonrpcMessage
	if err := json.NewDecoder(client).Decode(&msg); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if msg.Method != "ok" {
		t.Fatalf("got method %q, want ok", msg.Method)
	}
}

// stopOnCloseConn is a scriptedConn whose Close calls Server.Stop, the way an
// embedding application's connection type might (issue #116). Stop then
// closes every registered codec, including the one whose abort is running.
type stopOnCloseConn struct {
	*scriptedConn
	srv *Server
}

func (c *stopOnCloseConn) Close() error {
	c.srv.Stop()
	return c.scriptedConn.Close()
}

// abort must survive Stop being re-entered from Conn.Close: the re-entered
// close of the same codec returns at once instead of blocking. Fails if
// abort holds any lock that close needs, or waits for anything.
func TestAbortReentrantStopNoDeadlock(t *testing.T) {
	srv := NewServer()
	if err := srv.RegisterName("test", issue116Probe{}); err != nil {
		t.Fatal(err)
	}
	inner, _ := newScriptedConn(t)
	conn := &stopOnCloseConn{scriptedConn: inner, srv: srv}
	codec := NewCodec(conn).(*jsonCodec)
	served := make(chan struct{})
	go func() {
		srv.ServeCodec(codec, 0)
		close(served)
	}()
	issue116WaitRegistered(t, srv, codec)

	inner.failWrites(errTransport)
	aborted := make(chan struct{})
	go func() {
		codec.writeJSON(context.Background(), &jsonrpcMessage{Version: vsn, Method: "x"})
		close(aborted)
	}()
	issue116AwaitDone(t, aborted, "writeJSON with a failing transport")
	issue116AwaitDone(t, served, "ServeCodec return after abort")
	if got := inner.closeCount(); got != 1 {
		t.Fatalf("connection closed %d times, want 1", got)
	}
}

// While one goroutine's write fails and aborts the codec, other writers
// either complete or get an error; none of them starts a transport write
// after the failure was recorded, and the connection is closed once.
// Run with -race -count=20.
func TestConcurrentWritesDuringAbort(t *testing.T) {
	conn, client := newScriptedConn(t)
	codec := NewCodec(conn).(*jsonCodec)
	go func() { // drain whatever reaches the test end
		buf := make([]byte, 4096)
		for {
			if _, err := client.Read(buf); err != nil {
				return
			}
		}
	}()

	const writers = 8
	var wg sync.WaitGroup
	results := make(chan error, writers)
	start := make(chan struct{})
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			results <- codec.writeJSON(context.Background(), &jsonrpcMessage{Version: vsn, Method: "m"})
		}()
	}
	conn.failWrites(errTransport)
	close(start)
	wg.Wait()
	close(results)

	awaitClosed(t, codec.closed(), "closed() after concurrent failing writes")
	if got := conn.closeCount(); got != 1 {
		t.Fatalf("connection closed %d times, want 1", got)
	}
	for err := range results {
		if !errors.Is(err, errTransport) {
			t.Fatalf("a concurrent writer got %v, want %v", err, errTransport)
		}
	}
}

// failingResponseWriter is an http.ResponseWriter whose Write fails, the
// way a response to a client that went away does.
type failingResponseWriter struct {
	header http.Header
}

func (w *failingResponseWriter) Header() http.Header       { return w.header }
func (w *failingResponseWriter) Write([]byte) (int, error) { return 0, errTransport }
func (w *failingResponseWriter) WriteHeader(int)           {}

// A failed response write on the HTTP single-request codec aborts the codec
// (closed() fires, the no-op Close runs) without panicking or hanging.
func TestAbortOnHTTPServerConnIsHarmless(t *testing.T) {
	srv := NewServer()
	if err := srv.RegisterName("test", issue116Probe{}); err != nil {
		t.Fatal(err)
	}
	body := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"test.ping"}`)
	req := httptest.NewRequest("POST", "/", body)
	codec := newHTTPServerConn(req, &failingResponseWriter{header: http.Header{}})
	done := make(chan struct{})
	go func() {
		srv.serveSingleRequest(context.Background(), codec)
		close(done)
	}()
	issue116AwaitDone(t, done, "serveSingleRequest with a failing writer")
	awaitClosed(t, codec.closed(), "closed() after the failed response write")
}

// A client whose connection fails to write gets an error from Call and can
// still Close; the codec's abort and the client's dispatch loop do not wait
// on each other.
func TestClientWriteFailureDoesNotDeadlock(t *testing.T) {
	conn, _ := newScriptedConn(t)
	conn.failWrites(errTransport)
	codec := NewCodec(conn)
	client := initClient(codec, randomIDGenerator(), new(serviceRegistry), 0)

	ctx, cancel := context.WithTimeout(context.Background(), teardownTestTimeout)
	defer cancel()
	var result string
	if err := client.CallContext(ctx, &result, "test.ping"); err == nil {
		t.Fatal("Call over a failed connection returned no error")
	}
	done := make(chan struct{})
	go func() {
		client.Close()
		close(done)
	}()
	issue116AwaitDone(t, done, "client Close after a write failure")
}
