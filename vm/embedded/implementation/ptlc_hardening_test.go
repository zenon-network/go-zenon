package implementation

import (
	"bytes"
	"crypto/ed25519"
	"math/big"
	"testing"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/btcec/v2/schnorr"

	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/vm/constants"
	"github.com/zenon-network/go-zenon/vm/embedded/definition"
)

var (
	unregisteredContract = types.Address{types.ContractAddrByte, 0xde, 0xad, 0xbe, 0xef}
	firstReservedAddress = types.Address{types.ContractAddrByte + 1, 0xde, 0xad, 0xbe, 0xef}
	lastReservedAddress  = types.Address{0xff, 0xde, 0xad, 0xbe, 0xef}

	ed25519SmallOrderLocks = map[string]string{
		"identity":                   "0100000000000000000000000000000000000000000000000000000000000000",
		"order 2":                    "ecffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff7f",
		"order 4":                    "0000000000000000000000000000000000000000000000000000000000000000",
		"order 4, negated":           "0000000000000000000000000000000000000000000000000000000000000080",
		"order 8, first":             "26e8958fc2b227b045c3f489f2ef98f0d5dfac05d3c63339b13802886d53fc05",
		"order 8, first, negated":    "26e8958fc2b227b045c3f489f2ef98f0d5dfac05d3c63339b13802886d53fc85",
		"order 8, second":            "c7176a703d4dd84fba3c0b760d10670f2a2053fa2c39ccc64ec7fd7792ac037a",
		"order 8, second, negated":   "c7176a703d4dd84fba3c0b760d10670f2a2053fa2c39ccc64ec7fd7792ac03fa",
		"identity, sign bit set":     "0100000000000000000000000000000000000000000000000000000000000080",
		"order 2, sign bit set":      "ecffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff",
		"order 4 as y = p":           "edffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff7f",
		"order 4 as y = p, negated":  "edffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff",
		"identity as y = p + 1":      "eeffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff7f",
		"identity as y = p + 1, neg": "eeffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff",
	}

	ed25519GroupOrder = []byte{
		0xed, 0xd3, 0xf5, 0x5c, 0x1a, 0x63, 0x12, 0x58, 0xd6, 0x9c, 0xf7, 0xa2, 0xde, 0xf9, 0xde, 0x14,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x10,
	}
)

func TestPtlc_UnlockWitnessSizeBound(t *testing.T) {
	id := types.NewHash([]byte("witness-size"))
	methods := map[string]struct {
		method validatePtlcSendBlockMethod
		data   func(witness []byte) []byte
	}{
		"unlock": {
			method: &UnlockPtlcMethod{definition.UnlockPtlcMethodName},
			data: func(witness []byte) []byte {
				return definition.ABIPtlc.PackMethodPanic(definition.UnlockPtlcMethodName, id, witness)
			},
		},
		"proxy unlock": {
			method: &ProxyUnlockPtlcMethod{definition.ProxyUnlockPtlcMethodName},
			data: func(witness []byte) []byte {
				return definition.ABIPtlc.PackMethodPanic(definition.ProxyUnlockPtlcMethodName, id, User1.Address, witness)
			},
		},
	}
	sizes := map[int]error{
		0:                           constants.ErrInvalidPointSignature,
		31:                          constants.ErrInvalidPointSignature,
		32:                          nil,
		33:                          constants.ErrInvalidPointSignature,
		63:                          constants.ErrInvalidPointSignature,
		64:                          nil,
		65:                          constants.ErrInvalidPointSignature,
		constants.MaxDataLength + 1: constants.ErrInvalidPointSignature,
		1 << 20:                     constants.ErrInvalidPointSignature,
	}

	for name, m := range methods {
		for size, want := range sizes {
			block := &nom.AccountBlock{Amount: big.NewInt(0), Data: m.data(bytes.Repeat([]byte{1}, size))}
			sent := len(block.Data)
			err := m.method.ValidateSendBlock(block)
			if err != want {
				t.Errorf("%s, %d-byte witness: got %v, want %v", name, size, err, want)
			}
			if want != nil && len(block.Data) != sent {
				t.Errorf("%s, %d-byte witness: the refused block was repacked", name, size)
			}
		}
	}
}

func TestPtlc_ED25519SmallOrderLockRejected(t *testing.T) {
	common.ExpectError(t, checkPointLock(definition.PointTypeED25519, User1.Public), nil)

	for name, encoded := range ed25519SmallOrderLocks {
		lock := mustDecodeHex(t, encoded)
		if err := checkPointLock(definition.PointTypeED25519, lock); err != constants.ErrInvalidPointLock {
			t.Errorf("%s: got %v, want %v", name, err, constants.ErrInvalidPointLock)
		}
		err := checkStoredPtlcInfo(&definition.PtlcInfo{
			Amount: big.NewInt(1), ExpirationTime: 1000000000,
			PointType: definition.PointTypeED25519, PointLock: lock,
		})
		if err != constants.ErrInvalidPointLock {
			t.Errorf("%s, as stored state: got %v, want %v", name, err, constants.ErrInvalidPointLock)
		}
	}
}

func TestPtlc_ED25519IdentityLockNeedsNoSecret(t *testing.T) {
	identity := mustDecodeHex(t, ed25519SmallOrderLocks["identity"])
	forged := append(append([]byte{}, identity...), make([]byte, 32)...)
	message := definition.GetPtlcUnlockMessage(100, definition.PointTypeED25519, types.NewHash([]byte("any")), User1.Address)
	if !ed25519.Verify(identity, message, forged) {
		t.Fatalf("the forged signature no longer verifies under the identity key")
	}

	data := definition.ABIPtlc.PackMethodPanic(definition.CreatePtlcMethodName, defaultPtlc.ExpirationTime, definition.PointTypeED25519, identity, types.ZeroAddress)
	method := &CreatePtlcMethod{definition.CreatePtlcMethodName}
	common.ExpectError(t, method.ValidateSendBlock(&nom.AccountBlock{Amount: big.NewInt(1), Data: data}), constants.ErrInvalidPointLock)
}

func TestPtlc_ED25519SignatureScalarOutOfRange(t *testing.T) {
	chainIdentifier := uint64(100)
	id := types.NewHash([]byte("s-out-of-range"))
	info := &definition.PtlcInfo{
		Id: id, TimeLocked: User1.Address, TokenStandard: types.ZnnTokenStandard, Amount: big.NewInt(1),
		ExpirationTime: 1000000000, PointType: definition.PointTypeED25519, PointLock: User1.Public,
	}
	signature := User1.Sign(definition.GetPtlcUnlockMessage(chainIdentifier, info.PointType, id, User1.Address))
	common.ExpectError(t, verifyPtlcSignature(info, chainIdentifier, id, User1.Address, signature), nil)

	shifted := append([]byte{}, signature...)
	carry := 0
	for i := 0; i < 32; i++ {
		sum := int(shifted[32+i]) + int(ed25519GroupOrder[i]) + carry
		shifted[32+i] = byte(sum)
		carry = sum >> 8
	}
	if carry != 0 {
		t.Fatalf("s + L does not fit 32 bytes for this signature")
	}
	common.ExpectError(t, verifyPtlcSignature(info, chainIdentifier, id, User1.Address, shifted), constants.ErrInvalidPointSignature)
}

func TestPtlc_BIP340AlternateNonceAccepted(t *testing.T) {
	privateKey, publicKey := btcec.PrivKeyFromBytes(bytes.Repeat([]byte{7}, 32))
	chainIdentifier := uint64(100)
	id := types.NewHash([]byte("bip340-alternate-nonce"))
	info := &definition.PtlcInfo{
		Id: id, TimeLocked: User1.Address, TokenStandard: types.ZnnTokenStandard, Amount: big.NewInt(1),
		ExpirationTime: 1000000000, PointType: definition.PointTypeBIP340, PointLock: schnorr.SerializePubKey(publicKey),
	}
	message := definition.GetPtlcUnlockMessage(chainIdentifier, info.PointType, id, User1.Address)

	first, err := schnorr.Sign(privateKey, message, schnorr.CustomNonce([32]byte{1}))
	common.FailIfErr(t, err)
	second, err := schnorr.Sign(privateKey, message, schnorr.CustomNonce([32]byte{2}))
	common.FailIfErr(t, err)
	if bytes.Equal(first.Serialize(), second.Serialize()) {
		t.Fatalf("two nonces gave one signature")
	}
	common.ExpectError(t, verifyPtlcSignature(info, chainIdentifier, id, User1.Address, first.Serialize()), nil)
	common.ExpectError(t, verifyPtlcSignature(info, chainIdentifier, id, User1.Address, second.Serialize()), nil)
}

func TestPtlc_CreateRejectsUnregisteredContractDestination(t *testing.T) {
	for name, pointType := range map[string]uint8{"ED25519": definition.PointTypeED25519, "point": definition.PointTypeSecp256k1Point} {
		lock := []byte(User1.Public)
		if pointType == definition.PointTypeSecp256k1Point {
			lock = pointOf(pointScalar(7))
		}
		param := definition.CreatePtlcParam{ExpirationTime: 1000000000, PointType: pointType, PointLock: lock, Destination: unregisteredContract}
		if err := checkPtlc(param); err != constants.ErrInvalidDestination {
			t.Errorf("%s lock: got %v, want %v", name, err, constants.ErrInvalidDestination)
		}
	}
}

func TestPtlc_ProxyUnlockRejectsUnpayableDestination(t *testing.T) {
	id := types.NewHash([]byte("unpayable"))
	method := &ProxyUnlockPtlcMethod{definition.ProxyUnlockPtlcMethodName}
	for name, c := range map[string]struct {
		destination types.Address
		want        error
	}{
		"an account":               {User1.Address, nil},
		"the zero address":         {types.ZeroAddress, constants.ErrInvalidDestination},
		"an embedded contract":     {types.PtlcContract, constants.ErrInvalidDestination},
		"an unregistered contract": {unregisteredContract, constants.ErrInvalidDestination},
		"the first reserved type":  {firstReservedAddress, constants.ErrInvalidDestination},
		"the last reserved type":   {lastReservedAddress, constants.ErrInvalidDestination},
	} {
		data := definition.ABIPtlc.PackMethodPanic(definition.ProxyUnlockPtlcMethodName, id, c.destination, bytes.Repeat([]byte{1}, 64))
		if err := method.ValidateSendBlock(&nom.AccountBlock{Amount: big.NewInt(0), Data: data}); err != c.want {
			t.Errorf("%s: got %v, want %v", name, err, c.want)
		}
	}
}

func TestPtlc_WildcardLockDoesNotPayUnpayableDestination(t *testing.T) {
	chainIdentifier := uint64(100)
	for name, destination := range map[string]types.Address{
		"the zero address":         types.ZeroAddress,
		"an embedded contract":     types.PtlcContract,
		"an unregistered contract": unregisteredContract,
		"the first reserved type":  firstReservedAddress,
		"the last reserved type":   lastReservedAddress,
	} {
		id := types.NewHash([]byte(name))
		ctx := newTestPtlcContext(chainIdentifier, 100)
		info := &definition.PtlcInfo{
			Id: id, TimeLocked: User1.Address, TokenStandard: types.ZnnTokenStandard, Amount: big.NewInt(1),
			ExpirationTime: 200, PointType: definition.PointTypeED25519, PointLock: User1.Public,
		}
		common.FailIfErr(t, info.Save(ctx.Storage()))

		signature := User1.Sign(definition.GetPtlcUnlockMessage(chainIdentifier, info.PointType, id, destination))
		common.ExpectError(t, verifyPtlcSignature(info, chainIdentifier, id, destination, signature), nil)

		blocks, err := unlockPtlc(ctx, &nom.AccountBlock{Address: User1.Address}, id, destination, signature)
		if err != constants.ErrInvalidDestination || blocks != nil {
			t.Errorf("%s: got %v and %d blocks, want %v and none", name, err, len(blocks), constants.ErrInvalidDestination)
		}
		if _, err := definition.GetPtlcInfo(ctx.Storage(), id); err != nil {
			t.Errorf("%s: the entry did not survive the refused unlock: %v", name, err)
		}

		paid := User1.Sign(definition.GetPtlcUnlockMessage(chainIdentifier, info.PointType, id, User1.Address))
		blocks, err = unlockPtlc(ctx, &nom.AccountBlock{Address: User1.Address}, id, User1.Address, paid)
		if err != nil || len(blocks) != 1 {
			t.Errorf("%s: the entry could not be opened to an account afterwards: %v", name, err)
		}
	}
}
