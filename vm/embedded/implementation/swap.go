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
	swapLog = common.EmbeddedLogger.New("contract", "swap")
)

func ApplyDecay(deposit *definition.SwapAssets, currentEpoch int) {
	percentageToGive := 100
	if currentEpoch < constants.SwapAssetDecayEpochsOffset {
		percentageToGive = 100
	} else {
		numTicks := (currentEpoch - constants.SwapAssetDecayEpochsOffset + 1) / constants.SwapAssetDecayTickEpochs
		decayFactor := constants.SwapAssetDecayTickValuePercentage * numTicks
		if decayFactor > 100 {
			percentageToGive = 0
		} else {
			percentageToGive = 100 - decayFactor
		}
	}

	deposit.Znn.Mul(deposit.Znn, big.NewInt(int64(percentageToGive)))
	deposit.Znn.Div(deposit.Znn, common.Big100)
	deposit.Qsr.Mul(deposit.Qsr, big.NewInt(int64(percentageToGive)))
	deposit.Qsr.Div(deposit.Qsr, common.Big100)
}

type SwapRetrieveAssetsMethod struct {
	MethodName string
}

func (p *SwapRetrieveAssetsMethod) GetPlasma(plasmaTable *constants.PlasmaTable) (uint64, error) {
	return plasmaTable.EmbeddedWDoubleWithdraw, nil
}
func (p *SwapRetrieveAssetsMethod) ValidateSendBlock(block *nom.AccountBlock) error {
	var err error
	param := new(definition.ParamRetrieveAssets)

	if err := definition.ABISwap.UnpackMethod(param, p.MethodName, block.Data); err != nil {
		return constants.ErrUnpackError
	}

	if _, err := CheckSwapSignature(SwapRetrieveAssets, block.Address, param.PublicKey, param.Signature); err != nil {
		return err
	}
	if block.Amount.Sign() != 0 {
		return constants.ErrInvalidTokenOrAmount
	}

	block.Data, err = definition.ABISwap.PackMethod(p.MethodName, param.PublicKey, param.Signature)
	return err
}
func (p *SwapRetrieveAssetsMethod) ReceiveBlock(context vm_context.AccountVmContext, sendBlock *nom.AccountBlock) ([]*nom.AccountBlock, error) {
	if err := p.ValidateSendBlock(sendBlock); err != nil {
		return nil, err
	}

	param := new(definition.ParamRetrieveAssets)
	err := definition.ABISwap.UnpackMethod(param, p.MethodName, sendBlock.Data)
	common.DealWithErr(err)
	swapLog.Debug("swap-assets-log", "address", sendBlock.Address, "public-key", param.PublicKey, "signature", param.Signature)

	publicKey, err := base64.StdEncoding.DecodeString(param.PublicKey)
	if err != nil {
		return nil, constants.ErrForbiddenParam
	}
	deposit, err := definition.GetSwapAssetsByKeyIdHash(context.Storage(), PubKeyToKeyIdHash(publicKey))
	if err == constants.ErrDataNonExistent {
		return nil, err
	} else {
		common.DealWithErr(err)
	}

	if deposit.Qsr.Cmp(common.Big0) == 0 && deposit.Znn.Cmp(common.Big0) == 0 {
		return nil, constants.ErrDataNonExistent
	}

	swapLog.Debug("deposit to withdraw", "znn", deposit.Znn, "qsr", deposit.Qsr)
	currentM, err := context.GetFrontierMomentum()
	common.DealWithErr(err)
	currentEpoch := int(context.EpochTicker().ToTick(*currentM.Timestamp))
	ApplyDecay(deposit, currentEpoch)

	result := make([]*nom.AccountBlock, 0, 2)
	if deposit.Znn.Sign() == +1 {
		result = append(result, &nom.AccountBlock{
			Address:       types.SwapContract,
			ToAddress:     types.TokenContract,
			BlockType:     nom.BlockTypeContractSend,
			Amount:        big.NewInt(0),
			TokenStandard: types.ZnnTokenStandard,
			Data: definition.ABIToken.PackMethodPanic(
				definition.MintMethodName,
				types.ZnnTokenStandard,
				deposit.Znn,
				sendBlock.Address,
			),
		})
	}
	if deposit.Qsr.Sign() == +1 {
		result = append(result, &nom.AccountBlock{
			Address:       types.SwapContract,
			ToAddress:     types.TokenContract,
			BlockType:     nom.BlockTypeContractSend,
			Amount:        big.NewInt(0),
			TokenStandard: types.ZnnTokenStandard,
			Data: definition.ABIToken.PackMethodPanic(
				definition.MintMethodName,
				types.QsrTokenStandard,
				deposit.Qsr,
				sendBlock.Address,
			),
		})
	}

	deposit.Znn = common.Big0
	deposit.Qsr = common.Big0
	common.DealWithErr(deposit.Save(context.Storage()))

	return result, nil
}
