package implementation

import (
	"bytes"
	"math/big"
	"testing"
	"time"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/btcec/v2/schnorr"
	"github.com/ethereum/go-ethereum/common/hexutil"

	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/crypto"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/vm/constants"
	"github.com/zenon-network/go-zenon/vm/embedded/definition"
	"github.com/zenon-network/go-zenon/wallet"
)

const (
	reclaimTestChain      = uint64(100)
	reclaimTestCreateTime = int64(100)
	reclaimTestExpiration = int64(200)
)

var (
	reclaimKey, _      = wallet.DeriveWithIndex(2, hexutil.MustDecode("0x01234567890123456789012345678902"))
	otherReclaimKey, _ = wallet.DeriveWithIndex(3, hexutil.MustDecode("0x01234567890123456789012345678902"))
	reclaimCaller, _   = wallet.DeriveWithIndex(4, hexutil.MustDecode("0x01234567890123456789012345678902"))
)

func setPtlcTime(ctx *testPtlcContext, timestamp int64) {
	t := time.Unix(timestamp, 0)
	ctx.momentum.Timestamp = &t
}

func createWithReclaimLockData(expiration int64, pointType uint8, pointLock []byte, destination types.Address, reclaimPointType uint8, reclaimPointLock []byte) []byte {
	return definition.ABIPtlc.PackMethodPanic(definition.CreateWithReclaimLockPtlcMethodName,
		expiration,
		pointType,
		pointLock,
		destination,
		reclaimPointType,
		reclaimPointLock,
	)
}

func createReclaimLockedPtlc(t *testing.T, ctx *testPtlcContext, name string, creator types.Address, reclaimPointType uint8, reclaimPointLock []byte) types.Hash {
	t.Helper()
	sendBlock := &nom.AccountBlock{
		Address:       creator,
		Hash:          types.NewHash([]byte(name)),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(7),
		Data:          createWithReclaimLockData(reclaimTestExpiration, definition.PointTypeED25519, User1.Public, types.ZeroAddress, reclaimPointType, reclaimPointLock),
	}
	blocks, err := (&CreateWithReclaimLockPtlcMethod{definition.CreateWithReclaimLockPtlcMethodName}).ReceiveBlock(ctx, sendBlock)
	common.FailIfErr(t, err)
	if blocks != nil {
		t.Fatalf("create should not emit contract sends, got %d", len(blocks))
	}
	return sendBlock.Hash
}

func reclaimWithSignature(ctx *testPtlcContext, caller types.Address, id types.Hash, signature []byte) ([]*nom.AccountBlock, error) {
	return (&ReclaimWithSignaturePtlcMethod{definition.ReclaimWithSignaturePtlcMethodName}).ReceiveBlock(ctx, &nom.AccountBlock{
		Address: caller,
		Data:    definition.ABIPtlc.PackMethodPanic(definition.ReclaimWithSignaturePtlcMethodName, id, signature),
		Amount:  big.NewInt(0),
	})
}

func plainReclaim(ctx *testPtlcContext, caller types.Address, id types.Hash) ([]*nom.AccountBlock, error) {
	return (&ReclaimPtlcMethod{definition.ReclaimPtlcMethodName}).ReceiveBlock(ctx, &nom.AccountBlock{
		Address: caller,
		Data:    definition.ABIPtlc.PackMethodPanic(definition.ReclaimPtlcMethodName, id),
		Amount:  big.NewInt(0),
	})
}

func signReclaim(key *wallet.KeyPair, chain uint64, id types.Hash, creator types.Address) []byte {
	return key.Sign(definition.GetPtlcReclaimMessage(chain, definition.PointTypeED25519, id, creator))
}

func ptlcReclaimMessageForContract(chainIdentifier uint64, contract types.Address, pointType uint8, id types.Hash, creator types.Address) []byte {
	return crypto.Hash(common.JoinBytes(
		[]byte(definition.PtlcReclaimMessageDomain),
		common.Uint64ToBytes(chainIdentifier),
		contract.Bytes(),
		[]byte{pointType},
		id.Bytes(),
		creator.Bytes(),
	))
}

func expectNoBlocks(t *testing.T, blocks []*nom.AccountBlock) {
	t.Helper()
	if blocks != nil {
		t.Fatalf("expected no generated blocks, got %d", len(blocks))
	}
}

func expectPaid(t *testing.T, blocks []*nom.AccountBlock, to types.Address, amount int64) {
	t.Helper()
	if len(blocks) != 1 {
		t.Fatalf("expected one generated block, got %d", len(blocks))
	}
	if blocks[0].ToAddress != to {
		t.Fatalf("paid %s, want %s", blocks[0].ToAddress, to)
	}
	if blocks[0].Amount.Cmp(big.NewInt(amount)) != 0 {
		t.Fatalf("paid %s, want %d", blocks[0].Amount, amount)
	}
}

func expectEntryGone(t *testing.T, ctx *testPtlcContext, id types.Hash) {
	t.Helper()
	_, err := definition.GetPtlcInfo(ctx.Storage(), id)
	common.ExpectError(t, err, constants.ErrDataNonExistent)
	_, err = definition.GetPtlcReclaimLock(ctx.Storage(), id)
	common.ExpectError(t, err, constants.ErrDataNonExistent)
}

func TestPtlc_ReclaimLockRules(t *testing.T) {
	_, bip340PubKey := btcec.PrivKeyFromBytes(bytes.Repeat([]byte{3}, 32))
	bip340Lock := schnorr.SerializePubKey(bip340PubKey)

	common.ExpectError(t, checkReclaimLock(definition.PointTypeED25519, reclaimKey.Public), nil)
	common.ExpectError(t, checkReclaimLock(definition.PointTypeBIP340, bip340Lock), nil)
	common.ExpectError(t, checkReclaimLock(definition.PointTypeSecp256k1Point, pointOf(pointScalar(7))), constants.ErrInvalidPointType)
	common.ExpectError(t, checkReclaimLock(3, reclaimKey.Public), constants.ErrInvalidPointType)
	common.ExpectError(t, checkReclaimLock(255, reclaimKey.Public), constants.ErrInvalidPointType)

	for name, encoded := range ed25519SmallOrderLocks {
		if err := checkReclaimLock(definition.PointTypeED25519, mustDecodeHex(t, encoded)); err != constants.ErrInvalidPointLock {
			t.Errorf("%s: got %v, want %v", name, err, constants.ErrInvalidPointLock)
		}
	}
	undecodable := bytes.Repeat([]byte{0x02}, 32)
	common.ExpectError(t, checkReclaimLock(definition.PointTypeED25519, undecodable), constants.ErrInvalidPointLock)
	common.ExpectError(t, checkReclaimLock(definition.PointTypeBIP340, offCurveX(t)[1:]), constants.ErrInvalidPointLock)
	common.ExpectError(t, checkReclaimLock(definition.PointTypeBIP340, bytes.Repeat([]byte{0xff}, 32)), constants.ErrInvalidPointLock)

	for _, pointType := range []uint8{definition.PointTypeED25519, definition.PointTypeBIP340} {
		for _, size := range []int{0, 1, 31, 33, 64} {
			if err := checkReclaimLock(pointType, bytes.Repeat([]byte{9}, size)); err != constants.ErrInvalidPointLock {
				t.Errorf("point type %d, %d-byte lock: got %v, want %v", pointType, size, err, constants.ErrInvalidPointLock)
			}
		}
	}
	common.ExpectError(t, checkReclaimLock(definition.PointTypeED25519, append(append([]byte{}, reclaimKey.Public...), 0)), constants.ErrInvalidPointLock)
	common.ExpectError(t, checkReclaimLock(definition.PointTypeBIP340, append([]byte{0x02}, bip340Lock...)), constants.ErrInvalidPointLock)
}

func TestPtlc_CreateWithReclaimLockSendBlock(t *testing.T) {
	method := &CreateWithReclaimLockPtlcMethod{definition.CreateWithReclaimLockPtlcMethodName}
	_, bip340PubKey := btcec.PrivKeyFromBytes(bytes.Repeat([]byte{3}, 32))
	bip340Lock := schnorr.SerializePubKey(bip340PubKey)
	identity := mustDecodeHex(t, ed25519SmallOrderLocks["identity"])

	tests := []struct {
		name             string
		pointType        uint8
		pointLock        []byte
		destination      types.Address
		reclaimPointType uint8
		reclaimPointLock []byte
		amount           *big.Int
		want             error
	}{
		{"ed25519 reclaim lock", definition.PointTypeED25519, User1.Public, types.ZeroAddress, definition.PointTypeED25519, reclaimKey.Public, big.NewInt(1), nil},
		{"bip340 reclaim lock", definition.PointTypeED25519, User1.Public, types.ZeroAddress, definition.PointTypeBIP340, bip340Lock, big.NewInt(1), nil},
		{"bip340 claim, ed25519 reclaim", definition.PointTypeBIP340, bip340Lock, types.ZeroAddress, definition.PointTypeED25519, reclaimKey.Public, big.NewInt(1), nil},
		{"secp256k1 claim, ed25519 reclaim", definition.PointTypeSecp256k1Point, pointOf(pointScalar(7)), User1.Address, definition.PointTypeED25519, reclaimKey.Public, big.NewInt(1), nil},
		{"same key both ways", definition.PointTypeED25519, User1.Public, types.ZeroAddress, definition.PointTypeED25519, User1.Public, big.NewInt(1), nil},
		{"secp256k1 reclaim lock", definition.PointTypeED25519, User1.Public, types.ZeroAddress, definition.PointTypeSecp256k1Point, pointOf(pointScalar(7)), big.NewInt(1), constants.ErrInvalidPointType},
		{"unknown reclaim type", definition.PointTypeED25519, User1.Public, types.ZeroAddress, 3, reclaimKey.Public, big.NewInt(1), constants.ErrInvalidPointType},
		{"identity reclaim lock", definition.PointTypeED25519, User1.Public, types.ZeroAddress, definition.PointTypeED25519, identity, big.NewInt(1), constants.ErrInvalidPointLock},
		{"short reclaim lock", definition.PointTypeED25519, User1.Public, types.ZeroAddress, definition.PointTypeED25519, reclaimKey.Public[1:], big.NewInt(1), constants.ErrInvalidPointLock},
		{"empty reclaim lock", definition.PointTypeED25519, User1.Public, types.ZeroAddress, definition.PointTypeED25519, nil, big.NewInt(1), constants.ErrInvalidPointLock},
		{"off-curve bip340 reclaim lock", definition.PointTypeED25519, User1.Public, types.ZeroAddress, definition.PointTypeBIP340, offCurveX(t)[1:], big.NewInt(1), constants.ErrInvalidPointLock},
		{"invalid claim lock", definition.PointTypeED25519, identity, types.ZeroAddress, definition.PointTypeED25519, reclaimKey.Public, big.NewInt(1), constants.ErrInvalidPointLock},
		{"secp256k1 claim without destination", definition.PointTypeSecp256k1Point, pointOf(pointScalar(7)), types.ZeroAddress, definition.PointTypeED25519, reclaimKey.Public, big.NewInt(1), constants.ErrInvalidDestination},
		{"unpayable destination", definition.PointTypeED25519, User1.Public, types.PtlcContract, definition.PointTypeED25519, reclaimKey.Public, big.NewInt(1), constants.ErrInvalidDestination},
		{"zero amount", definition.PointTypeED25519, User1.Public, types.ZeroAddress, definition.PointTypeED25519, reclaimKey.Public, big.NewInt(0), constants.ErrInvalidTokenOrAmount},
		{"negative amount", definition.PointTypeED25519, User1.Public, types.ZeroAddress, definition.PointTypeED25519, reclaimKey.Public, big.NewInt(-1), constants.ErrInvalidTokenOrAmount},
		{"nil amount", definition.PointTypeED25519, User1.Public, types.ZeroAddress, definition.PointTypeED25519, reclaimKey.Public, nil, constants.ErrInvalidTokenOrAmount},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			block := &nom.AccountBlock{
				Amount:        test.amount,
				TokenStandard: types.ZnnTokenStandard,
				Data:          createWithReclaimLockData(1000, test.pointType, test.pointLock, test.destination, test.reclaimPointType, test.reclaimPointLock),
			}
			common.ExpectError(t, method.ValidateSendBlock(block), test.want)
		})
	}

	common.ExpectError(t, method.ValidateSendBlock(&nom.AccountBlock{Amount: big.NewInt(1), Data: []byte{1, 2, 3}}), constants.ErrUnpackError)
	common.ExpectError(t, method.ValidateSendBlock(&nom.AccountBlock{
		Amount: big.NewInt(1),
		Data:   definition.ABIPtlc.PackMethodPanic(definition.CreatePtlcMethodName, int64(1000), definition.PointTypeED25519, User1.Public, types.ZeroAddress),
	}), constants.ErrUnpackError)

	gas, err := method.GetPlasma(&constants.PlasmaTable{EmbeddedSimple: 11, EmbeddedWWithdraw: 22})
	common.FailIfErr(t, err)
	if gas != 11 {
		t.Fatalf("plasma = %d, want the EmbeddedSimple price", gas)
	}
}

func TestPtlc_ReclaimWithSignatureSendBlock(t *testing.T) {
	method := &ReclaimWithSignaturePtlcMethod{definition.ReclaimWithSignaturePtlcMethodName}
	id := types.NewHash([]byte("reclaim-send"))

	for size, want := range map[int]error{
		0:                           constants.ErrInvalidPointSignature,
		32:                          constants.ErrInvalidPointSignature,
		63:                          constants.ErrInvalidPointSignature,
		64:                          nil,
		65:                          constants.ErrInvalidPointSignature,
		constants.MaxDataLength + 1: constants.ErrInvalidPointSignature,
	} {
		block := &nom.AccountBlock{Amount: big.NewInt(0), Data: definition.ABIPtlc.PackMethodPanic(definition.ReclaimWithSignaturePtlcMethodName, id, bytes.Repeat([]byte{1}, size))}
		sent := len(block.Data)
		if err := method.ValidateSendBlock(block); err != want {
			t.Errorf("%d-byte signature: got %v, want %v", size, err, want)
		}
		if want != nil && len(block.Data) != sent {
			t.Errorf("%d-byte signature: the refused block was repacked", size)
		}
	}

	signature := bytes.Repeat([]byte{1}, 64)
	for _, amount := range []*big.Int{nil, big.NewInt(1), big.NewInt(-1)} {
		block := &nom.AccountBlock{Amount: amount, Data: definition.ABIPtlc.PackMethodPanic(definition.ReclaimWithSignaturePtlcMethodName, id, signature)}
		common.ExpectError(t, method.ValidateSendBlock(block), constants.ErrInvalidTokenOrAmount)
	}
	common.ExpectError(t, method.ValidateSendBlock(&nom.AccountBlock{
		Amount: big.NewInt(0),
		Data:   definition.ABIPtlc.PackMethodPanic(definition.ReclaimPtlcMethodName, id),
	}), constants.ErrUnpackError)

	gas, err := method.GetPlasma(&constants.PlasmaTable{EmbeddedSimple: 11, EmbeddedWWithdraw: 22})
	common.FailIfErr(t, err)
	if gas != 22 {
		t.Fatalf("plasma = %d, want the EmbeddedWWithdraw price", gas)
	}
}

func TestPtlc_CreateWithReclaimLockStoresBoth(t *testing.T) {
	ctx := newTestPtlcContext(reclaimTestChain, reclaimTestCreateTime)
	id := createReclaimLockedPtlc(t, ctx, "stores-both", User1.Address, definition.PointTypeED25519, reclaimKey.Public)

	info, err := definition.GetPtlcInfo(ctx.Storage(), id)
	common.FailIfErr(t, err)
	if info.TimeLocked != User1.Address || info.ExpirationTime != reclaimTestExpiration || info.PointType != definition.PointTypeED25519 || !bytes.Equal(info.PointLock, User1.Public) || info.Amount.Cmp(big.NewInt(7)) != 0 {
		t.Fatalf("unexpected entry %s", info)
	}
	lock, err := definition.GetPtlcReclaimLock(ctx.Storage(), id)
	common.FailIfErr(t, err)
	if lock.Id != id || lock.PointType != definition.PointTypeED25519 || !bytes.Equal(lock.PointLock, reclaimKey.Public) {
		t.Fatalf("unexpected reclaim lock %s", lock)
	}

	for _, test := range []struct {
		name       string
		now        int64
		expiration int64
		want       error
	}{
		{"before expiration", 100, 101, nil},
		{"at expiration", 100, 100, constants.ErrInvalidExpirationTime},
		{"after expiration", 100, 99, constants.ErrInvalidExpirationTime},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx := newTestPtlcContext(reclaimTestChain, test.now)
			sendBlock := &nom.AccountBlock{
				Address:       User1.Address,
				Hash:          types.NewHash([]byte(test.name)),
				TokenStandard: types.ZnnTokenStandard,
				Amount:        big.NewInt(1),
				Data:          createWithReclaimLockData(test.expiration, definition.PointTypeED25519, User1.Public, types.ZeroAddress, definition.PointTypeED25519, reclaimKey.Public),
			}
			blocks, err := (&CreateWithReclaimLockPtlcMethod{definition.CreateWithReclaimLockPtlcMethodName}).ReceiveBlock(ctx, sendBlock)
			common.ExpectError(t, err, test.want)
			expectNoBlocks(t, blocks)
			_, infoErr := definition.GetPtlcInfo(ctx.Storage(), sendBlock.Hash)
			_, lockErr := definition.GetPtlcReclaimLock(ctx.Storage(), sendBlock.Hash)
			if test.want == nil && (infoErr != nil || lockErr != nil) {
				t.Fatalf("entry %v, reclaim lock %v", infoErr, lockErr)
			}
			if test.want != nil && (infoErr != constants.ErrDataNonExistent || lockErr != constants.ErrDataNonExistent) {
				t.Fatalf("refused create stored entry %v, reclaim lock %v", infoErr, lockErr)
			}
		})
	}
}

func TestPtlc_ReclaimWithSignature(t *testing.T) {
	ctx := newTestPtlcContext(reclaimTestChain, reclaimTestCreateTime)
	id := createReclaimLockedPtlc(t, ctx, "reclaim-with-signature", User1.Address, definition.PointTypeED25519, reclaimKey.Public)
	otherId := createReclaimLockedPtlc(t, ctx, "reclaim-with-signature-other", User1.Address, definition.PointTypeED25519, reclaimKey.Public)
	valid := signReclaim(reclaimKey, reclaimTestChain, id, User1.Address)

	blocks, err := reclaimWithSignature(ctx, User1.Address, id, valid)
	common.ExpectError(t, err, constants.ReclaimNotDue)
	expectNoBlocks(t, blocks)

	setPtlcTime(ctx, reclaimTestExpiration-1)
	blocks, err = reclaimWithSignature(ctx, User1.Address, id, valid)
	common.ExpectError(t, err, constants.ReclaimNotDue)
	expectNoBlocks(t, blocks)

	setPtlcTime(ctx, reclaimTestExpiration)

	if !bytes.Equal(ptlcReclaimMessageForContract(reclaimTestChain, types.PtlcContract, definition.PointTypeED25519, id, User1.Address), definition.GetPtlcReclaimMessage(reclaimTestChain, definition.PointTypeED25519, id, User1.Address)) {
		t.Fatal("the test's reclaim message differs from the contract's in more than the contract address")
	}

	shifted := append([]byte{}, valid...)
	carry := 0
	for i := 0; i < 32; i++ {
		sum := int(shifted[32+i]) + int(ed25519GroupOrder[i]) + carry
		shifted[32+i] = byte(sum)
		carry = sum >> 8
	}

	refused := map[string][]byte{
		"zero signature":           make([]byte, 64),
		"another key":              signReclaim(otherReclaimKey, reclaimTestChain, id, User1.Address),
		"the claim key":            signReclaim(User1, reclaimTestChain, id, User1.Address),
		"another entry":            signReclaim(reclaimKey, reclaimTestChain, otherId, User1.Address),
		"another chain":            signReclaim(reclaimKey, reclaimTestChain+1, id, User1.Address),
		"another creator":          signReclaim(reclaimKey, reclaimTestChain, id, reclaimCaller.Address),
		"bip340 point type":        reclaimKey.Sign(definition.GetPtlcReclaimMessage(reclaimTestChain, definition.PointTypeBIP340, id, User1.Address)),
		"the unlock message":       reclaimKey.Sign(definition.GetPtlcUnlockMessage(reclaimTestChain, definition.PointTypeED25519, id, User1.Address)),
		"the raw entry id":         reclaimKey.Sign(id.Bytes()),
		"s + L":                    shifted,
		"flipped bit in R":         append([]byte{valid[0] ^ 1}, valid[1:]...),
		"flipped bit in s":         append(append([]byte{}, valid[:40]...), append([]byte{valid[40] ^ 1}, valid[41:]...)...),
		"another contract address": reclaimKey.Sign(ptlcReclaimMessageForContract(reclaimTestChain, types.HtlcContract, definition.PointTypeED25519, id, User1.Address)),
	}
	for name, signature := range refused {
		blocks, err := reclaimWithSignature(ctx, User1.Address, id, signature)
		if err != constants.ErrInvalidPointSignature {
			t.Errorf("%s: got %v, want %v", name, err, constants.ErrInvalidPointSignature)
		}
		expectNoBlocks(t, blocks)
	}

	blocks, err = reclaimWithSignature(ctx, reclaimCaller.Address, id, valid)
	common.ExpectError(t, err, constants.ErrPermissionDenied)
	expectNoBlocks(t, blocks)

	blocks, err = plainReclaim(ctx, User1.Address, id)
	common.ExpectError(t, err, constants.ErrReclaimSignatureRequired)
	expectNoBlocks(t, blocks)

	blocks, err = plainReclaim(ctx, reclaimCaller.Address, id)
	common.ExpectError(t, err, constants.ErrPermissionDenied)
	expectNoBlocks(t, blocks)

	unlockSignature := User1.Sign(definition.GetPtlcUnlockMessage(reclaimTestChain, definition.PointTypeED25519, id, User1.Address))
	blocks, err = unlockPtlc(ctx, &nom.AccountBlock{Address: User1.Address}, id, User1.Address, unlockSignature)
	common.ExpectError(t, err, constants.ErrExpired)
	expectNoBlocks(t, blocks)

	blocks, err = reclaimWithSignature(ctx, User1.Address, id, valid)
	common.FailIfErr(t, err)
	expectPaid(t, blocks, User1.Address, 7)
	expectEntryGone(t, ctx, id)

	blocks, err = reclaimWithSignature(ctx, User1.Address, id, valid)
	common.ExpectError(t, err, constants.ErrDataNonExistent)
	expectNoBlocks(t, blocks)

	blocks, err = reclaimWithSignature(ctx, User1.Address, otherId, valid)
	common.ExpectError(t, err, constants.ErrInvalidPointSignature)
	expectNoBlocks(t, blocks)

	blocks, err = reclaimWithSignature(ctx, User1.Address, otherId, signReclaim(reclaimKey, reclaimTestChain, otherId, User1.Address))
	common.FailIfErr(t, err)
	expectPaid(t, blocks, User1.Address, 7)
	expectEntryGone(t, ctx, otherId)
}

func TestPtlc_ReclaimWithSignatureBIP340(t *testing.T) {
	privateKey, publicKey := btcec.PrivKeyFromBytes(bytes.Repeat([]byte{5}, 32))
	ctx := newTestPtlcContext(reclaimTestChain, reclaimTestExpiration)
	setPtlcTime(ctx, reclaimTestCreateTime)
	id := createReclaimLockedPtlc(t, ctx, "reclaim-bip340", User1.Address, definition.PointTypeBIP340, schnorr.SerializePubKey(publicKey))
	setPtlcTime(ctx, reclaimTestExpiration)

	wrongType, err := schnorr.Sign(privateKey, definition.GetPtlcReclaimMessage(reclaimTestChain, definition.PointTypeED25519, id, User1.Address))
	common.FailIfErr(t, err)
	blocks, err := reclaimWithSignature(ctx, User1.Address, id, wrongType.Serialize())
	common.ExpectError(t, err, constants.ErrInvalidPointSignature)
	expectNoBlocks(t, blocks)

	unlockMessage, err := schnorr.Sign(privateKey, definition.GetPtlcUnlockMessage(reclaimTestChain, definition.PointTypeBIP340, id, User1.Address))
	common.FailIfErr(t, err)
	blocks, err = reclaimWithSignature(ctx, User1.Address, id, unlockMessage.Serialize())
	common.ExpectError(t, err, constants.ErrInvalidPointSignature)
	expectNoBlocks(t, blocks)

	signature, err := schnorr.Sign(privateKey, definition.GetPtlcReclaimMessage(reclaimTestChain, definition.PointTypeBIP340, id, User1.Address))
	common.FailIfErr(t, err)
	blocks, err = reclaimWithSignature(ctx, User1.Address, id, signature.Serialize())
	common.FailIfErr(t, err)
	expectPaid(t, blocks, User1.Address, 7)
	expectEntryGone(t, ctx, id)
}

func TestPtlc_ReclaimWithSignatureNeedsReclaimLock(t *testing.T) {
	ctx := newTestPtlcContext(reclaimTestChain, reclaimTestExpiration)
	id := types.NewHash([]byte("no-reclaim-lock"))
	info := &definition.PtlcInfo{
		Id:             id,
		TimeLocked:     User1.Address,
		TokenStandard:  types.ZnnTokenStandard,
		Amount:         big.NewInt(3),
		ExpirationTime: reclaimTestExpiration,
		PointType:      definition.PointTypeED25519,
		PointLock:      User1.Public,
	}
	common.FailIfErr(t, info.Save(ctx.Storage()))

	for _, key := range []*wallet.KeyPair{User1, reclaimKey} {
		blocks, err := reclaimWithSignature(ctx, User1.Address, id, signReclaim(key, reclaimTestChain, id, User1.Address))
		common.ExpectError(t, err, constants.ErrNoReclaimLock)
		expectNoBlocks(t, blocks)
	}

	blocks, err := reclaimWithSignature(ctx, reclaimCaller.Address, id, signReclaim(User1, reclaimTestChain, id, User1.Address))
	common.ExpectError(t, err, constants.ErrPermissionDenied)
	expectNoBlocks(t, blocks)

	blocks, err = reclaimWithSignature(ctx, User1.Address, types.NewHash([]byte("missing")), signReclaim(User1, reclaimTestChain, id, User1.Address))
	common.ExpectError(t, err, constants.ErrDataNonExistent)
	expectNoBlocks(t, blocks)

	setPtlcTime(ctx, reclaimTestExpiration-1)
	blocks, err = plainReclaim(ctx, User1.Address, id)
	common.ExpectError(t, err, constants.ReclaimNotDue)
	expectNoBlocks(t, blocks)

	setPtlcTime(ctx, reclaimTestExpiration)
	blocks, err = plainReclaim(ctx, User1.Address, id)
	common.FailIfErr(t, err)
	expectPaid(t, blocks, User1.Address, 3)
	expectEntryGone(t, ctx, id)
}

func TestPtlc_PlainReclaimRefusedForReclaimLock(t *testing.T) {
	ctx := newTestPtlcContext(reclaimTestChain, reclaimTestCreateTime)
	id := createReclaimLockedPtlc(t, ctx, "plain-reclaim-refused", User1.Address, definition.PointTypeED25519, reclaimKey.Public)

	for _, now := range []int64{reclaimTestCreateTime, reclaimTestExpiration, reclaimTestExpiration + 1000000} {
		setPtlcTime(ctx, now)
		blocks, err := plainReclaim(ctx, User1.Address, id)
		common.ExpectError(t, err, constants.ErrReclaimSignatureRequired)
		expectNoBlocks(t, blocks)
	}

	_, err := definition.GetPtlcInfo(ctx.Storage(), id)
	common.FailIfErr(t, err)
	_, err = definition.GetPtlcReclaimLock(ctx.Storage(), id)
	common.FailIfErr(t, err)
}

func TestPtlc_UnlockDeletesReclaimLock(t *testing.T) {
	for _, test := range []struct {
		name   string
		unlock func(ctx *testPtlcContext, id types.Hash, witness []byte) ([]*nom.AccountBlock, error)
	}{
		{"unlock", func(ctx *testPtlcContext, id types.Hash, witness []byte) ([]*nom.AccountBlock, error) {
			return (&UnlockPtlcMethod{definition.UnlockPtlcMethodName}).ReceiveBlock(ctx, &nom.AccountBlock{
				Address: reclaimCaller.Address,
				Data:    definition.ABIPtlc.PackMethodPanic(definition.UnlockPtlcMethodName, id, witness),
				Amount:  big.NewInt(0),
			})
		}},
		{"proxy unlock", func(ctx *testPtlcContext, id types.Hash, witness []byte) ([]*nom.AccountBlock, error) {
			return (&ProxyUnlockPtlcMethod{definition.ProxyUnlockPtlcMethodName}).ReceiveBlock(ctx, &nom.AccountBlock{
				Address: otherReclaimKey.Address,
				Data:    definition.ABIPtlc.PackMethodPanic(definition.ProxyUnlockPtlcMethodName, id, reclaimCaller.Address, witness),
				Amount:  big.NewInt(0),
			})
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx := newTestPtlcContext(reclaimTestChain, reclaimTestCreateTime)
			id := createReclaimLockedPtlc(t, ctx, "unlock-deletes-reclaim-lock", User1.Address, definition.PointTypeED25519, reclaimKey.Public)
			setPtlcTime(ctx, reclaimTestExpiration-1)

			reclaimSignature := signReclaim(reclaimKey, reclaimTestChain, id, User1.Address)
			blocks, err := test.unlock(ctx, id, reclaimSignature)
			common.ExpectError(t, err, constants.ErrInvalidPointSignature)
			expectNoBlocks(t, blocks)

			blocks, err = test.unlock(ctx, id, User1.Sign(definition.GetPtlcUnlockMessage(reclaimTestChain, definition.PointTypeED25519, id, reclaimCaller.Address)))
			common.FailIfErr(t, err)
			expectPaid(t, blocks, reclaimCaller.Address, 7)
			expectEntryGone(t, ctx, id)

			setPtlcTime(ctx, reclaimTestExpiration)
			blocks, err = reclaimWithSignature(ctx, User1.Address, id, reclaimSignature)
			common.ExpectError(t, err, constants.ErrDataNonExistent)
			expectNoBlocks(t, blocks)
		})
	}
}

func FuzzPtlcReclaimLockValidateSendBlockNoPanic(f *testing.F) {
	validID := types.NewHash([]byte("fuzz-reclaim-id"))
	f.Add(uint8(0), createWithReclaimLockData(1000, definition.PointTypeED25519, User1.Public, types.ZeroAddress, definition.PointTypeED25519, reclaimKey.Public), int64(2))
	f.Add(uint8(1), definition.ABIPtlc.PackMethodPanic(definition.ReclaimWithSignaturePtlcMethodName, validID, bytes.Repeat([]byte{1}, 64)), int64(1))
	f.Add(uint8(0), []byte{}, int64(0))
	f.Add(uint8(1), []byte{0xff, 0x00, 0x01}, int64(-1))

	methods := []validatePtlcSendBlockMethod{
		&CreateWithReclaimLockPtlcMethod{definition.CreateWithReclaimLockPtlcMethodName},
		&ReclaimWithSignaturePtlcMethod{definition.ReclaimWithSignaturePtlcMethodName},
	}

	f.Fuzz(func(t *testing.T, methodIndex uint8, data []byte, amountKind int64) {
		method := methods[int(methodIndex)%len(methods)]
		_ = method.ValidateSendBlock(&nom.AccountBlock{
			Data:          append([]byte(nil), data...),
			Amount:        fuzzAmount(amountKind),
			TokenStandard: types.ZnnTokenStandard,
		})
	})
}

func FuzzPtlcReclaimSignatureDomainMutationRejected(f *testing.F) {
	id := types.NewHash([]byte("reclaim-domain-fuzz"))
	signature := signReclaim(reclaimKey, reclaimTestChain, id, User1.Address)
	f.Add(reclaimTestChain+1, id.Bytes(), User1.Address.Bytes())
	f.Add(reclaimTestChain, types.NewHash([]byte("other")).Bytes(), User1.Address.Bytes())
	f.Add(reclaimTestChain, id.Bytes(), reclaimCaller.Address.Bytes())

	f.Fuzz(func(t *testing.T, chain uint64, idBytes, creatorBytes []byte) {
		mutatedId := fuzzHash(idBytes)
		mutatedCreator := fuzzAddress(creatorBytes)
		if chain == reclaimTestChain && mutatedId == id && mutatedCreator == User1.Address {
			return
		}
		info := &definition.PtlcInfo{Id: mutatedId, TimeLocked: mutatedCreator}
		lock := &definition.PtlcReclaimLock{Id: mutatedId, PointType: definition.PointTypeED25519, PointLock: reclaimKey.Public}
		if err := verifyPtlcReclaimSignature(info, lock, chain, signature); err != constants.ErrInvalidPointSignature {
			t.Fatalf("mutated reclaim domain: got %v, want %v", err, constants.ErrInvalidPointSignature)
		}
	})
}

func FuzzPtlcReclaimLockCheckAndVerify(f *testing.F) {
	id := types.NewHash([]byte("reclaim-check-verify-fuzz"))
	info := &definition.PtlcInfo{Id: id, TimeLocked: User1.Address}
	bip340PrivateKey, bip340PublicKey := btcec.PrivKeyFromBytes(bytes.Repeat([]byte{5}, 32))
	bip340Signature, err := schnorr.Sign(bip340PrivateKey, definition.GetPtlcReclaimMessage(reclaimTestChain, definition.PointTypeBIP340, id, User1.Address))
	if err != nil {
		f.Fatalf("sign BIP340 seed: %v", err)
	}

	valid := []struct {
		pointType uint8
		pointLock []byte
		signature []byte
	}{
		{definition.PointTypeED25519, reclaimKey.Public, signReclaim(reclaimKey, reclaimTestChain, id, User1.Address)},
		{definition.PointTypeBIP340, schnorr.SerializePubKey(bip340PublicKey), bip340Signature.Serialize()},
	}
	for _, seed := range valid {
		lock := &definition.PtlcReclaimLock{Id: id, PointType: seed.pointType, PointLock: seed.pointLock}
		if err := verifyPtlcReclaimSignature(info, lock, reclaimTestChain, seed.signature); err != nil {
			f.Fatalf("point type %d: the seed signature does not verify: %v", seed.pointType, err)
		}
		f.Add(seed.pointType, seed.pointLock, seed.signature)
	}
	f.Add(definition.PointTypeSecp256k1Point, pointOf(pointScalar(7)), pointScalar(7))
	f.Add(definition.PointTypeED25519, []byte{}, []byte{})
	f.Add(definition.PointTypeED25519, append([]byte{1}, make([]byte, 31)...), make([]byte, 64))
	f.Add(definition.PointTypeBIP340, bytes.Repeat([]byte{0xff}, 32), bytes.Repeat([]byte{0xff}, 64))
	f.Add(uint8(99), bytes.Repeat([]byte{1}, 32), bytes.Repeat([]byte{1}, 64))

	f.Fuzz(func(t *testing.T, pointType uint8, pointLock, signature []byte) {
		checked := checkReclaimLock(pointType, pointLock)
		switch checked {
		case nil:
			if !isReclaimPointType(pointType) || len(pointLock) != int(definition.PointTypePubKeySizes[pointType]) {
				t.Fatalf("accepted a reclaim lock of type %d and %d bytes", pointType, len(pointLock))
			}
		case constants.ErrInvalidPointType:
			if isReclaimPointType(pointType) {
				t.Fatalf("refused reclaim point type %d as a type", pointType)
			}
		case constants.ErrInvalidPointLock:
			if !isReclaimPointType(pointType) {
				t.Fatalf("judged the lock of point type %d, which a reclaim lock cannot have", pointType)
			}
		default:
			t.Fatalf("unexpected reclaim lock error: %v", checked)
		}

		lock := &definition.PtlcReclaimLock{Id: id, PointType: pointType, PointLock: append([]byte(nil), pointLock...)}
		verified := verifyPtlcReclaimSignature(info, lock, reclaimTestChain, signature)
		switch verified {
		case nil, constants.ErrInvalidPointSignature:
			if checked != nil {
				t.Fatalf("judged a signature under a refused reclaim lock (%v): %v", checked, verified)
			}
		case constants.ErrInvalidPointType, constants.ErrInvalidPointLock:
			if verified != checked {
				t.Fatalf("verification answered %v for a reclaim lock checked as %v", verified, checked)
			}
		default:
			t.Fatalf("unexpected reclaim signature error: %v", verified)
		}
	})
}

func TestPtlc_UnlockWithoutReclaimLockWritesNothingElse(t *testing.T) {
	ctx := newTestPtlcContext(reclaimTestChain, reclaimTestCreateTime)
	id := types.NewHash([]byte("unlock-no-reclaim-lock"))
	info := &definition.PtlcInfo{
		Id:             id,
		TimeLocked:     User1.Address,
		TokenStandard:  types.ZnnTokenStandard,
		Amount:         big.NewInt(1),
		ExpirationTime: reclaimTestExpiration,
		PointType:      definition.PointTypeED25519,
		PointLock:      User1.Public,
	}
	common.FailIfErr(t, info.Save(ctx.Storage()))
	before, err := ctx.Storage().Changes()
	common.FailIfErr(t, err)

	signature := User1.Sign(definition.GetPtlcUnlockMessage(reclaimTestChain, definition.PointTypeED25519, id, User1.Address))
	_, err = unlockPtlc(ctx, &nom.AccountBlock{Address: User1.Address}, id, User1.Address, signature)
	common.FailIfErr(t, err)

	after, err := ctx.Storage().Changes()
	common.FailIfErr(t, err)
	if bytes.Equal(before.Dump(), after.Dump()) {
		t.Fatal("the unlock wrote nothing")
	}

	reference := newTestPtlcContext(reclaimTestChain, reclaimTestCreateTime)
	common.FailIfErr(t, info.Save(reference.Storage()))
	common.FailIfErr(t, info.Delete(reference.Storage()))
	expected, err := reference.Storage().Changes()
	common.FailIfErr(t, err)
	if !bytes.Equal(after.Dump(), expected.Dump()) {
		t.Fatalf("unlock of an entry without a reclaim lock changed more than the entry:\n%x\nwant\n%x", after.Dump(), expected.Dump())
	}

	probe := newTestPtlcContext(reclaimTestChain, reclaimTestCreateTime)
	common.FailIfErr(t, info.Save(probe.Storage()))
	common.FailIfErr(t, info.Delete(probe.Storage()))
	common.FailIfErr(t, (&definition.PtlcReclaimLock{Id: id}).Delete(probe.Storage()))
	probed, err := probe.Storage().Changes()
	common.FailIfErr(t, err)
	if bytes.Equal(probed.Dump(), expected.Dump()) {
		t.Fatal("deleting a missing reclaim lock no longer shows in the patch; this test cannot see an unconditional delete")
	}
}
