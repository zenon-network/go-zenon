package tests

import (
	"math/big"
	"testing"
	"time"

	g "github.com/zenon-network/go-zenon/chain/genesis/mock"
	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/rpc/api/embedded"
	"github.com/zenon-network/go-zenon/vm/constants"
	"github.com/zenon-network/go-zenon/vm/embedded/definition"
	"github.com/zenon-network/go-zenon/zenon/mock"
)

// Issue #198, third acceptance item. Before DynamicPlasmaSpork the legacy
// uint32 sum check accepts a percentage pair that wraps to exactly 10,000
// (#210 gates the stricter per-value check on the spork), so such a
// configuration can still be installed on a pre-spork network. With an
// unhalted due epoch and positive stake weight on the oversized token, the
// per-stake allocation exceeds the epoch budget; the reward update must fail
// on the budget guard and leave no trace: no reward deposit, no history
// entry, no mint descendants, and the epoch marker not advanced, so the same
// epoch is paid once a valid split is installed.

const (
	wrapHigh = uint32(4294957296) // wrapHigh + 20000 == 10000 mod 2^32
	wrapLow  = uint32(20000)
)

var (
	epochZnn = big.NewInt(1872 * g.Zexp)
	epochQsr = big.NewInt(5000 * g.Zexp)
)

// installOverflowTuples stands the fixture up with the wrapping pair on one
// side and a legitimate split on the other, and asserts it was installed.
func installOverflowTuples(t *testing.T, z mock.MockZenon, overflowZnn bool) *embedded.LiquidityApi {
	t.Helper()
	activateLiquidityStep1(t, z)
	issueMultipleTokensSetup(t, z)

	valid := []uint32{5000, 5000}
	wrap := []uint32{wrapHigh, wrapLow}
	minAmounts := []*big.Int{big.NewInt(1000), big.NewInt(2000)}
	znn, qsr := wrap, valid
	if !overflowZnn {
		znn, qsr = valid, wrap
	}
	setTokensTuple(t, z, g.User5.Address, tokensString, znn, qsr, minAmounts)

	liquidityAPI := embedded.NewLiquidityApi(z)
	info, err := liquidityAPI.GetLiquidityInfo()
	common.FailIfErr(t, err)
	if len(info.TokenTuples) != 2 || info.TokenTuples[0].ZnnPercentage != znn[0] || info.TokenTuples[0].QsrPercentage != qsr[0] {
		t.Fatalf("overflow configuration not installed: %+v", info.TokenTuples)
	}
	return liquidityAPI
}

// rewardHistory reads the epoch's reward history for address straight from
// the liquidity contract's storage. The RPC pager walks back from the last
// updated epoch, so after a rolled-back update it would never look at the
// epoch that failed; the storage read does.
func rewardHistory(t *testing.T, z mock.MockZenon, address types.Address, epoch uint64) *definition.RewardDepositHistory {
	t.Helper()
	storage := z.Chain().GetFrontierAccountStore(types.LiquidityContract).Storage()
	history, err := definition.GetRewardDepositHistory(storage, epoch, &address)
	common.FailIfErr(t, err)
	return history
}

func expectNoRewardForEpoch(t *testing.T, z mock.MockZenon, liquidityAPI *embedded.LiquidityApi, address types.Address, epoch uint64) {
	t.Helper()
	reward, err := liquidityAPI.GetUncollectedReward(address)
	common.FailIfErr(t, err)
	if reward.Znn.Sign() != 0 || reward.Qsr.Sign() != 0 {
		t.Fatalf("want no uncollected reward, got znn=%v qsr=%v", reward.Znn, reward.Qsr)
	}
	if history := rewardHistory(t, z, address, epoch); history.Znn.Sign() != 0 || history.Qsr.Sign() != 0 {
		t.Fatalf("want no epoch %d reward history, got znn=%v qsr=%v", epoch, history.Znn, history.Qsr)
	}
}

func contractFrontierHeight(t *testing.T, z mock.MockZenon) uint64 {
	t.Helper()
	frontier, err := z.Chain().GetFrontierAccountStore(types.LiquidityContract).Frontier()
	common.FailIfErr(t, err)
	if frontier == nil {
		return 0
	}
	return frontier.Height
}

// expectOnlyFailedReceivesSince walks the liquidity contract's own chain
// above fromHeight. Descendant blocks of a receive are ContractSend blocks
// on this chain, committed with it and placed immediately before it in
// height order, so a window in which every update failed must hold receives
// only. Reads do not populate AccountBlock.DescendantBlocks, hence the
// walk. It returns how many receives it saw.
func expectOnlyFailedReceivesSince(t *testing.T, z mock.MockZenon, fromHeight uint64) int {
	t.Helper()
	accountStore := z.Chain().GetFrontierAccountStore(types.LiquidityContract)
	receives := 0
	for h := fromHeight + 1; h <= contractFrontierHeight(t, z); h++ {
		block, err := accountStore.ByHeight(h)
		common.FailIfErr(t, err)
		if block.BlockType == nom.BlockTypeContractSend {
			t.Fatalf("want no descendant blocks from a failed update, found ContractSend at height %d to %v", h, block.ToAddress)
		}
		receives++
	}
	return receives
}

func updateCall(from types.Address) *nom.AccountBlock {
	return &nom.AccountBlock{
		Address:   from,
		ToAddress: types.LiquidityContract,
		Data:      definition.ABICommon.PackMethodPanic(definition.UpdateMethodName),
	}
}

func TestLiquidity_UpdateRejectsOverflowingTuplesAndRollsBack(t *testing.T) {
	for _, tc := range []struct {
		name        string
		overflowZnn bool
	}{
		{"znn-overflow", true},
		{"qsr-overflow", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			z := mock.NewMockZenonWithCustomEpochDuration(t, time.Hour)
			defer z.StopPanic()
			saveSporkState(t)
			liquidityAPI := installOverflowTuples(t, z, tc.overflowZnn)

			// Positive stake weight on the oversized token (tokens[0]).
			defer z.CallContract(liquidityStake(g.User1.Address, tokens[0], big.NewInt(10*g.Zexp), 1)).Error(t, nil)
			insertMomentums(z, 2)

			// Epoch 0 was paid during setup; the contract holds exactly one epoch.
			z.ExpectBalance(types.LiquidityContract, types.ZnnTokenStandard, epochZnn.Int64())
			z.ExpectBalance(types.LiquidityContract, types.QsrTokenStandard, epochQsr.Int64())

			// Epoch 1 becomes due at height 721 plus the reward time limit.
			// Each failed update is rolled back, so the contract's last-update
			// height never advances and the pillar is eligible to send Update
			// again on the following momentums; every attempt fails on the
			// budget guard. Nothing may be committed.
			startHeight := contractFrontierHeight(t, z)
			z.InsertMomentumsTo(360*2 + 70)

			// A due Update sent directly returns the guard's error.
			failed := z.CallContract(updateCall(g.User1.Address))
			insertMomentums(z, 2)
			failed.Error(t, constants.ErrInvalidRewards)

			// Only receives landed on the contract chain: no mint descendants.
			if receives := expectOnlyFailedReceivesSince(t, z, startHeight); receives == 0 {
				t.Fatal("expected at least one failed update receive on the contract chain")
			}
			expectNoRewardForEpoch(t, z, liquidityAPI, g.User1.Address, 1)
			z.ExpectBalance(types.LiquidityContract, types.ZnnTokenStandard, epochZnn.Int64())
			z.ExpectBalance(types.LiquidityContract, types.QsrTokenStandard, epochQsr.Int64())

			// The epoch marker was rolled back too: once a valid split is in
			// place the next update pays epoch 1, half to the single staker on
			// tokens[0] and the other token's half minted to the contract. The
			// repair ends near height 835, well before epoch 2 is due (around
			// 1081), so this cannot be an epoch 2 payment standing in for a
			// skipped epoch 1.
			valid := []uint32{5000, 5000}
			minAmounts := []*big.Int{big.NewInt(1000), big.NewInt(2000)}
			setTokensTuple(t, z, g.User5.Address, tokensString, valid, valid, minAmounts)
			insertMomentums(z, 30)

			common.Json(liquidityAPI.GetUncollectedReward(g.User1.Address)).Equals(t, `
{
	"address": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
	"znnAmount": "93600000000",
	"qsrAmount": "250000000000"
}`)
			z.ExpectBalance(types.LiquidityContract, types.ZnnTokenStandard, epochZnn.Int64()+936*g.Zexp)
			z.ExpectBalance(types.LiquidityContract, types.QsrTokenStandard, epochQsr.Int64()+2500*g.Zexp)
			history := rewardHistory(t, z, g.User1.Address, 1)
			if history.Znn.Cmp(big.NewInt(936*g.Zexp)) != 0 || history.Qsr.Cmp(big.NewInt(2500*g.Zexp)) != 0 {
				t.Fatalf("want epoch 1 history of exactly one payment, got znn=%v qsr=%v", history.Znn, history.Qsr)
			}
		})
	}
}

// Halted control: the same configuration and stake, but a halted contract
// mints the epoch emission to itself and never reaches the allocation loop.
func TestLiquidity_UpdateOverflowingTuplesHaltedMintsToContract(t *testing.T) {
	z := mock.NewMockZenonWithCustomEpochDuration(t, time.Hour)
	defer z.StopPanic()
	saveSporkState(t)
	liquidityAPI := installOverflowTuples(t, z, true)

	defer z.CallContract(liquidityStake(g.User1.Address, tokens[0], big.NewInt(10*g.Zexp), 1)).Error(t, nil)
	insertMomentums(z, 2)
	defer z.CallContract(setIsHalted(g.User5.Address, true)).Error(t, nil)
	insertMomentums(z, 2)

	z.InsertMomentumsTo(360*2 + 70)
	expectNoRewardForEpoch(t, z, liquidityAPI, g.User1.Address, 1)
	z.ExpectBalance(types.LiquidityContract, types.ZnnTokenStandard, 2*epochZnn.Int64())
	z.ExpectBalance(types.LiquidityContract, types.QsrTokenStandard, 2*epochQsr.Int64())
}

// Zero-active-stake control: with no stake weight there is no allocation,
// so the oversized percentage never produces funds and the whole emission
// is minted to the contract.
func TestLiquidity_UpdateOverflowingTuplesNoStakeMintsToContract(t *testing.T) {
	z := mock.NewMockZenonWithCustomEpochDuration(t, time.Hour)
	defer z.StopPanic()
	saveSporkState(t)
	liquidityAPI := installOverflowTuples(t, z, true)

	z.InsertMomentumsTo(360*2 + 70)
	expectNoRewardForEpoch(t, z, liquidityAPI, g.User1.Address, 1)
	z.ExpectBalance(types.LiquidityContract, types.ZnnTokenStandard, 2*epochZnn.Int64())
	z.ExpectBalance(types.LiquidityContract, types.QsrTokenStandard, 2*epochQsr.Int64())
}
