package protocol_test

import (
	"errors"
	"testing"

	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/protocol"
	"github.com/zenon-network/go-zenon/zenon/mock"
)

// InsertChainMoved reports, under the insert lock, whether the call left the
// frontier somewhere else: not for a batch the node already had, not for an
// extension that was rejected, not for a replacement that failed and was
// restored, yes for a valid extension, yes for a valid replacement, and yes
// for a valid prefix that stayed on the chain before a later candidate
// failed. A rejected branch is remembered, so serving it again is refused
// before any rollback and reports false as well.
func TestInsertChainMoved_ReportsOnlyRetainedProgress(t *testing.T) {
	t.Run("valid extension", func(t *testing.T) {
		f := newRestoreFixture(t)
		defer f.z.StopPanic()
		_, moved, err := f.bridge.InsertChainMoved([]*nom.DetailedMomentum{f.detailed})
		common.FailIfErr(t, err)
		common.Expect(t, moved, true)
		common.Expect(t, f.z.Chain().GetFrontierMomentumStore().Identifier(), f.detailed.Momentum.Identifier())
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
	t.Run("batch already known", func(t *testing.T) {
		z := mock.NewMockZenon(t)
		defer z.StopPanic()
		z.InsertMomentumsTo(6)
		_, bridge := newSideChainBridge(z)
		_, moved, err := bridge.InsertChainMoved(prefetchAbove(t, z, 4, 6))
		common.FailIfErr(t, err)
		common.Expect(t, moved, false)
	})
	t.Run("rejected replacement restored", func(t *testing.T) {
		z := mock.NewMockZenon(t)
		defer z.StopPanic()
		z.InsertMomentumsTo(6)
		_, bridge := newSideChainBridge(z)
		before := snapshotChain(t, z.Chain())
		target := momentumAt(t, z.Chain(), 5)
		head := producerMomentum(t, z, target, 6)
		_, moved, err := bridge.InsertChainMoved(detailedOf(head, attackerMomentum(head, 7)))
		if err == nil {
			t.Fatal("expected the side chain to be rejected")
		}
		common.Expect(t, moved, false)
		expectChainEquals(t, z.Chain(), before)
	})
	t.Run("rejected replacement served again, then the chain advances", func(t *testing.T) {
		z := mock.NewMockZenon(t)
		defer z.StopPanic()
		z.InsertMomentumsTo(6)
		extension := buildFork(t, z, 6, 7, 1)
		counting, bridge := newSideChainBridge(z)
		before := snapshotChain(t, z.Chain())
		target := momentumAt(t, z.Chain(), 5)
		head := producerMomentum(t, z, target, 6)
		side := detailedOf(head, attackerMomentum(head, 7))

		// The branch is rolled back to, rejected and the original branch
		// restored: the frontier is where it was, so not progress.
		_, moved, err := bridge.InsertChainMoved(side)
		var rejected *protocol.SideChainRejected
		if !errors.As(err, &rejected) {
			t.Fatalf("expected the side chain to be rejected, got %v", err)
		}
		common.Expect(t, moved, false)
		expectRolledBack(t, counting)
		rollbacks := counting.rollbacks
		expectChainEquals(t, z.Chain(), before)

		// Served again, the branch is refused before any rollback, so the
		// node keeps its own momentum and the call reports false. Without
		// the memory this rolled the node back once per session.
		for round := 0; round < 3; round++ {
			_, moved, err = bridge.InsertChainMoved(side)
			if !errors.Is(err, protocol.ErrSideChainKnownInvalid) {
				t.Fatalf("round %d: expected the side chain to be refused as known invalid, got %v", round, err)
			}
			common.Expect(t, moved, false)
			common.Expect(t, counting.rollbacks, rollbacks)
			expectChainEquals(t, z.Chain(), before)
		}

		// The chain advancing on a valid momentum is progress.
		_, moved, err = bridge.InsertChainMoved(extension)
		common.FailIfErr(t, err)
		common.Expect(t, moved, true)
		expectFrontierAt(t, z.Chain(), extension[0].Momentum)
	})
	t.Run("valid replacement", func(t *testing.T) {
		f := newForkFixture(t, 6, 7)
		defer f.z.StopPanic()
		_, bridge := newSideChainBridge(f.z)
		_, moved, err := bridge.InsertChainMoved(f.fork)
		common.FailIfErr(t, err)
		common.Expect(t, moved, true)
		expectFrontierAt(t, f.z.Chain(), f.fork[1].Momentum)
	})
	t.Run("kept prefix before a failure", func(t *testing.T) {
		f := newForkFixture(t, 7, 9)
		defer f.z.StopPanic()
		_, bridge := newSideChainBridge(f.z)
		candidates := append([]*nom.DetailedMomentum{}, f.fork[:3]...)
		candidates = append(candidates, invalidTail(f.fork[2].Momentum, 10)...)
		_, moved, err := bridge.InsertChainMoved(candidates)
		if err == nil {
			t.Fatal("expected the tail to be rejected")
		}
		common.Expect(t, moved, true)
		expectFrontierAt(t, f.z.Chain(), f.fork[2].Momentum)
	})
}
