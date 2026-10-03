package subscribe

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/zenon-network/go-zenon/chain"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/types"
	rpc "github.com/zenon-network/go-zenon/rpc/server"
)

// A full install backlog must be reported to the caller instead of blocking
// the enqueue while stopLock is held, otherwise a broadcast stalled on one
// slow client would hold every other subscribe call and Stop behind the lock.
func TestSubscribeBacklogFullDoesNotBlock(t *testing.T) {
	api := &Api{
		log:       common.RPCLogger.New("module", "subscribe_api_test"),
		installCh: make(chan *Subscription, 1),
		stopped:   make(chan struct{}),
	}
	// no worker drains installCh; fill it
	api.installCh <- &Subscription{}

	rpcServer := rpc.NewServer()
	if err := rpcServer.RegisterName("ledger", api); err != nil {
		t.Fatal(err)
	}
	client := rpc.DialInProc(rpcServer)
	defer client.Close()

	ch := make(chan *Momentum, 1)
	done := make(chan error, 1)
	go func() {
		_, err := client.Subscribe(context.Background(), "ledger", ch, "momentums")
		done <- err
	}()

	select {
	case err := <-done:
		if err == nil || err.Error() != ErrSubscribeBacklogFull.Error() {
			t.Fatalf("expected %v, got %v", ErrSubscribeBacklogFull, err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("subscribe blocked on a full backlog")
	}

	// the lock was released, so a later Stop-style acquisition must not hang
	api.stopLock.Lock()
	api.stopLock.Unlock()
}

// backlogProbe is a test-only RPC service that, after a rejected subscribe,
// checks that no subscription was created on the connection's notifier: the
// RPC handler takes and owns whatever the notifier holds even when the method
// returns an error, so a subscription created before the rejection would leak
// under an ID the client never learns.
type backlogProbe struct {
	api                    *Api
	rejectedWithoutSubscri bool
}

func (p *backlogProbe) Momentums(ctx context.Context) (*rpc.Subscription, error) {
	sub, err := p.api.subscribe(ctx, NewMomentumsSubscription())
	if err == ErrSubscribeBacklogFull {
		notifier, _ := rpc.NotifierFromContext(ctx)
		// CreateSubscription panics if the notifier already holds one
		func() {
			defer func() {
				if recover() == nil {
					p.rejectedWithoutSubscri = true
				}
			}()
			notifier.CreateSubscription()
		}()
	}
	return sub, err
}

func TestRejectedSubscribeLeavesNoHandlerOwnedSubscription(t *testing.T) {
	api := &Api{
		log:       common.RPCLogger.New("module", "subscribe_api_test"),
		installCh: make(chan *Subscription, 1),
		stopped:   make(chan struct{}),
	}
	api.installCh <- &Subscription{}
	probe := &backlogProbe{api: api}

	rpcServer := rpc.NewServer()
	if err := rpcServer.RegisterName("ledger", probe); err != nil {
		t.Fatal(err)
	}
	client := rpc.DialInProc(rpcServer)
	defer client.Close()

	ch := make(chan *Momentum, 1)
	if _, err := client.Subscribe(context.Background(), "ledger", ch, "momentums"); err == nil {
		t.Fatal("expected the full backlog to reject the subscription")
	}
	if !probe.rejectedWithoutSubscri {
		t.Fatal("a subscription was created on the notifier before the backlog rejection")
	}
}

type stubChain struct{ chain.Chain }

func (stubChain) Register(chain.MomentumEventListener)   {}
func (stubChain) UnRegister(chain.MomentumEventListener) {}

// startTestServer builds the singleton server the way zenon.Zenon does and
// exposes it through an in-process RPC server, the same path a WebSocket
// client takes after the codec layer.
func startTestServer(t *testing.T) (*Server, *rpc.Server) {
	t.Helper()
	server := GetSubscribeServer(stubChain{})
	if err := server.Init(); err != nil {
		t.Fatal(err)
	}
	if err := server.Start(); err != nil {
		t.Fatal(err)
	}
	rpcServer := rpc.NewServer()
	if err := rpcServer.RegisterName("ledger", GetSubscribeApi()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		rpcServer.Stop()
		if err := server.Stop(); err != nil {
			t.Error(err)
		}
	})
	return server, rpcServer
}

// fillSubscriptions opens connections and subscribes on each until the
// connection's own budget is exhausted, continuing on new connections until
// the server reports its global limit. It returns the open clients and the
// number of accepted subscriptions.
func fillSubscriptions(t *testing.T, rpcServer *rpc.Server, args ...interface{}) ([]*rpc.Client, int) {
	t.Helper()
	var clients []*rpc.Client
	accepted := 0
	for {
		client := rpc.DialInProc(rpcServer)
		clients = append(clients, client)
		for {
			_, err := client.Subscribe(context.Background(), "ledger", make(chan interface{}, 1), args...)
			if err == nil {
				accepted++
				continue
			}
			switch err.Error() {
			case ErrSubscribeBacklogFull.Error():
				// transient: the worker has not drained installCh yet
				time.Sleep(time.Millisecond)
				continue
			case rpc.ErrTooManySubscriptions.Error():
				// this connection is full, move to the next one
			case ErrSubscriptionLimitReached.Error():
				return clients, accepted
			default:
				t.Fatalf("unexpected subscribe error after %d subscriptions: %v", accepted, err)
			}
			break
		}
		if len(clients) > DefaultMaxSubscriptions {
			t.Fatalf("opened %d connections without reaching the global limit", len(clients))
		}
	}
}

func closeAll(clients []*rpc.Client) {
	for _, c := range clients {
		c.Close()
	}
}

// waitForCapacity retries a subscribe on a fresh connection until it is
// accepted or the deadline passes. Capacity is returned by watchers and the
// install backlog is drained by the worker, both asynchronously to the
// caller, so a limit error and a full backlog are both transient here.
func waitForCapacity(t *testing.T, rpcServer *rpc.Server, args ...interface{}) *rpc.Client {
	t.Helper()
	client := rpc.DialInProc(rpcServer)
	deadline := time.Now().Add(10 * time.Second)
	for {
		_, err := client.Subscribe(context.Background(), "ledger", make(chan interface{}, 1), args...)
		if err == nil {
			return client
		}
		switch err.Error() {
		case ErrSubscriptionLimitReached.Error(), ErrSubscribeBacklogFull.Error():
		default:
			t.Fatalf("unexpected subscribe error: %v", err)
		}
		if time.Now().After(deadline) {
			t.Fatal("capacity was not returned")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// The server accepts exactly DefaultMaxSubscriptions live subscriptions across all
// connections; reservations are taken synchronously in subscribe, so the
// count is exact even though installation is asynchronous.
func TestGlobalSubscriptionLimit(t *testing.T) {
	_, rpcServer := startTestServer(t)

	clients, accepted := fillSubscriptions(t, rpcServer, "momentums")
	defer closeAll(clients)
	if accepted != DefaultMaxSubscriptions {
		t.Fatalf("accepted %d subscriptions, want %d", accepted, DefaultMaxSubscriptions)
	}

	// Still full on a fresh connection.
	extra := rpc.DialInProc(rpcServer)
	defer extra.Close()
	_, err := extra.Subscribe(context.Background(), "ledger", make(chan interface{}, 1), "momentums")
	if err == nil || err.Error() != ErrSubscriptionLimitReached.Error() {
		t.Fatalf("expected %v, got %v", ErrSubscriptionLimitReached, err)
	}
}

// The global limit is configurable: set to three, the fourth subscription is
// refused whichever connection it arrives on, and a value below one restores
// the default. The limit is restored afterwards because the server is a
// singleton shared by every test in the package.
func TestConfiguredGlobalSubscriptionLimit(t *testing.T) {
	server, rpcServer := startTestServer(t)
	server.SetMaxSubscriptions(3)
	t.Cleanup(func() { server.SetMaxSubscriptions(0) })
	if got := server.MaxSubscriptions(); got != 3 {
		t.Fatalf("limit is %d, want 3", got)
	}

	clients, accepted := fillSubscriptions(t, rpcServer, "momentums")
	defer closeAll(clients)
	if accepted != 3 {
		t.Fatalf("accepted %d subscriptions, want 3", accepted)
	}

	extra := rpc.DialInProc(rpcServer)
	defer extra.Close()
	_, err := extra.Subscribe(context.Background(), "ledger", make(chan interface{}, 1), "momentums")
	if err == nil || err.Error() != ErrSubscriptionLimitReached.Error() {
		t.Fatalf("expected %v, got %v", ErrSubscriptionLimitReached, err)
	}

	server.SetMaxSubscriptions(0)
	if got := server.MaxSubscriptions(); got != DefaultMaxSubscriptions {
		t.Fatalf("limit after reset is %d, want %d", got, DefaultMaxSubscriptions)
	}
	// The reset applies to the next admission: the fourth subscription,
	// refused above, is accepted now that the limit is back to the default.
	if _, err := extra.Subscribe(context.Background(), "ledger", make(chan interface{}, 1), "momentums"); err != nil {
		t.Fatalf("subscribe after reset: %v", err)
	}
}

// Unsubscribing returns capacity as soon as the subscription's watcher sees
// the client let go; no broadcast of its event type is needed.
func TestUnsubscribeReturnsGlobalCapacity(t *testing.T) {
	_, rpcServer := startTestServer(t)

	first := rpc.DialInProc(rpcServer)
	defer first.Close()
	ch := make(chan *Momentum, 1)
	sub, err := first.Subscribe(context.Background(), "ledger", ch, "momentums")
	if err != nil {
		t.Fatal(err)
	}
	clients, _ := fillSubscriptions(t, rpcServer, "momentums")
	defer closeAll(clients)

	sub.Unsubscribe()

	client := waitForCapacity(t, rpcServer, "momentums")
	defer client.Close()
}

// A connection that goes away without unsubscribing must return its
// capacity even when no event ever matches its subscriptions: address
// filtered subscriptions are only visited by broadcasts that carry a block
// for that address, so removal must not depend on a broadcast.
func TestClosedConnectionsReturnGlobalCapacity(t *testing.T) {
	_, rpcServer := startTestServer(t)

	address := types.PillarContract
	clients, accepted := fillSubscriptions(t, rpcServer, "accountBlocksByAddress", address)
	if accepted != DefaultMaxSubscriptions {
		t.Fatalf("accepted %d subscriptions, want %d", accepted, DefaultMaxSubscriptions)
	}
	closeAll(clients)

	client := waitForCapacity(t, rpcServer, "accountBlocksByAddress", address)
	defer client.Close()
}

// A client that repeatedly subscribes and unsubscribes on one connection
// stays within its own budget. Each unsubscribe must return the global slot
// as well, without waiting for a matching event: closed entries must not
// count against the global limit in the meantime.
func TestUnsubscribeChurnDoesNotStarveGlobalCapacity(t *testing.T) {
	_, rpcServer := startTestServer(t)

	address := types.PillarContract
	churner := rpc.DialInProc(rpcServer)
	defer churner.Close()
	const perRound = 64
	for round := 0; round*perRound < DefaultMaxSubscriptions; round++ {
		subs := make([]*rpc.ClientSubscription, 0, perRound)
		for len(subs) < perRound {
			sub, err := churner.Subscribe(context.Background(), "ledger", make(chan interface{}, 1), "accountBlocksByAddress", address)
			if err != nil {
				if err.Error() == ErrSubscribeBacklogFull.Error() {
					time.Sleep(time.Millisecond)
					continue
				}
				t.Fatalf("round %d: subscribe %d failed: %v", round, len(subs), err)
			}
			subs = append(subs, sub)
		}
		for _, sub := range subs {
			sub.Unsubscribe()
		}
	}

	// The churner holds nothing now; a fresh client must be admitted.
	client := waitForCapacity(t, rpcServer, "accountBlocksByAddress", address)
	defer client.Close()
}

// startTestServerWithoutWorker builds the singleton server like
// startTestServer but never starts the worker, so nothing drains installCh
// or broadcasts: the test plays the worker itself by taking queued entries
// with takeQueued and calling install, which makes the ordering between
// install, the client letting go, and the worker's own cleanup explicit.
func startTestServerWithoutWorker(t *testing.T) (*Server, *rpc.Server) {
	t.Helper()
	server := GetSubscribeServer(stubChain{})
	if err := server.Init(); err != nil {
		t.Fatal(err)
	}
	rpcServer := rpc.NewServer()
	if err := rpcServer.RegisterName("ledger", server.Api); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		rpcServer.Stop()
		// Stop waits for every watcher, so a watcher that does not
		// terminate hangs the test here.
		if err := server.Stop(); err != nil {
			t.Error(err)
		}
	})
	return server, rpcServer
}

func takeQueued(t *testing.T, server *Server) *Subscription {
	t.Helper()
	select {
	case sub := <-server.installCh:
		return sub
	case <-time.After(5 * time.Second):
		t.Fatal("no subscription was queued for install")
		return nil
	}
}

func isInstalled(server *Server, sub *Subscription) bool {
	server.subsMu.Lock()
	defer server.subsMu.Unlock()
	_, ok := server.subscriptions[sub.options.subscriptionType][sub.rpc.ID]
	return ok
}

func waitUninstalled(t *testing.T, server *Server, sub *Subscription) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for isInstalled(server, sub) {
		if time.Now().After(deadline) {
			t.Fatal("subscription was not uninstalled")
		}
		time.Sleep(time.Millisecond)
	}
}

// An installed subscription is removed, and its global slot returned, when
// the client unsubscribes or disconnects even though no worker runs at all:
// reclamation must not depend on the worker visiting the entry, which a
// broadcast-driven sweep would, and a stalled worker must not hold slots.
func TestClientLettingGoReclaimsSlotWithoutWorker(t *testing.T) {
	for _, tc := range []struct {
		name  string
		letGo func(client *rpc.Client, sub *rpc.ClientSubscription)
	}{
		{"unsubscribe", func(_ *rpc.Client, sub *rpc.ClientSubscription) { sub.Unsubscribe() }},
		{"disconnect", func(client *rpc.Client, _ *rpc.ClientSubscription) { client.Close() }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server, rpcServer := startTestServerWithoutWorker(t)
			client := rpc.DialInProc(rpcServer)
			defer client.Close()

			clientSub, err := client.Subscribe(context.Background(), "ledger", make(chan interface{}, 1), "momentums")
			if err != nil {
				t.Fatal(err)
			}
			queued := takeQueued(t, server)
			server.install(queued)
			if !isInstalled(server, queued) || server.live.Load() != 1 {
				t.Fatalf("expected one installed subscription holding one slot, live=%d", server.live.Load())
			}

			tc.letGo(client, clientSub)

			waitUninstalled(t, server, queued)
			if live := server.live.Load(); live != 0 {
				t.Fatalf("slot not returned exactly once, live=%d", live)
			}
		})
	}
}

// The worker owns subscription.notifier and clears it in Closed when the
// next broadcast finds the client gone. That can happen right after install,
// before the entry's watcher has run, so the watcher must not read the field:
// it has to wait on signals captured by install. The client lets go while
// the entry is still queued, then the worker installs it and immediately
// performs its broadcast-side cleanup. Under the race detector this fails
// if the watcher reads the field; without it, a watcher that runs after the
// field is cleared dereferences nil. Either way the slot must be released
// exactly once and the watcher must still terminate on Stop.
func TestWatcherIgnoresWorkerCleanupOfLeftEntry(t *testing.T) {
	server, rpcServer := startTestServerWithoutWorker(t)
	client := rpc.DialInProc(rpcServer)
	defer client.Close()

	clientSub, err := client.Subscribe(context.Background(), "ledger", make(chan interface{}, 1), "momentums")
	if err != nil {
		t.Fatal(err)
	}
	queued := takeQueued(t, server)
	clientSub.Unsubscribe()

	// worker: install, then the next broadcast sees the client gone
	server.install(queued)
	if !queued.Closed() {
		t.Fatal("expected the entry to report its client gone")
	}
	server.uninstall(queued)

	waitUninstalled(t, server, queued)
	if live := server.live.Load(); live != 0 {
		t.Fatalf("slot not returned exactly once, live=%d", live)
	}
}

// Connections racing for the last slots cannot overshoot the global limit:
// the check and the increment in subscribe are serialized by stopLock, so
// concurrent subscribe calls from many connections are accepted exactly
// DefaultMaxSubscriptions times in total and the next request is rejected.
func TestGlobalSubscriptionLimitUnderConcurrentAdmission(t *testing.T) {
	_, rpcServer := startTestServer(t)

	const connections = 2 * DefaultMaxSubscriptions / 64
	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		accepted int
		clients  []*rpc.Client
		failure  error
	)
	for i := 0; i < connections; i++ {
		client := rpc.DialInProc(rpcServer)
		clients = append(clients, client)
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				_, err := client.Subscribe(context.Background(), "ledger", make(chan interface{}, 1), "momentums")
				if err == nil {
					mu.Lock()
					accepted++
					mu.Unlock()
					continue
				}
				switch err.Error() {
				case ErrSubscribeBacklogFull.Error():
					time.Sleep(time.Millisecond)
					continue
				case rpc.ErrTooManySubscriptions.Error(), ErrSubscriptionLimitReached.Error():
					return
				default:
					mu.Lock()
					failure = err
					mu.Unlock()
					return
				}
			}
		}()
	}
	wg.Wait()
	defer closeAll(clients)
	if failure != nil {
		t.Fatalf("unexpected subscribe error: %v", failure)
	}
	if accepted != DefaultMaxSubscriptions {
		t.Fatalf("accepted %d subscriptions concurrently, want %d", accepted, DefaultMaxSubscriptions)
	}

	extra := rpc.DialInProc(rpcServer)
	defer extra.Close()
	_, err := extra.Subscribe(context.Background(), "ledger", make(chan interface{}, 1), "momentums")
	if err == nil || err.Error() != ErrSubscriptionLimitReached.Error() {
		t.Fatalf("expected %v, got %v", ErrSubscriptionLimitReached, err)
	}
}
