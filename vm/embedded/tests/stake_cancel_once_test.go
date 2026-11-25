package tests

import (
	"math/big"
	"testing"
	"time"

	g "github.com/zenon-network/go-zenon/chain/genesis/mock"
	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/rpc/api"
	"github.com/zenon-network/go-zenon/rpc/api/embedded"
	"github.com/zenon-network/go-zenon/vm/constants"
	"github.com/zenon-network/go-zenon/vm/embedded/definition"
	"github.com/zenon-network/go-zenon/zenon/mock"
)

// Momentum timestamps on the mock chain: height h is at
// genesisTimestamp + 10*(h-1); with a one-hour epoch, epoch k starts at
// height 360*k + 1.
const mockGenesisTimestamp = 1000000000

func heightAt(timestamp int64) uint64 {
	return uint64((timestamp-mockGenesisTimestamp)/10) + 1
}

func stakeSend(user types.Address, amount int64) *nom.AccountBlock {
	return &nom.AccountBlock{
		Address:       user,
		ToAddress:     types.StakeContract,
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(amount),
		Data:          definition.ABIStake.PackMethodPanic(definition.StakeMethodName, constants.StakeTimeMinSec),
	}
}

func cancelStakeSend(user types.Address, id types.Hash) *nom.AccountBlock {
	return &nom.AccountBlock{
		Address:   user,
		ToAddress: types.StakeContract,
		Data:      definition.ABIStake.PackMethodPanic(definition.CancelStakeMethodName, id),
	}
}

// storedStake reads the stake entry from the stake contract's frontier
// storage, including a revoked entry, which the RPC list excludes.
func storedStake(t *testing.T, z mock.MockZenon, id types.Hash, owner types.Address) (*definition.StakeInfo, error) {
	_, context, err := api.GetFrontierContext(z.Chain(), types.StakeContract)
	common.FailIfErr(t, err)
	return definition.GetStakeInfo(context.Storage(), id, owner)
}

func storedLiquidityStake(t *testing.T, z mock.MockZenon, id types.Hash, owner types.Address) (*definition.LiquidityStakeEntry, error) {
	_, context, err := api.GetFrontierContext(z.Chain(), types.LiquidityContract)
	common.FailIfErr(t, err)
	return definition.GetLiquidityStakeEntry(context.Storage(), id, owner)
}

// stakeAndExpire stakes 10 ZNN from User1 and advances past the minimum
// stake time. It returns the entry id.
func stakeAndExpire(t *testing.T, z mock.MockZenon) types.Hash {
	defer z.CallContract(stakeSend(g.User1.Address, 10*g.Zexp)).Error(t, nil)
	z.InsertNewMomentum()
	z.InsertNewMomentum()
	list, err := embedded.NewStakeApi(z).GetEntriesByAddress(g.User1.Address, 0, 10)
	common.FailIfErr(t, err)
	if len(list.Entries) != 1 {
		t.Fatalf("expected one stake entry, got %d", len(list.Entries))
	}
	z.InsertMomentumsTo(heightAt(list.Entries[0].ExpirationTimestamp) + 10)
	return list.Entries[0].Id
}

// Before the spork a second cancel is accepted, refunds nothing and
// updates RevokeTime, preserving historical replay semantics.
func TestStakeCancelTwice_BeforeSporkMovesRevokeTime(t *testing.T) {
	z := mock.NewMockZenonWithCustomEpochDuration(t, time.Hour)
	defer z.StopPanic()

	id := stakeAndExpire(t, z)

	defer z.CallContract(cancelStakeSend(g.User1.Address, id)).Error(t, nil)
	z.InsertNewMomentum()
	z.InsertNewMomentum()
	first, err := storedStake(t, z, id, g.User1.Address)
	common.FailIfErr(t, err)
	if first.RevokeTime == 0 || first.Amount.Sign() != 0 {
		t.Fatalf("first cancel: revoke time %d amount %s", first.RevokeTime, first.Amount)
	}

	z.InsertMomentumsTo(heightAt(first.RevokeTime) + 30)
	defer z.CallContract(cancelStakeSend(g.User1.Address, id)).Error(t, nil)
	z.InsertNewMomentum()
	z.InsertNewMomentum()
	second, err := storedStake(t, z, id, g.User1.Address)
	common.FailIfErr(t, err)
	if second.RevokeTime <= first.RevokeTime {
		t.Fatalf("pre-spork second cancel: revoke time %d, want later than %d", second.RevokeTime, first.RevokeTime)
	}
}

// From the spork a second cancel fails with ErrStakeAlreadyRevoked,
// leaves RevokeTime unchanged, and the entry is deleted when its
// epoch settles.
func TestStakeCancelTwice_AfterSporkRejected(t *testing.T) {
	z := mock.NewMockZenonWithCustomEpochDuration(t, time.Hour)
	defer z.StopPanic()
	activateDynamicPlasma(t, z)

	id := stakeAndExpire(t, z)

	defer z.CallContract(cancelStakeSend(g.User1.Address, id)).Error(t, nil)
	z.InsertNewMomentum()
	z.InsertNewMomentum()
	first, err := storedStake(t, z, id, g.User1.Address)
	common.FailIfErr(t, err)
	if first.RevokeTime == 0 {
		t.Fatal("first cancel did not revoke the entry")
	}
	autoreceive(t, z, g.User1.Address)
	z.InsertNewMomentum()
	refunded, err := z.Chain().GetFrontierAccountStore(g.User1.Address).GetBalance(types.ZnnTokenStandard)
	common.FailIfErr(t, err)

	z.InsertMomentumsTo(heightAt(first.RevokeTime) + 30)
	defer z.CallContract(cancelStakeSend(g.User1.Address, id)).Error(t, constants.ErrStakeAlreadyRevoked)
	z.InsertNewMomentum()
	z.InsertNewMomentum()
	second, err := storedStake(t, z, id, g.User1.Address)
	common.FailIfErr(t, err)
	if second.RevokeTime != first.RevokeTime {
		t.Fatalf("post-spork second cancel moved revoke time: %d, want %d", second.RevokeTime, first.RevokeTime)
	}
	autoreceive(t, z, g.User1.Address)
	z.InsertNewMomentum()
	after, err := z.Chain().GetFrontierAccountStore(g.User1.Address).GetBalance(types.ZnnTokenStandard)
	common.FailIfErr(t, err)
	if after.Cmp(refunded) != 0 {
		t.Fatalf("second cancel changed the ZNN balance: %s, want %s", after, refunded)
	}

	// The epoch containing the revoke time is settled within two epochs.
	z.InsertMomentumsTo(heightAt(first.RevokeTime) + 2*360 + 30)
	if _, err := storedStake(t, z, id, g.User1.Address); err != constants.ErrDataNonExistent {
		t.Fatalf("entry after its epoch's update: err %v, want ErrDataNonExistent", err)
	}
}

// An entry revoked before DynamicPlasmaSpork activates still cannot be
// cancelled again once the spork is enforced.
func TestStakeCancelBeforeSpork_RepeatAfterSporkRejected(t *testing.T) {
	z := mock.NewMockZenonWithCustomEpochDuration(t, time.Hour)
	defer z.StopPanic()

	id := stakeAndExpire(t, z)

	defer z.CallContract(cancelStakeSend(g.User1.Address, id)).Error(t, nil)
	z.InsertNewMomentum()
	z.InsertNewMomentum()
	first, err := storedStake(t, z, id, g.User1.Address)
	common.FailIfErr(t, err)
	if first.RevokeTime == 0 {
		t.Fatal("first cancel did not revoke the entry")
	}

	activateDynamicPlasmaPastHeight(t, z)

	defer z.CallContract(cancelStakeSend(g.User1.Address, id)).Error(t, constants.ErrStakeAlreadyRevoked)
	z.InsertNewMomentum()
	z.InsertNewMomentum()
	second, err := storedStake(t, z, id, g.User1.Address)
	common.FailIfErr(t, err)
	if second.RevokeTime != first.RevokeTime {
		t.Fatalf("post-spork repeat of pre-spork cancel moved revoke time: %d, want %d", second.RevokeTime, first.RevokeTime)
	}
}

// zeroWeightBoundary is the start of epoch 5. The contracts' periodic
// update runs every 361 momentums (heights 361, 722, ...), drifting one
// momentum per epoch from the epoch grid; at epoch 2 it lands on the
// momentum right after the boundary and competes with the cancel receive,
// at epoch 5 it is four momentums away.
const zeroWeightBoundary = int64(mockGenesisTimestamp + 5*3600)

// cancelAtEpochBoundary cancels the entry so that the receive runs on the
// first momentum of epoch 5: the entry then carries full weight in epoch 4
// (RevokeTime == endTime, so not deleted there) and zero weight in every
// later epoch, and if it is the only stake entry, later epochs have zero
// cumulated weight. The contract receive is generated against the frontier
// one momentum after the send's, so the send goes in the momentum before
// the boundary.
func cancelAtEpochBoundary(t *testing.T, z mock.MockZenon, id types.Hash) int64 {
	boundary := zeroWeightBoundary
	z.InsertMomentumsTo(heightAt(boundary) - 1)
	defer z.CallContract(cancelStakeSend(g.User1.Address, id)).Error(t, nil)
	z.InsertNewMomentum()
	z.InsertNewMomentum()
	info, err := storedStake(t, z, id, g.User1.Address)
	common.FailIfErr(t, err)
	if info.RevokeTime != boundary {
		t.Fatalf("revoke time %d, want the epoch boundary %d (fixture timing changed)", info.RevokeTime, boundary)
	}
	return boundary
}

// Before the spork an entry whose RevokeTime falls on an epoch boundary,
// with no other weighted stake, is never deleted: the epoch's update
// returns on zero cumulated weight before the deletion loop.
func TestStakeZeroWeightEpoch_BeforeSporkKeepsRevokedEntry(t *testing.T) {
	z := mock.NewMockZenonWithCustomEpochDuration(t, time.Hour)
	defer z.StopPanic()

	id := stakeAndExpireBefore(t, z, zeroWeightBoundary)
	boundary := cancelAtEpochBoundary(t, z, id)

	z.InsertMomentumsTo(heightAt(boundary) + 3*360)
	if _, err := storedStake(t, z, id, g.User1.Address); err != nil {
		t.Fatalf("pre-spork: entry should be retained, got err %v", err)
	}
}

// From the spork revoked entries with RevokeTime < endTime are deleted at
// the zero-weight epoch's update, while entries revoked at endTime or still
// active are preserved.
func TestStakeZeroWeightEpoch_AfterSporkDeletesRevokedEntry(t *testing.T) {
	z := mock.NewMockZenonWithCustomEpochDuration(t, time.Hour)
	defer z.StopPanic()
	activateDynamicPlasma(t, z)

	// Two entries revoked on the epoch 5 boundary verify multi-entry deletion
	// inside IterateStakeEntries on the zero-weight path.
	id1 := stakeAndExpireBefore(t, z, zeroWeightBoundary)
	defer z.CallContract(stakeSend(g.User1.Address, 10*g.Zexp)).Error(t, nil)
	z.InsertNewMomentum()
	z.InsertNewMomentum()
	list, err := embedded.NewStakeApi(z).GetEntriesByAddress(g.User1.Address, 0, 10)
	common.FailIfErr(t, err)
	if len(list.Entries) != 2 {
		t.Fatalf("expected two stake entries, got %d", len(list.Entries))
	}
	var id2 types.Hash
	for _, e := range list.Entries {
		if e.Id != id1 {
			id2 = e.Id
		}
	}

	boundary := zeroWeightBoundary
	z.InsertMomentumsTo(heightAt(boundary) - 1)
	defer z.CallContract(cancelStakeSend(g.User1.Address, id1)).Error(t, nil)
	defer z.CallContract(cancelStakeSend(g.User1.Address, id2)).Error(t, nil)
	z.InsertNewMomentum()
	z.InsertNewMomentum()

	for _, id := range []types.Hash{id1, id2} {
		info, err := storedStake(t, z, id, g.User1.Address)
		common.FailIfErr(t, err)
		if info.RevokeTime != boundary {
			t.Fatalf("entry %s revoke time %d, want epoch boundary %d", id, info.RevokeTime, boundary)
		}
	}

	// After epoch 4 settles (height 5*361 = 1805), RevokeTime == epoch4.endTime,
	// so both entries must still exist.
	z.InsertMomentumsTo(heightAt(boundary) + 20)
	for _, id := range []types.Hash{id1, id2} {
		if _, err := storedStake(t, z, id, g.User1.Address); err != nil {
			t.Fatalf("entry %s deleted prematurely at epoch 4 boundary: %v", id, err)
		}
	}

	// Create an active entry after epoch 5 ends (in epoch 6), right before
	// epoch 5 settles at height 6*361 = 2166, so epoch 5 still has zero weight
	// while an active entry exists in storage.
	z.InsertMomentumsTo(heightAt(boundary) + 360 + 1)
	defer z.CallContract(stakeSend(g.User1.Address, 10*g.Zexp)).Error(t, nil)
	z.InsertNewMomentum()
	z.InsertNewMomentum()
	activeList, err := embedded.NewStakeApi(z).GetEntriesByAddress(g.User1.Address, 0, 10)
	common.FailIfErr(t, err)
	if len(activeList.Entries) != 1 {
		t.Fatalf("expected one active stake entry, got %d", len(activeList.Entries))
	}
	activeId := activeList.Entries[0].Id

	// Advance past epoch 5's update; both revoked entries must be deleted and
	// the active entry must remain.
	z.InsertMomentumsTo(heightAt(boundary) + 360 + 30)
	for _, id := range []types.Hash{id1, id2} {
		if _, err := storedStake(t, z, id, g.User1.Address); err != constants.ErrDataNonExistent {
			t.Fatalf("post-spork: entry %s after zero-weight epoch update: err %v, want ErrDataNonExistent", id, err)
		}
	}
	activeInfo, err := storedStake(t, z, activeId, g.User1.Address)
	common.FailIfErr(t, err)
	if activeInfo.RevokeTime != 0 {
		t.Fatalf("active entry unexpectedly mutated: revoke time %d", activeInfo.RevokeTime)
	}
}

// stakeAndExpireBefore stakes and checks the entry expires before the
// given timestamp, without advancing the chain past it.
func stakeAndExpireBefore(t *testing.T, z mock.MockZenon, deadline int64) types.Hash {
	defer z.CallContract(stakeSend(g.User1.Address, 10*g.Zexp)).Error(t, nil)
	z.InsertNewMomentum()
	z.InsertNewMomentum()
	list, err := embedded.NewStakeApi(z).GetEntriesByAddress(g.User1.Address, 0, 10)
	common.FailIfErr(t, err)
	if len(list.Entries) != 1 {
		t.Fatalf("expected one stake entry, got %d", len(list.Entries))
	}
	if list.Entries[0].ExpirationTimestamp >= deadline {
		t.Fatalf("entry expires at %d, after %d", list.Entries[0].ExpirationTimestamp, deadline)
	}
	return list.Entries[0].Id
}

// liquidityCancelFixture builds the two-token liquidity fixture, unlocks
// User1's entries and cancels the first one. It returns the entry id.
func liquidityCancelFixture(t *testing.T, z mock.MockZenon, sporkActive bool) types.Hash {
	activateLiquidityStep4(t, z)
	if sporkActive {
		activateDynamicPlasmaPastHeight(t, z)
	}
	entries, err := embedded.NewLiquidityApi(z).GetLiquidityStakeEntriesByAddress(g.User1.Address, 0, 5)
	common.FailIfErr(t, err)
	id := entries.Entries[0].Id

	defer z.CallContract(unlockLiquidityEntries(g.User5.Address, tokens[0])).Error(t, nil)
	insertMomentums(z, 2)
	defer z.CallContract(cancelStakeLiq(g.User1.Address, id)).Error(t, nil)
	insertMomentums(z, 2)
	first, err := storedLiquidityStake(t, z, id, g.User1.Address)
	common.FailIfErr(t, err)
	if first.RevokeTime == 0 {
		t.Fatal("first liquidity cancel did not revoke the entry")
	}
	return id
}

func TestLiquidityCancelTwice_BeforeSporkMovesRevokeTime(t *testing.T) {
	z := mock.NewMockZenonWithCustomEpochDuration(t, time.Hour)
	defer z.StopPanic()
	id := liquidityCancelFixture(t, z, false)
	first, err := storedLiquidityStake(t, z, id, g.User1.Address)
	common.FailIfErr(t, err)

	insertMomentums(z, 3)
	defer z.CallContract(cancelStakeLiq(g.User1.Address, id)).Error(t, nil)
	insertMomentums(z, 2)
	second, err := storedLiquidityStake(t, z, id, g.User1.Address)
	common.FailIfErr(t, err)
	if second.RevokeTime <= first.RevokeTime {
		t.Fatalf("pre-spork second liquidity cancel: revoke time %d, want later than %d", second.RevokeTime, first.RevokeTime)
	}
}

func TestLiquidityCancelTwice_AfterSporkRejected(t *testing.T) {
	z := mock.NewMockZenonWithCustomEpochDuration(t, time.Hour)
	defer z.StopPanic()
	id := liquidityCancelFixture(t, z, true)
	first, err := storedLiquidityStake(t, z, id, g.User1.Address)
	common.FailIfErr(t, err)

	autoreceive(t, z, g.User1.Address)
	insertMomentums(z, 1)
	refunded, err := z.Chain().GetFrontierAccountStore(g.User1.Address).GetBalance(first.TokenStandard)
	common.FailIfErr(t, err)

	insertMomentums(z, 3)
	defer z.CallContract(cancelStakeLiq(g.User1.Address, id)).Error(t, constants.ErrStakeAlreadyRevoked)
	insertMomentums(z, 2)
	second, err := storedLiquidityStake(t, z, id, g.User1.Address)
	common.FailIfErr(t, err)
	if second.RevokeTime != first.RevokeTime {
		t.Fatalf("post-spork second liquidity cancel moved revoke time: %d, want %d", second.RevokeTime, first.RevokeTime)
	}

	autoreceive(t, z, g.User1.Address)
	insertMomentums(z, 1)
	after, err := z.Chain().GetFrontierAccountStore(g.User1.Address).GetBalance(first.TokenStandard)
	common.FailIfErr(t, err)
	if after.Cmp(refunded) != 0 {
		t.Fatalf("second liquidity cancel changed the LP token balance: %s, want %s", after, refunded)
	}

	z.InsertMomentumsTo(heightAt(first.RevokeTime) + 2*360 + 30)
	if _, err := storedLiquidityStake(t, z, id, g.User1.Address); err != constants.ErrDataNonExistent {
		t.Fatalf("liquidity entry after its epoch's update: err %v, want ErrDataNonExistent", err)
	}
}

// A liquidity stake revoked before DynamicPlasmaSpork activates still
// cannot be cancelled again once the spork is enforced.
func TestLiquidityCancelBeforeSpork_RepeatAfterSporkRejected(t *testing.T) {
	z := mock.NewMockZenonWithCustomEpochDuration(t, time.Hour)
	defer z.StopPanic()
	id := liquidityCancelFixture(t, z, false)
	first, err := storedLiquidityStake(t, z, id, g.User1.Address)
	common.FailIfErr(t, err)

	activateDynamicPlasmaPastHeight(t, z)

	defer z.CallContract(cancelStakeLiq(g.User1.Address, id)).Error(t, constants.ErrStakeAlreadyRevoked)
	insertMomentums(z, 2)
	second, err := storedLiquidityStake(t, z, id, g.User1.Address)
	common.FailIfErr(t, err)
	if second.RevokeTime != first.RevokeTime {
		t.Fatalf("post-spork repeat of pre-spork liquidity cancel moved revoke time: %d, want %d", second.RevokeTime, first.RevokeTime)
	}
}

// removedTupleCancelFixture removes tokens[0] from the reward tuples
// (SetTokenTuple is behind a one-epoch time challenge, so this completes
// before the cancel), then unlocks and cancels User1's tokens[0] entry.
// From then on the reward loop skips that entry before its deletion.
func removedTupleCancelFixture(t *testing.T, z mock.MockZenon, sporkActive bool) types.Hash {
	activateLiquidityStep4(t, z)
	if sporkActive {
		activateDynamicPlasmaPastHeight(t, z)
	}
	entries, err := embedded.NewLiquidityApi(z).GetLiquidityStakeEntriesByAddress(g.User1.Address, 0, 5)
	common.FailIfErr(t, err)
	var id types.Hash
	for _, e := range entries.Entries {
		if e.TokenStandard == tokens[0] {
			id = e.Id
		}
	}

	setTokensTuple(t, z, g.User5.Address,
		[]string{tokens[1].String()},
		[]uint32{constants.LiquidityZnnTotalPercentages},
		[]uint32{constants.LiquidityQsrTotalPercentages},
		[]*big.Int{big.NewInt(2000)})
	info, err := embedded.NewLiquidityApi(z).GetLiquidityInfo()
	common.FailIfErr(t, err)
	if len(info.TokenTuples) != 1 || info.TokenTuples[0].TokenStandard != tokens[1].String() {
		t.Fatalf("tuples after removal: %+v", info.TokenTuples)
	}

	defer z.CallContract(unlockLiquidityEntries(g.User5.Address, tokens[0])).Error(t, nil)
	insertMomentums(z, 2)
	defer z.CallContract(cancelStakeLiq(g.User1.Address, id)).Error(t, nil)
	insertMomentums(z, 2)
	first, err := storedLiquidityStake(t, z, id, g.User1.Address)
	common.FailIfErr(t, err)
	if first.RevokeTime == 0 {
		t.Fatal("cancel did not revoke the entry")
	}
	return id
}

// A revoked entry whose token has left the reward tuples is skipped by
// the reward loop before its deletion. Before the spork it stays; from
// the spork it is deleted at the next update.
func TestLiquidityRemovedTuple_BeforeSporkKeepsRevokedEntry(t *testing.T) {
	z := mock.NewMockZenonWithCustomEpochDuration(t, time.Hour)
	defer z.StopPanic()
	id := removedTupleCancelFixture(t, z, false)

	insertMomentums(z, 3*360)
	if _, err := storedLiquidityStake(t, z, id, g.User1.Address); err != nil {
		t.Fatalf("pre-spork: entry of a removed token should be retained, got err %v", err)
	}
}

func TestLiquidityRemovedTuple_AfterSporkDeletesRevokedEntry(t *testing.T) {
	z := mock.NewMockZenonWithCustomEpochDuration(t, time.Hour)
	defer z.StopPanic()
	activateLiquidityStep4(t, z)
	activateDynamicPlasmaPastHeight(t, z)

	// Stake a second tokens[0] entry that stays active after the tuple is
	// removed, to verify that only revoked entries are deleted on the skip path.
	defer z.CallContract(liquidityStake(g.User1.Address, tokens[0], big.NewInt(10*g.Zexp), 2)).Error(t, nil)
	insertMomentums(z, 2)

	entries, err := embedded.NewLiquidityApi(z).GetLiquidityStakeEntriesByAddress(g.User1.Address, 0, 10)
	common.FailIfErr(t, err)
	var revokedID, activeID types.Hash
	for _, e := range entries.Entries {
		if e.TokenStandard != tokens[0] {
			continue
		}
		if revokedID == types.ZeroHash {
			revokedID = e.Id
		} else {
			activeID = e.Id
		}
	}
	if revokedID == types.ZeroHash || activeID == types.ZeroHash {
		t.Fatalf("expected two tokens[0] entries, got %+v", entries.Entries)
	}

	setTokensTuple(t, z, g.User5.Address,
		[]string{tokens[1].String()},
		[]uint32{constants.LiquidityZnnTotalPercentages},
		[]uint32{constants.LiquidityQsrTotalPercentages},
		[]*big.Int{big.NewInt(2000)})

	defer z.CallContract(unlockLiquidityEntries(g.User5.Address, tokens[0])).Error(t, nil)
	insertMomentums(z, 2)

	// Cancel exactly on the epoch 5 boundary (RevokeTime == epoch4.endTime)
	// while tokens[0] is absent from TokenTuples, so epoch 4's settlement hits
	// the removed-token skip branch with RevokeTime == endTime.
	z.InsertMomentumsTo(heightAt(zeroWeightBoundary) - 1)
	defer z.CallContract(cancelStakeLiq(g.User1.Address, revokedID)).Error(t, nil)
	insertMomentums(z, 2)
	revokedInfo, err := storedLiquidityStake(t, z, revokedID, g.User1.Address)
	common.FailIfErr(t, err)
	if revokedInfo.RevokeTime != zeroWeightBoundary {
		t.Fatalf("revoke time %d, want epoch boundary %d (fixture timing changed)", revokedInfo.RevokeTime, zeroWeightBoundary)
	}

	// After epoch 4 settles, RevokeTime == epoch4.endTime so the skipped-token
	// branch must keep the entry until epoch 5 settles.
	z.InsertMomentumsTo(heightAt(zeroWeightBoundary) + 20)
	if _, err := storedLiquidityStake(t, z, revokedID, g.User1.Address); err != nil {
		t.Fatalf("post-spork: removed-token entry deleted prematurely at epoch 4 boundary: %v", err)
	}

	z.InsertMomentumsTo(heightAt(zeroWeightBoundary) + 2*360)
	if _, err := storedLiquidityStake(t, z, revokedID, g.User1.Address); err != constants.ErrDataNonExistent {
		t.Fatalf("post-spork: revoked entry of a removed token after update: err %v, want ErrDataNonExistent", err)
	}
	activeInfo, err := storedLiquidityStake(t, z, activeID, g.User1.Address)
	common.FailIfErr(t, err)
	if activeInfo.RevokeTime != 0 {
		t.Fatalf("post-spork: active entry of a removed token should stay unrevoked, got revoke time %d", activeInfo.RevokeTime)
	}
}

// Two Cancel sends for the same entry received in one momentum (e.g. a
// wallet double-send): before the spork both succeed; after it the second
// fails. In both cases the principal is refunded exactly once and the
// contract keeps receiving.
func stakeSameMomentumDoubleCancel(t *testing.T, sporkActive bool) {
	z := mock.NewMockZenonWithCustomEpochDuration(t, time.Hour)
	defer z.StopPanic()
	if sporkActive {
		activateDynamicPlasma(t, z)
	}
	id := stakeAndExpire(t, z)
	autoreceive(t, z, g.User1.Address)
	z.InsertNewMomentum()
	start, err := z.Chain().GetFrontierAccountStore(g.User1.Address).GetBalance(types.ZnnTokenStandard)
	common.FailIfErr(t, err)

	secondErr := error(nil)
	if sporkActive {
		secondErr = constants.ErrStakeAlreadyRevoked
	}
	defer z.CallContract(cancelStakeSend(g.User1.Address, id)).Error(t, nil)
	defer z.CallContract(cancelStakeSend(g.User1.Address, id)).Error(t, secondErr)
	z.InsertNewMomentum()
	z.InsertNewMomentum()
	autoreceive(t, z, g.User1.Address)
	z.InsertNewMomentum()
	end, err := z.Chain().GetFrontierAccountStore(g.User1.Address).GetBalance(types.ZnnTokenStandard)
	common.FailIfErr(t, err)
	if got, want := new(big.Int).Sub(end, start), big.NewInt(10*g.Zexp); got.Cmp(want) != 0 {
		t.Fatalf("refunded %s, want exactly one principal %s", got, want)
	}

	// The contract still processes calls after the failed duplicate.
	defer z.CallContract(stakeSend(g.User1.Address, 10*g.Zexp)).Error(t, nil)
	z.InsertNewMomentum()
	z.InsertNewMomentum()
}

func TestStakeSameMomentumDoubleCancel_BeforeSpork(t *testing.T) {
	stakeSameMomentumDoubleCancel(t, false)
}

func TestStakeSameMomentumDoubleCancel_AfterSpork(t *testing.T) {
	stakeSameMomentumDoubleCancel(t, true)
}

func liquiditySameMomentumDoubleCancel(t *testing.T, sporkActive bool) {
	z := mock.NewMockZenonWithCustomEpochDuration(t, time.Hour)
	defer z.StopPanic()
	activateLiquidityStep4(t, z)
	if sporkActive {
		activateDynamicPlasmaPastHeight(t, z)
	}
	entries, err := embedded.NewLiquidityApi(z).GetLiquidityStakeEntriesByAddress(g.User1.Address, 0, 5)
	common.FailIfErr(t, err)
	entry := entries.Entries[0]
	defer z.CallContract(unlockLiquidityEntries(g.User5.Address, entry.TokenStandard)).Error(t, nil)
	insertMomentums(z, 2)
	start, err := z.Chain().GetFrontierAccountStore(g.User1.Address).GetBalance(entry.TokenStandard)
	common.FailIfErr(t, err)

	secondErr := error(nil)
	if sporkActive {
		secondErr = constants.ErrStakeAlreadyRevoked
	}
	defer z.CallContract(cancelStakeLiq(g.User1.Address, entry.Id)).Error(t, nil)
	defer z.CallContract(cancelStakeLiq(g.User1.Address, entry.Id)).Error(t, secondErr)
	insertMomentums(z, 2)
	autoreceive(t, z, g.User1.Address)
	insertMomentums(z, 1)
	end, err := z.Chain().GetFrontierAccountStore(g.User1.Address).GetBalance(entry.TokenStandard)
	common.FailIfErr(t, err)
	if got := new(big.Int).Sub(end, start); got.Cmp(entry.Amount) != 0 {
		t.Fatalf("refunded %s, want exactly one principal %s", got, entry.Amount)
	}

	// The contract still processes calls after the failed duplicate.
	defer z.CallContract(liquidityStake(g.User1.Address, tokens[1], big.NewInt(50*g.Zexp), 12)).Error(t, nil)
	insertMomentums(z, 2)
}

func TestLiquiditySameMomentumDoubleCancel_BeforeSpork(t *testing.T) {
	liquiditySameMomentumDoubleCancel(t, false)
}

func TestLiquiditySameMomentumDoubleCancel_AfterSpork(t *testing.T) {
	liquiditySameMomentumDoubleCancel(t, true)
}

// liquidityZeroWeightFixture cancels User1's tokens[0] entry, the only
// entry of that configured token, so that RevokeTime lands exactly on the
// epoch 5 boundary: the token then has zero cumulated weight in epoch 5
// and later, and the reward loop skips the entry before its deletion.
func liquidityZeroWeightFixture(t *testing.T, z mock.MockZenon, sporkActive bool) types.Hash {
	activateLiquidityStep4(t, z)
	if sporkActive {
		activateDynamicPlasmaPastHeight(t, z)
	}
	entries, err := embedded.NewLiquidityApi(z).GetLiquidityStakeEntriesByAddress(g.User1.Address, 0, 5)
	common.FailIfErr(t, err)
	var id types.Hash
	count := 0
	for _, e := range entries.Entries {
		if e.TokenStandard == tokens[0] {
			id = e.Id
			count++
		}
	}
	if count != 1 {
		t.Fatalf("fixture has %d tokens[0] entries for User1, want 1", count)
	}
	defer z.CallContract(unlockLiquidityEntries(g.User5.Address, tokens[0])).Error(t, nil)
	insertMomentums(z, 2)

	z.InsertMomentumsTo(heightAt(zeroWeightBoundary) - 1)
	defer z.CallContract(cancelStakeLiq(g.User1.Address, id)).Error(t, nil)
	insertMomentums(z, 2)
	info, err := storedLiquidityStake(t, z, id, g.User1.Address)
	common.FailIfErr(t, err)
	if info.RevokeTime != zeroWeightBoundary {
		t.Fatalf("revoke time %d, want the epoch boundary %d (fixture timing changed)", info.RevokeTime, zeroWeightBoundary)
	}
	return id
}

func TestLiquidityZeroWeightToken_BeforeSporkKeepsRevokedEntry(t *testing.T) {
	z := mock.NewMockZenonWithCustomEpochDuration(t, time.Hour)
	defer z.StopPanic()
	id := liquidityZeroWeightFixture(t, z, false)

	z.InsertMomentumsTo(heightAt(zeroWeightBoundary) + 3*360)
	if _, err := storedLiquidityStake(t, z, id, g.User1.Address); err != nil {
		t.Fatalf("pre-spork: zero-weight entry should be retained, got err %v", err)
	}
}

func TestLiquidityZeroWeightToken_AfterSporkDeletesRevokedEntry(t *testing.T) {
	z := mock.NewMockZenonWithCustomEpochDuration(t, time.Hour)
	defer z.StopPanic()
	id := liquidityZeroWeightFixture(t, z, true)

	// After epoch 4 settles, RevokeTime == epoch4.endTime so entry stays.
	z.InsertMomentumsTo(heightAt(zeroWeightBoundary) + 20)
	if _, err := storedLiquidityStake(t, z, id, g.User1.Address); err != nil {
		t.Fatalf("post-spork: entry deleted prematurely at epoch 4 boundary: %v", err)
	}

	z.InsertMomentumsTo(heightAt(zeroWeightBoundary) + 2*360)
	if _, err := storedLiquidityStake(t, z, id, g.User1.Address); err != constants.ErrDataNonExistent {
		t.Fatalf("post-spork: zero-weight entry after the update: err %v, want ErrDataNonExistent", err)
	}
}

// While the liquidity contract is halted its reward update returns before
// the loop, so a revoked entry is kept, before and after the spork alike:
// this change leaves the halted path as it is.
func TestLiquidityHalted_AfterSporkKeepsRevokedEntry(t *testing.T) {
	z := mock.NewMockZenonWithCustomEpochDuration(t, time.Hour)
	defer z.StopPanic()
	id := liquidityCancelFixture(t, z, true)
	defer z.CallContract(setIsHalted(g.User5.Address, true)).Error(t, nil)
	insertMomentums(z, 2)

	insertMomentums(z, 3*360)
	if _, err := storedLiquidityStake(t, z, id, g.User1.Address); err != nil {
		t.Fatalf("halted: revoked entry should be retained, got err %v", err)
	}
}
