package protocol_test

// Characterization tests for the 30-momentum rollback cap in the side-chain
// import path (protocol/chain_bridge.go refuses when
// ourFrontier.Height-target.Height > 30), pinned for go-zenon issue #312:
// a pillar that stale-produced past the cap cannot be brought back by resumed
// block service alone; only an explicit rollback within the cap admits the
// network's branch again. Requested by 0x3639 on PR #306.

import (
	"strings"
	"testing"

	"github.com/zenon-network/go-zenon/common"
)

// TestInsertChain_StaleProductionDepthThirtyOne: local frontier 36, fork base
// 5, so 31 self-produced momentums sit above the common ancestor. The network's
// one-momentum-longer branch is refused before anything is removed, the refusal
// survives continued local growth (service resumed, stale tip advances to 38,
// depth 33), and only an explicit rollback back within the cap admits the
// branch.
func TestInsertChain_StaleProductionDepthThirtyOne(t *testing.T) {
	f := newForkFixture(t, 36, 37)
	defer f.z.StopPanic()

	counting, bridge := newSideChainBridge(f.z)
	_, err := bridge.InsertChain(f.fork)
	if err == nil || !strings.Contains(err.Error(), "Too far") {
		t.Fatalf("expected too-far refusal at depth 31, got %v", err)
	}
	common.Expect(t, counting.rollbacks, 0)
	expectChainEquals(t, f.z.Chain(), f.original)

	// Service resumes on the stale tip: two more locally produced momentums.
	f.z.InsertMomentumsTo(38)
	counting2, bridge2 := newSideChainBridge(f.z)
	_, err = bridge2.InsertChain(f.fork)
	if err == nil || !strings.Contains(err.Error(), "Too far") {
		t.Fatalf("expected too-far refusal at depth 33 after resumed service, got %v", err)
	}
	common.Expect(t, counting2.rollbacks, 0)

	// Recovery control: an explicit rollback that brings the frontier back
	// within the cap (one momentum above the fork point) admits the same
	// branch; the replacement rolls the remaining local momentum back.
	insert := f.z.Chain().AcquireInsert("test rollback")
	common.FailIfErr(t, f.z.Chain().RollbackTo(insert, momentumAt(t, f.z.Chain(), 6).Identifier()))
	insert.Unlock()
	counting3, bridge3 := newSideChainBridge(f.z)
	_, err = bridge3.InsertChain(f.fork)
	common.FailIfErr(t, err)
	common.Expect(t, counting3.rollbacks, 1)
	common.Expect(t, f.z.Chain().GetFrontierMomentumStore().Identifier(), f.fork[len(f.fork)-1].Momentum.Identifier())
}

// TestInsertChain_StaleProductionDepthThirty: same geometry one momentum
// shallower (frontier 35, fork base 5, depth 30). The identical branch
// replaces the local one — the boundary is exactly 30 vs 31.
func TestInsertChain_StaleProductionDepthThirty(t *testing.T) {
	f := newForkFixture(t, 35, 36)
	defer f.z.StopPanic()

	counting, bridge := newSideChainBridge(f.z)
	_, err := bridge.InsertChain(f.fork)
	common.FailIfErr(t, err)
	common.Expect(t, counting.rollbacks, 1)
	common.Expect(t, f.z.Chain().GetFrontierMomentumStore().Identifier(), f.fork[len(f.fork)-1].Momentum.Identifier())
}
