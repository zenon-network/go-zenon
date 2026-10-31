package protocol_test

import (
	"testing"

	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/zenon/mock"
)

// InsertChainMoved reports, under the insert lock, whether the call left the
// frontier somewhere else: not for a batch the node already had, not for a
// replacement that failed and was restored, yes for a valid replacement,
// and yes for a valid prefix that stayed on the chain before a later
// candidate failed.
func TestInsertChainMoved_ReportsOnlyRetainedProgress(t *testing.T) {
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
