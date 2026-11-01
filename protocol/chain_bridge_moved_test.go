package protocol_test

import (
	"testing"

	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common"
)

// InsertChainMoved reports, under the insert lock, whether the call left the
// frontier higher than it found it: not for a batch the node already had,
// not for an extension that was rejected, yes for a valid extension. On this
// release line a side-chain replacement is rolled back to the fork point
// before it is applied and is not restored when it fails (public dev
// restores it, #83), so a rejected replacement leaves the frontier below
// where it started: that reports false, the retry from the fork point is an
// extension and reports false, and only re-advancing the chain reports true
// again. The last case is run three times over, since the node produces and
// is rolled back again when the same branch is served again.
func TestInsertChainMoved_ReportsOnlyRetainedProgress(t *testing.T) {
	t.Run("valid extension", func(t *testing.T) {
		f := newRestoreFixture(t)
		defer f.z.StopPanic()
		_, moved, err := f.bridge.InsertChainMoved([]*nom.DetailedMomentum{f.detailed})
		common.FailIfErr(t, err)
		common.Expect(t, moved, true)
		common.Expect(t, f.z.Chain().GetFrontierMomentumStore().Identifier(), f.detailed.Momentum.Identifier())
	})
	t.Run("batch already known", func(t *testing.T) {
		f := newRestoreFixture(t)
		defer f.z.StopPanic()
		_, err := f.bridge.InsertChain([]*nom.DetailedMomentum{f.detailed})
		common.FailIfErr(t, err)
		_, moved, err := f.bridge.InsertChainMoved([]*nom.DetailedMomentum{f.detailed})
		common.FailIfErr(t, err)
		common.Expect(t, moved, false)
	})
	t.Run("rejected extension", func(t *testing.T) {
		f := newRestoreFixture(t)
		defer f.z.StopPanic()
		_, moved, err := f.bridge.InsertChainMoved([]*nom.DetailedMomentum{f.tamperedMomentum()})
		if err == nil {
			t.Fatal("expected the tampered momentum to be rejected")
		}
		common.Expect(t, moved, false)
		common.Expect(t, f.z.Chain().GetFrontierMomentumStore().Identifier(), f.previous)
	})
	t.Run("rejected replacement, retried, chain re-advanced", func(t *testing.T) {
		f := newRestoreFixture(t)
		defer f.z.StopPanic()
		side := f.rejectedSideChain()
		for round := 0; round < 3; round++ {
			// The node is on its own momentum above the fork point.
			_, moved, err := f.bridge.InsertChainMoved([]*nom.DetailedMomentum{f.detailed})
			common.FailIfErr(t, err)
			common.Expect(t, moved, true)
			frontier := f.z.Chain().GetFrontierMomentumStore().Identifier()
			common.Expect(t, frontier, f.detailed.Momentum.Identifier())

			// A longer side chain forks below it; the rollback to the fork
			// point happens, the first foreign momentum is rejected, and the
			// frontier stays at the fork point: lower than before, so not
			// progress.
			_, moved, err = f.bridge.InsertChainMoved(side)
			if err == nil {
				t.Fatalf("round %d: expected the side chain to be rejected", round)
			}
			common.Expect(t, moved, false)
			common.Expect(t, f.z.Chain().GetFrontierMomentumStore().Identifier(), f.previous)

			// The same branch served again is now an extension of the fork
			// point; it fails without moving anything.
			_, moved, err = f.bridge.InsertChainMoved(side)
			if err == nil {
				t.Fatalf("round %d: expected the retried side chain to be rejected", round)
			}
			common.Expect(t, moved, false)
			common.Expect(t, f.z.Chain().GetFrontierMomentumStore().Identifier(), f.previous)
		}
	})
}
