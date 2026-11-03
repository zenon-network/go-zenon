package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// httpClientTestTimeout bounds every client call in these tests. The fixed
// client answers at once; the bound only turns a regression into a failure
// instead of a hung test binary.
const httpClientTestTimeout = 5 * time.Second

// roundTripFunc lets a test play the HTTP server the client talks to without
// binding a listener.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// newScriptedHTTPClient returns an HTTP RPC client whose every request is
// answered by reply, which receives the decoded request IDs.
func newScriptedHTTPClient(t *testing.T, status int, reply func(ids []json.RawMessage) io.ReadCloser) *Client {
	t.Helper()
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			return nil, err
		}
		var ids []json.RawMessage
		var batch []jsonrpcMessage
		if json.Unmarshal(body, &batch) == nil {
			for _, m := range batch {
				ids = append(ids, m.ID)
			}
		} else {
			var msg jsonrpcMessage
			if err := json.Unmarshal(body, &msg); err != nil {
				return nil, err
			}
			ids = append(ids, msg.ID)
		}
		return &http.Response{
			StatusCode: status,
			Status:     http.StatusText(status),
			Header:     make(http.Header),
			Body:       reply(ids),
			Request:    r,
		}, nil
	})
	client, err := DialHTTPWithClient("http://scripted.invalid", &http.Client{Transport: transport})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.Close)
	return client
}

func replyBody(s string) io.ReadCloser { return io.NopCloser(strings.NewReader(s)) }

// result is a successful reply to id carrying the string v.
func result(id json.RawMessage, v string) string {
	return fmt.Sprintf(`{"jsonrpc":"2.0","id":%s,"result":%q}`, id, v)
}

type clientOutcome struct {
	err      error
	panicked interface{}
}

// runClientCall runs call on its own goroutine with a bounded context and
// reports how it ended. A call still running after the bound fails the test:
// blocking is the defect, not a slow test.
func runClientCall(t *testing.T, call func(ctx context.Context) error) clientOutcome {
	t.Helper()
	done := make(chan clientOutcome, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				done <- clientOutcome{panicked: r}
			}
		}()
		ctx, cancel := context.WithTimeout(context.Background(), httpClientTestTimeout)
		defer cancel()
		done <- clientOutcome{err: call(ctx)}
	}()
	select {
	case out := <-done:
		if errors.Is(out.err, context.DeadlineExceeded) {
			t.Fatalf("call waited for its context to expire: %v", out.err)
		}
		return out
	case <-time.After(httpClientTestTimeout + 2*time.Second):
		t.Fatalf("call still blocked %v after its context expired", 2*time.Second)
		return clientOutcome{}
	}
}

func twoElemBatch() []BatchElem {
	return []BatchElem{
		{Method: "test.echo", Args: []interface{}{"a"}, Result: new(string)},
		{Method: "test.echo", Args: []interface{}{"b"}, Result: new(string)},
	}
}

// An HTTP batch reply is accepted only if every element answers a request of
// the batch, at most once, with no more elements than requests. Any other
// reply fails the whole call with one error and leaves every element alone.
func TestHTTPBatchRejectsInvalidResponse(t *testing.T) {
	tests := []struct {
		name  string
		reply func(ids []json.RawMessage) string
	}{
		{"unknown id", func(ids []json.RawMessage) string {
			return "[" + result(ids[0], "a") + "," + result(json.RawMessage("999"), "b") + "]"
		}},
		{"null id", func(ids []json.RawMessage) string {
			return "[" + result(ids[0], "a") + `,{"jsonrpc":"2.0","id":null,"error":{"code":-32600,"message":"x"}}]`
		}},
		{"missing id", func(ids []json.RawMessage) string {
			return "[" + result(ids[0], "a") + `,{"jsonrpc":"2.0","result":"b"}]`
		}},
		{"null element", func(ids []json.RawMessage) string {
			return "[" + result(ids[0], "a") + ",null]"
		}},
		{"duplicate id", func(ids []json.RawMessage) string {
			return "[" + result(ids[0], "a") + "," + result(ids[0], "again") + "]"
		}},
		{"id spelled as a string", func(ids []json.RawMessage) string {
			return "[" + result(ids[0], "a") + "," + result(json.RawMessage(fmt.Sprintf("%q", ids[1])), "b") + "]"
		}},
		{"extra element", func(ids []json.RawMessage) string {
			return "[" + result(ids[0], "a") + "," + result(ids[1], "b") + "," + result(ids[0], "again") + "]"
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := newScriptedHTTPClient(t, http.StatusOK, func(ids []json.RawMessage) io.ReadCloser {
				return replyBody(tt.reply(ids))
			})
			batch := twoElemBatch()
			out := runClientCall(t, func(ctx context.Context) error { return client.BatchCallContext(ctx, batch) })
			if out.panicked != nil {
				t.Fatalf("BatchCallContext panicked: %v", out.panicked)
			}
			if !errors.Is(out.err, errInvalidBatchResponse) {
				t.Fatalf("got error %v, want %v", out.err, errInvalidBatchResponse)
			}
			for i, elem := range batch {
				if elem.Error != nil || *elem.Result.(*string) != "" {
					t.Errorf("element %d was filled from a rejected reply: result %q, error %v", i, *elem.Result.(*string), elem.Error)
				}
			}
		})
	}
}

// A shorter reply is complete once the body has been read: the answered
// elements keep their results, and each unanswered one gets
// ErrMissingBatchResponse instead of the call waiting for a reply that cannot
// arrive.
func TestHTTPBatchMarksMissingResponses(t *testing.T) {
	tests := []struct {
		name     string
		reply    func(ids []json.RawMessage) string
		answered bool // whether element 1 is answered with "b"
	}{
		{"one of two", func(ids []json.RawMessage) string { return "[" + result(ids[1], "b") + "]" }, true},
		{"empty array", func([]json.RawMessage) string { return "[]" }, false},
		{"null", func([]json.RawMessage) string { return "null" }, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := newScriptedHTTPClient(t, http.StatusOK, func(ids []json.RawMessage) io.ReadCloser {
				return replyBody(tt.reply(ids))
			})
			batch := twoElemBatch()
			out := runClientCall(t, func(ctx context.Context) error { return client.BatchCallContext(ctx, batch) })
			if out.panicked != nil || out.err != nil {
				t.Fatalf("BatchCallContext: error %v, panic %v", out.err, out.panicked)
			}
			if batch[0].Error != ErrMissingBatchResponse {
				t.Errorf("element 0: error %v, want %v", batch[0].Error, ErrMissingBatchResponse)
			}
			if tt.answered {
				if batch[1].Error != nil || *batch[1].Result.(*string) != "b" {
					t.Errorf("element 1: result %q, error %v", *batch[1].Result.(*string), batch[1].Error)
				}
			} else if batch[1].Error != ErrMissingBatchResponse {
				t.Errorf("element 1: error %v, want %v", batch[1].Error, ErrMissingBatchResponse)
			}
		})
	}
}

// The honest path: a complete reply in any order fills every element.
func TestHTTPBatchAcceptsReorderedResponse(t *testing.T) {
	client := newScriptedHTTPClient(t, http.StatusOK, func(ids []json.RawMessage) io.ReadCloser {
		return replyBody("[" + result(ids[1], "b") + `,{"jsonrpc":"2.0","id":` + string(ids[0]) + `,"error":{"code":-32000,"message":"boom"}}]`)
	})
	batch := twoElemBatch()
	out := runClientCall(t, func(ctx context.Context) error { return client.BatchCallContext(ctx, batch) })
	if out.panicked != nil || out.err != nil {
		t.Fatalf("BatchCallContext: error %v, panic %v", out.err, out.panicked)
	}
	if batch[0].Error == nil || batch[0].Error.Error() != "boom" {
		t.Errorf("element 0: error %v, want boom", batch[0].Error)
	}
	if batch[1].Error != nil || *batch[1].Result.(*string) != "b" {
		t.Errorf("element 1: result %q, error %v", *batch[1].Result.(*string), batch[1].Error)
	}
}

// trackedBody is a response body that counts what the client reads from it
// and records whether it was closed.
type trackedBody struct {
	r      io.Reader
	read   int64
	closed int32
}

func (b *trackedBody) Read(p []byte) (int, error) {
	n, err := b.r.Read(p)
	atomic.AddInt64(&b.read, int64(n))
	return n, err
}

func (b *trackedBody) Close() error {
	atomic.StoreInt32(&b.closed, 1)
	return nil
}

// A non-2xx reply keeps at most maxHTTPErrorBodySize bytes of its body in the
// HTTPError, reads no more than that, and closes the body.
func TestHTTPErrorBodyBounded(t *testing.T) {
	body := &trackedBody{r: bytes.NewReader(bytes.Repeat([]byte("e"), 4*maxHTTPErrorBodySize))}
	client := newScriptedHTTPClient(t, http.StatusBadGateway, func([]json.RawMessage) io.ReadCloser { return body })

	var res string
	out := runClientCall(t, func(ctx context.Context) error { return client.CallContext(ctx, &res, "test.echo", "a") })
	var httpErr HTTPError
	if !errors.As(out.err, &httpErr) {
		t.Fatalf("got error %v, want an HTTPError", out.err)
	}
	if httpErr.StatusCode != http.StatusBadGateway {
		t.Errorf("status %d, want %d", httpErr.StatusCode, http.StatusBadGateway)
	}
	if len(httpErr.Body) != maxHTTPErrorBodySize {
		t.Errorf("HTTPError.Body holds %d bytes, want %d", len(httpErr.Body), maxHTTPErrorBodySize)
	}
	if read := atomic.LoadInt64(&body.read); read > maxHTTPErrorBodySize {
		t.Errorf("client read %d bytes of the error body, want at most %d", read, maxHTTPErrorBodySize)
	}
	if atomic.LoadInt32(&body.closed) == 0 {
		t.Error("error response body was not closed")
	}
}

// A notification expects no reply. Whatever the server answers, an empty
// body (which is what this package's server sends) or an unsolicited
// message, Notify returns nil once the request was delivered.
func TestHTTPNotifyIgnoresReply(t *testing.T) {
	tests := []struct {
		name  string
		reply string
	}{
		{"empty body", ""},
		{"unsolicited reply", `{"jsonrpc":"2.0","id":1,"result":"x"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var sent int32
			client := newScriptedHTTPClient(t, http.StatusOK, func(ids []json.RawMessage) io.ReadCloser {
				if len(ids) != 1 || ids[0] != nil {
					t.Errorf("notification carried ids %s", ids)
				}
				atomic.AddInt32(&sent, 1)
				return replyBody(tt.reply)
			})
			out := runClientCall(t, func(ctx context.Context) error { return client.Notify(ctx, "test.echo", "a") })
			if out.panicked != nil || out.err != nil {
				t.Fatalf("Notify: error %v, panic %v", out.err, out.panicked)
			}
			if got := atomic.LoadInt32(&sent); got != 1 {
				t.Fatalf("server saw %d requests, want 1", got)
			}
		})
	}
}

// OPTIONS carries no RPC: the server answers it without reading the body,
// so a JSON-RPC body sent with it is never executed.
func TestServeHTTPOptionsNotDispatched(t *testing.T) {
	server, svc := newBatchTestServer(t)
	for _, body := range []string{"", `{"jsonrpc":"2.0","id":1,"method":"test.echo","params":["a"]}`, string(batchOf(3))} {
		req := httptest.NewRequest(http.MethodOptions, "/", strings.NewReader(body))
		req.Header.Set("content-type", contentType)
		rec := httptest.NewRecorder()
		server.ServeHTTP(rec, req)

		if rec.Code != http.StatusNoContent {
			t.Errorf("body %q: status %d, want %d", body, rec.Code, http.StatusNoContent)
		}
		if rec.Body.Len() != 0 {
			t.Errorf("body %q: response body %q, want none", body, rec.Body.String())
		}
	}
	if got := atomic.LoadInt32(&svc.calls); got != 0 {
		t.Fatalf("OPTIONS dispatched %d calls", got)
	}
}
