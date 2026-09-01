package subscribe

import (
	"context"
	"testing"
	"time"

	"github.com/zenon-network/go-zenon/common"
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
