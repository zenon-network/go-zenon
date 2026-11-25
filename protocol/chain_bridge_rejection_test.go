package protocol_test

import (
	"errors"
	"testing"

	g "github.com/zenon-network/go-zenon/chain/genesis/mock"
	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/types"
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

// A tampered copy served under the genuine momentum hash is remembered by the
// bytes that were served, so the genuine branch is still judged on its own
// merits afterwards. The tampered fields are ones no hash covers.
func TestInsertChain_TamperedCopyDoesNotPoisonTheGenuineBranch(t *testing.T) {
	cases := []struct {
		name   string
		tamper func(batch []*nom.DetailedMomentum)
	}{
		{"momentum signature", func(batch []*nom.DetailedMomentum) {
			copied := *batch[0].Momentum
			copied.Signature = append([]byte(nil), copied.Signature...)
			copied.Signature[0] ^= 0xff
			batch[0] = &nom.DetailedMomentum{Momentum: &copied, AccountBlocks: batch[0].AccountBlocks}
		}},
		{"block signature", func(batch []*nom.DetailedMomentum) {
			blocks := make([]*nom.AccountBlock, len(batch[0].AccountBlocks))
			for i, block := range batch[0].AccountBlocks {
				blocks[i] = block.Copy()
			}
			blocks[0].Signature = append([]byte(nil), blocks[0].Signature...)
			blocks[0].Signature[0] ^= 0xff
			batch[0] = &nom.DetailedMomentum{Momentum: batch[0].Momentum, AccountBlocks: blocks}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			z := mock.NewMockZenon(t)
			defer z.StopPanic()
			z.InsertMomentumsTo(6)
			fork := buildFork(t, z, 5, 7, 1)
			if len(fork[0].AccountBlocks) == 0 {
				t.Fatal("fork head carries no account block")
			}
			counting, bridge := newSideChainBridge(z)

			tampered := append([]*nom.DetailedMomentum{}, fork...)
			tc.tamper(tampered)
			common.Expect(t, tampered[0].Momentum.Hash, fork[0].Momentum.Hash)

			_, err := bridge.InsertChain(tampered)
			var rejected *protocol.SideChainRejected
			if !errors.As(err, &rejected) {
				t.Fatalf("expected the tampered copy to be rejected, got %v", err)
			}
			_, err = bridge.InsertChain(tampered)
			if !errors.Is(err, protocol.ErrSideChainKnownInvalid) {
				t.Fatalf("expected the same bytes to be refused as known invalid, got %v", err)
			}
			rollbacks := counting.rollbacks

			_, err = bridge.InsertChain(fork)
			common.FailIfErr(t, err)
			common.Expect(t, counting.rollbacks, rollbacks+1)
			expectFrontierAt(t, z.Chain(), fork[len(fork)-1].Momentum)
		})
	}
}

// A batch whose later candidate does not extend the one before it is refused
// as malformed before any check that depends on state, so a genuine
// momentum offered behind a competing one is never judged under the wrong
// state, never remembered, and still inserts on its own afterwards. The
// refusal is an ordinary failure, not a SideChainRejected: the batch's
// composition failed, not the candidate or the peer that delivered it.
func TestInsertChain_MixedBatchIsRefusedBeforeRollbackAndDoesNotPoisonTheExtension(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()
	z.InsertMomentumsTo(6)
	// A6: a valid competing momentum at height 6, forking below our frontier.
	competing := buildFork(t, z, 5, 6, 2)
	// B7: a genuine extension of our own momentum 6, carrying a User1 send
	// whose account height collides with the one A6 carries.
	extension := buildFork(t, z, 6, 7, 1)
	counting, bridge := newSideChainBridge(z)
	before := snapshotChain(t, z.Chain())

	_, err := bridge.InsertChain([]*nom.DetailedMomentum{competing[0], extension[0]})
	if err == nil {
		t.Fatal("expected the mixed batch to be refused")
	}
	var rejected *protocol.SideChainRejected
	if errors.As(err, &rejected) || errors.Is(err, protocol.ErrSideChainKnownInvalid) {
		t.Fatalf("a mixed batch is malformed, not a verdict on a candidate: %v", err)
	}
	common.Expect(t, counting.rollbacks, 0)
	expectChainEquals(t, z.Chain(), before)

	_, err = bridge.InsertChain(extension)
	common.FailIfErr(t, err)
	expectFrontierAt(t, z.Chain(), extension[0].Momentum)
}

// When a valid prefix of the replacement commits and a later candidate is
// rejected, the prefix stays once it is longer than the branch it replaced,
// and the rejected candidate is remembered. The retry skips the prefix the
// node now has and offers the rejected candidate as an extension of it; the
// memory refuses it there, before its content is applied. (A prefix that is
// not longer is rolled back and the branch restored; see
// TestInsertChain_RejectedSideChainIsRefusedBeforeRollbackOnRetry.)
func TestInsertChain_RejectedCandidateAfterCommittedPrefixIsRefusedAsExtension(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()
	z.InsertMomentumsTo(6)
	fork := buildFork(t, z, 5, 7, 1)
	counting, bridge := newSideChainBridge(z)

	bad := attackerMomentum(fork[1].Momentum, 8)
	batch := append(append([]*nom.DetailedMomentum{}, fork...), detailedOf(bad)...)

	_, err := bridge.InsertChain(batch)
	var rejected *protocol.SideChainRejected
	if !errors.As(err, &rejected) {
		t.Fatalf("expected a SideChainRejected, got %v", err)
	}
	common.Expect(t, rejected.Momentum, bad.Identifier())
	common.Expect(t, counting.rollbacks, 1)
	expectFrontierAt(t, z.Chain(), fork[1].Momentum)
	withPrefix := snapshotChain(t, z.Chain())

	_, err = bridge.InsertChain(batch)
	if !errors.Is(err, protocol.ErrSideChainKnownInvalid) {
		t.Fatalf("expected the retry to be refused as known invalid, got %v", err)
	}
	if !errors.As(err, &rejected) {
		t.Fatalf("the refusal must stay attributable to the peer, got %v", err)
	}
	common.Expect(t, counting.rollbacks, 1)
	expectChainEquals(t, z.Chain(), withPrefix)
}

// A plain extension that fails a consensus check, with no rollback behind
// it, is classified like a failed replacement: the verdict is attributed to
// the serving peer and the served bytes are remembered, so the next offer is
// refused before its content is applied. This is the path a node at a fork
// point takes first: the other branch extends its frontier until its own
// next momentum turns that branch into a side chain. The chain is left as it
// was on both attempts.
func TestInsertChain_RejectedExtensionIsRememberedAndAttributed(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()
	z.InsertMomentumsTo(6)
	counting, bridge := newSideChainBridge(z)
	before := snapshotChain(t, z.Chain())

	bad := attackerMomentum(momentumAt(t, z.Chain(), 6), 7)
	batch := detailedOf(bad)

	_, err := bridge.InsertChain(batch)
	var rejected *protocol.SideChainRejected
	if !errors.As(err, &rejected) {
		t.Fatalf("expected a failed extension to be a SideChainRejected, got %v", err)
	}
	if !errors.Is(err, verifier.ErrMProducerInvalid) {
		t.Fatalf("the rejection must keep its cause, got %v", err)
	}
	common.Expect(t, rejected.Momentum, bad.Identifier())
	common.Expect(t, counting.rollbacks, 0)
	expectChainEquals(t, z.Chain(), before)

	_, err = bridge.InsertChain(batch)
	if !errors.Is(err, protocol.ErrSideChainKnownInvalid) {
		t.Fatalf("expected the retry to be refused as known invalid, got %v", err)
	}
	if !errors.As(err, &rejected) {
		t.Fatalf("the refusal must stay attributable to the peer, got %v", err)
	}
	common.Expect(t, counting.rollbacks, 0)
	expectChainEquals(t, z.Chain(), before)

	// A descendant batch carrying the remembered candidate is refused the
	// same way, before anything is applied.
	_, err = bridge.InsertChain(detailedOf(bad, attackerMomentum(bad, 8)))
	if !errors.Is(err, protocol.ErrSideChainKnownInvalid) {
		t.Fatalf("expected a descendant batch to be refused as known invalid, got %v", err)
	}
	common.Expect(t, counting.rollbacks, 0)
	expectChainEquals(t, z.Chain(), before)
}

// What the pool holds must not decide a remembered verdict. The momentum's
// blocks are applied on top of the pool, and a block the pool already holds
// with the same bytes is not applied again, so this pins that the pool can
// neither turn a tampered copy into something other than its own verdict nor
// get the genuine bytes remembered. The same tampered momentum is offered
// against three pool states on the extension path, where nothing is rolled
// back first: in each it is rejected with the same cause, remembered, and
// refused on retry, the pool is left as it was, and the genuine momentum
// then inserts on its first offer.
func TestInsertChain_PoolStateDoesNotColourARememberedVerdict(t *testing.T) {
	cases := []struct {
		name string
		seed func(t *testing.T, f *restoreFixture) []*nom.AccountBlock
	}{
		{"empty pool", func(t *testing.T, f *restoreFixture) []*nom.AccountBlock {
			return nil
		}},
		{"genuine copy and descendant", func(t *testing.T, f *restoreFixture) []*nom.AccountBlock {
			common.FailIfErr(t, f.bridge.AddAccountBlocks([]*nom.AccountBlock{f.receive.Block}))
			descendant := f.generateReceive(t, f.sends[1])
			f.addToPool(t, descendant)
			return []*nom.AccountBlock{f.receive.Block, descendant.Block}
		}},
		{"competing chain and another account", func(t *testing.T, f *restoreFixture) []*nom.AccountBlock {
			first := f.generateReceive(t, f.sends[1])
			f.addToPool(t, first)
			second := f.generateReceive(t, f.sends[0])
			f.addToPool(t, second)
			if first.Block.Hash == f.receive.Block.Hash {
				t.Fatal("competing block unexpectedly has the same hash")
			}
			f.addToPool(t, f.generateSend(t))
			return []*nom.AccountBlock{first.Block, second.Block}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newRestoreFixture(t)
			defer f.z.StopPanic()
			pool := tc.seed(t, f)
			// The pool frontier of the account the momentum's block belongs
			// to, which is also what an empty pool is compared by.
			poolFrontier := func() types.HashHeight {
				return f.z.Chain().GetFrontierAccountStore(g.User2.Address).Identifier()
			}
			poolBefore := poolFrontier()
			expectPoolUnchanged := func() {
				t.Helper()
				common.Expect(t, poolFrontier(), poolBefore)
				if pool != nil {
					f.expectPoolChain(t, pool...)
				}
			}
			tampered := f.tamperedMomentum()

			_, err := f.bridge.InsertChain([]*nom.DetailedMomentum{tampered})
			var rejected *protocol.SideChainRejected
			if !errors.As(err, &rejected) {
				t.Fatalf("expected the tampered copy to be rejected whatever the pool holds, got %v", err)
			}
			expectChangesHashError(t, err)
			common.Expect(t, rejected.Momentum, f.detailed.Momentum.Identifier())
			common.Expect(t, f.z.Chain().GetFrontierMomentumStore().Identifier(), f.previous)
			expectPoolUnchanged()

			_, err = f.bridge.InsertChain([]*nom.DetailedMomentum{tampered})
			if !errors.Is(err, protocol.ErrSideChainKnownInvalid) {
				t.Fatalf("expected the same bytes to be refused as known invalid, got %v", err)
			}
			expectPoolUnchanged()

			// The genuine bytes were never remembered, so they insert on
			// their first offer.
			_, err = f.bridge.InsertChain([]*nom.DetailedMomentum{f.detailed})
			common.FailIfErr(t, err)
			common.Expect(t, f.z.Chain().GetFrontierMomentumStore().Identifier(), f.detailed.Momentum.Identifier())
		})
	}
}
