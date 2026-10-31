package protocol_test

import (
	"errors"
	"testing"

	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/protocol"
	"github.com/zenon-network/go-zenon/verifier"
	"github.com/zenon-network/go-zenon/zenon/mock"
)

// A side chain rejected on a consensus check after the rollback is refused
// before any rollback when it is offered again, by the same or another
// peer, and so is any batch that descends from the rejected momentum.
func TestInsertChain_RejectedSideChainIsRefusedBeforeRollbackOnRetry(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()
	z.InsertMomentumsTo(6)
	counting, bridge := newSideChainBridge(z)
	before := snapshotChain(t, z.Chain())

	target := momentumAt(t, z.Chain(), 5)
	head := producerMomentum(t, z, target, 6)
	tail := attackerMomentum(head, 7)
	batch := detailedOf(head, tail)

	_, err := bridge.InsertChain(batch)
	var rejected *protocol.SideChainRejected
	if !errors.As(err, &rejected) {
		t.Fatalf("expected a SideChainRejected, got %v", err)
	}
	if !errors.Is(err, verifier.ErrMChangesHashInvalid) {
		t.Fatalf("the rejection must keep its cause, got %v", err)
	}
	common.Expect(t, rejected.Momentum, head.Identifier())
	expectRolledBack(t, counting)
	rollbacks := counting.rollbacks
	expectChainEquals(t, z.Chain(), before)

	_, err = bridge.InsertChain(batch)
	if !errors.Is(err, protocol.ErrSideChainKnownInvalid) {
		t.Fatalf("expected the retry to be refused as known invalid, got %v", err)
	}
	common.Expect(t, counting.rollbacks, rollbacks)
	expectChainEquals(t, z.Chain(), before)

	_, err = bridge.InsertChain(detailedOf(head, tail, attackerMomentum(tail, 8)))
	if !errors.Is(err, protocol.ErrSideChainKnownInvalid) {
		t.Fatalf("expected a descendant batch to be refused as known invalid, got %v", err)
	}
	common.Expect(t, counting.rollbacks, rollbacks)
	expectChainEquals(t, z.Chain(), before)
}

// Remembering a rejected branch must not refuse a different, valid branch
// from the same fork point.
func TestInsertChain_RejectionMemoryDoesNotRefuseAnotherFork(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()
	z.InsertMomentumsTo(6)
	fork := buildFork(t, z, 5, 7, 1)
	counting, bridge := newSideChainBridge(z)

	target := momentumAt(t, z.Chain(), 5)
	head := producerMomentum(t, z, target, 6)
	_, err := bridge.InsertChain(detailedOf(head, attackerMomentum(head, 7)))
	var rejected *protocol.SideChainRejected
	if !errors.As(err, &rejected) {
		t.Fatalf("expected a SideChainRejected, got %v", err)
	}
	rollbacks := counting.rollbacks

	_, err = bridge.InsertChain(fork)
	common.FailIfErr(t, err)
	common.Expect(t, counting.rollbacks, rollbacks+1)
	expectFrontierAt(t, z.Chain(), fork[1].Momentum)
}

// A head refused before the rollback, here one not signed by the elected
// producer, is remembered as well, so the next offer skips even the
// pre-rollback checks.
func TestInsertChain_PreRollbackRejectionIsRememberedToo(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()
	z.InsertMomentumsTo(6)
	counting, bridge := newSideChainBridge(z)
	before := snapshotChain(t, z.Chain())

	target := momentumAt(t, z.Chain(), 5)
	head := attackerMomentum(target, 6)
	batch := detailedOf(head, attackerMomentum(head, 7))

	_, err := bridge.InsertChain(batch)
	var rejected *protocol.SideChainRejected
	if !errors.As(err, &rejected) {
		t.Fatalf("expected a SideChainRejected, got %v", err)
	}
	if errors.Is(err, protocol.ErrSideChainKnownInvalid) {
		t.Fatal("the first offer must be judged on its merits, not refused as known")
	}
	common.Expect(t, counting.rollbacks, 0)

	_, err = bridge.InsertChain(batch)
	if !errors.Is(err, protocol.ErrSideChainKnownInvalid) {
		t.Fatalf("expected the retry to be refused as known invalid, got %v", err)
	}
	common.Expect(t, counting.rollbacks, 0)
	expectChainEquals(t, z.Chain(), before)
}
