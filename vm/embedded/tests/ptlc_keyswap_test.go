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

type keySwap struct {
	secret, point          []byte
	aliceKey, bobKey       *btcec.PrivateKey
	aliceLock, bobLock     types.Hash
	forBob, forAlice       []byte
	aliceMsg, bobMsg       []byte
	aliceExpiry, bobExpiry int64
}

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

			unlockPtlcAs(t, z, bob, s.aliceLock, append(append([]byte{}, s.forBob[1:33]...), s.forBob[33:]...), constants.ErrInvalidPointSignature)

			aliceSig := adaptorComplete(s.forAlice, s.secret)
			unlockPtlcAs(t, z, alice, s.bobLock, aliceSig, nil)
			autoreceive(t, z, alice.Address)
			z.ExpectBalance(alice.Address, types.ZnnTokenStandard, 12010*g.Zexp)

			learned := adaptorExtract(s.forAlice, aliceSig)
			if !bytes.Equal(learned, s.secret) {
				t.Fatal("the signature Alice published does not give Bob the secret")
			}
			if !bytes.Equal(pointFor(learned), s.point) {
				t.Fatal("what Bob extracted is not the scalar behind T")
			}

			proxyUnlockPtlcAs(t, z, g.User3, s.aliceLock, bob.Address, aliceSig, constants.ErrInvalidPointSignature)

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

func TestPtlc_keySwap_keysPayOnlyTheCounterparty(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()
	activatePtlc(t, z)
	alice, bob := g.User1, g.User2
	s := newKeySwap(t, z, alice, bob, bob.Address, false, false)

	back := signBIP340Unlock(t, z, s.aliceKey, s.aliceLock, alice.Address)
	unlockPtlcAs(t, z, alice, s.aliceLock, back, constants.ErrPermissionDenied)
	unlockPtlcAs(t, z, bob, s.bobLock, signBIP340Unlock(t, z, s.bobKey, s.bobLock, bob.Address), constants.ErrPermissionDenied)
	unlockPtlcAs(t, z, bob, s.aliceLock, signBIP340Unlock(t, z, s.bobKey, s.aliceLock, bob.Address), constants.ErrInvalidPointSignature)
	reclaimPtlcAs(t, z, alice, s.aliceLock, constants.ReclaimNotDue)
	reclaimPtlcAs(t, z, bob, s.bobLock, constants.ReclaimNotDue)

	z.ExpectBalance(types.PtlcContract, types.ZnnTokenStandard, 10*g.Zexp)
	z.ExpectBalance(types.PtlcContract, types.QsrTokenStandard, 100*g.Zexp)
}

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

	unlockPtlcAs(t, z, bob, aliceLock, append(append([]byte{}, forBob[1:33]...), forBob[33:]...), constants.ErrInvalidPointSignature)
	reclaimPtlcAs(t, z, alice, aliceLock, constants.ReclaimNotDue)

	z.InsertMomentumsTo(310)
	reclaimPtlcAs(t, z, alice, aliceLock, nil)
	autoreceive(t, z, alice.Address)
	z.ExpectBalance(alice.Address, types.QsrTokenStandard, 120000*g.Zexp)
	z.ExpectBalance(types.PtlcContract, types.QsrTokenStandard, 0)
}

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

func TestPtlc_keySwap_lateClaimCostsTheClaimer(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()
	activatePtlc(t, z)
	alice, bob := g.User1, g.User2
	s := newKeySwap(t, z, alice, bob, bob.Address, false, true)

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
