package server

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// subscriptionTestService exposes one subscription method that behaves like
// a normal service callback: it creates the notifier subscription and returns
// it. failBeforeCreate makes it return an error without creating anything,
// which is how a service reports a rejected request.
type subscriptionTestService struct {
	failBeforeCreate bool
	failAfterCreate  bool
}

var errServiceRejected = errors.New("service rejected the subscription")

func (s *subscriptionTestService) Events(ctx context.Context) (*Subscription, error) {
	notifier, ok := NotifierFromContext(ctx)
	if !ok {
		return nil, ErrNotificationsUnsupported
	}
	if s.failBeforeCreate {
		return nil, errServiceRejected
	}
	sub := notifier.CreateSubscription()
	if s.failAfterCreate {
		return nil, errServiceRejected
	}
	return sub, nil
}

func newSubscriptionTestServer(t *testing.T, svc *subscriptionTestService) *Server {
	t.Helper()
	server := NewServer()
	if err := server.RegisterName("test", svc); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(server.Stop)
	return server
}

func subscribeN(t *testing.T, client *Client, n int) []*ClientSubscription {
	t.Helper()
	subs := make([]*ClientSubscription, 0, n)
	for i := 0; i < n; i++ {
		sub, err := client.Subscribe(context.Background(), "test", make(chan int, 1), "events")
		if err != nil {
			t.Fatalf("subscription %d of %d failed: %v", i+1, n, err)
		}
		subs = append(subs, sub)
	}
	return subs
}

func assertLimitError(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("expected the per-connection subscription limit to reject the request")
	}
	if err.Error() != ErrTooManySubscriptions.Error() {
		t.Fatalf("expected %q, got %q", ErrTooManySubscriptions, err)
	}
}

// One connection may hold at most maxSubscriptionsPerConn server
// subscriptions. Unsubscribing frees a slot and other connections keep their
// own budget.
func TestSubscriptionLimitPerConnection(t *testing.T) {
	server := newSubscriptionTestServer(t, &subscriptionTestService{})
	client := DialInProc(server)
	defer client.Close()

	subs := subscribeN(t, client, maxSubscriptionsPerConn)

	_, err := client.Subscribe(context.Background(), "test", make(chan int, 1), "events")
	assertLimitError(t, err)

	subs[0].Unsubscribe()
	subscribeN(t, client, 1)

	other := DialInProc(server)
	defer other.Close()
	subscribeN(t, other, maxSubscriptionsPerConn)
}

// A batch cannot exceed the limit either: the reservation is taken per call
// before the service runs, not when the batch's subscriptions are recorded
// at the end.
func TestSubscriptionLimitCoversBatches(t *testing.T) {
	server := newSubscriptionTestServer(t, &subscriptionTestService{})
	client := DialInProc(server)
	defer client.Close()

	const extra = 5
	batch := make([]BatchElem, maxSubscriptionsPerConn+extra)
	ids := make([]string, len(batch))
	for i := range batch {
		batch[i] = BatchElem{
			Method: "test.subscribe",
			Args:   []interface{}{"events"},
			Result: &ids[i],
		}
	}
	if err := client.BatchCall(batch); err != nil {
		t.Fatal(err)
	}
	accepted, rejected := 0, 0
	for i, elem := range batch {
		switch {
		case elem.Error == nil:
			if ids[i] == "" {
				t.Fatalf("batch element %d has no error but no subscription ID", i)
			}
			accepted++
		case elem.Error.Error() == ErrTooManySubscriptions.Error():
			rejected++
		default:
			t.Fatalf("batch element %d: unexpected error %v", i, elem.Error)
		}
	}
	if accepted != maxSubscriptionsPerConn || rejected != extra {
		t.Fatalf("accepted %d rejected %d, want %d and %d", accepted, rejected, maxSubscriptionsPerConn, extra)
	}

	// The connection is full now.
	_, err := client.Subscribe(context.Background(), "test", make(chan int, 1), "events")
	assertLimitError(t, err)

	// Unsubscribing an ID accepted in the batch frees its slot.
	var ok bool
	if err := client.Call(&ok, "test.unsubscribe", ids[0]); err != nil || !ok {
		t.Fatalf("unsubscribe failed: ok=%v err=%v", ok, err)
	}
	subscribeN(t, client, 1)
}

// A request the service rejects before creating a subscription must not
// consume the connection's budget.
func TestRejectedSubscriptionReleasesReservation(t *testing.T) {
	svc := &subscriptionTestService{failBeforeCreate: true}
	server := newSubscriptionTestServer(t, svc)
	client := DialInProc(server)
	defer client.Close()

	for i := 0; i < maxSubscriptionsPerConn+1; i++ {
		_, err := client.Subscribe(context.Background(), "test", make(chan int, 1), "events")
		if err == nil || !strings.Contains(err.Error(), errServiceRejected.Error()) {
			t.Fatalf("attempt %d: expected the service error, got %v", i, err)
		}
	}

	svc.failBeforeCreate = false
	subscribeN(t, client, maxSubscriptionsPerConn)
}

// A subscription the service created before returning an error is still
// recorded by the handler (existing behavior), so it holds a slot until the
// connection closes: the reservation is converted, not dropped.
func TestSubscriptionCreatedBeforeErrorHoldsSlot(t *testing.T) {
	svc := &subscriptionTestService{failAfterCreate: true}
	server := newSubscriptionTestServer(t, svc)
	client := DialInProc(server)
	defer client.Close()

	for i := 0; i < maxSubscriptionsPerConn; i++ {
		_, err := client.Subscribe(context.Background(), "test", make(chan int, 1), "events")
		if err == nil || !strings.Contains(err.Error(), errServiceRejected.Error()) {
			t.Fatalf("attempt %d: expected the service error, got %v", i, err)
		}
	}
	_, err := client.Subscribe(context.Background(), "test", make(chan int, 1), "events")
	assertLimitError(t, err)

	// A fresh connection is unaffected.
	other := DialInProc(server)
	defer other.Close()
	svc.failAfterCreate = false
	subscribeN(t, other, maxSubscriptionsPerConn)
}

// Requests that fail argument validation are answered with their own errors,
// not the limit error, and do not consume budget.
func TestSubscriptionLimitCheckedAfterValidation(t *testing.T) {
	server := newSubscriptionTestServer(t, &subscriptionTestService{})
	client := DialInProc(server)
	defer client.Close()

	for i := 0; i < maxSubscriptionsPerConn+1; i++ {
		_, err := client.Subscribe(context.Background(), "test", make(chan int, 1), "missing")
		if err == nil || err.Error() == ErrTooManySubscriptions.Error() {
			t.Fatalf("attempt %d: expected a not-found error, got %v", i, err)
		}
	}
	subscribeN(t, client, maxSubscriptionsPerConn)
}

// Closing the connection releases everything, so a server that has seen many
// full connections keeps accepting new ones.
func TestSubscriptionLimitResetsPerConnection(t *testing.T) {
	server := newSubscriptionTestServer(t, &subscriptionTestService{})
	for round := 0; round < 3; round++ {
		client := DialInProc(server)
		subscribeN(t, client, maxSubscriptionsPerConn)
		_, err := client.Subscribe(context.Background(), "test", make(chan int, 1), "events")
		assertLimitError(t, err)
		client.Close()
	}
}
