package subscribe_test

import (
	"context"
	"testing"

	"github.com/zenon-network/go-zenon/chain"
	"github.com/zenon-network/go-zenon/rpc/api/subscribe"
	rpc "github.com/zenon-network/go-zenon/rpc/server"
)

type stubChain struct{ chain.Chain }

func (stubChain) Register(chain.MomentumEventListener)   {}
func (stubChain) UnRegister(chain.MomentumEventListener) {}

// TestSubscribeAfterStopFails asserts that once Server.Stop has run, every
// subsequent subscription attempt fails deterministically. Without the
// Stop/install synchronization the install channel's buffer keeps the send
// case ready after `stopped` closes, so the select in Api.subscribe accepted
// roughly half of post-stop subscriptions, handing the client an ID that no
// worker would ever install; the notifier subscription created before the
// select also stayed registered on the RPC connection even when the stopped
// branch won.
func TestSubscribeAfterStopFails(t *testing.T) {
	server := subscribe.GetSubscribeServer(stubChain{})
	if err := server.Init(); err != nil {
		t.Fatal(err)
	}
	if err := server.Start(); err != nil {
		t.Fatal(err)
	}

	rpcServer := rpc.NewServer()
	if err := rpcServer.RegisterName("ledger", subscribe.GetSubscribeApi()); err != nil {
		t.Fatal(err)
	}
	client := rpc.DialInProc(rpcServer)
	defer client.Close()

	ch := make(chan *subscribe.Momentum, 4)

	// Sanity: the wiring accepts subscriptions while the server is running, so
	// the post-stop loop below cannot pass vacuously.
	sub, err := client.Subscribe(context.Background(), "ledger", ch, "momentums")
	if err != nil {
		t.Fatalf("pre-stop subscription failed: %v", err)
	}
	sub.Unsubscribe()

	if err := server.Stop(); err != nil {
		t.Fatal(err)
	}

	// The install channel has spare capacity here, so only the stopped check
	// (not backpressure) can reject these attempts.
	for i := 0; i < 100; i++ {
		if _, err := client.Subscribe(context.Background(), "ledger", ch, "momentums"); err == nil {
			t.Fatalf("subscription attempt %d accepted after Stop", i)
		}
	}
}
