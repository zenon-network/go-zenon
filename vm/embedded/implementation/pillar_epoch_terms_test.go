package implementation

import (
	"errors"
	"math/big"
	"testing"
	"time"

	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/db"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/consensus/api"
	"github.com/zenon-network/go-zenon/vm/constants"
	"github.com/zenon-network/go-zenon/vm/embedded/definition"
)

// Tests for issue #228: once types.EpochRewardTermsSpork is enforced, an
// epoch's pillar reward is split with the give-percentages that were in
// force during that epoch, not those stored in PillarInfo when the epoch
// is settled.
//
// The harness is testEpochBoundContext (epoch_reward_bound_test.go). Its
// epoch ticker has 1-second epochs starting at unix 0, so epoch e covers
// unix seconds [e, e+1) and epoch e can be settled once the frontier
// momentum is at or after e+1+constants.RewardTimeLimit.

const termsPillarName = "terms-pillar"

var (
	termsOwner      = termsTestAddress(1) // stake and producing address
	termsRewardAddr = termsTestAddress(2) // reward withdraw address
	termsBacker     = termsTestAddress(3) // the pillar's only delegator
)

func termsTestAddress(b byte) types.Address {
	var a types.Address
	a[0] = types.UserAddrByte
	a[1] = b
	a[types.AddressSize-1] = b
	return a
}

// settleTime is the earliest frontier time at which epoch can be settled.
func settleTime(epoch uint64) int64 {
	return int64(epoch) + 1 + constants.RewardTimeLimit
}

type termsHarness struct {
	t   *testing.T
	ctx *testEpochBoundContext
}

// newTermsHarness registers termsPillarName with the given split, marks
// epoch 0 as settled and gives every epoch in [1, lastEpoch] the same
// statistics: the pillar produced all 10 of its 10 expected momentums,
// holds the whole delegated weight of 1000 and has termsBacker as its
// only delegator.
func newTermsHarness(t *testing.T, termsEnforced bool, giveBlock, giveDelegate uint8, lastEpoch uint64) *termsHarness {
	t.Helper()
	ctx := newTestEpochBoundContext(t, time.Unix(0, 0))
	if termsEnforced {
		ctx.activeSporks = map[types.Hash]bool{types.EpochRewardTermsSpork.SporkId: true}
	}

	common.FailIfErr(t, (&definition.PillarInfo{
		Name:                         termsPillarName,
		BlockProducingAddress:        termsOwner,
		RewardWithdrawAddress:        termsRewardAddr,
		StakeAddress:                 termsOwner,
		Amount:                       constants.PillarStakeAmount,
		RegistrationTime:             0,
		GiveBlockRewardPercentage:    giveBlock,
		GiveDelegateRewardPercentage: giveDelegate,
		PillarType:                   definition.NormalPillarType,
	}).Save(ctx.Storage()))
	common.FailIfErr(t, (&definition.LastEpochUpdate{LastEpoch: 0}).Save(ctx.Storage()))

	ctx.epochStats = make(map[uint64]*api.EpochStats)
	ctx.pillarDelegations = make(map[uint64]map[string]*types.PillarDelegationDetail)
	for e := uint64(1); e <= lastEpoch; e++ {
		ctx.epochStats[e] = &api.EpochStats{
			Epoch: e,
			Pillars: map[string]*api.EpochPillarStats{
				termsPillarName: {
					Epoch:            e,
					BlockNum:         10,
					ExpectedBlockNum: 10,
					Weight:           big.NewInt(1000),
					Name:             termsPillarName,
				},
			},
			TotalWeight: big.NewInt(1000),
			TotalBlocks: 10,
		}
		ctx.pillarDelegations[e] = map[string]*types.PillarDelegationDetail{
			termsPillarName: {
				PillarDelegation: types.PillarDelegation{
					Name:   termsPillarName,
					Weight: big.NewInt(1000),
				},
				Backers: map[types.Address]*big.Int{termsBacker: big.NewInt(1000)},
			},
		}
	}
	return &termsHarness{t: t, ctx: ctx}
}

func (h *termsHarness) setFrontier(unix int64) {
	ts := time.Unix(unix, 0)
	h.ctx.momentum = &nom.Momentum{ChainIdentifier: 100, Timestamp: &ts, Height: 10}
}

func (h *termsHarness) setTermsEnforced(enforced bool) {
	h.ctx.activeSporks = map[types.Hash]bool{types.EpochRewardTermsSpork.SporkId: enforced}
}

// update runs UpdatePillar for termsPillarName at frontier time unix,
// keeping the producing address and setting the reward address and split.
func (h *termsHarness) update(unix int64, rewardAddr types.Address, giveBlock, giveDelegate uint8) error {
	h.t.Helper()
	h.setFrontier(unix)
	method := &UpdatePillarMethod{MethodName: definition.UpdatePillarMethodName}
	_, err := method.ReceiveBlock(h.ctx, &nom.AccountBlock{
		Address:       termsOwner,
		ToAddress:     types.PillarContract,
		BlockType:     nom.BlockTypeUserSend,
		Amount:        big.NewInt(0),
		TokenStandard: types.ZnnTokenStandard,
		Data: definition.ABIPillars.PackMethodPanic(definition.UpdatePillarMethodName,
			termsPillarName, termsOwner, rewardAddr, giveBlock, giveDelegate),
	})
	return err
}

func (h *termsHarness) mustUpdate(unix int64, giveBlock, giveDelegate uint8) {
	h.t.Helper()
	common.FailIfErr(h.t, h.update(unix, termsRewardAddr, giveBlock, giveDelegate))
}

// settle runs one pillar reward update at frontier time unix and checks
// that it settled every epoch up to wantLast.
func (h *termsHarness) settle(unix int64, wantLast int64) {
	h.t.Helper()
	h.setFrontier(unix)
	common.FailIfErr(h.t, updatePillarRewards(h.ctx))
	last, err := definition.GetLastEpochUpdate(h.ctx.Storage())
	common.FailIfErr(h.t, err)
	if last.LastEpoch != wantLast {
		h.t.Fatalf("last settled epoch %d, want %d", last.LastEpoch, wantLast)
	}
}

// epochRewards returns the momentum and delegation rewards the harness
// statistics earn in epoch: 10 produced momentums at the epoch's
// per-momentum rates.
func epochRewards(epoch uint64) (block, delegation *big.Int) {
	delegationPerMomentum, blockPerMomentum := constants.PillarRewardPerMomentum(epoch)
	return new(big.Int).Mul(blockPerMomentum, big.NewInt(10)), new(big.Int).Mul(delegationPerMomentum, big.NewInt(10))
}

// expectedSplit returns the delegator and pillar shares of epoch's reward
// under the split giveBlock/giveDelegate.
func expectedSplit(epoch uint64, giveBlock, giveDelegate uint8) (delegator, pillar, total *big.Int) {
	block, delegation := epochRewards(epoch)
	total = new(big.Int).Add(block, delegation)
	delegator = new(big.Int).Mul(block, big.NewInt(int64(giveBlock)))
	delegator.Add(delegator, new(big.Int).Mul(delegation, big.NewInt(int64(giveDelegate))))
	delegator.Quo(delegator, big.NewInt(100))
	pillar = new(big.Int).Sub(total, delegator)
	return
}

// expectEpoch checks the PillarEpochHistory entry and the per-epoch
// RewardDepositHistory credits of the delegator and the pillar's reward
// address for epoch against the split giveBlock/giveDelegate.
func (h *termsHarness) expectEpoch(epoch uint64, giveBlock, giveDelegate uint8) {
	h.t.Helper()
	storage := h.ctx.Storage()

	history, err := definition.GetPillarEpochHistoryList(storage, epoch)
	common.FailIfErr(h.t, err)
	if len(history) != 1 || history[0].Name != termsPillarName {
		h.t.Fatalf("epoch %d: history %+v, want one entry for %s", epoch, history, termsPillarName)
	}
	if history[0].GiveBlockRewardPercentage != giveBlock || history[0].GiveDelegateRewardPercentage != giveDelegate {
		h.t.Fatalf("epoch %d: history split %d/%d, want %d/%d", epoch,
			history[0].GiveBlockRewardPercentage, history[0].GiveDelegateRewardPercentage, giveBlock, giveDelegate)
	}

	wantDelegator, wantPillar, total := expectedSplit(epoch, giveBlock, giveDelegate)
	delegator, err := definition.GetRewardDepositHistory(storage, epoch, &termsBacker)
	common.FailIfErr(h.t, err)
	pillar, err := definition.GetRewardDepositHistory(storage, epoch, &termsRewardAddr)
	common.FailIfErr(h.t, err)
	if delegator.Znn.Cmp(wantDelegator) != 0 {
		h.t.Fatalf("epoch %d: delegator credited %s, want %s (split %d/%d)", epoch, delegator.Znn, wantDelegator, giveBlock, giveDelegate)
	}
	if pillar.Znn.Cmp(wantPillar) != 0 {
		h.t.Fatalf("epoch %d: pillar credited %s, want %s (split %d/%d)", epoch, pillar.Znn, wantPillar, giveBlock, giveDelegate)
	}
	if sum := new(big.Int).Add(delegator.Znn, pillar.Znn); sum.Cmp(total) != 0 {
		h.t.Fatalf("epoch %d: delegator + pillar = %s, want the epoch total %s", epoch, sum, total)
	}
	if delegator.Qsr.Sign() != 0 || pillar.Qsr.Sign() != 0 {
		h.t.Fatalf("epoch %d: unexpected QSR credit", epoch)
	}
}

// expectDeposits checks the cumulative RewardDeposit of the delegator and
// the pillar's reward address against the sum of the given per-epoch
// splits.
func (h *termsHarness) expectDeposits(splits map[uint64][2]uint8) {
	h.t.Helper()
	wantDelegator, wantPillar := big.NewInt(0), big.NewInt(0)
	for epoch, split := range splits {
		d, p, _ := expectedSplit(epoch, split[0], split[1])
		wantDelegator.Add(wantDelegator, d)
		wantPillar.Add(wantPillar, p)
	}
	delegator, err := definition.GetRewardDeposit(h.ctx.Storage(), &termsBacker)
	common.FailIfErr(h.t, err)
	pillar, err := definition.GetRewardDeposit(h.ctx.Storage(), &termsRewardAddr)
	common.FailIfErr(h.t, err)
	if delegator.Znn.Cmp(wantDelegator) != 0 {
		h.t.Fatalf("delegator RewardDeposit %s, want %s", delegator.Znn, wantDelegator)
	}
	if pillar.Znn.Cmp(wantPillar) != 0 {
		h.t.Fatalf("pillar RewardDeposit %s, want %s", pillar.Znn, wantPillar)
	}
}

// expectTerms checks the stored PillarEpochTerms records of
// termsPillarName, as {EffectiveEpoch, giveBlock, giveDelegate} triples.
func (h *termsHarness) expectTerms(want ...[3]uint64) {
	h.t.Helper()
	list, err := definition.GetPillarEpochTermsList(h.ctx.Storage(), termsPillarName)
	common.FailIfErr(h.t, err)
	got := make([][3]uint64, 0, len(list))
	for _, r := range list {
		got = append(got, [3]uint64{r.EffectiveEpoch, uint64(r.GiveBlockRewardPercentage), uint64(r.GiveDelegateRewardPercentage)})
	}
	if len(got) != len(want) {
		h.t.Fatalf("terms records %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			h.t.Fatalf("terms records %v, want %v", got, want)
		}
	}
}

// The harness's reward figures, pinned so the split assertions compare
// against fixed amounts rather than only against the formula. Epochs 1-6
// fall in the first reward tick: each earns a momentum reward of
// 833333330 and a delegation reward of 400000000 (ZNN smallest units).
// A 0/100 epoch credits 400000000 to the delegator and 833333330 to the
// pillar; a 40/25 epoch credits (40*833333330 + 25*400000000)/100 =
// 433333332 and 799999998.
func TestEpochRewardTerms_ExactCredits(t *testing.T) {
	block, delegation := epochRewards(1)
	common.Expect(t, block.String(), "833333330")
	common.Expect(t, delegation.String(), "400000000")

	h := newTermsHarness(t, true, 0, 100, 2)
	h.mustUpdate(1, 40, 25) // epoch 1: governs epoch 2
	h.settle(settleTime(2), 2)

	storage := h.ctx.Storage()
	for _, c := range []struct {
		epoch     uint64
		addr      types.Address
		wantEpoch string
	}{
		{1, termsBacker, "400000000"},
		{1, termsRewardAddr, "833333330"},
		{2, termsBacker, "433333332"},
		{2, termsRewardAddr, "799999998"},
	} {
		hist, err := definition.GetRewardDepositHistory(storage, c.epoch, &c.addr)
		common.FailIfErr(t, err)
		common.Expect(t, hist.Znn.String(), c.wantEpoch)
	}
	for _, c := range []struct {
		addr types.Address
		want string
	}{
		{termsBacker, "833333332"},
		{termsRewardAddr, "1633333328"},
	} {
		deposit, err := definition.GetRewardDeposit(storage, &c.addr)
		common.FailIfErr(t, err)
		common.Expect(t, deposit.Znn.String(), c.want)
	}
	// 833333332 + 1633333328 = 2 * (833333330 + 400000000)
	h.expectEpoch(1, 0, 100)
	h.expectEpoch(2, 40, 25)
}

// The issue's scenario: a pillar advertising 0/100 changes to 0/0 after
// epochs 1-3 have ended but before they are settled. With the spork the
// three epochs are still split 0/100; without it (pinned legacy
// behaviour) they are split with the 0/0 stored at settlement time and
// the delegator receives nothing.
func TestEpochRewardTerms_ChangeAfterEpochEndDoesNotReachIt(t *testing.T) {
	for _, tc := range []struct {
		name          string
		termsEnforced bool
		want          [2]uint8
	}{
		{"spork", true, [2]uint8{0, 100}},
		{"pre-spork", false, [2]uint8{0, 0}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newTermsHarness(t, tc.termsEnforced, 0, 100, 3)
			// epoch 3 ended at unix 4; settlement opens at 3604
			h.mustUpdate(10, 0, 0)
			h.settle(settleTime(3), 3)

			splits := map[uint64][2]uint8{}
			for e := uint64(1); e <= 3; e++ {
				h.expectEpoch(e, tc.want[0], tc.want[1])
				splits[e] = tc.want
			}
			h.expectDeposits(splits)

			if tc.termsEnforced {
				h.expectTerms([3]uint64{0, 0, 100}, [3]uint64{11, 0, 0})
			} else {
				h.expectTerms()
			}
		})
	}
}

// A change applies from the epoch after the one it executes in: a
// block-only change in epoch 2 governs epochs 3-4, and of two changes in
// epoch 4 the later one (delegate-only relative to the epoch 3-4 split)
// governs epochs 5-6. Earlier epochs keep the baseline.
func TestEpochRewardTerms_ChangeTakesEffectNextEpoch(t *testing.T) {
	h := newTermsHarness(t, true, 0, 100, 6)

	h.mustUpdate(2, 40, 100) // epoch 2, block share only
	h.mustUpdate(4, 90, 90)  // epoch 4, overwritten below
	h.mustUpdate(4, 40, 25)  // epoch 4, delegate share only vs 40/100
	h.expectTerms([3]uint64{0, 0, 100}, [3]uint64{3, 40, 100}, [3]uint64{5, 40, 25})

	h.settle(settleTime(6), 6)

	want := map[uint64][2]uint8{
		1: {0, 100}, 2: {0, 100},
		3: {40, 100}, 4: {40, 100},
		5: {40, 25}, 6: {40, 25},
	}
	for e, split := range want {
		h.expectEpoch(e, split[0], split[1])
	}
	h.expectDeposits(want)
}

// Several unsettled epochs behind the frontier keep their own terms even
// when the split changes again before any of them is settled.
func TestEpochRewardTerms_SeveralUnsettledEpochs(t *testing.T) {
	h := newTermsHarness(t, true, 10, 10, 5)

	h.mustUpdate(1, 20, 20)
	h.mustUpdate(2, 30, 30)
	h.mustUpdate(3, 40, 40)
	// after epoch 5 ended, before settlement: must reach no settled epoch
	h.mustUpdate(100, 100, 100)

	h.settle(settleTime(5), 5)

	want := map[uint64][2]uint8{
		1: {10, 10},
		2: {20, 20},
		3: {30, 30},
		4: {40, 40},
		5: {40, 40},
	}
	for e, split := range want {
		h.expectEpoch(e, split[0], split[1])
	}
	h.expectDeposits(want)
}

// An UpdatePillar that leaves both give-percentages unchanged (here it
// only moves the reward address) records no terms.
func TestEpochRewardTerms_UnchangedPercentagesRecordNothing(t *testing.T) {
	h := newTermsHarness(t, true, 25, 75, 1)
	common.FailIfErr(t, h.update(1, termsTestAddress(9), 25, 75))
	h.expectTerms()

	pillar, err := definition.GetPillarInfo(h.ctx.Storage(), termsPillarName)
	common.FailIfErr(t, err)
	common.Expect(t, pillar.RewardWithdrawAddress, termsTestAddress(9))
}

// Interaction with MaxRewardEpochsPerCall (#234): settlement of a backlog
// is spread over several Update calls, and an UpdatePillar between two of
// them must not change the split of epochs that had already ended. The
// pre-spork run pins the legacy behaviour, where the later batch picks up
// the new split.
func TestEpochRewardTerms_ChangeBetweenSettlementBatches(t *testing.T) {
	due := uint64(constants.MaxRewardEpochsPerCall + 5)
	for _, tc := range []struct {
		name          string
		termsEnforced bool
		laterBatch    [2]uint8
	}{
		{"spork", true, [2]uint8{0, 100}},
		{"pre-spork", false, [2]uint8{0, 0}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newTermsHarness(t, tc.termsEnforced, 0, 100, due)
			if !h.ctx.dynPlasmaEnforced {
				t.Fatal("the batch bound needs the Dynamic Plasma spork")
			}

			h.settle(settleTime(due), int64(constants.MaxRewardEpochsPerCall))
			h.mustUpdate(settleTime(due), 0, 0)
			h.settle(settleTime(due), int64(due))

			want := map[uint64][2]uint8{}
			for e := uint64(1); e <= due; e++ {
				split := [2]uint8{0, 100}
				if e > uint64(constants.MaxRewardEpochsPerCall) {
					split = tc.laterBatch
				}
				h.expectEpoch(e, split[0], split[1])
				want[e] = split
			}
			h.expectDeposits(want)
		})
	}
}

// Activation: a change made before the spork records nothing, so epochs
// settled after activation fall back to PillarInfo until the pillar's
// first post-spork change, whose baseline record is the PillarInfo split
// at that time. Epochs settled before activation are not revisited.
func TestEpochRewardTerms_ActivationTransition(t *testing.T) {
	h := newTermsHarness(t, false, 0, 100, 4)

	h.settle(settleTime(1), 1) // pre-spork, at 0/100
	h.mustUpdate(settleTime(1), 30, 30)
	h.expectTerms()

	h.setTermsEnforced(true)
	h.settle(settleTime(2), 2) // no record: PillarInfo's 30/30
	h.mustUpdate(settleTime(2), 50, 50)
	h.expectTerms([3]uint64{0, 30, 30}, [3]uint64{uint64(settleTime(2)) + 1, 50, 50})
	h.settle(settleTime(4), 4)

	want := map[uint64][2]uint8{
		1: {0, 100},
		2: {30, 30},
		3: {30, 30},
		4: {30, 30},
	}
	for e, split := range want {
		h.expectEpoch(e, split[0], split[1])
	}
	h.expectDeposits(want)
}

// Settlement and changes prune the records that can no longer govern an
// unsettled epoch, keeping the one that governs the next epoch to settle.
func TestEpochRewardTerms_PruneKeepsGoverningRecord(t *testing.T) {
	h := newTermsHarness(t, true, 0, 100, 6)

	h.mustUpdate(2, 10, 10)
	h.expectTerms([3]uint64{0, 0, 100}, [3]uint64{3, 10, 10})

	h.settle(settleTime(2), 2) // record 0 still governs epoch 2
	h.expectTerms([3]uint64{0, 0, 100}, [3]uint64{3, 10, 10})
	h.settle(settleTime(4), 4) // settling epoch 3 drops record 0 without a change
	h.expectTerms([3]uint64{3, 10, 10})

	h.mustUpdate(settleTime(4), 20, 20)
	h.expectTerms([3]uint64{3, 10, 10}, [3]uint64{uint64(settleTime(4)) + 1, 20, 20})

	h.settle(settleTime(5), 5) // first unsettled epoch is now 6
	h.mustUpdate(settleTime(5), 30, 30)
	h.expectTerms([3]uint64{3, 10, 10}, [3]uint64{uint64(settleTime(4)) + 1, 20, 20}, [3]uint64{uint64(settleTime(5)) + 1, 30, 30})

	h.settle(settleTime(6), 6)
	want := map[uint64][2]uint8{
		1: {0, 100}, 2: {0, 100},
		3: {10, 10}, 4: {10, 10}, 5: {10, 10}, 6: {10, 10},
	}
	for e, split := range want {
		h.expectEpoch(e, split[0], split[1])
	}
	h.expectDeposits(want)
}

// Records are keyed by big-endian EffectiveEpoch, so they iterate in
// numeric order across byte boundaries, including when the records are
// split between a store and a snapshot layered over it, and lookups pick
// the highest EffectiveEpoch not above the queried epoch.
func TestPillarEpochTerms_OrderAcrossByteBoundaries(t *testing.T) {
	epochs := []uint64{65536, 0, 256, 1 << 32, 1, 255, 65535, 2, 1<<32 - 1}
	base := db.NewMemDB()
	for i, e := range epochs[:4] {
		common.FailIfErr(t, (&definition.PillarEpochTerms{Name: termsPillarName, EffectiveEpoch: e, GiveBlockRewardPercentage: uint8(i), GiveDelegateRewardPercentage: uint8(e % 101)}).Save(base))
	}
	// another pillar's records must not interleave
	common.FailIfErr(t, (&definition.PillarEpochTerms{Name: "other", EffectiveEpoch: 300, GiveBlockRewardPercentage: 99}).Save(base))
	storage := base.Snapshot()
	for i, e := range epochs[4:] {
		common.FailIfErr(t, (&definition.PillarEpochTerms{Name: termsPillarName, EffectiveEpoch: e, GiveBlockRewardPercentage: uint8(4 + i), GiveDelegateRewardPercentage: uint8(e % 101)}).Save(storage))
	}

	list, err := definition.GetPillarEpochTermsList(storage, termsPillarName)
	common.FailIfErr(t, err)
	wantOrder := []uint64{0, 1, 2, 255, 256, 65535, 65536, 1<<32 - 1, 1 << 32}
	if len(list) != len(wantOrder) {
		t.Fatalf("%d records, want %d", len(list), len(wantOrder))
	}
	for i, r := range list {
		if r.EffectiveEpoch != wantOrder[i] || r.Name != termsPillarName {
			t.Fatalf("record %d is %s@%d, want %s@%d", i, r.Name, r.EffectiveEpoch, termsPillarName, wantOrder[i])
		}
		if r.GiveDelegateRewardPercentage != uint8(r.EffectiveEpoch%101) {
			t.Fatalf("record %d: value %d does not belong to epoch %d", i, r.GiveDelegateRewardPercentage, r.EffectiveEpoch)
		}
	}

	for _, q := range []struct{ epoch, governing uint64 }{
		{0, 0}, {1, 1}, {3, 2},
		{254, 2}, {255, 255}, {256, 256}, {257, 256},
		{65534, 256}, {65535, 65535}, {65536, 65536}, {65537, 65536},
		{1<<32 - 2, 65536}, {1<<32 - 1, 1<<32 - 1}, {1 << 32, 1 << 32}, {1<<64 - 1, 1 << 32},
	} {
		r, err := definition.GetPillarEpochTermsForEpoch(storage, termsPillarName, q.epoch)
		common.FailIfErr(t, err)
		if r.EffectiveEpoch != q.governing {
			t.Fatalf("epoch %d governed by %d, want %d", q.epoch, r.EffectiveEpoch, q.governing)
		}
	}

	if _, err := definition.GetPillarEpochTermsForEpoch(storage, "missing", 5); err != constants.ErrDataNonExistent {
		t.Fatalf("pillar without records: err %v, want ErrDataNonExistent", err)
	}
	late := db.NewMemDB()
	common.FailIfErr(t, (&definition.PillarEpochTerms{Name: termsPillarName, EffectiveEpoch: 10}).Save(late))
	if _, err := definition.GetPillarEpochTermsForEpoch(late, termsPillarName, 9); err != constants.ErrDataNonExistent {
		t.Fatalf("epoch before the first record: err %v, want ErrDataNonExistent", err)
	}
}

// failingStorageContext routes Storage through failingDB, which fails
// Put or Delete on one chosen key.
type failingStorageContext struct {
	*testEpochBoundContext
	failPut    []byte
	failDelete []byte
}

var errInjectedWrite = errors.New("injected storage write failure")

type failingDB struct {
	db.DB
	failPut    []byte
	failDelete []byte
}

func (f *failingDB) Put(key, value []byte) error {
	if f.failPut != nil && string(key) == string(f.failPut) {
		return errInjectedWrite
	}
	return f.DB.Put(key, value)
}

func (f *failingDB) Delete(key []byte) error {
	if f.failDelete != nil && string(key) == string(f.failDelete) {
		return errInjectedWrite
	}
	return f.DB.Delete(key)
}

func (ctx *failingStorageContext) Storage() db.DB {
	return &failingDB{DB: ctx.testEpochBoundContext.Storage(), failPut: ctx.failPut, failDelete: ctx.failDelete}
}

// A failing write of the baseline record, the new record, a pruned
// record's delete or the epoch history is returned as an error instead
// of being ignored or panicking, so the VM can roll the receive back
// (see vm/pillar_epoch_terms_rollback_test.go for the rollback itself).
func TestEpochRewardTerms_WriteFailuresAreReturned(t *testing.T) {
	t.Run("baseline", func(t *testing.T) {
		h := newTermsHarness(t, true, 0, 100, 1)
		fc := &failingStorageContext{testEpochBoundContext: h.ctx, failPut: definition.GetPillarEpochTermsKey(termsPillarName, 0)}
		h.ctx.momentum = &nom.Momentum{Timestamp: timePtr(2)}
		expectInjected(t, updateWith(fc, 0, 0))
	})
	t.Run("new terms", func(t *testing.T) {
		h := newTermsHarness(t, true, 0, 100, 1)
		fc := &failingStorageContext{testEpochBoundContext: h.ctx, failPut: definition.GetPillarEpochTermsKey(termsPillarName, 3)}
		h.ctx.momentum = &nom.Momentum{Timestamp: timePtr(2)}
		expectInjected(t, updateWith(fc, 0, 0))
	})
	t.Run("update prune delete", func(t *testing.T) {
		// settling epoch 2 keeps record 0 (it governs epoch 2); the
		// change then prunes it because record 3 governs epoch 3
		h := newTermsHarness(t, true, 0, 100, 2)
		h.mustUpdate(2, 10, 10)
		h.settle(settleTime(2), 2)
		h.expectTerms([3]uint64{0, 0, 100}, [3]uint64{3, 10, 10})
		fc := &failingStorageContext{testEpochBoundContext: h.ctx, failDelete: definition.GetPillarEpochTermsKey(termsPillarName, 0)}
		expectInjected(t, updateWith(fc, 20, 20))
	})
	t.Run("settlement prune delete", func(t *testing.T) {
		h := newTermsHarness(t, true, 0, 100, 3)
		h.mustUpdate(2, 10, 10)
		fc := &failingStorageContext{testEpochBoundContext: h.ctx, failDelete: definition.GetPillarEpochTermsKey(termsPillarName, 0)}
		h.setFrontier(settleTime(3))
		expectInjected(t, updatePillarRewards(fc))
	})
	t.Run("epoch history", func(t *testing.T) {
		h := newTermsHarness(t, true, 0, 100, 1)
		h.mustUpdate(1, 10, 10)
		fc := &failingStorageContext{testEpochBoundContext: h.ctx, failPut: epochHistoryKey(t, 1)}
		h.setFrontier(settleTime(1))
		expectInjected(t, updatePillarRewards(fc))
	})
}

func timePtr(unix int64) *time.Time {
	ts := time.Unix(unix, 0)
	return &ts
}

func updateWith(ctx *failingStorageContext, giveBlock, giveDelegate uint8) error {
	method := &UpdatePillarMethod{MethodName: definition.UpdatePillarMethodName}
	_, err := method.ReceiveBlock(ctx, &nom.AccountBlock{
		Address:   termsOwner,
		ToAddress: types.PillarContract,
		BlockType: nom.BlockTypeUserSend,
		Amount:    big.NewInt(0),
		Data: definition.ABIPillars.PackMethodPanic(definition.UpdatePillarMethodName,
			termsPillarName, termsOwner, termsRewardAddr, giveBlock, giveDelegate),
	})
	return err
}

func expectInjected(t *testing.T, err error) {
	t.Helper()
	if !errors.Is(err, errInjectedWrite) {
		t.Fatalf("err %v, want the injected write failure", err)
	}
}

// epochHistoryKey finds the storage key of the epoch's history entry by
// saving one into a scratch store, since the key layout is unexported.
func epochHistoryKey(t *testing.T, epoch uint64) []byte {
	t.Helper()
	scratch := db.NewMemDB()
	common.FailIfErr(t, (&definition.PillarEpochHistory{Name: termsPillarName, Epoch: epoch, Weight: big.NewInt(0)}).Save(scratch))
	it := scratch.NewIterator(nil)
	defer it.Release()
	if !it.Next() {
		t.Fatal("no history key")
	}
	return append([]byte(nil), it.Key()...)
}
