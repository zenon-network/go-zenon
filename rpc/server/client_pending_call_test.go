package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"sync"
	"testing"
	"time"
)

const pendingCallTestTimeout = 5 * time.Second

var errPeerGone = errors.New("peer gone")

// readEvent is one thing the scripted codec hands to Client.read: either
// messages or an error.
type readEvent struct {
	msgs []*jsonrpcMessage
	err  error
}

// scriptedClientCodec is a ServerCodec whose reads and writes the test drives.
// Every writeJSON records the message, reports on writing, then waits at the
// write gate and returns writeErr; readBatch returns the next injected event,
// or io.EOF once the codec is closed. Events are read by one goroutine and
// handed to dispatch one at a time, so two events injected in order are
// processed in that order.
type scriptedClientCodec struct {
	mu        sync.Mutex
	written   []*jsonrpcMessage
	writing   chan struct{} // one send per writeJSON call
	writeGate chan struct{}
	writeErr  error
	events    chan readEvent
	done      chan interface{}
	closeOnce sync.Once
}

func newScriptedClientCodec() *scriptedClientCodec {
	return &scriptedClientCodec{
		writing:   make(chan struct{}, 16),
		writeGate: make(chan struct{}),
		events:    make(chan readEvent, 16),
		done:      make(chan interface{}),
	}
}

func (c *scriptedClientCodec) writeJSON(_ context.Context, v interface{}) error {
	c.mu.Lock()
	if msg, ok := v.(*jsonrpcMessage); ok {
		c.written = append(c.written, msg)
	} else if msgs, ok := v.([]*jsonrpcMessage); ok {
		c.written = append(c.written, msgs...)
	}
	err, gate := c.writeErr, c.writeGate
	c.mu.Unlock()
	c.writing <- struct{}{}
	<-gate
	return err
}

func (c *scriptedClientCodec) readBatch() ([]*jsonrpcMessage, bool, error) {
	select {
	case ev := <-c.events:
		return ev.msgs, len(ev.msgs) > 1, ev.err
	case <-c.done:
		return nil, false, io.EOF
	}
}

func (c *scriptedClientCodec) close()                     { c.closeOnce.Do(func() { close(c.done) }) }
func (c *scriptedClientCodec) closed() <-chan interface{} { return c.done }
func (c *scriptedClientCodec) remoteAddr() string         { return "scripted" }

// releaseWrites lets every writeJSON waiting at the current gate return and
// arms a fresh gate, so each write a test wants to hold needs its own
// release. It is safe to call with nothing waiting.
func (c *scriptedClientCodec) releaseWrites() {
	c.mu.Lock()
	close(c.writeGate)
	c.writeGate = make(chan struct{})
	c.mu.Unlock()
}

func (c *scriptedClientCodec) setWriteErr(err error) {
	c.mu.Lock()
	c.writeErr = err
	c.mu.Unlock()
}

// lastRequestID returns the ID of the most recently written request.
func (c *scriptedClientCodec) lastRequestID(t *testing.T) json.RawMessage {
	t.Helper()
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.written) == 0 {
		t.Fatal("no request has been written")
	}
	return c.written[len(c.written)-1].ID
}

func (c *scriptedClientCodec) awaitWriting(t *testing.T) {
	t.Helper()
	select {
	case <-c.writing:
	case <-time.After(pendingCallTestTimeout):
		t.Fatal("the client never started writing the request")
	}
}

func (c *scriptedClientCodec) injectReply(id json.RawMessage, result string) {
	c.events <- readEvent{msgs: []*jsonrpcMessage{{Version: vsn, ID: id, Result: json.RawMessage(result)}}}
}

func (c *scriptedClientCodec) injectReadError(err error) { c.events <- readEvent{err: err} }

func (c *scriptedClientCodec) awaitClosed(t *testing.T) {
	t.Helper()
	select {
	case <-c.done:
	case <-time.After(pendingCallTestTimeout):
		t.Fatal("dispatch did not close the codec after the read error")
	}
}

func newScriptedClient(t *testing.T) (*Client, *scriptedClientCodec) {
	t.Helper()
	codec := newScriptedClientCodec()
	client := initClient(codec, randomIDGenerator(), new(serviceRegistry), 0)
	t.Cleanup(func() {
		codec.releaseWrites()
		client.Close()
	})
	return client, codec
}

// callInBackground starts client.Call and returns a channel for its result.
func callInBackground(client *Client, method string) <-chan error {
	done := make(chan error, 1)
	go func() {
		var result string
		done <- client.Call(&result, method)
	}()
	return done
}

func awaitCall(t *testing.T, done <-chan error, what string) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-time.After(pendingCallTestTimeout):
		t.Fatalf("%s: Call did not return within %v", what, pendingCallTestTimeout)
		return nil
	}
}

// The read loop exits while the request is still being written, and the
// write then completes. The connection close skipped the request as in
// flight, no reply can arrive for it, and nothing else will touch it: the
// call must fail instead of waiting forever. This is the ordering behind the
// TestStopFromRegisteredMethod flake. Fails if dispatch's reqSent case leaves
// an unanswered in-flight request alone after the read loop has gone.
func TestCallFailsWhenReadLoopExitsDuringWrite(t *testing.T) {
	client, codec := newScriptedClient(t)
	done := callInBackground(client, "test.ping")
	codec.awaitWriting(t)

	codec.injectReadError(errPeerGone)
	codec.awaitClosed(t)  // dispatch has processed the read error
	codec.releaseWrites() // the write now reports success

	err := awaitCall(t, done, "read loop gone during the write")
	if !errors.Is(err, errDead) {
		t.Fatalf("got %v, want %v", err, errDead)
	}
}

// The reply arrives before the read loop exits, while the write is still
// reported as in progress. The call completed; the late write completion
// must not overwrite it with an error. (A fix that fails every in-flight
// request on reqSent after a read error breaks this.)
func TestCallKeepsReplyThatArrivedBeforeReadLoopExit(t *testing.T) {
	client, codec := newScriptedClient(t)
	done := callInBackground(client, "test.ping")
	codec.awaitWriting(t)

	codec.injectReply(codec.lastRequestID(t), `"pong"`)
	codec.injectReadError(errPeerGone) // ordered after the reply by the read goroutine
	codec.awaitClosed(t)
	codec.releaseWrites()

	if err := awaitCall(t, done, "reply before read loop exit"); err != nil {
		t.Fatalf("a call whose reply had arrived failed with %v", err)
	}
	awaitReqSentProcessed(t, client, codec)
}

// awaitReqSentProcessed proves dispatch has run the reqSent case for the
// previous request: a new request is only admitted (and so only starts
// writing) once that case has re-armed reqInit. The read loop is gone, so
// the probe itself is failed by the branch under test when its write is
// released. Without this, a test can finish on a buffered reply before the
// branch under test has executed.
func awaitReqSentProcessed(t *testing.T, client *Client, codec *scriptedClientCodec) {
	t.Helper()
	probe := callInBackground(client, "test.probe")
	codec.awaitWriting(t)
	codec.releaseWrites()
	if err := awaitCall(t, probe, "probe after the read loop exit"); !errors.Is(err, errDead) {
		t.Fatalf("probe: got %v, want %v", err, errDead)
	}
}

// A subscription reply arrives before the read loop exits, while the write
// is still in progress. handleResponse has already closed the request's
// channel; the late write completion must not close it again (panic) nor
// fail the established subscription. The subscription then ends with the
// read error, as every subscription on a closed connection does.
func TestSubscribeKeepsReplyThatArrivedBeforeReadLoopExit(t *testing.T) {
	client, codec := newScriptedClient(t)
	type result struct {
		sub *ClientSubscription
		err error
	}
	done := make(chan result, 1)
	go func() {
		sub, err := client.Subscribe(context.Background(), "test", make(chan int, 1), "events")
		done <- result{sub, err}
	}()
	codec.awaitWriting(t)

	codec.injectReply(codec.lastRequestID(t), `"0xabc"`)
	codec.injectReadError(errPeerGone)
	codec.awaitClosed(t)
	codec.releaseWrites()

	var got result
	select {
	case got = <-done:
	case <-time.After(pendingCallTestTimeout):
		t.Fatal("Subscribe did not return")
	}
	if got.err != nil {
		t.Fatalf("a subscription whose reply had arrived failed with %v", got.err)
	}
	select {
	case err := <-got.sub.Err():
		if !errors.Is(err, errPeerGone) {
			t.Fatalf("subscription ended with %v, want %v", err, errPeerGone)
		}
	case <-time.After(pendingCallTestTimeout):
		t.Fatal("the subscription did not end with the read error")
	}
	// The branch under test runs here; a second close of the subscription's
	// channel would panic.
	awaitReqSentProcessed(t, client, codec)
}

// A batch with one of two replies delivered when the read loop exits is
// still outstanding: the late write completion must fail it, so BatchCall
// returns instead of waiting for the second reply forever.
func TestBatchFailsWhenReadLoopExitsWithRepliesOutstanding(t *testing.T) {
	client, codec := newScriptedClient(t)
	batch := []BatchElem{{Method: "test.a", Result: new(string)}, {Method: "test.b", Result: new(string)}}
	done := make(chan error, 1)
	go func() { done <- client.BatchCall(batch) }()
	codec.awaitWriting(t)

	codec.mu.Lock()
	firstID := codec.written[0].ID
	codec.mu.Unlock()
	codec.injectReply(firstID, `"a"`)
	codec.injectReadError(errPeerGone)
	codec.awaitClosed(t)
	codec.releaseWrites()

	err := awaitCall(t, done, "batch with one reply outstanding")
	if !errors.Is(err, errDead) {
		t.Fatalf("got %v, want %v", err, errDead)
	}
}

// The usual order: the write completes, then the read loop exits. The
// request is in respWait and is failed by the connection close with the
// read error, as before. A second request whose write is in flight at that
// moment is failed by the fix instead of waiting forever.
func TestCallsFailWhenReadLoopExitsAfterWrite(t *testing.T) {
	client, codec := newScriptedClient(t)
	first := callInBackground(client, "test.first")
	codec.awaitWriting(t)
	codec.releaseWrites()
	// A second request can only start writing once the first write's
	// completion has been processed by dispatch.
	second := callInBackground(client, "test.second")
	codec.awaitWriting(t)

	codec.injectReadError(errPeerGone)
	codec.awaitClosed(t)
	codec.releaseWrites() // the second write now reports success

	if err := awaitCall(t, first, "request in respWait at the read error"); !errors.Is(err, errPeerGone) {
		t.Fatalf("first call: got %v, want %v", err, errPeerGone)
	}
	if err := awaitCall(t, second, "request in flight at the read error"); !errors.Is(err, errDead) {
		t.Fatalf("second call: got %v, want %v", err, errDead)
	}
}

// A client with a reconnect function keeps its retry: the first write fails,
// the client reconnects, the in-flight request moves to the new connection,
// its retried write succeeds, and the reply on the new connection completes
// the call. The read loop is live again before reqSent is processed, so the
// fix must not fail the request.
func TestReconnectRetriesInFlightRequest(t *testing.T) {
	first := newScriptedClientCodec()
	second := newScriptedClientCodec()
	codecs := make(chan *scriptedClientCodec, 2)
	codecs <- first
	codecs <- second
	client, err := newClient(context.Background(), func(context.Context) (ServerCodec, error) {
		select {
		case c := <-codecs:
			return c, nil
		default:
			return nil, errors.New("no more codecs")
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		first.releaseWrites()
		second.releaseWrites()
		client.Close()
	})

	first.setWriteErr(errPeerGone)
	done := callInBackground(client, "test.ping")
	first.awaitWriting(t)
	first.releaseWrites() // the first write fails, the client reconnects and retries

	second.awaitWriting(t)
	second.releaseWrites()
	second.injectReply(second.lastRequestID(t), `"pong"`)
	if err := awaitCall(t, done, "retried request after reconnect"); err != nil {
		t.Fatalf("retried call failed with %v", err)
	}
}

// A notification expects no reply and has no response channel. The read
// loop exits while it is being written; the write then completes. Nothing
// is outstanding, so nothing is closed, and Notify returns nil as before.
func TestNotifyDuringReadLoopExitDoesNotPanic(t *testing.T) {
	client, codec := newScriptedClient(t)
	done := make(chan error, 1)
	go func() { done <- client.Notify(context.Background(), "test.event") }()
	codec.awaitWriting(t)

	codec.injectReadError(errPeerGone)
	codec.awaitClosed(t)
	codec.releaseWrites()

	if err := awaitCall(t, done, "notification during the read loop exit"); err != nil {
		t.Fatalf("Notify returned %v", err)
	}
}

// A batch whose write completed normally and then had one of two replies
// delivered is cut short by the connection close. The delivered element is
// populated and the call returns the read error. wait consumes the
// buffered reply before it reads the terminal error, so this ordering
// is free of the race the detector reported on the old wait.
func TestPartialBatchCancelledAfterWriteKeepsDeliveredReply(t *testing.T) {
	client, codec := newScriptedClient(t)
	a, b := new(string), new(string)
	batch := []BatchElem{{Method: "test.a", Result: a}, {Method: "test.b", Result: b}}
	done := make(chan error, 1)
	go func() { done <- client.BatchCall(batch) }()
	codec.awaitWriting(t)
	codec.mu.Lock()
	firstID := codec.written[0].ID
	codec.mu.Unlock()
	codec.releaseWrites() // the write completes normally
	// A probe request is admitted only once dispatch has processed the
	// batch's write completion, so the batch is a queued request, not the
	// in-flight one, when the read error arrives. (Otherwise the two could
	// be processed in either order, and both outcomes are legitimate.)
	probe := callInBackground(client, "test.probe")
	codec.awaitWriting(t)

	codec.injectReply(firstID, `"a"`)
	codec.injectReadError(errPeerGone)
	codec.awaitClosed(t)
	codec.releaseWrites() // the probe's write completes; it is failed as in flight

	err := awaitCall(t, done, "partial batch cut short by the connection close")
	if !errors.Is(err, errPeerGone) {
		t.Fatalf("got %v, want %v", err, errPeerGone)
	}
	if err := awaitCall(t, probe, "probe in flight at the read error"); !errors.Is(err, errDead) {
		t.Fatalf("probe: got %v, want %v", err, errDead)
	}
	if *a != "a" {
		t.Fatalf("delivered element not populated: %q", *a)
	}
	if batch[0].Error != nil {
		t.Fatalf("delivered element carries an error: %v", batch[0].Error)
	}
}
