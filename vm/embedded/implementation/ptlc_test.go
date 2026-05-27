package implementation

import (
	"bytes"
	"math/big"
	"testing"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/btcec/v2/schnorr"
	"github.com/ethereum/go-ethereum/common/hexutil"

	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/vm/constants"
	"github.com/zenon-network/go-zenon/vm/embedded/definition"
	"github.com/zenon-network/go-zenon/wallet"
)

var (
	User1, _ = wallet.DeriveWithIndex(1, hexutil.MustDecode("0x01234567890123456789012345678902"))

	defaultPtlc = definition.CreatePtlcParam{
		ExpirationTime: 1000000000,
		PointType:      0,
		PointLock:      User1.Public,
	}
)

func TestPtlc_PointType(t *testing.T) {
	ptlc := defaultPtlc
	common.ExpectError(t, checkPtlc(ptlc), nil)
	ptlc.PointType = 1
	common.ExpectError(t, checkPtlc(ptlc), nil)
	ptlc.PointType = 2
	common.ExpectError(t, checkPtlc(ptlc), constants.ErrInvalidPointType)
}

func TestPtlc_LockLength(t *testing.T) {
	ptlc := defaultPtlc
	ptlc.PointLock = ptlc.PointLock[1:]
	common.ExpectError(t, checkPtlc(ptlc), constants.ErrInvalidPointLock)
	ptlc.PointType = 1
	common.ExpectError(t, checkPtlc(ptlc), constants.ErrInvalidPointLock)
}

func TestPtlc_StoredInfoValidation(t *testing.T) {
	info := &definition.PtlcInfo{
		Id:             types.Hash{},
		TimeLocked:     User1.Address,
		TokenStandard:  types.ZnnTokenStandard,
		Amount:         big.NewInt(1),
		ExpirationTime: 1000000000,
		PointType:      definition.PointTypeED25519,
		PointLock:      User1.Public,
	}

	common.ExpectError(t, checkStoredPtlcInfo(info), nil)

	info.PointType = 99
	common.ExpectError(t, checkStoredPtlcInfo(info), constants.ErrInvalidPointType)

	info.PointType = definition.PointTypeED25519
	info.PointLock = info.PointLock[1:]
	common.ExpectError(t, checkStoredPtlcInfo(info), constants.ErrInvalidPointLock)

	info.PointLock = User1.Public
	info.Amount = big.NewInt(0)
	common.ExpectError(t, checkStoredPtlcInfo(info), constants.ErrInvalidTokenOrAmount)

	info.Amount = big.NewInt(1)
	info.ExpirationTime = 0
	common.ExpectError(t, checkStoredPtlcInfo(info), constants.ErrInvalidExpirationTime)
}

func TestPtlc_VerifySignatureStableErrors(t *testing.T) {
	id := types.Hash{}
	destination := User1.Address
	privateKeyBytes := bytes.Repeat([]byte{1}, 32)
	privateKey, publicKey := btcec.PrivKeyFromBytes(privateKeyBytes)

	info := &definition.PtlcInfo{
		Id:             id,
		TimeLocked:     User1.Address,
		TokenStandard:  types.ZnnTokenStandard,
		Amount:         big.NewInt(1),
		ExpirationTime: 1000000000,
		PointType:      definition.PointTypeBIP340,
		PointLock:      schnorr.SerializePubKey(publicKey),
	}
	message := definition.GetPtlcUnlockMessage(info.PointType, id, destination)

	common.ExpectError(t, verifyPtlcSignature(info, id, destination, bytes.Repeat([]byte{0xff}, 64)), constants.ErrInvalidPointSignature)

	signature, err := schnorr.Sign(privateKey, message)
	common.FailIfErr(t, err)

	info.PointLock = bytes.Repeat([]byte{0xff}, 32)
	common.ExpectError(t, verifyPtlcSignature(info, id, destination, signature.Serialize()), constants.ErrInvalidPointLock)

	info.PointType = 99
	common.ExpectError(t, verifyPtlcSignature(info, id, destination, nil), constants.ErrInvalidPointType)
}
