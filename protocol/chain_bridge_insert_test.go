package protocol_test

import (
	"testing"

	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/protocol"
	"github.com/zenon-network/go-zenon/zenon/mock"
)

// TestInsertChain_SideChainAboveFrontierDoesNotPanic reproduces the reorg crash:
// when the downloader delivers a side-chain whose head is more than one momentum
// above our frontier, InsertChain looked up the momentum at head.Height-1 (which
// is not in the store), got (nil, nil) back from GetMomentumByHeight, checked only
// the error, and dereferenced the nil momentum at chain_bridge.go:160 -> SIGSEGV.
// The downloader goroutine has no recover, so this crashes the whole node.
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
