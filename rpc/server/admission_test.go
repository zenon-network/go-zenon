package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// batchSubscriptionService creates subscriptions and counts the ones the
// server has closed, so a test can tell whether an unsubscribe reached the
// server's table.
type batchSubscriptionService struct {
	closed int32
}

func (s *batchSubscriptionService) Events(ctx context.Context) (*Subscription, error) {
	notifier, ok := NotifierFromContext(ctx)
	if !ok {
		return nil, ErrNotificationsUnsupported
	}
	sub := notifier.CreateSubscription()
	go func() {
		<-sub.Err()
		atomic.AddInt32(&s.closed, 1)
	}()
	return sub, nil
}

// waitFor polls cond until it holds or the deadline passes.
func waitFor(t *testing.T, cond func() bool, what string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(time.Millisecond)
	}
}

func isTooManyPending(err error) bool {
	return err != nil && strings.Contains(err.Error(), "too many pending requests")
}

// saturate fills the connection: maxPendingCalls blocking calls are
// accepted and running, and one more has been rejected, which proves every
// accepted message is counted. The returned function waits for the accepted
// calls once the caller releases the service (or closes the connection) and
// returns their errors.
func saturate(t *testing.T, client *Client, svc *batchTestService) (wait func() []error) {
	t.Helper()
	var wg sync.WaitGroup
	errs := make(chan error, maxPendingCalls+1)
	for i := 0; i < maxPendingCalls+1; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var res string
			errs <- client.Call(&res, "test.block")
		}()
	}
	select {
	case err := <-errs:
		if !isTooManyPending(err) {
			t.Fatalf("unexpected early result: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the connection was never saturated")
	}
	// The rejection can arrive before the last accepted goroutine has
	// entered the service; every accepted call runs at once, so it does.
	waitFor(t, func() bool { return atomic.LoadInt32(&svc.inside) == maxPendingCalls }, "every accepted call to run")
	return func() []error {
		wg.Wait()
		close(errs)
		var out []error
		for err := range errs {
			out = append(out, err)
		}
		return out
	}
}

// requireAllServed asserts that every accepted call of saturate succeeded.
func requireAllServed(t *testing.T, errs []error) {
	t.Helper()
	for _, err := range errs {
		if err != nil {
			t.Fatalf("accepted call failed: %v", err)
		}
	}
}

// A client that has saturated its connection can still release a
// subscription: the unsubscribe runs on the dispatch loop instead of being
// answered with the overload error, so the server's entry is removed while
// the load is still there.
func TestUnsubscribeIsServedOnSaturatedConnection(t *testing.T) {
	server, svc := newBatchTestServer(t)
	subs := &batchSubscriptionService{}
	if err := server.RegisterName("sub", subs); err != nil {
		t.Fatal(err)
	}
	client := DialInProc(server)
	defer client.Close()

	sub, err := client.Subscribe(context.Background(), "sub", make(chan int, 1), "events")
	if err != nil {
		t.Fatal(err)
	}
	wait := saturate(t, client, svc)

	// The bundled client's Unsubscribe issues the remote call itself and
	// discards its error; the removal has to succeed on the first attempt.
	done := make(chan struct{})
	go func() {
		sub.Unsubscribe()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Unsubscribe did not return")
	}
	deadline := time.Now().Add(2 * time.Second)
	for atomic.LoadInt32(&subs.closed) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("the server-side subscription was not closed while the connection was saturated")
		}
		time.Sleep(time.Millisecond)
	}

	close(svc.release)
	requireAllServed(t, wait())
	// The entry is gone: unsubscribing it again is refused.
	var ok bool
	err = client.Call(&ok, "sub.unsubscribe", sub.subid)
	if err == nil || !strings.Contains(err.Error(), ErrSubscriptionNotFound.Error()) {
		t.Fatalf("second unsubscribe: got (%v, %v), want subscription not found", ok, err)
	}
}

// The same holds for an unsubscribe carried in a batch: it is answered on
// the spot while the calls beside it get the overload error.
func TestUnsubscribeInBatchIsServedOnSaturatedConnection(t *testing.T) {
	server, svc := newBatchTestServer(t)
	subs := &batchSubscriptionService{}
	if err := server.RegisterName("sub", subs); err != nil {
		t.Fatal(err)
	}
	client := DialInProc(server)
	defer client.Close()

	sub, err := client.Subscribe(context.Background(), "sub", make(chan int, 1), "events")
	if err != nil {
		t.Fatal(err)
	}
	defer sub.Unsubscribe()
	wait := saturate(t, client, svc)

	var (
		removed bool
		echoed  string
	)
	batch := []BatchElem{
		{Method: "sub.unsubscribe", Args: []interface{}{sub.subid}, Result: &removed},
		{Method: "test.echo", Args: []interface{}{"x"}, Result: &echoed},
	}
	if err := client.BatchCall(batch); err != nil {
		t.Fatal(err)
	}
	if batch[0].Error != nil || !removed {
		t.Fatalf("unsubscribe in a saturated batch: (%v, %v), want (true, nil)", removed, batch[0].Error)
	}
	if !isTooManyPending(batch[1].Error) {
		t.Fatalf("call beside it: error %v, want the overload error", batch[1].Error)
	}
	// The service observes the closed error channel on a goroutine of its
	// own, so the count follows the answer by a moment.
	waitFor(t, func() bool { return atomic.LoadInt32(&subs.closed) == 1 }, "the server-side subscription to close")
	close(svc.release)
	requireAllServed(t, wait())
}

// gatedConn is a net.Conn whose writes the test can hold back or fail. A
// held write honours the write deadline, like a stalled socket would. The
// number of writes that have reached the gate is counted, so a test can
// wait for the writer to be inside a held write instead of guessing.
type gatedConn struct {
	net.Conn
	mu       sync.Mutex
	gate     chan struct{} // non-nil while held; closed on release
	deadline time.Time
	fail     bool
	closed   bool
	held     int32 // writes that have blocked on the gate
	finished int32 // writes that have returned from a held wait
}

// Close marks the connection closed, so that later writes fail at once as
// they do on a closed socket, whether or not the transport is held.
func (c *gatedConn) Close() error {
	c.mu.Lock()
	c.closed = true
	c.mu.Unlock()
	return c.Conn.Close()
}

func (c *gatedConn) heldWrites() int32 { return atomic.LoadInt32(&c.held) }

func (c *gatedConn) hold() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.gate = make(chan struct{})
}

func (c *gatedConn) release() {
	c.mu.Lock()
	defer c.mu.Unlock()
	close(c.gate)
	c.gate = nil
}

func (c *gatedConn) failWrites() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.fail = true
}

func (c *gatedConn) SetWriteDeadline(t time.Time) error {
	c.mu.Lock()
	c.deadline = t
	c.mu.Unlock()
	return c.Conn.SetWriteDeadline(t)
}

func (c *gatedConn) Write(p []byte) (int, error) {
	c.mu.Lock()
	gate, deadline, fail, closed := c.gate, c.deadline, c.fail, c.closed
	c.mu.Unlock()
	if closed {
		return 0, net.ErrClosed
	}
	if fail {
		return 0, errors.New("transport failed")
	}
	if gate != nil {
		var timeout <-chan time.Time
		if !deadline.IsZero() {
			timeout = time.After(time.Until(deadline))
		}
		atomic.AddInt32(&c.held, 1)
		defer atomic.AddInt32(&c.finished, 1)
		select {
		case <-gate:
		case <-timeout:
			return 0, os.ErrDeadlineExceeded
		}
	}
	return c.Conn.Write(p)
}

// pipeServer serves the given server over one side of a net.Pipe wrapped in
// a gatedConn and returns a client on the other side plus a channel closed
// when ServeCodec returns.
func pipeServer(t *testing.T, server *Server) (*Client, *gatedConn, <-chan struct{}) {
	t.Helper()
	return pipeServerWith(t, server, func(c *gatedConn) net.Conn { return c })
}

// pipeServerWith is pipeServer with the server-side connection wrapped once
// more by wrap.
func pipeServerWith(t *testing.T, server *Server, wrap func(*gatedConn) net.Conn) (*Client, *gatedConn, <-chan struct{}) {
	t.Helper()
	p1, p2 := net.Pipe()
	serverConn := &gatedConn{Conn: p1}
	served := make(chan struct{})
	go func() {
		server.ServeCodec(NewCodec(wrap(serverConn)), 0)
		close(served)
	}()
	client, err := newClient(context.Background(), func(context.Context) (ServerCodec, error) {
		return NewCodec(p2), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.Close)
	return client, serverConn, served
}

// callbackService calls back into the client and records when the reply
// came back, which needs the connection's dispatch loop to be running.
type callbackService struct {
	entered  int32
	returned int32
}

func (s *callbackService) Callback(ctx context.Context) (string, error) {
	atomic.AddInt32(&s.entered, 1)
	client, ok := ClientFromContext(ctx)
	if !ok {
		return "", fmt.Errorf("no client in context")
	}
	var res string
	err := client.CallContext(ctx, &res, "peer.hold")
	atomic.AddInt32(&s.returned, 1)
	return res, err
}

// holdingPeer is the client-side service the callback calls; it answers
// once released.
type holdingPeer struct {
	entered int32
	release chan struct{}
}

func (p *holdingPeer) Hold() string {
	atomic.AddInt32(&p.entered, 1)
	<-p.release
	return "held"
}

// An overload rejection is written by the connection's answer writer, not
// by the dispatch loop: while the transport is stalled on that write, the
// loop still delivers the reply to a reverse call, so the call that made it
// completes. Once the transport drains, everything is written and nothing
// is lost or executed twice.
func TestRejectionWriteStallDoesNotBlockReverseReplies(t *testing.T) {
	server, svc := newBatchTestServer(t)
	cb := &callbackService{}
	if err := server.RegisterName("cb", cb); err != nil {
		t.Fatal(err)
	}
	client, serverConn, _ := pipeServer(t, server)
	peer := &holdingPeer{release: make(chan struct{})}
	if err := client.RegisterName("peer", peer); err != nil {
		t.Fatal(err)
	}

	// One call that has made its reverse call and is waiting for the
	// reply, then enough blocking calls to take every other pending place,
	// and one more that is rejected, which proves the count is full.
	var wg sync.WaitGroup
	cbErr := make(chan error, 1)
	wg.Add(1)
	go func() {
		defer wg.Done()
		var res string
		err := client.Call(&res, "cb.callback")
		if err == nil && res != "held" {
			err = fmt.Errorf("callback returned %q", res)
		}
		cbErr <- err
	}()
	waitFor(t, func() bool { return atomic.LoadInt32(&peer.entered) == 1 }, "the reverse call to reach the client")
	blockErrs := make(chan error, maxPendingCalls+1)
	for i := 0; i < maxPendingCalls; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var res string
			blockErrs <- client.Call(&res, "test.block")
		}()
	}
	select {
	case err := <-blockErrs:
		if !isTooManyPending(err) {
			t.Fatalf("unexpected early result: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the connection was never saturated")
	}

	// Hold the transport and provoke a rejection, whose write now stalls.
	// Then let the peer answer the reverse call: the reply travels over the
	// client's side of the pipe and must be dispatched although the
	// server's side cannot write.
	serverConn.hold()
	wg.Add(1)
	go func() {
		defer wg.Done()
		var res string
		blockErrs <- client.Call(&res, "test.block")
	}()
	waitFor(t, func() bool { return serverConn.heldWrites() == 1 }, "the rejection's write to stall")
	close(peer.release)
	deadline := time.Now().Add(2 * time.Second)
	for atomic.LoadInt32(&cb.returned) == 0 {
		if time.Now().After(deadline) {
			serverConn.release()
			t.Fatal("the reverse reply was not dispatched while a rejection write was stalled")
		}
		time.Sleep(time.Millisecond)
	}
	serverConn.release()

	close(svc.release)
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("calls did not complete after the transport drained")
	}
	if err := <-cbErr; err != nil {
		t.Fatalf("callback failed: %v", err)
	}
	close(blockErrs)
	rejected, served := 0, 0
	for err := range blockErrs {
		switch {
		case err == nil:
			served++
		case isTooManyPending(err):
			rejected++
		default:
			t.Fatalf("blocking call failed: %v", err)
		}
	}
	// One rejection was consumed above as the saturation proof; the call
	// sent while the transport was held is the other.
	if served != maxPendingCalls-1 || rejected != 1 {
		t.Fatalf("%d served and %d rejected, want %d and 1", served, rejected, maxPendingCalls-1)
	}
	if got := atomic.LoadInt32(&svc.calls); got != maxPendingCalls-1 {
		t.Fatalf("%d blocking calls executed, want %d", got, maxPendingCalls-1)
	}
}

// recursingService and recursingPeer call each other back through the same
// connection, one level per hop, so a chain of the requested depth holds a
// pending place on both sides at every level.
type recursingService struct{}

func (recursingService) Recurse(ctx context.Context, depth int) (string, error) {
	if depth == 0 {
		return "bottom", nil
	}
	client, ok := ClientFromContext(ctx)
	if !ok {
		return "", fmt.Errorf("no client in context")
	}
	var res string
	if err := client.CallContext(ctx, &res, "peer.recurse", depth-1); err != nil {
		return "", err
	}
	return res, nil
}

type recursingPeer struct{ client *Client }

func (p *recursingPeer) Recurse(ctx context.Context, depth int) (string, error) {
	if depth == 0 {
		return "bottom", nil
	}
	var res string
	if err := p.client.CallContext(ctx, &res, "test.recurse", depth-1); err != nil {
		return "", err
	}
	return res, nil
}

// Mutual recursion through reverse calls holds one pending place per hop on
// each side. A chain that fits completes; one that does not is refused at
// the hop that exceeds the limit and unwinds with that error, rather than
// waiting for places its own ancestors hold.
func TestNestedReverseCallsUnwindAtCapacity(t *testing.T) {
	server := NewServer()
	if err := server.RegisterName("test", recursingService{}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(server.Stop)
	client := DialInProc(server)
	defer client.Close()
	if err := client.RegisterName("peer", &recursingPeer{client: client}); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var res string
	// Every hop takes a place on the side that serves it, so the deepest
	// chain that fits has maxPendingCalls hops on each side.
	if err := client.CallContext(ctx, &res, "test.recurse", 2*maxPendingCalls-1); err != nil || res != "bottom" {
		t.Fatalf("chain within the limit: (%q, %v)", res, err)
	}
	err := client.CallContext(ctx, &res, "test.recurse", 2*maxPendingCalls+1)
	if !isTooManyPending(err) {
		t.Fatalf("chain beyond the limit: got %v, want the overload error", err)
	}
	// The connection is intact and empty afterwards.
	if err := client.CallContext(ctx, &res, "test.recurse", 2*maxPendingCalls-1); err != nil || res != "bottom" {
		t.Fatalf("chain after unwinding: (%q, %v)", res, err)
	}
}

type panickingService struct{}

func (panickingService) Panic() string { panic("callback panicked") }

// A callback that panics is answered with an error and releases its
// pending place: after many such calls the connection still accepts a full
// set of new ones.
func TestPanickingCallbackReleasesPendingPlace(t *testing.T) {
	server, svc := newBatchTestServer(t)
	if err := server.RegisterName("panic", panickingService{}); err != nil {
		t.Fatal(err)
	}
	client := DialInProc(server)
	defer client.Close()

	for i := 0; i < 2*maxPendingCalls; i++ {
		var res string
		if err := client.Call(&res, "panic.panic"); err == nil {
			t.Fatalf("call %d: a panicking callback was answered without an error", i)
		}
	}
	wait := saturate(t, client, svc)
	close(svc.release)
	requireAllServed(t, wait())
}

// readRawReply reads one message from a raw WebSocket connection.
func readRawReply(t *testing.T, conn *websocket.Conn) jsonrpcMessage {
	t.Helper()
	_, out, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var msg jsonrpcMessage
	if err := json.Unmarshal(out, &msg); err != nil {
		t.Fatalf("undecodable reply %q: %v", out, err)
	}
	return msg
}

func requireErrorReply(t *testing.T, what string, msg jsonrpcMessage, wantID string, wantCode int, wantText string) {
	t.Helper()
	if string(msg.ID) != wantID || msg.Error == nil || msg.Error.Code != wantCode || !strings.Contains(msg.Error.Message, wantText) {
		t.Fatalf("%s: got %s, want id %s, code %d, message containing %q", what, msg.String(), wantID, wantCode, wantText)
	}
}

// Messages that are not valid calls are answered with the invalid-request
// error whether or not the connection is saturated; a valid notification
// stays silent either way. An oversized batch is answered on a saturated
// connection too.
func TestInvalidRequestsAreAnsweredOnSaturatedConnection(t *testing.T) {
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
	send := func(raw []byte) {
		t.Helper()
		if err := conn.WriteMessage(websocket.TextMessage, raw); err != nil {
			t.Fatal(err)
		}
	}

	// One socket delivers messages in order, so a probe sent after the
	// blocking calls is counted after all of them.
	for i := 0; i < maxPendingCalls; i++ {
		send([]byte(fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"method":"test.block","params":[]}`, i+1)))
	}
	send([]byte(`{"jsonrpc":"2.0","id":"probe","method":"test.block","params":[]}`))
	requireErrorReply(t, "probe", readRawReply(t, conn), `"probe"`, -32005, "too many pending")

	inputs := []struct {
		raw      string
		wantID   string
		wantCode int
		wantText string
	}{
		{`1`, "null", -32600, "invalid request"},
		{`null`, "null", -32600, "invalid request"},
		{`{}`, "null", -32600, "invalid request"},
		{`{"jsonrpc":"2.0","id":"i"}`, `"i"`, -32600, "invalid request"},
		{`[]`, "null", -32600, "empty batch"},
		{string(batchOf(maxBatchRequests + 1)), "null", -32600, "batch too large"},
		{`{"jsonrpc":"2.0","method":"test.echo","params":["n"]}`, "", 0, ""}, // notification: silent
		{`{"jsonrpc":"2.0","id":"c","method":"test.echo","params":["x"]}`, `"c"`, -32005, "too many pending"},
	}
	for _, in := range inputs {
		send([]byte(in.raw))
	}
	// The answers are written in dispatch order, so the replies arrive in
	// the order the inputs were sent, with nothing for the notification.
	for _, in := range inputs {
		if in.wantID == "" {
			continue
		}
		requireErrorReply(t, fmt.Sprintf("%.60s", in.raw), readRawReply(t, conn), in.wantID, in.wantCode, in.wantText)
	}
	// A final probe's reply comes next, so nothing else was queued.
	send([]byte(`{"jsonrpc":"2.0","id":"after","method":"test.block","params":[]}`))
	requireErrorReply(t, "final probe", readRawReply(t, conn), `"after"`, -32005, "too many pending")

	close(svc.release)
	for i := 0; i < maxPendingCalls; i++ {
		if msg := readRawReply(t, conn); msg.Error != nil {
			t.Fatalf("accepted call failed: %s", msg.String())
		}
	}
	if got := atomic.LoadInt32(&svc.calls); got != maxPendingCalls {
		t.Fatalf("%d calls executed, want %d", got, maxPendingCalls)
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

// unsubscribeRequest is a raw unsubscribe call for a subscription that does
// not exist, with an id of the given size; the answer echoes the id.
func unsubscribeRequest(id int, idSize int) []byte {
	return []byte(fmt.Sprintf(`{"jsonrpc":"2.0","id":"%d-%s","method":"test.unsubscribe","params":["0x1"]}`,
		id, strings.Repeat("x", idSize)))
}

// A peer that keeps sending while not reading its answers is disconnected
// once the answers it has not read retain more than maxQueuedAnswerBytes,
// rather than having them pile up; the first answer is always queued. A
// peer that reads gets every answer.
func TestNonReadingPeerIsShedByRetainedAnswerBytes(t *testing.T) {
	server, _ := newBatchTestServer(t)
	ts := httptest.NewServer(server.WebsocketHandler([]string{"*"}))
	defer ts.Close()
	dial := func() *websocket.Conn {
		t.Helper()
		conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(ts.URL, "http"), nil)
		if err != nil {
			t.Fatal(err)
		}
		if err := conn.SetReadDeadline(time.Now().Add(20 * time.Second)); err != nil {
			t.Fatal(err)
		}
		return conn
	}
	// Larger than any socket buffer, so an unread answer stays with the
	// writer, and larger than the byte bound, so the second one overflows it.
	const idSize = 12 << 20

	t.Run("not reading", func(t *testing.T) {
		conn := dial()
		defer func() { _ = conn.Close() }()
		for i := 0; i < 3; i++ {
			if err := conn.WriteMessage(websocket.TextMessage, unsubscribeRequest(i, idSize)); err != nil {
				if i == 0 {
					t.Fatalf("first request: %v", err)
				}
				break // the server has already closed the connection
			}
		}
		// Give the server time to parse the later requests while the first
		// answer is stuck with nobody reading it, then read: at most the
		// first answer arrives before the server closes the connection.
		time.Sleep(time.Second)
		answers := 0
		for {
			_, _, err := conn.ReadMessage()
			if err != nil {
				break
			}
			answers++
			if answers > 1 {
				t.Fatal("the second answer was delivered although the peer was not reading when it was queued")
			}
		}
	})
	t.Run("reading", func(t *testing.T) {
		conn := dial()
		defer func() { _ = conn.Close() }()
		errs := make(chan error, 1)
		go func() {
			for i := 0; i < 3; i++ {
				if err := conn.WriteMessage(websocket.TextMessage, unsubscribeRequest(i, idSize)); err != nil {
					errs <- err
					return
				}
			}
			errs <- nil
		}()
		for i := 0; i < 3; i++ {
			msg := readRawReply(t, conn)
			if msg.Error == nil || !strings.Contains(msg.Error.Message, ErrSubscriptionNotFound.Error()) {
				t.Fatalf("answer %d: %.120s", i, msg.String())
			}
		}
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	})
}

// An answer the writer cannot deliver means the transport is gone or the
// peer has stopped reading: the connection is closed, which releases the
// peer's outstanding calls with an error instead of leaving them waiting.
func TestAnswerWriteFailureClosesConnection(t *testing.T) {
	server, svc := newBatchTestServer(t)
	client, serverConn, served := pipeServer(t, server)
	wait := saturate(t, client, svc)

	serverConn.failWrites()
	// The call that provokes the failed write is not waited for: when the
	// server closes right after reading a request, the bundled client can
	// see the read error before it has finished sending, and it exempts
	// the request being sent from cancellation. That is the client's
	// pre-existing reconnect logic; the calls already registered are what
	// the close must release.
	trigger := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		var res string
		trigger <- client.CallContext(ctx, &res, "test.block")
	}()
	results := make(chan []error, 1)
	go func() { results <- wait() }()
	select {
	case errs := <-results:
		for _, err := range errs {
			if err == nil {
				t.Fatal("an accepted call was answered although the transport had failed")
			}
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the accepted calls were not released: the server did not close the connection after a failed write")
	}
	close(svc.release)
	select {
	case <-served:
	case <-time.After(10 * time.Second):
		t.Fatal("ServeCodec did not return after the connection was closed")
	}
	if err := <-trigger; err == nil || isTooManyPending(err) {
		t.Fatalf("the call that provoked the failure returned %v", err)
	}
}

// heldHandler builds a handler on the server side of a net.Pipe wrapped by
// wrap, holds the transport, and queues n unsubscribe answers on the
// handler directly: after the first has reached the held write, the rest
// are in the queue. Nothing about it depends on scheduling.
func heldHandler(t *testing.T, server *Server, wrap func(*gatedConn) net.Conn, n int) (*handler, *gatedConn) {
	t.Helper()
	p1, p2 := net.Pipe()
	t.Cleanup(func() { _ = p2.Close() })
	serverConn := &gatedConn{Conn: p1}
	h := newHandler(context.Background(), NewCodec(wrap(serverConn)), server.idgen, &server.services)
	serverConn.hold()
	for i := 0; i < n; i++ {
		h.handleMsg(&jsonrpcMessage{
			Version: "2.0",
			ID:      json.RawMessage(fmt.Sprint(i + 1)),
			Method:  "test.unsubscribe",
			Params:  json.RawMessage(`["0x1"]`),
		})
	}
	waitFor(t, func() bool { return serverConn.heldWrites() == 1 }, "the writer to block on the held transport")
	if got := len(h.answers); got != n-1 {
		t.Fatalf("%d answers queued behind the held write, want %d", got, n-1)
	}
	return h, serverConn
}

// closeWithin closes the handler and fails unless close returns within the
// bound; it returns how long close took.
func closeWithin(t *testing.T, h *handler, bound time.Duration) time.Duration {
	t.Helper()
	start := time.Now()
	done := make(chan struct{})
	go func() {
		h.close(io.EOF, nil)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(bound):
		t.Fatalf("close did not return within %v", bound)
	}
	return time.Since(start)
}

// Closing a connection with answers still queued for a stalled transport
// takes at most the write already in progress plus one shared deadline for
// the rest, not one deadline per queued answer; the writer has exited by
// the time close returns.
func TestCloseDrainsQueuedAnswersUnderOneDeadline(t *testing.T) {
	if testing.Short() {
		t.Skip("waits for the write deadline")
	}
	server, _ := newBatchTestServer(t)
	const queued = 5
	h, serverConn := heldHandler(t, server, func(c *gatedConn) net.Conn { return c }, queued)

	took := closeWithin(t, h, 2*defaultWriteTimeout)
	if took > defaultWriteTimeout+defaultWriteTimeout/2 {
		t.Fatalf("close took %v, want at most the write in progress plus one deadline", took)
	}
	// The write in progress times out on its own deadline, which the close
	// deadline follows by the few milliseconds between the two; the writer
	// then fails the rest on the closed connection and exits.
	select {
	case <-h.writerDone:
	case <-time.After(time.Second):
		t.Fatal("the writer did not exit after close although the transport honours deadlines")
	}
	if got := atomic.LoadInt32(&serverConn.finished); got != 1 {
		t.Fatalf("%d held writes returned, want only the one in progress (the rest fail on the closed connection)", got)
	}
	serverConn.release()
}

// noDeadlineConn is a gatedConn that, like a stdio transport, supports no
// write deadline: a held write blocks until released, whatever the deadline.
type noDeadlineConn struct{ *gatedConn }

func (c *noDeadlineConn) SetWriteDeadline(time.Time) error {
	return errors.New("deadline not supported")
}

// On a transport without write deadlines, closing a connection whose
// answer writer is stuck still returns after the close deadline: the
// connection is closed under the write and the writer is left to finish on
// its own. Without that, close would wait forever.
func TestCloseIsBoundedWithoutWriteDeadlines(t *testing.T) {
	if testing.Short() {
		t.Skip("waits for the write deadline")
	}
	server, _ := newBatchTestServer(t)
	const queued = 3
	h, serverConn := heldHandler(t, server, func(c *gatedConn) net.Conn { return &noDeadlineConn{c} }, queued)

	took := closeWithin(t, h, 2*defaultWriteTimeout)
	if took < defaultWriteTimeout/2 || took > defaultWriteTimeout+defaultWriteTimeout/2 {
		t.Fatalf("close took %v, want about one deadline", took)
	}
	// The writer is still inside the write close could not interrupt.
	select {
	case <-h.writerDone:
		t.Fatal("the writer exited although its write can only end when the transport lets it")
	case <-time.After(500 * time.Millisecond):
	}
	if got := atomic.LoadInt32(&serverConn.finished); got != 0 {
		t.Fatalf("%d held writes returned before the transport was released", got)
	}
	// Once the transport lets it, the writer finds the connection closed,
	// fails the rest at once and exits.
	serverConn.release()
	select {
	case <-h.writerDone:
	case <-time.After(5 * time.Second):
		t.Fatal("the writer did not exit after the transport was released")
	}
}

// blockingResponseWriter is an http.ResponseWriter whose first write blocks
// until the test releases it, like a client that has stopped reading.
type blockingResponseWriter struct {
	*httptest.ResponseRecorder
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (w *blockingResponseWriter) Write(p []byte) (int, error) {
	w.once.Do(func() {
		close(w.entered)
		<-w.release
	})
	return w.ResponseRecorder.Write(p)
}

// Over HTTP the response writer belongs to the request: an answer produced
// on the dispatch path is written before ServeHTTP returns, however long
// the write takes, rather than by a goroutine that could outlive it.
func TestHTTPAnswerIsWrittenBeforeServeHTTPReturns(t *testing.T) {
	if testing.Short() {
		t.Skip("waits past the write deadline")
	}
	server, _ := newBatchTestServer(t)
	w := &blockingResponseWriter{
		ResponseRecorder: httptest.NewRecorder(),
		entered:          make(chan struct{}),
		release:          make(chan struct{}),
	}
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(
		`{"jsonrpc":"2.0","id":7,"method":"test.unsubscribe","params":["0x1"]}`))
	req.Header.Set("Content-Type", "application/json")
	done := make(chan struct{})
	go func() {
		server.ServeHTTP(w, req)
		close(done)
	}()
	select {
	case <-w.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("the answer was never written")
	}
	// Stall the write past the deadline a persistent connection's close
	// would give up at; ServeHTTP must still be waiting on it.
	select {
	case <-done:
		t.Fatal("ServeHTTP returned while the answer was still being written")
	case <-time.After(defaultWriteTimeout + time.Second):
	}
	close(w.release)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("ServeHTTP did not return after the write completed")
	}
	var msg jsonrpcMessage
	if err := json.Unmarshal(w.Body.Bytes(), &msg); err != nil {
		t.Fatalf("undecodable reply %q: %v", w.Body.Bytes(), err)
	}
	requireErrorReply(t, "HTTP unsubscribe", msg, "7", defaultErrorCode, ErrSubscriptionNotFound.Error())
}

// Over HTTP the handler is closed right after dispatch; an unsubscribe
// answer, produced on the dispatch path, is still delivered, alone or as
// the only element of a batch.
func TestUnsubscribeOverHTTPIsAnswered(t *testing.T) {
	server, _ := newBatchTestServer(t)
	ts := httptest.NewServer(server)
	defer ts.Close()

	out := postHTTP(t, ts.URL, []byte(`{"jsonrpc":"2.0","id":7,"method":"test.unsubscribe","params":["0x1"]}`))
	var msg jsonrpcMessage
	if err := json.Unmarshal(out, &msg); err != nil {
		t.Fatalf("undecodable reply %q: %v", out, err)
	}
	requireErrorReply(t, "single unsubscribe", msg, "7", defaultErrorCode, ErrSubscriptionNotFound.Error())

	out = postHTTP(t, ts.URL, []byte(`[{"jsonrpc":"2.0","id":8,"method":"test.unsubscribe","params":["0x1"]}]`))
	var answers []jsonrpcMessage
	if err := json.Unmarshal(out, &answers); err != nil || len(answers) != 1 {
		t.Fatalf("batch of one unsubscribe: %q (err %v)", out, err)
	}
	requireErrorReply(t, "batch unsubscribe", answers[0], "8", defaultErrorCode, ErrSubscriptionNotFound.Error())
}

// An unsubscribe inside a batch is handled before the batch's calls and is
// not subject to the response budget: it is answered even after the budget
// has been crossed, while a call in the same position is not executed.
func TestUnsubscribeInBatchBypassesResponseBudget(t *testing.T) {
	server, svc := newBatchTestServer(t)
	ts := httptest.NewServer(server)
	defer ts.Close()

	const each = 4 * 1000 * 1000
	n := maxBatchResponseBytes/each + 1
	var b bytes.Buffer
	b.WriteByte('[')
	for i := 0; i < n; i++ {
		fmt.Fprintf(&b, `{"jsonrpc":"2.0","id":%d,"method":"test.big","params":[%d]},`, i+1, each)
	}
	b.WriteString(`{"jsonrpc":"2.0","id":"u","method":"test.unsubscribe","params":["0x1"]},`)
	b.WriteString(`{"jsonrpc":"2.0","id":"c","method":"test.echo","params":["x"]}`)
	b.WriteByte(']')

	out := postHTTP(t, ts.URL, b.Bytes())
	var answers []jsonrpcMessage
	if err := json.Unmarshal(out, &answers); err != nil {
		t.Fatalf("undecodable batch reply: %v", err)
	}
	byID := map[string]jsonrpcMessage{}
	for _, a := range answers {
		byID[string(a.ID)] = a
	}
	if len(byID) != n+2 {
		t.Fatalf("%d answers, want %d", len(byID), n+2)
	}
	requireErrorReply(t, "unsubscribe after the budget", byID[`"u"`], `"u"`, defaultErrorCode, ErrSubscriptionNotFound.Error())
	requireErrorReply(t, "call after the budget", byID[`"c"`], `"c"`, -32003, "batch response too large")
	if got := atomic.LoadInt32(&svc.calls); got != int32(n) {
		t.Fatalf("%d calls executed, want %d", got, n)
	}
}
