package protocol_test

import (
	"testing"

	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common"
)

// InsertChainMoved reports, under the insert lock, whether the call left the
// frontier somewhere else: not for a batch the node already had, not for an
// extension that was rejected, yes for a valid extension. On this release
// line a side-chain replacement is rolled back to the fork point before it
// is applied and is not restored when it fails (public dev restores it,
// #83), so a rejected replacement also moves the frontier, down to the fork
// point, and reports true once; the retry from the fork point is an
// extension and reports false. The fixture here cannot build a competing
// branch, so that case is documented rather than exercised.
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
}
