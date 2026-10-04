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
	// rejectFirst makes the first n calls fail before creating a
	// subscription, then clears itself.
	rejectFirst int
	// panicBeforeCreate and panicAfterCreate make the callback panic, which
	// the RPC server recovers into an error response.
	panicBeforeCreate bool
	panicAfterCreate  bool
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
	if s.rejectFirst > 0 {
		s.rejectFirst--
		return nil, errServiceRejected
	}
	if s.panicBeforeCreate {
		panic("service panicked before creating the subscription")
	}
	sub := notifier.CreateSubscription()
	if s.failAfterCreate {
		return nil, errServiceRejected
	}
	if s.panicAfterCreate {
		panic("service panicked after creating the subscription")
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

// One connection may hold at most DefaultMaxSubscriptionsPerConn server
// subscriptions. Unsubscribing frees a slot and other connections keep their
// own budget.
func TestSubscriptionLimitPerConnection(t *testing.T) {
	server := newSubscriptionTestServer(t, &subscriptionTestService{})
	client := DialInProc(server)
	defer client.Close()

	subs := subscribeN(t, client, DefaultMaxSubscriptionsPerConn)

	_, err := client.Subscribe(context.Background(), "test", make(chan int, 1), "events")
	assertLimitError(t, err)

	subs[0].Unsubscribe()
	subscribeN(t, client, 1)

	other := DialInProc(server)
	defer other.Close()
	subscribeN(t, other, DefaultMaxSubscriptionsPerConn)
}

// The per-connection limit is configurable: a server set to two refuses the
// third subscription on a connection, each connection gets its own two, and a
// value below one restores the default.
func TestConfiguredSubscriptionLimitPerConnection(t *testing.T) {
	server := newSubscriptionTestServer(t, &subscriptionTestService{})
	server.SetMaxSubscriptionsPerConn(2)
	if got := server.MaxSubscriptionsPerConn(); got != 2 {
		t.Fatalf("limit is %d, want 2", got)
	}

	client := DialInProc(server)
	defer client.Close()
	subscribeN(t, client, 2)
	_, err := client.Subscribe(context.Background(), "test", make(chan int, 1), "events")
	assertLimitError(t, err)

	other := DialInProc(server)
	defer other.Close()
	subscribeN(t, other, 2)
	_, err = other.Subscribe(context.Background(), "test", make(chan int, 1), "events")
	assertLimitError(t, err)

	server.SetMaxSubscriptionsPerConn(0)
	if got := server.MaxSubscriptionsPerConn(); got != DefaultMaxSubscriptionsPerConn {
		t.Fatalf("limit after reset is %d, want %d", got, DefaultMaxSubscriptionsPerConn)
	}
	// The reset applies to connections served from now on: a third
	// subscription, refused above, is admitted on a new connection.
	reset := DialInProc(server)
	defer reset.Close()
	subscribeN(t, reset, 3)
}

// A batch cannot exceed the limit either: the reservation is taken per call
// before the service runs, not when the batch's subscriptions are recorded
// at the end.
func TestSubscriptionLimitCoversBatches(t *testing.T) {
	server := newSubscriptionTestServer(t, &subscriptionTestService{})
	client := DialInProc(server)
	defer client.Close()

	const extra = 5
	batch := make([]BatchElem, DefaultMaxSubscriptionsPerConn+extra)
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
	if accepted != DefaultMaxSubscriptionsPerConn || rejected != extra {
		t.Fatalf("accepted %d rejected %d, want %d and %d", accepted, rejected, DefaultMaxSubscriptionsPerConn, extra)
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

	for i := 0; i < DefaultMaxSubscriptionsPerConn+1; i++ {
		_, err := client.Subscribe(context.Background(), "test", make(chan int, 1), "events")
		if err == nil || !strings.Contains(err.Error(), errServiceRejected.Error()) {
			t.Fatalf("attempt %d: expected the service error, got %v", i, err)
		}
	}

	svc.failBeforeCreate = false
	subscribeN(t, client, DefaultMaxSubscriptionsPerConn)
}

// Within one batch, a request the service rejected must not hold its
// reservation against later elements of the same batch: a batch of
// DefaultMaxSubscriptionsPerConn rejections followed by one accepted request must
// accept that last request.
func TestRejectedSubscriptionInBatchFreesSlotForLaterElement(t *testing.T) {
	svc := &subscriptionTestService{rejectFirst: DefaultMaxSubscriptionsPerConn}
	server := newSubscriptionTestServer(t, svc)
	client := DialInProc(server)
	defer client.Close()

	batch := make([]BatchElem, DefaultMaxSubscriptionsPerConn+1)
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
	for i := 0; i < DefaultMaxSubscriptionsPerConn; i++ {
		if batch[i].Error == nil || !strings.Contains(batch[i].Error.Error(), errServiceRejected.Error()) {
			t.Fatalf("batch element %d: expected the service error, got %v", i, batch[i].Error)
		}
	}
	last := batch[DefaultMaxSubscriptionsPerConn]
	if last.Error != nil {
		t.Fatalf("last batch element: expected success after rejected elements, got %v", last.Error)
	}
	if ids[DefaultMaxSubscriptionsPerConn] == "" {
		t.Fatal("last batch element has no subscription ID")
	}

	// Exactly one slot is in use afterwards.
	subscribeN(t, client, DefaultMaxSubscriptionsPerConn-1)
	_, err := client.Subscribe(context.Background(), "test", make(chan int, 1), "events")
	assertLimitError(t, err)
}

// A subscription the service created before returning an error is still
// recorded by the handler (existing behavior), so it holds a slot until the
// connection closes: the reservation is converted, not dropped.
func TestSubscriptionCreatedBeforeErrorHoldsSlot(t *testing.T) {
	svc := &subscriptionTestService{failAfterCreate: true}
	server := newSubscriptionTestServer(t, svc)
	client := DialInProc(server)
	defer client.Close()

	for i := 0; i < DefaultMaxSubscriptionsPerConn; i++ {
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
	subscribeN(t, other, DefaultMaxSubscriptionsPerConn)
}

// Requests that fail argument validation are answered with their own errors,
// not the limit error, and do not consume budget.
func TestSubscriptionLimitCheckedAfterValidation(t *testing.T) {
	server := newSubscriptionTestServer(t, &subscriptionTestService{})
	client := DialInProc(server)
	defer client.Close()

	for i := 0; i < DefaultMaxSubscriptionsPerConn+1; i++ {
		_, err := client.Subscribe(context.Background(), "test", make(chan int, 1), "missing")
		if err == nil || err.Error() == ErrTooManySubscriptions.Error() {
			t.Fatalf("attempt %d: expected a not-found error, got %v", i, err)
		}
	}
	subscribeN(t, client, DefaultMaxSubscriptionsPerConn)
}

// Closing the connection releases everything, so a server that has seen many
// full connections keeps accepting new ones.
func TestSubscriptionLimitResetsPerConnection(t *testing.T) {
	server := newSubscriptionTestServer(t, &subscriptionTestService{})
	for round := 0; round < 3; round++ {
		client := DialInProc(server)
		subscribeN(t, client, DefaultMaxSubscriptionsPerConn)
		_, err := client.Subscribe(context.Background(), "test", make(chan int, 1), "events")
		assertLimitError(t, err)
		client.Close()
	}
}

// A callback that panics is answered with the server's crash error and its
// reservation is accounted like any other return: released when it panicked
// before creating a subscription, converted into a held slot when it
// panicked after (the subscription exists on the connection, as with an
// error returned after creation).
func TestPanickingSubscriptionCallbackKeepsBudgetExact(t *testing.T) {
	t.Run("before create", func(t *testing.T) {
		svc := &subscriptionTestService{panicBeforeCreate: true}
		server := newSubscriptionTestServer(t, svc)
		client := DialInProc(server)
		defer client.Close()

		for i := 0; i < DefaultMaxSubscriptionsPerConn+1; i++ {
			_, err := client.Subscribe(context.Background(), "test", make(chan int, 1), "events")
			if err == nil || !strings.Contains(err.Error(), "crashed") {
				t.Fatalf("attempt %d: expected the crash error, got %v", i, err)
			}
		}
		svc.panicBeforeCreate = false
		subscribeN(t, client, DefaultMaxSubscriptionsPerConn)
	})
	t.Run("after create", func(t *testing.T) {
		svc := &subscriptionTestService{panicAfterCreate: true}
		server := newSubscriptionTestServer(t, svc)
		client := DialInProc(server)
		defer client.Close()

		for i := 0; i < DefaultMaxSubscriptionsPerConn; i++ {
			_, err := client.Subscribe(context.Background(), "test", make(chan int, 1), "events")
			if err == nil || !strings.Contains(err.Error(), "crashed") {
				t.Fatalf("attempt %d: expected the crash error, got %v", i, err)
			}
		}
		svc.panicAfterCreate = false
		_, err := client.Subscribe(context.Background(), "test", make(chan int, 1), "events")
		assertLimitError(t, err)
	})
}

// Single requests sent concurrently on one connection each run on their own
// call goroutine; the reservation is taken under subLock and counts calls in
// flight, so exactly DefaultMaxSubscriptionsPerConn of them are accepted and every
// other one is answered with the limit error.
func TestConcurrentSubscribesRespectPerConnectionLimit(t *testing.T) {
	server := newSubscriptionTestServer(t, &subscriptionTestService{})
	client := DialInProc(server)
	defer client.Close()

	const attempts = 3 * DefaultMaxSubscriptionsPerConn
	results := make(chan error, attempts)
	for i := 0; i < attempts; i++ {
		go func() {
			_, err := client.Subscribe(context.Background(), "test", make(chan int, 1), "events")
			results <- err
		}()
	}
	accepted := 0
	for i := 0; i < attempts; i++ {
		switch err := <-results; {
		case err == nil:
			accepted++
		case err.Error() == ErrTooManySubscriptions.Error():
		case strings.Contains(err.Error(), "too many pending requests"):
			// The connection's admission cap can refuse a request outright
			// while the burst is in flight; that says nothing about the
			// subscription limit, which the top-up below still reaches.
		default:
			t.Fatalf("unexpected subscribe error: %v", err)
		}
	}
	if accepted > DefaultMaxSubscriptionsPerConn {
		t.Fatalf("accepted %d concurrent subscriptions, limit is %d", accepted, DefaultMaxSubscriptionsPerConn)
	}
	// Whatever the burst left, the limit is reached at exactly
	// DefaultMaxSubscriptionsPerConn and not before.
	for accepted < DefaultMaxSubscriptionsPerConn {
		if _, err := client.Subscribe(context.Background(), "test", make(chan int, 1), "events"); err != nil {
			t.Fatalf("subscription %d after the burst failed: %v", accepted+1, err)
		}
		accepted++
	}
	_, err := client.Subscribe(context.Background(), "test", make(chan int, 1), "events")
	assertLimitError(t, err)
}
