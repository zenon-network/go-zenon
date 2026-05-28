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
	"github.com/zenon-network/go-zenon/common/crypto"
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

func ptlcUnlockMessageForContract(chainIdentifier uint64, contract types.Address, pointType uint8, id types.Hash, destination types.Address) []byte {
	return crypto.Hash(common.JoinBytes(
		[]byte(definition.PtlcUnlockMessageDomain),
		common.Uint64ToBytes(chainIdentifier),
		contract.Bytes(),
		[]byte{pointType},
		id.Bytes(),
		destination.Bytes(),
	))
}

func TestPtlc_VerifySignatureRejectsWrongDomainFields(t *testing.T) {
	chainIdentifier := uint64(100)
	id := types.NewHash([]byte("ptlc-domain-id"))
	otherId := types.NewHash([]byte("ptlc-domain-other-id"))
	destination := User1.Address

	edInfo := &definition.PtlcInfo{
		Id:             id,
		TimeLocked:     User1.Address,
		TokenStandard:  types.ZnnTokenStandard,
		Amount:         big.NewInt(1),
		ExpirationTime: 1000000000,
		PointType:      definition.PointTypeED25519,
		PointLock:      User1.Public,
	}

	common.ExpectError(t, verifyPtlcSignature(edInfo, chainIdentifier, id, destination, User1.Sign(ptlcUnlockMessageForContract(
		chainIdentifier,
		types.HtlcContract,
		definition.PointTypeED25519,
		id,
		destination,
	))), constants.ErrInvalidPointSignature)

	common.ExpectError(t, verifyPtlcSignature(edInfo, chainIdentifier, id, destination, User1.Sign(definition.GetPtlcUnlockMessage(
		chainIdentifier,
		definition.PointTypeBIP340,
		id,
		destination,
	))), constants.ErrInvalidPointSignature)

	common.ExpectError(t, verifyPtlcSignature(edInfo, chainIdentifier, id, destination, User1.Sign(definition.GetPtlcUnlockMessage(
		chainIdentifier,
		definition.PointTypeED25519,
		otherId,
		destination,
	))), constants.ErrInvalidPointSignature)

	privateKeyBytes := bytes.Repeat([]byte{2}, 32)
	privateKey, publicKey := btcec.PrivKeyFromBytes(privateKeyBytes)
	bip340Info := &definition.PtlcInfo{
		Id:             id,
		TimeLocked:     User1.Address,
		TokenStandard:  types.ZnnTokenStandard,
		Amount:         big.NewInt(1),
		ExpirationTime: 1000000000,
		PointType:      definition.PointTypeBIP340,
		PointLock:      schnorr.SerializePubKey(publicKey),
	}

	wrongContractSignature, err := schnorr.Sign(privateKey, ptlcUnlockMessageForContract(
		chainIdentifier,
		types.HtlcContract,
		definition.PointTypeBIP340,
		id,
		destination,
	))
	common.FailIfErr(t, err)
	common.ExpectError(t, verifyPtlcSignature(bip340Info, chainIdentifier, id, destination, wrongContractSignature.Serialize()), constants.ErrInvalidPointSignature)

	wrongPointTypeSignature, err := schnorr.Sign(privateKey, definition.GetPtlcUnlockMessage(
		chainIdentifier,
		definition.PointTypeED25519,
		id,
		destination,
	))
	common.FailIfErr(t, err)
	common.ExpectError(t, verifyPtlcSignature(bip340Info, chainIdentifier, id, destination, wrongPointTypeSignature.Serialize()), constants.ErrInvalidPointSignature)

	wrongIdSignature, err := schnorr.Sign(privateKey, definition.GetPtlcUnlockMessage(
		chainIdentifier,
		definition.PointTypeBIP340,
		otherId,
		destination,
	))
	common.FailIfErr(t, err)
	common.ExpectError(t, verifyPtlcSignature(bip340Info, chainIdentifier, id, destination, wrongIdSignature.Serialize()), constants.ErrInvalidPointSignature)
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
		name      string
		pubKey    string
		message   string
		signature string
		want      error
	}{
		{
			name:      "vector 0 valid",
			pubKey:    pubKey0,
			message:   message0,
			signature: "E907831F80848D1069A5371B402410364BDF1C5F8307B0084C55F1CE2DCA821525F66A4A85EA8B71E482A74F382D2CE5EBEEE8FDB2172F477DF4900D310536C0",
			want:      nil,
		},
		{
			name:      "vector 3 all-ff message valid",
			pubKey:    "25D1DFF95105F5253C4022F628A996AD3A0D95FBF21D468A1B33F8C160D8F517",
			message:   "FFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFF",
			signature: "7EB0509757E246F19449885651611CB965ECC1A187DD51B64FDA1EDC9637D5EC97582B9CB13DB3933705B32BA982AF5AF25FD78881EBB32771FC5922EFC66EA3",
			want:      nil,
		},
		{
			name:      "vector 7 invalid signature",
			pubKey:    pubKey1,
			message:   message1,
			signature: "1FA62E331EDBC21C394792D2AB1100A7B432B013DF3F6FF4F99FCB33E0E1515F28890B3EDB6E7189B630448B515CE4F8622A954CFE545735AAEA5134FCCDB2BD",
			want:      constants.ErrInvalidPointSignature,
		},
		{
			name:      "wrong message",
			pubKey:    pubKey0,
			message:   "0100000000000000000000000000000000000000000000000000000000000000",
			signature: "E907831F80848D1069A5371B402410364BDF1C5F8307B0084C55F1CE2DCA821525F66A4A85EA8B71E482A74F382D2CE5EBEEE8FDB2172F477DF4900D310536C0",
			want:      constants.ErrInvalidPointSignature,
		},
		{
			name:      "wrong x-only public key",
			pubKey:    pubKey1,
			message:   message0,
			signature: "E907831F80848D1069A5371B402410364BDF1C5F8307B0084C55F1CE2DCA821525F66A4A85EA8B71E482A74F382D2CE5EBEEE8FDB2172F477DF4900D310536C0",
			want:      constants.ErrInvalidPointSignature,
		},
		{
			name:      "vector 14 invalid public key",
			pubKey:    "FFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFEFFFFFC30",
			message:   message1,
			signature: "6CFF5C3BA86C69EA4B7376F31A9BCB4F74C1976089B2D9963DA2E5543E17776969E89B4C5564D00349106B8497785DD7D1D713A8AE82B32FA79D5F7FC407D39B",
			want:      constants.ErrInvalidPointLock,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			common.ExpectError(t, verifyBIP340Signature(mustDecodeHex(t, test.message), mustDecodeHex(t, test.pubKey), mustDecodeHex(t, test.signature)), test.want)
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

func TestPtlc_CreateExpirationBoundary(t *testing.T) {
	method := &CreatePtlcMethod{definition.CreatePtlcMethodName}
	tests := []struct {
		name       string
		now        int64
		expiration int64
		want       error
	}{
		{name: "before expiration", now: 100, expiration: 101, want: nil},
		{name: "at expiration", now: 100, expiration: 100, want: constants.ErrInvalidExpirationTime},
		{name: "after expiration", now: 100, expiration: 99, want: constants.ErrInvalidExpirationTime},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := newTestPtlcContext(100, test.now)
			sendBlock := &nom.AccountBlock{
				Address:       User1.Address,
				Hash:          types.NewHash([]byte(test.name)),
				TokenStandard: types.ZnnTokenStandard,
				Amount:        big.NewInt(1),
				Data: definition.ABIPtlc.PackMethodPanic(
					definition.CreatePtlcMethodName,
					test.expiration,
					definition.PointTypeED25519,
					User1.Public,
				),
			}

			blocks, err := method.ReceiveBlock(ctx, sendBlock)
			common.ExpectError(t, err, test.want)
			if blocks != nil {
				t.Fatalf("create should not emit contract sends, got %d", len(blocks))
			}
			if test.want == nil {
				info, err := definition.GetPtlcInfo(ctx.Storage(), sendBlock.Hash)
				common.ExpectError(t, err, nil)
				if info == nil || info.ExpirationTime != test.expiration {
					t.Fatalf("unexpected stored PTLC info: %+v", info)
				}
			}
		})
	}
}

func FuzzPtlcValidateSendBlockNoPanic(f *testing.F) {
	validID := types.NewHash([]byte("fuzz-id"))
	f.Add(uint8(0), definition.ABIPtlc.PackMethodPanic(definition.CreatePtlcMethodName, defaultPtlc.ExpirationTime, defaultPtlc.PointType, defaultPtlc.PointLock), int64(2))
	f.Add(uint8(1), definition.ABIPtlc.PackMethodPanic(definition.ReclaimPtlcMethodName, validID), int64(1))
	f.Add(uint8(2), definition.ABIPtlc.PackMethodPanic(definition.UnlockPtlcMethodName, validID, bytes.Repeat([]byte{1}, 64)), int64(1))
	f.Add(uint8(3), definition.ABIPtlc.PackMethodPanic(definition.ProxyUnlockPtlcMethodName, validID, User1.Address, bytes.Repeat([]byte{1}, 64)), int64(1))
	f.Add(uint8(0), []byte{}, int64(0))
	f.Add(uint8(2), []byte{0xff, 0x00, 0x01}, int64(-1))

	methods := []validatePtlcSendBlockMethod{
		&CreatePtlcMethod{definition.CreatePtlcMethodName},
		&ReclaimPtlcMethod{definition.ReclaimPtlcMethodName},
		&UnlockPtlcMethod{definition.UnlockPtlcMethodName},
		&ProxyUnlockPtlcMethod{definition.ProxyUnlockPtlcMethodName},
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

func FuzzPtlcStoredInfoValidation(f *testing.F) {
	f.Add(int64(1), int64(100), definition.PointTypeED25519, []byte(User1.Public))
	f.Add(int64(1), int64(100), definition.PointTypeBIP340, bytes.Repeat([]byte{2}, 32))
	f.Add(int64(0), int64(100), definition.PointTypeED25519, []byte(User1.Public))
	f.Add(int64(1), int64(0), definition.PointTypeED25519, []byte(User1.Public))
	f.Add(int64(1), int64(100), uint8(99), []byte(User1.Public))
	f.Add(int64(1), int64(100), definition.PointTypeED25519, []byte(User1.Public[:31]))

	f.Fuzz(func(t *testing.T, amount int64, expiration int64, pointType uint8, pointLock []byte) {
		info := &definition.PtlcInfo{
			Id:             types.NewHash([]byte("stored-info-fuzz")),
			TimeLocked:     User1.Address,
			TokenStandard:  types.ZnnTokenStandard,
			Amount:         big.NewInt(amount),
			ExpirationTime: expiration,
			PointType:      pointType,
			PointLock:      append([]byte(nil), pointLock...),
		}

		err := checkStoredPtlcInfo(info)
		if err == nil {
			if amount <= 0 {
				t.Fatalf("accepted non-positive amount %d", amount)
			}
			if expiration <= 0 {
				t.Fatalf("accepted non-positive expiration %d", expiration)
			}
			size, ok := definition.PointTypePubKeySizes[pointType]
			if !ok {
				t.Fatalf("accepted invalid point type %d", pointType)
			}
			if len(pointLock) != int(size) {
				t.Fatalf("accepted point lock length %d for point type %d", len(pointLock), pointType)
			}
		}
	})
}

func FuzzPtlcED25519DomainMutationRejected(f *testing.F) {
	chainIdentifier := uint64(100)
	id := types.NewHash([]byte("ed25519-domain-fuzz"))
	destination := User1.Address
	validSignature := User1.Sign(definition.GetPtlcUnlockMessage(chainIdentifier, definition.PointTypeED25519, id, destination))

	f.Add(chainIdentifier, id.Bytes(), destination.Bytes(), validSignature)
	f.Add(chainIdentifier+1, id.Bytes(), destination.Bytes(), validSignature)
	f.Add(chainIdentifier, types.NewHash([]byte("other-id")).Bytes(), destination.Bytes(), validSignature)
	f.Add(chainIdentifier, id.Bytes(), types.PubKeyToAddress([]byte("other-destination")).Bytes(), validSignature)
	f.Add(chainIdentifier, id.Bytes(), destination.Bytes(), validSignature[:63])

	f.Fuzz(func(t *testing.T, chainIdentifier uint64, idBytes []byte, destinationBytes []byte, signature []byte) {
		id := fuzzHash(idBytes)
		destination := fuzzAddress(destinationBytes)
		info := &definition.PtlcInfo{
			Id:             id,
			TimeLocked:     User1.Address,
			TokenStandard:  types.ZnnTokenStandard,
			Amount:         big.NewInt(1),
			ExpirationTime: 1000000000,
			PointType:      definition.PointTypeED25519,
			PointLock:      User1.Public,
		}

		err := verifyPtlcSignature(info, chainIdentifier, id, destination, signature)
		expectedSignature := User1.Sign(definition.GetPtlcUnlockMessage(chainIdentifier, definition.PointTypeED25519, id, destination))
		if err == nil && !bytes.Equal(signature, expectedSignature) {
			t.Fatalf("accepted unexpected ED25519 signature for chain=%d id=%s destination=%s", chainIdentifier, id, destination)
		}
	})
}

func FuzzPtlcBIP340DomainMutationRejected(f *testing.F) {
	privateKeyBytes := bytes.Repeat([]byte{7}, 32)
	privateKey, publicKey := btcec.PrivKeyFromBytes(privateKeyBytes)
	chainIdentifier := uint64(100)
	id := types.NewHash([]byte("bip340-domain-fuzz"))
	destination := User1.Address
	validSignature, err := schnorr.Sign(privateKey, definition.GetPtlcUnlockMessage(chainIdentifier, definition.PointTypeBIP340, id, destination))
	if err != nil {
		f.Fatalf("sign BIP340 seed: %v", err)
	}

	f.Add(chainIdentifier, id.Bytes(), destination.Bytes(), validSignature.Serialize())
	f.Add(chainIdentifier+1, id.Bytes(), destination.Bytes(), validSignature.Serialize())
	f.Add(chainIdentifier, types.NewHash([]byte("other-id")).Bytes(), destination.Bytes(), validSignature.Serialize())
	f.Add(chainIdentifier, id.Bytes(), types.PubKeyToAddress([]byte("other-destination")).Bytes(), validSignature.Serialize())
	f.Add(chainIdentifier, id.Bytes(), destination.Bytes(), validSignature.Serialize()[:63])

	f.Fuzz(func(t *testing.T, chainIdentifier uint64, idBytes []byte, destinationBytes []byte, signature []byte) {
		id := fuzzHash(idBytes)
		destination := fuzzAddress(destinationBytes)
		info := &definition.PtlcInfo{
			Id:             id,
			TimeLocked:     User1.Address,
			TokenStandard:  types.ZnnTokenStandard,
			Amount:         big.NewInt(1),
			ExpirationTime: 1000000000,
			PointType:      definition.PointTypeBIP340,
			PointLock:      schnorr.SerializePubKey(publicKey),
		}

		err := verifyPtlcSignature(info, chainIdentifier, id, destination, signature)
		expectedSignature, signErr := schnorr.Sign(privateKey, definition.GetPtlcUnlockMessage(chainIdentifier, definition.PointTypeBIP340, id, destination))
		common.FailIfErr(t, signErr)
		if err == nil && !bytes.Equal(signature, expectedSignature.Serialize()) {
			t.Fatalf("accepted unexpected BIP340 signature for chain=%d id=%s destination=%s", chainIdentifier, id, destination)
		}
	})
}

func fuzzAmount(kind int64) *big.Int {
	switch kind & 3 {
	case 0:
		return nil
	case 1:
		return big.NewInt(0)
	case 2:
		amount := big.NewInt(kind)
		if amount.Sign() <= 0 {
			amount.Neg(amount)
			amount.Add(amount, big.NewInt(1))
		}
		return amount
	default:
		amount := big.NewInt(kind)
		if amount.Sign() >= 0 {
			amount.Neg(amount)
			amount.Sub(amount, big.NewInt(1))
		}
		return amount
	}
}

func fuzzHash(data []byte) types.Hash {
	if len(data) == types.HashSize {
		hash, err := types.BytesToHash(data)
		if err == nil {
			return hash
		}
	}
	return types.NewHash(data)
}

func fuzzAddress(data []byte) types.Address {
	if len(data) == types.AddressSize {
		address, err := types.BytesToAddress(data)
		if err == nil {
			return address
		}
	}
	return types.PubKeyToAddress(data)
}
