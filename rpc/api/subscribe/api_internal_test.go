package subscribe

import (
	"context"
	"testing"
	"time"

	"github.com/zenon-network/go-zenon/chain"
	"github.com/zenon-network/go-zenon/chain/nom"
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
		if len(clients) > maxSubscriptions {
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
// accepted or the deadline passes; capacity is returned by the worker, which
// runs asynchronously to the caller.
func waitForCapacity(t *testing.T, rpcServer *rpc.Server, args ...interface{}) *rpc.Client {
	t.Helper()
	client := rpc.DialInProc(rpcServer)
	deadline := time.Now().Add(10 * time.Second)
	for {
		_, err := client.Subscribe(context.Background(), "ledger", make(chan interface{}, 1), args...)
		if err == nil {
			return client
		}
		if err.Error() != ErrSubscriptionLimitReached.Error() {
			t.Fatalf("unexpected subscribe error: %v", err)
		}
		if time.Now().After(deadline) {
			t.Fatal("capacity was not returned")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// The server accepts exactly maxSubscriptions live subscriptions across all
// connections; reservations are taken synchronously in subscribe, so the
// count is exact even though installation is asynchronous.
func TestGlobalSubscriptionLimit(t *testing.T) {
	_, rpcServer := startTestServer(t)

	clients, accepted := fillSubscriptions(t, rpcServer, "momentums")
	defer closeAll(clients)
	if accepted != maxSubscriptions {
		t.Fatalf("accepted %d subscriptions, want %d", accepted, maxSubscriptions)
	}

	// Still full on a fresh connection.
	extra := rpc.DialInProc(rpcServer)
	defer extra.Close()
	_, err := extra.Subscribe(context.Background(), "ledger", make(chan interface{}, 1), "momentums")
	if err == nil || err.Error() != ErrSubscriptionLimitReached.Error() {
		t.Fatalf("expected %v, got %v", ErrSubscriptionLimitReached, err)
	}
}

// Unsubscribing returns capacity once the worker visits the subscription
// during a broadcast of its event type.
func TestUnsubscribeReturnsGlobalCapacity(t *testing.T) {
	server, rpcServer := startTestServer(t)

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
	server.InsertMomentum(&nom.DetailedMomentum{Momentum: &nom.Momentum{Height: 1}})

	client := waitForCapacity(t, rpcServer, "momentums")
	defer client.Close()
}

// A connection that goes away without unsubscribing must return its
// capacity even when no event ever matches its subscriptions: address
// filtered subscriptions are only visited by broadcasts that carry a block
// for that address, so the worker sweeps them on a timer.
func TestClosedConnectionsReturnGlobalCapacity(t *testing.T) {
	old := sweepInterval
	sweepInterval = 20 * time.Millisecond
	defer func() { sweepInterval = old }()
	_, rpcServer := startTestServer(t)

	address := types.PillarContract
	clients, accepted := fillSubscriptions(t, rpcServer, "accountBlocksByAddress", address)
	if accepted != maxSubscriptions {
		t.Fatalf("accepted %d subscriptions, want %d", accepted, maxSubscriptions)
	}
	closeAll(clients)

	client := waitForCapacity(t, rpcServer, "accountBlocksByAddress", address)
	defer client.Close()
}
