package server

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// admissionTestService has methods that hold an admission slot in the
// different ways a request can end: released by the test, by a panic, or by
// its context ending.
type admissionTestService struct {
	running  int32 // atomic: calls inside Block or WaitCtx
	maxSeen  int32 // atomic: peak of running
	signaled int32 // atomic: Signal calls executed
	release  chan struct{}

	closeRelease sync.Once
}

func newAdmissionTestService() *admissionTestService {
	return &admissionTestService{release: make(chan struct{})}
}

// releaseOnce unblocks every Block call, now and later. It may be called
// any number of times.
func (s *admissionTestService) releaseOnce() {
	s.closeRelease.Do(func() { close(s.release) })
}

func (s *admissionTestService) enter() {
	cur := atomic.AddInt32(&s.running, 1)
	for {
		max := atomic.LoadInt32(&s.maxSeen)
		if cur <= max || atomic.CompareAndSwapInt32(&s.maxSeen, max, cur) {
			return
		}
	}
}

// Block runs until the test releases it.
func (s *admissionTestService) Block() string {
	s.enter()
	defer atomic.AddInt32(&s.running, -1)
	<-s.release
	return "released"
}

// WaitCtx runs until its context ends.
func (s *admissionTestService) WaitCtx(ctx context.Context) error {
	s.enter()
	defer atomic.AddInt32(&s.running, -1)
	<-ctx.Done()
	return ctx.Err()
}

// Panic crashes the method handler.
func (s *admissionTestService) Panic() string {
	panic("admission test panic")
}

// Signal counts its executions.
func (s *admissionTestService) Signal() string {
	atomic.AddInt32(&s.signaled, 1)
	return "signaled"
}

// newAdmissionTestServer serves an admissionTestService over HTTP with the
// given admission limit.
func newAdmissionTestServer(t *testing.T, limit int) (*Server, *admissionTestService, *httptest.Server) {
	t.Helper()
	svc := newAdmissionTestService()
	srv := NewServer()
	srv.SetMaxConcurrentHTTPRequests(limit)
	if err := srv.RegisterName("test", svc); err != nil {
		t.Fatal(err)
	}
	hs := httptest.NewServer(srv)
	t.Cleanup(func() {
		// Unblock anything still running before the HTTP server waits for
		// its handlers.
		svc.releaseOnce()
		hs.Close()
		srv.Stop()
	})
	return srv, svc, hs
}

type admissionReply struct {
	ID     json.RawMessage `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *jsonError      `json:"error"`
}

// admissionHTTPClient bounds every test request, so a test that fails with
// a call still blocked cannot hang the run.
var admissionHTTPClient = &http.Client{Timeout: 30 * time.Second}

// postRaw sends body to the server and returns the status and raw body.
func postRaw(ctx context.Context, url, body string) (int, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(body))
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := admissionHTTPClient.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	return resp.StatusCode, raw, err
}

// post sends body and decodes the reply as a single response object,
// whatever the status.
func post(t *testing.T, url, body string) (int, admissionReply) {
	t.Helper()
	status, raw, err := postRaw(context.Background(), url, body)
	if err != nil {
		t.Fatalf("POST %s: %v", body, err)
	}
	var reply admissionReply
	if err := json.Unmarshal(raw, &reply); err != nil {
		t.Fatalf("POST %s: status %d, undecodable reply %q: %v", body, status, raw, err)
	}
	return status, reply
}

func callBody(method string, id int) string {
	return fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"method":%q,"params":[]}`, id, method)
}

func batchBody(methods ...string) string {
	parts := make([]string, len(methods))
	for i, m := range methods {
		parts[i] = callBody(m, i+1)
	}
	return "[" + strings.Join(parts, ",") + "]"
}

// requireServed fails unless a call reply is a 200 without an error.
func requireServed(t *testing.T, what string, status int, reply admissionReply) {
	t.Helper()
	if status != http.StatusOK || reply.Error != nil {
		t.Fatalf("%s: status %d error %+v, want a served call", what, status, reply.Error)
	}
}

// requireOverloaded fails unless the reply is the overload refusal: HTTP 503
// with a JSON-RPC error with code -32005 and a null id.
func requireOverloaded(t *testing.T, what string, status int, reply admissionReply) {
	t.Helper()
	if status != http.StatusServiceUnavailable {
		t.Fatalf("%s: status %d, want 503", what, status)
	}
	if reply.Error == nil || reply.Error.Code != -32005 || !strings.Contains(reply.Error.Message, "overloaded") {
		t.Fatalf("%s: error %+v, want code -32005 saying the server is overloaded", what, reply.Error)
	}
	if string(reply.ID) != "null" {
		t.Fatalf("%s: id %s, want null", what, reply.ID)
	}
}

// waitUntil polls cond until it holds or five seconds pass.
func waitUntil(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(time.Millisecond)
	}
}

// startBlockingCalls issues n test.block requests over HTTP and waits until
// all of them are running. The returned channel receives each request's
// error, nil for a served call, once the service is released.
func startBlockingCalls(t *testing.T, url string, svc *admissionTestService, n int) <-chan error {
	t.Helper()
	errc := make(chan error, n)
	for i := 0; i < n; i++ {
		go func(id int) {
			status, raw, err := postRaw(context.Background(), url, callBody("test.block", id))
			if err == nil && (status != http.StatusOK || !strings.Contains(string(raw), `"released"`)) {
				err = fmt.Errorf("status %d reply %s", status, raw)
			}
			errc <- err
		}(100 + i)
	}
	waitUntil(t, fmt.Sprintf("%d blocking calls to run", n), func() bool {
		return atomic.LoadInt32(&svc.running) == int32(n)
	})
	return errc
}

func TestDefaultMaxConcurrentHTTPRequests(t *testing.T) {
	// The default is pinned so a change to the per-connection limit does
	// not move it unnoticed.
	if got := DefaultMaxConcurrentHTTPRequests; got != 256 {
		t.Fatalf("DefaultMaxConcurrentHTTPRequests = %d, want 256 (4 x %d)", got, maxConcurrentCallsPerConn)
	}
	srv := NewServer()
	defer srv.Stop()
	if got := srv.MaxConcurrentHTTPRequests(); got != DefaultMaxConcurrentHTTPRequests {
		t.Fatalf("new server limit = %d, want %d", got, DefaultMaxConcurrentHTTPRequests)
	}
	srv.SetMaxConcurrentHTTPRequests(7)
	if got := srv.MaxConcurrentHTTPRequests(); got != 7 {
		t.Fatalf("configured limit = %d, want 7", got)
	}
	for _, n := range []int{0, -1} {
		srv.SetMaxConcurrentHTTPRequests(n)
		if got := srv.MaxConcurrentHTTPRequests(); got != DefaultMaxConcurrentHTTPRequests {
			t.Fatalf("SetMaxConcurrentHTTPRequests(%d) left limit %d, want the default %d", n, got, DefaultMaxConcurrentHTTPRequests)
		}
	}
}

// Once the limit is reached the next HTTP request is answered at once with
// the overload error and nothing in it runs; when the running calls
// complete their slots are free again.
func TestHTTPAdmissionRefusesExcessRequest(t *testing.T) {
	const limit = 3
	srv, svc, hs := newAdmissionTestServer(t, limit)
	blocked := startBlockingCalls(t, hs.URL, svc, limit)

	start := time.Now()
	status, reply := post(t, hs.URL, callBody("test.signal", 1))
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("overloaded request took %v to be answered", elapsed)
	}
	requireOverloaded(t, "call at the limit", status, reply)

	// A notification and a malformed request are refused the same way:
	// admission comes before the body is read.
	status, reply = post(t, hs.URL, `{"jsonrpc":"2.0","method":"test.signal","params":[]}`)
	requireOverloaded(t, "notification at the limit", status, reply)
	status, reply = post(t, hs.URL, `not json`)
	requireOverloaded(t, "malformed request at the limit", status, reply)
	if n := atomic.LoadInt32(&svc.signaled); n != 0 {
		t.Fatalf("refused requests ran %d calls", n)
	}

	svc.releaseOnce()
	for i := 0; i < limit; i++ {
		if err := <-blocked; err != nil {
			t.Fatalf("blocking call %d: %v", i, err)
		}
	}
	waitUntil(t, "slots to be released", func() bool { return srv.httpRequests.inFlight() == 0 })

	// Every slot is usable again.
	for i := 0; i < limit+2; i++ {
		status, reply := post(t, hs.URL, callBody("test.signal", 10+i))
		requireServed(t, "call after release", status, reply)
	}
	waitUntil(t, "slots to be released", func() bool { return srv.httpRequests.inFlight() == 0 })
}

// A method that panics returns its request's slot.
func TestHTTPAdmissionReleasedOnPanic(t *testing.T) {
	srv, _, hs := newAdmissionTestServer(t, 1)
	for i := 0; i < 3; i++ {
		status, reply := post(t, hs.URL, callBody("test.panic", i))
		if status != http.StatusOK || reply.Error == nil || reply.Error.Code == -32005 {
			t.Fatalf("panicking call %d: status %d error %+v, want the handler-crashed error", i, status, reply.Error)
		}
	}
	waitUntil(t, "the slot to be released", func() bool { return srv.httpRequests.inFlight() == 0 })
	status, reply := post(t, hs.URL, callBody("test.signal", 9))
	requireServed(t, "call after panics", status, reply)
}

// A request whose context ends returns its slot when its call returns.
func TestHTTPAdmissionReleasedOnContextCancel(t *testing.T) {
	srv, svc, hs := newAdmissionTestServer(t, 1)

	ctx, cancel := context.WithCancel(context.Background())
	// Deferred calls run before the cleanup that closes the HTTP server,
	// which would otherwise wait for this request on a failure path.
	defer cancel()
	errc := make(chan error, 1)
	go func() {
		_, _, err := postRaw(ctx, hs.URL, callBody("test.waitCtx", 1))
		errc <- err
	}()
	waitUntil(t, "the waiting call to run", func() bool { return atomic.LoadInt32(&svc.running) == 1 })
	status, reply := post(t, hs.URL, callBody("test.signal", 2))
	requireOverloaded(t, "call while the slot is held", status, reply)

	cancel()
	if err := <-errc; !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled request returned %v, want context.Canceled", err)
	}
	waitUntil(t, "the slot to be released", func() bool { return srv.httpRequests.inFlight() == 0 })
	status, reply = post(t, hs.URL, callBody("test.signal", 3))
	requireServed(t, "call after cancel", status, reply)
}

// A batch is one request: at the limit none of its calls run, while it runs
// it holds one slot that other requests see, it runs all of its calls on
// that slot, and concurrent batches never run more calls at once than the
// limit allows.
func TestHTTPAdmissionBatches(t *testing.T) {
	t.Run("saturated", func(t *testing.T) {
		const limit = 2
		_, svc, hs := newAdmissionTestServer(t, limit)
		blocked := startBlockingCalls(t, hs.URL, svc, limit)

		status, reply := post(t, hs.URL, batchBody("test.signal", "test.signal", "test.signal"))
		requireOverloaded(t, "batch at the limit", status, reply)
		if n := atomic.LoadInt32(&svc.signaled); n != 0 {
			t.Fatalf("%d batch calls ran at the limit", n)
		}
		svc.releaseOnce()
		for i := 0; i < limit; i++ {
			if err := <-blocked; err != nil {
				t.Fatal(err)
			}
		}
	})

	t.Run("running batch holds a slot", func(t *testing.T) {
		srv, svc, hs := newAdmissionTestServer(t, 1)
		type result struct {
			status int
			raw    []byte
			err    error
		}
		done := make(chan result, 1)
		go func() {
			status, raw, err := postRaw(context.Background(), hs.URL, batchBody("test.block", "test.signal", "test.signal"))
			done <- result{status, raw, err}
		}()
		waitUntil(t, "the batch's first call to run", func() bool { return atomic.LoadInt32(&svc.running) == 1 })

		status, reply := post(t, hs.URL, callBody("test.signal", 9))
		requireOverloaded(t, "single call beside a running batch", status, reply)
		svc.releaseOnce()
		res := <-done
		if res.err != nil || res.status != http.StatusOK {
			t.Fatalf("batch: status %d err %v", res.status, res.err)
		}
		var replies []admissionReply
		if err := json.Unmarshal(res.raw, &replies); err != nil {
			t.Fatalf("batch reply %q: %v", res.raw, err)
		}
		if len(replies) != 3 {
			t.Fatalf("got %d batch replies, want 3", len(replies))
		}
		for i, r := range replies {
			if r.Error != nil {
				t.Fatalf("batch element %d: %+v", i, r.Error)
			}
		}
		if n := atomic.LoadInt32(&svc.signaled); n != 2 {
			t.Fatalf("batch ran %d signal calls, want 2", n)
		}
		waitUntil(t, "the slot to be released", func() bool { return srv.httpRequests.inFlight() == 0 })
	})

	t.Run("concurrent batches stay within the limit", func(t *testing.T) {
		const (
			limit     = 4
			batches   = 10
			batchSize = 5
		)
		_, svc, hs := newAdmissionTestServer(t, limit)
		methods := make([]string, batchSize)
		for i := range methods {
			methods[i] = "test.block"
		}
		statuses := make(chan int, batches)
		var wg sync.WaitGroup
		for i := 0; i < batches; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				status, _, err := postRaw(context.Background(), hs.URL, batchBody(methods...))
				if err != nil {
					t.Error(err)
				}
				statuses <- status
			}()
		}
		waitUntil(t, "the limit to fill", func() bool { return atomic.LoadInt32(&svc.running) == limit })
		time.Sleep(50 * time.Millisecond) // room to exceed the limit if it were going to
		if max := atomic.LoadInt32(&svc.maxSeen); max > limit {
			t.Fatalf("observed %d calls running at once, limit %d", max, limit)
		}
		svc.releaseOnce()
		wg.Wait()
		close(statuses)

		if max := atomic.LoadInt32(&svc.maxSeen); max > limit {
			t.Fatalf("observed %d calls running at once, limit %d", max, limit)
		}
		var served, refused int
		for status := range statuses {
			switch status {
			case http.StatusOK:
				served++
			case http.StatusServiceUnavailable:
				refused++
			default:
				t.Fatalf("unexpected status %d", status)
			}
		}
		if served < limit || served+refused != batches {
			t.Fatalf("served %d, refused %d of %d batches", served, refused, batches)
		}
	})
}

// The HTTP limit does not reach connection-based transports: a WebSocket
// connection on a server whose HTTP limit is one still runs many calls at
// once, bounded only by its own call slots.
func TestHTTPAdmissionLeavesWebSocketUnchanged(t *testing.T) {
	svc := newAdmissionTestService()
	srv := NewServer()
	srv.SetMaxConcurrentHTTPRequests(1)
	if err := srv.RegisterName("test", svc); err != nil {
		t.Fatal(err)
	}
	hs := httptest.NewServer(srv.WebsocketHandler(nil))
	// Registered before the deferred client close below, so it runs after
	// it; releaseOnce unblocks the calls first on every exit path.
	t.Cleanup(func() {
		hs.Close()
		srv.Stop()
	})
	defer svc.releaseOnce()

	client, err := DialWebsocket(context.Background(), "ws"+strings.TrimPrefix(hs.URL, "http"), "")
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	const n = 5
	errc := make(chan error, n)
	for i := 0; i < n; i++ {
		go func() {
			var res string
			errc <- client.Call(&res, "test.block")
		}()
	}
	waitUntil(t, "WebSocket calls to run beyond the HTTP limit", func() bool { return atomic.LoadInt32(&svc.running) == n })
	if srv.httpRequests.inFlight() != 0 {
		t.Fatalf("WebSocket calls took %d HTTP admission slots", srv.httpRequests.inFlight())
	}
	svc.releaseOnce()
	for i := 0; i < n; i++ {
		if err := <-errc; err != nil {
			t.Fatalf("WebSocket call: %v", err)
		}
	}
}

// A Go client sees the refusal as an HTTPError with status 503 whose body
// carries the JSON-RPC overload error.
func TestHTTPAdmissionErrorThroughClient(t *testing.T) {
	_, svc, hs := newAdmissionTestServer(t, 1)
	blocked := startBlockingCalls(t, hs.URL, svc, 1)

	client, err := DialHTTP(hs.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	var res string
	err = client.Call(&res, "test.signal")
	var httpErr HTTPError
	if !errors.As(err, &httpErr) || httpErr.StatusCode != http.StatusServiceUnavailable || !strings.Contains(string(httpErr.Body), "-32005") {
		t.Fatalf("client call at the limit: got %v, want HTTPError 503 with code -32005 in the body", err)
	}
	svc.releaseOnce()
	if err := <-blocked; err != nil {
		t.Fatal(err)
	}
}

// blockingResponseWriter is an http.ResponseWriter whose first Write starts
// a reply that ends only when the test unblocks it or the request's
// context ends.
type blockingResponseWriter struct {
	header  http.Header
	ctx     context.Context
	unblock chan struct{}
	err     error // returned once unblocked

	writing   chan struct{} // closed when the first Write starts
	startOnce sync.Once
}

func (w *blockingResponseWriter) Header() http.Header { return w.header }
func (w *blockingResponseWriter) WriteHeader(int)     {}

func (w *blockingResponseWriter) Write(p []byte) (int, error) {
	w.startOnce.Do(func() { close(w.writing) })
	select {
	case <-w.unblock:
		if w.err != nil {
			return 0, w.err
		}
		return len(p), nil
	case <-w.ctx.Done():
		return 0, w.ctx.Err()
	}
}

// A request holds its slot until its reply has been written, whatever the
// reply is: a call's result, a batch's answers, or the error answering a
// malformed, invalid or empty request. The slot is returned when the write
// ends normally, with an error, or because the request's context ended.
func TestHTTPAdmissionHeldThroughReplyWrite(t *testing.T) {
	cases := []struct {
		name   string
		body   string
		calls  int32 // signal calls the request runs
		cancel bool  // end the write by canceling the request
		err    error // error the unblocked write returns
	}{
		{name: "single call", body: callBody("test.signal", 1), calls: 1},
		{name: "batch", body: batchBody("test.signal", "test.signal", "test.signal"), calls: 3},
		{name: "parse error", body: `not json`},
		{name: "invalid request", body: `{"jsonrpc":"2.0","id":"` + strings.Repeat("x", 4096) + `"}`},
		{name: "empty batch", body: `[]`},
		{name: "write error", body: callBody("test.signal", 1), calls: 1, err: errors.New("write failed")},
		{name: "context canceled", body: callBody("test.signal", 1), calls: 1, cancel: true},
		{name: "batch context canceled", body: batchBody("test.signal", "test.signal"), calls: 2, cancel: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv, svc, hs := newAdmissionTestServer(t, 1)

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			w := &blockingResponseWriter{
				header:  make(http.Header),
				ctx:     ctx,
				unblock: make(chan struct{}),
				err:     tc.err,
				writing: make(chan struct{}),
			}
			var unblockOnce sync.Once
			unblock := func() { unblockOnce.Do(func() { close(w.unblock) }) }
			defer unblock()
			req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(tc.body)).WithContext(ctx)
			req.Header.Set("Content-Type", "application/json")

			served := make(chan struct{})
			go func() {
				defer close(served)
				srv.ServeHTTP(w, req)
			}()
			select {
			case <-w.writing:
			case <-time.After(5 * time.Second):
				t.Fatal("reply write did not start")
			}
			if n := atomic.LoadInt32(&svc.signaled); n != tc.calls {
				t.Fatalf("%d calls ran before the write, want %d", n, tc.calls)
			}
			if n := srv.httpRequests.inFlight(); n != 1 {
				t.Fatalf("%d slots held while the reply is written, want 1", n)
			}
			status, reply := post(t, hs.URL, callBody("test.signal", 9))
			requireOverloaded(t, "request while a reply holds the only slot", status, reply)

			if tc.cancel {
				cancel()
			} else {
				unblock()
			}
			select {
			case <-served:
			case <-time.After(5 * time.Second):
				t.Fatal("ServeHTTP did not return after the write ended")
			}
			if n := srv.httpRequests.inFlight(); n != 0 {
				t.Fatalf("%d slots held after the request was served", n)
			}
			status, reply = post(t, hs.URL, callBody("test.signal", 10))
			requireServed(t, "call after the write ended", status, reply)
		})
	}
}

// A request whose declared body is longer than the JSON it carries holds its
// slot while net/http waits for the rest of the body, not only while the
// call runs, and its slot is returned when the connection goes away. The
// refusal sent meanwhile closes its connection instead of keeping it alive
// behind a body nothing will read.
func TestHTTPAdmissionHeldWhileBodyDrains(t *testing.T) {
	srv, svc, hs := newAdmissionTestServer(t, 1)

	conn, err := net.Dial("tcp", hs.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	body := callBody("test.signal", 1)
	req := fmt.Sprintf("POST / HTTP/1.1\r\nHost: 127.0.0.1\r\nContent-Type: application/json\r\nContent-Length: %d\r\n\r\n%s",
		len(body)+64, body)
	if _, err := conn.Write([]byte(req)); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, "the call to run", func() bool { return atomic.LoadInt32(&svc.signaled) == 1 })
	time.Sleep(50 * time.Millisecond) // room for the slot to be released if it were going to
	if n := srv.httpRequests.inFlight(); n != 1 {
		t.Fatalf("%d slots held while the body is still arriving, want 1", n)
	}

	refusal, err := http.NewRequest(http.MethodPost, hs.URL, strings.NewReader(callBody("test.signal", 2)))
	if err != nil {
		t.Fatal(err)
	}
	refusal.Header.Set("Content-Type", "application/json")
	resp, err := admissionHTTPClient.Do(refusal)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("request while a body drains: status %d, want 503", resp.StatusCode)
	}
	if !resp.Close {
		t.Fatal("refusal kept its connection alive, want Connection: close")
	}

	conn.Close()
	waitUntil(t, "the slot to be released", func() bool { return srv.httpRequests.inFlight() == 0 })
	status, reply := post(t, hs.URL, callBody("test.signal", 3))
	requireServed(t, "call after the connection closed", status, reply)
}

// sendIncomplete writes a POST whose declared body is 64 bytes longer than
// body, so the server is left waiting for the rest of it.
func sendIncomplete(t *testing.T, addr, method, contentType, body string) net.Conn {
	t.Helper()
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	req := fmt.Sprintf("%s / HTTP/1.1\r\nHost: 127.0.0.1\r\nContent-Type: %s\r\nContent-Length: %d\r\n\r\n%s",
		method, contentType, len(body)+64, body)
	if _, err := conn.Write([]byte(req)); err != nil {
		t.Fatal(err)
	}
	return conn
}

// A refused request whose body never finishes arriving is answered and its
// connection closed at once: the server does not sit draining a body it
// will never use. The test server has no ReadTimeout, so without the
// refusal's read deadline the connection would stay open indefinitely.
func TestHTTPAdmissionRefusalClosesIncompleteBody(t *testing.T) {
	_, svc, hs := newAdmissionTestServer(t, 1)
	blocked := startBlockingCalls(t, hs.URL, svc, 1)

	conn := sendIncomplete(t, hs.Listener.Addr().String(), http.MethodPost, "application/json", callBody("test.signal", 1))
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	reader := bufio.NewReader(conn)
	resp, err := http.ReadResponse(reader, nil)
	if err != nil {
		t.Fatalf("refusal: %v", err)
	}
	raw, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable || !strings.Contains(string(raw), "-32005") {
		t.Fatalf("refusal: status %d body %q, want 503 with -32005", resp.StatusCode, raw)
	}
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, err := reader.ReadByte(); err != io.EOF {
		t.Fatalf("after the refusal: read returned %v, want io.EOF (connection closed by the server)", err)
	}
	if n := atomic.LoadInt32(&svc.signaled); n != 0 {
		t.Fatalf("refused request ran %d calls", n)
	}
	svc.releaseOnce()
	if err := <-blocked; err != nil {
		t.Fatal(err)
	}
}

// Requests that ServeHTTP answers before reading a call (a failed
// validation, an OPTIONS request) are admitted too, so a body left
// unfinished behind them holds a slot rather than draining outside the
// bound.
func TestHTTPAdmissionCoversEarlyAnswers(t *testing.T) {
	cases := []struct {
		name, method, contentType string
	}{
		{"invalid content type", http.MethodPost, "text/plain"},
		{"options", http.MethodOptions, "application/json"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv, _, hs := newAdmissionTestServer(t, 1)
			conn := sendIncomplete(t, hs.Listener.Addr().String(), tc.method, tc.contentType, "x")
			waitUntil(t, "the request to be admitted", func() bool { return srv.httpRequests.inFlight() == 1 })
			time.Sleep(50 * time.Millisecond) // room for the slot to be released if it were going to
			if n := srv.httpRequests.inFlight(); n != 1 {
				t.Fatalf("%d slots held while the body drains, want 1", n)
			}
			status, reply := post(t, hs.URL, callBody("test.signal", 2))
			requireOverloaded(t, "request beside the draining one", status, reply)
			conn.Close()
			waitUntil(t, "the slot to be released", func() bool { return srv.httpRequests.inFlight() == 0 })
		})
	}
}
