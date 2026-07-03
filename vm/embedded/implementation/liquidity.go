package implementation

import (
	"bytes"
	eabi "github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/crypto"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/vm/constants"
	"github.com/zenon-network/go-zenon/vm/embedded/definition"
	"github.com/zenon-network/go-zenon/vm/vm_context"
	"math/big"
	"reflect"
	"sort"
)

var (
	liquidityLog = common.EmbeddedLogger.New("contract", "liquidity")
)

// UpdateEmbeddedLiquidityMethod (Update) is the original liquidity
// update, registered while the contract is a plain emission sink: it
// mints each due epoch's liquidity share of the network emission to
// the contract itself, distributing nothing. The bridge-and-liquidity
// spork replaces it with UpdateRewardEmbeddedLiquidityMethod — see
// vm/embedded/embedded.go.
type UpdateEmbeddedLiquidityMethod struct {
	MethodName string
}

// GetPlasma quotes the EmbeddedSimple tier; the descendant mint calls
// are contract sends, which need no plasma.
func (method *UpdateEmbeddedLiquidityMethod) GetPlasma(plasmaTable *constants.PlasmaTable) (uint64, error) {
	return plasmaTable.EmbeddedSimple, nil
}

// ValidateSendBlock accepts an argument-less call carrying no
// tokens: extra ABI arguments fail with constants.ErrUnpackError and
// a non-zero Amount with constants.ErrInvalidTokenOrAmount.
func (method *UpdateEmbeddedLiquidityMethod) ValidateSendBlock(block *nom.AccountBlock) error {
	var err error

	if err := definition.ABILiquidity.UnpackEmptyMethod(method.MethodName, block.Data); err != nil {
		return constants.ErrUnpackError
	}

	if block.Amount.Sign() != 0 {
		return constants.ErrInvalidTokenOrAmount
	}

	block.Data, err = definition.ABILiquidity.PackMethod(method.MethodName)
	return err
}

// ReceiveBlock processes all due reward epochs, subject to the
// common update throttle (constants.ErrUpdateTooRecent when called
// again within constants.UpdateMinNumMomentums momentums), and
// returns the descendant mint calls produced by
// updateLiquidityRewards.
func (method *UpdateEmbeddedLiquidityMethod) ReceiveBlock(context vm_context.AccountVmContext, sendBlock *nom.AccountBlock) ([]*nom.AccountBlock, error) {
	if err := method.ValidateSendBlock(sendBlock); err != nil {
		liquidityLog.Debug("invalid update - syntactic validation failed", "address", sendBlock.Address, "reason", err)
		return nil, err
	}

	if err := checkAndPerformUpdate(context); err != nil {
		liquidityLog.Debug("invalid update - cannot perform update", "address", sendBlock.Address, "reason", err)
		return nil, err
	}

	return updateLiquidityRewards(context)
}

// computeLiquidityRewardsForEpoch returns the two descendant blocks
// that mint the epoch's liquidity share of the network emission —
// constants.LiquidityRewardForEpoch — in ZNN and QSR to the liquidity
// contract itself.
func computeLiquidityRewardsForEpoch(context vm_context.AccountVmContext, epoch uint64) ([]*nom.AccountBlock, error) {
	totalZnnAmount, totalQsrAmount := constants.LiquidityRewardForEpoch(epoch)

	liquidityLog.Debug("updating liquidity reward", "epoch", epoch, "znn-amount", totalZnnAmount, "qsr-amount", totalQsrAmount)

	// return blocks that issue tokens to liquidity embedded
	return []*nom.AccountBlock{
		{
			ToAddress: types.TokenContract,
			Amount:    common.Big0,
			Data: definition.ABIToken.PackMethodPanic(
				definition.MintMethodName,
				types.ZnnTokenStandard,
				totalZnnAmount,
				types.LiquidityContract,
			),
		},
		{
			ToAddress: types.TokenContract,
			Amount:    common.Big0,
			Data: definition.ABIToken.PackMethodPanic(
				definition.MintMethodName,
				types.QsrTokenStandard,
				totalQsrAmount,
				types.LiquidityContract,
			),
		},
	}, nil
}

// updateLiquidityRewards advances the epoch ratchet over every due
// epoch — see checkAndPerformUpdateEpoch — collecting the mint blocks
// of computeLiquidityRewardsForEpoch, and stops early once the result
// already holds constants.MaxEpochsPerUpdate blocks. Note that the
// stop condition counts blocks (two per epoch) rather than epochs and
// is checked after the ratchet has already advanced.
func updateLiquidityRewards(context vm_context.AccountVmContext) ([]*nom.AccountBlock, error) {
	lastEpoch, err := definition.GetLastEpochUpdate(context.Storage())
	if err != nil {
		return nil, err
	}

	result := make([]*nom.AccountBlock, 0)

	for {
		if err := checkAndPerformUpdateEpoch(context, lastEpoch); err == constants.ErrEpochUpdateTooRecent || len(result) >= constants.MaxEpochsPerUpdate {
			liquidityLog.Debug("invalid update - rewards not due yet", "epoch", lastEpoch.LastEpoch+1)
			return result, nil
		} else if err != nil {
			liquidityLog.Error("unknown panic", "reason", err)
			return nil, err
		}
		if blocks, err := computeLiquidityRewardsForEpoch(context, uint64(lastEpoch.LastEpoch)); err != nil {
			return nil, err
		} else {
			result = append(result, blocks...)
		}
	}
}

// FundMethod (Fund) lets the spork address move ZNN and QSR from the
// liquidity contract's balance to the accelerator contract as a
// donation, funding accelerator projects from the accumulated
// liquidity emission.
type FundMethod struct {
	MethodName string
}

// Fee returns a zero fee. It is not part of the embedded.Method
// interface and has no callers.
func (p *FundMethod) Fee() (*big.Int, error) {
	return big.NewInt(0), nil
}

// GetPlasma quotes the EmbeddedSimple tier; the descendant donations
// are contract sends, which need no plasma.
func (p *FundMethod) GetPlasma(plasmaTable *constants.PlasmaTable) (uint64, error) {
	return plasmaTable.EmbeddedSimple, nil
}

// ValidateSendBlock accepts a packed definition.FundParam (ZNN and
// QSR amounts) sent by the spork address; any other sender fails with
// constants.ErrPermissionDenied.
func (p *FundMethod) ValidateSendBlock(block *nom.AccountBlock) error {
	if block.Address != *types.SporkAddress {
		return constants.ErrPermissionDenied
	}

	var err error
	param := new(definition.FundParam)

	if err := definition.ABILiquidity.UnpackMethod(param, p.MethodName, block.Data); err != nil {
		return constants.ErrUnpackError
	}

	block.Data, err = definition.ABILiquidity.PackMethod(p.MethodName, param.ZnnReward, param.QsrReward)
	return err
}

// ReceiveBlock returns two descendant sends donating the requested
// ZNN and QSR amounts to the accelerator contract via Donate. The
// contract's balance must cover both amounts, else
// constants.ErrInvalidTokenOrAmount. While the accelerator spork is
// not enforced the call is a no-op.
func (p *FundMethod) ReceiveBlock(context vm_context.AccountVmContext, sendBlock *nom.AccountBlock) ([]*nom.AccountBlock, error) {
	if err := p.ValidateSendBlock(sendBlock); err != nil {
		return nil, err
	}

	param := new(definition.FundParam)
	err := definition.ABILiquidity.UnpackMethod(param, p.MethodName, sendBlock.Data)
	common.DealWithErr(err)

	blocks := make([]*nom.AccountBlock, 0)
	if context.IsAcceleratorSporkEnforced() {
		znnBalance, err := context.GetBalance(types.ZnnTokenStandard)
		if err != nil {
			return nil, err
		}
		qsrBalance, err := context.GetBalance(types.QsrTokenStandard)
		if err != nil {
			return nil, err
		}
		if znnBalance.Cmp(param.ZnnReward) != -1 && qsrBalance.Cmp(param.QsrReward) != -1 {
			znnReward := &nom.AccountBlock{
				Address:       types.LiquidityContract,
				ToAddress:     types.AcceleratorContract,
				Data:          definition.ABICommon.PackMethodPanic(definition.DonateMethodName),
				TokenStandard: types.ZnnTokenStandard,
				Amount:        param.ZnnReward,
			}
			blocks = append(blocks, znnReward)

			qsrReward := &nom.AccountBlock{
				Address:       types.LiquidityContract,
				ToAddress:     types.AcceleratorContract,
				Data:          definition.ABICommon.PackMethodPanic(definition.DonateMethodName),
				TokenStandard: types.QsrTokenStandard,
				Amount:        param.QsrReward,
			}
			blocks = append(blocks, qsrReward)
			liquidityLog.Debug("donate reward to accelerator", "znn-amount", znnReward.Amount, "qsr-amount", qsrReward.Amount)
		} else {
			liquidityLog.Debug("invalid send reward - not enough funds")
			return nil, constants.ErrInvalidTokenOrAmount
		}
	}
	return blocks, nil
}

// BurnZnnMethod (BurnZnn) lets the spork address burn ZNN from the
// liquidity contract's balance, removing part of the accumulated
// liquidity emission from circulation.
type BurnZnnMethod struct {
	MethodName string
}

// Fee returns a zero fee. It is not part of the embedded.Method
// interface and has no callers.
func (p *BurnZnnMethod) Fee() (*big.Int, error) {
	return big.NewInt(0), nil
}

// GetPlasma quotes the EmbeddedSimple tier; the descendant burn is a
// contract send, which needs no plasma.
func (p *BurnZnnMethod) GetPlasma(plasmaTable *constants.PlasmaTable) (uint64, error) {
	return plasmaTable.EmbeddedSimple, nil
}

// ValidateSendBlock accepts a packed definition.BurnParam (the burn
// amount) sent by the spork address; any other sender fails with
// constants.ErrPermissionDenied.
func (p *BurnZnnMethod) ValidateSendBlock(block *nom.AccountBlock) error {
	if block.Address != *types.SporkAddress {
		return constants.ErrPermissionDenied
	}

	var err error
	param := new(definition.BurnParam)

	if err := definition.ABILiquidity.UnpackMethod(param, p.MethodName, block.Data); err != nil {
		return constants.ErrUnpackError
	}

	block.Data, err = definition.ABILiquidity.PackMethod(p.MethodName, param.BurnAmount)
	return err
}

// ReceiveBlock returns one descendant send burning the requested
// amount through the token contract's Burn method. The contract's
// ZNN balance must cover it, else constants.ErrInvalidTokenOrAmount.
// While the accelerator spork is not enforced the call is a no-op.
func (p *BurnZnnMethod) ReceiveBlock(context vm_context.AccountVmContext, sendBlock *nom.AccountBlock) ([]*nom.AccountBlock, error) {
	if err := p.ValidateSendBlock(sendBlock); err != nil {
		return nil, err
	}

	param := new(definition.BurnParam)
	err := definition.ABILiquidity.UnpackMethod(param, p.MethodName, sendBlock.Data)
	common.DealWithErr(err)

	blocks := make([]*nom.AccountBlock, 0)
	if context.IsAcceleratorSporkEnforced() {
		znnBalance, err := context.GetBalance(types.ZnnTokenStandard)
		if err != nil {
			return nil, err
		}
		if znnBalance.Cmp(param.BurnAmount) != -1 {
			burnBlock := &nom.AccountBlock{
				Address:       types.LiquidityContract,
				ToAddress:     types.TokenContract,
				Data:          definition.ABIToken.PackMethodPanic(definition.BurnMethodName),
				TokenStandard: types.ZnnTokenStandard,
				Amount:        param.BurnAmount,
			}
			blocks = append(blocks, burnBlock)
			liquidityLog.Debug("burn ZNN", "znn-amount", burnBlock.Amount)
		} else {
			liquidityLog.Debug("invalid burn ZNN - not enough funds")
			return nil, constants.ErrInvalidTokenOrAmount
		}
	}
	return blocks, nil
}

// SetTokenTupleMethod (SetTokenTuple) is the administrator method
// that replaces the contract's whole list of stakable token tuples —
// each a ZTS with its ZNN and QSR reward percentages and minimum
// stake amount. The change is protected by a soft-delay time
// challenge.
type SetTokenTupleMethod struct {
	MethodName string
}

// GetPlasma quotes the EmbeddedSimple tier; the call sends no
// response block.
func (p *SetTokenTupleMethod) GetPlasma(plasmaTable *constants.PlasmaTable) (uint64, error) {
	return plasmaTable.EmbeddedSimple, nil
}

// ValidateSendBlock accepts a packed definition.TokenTuplesParam
// carrying no tokens. Its four arrays must have equal lengths
// (constants.ErrForbiddenParam otherwise) and, when non-empty, every
// token standard must parse to a non-zero ZTS without duplicates and
// the ZNN and QSR percentages must each sum to their basis-point
// denominators — constants.LiquidityZnnTotalPercentages and
// constants.LiquidityQsrTotalPercentages (both 10,000) — else
// constants.ErrInvalidPercentages.
func (p *SetTokenTupleMethod) ValidateSendBlock(block *nom.AccountBlock) error {
	var err error
	param := new(definition.TokenTuplesParam)

	if err := definition.ABILiquidity.UnpackMethod(param, p.MethodName, block.Data); err != nil {
		return constants.ErrUnpackError
	}

	if block.Amount.Sign() != 0 {
		return constants.ErrInvalidTokenOrAmount
	}

	length := len(param.TokenStandards)
	if length != len(param.ZnnPercentages) || length != len(param.QsrPercentages) || length != len(param.MinAmounts) {
		return constants.ErrForbiddenParam
	}

	if length != 0 {
		totalZnn := uint32(0)
		totalQsr := uint32(0)

		tokensMap := make(map[string]bool)
		for index := 0; index < length; index++ {
			zts, errParse := types.ParseZTS(param.TokenStandards[index])
			if errParse != nil {
				return errParse
			} else if reflect.DeepEqual(zts.Bytes(), types.ZeroTokenStandard.Bytes()) {
				return constants.ErrForbiddenParam
			}
			ok, _ := tokensMap[zts.String()]
			if ok {
				// duplicate zts
				return constants.ErrForbiddenParam
			}
			tokensMap[zts.String()] = true

			totalZnn += param.ZnnPercentages[index]
			totalQsr += param.QsrPercentages[index]
		}

		if totalZnn != constants.LiquidityZnnTotalPercentages || totalQsr != constants.LiquidityQsrTotalPercentages {
			return constants.ErrInvalidPercentages
		}
	}

	block.Data, err = definition.ABILiquidity.PackMethod(p.MethodName, param.TokenStandards, param.ZnnPercentages, param.QsrPercentages, param.MinAmounts)
	return err
}

// ReceiveBlock replaces the token-tuple list in the LiquidityInfo.
// Security must be initialized (CheckSecurityInitialized) and the
// sender must be the administrator, else
// constants.ErrPermissionDenied. The change passes a TimeChallenge
// over the encoded tuples with the security info's SoftDelay: the
// first call only records the challenge and the list is saved when
// the call is repeated with identical parameters after the delay.
func (p *SetTokenTupleMethod) ReceiveBlock(context vm_context.AccountVmContext, sendBlock *nom.AccountBlock) ([]*nom.AccountBlock, error) {
	if err := p.ValidateSendBlock(sendBlock); err != nil {
		return nil, err
	}

	param := new(definition.TokenTuplesParam)
	err := definition.ABILiquidity.UnpackMethod(param, p.MethodName, sendBlock.Data)
	if err != nil {
		return nil, err
	}

	if _, errSec := CheckSecurityInitialized(context); errSec != nil {
		return nil, errSec
	}

	liquidityInfo, err := definition.GetLiquidityInfo(context.Storage())
	if err != nil {
		return nil, err
	}

	if sendBlock.Address.String() != liquidityInfo.Administrator.String() {
		return nil, constants.ErrPermissionDenied
	}

	liquidityInfo.TokenTuples = make([]definition.TokenTuple, 0)
	for i := 0; i < len(param.TokenStandards); i++ {
		tokenTuple := definition.TokenTuple{
			TokenStandard: param.TokenStandards[i],
			ZnnPercentage: param.ZnnPercentages[i],
			QsrPercentage: param.QsrPercentages[i],
			MinAmount:     param.MinAmounts[i],
		}
		liquidityInfo.TokenTuples = append(liquidityInfo.TokenTuples, tokenTuple)
	}
	liquidityInfoVariable, err := definition.EncodeLiquidityInfo(liquidityInfo)
	if err != nil {
		return nil, err
	}

	securityInfo, err := definition.GetSecurityInfoVariable(context.Storage())
	if err != nil {
		return nil, err
	}

	paramsHash := crypto.Hash(liquidityInfoVariable.TokenTuples...)
	if timeChallengeInfo, errTimeChallenge := TimeChallenge(context, p.MethodName, paramsHash, securityInfo.SoftDelay); errTimeChallenge != nil {
		return nil, errTimeChallenge
	} else {
		// if paramsHash is not zero it means we had a new challenge and we can't go further to save the change into local db
		if !timeChallengeInfo.ParamsHash.IsZero() {
			return nil, nil
		}
	}

	common.DealWithErr(liquidityInfoVariable.Save(context.Storage()))
	return nil, nil
}

// LiquidityStakeMethod (LiquidityStake) locks the sent amount of a
// configured token tuple's ZTS for the chosen duration, earning a
// share of the per-token liquidity rewards weighted by amount and
// duration.
type LiquidityStakeMethod struct {
	MethodName string
}

// getWeightedLiquidityStakeAmount scales the staked amount by the
// constants.LiquidityStakeWeights multiplier for the chosen duration:
// a stake of n time units counts n times its amount.
func getWeightedLiquidityStakeAmount(amount *big.Int, stakingTime int64) *big.Int {
	period := stakingTime / constants.StakeTimeUnitSec
	weighted := big.NewInt(constants.LiquidityStakeWeights[period])
	weighted.Mul(weighted, amount)
	return weighted
}

// GetPlasma quotes the EmbeddedSimple tier; staking sends no
// response block.
func (p *LiquidityStakeMethod) GetPlasma(plasmaTable *constants.PlasmaTable) (uint64, error) {
	return plasmaTable.EmbeddedSimple, nil
}

// ValidateSendBlock accepts a packed duration in seconds, which must
// be a whole number of constants.StakeTimeUnitSec units between
// constants.StakeTimeMinSec and constants.StakeTimeMaxSec (1 to 12
// units of 30 days), else constants.ErrInvalidStakingPeriod. The
// sent token and amount are only checked in ReceiveBlock, against
// the configured token tuples.
func (p *LiquidityStakeMethod) ValidateSendBlock(block *nom.AccountBlock) error {
	var err error
	var stakeTime int64

	if err := definition.ABILiquidity.UnpackMethod(&stakeTime, p.MethodName, block.Data); err != nil {
		return constants.ErrUnpackError
	}

	if stakeTime < constants.StakeTimeMinSec || stakeTime > constants.StakeTimeMaxSec || stakeTime%constants.StakeTimeUnitSec != 0 {
		return constants.ErrInvalidStakingPeriod
	}

	block.Data, err = definition.ABILiquidity.PackMethod(p.MethodName, stakeTime)
	return err
}

// ReceiveBlock saves a definition.LiquidityStakeEntry keyed by the
// send block's hash, with the weighted amount from
// getWeightedLiquidityStakeAmount and the expiration set one duration
// past the frontier momentum. The sent token must be a configured
// tuple (constants.ErrInvalidToken otherwise) and the amount at least
// its MinAmount, else constants.ErrInvalidTokenOrAmount. No
// descendant blocks are emitted.
func (p *LiquidityStakeMethod) ReceiveBlock(context vm_context.AccountVmContext, sendBlock *nom.AccountBlock) ([]*nom.AccountBlock, error) {
	if err := p.ValidateSendBlock(sendBlock); err != nil {
		return nil, err
	}

	var stakeTime int64
	common.DealWithErr(definition.ABILiquidity.UnpackMethod(&stakeTime, p.MethodName, sendBlock.Data))

	momentum, err := context.GetFrontierMomentum()
	common.DealWithErr(err)

	liquidityInfo, err := definition.GetLiquidityInfo(context.Storage())
	common.DealWithErr(err)

	found := false
	for _, tokenTuple := range liquidityInfo.TokenTuples {
		if tokenTuple.TokenStandard == sendBlock.TokenStandard.String() {
			if sendBlock.Amount.Cmp(tokenTuple.MinAmount) == -1 {
				return nil, constants.ErrInvalidTokenOrAmount
			}

			found = true
			break
		}
	}
	if !found {
		return nil, constants.ErrInvalidToken
	}

	stakeEntry := definition.LiquidityStakeEntry{
		Amount:         sendBlock.Amount,
		TokenStandard:  sendBlock.TokenStandard,
		WeightedAmount: getWeightedLiquidityStakeAmount(sendBlock.Amount, stakeTime),
		StartTime:      momentum.Timestamp.Unix(),
		RevokeTime:     0,
		ExpirationTime: momentum.Timestamp.Unix() + stakeTime,
		StakeAddress:   sendBlock.Address,
		Id:             sendBlock.Hash,
	}

	common.DealWithErr(stakeEntry.Save(context.Storage()))
	stakeLog.Debug("created liquidity stake entry", "id", stakeEntry.Id, "owner", stakeEntry.StakeAddress, "amount", stakeEntry.Amount, "weighted-amount", stakeEntry.WeightedAmount, "duration-in-days", stakeTime/24/60/60)
	return nil, nil
}

// CancelLiquidityStakeMethod (CancelLiquidityStake) refunds an
// expired liquidity stake entry to its owner.
type CancelLiquidityStakeMethod struct {
	MethodName string
}

// GetPlasma quotes the EmbeddedWWithdraw tier, covering the one
// refund block the call sends back.
func (p *CancelLiquidityStakeMethod) GetPlasma(plasmaTable *constants.PlasmaTable) (uint64, error) {
	return plasmaTable.EmbeddedWWithdraw, nil
}

// ValidateSendBlock accepts a packed entry id (types.Hash) carrying
// no tokens; a non-zero amount fails with
// constants.ErrInvalidTokenOrAmount.
func (p *CancelLiquidityStakeMethod) ValidateSendBlock(block *nom.AccountBlock) error {
	var err error
	id := new(types.Hash)
	if err := definition.ABILiquidity.UnpackMethod(id, p.MethodName, block.Data); err != nil {
		return constants.ErrUnpackError
	}

	if block.Amount.Sign() != 0 {
		return constants.ErrInvalidTokenOrAmount
	}

	block.Data, err = definition.ABILiquidity.PackMethod(p.MethodName, id)
	return err
}

// ReceiveBlock refunds the staked amount in one descendant send. The
// entry is looked up by id and sender (constants.ErrDataNonExistent
// when absent) and must have expired, else constants.RevokeNotDue.
// Rather than deleting the entry it records the revoke time and
// zeroes the amount, leaving the next reward run to count the stake's
// active time and delete it — see
// computeLiquidityStakeRewardsForEpoch.
func (p *CancelLiquidityStakeMethod) ReceiveBlock(context vm_context.AccountVmContext, sendBlock *nom.AccountBlock) ([]*nom.AccountBlock, error) {
	if err := p.ValidateSendBlock(sendBlock); err != nil {
		return nil, err
	}

	id := new(types.Hash)
	common.DealWithErr(definition.ABILiquidity.UnpackMethod(id, p.MethodName, sendBlock.Data))

	stakeInfo, err := definition.GetLiquidityStakeEntry(context.Storage(), *id, sendBlock.Address)
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

	stakeLog.Debug("revoked liquidity stake entry", "id", stakeInfo.Id, "owner", stakeInfo.StakeAddress, "start-time", stakeInfo.StartTime, "revoke-time", stakeInfo.RevokeTime)

	return []*nom.AccountBlock{
		{
			Address:       types.LiquidityContract,
			ToAddress:     stakeInfo.StakeAddress,
			BlockType:     nom.BlockTypeContractSend,
			Amount:        amount,
			TokenStandard: stakeInfo.TokenStandard,
			Data:          nil,
		},
	}, nil
}

// UpdateRewardEmbeddedLiquidityMethod (Update) replaces
// UpdateEmbeddedLiquidityMethod once the bridge-and-liquidity spork
// is enforced: instead of parking the epoch emission in the contract
// it distributes it — plus any administrator-set additional rewards —
// to liquidity stakers as collectable reward deposits, one epoch per
// call.
type UpdateRewardEmbeddedLiquidityMethod struct {
	MethodName string
}

// GetPlasma quotes the EmbeddedSimple tier; the descendant mint and
// burn calls are contract sends, which need no plasma.
func (method *UpdateRewardEmbeddedLiquidityMethod) GetPlasma(plasmaTable *constants.PlasmaTable) (uint64, error) {
	return plasmaTable.EmbeddedSimple, nil
}

// ValidateSendBlock accepts an argument-less call carrying no
// tokens: extra ABI arguments fail with constants.ErrUnpackError and
// a non-zero Amount with constants.ErrInvalidTokenOrAmount.
func (method *UpdateRewardEmbeddedLiquidityMethod) ValidateSendBlock(block *nom.AccountBlock) error {
	var err error

	if err := definition.ABILiquidity.UnpackEmptyMethod(method.MethodName, block.Data); err != nil {
		return constants.ErrUnpackError
	}

	if block.Amount.Sign() != 0 {
		return constants.ErrInvalidTokenOrAmount
	}

	block.Data, err = definition.ABILiquidity.PackMethod(method.MethodName)
	return err
}

// ReceiveBlock processes the next due reward epoch, subject to the
// common update throttle (constants.ErrUpdateTooRecent when called
// again within constants.UpdateMinNumMomentums momentums), and
// returns the descendant blocks produced by
// updateLiquidityStakeRewards.
func (method *UpdateRewardEmbeddedLiquidityMethod) ReceiveBlock(context vm_context.AccountVmContext, sendBlock *nom.AccountBlock) ([]*nom.AccountBlock, error) {
	if err := method.ValidateSendBlock(sendBlock); err != nil {
		liquidityLog.Debug("invalid update - syntactic validation failed", "address", sendBlock.Address, "reason", err)
		return nil, err
	}

	if err := checkAndPerformUpdate(context); err != nil {
		liquidityLog.Debug("invalid update - cannot perform update", "address", sendBlock.Address, "reason", err)
		return nil, err
	}

	return updateLiquidityStakeRewards(context)
}

// getWeightedLiquidityStake integrates the entry's weighted amount
// over time: the weighted amount times the seconds of overlap
// between [startTime, endTime) and the stake's active interval, from
// its start until its revoke time (or indefinitely while unrevoked).
func getWeightedLiquidityStake(info *definition.LiquidityStakeEntry, startTime, endTime int64) *big.Int {
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

// computeLiquidityStakeRewardsForEpoch distributes the epoch's
// liquidity rewards to stakers. While the contract is halted it only
// mints the epoch emission (constants.LiquidityRewardForEpoch) to the
// contract itself, exactly like the pre-spork Update. Otherwise:
//   - when the contract's balance covers the administrator-set
//     additional rewards (see SetAdditionalReward), they are added to
//     the distributable totals and burned from the balance — the
//     burn offsets the later mints, since deposits are paid out as
//     freshly minted coins by CollectReward
//   - the ZNN and QSR totals are split between the configured token
//     tuples by their basis-point percentages (out of
//     constants.LiquidityZnnTotalPercentages /
//     constants.LiquidityQsrTotalPercentages)
//   - each token's share is divided among its stake entries pro-rata
//     to their time-weighted stake over the epoch
//     (getWeightedLiquidityStake), credited via addReward and
//     collected through CollectReward; entries revoked before the
//     epoch's end are deleted
//   - crediting more than the totals fails with
//     constants.ErrInvalidRewards, and any undistributed remainder
//     (rounding dust, tokens without stakers) is minted to the
//     contract itself
func computeLiquidityStakeRewardsForEpoch(context vm_context.AccountVmContext, epoch uint64) ([]*nom.AccountBlock, error) {
	liquidityInfo, err := definition.GetLiquidityInfo(context.Storage())
	if err != nil {
		return nil, err
	}
	totalZnnAmount, totalQsrAmount := constants.LiquidityRewardForEpoch(epoch)
	if liquidityInfo.IsHalted {
		// return blocks that issue tokens to liquidity embedded
		return []*nom.AccountBlock{
			{
				ToAddress: types.TokenContract,
				Amount:    common.Big0,
				Data: definition.ABIToken.PackMethodPanic(
					definition.MintMethodName,
					types.ZnnTokenStandard,
					totalZnnAmount,
					types.LiquidityContract,
				),
			},
			{
				ToAddress: types.TokenContract,
				Amount:    common.Big0,
				Data: definition.ABIToken.PackMethodPanic(
					definition.MintMethodName,
					types.QsrTokenStandard,
					totalQsrAmount,
					types.LiquidityContract,
				),
			},
		}, nil
	}

	znnBalance, err := context.GetBalance(types.ZnnTokenStandard)
	if err != nil {
		return nil, err
	}
	qsrBalance, err := context.GetBalance(types.QsrTokenStandard)
	if err != nil {
		return nil, err
	}
	blocks := make([]*nom.AccountBlock, 0)
	if znnBalance.Cmp(liquidityInfo.ZnnReward) != -1 && qsrBalance.Cmp(liquidityInfo.QsrReward) != -1 {
		if liquidityInfo.ZnnReward.Sign() > 0 {
			totalZnnAmount = totalZnnAmount.Add(totalZnnAmount, liquidityInfo.ZnnReward)
			znnBurnBlock := &nom.AccountBlock{
				Address:       types.LiquidityContract,
				ToAddress:     types.TokenContract,
				Data:          definition.ABIToken.PackMethodPanic(definition.BurnMethodName),
				TokenStandard: types.ZnnTokenStandard,
				Amount:        liquidityInfo.ZnnReward,
			}
			blocks = append(blocks, znnBurnBlock)
			liquidityLog.Debug("distribute znn rewards from the liquidity contract", "znn-amount", liquidityInfo.ZnnReward)
		}

		if liquidityInfo.QsrReward.Sign() > 0 {
			totalQsrAmount = totalQsrAmount.Add(totalQsrAmount, liquidityInfo.QsrReward)
			qsrBurnBlock := &nom.AccountBlock{
				Address:       types.LiquidityContract,
				ToAddress:     types.TokenContract,
				Data:          definition.ABIToken.PackMethodPanic(definition.BurnMethodName),
				TokenStandard: types.QsrTokenStandard,
				Amount:        liquidityInfo.QsrReward,
			}
			blocks = append(blocks, qsrBurnBlock)
			liquidityLog.Debug("distribute qsr rewards from the liquidity contract", "qsr-amount", liquidityInfo.QsrReward)
		}
	}

	startTime, endTime := context.EpochTicker().ToTime(epoch)
	totalZnnFunds := big.NewInt(0)
	totalQsrFunds := big.NewInt(0)

	liquidityLog.Debug("updating liquidity stake reward", "epoch", epoch, "znn-total-amount", totalZnnAmount, "qsr-total-amount", totalQsrAmount)

	znnRewards := make(map[string]*big.Int)
	qsrRewards := make(map[string]*big.Int)

	for _, token := range liquidityInfo.TokenTuples {
		totalZnn := new(big.Int).Set(totalZnnAmount)
		totalQsr := new(big.Int).Set(totalQsrAmount)
		znnReward := totalZnn.Mul(totalZnn, big.NewInt(int64(token.ZnnPercentage)))
		znnReward = znnReward.Div(znnReward, big.NewInt(int64(constants.LiquidityZnnTotalPercentages)))
		znnRewards[token.TokenStandard] = znnReward
		qsrReward := totalQsr.Mul(totalQsr, big.NewInt(int64(token.QsrPercentage)))
		qsrReward = qsrReward.Div(qsrReward, big.NewInt(int64(constants.LiquidityQsrTotalPercentages)))
		qsrRewards[token.TokenStandard] = qsrReward

		liquidityLog.Debug("calculating percentages for each token", "epoch", epoch, "token-standard", token.TokenStandard, "znn-percentage", token.ZnnPercentage, "qsr-percentage", token.QsrPercentage, "znn-rewards", znnRewards[token.TokenStandard], "qsr-rewards", qsrRewards[token.TokenStandard])
	}
	liquidityStakeList := definition.GetAllLiquidityStakeEntries(context.Storage())

	cumulatedStake := make(map[string]*big.Int)
	for _, stakeEntry := range liquidityStakeList {
		weightedLiquidityStake := getWeightedLiquidityStake(stakeEntry, startTime.Unix(), endTime.Unix())
		currentCumulatedStake, ok := cumulatedStake[stakeEntry.TokenStandard.String()]
		if !ok {
			currentCumulatedStake = big.NewInt(0)
		}
		currentCumulatedStake.Add(currentCumulatedStake, weightedLiquidityStake)
		cumulatedStake[stakeEntry.TokenStandard.String()] = currentCumulatedStake
	}

	for _, stakeEntry := range liquidityStakeList {
		znnReward, ok := znnRewards[stakeEntry.TokenStandard.String()]
		if !ok {
			continue
		}
		qsrReward, ok := qsrRewards[stakeEntry.TokenStandard.String()]
		if !ok {
			continue
		}

		znnAmount := new(big.Int).Set(znnReward)
		qsrAmount := new(big.Int).Set(qsrReward)

		totalCumulatedStake, ok := cumulatedStake[stakeEntry.TokenStandard.String()]
		if !ok {
			continue
		}
		if totalCumulatedStake.Sign() == 0 {
			continue
		}

		weight := getWeightedLiquidityStake(stakeEntry, startTime.Unix(), endTime.Unix())
		znnAmount.Mul(znnAmount, weight)
		znnAmount.Quo(znnAmount, totalCumulatedStake)

		qsrAmount.Mul(qsrAmount, weight)
		qsrAmount.Quo(qsrAmount, totalCumulatedStake)

		addReward(context, epoch, definition.RewardDeposit{
			Address: &stakeEntry.StakeAddress,
			Znn:     znnAmount,
			Qsr:     qsrAmount,
		})

		totalZnnFunds = totalZnnFunds.Add(totalZnnFunds, znnAmount)
		totalQsrFunds = totalQsrFunds.Add(totalQsrFunds, qsrAmount)
		liquidityLog.Debug("updating liquidity stake reward", "id", stakeEntry.Id, "stake-address", stakeEntry.StakeAddress, "token-standard", stakeEntry.TokenStandard, "znn-amount", znnAmount, "qsr-amount", qsrAmount)
		if stakeEntry.RevokeTime != 0 && stakeEntry.RevokeTime < endTime.Unix() {
			common.DealWithErr(stakeEntry.Delete(context.Storage()))
		}
	}
	if totalZnnFunds.Cmp(totalZnnAmount) > 0 || totalQsrFunds.Cmp(totalQsrAmount) > 0 {
		return nil, constants.ErrInvalidRewards
	}
	if totalZnnFunds.Cmp(totalZnnAmount) < 0 {
		znnReward := new(big.Int).Set(totalZnnAmount)
		znnReward.Sub(znnReward, totalZnnFunds)
		blocks = append(blocks, &nom.AccountBlock{
			ToAddress: types.TokenContract,
			Amount:    common.Big0,
			Data: definition.ABIToken.PackMethodPanic(
				definition.MintMethodName,
				types.ZnnTokenStandard,
				znnReward,
				types.LiquidityContract,
			),
		})
		liquidityLog.Debug("updating liquidity balance", "epoch", epoch, "znnReward", znnReward)
	}
	if totalQsrFunds.Cmp(totalQsrAmount) < 0 {
		qsrReward := new(big.Int).Set(totalQsrAmount)
		qsrReward.Sub(qsrReward, totalQsrFunds)
		blocks = append(blocks, &nom.AccountBlock{
			ToAddress: types.TokenContract,
			Amount:    common.Big0,
			Data: definition.ABIToken.PackMethodPanic(
				definition.MintMethodName,
				types.QsrTokenStandard,
				qsrReward,
				types.LiquidityContract,
			),
		})
		liquidityLog.Debug("updating liquidity balance", "epoch", epoch, "qsrReward", qsrReward)
	}
	return blocks, nil
}

// updateLiquidityStakeRewards advances the epoch ratchet by at most
// one epoch — unlike updateLiquidityRewards it does not loop — and
// returns that epoch's blocks from
// computeLiquidityStakeRewardsForEpoch, or none when no epoch is due.
func updateLiquidityStakeRewards(context vm_context.AccountVmContext) ([]*nom.AccountBlock, error) {
	lastEpoch, err := definition.GetLastEpochUpdate(context.Storage())
	if err != nil {
		return nil, err
	}

	result := make([]*nom.AccountBlock, 0)

	if err := checkAndPerformUpdateEpoch(context, lastEpoch); err == constants.ErrEpochUpdateTooRecent {
		liquidityLog.Debug("invalid update - rewards not due yet", "epoch", lastEpoch.LastEpoch+1)
		return nil, nil
	} else if err != nil {
		liquidityLog.Error("unknown panic", "reason", err)
		return nil, err
	}
	if blocks, err := computeLiquidityStakeRewardsForEpoch(context, uint64(lastEpoch.LastEpoch)); err != nil {
		return nil, err
	} else if blocks != nil {
		result = append(result, blocks...)
	}
	return result, nil
}

// SetIsHalted (SetIsHalted) is the administrator method that halts
// or resumes the contract, taking effect immediately — no time
// challenge. While halted, reward runs mint the epoch emission to
// the contract itself instead of distributing it to stakers.
type SetIsHalted struct {
	MethodName string
}

// GetPlasma quotes the EmbeddedSimple tier; the call sends no
// response block.
func (p *SetIsHalted) GetPlasma(plasmaTable *constants.PlasmaTable) (uint64, error) {
	return plasmaTable.EmbeddedSimple, nil
}

// ValidateSendBlock accepts a packed bool — the new halted state —
// carried by no tokens; a non-zero amount fails with
// constants.ErrInvalidTokenOrAmount.
func (p *SetIsHalted) ValidateSendBlock(block *nom.AccountBlock) error {
	var err error

	param := new(bool)
	if err := definition.ABILiquidity.UnpackMethod(param, p.MethodName, block.Data); err != nil {
		return constants.ErrUnpackError
	}

	if block.Amount.Sign() != 0 {
		return constants.ErrInvalidTokenOrAmount
	}

	block.Data, err = definition.ABILiquidity.PackMethod(p.MethodName, *param)
	return err
}

// ReceiveBlock saves the new IsHalted flag in the LiquidityInfo. The
// sender must be the administrator, else
// constants.ErrPermissionDenied. No descendant blocks are emitted.
func (p *SetIsHalted) ReceiveBlock(context vm_context.AccountVmContext, sendBlock *nom.AccountBlock) ([]*nom.AccountBlock, error) {
	if err := p.ValidateSendBlock(sendBlock); err != nil {
		return nil, err
	}

	param := new(bool)
	if err := definition.ABILiquidity.UnpackMethod(param, p.MethodName, sendBlock.Data); err != nil {
		return nil, constants.ErrUnpackError
	}

	liquidityInfo, err := definition.GetLiquidityInfo(context.Storage())
	if err != nil {
		return nil, err
	}
	if sendBlock.Address.String() != liquidityInfo.Administrator.String() {
		return nil, constants.ErrPermissionDenied
	}

	liquidityInfo.IsHalted = *param
	liquidityInfoVariable, err := definition.EncodeLiquidityInfo(liquidityInfo)
	if err != nil {
		return nil, err
	}
	common.DealWithErr(liquidityInfoVariable.Save(context.Storage()))
	return nil, nil
}

// UnlockLiquidityStakeEntries (UnlockLiquidityStakeEntries) is the
// administrator method that expires every active stake entry of one
// token immediately, letting holders cancel and withdraw without
// waiting out their chosen durations — typically when a tuple is
// retired from the reward configuration.
type UnlockLiquidityStakeEntries struct {
	MethodName string
}

// GetPlasma quotes the EmbeddedSimple tier; the call sends no
// response block.
func (p *UnlockLiquidityStakeEntries) GetPlasma(plasmaTable *constants.PlasmaTable) (uint64, error) {
	return plasmaTable.EmbeddedSimple, nil
}

// ValidateSendBlock accepts an argument-less call carrying a zero
// amount; the token whose entries to unlock is given by the send
// block's TokenStandard field rather than an ABI argument.
func (p *UnlockLiquidityStakeEntries) ValidateSendBlock(block *nom.AccountBlock) error {
	var err error

	if err := definition.ABILiquidity.UnpackEmptyMethod(p.MethodName, block.Data); err != nil {
		return constants.ErrUnpackError
	}

	if block.Amount.Sign() != 0 {
		return constants.ErrInvalidTokenOrAmount
	}

	block.Data, err = definition.ABILiquidity.PackMethod(p.MethodName)
	return err
}

// ReceiveBlock brings the expiration time of every entry of the send
// block's token standard forward to the frontier momentum's
// timestamp, leaving already-expired entries untouched. The sender
// must be the administrator, else constants.ErrPermissionDenied. No
// descendant blocks are emitted.
func (p *UnlockLiquidityStakeEntries) ReceiveBlock(context vm_context.AccountVmContext, sendBlock *nom.AccountBlock) ([]*nom.AccountBlock, error) {
	if err := p.ValidateSendBlock(sendBlock); err != nil {
		return nil, err
	}

	liquidityInfo, err := definition.GetLiquidityInfo(context.Storage())
	if err != nil {
		return nil, err
	}

	if sendBlock.Address.String() != liquidityInfo.Administrator.String() {
		return nil, constants.ErrPermissionDenied
	}

	liquidityStakeList := definition.GetAllLiquidityStakeEntries(context.Storage())
	momentum, _ := context.GetFrontierMomentum()
	for _, entry := range liquidityStakeList {
		if entry.TokenStandard.String() == sendBlock.TokenStandard.String() {
			if entry.ExpirationTime > momentum.Timestamp.Unix() {
				entry.ExpirationTime = momentum.Timestamp.Unix()
				common.DealWithErr(entry.Save(context.Storage()))
			}
		}
	}
	return nil, nil
}

// SetAdditionalReward (SetAdditionalReward) is the administrator
// method that sets the extra ZNN and QSR amounts distributed to
// stakers each epoch from the contract's own balance, on top of the
// network emission — see computeLiquidityStakeRewardsForEpoch. The
// change is protected by a soft-delay time challenge.
type SetAdditionalReward struct {
	MethodName string
}

// GetPlasma quotes the EmbeddedSimple tier; the call sends no
// response block.
func (p *SetAdditionalReward) GetPlasma(plasmaTable *constants.PlasmaTable) (uint64, error) {
	return plasmaTable.EmbeddedSimple, nil
}

// ValidateSendBlock accepts a packed
// definition.SetAdditionalRewardParam (ZNN and QSR amounts) carrying
// no tokens; a non-zero amount fails with
// constants.ErrInvalidTokenOrAmount.
func (p *SetAdditionalReward) ValidateSendBlock(block *nom.AccountBlock) error {
	var err error

	param := new(definition.SetAdditionalRewardParam)
	if err := definition.ABILiquidity.UnpackMethod(param, p.MethodName, block.Data); err != nil {
		return constants.ErrUnpackError
	}

	if block.Amount.Sign() != 0 {
		return constants.ErrInvalidTokenOrAmount
	}

	block.Data, err = definition.ABILiquidity.PackMethod(p.MethodName, param.ZnnReward, param.QsrReward)
	return err
}

// ReceiveBlock saves the new reward amounts in the LiquidityInfo.
// The sender must be the administrator, else
// constants.ErrPermissionDenied. The change passes a TimeChallenge
// over the packed amounts with the security info's SoftDelay: the
// first call only records the challenge and the amounts are saved
// when the call is repeated with identical parameters after the
// delay.
func (p *SetAdditionalReward) ReceiveBlock(context vm_context.AccountVmContext, sendBlock *nom.AccountBlock) ([]*nom.AccountBlock, error) {
	if err := p.ValidateSendBlock(sendBlock); err != nil {
		return nil, err
	}

	param := new(definition.SetAdditionalRewardParam)
	err := definition.ABILiquidity.UnpackMethod(param, p.MethodName, sendBlock.Data)
	if err != nil {
		return nil, err
	}
	liquidityInfo, err := definition.GetLiquidityInfo(context.Storage())
	if err != nil {
		return nil, err
	}

	if sendBlock.Address.String() != liquidityInfo.Administrator.String() {
		return nil, constants.ErrPermissionDenied
	}

	args := eabi.Arguments{{Type: definition.Uint256Ty}, {Type: definition.Uint256Ty}}
	values := make([]interface{}, 0)
	values = append(values,
		big.NewInt(0).Set(param.ZnnReward),
		big.NewInt(0).Set(param.QsrReward),
	)
	messageBytes, err := args.PackValues(values)
	if err != nil {
		return nil, err
	}
	paramsHash := crypto.Hash(messageBytes)

	securityInfo, err := definition.GetSecurityInfoVariable(context.Storage())
	if err != nil {
		return nil, err
	}
	if timeChallengeInfo, errTimeChallenge := TimeChallenge(context, p.MethodName, paramsHash, securityInfo.SoftDelay); errTimeChallenge != nil {
		return nil, errTimeChallenge
	} else {
		// if paramsHash is not zero it means we had a new challenge and we can't go further to save the change into local db
		if !timeChallengeInfo.ParamsHash.IsZero() {
			return nil, nil
		}
	}

	liquidityInfo.ZnnReward = param.ZnnReward
	liquidityInfo.QsrReward = param.QsrReward
	liquidityInfoVariable, err := definition.EncodeLiquidityInfo(liquidityInfo)
	if err != nil {
		return nil, err
	}
	common.DealWithErr(liquidityInfoVariable.Save(context.Storage()))
	return nil, nil
}

// ChangeAdministratorLiquidity (ChangeAdministrator) is the
// administrator method that hands the role to another address,
// protected by an administrator-delay time challenge; the liquidity
// counterpart of the bridge's ChangeAdministratorMethod.
type ChangeAdministratorLiquidity struct {
	MethodName string
}

// GetPlasma quotes the EmbeddedSimple tier; the call sends no
// response block.
func (p *ChangeAdministratorLiquidity) GetPlasma(plasmaTable *constants.PlasmaTable) (uint64, error) {
	return plasmaTable.EmbeddedSimple, nil
}

// ValidateSendBlock accepts a packed types.Address carrying no
// tokens. The address is re-parsed to verify its checksum, which the
// ABI alone does not, and must not be the zero address, else
// constants.ErrForbiddenParam.
func (p *ChangeAdministratorLiquidity) ValidateSendBlock(block *nom.AccountBlock) error {
	var err error

	address := new(types.Address)
	if err = definition.ABILiquidity.UnpackMethod(address, p.MethodName, block.Data); err != nil {
		return constants.ErrUnpackError
	}

	if block.Amount.Sign() != 0 {
		return constants.ErrInvalidTokenOrAmount
	}

	// we also check with this method because in the abi the checksum is not verified
	parsedAddress, err := types.ParseAddress(address.String())
	if err != nil {
		return err
	} else if parsedAddress.IsZero() {
		return constants.ErrForbiddenParam
	}

	block.Data, err = definition.ABILiquidity.PackMethod(p.MethodName, address)
	return err
}

// ReceiveBlock replaces the administrator in the LiquidityInfo.
// Security must be initialized (CheckSecurityInitialized) and the
// sender must be the current administrator, else
// constants.ErrPermissionDenied. The change passes a TimeChallenge
// over the new address with the security info's AdministratorDelay:
// the first call only records the challenge and the handover happens
// when the call is repeated with the same address after the delay.
func (p *ChangeAdministratorLiquidity) ReceiveBlock(context vm_context.AccountVmContext, sendBlock *nom.AccountBlock) ([]*nom.AccountBlock, error) {
	if err := p.ValidateSendBlock(sendBlock); err != nil {
		return nil, err
	}

	address := new(types.Address)
	err := definition.ABILiquidity.UnpackMethod(address, p.MethodName, sendBlock.Data)
	if err != nil {
		return nil, err
	}

	if _, errSec := CheckSecurityInitialized(context); errSec != nil {
		return nil, errSec
	}

	liquidityInfo, err := definition.GetLiquidityInfo(context.Storage())
	if err != nil {
		return nil, err
	}

	if sendBlock.Address.String() != liquidityInfo.Administrator.String() {
		return nil, constants.ErrPermissionDenied
	}

	securityInfo, err := definition.GetSecurityInfoVariable(context.Storage())
	if err != nil {
		return nil, err
	}
	paramsHash := crypto.Hash(address.Bytes())
	if timeChallengeInfo, errTimeChallenge := TimeChallenge(context, p.MethodName, paramsHash, securityInfo.AdministratorDelay); errTimeChallenge != nil {
		return nil, errTimeChallenge
	} else {
		// if paramsHash is not zero it means we had a new challenge and we can't go further to save the change into local db
		if !timeChallengeInfo.ParamsHash.IsZero() {
			return nil, nil
		}
	}

	err = liquidityInfo.Administrator.SetBytes(address.Bytes())
	if err != nil {
		return nil, err
	}

	liquidityInfoVariable, err := definition.EncodeLiquidityInfo(liquidityInfo)
	if err != nil {
		return nil, err
	}
	common.DealWithErr(liquidityInfoVariable.Save(context.Storage()))
	return nil, nil
}

// NominateGuardiansLiquidity (NominateGuardians) is the
// administrator method that installs the contract's guardian set —
// the addresses able to elect a new administrator after an emergency
// — protected by an administrator-delay time challenge; the
// liquidity counterpart of the bridge's NominateGuardiansMethod.
type NominateGuardiansLiquidity struct {
	MethodName string
}

// GetPlasma quotes the EmbeddedSimple tier; the call sends no
// response block.
func (p *NominateGuardiansLiquidity) GetPlasma(plasmaTable *constants.PlasmaTable) (uint64, error) {
	return plasmaTable.EmbeddedSimple, nil
}

// ValidateSendBlock accepts a packed slice of at least
// constants.MinGuardians addresses (constants.ErrInvalidGuardians
// otherwise) carrying no tokens. Each address is re-parsed to verify
// its checksum, which the ABI alone does not, and must not be the
// zero address, else constants.ErrForbiddenParam.
func (p *NominateGuardiansLiquidity) ValidateSendBlock(block *nom.AccountBlock) error {
	var err error

	guardians := new([]types.Address)
	if err := definition.ABILiquidity.UnpackMethod(guardians, p.MethodName, block.Data); err != nil {
		return constants.ErrUnpackError
	}

	if block.Amount.Sign() != 0 {
		return constants.ErrInvalidTokenOrAmount
	}

	if len(*guardians) < constants.MinGuardians {
		return constants.ErrInvalidGuardians
	}
	for _, address := range *guardians {
		// we also check with this method because in the abi the checksum is not verified
		parsedAddress, err := types.ParseAddress(address.String())
		if err != nil {
			return err
		} else if parsedAddress.IsZero() {
			return constants.ErrForbiddenParam
		}
	}

	block.Data, err = definition.ABILiquidity.PackMethod(p.MethodName, guardians)
	return err
}

// ReceiveBlock replaces the guardian set in the SecurityInfo,
// resetting all guardian votes to empty. The sender must be the
// administrator, else constants.ErrPermissionDenied. The guardians
// are sorted by address string before hashing and storing, making
// the time challenge order-insensitive; the change passes a
// TimeChallenge with the security info's AdministratorDelay and is
// applied when the call is repeated with the same set after the
// delay.
func (p *NominateGuardiansLiquidity) ReceiveBlock(context vm_context.AccountVmContext, sendBlock *nom.AccountBlock) ([]*nom.AccountBlock, error) {
	if err := p.ValidateSendBlock(sendBlock); err != nil {
		return nil, err
	}

	guardians := new([]types.Address)
	err := definition.ABILiquidity.UnpackMethod(guardians, p.MethodName, sendBlock.Data)
	if err != nil {
		return nil, err
	}

	liquidityInfo, err := definition.GetLiquidityInfo(context.Storage())
	if err != nil {
		return nil, err
	}

	if sendBlock.Address.String() != liquidityInfo.Administrator.String() {
		return nil, constants.ErrPermissionDenied
	}

	securityInfo, err := definition.GetSecurityInfoVariable(context.Storage())
	if err != nil {
		return nil, err
	}

	sort.Slice(*guardians, func(i, j int) bool {
		return (*guardians)[i].String() < (*guardians)[j].String()
	})

	guardiansBytes := make([]byte, 0)
	for _, g := range *guardians {
		guardiansBytes = append(guardiansBytes, g.Bytes()...)
	}
	paramsHash := crypto.Hash(guardiansBytes)
	if timeChallengeInfo, errTimeChallenge := TimeChallenge(context, p.MethodName, paramsHash, securityInfo.AdministratorDelay); errTimeChallenge != nil {
		return nil, errTimeChallenge
	} else {
		// if paramsHash is not zero it means we had a new challenge and we can't go further to save the change into local db
		if !timeChallengeInfo.ParamsHash.IsZero() {
			return nil, nil
		}
	}

	securityInfo.Guardians = make([]types.Address, 0)
	securityInfo.GuardiansVotes = make([]types.Address, 0)
	for _, guardian := range *guardians {
		securityInfo.Guardians = append(securityInfo.Guardians, guardian)
		// append empty vote
		securityInfo.GuardiansVotes = append(securityInfo.GuardiansVotes, types.Address{})
	}

	common.DealWithErr(securityInfo.Save(context.Storage()))
	return nil, nil
}

// ProposeAdministratorLiquidity (ProposeAdministrator) is the
// guardian method that votes for a new administrator while the
// contract is in emergency (administrator zeroed); the liquidity
// counterpart of the bridge's ProposeAdministratorMethod.
type ProposeAdministratorLiquidity struct {
	MethodName string
}

// GetPlasma quotes the EmbeddedSimple tier; the call sends no
// response block.
func (p *ProposeAdministratorLiquidity) GetPlasma(plasmaTable *constants.PlasmaTable) (uint64, error) {
	return plasmaTable.EmbeddedSimple, nil
}

// ValidateSendBlock accepts a packed types.Address carrying no
// tokens. The address is re-parsed to verify its checksum, which the
// ABI alone does not, and must not be the zero address, else
// constants.ErrForbiddenParam.
func (p *ProposeAdministratorLiquidity) ValidateSendBlock(block *nom.AccountBlock) error {
	var err error

	address := new(types.Address)
	if err := definition.ABILiquidity.UnpackMethod(address, p.MethodName, block.Data); err != nil {
		return constants.ErrUnpackError
	}

	if block.Amount.Sign() != 0 {
		return constants.ErrInvalidTokenOrAmount
	}

	// we also check with this method because in the abi the checksum is not verified
	parsedAddress, err := types.ParseAddress(address.String())
	if err != nil {
		return err
	} else if parsedAddress.IsZero() {
		return constants.ErrForbiddenParam
	}

	block.Data, err = definition.ABILiquidity.PackMethod(p.MethodName, *address)
	return err
}

// ReceiveBlock records the guardian sender's vote, overwriting its
// previous one. The administrator must be the zero address
// (constants.ErrNotEmergency otherwise) and the sender a guardian,
// else constants.ErrNotGuardian. Once a proposed address gathers the
// votes of a strict majority of guardian slots it becomes the new
// administrator and all votes are reset; as with the bridge's
// machinery, a duplicated guardian address still casts one vote but
// widens the majority threshold. No descendant blocks are emitted.
func (p *ProposeAdministratorLiquidity) ReceiveBlock(context vm_context.AccountVmContext, sendBlock *nom.AccountBlock) ([]*nom.AccountBlock, error) {
	if err := p.ValidateSendBlock(sendBlock); err != nil {
		return nil, err
	}

	proposedAddress := new(types.Address)
	if err := definition.ABILiquidity.UnpackMethod(proposedAddress, p.MethodName, sendBlock.Data); err != nil {
		return nil, constants.ErrUnpackError
	}

	liquidityInfo, err := definition.GetLiquidityInfo(context.Storage())
	if err != nil {
		return nil, err
	}

	if !liquidityInfo.Administrator.IsZero() {
		return nil, constants.ErrNotEmergency
	}

	securityInfo, err := definition.GetSecurityInfoVariable(context.Storage())
	if err != nil {
		return nil, err
	}

	found := false
	for idx, guardian := range securityInfo.Guardians {
		if bytes.Equal(guardian.Bytes(), sendBlock.Address.Bytes()) {
			found = true
			if err := securityInfo.GuardiansVotes[idx].SetBytes(proposedAddress.Bytes()); err != nil {
				return nil, err
			}
			break
		}
	}
	if !found {
		return nil, constants.ErrNotGuardian
	}

	votes := make(map[string]uint8)

	threshold := uint8(len(securityInfo.Guardians) / 2)
	for _, vote := range securityInfo.GuardiansVotes {
		if !vote.IsZero() {
			votes[vote.String()] += 1
			// we got a majority, so we change the administrator pub key
			if votes[vote.String()] > threshold {
				votedAddress, errParse := types.ParseAddress(vote.String())
				if errParse != nil {
					return nil, errParse
				} else if votedAddress.IsZero() {
					return nil, constants.ErrForbiddenParam
				}
				if errSet := liquidityInfo.Administrator.SetBytes(votedAddress.Bytes()); errSet != nil {
					return nil, errSet
				}
				liquidityInfoVariable, err := definition.EncodeLiquidityInfo(liquidityInfo)
				if err != nil {
					return nil, err
				}
				common.DealWithErr(liquidityInfoVariable.Save(context.Storage()))
				for idx, _ := range securityInfo.GuardiansVotes {
					securityInfo.GuardiansVotes[idx] = types.Address{}
				}
				break
			}
		}
	}
	common.DealWithErr(securityInfo.Save(context.Storage()))
	return nil, nil
}

// EmergencyLiquidity (Emergency) is the administrator's kill switch:
// it renounces the administrator role and halts the contract in a
// single, immediate call. Control can only be restored by the
// guardians through ProposeAdministrator; the liquidity counterpart
// of the bridge's EmergencyMethod.
type EmergencyLiquidity struct {
	MethodName string
}

// GetPlasma quotes the EmbeddedSimple tier; the call sends no
// response block.
func (p *EmergencyLiquidity) GetPlasma(plasmaTable *constants.PlasmaTable) (uint64, error) {
	return plasmaTable.EmbeddedSimple, nil
}

// ValidateSendBlock accepts an argument-less call carrying no
// tokens: extra ABI arguments fail with constants.ErrUnpackError and
// a non-zero Amount with constants.ErrInvalidTokenOrAmount.
func (p *EmergencyLiquidity) ValidateSendBlock(block *nom.AccountBlock) error {
	var err error
	if err := definition.ABILiquidity.UnpackEmptyMethod(p.MethodName, block.Data); err != nil {
		return constants.ErrUnpackError
	}

	if block.Amount.Sign() != 0 {
		return constants.ErrInvalidTokenOrAmount
	}

	block.Data, err = definition.ABILiquidity.PackMethod(p.MethodName)
	return err
}

// ReceiveBlock zeroes the administrator and sets IsHalted in the
// LiquidityInfo. Security must be initialized
// (CheckSecurityInitialized) and the sender must be the
// administrator, else constants.ErrPermissionDenied. There is no
// time challenge — the emergency takes effect at once. No descendant
// blocks are emitted.
func (p *EmergencyLiquidity) ReceiveBlock(context vm_context.AccountVmContext, sendBlock *nom.AccountBlock) ([]*nom.AccountBlock, error) {
	if err := p.ValidateSendBlock(sendBlock); err != nil {
		return nil, err
	}

	err := definition.ABILiquidity.UnpackEmptyMethod(p.MethodName, sendBlock.Data)
	if err != nil {
		return nil, err
	}

	if _, err := CheckSecurityInitialized(context); err != nil {
		return nil, err
	}

	liquidityInfo, err := definition.GetLiquidityInfo(context.Storage())
	if err != nil {
		return nil, err
	}

	if sendBlock.Address.String() != liquidityInfo.Administrator.String() {
		return nil, constants.ErrPermissionDenied
	}

	if errSet := liquidityInfo.Administrator.SetBytes(types.ZeroAddress.Bytes()); errSet != nil {
		return nil, errSet
	}
	liquidityInfo.IsHalted = true

	liquidityInfoVariable, err := definition.EncodeLiquidityInfo(liquidityInfo)
	if err != nil {
		return nil, err
	}
	common.DealWithErr(liquidityInfoVariable.Save(context.Storage()))
	return nil, nil
}
