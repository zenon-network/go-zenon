package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"testing"
	"time"
)

// newTestNotifier wires a notifier to a codec over a scripted connection,
// the way handleSubscribe does, and creates its subscription.
func newTestNotifier(t *testing.T, conn *scriptedConn) (*Notifier, *Subscription) {
	t.Helper()
	codec := NewCodec(conn)
	h := newHandler(context.Background(), codec, randomIDGenerator(), new(serviceRegistry), 0)
	n := &Notifier{h: h, namespace: "test"}
	return n, n.CreateSubscription()
}

// readNotifications decodes n messages from the test end and returns their
// subscription payloads, in wire order.
func readNotifications(r interface{ Read([]byte) (int, error) }, n int) ([]json.RawMessage, error) {
	dec := json.NewDecoder(r)
	out := make([]json.RawMessage, 0, n)
	for i := 0; i < n; i++ {
		var msg jsonrpcMessage
		if err := dec.Decode(&msg); err != nil {
			return nil, fmt.Errorf("message %d: %w", i+1, err)
		}
		var res subscriptionResult
		if err := json.Unmarshal(msg.Params, &res); err != nil {
			return nil, fmt.Errorf("message %d params: %w", i+1, err)
		}
		out = append(out, res.Result)
	}
	return out, nil
}

// A flush failure during activation is terminal for the notifier: activate
// returns the error, the buffer is dropped, and every later Notify returns
// the same error without writing. Fails if activate keeps the buffer or
// Notify keeps appending to it after a failed flush.
func TestActivationFlushFailureIsTerminal(t *testing.T) {
	conn, _ := newScriptedConn(t)
	n, sub := newTestNotifier(t, conn)
	if err := n.Notify(sub.ID, 1); err != nil {
		t.Fatal(err)
	}
	if err := n.Notify(sub.ID, 2); err != nil {
		t.Fatal(err)
	}
	conn.failWrites(errTransport)

	if err := n.activate(); !errors.Is(err, errTransport) {
		t.Fatalf("activate: got %v, want %v", err, errTransport)
	}
	n.mu.Lock()
	state, buffered := n.state, len(n.buffer)
	n.mu.Unlock()
	if state != notifierFailed {
		t.Fatalf("state after a failed flush is %v, want notifierFailed", state)
	}
	if buffered != 0 {
		t.Fatalf("%d messages retained after a failed flush, want 0", buffered)
	}
	writes := conn.writeCount()
	if err := n.Notify(sub.ID, 3); !errors.Is(err, errTransport) {
		t.Fatalf("Notify after failure: got %v, want %v", err, errTransport)
	}
	if conn.writeCount() != writes {
		t.Fatal("Notify after failure reached the connection")
	}
}

// The pre-activation buffer is bounded. The notification that would exceed
// it fails the notifier, so a subscription whose client never got its ID
// cannot grow memory without limit. Fails if the bound is removed.
func TestPreActivationBufferIsBounded(t *testing.T) {
	conn, _ := newScriptedConn(t)
	n, sub := newTestNotifier(t, conn)
	for i := 0; i < maxPendingNotifications; i++ {
		if err := n.Notify(sub.ID, i); err != nil {
			t.Fatalf("notification %d: %v", i+1, err)
		}
	}
	if err := n.Notify(sub.ID, "one too many"); !errors.Is(err, ErrNotificationBufferFull) {
		t.Fatalf("got %v, want %v", err, ErrNotificationBufferFull)
	}
	if err := n.activate(); !errors.Is(err, ErrNotificationBufferFull) {
		t.Fatalf("activate after overflow: got %v, want %v", err, ErrNotificationBufferFull)
	}
	if conn.writeCount() != 0 {
		t.Fatal("an overflowed buffer was flushed")
	}
	// The client holds an ID that can never deliver: the connection is torn
	// down so the subscription is reclaimed like any other terminal failure.
	codec := n.h.conn.(*jsonCodec)
	awaitClosed(t, codec.closed(), "connection torn down after the overflow")
	if err := codec.failed(); !errors.Is(err, ErrNotificationBufferFull) {
		t.Fatalf("codec recorded %v, want %v", err, ErrNotificationBufferFull)
	}
}

// Notify never waits behind a write in progress. While activate is blocked
// flushing, a concurrent Notify returns at once (its message joins the
// buffer), and the wire order is response-independent: every buffered
// message before any live one. Fails if n.mu is held across the write.
func TestNotifyDoesNotBlockBehindFlush(t *testing.T) {
	conn, client := newScriptedConn(t)
	n, sub := newTestNotifier(t, conn)
	if err := n.Notify(sub.ID, "buffered-1"); err != nil {
		t.Fatal(err)
	}
	reached, release := conn.gateWrites()
	t.Cleanup(release)

	activated := make(chan error, 1)
	go func() { activated <- n.activate() }()
	issue116AwaitDone(t, reached, "activate reaching the gated write")

	done := make(chan error, 1)
	go func() { done <- n.Notify(sub.ID, "buffered-2") }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Notify blocked behind the flush write")
	}

	release()
	// The reader runs off the test goroutine, so it reports through the
	// channel and the test goroutine fails the test.
	type read struct {
		msgs []json.RawMessage
		err  error
	}
	got := make(chan read, 1)
	go func() {
		msgs, err := readNotifications(client, 2)
		got <- read{msgs, err}
	}()
	if err := <-activated; err != nil {
		t.Fatal(err)
	}
	order := <-got
	if order.err != nil {
		t.Fatal(order.err)
	}
	if string(order.msgs[0]) != `"buffered-1"` || string(order.msgs[1]) != `"buffered-2"` {
		t.Fatalf("wire order %s, %s; want buffered-1 then buffered-2", order.msgs[0], order.msgs[1])
	}
	// Now live: a Notify writes directly.
	go func() {
		msgs, err := readNotifications(client, 1)
		got <- read{msgs, err}
	}()
	if err := n.Notify(sub.ID, "live"); err != nil {
		t.Fatal(err)
	}
	live := <-got
	if live.err != nil {
		t.Fatal(live.err)
	}
	if string(live.msgs[0]) != `"live"` {
		t.Fatalf("live message %s, want live", live.msgs[0])
	}
}

// After the connection was closed by its own lifecycle (peer gone), a
// notifier that never failed reports an error from Notify and does not
// panic. Fails if Notify assumes an open connection once active.
func TestNotifyAfterLifecycleCloseReturnsError(t *testing.T) {
	conn, client := newScriptedConn(t)
	n, sub := newTestNotifier(t, conn)
	if err := n.activate(); err != nil {
		t.Fatal(err)
	}
	client.Close()
	n.h.conn.(*jsonCodec).close()
	if err := n.Notify(sub.ID, "after close"); err == nil {
		t.Fatal("Notify after the connection closed returned nil")
	}
	if err := n.Notify(sub.ID, "again"); err == nil {
		t.Fatal("second Notify after the connection closed returned nil")
	}
}

// serveScripted drives a server over a scripted connection with raw
// JSON-RPC, so a test controls every byte the server reads and every write
// it makes.
func serveScripted(t *testing.T, srv *Server) (*scriptedConn, net.Conn, *jsonCodec) {
	t.Helper()
	conn, client := newScriptedConn(t)
	codec := NewCodec(conn).(*jsonCodec)
	go srv.ServeCodec(codec, 0)
	return conn, client, codec
}

// The subscribe response is written, then the flush of notifications sent
// before the call returned fails. The response reached the client, the
// notifier is failed, the codec is torn down, and the handler's own
// bookkeeping (cancelServerSubscriptions on close) runs without blocking on
// the failed notifier. Single request and batch.
func TestActivationFlushFailureThroughHandler(t *testing.T) {
	for _, tc := range []struct {
		name    string
		request string
		replies int // responses the client reads before the flush starts
	}{
		{"single", `{"jsonrpc":"2.0","id":1,"method":"test.subscribe","params":["events"]}`, 1},
		{"batch", `[{"jsonrpc":"2.0","id":1,"method":"test.subscribe","params":["events"]},{"jsonrpc":"2.0","id":2,"method":"test.subscribe","params":["events"]}]`, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc := &subscriptionTestService{notifyBeforeReturn: 2}
			srv := newSubscriptionTestServer(t, svc)
			conn, client, codec := serveScripted(t, srv)
			// The response write(s) succeed; the write after them, the
			// flush, fails. Decided by write number, before the request is
			// sent, so it cannot race the server's first flush write.
			conn.failWritesAfter(tc.replies, errTransport)

			if _, err := client.Write([]byte(tc.request)); err != nil {
				t.Fatal(err)
			}
			dec := json.NewDecoder(client)
			var raw json.RawMessage
			for i := 0; i < tc.replies; i++ {
				if err := dec.Decode(&raw); err != nil {
					t.Fatalf("response %d: %v", i+1, err)
				}
			}

			awaitClosed(t, codec.closed(), "codec closed after the flush failed")
			if err := codec.failed(); !errors.Is(err, errTransport) {
				t.Fatalf("codec recorded %v, want %v", err, errTransport)
			}
		})
	}
}

// An overflow while activate is writing a detached batch fails the
// notifier, and activate must keep that failure: it must not find the
// cleared buffer empty and move to active, which would silently drop the
// overflowed messages and then resume delivery. Fails if activate's drain
// loop does not re-check the state after re-acquiring the lock.
func TestOverflowDuringFlushStaysFailed(t *testing.T) {
	conn, client := newScriptedConn(t)
	go io.Copy(io.Discard, client) // let the released batch write complete
	n, sub := newTestNotifier(t, conn)
	if err := n.Notify(sub.ID, "buffered"); err != nil {
		t.Fatal(err)
	}
	reached, release := conn.gateWrites()
	t.Cleanup(release)
	activated := make(chan error, 1)
	go func() { activated <- n.activate() }()
	issue116AwaitDone(t, reached, "activate reaching the gated write")

	for i := 0; i < maxPendingNotifications; i++ {
		if err := n.Notify(sub.ID, i); err != nil {
			t.Fatalf("notification %d during the flush: %v", i+1, err)
		}
	}
	if err := n.Notify(sub.ID, "overflow"); !errors.Is(err, ErrNotificationBufferFull) {
		t.Fatalf("overflow during the flush: got %v, want %v", err, ErrNotificationBufferFull)
	}
	codec := n.h.conn.(*jsonCodec)
	awaitClosed(t, codec.closed(), "connection torn down after the overflow")
	if err := codec.failed(); !errors.Is(err, ErrNotificationBufferFull) {
		t.Fatalf("codec recorded %v, want %v", err, ErrNotificationBufferFull)
	}

	release()
	if err := <-activated; !errors.Is(err, ErrNotificationBufferFull) {
		t.Fatalf("activate after an overflow during its flush: got %v, want %v", err, ErrNotificationBufferFull)
	}
	if err := n.Notify(sub.ID, "later"); !errors.Is(err, ErrNotificationBufferFull) {
		t.Fatalf("Notify after the overflow: got %v, want %v", err, ErrNotificationBufferFull)
	}
}
