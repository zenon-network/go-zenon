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
	sentinelLog = common.EmbeddedLogger.New("contract", "sentinel")
)

// GetSentinelRevokeStatus reports where a sentinel stands in its
// revocation cycle at the momentum's timestamp. The cycle repeats
// from the registration time: constants.SentinelLockTimeWindow (27
// days) of lock followed by constants.SentinelRevokeTimeWindow (3
// days) of revocability.
// It returns:
//   - true and the seconds the sentinel can still be revoked for,
//     when revocation is currently possible
//   - false and the seconds until revocation opens, otherwise
func GetSentinelRevokeStatus(registrationTime int64, m *nom.Momentum) (bool, int64) {
	epochTime := (m.Timestamp.Unix() - registrationTime) % (constants.SentinelLockTimeWindow + constants.SentinelRevokeTimeWindow)
	if epochTime < constants.SentinelLockTimeWindow {
		return false, constants.SentinelLockTimeWindow - epochTime
	} else {
		return true, (constants.SentinelLockTimeWindow + constants.SentinelRevokeTimeWindow) - epochTime
	}
}

// RegisterSentinelMethod (RegisterSentinel) creates a sentinel owned
// by the sender. The send must carry exactly
// constants.SentinelZnnRegisterAmount ZNN and the sender must have
// deposited constants.SentinelQsrDepositAmount QSR via DepositQsr
// beforehand; both amounts stay locked in the sentinel until
// revocation.
type RegisterSentinelMethod struct {
	MethodName string
}

// GetPlasma quotes the EmbeddedSimple tier; the call sends no
// response block.
func (method *RegisterSentinelMethod) GetPlasma(plasmaTable *constants.PlasmaTable) (uint64, error) {
	return plasmaTable.EmbeddedSimple, nil
}

// ValidateSendBlock accepts an argument-less call that must carry
// exactly constants.SentinelZnnRegisterAmount ZNN; anything else
// fails with constants.ErrInvalidTokenOrAmount.
func (method *RegisterSentinelMethod) ValidateSendBlock(block *nom.AccountBlock) error {
	var err error

	if err := definition.ABISentinel.UnpackEmptyMethod(method.MethodName, block.Data); err != nil {
		return constants.ErrUnpackError
	}

	if block.TokenStandard != types.ZnnTokenStandard || block.Amount.Cmp(constants.SentinelZnnRegisterAmount) != 0 {
		return constants.ErrInvalidTokenOrAmount
	}

	block.Data, err = definition.ABISentinel.PackMethod(method.MethodName)
	return err
}

// ReceiveBlock registers the sentinel: any existing entry for the
// sender — active or revoked, since revoked entries persist — fails
// with constants.ErrAlreadyRegistered, so an address can register at
// most once; a deposit below constants.SentinelQsrDepositAmount
// fails with constants.ErrNotEnoughDepositedQsr. The QSR is consumed
// and the SentinelInfo saved with the frontier timestamp as its
// registration time.
func (method *RegisterSentinelMethod) ReceiveBlock(context vm_context.AccountVmContext, sendBlock *nom.AccountBlock) ([]*nom.AccountBlock, error) {
	if err := method.ValidateSendBlock(sendBlock); err != nil {
		sentinelLog.Debug("invalid register - syntactic validation failed", "address", sendBlock.Address, "reason", err)
		return nil, err
	}

	sentinel := definition.GetSentinelInfoByOwner(context.Storage(), sendBlock.Address)
	if sentinel != nil {
		sentinelLog.Debug("invalid register - existing address", "address", sendBlock.Address)
		return nil, constants.ErrAlreadyRegistered
	}

	if err := checkAndConsumeQsr(context, sendBlock.Address, constants.SentinelQsrDepositAmount); err != nil {
		sentinelLog.Debug("invalid register - not enough deposited qsr", "address", sendBlock.Address)
		return nil, err
	}

	frontierMomentum, err := context.GetFrontierMomentum()
	common.DealWithErr(err)

	sentinel = &definition.SentinelInfo{
		SentinelInfoKey: definition.SentinelInfoKey{
			Owner: sendBlock.Address,
		},
		RegistrationTimestamp: frontierMomentum.Timestamp.Unix(),
		RevokeTimestamp:       0,
		ZnnAmount:             constants.SentinelZnnRegisterAmount,
		QsrAmount:             constants.SentinelQsrDepositAmount,
	}
	sentinel.Save(context.Storage())
	sentinelLog.Debug("successfully register", "sentinel", sentinel)
	return nil, nil
}

// RevokeSentinelMethod (RevokeSentinel) closes the sender's sentinel
// and returns its locked ZNN and QSR. Revocation is only possible
// inside the periodic revoke window — see GetSentinelRevokeStatus —
// and is permanent: the revoked entry persists and blocks the owner
// from registering again.
type RevokeSentinelMethod struct {
	MethodName string
}

// GetPlasma quotes the EmbeddedWDoubleWithdraw tier, covering the
// two refund blocks (ZNN and QSR) the revocation sends back.
func (method *RevokeSentinelMethod) GetPlasma(plasmaTable *constants.PlasmaTable) (uint64, error) {
	return plasmaTable.EmbeddedWDoubleWithdraw, nil
}

// ValidateSendBlock accepts an argument-less call carrying no
// tokens.
func (method *RevokeSentinelMethod) ValidateSendBlock(block *nom.AccountBlock) error {
	var err error

	if err := definition.ABISentinel.UnpackEmptyMethod(method.MethodName, block.Data); err != nil {
		return constants.ErrUnpackError
	}

	if block.Amount.Sign() != 0 {
		return constants.ErrInvalidTokenOrAmount
	}

	block.Data, err = definition.ABISentinel.PackMethod(method.MethodName)
	return err
}

// ReceiveBlock revokes the sender's sentinel: it must exist
// (constants.ErrDataNonExistent), not be revoked already
// (constants.ErrAlreadyRevoked) and sit inside its revoke window
// (constants.RevokeNotDue). The revoke timestamp is stamped, both
// stored amounts zeroed and two descendant sends refund the locked
// ZNN and QSR to the owner.
func (method *RevokeSentinelMethod) ReceiveBlock(context vm_context.AccountVmContext, sendBlock *nom.AccountBlock) ([]*nom.AccountBlock, error) {
	if err := method.ValidateSendBlock(sendBlock); err != nil {
		sentinelLog.Debug("invalid revoke - syntactic validation failed", "address", sendBlock.Address, "reason", err)
		return nil, err
	}

	frontierMomentum, err := context.GetFrontierMomentum()
	common.DealWithErr(err)

	sentinel := definition.GetSentinelInfoByOwner(context.Storage(), sendBlock.Address)
	if sentinel == nil {
		sentinelLog.Debug("invalid revoke - sentinel is not registered", "address", sendBlock.Address)
		return nil, constants.ErrDataNonExistent
	}

	if sentinel.RevokeTimestamp != 0 {
		sentinelLog.Debug("invalid revoke - sentinel is already revoked", "address", sendBlock.Address)
		return nil, constants.ErrAlreadyRevoked
	}

	if canRevoke, untilRevoke := GetSentinelRevokeStatus(sentinel.RegistrationTimestamp, frontierMomentum); !canRevoke {
		sentinelLog.Debug("invalid revoke - cannot be revoked yet", "address", sendBlock.Address, "until-revoke", untilRevoke)
		return nil, constants.RevokeNotDue
	}

	znnAmount := new(big.Int).Set(sentinel.ZnnAmount)
	qsrAmount := new(big.Int).Set(sentinel.QsrAmount)

	sentinel.RevokeTimestamp = frontierMomentum.Timestamp.Unix()
	sentinel.ZnnAmount.Set(common.Big0)
	sentinel.QsrAmount.Set(common.Big0)
	sentinel.Save(context.Storage())
	sentinelLog.Debug("successfully revoke", "sentinel", sentinel)
	return []*nom.AccountBlock{
		{
			ToAddress:     sentinel.Owner,
			Amount:        znnAmount,
			TokenStandard: types.ZnnTokenStandard,
		},
		{
			ToAddress:     sentinel.Owner,
			Amount:        qsrAmount,
			TokenStandard: types.QsrTokenStandard,
		},
	}, nil
}

// UpdateEmbeddedSentinelMethod (Update) advances the sentinel
// contract's reward bookkeeping. Anyone may call it; it is throttled
// to one run every constants.UpdateMinNumMomentums momentums.
type UpdateEmbeddedSentinelMethod struct {
	MethodName string
}

// GetPlasma quotes the EmbeddedSimple tier; the call sends no
// response block.
func (method *UpdateEmbeddedSentinelMethod) GetPlasma(plasmaTable *constants.PlasmaTable) (uint64, error) {
	return plasmaTable.EmbeddedSimple, nil
}

// ValidateSendBlock accepts an argument-less call carrying no
// tokens.
func (method *UpdateEmbeddedSentinelMethod) ValidateSendBlock(block *nom.AccountBlock) error {
	var err error

	if err := definition.ABISentinel.UnpackEmptyMethod(method.MethodName, block.Data); err != nil {
		return constants.ErrUnpackError
	}

	if block.Amount.Sign() != 0 {
		return constants.ErrInvalidTokenOrAmount
	}

	block.Data, err = definition.ABISentinel.PackMethod(method.MethodName)
	return err
}

// ReceiveBlock runs the update throttle (constants.ErrUpdateTooRecent
// when called again too soon) and then credits the sentinel rewards
// of every epoch that has become due (updateSentinelRewards),
// collectable via CollectReward.
func (method *UpdateEmbeddedSentinelMethod) ReceiveBlock(context vm_context.AccountVmContext, sendBlock *nom.AccountBlock) ([]*nom.AccountBlock, error) {
	if err := method.ValidateSendBlock(sendBlock); err != nil {
		sentinelLog.Debug("invalid update - syntactic validation failed", "address", sendBlock.Address, "reason", err)
		return nil, err
	}

	if err := checkAndPerformUpdate(context); err != nil {
		sentinelLog.Debug("invalid update - cannot perform update", "address", sendBlock.Address, "reason", err)
		return nil, err
	}

	// Update epochRewards
	err := updateSentinelRewards(context)
	return nil, err
}

// getWeightedSentinel returns 1 when the sentinel was registered
// (and not yet revoked) for more than 90% of the epoch between
// startTime and endTime, and 0 otherwise; registration and revoke
// timestamps clip the active span.
func getWeightedSentinel(info *definition.SentinelInfo, startTime, endTime int64) *big.Int {
	epochDuration := endTime - startTime
	startTime = common.MaxInt64(startTime, info.RegistrationTimestamp)
	if info.RevokeTimestamp != 0 {
		endTime = common.MinInt64(endTime, info.RevokeTimestamp)
	}

	if startTime >= endTime {
		return common.Big0
	}
	if epochDuration*90 < (endTime-startTime)*100 {
		return common.Big1
	}
	return common.Big0
}

// computeSentinelRewardsForEpoch splits the epoch's sentinel rewards
// (constants.SentinelRewardForEpoch) equally among the sentinels
// that pass the 90% activity test, crediting each owner's
// RewardDeposit; with no qualifying sentinel the epoch's rewards are
// simply not distributed.
func computeSentinelRewardsForEpoch(context vm_context.AccountVmContext, epoch uint64) error {
	startTime, endTime := context.EpochTicker().ToTime(epoch)

	cumulatedSentinel := big.NewInt(0)
	totalZnnAmount, totalQsrAmount := constants.SentinelRewardForEpoch(epoch)

	err := definition.IterateSentinelEntries(context.Storage(), func(sentinelInfo *definition.SentinelInfo) error {
		cumulatedSentinel.Add(cumulatedSentinel, getWeightedSentinel(sentinelInfo, startTime.Unix(), endTime.Unix()))
		return nil
	})
	if err != nil {
		sentinelLog.Debug("unable to update sentinel reward", "epoch", epoch, "start-time", startTime.Unix(), "end-time", endTime.Unix(), "reason", err)
		return err
	}

	sentinelLog.Debug("updating sentinel reward", "epoch", epoch, "total-znn-reward", totalZnnAmount, "total-qsr-reward", totalQsrAmount, "cumulated-sentinel", cumulatedSentinel, "start-time", startTime.Unix(), "end-time", endTime.Unix())
	if cumulatedSentinel.Sign() == 0 {
		return nil
	}

	err = definition.IterateSentinelEntries(context.Storage(), func(sentinelInfo *definition.SentinelInfo) error {
		weight := getWeightedSentinel(sentinelInfo, startTime.Unix(), endTime.Unix())
		if weight.Sign() == 0 {
			return nil
		}

		znnReward := new(big.Int).Set(totalZnnAmount)
		znnReward.Mul(znnReward, weight)
		znnReward.Quo(znnReward, cumulatedSentinel)
		qsrReward := new(big.Int).Set(totalQsrAmount)
		qsrReward.Mul(qsrReward, weight)
		qsrReward.Quo(qsrReward, cumulatedSentinel)

		sentinelLog.Debug("giving rewards", "address", sentinelInfo.Owner, "epoch", epoch, "znn-amount", znnReward, "qsr-amount", qsrReward)
		addReward(context, epoch, definition.RewardDeposit{
			Address: &sentinelInfo.Owner,
			Znn:     znnReward,
			Qsr:     qsrReward,
		})

		return nil
	})
	if err != nil {
		return err
	}

	return nil
}

// updateSentinelRewards distributes the rewards of every epoch that
// has become due, advancing the epoch marker one epoch at a time
// until checkAndPerformUpdateEpoch reports the next epoch is still
// too recent.
func updateSentinelRewards(context vm_context.AccountVmContext) error {
	lastEpoch, err := definition.GetLastEpochUpdate(context.Storage())
	if err != nil {
		return err
	}

	for {
		if err := checkAndPerformUpdateEpoch(context, lastEpoch); err == constants.ErrEpochUpdateTooRecent {
			sentinelLog.Debug("invalid update - rewards not due yet", "epoch", lastEpoch.LastEpoch+1)
			return nil
		} else if err != nil {
			sentinelLog.Error("unknown panic", "reason", err)
			return err
		}
		if err := computeSentinelRewardsForEpoch(context, uint64(lastEpoch.LastEpoch)); err != nil {
			return err
		}
	}
}
