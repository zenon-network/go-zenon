package node

import (
	"bufio"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	rpc "github.com/zenon-network/go-zenon/rpc/server"
)

// The node enables the HTTP request limit and the connection cap by
// default.
func TestRPCConfigDefaultHTTPAdmissionLimits(t *testing.T) {
	if got := DefaultNodeConfig.RPC.MaxConcurrentHTTPRequests; got != 256 || rpc.DefaultMaxConcurrentHTTPRequests != 256 {
		t.Fatalf("default MaxConcurrentHTTPRequests is %d, want 256", got)
	}
	if got := DefaultNodeConfig.RPC.MaxHTTPConnections; got != 1024 || DefaultMaxHTTPConnections != 1024 {
		t.Fatalf("default MaxHTTPConnections is %d, want 1024", got)
	}
}

// Zero selects the default connection cap and a negative value removes it.
func TestEffectiveMaxHTTPConnections(t *testing.T) {
	for _, tc := range []struct{ in, want int }{
		{0, DefaultMaxHTTPConnections},
		{-1, 0},
		{1, 1},
		{5000, 5000},
	} {
		if got := effectiveMaxHTTPConnections(tc.in); got != tc.want {
			t.Errorf("effectiveMaxHTTPConnections(%d) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

// The configured request limit reaches the HTTP RPC server; zero selects the
// default.
func TestEnableRPCTakesConfiguredRequestLimit(t *testing.T) {
	h := newHTTPServer(rpc.DefaultHTTPTimeouts)
	if err := h.enableRPC(nil, httpConfig{MaxConcurrentRequests: 7}); err != nil {
		t.Fatal(err)
	}
	defer h.stop()
	if got := h.httpHandler.Load().(*rpcHandler).server.MaxConcurrentHTTPRequests(); got != 7 {
		t.Fatalf("HTTP server request limit is %d, want 7", got)
	}

	unset := newHTTPServer(rpc.DefaultHTTPTimeouts)
	if err := unset.enableRPC(nil, httpConfig{}); err != nil {
		t.Fatal(err)
	}
	defer unset.stop()
	if got := unset.httpHandler.Load().(*rpcHandler).server.MaxConcurrentHTTPRequests(); got != rpc.DefaultMaxConcurrentHTTPRequests {
		t.Fatalf("unset request limit is %d, want the default %d", got, rpc.DefaultMaxConcurrentHTTPRequests)
	}
}

// startCappedHTTPServer starts an HTTP RPC server on a loopback port with
// the given connection cap and returns its address.
func startCappedHTTPServer(t *testing.T, maxConns int) (*httpServer, string) {
	t.Helper()
	h := newHTTPServer(rpc.DefaultHTTPTimeouts)
	if err := h.setListenAddr("127.0.0.1", 0); err != nil {
		t.Fatal(err)
	}
	if err := h.enableRPC(nil, httpConfig{MaxConnections: maxConns}); err != nil {
		t.Fatal(err)
	}
	if err := h.start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(h.stop)
	return h, h.listenAddr()
}

// sendModules writes an rpc.modules request on conn, keeping the connection
// alive, and returns a reader for the response.
func sendModules(t *testing.T, conn net.Conn) *bufio.Reader {
	t.Helper()
	body := `{"jsonrpc":"2.0","id":1,"method":"rpc.modules","params":[]}`
	req := "POST / HTTP/1.1\r\nHost: 127.0.0.1\r\nContent-Type: application/json\r\n" +
		"Content-Length: " + strconv.Itoa(len(body)) + "\r\n\r\n" + body
	if _, err := conn.Write([]byte(req)); err != nil {
		t.Fatal(err)
	}
	return bufio.NewReader(conn)
}

// With MaxConnections set, a connection beyond the cap is not served while
// the cap is held, including by an idle keep-alive connection, and is
// served once a slot frees.
func TestHTTPMaxConnectionsCapsListener(t *testing.T) {
	_, addr := startCappedHTTPServer(t, 1)

	first, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	resp, err := http.ReadResponse(sendModules(t, first), nil)
	if err != nil {
		t.Fatalf("first connection: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("first connection: status %d", resp.StatusCode)
	}
	// first stays open and idle: it keeps the only slot.

	second, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	reader := sendModules(t, second)
	second.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
	if _, err := reader.ReadByte(); err == nil {
		t.Fatal("connection beyond the cap was served while the cap was held")
	} else if ne, ok := err.(net.Error); !ok || !ne.Timeout() {
		t.Fatalf("connection beyond the cap: got %v, want a read timeout", err)
	}

	first.Close()
	second.SetReadDeadline(time.Now().Add(5 * time.Second))
	resp, err = http.ReadResponse(reader, nil)
	if err != nil {
		t.Fatalf("second connection after the first closed: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("second connection: status %d", resp.StatusCode)
	}
}

// With a negative MaxConnections the listener is not capped. Three
// connections held open at once are all served.
func TestHTTPMaxConnectionsNegativeIsUncapped(t *testing.T) {
	_, addr := startCappedHTTPServer(t, -1)
	var conns []net.Conn
	defer func() {
		for _, c := range conns {
			c.Close()
		}
	}()
	for i := 0; i < 3; i++ {
		c, err := net.Dial("tcp", addr)
		if err != nil {
			t.Fatal(err)
		}
		conns = append(conns, c)
		reader := sendModules(t, c)
		c.SetReadDeadline(time.Now().Add(5 * time.Second))
		resp, err := http.ReadResponse(reader, nil)
		if err != nil {
			t.Fatalf("connection %d: %v", i, err)
		}
		resp.Body.Close()
	}
}

// gzipTailWriter is an http.ResponseWriter that lets the first Write
// through and blocks later ones until unblock is closed. The gzip handler
// writes its header on the first Write of the reply and the compressed body
// and footer when it closes, after the RPC server has returned.
type gzipTailWriter struct {
	header  http.Header
	writes  int32         // atomic
	tail    chan struct{} // closed when a write after the first starts
	once    sync.Once
	unblock chan struct{}
}

func (w *gzipTailWriter) Header() http.Header { return w.header }
func (w *gzipTailWriter) WriteHeader(int)     {}

func (w *gzipTailWriter) Write(p []byte) (int, error) {
	if atomic.AddInt32(&w.writes, 1) > 1 {
		w.once.Do(func() { close(w.tail) })
		<-w.unblock
	}
	return len(p), nil
}

// The HTTP request limit covers the whole handler stack: a request whose
// gzip output is still being written when the RPC server has returned keeps
// its slot until that output is done.
func TestHTTPAdmissionCoversGzipClose(t *testing.T) {
	h := newHTTPServer(rpc.DefaultHTTPTimeouts)
	if err := h.enableRPC(nil, httpConfig{MaxConcurrentRequests: 1}); err != nil {
		t.Fatal(err)
	}
	defer h.stop()
	handler := h.httpHandler.Load().(*rpcHandler).Handler

	newRequest := func(gzip bool) *http.Request {
		req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"rpc.modules","params":[]}`))
		req.Host = "127.0.0.1"
		req.Header.Set("Content-Type", "application/json")
		if gzip {
			req.Header.Set("Accept-Encoding", "gzip")
		}
		return req
	}

	w := &gzipTailWriter{header: make(http.Header), tail: make(chan struct{}), unblock: make(chan struct{})}
	var unblockOnce sync.Once
	unblock := func() { unblockOnce.Do(func() { close(w.unblock) }) }
	defer unblock()
	served := make(chan struct{})
	go func() {
		defer close(served)
		handler.ServeHTTP(w, newRequest(true))
	}()
	select {
	case <-w.tail:
	case <-time.After(5 * time.Second):
		t.Fatal("gzip output did not reach its close-time write")
	}

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, newRequest(false))
	if rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), "-32005") {
		t.Fatalf("request while gzip output holds the only slot: status %d body %q, want 503 with -32005", rec.Code, rec.Body.String())
	}

	unblock()
	select {
	case <-served:
	case <-time.After(5 * time.Second):
		t.Fatal("gzip request did not finish")
	}
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, newRequest(false))
	if rec.Code != http.StatusOK {
		t.Fatalf("request after the gzip output finished: status %d body %q", rec.Code, rec.Body.String())
	}
}

// A connection that has sent only part of its headers holds a slot of the
// cap as well, so slow-header connections cannot grow past it.
func TestHTTPMaxConnectionsCountsSlowHeaders(t *testing.T) {
	_, addr := startCappedHTTPServer(t, 1)

	slow, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer slow.Close()
	if _, err := slow.Write([]byte("POST / HTTP/1.1\r\nHost: 127.0.0.1\r\n")); err != nil {
		t.Fatal(err)
	}

	second, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	reader := sendModules(t, second)
	second.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
	if _, err := reader.ReadByte(); err == nil {
		t.Fatal("connection beyond the cap was served while a slow-header connection held it")
	} else if ne, ok := err.(net.Error); !ok || !ne.Timeout() {
		t.Fatalf("connection beyond the cap: got %v, want a read timeout", err)
	}

	slow.Close()
	second.SetReadDeadline(time.Now().Add(5 * time.Second))
	resp, err := http.ReadResponse(reader, nil)
	if err != nil {
		t.Fatalf("second connection after the slow one closed: %v", err)
	}
	resp.Body.Close()
}

// A server configured with a zero cap runs with the default one: with the
// default lowered to one, a second connection is not served while a first
// holds the cap.
func TestHTTPMaxConnectionsZeroSelectsDefault(t *testing.T) {
	saved := defaultMaxHTTPConnections
	defaultMaxHTTPConnections = 1
	defer func() { defaultMaxHTTPConnections = saved }()

	h, addr := startCappedHTTPServer(t, 0)
	h.mu.Lock()
	got := h.maxConns
	h.mu.Unlock()
	if got != 1 {
		t.Fatalf("listener cap is %d, want the default (lowered to 1)", got)
	}

	first, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	if _, err := first.Write([]byte("POST / HTTP/1.1\r\nHost: 127.0.0.1\r\n")); err != nil {
		t.Fatal(err)
	}
	second, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	reader := sendModules(t, second)
	second.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
	if _, err := reader.ReadByte(); err == nil {
		t.Fatal("zero configuration left the listener uncapped")
	} else if ne, ok := err.(net.Error); !ok || !ne.Timeout() {
		t.Fatalf("connection beyond the default cap: got %v, want a read timeout", err)
	}
	first.Close()
	second.SetReadDeadline(time.Now().Add(5 * time.Second))
	resp, err := http.ReadResponse(reader, nil)
	if err != nil {
		t.Fatalf("second connection after the first closed: %v", err)
	}
	resp.Body.Close()
}
