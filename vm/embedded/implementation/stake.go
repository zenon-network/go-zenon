package implementation

import (
	"math/big"

	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/vm/constants"
	"github.com/zenon-network/go-zenon/vm/embedded/definition"
	"github.com/zenon-network/go-zenon/vm/vm_context"
)

var (
	stakeLog = common.EmbeddedLogger.New("contract", "stake")
)

// StakeMethod (Stake) locks the sent ZNN for a chosen duration in
// exchange for a share of the epoch QSR rewards; longer durations
// earn a higher reward weight. The locked amount is reclaimed with
// Cancel once the duration has elapsed.
type StakeMethod struct {
	MethodName string
}

// getWeightedStakeAmount returns the stake's reward weight: amount
// times (9 + duration units) / 10, where a unit is
// constants.StakeTimeUnitSec (30 days) — 1.0x at the 1-unit minimum,
// rising 0.1x per extra unit to 2.1x at the 12-unit maximum.
func getWeightedStakeAmount(amount *big.Int, stakingTime int64) *big.Int {
	weighted := big.NewInt(9 + stakingTime/constants.StakeTimeUnitSec)
	weighted.Mul(weighted, amount)
	weighted.Div(weighted, big.NewInt(10))
	return weighted
}

// GetPlasma quotes the EmbeddedSimple tier; the call sends no
// response block.
func (p *StakeMethod) GetPlasma(plasmaTable *constants.PlasmaTable) (uint64, error) {
	return plasmaTable.EmbeddedSimple, nil
}

// ValidateSendBlock accepts a duration in seconds, carried by a send
// of at least constants.StakeMinAmount ZNN
// (constants.ErrInvalidTokenOrAmount otherwise). The duration must be
// a whole multiple of constants.StakeTimeUnitSec between
// constants.StakeTimeMinSec and constants.StakeTimeMaxSec, else
// constants.ErrInvalidStakingPeriod.
func (p *StakeMethod) ValidateSendBlock(block *nom.AccountBlock) error {
	var err error
	var stakeTime int64

	if err := definition.ABIStake.UnpackMethod(&stakeTime, p.MethodName, block.Data); err != nil {
		return constants.ErrUnpackError
	}

	if block.Amount.Cmp(constants.StakeMinAmount) == -1 || block.TokenStandard != types.ZnnTokenStandard {
		return constants.ErrInvalidTokenOrAmount
	}
	if stakeTime < constants.StakeTimeMinSec || stakeTime > constants.StakeTimeMaxSec || stakeTime%constants.StakeTimeUnitSec != 0 {
		return constants.ErrInvalidStakingPeriod
	}

	block.Data, err = definition.ABIStake.PackMethod(p.MethodName, stakeTime)
	return err
}

// ReceiveBlock creates the StakeInfo entry, with the send block's
// hash as id, the sent amount, its weighted amount, the frontier
// momentum's timestamp as start time and start plus duration as
// expiration time. It cannot fail past validation and emits no
// descendant blocks.
func (p *StakeMethod) ReceiveBlock(context vm_context.AccountVmContext, sendBlock *nom.AccountBlock) ([]*nom.AccountBlock, error) {
	if err := p.ValidateSendBlock(sendBlock); err != nil {
		return nil, err
	}

	var stakeTime int64
	common.DealWithErr(definition.ABIStake.UnpackMethod(&stakeTime, p.MethodName, sendBlock.Data))

	momentum, err := context.GetFrontierMomentum()
	common.DealWithErr(err)

	stakeInfo := definition.StakeInfo{
		Amount:         sendBlock.Amount,
		WeightedAmount: getWeightedStakeAmount(sendBlock.Amount, stakeTime),
		StartTime:      momentum.Timestamp.Unix(),
		RevokeTime:     0,
		ExpirationTime: momentum.Timestamp.Unix() + stakeTime,
		StakeAddress:   sendBlock.Address,
		Id:             sendBlock.Hash,
	}

	common.DealWithErr(stakeInfo.Save(context.Storage()))
	stakeLog.Debug("created stake entry", "id", stakeInfo.Id, "owner", stakeInfo.StakeAddress, "amount", stakeInfo.Amount, "weighted-amount", stakeInfo.WeightedAmount, "duration-in-days", stakeTime/24/60/60)
	return nil, nil
}

// CancelStakeMethod (Cancel) revokes one of the sender's expired
// stake entries by id and refunds the locked ZNN; rewards already
// accrued stay collectable via CollectReward.
type CancelStakeMethod struct {
	MethodName string
}

// GetPlasma quotes the EmbeddedWWithdraw tier, covering the one
// refund block the cancellation sends back.
func (p *CancelStakeMethod) GetPlasma(plasmaTable *constants.PlasmaTable) (uint64, error) {
	return plasmaTable.EmbeddedWWithdraw, nil
}

// ValidateSendBlock accepts a stake-entry id (the hash of the
// original Stake send block) carried by a send with no tokens.
func (p *CancelStakeMethod) ValidateSendBlock(block *nom.AccountBlock) error {
	var err error
	id := new(types.Hash)

	if err := definition.ABIStake.UnpackMethod(id, p.MethodName, block.Data); err != nil {
		return constants.ErrUnpackError
	}

	if block.Amount.Sign() != 0 {
		return constants.ErrInvalidTokenOrAmount
	}

	block.Data, err = definition.ABIStake.PackMethod(p.MethodName, id)
	return err
}

// ReceiveBlock revokes the stake entry stored under the sender's
// address and the given id — a missing entry fails with
// constants.ErrDataNonExistent, so only the staker can cancel — once
// the frontier momentum has reached the expiration time
// (constants.RevokeNotDue before that). The revoke time is stamped
// and the stored amount zeroed; the entry itself is kept until
// computeStakeRewardsForEpoch pays its final epoch and deletes it.
// One descendant send refunds the locked ZNN to the staker.
func (p *CancelStakeMethod) ReceiveBlock(context vm_context.AccountVmContext, sendBlock *nom.AccountBlock) ([]*nom.AccountBlock, error) {
	if err := p.ValidateSendBlock(sendBlock); err != nil {
		return nil, err
	}

	id := new(types.Hash)
	common.DealWithErr(definition.ABIStake.UnpackMethod(id, p.MethodName, sendBlock.Data))

	stakeInfo, err := definition.GetStakeInfo(context.Storage(), *id, sendBlock.Address)
	if err == constants.ErrDataNonExistent {
		return nil, constants.ErrDataNonExistent
	} else {
		common.DealWithErr(err)
	}

	momentum, err := context.GetFrontierMomentum()
	common.DealWithErr(err)

	if stakeInfo.ExpirationTime > momentum.Timestamp.Unix() {
		return nil, constants.RevokeNotDue
	}

	amount := stakeInfo.Amount
	stakeInfo.RevokeTime = momentum.Timestamp.Unix()
	// signal that the amount has been received, to future-proof
	stakeInfo.Amount = common.Big0
	common.DealWithErr(stakeInfo.Save(context.Storage()))

	stakeLog.Debug("revoked stake entry", "id", stakeInfo.Id, "owner", stakeInfo.StakeAddress, "start-time", stakeInfo.StartTime, "revoke-time", stakeInfo.RevokeTime)

	return []*nom.AccountBlock{
		{
			Address:       types.StakeContract,
			ToAddress:     stakeInfo.StakeAddress,
			BlockType:     nom.BlockTypeContractSend,
			Amount:        amount,
			TokenStandard: types.ZnnTokenStandard,
			Data:          nil,
		},
	}, nil
}

// UpdateEmbeddedStakeMethod (Update) advances the stake contract's
// reward bookkeeping. Anyone may call it; it is throttled to one run
// every constants.UpdateMinNumMomentums momentums.
type UpdateEmbeddedStakeMethod struct {
	MethodName string
}

// GetPlasma quotes the EmbeddedSimple tier; the call sends no
// response block.
func (p *UpdateEmbeddedStakeMethod) GetPlasma(plasmaTable *constants.PlasmaTable) (uint64, error) {
	return plasmaTable.EmbeddedSimple, nil
}

// ValidateSendBlock accepts an argument-less call carrying no
// tokens.
func (p *UpdateEmbeddedStakeMethod) ValidateSendBlock(block *nom.AccountBlock) error {
	var err error

	if err := definition.ABIStake.UnpackEmptyMethod(p.MethodName, block.Data); err != nil {
		return constants.ErrUnpackError
	}

	if block.Amount.Sign() != 0 {
		return constants.ErrInvalidTokenOrAmount
	}

	block.Data, err = definition.ABIStake.PackMethod(p.MethodName)
	return err
}

// ReceiveBlock runs the update throttle (constants.ErrUpdateTooRecent
// when called again too soon) and then distributes the stake rewards
// of every epoch that has become due (updateStakeRewards), crediting
// RewardDeposit entries collectable via CollectReward.
func (p *UpdateEmbeddedStakeMethod) ReceiveBlock(context vm_context.AccountVmContext, sendBlock *nom.AccountBlock) ([]*nom.AccountBlock, error) {
	if err := p.ValidateSendBlock(sendBlock); err != nil {
		return nil, err
	}

	if err := checkAndPerformUpdate(context); err != nil {
		return nil, err
	}

	// Update epochRewards
	err := updateStakeRewards(context)
	return nil, err
}

// getWeightedStake integrates the entry's weighted amount over time:
// weighted amount times the seconds the entry was active inside
// [startTime, endTime), clipped to the entry's start time and — for
// revoked entries — its revoke time; zero when the windows don't
// overlap.
func getWeightedStake(info *definition.StakeInfo, startTime, endTime int64) *big.Int {
	startTime = common.MaxInt64(startTime, info.StartTime)
	if info.RevokeTime != 0 {
		endTime = common.MinInt64(endTime, info.RevokeTime)
	}

	if startTime >= endTime {
		return big.NewInt(0)
	}
	cumulatedStake := big.NewInt(endTime - startTime)
	cumulatedStake.Mul(cumulatedStake, info.WeightedAmount)

	return cumulatedStake
}

// computeStakeRewardsForEpoch splits the epoch's QSR reward
// (constants.StakeQsrRewardPerEpoch) among all stake entries pro-rata
// to their time-weighted stake (getWeightedStake) over the epoch,
// crediting each staker's RewardDeposit. Entries revoked before the
// epoch's end have now earned their last reward and are deleted; when
// no stake was active the epoch's reward is simply not distributed.
func computeStakeRewardsForEpoch(context vm_context.AccountVmContext, epoch uint64) error {
	startTime, endTime := context.EpochTicker().ToTime(epoch)

	cumulatedStake := big.NewInt(0)
	totalAmount := constants.StakeQsrRewardPerEpoch(epoch)

	err := definition.IterateStakeEntries(context.Storage(), func(stakeInfo *definition.StakeInfo) error {
		cumulatedStake.Add(cumulatedStake, getWeightedStake(stakeInfo, startTime.Unix(), endTime.Unix()))
		return nil
	})
	if err != nil {
		return err
	}

	stakeLog.Debug("updating stake reward", "epoch", epoch, "total-reward", constants.StakeQsrRewardPerEpoch(epoch), "cumulated-stake", cumulatedStake, "start-time", startTime.Unix(), "end-time", endTime.Unix())
	if cumulatedStake.Sign() == 0 {
		return nil
	}

	err = definition.IterateStakeEntries(context.Storage(), func(stakeInfo *definition.StakeInfo) error {
		reward := new(big.Int).Set(totalAmount)
		reward.Mul(reward, getWeightedStake(stakeInfo, startTime.Unix(), endTime.Unix()))
		reward.Quo(reward, cumulatedStake)

		addReward(context, epoch, definition.RewardDeposit{
			Address: &stakeInfo.StakeAddress,
			Znn:     common.Big0,
			Qsr:     reward,
		})

		stakeLog.Debug("giving rewards", "address", stakeInfo.StakeAddress, "id", stakeInfo.Id, "epoch", epoch, "qsr-amount", reward)

		if stakeInfo.RevokeTime != 0 && stakeInfo.RevokeTime < endTime.Unix() {
			stakeLog.Debug("deleted stake entry", "id", stakeInfo.Id, "revoke-time", stakeInfo.RevokeTime)
			common.DealWithErr(stakeInfo.Delete(context.Storage()))
		}
		return nil
	})
	if err != nil {
		return err
	}

	return nil
}

// updateStakeRewards distributes the rewards of every epoch that has
// become due, advancing the epoch marker one epoch at a time until
// checkAndPerformUpdateEpoch reports the next epoch is still too
// recent.
func updateStakeRewards(context vm_context.AccountVmContext) error {
	lastEpoch, err := definition.GetLastEpochUpdate(context.Storage())
	if err != nil {
		return err
	}

	for {
		if err := checkAndPerformUpdateEpoch(context, lastEpoch); err == constants.ErrEpochUpdateTooRecent {
			stakeLog.Debug("invalid update - rewards not due yet", "epoch", lastEpoch.LastEpoch+1)
			return nil
		} else if err != nil {
			stakeLog.Error("unknown panic", "reason", err)
			return err
		}
		if err := computeStakeRewardsForEpoch(context, uint64(lastEpoch.LastEpoch)); err != nil {
			return err
		}
	}
}
