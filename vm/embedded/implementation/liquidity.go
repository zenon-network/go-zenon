package implementation

import (
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
		if err := checkAndPerformUpdateEpoch(context, lastEpoch); err == constants.ErrEpochUpdateTooRecent {
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
