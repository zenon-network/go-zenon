package implementation

import (
	"encoding/base64"
	"math/big"

	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/vm/constants"
	"github.com/zenon-network/go-zenon/vm/embedded/definition"
	"github.com/zenon-network/go-zenon/vm/vm_context"
)

var (
	liquidityLog = common.EmbeddedLogger.New("contract", "liquidity")
)

type UpdateEmbeddedLiquidityMethod struct {
	MethodName string
}

func (method *UpdateEmbeddedLiquidityMethod) GetPlasma(plasmaTable *constants.PlasmaTable) (uint64, error) {
	return plasmaTable.EmbeddedSimple, nil
}
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

type FundMethod struct {
	MethodName string
}

func (p *FundMethod) Fee() (*big.Int, error) {
	return big.NewInt(0), nil
}
func (p *FundMethod) GetPlasma(plasmaTable *constants.PlasmaTable) (uint64, error) {
	return plasmaTable.EmbeddedSimple, nil
}
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

type BurnZnnMethod struct {
	MethodName string
}

func (p *BurnZnnMethod) Fee() (*big.Int, error) {
	return big.NewInt(0), nil
}
func (p *BurnZnnMethod) GetPlasma(plasmaTable *constants.PlasmaTable) (uint64, error) {
	return plasmaTable.EmbeddedSimple, nil
}
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

type SetTokenTupleMethod struct {
	MethodName string
}

func (p *SetTokenTupleMethod) GetPlasma(plasmaTable *constants.PlasmaTable) (uint64, error) {
	return plasmaTable.EmbeddedSimple, nil
}
func (p *SetTokenTupleMethod) ValidateSendBlock(block *nom.AccountBlock) error {
	var err error
	param := new(definition.TokenTuplesParam)

	if err := definition.ABILiquidity.UnpackMethod(param, p.MethodName, block.Data); err != nil {
		return constants.ErrUnpackError
	}

	if block.Amount.Sign() != 0 {
		return constants.ErrInvalidTokenOrAmount
	}

	totalZnn := uint32(0)
	for _, percentage := range param.ZnnPercentages {
		totalZnn += percentage
	}
	if totalZnn != constants.LiquidityZnnTotalPercentages {
		return constants.ErrInvalidPercentages
	}

	totalQsr := uint32(0)
	for _, percentage := range param.QsrPercentages {
		totalQsr += percentage
	}
	if totalQsr != constants.LiquidityQsrTotalPercentages {
		return constants.ErrInvalidPercentages
	}

	if len(param.TokenStandards) != len(param.ZnnPercentages) || len(param.TokenStandards) != len(param.QsrPercentages) || len(param.ZnnPercentages) != len(param.QsrPercentages) {
		return constants.ErrInvalidArguments
	}

	block.Data, err = definition.ABILiquidity.PackMethod(p.MethodName, param.TokenStandards, param.ZnnPercentages, param.QsrPercentages)
	return err
}
func (p *SetTokenTupleMethod) ReceiveBlock(context vm_context.AccountVmContext, sendBlock *nom.AccountBlock) ([]*nom.AccountBlock, error) {
	if err := p.ValidateSendBlock(sendBlock); err != nil {
		return nil, err
	}

	param := new(definition.TokenTuplesParam)
	err := definition.ABILiquidity.UnpackMethod(param, p.MethodName, sendBlock.Data)
	if err != nil {
		return nil, err
	}

	liquidityInfo, err := definition.GetLiquidityInfo(context.Storage())
	if err != nil {
		return nil, err
	}

	senderPubKey := base64.StdEncoding.EncodeToString(sendBlock.PublicKey)
	if senderPubKey != liquidityInfo.AdministratorPubKey {
		return nil, constants.ErrPermissionDenied
	}

	liquidityInfo.TokenTuples = make([]definition.TokenTuple, 0)
	for i := 0; i < len(param.TokenStandards); i++ {
		tokenStandard := param.TokenStandards[i]
		znnPercentage := param.ZnnPercentages[i]
		qsrPercentage := param.QsrPercentages[i]
		tokenTuple := definition.TokenTuple{
			TokenStandard: tokenStandard,
			ZnnPercentage: znnPercentage,
			QsrPercentage: qsrPercentage,
		}
		liquidityInfo.TokenTuples = append(liquidityInfo.TokenTuples, tokenTuple)
	}

	liquidityInfoVariable, err := definition.EncodeLiquidityInfo(liquidityInfo)
	if err != nil {
		return nil, err
	}
	common.DealWithErr(liquidityInfoVariable.Save(context.Storage()))
	return nil, nil
}

type LiquidityStakeMethod struct {
	MethodName string
}

func getWeightedLiquidityStakeAmount(amount *big.Int, stakingTime int64) *big.Int {
	weighted := big.NewInt(9 + stakingTime/constants.StakeTimeUnitSec)
	weighted.Mul(weighted, amount)
	weighted.Div(weighted, big.NewInt(10))
	return weighted
}

func (p *LiquidityStakeMethod) GetPlasma(plasmaTable *constants.PlasmaTable) (uint64, error) {
	return plasmaTable.EmbeddedSimple, nil
}
func (p *LiquidityStakeMethod) ValidateSendBlock(block *nom.AccountBlock) error {
	var err error
	var stakeTime int64

	if err := definition.ABILiquidity.UnpackMethod(&stakeTime, p.MethodName, block.Data); err != nil {
		return constants.ErrUnpackError
	}

	if block.Amount.Cmp(constants.StakeMinAmount) == -1 {
		return constants.ErrInvalidTokenOrAmount
	}
	if stakeTime < constants.StakeTimeMinSec || stakeTime > constants.StakeTimeMaxSec || stakeTime%constants.StakeTimeUnitSec != 0 {
		return constants.ErrInvalidStakingPeriod
	}

	block.Data, err = definition.ABILiquidity.PackMethod(p.MethodName, stakeTime)
	return err
}
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
