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
