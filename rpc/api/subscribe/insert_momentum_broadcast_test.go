package subscribe

import (
	"context"
	"testing"
	"time"

	"github.com/zenon-network/go-zenon/chain"
	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common/types"
	rpc "github.com/zenon-network/go-zenon/rpc/server"
)

type noopChain struct{ chain.Chain }

func (noopChain) Register(chain.MomentumEventListener)   {}
func (noopChain) UnRegister(chain.MomentumEventListener) {}

// waitInstalled blocks until the worker has installed n subscriptions of the
// given type. Subscribe only queues the entry; the worker installs it on its
// next loop iteration, and a momentum inserted before that would be
// broadcast to nobody.
func waitInstalled(t *testing.T, server *Server, subscriptionType SubscriptionType, n int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if len(server.installed(subscriptionType)) == n {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("subscription type %d not installed within timeout", subscriptionType)
}

func recvBlocks(t *testing.T, ch <-chan []*AccountBlock, what string) []*AccountBlock {
	t.Helper()
	select {
	case blocks := <-ch:
		return blocks
	case <-time.After(5 * time.Second):
		t.Fatalf("no %s notification within timeout", what)
		return nil
	}
}

func hashCounts(blocks []*AccountBlock) map[types.Hash]int {
	counts := make(map[types.Hash]int, len(blocks))
	for _, block := range blocks {
		counts[block.Hash]++
	}
	return counts
}

// TestInsertMomentum_DescendantNotifiedOnce drives a momentum through the
// real worker and RPC notifier and asserts that a descendant block reaches
// each subscription exactly once.
//
// A contract receive that returns tokens (plasma CancelFuse, stake cancel,
// sentinel revoke, HTLC reclaim) produces a descendant send that the pool
// commits as its own account block at its own height, and the producer lists
// every pending height, so the momentum content carries the descendant and
// its parent as separate entries. Flattening the parent's DescendantBlocks on
// top of that listed the descendant twice.
func TestInsertMomentum_DescendantNotifiedOnce(t *testing.T) {
	server := GetSubscribeServer(noopChain{})
	if err := server.Init(); err != nil {
		t.Fatal(err)
	}
	if err := server.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Stop() })

	rpcServer := rpc.NewServer()
	if err := rpcServer.RegisterName("ledger", GetSubscribeApi()); err != nil {
		t.Fatal(err)
	}
	client := rpc.DialInProc(rpcServer)
	defer client.Close()

	user := types.ParseAddressPanic("z1qqdt06lnwz57x38rwlyutcx5wgrtl0ynkfe3kv")
	descendantHash := types.HexToHashPanic("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	parentHash := types.HexToHashPanic("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")

	// Refund send from the contract to the user, committed at height 4.
	descendant := &nom.AccountBlock{
		BlockType: nom.BlockTypeContractSend,
		Hash:      descendantHash,
		Height:    4,
		Address:   types.PlasmaContract,
		ToAddress: user,
	}
	// The contract receive that produced it, committed at height 5 and
	// carrying the descendant in its batch.
	parent := &nom.AccountBlock{
		BlockType:        nom.BlockTypeContractReceive,
		Hash:             parentHash,
		Height:           5,
		Address:          types.PlasmaContract,
		FromBlockHash:    types.HexToHashPanic("cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"),
		DescendantBlocks: []*nom.AccountBlock{descendant},
	}
	detailed := &nom.DetailedMomentum{
		Momentum:      &nom.Momentum{Hash: types.HexToHashPanic("dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"), Height: 10},
		AccountBlocks: []*nom.AccountBlock{descendant, parent},
	}

	ctx := context.Background()
	unreceivedCh := make(chan []*AccountBlock, 4)
	if _, err := client.Subscribe(ctx, "ledger", unreceivedCh, "unreceivedAccountBlocksByAddress", user); err != nil {
		t.Fatal(err)
	}
	byAddressCh := make(chan []*AccountBlock, 4)
	if _, err := client.Subscribe(ctx, "ledger", byAddressCh, "accountBlocksByAddress", types.PlasmaContract); err != nil {
		t.Fatal(err)
	}
	allCh := make(chan []*AccountBlock, 4)
	if _, err := client.Subscribe(ctx, "ledger", allCh, "allAccountBlocks"); err != nil {
		t.Fatal(err)
	}
	waitInstalled(t, server, UnreceivedAccountBlocksSubscriptionByAddress, 1)
	waitInstalled(t, server, AccountBlocksSubscriptionByAddress, 1)
	waitInstalled(t, server, AllAccountBlocksSubscription, 1)

	server.InsertMomentum(detailed)

	// One momentum is one broadcast per subscription, so a single
	// notification carries everything the subscriber will ever see for it.
	unreceived := recvBlocks(t, unreceivedCh, "unreceived")
	if counts := hashCounts(unreceived); len(unreceived) != 1 || counts[descendantHash] != 1 {
		t.Errorf("unreceived blocks for the user: want the refund once, got %d entries, refund %d times", len(unreceived), counts[descendantHash])
	}

	byAddress := recvBlocks(t, byAddressCh, "by-address")
	if counts := hashCounts(byAddress); len(byAddress) != 2 || counts[descendantHash] != 1 || counts[parentHash] != 1 {
		t.Errorf("blocks for the contract: want refund and receive once each, got %d entries, refund %d times, receive %d times", len(byAddress), counts[descendantHash], counts[parentHash])
	}

	all := recvBlocks(t, allCh, "all")
	if counts := hashCounts(all); len(all) != 2 || counts[descendantHash] != 1 || counts[parentHash] != 1 {
		t.Errorf("all blocks: want refund and receive once each, got %d entries, refund %d times, receive %d times", len(all), counts[descendantHash], counts[parentHash])
	}
}
