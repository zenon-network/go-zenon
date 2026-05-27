package implementation

import (
	"bytes"
	"math/big"
	"testing"
	"time"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/btcec/v2/schnorr"
	"github.com/ethereum/go-ethereum/common/hexutil"

	accountstore "github.com/zenon-network/go-zenon/chain/account"
	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/chain/store"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/db"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/consensus/api"
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
	chainIdentifier := uint64(100)
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
	message := definition.GetPtlcUnlockMessage(chainIdentifier, info.PointType, id, destination)

	common.ExpectError(t, verifyPtlcSignature(info, chainIdentifier, id, destination, bytes.Repeat([]byte{0xff}, 64)), constants.ErrInvalidPointSignature)

	signature, err := schnorr.Sign(privateKey, message)
	common.FailIfErr(t, err)

	info.PointLock = bytes.Repeat([]byte{0xff}, 32)
	common.ExpectError(t, verifyPtlcSignature(info, chainIdentifier, id, destination, signature.Serialize()), constants.ErrInvalidPointLock)

	info.PointType = 99
	common.ExpectError(t, verifyPtlcSignature(info, chainIdentifier, id, destination, nil), constants.ErrInvalidPointType)
}

type testPtlcContext struct {
	store.Account
	momentum *nom.Momentum
}

func newTestPtlcContext(chainIdentifier uint64, timestamp int64) *testPtlcContext {
	t := time.Unix(timestamp, 0)
	return &testPtlcContext{
		Account: accountstore.NewAccountStore(types.PtlcContract, db.NewMemDB()),
		momentum: &nom.Momentum{
			ChainIdentifier: chainIdentifier,
			Timestamp:       &t,
		},
	}
}

func (ctx *testPtlcContext) MomentumStore() store.Momentum {
	return nil
}

func (ctx *testPtlcContext) GetFrontierMomentum() (*nom.Momentum, error) {
	return ctx.momentum, nil
}

func (ctx *testPtlcContext) GetGenesisMomentum() *nom.Momentum {
	return ctx.momentum
}

func (ctx *testPtlcContext) Save()  {}
func (ctx *testPtlcContext) Reset() {}
func (ctx *testPtlcContext) Done()  {}

func (ctx *testPtlcContext) AddBalance(ts *types.ZenonTokenStandard, amount *big.Int) {}
func (ctx *testPtlcContext) SubBalance(ts *types.ZenonTokenStandard, amount *big.Int) {}

func (ctx *testPtlcContext) IsAcceleratorSporkEnforced() bool {
	return true
}

func (ctx *testPtlcContext) IsBridgeAndLiquiditySporkEnforced() bool {
	return true
}

func (ctx *testPtlcContext) IsHtlcSporkEnforced() bool {
	return true
}

func (ctx *testPtlcContext) IsPtlcSporkEnforced() bool {
	return true
}

func (ctx *testPtlcContext) GetPillarWeights() (map[string]*big.Int, error) {
	return nil, nil
}

func (ctx *testPtlcContext) EpochTicker() common.Ticker {
	return common.NewTicker(time.Unix(0, 0), time.Second)
}

func (ctx *testPtlcContext) EpochStats(epoch uint64) (*api.EpochStats, error) {
	return nil, nil
}

func (ctx *testPtlcContext) GetPillarDelegationsByEpoch(epoch uint64) (map[string]*types.PillarDelegationDetail, error) {
	return nil, nil
}

func TestPtlc_UnlockRejectsCorruptStoredPointType(t *testing.T) {
	chainIdentifier := uint64(100)
	id := types.NewHash([]byte("corrupt-point-type"))
	ctx := newTestPtlcContext(chainIdentifier, 100)

	ptlcInfo := &definition.PtlcInfo{
		Id:             id,
		TimeLocked:     User1.Address,
		TokenStandard:  types.ZnnTokenStandard,
		Amount:         big.NewInt(1),
		ExpirationTime: 200,
		PointType:      99,
		PointLock:      User1.Public,
	}
	common.FailIfErr(t, ptlcInfo.Save(ctx.Storage()))

	blocks, err := unlockPtlc(ctx, &nom.AccountBlock{Address: User1.Address}, id, User1.Address, nil)
	common.ExpectError(t, err, constants.ErrInvalidPointType)
	if blocks != nil {
		t.Fatalf("expected no generated blocks, got %d", len(blocks))
	}
}
