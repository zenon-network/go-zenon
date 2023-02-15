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

	block.Data, err = definition.ABILiquidity.PackMethod(p.MethodName, param.TokenStandards, param.ZnnPercentages, param.QsrPercentages, param.MinAmounts)
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

	if sendBlock.Address.String() != liquidityInfo.Administrator.String() {
		return nil, constants.ErrPermissionDenied
	}

	liquidityInfo.TokenTuples = make([]definition.TokenTuple, 0)
	for i := 0; i < len(param.TokenStandards); i++ {
		tokenStandard := param.TokenStandards[i]
		znnPercentage := param.ZnnPercentages[i]
		qsrPercentage := param.QsrPercentages[i]
		minAmount := param.MinAmounts[i]
		tokenTuple := definition.TokenTuple{
			TokenStandard: tokenStandard,
			ZnnPercentage: znnPercentage,
			QsrPercentage: qsrPercentage,
			MinAmount:     minAmount,
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
	period := stakingTime / constants.StakeTimeUnitSec
	weighted := big.NewInt(constants.LiquidityStakeWeights[period])
	weighted.Mul(weighted, amount)
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
	var token definition.TokenTuple
	for _, tokenTuple := range liquidityInfo.TokenTuples {
		if tokenTuple.TokenStandard == sendBlock.TokenStandard.String() {
			found = true
			token = tokenTuple
			break
		}
	}
	if !found {
		return nil, constants.ErrInvalidToken
	} else {
		if sendBlock.Amount.Cmp(token.MinAmount) == -1 {
			return nil, constants.ErrInvalidTokenOrAmount
		}
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

type CancelLiquidityStakeMethod struct {
	MethodName string
}

func (p *CancelLiquidityStakeMethod) GetPlasma(plasmaTable *constants.PlasmaTable) (uint64, error) {
	return plasmaTable.EmbeddedWWithdraw, nil
}
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

type UpdateRewardEmbeddedLiquidityMethod struct {
	MethodName string
}

func (method *UpdateRewardEmbeddedLiquidityMethod) GetPlasma(plasmaTable *constants.PlasmaTable) (uint64, error) {
	return plasmaTable.EmbeddedSimple, nil
}
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

// weighted liquidity stake amount over time
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
		znnAmount.Mul(znnAmount, getWeightedLiquidityStake(stakeEntry, startTime.Unix(), endTime.Unix()))
		znnAmount.Quo(znnAmount, totalCumulatedStake)

		qsrAmount.Mul(qsrAmount, getWeightedLiquidityStake(stakeEntry, startTime.Unix(), endTime.Unix()))
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

func updateLiquidityStakeRewards(context vm_context.AccountVmContext) ([]*nom.AccountBlock, error) {
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
		if blocks, err := computeLiquidityStakeRewardsForEpoch(context, uint64(lastEpoch.LastEpoch)); err != nil {
			return nil, err
		} else if blocks != nil {
			result = append(result, blocks...)
		}
	}
}

type StopLiquidityStake struct {
	MethodName string
}

func (p *StopLiquidityStake) GetPlasma(plasmaTable *constants.PlasmaTable) (uint64, error) {
	return plasmaTable.EmbeddedSimple, nil
}
func (p *StopLiquidityStake) ValidateSendBlock(block *nom.AccountBlock) error {
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
func (p *StopLiquidityStake) ReceiveBlock(context vm_context.AccountVmContext, sendBlock *nom.AccountBlock) ([]*nom.AccountBlock, error) {
	if err := p.ValidateSendBlock(sendBlock); err != nil {
		return nil, err
	}

	err := definition.ABILiquidity.UnpackEmptyMethod(p.MethodName, sendBlock.Data)
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

	liquidityInfo.IsHalted = true

	liquidityInfoVariable, err := definition.EncodeLiquidityInfo(liquidityInfo)
	if err != nil {
		return nil, err
	}
	common.DealWithErr(liquidityInfoVariable.Save(context.Storage()))
	return nil, nil
}

type StartLiquidityStake struct {
	MethodName string
}

func (p *StartLiquidityStake) GetPlasma(plasmaTable *constants.PlasmaTable) (uint64, error) {
	return plasmaTable.EmbeddedSimple, nil
}
func (p *StartLiquidityStake) ValidateSendBlock(block *nom.AccountBlock) error {
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
func (p *StartLiquidityStake) ReceiveBlock(context vm_context.AccountVmContext, sendBlock *nom.AccountBlock) ([]*nom.AccountBlock, error) {
	if err := p.ValidateSendBlock(sendBlock); err != nil {
		return nil, err
	}

	err := definition.ABILiquidity.UnpackEmptyMethod(p.MethodName, sendBlock.Data)
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

	liquidityInfo.IsHalted = false

	liquidityInfoVariable, err := definition.EncodeLiquidityInfo(liquidityInfo)
	if err != nil {
		return nil, err
	}
	common.DealWithErr(liquidityInfoVariable.Save(context.Storage()))
	return nil, nil
}

type UnlockLiquidityStakeEntries struct {
	MethodName string
}

func (p *UnlockLiquidityStakeEntries) GetPlasma(plasmaTable *constants.PlasmaTable) (uint64, error) {
	return plasmaTable.EmbeddedSimple, nil
}
func (p *UnlockLiquidityStakeEntries) ValidateSendBlock(block *nom.AccountBlock) error {
	var err error

	tokenStandard := types.ZnnTokenStandard
	if err := definition.ABILiquidity.UnpackMethod(tokenStandard, p.MethodName, block.Data); err != nil {
		return constants.ErrUnpackError
	}

	if block.Amount.Sign() != 0 {
		return constants.ErrInvalidTokenOrAmount
	}

	block.Data, err = definition.ABILiquidity.PackMethod(p.MethodName, tokenStandard)
	return err
}
func (p *UnlockLiquidityStakeEntries) ReceiveBlock(context vm_context.AccountVmContext, sendBlock *nom.AccountBlock) ([]*nom.AccountBlock, error) {
	if err := p.ValidateSendBlock(sendBlock); err != nil {
		return nil, err
	}

	tokenStandard := types.ZnnTokenStandard
	err := definition.ABILiquidity.UnpackMethod(tokenStandard, p.MethodName, sendBlock.Data)
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

	liquidityStakeList := definition.GetAllLiquidityStakeEntries(context.Storage())
	momentum, _ := context.GetFrontierMomentum()
	for _, entry := range liquidityStakeList {
		if entry.TokenStandard.String() == tokenStandard.String() {
			if entry.ExpirationTime > momentum.Timestamp.Unix() {
				entry.ExpirationTime = momentum.Timestamp.Unix()
				common.DealWithErr(entry.Save(context.Storage()))
			}
		}
	}
	return nil, nil
}

type SetAdditionalReward struct {
	MethodName string
}

func (p *SetAdditionalReward) GetPlasma(plasmaTable *constants.PlasmaTable) (uint64, error) {
	return plasmaTable.EmbeddedSimple, nil
}
func (p *SetAdditionalReward) ValidateSendBlock(block *nom.AccountBlock) error {
	var err error

	param := new(definition.SetAdditionalRewardParam)
	if err := definition.ABILiquidity.UnpackMethod(param, p.MethodName, block.Data); err != nil {
		return constants.ErrUnpackError
	}

	if block.Amount.Sign() != 0 {
		return constants.ErrInvalidTokenOrAmount
	}

	if param.ZnnReward.Sign() == -1 || param.QsrReward.Sign() == -1 {
		return constants.ErrForbiddenParam
	}

	block.Data, err = definition.ABILiquidity.PackMethod(p.MethodName, param.ZnnReward, param.QsrReward)
	return err
}
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

	liquidityInfo.ZnnReward = param.ZnnReward
	liquidityInfo.QsrReward = param.QsrReward
	liquidityInfoVariable, err := definition.EncodeLiquidityInfo(liquidityInfo)
	if err != nil {
		return nil, err
	}
	common.DealWithErr(liquidityInfoVariable.Save(context.Storage()))
	return nil, nil
}

type ChangeLiquidityAdministrator struct {
	MethodName string
}

func (p *ChangeLiquidityAdministrator) GetPlasma(plasmaTable *constants.PlasmaTable) (uint64, error) {
	return plasmaTable.EmbeddedSimple, nil
}
func (p *ChangeLiquidityAdministrator) ValidateSendBlock(block *nom.AccountBlock) error {
	var err error

	address := new(types.Address)
	if err = definition.ABILiquidity.UnpackMethod(address, p.MethodName, block.Data); err != nil {
		return constants.ErrUnpackError
	}

	if block.Amount.Sign() != 0 {
		return constants.ErrInvalidTokenOrAmount
	}
	block.Data, err = definition.ABILiquidity.PackMethod(p.MethodName, address)
	return err
}
func (p *ChangeLiquidityAdministrator) ReceiveBlock(context vm_context.AccountVmContext, sendBlock *nom.AccountBlock) ([]*nom.AccountBlock, error) {
	if err := p.ValidateSendBlock(sendBlock); err != nil {
		return nil, err
	}

	address := new(types.Address)
	err := definition.ABILiquidity.UnpackMethod(address, p.MethodName, sendBlock.Data)
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
