package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// batchTestService counts invocations and can hand back large results or
// block until released, so the tests can observe execution rather than
// only responses.
type batchTestService struct {
	calls   int32
	inside  int32
	release chan struct{}
}

func (s *batchTestService) Echo(v string) string {
	atomic.AddInt32(&s.calls, 1)
	return v
}

func (s *batchTestService) Big(n int) string {
	atomic.AddInt32(&s.calls, 1)
	return strings.Repeat("x", n)
}

// Reverse calls back into the client on the same connection; the reply
// can only be delivered if the connection's dispatch loop keeps running.
func (s *batchTestService) Reverse(ctx context.Context) (string, error) {
	atomic.AddInt32(&s.calls, 1)
	client, ok := ClientFromContext(ctx)
	if !ok {
		return "", fmt.Errorf("no client in context")
	}
	<-s.release
	var res string
	if err := client.CallContext(ctx, &res, "peer.echo", "back"); err != nil {
		return "", err
	}
	return res, nil
}

// Fail returns an error carrying n bytes of data.
func (s *batchTestService) Fail(n int) (string, error) {
	atomic.AddInt32(&s.calls, 1)
	return "", &bigDataError{strings.Repeat("e", n)}
}

type bigDataError struct{ data string }

func (e *bigDataError) Error() string          { return "failed with data" }
func (e *bigDataError) ErrorData() interface{} { return e.data }

type peerService struct{}

func (peerService) Echo(v string) string { return v }

func (s *batchTestService) Block() string {
	atomic.AddInt32(&s.calls, 1)
	atomic.AddInt32(&s.inside, 1)
	defer atomic.AddInt32(&s.inside, -1)
	<-s.release
	return "released"
}

func newBatchTestServer(t *testing.T) (*Server, *batchTestService) {
	t.Helper()
	svc := &batchTestService{release: make(chan struct{})}
	server := NewServer()
	if err := server.RegisterName("test", svc); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(server.Stop)
	return server, svc
}

// batchOf builds a JSON array of n test.echo calls.
func batchOf(n int) []byte {
	var b bytes.Buffer
	b.WriteByte('[')
	for i := 0; i < n; i++ {
		if i > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, `{"jsonrpc":"2.0","id":%d,"method":"test.echo","params":["a"]}`, i+1)
	}
	b.WriteByte(']')
	return b.Bytes()
}

// isBatchTooLarge reports whether body is the single invalid-request error
// the server sends for an oversized batch.
func isBatchTooLarge(t *testing.T, body []byte) bool {
	t.Helper()
	var msg jsonrpcMessage
	if err := json.Unmarshal(body, &msg); err != nil {
		return false // an array, or garbage
	}
	return msg.Error != nil && msg.Error.Code == -32600 && strings.Contains(msg.Error.Message, "batch too large")
}

func postHTTP(t *testing.T, url string, body []byte) []byte {
	t.Helper()
	resp, err := http.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	out, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// A batch of exactly maxBatchRequests runs; one more is answered with a
// single invalid-request error before any of its calls run. HTTP path.
func TestBatchRequestLimitHTTP(t *testing.T) {
	server, svc := newBatchTestServer(t)
	ts := httptest.NewServer(server)
	defer ts.Close()

	out := postHTTP(t, ts.URL, batchOf(maxBatchRequests))
	var answers []jsonrpcMessage
	if err := json.Unmarshal(out, &answers); err != nil || len(answers) != maxBatchRequests {
		t.Fatalf("batch at the limit: got %d answers (err %v)", len(answers), err)
	}
	if got := atomic.LoadInt32(&svc.calls); got != maxBatchRequests {
		t.Fatalf("batch at the limit executed %d calls", got)
	}

	atomic.StoreInt32(&svc.calls, 0)
	out = postHTTP(t, ts.URL, batchOf(maxBatchRequests+1))
	if !isBatchTooLarge(t, out) {
		t.Fatalf("batch over the limit was not rejected: %.200s", out)
	}
	if got := atomic.LoadInt32(&svc.calls); got != 0 {
		t.Fatalf("rejected batch still executed %d calls", got)
	}
}

// Same over a persistent WebSocket connection, which must stay usable after
// the rejection.
func TestBatchRequestLimitWebSocket(t *testing.T) {
	server, svc := newBatchTestServer(t)
	ts := httptest.NewServer(server.WebsocketHandler([]string{"*"}))
	defer ts.Close()

	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(ts.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.SetReadDeadline(time.Now().Add(10 * time.Second))

	if err := conn.WriteMessage(websocket.TextMessage, batchOf(maxBatchRequests+1)); err != nil {
		t.Fatal(err)
	}
	_, out, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("no reply to the oversized batch: %v", err)
	}
	if !isBatchTooLarge(t, out) {
		t.Fatalf("batch over the limit was not rejected: %.200s", out)
	}
	if got := atomic.LoadInt32(&svc.calls); got != 0 {
		t.Fatalf("rejected batch still executed %d calls", got)
	}

	// The connection is still served.
	if err := conn.WriteMessage(websocket.TextMessage, []byte(`{"jsonrpc":"2.0","id":"after","method":"test.echo","params":["ok"]}`)); err != nil {
		t.Fatal(err)
	}
	_, out, err = conn.ReadMessage()
	if err != nil {
		t.Fatalf("connection unusable after the rejection: %v", err)
	}
	var msg jsonrpcMessage
	if err := json.Unmarshal(out, &msg); err != nil || msg.Error != nil || string(msg.Result) != `"ok"` {
		t.Fatalf("unexpected reply after the rejection: %s", out)
	}
}

// Compact invalid elements count like any other: they cannot be used to
// slip past the limit, and they do not need a method to be executed.
func TestBatchRequestLimitCountsInvalidElements(t *testing.T) {
	server, _ := newBatchTestServer(t)
	ts := httptest.NewServer(server)
	defer ts.Close()

	body := []byte("[" + strings.Repeat("1,", maxBatchRequests) + "1]")
	if out := postHTTP(t, ts.URL, body); !isBatchTooLarge(t, out) {
		t.Fatalf("oversized batch of invalid elements was not rejected: %.200s", out)
	}
	body = []byte("[" + strings.Repeat("1,", maxBatchRequests-1) + "1]")
	out := postHTTP(t, ts.URL, body)
	var answers []jsonrpcMessage
	if err := json.Unmarshal(out, &answers); err != nil || len(answers) != maxBatchRequests {
		t.Fatalf("batch of invalid elements at the limit: got %d answers (err %v)", len(answers), err)
	}
}

// Once the results accumulated for a batch exceed the response budget, the
// remaining calls are not executed and are answered with an error instead.
func TestBatchResponseSizeLimit(t *testing.T) {
	server, svc := newBatchTestServer(t)
	client := DialInProc(server)
	defer client.Close()

	const each = 4 * 1000 * 1000
	n := maxBatchResponseBytes/each + 4
	batch := make([]BatchElem, n)
	results := make([]string, n)
	for i := range batch {
		batch[i] = BatchElem{Method: "test.big", Args: []interface{}{each}, Result: &results[i]}
	}
	if err := client.BatchCall(batch); err != nil {
		t.Fatal(err)
	}
	served, rejected, total := 0, 0, 0
	for i, elem := range batch {
		switch {
		case elem.Error == nil:
			served++
			total += len(results[i])
		case strings.Contains(elem.Error.Error(), "batch response too large"):
			rejected++
		default:
			t.Fatalf("element %d: unexpected error %v", i, elem.Error)
		}
	}
	if rejected == 0 {
		t.Fatalf("all %d results (%d bytes) were served", served, total)
	}
	// The overflowing result is still delivered; nothing beyond it is.
	if total > maxBatchResponseBytes+each {
		t.Fatalf("served %d bytes, budget is %d", total, maxBatchResponseBytes)
	}
	if got := int(atomic.LoadInt32(&svc.calls)); got != served {
		t.Fatalf("%d calls executed for %d served results", got, served)
	}
}

// Error payloads count against the budget like results do.
func TestBatchResponseSizeLimitCountsErrors(t *testing.T) {
	server, svc := newBatchTestServer(t)
	client := DialInProc(server)
	defer client.Close()

	const each = 4 * 1000 * 1000
	n := maxBatchResponseBytes/each + 4
	batch := make([]BatchElem, n)
	for i := range batch {
		batch[i] = BatchElem{Method: "test.fail", Args: []interface{}{each}, Result: new(string)}
	}
	if err := client.BatchCall(batch); err != nil {
		t.Fatal(err)
	}
	served, rejected := 0, 0
	for i, elem := range batch {
		switch {
		case elem.Error == nil:
			t.Fatalf("element %d succeeded", i)
		case strings.Contains(elem.Error.Error(), "batch response too large"):
			rejected++
		case strings.Contains(elem.Error.Error(), "failed with data"):
			served++
		default:
			t.Fatalf("element %d: unexpected error %v", i, elem.Error)
		}
	}
	if rejected == 0 {
		t.Fatalf("all %d error payloads were served", served)
	}
	if got := int(atomic.LoadInt32(&svc.calls)); got != served {
		t.Fatalf("%d calls executed for %d served errors", got, served)
	}
}

// The client refuses to send a batch the server would reject as a whole,
// because the server's single error carries no IDs it could resolve.
func TestClientRefusesOversizedBatch(t *testing.T) {
	server, svc := newBatchTestServer(t)
	client := DialInProc(server)
	defer client.Close()

	batch := make([]BatchElem, maxBatchRequests+1)
	for i := range batch {
		batch[i] = BatchElem{Method: "test.echo", Args: []interface{}{"a"}, Result: new(string)}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := client.BatchCallContext(ctx, batch)
	if err == nil || ctx.Err() != nil {
		t.Fatalf("expected an immediate error, got %v (ctx %v)", err, ctx.Err())
	}
	if got := atomic.LoadInt32(&svc.calls); got != 0 {
		t.Fatalf("%d calls executed", got)
	}
}

// Callbacks that call back into the client need the connection's dispatch
// loop to deliver their replies, so waiting for a call slot must never
// happen on that loop. More calls than slots, all of them reverse calls,
// have to complete.
func TestReverseCallsBeyondSlotsComplete(t *testing.T) {
	server, svc := newBatchTestServer(t)
	client := DialInProc(server)
	defer client.Close()
	if err := client.RegisterName("peer", peerService{}); err != nil {
		t.Fatal(err)
	}

	n := maxInFlightCalls + 1
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			var res string
			errs <- client.CallContext(ctx, &res, "test.reverse")
		}()
	}
	// Let every request reach the server before the callbacks proceed.
	deadline := time.Now().Add(5 * time.Second)
	for atomic.LoadInt32(&svc.calls) < maxInFlightCalls {
		if time.Now().After(deadline) {
			t.Fatalf("only %d calls reached the service", atomic.LoadInt32(&svc.calls))
		}
		time.Sleep(time.Millisecond)
	}
	time.Sleep(50 * time.Millisecond)
	close(svc.release)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("reverse call failed: %v", err)
		}
	}
}

// Beyond the outstanding-message cap, a message is answered at once with an
// overload error instead of being queued; earlier calls still complete.
func TestPendingCallsOverloadIsRejected(t *testing.T) {
	server, svc := newBatchTestServer(t)
	client := DialInProc(server)
	defer client.Close()

	const extra = 5
	n := maxPendingCalls + extra
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var res string
			errs <- client.CallContext(context.Background(), &res, "test.block")
		}()
	}
	// Wait until the rejections have come back, which happens while the
	// accepted calls are still blocked.
	deadline := time.Now().Add(5 * time.Second)
	var rejected []error
	for len(rejected) < extra {
		if time.Now().After(deadline) {
			t.Fatalf("only %d messages were rejected", len(rejected))
		}
		select {
		case err := <-errs:
			if err == nil || !strings.Contains(err.Error(), "too many pending requests") {
				t.Fatalf("unexpected early result: %v", err)
			}
			rejected = append(rejected, err)
		default:
			time.Sleep(time.Millisecond)
		}
	}
	if got := atomic.LoadInt32(&svc.inside); got != maxInFlightCalls {
		t.Fatalf("%d calls executing, want %d", got, maxInFlightCalls)
	}
	close(svc.release)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("accepted call failed: %v", err)
		}
	}
	if got := atomic.LoadInt32(&svc.calls); got != maxPendingCalls {
		t.Fatalf("%d calls executed, want %d", got, maxPendingCalls)
	}
}

// One connection runs at most maxInFlightCalls call procedures at a time;
// further messages wait for a slot and run once earlier calls finish.
func TestInFlightCallsPerConnection(t *testing.T) {
	server, svc := newBatchTestServer(t)
	client := DialInProc(server)
	defer client.Close()

	const extra = 8
	var wg sync.WaitGroup
	errs := make(chan error, maxInFlightCalls+extra)
	for i := 0; i < maxInFlightCalls+extra; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var res string
			errs <- client.CallContext(context.Background(), &res, "test.block")
		}()
	}

	deadline := time.Now().Add(5 * time.Second)
	for atomic.LoadInt32(&svc.inside) < maxInFlightCalls {
		if time.Now().After(deadline) {
			t.Fatalf("only %d calls entered the service", atomic.LoadInt32(&svc.inside))
		}
		time.Sleep(time.Millisecond)
	}
	time.Sleep(100 * time.Millisecond)
	if got := atomic.LoadInt32(&svc.inside); got != maxInFlightCalls {
		t.Fatalf("%d calls in flight on one connection, limit is %d", got, maxInFlightCalls)
	}

	close(svc.release)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("call failed: %v", err)
		}
	}
	if got := atomic.LoadInt32(&svc.calls); got != maxInFlightCalls+extra {
		t.Fatalf("%d calls executed in total", got)
	}
}
