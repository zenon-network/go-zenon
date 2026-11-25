package protocol_test

import (
	"errors"
	"math/big"
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
	"github.com/zenon-network/go-zenon/protocol/downloader"
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
	panicAccountStoreAt   *types.HashHeight
}

func (c *extensionGuardChain) GetMomentumStore(identifier types.HashHeight) store.Momentum {
	c.getMomentumStoreCalls++
	if c.getMomentumStoreByID == nil {
		c.getMomentumStoreByID = make(map[types.HashHeight]int)
	}
	c.getMomentumStoreByID[identifier]++
	return c.Chain.GetMomentumStore(identifier)
}

func (c *extensionGuardChain) GetAccountStore(address types.Address, identifier types.HashHeight) store.Account {
	if c.panicAccountStoreAt != nil && identifier == *c.panicAccountStoreAt {
		panic("injected storage read failure")
	}
	return c.Chain.GetAccountStore(address, identifier)
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

	// Even the valid receive block is refused on standalone peer gossip at gap >= 360,
	// and refusing it must NOT drop later valid user blocks in the same peer batch.
	guarded.resetCounts()
	if err := bridge.AddAccountBlocks([]*nom.AccountBlock{exec.Transaction.Block.Copy()}); err == nil {
		t.Fatal("expected valid stale embedded receive to be refused on peer gossip")
	}
	common.Expect(t, guarded.getMomentumStoreCalls, 0)

	genSupervisor := vm.NewSupervisor(z.Chain(), z.Consensus())
	userTx, err := genSupervisor.GenerateFromTemplate(&nom.AccountBlock{
		BlockType:     nom.BlockTypeUserSend,
		Address:       g.User1.Address,
		ToAddress:     g.User2.Address,
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(1),
	}, g.User1.Signer)
	common.FailIfErr(t, err)

	insert := z.Chain().AcquireInsert("snapshot user pool before mixed peer batch")
	userSnap := z.Chain().SnapshotUncommitted(insert, []types.Address{g.User1.Address})
	insert.Unlock()

	guarded.resetCounts()
	if err := bridge.AddAccountBlocks([]*nom.AccountBlock{exec.Transaction.Block.Copy(), userTx.Block.Copy()}); err == nil {
		t.Fatal("expected mixed batch containing a stale embedded receive to report the policy error")
	}
	common.Expect(t, guarded.getMomentumStoreCalls, 0)
	if z.Chain().GetPatch(userTx.Block.Address, userTx.Block.Identifier()) == nil {
		t.Fatal("stale embedded receive refusal dropped a subsequent valid user transaction from the same batch")
	}

	insert = z.Chain().AcquireInsert("restore user pool after mixed peer batch")
	common.FailIfErr(t, z.Chain().RestoreUncommitted(insert, userSnap))
	insert.Unlock()

	// Build a genuine producer-signed extension momentum containing the receive
	// without leaving the receive in the local pool, then test tampered
	// deliveries followed by the genuine delivery via InsertChain.
	insert = z.Chain().AcquireInsert("build signed extension momentum for stalled head")
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
	momTx, err := genSupervisor.GenerateMomentum(&nom.DetailedMomentum{
		Momentum:      template,
		AccountBlocks: []*nom.AccountBlock{exec.Transaction.Block.Copy()},
	}, electedKey(t, z, nextTs).Signer)
	common.FailIfErr(t, err)
	common.FailIfErr(t, z.Chain().RestoreUncommitted(insert, snap))
	insert.Unlock()

	// 1. A tampered body under the signed momentum's content header (declared
	// Hash matches header, ComputeHash does not) must be rejected before any
	// historical store reconstruction.
	tamperedBody := exec.Transaction.Block.Copy()
	tamperedBody.Data = []byte("tampered-body")
	guarded.resetCounts()
	_, err = bridge.InsertChain([]*nom.DetailedMomentum{{
		Momentum:      momTx.Momentum,
		AccountBlocks: []*nom.AccountBlock{tamperedBody},
	}})
	var rejected *protocol.SideChainRejected
	if !errors.As(err, &rejected) || !errors.Is(err, verifier.ErrABHashInvalid) {
		t.Fatalf("expected SideChainRejected(ErrABHashInvalid) for tampered body, got %v", err)
	}
	common.Expect(t, guarded.getMomentumStoreByID[confirmMomentum.Identifier()], 0)

	// 2. Tampering an unhashed credential field on the embedded receive under
	// the signed header must also be rejected before historical reconstruction.
	tamperedSig := exec.Transaction.Block.Copy()
	tamperedSig.Signature = []byte("invalid-embedded-signature")
	guarded.resetCounts()
	_, err = bridge.InsertChain([]*nom.DetailedMomentum{{
		Momentum:      momTx.Momentum,
		AccountBlocks: []*nom.AccountBlock{tamperedSig},
	}})
	if !errors.As(err, &rejected) || !errors.Is(err, verifier.ErrABSignatureMustBeZero) {
		t.Fatalf("expected SideChainRejected(ErrABSignatureMustBeZero), got %v", err)
	}
	common.Expect(t, guarded.getMomentumStoreByID[confirmMomentum.Identifier()], 0)

	// 3. Repeated distinct deliveries with tampered ChangesHash (distinct bytes
	// that miss rejectedCandidates) must reconstruct the historical store at
	// most once for this canonical predecessor and receive.
	for i := 0; i < 4; i++ {
		tamperedCH := exec.Transaction.Block.Copy()
		tamperedCH.ChangesHash = types.NewHash([]byte{byte('a' + i), 'b', 'a', 'd'})
		guarded.resetCounts()
		_, err = bridge.InsertChain([]*nom.DetailedMomentum{{
			Momentum:      momTx.Momentum,
			AccountBlocks: []*nom.AccountBlock{tamperedCH},
		}})
		if !errors.As(err, &rejected) || !errors.Is(err, constants.ErrAutoReceiveChangesHashMismatch) {
			t.Fatalf("attempt %d: expected SideChainRejected(ErrAutoReceiveChangesHashMismatch), got %v", i, err)
		}
		wantStoreCalls := 0
		if i == 0 {
			wantStoreCalls = 1
		}
		common.Expect(t, guarded.getMomentumStoreByID[confirmMomentum.Identifier()], wantStoreCalls)
	}

	// 4. After all those rejected tampered deliveries, the genuine delivery of
	// the same signed momentum must still import cleanly.
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

// buildSignedExtensionMomentum builds a genuine producer-signed extension
// momentum containing the supplied transactions without leaving them in z's
// uncommitted account pool.
func buildSignedExtensionMomentum(t *testing.T, z mock.MockZenon, txs ...*nom.AccountBlockTransaction) *nom.DetailedMomentum {
	t.Helper()
	frontier, err := z.Chain().GetFrontierMomentumStore().GetFrontierMomentum()
	common.FailIfErr(t, err)

	addresses := make([]types.Address, 0, len(txs))
	blocks := make([]*nom.AccountBlock, 0, len(txs))
	for _, tx := range txs {
		addresses = append(addresses, tx.Block.Address)
		blocks = append(blocks, tx.Block)
	}

	insert := z.Chain().AcquireInsert("build signed extension momentum")
	snap := z.Chain().SnapshotUncommitted(insert, addresses)
	for _, tx := range txs {
		common.FailIfErr(t, z.Chain().ForceAddAccountBlockTransaction(insert, &nom.AccountBlockTransaction{
			Block:   tx.Block.Copy(),
			Changes: tx.Changes,
		}))
	}
	nextTs := time.Unix(int64(frontier.TimestampUnix+10), 0)
	content := nom.NewMomentumContent(blocks)
	byID := make(map[types.HashHeight]*nom.AccountBlock, len(blocks))
	for _, b := range blocks {
		byID[b.Identifier()] = b.Copy()
	}
	ordered := make([]*nom.AccountBlock, len(content))
	for i, h := range content {
		ordered[i] = byID[h.Identifier()]
	}
	template := &nom.Momentum{
		Version:         frontier.Version,
		ChainIdentifier: frontier.ChainIdentifier,
		PreviousHash:    frontier.Hash,
		Height:          frontier.Height + 1,
		TimestampUnix:   uint64(nextTs.Unix()),
		Timestamp:       &nextTs,
		Content:         content,
	}
	genSupervisor := vm.NewSupervisor(z.Chain(), z.Consensus())
	momTx, err := genSupervisor.GenerateMomentum(&nom.DetailedMomentum{
		Momentum:      template,
		AccountBlocks: ordered,
	}, electedKey(t, z, nextTs).Signer)
	common.FailIfErr(t, err)
	common.FailIfErr(t, z.Chain().RestoreUncommitted(insert, snap))
	insert.Unlock()

	return &nom.DetailedMomentum{
		Momentum:      momTx.Momentum,
		AccountBlocks: ordered,
	}
}

func cloneDetailed(d *nom.DetailedMomentum) *nom.DetailedMomentum {
	blocks := make([]*nom.AccountBlock, len(d.AccountBlocks))
	for i, b := range d.AccountBlocks {
		blocks[i] = b.Copy()
	}
	m := *d.Momentum
	return &nom.DetailedMomentum{Momentum: &m, AccountBlocks: blocks}
}

// Covers three properties on a multi-block signed extension carrying stalled
// far embedded receives alongside a user block:
//  1. A sibling mislabeled as BlockTypeContractSend (with its original Hash)
//     fails the body-commitment precheck with 0 historical reconstructions.
//  2. When the far receives succeed and a later sibling fails, retries reuse
//     the cached patchDump with 0 historical lookups at the receive ack.
//  3. A storage panic inside VerifyBlockInMomentum on a cache hit is recovered
//     as constants.ErrVmRunPanic (not an uncaught panic, not SideChainRejected)
//     and restores earlier pool mutations, after which the genuine momentum
//     still imports and commits expected state.
func TestInsertChain_FarEmbeddedPatchReuseMislabeledSiblingAndPanicRecovery(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()

	held := types.PillarContract
	holder := z.(interface{ HoldInsertsFor(*types.Address) })
	holder.HoldInsertsFor(&held)
	defer holder.HoldInsertsFor(nil)

	send1 := z.InsertSendBlock(&nom.AccountBlock{
		Address:       g.Pillar4.Address,
		ToAddress:     types.PillarContract,
		Data:          definition.ABIPillars.PackMethodPanic(definition.DepositQsrMethodName),
		TokenStandard: types.QsrTokenStandard,
		Amount:        big.NewInt(10 * g.Zexp),
	}, nil, mock.SkipVmChanges)
	send2 := z.InsertSendBlock(&nom.AccountBlock{
		Address:       g.Pillar4.Address,
		ToAddress:     types.PillarContract,
		Data:          definition.ABIPillars.PackMethodPanic(definition.DepositQsrMethodName),
		TokenStandard: types.QsrTokenStandard,
		Amount:        big.NewInt(10 * g.Zexp),
	}, nil, mock.SkipVmChanges)

	z.InsertNewMomentum()
	confirmHeight, err := z.Chain().GetFrontierMomentumStore().GetBlockConfirmationHeight(send1.Hash)
	common.FailIfErr(t, err)
	confirmMomentum := momentumAt(t, z.Chain(), confirmHeight)
	z.InsertMomentumsTo(confirmHeight + 360)

	// Generate recv1 and recv2 locally (temporarily placing recv1 in the pool
	// so recv2 builds on recv1) plus a sibling user send.
	genSupervisor := vm.NewSupervisor(z.Chain(), z.Consensus())
	insert := z.Chain().AcquireInsert("generate sequential stalled receives")
	snap := z.Chain().SnapshotUncommitted(insert, []types.Address{types.PillarContract})
	exec1, err := genSupervisor.GenerateAutoReceive(send1)
	common.FailIfErr(t, err)
	recv1PatchDump := exec1.Transaction.Changes.Dump()
	common.FailIfErr(t, z.Chain().ForceAddAccountBlockTransaction(insert, exec1.Transaction))
	exec2, err := genSupervisor.GenerateAutoReceive(send2)
	common.FailIfErr(t, err)
	recv2PatchDump := exec2.Transaction.Changes.Dump()
	common.FailIfErr(t, z.Chain().RestoreUncommitted(insert, snap))
	insert.Unlock()

	userTx, err := genSupervisor.GenerateFromTemplate(&nom.AccountBlock{
		BlockType:     nom.BlockTypeUserSend,
		Address:       g.User1.Address,
		ToAddress:     g.User2.Address,
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(7),
	}, g.User1.Signer)
	common.FailIfErr(t, err)

	recv1Patch, err := db.NewPatchFromDump(recv1PatchDump)
	common.FailIfErr(t, err)
	recv2Patch, err := db.NewPatchFromDump(recv2PatchDump)
	common.FailIfErr(t, err)
	genuine := buildSignedExtensionMomentum(t, z,
		&nom.AccountBlockTransaction{Block: exec1.Transaction.Block, Changes: recv1Patch},
		&nom.AccountBlockTransaction{Block: exec2.Transaction.Block, Changes: recv2Patch},
		userTx,
	)

	userIdx := -1
	for i, b := range genuine.AccountBlocks {
		if b.Address == g.User1.Address {
			userIdx = i
			break
		}
	}
	if userIdx < 0 {
		t.Fatal("expected user send in generated extension momentum")
	}

	guarded := &extensionGuardChain{Chain: z.Chain()}
	cs := consensus.NewConsensus(db.NewMemDB(), guarded, true)
	common.FailIfErr(t, cs.Init())
	common.FailIfErr(t, cs.Start())
	defer func() { common.FailIfErr(t, cs.Stop()) }()

	supervisor := vm.NewSupervisor(guarded, cs)
	bridge := protocol.NewChainBridge(guarded, cs, verifier.NewVerifier(guarded, cs, vm.CanonicalBasePlasma), supervisor)

	// 1. Mislabeled sibling declaring BlockTypeContractSend (keeping its
	// original Hash) must fail the body-commitment precheck with 0 historical
	// store reconstructions for the earlier receives.
	mislabeled := cloneDetailed(genuine)
	mislabeled.AccountBlocks[userIdx].BlockType = nom.BlockTypeContractSend
	guarded.resetCounts()
	_, err = bridge.InsertChain([]*nom.DetailedMomentum{mislabeled})
	var rejected *protocol.SideChainRejected
	if !errors.As(err, &rejected) || !errors.Is(err, verifier.ErrABHashInvalid) {
		t.Fatalf("expected SideChainRejected(ErrABHashInvalid) for mislabeled ContractSend, got %v", err)
	}
	common.Expect(t, guarded.getMomentumStoreByID[confirmMomentum.Identifier()], 0)

	// 2. First delivery with a tampered sibling fails ApplyMomentum after both
	// far receives succeed and populate patchDump (2 historical lookups total).
	badSibling1 := cloneDetailed(genuine)
	badSibling1.AccountBlocks[userIdx].ChangesHash = types.NewHash([]byte("bad-sibling-1"))
	guarded.resetCounts()
	_, err = bridge.InsertChain([]*nom.DetailedMomentum{badSibling1})
	if !errors.As(err, &rejected) || !errors.Is(err, verifier.ErrMChangesHashInvalid) {
		t.Fatalf("expected SideChainRejected(ErrMChangesHashInvalid), got %v", err)
	}
	common.Expect(t, guarded.getMomentumStoreByID[confirmMomentum.Identifier()], 2)

	// Retry with a different tampered sibling must hit patchDump for both far
	// receives with 0 historical store lookups at confirmMomentum.
	badSibling2 := cloneDetailed(genuine)
	badSibling2.AccountBlocks[userIdx].ChangesHash = types.NewHash([]byte("bad-sibling-2"))
	guarded.resetCounts()
	_, err = bridge.InsertChain([]*nom.DetailedMomentum{badSibling2})
	if !errors.As(err, &rejected) || !errors.Is(err, verifier.ErrMChangesHashInvalid) {
		t.Fatalf("expected SideChainRejected(ErrMChangesHashInvalid) on retry, got %v", err)
	}
	common.Expect(t, guarded.getMomentumStoreByID[confirmMomentum.Identifier()], 0)

	// 3. A storage panic inside VerifyBlockInMomentum on recv2's cache hit
	// (after recv1 was already force-added to the pool) must be recovered as
	// ErrVmRunPanic, remain unclassified, and restore recv1 out of the pool.
	panicPrev := exec2.Transaction.Block.Previous()
	guarded.panicAccountStoreAt = &panicPrev
	guarded.resetCounts()
	_, err = bridge.InsertChain([]*nom.DetailedMomentum{cloneDetailed(genuine)})
	guarded.panicAccountStoreAt = nil
	if !errors.Is(err, constants.ErrVmRunPanic) {
		t.Fatalf("expected constants.ErrVmRunPanic on cache-hit storage panic, got %v", err)
	}
	if errors.As(err, &rejected) {
		t.Fatalf("ErrVmRunPanic must not be classified as SideChainRejected: %v", err)
	}
	common.Expect(t, guarded.getMomentumStoreByID[confirmMomentum.Identifier()], 0)
	if z.Chain().GetPatch(exec1.Transaction.Block.Address, exec1.Transaction.Block.Identifier()) != nil {
		t.Fatal("earlier pool insertion for recv1 was not restored after VerifyBlockInMomentum panic")
	}

	// 4. Genuine delivery reuses patchDump (0 historical lookups at ack) and
	// commits all three blocks with expected state.
	beforeBalance, err := z.Chain().GetFrontierMomentumStore().GetAccountStore(g.User1.Address).GetBalance(types.ZnnTokenStandard)
	common.FailIfErr(t, err)
	guarded.resetCounts()
	_, err = bridge.InsertChain([]*nom.DetailedMomentum{cloneDetailed(genuine)})
	common.FailIfErr(t, err)
	common.Expect(t, guarded.getMomentumStoreByID[confirmMomentum.Identifier()], 0)
	expectFrontierAt(t, z.Chain(), genuine.Momentum)

	afterBalance, err := z.Chain().GetFrontierMomentumStore().GetAccountStore(g.User1.Address).GetBalance(types.ZnnTokenStandard)
	common.FailIfErr(t, err)
	common.Expect(t, new(big.Int).Sub(beforeBalance, afterBalance).Int64(), int64(7))
	for _, s := range []*nom.AccountBlock{send1, send2} {
		recv, err := z.Chain().GetFrontierMomentumStore().GetBlockWhichReceives(s.Hash)
		common.FailIfErr(t, err)
		if recv == nil {
			t.Fatalf("expected send %v to be received after genuine commit", s.Hash)
		}
	}
}

// Covers the retry bounds for non-patch-cacheable far embedded receives:
//  1. Oversized patches exceeding the memory cutoff are bounded by cooldown
//     when a later sibling fails, then import cleanly once cooldown elapses.
//  2. Valid contract failure receipts (executionErr != nil, e.g. CollectReward
//     with zero rewards returning ErrNothingToWithdraw) bound mismatched retries
//     without quarantining, remain eligible to retry after cooldown for
//     transient storage errors, and allow the genuine matching receipt immediately.
func TestInsertChain_FarEmbeddedOversizedPatchAndFailureReceiptRetryBounds(t *testing.T) {
	t.Run("oversized patch cooldown", func(t *testing.T) {
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
		confirmMomentum := momentumAt(t, z.Chain(), confirmHeight)
		z.InsertMomentumsTo(confirmHeight + 360)

		genSupervisor := vm.NewSupervisor(z.Chain(), z.Consensus())
		exec, err := genSupervisor.GenerateAutoReceive(send)
		common.FailIfErr(t, err)
		userTx, err := genSupervisor.GenerateFromTemplate(&nom.AccountBlock{
			BlockType:     nom.BlockTypeUserSend,
			Address:       g.User1.Address,
			ToAddress:     g.User2.Address,
			TokenStandard: types.ZnnTokenStandard,
			Amount:        big.NewInt(3),
		}, g.User1.Signer)
		common.FailIfErr(t, err)
		genuine := buildSignedExtensionMomentum(t, z, exec.Transaction, userTx)

		userIdx := -1
		for i, b := range genuine.AccountBlocks {
			if b.Address == g.User1.Address {
				userIdx = i
				break
			}
		}

		guarded := &extensionGuardChain{Chain: z.Chain()}
		cs := consensus.NewConsensus(db.NewMemDB(), guarded, true)
		common.FailIfErr(t, cs.Init())
		common.FailIfErr(t, cs.Start())
		defer func() { common.FailIfErr(t, cs.Stop()) }()

		supervisor := vm.NewSupervisor(guarded, cs)
		bridge := protocol.NewChainBridge(guarded, cs, verifier.NewVerifier(guarded, cs, vm.CanonicalBasePlasma), supervisor)

		now := time.Unix(2000000000, 0)
		protocol.ConfigureEmbeddedReceiveCacheForTest(bridge, 1, 30*time.Second, func() time.Time { return now })

		// First attempt with failing sibling reconstructs once and arms cooldown.
		bad1 := cloneDetailed(genuine)
		bad1.AccountBlocks[userIdx].ChangesHash = types.NewHash([]byte("oversized-bad-1"))
		guarded.resetCounts()
		_, err = bridge.InsertChain([]*nom.DetailedMomentum{bad1})
		var rejected *protocol.SideChainRejected
		if !errors.As(err, &rejected) || !errors.Is(err, verifier.ErrMChangesHashInvalid) {
			t.Fatalf("expected SideChainRejected(ErrMChangesHashInvalid), got %v", err)
		}
		common.Expect(t, guarded.getMomentumStoreByID[confirmMomentum.Identifier()], 1)

		// Subsequent retries during cooldown perform 0 historical lookups and
		// return an ImportDeferredError carrying RetryAfter (so the downloader
		// preserves the peer connection and retries after expiry).
		for i := 2; i <= 4; i++ {
			bad := cloneDetailed(genuine)
			bad.AccountBlocks[userIdx].ChangesHash = types.NewHash([]byte{byte('0' + i), 'b', 'a', 'd'})
			guarded.resetCounts()
			_, err = bridge.InsertChain([]*nom.DetailedMomentum{bad})
			var deferred *downloader.ImportDeferredError
			if !errors.As(err, &deferred) || !errors.Is(err, downloader.ErrImportDeferred) {
				t.Fatalf("attempt %d: expected ImportDeferredError during cooldown, got %v", i, err)
			}
			if !deferred.RetryAfter.Equal(now.Add(30 * time.Second)) {
				t.Fatalf("attempt %d: RetryAfter=%v, want %v", i, deferred.RetryAfter, now.Add(30*time.Second))
			}
			if errors.As(err, &rejected) {
				t.Fatalf("attempt %d: cooldown deferral must not be classified as SideChainRejected: %v", i, err)
			}
			common.Expect(t, guarded.getMomentumStoreByID[confirmMomentum.Identifier()], 0)
		}

		// Even delivering the genuine momentum during cooldown returns
		// ImportDeferredError (never dropping/quarantining the peer).
		guarded.resetCounts()
		_, err = bridge.InsertChain([]*nom.DetailedMomentum{cloneDetailed(genuine)})
		var deferred *downloader.ImportDeferredError
		if !errors.As(err, &deferred) || !errors.Is(err, downloader.ErrImportDeferred) {
			t.Fatalf("expected genuine delivery during cooldown to return ImportDeferredError, got %v", err)
		}
		common.Expect(t, guarded.getMomentumStoreByID[confirmMomentum.Identifier()], 0)

		// Once cooldown elapses, the genuine momentum is eligible again and commits.
		now = now.Add(31 * time.Second)
		guarded.resetCounts()
		_, err = bridge.InsertChain([]*nom.DetailedMomentum{cloneDetailed(genuine)})
		common.FailIfErr(t, err)
		common.Expect(t, guarded.getMomentumStoreByID[confirmMomentum.Identifier()], 1)
		expectFrontierAt(t, z.Chain(), genuine.Momentum)
	})

	t.Run("matching failure receipt stays unclassified after sibling failure", func(t *testing.T) {
		z := mock.NewMockZenon(t)
		defer z.StopPanic()

		held := types.PillarContract
		holder := z.(interface{ HoldInsertsFor(*types.Address) })
		holder.HoldInsertsFor(&held)
		defer holder.HoldInsertsFor(nil)

		send := z.InsertSendBlock(&nom.AccountBlock{
			Address:   g.User1.Address,
			ToAddress: types.PillarContract,
			Data:      definition.ABIPillars.PackMethodPanic(definition.CollectRewardMethodName),
		}, nil, mock.SkipVmChanges)

		z.InsertNewMomentum()
		confirmHeight, err := z.Chain().GetFrontierMomentumStore().GetBlockConfirmationHeight(send.Hash)
		common.FailIfErr(t, err)
		confirmMomentum := momentumAt(t, z.Chain(), confirmHeight)
		z.InsertMomentumsTo(confirmHeight + 360)

		genSupervisor := vm.NewSupervisor(z.Chain(), z.Consensus())
		exec, err := genSupervisor.GenerateAutoReceive(send)
		common.FailIfErr(t, err)
		if !errors.Is(exec.ReturnedError, constants.ErrNothingToWithdraw) {
			t.Fatalf("expected ErrNothingToWithdraw failure receipt, got %v", exec.ReturnedError)
		}
		userTx, err := genSupervisor.GenerateFromTemplate(&nom.AccountBlock{
			BlockType:     nom.BlockTypeUserSend,
			Address:       g.User1.Address,
			ToAddress:     g.User2.Address,
			TokenStandard: types.ZnnTokenStandard,
			Amount:        big.NewInt(5),
		}, g.User1.Signer)
		common.FailIfErr(t, err)
		genuine := buildSignedExtensionMomentum(t, z, exec.Transaction, userTx)

		recvIdx, userIdx := -1, -1
		for i, b := range genuine.AccountBlocks {
			switch b.Address {
			case types.PillarContract:
				recvIdx = i
			case g.User1.Address:
				userIdx = i
			}
		}

		guarded := &extensionGuardChain{Chain: z.Chain()}
		cs := consensus.NewConsensus(db.NewMemDB(), guarded, true)
		common.FailIfErr(t, cs.Init())
		common.FailIfErr(t, cs.Start())
		defer func() { common.FailIfErr(t, cs.Stop()) }()

		supervisor := vm.NewSupervisor(guarded, cs)
		bridge := protocol.NewChainBridge(guarded, cs, verifier.NewVerifier(guarded, cs, vm.CanonicalBasePlasma), supervisor)
		now := time.Unix(2000000000, 0)
		protocol.ConfigureEmbeddedReceiveCacheForTest(bridge, 64*1024, 30*time.Second, func() time.Time { return now })

		// 1. Matching failure receipt succeeds while the later user sibling fails
		// ApplyMomentum, restoring the pool. The cached bound must stay on the
		// soft/unclassified path (NOT hasDeterministic).
		badSibling := cloneDetailed(genuine)
		badSibling.AccountBlocks[userIdx].ChangesHash = types.NewHash([]byte("fail-receipt-bad-sibling"))
		guarded.resetCounts()
		_, err = bridge.InsertChain([]*nom.DetailedMomentum{badSibling})
		var rejected *protocol.SideChainRejected
		if !errors.As(err, &rejected) || !errors.Is(err, verifier.ErrMChangesHashInvalid) {
			t.Fatalf("expected SideChainRejected(ErrMChangesHashInvalid) for bad sibling, got %v", err)
		}
		common.Expect(t, guarded.getMomentumStoreByID[confirmMomentum.Identifier()], 1)

		// 2. A subsequent tampered ChangesHash on the failure receipt must NOT
		// become a permanent SideChainRejected verdict under issue #239; during
		// cooldown it returns ImportDeferredError with 0 historical lookups.
		badReceipt := cloneDetailed(genuine)
		badReceipt.AccountBlocks[recvIdx].ChangesHash = types.NewHash([]byte("fail-receipt-tampered"))
		guarded.resetCounts()
		_, err = bridge.InsertChain([]*nom.DetailedMomentum{badReceipt})
		if errors.As(err, &rejected) {
			t.Fatalf("matching failure receipt must not turn later mismatch into SideChainRejected: %v", err)
		}
		if !errors.Is(err, downloader.ErrImportDeferred) {
			t.Fatalf("expected ImportDeferredError during cooldown, got %v", err)
		}
		common.Expect(t, guarded.getMomentumStoreByID[confirmMomentum.Identifier()], 0)

		// 3. The genuine momentum reuses the cached patchDump (0 historical lookups)
		// and commits cleanly.
		guarded.resetCounts()
		_, err = bridge.InsertChain([]*nom.DetailedMomentum{cloneDetailed(genuine)})
		common.FailIfErr(t, err)
		common.Expect(t, guarded.getMomentumStoreByID[confirmMomentum.Identifier()], 0)
		expectFrontierAt(t, z.Chain(), genuine.Momentum)
	})

	t.Run("historical multi-receive unhashed predecessor plasma mutation stays bounded", func(t *testing.T) {
		savedRuleHeight := definition.CommunitySporkAddressRenewalStartHeight
		definition.CommunitySporkAddressRenewalStartHeight = 1 << 40
		defer func() { definition.CommunitySporkAddressRenewalStartHeight = savedRuleHeight }()

		z := mock.NewMockZenon(t)
		defer z.StopPanic()

		held := types.PillarContract
		holder := z.(interface{ HoldInsertsFor(*types.Address) })
		holder.HoldInsertsFor(&held)
		defer holder.HoldInsertsFor(nil)

		send1 := z.InsertSendBlock(&nom.AccountBlock{
			Address:       g.Pillar4.Address,
			ToAddress:     types.PillarContract,
			Data:          definition.ABIPillars.PackMethodPanic(definition.DepositQsrMethodName),
			TokenStandard: types.QsrTokenStandard,
			Amount:        big.NewInt(10 * g.Zexp),
		}, nil, mock.SkipVmChanges)
		send2 := z.InsertSendBlock(&nom.AccountBlock{
			Address:       g.Pillar4.Address,
			ToAddress:     types.PillarContract,
			Data:          definition.ABIPillars.PackMethodPanic(definition.DepositQsrMethodName),
			TokenStandard: types.QsrTokenStandard,
			Amount:        big.NewInt(10 * g.Zexp),
		}, nil, mock.SkipVmChanges)

		z.InsertNewMomentum()
		confirmHeight, err := z.Chain().GetFrontierMomentumStore().GetBlockConfirmationHeight(send1.Hash)
		common.FailIfErr(t, err)
		confirmMomentum := momentumAt(t, z.Chain(), confirmHeight)
		z.InsertMomentumsTo(confirmHeight + 360)

		genSupervisor := vm.NewSupervisor(z.Chain(), z.Consensus())
		insert := z.Chain().AcquireInsert("generate sequential stalled receives below renewal")
		snap := z.Chain().SnapshotUncommitted(insert, []types.Address{types.PillarContract})
		exec1, err := genSupervisor.GenerateAutoReceive(send1)
		common.FailIfErr(t, err)
		p1Dump := exec1.Transaction.Changes.Dump()
		common.FailIfErr(t, z.Chain().ForceAddAccountBlockTransaction(insert, exec1.Transaction))
		exec2, err := genSupervisor.GenerateAutoReceive(send2)
		common.FailIfErr(t, err)
		p2Dump := exec2.Transaction.Changes.Dump()
		common.FailIfErr(t, z.Chain().RestoreUncommitted(insert, snap))
		insert.Unlock()

		p1, err := db.NewPatchFromDump(p1Dump)
		common.FailIfErr(t, err)
		p2, err := db.NewPatchFromDump(p2Dump)
		common.FailIfErr(t, err)
		genuine := buildSignedExtensionMomentum(t, z,
			&nom.AccountBlockTransaction{Block: exec1.Transaction.Block, Changes: p1},
			&nom.AccountBlockTransaction{Block: exec2.Transaction.Block, Changes: p2},
		)

		guarded := &extensionGuardChain{Chain: z.Chain()}
		cs := consensus.NewConsensus(db.NewMemDB(), guarded, true)
		common.FailIfErr(t, cs.Init())
		common.FailIfErr(t, cs.Start())
		defer func() { common.FailIfErr(t, cs.Stop()) }()

		supervisor := vm.NewSupervisor(guarded, cs)
		bridge := protocol.NewChainBridge(guarded, cs, verifier.NewVerifier(guarded, cs, vm.CanonicalBasePlasma), supervisor)
		now := time.Unix(2000000000, 0)
		protocol.ConfigureEmbeddedReceiveCacheForTest(bridge, 64*1024, 30*time.Second, func() time.Time { return now })

		// Mutate recv1's unhashed BasePlasma (changing recv1's pool patch hash
		// below the renewal height) while tampering recv2's ChangesHash across
		// retries: recv2's reconstruction bound must NOT depend on prevPatchHash.
		attempt1 := cloneDetailed(genuine)
		attempt1.AccountBlocks[0].BasePlasma = 101
		attempt1.AccountBlocks[1].ChangesHash = types.NewHash([]byte("multi-recv-bad-1"))
		guarded.resetCounts()
		_, err = bridge.InsertChain([]*nom.DetailedMomentum{attempt1})
		var rejected *protocol.SideChainRejected
		if !errors.As(err, &rejected) || !errors.Is(err, constants.ErrAutoReceiveChangesHashMismatch) {
			t.Fatalf("attempt 1: expected SideChainRejected(ErrAutoReceiveChangesHashMismatch), got %v", err)
		}
		common.Expect(t, guarded.getMomentumStoreByID[confirmMomentum.Identifier()], 2)

		attempt2 := cloneDetailed(genuine)
		attempt2.AccountBlocks[0].BasePlasma = 202
		attempt2.AccountBlocks[1].ChangesHash = types.NewHash([]byte("multi-recv-bad-2"))
		guarded.resetCounts()
		_, err = bridge.InsertChain([]*nom.DetailedMomentum{attempt2})
		if !errors.As(err, &rejected) || !errors.Is(err, constants.ErrAutoReceiveChangesHashMismatch) {
			t.Fatalf("attempt 2: expected SideChainRejected(ErrAutoReceiveChangesHashMismatch), got %v", err)
		}
		// recv1 reuses its patchDump (0 lookups) and recv2 hits the bound
		// keyed without prevPatchHash (0 lookups).
		common.Expect(t, guarded.getMomentumStoreByID[confirmMomentum.Identifier()], 0)

		// Even when recv2 matches its expected hashes but recv1's mutated
		// BasePlasma misses recv2's patchDump key, cooldown bounds recv2.
		attempt3 := cloneDetailed(genuine)
		attempt3.AccountBlocks[0].BasePlasma = 303
		guarded.resetCounts()
		_, err = bridge.InsertChain([]*nom.DetailedMomentum{attempt3})
		if !errors.As(err, &rejected) || !errors.Is(err, verifier.ErrMChangesHashInvalid) {
			t.Fatalf("attempt 3: expected SideChainRejected(ErrMChangesHashInvalid), got %v", err)
		}
		common.Expect(t, guarded.getMomentumStoreByID[confirmMomentum.Identifier()], 1)

		attempt4 := cloneDetailed(genuine)
		attempt4.AccountBlocks[0].BasePlasma = 404
		guarded.resetCounts()
		_, err = bridge.InsertChain([]*nom.DetailedMomentum{attempt4})
		if !errors.Is(err, downloader.ErrImportDeferred) {
			t.Fatalf("attempt 4: expected ImportDeferredError under cooldown, got %v", err)
		}
		common.Expect(t, guarded.getMomentumStoreByID[confirmMomentum.Identifier()], 0)
	})

	t.Run("failed contract receipt cooldown and genuine import", func(t *testing.T) {
		z := mock.NewMockZenon(t)
		defer z.StopPanic()

		held := types.PillarContract
		holder := z.(interface{ HoldInsertsFor(*types.Address) })
		holder.HoldInsertsFor(&held)
		defer holder.HoldInsertsFor(nil)

		// User1 has no pillar rewards, so CollectReward returns
		// constants.ErrNothingToWithdraw (valid receipt with executionErr != nil).
		send := z.InsertSendBlock(&nom.AccountBlock{
			Address:   g.User1.Address,
			ToAddress: types.PillarContract,
			Data:      definition.ABIPillars.PackMethodPanic(definition.CollectRewardMethodName),
		}, nil, mock.SkipVmChanges)

		z.InsertNewMomentum()
		confirmHeight, err := z.Chain().GetFrontierMomentumStore().GetBlockConfirmationHeight(send.Hash)
		common.FailIfErr(t, err)
		confirmMomentum := momentumAt(t, z.Chain(), confirmHeight)
		z.InsertMomentumsTo(confirmHeight + 360)

		genSupervisor := vm.NewSupervisor(z.Chain(), z.Consensus())
		exec, err := genSupervisor.GenerateAutoReceive(send)
		common.FailIfErr(t, err)
		if !errors.Is(exec.ReturnedError, constants.ErrNothingToWithdraw) {
			t.Fatalf("expected ErrNothingToWithdraw failure receipt, got %v", exec.ReturnedError)
		}
		genuine := buildSignedExtensionMomentum(t, z, exec.Transaction)

		guarded := &extensionGuardChain{Chain: z.Chain()}
		cs := consensus.NewConsensus(db.NewMemDB(), guarded, true)
		common.FailIfErr(t, cs.Init())
		common.FailIfErr(t, cs.Start())
		defer func() { common.FailIfErr(t, cs.Stop()) }()

		supervisor := vm.NewSupervisor(guarded, cs)
		bridge := protocol.NewChainBridge(guarded, cs, verifier.NewVerifier(guarded, cs, vm.CanonicalBasePlasma), supervisor)

		now := time.Unix(2000000000, 0)
		protocol.ConfigureEmbeddedReceiveCacheForTest(bridge, 64*1024, 30*time.Second, func() time.Time { return now })

		// 1. First tampered ChangesHash delivery reconstructs once and returns
		// an unclassified error (preserving issue #239 semantics).
		bad1 := cloneDetailed(genuine)
		bad1.AccountBlocks[0].ChangesHash = types.NewHash([]byte("fail-receipt-bad-1"))
		guarded.resetCounts()
		_, err = bridge.InsertChain([]*nom.DetailedMomentum{bad1})
		var rejected *protocol.SideChainRejected
		if err == nil || errors.As(err, &rejected) {
			t.Fatalf("expected unclassified error on failed-generation mismatch, got %v", err)
		}
		common.Expect(t, guarded.getMomentumStoreByID[confirmMomentum.Identifier()], 1)

		// 2. Further tampered deliveries during cooldown perform 0 historical lookups.
		for i := 2; i <= 4; i++ {
			bad := cloneDetailed(genuine)
			bad.AccountBlocks[0].ChangesHash = types.NewHash([]byte{byte('a' + i), 'f', 'a', 'i', 'l'})
			guarded.resetCounts()
			_, err = bridge.InsertChain([]*nom.DetailedMomentum{bad})
			if err == nil || errors.As(err, &rejected) {
				t.Fatalf("attempt %d: expected unclassified deferral error, got %v", i, err)
			}
			common.Expect(t, guarded.getMomentumStoreByID[confirmMomentum.Identifier()], 0)
		}

		// 3. After cooldown elapses, a retry is eligible again (e.g. if the
		// prior failure was a transient storage error), re-arming the cooldown.
		now = now.Add(31 * time.Second)
		bad5 := cloneDetailed(genuine)
		bad5.AccountBlocks[0].ChangesHash = types.NewHash([]byte("fail-receipt-bad-5"))
		guarded.resetCounts()
		_, err = bridge.InsertChain([]*nom.DetailedMomentum{bad5})
		if err == nil || errors.As(err, &rejected) {
			t.Fatalf("expected unclassified error after cooldown elapsed, got %v", err)
		}
		common.Expect(t, guarded.getMomentumStoreByID[confirmMomentum.Identifier()], 1)

		bad6 := cloneDetailed(genuine)
		bad6.AccountBlocks[0].ChangesHash = types.NewHash([]byte("fail-receipt-bad-6"))
		guarded.resetCounts()
		_, err = bridge.InsertChain([]*nom.DetailedMomentum{bad6})
		if err == nil || errors.As(err, &rejected) {
			t.Fatalf("expected unclassified deferral error inside renewed cooldown, got %v", err)
		}
		common.Expect(t, guarded.getMomentumStoreByID[confirmMomentum.Identifier()], 0)

		// 4. Even while the cooldown is active, the genuine matching failure
		// receipt bypasses the cooldown and imports immediately.
		guarded.resetCounts()
		_, err = bridge.InsertChain([]*nom.DetailedMomentum{cloneDetailed(genuine)})
		common.FailIfErr(t, err)
		common.Expect(t, guarded.getMomentumStoreByID[confirmMomentum.Identifier()], 1)
		expectFrontierAt(t, z.Chain(), genuine.Momentum)
	})
}

// A direct InsertChain caller supplying a prefetched block with a nil
// DescendantBlocks entry must be rejected in validatePrefetchedBlocks before
// firstRemembered serializes the block (avoiding a nil-pointer panic).
func TestInsertChain_NilDescendantRejectedBeforeSerialization(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()
	z.InsertMomentumsTo(4)

	frontier := momentumAt(t, z.Chain(), 4)
	block := &nom.AccountBlock{
		Version:          1,
		ChainIdentifier:  frontier.ChainIdentifier,
		BlockType:        nom.BlockTypeContractReceive,
		Address:          types.StakeContract,
		Height:           1,
		Hash:             types.NewHash([]byte("nil-descendant")),
		DescendantBlocks: []*nom.AccountBlock{nil},
	}
	m := unsignedMomentum(frontier, frontier.Height+1)
	m.Content = nom.NewMomentumContent([]*nom.AccountBlock{block})
	m.Hash = m.ComputeHash()

	_, bridge := newSideChainBridge(z)
	_, err := bridge.InsertChain([]*nom.DetailedMomentum{{
		Momentum:      m,
		AccountBlocks: []*nom.AccountBlock{block},
	}})
	if err == nil {
		t.Fatal("expected nil descendant block to be rejected")
	}
}
