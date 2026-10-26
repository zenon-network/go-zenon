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

// testEpochBoundContext is a minimal AccountVmContext for exercising the
// MaxRewardEpochsPerCall bound on stake reward updates. The frontier
// momentum timestamp is set far in the future so many epochs are due.
type testEpochBoundContext struct {
	store.Account
	momentum *nom.Momentum
}

func newTestEpochBoundContext(t *testing.T, frontierTime time.Time) *testEpochBoundContext {
	t.Helper()
	return &testEpochBoundContext{
		Account: accountstore.NewAccountStore(types.StakeContract, db.NewMemDB()),
		momentum: &nom.Momentum{
			ChainIdentifier: 100,
			Timestamp:       &frontierTime,
			Height:          10,
		},
	}
}

func (ctx *testEpochBoundContext) CacheStore() store.Cache       { return nil }
func (ctx *testEpochBoundContext) MomentumStore() store.Momentum { return nil }

func (ctx *testEpochBoundContext) GetFrontierMomentum() (*nom.Momentum, error) {
	return ctx.momentum, nil
}
func (ctx *testEpochBoundContext) GetGenesisMomentum() *nom.Momentum { return ctx.momentum }

func (ctx *testEpochBoundContext) Save()  {}
func (ctx *testEpochBoundContext) Reset() {}
func (ctx *testEpochBoundContext) Done()  {}

func (ctx *testEpochBoundContext) AddBalance(ts *types.ZenonTokenStandard, amount *big.Int) {}
func (ctx *testEpochBoundContext) SubBalance(ts *types.ZenonTokenStandard, amount *big.Int) {}

func (ctx *testEpochBoundContext) IsAcceleratorSporkEnforced() bool        { return true }
func (ctx *testEpochBoundContext) IsHtlcSporkEnforced() bool               { return true }
func (ctx *testEpochBoundContext) IsBridgeAndLiquiditySporkEnforced() bool { return true }
func (ctx *testEpochBoundContext) IsDynamicPlasmaSporkEnforced() bool      { return true }
func (ctx *testEpochBoundContext) IsPtlcSporkEnforced() bool               { return false }

func (ctx *testEpochBoundContext) GetPillarWeights() (map[string]*big.Int, error) {
	return nil, nil
}
func (ctx *testEpochBoundContext) EpochTicker() common.Ticker {
	return common.NewTicker(time.Unix(0, 0), time.Second)
}
func (ctx *testEpochBoundContext) EpochStats(epoch uint64) (*api.EpochStats, error) {
	return nil, nil
}
func (ctx *testEpochBoundContext) GetPillarDelegationsByEpoch(epoch uint64) (map[string]*types.PillarDelegationDetail, error) {
	return nil, nil
}

// seedStakeEntry stores a single stake entry with the given start time so
// computeStakeRewardsForEpoch has something to iterate.
func seedStakeEntry(t *testing.T, ctx *testEpochBoundContext, startTime int64) {
	t.Helper()
	addr := types.ParseAddressPanic("z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz")
	entry := &definition.StakeInfo{
		StakeAddress:   addr,
		Amount:         big.NewInt(100),
		StartTime:      startTime,
		RevokeTime:     0,
		ExpirationTime: startTime + 1000000,
		Id:             types.HexToHashPanic("0000000000000000000000000000000000000000000000000000000000000001"),
		WeightedAmount: big.NewInt(100),
	}
	common.FailIfErr(t, entry.Save(ctx.Storage()))
}

// TestUpdateStakeRewardsCappedAtMaxRewardEpochsPerCall verifies that when
// more than MaxRewardEpochsPerCall epochs are due, the updater settles
// exactly MaxRewardEpochsPerCall and leaves the rest for the next call.
func TestUpdateStakeRewardsCappedAtMaxRewardEpochsPerCall(t *testing.T) {
	// The test EpochTicker uses 1-second intervals (not 3600s). For epoch N
	// to be due, frontierTime must be >= (N+1)*1 + RewardTimeLimit(3600).
	// We want exactly MaxRewardEpochsPerCall+5 epochs due.
	epochsToMakeDue := constants.MaxRewardEpochsPerCall + 5
	// For epoch 55: need frontier >= 55+1+3600 = 3656
	// For epoch 56 to NOT be due: frontier < 56+1+3600 = 3657
	frontierTime := time.Unix(int64(epochsToMakeDue)+1+3600, 0)
	ctx := newTestEpochBoundContext(t, frontierTime)

	// Set the last-epoch marker to 0 so epochs 1..epochsToMakeDue are all due.
	lastEpoch := &definition.LastEpochUpdate{LastEpoch: 0}
	common.FailIfErr(t, lastEpoch.Save(ctx.Storage()))

	// Seed one stake entry.
	seedStakeEntry(t, ctx, 0)

	// First call: should settle exactly MaxRewardEpochsPerCall epochs.
	err := updateStakeRewards(ctx)
	common.FailIfErr(t, err)

	updated, err := definition.GetLastEpochUpdate(ctx.Storage())
	common.FailIfErr(t, err)
	if updated.LastEpoch != int64(constants.MaxRewardEpochsPerCall) {
		t.Fatalf("expected %d epochs settled, got %d", constants.MaxRewardEpochsPerCall, updated.LastEpoch)
	}

	// Second call: should settle the remaining 5 epochs (all were due from
	// the start because the frontier time is far ahead).
	err = updateStakeRewards(ctx)
	common.FailIfErr(t, err)

	updated2, err := definition.GetLastEpochUpdate(ctx.Storage())
	common.FailIfErr(t, err)
	expected := int64(epochsToMakeDue)
	if updated2.LastEpoch != expected {
		t.Fatalf("expected %d total epochs settled after second call, got %d", expected, updated2.LastEpoch)
	}

	// Third call: no more due epochs, should be a no-op.
	err = updateStakeRewards(ctx)
	common.FailIfErr(t, err)

	updated3, err := definition.GetLastEpochUpdate(ctx.Storage())
	common.FailIfErr(t, err)
	if updated3.LastEpoch != expected {
		t.Fatalf("expected no change on third call, got %d", updated3.LastEpoch)
	}
}

// TestUpdateStakeRewardsCapDoesNotSkipEpoch verifies that the cap check
// happens before the epoch marker advances, so a capped call never skips
// an epoch. This is the key difference from the legacy liquidity pattern.
func TestUpdateStakeRewardsCapDoesNotSkipEpoch(t *testing.T) {
	// Make exactly MaxRewardEpochsPerCall epochs due.
	epochsToMakeDue := constants.MaxRewardEpochsPerCall
	// For epoch 50: need frontier >= 50+1+3600 = 3651
	// For epoch 51 to NOT be due: frontier < 51+1+3600 = 3652
	frontierTime := time.Unix(int64(epochsToMakeDue)+1+3600, 0)
	ctx := newTestEpochBoundContext(t, frontierTime)

	lastEpoch := &definition.LastEpochUpdate{LastEpoch: 0}
	common.FailIfErr(t, lastEpoch.Save(ctx.Storage()))
	seedStakeEntry(t, ctx, 0)

	// Settle all due epochs.
	err := updateStakeRewards(ctx)
	common.FailIfErr(t, err)

	updated, err := definition.GetLastEpochUpdate(ctx.Storage())
	common.FailIfErr(t, err)
	if updated.LastEpoch != int64(epochsToMakeDue) {
		t.Fatalf("expected %d epochs settled, got %d", epochsToMakeDue, updated.LastEpoch)
	}

	// Every epoch must have a reward deposit for the staker. If the cap
	// advanced the marker without computing rewards, the last epoch would
	// have no deposit.
	addr := types.ParseAddressPanic("z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz")
	for epoch := int64(1); epoch <= int64(epochsToMakeDue); epoch++ {
		deposit, err := definition.GetRewardDeposit(ctx.Storage(), &addr)
		common.FailIfErr(t, err)
		if deposit == nil {
			t.Fatalf("no reward deposit found for epoch %d", epoch)
		}
		if deposit.Qsr.Sign() <= 0 {
			t.Fatalf("epoch %d has no QSR reward", epoch)
		}
	}
}
