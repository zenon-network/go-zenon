package implementation

import (
	"math/big"
	"testing"

	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/vm/constants"
	"github.com/zenon-network/go-zenon/vm/embedded/definition"
)

// seedLiquidityRewardRun stores a two-token configuration and one stake
// entry with positive weight on the first token, so the epoch-0 reward run
// allocates that token's whole share to the staker.
func seedLiquidityRewardRun(t *testing.T, ctx *testLiquidityContext, znn, qsr []uint32) types.Address {
	t.Helper()
	first := types.NewZenonTokenStandard([]byte("liq-1"))
	second := types.NewZenonTokenStandard([]byte("liq-2"))
	info := &definition.LiquidityInfo{
		Administrator: types.ZeroAddress,
		ZnnReward:     big.NewInt(0),
		QsrReward:     big.NewInt(0),
		TokenTuples: []definition.TokenTuple{
			{TokenStandard: first.String(), ZnnPercentage: znn[0], QsrPercentage: qsr[0], MinAmount: big.NewInt(1)},
			{TokenStandard: second.String(), ZnnPercentage: znn[1], QsrPercentage: qsr[1], MinAmount: big.NewInt(1)},
		},
	}
	variable, err := definition.EncodeLiquidityInfo(info)
	common.FailIfErr(t, err)
	common.FailIfErr(t, variable.Save(ctx.Storage()))

	staker := types.ParseAddressPanic("z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz")
	entry := &definition.LiquidityStakeEntry{
		Amount:         big.NewInt(10),
		TokenStandard:  first,
		WeightedAmount: big.NewInt(10),
		StartTime:      0,
		RevokeTime:     0,
		ExpirationTime: 100,
		StakeAddress:   staker,
		Id:             types.HexToHashPanic("0000000000000000000000000000000000000000000000000000000000000001"),
	}
	common.FailIfErr(t, entry.Save(ctx.Storage()))
	return staker
}

// A percentage above the denominator (one half of a pair that wraps to
// 10,000 in uint32) allocates more than the epoch budget to the staker; the
// reward run must stop on the budget guard with ErrInvalidRewards.
func TestComputeLiquidityStakeRewards_OverflowingTupleTripsBudgetGuard(t *testing.T) {
	for _, tc := range []struct {
		name     string
		znn, qsr []uint32
	}{
		{"znn-overflow", []uint32{4294957296, 20000}, []uint32{5000, 5000}},
		{"qsr-overflow", []uint32{5000, 5000}, []uint32{4294957296, 20000}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := newTestLiquidityContext()
			seedLiquidityRewardRun(t, ctx, tc.znn, tc.qsr)
			blocks, err := computeLiquidityStakeRewardsForEpoch(ctx, 0)
			if err != constants.ErrInvalidRewards {
				t.Fatalf("want ErrInvalidRewards, got err=%v blocks=%d", err, len(blocks))
			}
			if blocks != nil {
				t.Fatalf("want no descendant blocks on a failed run, got %d", len(blocks))
			}
		})
	}
}

// Control: a legitimate split pays the staker half of the emission and mints
// the other token's half back to the contract.
func TestComputeLiquidityStakeRewards_ValidTuplesPayOut(t *testing.T) {
	ctx := newTestLiquidityContext()
	staker := seedLiquidityRewardRun(t, ctx, []uint32{5000, 5000}, []uint32{5000, 5000})
	blocks, err := computeLiquidityStakeRewardsForEpoch(ctx, 0)
	common.FailIfErr(t, err)
	znn, qsr := constants.LiquidityRewardForEpoch(0)
	halfZnn, halfQsr := new(big.Int).Div(znn, big.NewInt(2)), new(big.Int).Div(qsr, big.NewInt(2))
	if len(blocks) != 2 {
		t.Fatalf("want the two mint descendants for the unallocated half, got %d", len(blocks))
	}
	for i, want := range []struct {
		zts    types.ZenonTokenStandard
		amount *big.Int
	}{{types.ZnnTokenStandard, halfZnn}, {types.QsrTokenStandard, halfQsr}} {
		if blocks[i].ToAddress != types.TokenContract || blocks[i].Amount.Sign() != 0 {
			t.Fatalf("descendant %d: want a zero-amount call to the token contract, got to=%v amount=%v", i, blocks[i].ToAddress, blocks[i].Amount)
		}
		param := new(definition.MintParam)
		common.FailIfErr(t, definition.ABIToken.UnpackMethod(param, definition.MintMethodName, blocks[i].Data))
		if param.TokenStandard != want.zts || param.Amount.Cmp(want.amount) != 0 || param.ReceiveAddress != types.LiquidityContract {
			t.Fatalf("descendant %d: want mint %v of %v to the liquidity contract, got %+v", i, want.amount, want.zts, param)
		}
	}
	deposit, err := definition.GetRewardDeposit(ctx.Storage(), &staker)
	common.FailIfErr(t, err)
	if deposit.Znn.Cmp(halfZnn) != 0 || deposit.Qsr.Cmp(halfQsr) != 0 {
		t.Fatalf("want half the epoch emission deposited, got znn=%v qsr=%v", deposit.Znn, deposit.Qsr)
	}
}
