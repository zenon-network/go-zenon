package implementation

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"math/big"
	"testing"

	"github.com/zenon-network/go-zenon/chain/account"
	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/chain/store"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/db"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/vm/constants"
	"github.com/zenon-network/go-zenon/vm/embedded/definition"
	"github.com/zenon-network/go-zenon/vm/vm_context"
)

// fakeMomentumStore implements only GetFrontierMomentum; every other store.Momentum method is
// unused by CreateMultisigMethod/ChangePolicyMethod and would panic if ever reached.
type fakeMomentumStore struct {
	store.Momentum
	height *uint64
}

func (f *fakeMomentumStore) GetFrontierMomentum() (*nom.Momentum, error) {
	return &nom.Momentum{Height: *f.height}, nil
}

// newMultisigTestContext builds a minimal in-memory AccountVmContext for the registry contract,
// with a frontier height that the test can mutate via the returned pointer (simulating momentum
// advancement across the maturity delay).
func newMultisigTestContext(height uint64) (vm_context.AccountVmContext, *uint64) {
	h := height
	ms := &fakeMomentumStore{height: &h}
	as := account.NewAccountStore(types.MultisigContract, db.NewMemDB())
	ctx := vm_context.NewAccountContext(ms, as, nil, nil)
	return ctx, &h
}

func genKey(t *testing.T) ed25519.PublicKey {
	t.Helper()
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	common.FailIfErr(t, err)
	return pub
}

func genSigners(t *testing.T, n int) []ed25519.PublicKey {
	t.Helper()
	out := make([]ed25519.PublicKey, n)
	for i := range out {
		out[i] = genKey(t)
	}
	return out
}

func toRawSigners(signers []ed25519.PublicKey) [][]byte {
	out := make([][]byte, len(signers))
	for i, s := range signers {
		out[i] = []byte(s)
	}
	return out
}

// keyPair is a signer with its private key, needed to produce the MultisigAuth Fix 7's
// receive-side re-authorisation now requires on every ChangePolicy send.
type keyPair struct {
	pub  ed25519.PublicKey
	priv ed25519.PrivateKey
}

func genKeyPair(t *testing.T) keyPair {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	common.FailIfErr(t, err)
	return keyPair{pub: pub, priv: priv}
}

func genKeyPairs(t *testing.T, n int) []keyPair {
	t.Helper()
	out := make([]keyPair, n)
	for i := range out {
		out[i] = genKeyPair(t)
	}
	return out
}

func pubsOf(keys []keyPair) []ed25519.PublicKey {
	out := make([]ed25519.PublicKey, len(keys))
	for i, k := range keys {
		out[i] = k.pub
	}
	return out
}

// signThreshold signs hash with exactly policy.Threshold keys whose public key is a member of
// policy.Signers (trial-match, mirroring the verifier/VerifyThresholdSignatures semantics - order
// does not matter).
func signThreshold(t *testing.T, policy definition.MultisigPolicy, keys []keyPair, hash types.Hash) [][]byte {
	t.Helper()
	sigs := make([][]byte, 0, policy.Threshold)
	for _, k := range keys {
		if len(sigs) == int(policy.Threshold) {
			break
		}
		for _, pk := range policy.Signers {
			if bytes.Equal(pk, k.pub) {
				sigs = append(sigs, ed25519.Sign(k.priv, hash.Bytes()))
				break
			}
		}
	}
	if len(sigs) != int(policy.Threshold) {
		t.Fatalf("not enough of the given keys are members of policy to reach threshold %d", policy.Threshold)
	}
	return sigs
}

func createMultisigSendBlock(creator types.Address, creatorPubKey ed25519.PublicKey, nonce uint64, threshold uint8, signers []ed25519.PublicKey) *nom.AccountBlock {
	data, err := definition.ABIMultisig.PackMethod(definition.CreateMultisigMethodName, nonce, threshold, toRawSigners(signers))
	if err != nil {
		panic(err)
	}
	return &nom.AccountBlock{
		Address:       creator,
		ToAddress:     types.MultisigContract,
		Amount:        constants.MultisigCreationBurnAmount,
		TokenStandard: types.ZnnTokenStandard,
		Data:          data,
		PublicKey:     creatorPubKey,
	}
}

// changePolicySendBlock builds a ChangePolicy send-block template WITHOUT MultisigAuth attached -
// used only by tests that exercise ValidateSendBlock (Fix 4) directly, which runs before
// ReceiveBlock's re-authorisation (Fix 7) is ever reached.
func changePolicySendBlock(multisigAddr types.Address, threshold uint8, signers []ed25519.PublicKey, lock bool) *nom.AccountBlock {
	data, err := definition.ABIMultisig.PackMethod(definition.ChangePolicyMethodName, threshold, toRawSigners(signers), lock)
	if err != nil {
		panic(err)
	}
	return &nom.AccountBlock{
		Address:   multisigAddr,
		ToAddress: types.MultisigContract,
		Amount:    big.NewInt(0),
		Data:      data,
	}
}

// signedChangePolicySendBlock builds a ChangePolicy send-block, computes its Hash (mirroring a
// frozen, PoW'd block), and attaches MultisigAuth threshold-signed by authKeys against authPolicy
// -- the policy ReceiveBlock is expected to re-authorise against (Fix 7).
func signedChangePolicySendBlock(t *testing.T, multisigAddr types.Address, threshold uint8, signers []ed25519.PublicKey, lock bool, authPolicy definition.MultisigPolicy, authKeys []keyPair) *nom.AccountBlock {
	t.Helper()
	block := changePolicySendBlock(multisigAddr, threshold, signers, lock)
	block.Hash = block.ComputeHash()
	block.MultisigAuth = &nom.MultisigAuth{Signatures: signThreshold(t, authPolicy, authKeys, block.Hash)}
	return block
}

// policiesEqual compares two policies as sets of signers (order-independent): a/b may come from
// either a freshly-built (insertion order) policy or one read back from storage (canonical,
// sorted order, via definition.ValidPolicy/CanonicalizeSigners).
func policiesEqual(a, b definition.MultisigPolicy) bool {
	if a.Threshold != b.Threshold || a.Locked != b.Locked || len(a.Signers) != len(b.Signers) {
		return false
	}
	for _, sa := range a.Signers {
		found := false
		for _, sb := range b.Signers {
			if bytes.Equal(sa, sb) {
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

// --- CreateMultisig ---

func TestCreateMultisig_HappyPath(t *testing.T) {
	ctx, _ := newMultisigTestContext(100)
	creator := types.PubKeyToAddress(genKey(t)) // unrelated funded account
	creatorPub := genKey(t)
	signers := append([]ed25519.PublicKey{creatorPub}, genSigners(t, 2)...)

	send := createMultisigSendBlock(creator, creatorPub, 7, 2, signers)
	method := &CreateMultisigMethod{MethodName: definition.CreateMultisigMethodName}

	blocks, err := method.ReceiveBlock(ctx, send)
	common.FailIfErr(t, err)
	if len(blocks) != 1 {
		t.Fatalf("expected exactly one generated block (the burn), got %v", blocks)
	}
	if blocks[0].ToAddress != types.TokenContract || blocks[0].Amount.Cmp(constants.MultisigCreationBurnAmount) != 0 {
		t.Fatalf("expected a burn descendant of MultisigCreationBurnAmount to TokenContract, got %+v", blocks[0])
	}

	derived := types.MultisigCreationToAddress(creatorPub, 7)
	rec, err := definition.GetMultisigRecord(ctx.Storage(), derived)
	common.FailIfErr(t, err)
	if rec == nil {
		t.Fatal("expected a record to be stored at the derived address")
	}
	if rec.Pending != nil {
		t.Fatal("expected no pending policy at create")
	}
	if rec.Active.Locked {
		t.Fatal("expected Locked == false at create")
	}
	if rec.Active.Threshold != 2 {
		t.Fatalf("expected threshold 2, got %d", rec.Active.Threshold)
	}
	if len(rec.Active.Signers) != 3 {
		t.Fatalf("expected 3 signers, got %d", len(rec.Active.Signers))
	}
}

func TestCreateMultisig_AddressDerivation(t *testing.T) {
	ctx, _ := newMultisigTestContext(100)
	creator := types.PubKeyToAddress(genKey(t))
	creatorPub := genKey(t)
	signers := append([]ed25519.PublicKey{creatorPub}, genSigners(t, 1)...)

	send := createMultisigSendBlock(creator, creatorPub, 42, 2, signers)
	method := &CreateMultisigMethod{MethodName: definition.CreateMultisigMethodName}
	_, err := method.ReceiveBlock(ctx, send)
	common.FailIfErr(t, err)

	derived := types.MultisigCreationToAddress(creatorPub, 42)
	rec, err := definition.GetMultisigRecord(ctx.Storage(), derived)
	common.FailIfErr(t, err)
	if rec == nil {
		t.Fatal("expected record at the derived address")
	}

	otherNonce := types.MultisigCreationToAddress(creatorPub, 43)
	other, err := definition.GetMultisigRecord(ctx.Storage(), otherNonce)
	common.FailIfErr(t, err)
	if other != nil {
		t.Fatal("did not expect a record at a different nonce's derived address")
	}
}

func TestCreateMultisig_AlreadyExists(t *testing.T) {
	ctx, _ := newMultisigTestContext(100)
	creator := types.PubKeyToAddress(genKey(t))
	creatorPub := genKey(t)
	signers := append([]ed25519.PublicKey{creatorPub}, genSigners(t, 1)...)

	method := &CreateMultisigMethod{MethodName: definition.CreateMultisigMethodName}
	send := createMultisigSendBlock(creator, creatorPub, 1, 2, signers)
	_, err := method.ReceiveBlock(ctx, send)
	common.FailIfErr(t, err)

	send2 := createMultisigSendBlock(creator, creatorPub, 1, 2, signers)
	_, err = method.ReceiveBlock(ctx, send2)
	common.ExpectError(t, err, constants.ErrMultisigAlreadyExists)
}

func TestCreateMultisig_CreatorNotInSigners(t *testing.T) {
	ctx, _ := newMultisigTestContext(100)
	creator := types.PubKeyToAddress(genKey(t))
	creatorPub := genKey(t)
	signers := genSigners(t, 2) // creatorPub deliberately not included

	method := &CreateMultisigMethod{MethodName: definition.CreateMultisigMethodName}
	send := createMultisigSendBlock(creator, creatorPub, 1, 2, signers)
	_, err := method.ReceiveBlock(ctx, send)
	common.ExpectError(t, err, constants.ErrMultisigInvalidPolicy)
}

func TestCreateMultisig_InvalidBounds(t *testing.T) {
	ctx, _ := newMultisigTestContext(100)
	creator := types.PubKeyToAddress(genKey(t))
	creatorPub := genKey(t)
	signers := []ed25519.PublicKey{creatorPub} // only 1 signer, below MinSigners

	method := &CreateMultisigMethod{MethodName: definition.CreateMultisigMethodName}
	send := createMultisigSendBlock(creator, creatorPub, 1, 2, signers)
	_, err := method.ReceiveBlock(ctx, send)
	common.ExpectError(t, err, constants.ErrMultisigInvalidPolicy)
}

// TestCreateMultisig_ValidateSendBlock_InvalidPolicy is Fix 4's stateless send-time policy check:
// a send with 17 signers (above MaxSigners=16), threshold 1 (below the >=2 floor), and duplicate
// signers must be rejected by ValidateSendBlock itself, before ever reaching ReceiveBlock.
func TestCreateMultisig_ValidateSendBlock_InvalidPolicy(t *testing.T) {
	creatorPub := genKey(t)
	dup := genKey(t)
	signers := append([]ed25519.PublicKey{creatorPub}, dup, dup) // duplicate signer, also only 3 (fine on count) but threshold 1 is invalid and dup makes it invalid regardless
	for len(signers) < 17 {
		signers = append(signers, genKey(t))
	}

	method := &CreateMultisigMethod{MethodName: definition.CreateMultisigMethodName}
	send := createMultisigSendBlock(types.PubKeyToAddress(creatorPub), creatorPub, 1, 1, signers)
	err := method.ValidateSendBlock(send)
	common.ExpectError(t, err, constants.ErrMultisigInvalidPolicy)
}

// TestCreateMultisig_ValidateSendBlock_WrongAmount is Fix 9: the send must burn exactly
// MultisigCreationBurnAmount of ZNN.
func TestCreateMultisig_ValidateSendBlock_WrongAmount(t *testing.T) {
	creatorPub := genKey(t)
	signers := append([]ed25519.PublicKey{creatorPub}, genSigners(t, 2)...)
	method := &CreateMultisigMethod{MethodName: definition.CreateMultisigMethodName}

	zeroAmount := createMultisigSendBlock(types.PubKeyToAddress(creatorPub), creatorPub, 1, 2, signers)
	zeroAmount.Amount = big.NewInt(0)
	err := method.ValidateSendBlock(zeroAmount)
	common.ExpectError(t, err, constants.ErrInvalidTokenOrAmount)

	wrongToken := createMultisigSendBlock(types.PubKeyToAddress(creatorPub), creatorPub, 1, 2, signers)
	wrongToken.TokenStandard = types.QsrTokenStandard
	err = method.ValidateSendBlock(wrongToken)
	common.ExpectError(t, err, constants.ErrInvalidTokenOrAmount)
}

// TestCreateMultisig_CreatorIsMultisig: a multisig account can never carry a PublicKey (the
// verifier requires it zero-length for multisig senders), so CreateMultisig rejects a multisig
// sender explicitly rather than falling through to a misleading ErrMultisigInvalidPolicy.
func TestCreateMultisig_CreatorIsMultisig(t *testing.T) {
	creatorPub := genKey(t)
	signers := append([]ed25519.PublicKey{creatorPub}, genSigners(t, 2)...)
	multisigCreator := types.MultisigCreationToAddress(genKey(t), 1) // an existing multisig account

	method := &CreateMultisigMethod{MethodName: definition.CreateMultisigMethodName}
	send := createMultisigSendBlock(multisigCreator, nil, 7, 2, signers)
	err := method.ValidateSendBlock(send)
	common.ExpectError(t, err, constants.ErrMultisigCreatorMustBeSingleSig)
}

// TestChangePolicy_ValidateSendBlock_InvalidPolicy is Fix 4's stateless send-time policy check for
// ChangePolicy: an out-of-bounds proposed policy is rejected by ValidateSendBlock itself.
func TestChangePolicy_ValidateSendBlock_InvalidPolicy(t *testing.T) {
	addr := types.MultisigCreationToAddress(genKey(t), 1)
	method := &ChangePolicyMethod{MethodName: definition.ChangePolicyMethodName}

	// threshold 1 is below the >=2 floor.
	send := changePolicySendBlock(addr, 1, genSigners(t, 2), false)
	err := method.ValidateSendBlock(send)
	common.ExpectError(t, err, constants.ErrMultisigInvalidPolicy)
}

// --- ChangePolicy ---

func TestChangePolicy_HappyPath(t *testing.T) {
	ctx, height := newMultisigTestContext(100)
	p0Keys := genKeyPairs(t, 2)
	p0 := definition.MultisigPolicy{Threshold: 2, Signers: pubsOf(p0Keys)}
	addr := types.MultisigCreationToAddress(p0Keys[0].pub, 1)
	common.FailIfErr(t, definition.SaveMultisigRecord(ctx.Storage(), addr, &definition.MultisigRecord{Active: p0}))

	*height = 100
	p1Signers := genSigners(t, 3)
	send := signedChangePolicySendBlock(t, addr, 2, p1Signers, false, p0, p0Keys)
	method := &ChangePolicyMethod{MethodName: definition.ChangePolicyMethodName}
	_, err := method.ReceiveBlock(ctx, send)
	common.FailIfErr(t, err)

	rec, err := definition.GetMultisigRecord(ctx.Storage(), addr)
	common.FailIfErr(t, err)
	if rec.Pending == nil {
		t.Fatal("expected a pending change to be staged")
	}
	if rec.PendingHeight != 100 {
		t.Fatalf("expected PendingHeight == frontier height (100), got %d", rec.PendingHeight)
	}
}

func TestChangePolicy_NoPolicy(t *testing.T) {
	ctx, _ := newMultisigTestContext(100)
	addr := types.MultisigCreationToAddress(genKey(t), 1)
	send := changePolicySendBlock(addr, 2, genSigners(t, 2), false)
	method := &ChangePolicyMethod{MethodName: definition.ChangePolicyMethodName}
	_, err := method.ReceiveBlock(ctx, send)
	common.ExpectError(t, err, constants.ErrMultisigNoPolicy)
}

func TestChangePolicy_Locked(t *testing.T) {
	ctx, _ := newMultisigTestContext(100)
	p0Keys := genKeyPairs(t, 2)
	addr := types.MultisigCreationToAddress(p0Keys[0].pub, 1)
	p0 := definition.MultisigPolicy{Threshold: 2, Signers: pubsOf(p0Keys), Locked: true}
	common.FailIfErr(t, definition.SaveMultisigRecord(ctx.Storage(), addr, &definition.MultisigRecord{Active: p0}))

	send := signedChangePolicySendBlock(t, addr, 2, genSigners(t, 2), false, p0, p0Keys)
	method := &ChangePolicyMethod{MethodName: definition.ChangePolicyMethodName}
	_, err := method.ReceiveBlock(ctx, send)
	common.ExpectError(t, err, constants.ErrMultisigLocked)
}

// TestChangePolicy_MultisigAuthMissing: a ChangePolicy send with no MultisigAuth attached at all
// must be rejected at receive time -- there is nothing to re-authorise against (Fix 7).
func TestChangePolicy_MultisigAuthMissing(t *testing.T) {
	ctx, _ := newMultisigTestContext(100)
	p0Keys := genKeyPairs(t, 2)
	addr := types.MultisigCreationToAddress(p0Keys[0].pub, 1)
	p0 := definition.MultisigPolicy{Threshold: 2, Signers: pubsOf(p0Keys)}
	common.FailIfErr(t, definition.SaveMultisigRecord(ctx.Storage(), addr, &definition.MultisigRecord{Active: p0}))

	send := changePolicySendBlock(addr, 2, genSigners(t, 2), false)
	send.Hash = send.ComputeHash()
	// deliberately no MultisigAuth attached
	method := &ChangePolicyMethod{MethodName: definition.ChangePolicyMethodName}
	_, err := method.ReceiveBlock(ctx, send)
	common.ExpectError(t, err, constants.ErrMultisigStaleAuthority)
}

// TestChangePolicy_StaleAuthorityAfterMaturity is the unit-level equivalent of
// TestMultisig_ChangePolicy_StaleAuthorityAfterMaturity (vm/embedded/tests/multisig_conformance_test.go):
// a send threshold-signed by the OLD active set, received after a rotation to a new set has
// matured, must be rejected -- the old set no longer has authority to mutate (or lock) the
// account, even though its signatures are individually well-formed.
func TestChangePolicy_StaleAuthorityAfterMaturity(t *testing.T) {
	ctx, height := newMultisigTestContext(0)
	oldKeys := genKeyPairs(t, 2)
	pOld := definition.MultisigPolicy{Threshold: 2, Signers: pubsOf(oldKeys)}
	addr := types.MultisigCreationToAddress(oldKeys[0].pub, 1)
	common.FailIfErr(t, definition.SaveMultisigRecord(ctx.Storage(), addr, &definition.MultisigRecord{Active: pOld}))

	method := &ChangePolicyMethod{MethodName: definition.ChangePolicyMethodName}

	// Rotate S_old -> S_new, staged at height 0.
	newKeys := genKeyPairs(t, 2)
	pNew := definition.MultisigPolicy{Threshold: 2, Signers: pubsOf(newKeys)}
	*height = 0
	_, err := method.ReceiveBlock(ctx, signedChangePolicySendBlock(t, addr, pNew.Threshold, pNew.Signers, false, pOld, oldKeys))
	common.FailIfErr(t, err)

	// Advance past maturity (PendingHeight(0) + 60), then submit a ChangePolicy send authorised
	// (signed) by the now-superseded S_old.
	*height = constants.MultisigPolicyMaturityDelay
	stale := signedChangePolicySendBlock(t, addr, 2, genSigners(t, 2), true, pOld, oldKeys)
	_, err = method.ReceiveBlock(ctx, stale)
	common.ExpectError(t, err, constants.ErrMultisigStaleAuthority)

	// Control: the same kind of change, signed by the CURRENT (matured) active set, succeeds.
	control := signedChangePolicySendBlock(t, addr, 2, genSigners(t, 3), false, pNew, newKeys)
	_, err = method.ReceiveBlock(ctx, control)
	common.FailIfErr(t, err)

	rec, err := definition.GetMultisigRecord(ctx.Storage(), addr)
	common.FailIfErr(t, err)
	if rec.Active.Locked {
		t.Fatal("expected the stale (superseded S_old) lock attempt to have been rejected")
	}
	if !policiesEqual(rec.Active, pNew) {
		t.Fatalf("expected Active == S_new (matured), got %+v", rec.Active)
	}
}

func TestChangePolicy_InvalidNextPolicy(t *testing.T) {
	ctx, _ := newMultisigTestContext(100)
	creatorPub := genKey(t)
	addr := types.MultisigCreationToAddress(creatorPub, 1)
	p0 := definition.MultisigPolicy{Threshold: 2, Signers: append([]ed25519.PublicKey{creatorPub}, genSigners(t, 1)...)}
	common.FailIfErr(t, definition.SaveMultisigRecord(ctx.Storage(), addr, &definition.MultisigRecord{Active: p0}))

	// threshold 1 is below MinSigners' implied floor (threshold must be >= 2)
	send := changePolicySendBlock(addr, 1, genSigners(t, 2), false)
	method := &ChangePolicyMethod{MethodName: definition.ChangePolicyMethodName}
	_, err := method.ReceiveBlock(ctx, send)
	common.ExpectError(t, err, constants.ErrMultisigInvalidPolicy)
}

func TestChangePolicy_SecondCallWhilePendingReplacesPending(t *testing.T) {
	ctx, height := newMultisigTestContext(100)
	p0Keys := genKeyPairs(t, 2)
	addr := types.MultisigCreationToAddress(p0Keys[0].pub, 1)
	p0 := definition.MultisigPolicy{Threshold: 2, Signers: pubsOf(p0Keys)}
	common.FailIfErr(t, definition.SaveMultisigRecord(ctx.Storage(), addr, &definition.MultisigRecord{Active: p0}))

	method := &ChangePolicyMethod{MethodName: definition.ChangePolicyMethodName}

	*height = 100
	p1Signers := genSigners(t, 3)
	_, err := method.ReceiveBlock(ctx, signedChangePolicySendBlock(t, addr, 2, p1Signers, false, p0, p0Keys))
	common.FailIfErr(t, err)

	// still well within the maturity window: Active is still P0 (unmatured), so the second call
	// must also be authorised by P0.
	*height = 110
	p1bSigners := genSigners(t, 4)
	_, err = method.ReceiveBlock(ctx, signedChangePolicySendBlock(t, addr, 3, p1bSigners, false, p0, p0Keys))
	common.FailIfErr(t, err)

	rec, err := definition.GetMultisigRecord(ctx.Storage(), addr)
	common.FailIfErr(t, err)
	if !policiesEqual(rec.Active, p0) {
		t.Fatal("Active must still be P0 while the change is unmatured")
	}
	if rec.Pending == nil {
		t.Fatal("expected a pending change")
	}
	if rec.Pending.Threshold != 3 || len(rec.Pending.Signers) != 4 {
		t.Fatalf("expected the unmatured pending to be replaced by the second call, got threshold=%d signers=%d", rec.Pending.Threshold, len(rec.Pending.Signers))
	}
	if rec.PendingHeight != 110 {
		t.Fatalf("expected PendingHeight to reset to the second call's height (110), got %d", rec.PendingHeight)
	}
}

// TestChangePolicy_ThreeChangesAcrossMaturityBoundary is the load-bearing test for
// ChangePolicy's materialise-before-stage behaviour: create P0; ChangePolicy->P1 staged; advance
// past maturity; ChangePolicy->P2 (assert Active==P1 at that point, NOT P0); advance past
// maturity again; ChangePolicy->P3 (assert Active==P2). At no point does Active ever revert to
// an older policy.
func TestChangePolicy_ThreeChangesAcrossMaturityBoundary(t *testing.T) {
	ctx, height := newMultisigTestContext(0)
	p0Keys := genKeyPairs(t, 2)
	addr := types.MultisigCreationToAddress(p0Keys[0].pub, 1)

	p0 := definition.MultisigPolicy{Threshold: 2, Signers: pubsOf(p0Keys)}
	common.FailIfErr(t, definition.SaveMultisigRecord(ctx.Storage(), addr, &definition.MultisigRecord{Active: p0}))

	method := &ChangePolicyMethod{MethodName: definition.ChangePolicyMethodName}

	// ChangePolicy -> P1, staged at height 0, authorised by P0.
	*height = 0
	p1Keys := genKeyPairs(t, 3)
	p1 := definition.MultisigPolicy{Threshold: 2, Signers: pubsOf(p1Keys)}
	_, err := method.ReceiveBlock(ctx, signedChangePolicySendBlock(t, addr, p1.Threshold, p1.Signers, false, p0, p0Keys))
	common.FailIfErr(t, err)

	rec, err := definition.GetMultisigRecord(ctx.Storage(), addr)
	common.FailIfErr(t, err)
	if !policiesEqual(rec.Active, p0) {
		t.Fatal("Active should still be P0 immediately after staging P1")
	}
	if rec.PendingHeight != 0 {
		t.Fatalf("expected PendingHeight 0, got %d", rec.PendingHeight)
	}

	// Advance past P1's maturity (PendingHeight(0) + 60). ChangePolicy -> P2: must materialise P1
	// into Active BEFORE staging P2, never reverting to P0 -- so this call must be authorised by
	// the now-active P1.
	*height = 60
	p2Keys := genKeyPairs(t, 4)
	p2 := definition.MultisigPolicy{Threshold: 3, Signers: pubsOf(p2Keys)}
	_, err = method.ReceiveBlock(ctx, signedChangePolicySendBlock(t, addr, p2.Threshold, p2.Signers, false, p1, p1Keys))
	common.FailIfErr(t, err)

	rec, err = definition.GetMultisigRecord(ctx.Storage(), addr)
	common.FailIfErr(t, err)
	if !policiesEqual(rec.Active, p1) {
		t.Fatalf("expected Active == P1 (matured) after staging P2, not P0 and not still-pending")
	}
	if rec.Pending == nil || rec.Pending.Threshold != p2.Threshold || len(rec.Pending.Signers) != len(p2.Signers) {
		t.Fatal("expected P2 to be staged as Pending")
	}
	if rec.PendingHeight != 60 {
		t.Fatalf("expected PendingHeight 60, got %d", rec.PendingHeight)
	}

	// Advance past P2's maturity (PendingHeight(60) + 60 = 120). ChangePolicy -> P3: Active must
	// now be P2, never reverting to P0 or P1 -- authorised by the now-active P2.
	*height = 120
	p3Signers := genSigners(t, 2)
	_, err = method.ReceiveBlock(ctx, signedChangePolicySendBlock(t, addr, 2, p3Signers, false, p2, p2Keys))
	common.FailIfErr(t, err)

	rec, err = definition.GetMultisigRecord(ctx.Storage(), addr)
	common.FailIfErr(t, err)
	if !policiesEqual(rec.Active, p2) {
		t.Fatal("expected Active == P2 (matured) after staging P3, never reverting to an older policy")
	}
	if rec.Pending == nil || rec.Pending.Threshold != 2 || len(rec.Pending.Signers) != len(p3Signers) {
		t.Fatal("expected P3 to be staged as Pending")
	}
	if rec.PendingHeight != 120 {
		t.Fatalf("expected PendingHeight 120, got %d", rec.PendingHeight)
	}
}

// TestChangePolicy_LockMaturesAndBlocksSubsequentChange covers the full lock lifecycle through
// maturity, distinct from TestChangePolicy_Locked (which presets Active.Locked = true directly,
// never exercising the maturity path at all). Here the lock is staged like any other change (it
// is NOT immediately effective) and only rejects a later ChangePolicy once Promote() has
// materialised it into Active at receive time. This is also a receive-side re-authorisation
// case: the second ChangePolicy call is built exactly like any ordinary call against an Active
// policy that is not (yet) locked -- ReceiveBlock's re-authorisation step (b2) is what catches it,
// based on the effective Active AT RECEIVE TIME, regardless of what the caller believed was
// active when the call was made.
//
// This is also Fix 5's verification test: the second call is rejected BEFORE reaching the final
// save (step e), so the intermediate write Fix 5 removes is never taken either way -- the
// promoted (locked) Active is only ever observable by re-deriving it (ActivePolicyAtHeight /
// GetMultisigRecord + Promote), never via the raw, unpromoted storage record, proving that
// removing the dead intermediate write does not change observable state.
func TestChangePolicy_LockMaturesAndBlocksSubsequentChange(t *testing.T) {
	ctx, height := newMultisigTestContext(0)
	p0Keys := genKeyPairs(t, 2)
	addr := types.MultisigCreationToAddress(p0Keys[0].pub, 1)
	p0 := definition.MultisigPolicy{Threshold: 2, Signers: pubsOf(p0Keys)}
	common.FailIfErr(t, definition.SaveMultisigRecord(ctx.Storage(), addr, &definition.MultisigRecord{Active: p0}))

	method := &ChangePolicyMethod{MethodName: definition.ChangePolicyMethodName}

	// Stage a lock=true change. It is NOT yet effective: Active must remain unlocked.
	*height = 0
	lockKeys := genKeyPairs(t, 2)
	lockPolicySigners := pubsOf(lockKeys)
	_, err := method.ReceiveBlock(ctx, signedChangePolicySendBlock(t, addr, 2, lockPolicySigners, true, p0, p0Keys))
	common.FailIfErr(t, err)

	rec, err := definition.GetMultisigRecord(ctx.Storage(), addr)
	common.FailIfErr(t, err)
	if rec.Active.Locked {
		t.Fatal("Active must not be Locked yet: the lock change is still pending, unmatured")
	}
	if rec.Pending == nil || !rec.Pending.Locked {
		t.Fatal("expected the lock to be staged as Pending")
	}
	pendingHeight := rec.PendingHeight

	// Advance to exactly the lock's maturity boundary, then attempt an ordinary ChangePolicy,
	// authorised by the (now-matured) locked policy's signers: the lock must materialise into
	// Active BEFORE the Locked check, and the call rejects.
	*height = constants.MultisigPolicyMaturityDelay
	lockedPolicy := definition.MultisigPolicy{Threshold: 2, Signers: lockPolicySigners, Locked: true}
	_, err = method.ReceiveBlock(ctx, signedChangePolicySendBlock(t, addr, 2, genSigners(t, 3), false, lockedPolicy, lockKeys))
	common.ExpectError(t, err, constants.ErrMultisigLocked)

	// The rejected call never reached the final save (step e), so the raw storage record still
	// carries the original, unpromoted Pending -- that is a storage-representation detail, not an
	// observable state change. The promoted Active is only visible by re-deriving it.
	rec, err = definition.GetMultisigRecord(ctx.Storage(), addr)
	common.FailIfErr(t, err)
	effective := definition.ActivePolicyAtHeight(rec, constants.MultisigPolicyMaturityDelay)
	if !effective.Locked {
		t.Fatal("expected the lock to have matured into the effective Active")
	}
	if rec.PendingHeight != pendingHeight {
		t.Fatalf("expected PendingHeight to be unchanged by the rejected call, got %d want %d", rec.PendingHeight, pendingHeight)
	}
}
