package implementation

import (
	"math/big"
	"testing"
	"time"

	accountstore "github.com/zenon-network/go-zenon/chain/account"
	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/chain/store"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/db"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/consensus/api"
	"github.com/zenon-network/go-zenon/vm/constants"
	"github.com/zenon-network/go-zenon/vm/embedded/definition"
)

// testLiquidityContext is a minimal AccountVmContext for exercising the
// SetTokenTupleMethod ReceiveBlock gate. IsDynamicPlasmaSporkEnforced
// returns true so the stricter CheckTokenTuplePercentages runs. The
// wrapping-percentage test returns before any storage access, so most
// methods are unused stubs.
type testLiquidityContext struct {
	store.Account
	momentum *nom.Momentum
}

func newTestLiquidityContext() *testLiquidityContext {
	t := time.Unix(1000000000, 0)
	return &testLiquidityContext{
		Account: accountstore.NewAccountStore(types.LiquidityContract, db.NewMemDB()),
		momentum: &nom.Momentum{
			ChainIdentifier: 100,
			Timestamp:       &t,
			Height:          10,
		},
	}
}

func (ctx *testLiquidityContext) CacheStore() store.Cache       { return nil }
func (ctx *testLiquidityContext) MomentumStore() store.Momentum { return nil }

func (ctx *testLiquidityContext) GetFrontierMomentum() (*nom.Momentum, error) {
	return ctx.momentum, nil
}
func (ctx *testLiquidityContext) GetGenesisMomentum() *nom.Momentum { return ctx.momentum }

func (ctx *testLiquidityContext) Save()  {}
func (ctx *testLiquidityContext) Reset() {}
func (ctx *testLiquidityContext) Done()  {}

func (ctx *testLiquidityContext) AddBalance(ts *types.ZenonTokenStandard, amount *big.Int) {}
func (ctx *testLiquidityContext) SubBalance(ts *types.ZenonTokenStandard, amount *big.Int) {}

func (ctx *testLiquidityContext) IsAcceleratorSporkEnforced() bool        { return true }
func (ctx *testLiquidityContext) IsHtlcSporkEnforced() bool               { return true }
func (ctx *testLiquidityContext) IsBridgeAndLiquiditySporkEnforced() bool { return true }
func (ctx *testLiquidityContext) IsDynamicPlasmaSporkEnforced() bool      { return true }
func (ctx *testLiquidityContext) IsPtlcSporkEnforced() bool               { return false }

func (ctx *testLiquidityContext) GetPillarWeights() (map[string]*big.Int, error) {
	return nil, nil
}
func (ctx *testLiquidityContext) EpochTicker() common.Ticker {
	return common.NewTicker(time.Unix(0, 0), time.Second)
}
func (ctx *testLiquidityContext) EpochStats(epoch uint64) (*api.EpochStats, error) {
	return nil, nil
}
func (ctx *testLiquidityContext) GetPillarDelegationsByEpoch(epoch uint64) (map[string]*types.PillarDelegationDetail, error) {
	return nil, nil
}

// TestSetTokenTuple_ReceiveBlockRejectsWrappingPercentages verifies that
// the ReceiveBlock gate catches the uint32 wrap attack from issue #198
// even when the wrapping send was admitted before enforcement (so the
// applySend gate never saw it). Without the CheckTokenTuplePercentages
// call in ReceiveBlock the wrapping tuples would be stored. The check
// fires before any storage access, so no security/liquidity setup is
// needed.
func TestSetTokenTuple_ReceiveBlockRejectsWrappingPercentages(t *testing.T) {
	ctx := newTestLiquidityContext()

	admin := types.ParseAddressPanic("z1qqaswvt0e3cc5sm7lygkyza9ra63cr8e6zre09")
	zts1 := types.BytesToZTSPanic([]byte("tok1aaaaaa"))
	zts2 := types.BytesToZTSPanic([]byte("tok2bbbbbb"))

	// The wrapping pair: uint32 sums wrap to exactly 10,000, passing the
	// legacy ValidateSendBlock check, but the widened uint64 accumulator
	// in CheckTokenTuplePercentages rejects it.
	wrapZnn := []uint32{4294957296, 20000}
	validQsr := []uint32{5000, 5000}
	minAmounts := []*big.Int{big.NewInt(1000), big.NewInt(2000)}

	sendBlock := &nom.AccountBlock{
		Address:   admin,
		ToAddress: types.LiquidityContract,
		Data: definition.ABILiquidity.PackMethodPanic(definition.SetTokenTupleMethodName,
			[]string{zts1.String(), zts2.String()},
			wrapZnn,
			validQsr,
			minAmounts,
		),
		Amount: big.NewInt(0),
	}

	method := &SetTokenTupleMethod{definition.SetTokenTupleMethodName}
	_, err := method.ReceiveBlock(ctx, sendBlock)
	if err != constants.ErrInvalidPercentages {
		t.Fatalf("expected %v, got %v", constants.ErrInvalidPercentages, err)
	}
}

// TestSetTokenTuple_ReceiveBlockRejectsQSRWrappingPercentages verifies the
// mirrored QSR-side wrap: valid ZNN with a wrapping QSR pair.
func TestSetTokenTuple_ReceiveBlockRejectsQSRWrappingPercentages(t *testing.T) {
	ctx := newTestLiquidityContext()

	admin := types.ParseAddressPanic("z1qqaswvt0e3cc5sm7lygkyza9ra63cr8e6zre09")
	zts1 := types.BytesToZTSPanic([]byte("tok1aaaaaa"))
	zts2 := types.BytesToZTSPanic([]byte("tok2bbbbbb"))

	validZnn := []uint32{5000, 5000}
	wrapQsr := []uint32{4294957296, 20000}
	minAmounts := []*big.Int{big.NewInt(1000), big.NewInt(2000)}

	sendBlock := &nom.AccountBlock{
		Address:   admin,
		ToAddress: types.LiquidityContract,
		Data: definition.ABILiquidity.PackMethodPanic(definition.SetTokenTupleMethodName,
			[]string{zts1.String(), zts2.String()},
			validZnn,
			wrapQsr,
			minAmounts,
		),
		Amount: big.NewInt(0),
	}

	method := &SetTokenTupleMethod{definition.SetTokenTupleMethodName}
	_, err := method.ReceiveBlock(ctx, sendBlock)
	if err != constants.ErrInvalidPercentages {
		t.Fatalf("expected %v, got %v", constants.ErrInvalidPercentages, err)
	}
}
