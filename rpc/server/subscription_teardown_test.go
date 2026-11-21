package server

import (
	"context"
	"encoding/json"
	"io"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

// teardownSubscribeService exposes one subscription method that hands every
// subscription it creates to the test.
type teardownSubscribeService struct {
	created chan *Subscription
	// notifyBeforeReturn sends that many notifications before the call
	// returns, into the notifier's pre-activation buffer.
	notifyBeforeReturn int
	// waitFor, when set, makes the callback close running and then wait
	// for the channel before it creates the subscription.
	waitFor <-chan error
	running chan struct{}
}

func (s *teardownSubscribeService) Events(ctx context.Context) (*Subscription, error) {
	notifier, ok := NotifierFromContext(ctx)
	if !ok {
		return nil, ErrNotificationsUnsupported
	}
	if s.waitFor != nil {
		close(s.running)
		<-s.waitFor
	}
	sub := notifier.CreateSubscription()
	for i := 0; i < s.notifyBeforeReturn; i++ {
		if err := notifier.Notify(sub.ID, i); err != nil {
			return nil, err
		}
	}
	s.created <- sub
	return sub, nil
}

// teardownWaiters counts the goroutines that are inside handler.close
// waiting for the call goroutines. A goroutine is only counted once it has
// entered the wait, so everything handler.close does before the wait has
// happened by then. It reads the goroutines' stacks, because the wait has
// no other effect a test could observe: it fails by timing out in
// awaitTeardownWait if handler.close is renamed or stops using the wait
// group.
func teardownWaiters() int {
	buf := make([]byte, 1<<20)
	for {
		n := runtime.Stack(buf, true)
		if n < len(buf) {
			buf = buf[:n]
			break
		}
		buf = make([]byte, 2*len(buf))
	}
	count := 0
	for _, stack := range strings.Split(string(buf), "\n\n") {
		if strings.Contains(stack, "rpc/server.(*handler).close(") &&
			strings.Contains(stack, "sync.(*WaitGroup).Wait(") {
			count++
		}
	}
	return count
}

// awaitTeardownWait returns once more goroutines than before are waiting
// in handler.close for the call goroutines.
func awaitTeardownWait(t *testing.T, before int) {
	t.Helper()
	deadline := time.Now().Add(teardownTestTimeout)
	for teardownWaiters() <= before {
		if time.Now().After(deadline) {
			t.Fatal("the connection's teardown never reached the wait for the call goroutines")
		}
		time.Sleep(time.Millisecond)
	}
}

// The peer goes away while the goroutine of a subscribe call is held in a
// write to it: the reply, or the activation flush of a notification sent
// before the reply. The subscription's Err channel must report the loss
// without waiting for that write, while the connection itself stays open
// until the call has returned. Fails if handler.close ends the subscriptions
// only after waiting for the call goroutines: the owner of the subscription
// would then learn of the loss when the write returns or reaches its
// deadline.
func TestConnectionLossEndsSubscriptionsBeforeBlockedCallWriteReturns(t *testing.T) {
	for _, tc := range []struct {
		name     string
		buffered int // notifications sent before the call returns
		replies  int // writes let through before the gate
	}{
		{"reply", 0, 0},
		{"activation flush", 1, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc := &teardownSubscribeService{created: make(chan *Subscription, 1), notifyBeforeReturn: tc.buffered}
			srv := NewServer()
			if err := srv.RegisterName("test", svc); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(srv.Stop)
			conn, client, codec := serveScripted(t, srv)
			// Decided by write number, before the request is sent, so the
			// gate cannot race the write it is meant to hold.
			reached, release := conn.gateWritesAfter(tc.replies)
			t.Cleanup(release)

			if _, err := client.Write([]byte(`{"jsonrpc":"2.0","id":1,"method":"test.subscribe","params":["events"]}`)); err != nil {
				t.Fatal(err)
			}
			dec := json.NewDecoder(client)
			for i := 0; i < tc.replies; i++ {
				var raw json.RawMessage
				if err := dec.Decode(&raw); err != nil {
					t.Fatalf("response %d: %v", i+1, err)
				}
			}
			select {
			case <-reached:
			case <-time.After(teardownTestTimeout):
				t.Fatal("the call's write never reached the gate")
			}
			sub := <-svc.created
			waiting := teardownWaiters()

			// The server's read loop sees the peer go and starts teardown.
			client.Close()
			select {
			case err := <-sub.Err():
				if err != io.EOF {
					t.Fatalf("subscription ended with %v, want %v", err, io.EOF)
				}
			case <-time.After(teardownTestTimeout):
				t.Fatal("the subscription was not ended while a call's write was held: " +
					"teardown waited for the call before ending the subscriptions")
			}
			// Once teardown is in its wait for the call, whose write is
			// still held, it does nothing more until the test releases the
			// write. Whatever it did before the wait has happened, so a
			// teardown that closes the connection before the call has
			// returned is seen here in every run.
			awaitTeardownWait(t, waiting)
			select {
			case <-codec.closed():
				t.Fatal("the connection was closed while a call was still writing")
			default:
			}

			release()
			awaitClosed(t, codec.closed(), "codec closed after the held write returned")
		})
	}
}

// A subscribe call still running when the connection goes registers its
// subscription after teardown has ended the ones it found. Teardown ends
// that one as well, once the call has returned. Fails if handler.close ends
// the subscriptions only before waiting for the call goroutines.
//
// The callback creates its subscription only after a subscription that was
// already registered has been ended. That one is ended by the first pass,
// which holds the handler's subscription lock until it is done, and the
// call registers its own under the same lock, so the registration comes
// after the first pass in every run.
func TestSubscriptionRegisteredDuringTeardownIsEnded(t *testing.T) {
	registered := &Subscription{ID: "registered", namespace: "test", err: make(chan error, 1)}
	firstPass := make(chan error)
	passed := sync.OnceFunc(func() { close(firstPass) })
	t.Cleanup(passed) // lets the callback go on a failure path as well
	svc := &teardownSubscribeService{
		created: make(chan *Subscription, 1),
		waitFor: firstPass,
		running: make(chan struct{}),
	}
	srv := NewServer()
	if err := srv.RegisterName("test", svc); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.Stop)

	h := newHandler(context.Background(), &discardWriter{done: make(chan interface{})}, srv.idgen, &srv.services, 0)
	h.subLock.Lock()
	h.serverSubs[registered.ID] = registered
	h.subLock.Unlock()
	h.handleMsg(&jsonrpcMessage{
		Version: vsn,
		ID:      json.RawMessage("1"),
		Method:  "test.subscribe",
		Params:  json.RawMessage(`["events"]`),
	}, false)
	select {
	case <-svc.running:
	case <-time.After(teardownTestTimeout):
		t.Fatal("the subscribe call never started")
	}

	// close ends the registered subscription, which lets the callback
	// create its own, and returns after the call's goroutine has finished.
	closed := make(chan struct{})
	go func() {
		h.close(io.EOF, nil)
		close(closed)
	}()
	select {
	case err := <-registered.Err():
		if err != io.EOF {
			t.Fatalf("registered subscription ended with %v, want %v", err, io.EOF)
		}
	case <-time.After(teardownTestTimeout):
		t.Fatal("teardown did not end the registered subscription while a call was running")
	}
	passed()
	select {
	case <-closed:
	case <-time.After(teardownTestTimeout):
		t.Fatal("handler.close did not return")
	}

	sub := <-svc.created
	select {
	case err := <-sub.Err():
		if err != io.EOF {
			t.Fatalf("subscription ended with %v, want %v", err, io.EOF)
		}
	default:
		t.Fatal("a subscription registered during teardown was left open")
	}
}
