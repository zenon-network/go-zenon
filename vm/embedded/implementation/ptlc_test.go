package implementation

import (
	"bytes"
	"encoding/hex"
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

	// All-0xff is outside the secp256k1 field/order ranges, so ParseSignature fails before verification.
	common.ExpectError(t, verifyPtlcSignature(info, chainIdentifier, id, destination, bytes.Repeat([]byte{0xff}, 64)), constants.ErrInvalidPointSignature)

	signature, err := schnorr.Sign(privateKey, message)
	common.FailIfErr(t, err)

	info.PointLock = bytes.Repeat([]byte{0xff}, 32)
	common.ExpectError(t, verifyPtlcSignature(info, chainIdentifier, id, destination, signature.Serialize()), constants.ErrInvalidPointLock)

	info.PointType = 99
	common.ExpectError(t, verifyPtlcSignature(info, chainIdentifier, id, destination, nil), constants.ErrInvalidPointType)
}

type validatePtlcSendBlockMethod interface {
	ValidateSendBlock(*nom.AccountBlock) error
}

func TestPtlc_ValidateAmountGuards(t *testing.T) {
	id := types.Hash{}
	signature := bytes.Repeat([]byte{1}, 64)

	tests := []struct {
		name   string
		method validatePtlcSendBlockMethod
		data   func() []byte
		amount *big.Int
		want   error
	}{
		{
			name:   "create nil amount",
			method: &CreatePtlcMethod{definition.CreatePtlcMethodName},
			data: func() []byte {
				return definition.ABIPtlc.PackMethodPanic(definition.CreatePtlcMethodName, defaultPtlc.ExpirationTime, defaultPtlc.PointType, defaultPtlc.PointLock)
			},
			amount: nil,
			want:   constants.ErrInvalidTokenOrAmount,
		},
		{
			name:   "create negative amount",
			method: &CreatePtlcMethod{definition.CreatePtlcMethodName},
			data: func() []byte {
				return definition.ABIPtlc.PackMethodPanic(definition.CreatePtlcMethodName, defaultPtlc.ExpirationTime, defaultPtlc.PointType, defaultPtlc.PointLock)
			},
			amount: big.NewInt(-1),
			want:   constants.ErrInvalidTokenOrAmount,
		},
		{
			name:   "create positive amount",
			method: &CreatePtlcMethod{definition.CreatePtlcMethodName},
			data: func() []byte {
				return definition.ABIPtlc.PackMethodPanic(definition.CreatePtlcMethodName, defaultPtlc.ExpirationTime, defaultPtlc.PointType, defaultPtlc.PointLock)
			},
			amount: big.NewInt(1),
			want:   nil,
		},
		{
			name:   "reclaim nil amount",
			method: &ReclaimPtlcMethod{definition.ReclaimPtlcMethodName},
			data: func() []byte {
				return definition.ABIPtlc.PackMethodPanic(definition.ReclaimPtlcMethodName, id)
			},
			amount: nil,
			want:   constants.ErrInvalidTokenOrAmount,
		},
		{
			name:   "reclaim negative amount",
			method: &ReclaimPtlcMethod{definition.ReclaimPtlcMethodName},
			data: func() []byte {
				return definition.ABIPtlc.PackMethodPanic(definition.ReclaimPtlcMethodName, id)
			},
			amount: big.NewInt(-1),
			want:   constants.ErrInvalidTokenOrAmount,
		},
		{
			name:   "reclaim zero amount",
			method: &ReclaimPtlcMethod{definition.ReclaimPtlcMethodName},
			data: func() []byte {
				return definition.ABIPtlc.PackMethodPanic(definition.ReclaimPtlcMethodName, id)
			},
			amount: big.NewInt(0),
			want:   nil,
		},
		{
			name:   "unlock nil amount",
			method: &UnlockPtlcMethod{definition.UnlockPtlcMethodName},
			data: func() []byte {
				return definition.ABIPtlc.PackMethodPanic(definition.UnlockPtlcMethodName, id, signature)
			},
			amount: nil,
			want:   constants.ErrInvalidTokenOrAmount,
		},
		{
			name:   "unlock negative amount",
			method: &UnlockPtlcMethod{definition.UnlockPtlcMethodName},
			data: func() []byte {
				return definition.ABIPtlc.PackMethodPanic(definition.UnlockPtlcMethodName, id, signature)
			},
			amount: big.NewInt(-1),
			want:   constants.ErrInvalidTokenOrAmount,
		},
		{
			name:   "unlock zero amount",
			method: &UnlockPtlcMethod{definition.UnlockPtlcMethodName},
			data: func() []byte {
				return definition.ABIPtlc.PackMethodPanic(definition.UnlockPtlcMethodName, id, signature)
			},
			amount: big.NewInt(0),
			want:   nil,
		},
		{
			name:   "proxy unlock nil amount",
			method: &ProxyUnlockPtlcMethod{definition.ProxyUnlockPtlcMethodName},
			data: func() []byte {
				return definition.ABIPtlc.PackMethodPanic(definition.ProxyUnlockPtlcMethodName, id, User1.Address, signature)
			},
			amount: nil,
			want:   constants.ErrInvalidTokenOrAmount,
		},
		{
			name:   "proxy unlock negative amount",
			method: &ProxyUnlockPtlcMethod{definition.ProxyUnlockPtlcMethodName},
			data: func() []byte {
				return definition.ABIPtlc.PackMethodPanic(definition.ProxyUnlockPtlcMethodName, id, User1.Address, signature)
			},
			amount: big.NewInt(-1),
			want:   constants.ErrInvalidTokenOrAmount,
		},
		{
			name:   "proxy unlock zero amount",
			method: &ProxyUnlockPtlcMethod{definition.ProxyUnlockPtlcMethodName},
			data: func() []byte {
				return definition.ABIPtlc.PackMethodPanic(definition.ProxyUnlockPtlcMethodName, id, User1.Address, signature)
			},
			amount: big.NewInt(0),
			want:   nil,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			common.ExpectError(t, test.method.ValidateSendBlock(&nom.AccountBlock{
				Data:          test.data(),
				Amount:        test.amount,
				TokenStandard: types.ZnnTokenStandard,
			}), test.want)
		})
	}
}

func mustDecodeHex(t *testing.T, value string) []byte {
	t.Helper()

	decoded, err := hex.DecodeString(value)
	if err != nil {
		t.Fatalf("invalid test vector hex: %v", err)
	}
	return decoded
}

// Selected BIP340 vectors from bitcoin/bips bip-0340/test-vectors.csv.
func TestPtlc_BIP340OfficialVectors(t *testing.T) {
	const (
		message0 = "0000000000000000000000000000000000000000000000000000000000000000"
		message1 = "243F6A8885A308D313198A2E03707344A4093822299F31D0082EFA98EC4E6C89"
		pubKey0  = "F9308A019258C31049344F85F89D5229B531C845836F99B08601F113BCE036F9"
		pubKey1  = "DFF1D77F2A671C5F36183726DB2341BE58FEAE1DA2DECED843240F7B502BA659"
	)

	tests := []struct {
		name          string
		pubKey        string
		message       string
		signature     string
		wantVerify    bool
		wantPubKeyErr bool
	}{
		{
			name:       "vector 0 valid",
			pubKey:     pubKey0,
			message:    message0,
			signature:  "E907831F80848D1069A5371B402410364BDF1C5F8307B0084C55F1CE2DCA821525F66A4A85EA8B71E482A74F382D2CE5EBEEE8FDB2172F477DF4900D310536C0",
			wantVerify: true,
		},
		{
			name:       "vector 3 all-ff message valid",
			pubKey:     "25D1DFF95105F5253C4022F628A996AD3A0D95FBF21D468A1B33F8C160D8F517",
			message:    "FFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFF",
			signature:  "7EB0509757E246F19449885651611CB965ECC1A187DD51B64FDA1EDC9637D5EC97582B9CB13DB3933705B32BA982AF5AF25FD78881EBB32771FC5922EFC66EA3",
			wantVerify: true,
		},
		{
			name:       "vector 7 invalid signature",
			pubKey:     pubKey1,
			message:    message1,
			signature:  "1FA62E331EDBC21C394792D2AB1100A7B432B013DF3F6FF4F99FCB33E0E1515F28890B3EDB6E7189B630448B515CE4F8622A954CFE545735AAEA5134FCCDB2BD",
			wantVerify: false,
		},
		{
			name:       "wrong message",
			pubKey:     pubKey0,
			message:    "0100000000000000000000000000000000000000000000000000000000000000",
			signature:  "E907831F80848D1069A5371B402410364BDF1C5F8307B0084C55F1CE2DCA821525F66A4A85EA8B71E482A74F382D2CE5EBEEE8FDB2172F477DF4900D310536C0",
			wantVerify: false,
		},
		{
			name:       "wrong x-only public key",
			pubKey:     pubKey1,
			message:    message0,
			signature:  "E907831F80848D1069A5371B402410364BDF1C5F8307B0084C55F1CE2DCA821525F66A4A85EA8B71E482A74F382D2CE5EBEEE8FDB2172F477DF4900D310536C0",
			wantVerify: false,
		},
		{
			name:          "vector 14 invalid public key",
			pubKey:        "FFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFEFFFFFC30",
			message:       message1,
			signature:     "6CFF5C3BA86C69EA4B7376F31A9BCB4F74C1976089B2D9963DA2E5543E17776969E89B4C5564D00349106B8497785DD7D1D713A8AE82B32FA79D5F7FC407D39B",
			wantVerify:    false,
			wantPubKeyErr: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			pubKey, err := schnorr.ParsePubKey(mustDecodeHex(t, test.pubKey))
			if test.wantPubKeyErr {
				if err == nil {
					t.Fatalf("expected public key parse error")
				}
				return
			}
			common.FailIfErr(t, err)

			signature, err := schnorr.ParseSignature(mustDecodeHex(t, test.signature))
			common.FailIfErr(t, err)

			if got := signature.Verify(mustDecodeHex(t, test.message), pubKey); got != test.wantVerify {
				t.Fatalf("unexpected verification result: got %v want %v", got, test.wantVerify)
			}
		})
	}
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
