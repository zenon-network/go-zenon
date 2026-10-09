//go:build testnet

package ptlc_test

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"math/big"
	"testing"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/btcec/v2/schnorr"

	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/vm/embedded/definition"
	"github.com/zenon-network/go-zenon/wallet"
)

// The live tests of what this branch adds to the contract: the secp256k1 point
// type, the fixed destination, and the two swaps built on them. Each runs
// against the devnet's RPC node with the two funded accounts, as the tests in
// ptlc_test.go do, and each is the live counterpart of mock-chain tests in
// vm/embedded/tests (ptlc_point_test.go, ptlc_keyswap_test.go).
//
// The devnet has two accounts with plasma, so where a test needs a third
// party to submit a call, the entry's creator stands in for it.

// ---- scalars and points -------------------------------------------------------

func randomScalar(t *testing.T) []byte {
	t.Helper()
	key, err := btcec.NewPrivateKey()
	if err != nil {
		t.Fatalf("random scalar: %v", err)
	}
	return key.Serialize()
}

func pointOf(scalar []byte) []byte {
	key, _ := btcec.PrivKeyFromBytes(scalar)
	return key.PubKey().SerializeCompressed()
}

func addScalars(a, b []byte) []byte {
	var ka, kb btcec.ModNScalar
	ka.SetByteSlice(a)
	kb.SetByteSlice(b)
	ka.Add(&kb)
	out := ka.Bytes()
	return out[:]
}

// ---- adaptor pre-signatures ---------------------------------------------------
//
// BIP-340 with the nonce offset by the point T, the Schnorr adaptor signature
// of Blockstream's scriptless-scripts notes, as docs/ptlc/SIGNING.md states it
// and as vm/embedded/tests/ptlc_keyswap_test.go has it:
// R = k*G + T, s' = k + e*d with k negated when R has odd y. A pre-signature
// is 33 bytes of R and 32 of s'. The contract sees none of this, only the
// completed signature.

func bip340Challenge(rx, px, msg []byte) btcec.ModNScalar {
	tag := sha256.Sum256([]byte("BIP0340/challenge"))
	h := sha256.New()
	h.Write(tag[:])
	h.Write(tag[:])
	h.Write(rx)
	h.Write(px)
	h.Write(msg)
	var digest [32]byte
	copy(digest[:], h.Sum(nil))
	var e btcec.ModNScalar
	e.SetBytes(&digest)
	return e
}

func hasOddY(p *btcec.JacobianPoint) bool {
	p.ToAffine()
	return p.Y.IsOdd()
}

func adaptorPreSign(t *testing.T, priv *btcec.PrivateKey, msg, adaptor []byte) []byte {
	t.Helper()
	tPub, err := btcec.ParsePubKey(adaptor)
	if err != nil {
		t.Fatalf("adaptor point: %v", err)
	}
	var tPoint btcec.JacobianPoint
	tPub.AsJacobian(&tPoint)

	d := priv.Key
	var p btcec.JacobianPoint
	btcec.ScalarBaseMultNonConst(&d, &p)
	if hasOddY(&p) {
		d.Negate()
	}
	px := p.X.Bytes()

	var nonce [32]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		t.Fatalf("nonce: %v", err)
	}
	var k btcec.ModNScalar
	k.SetBytes(&nonce)
	var kG, r btcec.JacobianPoint
	btcec.ScalarBaseMultNonConst(&k, &kG)
	btcec.AddNonConst(&kG, &tPoint, &r)
	if hasOddY(&r) {
		k.Negate()
	}
	rx := r.X.Bytes()
	e := bip340Challenge(rx[:], px[:], msg)
	var s btcec.ModNScalar
	s.Mul2(&e, &d).Add(&k)
	sb := s.Bytes()
	return append(btcec.NewPublicKey(&r.X, &r.Y).SerializeCompressed(), sb[:]...)
}

// adaptorVerify is what the receiver of a pre-signature checks before relying
// on it: s'*G == (R - T) + e*P, or (T - R) + e*P when R has odd y.
func adaptorVerify(pubx, msg, adaptor, presig []byte) bool {
	pPub, err := schnorr.ParsePubKey(pubx)
	if err != nil || len(presig) != 65 {
		return false
	}
	tPub, err := btcec.ParsePubKey(adaptor)
	if err != nil {
		return false
	}
	rPub, err := btcec.ParsePubKey(presig[:33])
	if err != nil {
		return false
	}
	var p, tPoint, r btcec.JacobianPoint
	pPub.AsJacobian(&p)
	tPub.AsJacobian(&tPoint)
	rPub.AsJacobian(&r)
	rx := r.X.Bytes()
	e := bip340Challenge(rx[:], pubx, msg)

	var s btcec.ModNScalar
	if s.SetByteSlice(presig[33:]) {
		return false
	}
	tPoint.Y.Negate(1).Normalize()
	var diff, eP, want, got btcec.JacobianPoint
	btcec.AddNonConst(&r, &tPoint, &diff)
	if presig[0] == 0x03 {
		diff.ToAffine()
		diff.Y.Negate(1).Normalize()
	}
	btcec.ScalarMultNonConst(&e, &p, &eP)
	btcec.AddNonConst(&diff, &eP, &want)
	btcec.ScalarBaseMultNonConst(&s, &got)
	want.ToAffine()
	got.ToAffine()
	return want.X.Equals(&got.X) && want.Y.Equals(&got.Y)
}

func adaptorComplete(presig, secret []byte) []byte {
	var s, k btcec.ModNScalar
	s.SetByteSlice(presig[33:])
	k.SetByteSlice(secret)
	if presig[0] == 0x03 {
		k.Negate()
	}
	s.Add(&k)
	sb := s.Bytes()
	return append(append([]byte{}, presig[1:33]...), sb[:]...)
}

func adaptorExtract(presig, signature []byte) []byte {
	var sPre, s btcec.ModNScalar
	sPre.SetByteSlice(presig[33:])
	s.SetByteSlice(signature[32:])
	if presig[0] == 0x03 {
		s.Negate()
		sPre.Add(&s)
		out := sPre.Bytes()
		return out[:]
	}
	sPre.Negate()
	s.Add(&sPre)
	out := s.Bytes()
	return out[:]
}

// ---- harness additions --------------------------------------------------------

// createPtlcTo makes an entry that names leg.Destination as the only address
// it may pay, and holds the stored entry to the leg, destination included.
func (h *harness) createPtlcTo(locker *wallet.KeyPair, leg ptlcSwapLeg) types.Hash {
	h.t.Helper()

	data := definition.ABIPtlc.PackMethodPanic(definition.CreatePtlcMethodName, leg.ExpirationTime, leg.PointType, leg.PointLock, leg.Destination)
	id := h.mustPublishSend(locker, types.PtlcContract, leg.Amount, leg.TokenStandard, data)
	if status := h.waitContractStatus(id); status != contractSuccess {
		h.t.Fatalf("%s create status = %d, want success", leg.Name, status)
	}
	h.assertPtlcMatches(id, leg)
	if info := h.waitPtlc(id); info.Destination != leg.Destination {
		h.t.Fatalf("%s destination = %s, want %s", leg.Name, info.Destination, leg.Destination)
	}
	return id
}

// unlock sends Unlock(id, witness) as caller and returns the send block's hash
// once the contract has answered with the status wanted.
func (h *harness) unlock(caller *wallet.KeyPair, id types.Hash, witness []byte, want uint64, what string) types.Hash {
	h.t.Helper()

	data := definition.ABIPtlc.PackMethodPanic(definition.UnlockPtlcMethodName, id, witness)
	hash := h.mustPublishSend(caller, types.PtlcContract, big.NewInt(0), types.ZeroTokenStandard, data)
	if status := h.waitContractStatus(hash); status != want {
		h.t.Fatalf("%s: status = %d, want %d", what, status, want)
	}
	return hash
}

func (h *harness) proxyUnlock(caller *wallet.KeyPair, id types.Hash, destination types.Address, witness []byte, want uint64, what string) types.Hash {
	h.t.Helper()

	data := definition.ABIPtlc.PackMethodPanic(definition.ProxyUnlockPtlcMethodName, id, destination, witness)
	hash := h.mustPublishSend(caller, types.PtlcContract, big.NewInt(0), types.ZeroTokenStandard, data)
	if status := h.waitContractStatus(hash); status != want {
		h.t.Fatalf("%s: status = %d, want %d", what, status, want)
	}
	return hash
}

func (h *harness) reclaim(caller *wallet.KeyPair, id types.Hash, want uint64, what string) {
	h.t.Helper()

	data := definition.ABIPtlc.PackMethodPanic(definition.ReclaimPtlcMethodName, id)
	hash := h.mustPublishSend(caller, types.PtlcContract, big.NewInt(0), types.ZeroTokenStandard, data)
	if status := h.waitContractStatus(hash); status != want {
		h.t.Fatalf("%s: status = %d, want %d", what, status, want)
	}
}

// observedWitness reads the witness of an unlock back from the caller's send
// block: what anyone watching the chain has, whatever the contract answered.
func (h *harness) observedWitness(unlockHash types.Hash) []byte {
	h.t.Helper()
	return unpackUnlockSignature(h.t, h.waitBlock(unlockHash).Data)
}

// balances is a snapshot of both parties and the contract in two tokens, and
// expect holds the chain to it plus the changes given, in whole coins.
type balances struct {
	h                        *harness
	alice, bob               *wallet.KeyPair
	aZnn, aQsr               *big.Int
	bZnn, bQsr               *big.Int
	contractZnn, contractQsr *big.Int
}

func (h *harness) snapshot(alice, bob *wallet.KeyPair) *balances {
	h.t.Helper()
	h.receiveAll(alice)
	h.receiveAll(bob)
	return &balances{
		h: h, alice: alice, bob: bob,
		aZnn: h.balance(alice.Address, types.ZnnTokenStandard), aQsr: h.balance(alice.Address, types.QsrTokenStandard),
		bZnn: h.balance(bob.Address, types.ZnnTokenStandard), bQsr: h.balance(bob.Address, types.QsrTokenStandard),
		contractZnn: h.balance(types.PtlcContract, types.ZnnTokenStandard), contractQsr: h.balance(types.PtlcContract, types.QsrTokenStandard),
	}
}

func (b *balances) expect(aZnn, aQsr, bZnn, bQsr int64) {
	b.h.t.Helper()
	b.h.receiveAll(b.alice)
	b.h.receiveAll(b.bob)
	check := func(who string, address types.Address, token types.ZenonTokenStandard, before *big.Int, delta int64) {
		want := new(big.Int).Add(before, big.NewInt(delta*zexp))
		if got := b.h.balance(address, token); got.Cmp(want) != 0 {
			b.h.t.Fatalf("%s %s balance = %s, want %s", who, token, got, want)
		}
	}
	check("Alice", b.alice.Address, types.ZnnTokenStandard, b.aZnn, aZnn)
	check("Alice", b.alice.Address, types.QsrTokenStandard, b.aQsr, aQsr)
	check("Bob", b.bob.Address, types.ZnnTokenStandard, b.bZnn, bZnn)
	check("Bob", b.bob.Address, types.QsrTokenStandard, b.bQsr, bQsr)
	check("contract", types.PtlcContract, types.ZnnTokenStandard, b.contractZnn, 0)
	check("contract", types.PtlcContract, types.QsrTokenStandard, b.contractQsr, 0)
}

// ---- the point type -----------------------------------------------------------

// A point lock is opened by the scalar behind it, pays only the address the
// entry names, and cannot be created without one.
func TestPtlcPointLockViaRPC(t *testing.T) {
	h := newHarness(t)
	alice, bob := h.keys[1], h.keys[3]
	before := h.snapshot(alice, bob)

	secret := randomScalar(t)
	leg := ptlcSwapLeg{
		Name: "alice-locks-znn-to-a-point-for-bob", Locker: alice.Address, Destination: bob.Address,
		TokenStandard: types.ZnnTokenStandard, Amount: oneZNN(2), ExpirationTime: h.currentTimestamp() + 600,
		PointType: definition.PointTypeSecp256k1Point, PointLock: pointOf(secret),
	}

	t.Log("a point lock that names nobody is a bearer lock: the node does not take the block")
	bearer := definition.ABIPtlc.PackMethodPanic(definition.CreatePtlcMethodName, leg.ExpirationTime, leg.PointType, leg.PointLock, types.ZeroAddress)
	if _, err := h.publishSend(alice, types.PtlcContract, leg.Amount, leg.TokenStandard, bearer); err == nil {
		t.Fatalf("point lock with no destination unexpectedly accepted")
	}
	t.Log("nor one whose lock is an uncompressed-style or off-curve encoding")
	badLock := append([]byte{0x04}, leg.PointLock[1:]...)
	bad := definition.ABIPtlc.PackMethodPanic(definition.CreatePtlcMethodName, leg.ExpirationTime, leg.PointType, badLock, bob.Address)
	if _, err := h.publishSend(alice, types.PtlcContract, leg.Amount, leg.TokenStandard, bad); err == nil {
		t.Fatalf("point lock with prefix 04 unexpectedly accepted")
	}

	id := h.createPtlcTo(alice, leg)

	t.Log("a scalar that is not the lock's is refused, and the entry survives it")
	h.unlock(bob, id, randomScalar(t), contractFail, "unlock with a wrong scalar")
	h.waitPtlc(id)

	t.Log("the right scalar from an account the entry does not name is refused")
	h.unlock(alice, id, secret, contractFail, "unlock by an address that is not the destination")
	h.proxyUnlock(alice, id, alice.Address, secret, contractFail, "proxy unlock to another destination")
	h.waitPtlc(id)

	t.Log("the scalar is public now, and it still pays only Bob: anyone may deliver it")
	h.proxyUnlock(alice, id, bob.Address, secret, contractSuccess, "proxy unlock to the named destination")
	h.waitPtlcDeleted(id)
	before.expect(-2, 0, +2, 0)
}

// A key lock may name its destination too. A signature over any other address
// then verifies and is refused all the same.
func TestPtlcFixedDestinationKeyLockViaRPC(t *testing.T) {
	h := newHarness(t)
	alice, bob := h.keys[1], h.keys[3]
	before := h.snapshot(alice, bob)

	key, err := btcec.NewPrivateKey()
	if err != nil {
		t.Fatalf("key: %v", err)
	}
	leg := ptlcSwapLeg{
		Name: "alice-locks-znn-to-a-key-for-bob-only", Locker: alice.Address, Destination: bob.Address,
		TokenStandard: types.ZnnTokenStandard, Amount: oneZNN(2), ExpirationTime: h.currentTimestamp() + 600,
		PointType: definition.PointTypeBIP340, PointLock: schnorr.SerializePubKey(key.PubKey()),
	}

	t.Log("an entry may not name an embedded contract, which could not take the payout")
	toContract := definition.ABIPtlc.PackMethodPanic(definition.CreatePtlcMethodName, leg.ExpirationTime, leg.PointType, leg.PointLock, types.PlasmaContract)
	if _, err := h.publishSend(alice, types.PtlcContract, leg.Amount, leg.TokenStandard, toContract); err == nil {
		t.Fatalf("entry with an embedded destination unexpectedly accepted")
	}

	id := h.createPtlcTo(alice, leg)

	t.Log("the lock's key signs \"pay Alice\": a valid signature for a destination the entry does not allow")
	toAlice := h.signBIP340Unlock(key, id, alice.Address)
	verifyObservedBIP340Unlock(t, leg.PointLock, h.chainIdentifier(), id, alice.Address, toAlice)
	h.unlock(alice, id, toAlice, contractFail, "unlock to an address the entry does not name")
	h.proxyUnlock(bob, id, alice.Address, toAlice, contractFail, "proxy unlock to an address the entry does not name")
	h.waitPtlc(id)

	t.Log("and \"pay Bob\", which is accepted")
	h.unlock(bob, id, h.signBIP340Unlock(key, id, bob.Address), contractSuccess, "unlock to the named destination")
	h.waitPtlcDeleted(id)
	before.expect(-2, 0, +2, 0)
}

// ---- swaps --------------------------------------------------------------------

// Two point locks with a tweak, in the safe order: Alice holds t, locks first
// and expires last. Her claim publishes t; Bob reads it from the chain, adds
// the tweak the two agreed, and claims. The chain shows two unrelated points.
func TestPtlcPointLockTweakedSwapViaRPC(t *testing.T) {
	h := newHarness(t)
	alice, bob := h.keys[1], h.keys[3]
	before := h.snapshot(alice, bob)

	secret, tweak := randomScalar(t), randomScalar(t)
	tweaked := addScalars(secret, tweak)
	now := h.currentTimestamp()
	aliceLeg := ptlcSwapLeg{
		Name: "alice-locks-qsr-on-the-tweaked-point", Locker: alice.Address, Destination: bob.Address,
		TokenStandard: types.QsrTokenStandard, Amount: oneQSR(100), ExpirationTime: now + 900,
		PointType: definition.PointTypeSecp256k1Point, PointLock: pointOf(tweaked),
	}
	bobLeg := ptlcSwapLeg{
		Name: "bob-locks-znn-on-the-point", Locker: bob.Address, Destination: alice.Address,
		TokenStandard: types.ZnnTokenStandard, Amount: oneZNN(3), ExpirationTime: now + 600,
		PointType: definition.PointTypeSecp256k1Point, PointLock: pointOf(secret),
	}
	if bytes.Equal(aliceLeg.PointLock, bobLeg.PointLock) {
		t.Fatalf("the tweak did not change the point")
	}

	t.Log("Alice holds t, so she locks first and her entry expires last")
	aliceID := h.createPtlcTo(alice, aliceLeg)
	t.Log("Bob checks her entry on the chain, then locks, expiring five minutes sooner")
	bobID := h.createPtlcTo(bob, bobLeg)

	t.Log("Bob cannot open Alice's entry with the tweak alone")
	h.unlock(bob, aliceID, tweak, contractFail, "unlock with the tweak and no secret")

	t.Log("Alice claims Bob's ZNN, which puts t on the chain")
	aliceUnlock := h.unlock(alice, bobID, secret, contractSuccess, "Alice's claim")
	observed := h.observedWitness(aliceUnlock)
	if !bytes.Equal(pointOf(observed), bobLeg.PointLock) {
		t.Fatalf("what the chain shows is not the scalar behind Bob's lock")
	}

	t.Log("Bob reads t from her send block, adds the tweak, and claims her QSR")
	h.unlock(bob, aliceID, addScalars(observed, tweak), contractSuccess, "Bob's claim")
	h.waitPtlcDeleted(aliceID)
	h.waitPtlcDeleted(bobID)
	before.expect(+3, -100, -3, +100)
}

// keySwapTerms is a swap on two BIP340 key locks, each to a key its maker made
// for the swap and each naming the other party as its destination, with an
// adaptor pre-signature each way on T = t*G.
type keySwapTerms struct {
	secret, point    []byte
	aliceKey, bobKey *btcec.PrivateKey
	aliceLeg, bobLeg ptlcSwapLeg
	aliceID, bobID   types.Hash
	forBob, forAlice []byte // the pre-signatures, named by who holds them
}

// lockKeySwap takes the swap as far as both entries locked and both
// pre-signatures made and verified. aliceNames is the destination Alice's
// entry is created with: Bob, or nobody.
func (h *harness) lockKeySwap(alice, bob *wallet.KeyPair, aliceNames types.Address) *keySwapTerms {
	h.t.Helper()
	t := h.t

	aliceKey, err := btcec.NewPrivateKey()
	if err != nil {
		t.Fatalf("key: %v", err)
	}
	bobKey, err := btcec.NewPrivateKey()
	if err != nil {
		t.Fatalf("key: %v", err)
	}
	s := &keySwapTerms{secret: randomScalar(t), aliceKey: aliceKey, bobKey: bobKey}
	s.point = pointOf(s.secret)
	now := h.currentTimestamp()
	s.aliceLeg = ptlcSwapLeg{
		Name: "alice-locks-qsr-to-her-own-key", Locker: alice.Address, Destination: aliceNames,
		TokenStandard: types.QsrTokenStandard, Amount: oneQSR(100), ExpirationTime: now + 900,
		PointType: definition.PointTypeBIP340, PointLock: schnorr.SerializePubKey(aliceKey.PubKey()),
	}
	s.bobLeg = ptlcSwapLeg{
		Name: "bob-locks-znn-to-his-own-key", Locker: bob.Address, Destination: alice.Address,
		TokenStandard: types.ZnnTokenStandard, Amount: oneZNN(3), ExpirationTime: now + 600,
		PointType: definition.PointTypeBIP340, PointLock: schnorr.SerializePubKey(bobKey.PubKey()),
	}
	chain := h.chainIdentifier()

	t.Log("Alice holds t. She locks first, to a key of her own, and her entry expires last")
	s.aliceID = h.createPtlcTo(alice, s.aliceLeg)
	aliceMsg := definition.GetPtlcUnlockMessage(chain, definition.PointTypeBIP340, s.aliceID, bob.Address)
	s.forBob = adaptorPreSign(t, aliceKey, aliceMsg, s.point)
	t.Log("She gives Bob a pre-signature over \"pay Bob\" for her entry, with t left out; Bob verifies it")
	if !adaptorVerify(s.aliceLeg.PointLock, aliceMsg, s.point, s.forBob) {
		t.Fatalf("Alice's pre-signature does not verify")
	}

	t.Log("Bob locks to a key of his own, payable only to Alice, expiring five minutes sooner")
	s.bobID = h.createPtlcTo(bob, s.bobLeg)
	bobMsg := definition.GetPtlcUnlockMessage(chain, definition.PointTypeBIP340, s.bobID, alice.Address)
	s.forAlice = adaptorPreSign(t, bobKey, bobMsg, s.point)
	t.Log("He gives Alice a pre-signature over \"pay Alice\" for his entry; Alice verifies it")
	if !adaptorVerify(s.bobLeg.PointLock, bobMsg, s.point, s.forAlice) {
		t.Fatalf("Bob's pre-signature does not verify")
	}
	return s
}

// The swap with nothing of the secret on the chain. Alice completes Bob's
// pre-signature with t and claims; her signature on the chain, set against the
// pre-signature Bob made, gives him t, and he completes hers. No key is held
// by two parties: each lock's key can only ever pay the other party.
func TestPtlcKeySwapAdaptorViaRPC(t *testing.T) {
	h := newHarness(t)
	alice, bob := h.keys[1], h.keys[3]
	before := h.snapshot(alice, bob)
	s := h.lockKeySwap(alice, bob, bob.Address)
	chain := h.chainIdentifier()

	t.Log("a pre-signature is not a signature: without t it opens nothing")
	asSignature := append(append([]byte{}, s.forBob[1:33]...), s.forBob[33:]...)
	h.unlock(bob, s.aliceID, asSignature, contractFail, "unlock with an uncompleted pre-signature")

	t.Log("neither party can sign its own entry back to itself: the destination is fixed")
	h.unlock(alice, s.aliceID, h.signBIP340Unlock(s.aliceKey, s.aliceID, alice.Address), contractFail, "Alice signing her own entry to herself")

	t.Log("Alice completes Bob's pre-signature with t and claims his ZNN")
	aliceUnlock := h.unlock(alice, s.bobID, adaptorComplete(s.forAlice, s.secret), contractSuccess, "Alice's claim")
	observed := h.observedWitness(aliceUnlock)
	verifyObservedBIP340Unlock(t, s.bobLeg.PointLock, chain, s.bobID, alice.Address, observed)

	t.Log("Bob takes t from her signature and the pre-signature he made, and completes hers")
	learned := adaptorExtract(s.forAlice, observed)
	if !bytes.Equal(pointOf(learned), s.point) {
		t.Fatalf("what Bob extracted is not the scalar behind T")
	}
	bobUnlock := h.unlock(bob, s.aliceID, adaptorComplete(s.forBob, learned), contractSuccess, "Bob's claim")
	h.waitPtlcDeleted(s.aliceID)
	h.waitPtlcDeleted(s.bobID)
	before.expect(+3, -100, -3, +100)

	// What the swap is for: read back everything the two entries and the two
	// claims put on the chain, and find neither the secret nor its point.
	var onChain []byte
	for _, hash := range []types.Hash{s.aliceID, s.bobID, aliceUnlock, bobUnlock} {
		onChain = append(append(onChain, h.waitBlock(hash).Data...), 0xff)
	}
	if bytes.Contains(onChain, s.secret) {
		t.Fatalf("the secret is on the chain")
	}
	if bytes.Contains(onChain, s.point[1:]) {
		t.Fatalf("the secret's point is on the chain")
	}
	if !bytes.Contains(onChain, s.aliceLeg.PointLock) || !bytes.Contains(onChain, s.bobLeg.PointLock) {
		t.Fatalf("the lock keys are not in the blocks read back, so this check is not reading the swap")
	}
}

// The same swap with Alice's entry naming no destination, which the contract
// takes for a key lock. The key is hers: she signs the entry back to herself,
// then claims Bob's as agreed. Bob learns t and completes a signature for an
// entry that is gone. The party locking second must read the first entry and
// refuse one that does not name it; nothing on the chain does it for him.
func TestPtlcKeySwapOpenDestinationLosesViaRPC(t *testing.T) {
	h := newHarness(t)
	alice, bob := h.keys[1], h.keys[3]
	before := h.snapshot(alice, bob)
	s := h.lockKeySwap(alice, bob, types.ZeroAddress)

	if info := h.waitPtlc(s.aliceID); !info.Destination.IsZero() {
		t.Fatalf("Alice's entry names %s; this test needs it to name nobody", info.Destination)
	}

	t.Log("Alice signs her own entry back to herself with the lock's key")
	h.unlock(alice, s.aliceID, h.signBIP340Unlock(s.aliceKey, s.aliceID, alice.Address), contractSuccess, "Alice taking her own entry back")
	h.waitPtlcDeleted(s.aliceID)

	t.Log("and claims Bob's ZNN with the completed pre-signature")
	aliceUnlock := h.unlock(alice, s.bobID, adaptorComplete(s.forAlice, s.secret), contractSuccess, "Alice's claim")

	t.Log("Bob learns t, and the signature it completes is for an entry that no longer exists")
	learned := adaptorExtract(s.forAlice, h.observedWitness(aliceUnlock))
	if !bytes.Equal(pointOf(learned), s.point) {
		t.Fatalf("what Bob extracted is not the scalar behind T")
	}
	h.unlock(bob, s.aliceID, adaptorComplete(s.forBob, learned), contractFail, "Bob's claim on the entry Alice took back")
	before.expect(+3, 0, -3, 0)
}

// A claim sent too late is refused, and its witness is on the chain all the
// same. Alice holds t and claims Bob's entry after its expiry: the contract
// says no, Bob reads t from her refused call, takes his own funds back, and
// takes hers with t. The holder of a secret must not unlock near the deadline.
func TestPtlcLateClaimPublishesWitnessViaRPC(t *testing.T) {
	h := newHarness(t)
	alice, bob := h.keys[1], h.keys[3]
	before := h.snapshot(alice, bob)

	secret := randomScalar(t)
	now := h.currentTimestamp()
	aliceLeg := ptlcSwapLeg{
		Name: "alice-locks-qsr-for-bob", Locker: alice.Address, Destination: bob.Address,
		TokenStandard: types.QsrTokenStandard, Amount: oneQSR(100), ExpirationTime: now + 900,
		PointType: definition.PointTypeSecp256k1Point, PointLock: pointOf(secret),
	}
	bobLeg := ptlcSwapLeg{
		Name: "bob-locks-znn-for-alice-briefly", Locker: bob.Address, Destination: alice.Address,
		TokenStandard: types.ZnnTokenStandard, Amount: oneZNN(3), ExpirationTime: now + 150,
		PointType: definition.PointTypeSecp256k1Point, PointLock: pointOf(secret),
	}
	aliceID := h.createPtlcTo(alice, aliceLeg)
	bobID := h.createPtlcTo(bob, bobLeg)

	t.Log("Alice waits until Bob's entry has expired, and claims it anyway")
	h.waitTimestampAtLeast(bobLeg.ExpirationTime)
	late := h.unlock(alice, bobID, secret, contractFail, "Alice's claim after Bob's expiry")
	h.waitPtlc(bobID)

	t.Log("the contract refused her, and her call is on the chain with t in it")
	observed := h.observedWitness(late)
	if !bytes.Equal(pointOf(observed), bobLeg.PointLock) {
		t.Fatalf("the refused call does not carry the scalar")
	}

	t.Log("Bob takes his ZNN back, and Alice's QSR with her secret")
	h.reclaim(bob, bobID, contractSuccess, "Bob's reclaim")
	h.unlock(bob, aliceID, observed, contractSuccess, "Bob's claim with the secret from the refused call")
	h.waitPtlcDeleted(aliceID)
	h.waitPtlcDeleted(bobID)
	before.expect(0, -100, 0, +100)
}
