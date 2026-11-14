package server

import (
	"context"
	"encoding/json"
	"errors"
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

// batchCancelService lets a test stop a batch between two of its calls. The
// first Step call returns when its context ends, which is the moment the
// server has seen the connection go. Every later Step call reports that it
// started and then blocks until the test releases it, so a batch that keeps
// running after its context ended is caught on its very next call and holds
// whatever it holds until the test lets go.
type batchCancelService struct {
	started int32 // atomic: Step calls that began

	first   chan struct{} // closed when the first Step call is running
	later   chan struct{} // closed when any later Step call is running
	release chan struct{} // closed by the test to let later calls return
	subs    chan *Subscription

	laterOnce   sync.Once
	releaseOnce sync.Once
}

func newBatchCancelService() *batchCancelService {
	return &batchCancelService{
		first:   make(chan struct{}),
		later:   make(chan struct{}),
		release: make(chan struct{}),
		subs:    make(chan *Subscription, 1),
	}
}

func (s *batchCancelService) Step(ctx context.Context) string {
	if atomic.AddInt32(&s.started, 1) == 1 {
		close(s.first)
		<-ctx.Done()
		return "first"
	}
	s.laterOnce.Do(func() { close(s.later) })
	<-s.release
	return "later"
}

// Events creates a subscription and hands it to the test.
func (s *batchCancelService) Events(ctx context.Context) (*Subscription, error) {
	notifier, ok := NotifierFromContext(ctx)
	if !ok {
		return nil, ErrNotificationsUnsupported
	}
	sub := notifier.CreateSubscription()
	s.subs <- sub
	return sub, nil
}

// releaseLater unblocks every later Step call, now and from here on.
func (s *batchCancelService) releaseLater() {
	s.releaseOnce.Do(func() { close(s.release) })
}

func (s *batchCancelService) awaitFirst(t *testing.T) {
	t.Helper()
	select {
	case <-s.first:
	case <-time.After(teardownTestTimeout):
		t.Fatal("the first call of the batch never started")
	}
}

// stepBatch builds a batch of n batch.step calls with ids 1 to n.
func stepBatch(n int) string {
	methods := make([]string, n)
	for i := range methods {
		methods[i] = "batch.step"
	}
	return batchBody(methods...)
}

// A batch over HTTP whose client has gone starts no further call: its
// admission slot is free for the next request while most of its calls have
// not run. Fails if the batch loop does not look at its context between
// calls, in which case the second call starts and keeps the slot.
func TestHTTPBatchStopsWhenClientIsGone(t *testing.T) {
	const calls = 5
	srv, _, hs := newAdmissionTestServer(t, 1)
	svc := newBatchCancelService()
	if err := srv.RegisterName("batch", svc); err != nil {
		t.Fatal(err)
	}
	// Deferred calls run before the cleanup that closes the HTTP server,
	// which would otherwise wait for a call this test left blocked.
	defer svc.releaseLater()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errc := make(chan error, 1)
	go func() {
		_, _, err := postRaw(ctx, hs.URL, stepBatch(calls))
		errc <- err
	}()
	svc.awaitFirst(t)
	status, reply := post(t, hs.URL, callBody("test.signal", 1))
	requireOverloaded(t, "call while the batch holds the slot", status, reply)

	// The client gives up. Its request's context ends on the server, which
	// lets the first call return.
	cancel()
	if err := <-errc; !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled batch returned %v, want context.Canceled", err)
	}

	deadline := time.Now().Add(teardownTestTimeout)
	for srv.httpRequests.inFlight() != 0 {
		select {
		case <-svc.later:
			t.Fatal("the batch started its next call after its client was gone, and still holds its admission slot")
		default:
		}
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for the batch to release its admission slot")
		}
		time.Sleep(time.Millisecond)
	}
	status, reply = post(t, hs.URL, callBody("test.signal", 2))
	requireServed(t, "call after the batch's client left", status, reply)
	if n := atomic.LoadInt32(&svc.started); n != 1 {
		t.Fatalf("the batch ran %d of its %d calls, want 1: the client left during the first", n, calls)
	}
}

// A batch over WebSocket whose peer has gone starts no further call, so the
// connection's teardown, which waits for the batch, completes. Fails if the
// batch loop does not look at its context between calls.
func TestWebsocketBatchStopsWhenPeerIsGone(t *testing.T) {
	const calls = 5
	srv := NewServer()
	svc := newBatchCancelService()
	if err := srv.RegisterName("batch", svc); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.Stop)
	defer svc.releaseLater()

	served := make(chan struct{})
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		srv.ServeCodec(newWebsocketCodec(conn), 0)
		close(served)
	}))
	t.Cleanup(ts.Close)

	conn := dialRaw(t, "ws"+strings.TrimPrefix(ts.URL, "http"))
	if err := conn.WriteMessage(websocket.TextMessage, []byte(stepBatch(calls))); err != nil {
		t.Fatal(err)
	}
	svc.awaitFirst(t)

	// The peer goes away without a close handshake. The server's read fails,
	// the connection's teardown ends the batch's context, and the first call
	// returns.
	if err := conn.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-served:
	case <-svc.later:
		t.Fatal("the batch started its next call after its peer was gone, and the connection's teardown waits for it")
	case <-time.After(teardownTestTimeout):
		t.Fatal("the server is still serving a connection whose peer is gone")
	}
	if n := atomic.LoadInt32(&svc.started); n != 1 {
		t.Fatalf("the batch ran %d of its %d calls, want 1: the peer left during the first", n, calls)
	}
}

// recordingWriter is a jsonWriter that keeps every message it is given.
type recordingWriter struct {
	mu   sync.Mutex
	msgs []interface{}
	done chan interface{}
}

func (w *recordingWriter) writeJSON(_ context.Context, v interface{}) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.msgs = append(w.msgs, v)
	return nil
}
func (w *recordingWriter) closed() <-chan interface{} { return w.done }
func (w *recordingWriter) remoteAddr() string         { return "" }

func (w *recordingWriter) written() []interface{} {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]interface{}(nil), w.msgs...)
}

// A batch that is stopped keeps what its executed calls did: their answers
// are handed to the connection, and a subscription one of them created is
// installed, so the connection's teardown ends it and its reservation is
// returned. The calls that were not started, a notification among them, are
// neither run nor answered. Fails if the batch runs on after its context
// ended, and if stopping it skips the bookkeeping that follows the loop.
func TestStoppedBatchKeepsWhatItRan(t *testing.T) {
	svc := newBatchCancelService()
	defer svc.releaseLater()
	server := NewServer()
	if err := server.RegisterName("batch", svc); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(server.Stop)

	w := &recordingWriter{done: make(chan interface{})}
	h := newHandler(context.Background(), w, server.idgen, &server.services, 0)
	h.handleBatch([]*jsonrpcMessage{
		{Version: vsn, ID: json.RawMessage("1"), Method: "batch.subscribe", Params: json.RawMessage(`["events"]`)},
		{Version: vsn, ID: json.RawMessage("2"), Method: "batch.step"},
		{Version: vsn, ID: json.RawMessage("3"), Method: "batch.step"},
		{Version: vsn, Method: "batch.step"},
		{Version: vsn, ID: json.RawMessage("5"), Method: "batch.step"},
	}, false)
	svc.awaitFirst(t)
	var sub *Subscription
	select {
	case sub = <-svc.subs:
	default:
		t.Fatal("the batch's subscribe call created no subscription before its next call ran")
	}

	closed := make(chan struct{})
	go func() {
		h.close(io.EOF, nil)
		close(closed)
	}()
	select {
	case <-closed:
	case <-svc.later:
		t.Fatal("the batch started its next call after its context ended")
	case <-time.After(teardownTestTimeout):
		t.Fatal("handler.close is still waiting for the batch")
	}
	if n := atomic.LoadInt32(&svc.started); n != 1 {
		t.Fatalf("the batch ran %d step calls, want 1", n)
	}

	// The subscription was installed, so close found and ended it.
	select {
	case err := <-sub.Err():
		if err != io.EOF {
			t.Fatalf("subscription ended with %v, want io.EOF from close", err)
		}
	case <-time.After(teardownTestTimeout):
		t.Fatal("the subscription created by the batch was never ended: it was not installed")
	}
	h.subLock.Lock()
	pending, installed := h.pendingSubs, len(h.serverSubs)
	h.subLock.Unlock()
	if pending != 0 || installed != 0 {
		t.Fatalf("after close: %d reservations pending, %d subscriptions installed, want 0 and 0", pending, installed)
	}

	// One reply, holding the answers of the two calls that ran.
	msgs := w.written()
	if len(msgs) != 1 {
		t.Fatalf("handler wrote %d messages, want the one batch reply: %v", len(msgs), msgs)
	}
	answers, ok := msgs[0].([]*jsonrpcMessage)
	if !ok {
		t.Fatalf("handler wrote %T, want a batch reply", msgs[0])
	}
	var ids []string
	for _, a := range answers {
		if a.Error != nil {
			t.Fatalf("answer %s is an error: %v", a.ID, a.Error)
		}
		ids = append(ids, string(a.ID))
	}
	if got := strings.Join(ids, ","); got != "1,2" {
		t.Fatalf("batch reply answers ids %s, want 1,2", got)
	}
}
