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
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// batchTestService counts invocations and can hand back large results or
// block until released, so the tests can observe execution rather than
// only responses.
type batchTestService struct {
	calls int32
}

func (s *batchTestService) Echo(v string) string {
	atomic.AddInt32(&s.calls, 1)
	return v
}

func (s *batchTestService) Big(n int) string {
	atomic.AddInt32(&s.calls, 1)
	return strings.Repeat("x", n)
}

// Fail returns an error carrying n bytes of data.
func (s *batchTestService) Fail(n int) (string, error) {
	atomic.AddInt32(&s.calls, 1)
	return "", &bigDataError{strings.Repeat("e", n)}
}

type bigDataError struct{ data string }

func (e *bigDataError) Error() string          { return "failed with data" }
func (e *bigDataError) ErrorData() interface{} { return e.data }

func newBatchTestServer(t *testing.T) (*Server, *batchTestService) {
	t.Helper()
	svc := &batchTestService{}
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
	defer func() { _ = resp.Body.Close() }()
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
	defer func() { _ = conn.Close() }()
	if err := conn.SetReadDeadline(time.Now().Add(10 * time.Second)); err != nil {
		t.Fatal(err)
	}

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

// Once a batch's response budget is exhausted, the remaining elements are
// answered the way they would have been if executed: invalid ones with the
// invalid-request error, calls with the budget error, notifications not at
// all.
func TestBudgetFallbackAnswersInvalidElements(t *testing.T) {
	server, svc := newBatchTestServer(t)
	ts := httptest.NewServer(server)
	defer ts.Close()

	const each = 4 * 1000 * 1000
	n := maxBatchResponseBytes/each + 1 // the n-th result crosses the budget
	var b bytes.Buffer
	b.WriteByte('[')
	for i := 0; i < n; i++ {
		fmt.Fprintf(&b, `{"jsonrpc":"2.0","id":%d,"method":"test.big","params":[%d]},`, i+1, each)
	}
	b.WriteString(`1,`)
	b.WriteString(`{"jsonrpc":"2.0","id":"i"},`)
	b.WriteString(`{"jsonrpc":"2.0","method":"test.echo","params":["n"]},`)
	b.WriteString(`{"jsonrpc":"2.0","id":"c","method":"test.echo","params":["x"]}`)
	b.WriteByte(']')

	out := postHTTP(t, ts.URL, b.Bytes())
	var answers []jsonrpcMessage
	if err := json.Unmarshal(out, &answers); err != nil {
		t.Fatalf("undecodable batch reply: %v", err)
	}
	if len(answers) != n+3 {
		t.Fatalf("%d answers, want %d (%d results and 3 errors)", len(answers), n+3, n)
	}
	for i := 0; i < n; i++ {
		if answers[i].Error != nil || len(answers[i].Result) != each+2 {
			t.Fatalf("result %d: %.100s", i+1, answers[i].String())
		}
	}
	requireErrorReply(t, "bare number", answers[n], "null", -32600, "invalid request")
	requireErrorReply(t, "id without method", answers[n+1], `"i"`, -32600, "invalid request")
	requireErrorReply(t, "call after the budget", answers[n+2], `"c"`, -32003, "batch response too large")
	if got := atomic.LoadInt32(&svc.calls); got != int32(n) {
		t.Fatalf("%d calls executed, want %d", got, n)
	}
}

func requireErrorReply(t *testing.T, what string, msg jsonrpcMessage, wantID string, wantCode int, wantText string) {
	t.Helper()
	if string(msg.ID) != wantID || msg.Error == nil || msg.Error.Code != wantCode || !strings.Contains(msg.Error.Message, wantText) {
		t.Fatalf("%s: got %s, want id %s, code %d, message containing %q", what, msg.String(), wantID, wantCode, wantText)
	}
}
