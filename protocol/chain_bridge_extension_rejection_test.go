package protocol_test

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/zenon-network/go-zenon/chain"
	g "github.com/zenon-network/go-zenon/chain/genesis/mock"
	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/chain/store"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/db"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/consensus"
	"github.com/zenon-network/go-zenon/protocol"
	"github.com/zenon-network/go-zenon/verifier"
	"github.com/zenon-network/go-zenon/vm"
	"github.com/zenon-network/go-zenon/vm/constants"
	"github.com/zenon-network/go-zenon/vm/embedded/definition"
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
	getMomentumStoreByID  map[types.HashHeight]int
	forceAddCalls         int
	snapshotCalls         int
}

func (c *extensionGuardChain) GetMomentumStore(identifier types.HashHeight) store.Momentum {
	c.getMomentumStoreCalls++
	if c.getMomentumStoreByID == nil {
		c.getMomentumStoreByID = make(map[types.HashHeight]int)
	}
	c.getMomentumStoreByID[identifier]++
	return c.Chain.GetMomentumStore(identifier)
}

func (c *extensionGuardChain) resetCounts() {
	c.getMomentumStoreCalls = 0
	c.getMomentumStoreByID = nil
	c.forceAddCalls = 0
	c.snapshotCalls = 0
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

// An in-process caller may pass a valid extension momentum whose cached
// Timestamp pointer is nil while TimestampUnix is populated. insertMomentums
// must forward its initialized candidate copy to ApplyMomentum without panicking
// and without mutating the caller's Momentum struct.
func TestInsertChain_ExtensionWithNilCachedTimestampImportsCleanly(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()
	z.InsertMomentumsTo(6)

	extension := buildFork(t, z, 6, 7, 1)
	extension[0].Momentum.Timestamp = nil
	_, bridge := newSideChainBridge(z)

	_, err := bridge.InsertChain(extension)
	common.FailIfErr(t, err)
	expectFrontierAt(t, z.Chain(), extension[0].Momentum)
	if extension[0].Momentum.Timestamp != nil {
		t.Fatal("InsertChain mutated caller's Momentum.Timestamp")
	}
}

// When an embedded contract queue has a genuinely old stalled head at or beyond
// the peer acknowledgement gap, standalone peer gossip via AddAccountBlocks
// must refuse receives for that head without triggering historical rollback
// replay, while local GenerateAutoReceive and signed-momentum InsertChain
// remain able to execute and import the receive (each opening the historical
// momentum store only once, sharing it with the pillar reader).
func TestAddAccountBlocks_RefusesStaleEmbeddedHeadWhilePreservingLocalAndMomentumImport(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()

	held := types.PillarContract
	holder := z.(interface{ HoldInsertsFor(*types.Address) })
	holder.HoldInsertsFor(&held)
	defer holder.HoldInsertsFor(nil)

	send := z.InsertSendBlock(&nom.AccountBlock{
		Address:       g.Pillar4.Address,
		ToAddress:     types.PillarContract,
		Data:          definition.ABIPillars.PackMethodPanic(definition.DepositQsrMethodName),
		TokenStandard: types.QsrTokenStandard,
		Amount:        constants.PillarQsrStakeBaseAmount,
	}, nil, mock.SkipVmChanges)

	z.InsertNewMomentum()
	confirmHeight, err := z.Chain().GetFrontierMomentumStore().GetBlockConfirmationHeight(send.Hash)
	common.FailIfErr(t, err)
	if confirmHeight == 0 {
		t.Fatal("expected send block to be confirmed in momentum")
	}
	confirmMomentum := momentumAt(t, z.Chain(), confirmHeight)

	// Advance frontier by 360 momentums while holding PillarContract's receive
	// so send remains the genuine stalled queue head beyond the near-cache window.
	z.InsertMomentumsTo(confirmHeight + 360)

	pillarStore := z.Chain().GetFrontierAccountStore(types.PillarContract)
	head, err := pillarStore.SequencerFront(z.Chain().GetFrontierMomentumStore().GetAccountMailbox(types.PillarContract))
	common.FailIfErr(t, err)
	if head == nil || head.Hash != send.Hash {
		t.Fatalf("expected PillarContract queue head %v, got %v", send.Hash, head)
	}

	guarded := &extensionGuardChain{Chain: z.Chain()}
	cs := consensus.NewConsensus(db.NewMemDB(), guarded, true)
	common.FailIfErr(t, cs.Init())
	common.FailIfErr(t, cs.Start())
	defer func() { common.FailIfErr(t, cs.Stop()) }()

	supervisor := vm.NewSupervisor(guarded, cs)
	bridge := protocol.NewChainBridge(guarded, cs, verifier.NewVerifier(guarded, cs, vm.CanonicalBasePlasma), supervisor)
	guarded.resetCounts()

	frontier := momentumAt(t, z.Chain(), confirmHeight+360)
	prev := pillarStore.Identifier()
	crafted := &nom.AccountBlock{
		Version:              1,
		ChainIdentifier:      frontier.ChainIdentifier,
		BlockType:            nom.BlockTypeContractReceive,
		Address:              types.PillarContract,
		Height:               prev.Height + 1,
		PreviousHash:         prev.Hash,
		MomentumAcknowledged: confirmMomentum.Identifier(),
		FromBlockHash:        send.Hash,
		ChangesHash:          types.NewHash([]byte("tampered")),
	}
	crafted.Hash = crafted.ComputeHash()

	for i := 0; i < 3; i++ {
		if err := bridge.AddAccountBlocks([]*nom.AccountBlock{crafted.Copy()}); err == nil {
			t.Fatalf("attempt %d: expected stale peer embedded receive to be refused", i)
		}
	}
	common.Expect(t, guarded.getMomentumStoreCalls, 0)

	// Local generation must still succeed at any gap and open the historical
	// momentum store only once (reusing it for FixedPillarReaderForStore).
	exec, err := supervisor.GenerateAutoReceive(send)
	common.FailIfErr(t, err)
	if exec.ReturnedError != nil {
		t.Fatalf("unexpected contract execution error: %v", exec.ReturnedError)
	}
	common.Expect(t, guarded.getMomentumStoreByID[confirmMomentum.Identifier()], 1)
	common.Expect(t, guarded.getMomentumStoreCalls, 1)

	// Even the valid receive block is refused on standalone peer gossip at gap >= 360.
	guarded.resetCounts()
	if err := bridge.AddAccountBlocks([]*nom.AccountBlock{exec.Transaction.Block.Copy()}); err == nil {
		t.Fatal("expected valid stale embedded receive to be refused on peer gossip")
	}
	common.Expect(t, guarded.getMomentumStoreCalls, 0)

	// Build a genuine producer-signed extension momentum containing the receive
	// without leaving the receive in the local pool, then import it via InsertChain.
	insert := z.Chain().AcquireInsert("build signed extension momentum for stalled head")
	snap := z.Chain().SnapshotUncommitted(insert, []types.Address{types.PillarContract})
	common.FailIfErr(t, z.Chain().ForceAddAccountBlockTransaction(insert, exec.Transaction))
	nextTs := time.Unix(int64(frontier.TimestampUnix+10), 0)
	template := &nom.Momentum{
		Version:         frontier.Version,
		ChainIdentifier: frontier.ChainIdentifier,
		PreviousHash:    frontier.Hash,
		Height:          frontier.Height + 1,
		TimestampUnix:   uint64(nextTs.Unix()),
		Timestamp:       &nextTs,
		Content:         nom.NewMomentumContent([]*nom.AccountBlock{exec.Transaction.Block}),
	}
	genSupervisor := vm.NewSupervisor(z.Chain(), z.Consensus())
	momTx, err := genSupervisor.GenerateMomentum(&nom.DetailedMomentum{
		Momentum:      template,
		AccountBlocks: []*nom.AccountBlock{exec.Transaction.Block.Copy()},
	}, electedKey(t, z, nextTs).Signer)
	common.FailIfErr(t, err)
	common.FailIfErr(t, z.Chain().RestoreUncommitted(insert, snap))
	insert.Unlock()

	guarded.resetCounts()
	_, err = bridge.InsertChain([]*nom.DetailedMomentum{{
		Momentum:      momTx.Momentum,
		AccountBlocks: []*nom.AccountBlock{exec.Transaction.Block.Copy()},
	}})
	common.FailIfErr(t, err)
	expectFrontierAt(t, z.Chain(), momTx.Momentum)
	common.Expect(t, guarded.getMomentumStoreByID[confirmMomentum.Identifier()], 1)

	recv, err := z.Chain().GetFrontierMomentumStore().GetBlockWhichReceives(send.Hash)
	common.FailIfErr(t, err)
	if recv == nil || recv.Hash != exec.Transaction.Block.Hash {
		t.Fatalf("expected send %v to be received by %v, got %v", send.Hash, exec.Transaction.Block.Hash, recv)
	}
}
