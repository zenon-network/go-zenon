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
		if znnBalance.Cmp(param.ZnnReward) > 0 && qsrBalance.Cmp(param.QsrReward) > 0 {
			znnReward := &nom.AccountBlock{
				Address:       types.LiquidityContract,
				ToAddress:     types.AcceleratorContract,
				Data:          definition.ABICommon.PackMethodPanic(definition.DonateMethodName),
				TokenStandard: types.ZnnTokenStandard,
				Amount:        param.ZnnReward,
			}
			blocks = append(blocks, znnReward)
			znnBalance = znnBalance.Sub(znnBalance, znnReward.Amount)
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
		if znnBalance.Cmp(param.BurnAmount) > 0 {
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
