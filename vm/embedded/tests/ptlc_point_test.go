package tests

import (
	"bytes"
	"math/big"
	"testing"

	"github.com/btcsuite/btcd/btcec/v2"

	g "github.com/zenon-network/go-zenon/chain/genesis/mock"
	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/rpc/api/embedded"
	"github.com/zenon-network/go-zenon/vm/constants"
	"github.com/zenon-network/go-zenon/vm/embedded/definition"
	"github.com/zenon-network/go-zenon/wallet"
	"github.com/zenon-network/go-zenon/zenon/mock"
)

func scalarBytes(k *btcec.ModNScalar) []byte {
	b := k.Bytes()
	return b[:]
}

func pointFor(t []byte) []byte {
	var k btcec.ModNScalar
	k.SetByteSlice(t)
	var p btcec.JacobianPoint
	btcec.ScalarBaseMultNonConst(&k, &p)
	p.ToAffine()
	return btcec.NewPublicKey(&p.X, &p.Y).SerializeCompressed()
}

func addScalars(a, b []byte) []byte {
	var ka, kb btcec.ModNScalar
	ka.SetByteSlice(a)
	kb.SetByteSlice(b)
	ka.Add(&kb)
	return scalarBytes(&ka)
}

func createPointPtlc(t *testing.T, z mock.MockZenon, creator *wallet.KeyPair, point []byte, destination types.Address, expirationTime int64, token types.ZenonTokenStandard, amount *big.Int, expected error) types.Hash {
	t.Helper()
	block := z.InsertSendBlock(&nom.AccountBlock{
		Address:   creator.Address,
		ToAddress: types.PtlcContract,
		Data: definition.ABIPtlc.PackMethodPanic(definition.CreatePtlcMethodName,
			expirationTime,
			definition.PointTypeSecp256k1Point,
			point,
			destination,
		),
		TokenStandard: token,
		Amount:        amount,
	}, expected, mock.SkipVmChanges)
	z.InsertNewMomentum()
	z.InsertNewMomentum()
	if block == nil {
		return types.Hash{}
	}
	return block.Hash
}

func unlockPtlcAs(t *testing.T, z mock.MockZenon, caller *wallet.KeyPair, id types.Hash, witness []byte, expected error) {
	t.Helper()
	call := z.CallContract(&nom.AccountBlock{
		Address:   caller.Address,
		ToAddress: types.PtlcContract,
		Data: definition.ABIPtlc.PackMethodPanic(definition.UnlockPtlcMethodName,
			id,
			witness,
		),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(0),
	})
	z.InsertNewMomentum()
	z.InsertNewMomentum()
	call.Error(t, expected)
}

func unlockPtlcRefusedAtSend(t *testing.T, z mock.MockZenon, caller *wallet.KeyPair, id types.Hash, witness []byte) {
	t.Helper()
	z.InsertSendBlock(&nom.AccountBlock{
		Address:   caller.Address,
		ToAddress: types.PtlcContract,
		Data: definition.ABIPtlc.PackMethodPanic(definition.UnlockPtlcMethodName,
			id,
			witness,
		),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(0),
	}, constants.ErrInvalidPointSignature, mock.NoVmChanges)
}

func proxyUnlockPtlcAs(t *testing.T, z mock.MockZenon, caller *wallet.KeyPair, id types.Hash, destination types.Address, witness []byte, expected error) {
	t.Helper()
	call := z.CallContract(&nom.AccountBlock{
		Address:   caller.Address,
		ToAddress: types.PtlcContract,
		Data: definition.ABIPtlc.PackMethodPanic(definition.ProxyUnlockPtlcMethodName,
			id,
			destination,
			witness,
		),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(0),
	})
	z.InsertNewMomentum()
	z.InsertNewMomentum()
	call.Error(t, expected)
}

func TestPtlc_pointLock_create_rules(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()
	activatePtlc(t, z)

	var k btcec.ModNScalar
	k.SetInt(7)
	secret := scalarBytes(&k)
	point := pointFor(secret)
	expiry := int64(genesisTimestamp + 3000)

	createPointPtlc(t, z, g.User1, point, types.ZeroAddress, expiry, types.ZnnTokenStandard, big.NewInt(10*g.Zexp), constants.ErrInvalidDestination)
	createPointPtlc(t, z, g.User1, point, types.HtlcContract, expiry, types.ZnnTokenStandard, big.NewInt(10*g.Zexp), constants.ErrInvalidDestination)
	_, somePub := btcec.PrivKeyFromBytes(secret)
	uncompressed := somePub.SerializeUncompressed()
	createPointPtlc(t, z, g.User1, uncompressed, g.User2.Address, expiry, types.ZnnTokenStandard, big.NewInt(10*g.Zexp), constants.ErrInvalidPointLock)
	bad := append([]byte{0x04}, point[1:]...)
	createPointPtlc(t, z, g.User1, bad, g.User2.Address, expiry, types.ZnnTokenStandard, big.NewInt(10*g.Zexp), constants.ErrInvalidPointLock)
	notOnCurve := append([]byte{0x02}, bytes.Repeat([]byte{0xff}, 32)...)
	createPointPtlc(t, z, g.User1, notOnCurve, g.User2.Address, expiry, types.ZnnTokenStandard, big.NewInt(10*g.Zexp), constants.ErrInvalidPointLock)
	createED25519Ptlc(t, z, g.User2.Public, expiry, types.ZnnTokenStandard, big.NewInt(1*g.Zexp))
	z.InsertNewMomentum()

	z.ExpectBalance(g.User1.Address, types.ZnnTokenStandard, 11999*g.Zexp)
	z.ExpectBalance(types.PtlcContract, types.ZnnTokenStandard, 1*g.Zexp)
}

func TestPtlc_pointLock_unlock(t *testing.T) {
	z := mock.NewMockZenon(t)
	ptlcApi := embedded.NewPtlcApi(z)
	defer z.StopPanic()
	activatePtlc(t, z)

	var k btcec.ModNScalar
	k.SetInt(123456789)
	secret := scalarBytes(&k)
	point := pointFor(secret)
	expiry := int64(genesisTimestamp + 3000)

	id := createPointPtlc(t, z, g.User1, point, g.User2.Address, expiry, types.ZnnTokenStandard, big.NewInt(10*g.Zexp), nil)
	z.ExpectBalance(g.User1.Address, types.ZnnTokenStandard, 11990*g.Zexp)
	z.ExpectBalance(types.PtlcContract, types.ZnnTokenStandard, 10*g.Zexp)

	info, err := ptlcApi.GetById(id)
	common.FailIfErr(t, err)
	if info.PointType != definition.PointTypeSecp256k1Point || !bytes.Equal(info.PointLock, point) || info.Destination != g.User2.Address {
		t.Fatalf("stored entry does not match: %v", info)
	}

	unlockPtlcAs(t, z, g.User3, id, secret, constants.ErrPermissionDenied)
	proxyUnlockPtlcAs(t, z, g.User3, id, g.User3.Address, secret, constants.ErrPermissionDenied)
	z.ExpectBalance(types.PtlcContract, types.ZnnTokenStandard, 10*g.Zexp)

	var wrong btcec.ModNScalar
	wrong.SetInt(123456788)
	unlockPtlcAs(t, z, g.User2, id, scalarBytes(&wrong), constants.ErrInvalidPointScalar)
	unlockPtlcRefusedAtSend(t, z, g.User2, id, secret[1:])
	unlockPtlcAs(t, z, g.User2, id, make([]byte, 32), constants.ErrInvalidPointScalar)
	order := btcec.S256().N.Bytes()
	unlockPtlcAs(t, z, g.User2, id, order, constants.ErrInvalidPointScalar)
	unlockPtlcRefusedAtSend(t, z, g.User2, id, append([]byte{0}, secret...))
	z.ExpectBalance(types.PtlcContract, types.ZnnTokenStandard, 10*g.Zexp)

	proxyUnlockPtlcAs(t, z, g.User3, id, g.User2.Address, secret, nil)
	z.ExpectBalance(types.PtlcContract, types.ZnnTokenStandard, 0)
	autoreceive(t, z, g.User2.Address)
	z.ExpectBalance(g.User2.Address, types.ZnnTokenStandard, 8010*g.Zexp)

	_, err = ptlcApi.GetById(id)
	common.ExpectError(t, err, constants.ErrDataNonExistent)
	unlockPtlcAs(t, z, g.User2, id, secret, constants.ErrDataNonExistent)
}

func TestPtlc_pointLock_expiry_and_reclaim(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()
	activatePtlc(t, z)

	var k btcec.ModNScalar
	k.SetInt(42)
	secret := scalarBytes(&k)
	point := pointFor(secret)
	expiry := int64(genesisTimestamp + 300)

	id := createPointPtlc(t, z, g.User1, point, g.User2.Address, expiry, types.QsrTokenStandard, big.NewInt(100*g.Zexp), nil)

	reclaim := z.CallContract(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.PtlcContract,
		Data:      definition.ABIPtlc.PackMethodPanic(definition.ReclaimPtlcMethodName, id),
	})
	z.InsertNewMomentum()
	z.InsertNewMomentum()
	reclaim.Error(t, constants.ReclaimNotDue)

	z.InsertMomentumsTo(40)

	unlockPtlcAs(t, z, g.User2, id, secret, constants.ErrExpired)

	reclaim = z.CallContract(&nom.AccountBlock{
		Address:   g.User2.Address,
		ToAddress: types.PtlcContract,
		Data:      definition.ABIPtlc.PackMethodPanic(definition.ReclaimPtlcMethodName, id),
	})
	z.InsertNewMomentum()
	z.InsertNewMomentum()
	reclaim.Error(t, constants.ErrPermissionDenied)

	reclaim = z.CallContract(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.PtlcContract,
		Data:      definition.ABIPtlc.PackMethodPanic(definition.ReclaimPtlcMethodName, id),
	})
	z.InsertNewMomentum()
	z.InsertNewMomentum()
	reclaim.Error(t, nil)
	autoreceive(t, z, g.User1.Address)
	z.ExpectBalance(g.User1.Address, types.QsrTokenStandard, 120000*g.Zexp)
	z.ExpectBalance(types.PtlcContract, types.QsrTokenStandard, 0)
}

func TestPtlc_fixedDestination_keyLock(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()
	activatePtlc(t, z)

	expiry := int64(genesisTimestamp + 3000)

	block := z.InsertSendBlock(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.PtlcContract,
		Data: definition.ABIPtlc.PackMethodPanic(definition.CreatePtlcMethodName,
			expiry,
			definition.PointTypeED25519,
			g.User2.Public,
			g.User2.Address,
		),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(10 * g.Zexp),
	}, nil, mock.SkipVmChanges)
	z.InsertNewMomentum()
	z.InsertNewMomentum()
	id := block.Hash

	toUser3 := g.User2.Sign(ptlcUnlockMessage(z, definition.PointTypeED25519, id, g.User3.Address))
	proxyUnlockPtlcAs(t, z, g.User3, id, g.User3.Address, toUser3, constants.ErrPermissionDenied)
	z.ExpectBalance(types.PtlcContract, types.ZnnTokenStandard, 10*g.Zexp)

	toUser2 := g.User2.Sign(ptlcUnlockMessage(z, definition.PointTypeED25519, id, g.User2.Address))
	unlockPtlcAs(t, z, g.User2, id, toUser2, nil)
	z.ExpectBalance(types.PtlcContract, types.ZnnTokenStandard, 0)
	autoreceive(t, z, g.User2.Address)
	z.ExpectBalance(g.User2.Address, types.ZnnTokenStandard, 8010*g.Zexp)
}

func TestPtlc_pointLock_tweakedSwap(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()
	activatePtlc(t, z)

	alice, bob := g.User1, g.User2

	var kt, kd btcec.ModNScalar
	kt.SetInt(987654321)
	kd.SetInt(1122334455)
	tSecret := scalarBytes(&kt)
	dTweak := scalarBytes(&kd)
	tPoint := pointFor(tSecret)
	t2Secret := addScalars(tSecret, dTweak)
	t2Point := pointFor(t2Secret)
	if bytes.Equal(tPoint, t2Point) {
		t.Fatal("the tweak did not change the point")
	}

	aliceLock := createPointPtlc(t, z, alice, t2Point, bob.Address, int64(genesisTimestamp+6000), types.QsrTokenStandard, big.NewInt(100*g.Zexp), nil)
	bobLock := createPointPtlc(t, z, bob, tPoint, alice.Address, int64(genesisTimestamp+3000), types.ZnnTokenStandard, big.NewInt(10*g.Zexp), nil)

	unlockPtlcAs(t, z, bob, aliceLock, dTweak, constants.ErrInvalidPointScalar)

	unlockPtlcAs(t, z, alice, bobLock, tSecret, nil)
	autoreceive(t, z, alice.Address)
	z.ExpectBalance(alice.Address, types.ZnnTokenStandard, 12010*g.Zexp)

	unlockPtlcAs(t, z, bob, aliceLock, t2Secret, nil)
	autoreceive(t, z, bob.Address)
	z.ExpectBalance(bob.Address, types.QsrTokenStandard, 80100*g.Zexp)
	z.ExpectBalance(alice.Address, types.QsrTokenStandard, 119900*g.Zexp)
	z.ExpectBalance(bob.Address, types.ZnnTokenStandard, 7990*g.Zexp)
	z.ExpectBalance(types.PtlcContract, types.ZnnTokenStandard, 0)
	z.ExpectBalance(types.PtlcContract, types.QsrTokenStandard, 0)
}

func reclaimPtlcAs(t *testing.T, z mock.MockZenon, caller *wallet.KeyPair, id types.Hash, expected error) {
	t.Helper()
	call := z.CallContract(&nom.AccountBlock{
		Address:   caller.Address,
		ToAddress: types.PtlcContract,
		Data:      definition.ABIPtlc.PackMethodPanic(definition.ReclaimPtlcMethodName, id),
	})
	z.InsertNewMomentum()
	z.InsertNewMomentum()
	call.Error(t, expected)
}

func TestPtlc_pointLock_swapOrder_wrongOrderLosesBob(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()
	activatePtlc(t, z)

	alice, bob := g.User1, g.User2

	var kt, kd btcec.ModNScalar
	kt.SetInt(5550123)
	kd.SetInt(777)
	tSecret := scalarBytes(&kt)
	dTweak := scalarBytes(&kd)
	t2Secret := addScalars(tSecret, dTweak)

	bobLock := createPointPtlc(t, z, bob, pointFor(tSecret), alice.Address, int64(genesisTimestamp+6000), types.ZnnTokenStandard, big.NewInt(10*g.Zexp), nil)
	aliceLock := createPointPtlc(t, z, alice, pointFor(t2Secret), bob.Address, int64(genesisTimestamp+3000), types.QsrTokenStandard, big.NewInt(100*g.Zexp), nil)

	z.InsertMomentumsTo(310)
	reclaimPtlcAs(t, z, alice, aliceLock, nil)
	unlockPtlcAs(t, z, alice, bobLock, tSecret, nil)

	unlockPtlcAs(t, z, bob, aliceLock, t2Secret, constants.ErrDataNonExistent)

	autoreceive(t, z, alice.Address)
	z.ExpectBalance(alice.Address, types.ZnnTokenStandard, 12010*g.Zexp)
	z.ExpectBalance(alice.Address, types.QsrTokenStandard, 120000*g.Zexp)
	z.ExpectBalance(bob.Address, types.ZnnTokenStandard, 7990*g.Zexp)
	z.ExpectBalance(bob.Address, types.QsrTokenStandard, 80000*g.Zexp)
}

func TestPtlc_pointLock_swapOrder_lateRevealCostsTheRevealer(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()
	activatePtlc(t, z)

	alice, bob := g.User1, g.User2

	var kt, kd btcec.ModNScalar
	kt.SetInt(4440321)
	kd.SetInt(999)
	tSecret := scalarBytes(&kt)
	dTweak := scalarBytes(&kd)
	t2Secret := addScalars(tSecret, dTweak)

	aliceLock := createPointPtlc(t, z, alice, pointFor(t2Secret), bob.Address, int64(genesisTimestamp+6000), types.QsrTokenStandard, big.NewInt(100*g.Zexp), nil)
	bobLock := createPointPtlc(t, z, bob, pointFor(tSecret), alice.Address, int64(genesisTimestamp+3000), types.ZnnTokenStandard, big.NewInt(10*g.Zexp), nil)

	z.InsertMomentumsTo(310)
	unlockPtlcAs(t, z, alice, bobLock, tSecret, constants.ErrExpired)
	reclaimPtlcAs(t, z, bob, bobLock, nil)
	unlockPtlcAs(t, z, bob, aliceLock, t2Secret, nil)

	autoreceive(t, z, bob.Address)
	z.ExpectBalance(bob.Address, types.ZnnTokenStandard, 8000*g.Zexp)
	z.ExpectBalance(bob.Address, types.QsrTokenStandard, 80100*g.Zexp)
	z.ExpectBalance(alice.Address, types.ZnnTokenStandard, 12000*g.Zexp)
	z.ExpectBalance(alice.Address, types.QsrTokenStandard, 119900*g.Zexp)
}
