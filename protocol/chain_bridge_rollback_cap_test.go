package protocol_test

// Characterization tests for the 30-momentum rollback cap in the side-chain
// import path (protocol/chain_bridge.go refuses when
// ourFrontier.Height-target.Height > 30), pinned for go-zenon issue #312:
// a pillar that stale-produced past the cap cannot be brought back by resumed
// block service alone; only an explicit rollback within the cap admits the
// network's branch again.

import (
	"testing"

	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/protocol/downloader"
)

// TestInsertChain_StaleProductionDepthThirtyOne: local frontier 36, fork base
// 5, so 31 self-produced momentums sit above the common ancestor. The network's
// branch (ending at 39, longer than the local tip at every step) is refused
// before anything is removed, the refusal survives continued local growth
// (service resumed, stale tip advances to 38, depth 33), and only an explicit
// rollback back within the cap admits the branch. All three attempts go
// through one bridge, so the refusal is also shown not to be remembered.
func TestInsertChain_StaleProductionDepthThirtyOne(t *testing.T) {
	f := newForkFixture(t, 36, 39)
	defer f.z.StopPanic()
	common.Expect(t, f.removed, 31)

	counting, bridge := newSideChainBridge(f.z)
	_, err := bridge.InsertChain(f.fork)
	expectBatchVerdict(t, bridge, err, "Too far", downloader.ErrBatchMisplaced)
	common.Expect(t, counting.rollbacks, 0)
	expectChainEquals(t, f.z.Chain(), f.original)

	// Service resumes on the stale tip: two more locally produced momentums.
	f.z.InsertMomentumsTo(38)
	expectFrontierAt(t, f.z.Chain(), momentumAt(t, f.z.Chain(), 38))
	grown := snapshotChain(t, f.z.Chain())
	_, err = bridge.InsertChain(f.fork)
	expectBatchVerdict(t, bridge, err, "Too far", downloader.ErrBatchMisplaced)
	common.Expect(t, counting.rollbacks, 0)
	expectChainEquals(t, f.z.Chain(), grown)

	// Recovery control: an explicit rollback of the canonical chain and the
	// cache to height 35, depth 30 above the fork base, the shallowest
	// rollback that should recover, admits the same branch on the same
	// bridge; the replacement then removes the full 30.
	at35 := momentumAt(t, f.z.Chain(), 35)
	insert := f.z.Chain().AcquireInsert("test rollback")
	common.FailIfErr(t, f.z.Chain().RollbackTo(insert, at35.Identifier()))
	common.FailIfErr(t, f.z.Chain().RollbackCacheTo(insert, at35.Identifier()))
	insert.Unlock()
	expectFrontierAt(t, f.z.Chain(), at35)
	_, err = bridge.InsertChain(f.fork)
	common.FailIfErr(t, err)
	common.Expect(t, counting.rollbacks, 1)
	expectFrontierAt(t, f.z.Chain(), f.fork[len(f.fork)-1].Momentum)
}

// TestInsertChain_StaleProductionDepthThirty: same geometry one momentum
// shallower (frontier 35, fork base 5, depth 30). The identical branch
// replaces the local one — the boundary is exactly 30 vs 31.
func TestInsertChain_StaleProductionDepthThirty(t *testing.T) {
	f := newForkFixture(t, 35, 39)
	defer f.z.StopPanic()
	common.Expect(t, f.removed, 30)

	counting, bridge := newSideChainBridge(f.z)
	_, err := bridge.InsertChain(f.fork)
	common.FailIfErr(t, err)
	common.Expect(t, counting.rollbacks, 1)
	expectFrontierAt(t, f.z.Chain(), f.fork[len(f.fork)-1].Momentum)
}
