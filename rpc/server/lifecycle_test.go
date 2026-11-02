package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// lifecycleTestService has a method that only returns once its context is
// cancelled, and one that checks its context after doing some work.
type lifecycleTestService struct {
	waiting chan struct{}
	once    sync.Once
}

func (s *lifecycleTestService) Wait(ctx context.Context) error {
	s.once.Do(func() { close(s.waiting) })
	<-ctx.Done()
	return ctx.Err()
}

func (s *lifecycleTestService) Slow(ctx context.Context) (string, error) {
	time.Sleep(50 * time.Millisecond)
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return "done", nil
}

// discardWriter is a jsonWriter that accepts and drops every message.
type discardWriter struct{ done chan interface{} }

func (w *discardWriter) writeJSON(context.Context, interface{}) error { return nil }
func (w *discardWriter) closed() <-chan interface{}                   { return w.done }
func (w *discardWriter) remoteAddr() string                           { return "" }

// Closing a connection cancels the contexts of the calls still running on it
// before waiting for them, so a method that returns when its context ends
// cannot hold the connection's teardown open.
func TestHandlerCloseCancelsCallsBeforeWaiting(t *testing.T) {
	svc := &lifecycleTestService{waiting: make(chan struct{})}
	server := NewServer()
	if err := server.RegisterName("test", svc); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(server.Stop)

	h := newHandler(context.Background(), &discardWriter{done: make(chan interface{})}, server.idgen, &server.services, 0)
	h.handleMsg(&jsonrpcMessage{Version: vsn, ID: json.RawMessage("1"), Method: "test.wait"}, false)
	select {
	case <-svc.waiting:
	case <-time.After(pendingCallTestTimeout):
		t.Fatal("the call never started")
	}

	closed := make(chan struct{})
	go func() {
		h.close(io.EOF, nil)
		close(closed)
	}()
	select {
	case <-closed:
	case <-time.After(pendingCallTestTimeout):
		t.Fatal("handler.close is waiting for a call whose context it has not cancelled")
	}
}

// Over HTTP the answer is written by the call's goroutine after
// serveSingleRequest has handed the request over; the call runs to the end
// with a live context and its answer reaches the response.
func TestHTTPCallFinishesBeforeTeardown(t *testing.T) {
	server := NewServer()
	if err := server.RegisterName("test", &lifecycleTestService{}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(server.Stop)

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"test.slow"}`))
	req.Header.Set("content-type", contentType)
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)

	var msg jsonrpcMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &msg); err != nil {
		t.Fatalf("response %q: %v", rec.Body.String(), err)
	}
	if msg.Error != nil || string(msg.Result) != `"done"` {
		t.Fatalf("got result %s, error %v; want \"done\"", msg.Result, msg.Error)
	}
}

// newLifecycleClient is newScriptedClient with a cleanup that reports a
// Client.Close that does not return instead of hanging the test binary: a
// stuck teardown is what these tests look for.
func newLifecycleClient(t *testing.T) (*Client, *scriptedClientCodec) {
	t.Helper()
	codec := newScriptedClientCodec()
	client := initClient(codec, randomIDGenerator(), new(serviceRegistry), 0)
	t.Cleanup(func() {
		codec.releaseWrites()
		closed := make(chan struct{})
		go func() {
			client.Close()
			close(closed)
		}()
		select {
		case <-closed:
		case <-time.After(pendingCallTestTimeout):
			t.Error("Client.Close did not return: the connection's teardown is stuck")
		}
	})
	return client, codec
}

// subscribeScripted subscribes on a scripted client and answers the request
// with subID. It returns what Subscribe returned.
func subscribeScripted(t *testing.T, client *Client, codec *scriptedClientCodec, ch interface{}, subID string) (*ClientSubscription, error) {
	t.Helper()
	type result struct {
		sub *ClientSubscription
		err error
	}
	done := make(chan result, 1)
	go func() {
		sub, err := client.Subscribe(context.Background(), "test", ch, "events")
		done <- result{sub, err}
	}()
	codec.awaitWriting(t)
	codec.releaseWrites()
	codec.injectReply(codec.lastRequestID(t), fmt.Sprintf("%q", subID))
	select {
	case got := <-done:
		return got.sub, got.err
	case <-time.After(pendingCallTestTimeout):
		t.Fatal("Subscribe did not return")
		return nil, nil
	}
}

func injectNotification(codec *scriptedClientCodec, subID string, result string) {
	params, _ := json.Marshal(&subscriptionResult{ID: subID, Result: json.RawMessage(result)})
	codec.events <- readEvent{msgs: []*jsonrpcMessage{{Version: vsn, Method: "test" + notificationMethodSuffix, Params: params}}}
}

func awaitClientClose(t *testing.T, client *Client) {
	t.Helper()
	closed := make(chan struct{})
	go func() {
		client.Close()
		close(closed)
	}()
	select {
	case <-closed:
	case <-time.After(pendingCallTestTimeout):
		t.Fatal("Client.Close did not return: the connection's teardown is stuck")
	}
}

// A server that answers a second subscribe with an ID already in use on the
// connection gets an error for it. The first subscription keeps its state:
// it still receives the notifications for its ID, and the connection's
// teardown still stops its forwarding loop.
func TestClientRejectsDuplicateSubscriptionID(t *testing.T) {
	client, codec := newLifecycleClient(t)

	first := make(chan int, 4)
	sub, err := subscribeScripted(t, client, codec, first, "0xabc")
	if err != nil {
		t.Fatalf("first subscribe: %v", err)
	}
	dup, err := subscribeScripted(t, client, codec, make(chan int, 4), "0xabc")
	if !errors.Is(err, errDuplicateSubscriptionID) || dup != nil {
		t.Fatalf("second subscribe with the same ID: got subscription %t, error %v; want %v", dup != nil, err, errDuplicateSubscriptionID)
	}

	injectNotification(codec, "0xabc", "7")
	select {
	case v := <-first:
		if v != 7 {
			t.Fatalf("first subscription received %d, want 7", v)
		}
	case <-time.After(pendingCallTestTimeout):
		t.Fatal("first subscription no longer receives its notifications")
	}

	awaitClientClose(t, client)
	select {
	case <-sub.Err():
	case <-time.After(pendingCallTestTimeout):
		t.Fatal("first subscription did not end with the connection")
	}
}

// Once the server has confirmed an unsubscribe, that subscription has ended
// and no longer owns its ID: a later subscribe the server answers with the
// same ID on the same connection succeeds, and the notifications for the ID
// go to the new subscription only.
func TestClientReusesIDOfEndedSubscription(t *testing.T) {
	client, codec := newLifecycleClient(t)

	old := make(chan int, 4)
	sub, err := subscribeScripted(t, client, codec, old, "0xabc")
	if err != nil {
		t.Fatalf("first subscribe: %v", err)
	}
	done := make(chan struct{})
	go func() {
		sub.Unsubscribe()
		close(done)
	}()
	codec.awaitWriting(t) // the unsubscribe request
	codec.releaseWrites()
	codec.injectReply(codec.lastRequestID(t), "true")
	select {
	case <-done:
	case <-time.After(subscribeTimeout / 2):
		t.Fatal("Unsubscribe did not return after the server confirmed it")
	}
	// Unsubscribe also returns after a failed or timed-out request; only a
	// nil outcome means the server confirmed.
	if sub.unsubscribeErr != nil {
		t.Fatalf("the unsubscribe request failed: %v", sub.unsubscribeErr)
	}

	reused := make(chan int, 4)
	if _, err := subscribeScripted(t, client, codec, reused, "0xabc"); err != nil {
		t.Fatalf("subscribe reusing the ID of an ended subscription: %v", err)
	}
	injectNotification(codec, "0xabc", "7")
	select {
	case v := <-reused:
		if v != 7 {
			t.Fatalf("new subscription received %d, want 7", v)
		}
	case <-time.After(pendingCallTestTimeout):
		t.Fatal("new subscription does not receive the notifications for its ID")
	}
	select {
	case v := <-old:
		t.Fatalf("ended subscription received %d", v)
	default:
	}
}

// Unsubscribe asks the server to drop the subscription and waits for the
// answer, but not for longer than subscribeTimeout: a server that withholds
// the answer cannot keep Unsubscribe from returning.
func TestUnsubscribeBoundedWhenReplyWithheld(t *testing.T) {
	client, codec := newLifecycleClient(t)
	sub, err := subscribeScripted(t, client, codec, make(chan int, 1), "0xabc")
	if err != nil {
		t.Fatal(err)
	}

	done := make(chan struct{})
	go func() {
		sub.Unsubscribe()
		close(done)
	}()
	codec.awaitWriting(t) // the unsubscribe request; never answered
	codec.releaseWrites()

	select {
	case <-done:
	case <-time.After(subscribeTimeout + 2*time.Second):
		t.Fatalf("Unsubscribe still waiting %v after the server withheld its answer", subscribeTimeout+2*time.Second)
	}
}

// The connection breaks while an unsubscribe request is being written. The
// connection's teardown waits for the subscription's forwarding loop, and
// the unsubscribe request needs the dispatch loop that runs the teardown:
// the teardown must not wait for the request, so both finish promptly
// instead of waiting on each other.
func TestConnectionLossDuringUnsubscribeDoesNotDeadlock(t *testing.T) {
	client, codec := newLifecycleClient(t)
	sub, err := subscribeScripted(t, client, codec, make(chan int, 1), "0xabc")
	if err != nil {
		t.Fatal(err)
	}

	done := make(chan struct{})
	go func() {
		sub.Unsubscribe()
		close(done)
	}()
	codec.awaitWriting(t) // the unsubscribe request, held in the write
	codec.injectReadError(errPeerGone)
	codec.awaitClosed(t)
	codec.releaseWrites()

	// Well inside subscribeTimeout: the request's deadline must not be what
	// ends the wait.
	select {
	case <-done:
	case <-time.After(subscribeTimeout / 2):
		t.Fatal("Unsubscribe did not return after the connection was torn down")
	}
	awaitClientClose(t, client)
}

// The client queues the notifications its application has not taken yet.
// The queue is bounded by the bytes it holds as well as by count: a server
// sending large notifications to an application that is not reading ends
// the subscription with ErrSubscriptionQueueOverflow long before the count
// limit is reached.
func TestClientSubscriptionQueueBoundedInBytes(t *testing.T) {
	client, codec := newLifecycleClient(t)
	const size = 1 << 20
	sub, err := subscribeScripted(t, client, codec, make(chan json.RawMessage), "0xabc") // never read
	if err != nil {
		t.Fatal(err)
	}

	payload := `"` + strings.Repeat("x", size-2) + `"`
	n := maxClientSubscriptionBufferBytes/size + 2
	go func() {
		for i := 0; i < n; i++ {
			injectNotification(codec, "0xabc", payload)
		}
	}()

	// The overflow unsubscribes on the server; answer that request.
	select {
	case <-codec.writing:
	case <-time.After(pendingCallTestTimeout):
		t.Fatalf("%d notifications of %d bytes queued without overflow", n, size)
	}
	codec.injectReply(codec.lastRequestID(t), "true")
	codec.releaseWrites()

	select {
	case err := <-sub.Err():
		if !errors.Is(err, ErrSubscriptionQueueOverflow) {
			t.Fatalf("subscription ended with %v, want %v", err, ErrSubscriptionQueueOverflow)
		}
	case <-time.After(pendingCallTestTimeout):
		t.Fatalf("%d notifications of %d bytes queued without overflow", n, size)
	}
}

// reverseCallService calls back to the peer that called it, through the
// client the connection's handler serves it from.
type reverseCallService struct {
	ctx  chan context.Context
	done chan error
}

func (s *reverseCallService) Ask(ctx context.Context) error {
	s.ctx <- ctx
	client, _ := ClientFromContext(ctx)
	var answer string
	err := client.CallContext(ctx, &answer, "peer.answer")
	s.done <- err
	return err
}

// A method serving a peer's call calls back to that peer, and the connection
// breaks while the call-back request is being written. Teardown cancels the
// method's context and waits for it; the call-back then gives up on its
// cancelled context, and its giving up must not need the dispatch loop that
// is running the teardown.
func TestReverseCallDuringTeardownDoesNotDeadlock(t *testing.T) {
	svc := &reverseCallService{ctx: make(chan context.Context, 1), done: make(chan error, 1)}
	reg := new(serviceRegistry)
	if err := reg.registerName("test", svc); err != nil {
		t.Fatal(err)
	}
	codec := newScriptedClientCodec()
	client := initClient(codec, randomIDGenerator(), reg, 0)
	t.Cleanup(func() {
		codec.releaseWrites()
		closed := make(chan struct{})
		go func() {
			client.Close()
			close(closed)
		}()
		select {
		case <-closed:
		case <-time.After(pendingCallTestTimeout):
			t.Error("Client.Close did not return: the connection's teardown is stuck")
		}
	})

	codec.events <- readEvent{msgs: []*jsonrpcMessage{{Version: vsn, ID: json.RawMessage("1"), Method: "test.ask"}}}
	var callCtx context.Context
	select {
	case callCtx = <-svc.ctx:
	case <-time.After(pendingCallTestTimeout):
		t.Fatal("the peer's call never ran")
	}
	codec.awaitWriting(t) // the call-back request, held in the write
	codec.injectReadError(errPeerGone)
	select {
	case <-callCtx.Done():
	case <-time.After(pendingCallTestTimeout):
		t.Fatal("teardown did not cancel the running call's context")
	}
	codec.releaseWrites()

	select {
	case err := <-svc.done:
		if err == nil {
			t.Fatal("the call-back succeeded on a broken connection")
		}
	case <-time.After(pendingCallTestTimeout):
		t.Fatal("the call-back did not return after its context was cancelled")
	}
	codec.awaitWriting(t) // the method's own answer
	codec.releaseWrites()
	codec.awaitClosed(t)
}
