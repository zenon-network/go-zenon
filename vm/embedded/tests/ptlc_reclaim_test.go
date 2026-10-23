package tests

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha512"
	"math/big"
	"strings"
	"testing"

	"filippo.io/edwards25519"

	g "github.com/zenon-network/go-zenon/chain/genesis/mock"
	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/rpc/api/embedded"
	rpc "github.com/zenon-network/go-zenon/rpc/server"
	"github.com/zenon-network/go-zenon/vm/constants"
	"github.com/zenon-network/go-zenon/vm/embedded/definition"
	"github.com/zenon-network/go-zenon/wallet"
	"github.com/zenon-network/go-zenon/zenon/mock"
)

type edKey struct {
	secret *edwards25519.Scalar
	public []byte
}

func edScalarFrom(label string) *edwards25519.Scalar {
	digest := sha512.Sum512([]byte(label))
	s, err := edwards25519.NewScalar().SetUniformBytes(digest[:])
	if err != nil {
		panic(err)
	}
	return s
}

func newEdKey(label string) *edKey {
	s := edScalarFrom(label)
	return &edKey{secret: s, public: new(edwards25519.Point).ScalarBaseMult(s).Bytes()}
}

func edChallenge(r, public, message []byte) *edwards25519.Scalar {
	h := sha512.New()
	h.Write(r)
	h.Write(public)
	h.Write(message)
	k, err := edwards25519.NewScalar().SetUniformBytes(h.Sum(nil))
	if err != nil {
		panic(err)
	}
	return k
}

func edAdaptorPreSign(key *edKey, message []byte, adaptor *edKey) []byte {
	return edAdaptorPreSignWithNonceOffset(key, message, adaptor, edwards25519.NewIdentityPoint())
}

func edAdaptorPreSignWithNonceOffset(key *edKey, message []byte, adaptor *edKey, offset *edwards25519.Point) []byte {
	nonce := sha512.New()
	nonce.Write([]byte("adaptor-nonce"))
	nonce.Write(key.secret.Bytes())
	nonce.Write(adaptor.public)
	nonce.Write(offset.Bytes())
	nonce.Write(message)
	r, err := edwards25519.NewScalar().SetUniformBytes(nonce.Sum(nil))
	if err != nil {
		panic(err)
	}
	adaptorPoint, err := new(edwards25519.Point).SetBytes(adaptor.public)
	if err != nil {
		panic(err)
	}
	R := new(edwards25519.Point).Add(new(edwards25519.Point).Add(new(edwards25519.Point).ScalarBaseMult(r), adaptorPoint), offset).Bytes()
	k := edChallenge(R, key.public, message)
	s := edwards25519.NewScalar().MultiplyAdd(k, key.secret, r)
	return append(R, s.Bytes()...)
}

func edAdaptorVerify(public, message, adaptorPublic, presig []byte) bool {
	return edAdaptorCheck(public, message, adaptorPublic, presig, false)
}

func edAdaptorVerifyCofactored(public, message, adaptorPublic, presig []byte) bool {
	return edAdaptorCheck(public, message, adaptorPublic, presig, true)
}

func edAdaptorCheck(public, message, adaptorPublic, presig []byte, cofactored bool) bool {
	A, err := new(edwards25519.Point).SetBytes(public)
	if err != nil {
		return false
	}
	T, err := new(edwards25519.Point).SetBytes(adaptorPublic)
	if err != nil {
		return false
	}
	R, err := new(edwards25519.Point).SetBytes(presig[:32])
	if err != nil {
		return false
	}
	s, err := edwards25519.NewScalar().SetCanonicalBytes(presig[32:])
	if err != nil {
		return false
	}
	k := edChallenge(presig[:32], public, message)
	lhs := new(edwards25519.Point).ScalarBaseMult(s)
	rhs := new(edwards25519.Point).Add(new(edwards25519.Point).Subtract(R, T), new(edwards25519.Point).ScalarMult(k, A))
	if cofactored {
		lhs.MultByCofactor(lhs)
		rhs.MultByCofactor(rhs)
	}
	return lhs.Equal(rhs) == 1
}

func edAdaptorComplete(presig []byte, secret *edwards25519.Scalar) []byte {
	sPre, err := edwards25519.NewScalar().SetCanonicalBytes(presig[32:])
	if err != nil {
		panic(err)
	}
	s := edwards25519.NewScalar().Add(sPre, secret)
	return append(append([]byte{}, presig[:32]...), s.Bytes()...)
}

func edAdaptorExtract(presig, signature []byte) []byte {
	sPre, err := edwards25519.NewScalar().SetCanonicalBytes(presig[32:])
	if err != nil {
		panic(err)
	}
	s, err := edwards25519.NewScalar().SetCanonicalBytes(signature[32:])
	if err != nil {
		panic(err)
	}
	return edwards25519.NewScalar().Subtract(s, sPre).Bytes()
}

func expectGoZenonAccepts(t *testing.T, public, message, signature []byte) {
	t.Helper()
	valid, err := wallet.VerifySignature(ed25519.PublicKey(public), message, signature)
	common.FailIfErr(t, err)
	if !valid || !ed25519.Verify(public, message, signature) {
		t.Fatal("the completed adaptor signature does not verify as Ed25519")
	}
}

func ptlcReclaimMessage(z mock.MockZenon, pointType uint8, id types.Hash, creator types.Address) []byte {
	return definition.GetPtlcReclaimMessage(z.Chain().ChainIdentifier(), pointType, id, creator)
}

func createReclaimLockedPtlc(t *testing.T, z mock.MockZenon, creator *wallet.KeyPair, claimLock []byte, destination types.Address, reclaimPointType uint8, reclaimLock []byte, expirationTime int64, amount *big.Int) types.Hash {
	t.Helper()
	block := z.InsertSendBlock(&nom.AccountBlock{
		Address:   creator.Address,
		ToAddress: types.PtlcContract,
		Data: definition.ABIPtlc.PackMethodPanic(definition.CreateWithReclaimLockPtlcMethodName,
			expirationTime,
			definition.PointTypeED25519,
			claimLock,
			destination,
			reclaimPointType,
			reclaimLock,
		),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        amount,
	}, nil, mock.SkipVmChanges)
	z.InsertNewMomentum()
	z.InsertNewMomentum()
	return block.Hash
}

func reclaimWithSignatureAs(t *testing.T, z mock.MockZenon, caller *wallet.KeyPair, id types.Hash, signature []byte, expected error) []byte {
	t.Helper()
	call := z.CallContract(&nom.AccountBlock{
		Address:   caller.Address,
		ToAddress: types.PtlcContract,
		Data:      definition.ABIPtlc.PackMethodPanic(definition.ReclaimWithSignaturePtlcMethodName, id, signature),
	})
	published, err := z.Chain().GetFrontierAccountStore(caller.Address).Frontier()
	common.FailIfErr(t, err)
	z.InsertNewMomentum()
	z.InsertNewMomentum()
	call.Error(t, expected)
	return published.Data
}

func lastSendData(t *testing.T, z mock.MockZenon, address types.Address) []byte {
	t.Helper()
	block, err := z.Chain().GetFrontierAccountStore(address).Frontier()
	common.FailIfErr(t, err)
	return block.Data
}

type moneroSwap struct {
	alice, bob            *wallet.KeyPair
	aliceKey, bobKey      *edKey
	aliceShare, bobShare  *edKey
	id                    types.Hash
	expiry                int64
	reclaimPresig         []byte
	reclaimMsg, unlockMsg []byte
	unlockPresig          []byte
}

func newMoneroSwap(t *testing.T, z mock.MockZenon, label string) *moneroSwap {
	t.Helper()
	s := &moneroSwap{
		alice:      g.User1,
		bob:        g.User2,
		aliceKey:   newEdKey(label + "/alice ptlc key"),
		bobKey:     newEdKey(label + "/bob ptlc key"),
		aliceShare: newEdKey(label + "/alice monero share s_a"),
		bobShare:   newEdKey(label + "/bob monero share s_b"),
		expiry:     int64(genesisTimestamp + 600),
	}
	s.id = createReclaimLockedPtlc(t, z, s.bob, s.bobKey.public, s.alice.Address, definition.PointTypeED25519, s.aliceKey.public, s.expiry, big.NewInt(10*g.Zexp))

	s.reclaimMsg = ptlcReclaimMessage(z, definition.PointTypeED25519, s.id, s.bob.Address)
	s.reclaimPresig = edAdaptorPreSign(s.aliceKey, s.reclaimMsg, s.bobShare)
	if !edAdaptorVerify(s.aliceKey.public, s.reclaimMsg, s.bobShare.public, s.reclaimPresig) {
		t.Fatal("Bob cannot verify Alice's reclaim pre-signature")
	}

	s.unlockMsg = ptlcUnlockMessage(z, definition.PointTypeED25519, s.id, s.alice.Address)
	s.unlockPresig = edAdaptorPreSign(s.bobKey, s.unlockMsg, s.aliceShare)
	if !edAdaptorVerify(s.bobKey.public, s.unlockMsg, s.aliceShare.public, s.unlockPresig) {
		t.Fatal("Alice cannot verify Bob's unlock pre-signature")
	}
	return s
}

func TestPtlc_reclaimLock_bobWithholdsAndRevealsHisShare(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()
	activatePtlc(t, z)
	s := newMoneroSwap(t, z, "withhold")
	z.ExpectBalance(types.PtlcContract, types.ZnnTokenStandard, 10*g.Zexp)

	reclaimWithSignatureAs(t, z, s.bob, s.id, edAdaptorComplete(s.reclaimPresig, s.bobShare.secret), constants.ReclaimNotDue)
	z.InsertMomentumsTo(70)

	reclaimPtlcAs(t, z, s.bob, s.id, constants.ErrReclaimSignatureRequired)
	reclaimWithSignatureAs(t, z, s.bob, s.id, s.reclaimPresig, constants.ErrInvalidPointSignature)
	reclaimWithSignatureAs(t, z, s.bob, s.id, edAdaptorComplete(s.reclaimPresig, s.aliceShare.secret), constants.ErrInvalidPointSignature)
	unlockPtlcAs(t, z, s.alice, s.id, edAdaptorComplete(s.unlockPresig, s.aliceShare.secret), constants.ErrExpired)

	completed := edAdaptorComplete(s.reclaimPresig, s.bobShare.secret)
	expectGoZenonAccepts(t, s.aliceKey.public, s.reclaimMsg, completed)
	reclaimWithSignatureAs(t, z, s.alice, s.id, completed, constants.ErrPermissionDenied)
	published := reclaimWithSignatureAs(t, z, s.bob, s.id, completed, nil)
	autoreceive(t, z, s.bob.Address)
	z.ExpectBalance(types.PtlcContract, types.ZnnTokenStandard, 0)
	z.ExpectBalance(s.bob.Address, types.ZnnTokenStandard, 8000*g.Zexp)
	z.ExpectBalance(s.alice.Address, types.ZnnTokenStandard, 12000*g.Zexp)

	param := new(definition.ReclaimWithSignaturePtlcParam)
	common.FailIfErr(t, definition.ABIPtlc.UnpackMethod(param, definition.ReclaimWithSignaturePtlcMethodName, published))
	if param.Id != s.id {
		t.Fatalf("published reclaim names %v, want %v", param.Id, s.id)
	}
	learned := edAdaptorExtract(s.reclaimPresig, param.Signature)
	if !bytes.Equal(learned, s.bobShare.secret.Bytes()) {
		t.Fatal("the reclaim Bob published does not give Alice his share")
	}
	learnedScalar, err := edwards25519.NewScalar().SetCanonicalBytes(learned)
	common.FailIfErr(t, err)
	if !bytes.Equal(new(edwards25519.Point).ScalarBaseMult(learnedScalar).Bytes(), s.bobShare.public) {
		t.Fatal("the extracted share does not match S_b")
	}

	reclaimWithSignatureAs(t, z, s.bob, s.id, completed, constants.ErrDataNonExistent)
	unlockPtlcAs(t, z, s.alice, s.id, edAdaptorComplete(s.unlockPresig, s.aliceShare.secret), constants.ErrDataNonExistent)
}

func TestPtlc_reclaimLock_aliceClaimsAndRevealsHerShare(t *testing.T) {
	for name, proxy := range map[string]bool{"unlock": false, "proxy unlock": true} {
		t.Run(name, func(t *testing.T) {
			aliceClaims(t, proxy)
		})
	}
}

func aliceClaims(t *testing.T, proxy bool) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()
	activatePtlc(t, z)
	s := newMoneroSwap(t, z, "claim")

	unlockPtlcAs(t, z, s.alice, s.id, s.unlockPresig, constants.ErrInvalidPointSignature)
	completed := edAdaptorComplete(s.unlockPresig, s.aliceShare.secret)
	expectGoZenonAccepts(t, s.bobKey.public, s.unlockMsg, completed)

	var published []byte
	if proxy {
		proxyUnlockPtlcAs(t, z, g.User3, s.id, s.alice.Address, completed, nil)
		param := new(definition.ProxyUnlockPtlcParam)
		common.FailIfErr(t, definition.ABIPtlc.UnpackMethod(param, definition.ProxyUnlockPtlcMethodName, lastSendData(t, z, g.User3.Address)))
		published = param.Signature
	} else {
		unlockPtlcAs(t, z, s.alice, s.id, completed, nil)
		param := new(definition.UnlockPtlcParam)
		common.FailIfErr(t, definition.ABIPtlc.UnpackMethod(param, definition.UnlockPtlcMethodName, lastSendData(t, z, s.alice.Address)))
		published = param.Signature
	}
	autoreceive(t, z, s.alice.Address)
	z.ExpectBalance(types.PtlcContract, types.ZnnTokenStandard, 0)
	z.ExpectBalance(s.alice.Address, types.ZnnTokenStandard, 12010*g.Zexp)
	z.ExpectBalance(s.bob.Address, types.ZnnTokenStandard, 7990*g.Zexp)

	if !bytes.Equal(edAdaptorExtract(s.unlockPresig, published), s.aliceShare.secret.Bytes()) {
		t.Fatal("the unlock Alice published does not give Bob her share")
	}

	_, err := embedded.NewPtlcApi(z).GetReclaimLockById(s.id)
	common.ExpectError(t, err, constants.ErrDataNonExistent)

	z.InsertMomentumsTo(70)
	reclaimWithSignatureAs(t, z, s.bob, s.id, edAdaptorComplete(s.reclaimPresig, s.bobShare.secret), constants.ErrDataNonExistent)
	reclaimPtlcAs(t, z, s.bob, s.id, constants.ErrDataNonExistent)
}

func TestPtlc_reclaimLock_termsAliceMustCheck(t *testing.T) {
	bob := g.User2
	aliceKey := newEdKey("terms/alice")
	expiry := int64(genesisTimestamp + 600)

	for _, test := range []struct {
		name        string
		destination types.Address
		reclaimLock []byte
		bobOut      func(t *testing.T, z mock.MockZenon, id types.Hash)
	}{
		{"open destination", types.ZeroAddress, aliceKey.public, func(t *testing.T, z mock.MockZenon, id types.Hash) {
			unlockPtlcAs(t, z, bob, id, bob.Sign(ptlcUnlockMessage(z, definition.PointTypeED25519, id, bob.Address)), nil)
		}},
		{"destination is bob", bob.Address, aliceKey.public, func(t *testing.T, z mock.MockZenon, id types.Hash) {
			unlockPtlcAs(t, z, bob, id, bob.Sign(ptlcUnlockMessage(z, definition.PointTypeED25519, id, bob.Address)), nil)
		}},
		{"reclaim lock is bob's", g.User1.Address, bob.Public, func(t *testing.T, z mock.MockZenon, id types.Hash) {
			z.InsertMomentumsTo(70)
			reclaimWithSignatureAs(t, z, bob, id, bob.Sign(ptlcReclaimMessage(z, definition.PointTypeED25519, id, bob.Address)), nil)
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			z := mock.NewMockZenon(t)
			defer z.StopPanic()
			activatePtlc(t, z)

			id := createReclaimLockedPtlc(t, z, bob, bob.Public, test.destination, definition.PointTypeED25519, test.reclaimLock, expiry, big.NewInt(10*g.Zexp))
			test.bobOut(t, z, id)

			autoreceive(t, z, bob.Address)
			z.ExpectBalance(bob.Address, types.ZnnTokenStandard, 8000*g.Zexp)
			z.ExpectBalance(types.PtlcContract, types.ZnnTokenStandard, 0)
			_, err := embedded.NewPtlcApi(z).GetReclaimLockById(id)
			common.ExpectError(t, err, constants.ErrDataNonExistent)
		})
	}
}

func TestPtlc_reclaimLock_earlyReclaimCostsBob(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()
	activatePtlc(t, z)
	s := newMoneroSwap(t, z, "early")

	completed := edAdaptorComplete(s.reclaimPresig, s.bobShare.secret)
	published := reclaimWithSignatureAs(t, z, s.bob, s.id, completed, constants.ReclaimNotDue)
	param := new(definition.ReclaimWithSignaturePtlcParam)
	common.FailIfErr(t, definition.ABIPtlc.UnpackMethod(param, definition.ReclaimWithSignaturePtlcMethodName, published))
	if !bytes.Equal(edAdaptorExtract(s.reclaimPresig, param.Signature), s.bobShare.secret.Bytes()) {
		t.Fatal("the refused reclaim does not give Alice Bob's share")
	}

	unlockPtlcAs(t, z, s.alice, s.id, edAdaptorComplete(s.unlockPresig, s.aliceShare.secret), nil)
	autoreceive(t, z, s.alice.Address)
	z.ExpectBalance(s.alice.Address, types.ZnnTokenStandard, 12010*g.Zexp)

	z.InsertMomentumsTo(70)
	reclaimWithSignatureAs(t, z, s.bob, s.id, completed, constants.ErrDataNonExistent)
	z.ExpectBalance(s.bob.Address, types.ZnnTokenStandard, 7990*g.Zexp)
	z.ExpectBalance(types.PtlcContract, types.ZnnTokenStandard, 0)
}

func TestPtlc_reclaimLock_unusablePresignatureLocksBobOut(t *testing.T) {
	order2Encoding := append(bytes.Repeat([]byte{0xff}, 31), 0x7f)
	order2Encoding[0] = 0xec
	order2, err := new(edwards25519.Point).SetBytes(order2Encoding)
	common.FailIfErr(t, err)

	for _, test := range []struct {
		name      string
		signature func(t *testing.T, s *moneroSwap) []byte
	}{
		{"no pre-signature", func(t *testing.T, s *moneroSwap) []byte {
			return edAdaptorComplete(edAdaptorPreSign(s.bobKey, s.reclaimMsg, s.bobShare), s.bobShare.secret)
		}},
		{"torsioned nonce", func(t *testing.T, s *moneroSwap) []byte {
			presig := edAdaptorPreSignWithNonceOffset(s.aliceKey, s.reclaimMsg, s.bobShare, order2)
			if !edAdaptorVerifyCofactored(s.aliceKey.public, s.reclaimMsg, s.bobShare.public, presig) {
				t.Fatal("the torsioned pre-signature fails the cofactored check")
			}
			if edAdaptorVerify(s.aliceKey.public, s.reclaimMsg, s.bobShare.public, presig) {
				t.Fatal("the torsioned pre-signature passes the check the contract makes")
			}
			return edAdaptorComplete(presig, s.bobShare.secret)
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			z := mock.NewMockZenon(t)
			defer z.StopPanic()
			activatePtlc(t, z)
			s := newMoneroSwap(t, z, "locked out/"+test.name)

			signature := test.signature(t, s)
			if ed25519.Verify(s.aliceKey.public, s.reclaimMsg, signature) {
				t.Fatal("Bob holds a reclaim signature under Alice's key")
			}

			z.InsertMomentumsTo(70)
			reclaimWithSignatureAs(t, z, s.bob, s.id, signature, constants.ErrInvalidPointSignature)
			reclaimPtlcAs(t, z, s.bob, s.id, constants.ErrReclaimSignatureRequired)

			z.ExpectBalance(types.PtlcContract, types.ZnnTokenStandard, 10*g.Zexp)
			z.ExpectBalance(s.bob.Address, types.ZnnTokenStandard, 7990*g.Zexp)
			ptlcApi := embedded.NewPtlcApi(z)
			_, err := ptlcApi.GetById(s.id)
			common.FailIfErr(t, err)
			_, err = ptlcApi.GetReclaimLockById(s.id)
			common.FailIfErr(t, err)
		})
	}
}

func TestPtlc_reclaimLock_signatureNotReplayable(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()
	activatePtlc(t, z)

	aliceKey := newEdKey("replay/alice")
	bobKey := newEdKey("replay/bob")
	share := newEdKey("replay/s_b")
	expiry := int64(genesisTimestamp + 600)
	first := createReclaimLockedPtlc(t, z, g.User2, bobKey.public, g.User1.Address, definition.PointTypeED25519, aliceKey.public, expiry, big.NewInt(10*g.Zexp))
	second := createReclaimLockedPtlc(t, z, g.User2, bobKey.public, g.User1.Address, definition.PointTypeED25519, aliceKey.public, expiry, big.NewInt(10*g.Zexp))
	z.ExpectBalance(types.PtlcContract, types.ZnnTokenStandard, 20*g.Zexp)

	firstSignature := edAdaptorComplete(edAdaptorPreSign(aliceKey, ptlcReclaimMessage(z, definition.PointTypeED25519, first, g.User2.Address), share), share.secret)
	z.InsertMomentumsTo(70)

	reclaimWithSignatureAs(t, z, g.User2, second, firstSignature, constants.ErrInvalidPointSignature)
	reclaimWithSignatureAs(t, z, g.User2, first, firstSignature, nil)
	reclaimWithSignatureAs(t, z, g.User2, second, firstSignature, constants.ErrInvalidPointSignature)
	z.ExpectBalance(types.PtlcContract, types.ZnnTokenStandard, 10*g.Zexp)

	unlockAsReclaim := edAdaptorComplete(edAdaptorPreSign(aliceKey, ptlcUnlockMessage(z, definition.PointTypeED25519, second, g.User2.Address), share), share.secret)
	reclaimWithSignatureAs(t, z, g.User2, second, unlockAsReclaim, constants.ErrInvalidPointSignature)

	secondSignature := edAdaptorComplete(edAdaptorPreSign(aliceKey, ptlcReclaimMessage(z, definition.PointTypeED25519, second, g.User2.Address), share), share.secret)
	reclaimWithSignatureAs(t, z, g.User2, second, secondSignature, nil)
	z.ExpectBalance(types.PtlcContract, types.ZnnTokenStandard, 0)
}

func TestPtlc_reclaimLock_legacyEntriesUnchanged(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()
	activatePtlc(t, z)

	id := createED25519Ptlc(t, z, g.User2.Public, int64(genesisTimestamp+600), types.ZnnTokenStandard, big.NewInt(10*g.Zexp))
	z.InsertNewMomentum()
	_, err := embedded.NewPtlcApi(z).GetReclaimLockById(id)
	common.ExpectError(t, err, constants.ErrDataNonExistent)

	z.InsertMomentumsTo(70)
	signature := g.User1.Sign(ptlcReclaimMessage(z, definition.PointTypeED25519, id, g.User1.Address))
	reclaimWithSignatureAs(t, z, g.User1, id, signature, constants.ErrNoReclaimLock)
	reclaimPtlcAs(t, z, g.User2, id, constants.ErrPermissionDenied)
	reclaimPtlcAs(t, z, g.User1, id, nil)
	autoreceive(t, z, g.User1.Address)
	z.ExpectBalance(g.User1.Address, types.ZnnTokenStandard, 12000*g.Zexp)
	z.ExpectBalance(types.PtlcContract, types.ZnnTokenStandard, 0)
}

func TestPtlc_reclaimLock_bip340(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()
	activatePtlc(t, z)

	reclaimKey := keyWithParity(3003, true)
	id := createReclaimLockedPtlc(t, z, g.User2, newEdKey("bip340/bob").public, g.User1.Address, definition.PointTypeBIP340, xOnlyKey(reclaimKey), int64(genesisTimestamp+600), big.NewInt(10*g.Zexp))
	z.InsertMomentumsTo(70)

	var secret [32]byte
	secret[31] = 77
	message := ptlcReclaimMessage(z, definition.PointTypeBIP340, id, g.User2.Address)
	presig := adaptorPreSign(t, reclaimKey, message, pointFor(secret[:]), 91, true)
	signature := adaptorComplete(presig, secret[:])
	reclaimWithSignatureAs(t, z, g.User2, id, append(append([]byte{}, presig[1:33]...), presig[33:]...), constants.ErrInvalidPointSignature)
	published := reclaimWithSignatureAs(t, z, g.User2, id, signature, nil)

	param := new(definition.ReclaimWithSignaturePtlcParam)
	common.FailIfErr(t, definition.ABIPtlc.UnpackMethod(param, definition.ReclaimWithSignaturePtlcMethodName, published))
	if !bytes.Equal(adaptorExtract(presig, param.Signature), secret[:]) {
		t.Fatal("the BIP-340 reclaim does not reveal the adaptor secret")
	}
	autoreceive(t, z, g.User2.Address)
	z.ExpectBalance(g.User2.Address, types.ZnnTokenStandard, 8000*g.Zexp)
	z.ExpectBalance(types.PtlcContract, types.ZnnTokenStandard, 0)
}

func TestPtlc_reclaimLock_sendBlockRefusals(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()

	create := func(reclaimPointType uint8, reclaimLock []byte) *nom.AccountBlock {
		return &nom.AccountBlock{
			Address:   g.User1.Address,
			ToAddress: types.PtlcContract,
			Data: definition.ABIPtlc.PackMethodPanic(definition.CreateWithReclaimLockPtlcMethodName,
				int64(genesisTimestamp+600),
				definition.PointTypeED25519,
				g.User2.Public,
				types.ZeroAddress,
				reclaimPointType,
				reclaimLock,
			),
			TokenStandard: types.ZnnTokenStandard,
			Amount:        big.NewInt(10 * g.Zexp),
		}
	}
	reclaim := &nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.PtlcContract,
		Data:      definition.ABIPtlc.PackMethodPanic(definition.ReclaimWithSignaturePtlcMethodName, types.NewHash([]byte("any")), make([]byte, 64)),
	}

	z.InsertSendBlock(create(definition.PointTypeED25519, g.User1.Public), constants.ErrContractDoesntExist, mock.NoVmChanges)
	z.InsertSendBlock(reclaim, constants.ErrContractDoesntExist, mock.NoVmChanges)

	activatePtlc(t, z)

	identity := append([]byte{1}, make([]byte, 31)...)
	order2 := append(bytes.Repeat([]byte{0xff}, 31), 0x7f)
	order2[0] = 0xec
	z.InsertSendBlock(create(definition.PointTypeED25519, identity), constants.ErrInvalidPointLock, mock.NoVmChanges)
	z.InsertSendBlock(create(definition.PointTypeED25519, order2), constants.ErrInvalidPointLock, mock.NoVmChanges)
	z.InsertSendBlock(create(definition.PointTypeED25519, bytes.Repeat([]byte{0x02}, 32)), constants.ErrInvalidPointLock, mock.NoVmChanges)
	z.InsertSendBlock(create(definition.PointTypeED25519, g.User1.Public[1:]), constants.ErrInvalidPointLock, mock.NoVmChanges)
	z.InsertSendBlock(create(definition.PointTypeBIP340, bytes.Repeat([]byte{0xff}, 32)), constants.ErrInvalidPointLock, mock.NoVmChanges)
	z.InsertSendBlock(create(definition.PointTypeSecp256k1Point, pointFor(bytes.Repeat([]byte{1}, 32))), constants.ErrInvalidPointType, mock.NoVmChanges)
	z.InsertSendBlock(create(9, g.User1.Public), constants.ErrInvalidPointType, mock.NoVmChanges)

	for _, size := range []int{0, 32, 63, 65} {
		z.InsertSendBlock(&nom.AccountBlock{
			Address:   g.User1.Address,
			ToAddress: types.PtlcContract,
			Data:      definition.ABIPtlc.PackMethodPanic(definition.ReclaimWithSignaturePtlcMethodName, types.NewHash([]byte("any")), make([]byte, size)),
		}, constants.ErrInvalidPointSignature, mock.NoVmChanges)
	}
	z.InsertNewMomentum()
	z.ExpectBalance(types.PtlcContract, types.ZnnTokenStandard, 0)

	z.InsertSendBlock(create(definition.PointTypeED25519, g.User1.Public), nil, mock.SkipVmChanges)
	z.InsertNewMomentum()
	z.InsertNewMomentum()
	z.ExpectBalance(types.PtlcContract, types.ZnnTokenStandard, 10*g.Zexp)
}

func TestPtlc_getReclaimLockByIdOverJsonRpc(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()
	activatePtlc(t, z)

	server := rpc.NewServer()
	defer server.Stop()
	common.FailIfErr(t, server.RegisterName("embedded.ptlc", embedded.NewPtlcApi(z)))
	client := rpc.DialInProc(server)
	defer client.Close()

	aliceKey := newEdKey("rpc/alice")
	id := createReclaimLockedPtlc(t, z, g.User2, newEdKey("rpc/bob").public, g.User1.Address, definition.PointTypeED25519, aliceKey.public, int64(genesisTimestamp+600), big.NewInt(10*g.Zexp))

	var lock definition.PtlcReclaimLock
	common.FailIfErr(t, client.Call(&lock, "embedded.ptlc.getReclaimLockById", id.String()))
	if lock.Id != id || lock.PointType != definition.PointTypeED25519 || !bytes.Equal(lock.PointLock, aliceKey.public) {
		t.Fatalf("unexpected reclaim lock over the transport: %s", lock)
	}

	var entry map[string]interface{}
	common.FailIfErr(t, client.Call(&entry, "embedded.ptlc.getById", id.String()))
	if entry["id"] != id.String() || entry["amount"] != "1000000000" || entry["timeLocked"] != g.User2.Address.String() {
		t.Fatalf("unexpected entry over the transport: %v", entry)
	}
	if _, ok := entry["reclaimPointLock"]; ok {
		t.Fatalf("getById changed shape: %v", entry)
	}

	legacy := createED25519Ptlc(t, z, g.User2.Public, int64(genesisTimestamp+600), types.ZnnTokenStandard, big.NewInt(10*g.Zexp))
	var missing map[string]interface{}
	err := client.Call(&missing, "embedded.ptlc.getReclaimLockById", legacy.String())
	if err == nil || err.Error() != constants.ErrDataNonExistent.Error() {
		t.Fatalf("legacy entry: answered %v, %v", missing, err)
	}
	err = client.Call(&missing, "embedded.ptlc.getReclaimLockById", "not-a-hash")
	if err == nil || !strings.Contains(err.Error(), "invalid argument") {
		t.Fatalf("bad id: answered %v, %v", missing, err)
	}
}
