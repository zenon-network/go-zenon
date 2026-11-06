package protocol_test

import (
	"errors"
	"testing"

	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/protocol"
)

// InsertChainMoved reports, under the insert lock, whether the call left the
// frontier higher than it found it: not for a batch the node already had,
// not for an extension that was rejected, yes for a valid extension. On this
// release line a side-chain replacement is rolled back to the fork point
// before it is applied and is not restored when it fails (public dev
// restores it, #83), so a rejected replacement leaves the frontier below
// where it started: that reports false, the retry from the fork point is an
// extension and reports false, and only re-advancing the chain reports true
// again. The bridge remembers the rejected branch, so when the same branch
// is served again after the node re-advanced it is refused before any
// rollback: the node keeps its own momentum and the call reports false.
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
	t.Run("rejected replacement, retried, chain re-advanced, served again", func(t *testing.T) {
		f := newRestoreFixture(t)
		defer f.z.StopPanic()
		side := f.rejectedSideChain()

		// The node is on its own momentum above the fork point.
		_, moved, err := f.bridge.InsertChainMoved([]*nom.DetailedMomentum{f.detailed})
		common.FailIfErr(t, err)
		common.Expect(t, moved, true)
		common.Expect(t, f.z.Chain().GetFrontierMomentumStore().Identifier(), f.detailed.Momentum.Identifier())

		// A longer side chain forks below it; the rollback to the fork
		// point happens, the first foreign momentum is rejected, and the
		// frontier stays at the fork point: lower than before, so not
		// progress.
		_, moved, err = f.bridge.InsertChainMoved(side)
		if err == nil {
			t.Fatal("expected the side chain to be rejected")
		}
		common.Expect(t, moved, false)
		common.Expect(t, f.z.Chain().GetFrontierMomentumStore().Identifier(), f.previous)

		// The same branch served again is now an extension of the fork
		// point; the memory refuses it without moving anything.
		_, moved, err = f.bridge.InsertChainMoved(side)
		if !errors.Is(err, protocol.ErrSideChainKnownInvalid) {
			t.Fatalf("expected the retried side chain to be refused as known invalid, got %v", err)
		}
		common.Expect(t, moved, false)
		common.Expect(t, f.z.Chain().GetFrontierMomentumStore().Identifier(), f.previous)

		// The chain re-advances, which is progress.
		_, moved, err = f.bridge.InsertChainMoved([]*nom.DetailedMomentum{f.detailed})
		common.FailIfErr(t, err)
		common.Expect(t, moved, true)

		// Served again over the re-advanced chain, the branch is refused
		// before any rollback, so the node keeps its own momentum. Without
		// the memory this rolled the node back to the fork point once per
		// session.
		for round := 0; round < 3; round++ {
			_, moved, err = f.bridge.InsertChainMoved(side)
			if !errors.Is(err, protocol.ErrSideChainKnownInvalid) {
				t.Fatalf("round %d: expected the side chain to be refused as known invalid, got %v", round, err)
			}
			common.Expect(t, moved, false)
			common.Expect(t, f.z.Chain().GetFrontierMomentumStore().Identifier(), f.detailed.Momentum.Identifier())
		}
	})
}
