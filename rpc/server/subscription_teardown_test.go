package server

import (
	"context"
	"encoding/json"
	"io"
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
	// running, when set, makes the callback close it and then wait for its
	// context to end before it creates the subscription.
	running chan struct{}
}

func (s *teardownSubscribeService) Events(ctx context.Context) (*Subscription, error) {
	notifier, ok := NotifierFromContext(ctx)
	if !ok {
		return nil, ErrNotificationsUnsupported
	}
	if s.running != nil {
		close(s.running)
		<-ctx.Done()
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
			// Teardown is now waiting for the call, whose write is still
			// held, so the connection cannot have been closed yet.
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
func TestSubscriptionRegisteredDuringTeardownIsEnded(t *testing.T) {
	svc := &teardownSubscribeService{created: make(chan *Subscription, 1), running: make(chan struct{})}
	srv := NewServer()
	if err := srv.RegisterName("test", svc); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.Stop)

	h := newHandler(context.Background(), &discardWriter{done: make(chan interface{})}, srv.idgen, &srv.services, 0)
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

	// close cancels the call's context, which lets the callback create its
	// subscription, and returns after the call's goroutine has finished.
	closed := make(chan struct{})
	go func() {
		h.close(io.EOF, nil)
		close(closed)
	}()
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
