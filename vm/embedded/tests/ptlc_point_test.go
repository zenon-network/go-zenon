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

// The secp256k1 point type: the lock is a compressed point T, the witness is
// the scalar t with t*G == T, and the entry names the only address an unlock
// may pay, because a scalar seen in a send block binds nothing by itself.

// scalarBytes is a canonical 32-byte scalar.
func scalarBytes(k *btcec.ModNScalar) []byte {
	b := k.Bytes()
	return b[:]
}

// pointFor returns the compressed point t*G for a scalar given as bytes.
func pointFor(t []byte) []byte {
	var k btcec.ModNScalar
	k.SetByteSlice(t)
	var p btcec.JacobianPoint
	btcec.ScalarBaseMultNonConst(&k, &p)
	p.ToAffine()
	return btcec.NewPublicKey(&p.X, &p.Y).SerializeCompressed()
}

// addScalars returns (a + b) mod n.
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
		// a refused create has no block, and no entry
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

	// a point lock with no destination is a bearer lock: refused
	createPointPtlc(t, z, g.User1, point, types.ZeroAddress, expiry, types.ZnnTokenStandard, big.NewInt(10*g.Zexp), constants.ErrInvalidDestination)
	// an embedded contract cannot take a contract send with no data
	createPointPtlc(t, z, g.User1, point, types.HtlcContract, expiry, types.ZnnTokenStandard, big.NewInt(10*g.Zexp), constants.ErrInvalidDestination)
	// an uncompressed point is the wrong size
	_, somePub := btcec.PrivKeyFromBytes(secret)
	uncompressed := somePub.SerializeUncompressed()
	createPointPtlc(t, z, g.User1, uncompressed, g.User2.Address, expiry, types.ZnnTokenStandard, big.NewInt(10*g.Zexp), constants.ErrInvalidPointLock)
	// 33 bytes with a prefix that is not 0x02 or 0x03
	bad := append([]byte{0x04}, point[1:]...)
	createPointPtlc(t, z, g.User1, bad, g.User2.Address, expiry, types.ZnnTokenStandard, big.NewInt(10*g.Zexp), constants.ErrInvalidPointLock)
	// 33 bytes that are not on the curve
	notOnCurve := append([]byte{0x02}, bytes.Repeat([]byte{0xff}, 32)...)
	createPointPtlc(t, z, g.User1, notOnCurve, g.User2.Address, expiry, types.ZnnTokenStandard, big.NewInt(10*g.Zexp), constants.ErrInvalidPointLock)
	// a key type may fix a destination too
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

	// user 1 locks 10 ZNN to the point, payable to user 2
	id := createPointPtlc(t, z, g.User1, point, g.User2.Address, expiry, types.ZnnTokenStandard, big.NewInt(10*g.Zexp), nil)
	z.ExpectBalance(g.User1.Address, types.ZnnTokenStandard, 11990*g.Zexp)
	z.ExpectBalance(types.PtlcContract, types.ZnnTokenStandard, 10*g.Zexp)

	info, err := ptlcApi.GetById(id)
	common.FailIfErr(t, err)
	if info.PointType != definition.PointTypeSecp256k1Point || !bytes.Equal(info.PointLock, point) || info.Destination != g.User2.Address {
		t.Fatalf("stored entry does not match: %v", info)
	}

	// user 3 saw the scalar and tries to take the funds: refused, entry kept
	unlockPtlcAs(t, z, g.User3, id, secret, constants.ErrPermissionDenied)
	proxyUnlockPtlcAs(t, z, g.User3, id, g.User3.Address, secret, constants.ErrPermissionDenied)
	z.ExpectBalance(types.PtlcContract, types.ZnnTokenStandard, 10*g.Zexp)

	// the recipient with wrong witnesses: refused, entry kept
	var wrong btcec.ModNScalar
	wrong.SetInt(123456788)
	unlockPtlcAs(t, z, g.User2, id, scalarBytes(&wrong), constants.ErrInvalidPointScalar)
	unlockPtlcAs(t, z, g.User2, id, secret[1:], constants.ErrInvalidPointScalar)
	unlockPtlcAs(t, z, g.User2, id, make([]byte, 32), constants.ErrInvalidPointScalar)
	// the group order is not a canonical scalar
	order := btcec.S256().N.Bytes()
	unlockPtlcAs(t, z, g.User2, id, order, constants.ErrInvalidPointScalar)
	// t + n encodes the same residue but is not canonical, and is 33 bytes anyway
	unlockPtlcAs(t, z, g.User2, id, append([]byte{0}, secret...), constants.ErrInvalidPointScalar)
	z.ExpectBalance(types.PtlcContract, types.ZnnTokenStandard, 10*g.Zexp)

	// anyone may submit the proxy unlock, but only to the fixed destination
	proxyUnlockPtlcAs(t, z, g.User3, id, g.User2.Address, secret, nil)
	z.ExpectBalance(types.PtlcContract, types.ZnnTokenStandard, 0)
	autoreceive(t, z, g.User2.Address)
	z.ExpectBalance(g.User2.Address, types.ZnnTokenStandard, 8010*g.Zexp)

	// the entry is gone
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

	// nobody but the creator can reclaim, and not before expiry
	reclaim := z.CallContract(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.PtlcContract,
		Data:      definition.ABIPtlc.PackMethodPanic(definition.ReclaimPtlcMethodName, id),
	})
	z.InsertNewMomentum()
	z.InsertNewMomentum()
	reclaim.Error(t, constants.ReclaimNotDue)

	z.InsertMomentumsTo(40)

	// after expiry the right scalar no longer opens it
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

	// an ED25519 lock to user 2's key, payable only to user 2
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

	// user 2 signs for user 3 as destination; the signature is valid, the
	// destination is not
	toUser3 := g.User2.Sign(ptlcUnlockMessage(z, definition.PointTypeED25519, id, g.User3.Address))
	proxyUnlockPtlcAs(t, z, g.User3, id, g.User3.Address, toUser3, constants.ErrPermissionDenied)
	z.ExpectBalance(types.PtlcContract, types.ZnnTokenStandard, 10*g.Zexp)

	// and for user 2: accepted
	toUser2 := g.User2.Sign(ptlcUnlockMessage(z, definition.PointTypeED25519, id, g.User2.Address))
	unlockPtlcAs(t, z, g.User2, id, toUser2, nil)
	z.ExpectBalance(types.PtlcContract, types.ZnnTokenStandard, 0)
	autoreceive(t, z, g.User2.Address)
	z.ExpectBalance(g.User2.Address, types.ZnnTokenStandard, 8010*g.Zexp)
}

// A NoM-to-NoM swap on two point locks with a tweak: Alice holds t, Bob locks
// ZNN to T for Alice, Alice locks QSR to T + d*G for Bob, where d is a value
// the two agreed off chain. Alice's unlock publishes t; Bob adds d and unlocks
// his leg. The chain shows two unrelated points and two unrelated scalars.
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

	// Alice holds t, so she locks first and her entry expires last. Bob locks
	// only once hers is on the chain, and his expires first: whenever Alice
	// claims his entry, publishing t, hers is still open to him.
	aliceLock := createPointPtlc(t, z, alice, t2Point, bob.Address, int64(genesisTimestamp+6000), types.QsrTokenStandard, big.NewInt(100*g.Zexp), nil)
	bobLock := createPointPtlc(t, z, bob, tPoint, alice.Address, int64(genesisTimestamp+3000), types.ZnnTokenStandard, big.NewInt(10*g.Zexp), nil)

	// Bob cannot open Alice's leg yet: he does not know t
	unlockPtlcAs(t, z, bob, aliceLock, dTweak, constants.ErrInvalidPointScalar)

	// Alice claims Bob's ZNN, publishing t
	unlockPtlcAs(t, z, alice, bobLock, tSecret, nil)
	autoreceive(t, z, alice.Address)
	z.ExpectBalance(alice.Address, types.ZnnTokenStandard, 12010*g.Zexp)

	// Bob reads t from Alice's unlock, adds d, and claims Alice's QSR
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

// The order the tweaked swap above uses is the protocol's, not the contract's:
// the contract does what each call asks. This is the swap in the order SPEC.md
// §3 had until 2026-10-09, with the party who does not hold t locking first and
// expiring last. Alice waits out her own entry, takes it back, and then claims
// Bob's with t. When t becomes public, the entry Bob needed it for is gone.
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

	// past Alice's expiry, before Bob's
	z.InsertMomentumsTo(310)
	reclaimPtlcAs(t, z, alice, aliceLock, nil)
	unlockPtlcAs(t, z, alice, bobLock, tSecret, nil)

	// t is public now, and too late to be of use to Bob
	unlockPtlcAs(t, z, bob, aliceLock, t2Secret, constants.ErrDataNonExistent)

	autoreceive(t, z, alice.Address)
	z.ExpectBalance(alice.Address, types.ZnnTokenStandard, 12010*g.Zexp)
	z.ExpectBalance(alice.Address, types.QsrTokenStandard, 120000*g.Zexp)
	z.ExpectBalance(bob.Address, types.ZnnTokenStandard, 7990*g.Zexp)
	z.ExpectBalance(bob.Address, types.QsrTokenStandard, 80000*g.Zexp)
}

// The same attempt against the safe order fails, and costs Alice. Her claim on
// Bob's entry comes after his expiry, so the contract refuses it; but the
// refused call still carries t, and her own entry is open to Bob for another
// fifty minutes. Bob takes his ZNN back and her QSR as well. A witness is
// public the moment its block is, whatever the contract answers, which is why
// a client must not reveal one close to the deadline.
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

	// past Bob's expiry, before Alice's
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
