package verifier

import (
	"crypto/ed25519"
	"crypto/rand"
	"testing"
	"time"

	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/chain/store"
	"github.com/zenon-network/go-zenon/common/db"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/vm/constants"
	"github.com/zenon-network/go-zenon/vm/embedded/definition"
)

// --- minimal fakes for store.Momentum / store.Account ---------------------
//
// Both interfaces embed a nil instance of themselves so unimplemented methods
// panic loudly (nil pointer) if a code path under test reaches them
// unexpectedly, rather than silently behaving like a zero value.

type fakeAccountStore struct {
	store.Account
	storage    db.DB
	byHeight   map[uint64]*nom.AccountBlock
	identifier types.HashHeight
}

func (f *fakeAccountStore) Storage() db.DB {
	return f.storage
}
func (f *fakeAccountStore) ByHeight(height uint64) (*nom.AccountBlock, error) {
	return f.byHeight[height], nil
}
func (f *fakeAccountStore) Identifier() types.HashHeight {
	return f.identifier
}

type fakeMomentumStore struct {
	store.Momentum
	identifier       types.HashHeight
	frontierMomentum *nom.Momentum
	registryStorage  db.DB
	sporkActive      bool
}

func (f *fakeMomentumStore) Identifier() types.HashHeight {
	return f.identifier
}
func (f *fakeMomentumStore) GetFrontierMomentum() (*nom.Momentum, error) {
	return f.frontierMomentum, nil
}
func (f *fakeMomentumStore) GetAccountStore(address types.Address) store.Account {
	if address == types.MultisigContract {
		return &fakeAccountStore{storage: f.registryStorage}
	}
	return &fakeAccountStore{storage: db.DisableNotFound(db.NewMemDB())}
}
func (f *fakeMomentumStore) IsSporkActive(*types.ImplementedSpork) (bool, error) {
	return f.sporkActive, nil
}

// frontierOnlyStore is used for abv.frontierStore: only Identifier() is needed by the recency
// floor check exercised in these tests.
type frontierOnlyStore struct {
	store.Momentum
	identifier types.HashHeight
}

func (f *frontierOnlyStore) Identifier() types.HashHeight {
	return f.identifier
}

// --- helpers ----------------------------------------------------------------

func newMultisigAddress(t *testing.T) types.Address {
	t.Helper()
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return types.MultisigCreationToAddress(pub, 0)
}

type signerKey struct {
	pub  ed25519.PublicKey
	priv ed25519.PrivateKey
}

func generateSigners(t *testing.T, n int) []signerKey {
	t.Helper()
	out := make([]signerKey, n)
	for i := 0; i < n; i++ {
		pub, priv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		out[i] = signerKey{pub: pub, priv: priv}
	}
	return out
}

func policyFromSigners(t *testing.T, threshold uint8, signers []signerKey) definition.MultisigPolicy {
	t.Helper()
	pubs := make([]ed25519.PublicKey, len(signers))
	for i, s := range signers {
		pubs[i] = s.pub
	}
	p := definition.MultisigPolicy{Threshold: threshold, Signers: pubs}
	if err := definition.ValidPolicy(&p); err != nil {
		t.Fatal(err)
	}
	return p
}

// signFor returns a signature from the signer whose pubkey matches pub, given the test's signer
// pool.
func signFor(t *testing.T, signers []signerKey, pub ed25519.PublicKey, hash types.Hash) []byte {
	t.Helper()
	for _, s := range signers {
		if string(s.pub) == string(pub) {
			return ed25519.Sign(s.priv, hash.Bytes())
		}
	}
	t.Fatalf("no matching signer for pubkey")
	return nil
}

// signWithPolicy signs block.Hash with exactly policy.Threshold distinct active signers (in
// policy order), taken from the signers pool that produced the policy.
func signWithPolicy(t *testing.T, policy definition.MultisigPolicy, signers []signerKey, hash types.Hash) [][]byte {
	t.Helper()
	sigs := make([][]byte, 0, policy.Threshold)
	for i := 0; i < int(policy.Threshold); i++ {
		sigs = append(sigs, signFor(t, signers, policy.Signers[i], hash))
	}
	return sigs
}

func testMomentum(height uint64) *nom.Momentum {
	ts := time.Unix(1000+int64(height), 0)
	return &nom.Momentum{
		Height:    height,
		Hash:      types.NewHash([]byte{byte(height), byte(height >> 8)}),
		Timestamp: &ts,
	}
}

func maOf(m *nom.Momentum) types.HashHeight {
	return m.Identifier()
}

func newSendBlock(address types.Address, height uint64, ma types.HashHeight) *nom.AccountBlock {
	b := &nom.AccountBlock{
		Version:              1,
		ChainIdentifier:      1,
		BlockType:            nom.BlockTypeUserSend,
		Height:               height,
		MomentumAcknowledged: ma,
		Address:              address,
		ToAddress:            types.ZeroAddress,
	}
	if height > 1 {
		b.PreviousHash = types.NewHash([]byte("prev"))
	}
	b.Hash = b.ComputeHash()
	return b
}

// --- signature() tests -------------------------------------------------------

func TestMultisigSignature_NotActivated(t *testing.T) {
	addr := newMultisigAddress(t)
	frontier := testMomentum(10)
	block := newSendBlock(addr, 1, maOf(frontier))

	momentumStore := &fakeMomentumStore{
		identifier:       maOf(frontier),
		frontierMomentum: frontier,
		registryStorage:  db.DisableNotFound(db.NewMemDB()),
		sporkActive:      false,
	}

	abvt := &accountBlockTransactionVerifier{
		transaction:      &nom.AccountBlockTransaction{Block: block},
		momentumStore:    momentumStore,
		isMultisigActive: false,
	}
	if err := abvt.signature(); err != constants.ErrMultisigNotActivated {
		t.Fatalf("expected ErrMultisigNotActivated, got %v", err)
	}
}

func TestMultisigSignature_PublicKeyMustBeZero(t *testing.T) {
	addr := newMultisigAddress(t)
	frontier := testMomentum(10)
	block := newSendBlock(addr, 1, maOf(frontier))
	block.PublicKey = []byte{1, 2, 3}

	abvt := &accountBlockTransactionVerifier{
		transaction:      &nom.AccountBlockTransaction{Block: block},
		isMultisigActive: true,
	}
	if err := abvt.signature(); err != ErrABPublicKeyMustBeZero {
		t.Fatalf("expected ErrABPublicKeyMustBeZero, got %v", err)
	}
}

func TestMultisigSignature_SignatureMustBeZero(t *testing.T) {
	addr := newMultisigAddress(t)
	frontier := testMomentum(10)
	block := newSendBlock(addr, 1, maOf(frontier))
	block.Signature = []byte{1, 2, 3}

	abvt := &accountBlockTransactionVerifier{
		transaction:      &nom.AccountBlockTransaction{Block: block},
		isMultisigActive: true,
	}
	if err := abvt.signature(); err != ErrABSignatureMustBeZero {
		t.Fatalf("expected ErrABSignatureMustBeZero, got %v", err)
	}
}

func TestMultisigSignature_AuthMissing(t *testing.T) {
	addr := newMultisigAddress(t)
	frontier := testMomentum(10)
	block := newSendBlock(addr, 1, maOf(frontier))

	abvt := &accountBlockTransactionVerifier{
		transaction:      &nom.AccountBlockTransaction{Block: block},
		isMultisigActive: true,
	}
	if err := abvt.signature(); err != ErrABMultisigAuthMissing {
		t.Fatalf("expected ErrABMultisigAuthMissing, got %v", err)
	}
}

func TestMultisigSignature_NoPolicy(t *testing.T) {
	addr := newMultisigAddress(t)
	frontier := testMomentum(10)
	block := newSendBlock(addr, 1, maOf(frontier))
	block.MultisigAuth = &nom.MultisigAuth{Signatures: [][]byte{{1, 2, 3}}}

	momentumStore := &fakeMomentumStore{
		identifier:       maOf(frontier),
		frontierMomentum: frontier,
		registryStorage:  db.DisableNotFound(db.NewMemDB()), // no record saved
		sporkActive:      true,
	}

	abvt := &accountBlockTransactionVerifier{
		transaction:      &nom.AccountBlockTransaction{Block: block},
		momentumStore:    momentumStore,
		frontierStore:    momentumStore,
		isMultisigActive: true,
	}
	if err := abvt.signature(); err != constants.ErrMultisigNoPolicy {
		t.Fatalf("expected ErrMultisigNoPolicy, got %v", err)
	}
}

func setupRegistry(t *testing.T, addr types.Address, rec *definition.MultisigRecord) db.DB {
	t.Helper()
	storage := db.DisableNotFound(db.NewMemDB())
	if err := definition.SaveMultisigRecord(storage, addr, rec); err != nil {
		t.Fatal(err)
	}
	return storage
}

func TestMultisigSignature_ValidThreshold(t *testing.T) {
	addr := newMultisigAddress(t)
	frontier := testMomentum(10)
	signers := generateSigners(t, 3)
	policy := policyFromSigners(t, 2, signers)
	storage := setupRegistry(t, addr, &definition.MultisigRecord{Active: policy})

	block := newSendBlock(addr, 1, maOf(frontier))
	block.MultisigAuth = &nom.MultisigAuth{Signatures: signWithPolicy(t, policy, signers, block.Hash)}

	momentumStore := &fakeMomentumStore{
		identifier:       maOf(frontier),
		frontierMomentum: frontier,
		registryStorage:  storage,
		sporkActive:      true,
	}
	abvt := &accountBlockTransactionVerifier{
		transaction:      &nom.AccountBlockTransaction{Block: block},
		momentumStore:    momentumStore,
		frontierStore:    momentumStore,
		isMultisigActive: true,
	}
	if err := abvt.signature(); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
}

func TestMultisigSignature_WrongCount(t *testing.T) {
	addr := newMultisigAddress(t)
	frontier := testMomentum(10)
	signers := generateSigners(t, 3)
	policy := policyFromSigners(t, 2, signers)
	storage := setupRegistry(t, addr, &definition.MultisigRecord{Active: policy})

	block := newSendBlock(addr, 1, maOf(frontier))
	// only one signature, threshold is 2
	block.MultisigAuth = &nom.MultisigAuth{Signatures: [][]byte{signFor(t, signers, policy.Signers[0], block.Hash)}}

	momentumStore := &fakeMomentumStore{
		identifier:       maOf(frontier),
		frontierMomentum: frontier,
		registryStorage:  storage,
		sporkActive:      true,
	}
	abvt := &accountBlockTransactionVerifier{
		transaction:      &nom.AccountBlockTransaction{Block: block},
		momentumStore:    momentumStore,
		frontierStore:    momentumStore,
		isMultisigActive: true,
	}
	if err := abvt.signature(); err != constants.ErrMultisigThresholdNotMet {
		t.Fatalf("expected ErrMultisigThresholdNotMet, got %v", err)
	}
}

func TestMultisigSignature_JunkSignature(t *testing.T) {
	addr := newMultisigAddress(t)
	frontier := testMomentum(10)
	signers := generateSigners(t, 3)
	policy := policyFromSigners(t, 2, signers)
	storage := setupRegistry(t, addr, &definition.MultisigRecord{Active: policy})

	block := newSendBlock(addr, 1, maOf(frontier))
	good := signFor(t, signers, policy.Signers[0], block.Hash)
	junk := make([]byte, ed25519.SignatureSize)
	block.MultisigAuth = &nom.MultisigAuth{Signatures: [][]byte{good, junk}}

	momentumStore := &fakeMomentumStore{
		identifier:       maOf(frontier),
		frontierMomentum: frontier,
		registryStorage:  storage,
		sporkActive:      true,
	}
	abvt := &accountBlockTransactionVerifier{
		transaction:      &nom.AccountBlockTransaction{Block: block},
		momentumStore:    momentumStore,
		frontierStore:    momentumStore,
		isMultisigActive: true,
	}
	if err := abvt.signature(); err != ErrABSignatureInvalid {
		t.Fatalf("expected ErrABSignatureInvalid, got %v", err)
	}
}

func TestMultisigSignature_NonMemberSignature(t *testing.T) {
	addr := newMultisigAddress(t)
	frontier := testMomentum(10)
	signers := generateSigners(t, 3)
	policy := policyFromSigners(t, 2, signers)
	storage := setupRegistry(t, addr, &definition.MultisigRecord{Active: policy})
	outsider := generateSigners(t, 1)

	block := newSendBlock(addr, 1, maOf(frontier))
	good := signFor(t, signers, policy.Signers[0], block.Hash)
	notMember := ed25519.Sign(outsider[0].priv, block.Hash.Bytes())
	block.MultisigAuth = &nom.MultisigAuth{Signatures: [][]byte{good, notMember}}

	momentumStore := &fakeMomentumStore{
		identifier:       maOf(frontier),
		frontierMomentum: frontier,
		registryStorage:  storage,
		sporkActive:      true,
	}
	abvt := &accountBlockTransactionVerifier{
		transaction:      &nom.AccountBlockTransaction{Block: block},
		momentumStore:    momentumStore,
		frontierStore:    momentumStore,
		isMultisigActive: true,
	}
	if err := abvt.signature(); err != ErrABSignatureInvalid {
		t.Fatalf("expected ErrABSignatureInvalid, got %v", err)
	}
}

func TestMultisigSignature_DuplicateSignature(t *testing.T) {
	addr := newMultisigAddress(t)
	frontier := testMomentum(10)
	signers := generateSigners(t, 3)
	policy := policyFromSigners(t, 2, signers)
	storage := setupRegistry(t, addr, &definition.MultisigRecord{Active: policy})

	block := newSendBlock(addr, 1, maOf(frontier))
	good := signFor(t, signers, policy.Signers[0], block.Hash)
	// same signer twice: second use should fail (no distinct signer left to match)
	block.MultisigAuth = &nom.MultisigAuth{Signatures: [][]byte{good, good}}

	momentumStore := &fakeMomentumStore{
		identifier:       maOf(frontier),
		frontierMomentum: frontier,
		registryStorage:  storage,
		sporkActive:      true,
	}
	abvt := &accountBlockTransactionVerifier{
		transaction:      &nom.AccountBlockTransaction{Block: block},
		momentumStore:    momentumStore,
		frontierStore:    momentumStore,
		isMultisigActive: true,
	}
	if err := abvt.signature(); err != ErrABSignatureInvalid {
		t.Fatalf("expected ErrABSignatureInvalid, got %v", err)
	}
}

// --- producer() test ----------------------------------------------------------

func TestMultisigProducer_NoOp(t *testing.T) {
	addr := newMultisigAddress(t)
	block := newSendBlock(addr, 1, types.HashHeight{})
	abvt := &accountBlockTransactionVerifier{
		transaction: &nom.AccountBlockTransaction{Block: block},
	}
	if err := abvt.producer(); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
}

// --- recency floor / momentumAcknowledged() tests -----------------------------

func TestMultisigMomentumAcknowledged_RecencyFloor_FirstBlock(t *testing.T) {
	addr := newMultisigAddress(t)
	// frontier far ahead; MA is at height 1, way more than MultisigMaxMaLag below frontier.
	staleMA := testMomentum(1)
	frontierHeight := uint64(1) + constants.MultisigMaxMaLag + 1
	frontier := testMomentum(frontierHeight)

	block := newSendBlock(addr, 1, maOf(staleMA))

	momentumStore := &fakeMomentumStore{
		identifier:       maOf(staleMA),
		frontierMomentum: staleMA,
	}
	frontierStore := &frontierOnlyStore{identifier: maOf(frontier)}

	abv := &accountBlockVerifier{
		block:         block,
		accountStore:  &fakeAccountStore{},
		momentumStore: momentumStore,
		frontierStore: frontierStore,
	}
	if err := abv.momentumAcknowledged(); err != ErrABMATooOld {
		t.Fatalf("expected ErrABMATooOld for height-1 block with stale MA, got %v", err)
	}
}

func TestMultisigMomentumAcknowledged_RecencyFloor_OK(t *testing.T) {
	addr := newMultisigAddress(t)
	frontierHeight := uint64(100)
	ma := testMomentum(frontierHeight - constants.MultisigMaxMaLag) // exactly at the floor
	frontier := testMomentum(frontierHeight)

	block := newSendBlock(addr, 1, maOf(ma))

	momentumStore := &fakeMomentumStore{
		identifier:       maOf(ma),
		frontierMomentum: ma,
	}
	frontierStore := &frontierOnlyStore{identifier: maOf(frontier)}

	abv := &accountBlockVerifier{
		block:         block,
		accountStore:  &fakeAccountStore{},
		momentumStore: momentumStore,
		frontierStore: frontierStore,
	}
	if err := abv.momentumAcknowledged(); err != nil {
		t.Fatalf("expected nil at exactly the recency floor, got %v", err)
	}
}

func TestMultisigMomentumAcknowledged_RecencyFloor_BackdatedRevocation(t *testing.T) {
	addr := newMultisigAddress(t)
	// Simulate: account already has a previous block at height 1; this is its height-2 block,
	// citing a pre-rotation MA more than MultisigMaxMaLag below the current frontier.
	frontierHeight := constants.MultisigMaxMaLag + 200
	staleMA := testMomentum(10)
	frontier := testMomentum(frontierHeight)

	prevBlock := newSendBlock(addr, 1, maOf(testMomentum(5)))
	block := newSendBlock(addr, 2, maOf(staleMA))

	momentumStore := &fakeMomentumStore{
		identifier:       maOf(staleMA),
		frontierMomentum: staleMA,
	}
	frontierStore := &frontierOnlyStore{identifier: maOf(frontier)}

	abv := &accountBlockVerifier{
		block:         block,
		accountStore:  &fakeAccountStore{byHeight: map[uint64]*nom.AccountBlock{1: prevBlock}},
		momentumStore: momentumStore,
		frontierStore: frontierStore,
	}
	if err := abv.momentumAcknowledged(); err != ErrABMATooOld {
		t.Fatalf("expected ErrABMATooOld for backdated MA citing pre-rotation snapshot, got %v", err)
	}
}

func TestMultisigMomentumAcknowledged_OldSetWithinWindow_OK(t *testing.T) {
	addr := newMultisigAddress(t)
	frontierHeight := constants.MultisigMaxMaLag + 5
	ma := testMomentum(5) // within the window: frontierHeight - 5 <= MultisigMaxMaLag
	frontier := testMomentum(frontierHeight)

	prevBlock := newSendBlock(addr, 1, maOf(testMomentum(1)))
	block := newSendBlock(addr, 2, maOf(ma))

	momentumStore := &fakeMomentumStore{
		identifier:       maOf(ma),
		frontierMomentum: ma,
	}
	frontierStore := &frontierOnlyStore{identifier: maOf(frontier)}

	abv := &accountBlockVerifier{
		block:         block,
		accountStore:  &fakeAccountStore{byHeight: map[uint64]*nom.AccountBlock{1: prevBlock}},
		momentumStore: momentumStore,
		frontierStore: frontierStore,
	}
	if err := abv.momentumAcknowledged(); err != nil {
		t.Fatalf("expected nil for old-set block still within the recency window, got %v", err)
	}
}

// --- single-rotation maturity (signature() reads ActivePolicyAtHeight) --------

func TestMultisig_RotationMaturity(t *testing.T) {
	addr := newMultisigAddress(t)
	oldSigners := generateSigners(t, 3)
	oldPolicy := policyFromSigners(t, 2, oldSigners)
	newSigners := generateSigners(t, 3)
	newPolicy := policyFromSigners(t, 2, newSigners)

	const pendingHeight = uint64(50)
	rec := &definition.MultisigRecord{
		Active:        oldPolicy,
		Pending:       &newPolicy,
		PendingHeight: pendingHeight,
	}
	storage := setupRegistry(t, addr, rec)

	// MA is fixed; only the FRONTIER height (where signature() now reads the active policy) varies
	// across the maturity boundary.
	ma := testMomentum(1)
	block := newSendBlock(addr, 1, maOf(ma))
	oldSigs := signWithPolicy(t, oldPolicy, oldSigners, block.Hash)
	newSigs := signWithPolicy(t, newPolicy, newSigners, block.Hash)

	verify := func(frontierHeight uint64, sigs [][]byte) error {
		b := *block
		b.MultisigAuth = &nom.MultisigAuth{Signatures: sigs}
		momentumStore := &fakeMomentumStore{
			identifier:       maOf(ma),
			frontierMomentum: ma,
			registryStorage:  storage,
			sporkActive:      true,
		}
		frontierStore := &fakeMomentumStore{
			identifier:      maOf(testMomentum(frontierHeight)),
			registryStorage: storage,
		}
		abvt := &accountBlockTransactionVerifier{
			transaction:      &nom.AccountBlockTransaction{Block: &b},
			momentumStore:    momentumStore,
			frontierStore:    frontierStore,
			isMultisigActive: true,
		}
		return abvt.signature()
	}

	// Before maturity (frontier height < PendingHeight + 60): old policy still active. New-set
	// sigs fail because they don't match the (still active) old policy.
	beforeMaturityHeight := pendingHeight + constants.MultisigPolicyMaturityDelay - 1
	if err := verify(beforeMaturityHeight, newSigs); err != ErrABSignatureInvalid {
		t.Fatalf("expected new-set sigs to fail before maturity, got %v", err)
	}
	if err := verify(beforeMaturityHeight, oldSigs); err != nil {
		t.Fatalf("expected old-set sigs to pass before maturity, got %v", err)
	}

	// At/after maturity: new policy active.
	atMaturityHeight := pendingHeight + constants.MultisigPolicyMaturityDelay
	if err := verify(atMaturityHeight, newSigs); err != nil {
		t.Fatalf("expected new-set sigs to pass at maturity, got %v", err)
	}
	if err := verify(atMaturityHeight, oldSigs); err != ErrABSignatureInvalid {
		t.Fatalf("expected old-set sigs to fail at maturity, got %v", err)
	}
}

// --- MultisigAuth must be zero on non-multisig blocks (Fix 2) -----------------

func TestSignature_UserBlock_MultisigAuthMustBeZero(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	addr := types.PubKeyToAddress(pub)
	block := newSendBlock(addr, 1, types.HashHeight{})
	block.PublicKey = pub
	block.Signature = ed25519.Sign(priv, block.Hash.Bytes())
	block.MultisigAuth = &nom.MultisigAuth{Signatures: [][]byte{{1, 2, 3}}}

	abvt := &accountBlockTransactionVerifier{
		transaction: &nom.AccountBlockTransaction{Block: block},
	}
	if err := abvt.signature(); err != ErrABMultisigAuthMustBeZero {
		t.Fatalf("expected ErrABMultisigAuthMustBeZero, got %v", err)
	}
}

func TestSignature_EmbeddedBlock_MultisigAuthMustBeZero(t *testing.T) {
	block := newSendBlock(types.TokenContract, 1, types.HashHeight{})
	block.MultisigAuth = &nom.MultisigAuth{Signatures: [][]byte{{1, 2, 3}}}

	abvt := &accountBlockTransactionVerifier{
		transaction: &nom.AccountBlockTransaction{Block: block},
	}
	if err := abvt.signature(); err != ErrABMultisigAuthMustBeZero {
		t.Fatalf("expected ErrABMultisigAuthMustBeZero, got %v", err)
	}
}

func TestMultisigSignature_WrongLengthSignature(t *testing.T) {
	addr := newMultisigAddress(t)
	frontier := testMomentum(10)
	signers := generateSigners(t, 3)
	policy := policyFromSigners(t, 2, signers)
	storage := setupRegistry(t, addr, &definition.MultisigRecord{Active: policy})

	block := newSendBlock(addr, 1, maOf(frontier))
	good := signFor(t, signers, policy.Signers[0], block.Hash)
	wrongLength := good[:len(good)-1]
	block.MultisigAuth = &nom.MultisigAuth{Signatures: [][]byte{good, wrongLength}}

	momentumStore := &fakeMomentumStore{
		identifier:       maOf(frontier),
		frontierMomentum: frontier,
		registryStorage:  storage,
		sporkActive:      true,
	}
	abvt := &accountBlockTransactionVerifier{
		transaction:      &nom.AccountBlockTransaction{Block: block},
		momentumStore:    momentumStore,
		frontierStore:    momentumStore,
		isMultisigActive: true,
	}
	if err := abvt.signature(); err != ErrABSignatureInvalid {
		t.Fatalf("expected ErrABSignatureInvalid, got %v", err)
	}
}
