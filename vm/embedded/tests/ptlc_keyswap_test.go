package tests

import (
	"bytes"
	"crypto/sha256"
	"math/big"
	"testing"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/btcec/v2/schnorr"

	g "github.com/zenon-network/go-zenon/chain/genesis/mock"
	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/vm/constants"
	"github.com/zenon-network/go-zenon/vm/embedded/definition"
	"github.com/zenon-network/go-zenon/wallet"
	"github.com/zenon-network/go-zenon/zenon/mock"
)

// A swap on two BIP340 key locks, with no secret and no point on the chain.
//
// Alice holds t. Each party locks to a key of its own, made for this swap, and
// names the other as the only address the entry may pay. Each then hands the
// other an adaptor pre-signature on T = t*G over the unlock message of its own
// entry: a signature with t left out. Alice adds t to Bob's and claims his
// entry; the signature she publishes, set against the pre-signature Bob made,
// gives him t, and he adds it to hers.
//
// The fixed destination is what lets one key do the work Bitcoin needs a
// two-party key for. The owner of a lock's key can sign whenever it likes, but
// only ever to pay the other party (TestPtlc_keySwap_openDestinationLosesBob
// is the same swap without it).
//
// The adaptor arithmetic below is BIP-340's with the nonce offset by T, the
// Schnorr adaptor signature of Blockstream's scriptless-scripts notes, written
// out in docs/ptlc/SIGNING.md. It is here so that the test needs nothing
// outside this repository; the contract sees only ordinary signatures.

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

// keyWithParity is a private key whose public key has the y parity asked for.
// BIP-340 signs with the negated scalar when it is odd, so both are exercised.
func keyWithParity(seed uint32, odd bool) *btcec.PrivateKey {
	for i := seed; ; i++ {
		var k btcec.ModNScalar
		k.SetInt(i)
		var p btcec.JacobianPoint
		btcec.ScalarBaseMultNonConst(&k, &p)
		if hasOddY(&p) == odd {
			b := k.Bytes()
			key, _ := btcec.PrivKeyFromBytes(b[:])
			return key
		}
	}
}

// adaptorPreSign returns the pre-signature by priv over msg on the compressed
// point adaptor: 33 bytes of R = k*G + T and 32 of s' = k + e*d, with k negated
// when R has odd y. The nonce is the first at or above seed that gives R the
// parity asked for, so a test can reach both branches.
func adaptorPreSign(t *testing.T, priv *btcec.PrivateKey, msg, adaptor []byte, seed uint32, oddR bool) []byte {
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

	for i := seed; ; i++ {
		var k btcec.ModNScalar
		k.SetInt(i)
		var kG, r btcec.JacobianPoint
		btcec.ScalarBaseMultNonConst(&k, &kG)
		btcec.AddNonConst(&kG, &tPoint, &r)
		if hasOddY(&r) != oddR {
			continue
		}
		if oddR {
			k.Negate()
		}
		rx := r.X.Bytes()
		e := bip340Challenge(rx[:], px[:], msg)
		var s btcec.ModNScalar
		s.Mul2(&e, &d).Add(&k)
		sb := s.Bytes()
		return append(btcec.NewPublicKey(&r.X, &r.Y).SerializeCompressed(), sb[:]...)
	}
}

// adaptorComplete adds the secret to a pre-signature, giving the 64-byte
// BIP-340 signature the contract checks.
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

// adaptorExtract is what the maker of a pre-signature learns from the
// signature completed from it: the secret.
func adaptorExtract(presig, signature []byte) []byte {
	var sPre, s btcec.ModNScalar
	sPre.SetByteSlice(presig[33:])
	s.SetByteSlice(signature[32:])
	if presig[0] == 0x03 {
		s.Negate()
		sPre.Add(&s)
		return scalarBytes(&sPre)
	}
	sPre.Negate()
	s.Add(&sPre)
	return scalarBytes(&s)
}

func xOnlyKey(priv *btcec.PrivateKey) []byte {
	return schnorr.SerializePubKey(priv.PubKey())
}

func createKeyPtlc(t *testing.T, z mock.MockZenon, creator *wallet.KeyPair, key []byte, destination types.Address, expirationTime int64, token types.ZenonTokenStandard, amount *big.Int) types.Hash {
	t.Helper()
	block := z.InsertSendBlock(&nom.AccountBlock{
		Address:   creator.Address,
		ToAddress: types.PtlcContract,
		Data: definition.ABIPtlc.PackMethodPanic(definition.CreatePtlcMethodName,
			expirationTime,
			definition.PointTypeBIP340,
			key,
			destination,
		),
		TokenStandard: token,
		Amount:        amount,
	}, nil, mock.SkipVmChanges)
	z.InsertNewMomentum()
	z.InsertNewMomentum()
	return block.Hash
}

// keySwap is the two entries of one swap and what each party holds for the
// other's: Alice's entry pays Bob 100 QSR on her key, Bob's pays Alice 10 ZNN on
// his.
type keySwap struct {
	secret, point          []byte
	aliceKey, bobKey       *btcec.PrivateKey
	aliceLock, bobLock     types.Hash
	forBob, forAlice       []byte // the pre-signatures, named by who receives them
	aliceMsg, bobMsg       []byte
	aliceExpiry, bobExpiry int64
}

// newKeySwap locks both entries in the safe order, the holder of the secret
// first and last to expire, and exchanges the pre-signatures. oddKey and oddR
// choose the parity of both keys and both nonces.
func newKeySwap(t *testing.T, z mock.MockZenon, alice, bob *wallet.KeyPair, aliceDestination types.Address, oddKey, oddR bool) *keySwap {
	t.Helper()
	var kt btcec.ModNScalar
	kt.SetInt(424242)
	s := &keySwap{
		secret:      scalarBytes(&kt),
		aliceKey:    keyWithParity(1001, oddKey),
		bobKey:      keyWithParity(2002, oddKey),
		aliceExpiry: int64(genesisTimestamp + 6000),
		bobExpiry:   int64(genesisTimestamp + 3000),
	}
	s.point = pointFor(s.secret)

	s.aliceLock = createKeyPtlc(t, z, alice, xOnlyKey(s.aliceKey), aliceDestination, s.aliceExpiry, types.QsrTokenStandard, big.NewInt(100*g.Zexp))
	s.aliceMsg = ptlcUnlockMessage(z, definition.PointTypeBIP340, s.aliceLock, bob.Address)
	s.forBob = adaptorPreSign(t, s.aliceKey, s.aliceMsg, s.point, 31, oddR)

	s.bobLock = createKeyPtlc(t, z, bob, xOnlyKey(s.bobKey), alice.Address, s.bobExpiry, types.ZnnTokenStandard, big.NewInt(10*g.Zexp))
	s.bobMsg = ptlcUnlockMessage(z, definition.PointTypeBIP340, s.bobLock, alice.Address)
	s.forAlice = adaptorPreSign(t, s.bobKey, s.bobMsg, s.point, 57, oddR)
	return s
}

func TestPtlc_keySwap_adaptor(t *testing.T) {
	for _, c := range []struct {
		name         string
		oddKey, oddR bool
	}{
		{"even key, even nonce", false, false},
		{"even key, odd nonce", false, true},
		{"odd key, even nonce", true, false},
		{"odd key, odd nonce", true, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			z := mock.NewMockZenon(t)
			defer z.StopPanic()
			activatePtlc(t, z)
			alice, bob := g.User1, g.User2
			s := newKeySwap(t, z, alice, bob, bob.Address, c.oddKey, c.oddR)

			// A pre-signature is not a signature: without t it opens nothing.
			unlockPtlcAs(t, z, bob, s.aliceLock, append(append([]byte{}, s.forBob[1:33]...), s.forBob[33:]...), constants.ErrInvalidPointSignature)

			// Alice adds t to Bob's pre-signature and claims his ZNN.
			aliceSig := adaptorComplete(s.forAlice, s.secret)
			unlockPtlcAs(t, z, alice, s.bobLock, aliceSig, nil)
			autoreceive(t, z, alice.Address)
			z.ExpectBalance(alice.Address, types.ZnnTokenStandard, 12010*g.Zexp)

			// Bob made that pre-signature, so her signature tells him t.
			learned := adaptorExtract(s.forAlice, aliceSig)
			if !bytes.Equal(learned, s.secret) {
				t.Fatal("the signature Alice published does not give Bob the secret")
			}
			if !bytes.Equal(pointFor(learned), s.point) {
				t.Fatal("what Bob extracted is not the scalar behind T")
			}

			// Her signature says "pay Alice" for Bob's entry and is good for
			// nothing else; a third party copying it at Alice's entry gets no
			// further than the signature check.
			proxyUnlockPtlcAs(t, z, g.User3, s.aliceLock, bob.Address, aliceSig, constants.ErrInvalidPointSignature)

			// Bob adds t to Alice's pre-signature and claims her QSR.
			unlockPtlcAs(t, z, bob, s.aliceLock, adaptorComplete(s.forBob, learned), nil)
			autoreceive(t, z, bob.Address)
			z.ExpectBalance(bob.Address, types.QsrTokenStandard, 80100*g.Zexp)
			z.ExpectBalance(alice.Address, types.QsrTokenStandard, 119900*g.Zexp)
			z.ExpectBalance(bob.Address, types.ZnnTokenStandard, 7990*g.Zexp)
			z.ExpectBalance(types.PtlcContract, types.ZnnTokenStandard, 0)
			z.ExpectBalance(types.PtlcContract, types.QsrTokenStandard, 0)
		})
	}
}

// Before Alice claims, each lock's key can pay only the other party, and
// nobody else's signature pays anyone.
func TestPtlc_keySwap_keysPayOnlyTheCounterparty(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()
	activatePtlc(t, z)
	alice, bob := g.User1, g.User2
	s := newKeySwap(t, z, alice, bob, bob.Address, false, false)

	// Alice signs her own entry back to herself, outright, with her lock's key.
	// The signature is valid and the destination is not hers to choose.
	back := signBIP340Unlock(t, z, s.aliceKey, s.aliceLock, alice.Address)
	unlockPtlcAs(t, z, alice, s.aliceLock, back, constants.ErrPermissionDenied)
	// Bob likewise.
	unlockPtlcAs(t, z, bob, s.bobLock, signBIP340Unlock(t, z, s.bobKey, s.bobLock, bob.Address), constants.ErrPermissionDenied)
	// Bob does not hold Alice's key, so he cannot sign her entry to himself.
	unlockPtlcAs(t, z, bob, s.aliceLock, signBIP340Unlock(t, z, s.bobKey, s.aliceLock, bob.Address), constants.ErrInvalidPointSignature)
	// Neither takes its own back before its expiry.
	reclaimPtlcAs(t, z, alice, s.aliceLock, constants.ReclaimNotDue)
	reclaimPtlcAs(t, z, bob, s.bobLock, constants.ReclaimNotDue)

	z.ExpectBalance(types.PtlcContract, types.ZnnTokenStandard, 10*g.Zexp)
	z.ExpectBalance(types.PtlcContract, types.QsrTokenStandard, 100*g.Zexp)
}

// Bob accepts the terms and then never locks or pre-signs. Alice has locked and
// handed over a pre-signature that is of no use without t; she waits out her
// own expiry and takes her funds back.
func TestPtlc_keySwap_bobNeverLocks(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()
	activatePtlc(t, z)
	alice, bob := g.User1, g.User2

	var kt btcec.ModNScalar
	kt.SetInt(424242)
	point := pointFor(scalarBytes(&kt))
	aliceKey := keyWithParity(1001, false)
	aliceLock := createKeyPtlc(t, z, alice, xOnlyKey(aliceKey), bob.Address, int64(genesisTimestamp+3000), types.QsrTokenStandard, big.NewInt(100*g.Zexp))
	forBob := adaptorPreSign(t, aliceKey, ptlcUnlockMessage(z, definition.PointTypeBIP340, aliceLock, bob.Address), point, 31, false)

	// all Bob can do with what he holds
	unlockPtlcAs(t, z, bob, aliceLock, append(append([]byte{}, forBob[1:33]...), forBob[33:]...), constants.ErrInvalidPointSignature)
	reclaimPtlcAs(t, z, alice, aliceLock, constants.ReclaimNotDue)

	z.InsertMomentumsTo(310)
	reclaimPtlcAs(t, z, alice, aliceLock, nil)
	autoreceive(t, z, alice.Address)
	z.ExpectBalance(alice.Address, types.QsrTokenStandard, 120000*g.Zexp)
	z.ExpectBalance(types.PtlcContract, types.QsrTokenStandard, 0)
}

// Both lock and exchange pre-signatures, then Alice never claims. t stays with
// her, so neither entry can be opened, and each is reclaimed at its own expiry,
// Bob's first.
func TestPtlc_keySwap_aliceNeverClaims(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()
	activatePtlc(t, z)
	alice, bob := g.User1, g.User2
	s := newKeySwap(t, z, alice, bob, bob.Address, true, false)

	z.InsertMomentumsTo(310)
	reclaimPtlcAs(t, z, alice, s.aliceLock, constants.ReclaimNotDue)
	reclaimPtlcAs(t, z, bob, s.bobLock, nil)
	z.InsertMomentumsTo(610)
	reclaimPtlcAs(t, z, alice, s.aliceLock, nil)

	autoreceive(t, z, alice.Address)
	autoreceive(t, z, bob.Address)
	z.ExpectBalance(alice.Address, types.ZnnTokenStandard, 12000*g.Zexp)
	z.ExpectBalance(alice.Address, types.QsrTokenStandard, 120000*g.Zexp)
	z.ExpectBalance(bob.Address, types.ZnnTokenStandard, 8000*g.Zexp)
	z.ExpectBalance(bob.Address, types.QsrTokenStandard, 80000*g.Zexp)
}

// Alice claims after Bob's expiry. The contract refuses her, and her call is on
// the chain all the same with the completed signature in it: Bob extracts t,
// takes his ZNN back, and takes her QSR. A key lock opened from a pre-signature
// gives the secret away as surely as a point lock does, so the same rule holds:
// the holder of the secret must not unlock close to the deadline.
func TestPtlc_keySwap_lateClaimCostsTheClaimer(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()
	activatePtlc(t, z)
	alice, bob := g.User1, g.User2
	s := newKeySwap(t, z, alice, bob, bob.Address, false, true)

	// past Bob's expiry, before Alice's
	z.InsertMomentumsTo(310)
	aliceSig := adaptorComplete(s.forAlice, s.secret)
	unlockPtlcAs(t, z, alice, s.bobLock, aliceSig, constants.ErrExpired)

	learned := adaptorExtract(s.forAlice, aliceSig)
	if !bytes.Equal(pointFor(learned), s.point) {
		t.Fatal("the refused call does not give Bob the secret")
	}
	reclaimPtlcAs(t, z, bob, s.bobLock, nil)
	unlockPtlcAs(t, z, bob, s.aliceLock, adaptorComplete(s.forBob, learned), nil)

	autoreceive(t, z, bob.Address)
	z.ExpectBalance(bob.Address, types.ZnnTokenStandard, 8000*g.Zexp)
	z.ExpectBalance(bob.Address, types.QsrTokenStandard, 80100*g.Zexp)
	z.ExpectBalance(alice.Address, types.ZnnTokenStandard, 12000*g.Zexp)
	z.ExpectBalance(alice.Address, types.QsrTokenStandard, 119900*g.Zexp)
}

// The same swap with Alice's entry naming no destination, which the contract
// allows for a key lock. The key is hers, so she signs the entry back to
// herself and then claims Bob's. Bob learns t and completes a signature that
// would have paid him, for an entry that is gone. A client must refuse to lock
// against a key lock that does not name it (SPEC.md, the key swap's checks).
func TestPtlc_keySwap_openDestinationLosesBob(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()
	activatePtlc(t, z)
	alice, bob := g.User1, g.User2
	s := newKeySwap(t, z, alice, bob, types.ZeroAddress, false, false)

	unlockPtlcAs(t, z, alice, s.aliceLock, signBIP340Unlock(t, z, s.aliceKey, s.aliceLock, alice.Address), nil)
	aliceSig := adaptorComplete(s.forAlice, s.secret)
	unlockPtlcAs(t, z, alice, s.bobLock, aliceSig, nil)

	learned := adaptorExtract(s.forAlice, aliceSig)
	unlockPtlcAs(t, z, bob, s.aliceLock, adaptorComplete(s.forBob, learned), constants.ErrDataNonExistent)

	autoreceive(t, z, alice.Address)
	z.ExpectBalance(alice.Address, types.ZnnTokenStandard, 12010*g.Zexp)
	z.ExpectBalance(alice.Address, types.QsrTokenStandard, 120000*g.Zexp)
	z.ExpectBalance(bob.Address, types.ZnnTokenStandard, 7990*g.Zexp)
	z.ExpectBalance(bob.Address, types.QsrTokenStandard, 80000*g.Zexp)
}
