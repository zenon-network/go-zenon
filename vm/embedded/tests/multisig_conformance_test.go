package tests

import (
	"crypto/ed25519"
	"math/big"
	"testing"

	g "github.com/zenon-network/go-zenon/chain/genesis/mock"
	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/rpc/api"
	"github.com/zenon-network/go-zenon/verifier"
	"github.com/zenon-network/go-zenon/vm"
	"github.com/zenon-network/go-zenon/vm/constants"
	"github.com/zenon-network/go-zenon/vm/embedded/definition"
	"github.com/zenon-network/go-zenon/wallet"
	"github.com/zenon-network/go-zenon/zenon/mock"
)

// This file holds full-chain multisig conformance tests: scenarios that need a real
// running-chain fixture (as opposed to a direct contract-method or verifier-struct call) to be
// convincing.

// --- First block racing the CreateMultisig receive ---------------------------------------------

// TestMultisig_FirstBlockRacesCreateReceive: a CreateMultisig send has been accepted into the
// account pool but no momentum has cemented it yet (in this codebase, cementing the send and
// processing its embedded receive happen together in the SAME momentum -- there is no separate
// "cemented but not yet received" tick to observe). The race window this scenario cares about is
// therefore the one that genuinely exists: before that momentum lands, the registry record does
// not exist at all, and a block submitted from the (already offline-derivable) multisig address
// during that window must be rejected with ErrMultisigNoPolicy, not silently accepted or panic --
// proving the registry read is strictly gated on the receive having actually landed, not on the
// send having merely been seen/pooled.
func TestMultisig_FirstBlockRacesCreateReceive(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()
	activateMultisig(t, z)

	s1 := genMultisigWalletSigner(t)
	signers := []ed25519.PublicKey{g.User1.Public, s1.pub}
	const nonce = 99

	multisigAddr := wallet.DeriveMultisigAddress(g.User1.Public, nonce)
	// fund the not-yet-existing address ahead of time -- fusing plasma to an address is a
	// balance-table operation and does not require the beneficiary to have ever sent a block.
	fuseQsrTo(t, z, multisigAddr, 50)

	createTemplate := wallet.NewCreateMultisigTemplate(g.User1.Address, nonce, 2, signers)
	z.InsertSendBlock(createTemplate, nil, mock.SkipVmChanges)
	// deliberately no InsertNewMomentum() yet: the create send is only pooled, not cemented, and
	// its embedded receive has certainly not run.

	if rec, err := definition.GetMultisigRecord(z.Chain().GetFrontierAccountStore(types.MultisigContract).Storage(), multisigAddr); err != nil {
		t.Fatal(err)
	} else if rec != nil {
		t.Fatalf("expected no registry record yet (create not even cemented), got %+v", rec)
	}

	// Race: submit the multisig account's own first block while the registry is still empty.
	racing := wallet.NewChangePolicyTemplate(multisigAddr, 2, signers, false)
	supervisor := vm.NewSupervisor(z.Chain(), z.Consensus())
	frozen := freezeMultisigBlock(t, supervisor, racing)
	wallet.AssembleMultisigAuth(frozen, [][]byte{make([]byte, ed25519.SignatureSize)})

	ledgerApi := api.NewLedgerApi(z)
	err := ledgerApi.PublishRawTransaction(&api.AccountBlock{AccountBlock: *frozen})
	if err != constants.ErrMultisigNoPolicy {
		t.Fatalf("expected ErrMultisigNoPolicy for a block racing the creation receive, got %v", err)
	}

	// Once the receive lands, the registry exists and the same shape of block (properly signed)
	// is accepted -- already proven end-to-end by TestMultisigWallet_FullRoundTrip; here we only
	// need to confirm the race window closes.
	z.InsertNewMomentum()
	if rec, err := definition.GetMultisigRecord(z.Chain().GetFrontierAccountStore(types.MultisigContract).Storage(), multisigAddr); err != nil {
		t.Fatal(err)
	} else if rec == nil {
		t.Fatal("expected the registry record to exist once the CreateMultisig receive has landed")
	}
}

// --- Three changes across a maturity boundary, full chain ---------------------------------------

// TestMultisig_ThreeChangesAcrossMaturityBoundary_FullChain is the verifier/full-chain equivalent
// of implementation.TestChangePolicy_ThreeChangesAcrossMaturityBoundary (which calls
// ChangePolicyMethod.ReceiveBlock directly): here every change is a real signed block, built with
// the wallet helpers, submitted through PublishRawTransaction and confirmed by momenta, so the
// same non-reversion invariant is proven through the actual verifier + embedded-receive path, not
// just at the contract-method level.
func TestMultisig_ThreeChangesAcrossMaturityBoundary_FullChain(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()
	activateMultisig(t, z)

	s0a := genMultisigWalletSigner(t)
	s0b := genMultisigWalletSigner(t)
	signers0 := []ed25519.PublicKey{g.User1.Public, s0a.pub, s0b.pub}
	const nonce = 55
	const threshold = 2

	createTemplate := wallet.NewCreateMultisigTemplate(g.User1.Address, nonce, threshold, signers0)
	z.InsertSendBlock(createTemplate, nil, mock.SkipVmChanges)
	z.InsertNewMomentum()
	z.InsertNewMomentum()

	multisigAddr := wallet.DeriveMultisigAddress(g.User1.Public, nonce)
	fuseQsrTo(t, z, multisigAddr, 300)

	supervisor := vm.NewSupervisor(z.Chain(), z.Consensus())

	getRecord := func() *definition.MultisigRecord {
		rec, err := definition.GetMultisigRecord(z.Chain().GetFrontierAccountStore(types.MultisigContract).Storage(), multisigAddr)
		common.FailIfErr(t, err)
		return rec
	}
	submitChange := func(signBy []multisigWalletSigner, threshold uint8, newSigners []ed25519.PublicKey) {
		template := wallet.NewChangePolicyTemplate(multisigAddr, threshold, newSigners, false)
		frozen := freezeMultisigBlock(t, supervisor, template)
		sigs := make([][]byte, 0, len(signBy))
		for _, s := range signBy {
			sigs = append(sigs, wallet.SignMultisigBlock(frozen, s.pair))
		}
		wallet.AssembleMultisigAuth(frozen, sigs)
		ledgerApi := api.NewLedgerApi(z)
		common.FailIfErr(t, ledgerApi.PublishRawTransaction(&api.AccountBlock{AccountBlock: *frozen}))
		z.InsertNewMomentum()
		z.InsertNewMomentum()
	}
	policyEqualsSigners := func(rec *definition.MultisigRecord, want []ed25519.PublicKey) bool {
		if len(rec.Active.Signers) != len(want) {
			return false
		}
		for _, w := range want {
			found := false
			for _, s := range rec.Active.Signers {
				if string(s) == string(w) {
					found = true
					break
				}
			}
			if !found {
				return false
			}
		}
		return true
	}

	// P0 -> P1
	s1a := genMultisigWalletSigner(t)
	s1b := genMultisigWalletSigner(t)
	s1c := genMultisigWalletSigner(t)
	signers1 := []ed25519.PublicKey{s1a.pub, s1b.pub, s1c.pub}
	submitChange([]multisigWalletSigner{{pub: g.User1.Public, pair: g.User1}, s0a}, 2, signers1)

	rec := getRecord()
	if rec.Pending == nil || !policyEqualsSigners(&definition.MultisigRecord{Active: *rec.Pending}, signers1) {
		t.Fatalf("expected P1 staged as Pending, got %+v", rec)
	}
	if !policyEqualsSigners(rec, signers0) {
		t.Fatalf("expected Active still == P0 immediately after staging P1, got %+v", rec.Active)
	}
	pendingHeight1 := rec.PendingHeight

	// mature P1
	z.InsertMomentumsTo(pendingHeight1 + 60)

	// P1 -> P2, signed under P1's signers (must be P1, not P0, proving Active matured correctly)
	s2a := genMultisigWalletSigner(t)
	s2b := genMultisigWalletSigner(t)
	signers2 := []ed25519.PublicKey{s2a.pub, s2b.pub}
	submitChange([]multisigWalletSigner{s1a, s1b}, 2, signers2)

	rec = getRecord()
	if !policyEqualsSigners(rec, signers1) {
		t.Fatalf("expected Active == P1 (matured) after staging P2, never P0, got %+v", rec.Active)
	}
	if rec.Pending == nil || !policyEqualsSigners(&definition.MultisigRecord{Active: *rec.Pending}, signers2) {
		t.Fatalf("expected P2 staged as Pending, got %+v", rec)
	}
	pendingHeight2 := rec.PendingHeight

	// mature P2
	z.InsertMomentumsTo(pendingHeight2 + 60)

	// P2 -> P3, signed under P2's signers
	s3a := genMultisigWalletSigner(t)
	s3b := genMultisigWalletSigner(t)
	signers3 := []ed25519.PublicKey{s3a.pub, s3b.pub}
	submitChange([]multisigWalletSigner{s2a, s2b}, 2, signers3)

	rec = getRecord()
	if !policyEqualsSigners(rec, signers2) {
		t.Fatalf("expected Active == P2 (matured) after staging P3, never P0 or P1, got %+v", rec.Active)
	}
	if rec.Pending == nil || !policyEqualsSigners(&definition.MultisigRecord{Active: *rec.Pending}, signers3) {
		t.Fatalf("expected P3 staged as Pending, got %+v", rec)
	}
}

// --- Reorg-stability of Promote() ---------------------------------------------------------------

// TestMultisig_ReorgStability_UnmaturedChangeIsHarmless simulates a real momentum-chain reorg
// (chain.RollbackTo, the same primitive protocol/chain_bridge.go uses to switch to a longer
// side-chain) that removes a staged-but-unmatured ChangePolicy: since no block was ever
// authorised under it (nothing can be signed against a policy that only exists as an unmatured
// Pending -- the verifier always reads Promote(rec, H) for the CURRENT height, not a future
// one), rolling it back is provably harmless: the registry reverts to exactly its pre-change
// state and the chain can be rebuilt forward with different (or no) content with no fork in
// Active.
//
// What this test does NOT attempt (documented, not silently skipped): reproducing the
// protocol-level rule that an already-accepted reorg cannot reach more than 30 momentums deep
// (protocol/chain_bridge.go:176, itself the reason MultisigPolicyMaturityDelay/MultisigMaxMaLag
// are set to 60 = 2x that bound). That bound is enforced in the sync/AddMomentums path, which is
// a generic chain invariant this feature relies on rather than reimplements, and is not
// multisig-specific -- exercising it faithfully would require driving two independent chain
// instances through the protocol package, which is out of scope for this phase. What IS proven
// directly, in addition to the real reorg above: (a) definition.Promote()/ActivePolicyAtHeight
// are pure functions of (rec, H) with no hidden mutable state (vm/embedded/definition/multisig_test.go
// Promote table tests); (b) the only place that ever WRITES a materialised Active is
// ChangePolicyMethod.ReceiveBlock (vm/embedded/implementation/multisig.go) -- a mere read never
// mutates storage, so no amount of read-path activity ("time passing") can flip authority absent
// a new, independently-verified block; and (c) TestMultisig_RotationMaturity
// (verifier/account_block_multisig_test.go) already shows that once a rotation has matured, a
// block claiming authorisation under the superseded old set is rejected
// (ErrABSignatureInvalid) -- so even a byte-identical resurrection of an old-set block from
// before a deep reorg could not forge authority after the fact.
func TestMultisig_ReorgStability_UnmaturedChangeIsHarmless(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()
	activateMultisig(t, z)

	s1 := genMultisigWalletSigner(t)
	s2 := genMultisigWalletSigner(t)
	signers := []ed25519.PublicKey{g.User1.Public, s1.pub, s2.pub}
	const nonce = 77
	const threshold = 2

	createTemplate := wallet.NewCreateMultisigTemplate(g.User1.Address, nonce, threshold, signers)
	z.InsertSendBlock(createTemplate, nil, mock.SkipVmChanges)
	z.InsertNewMomentum()
	z.InsertNewMomentum()

	multisigAddr := wallet.DeriveMultisigAddress(g.User1.Public, nonce)
	fuseQsrTo(t, z, multisigAddr, 100)

	getRecord := func() *definition.MultisigRecord {
		rec, err := definition.GetMultisigRecord(z.Chain().GetFrontierAccountStore(types.MultisigContract).Storage(), multisigAddr)
		common.FailIfErr(t, err)
		return rec
	}

	preChangeIdentifier := z.Chain().GetFrontierMomentumStore().Identifier()
	if getRecord().Pending != nil {
		t.Fatal("expected no pending change before the snapshot")
	}

	// Stage (and mature-receive, i.e. cement) a ChangePolicy - well within the 30-momentum reorg
	// bound and nowhere near its own 60-momentum maturity delay.
	newSigners := []ed25519.PublicKey{g.User1.Public, s1.pub}
	changeTemplate := wallet.NewChangePolicyTemplate(multisigAddr, threshold, newSigners, false)
	supervisor := vm.NewSupervisor(z.Chain(), z.Consensus())
	frozen := freezeMultisigBlock(t, supervisor, changeTemplate)
	wallet.AssembleMultisigAuth(frozen, [][]byte{
		wallet.SignMultisigBlock(frozen, g.User1),
		wallet.SignMultisigBlock(frozen, s1.pair),
	})
	ledgerApi := api.NewLedgerApi(z)
	common.FailIfErr(t, ledgerApi.PublishRawTransaction(&api.AccountBlock{AccountBlock: *frozen}))
	z.InsertNewMomentum()
	z.InsertNewMomentum()

	staged := getRecord()
	if staged.Pending == nil {
		t.Fatal("expected the ChangePolicy to have staged a pending change before the reorg")
	}

	// Reorg: roll the momentum chain back to before the ChangePolicy was ever sent, exactly the
	// primitive protocol/chain_bridge.go:185 uses when switching to a longer side-chain.
	insert := z.Chain().AcquireInsert("test-reorg-multisig")
	if err := z.Chain().RollbackTo(insert, preChangeIdentifier); err != nil {
		insert.Unlock()
		t.Fatalf("rollback failed: %v", err)
	}
	if err := z.Chain().RollbackCacheTo(insert, preChangeIdentifier); err != nil {
		insert.Unlock()
		t.Fatalf("cache rollback failed: %v", err)
	}
	insert.Unlock()

	afterRollback := getRecord()
	if afterRollback.Pending != nil {
		t.Fatalf("expected the reorged-away ChangePolicy to leave no trace, got Pending=%+v", afterRollback.Pending)
	}
	if len(afterRollback.Active.Signers) != len(signers) {
		t.Fatalf("expected Active to still be the original policy after the reorg, got %+v", afterRollback.Active)
	}

	// Rebuild forward on the (now canonical) chain past where the reorged change would have
	// matured, with NO further change submitted: Active must never spontaneously flip.
	z.InsertMomentumsTo(preChangeIdentifier.Height + 60 + 5)
	final := getRecord()
	if final.Pending != nil || len(final.Active.Signers) != len(signers) {
		t.Fatalf("expected Active to remain the original policy indefinitely absent a new authorised change, got %+v", final)
	}
}

// --- Replay --------------------------------------------------------------------------------------

// TestMultisig_Replay covers both replay sub-cases: (a) signatures captured over one block's Hash
// cannot be replayed onto a different block (different content/height/hash) -- they fail
// signature verification, not some bespoke "replay" check, because the ed25519 preimage is the
// block's own frozen Hash; (b) resubmitting the byte-identical, already-cemented block at the
// same position is rejected (ErrABPrevHeightExists, the same-position "already received"
// rejection) and never re-applies the state change or advances the account frontier a second
// time.
func TestMultisig_Replay(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()
	activateMultisig(t, z)

	s1 := genMultisigWalletSigner(t)
	s2 := genMultisigWalletSigner(t)
	signers := []ed25519.PublicKey{g.User1.Public, s1.pub, s2.pub}
	const nonce = 88
	const threshold = 2

	createTemplate := wallet.NewCreateMultisigTemplate(g.User1.Address, nonce, threshold, signers)
	z.InsertSendBlock(createTemplate, nil, mock.SkipVmChanges)
	z.InsertNewMomentum()
	z.InsertNewMomentum()

	multisigAddr := wallet.DeriveMultisigAddress(g.User1.Public, nonce)
	fuseQsrTo(t, z, multisigAddr, 100)

	supervisor := vm.NewSupervisor(z.Chain(), z.Consensus())
	ledgerApi := api.NewLedgerApi(z)

	// First (legitimate) ChangePolicy, height 1.
	newSigners := []ed25519.PublicKey{g.User1.Public, s1.pub}
	changeTemplate := wallet.NewChangePolicyTemplate(multisigAddr, threshold, newSigners, false)
	frozen1 := freezeMultisigBlock(t, supervisor, changeTemplate)
	capturedSigs := [][]byte{
		wallet.SignMultisigBlock(frozen1, g.User1),
		wallet.SignMultisigBlock(frozen1, s1.pair),
	}
	wallet.AssembleMultisigAuth(frozen1, capturedSigs)
	common.FailIfErr(t, ledgerApi.PublishRawTransaction(&api.AccountBlock{AccountBlock: *frozen1}))
	z.InsertNewMomentum()
	z.InsertNewMomentum()

	if frontier := z.Chain().GetFrontierAccountStore(multisigAddr).Identifier(); frontier.Height != 1 {
		t.Fatalf("expected the first ChangePolicy to be confirmed at height 1, got %v", frontier.Height)
	}

	// (a) Replay at another position: reuse the captured signatures (signed over frozen1.Hash) on
	// a DIFFERENT block (different Data => different Hash). The signatures must fail to verify.
	otherSigners := []ed25519.PublicKey{g.User1.Public, s2.pub}
	otherTemplate := wallet.NewChangePolicyTemplate(multisigAddr, threshold, otherSigners, false)
	frozen2 := freezeMultisigBlock(t, supervisor, otherTemplate)
	wallet.AssembleMultisigAuth(frozen2, capturedSigs)
	err := ledgerApi.PublishRawTransaction(&api.AccountBlock{AccountBlock: *frozen2})
	if err != verifier.ErrABSignatureInvalid {
		t.Fatalf("expected ErrABSignatureInvalid replaying captured signatures onto a different block, got %v", err)
	}

	// (b) Replay at the same position: resubmit the byte-identical, already-cemented frozen1. The
	// structural verifier rejects it (ErrABPrevHeightExists: this account's height-1 slot is
	// already cemented by this very block) before the account-pool's own idempotent
	// "already-inserted" de-dup path is even reached -- either way, the frontier does not move.
	err = ledgerApi.PublishRawTransaction(&api.AccountBlock{AccountBlock: *frozen1})
	if err != verifier.ErrABPrevHeightExists {
		t.Fatalf("expected ErrABPrevHeightExists resubmitting an already-cemented block at the same position, got %v", err)
	}
	if frontier := z.Chain().GetFrontierAccountStore(multisigAddr).Identifier(); frontier.Height != 1 {
		t.Fatalf("expected the frontier to remain at height 1 after replaying the same block, got %v", frontier.Height)
	}
}

// --- CreateMultisig burn -------------------------------------------------------------------------

// TestMultisig_CreateBurnsZnn confirms the received CreateMultisig amount is actually burned (ZNN
// TotalSupply decreases by constants.MultisigCreationBurnAmount), not stranded in the contract
// balance.
func TestMultisig_CreateBurnsZnn(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()
	activateMultisig(t, z)

	tokenStore := z.Chain().GetFrontierAccountStore(types.TokenContract).Storage()
	before, err := definition.GetTokenInfo(tokenStore, types.ZnnTokenStandard)
	common.FailIfErr(t, err)
	supplyBefore := new(big.Int).Set(before.TotalSupply)

	s1 := genMultisigWalletSigner(t)
	signers := []ed25519.PublicKey{g.User1.Public, s1.pub}
	const nonce = 123

	createTemplate := wallet.NewCreateMultisigTemplate(g.User1.Address, nonce, 2, signers)
	z.InsertSendBlock(createTemplate, nil, mock.SkipVmChanges)
	z.InsertNewMomentum()
	z.InsertNewMomentum()

	multisigAddr := wallet.DeriveMultisigAddress(g.User1.Public, nonce)
	if rec, err := definition.GetMultisigRecord(z.Chain().GetFrontierAccountStore(types.MultisigContract).Storage(), multisigAddr); err != nil {
		t.Fatal(err)
	} else if rec == nil {
		t.Fatal("expected multisig record to exist after CreateMultisig")
	}

	after, err := definition.GetTokenInfo(z.Chain().GetFrontierAccountStore(types.TokenContract).Storage(), types.ZnnTokenStandard)
	common.FailIfErr(t, err)
	want := new(big.Int).Sub(supplyBefore, constants.MultisigCreationBurnAmount)
	if after.TotalSupply.Cmp(want) != 0 {
		t.Fatalf("expected TotalSupply to decrease by MultisigCreationBurnAmount, before=%v after=%v", supplyBefore, after.TotalSupply)
	}
}

// --- Poison-block liveness (Fix 8 follow-up) ------------------------------------------------------

// TestMultisig_StaleMomentumAcknowledged_DoesNotHaltProduction reproduces the reviewer's P0: a
// multisig block is frozen (pinning MomentumAcknowledged to the frontier at that moment), then the
// chain is advanced, without submitting it, until its MA lags the new frontier by exactly
// MultisigMaxMaLag. At that point it still passes the account-pool's mempool recency pre-filter
// (MA+MultisigMaxMaLag >= frontierH, the boundary is inclusive) but can never satisfy
// rawMomentumVerifier.content() for any future momentum (whose height is always frontier+1 or
// higher, i.e. one past the mempool anchor). Before the producer-side filter in
// pillar/worker_momentum.go, the block selector would keep re-selecting this "poison" block into
// every momentum attempt, content() would reject the whole momentum every time, and momentum
// production would halt forever. The fix must make production simply skip the block: a momentum
// is still produced, it does not contain the poison block, and the block itself is never
// committed.
func TestMultisig_StaleMomentumAcknowledged_DoesNotHaltProduction(t *testing.T) {
	// InsertMomentumsTo inserts momentums one at a time, so walking all the way to the widened
	// MultisigMaxMaLag would be tens of thousands of real insertions. Override it to a small value
	// for the duration of this test; its stale-MA assertions are unaffected by the magnitude.
	origMaxMaLag := constants.MultisigMaxMaLag
	constants.MultisigMaxMaLag = 60
	defer func() { constants.MultisigMaxMaLag = origMaxMaLag }()

	z := mock.NewMockZenon(t)
	defer z.StopPanic()
	activateMultisig(t, z)

	s1 := genMultisigWalletSigner(t)
	signers := []ed25519.PublicKey{g.User1.Public, s1.pub}
	const nonce = 300
	const threshold = 2

	createTemplate := wallet.NewCreateMultisigTemplate(g.User1.Address, nonce, threshold, signers)
	z.InsertSendBlock(createTemplate, nil, mock.SkipVmChanges)
	z.InsertNewMomentum()
	z.InsertNewMomentum()

	multisigAddr := wallet.DeriveMultisigAddress(g.User1.Public, nonce)
	fuseQsrTo(t, z, multisigAddr, 100)

	supervisor := vm.NewSupervisor(z.Chain(), z.Consensus())

	// Freeze a ChangePolicy send now: MomentumAcknowledged is pinned to the CURRENT frontier.
	changeTemplate := wallet.NewChangePolicyTemplate(multisigAddr, threshold, signers, false)
	frozen := freezeMultisigBlock(t, supervisor, changeTemplate)
	frozenMA := frozen.MomentumAcknowledged.Height

	// Advance the chain, WITHOUT submitting the frozen block yet, until its MA lags the frontier
	// by exactly MultisigMaxMaLag -- the mempool filter's inclusive boundary (still accepted) but
	// already the poison point for content() at the next momentum height (frontier+1).
	target := frozenMA + constants.MultisigMaxMaLag
	z.InsertMomentumsTo(target)

	wallet.AssembleMultisigAuth(frozen, [][]byte{
		wallet.SignMultisigBlock(frozen, g.User1),
		wallet.SignMultisigBlock(frozen, s1.pair),
	})
	ledgerApi := api.NewLedgerApi(z)
	common.FailIfErr(t, ledgerApi.PublishRawTransaction(&api.AccountBlock{AccountBlock: *frozen}))

	frontierBefore := z.Chain().GetFrontierMomentumStore().Identifier().Height

	// Triggering momentum generation must NOT halt production.
	z.InsertNewMomentum()

	frontierAfter := z.Chain().GetFrontierMomentumStore().Identifier().Height
	if frontierAfter != frontierBefore+1 {
		t.Fatalf("expected momentum production to continue despite the stale-MA multisig block in the pool, frontier went from %v to %v", frontierBefore, frontierAfter)
	}

	// The poison block must never be committed -- content() would reject any momentum containing
	// it. Check the actually-cemented chain (GetFrontierAccountBlock), not the pool-level
	// GetFrontierAccountStore, which also reflects uncommitted pooled blocks.
	assertNotCommitted := func() {
		t.Helper()
		block, err := z.Chain().GetFrontierMomentumStore().GetFrontierAccountBlock(multisigAddr)
		common.FailIfErr(t, err)
		if block != nil {
			t.Fatalf("expected the stale-MA multisig block to never be committed, got committed block at height %v", block.Height)
		}
	}
	assertNotCommitted()

	// Confirm production keeps working, and the block stays uncommitted, over further attempts.
	z.InsertNewMomentum()
	z.InsertNewMomentum()
	assertNotCommitted()
}

// containsPubKey reports whether pub is present in signers.
func containsPubKey(signers []ed25519.PublicKey, pub ed25519.PublicKey) bool {
	for _, s := range signers {
		if string(s) == string(pub) {
			return true
		}
	}
	return false
}

// --- Admission-time rejection of a stale-authority send after maturity -----------------------------

// TestMultisig_ChangePolicy_StaleAuthorityAfterMaturity: a ChangePolicy send threshold-signed by
// the OLD active set is frozen (capturing its send-time Hash) BEFORE a rotation to a new set
// matures, but only submitted AFTER the rotation has matured. The admission-time pre-filter
// (accountBlockTransactionVerifier.signature()) now reads the active policy at the LOCAL
// FRONTIER, not the block's own backdatable MomentumAcknowledged, so PublishRawTransaction itself
// rejects the stale-authority attempt -- it never reaches the pool, let alone the embedded
// receive. The registry is left untouched by the rejected attempt, proven here by asserting
// Active/Pending are unchanged, then that a change properly signed by the current active set
// (S_new) still succeeds afterwards (no regression).
func TestMultisig_ChangePolicy_StaleAuthorityAfterMaturity(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()
	activateMultisig(t, z)

	sOldA := genMultisigWalletSigner(t)
	signersOld := []ed25519.PublicKey{g.User1.Public, sOldA.pub}
	const nonce = 200
	const threshold = 2

	createTemplate := wallet.NewCreateMultisigTemplate(g.User1.Address, nonce, threshold, signersOld)
	z.InsertSendBlock(createTemplate, nil, mock.SkipVmChanges)
	z.InsertNewMomentum()
	z.InsertNewMomentum()

	multisigAddr := wallet.DeriveMultisigAddress(g.User1.Public, nonce)
	fuseQsrTo(t, z, multisigAddr, 300)

	supervisor := vm.NewSupervisor(z.Chain(), z.Consensus())
	ledgerApi := api.NewLedgerApi(z)

	// Rotate S_old -> S_new.
	sNewA := genMultisigWalletSigner(t)
	sNewB := genMultisigWalletSigner(t)
	signersNew := []ed25519.PublicKey{sNewA.pub, sNewB.pub}
	rotateTemplate := wallet.NewChangePolicyTemplate(multisigAddr, threshold, signersNew, false)
	rotateFrozen := freezeMultisigBlock(t, supervisor, rotateTemplate)
	wallet.AssembleMultisigAuth(rotateFrozen, [][]byte{
		wallet.SignMultisigBlock(rotateFrozen, g.User1),
		wallet.SignMultisigBlock(rotateFrozen, sOldA.pair),
	})
	common.FailIfErr(t, ledgerApi.PublishRawTransaction(&api.AccountBlock{AccountBlock: *rotateFrozen}))
	z.InsertNewMomentum()
	z.InsertNewMomentum()

	rec, err := definition.GetMultisigRecord(z.Chain().GetFrontierAccountStore(types.MultisigContract).Storage(), multisigAddr)
	common.FailIfErr(t, err)
	pendingHeight := rec.PendingHeight

	// Freeze and sign a ChangePolicy send NOW, still within the rotation's maturity window, capturing
	// its send-time Hash signed by S_old. Do not submit it yet.
	staleTemplate := wallet.NewChangePolicyTemplate(multisigAddr, threshold, signersNew, true)
	staleFrozen := freezeMultisigBlock(t, supervisor, staleTemplate)
	wallet.AssembleMultisigAuth(staleFrozen, [][]byte{
		wallet.SignMultisigBlock(staleFrozen, g.User1),
		wallet.SignMultisigBlock(staleFrozen, sOldA.pair),
	})

	// Advance past the rotation's maturity boundary before the stale send above is ever submitted.
	z.InsertMomentumsTo(pendingHeight + 60)

	// Submission itself is now rejected: the admission-time pre-filter reads the active policy at
	// the current local frontier, which has already matured to S_new, so S_old's signatures no
	// longer satisfy it.
	if err := ledgerApi.PublishRawTransaction(&api.AccountBlock{AccountBlock: *staleFrozen}); err == nil {
		t.Fatal("expected a stale-authority ChangePolicy send to be rejected at admission once the rotation has matured")
	}
	z.InsertNewMomentum()
	z.InsertNewMomentum()

	// The rejected attempt never reaches the pool or the embedded receive: the effective (promoted)
	// Active is still S_new, not locked, and there is no NEW pending change staged from the stale
	// attempt.
	frontierHeight := z.Chain().GetFrontierMomentumStore().Identifier().Height
	rec, err = definition.GetMultisigRecord(z.Chain().GetFrontierAccountStore(types.MultisigContract).Storage(), multisigAddr)
	common.FailIfErr(t, err)
	effective := definition.ActivePolicyAtHeight(rec, frontierHeight)
	if effective.Locked {
		t.Fatal("expected the stale (superseded S_old) lock attempt to have been rejected, but Active is Locked")
	}
	if len(effective.Signers) != len(signersNew) ||
		!containsPubKey(effective.Signers, sNewA.pub) || !containsPubKey(effective.Signers, sNewB.pub) {
		t.Fatalf("expected effective Active to be S_new, unaffected by the stale receive, got %+v", effective)
	}

	// Control: the same kind of change, signed by the CURRENT active set (S_new), is accepted.
	controlTemplate := wallet.NewChangePolicyTemplate(multisigAddr, threshold, signersOld, false)
	controlFrozen := freezeMultisigBlock(t, supervisor, controlTemplate)
	wallet.AssembleMultisigAuth(controlFrozen, [][]byte{
		wallet.SignMultisigBlock(controlFrozen, sNewA.pair),
		wallet.SignMultisigBlock(controlFrozen, sNewB.pair),
	})
	common.FailIfErr(t, ledgerApi.PublishRawTransaction(&api.AccountBlock{AccountBlock: *controlFrozen}))
	z.InsertNewMomentum()
	z.InsertNewMomentum()

	rec, err = definition.GetMultisigRecord(z.Chain().GetFrontierAccountStore(types.MultisigContract).Storage(), multisigAddr)
	common.FailIfErr(t, err)
	if rec.Pending == nil || len(rec.Pending.Signers) != len(signersOld) {
		t.Fatalf("expected the control change (signed by the current active set) to be staged, got %+v", rec)
	}
}

// --- Rotation landing between a send's freeze and its actual momentum inclusion -------------------

// TestMultisig_RotationBetweenSendAndInclusion_StillValid_Lands: a benign multisig send is
// threshold-signed under the account's original policy P0. Before it is ever submitted, P0 rotates
// to a SUPERSET P1 (same threshold, P0's signers still present) which matures. The send is then
// submitted -- content()'s live check at actual inclusion height authorises it against P1, which
// P0's signatures still satisfy, so the block lands normally: proof that live authorisation tracks
// whichever policy is genuinely active at inclusion, not a frozen snapshot from send time.
func TestMultisig_RotationBetweenSendAndInclusion_StillValid_Lands(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()
	activateMultisig(t, z)

	sA := genMultisigWalletSigner(t)
	signersP0 := []ed25519.PublicKey{g.User1.Public, sA.pub}
	const nonce = 400
	const threshold = 2

	createTemplate := wallet.NewCreateMultisigTemplate(g.User1.Address, nonce, threshold, signersP0)
	z.InsertSendBlock(createTemplate, nil, mock.SkipVmChanges)
	z.InsertNewMomentum()
	z.InsertNewMomentum()

	multisigAddr := wallet.DeriveMultisigAddress(g.User1.Public, nonce)
	fuseQsrTo(t, z, multisigAddr, 300)

	supervisor := vm.NewSupervisor(z.Chain(), z.Consensus())
	ledgerApi := api.NewLedgerApi(z)

	// Rotate P0 -> P1, a SUPERSET that still contains P0's signers at the same threshold.
	sB := genMultisigWalletSigner(t)
	signersP1 := []ed25519.PublicKey{g.User1.Public, sA.pub, sB.pub}
	rotateTemplate := wallet.NewChangePolicyTemplate(multisigAddr, threshold, signersP1, false)
	rotateFrozen := freezeMultisigBlock(t, supervisor, rotateTemplate)
	wallet.AssembleMultisigAuth(rotateFrozen, [][]byte{
		wallet.SignMultisigBlock(rotateFrozen, g.User1),
		wallet.SignMultisigBlock(rotateFrozen, sA.pair),
	})
	common.FailIfErr(t, ledgerApi.PublishRawTransaction(&api.AccountBlock{AccountBlock: *rotateFrozen}))
	z.InsertNewMomentum()
	z.InsertNewMomentum()

	rec, err := definition.GetMultisigRecord(z.Chain().GetFrontierAccountStore(types.MultisigContract).Storage(), multisigAddr)
	common.FailIfErr(t, err)
	pendingHeight := rec.PendingHeight

	// Freeze and sign a benign (non-ChangePolicy) send under the still-active P0, right after the
	// rotation has landed but well before it matures.
	sendTemplate := &nom.AccountBlock{
		Address:       multisigAddr,
		ToAddress:     g.User1.Address,
		BlockType:     nom.BlockTypeUserSend,
		Amount:        big.NewInt(0),
		TokenStandard: types.ZeroTokenStandard,
	}
	sendFrozen := freezeMultisigBlock(t, supervisor, sendTemplate)
	wallet.AssembleMultisigAuth(sendFrozen, [][]byte{
		wallet.SignMultisigBlock(sendFrozen, g.User1),
		wallet.SignMultisigBlock(sendFrozen, sA.pair),
	})

	// Let the rotation to P1 mature before the send is ever submitted.
	z.InsertMomentumsTo(pendingHeight + constants.MultisigPolicyMaturityDelay)

	// P0's signatures still satisfy P1 (a superset at the same threshold), so submission and
	// inclusion both succeed.
	common.FailIfErr(t, ledgerApi.PublishRawTransaction(&api.AccountBlock{AccountBlock: *sendFrozen}))
	z.InsertNewMomentum()
	z.InsertNewMomentum()

	frontier := z.Chain().GetFrontierAccountStore(multisigAddr).Identifier()
	if frontier.Height != 2 {
		t.Fatalf("expected the benign send to land as the account's second block, height = %v", frontier.Height)
	}
}

// TestMultisig_RotationBetweenSendAndInclusion_NoLongerValid_DoesNotHaltProduction: a benign
// multisig send is threshold-signed and admitted under the account's original policy P0, while a
// rotation to a DISJOINT policy P2 is still only pending. The send is submitted with just enough
// margin that it is accepted (P0 is still active at submission time), but the very next momentum
// produced afterwards is the one at which the rotation matures -- content()'s live check at that
// ACTUAL inclusion height would see P2 active, which P0's signatures do not satisfy. Proving the
// producer-side filter (pillar/worker_momentum.go, not content()) is what keeps the pipeline
// healthy: momentum production must keep advancing every tick, and the poisoned send must never be
// committed.
func TestMultisig_RotationBetweenSendAndInclusion_NoLongerValid_DoesNotHaltProduction(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()
	activateMultisig(t, z)

	sA := genMultisigWalletSigner(t)
	signersP0 := []ed25519.PublicKey{g.User1.Public, sA.pub}
	const nonce = 401
	const threshold = 2

	createTemplate := wallet.NewCreateMultisigTemplate(g.User1.Address, nonce, threshold, signersP0)
	z.InsertSendBlock(createTemplate, nil, mock.SkipVmChanges)
	z.InsertNewMomentum()
	z.InsertNewMomentum()

	multisigAddr := wallet.DeriveMultisigAddress(g.User1.Public, nonce)
	fuseQsrTo(t, z, multisigAddr, 300)

	supervisor := vm.NewSupervisor(z.Chain(), z.Consensus())
	ledgerApi := api.NewLedgerApi(z)

	// Rotate P0 -> P2, a DISJOINT signer set.
	sC := genMultisigWalletSigner(t)
	sD := genMultisigWalletSigner(t)
	signersP2 := []ed25519.PublicKey{sC.pub, sD.pub}
	rotateTemplate := wallet.NewChangePolicyTemplate(multisigAddr, threshold, signersP2, false)
	rotateFrozen := freezeMultisigBlock(t, supervisor, rotateTemplate)
	wallet.AssembleMultisigAuth(rotateFrozen, [][]byte{
		wallet.SignMultisigBlock(rotateFrozen, g.User1),
		wallet.SignMultisigBlock(rotateFrozen, sA.pair),
	})
	common.FailIfErr(t, ledgerApi.PublishRawTransaction(&api.AccountBlock{AccountBlock: *rotateFrozen}))
	z.InsertNewMomentum()
	z.InsertNewMomentum()

	rec, err := definition.GetMultisigRecord(z.Chain().GetFrontierAccountStore(types.MultisigContract).Storage(), multisigAddr)
	common.FailIfErr(t, err)
	pendingHeight := rec.PendingHeight

	// Freeze and sign a benign send under the still-active P0.
	sendTemplate := &nom.AccountBlock{
		Address:       multisigAddr,
		ToAddress:     g.User1.Address,
		BlockType:     nom.BlockTypeUserSend,
		Amount:        big.NewInt(0),
		TokenStandard: types.ZeroTokenStandard,
	}
	sendFrozen := freezeMultisigBlock(t, supervisor, sendTemplate)
	wallet.AssembleMultisigAuth(sendFrozen, [][]byte{
		wallet.SignMultisigBlock(sendFrozen, g.User1),
		wallet.SignMultisigBlock(sendFrozen, sA.pair),
	})

	// Advance to exactly one momentum short of maturity: P0 is still active, so the send is
	// admitted successfully.
	z.InsertMomentumsTo(pendingHeight + constants.MultisigPolicyMaturityDelay - 1)
	common.FailIfErr(t, ledgerApi.PublishRawTransaction(&api.AccountBlock{AccountBlock: *sendFrozen}))

	frontierBefore := z.Chain().GetFrontierMomentumStore().Identifier().Height

	// The very next momentum produced is the one at which the rotation matures: the pooled send no
	// longer satisfies the (now active) P2, but production must not halt.
	z.InsertNewMomentum()

	frontierAfter := z.Chain().GetFrontierMomentumStore().Identifier().Height
	if frontierAfter != frontierBefore+1 {
		t.Fatalf("expected momentum production to continue despite the now-unauthorised multisig block in the pool, frontier went from %v to %v", frontierBefore, frontierAfter)
	}

	assertNotCommitted := func() {
		t.Helper()
		block, err := z.Chain().GetFrontierMomentumStore().GetFrontierAccountBlock(multisigAddr)
		common.FailIfErr(t, err)
		if block != nil && block.Height >= sendFrozen.Height {
			t.Fatalf("expected the now-unauthorised multisig block to never be committed, got committed block at height %v", block.Height)
		}
	}
	assertNotCommitted()

	// Confirm production keeps working, and the block stays uncommitted, over further attempts.
	z.InsertNewMomentum()
	z.InsertNewMomentum()
	assertNotCommitted()
}
