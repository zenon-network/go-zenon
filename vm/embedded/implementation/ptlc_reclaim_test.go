package implementation

import (
	"bytes"
	"fmt"
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

func reclaimTestTerms(creator types.Address) *definition.PtlcInfo {
	return &definition.PtlcInfo{
		TimeLocked:     creator,
		TokenStandard:  types.ZnnTokenStandard,
		Amount:         big.NewInt(7),
		ExpirationTime: reclaimTestExpiration,
		PointType:      definition.PointTypeED25519,
		PointLock:      User1.Public,
		Destination:    types.ZeroAddress,
	}
}

func createReclaimLockedPtlcWithTerms(t *testing.T, ctx *testPtlcContext, name string, terms *definition.PtlcInfo, reclaimPointType uint8, reclaimPointLock []byte) *definition.PtlcInfo {
	t.Helper()
	sendBlock := &nom.AccountBlock{
		Address:       terms.TimeLocked,
		Hash:          types.NewHash([]byte(name)),
		TokenStandard: terms.TokenStandard,
		Amount:        new(big.Int).Set(terms.Amount),
		Data:          createWithReclaimLockData(terms.ExpirationTime, terms.PointType, terms.PointLock, terms.Destination, reclaimPointType, reclaimPointLock),
	}
	blocks, err := (&CreateWithReclaimLockPtlcMethod{definition.CreateWithReclaimLockPtlcMethodName}).ReceiveBlock(ctx, sendBlock)
	common.FailIfErr(t, err)
	if blocks != nil {
		t.Fatalf("create should not emit contract sends, got %d", len(blocks))
	}
	entry, err := definition.GetPtlcInfo(ctx.Storage(), sendBlock.Hash)
	common.FailIfErr(t, err)
	return entry
}

func createReclaimLockedPtlc(t *testing.T, ctx *testPtlcContext, name string, creator types.Address, reclaimPointType uint8, reclaimPointLock []byte) types.Hash {
	t.Helper()
	return createReclaimLockedPtlcWithTerms(t, ctx, name, reclaimTestTerms(creator), reclaimPointType, reclaimPointLock).Id
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

func signReclaim(key *wallet.KeyPair, chain uint64, entry *definition.PtlcInfo) []byte {
	return key.Sign(definition.GetPtlcReclaimMessage(chain, definition.PointTypeED25519, entry))
}

func ptlcReclaimMessageForContract(chainIdentifier uint64, contract types.Address, reclaimPointType uint8, entry *definition.PtlcInfo) []byte {
	return crypto.Hash(common.JoinBytes(
		[]byte(definition.PtlcReclaimMessageDomain),
		common.Uint64ToBytes(chainIdentifier),
		contract.Bytes(),
		[]byte{reclaimPointType},
		entry.TimeLocked.Bytes(),
		entry.Destination.Bytes(),
		entry.TokenStandard.Bytes(),
		common.BigIntToBytes(entry.Amount),
		common.Uint64ToBytes(uint64(entry.ExpirationTime)),
		[]byte{entry.PointType},
		entry.PointLock,
	))
}

func withReclaimTerms(entry *definition.PtlcInfo, edit func(*definition.PtlcInfo)) *definition.PtlcInfo {
	changed := *entry
	changed.Amount = new(big.Int).Set(entry.Amount)
	changed.PointLock = append([]byte(nil), entry.PointLock...)
	edit(&changed)
	return &changed
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
	terms := reclaimTestTerms(User1.Address)
	valid := signReclaim(reclaimKey, reclaimTestChain, terms)

	entry := createReclaimLockedPtlcWithTerms(t, ctx, "reclaim-with-signature", terms, definition.PointTypeED25519, reclaimKey.Public)
	id := entry.Id
	if !bytes.Equal(definition.GetPtlcReclaimMessage(reclaimTestChain, definition.PointTypeED25519, entry), definition.GetPtlcReclaimMessage(reclaimTestChain, definition.PointTypeED25519, terms)) {
		t.Fatal("the stored entry's reclaim message differs from the one signed before the create")
	}
	otherTerms := withReclaimTerms(terms, func(e *definition.PtlcInfo) { e.Amount = big.NewInt(8) })
	otherId := createReclaimLockedPtlcWithTerms(t, ctx, "reclaim-with-signature-other", otherTerms, definition.PointTypeED25519, reclaimKey.Public).Id
	sameTermsId := createReclaimLockedPtlcWithTerms(t, ctx, "reclaim-with-signature-same-terms", terms, definition.PointTypeED25519, reclaimKey.Public).Id

	blocks, err := reclaimWithSignature(ctx, User1.Address, id, valid)
	common.ExpectError(t, err, constants.ReclaimNotDue)
	expectNoBlocks(t, blocks)

	setPtlcTime(ctx, reclaimTestExpiration-1)
	blocks, err = reclaimWithSignature(ctx, User1.Address, id, valid)
	common.ExpectError(t, err, constants.ReclaimNotDue)
	expectNoBlocks(t, blocks)

	setPtlcTime(ctx, reclaimTestExpiration)

	if !bytes.Equal(ptlcReclaimMessageForContract(reclaimTestChain, types.PtlcContract, definition.PointTypeED25519, entry), definition.GetPtlcReclaimMessage(reclaimTestChain, definition.PointTypeED25519, entry)) {
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
		"another key":              signReclaim(otherReclaimKey, reclaimTestChain, entry),
		"the claim key":            signReclaim(User1, reclaimTestChain, entry),
		"another chain":            signReclaim(reclaimKey, reclaimTestChain+1, entry),
		"another creator":          signReclaim(reclaimKey, reclaimTestChain, withReclaimTerms(entry, func(e *definition.PtlcInfo) { e.TimeLocked = reclaimCaller.Address })),
		"another destination":      signReclaim(reclaimKey, reclaimTestChain, withReclaimTerms(entry, func(e *definition.PtlcInfo) { e.Destination = reclaimCaller.Address })),
		"another token":            signReclaim(reclaimKey, reclaimTestChain, withReclaimTerms(entry, func(e *definition.PtlcInfo) { e.TokenStandard = types.QsrTokenStandard })),
		"another amount":           signReclaim(reclaimKey, reclaimTestChain, otherTerms),
		"another expiration":       signReclaim(reclaimKey, reclaimTestChain, withReclaimTerms(entry, func(e *definition.PtlcInfo) { e.ExpirationTime++ })),
		"another point type":       signReclaim(reclaimKey, reclaimTestChain, withReclaimTerms(entry, func(e *definition.PtlcInfo) { e.PointType = definition.PointTypeBIP340 })),
		"another point lock":       signReclaim(reclaimKey, reclaimTestChain, withReclaimTerms(entry, func(e *definition.PtlcInfo) { e.PointLock = reclaimCaller.Public })),
		"bip340 reclaim type":      reclaimKey.Sign(definition.GetPtlcReclaimMessage(reclaimTestChain, definition.PointTypeBIP340, entry)),
		"the unlock message":       reclaimKey.Sign(definition.GetPtlcUnlockMessage(reclaimTestChain, definition.PointTypeED25519, id, User1.Address)),
		"the raw entry id":         reclaimKey.Sign(id.Bytes()),
		"s + L":                    shifted,
		"flipped bit in R":         append([]byte{valid[0] ^ 1}, valid[1:]...),
		"flipped bit in s":         append(append([]byte{}, valid[:40]...), append([]byte{valid[40] ^ 1}, valid[41:]...)...),
		"another contract address": reclaimKey.Sign(ptlcReclaimMessageForContract(reclaimTestChain, types.HtlcContract, definition.PointTypeED25519, entry)),
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

	blocks, err = reclaimWithSignature(ctx, User1.Address, otherId, signReclaim(reclaimKey, reclaimTestChain, otherTerms))
	common.FailIfErr(t, err)
	expectPaid(t, blocks, User1.Address, 8)
	expectEntryGone(t, ctx, otherId)

	blocks, err = reclaimWithSignature(ctx, User1.Address, sameTermsId, valid)
	common.FailIfErr(t, err)
	expectPaid(t, blocks, User1.Address, 7)
	expectEntryGone(t, ctx, sameTermsId)
}

func TestPtlc_ReclaimWithSignatureBIP340(t *testing.T) {
	privateKey, publicKey := btcec.PrivKeyFromBytes(bytes.Repeat([]byte{5}, 32))
	ctx := newTestPtlcContext(reclaimTestChain, reclaimTestExpiration)
	setPtlcTime(ctx, reclaimTestCreateTime)
	terms := reclaimTestTerms(User1.Address)
	signature, err := schnorr.Sign(privateKey, definition.GetPtlcReclaimMessage(reclaimTestChain, definition.PointTypeBIP340, terms))
	common.FailIfErr(t, err)
	entry := createReclaimLockedPtlcWithTerms(t, ctx, "reclaim-bip340", terms, definition.PointTypeBIP340, schnorr.SerializePubKey(publicKey))
	id := entry.Id
	setPtlcTime(ctx, reclaimTestExpiration)

	wrongType, err := schnorr.Sign(privateKey, definition.GetPtlcReclaimMessage(reclaimTestChain, definition.PointTypeED25519, entry))
	common.FailIfErr(t, err)
	blocks, err := reclaimWithSignature(ctx, User1.Address, id, wrongType.Serialize())
	common.ExpectError(t, err, constants.ErrInvalidPointSignature)
	expectNoBlocks(t, blocks)

	unlockMessage, err := schnorr.Sign(privateKey, definition.GetPtlcUnlockMessage(reclaimTestChain, definition.PointTypeBIP340, id, User1.Address))
	common.FailIfErr(t, err)
	blocks, err = reclaimWithSignature(ctx, User1.Address, id, unlockMessage.Serialize())
	common.ExpectError(t, err, constants.ErrInvalidPointSignature)
	expectNoBlocks(t, blocks)

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
		blocks, err := reclaimWithSignature(ctx, User1.Address, id, signReclaim(key, reclaimTestChain, info))
		common.ExpectError(t, err, constants.ErrNoReclaimLock)
		expectNoBlocks(t, blocks)
	}

	blocks, err := reclaimWithSignature(ctx, reclaimCaller.Address, id, signReclaim(User1, reclaimTestChain, info))
	common.ExpectError(t, err, constants.ErrPermissionDenied)
	expectNoBlocks(t, blocks)

	blocks, err = reclaimWithSignature(ctx, User1.Address, types.NewHash([]byte("missing")), signReclaim(User1, reclaimTestChain, info))
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
			entry := createReclaimLockedPtlcWithTerms(t, ctx, "unlock-deletes-reclaim-lock", reclaimTestTerms(User1.Address), definition.PointTypeED25519, reclaimKey.Public)
			id := entry.Id
			setPtlcTime(ctx, reclaimTestExpiration-1)

			reclaimSignature := signReclaim(reclaimKey, reclaimTestChain, entry)
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

func fuzzTokenStandard(data []byte) types.ZenonTokenStandard {
	var token types.ZenonTokenStandard
	if token.SetBytes(data) == nil {
		return token
	}
	return types.NewZenonTokenStandard(data)
}

func FuzzPtlcReclaimSignatureDomainMutationRejected(f *testing.F) {
	terms := reclaimTestTerms(User1.Address)
	signature := signReclaim(reclaimKey, reclaimTestChain, terms)
	id := types.NewHash([]byte("reclaim-domain-fuzz"))
	seed := func(chain uint64, edit func(*definition.PtlcInfo)) {
		e := withReclaimTerms(terms, edit)
		f.Add(chain, id.Bytes(), e.TimeLocked.Bytes(), e.Destination.Bytes(), e.TokenStandard.Bytes(), e.Amount.Bytes(), e.ExpirationTime, e.PointType, e.PointLock)
	}
	seed(reclaimTestChain, func(*definition.PtlcInfo) {})
	seed(reclaimTestChain+1, func(*definition.PtlcInfo) {})
	seed(reclaimTestChain, func(e *definition.PtlcInfo) { e.TimeLocked = reclaimCaller.Address })
	seed(reclaimTestChain, func(e *definition.PtlcInfo) { e.Destination = reclaimCaller.Address })
	seed(reclaimTestChain, func(e *definition.PtlcInfo) { e.TokenStandard = types.QsrTokenStandard })
	seed(reclaimTestChain, func(e *definition.PtlcInfo) { e.Amount = big.NewInt(8) })
	seed(reclaimTestChain, func(e *definition.PtlcInfo) { e.ExpirationTime++ })
	seed(reclaimTestChain, func(e *definition.PtlcInfo) { e.PointType = definition.PointTypeBIP340 })
	seed(reclaimTestChain, func(e *definition.PtlcInfo) { e.PointLock = reclaimCaller.Public })

	f.Fuzz(func(t *testing.T, chain uint64, idBytes, creatorBytes, destinationBytes, tokenBytes, amountBytes []byte, expiration int64, pointType uint8, pointLock []byte) {
		if len(amountBytes) > 32 {
			amountBytes = amountBytes[:32]
		}
		info := &definition.PtlcInfo{
			Id:             fuzzHash(idBytes),
			TimeLocked:     fuzzAddress(creatorBytes),
			Destination:    fuzzAddress(destinationBytes),
			TokenStandard:  fuzzTokenStandard(tokenBytes),
			Amount:         new(big.Int).SetBytes(amountBytes),
			ExpirationTime: expiration,
			PointType:      pointType,
			PointLock:      append([]byte(nil), pointLock...),
		}
		lock := &definition.PtlcReclaimLock{Id: info.Id, PointType: definition.PointTypeED25519, PointLock: reclaimKey.Public}
		err := verifyPtlcReclaimSignature(info, lock, chain, signature)

		sameTerms := chain == reclaimTestChain &&
			info.TimeLocked == terms.TimeLocked &&
			info.Destination == terms.Destination &&
			info.TokenStandard == terms.TokenStandard &&
			info.Amount.Cmp(terms.Amount) == 0 &&
			info.ExpirationTime == terms.ExpirationTime &&
			info.PointType == terms.PointType &&
			bytes.Equal(info.PointLock, terms.PointLock)
		if sameTerms && err != nil {
			t.Fatalf("entry %s has the signed terms and was refused: %v", info.Id, err)
		}
		if !sameTerms && err != constants.ErrInvalidPointSignature {
			t.Fatalf("mutated reclaim terms: got %v, want %v", err, constants.ErrInvalidPointSignature)
		}
	})
}

func FuzzPtlcReclaimLockCheckAndVerify(f *testing.F) {
	id := types.NewHash([]byte("reclaim-check-verify-fuzz"))
	info := reclaimTestTerms(User1.Address)
	info.Id = id
	bip340PrivateKey, bip340PublicKey := btcec.PrivKeyFromBytes(bytes.Repeat([]byte{5}, 32))
	bip340Signature, err := schnorr.Sign(bip340PrivateKey, definition.GetPtlcReclaimMessage(reclaimTestChain, definition.PointTypeBIP340, info))
	if err != nil {
		f.Fatalf("sign BIP340 seed: %v", err)
	}

	valid := []struct {
		pointType uint8
		pointLock []byte
		signature []byte
	}{
		{definition.PointTypeED25519, reclaimKey.Public, signReclaim(reclaimKey, reclaimTestChain, info)},
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

func reclaimTestUnlock(ctx *testPtlcContext, id types.Hash, witness []byte) ([]*nom.AccountBlock, error) {
	return (&UnlockPtlcMethod{definition.UnlockPtlcMethodName}).ReceiveBlock(ctx, &nom.AccountBlock{
		Address: reclaimCaller.Address,
		Data:    definition.ABIPtlc.PackMethodPanic(definition.UnlockPtlcMethodName, id, witness),
		Amount:  big.NewInt(0),
	})
}

func reclaimTestProxyUnlock(ctx *testPtlcContext, id types.Hash, witness []byte) ([]*nom.AccountBlock, error) {
	return (&ProxyUnlockPtlcMethod{definition.ProxyUnlockPtlcMethodName}).ReceiveBlock(ctx, &nom.AccountBlock{
		Address: otherReclaimKey.Address,
		Data:    definition.ABIPtlc.PackMethodPanic(definition.ProxyUnlockPtlcMethodName, id, reclaimCaller.Address, witness),
		Amount:  big.NewInt(0),
	})
}

func TestPtlc_ReclaimSignatureSizeFitsEveryReclaimType(t *testing.T) {
	count := 0
	for pointType := 0; pointType <= 255; pointType++ {
		if !isReclaimPointType(uint8(pointType)) {
			continue
		}
		count++
		if size := int(definition.PointTypeWitnessSizes[uint8(pointType)]); size != definition.PtlcReclaimSignatureSize {
			t.Errorf("point type %d signs with %d bytes, but a reclaim send block is checked for %d", pointType, size, definition.PtlcReclaimSignatureSize)
		}
	}
	if count != 2 {
		t.Fatalf("found %d reclaim lock types, want Ed25519 and BIP-340", count)
	}
}

func TestPtlc_ReclaimAndUnlockInOneMomentum(t *testing.T) {
	for _, unlock := range []struct {
		name string
		call func(ctx *testPtlcContext, id types.Hash, witness []byte) ([]*nom.AccountBlock, error)
	}{
		{"unlock", reclaimTestUnlock},
		{"proxy unlock", reclaimTestProxyUnlock},
	} {
		for _, now := range []int64{reclaimTestExpiration - 1, reclaimTestExpiration} {
			for _, reclaimFirst := range []bool{true, false} {
				name := fmt.Sprintf("%s, time %d, reclaim first %v", unlock.name, now, reclaimFirst)
				t.Run(name, func(t *testing.T) {
					ctx := newTestPtlcContext(reclaimTestChain, reclaimTestCreateTime)
					entry := createReclaimLockedPtlcWithTerms(t, ctx, name, reclaimTestTerms(User1.Address), definition.PointTypeED25519, reclaimKey.Public)
					setPtlcTime(ctx, now)

					reclaim := func() ([]*nom.AccountBlock, error) {
						return reclaimWithSignature(ctx, User1.Address, entry.Id, signReclaim(reclaimKey, reclaimTestChain, entry))
					}
					claim := func() ([]*nom.AccountBlock, error) {
						return unlock.call(ctx, entry.Id, User1.Sign(definition.GetPtlcUnlockMessage(reclaimTestChain, definition.PointTypeED25519, entry.Id, reclaimCaller.Address)))
					}
					calls := []func() ([]*nom.AccountBlock, error){claim, reclaim}
					if reclaimFirst {
						calls = []func() ([]*nom.AccountBlock, error){reclaim, claim}
					}

					var paid []*nom.AccountBlock
					for _, call := range calls {
						blocks, err := call()
						if err == nil {
							paid = append(paid, blocks...)
						} else {
							expectNoBlocks(t, blocks)
						}
					}

					if now < reclaimTestExpiration {
						expectPaid(t, paid, reclaimCaller.Address, 7)
					} else {
						expectPaid(t, paid, User1.Address, 7)
					}
					expectEntryGone(t, ctx, entry.Id)
				})
			}
		}
	}
}

func TestPtlc_SameKeyLocksClaimAndReclaim(t *testing.T) {
	bip340Private, bip340Public := btcec.PrivKeyFromBytes(bytes.Repeat([]byte{7}, 32))
	for _, test := range []struct {
		name      string
		pointType uint8
		public    []byte
		sign      func(t *testing.T, message []byte) []byte
	}{
		{"ed25519", definition.PointTypeED25519, reclaimKey.Public, func(t *testing.T, message []byte) []byte {
			return reclaimKey.Sign(message)
		}},
		{"bip340", definition.PointTypeBIP340, schnorr.SerializePubKey(bip340Public), func(t *testing.T, message []byte) []byte {
			signature, err := schnorr.Sign(bip340Private, message)
			common.FailIfErr(t, err)
			return signature.Serialize()
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx := newTestPtlcContext(reclaimTestChain, reclaimTestCreateTime)
			terms := withReclaimTerms(reclaimTestTerms(User1.Address), func(e *definition.PtlcInfo) {
				e.PointType = test.pointType
				e.PointLock = test.public
			})
			entry := createReclaimLockedPtlcWithTerms(t, ctx, "same-key-"+test.name, terms, test.pointType, test.public)
			claimed := createReclaimLockedPtlcWithTerms(t, ctx, "same-key-claimed-"+test.name, terms, test.pointType, test.public)
			reclaimSignature := test.sign(t, definition.GetPtlcReclaimMessage(reclaimTestChain, test.pointType, entry))

			setPtlcTime(ctx, reclaimTestExpiration-1)
			for name, call := range map[string]func(ctx *testPtlcContext, id types.Hash, witness []byte) ([]*nom.AccountBlock, error){
				"unlock":       reclaimTestUnlock,
				"proxy unlock": reclaimTestProxyUnlock,
			} {
				blocks, err := call(ctx, entry.Id, reclaimSignature)
				if err != constants.ErrInvalidPointSignature {
					t.Errorf("a reclaim signature as the %s witness: got %v, want %v", name, err, constants.ErrInvalidPointSignature)
				}
				expectNoBlocks(t, blocks)
			}
			blocks, err := reclaimTestUnlock(ctx, claimed.Id, test.sign(t, definition.GetPtlcUnlockMessage(reclaimTestChain, test.pointType, claimed.Id, reclaimCaller.Address)))
			common.FailIfErr(t, err)
			expectPaid(t, blocks, reclaimCaller.Address, 7)

			setPtlcTime(ctx, reclaimTestExpiration)
			for _, destination := range []types.Address{User1.Address, reclaimCaller.Address} {
				unlockSignature := test.sign(t, definition.GetPtlcUnlockMessage(reclaimTestChain, test.pointType, entry.Id, destination))
				blocks, err := reclaimWithSignature(ctx, User1.Address, entry.Id, unlockSignature)
				common.ExpectError(t, err, constants.ErrInvalidPointSignature)
				expectNoBlocks(t, blocks)
			}

			blocks, err = reclaimWithSignature(ctx, User1.Address, entry.Id, reclaimSignature)
			common.FailIfErr(t, err)
			expectPaid(t, blocks, User1.Address, 7)
			expectEntryGone(t, ctx, entry.Id)
		})
	}
}
