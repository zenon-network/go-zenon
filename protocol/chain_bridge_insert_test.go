package protocol_test

import (
	"testing"

	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/protocol"
	"github.com/zenon-network/go-zenon/zenon/mock"
)

// TestInsertChain_SideChainAboveFrontierDoesNotPanic covers the reorg path where
// the downloader delivers a side-chain whose head is more than one momentum above
// our frontier: the momentum at head.Height-1 is not in the store, so
// GetMomentumByHeight returns (nil, nil) and InsertChain must treat the missing
// momentum as an error rather than dereference it. The downloader goroutine has
// no recover, so a panic here would take down the whole node.
func TestInsertChain_SideChainAboveFrontierDoesNotPanic(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()
	z.InsertMomentumsTo(10) // frontier at height 10

	frontier, err := z.Chain().GetFrontierMomentumStore().GetFrontierMomentum()
	if err != nil {
		t.Fatal(err)
	}

	// Head two above the frontier: head.Height-1 (== frontier+1) is not stored,
	// and head.Previous() != frontier.Identifier(), so we enter the side-chain
	// branch and look up the missing height.
	head := &nom.Momentum{
		ChainIdentifier: frontier.ChainIdentifier,
		Height:          frontier.Height + 2,
		PreviousHash:    types.HexToHashPanic("ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"),
		TimestampUnix:   frontier.TimestampUnix + 20,
	}
	head.Hash = head.ComputeHash()

	cb := protocol.NewChainBridge(z.Chain(), nil, nil, nil)

	// Pre-fix: panics on the nil momentum. Post-fix: returns a clean error the
	// downloader can act on (drop peer, retry).
	if _, err := cb.InsertChain([]*nom.DetailedMomentum{{Momentum: head}}); err == nil {
		t.Fatal("expected an error for an unlinkable side-chain above the frontier, got nil")
	}
}
