package protocol_test

import (
	"errors"
	"sync"
	"testing"

	"github.com/zenon-network/go-zenon/chain"
	g "github.com/zenon-network/go-zenon/chain/genesis/mock"
	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/chain/store"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/protocol"
	"github.com/zenon-network/go-zenon/verifier"
	"github.com/zenon-network/go-zenon/vm"
	"github.com/zenon-network/go-zenon/zenon/mock"
)

const servingPeer = "serving-peer"

// expectQuarantinedBy reports err through the import-failure hook the
// downloader calls, on a fresh protocol manager, and checks whether the
// serving peer ends up quarantined.
func expectQuarantinedBy(t *testing.T, bridge protocol.ChainBridge, err error, quarantined bool) {
	t.Helper()
	pm := protocol.NewProtocolManager(1, 1, bridge)
	defer pm.Stop()
	pm.ReportImportFailure(servingPeer, err)
	if pm.Quarantined(servingPeer) != quarantined {
		t.Fatalf("expected quarantined=%v for import failure %v", quarantined, err)
	}
}

// A batch that extends the frontier and fails a consensus check is classified
// like a rejected side-chain: the rejection is typed and keeps its cause, the
// serving peer is quarantined, and the same bytes are refused before any of
// their content is applied when they are offered again.
func TestInsertChain_ExtensionRejectionIsRememberedAndQuarantinesThePeer(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()
	z.InsertMomentumsTo(6)
	counting, bridge := newSideChainBridge(z)
	before := snapshotChain(t, z.Chain())

	frontier := momentumAt(t, z.Chain(), 6)
	head := producerMomentum(t, z, frontier, 7)
	batch := detailedOf(head, attackerMomentum(head, 8))

	_, err := bridge.InsertChain(batch)
	var rejected *protocol.SideChainRejected
	if !errors.As(err, &rejected) {
		t.Fatalf("expected an extension-path consensus rejection to be a SideChainRejected, got %v", err)
	}
	if !errors.Is(err, verifier.ErrMChangesHashInvalid) {
		t.Fatalf("the rejection must keep its cause, got %v", err)
	}
	if errors.Is(err, protocol.ErrSideChainKnownInvalid) {
		t.Fatal("the first offer must be judged on its merits, not refused as known")
	}
	common.Expect(t, rejected.Momentum, head.Identifier())
	common.Expect(t, counting.rollbacks, 0)
	expectChainEquals(t, z.Chain(), before)
	expectQuarantinedBy(t, bridge, err, true)

	_, err = bridge.InsertChain(batch)
	if !errors.Is(err, protocol.ErrSideChainKnownInvalid) {
		t.Fatalf("expected the same bytes to be refused as known invalid, got %v", err)
	}
	expectQuarantinedBy(t, bridge, err, true)
	common.Expect(t, counting.rollbacks, 0)
	expectChainEquals(t, z.Chain(), before)
}

// A candidate following a committed valid prefix of the same extension
// batch is judged against the state the prefix left, which is its own
// previous momentum, so its rejection is classified too and the prefix
// stays.
func TestInsertChain_ExtensionRejectionAfterValidPrefixKeepsThePrefix(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()
	z.InsertMomentumsTo(6)
	extension := buildFork(t, z, 6, 7, 1)
	_, bridge := newSideChainBridge(z)

	invalid := producerMomentum(t, z, extension[0].Momentum, 8)
	batch := append(extension, detailedOf(invalid)...)

	index, err := bridge.InsertChain(batch)
	var rejected *protocol.SideChainRejected
	if !errors.As(err, &rejected) {
		t.Fatalf("expected a SideChainRejected, got %v", err)
	}
	common.Expect(t, index, 1)
	common.Expect(t, rejected.Momentum, invalid.Identifier())
	expectFrontierAt(t, z.Chain(), extension[0].Momentum)
	expectQuarantinedBy(t, bridge, err, true)
}

// Failures that are not a verdict on the candidate's bytes are neither
// remembered nor attributed to the peer: a store error while committing
// a genuine extension, and an extension whose parent the node does not
// have yet. Both batches insert once the condition clears.
func TestInsertChain_ExtensionTransientFailureIsNotClassified(t *testing.T) {
	t.Run("store error on commit", func(t *testing.T) {
		z := mock.NewMockZenon(t)
		defer z.StopPanic()
		z.InsertMomentumsTo(6)
		extension := buildFork(t, z, 6, 7, 1)
		counting, bridge := newSideChainBridge(z)
		before := snapshotChain(t, z.Chain())

		counting.failCommitOf = extension[0].Momentum.Hash
		_, err := bridge.InsertChain(extension)
		if err == nil {
			t.Fatal("expected the injected commit failure")
		}
		var rejected *protocol.SideChainRejected
		if errors.As(err, &rejected) {
			t.Fatalf("a store error is not a consensus verdict: %v", err)
		}
		expectQuarantinedBy(t, bridge, err, false)
		expectChainEquals(t, z.Chain(), before)

		counting.failCommitOf = [32]byte{}
		_, err = bridge.InsertChain(extension)
		common.FailIfErr(t, err)
		expectFrontierAt(t, z.Chain(), extension[0].Momentum)
	})
	t.Run("unknown parent", func(t *testing.T) {
		z := mock.NewMockZenon(t)
		defer z.StopPanic()
		z.InsertMomentumsTo(6)
		extension := buildFork(t, z, 6, 8, 1)
		counting, bridge := newSideChainBridge(z)
		before := snapshotChain(t, z.Chain())

		_, err := bridge.InsertChain(extension[1:])
		if err == nil {
			t.Fatal("expected a batch with an unknown parent to be refused")
		}
		var rejected *protocol.SideChainRejected
		if errors.As(err, &rejected) {
			t.Fatalf("an unknown parent is not a consensus verdict: %v", err)
		}
		expectQuarantinedBy(t, bridge, err, false)
		common.Expect(t, counting.rollbacks, 0)
		expectChainEquals(t, z.Chain(), before)

		_, err = bridge.InsertChain(extension)
		common.FailIfErr(t, err)
		expectFrontierAt(t, z.Chain(), extension[1].Momentum)
	})
}

// A branch rejected on the extension path is refused before any rollback
// once the node has a momentum of its own at that height, so the branch
// now arrives as a side-chain, and also when the batch it arrives in is
// not longer than the local chain; in both cases the serving peer is held
// to account.
func TestInsertChain_ExtensionRejectedBranchIsRefusedOnReconnectWithoutRollback(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()
	z.InsertMomentumsTo(6)
	counting, bridge := newSideChainBridge(z)

	frontier := momentumAt(t, z.Chain(), 6)
	head := producerMomentum(t, z, frontier, 7)
	_, err := bridge.InsertChain(detailedOf(head))
	var rejected *protocol.SideChainRejected
	if !errors.As(err, &rejected) {
		t.Fatalf("expected a SideChainRejected, got %v", err)
	}

	// The node moves on with a momentum of its own at the rejected height.
	z.InsertMomentumsTo(7)
	if momentumAt(t, z.Chain(), 7).Hash == head.Hash {
		t.Fatal("the node's own momentum must differ from the rejected one")
	}
	before := snapshotChain(t, z.Chain())

	second := attackerMomentum(head, 8)
	longer := detailedOf(head, second, attackerMomentum(second, 9))
	_, err = bridge.InsertChain(longer)
	if !errors.Is(err, protocol.ErrSideChainKnownInvalid) {
		t.Fatalf("expected the longer side-chain to be refused as known invalid, got %v", err)
	}
	expectQuarantinedBy(t, bridge, err, true)
	common.Expect(t, counting.rollbacks, 0)
	expectChainEquals(t, z.Chain(), before)

	_, err = bridge.InsertChain(detailedOf(head))
	if !errors.Is(err, protocol.ErrSideChainKnownInvalid) {
		t.Fatalf("expected a not-longer batch carrying the rejected momentum to be refused as known invalid, got %v", err)
	}
	expectQuarantinedBy(t, bridge, err, true)
	common.Expect(t, counting.rollbacks, 0)
	expectChainEquals(t, z.Chain(), before)
}

type extensionGuardChain struct {
	chain.Chain
	getMomentumStoreCalls int
	forceAddCalls         int
	snapshotCalls         int
}

func (c *extensionGuardChain) GetMomentumStore(identifier types.HashHeight) store.Momentum {
	c.getMomentumStoreCalls++
	return c.Chain.GetMomentumStore(identifier)
}

func (c *extensionGuardChain) ForceAddAccountBlockTransaction(insertLocker sync.Locker, transaction *nom.AccountBlockTransaction) error {
	c.forceAddCalls++
	return c.Chain.ForceAddAccountBlockTransaction(insertLocker, transaction)
}

func (c *extensionGuardChain) SnapshotUncommitted(insertLocker sync.Locker, addresses []types.Address) *chain.UncommittedSnapshot {
	c.snapshotCalls++
	return c.Chain.SnapshotUncommitted(insertLocker, addresses)
}

// An extension momentum that is unsigned or signed by a key other than the
// elected producer must be rejected before snapshotting the pool or applying
// any of its prefetched account blocks through the supervisor.
func TestInsertChain_ExtensionRefusesUnsignedOrNonProducerBeforeApplyingBlocks(t *testing.T) {
	cases := []struct {
		name      string
		sign      bool
		wantCause error
	}{
		{
			name:      "unsigned",
			sign:      false,
			wantCause: verifier.ErrMSignatureMissing,
		},
		{
			name:      "non-elected producer",
			sign:      true,
			wantCause: verifier.ErrMProducerInvalid,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			z := mock.NewMockZenon(t)
			defer z.StopPanic()
			z.InsertMomentumsTo(6)

			oldMomentum := momentumAt(t, z.Chain(), 1)
			frontier := momentumAt(t, z.Chain(), 6)
			before := snapshotChain(t, z.Chain())

			crafted := &nom.AccountBlock{
				Version:              1,
				ChainIdentifier:      frontier.ChainIdentifier,
				BlockType:            nom.BlockTypeContractReceive,
				Address:              types.StakeContract,
				Height:               1,
				MomentumAcknowledged: oldMomentum.Identifier(),
				TokenStandard:        types.ZnnTokenStandard,
				FromBlockHash:        types.NewHash([]byte("garbage")),
			}
			crafted.Hash = crafted.ComputeHash()

			m := unsignedMomentum(frontier, frontier.Height+1)
			m.Content = nom.NewMomentumContent([]*nom.AccountBlock{crafted})
			if tc.sign {
				m = signedBy(m, g.User1)
			} else {
				m.Hash = m.ComputeHash()
			}

			guarded := &extensionGuardChain{Chain: z.Chain()}
			supervisor := vm.NewSupervisor(guarded, z.Consensus())
			bridge := protocol.NewChainBridge(guarded, z.Consensus(), z.Verifier(), supervisor)

			_, err := bridge.InsertChain([]*nom.DetailedMomentum{{
				Momentum:      m,
				AccountBlocks: []*nom.AccountBlock{crafted},
			}})
			var rejected *protocol.SideChainRejected
			if !errors.As(err, &rejected) {
				t.Fatalf("expected SideChainRejected, got %v", err)
			}
			if !errors.Is(err, tc.wantCause) {
				t.Fatalf("expected cause %v, got %v", tc.wantCause, err)
			}
			common.Expect(t, guarded.getMomentumStoreCalls, 0)
			common.Expect(t, guarded.forceAddCalls, 0)
			common.Expect(t, guarded.snapshotCalls, 0)
			expectChainEquals(t, z.Chain(), before)
		})
	}
}
