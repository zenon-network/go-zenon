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
// MaxRewardEpochsPerCall bound on stake, sentinel and pillar reward
// updates. The frontier momentum timestamp is set far in the future so
// many epochs are due.
type testEpochBoundContext struct {
	store.Account
	momentum           *nom.Momentum
	dynPlasmaEnforced  bool
	epochStats         map[uint64]*api.EpochStats
	pillarDelegations  map[uint64]map[string]*types.PillarDelegationDetail
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
		dynPlasmaEnforced: true,
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
func (ctx *testEpochBoundContext) IsDynamicPlasmaSporkEnforced() bool      { return ctx.dynPlasmaEnforced }
func (ctx *testEpochBoundContext) IsPtlcSporkEnforced() bool               { return false }

func (ctx *testEpochBoundContext) GetPillarWeights() (map[string]*big.Int, error) {
	return nil, nil
}
func (ctx *testEpochBoundContext) EpochTicker() common.Ticker {
	return common.NewTicker(time.Unix(0, 0), time.Second)
}
func (ctx *testEpochBoundContext) EpochStats(epoch uint64) (*api.EpochStats, error) {
	if ctx.epochStats != nil {
		if stats, ok := ctx.epochStats[epoch]; ok {
			return stats, nil
		}
	}
	return &api.EpochStats{
		Epoch:       epoch,
		Pillars:     map[string]*api.EpochPillarStats{},
		TotalWeight: big.NewInt(0),
		TotalBlocks: 0,
	}, nil
}
func (ctx *testEpochBoundContext) GetPillarDelegationsByEpoch(epoch uint64) (map[string]*types.PillarDelegationDetail, error) {
	if ctx.pillarDelegations != nil {
		if delegations, ok := ctx.pillarDelegations[epoch]; ok {
			return delegations, nil
		}
	}
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

	// Every epoch must have a per-epoch reward deposit history entry.
	// If the cap advanced the marker without computing rewards (the
	// legacy liquidity pattern), the skipped epoch would have no history.
	addr := types.ParseAddressPanic("z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz")
	for epoch := int64(1); epoch <= int64(epochsToMakeDue); epoch++ {
		hisDeposit, err := definition.GetRewardDepositHistory(ctx.Storage(), uint64(epoch), &addr)
		common.FailIfErr(t, err)
		if hisDeposit == nil {
			t.Fatalf("no reward deposit history for epoch %d", epoch)
		}
		if hisDeposit.Qsr.Sign() <= 0 {
			t.Fatalf("epoch %d has no QSR reward in history", epoch)
		}
	}
}

// TestUpdateStakeRewardsUncappedBeforeSpork verifies that when the
// Dynamic Plasma spork is NOT enforced, the stake reward updater settles
// every due epoch in one call, preserving consensus compatibility with
// historical state.
func TestUpdateStakeRewardsUncappedBeforeSpork(t *testing.T) {
	// Make more than MaxRewardEpochsPerCall epochs due.
	epochsToMakeDue := constants.MaxRewardEpochsPerCall + 20
	frontierTime := time.Unix(int64(epochsToMakeDue)+1+3600, 0)
	ctx := newTestEpochBoundContext(t, frontierTime)
	ctx.dynPlasmaEnforced = false // pre-spork

	lastEpoch := &definition.LastEpochUpdate{LastEpoch: 0}
	common.FailIfErr(t, lastEpoch.Save(ctx.Storage()))
	seedStakeEntry(t, ctx, 0)

	// Pre-spork: all due epochs settle in one call, no cap.
	err := updateStakeRewards(ctx)
	common.FailIfErr(t, err)

	updated, err := definition.GetLastEpochUpdate(ctx.Storage())
	common.FailIfErr(t, err)
	if updated.LastEpoch != int64(epochsToMakeDue) {
		t.Fatalf("pre-spork: expected all %d epochs settled in one call, got %d",
			epochsToMakeDue, updated.LastEpoch)
	}
}

// seedSentinelEntry stores a single sentinel entry registered at the
// given time so computeSentinelRewardsForEpoch has something to iterate.
func seedSentinelEntry(t *testing.T, ctx *testEpochBoundContext, registrationTime int64) {
	t.Helper()
	addr := types.ParseAddressPanic("z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz")
	entry := &definition.SentinelInfo{
		SentinelInfoKey: definition.SentinelInfoKey{
			Owner: addr,
		},
		RegistrationTimestamp: registrationTime,
		RevokeTimestamp:       0,
		ZnnAmount:             big.NewInt(100),
		QsrAmount:             big.NewInt(100),
	}
	entry.Save(ctx.Storage())
}

// TestUpdateSentinelRewardsCappedAtMaxRewardEpochsPerCall verifies that
// the sentinel reward updater respects the same per-call epoch bound as
// the stake updater.
func TestUpdateSentinelRewardsCappedAtMaxRewardEpochsPerCall(t *testing.T) {
	epochsToMakeDue := constants.MaxRewardEpochsPerCall + 5
	frontierTime := time.Unix(int64(epochsToMakeDue)+1+3600, 0)
	ctx := newTestEpochBoundContext(t, frontierTime)

	lastEpoch := &definition.LastEpochUpdate{LastEpoch: 0}
	common.FailIfErr(t, lastEpoch.Save(ctx.Storage()))

	// Register sentinel at time 0 so it is active for all epochs.
	seedSentinelEntry(t, ctx, 0)

	// First call: should settle exactly MaxRewardEpochsPerCall epochs.
	err := updateSentinelRewards(ctx)
	common.FailIfErr(t, err)

	updated, err := definition.GetLastEpochUpdate(ctx.Storage())
	common.FailIfErr(t, err)
	if updated.LastEpoch != int64(constants.MaxRewardEpochsPerCall) {
		t.Fatalf("expected %d epochs settled, got %d",
			constants.MaxRewardEpochsPerCall, updated.LastEpoch)
	}

	// Second call: settle the remaining 5.
	err = updateSentinelRewards(ctx)
	common.FailIfErr(t, err)

	updated2, err := definition.GetLastEpochUpdate(ctx.Storage())
	common.FailIfErr(t, err)
	expected := int64(epochsToMakeDue)
	if updated2.LastEpoch != expected {
		t.Fatalf("expected %d total epochs settled after second call, got %d",
			expected, updated2.LastEpoch)
	}

	// Every epoch must have a per-epoch reward deposit history entry with
	// both ZNN and QSR credits. If the cap advanced the marker without
	// computing rewards, the skipped epochs would have zero or missing
	// history entries.
	addr := types.ParseAddressPanic("z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz")
	for epoch := int64(1); epoch <= int64(epochsToMakeDue); epoch++ {
		hisDeposit, err := definition.GetRewardDepositHistory(ctx.Storage(), uint64(epoch), &addr)
		common.FailIfErr(t, err)
		if hisDeposit == nil {
			t.Fatalf("no reward deposit history for epoch %d", epoch)
		}
		if hisDeposit.Znn.Sign() <= 0 {
			t.Fatalf("epoch %d has no ZNN reward in history", epoch)
		}
		if hisDeposit.Qsr.Sign() <= 0 {
			t.Fatalf("epoch %d has no QSR reward in history", epoch)
		}
	}

	// Third call: no more due epochs, should be a no-op. Capture the
	// current history so we can verify no duplicate credits appear.
	historyBefore := make(map[int64]*definition.RewardDepositHistory)
	for epoch := int64(1); epoch <= int64(epochsToMakeDue); epoch++ {
		h, err := definition.GetRewardDepositHistory(ctx.Storage(), uint64(epoch), &addr)
		common.FailIfErr(t, err)
		historyBefore[epoch] = h
	}

	err = updateSentinelRewards(ctx)
	common.FailIfErr(t, err)

	updated3, err := definition.GetLastEpochUpdate(ctx.Storage())
	common.FailIfErr(t, err)
	if updated3.LastEpoch != expected {
		t.Fatalf("expected no change on third call, got %d", updated3.LastEpoch)
	}
	for epoch := int64(1); epoch <= int64(epochsToMakeDue); epoch++ {
		h, err := definition.GetRewardDepositHistory(ctx.Storage(), uint64(epoch), &addr)
		common.FailIfErr(t, err)
		if h.Znn.Cmp(historyBefore[epoch].Znn) != 0 {
			t.Fatalf("epoch %d ZNN changed on no-op call: before %s, after %s",
				epoch, historyBefore[epoch].Znn, h.Znn)
		}
		if h.Qsr.Cmp(historyBefore[epoch].Qsr) != 0 {
			t.Fatalf("epoch %d QSR changed on no-op call: before %s, after %s",
				epoch, historyBefore[epoch].Qsr, h.Qsr)
		}
	}
}

// TestUpdateSentinelRewardsUncappedBeforeSpork verifies that the sentinel
// reward updater is unbounded when the Dynamic Plasma spork is not
// enforced.
func TestUpdateSentinelRewardsUncappedBeforeSpork(t *testing.T) {
	epochsToMakeDue := constants.MaxRewardEpochsPerCall + 20
	frontierTime := time.Unix(int64(epochsToMakeDue)+1+3600, 0)
	ctx := newTestEpochBoundContext(t, frontierTime)
	ctx.dynPlasmaEnforced = false

	lastEpoch := &definition.LastEpochUpdate{LastEpoch: 0}
	common.FailIfErr(t, lastEpoch.Save(ctx.Storage()))
	seedSentinelEntry(t, ctx, 0)

	err := updateSentinelRewards(ctx)
	common.FailIfErr(t, err)

	updated, err := definition.GetLastEpochUpdate(ctx.Storage())
	common.FailIfErr(t, err)
	if updated.LastEpoch != int64(epochsToMakeDue) {
		t.Fatalf("pre-spork: expected all %d epochs settled in one call, got %d",
			epochsToMakeDue, updated.LastEpoch)
	}
}

// seedPillarEntry stores a single pillar entry so
// computeDetailedPillarReward has something to iterate.
func seedPillarEntry(t *testing.T, ctx *testEpochBoundContext, name string, registrationTime int64) {
	t.Helper()
	addr := types.ParseAddressPanic("z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz")
	pillar := &definition.PillarInfo{
		Name:                         name,
		BlockProducingAddress:        addr,
		RewardWithdrawAddress:        addr,
		StakeAddress:                 addr,
		Amount:                       big.NewInt(100),
		RegistrationTime:             registrationTime,
		RevokeTime:                   0,
		GiveBlockRewardPercentage:    0,
		GiveDelegateRewardPercentage: 0,
		PillarType:                   definition.AnyPillarType,
	}
	common.FailIfErr(t, pillar.Save(ctx.Storage()))
}

// TestUpdatePillarRewardsCappedAtMaxRewardEpochsPerCall verifies that
// the pillar reward updater respects the same per-call epoch bound.
func TestUpdatePillarRewardsCappedAtMaxRewardEpochsPerCall(t *testing.T) {
	epochsToMakeDue := constants.MaxRewardEpochsPerCall + 5
	frontierTime := time.Unix(int64(epochsToMakeDue)+1+3600, 0)
	ctx := newTestEpochBoundContext(t, frontierTime)

	lastEpoch := &definition.LastEpochUpdate{LastEpoch: 0}
	common.FailIfErr(t, lastEpoch.Save(ctx.Storage()))

	// Register a pillar and provide epoch stats + delegations so
	// computeDetailedPillarReward can run.
	seedPillarEntry(t, ctx, "testpillar", 0)

	pillarStats := &api.EpochPillarStats{
		Epoch:            0,
		BlockNum:         1,
		ExpectedBlockNum: 1,
		Weight:           big.NewInt(100),
		Name:             "testpillar",
	}
	ctx.epochStats = make(map[uint64]*api.EpochStats)
	ctx.pillarDelegations = make(map[uint64]map[string]*types.PillarDelegationDetail)
	for e := uint64(1); e <= uint64(epochsToMakeDue); e++ {
		ctx.epochStats[e] = &api.EpochStats{
			Epoch:   e,
			Pillars: map[string]*api.EpochPillarStats{"testpillar": pillarStats},
			TotalWeight: big.NewInt(100),
			TotalBlocks: 1,
		}
		ctx.pillarDelegations[e] = map[string]*types.PillarDelegationDetail{
			"testpillar": {
				PillarDelegation: types.PillarDelegation{
					Name:   "testpillar",
					Weight: big.NewInt(100),
				},
				Backers: map[types.Address]*big.Int{},
			},
		}
	}

	// First call: should settle exactly MaxRewardEpochsPerCall epochs.
	err := updatePillarRewards(ctx)
	common.FailIfErr(t, err)

	updated, err := definition.GetLastEpochUpdate(ctx.Storage())
	common.FailIfErr(t, err)
	if updated.LastEpoch != int64(constants.MaxRewardEpochsPerCall) {
		t.Fatalf("expected %d epochs settled, got %d",
			constants.MaxRewardEpochsPerCall, updated.LastEpoch)
	}

	// Second call: settle the remaining 5.
	err = updatePillarRewards(ctx)
	common.FailIfErr(t, err)

	updated2, err := definition.GetLastEpochUpdate(ctx.Storage())
	common.FailIfErr(t, err)
	expected := int64(epochsToMakeDue)
	if updated2.LastEpoch != expected {
		t.Fatalf("expected %d total epochs settled after second call, got %d",
			expected, updated2.LastEpoch)
	}

	// Every epoch must have a per-epoch reward deposit history entry with
	// ZNN credits. Pillar rewards are ZNN-only (QSR is always zero).
	// If the cap advanced the marker without computing rewards, the
	// skipped epochs would have zero or missing history entries.
	addr := types.ParseAddressPanic("z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz")
	for epoch := int64(1); epoch <= int64(epochsToMakeDue); epoch++ {
		hisDeposit, err := definition.GetRewardDepositHistory(ctx.Storage(), uint64(epoch), &addr)
		common.FailIfErr(t, err)
		if hisDeposit == nil {
			t.Fatalf("no reward deposit history for epoch %d", epoch)
		}
		if hisDeposit.Znn.Sign() <= 0 {
			t.Fatalf("epoch %d has no ZNN reward in history", epoch)
		}
		if hisDeposit.Qsr.Sign() != 0 {
			t.Fatalf("epoch %d has unexpected QSR reward in history: %s", epoch, hisDeposit.Qsr)
		}
	}

	// Third call: no more due epochs, should be a no-op. Capture the
	// current history so we can verify no duplicate credits appear.
	historyBefore := make(map[int64]*definition.RewardDepositHistory)
	for epoch := int64(1); epoch <= int64(epochsToMakeDue); epoch++ {
		h, err := definition.GetRewardDepositHistory(ctx.Storage(), uint64(epoch), &addr)
		common.FailIfErr(t, err)
		historyBefore[epoch] = h
	}

	err = updatePillarRewards(ctx)
	common.FailIfErr(t, err)

	updated3, err := definition.GetLastEpochUpdate(ctx.Storage())
	common.FailIfErr(t, err)
	if updated3.LastEpoch != expected {
		t.Fatalf("expected no change on third call, got %d", updated3.LastEpoch)
	}
	for epoch := int64(1); epoch <= int64(epochsToMakeDue); epoch++ {
		h, err := definition.GetRewardDepositHistory(ctx.Storage(), uint64(epoch), &addr)
		common.FailIfErr(t, err)
		if h.Znn.Cmp(historyBefore[epoch].Znn) != 0 {
			t.Fatalf("epoch %d ZNN changed on no-op call: before %s, after %s",
				epoch, historyBefore[epoch].Znn, h.Znn)
		}
		if h.Qsr.Cmp(historyBefore[epoch].Qsr) != 0 {
			t.Fatalf("epoch %d QSR changed on no-op call: before %s, after %s",
				epoch, historyBefore[epoch].Qsr, h.Qsr)
		}
	}
}

// TestUpdatePillarRewardsUncappedBeforeSpork verifies that the pillar
// reward updater is unbounded when the Dynamic Plasma spork is not
// enforced.
func TestUpdatePillarRewardsUncappedBeforeSpork(t *testing.T) {
	epochsToMakeDue := constants.MaxRewardEpochsPerCall + 20
	frontierTime := time.Unix(int64(epochsToMakeDue)+1+3600, 0)
	ctx := newTestEpochBoundContext(t, frontierTime)
	ctx.dynPlasmaEnforced = false

	lastEpoch := &definition.LastEpochUpdate{LastEpoch: 0}
	common.FailIfErr(t, lastEpoch.Save(ctx.Storage()))
	seedPillarEntry(t, ctx, "testpillar", 0)

	pillarStats := &api.EpochPillarStats{
		Epoch:            0,
		BlockNum:         1,
		ExpectedBlockNum: 1,
		Weight:           big.NewInt(100),
		Name:             "testpillar",
	}
	ctx.epochStats = make(map[uint64]*api.EpochStats)
	ctx.pillarDelegations = make(map[uint64]map[string]*types.PillarDelegationDetail)
	for e := uint64(1); e <= uint64(epochsToMakeDue); e++ {
		ctx.epochStats[e] = &api.EpochStats{
			Epoch:   e,
			Pillars: map[string]*api.EpochPillarStats{"testpillar": pillarStats},
			TotalWeight: big.NewInt(100),
			TotalBlocks: 1,
		}
		ctx.pillarDelegations[e] = map[string]*types.PillarDelegationDetail{
			"testpillar": {
				PillarDelegation: types.PillarDelegation{
					Name:   "testpillar",
					Weight: big.NewInt(100),
				},
				Backers: map[types.Address]*big.Int{},
			},
		}
	}

	err := updatePillarRewards(ctx)
	common.FailIfErr(t, err)

	updated, err := definition.GetLastEpochUpdate(ctx.Storage())
	common.FailIfErr(t, err)
	if updated.LastEpoch != int64(epochsToMakeDue) {
		t.Fatalf("pre-spork: expected all %d epochs settled in one call, got %d",
			epochsToMakeDue, updated.LastEpoch)
	}
}
