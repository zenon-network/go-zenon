package implementation

import (
	"math/big"
	"regexp"

	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/vm/constants"
	"github.com/zenon-network/go-zenon/vm/embedded/definition"
	"github.com/zenon-network/go-zenon/vm/vm_context"
)

var (
	tokenLog = common.EmbeddedLogger.New("contract", "token")
)

type IssueMethod struct {
	MethodName string
}

func checkToken(param definition.IssueParam) error {
	// Valid names
	if len(param.TokenName) == 0 || len(param.TokenName) > constants.TokenNameLengthMax {
		return constants.ErrTokenInvalidText
	}
	if len(param.TokenSymbol) == 0 || len(param.TokenSymbol) > constants.TokenSymbolLengthMax {
		return constants.ErrTokenInvalidText
	}
	if len(param.TokenDomain) > constants.TokenDomainLengthMax {
		return constants.ErrTokenInvalidText
	}

	if ok, _ := regexp.MatchString("^([a-zA-Z0-9]+[-._]?)*[a-zA-Z0-9]$", param.TokenName); !ok {
		return constants.ErrTokenInvalidText
	}
	if ok, _ := regexp.MatchString("^[A-Z0-9]+$", param.TokenSymbol); !ok {
		return constants.ErrTokenInvalidText
	}
	if ok, _ := regexp.MatchString("^([A-Za-z0-9][A-Za-z0-9-]{0,61}[A-Za-z0-9]\\.)+[A-Za-z]{2,}$", param.TokenDomain); !ok && len(param.TokenDomain) != 0 {
		return constants.ErrTokenInvalidText
	}

	if param.TokenSymbol == "ZNN" || param.TokenSymbol == "QSR" {
		return constants.ErrTokenInvalidText
	}

	if param.Decimals > uint8(constants.TokenMaxDecimals) {
		return constants.ErrTokenInvalidText
	}

	// 0 or too big
	if param.MaxSupply.Cmp(constants.TokenMaxSupplyBig) > 0 {
		return constants.ErrTokenInvalidAmount
	}
	if param.MaxSupply.Cmp(common.Big0) == 0 {
		return constants.ErrTokenInvalidAmount
	}

	// total supply is less and equal in case of non-mintable coins
	if param.MaxSupply.Cmp(param.TotalSupply) == -1 {
		return constants.ErrTokenInvalidAmount
	}
	if !param.IsMintable && param.MaxSupply.Cmp(param.TotalSupply) != 0 {
		return constants.ErrTokenInvalidAmount
	}
	return nil
}
func newTokenID(sendBlockHash types.Hash) types.ZenonTokenStandard {
	return types.NewZenonTokenStandard(sendBlockHash.Bytes())
}

func (p *IssueMethod) GetPlasma(plasmaTable *constants.PlasmaTable) (uint64, error) {
	return plasmaTable.EmbeddedWWithdraw, nil
}
func (p *IssueMethod) ValidateSendBlock(block *nom.AccountBlock) error {
	var err error
	param := new(definition.IssueParam)

	if err := definition.ABIToken.UnpackMethod(param, p.MethodName, block.Data); err != nil {
		return constants.ErrUnpackError
	}
	if err = checkToken(*param); err != nil {
		return err
	}

	if block.TokenStandard != types.ZnnTokenStandard {
		return constants.ErrInvalidTokenOrAmount
	}
	if block.Amount.Cmp(constants.TokenIssueAmount) != 0 {
		return constants.ErrInvalidTokenOrAmount
	}

	block.Data, err = definition.ABIToken.PackMethod(
		p.MethodName,
		param.TokenName,
		param.TokenSymbol,
		param.TokenDomain,
		param.TotalSupply,
		param.MaxSupply,
		param.Decimals,
		param.IsMintable,
		param.IsBurnable,
		param.IsUtility)
	return err
}
func (p *IssueMethod) ReceiveBlock(context vm_context.AccountVmContext, sendBlock *nom.AccountBlock) ([]*nom.AccountBlock, error) {
	if err := p.ValidateSendBlock(sendBlock); err != nil {
		return nil, err
	}

	param := new(definition.IssueParam)
	err := definition.ABIToken.UnpackMethod(param, p.MethodName, sendBlock.Data)
	common.DealWithErr(err)

	tokenStandard := newTokenID(sendBlock.Hash)
	if _, err := definition.GetTokenInfo(context.Storage(), tokenStandard); err == constants.ErrDataNonExistent {
	} else if err == nil {
		return nil, constants.ErrIDNotUnique
	} else if err != constants.ErrDataNonExistent {
		common.DealWithErr(err)
	}

	tokenInfo := definition.TokenInfo{
		Owner:         sendBlock.Address,
		TokenName:     param.TokenName,
		TokenSymbol:   param.TokenSymbol,
		TokenDomain:   param.TokenDomain,
		TotalSupply:   param.TotalSupply,
		MaxSupply:     param.MaxSupply,
		Decimals:      param.Decimals,
		IsMintable:    param.IsMintable,
		IsBurnable:    param.IsBurnable,
		IsUtility:     param.IsUtility,
		TokenStandard: tokenStandard,
	}

	common.DealWithErr(tokenInfo.Save(context.Storage()))

	// add minted token to TokenContract
	context.AddBalance(&tokenStandard, param.TotalSupply)
	tokenLog.Debug("issued ZTS", "token", tokenInfo)
	return []*nom.AccountBlock{
		{
			Address:       types.TokenContract,
			ToAddress:     sendBlock.Address,
			BlockType:     nom.BlockTypeContractSend,
			Amount:        param.TotalSupply,
			TokenStandard: tokenStandard,
			Data:          []byte{},
		},
	}, nil
}

type MintMethod struct {
	MethodName string
}

func (p *MintMethod) GetPlasma(plasmaTable *constants.PlasmaTable) (uint64, error) {
	return plasmaTable.EmbeddedWWithdraw, nil
}
func (p *MintMethod) ValidateSendBlock(block *nom.AccountBlock) error {
	var err error
	param := new(definition.MintParam)
	if err := definition.ABIToken.UnpackMethod(param, p.MethodName, block.Data); err != nil {
		return constants.ErrUnpackError
	}
	if param.Amount.Sign() <= 0 {
		return constants.ErrInvalidTokenOrAmount
	}
	if block.Amount.Sign() != 0 {
		return constants.ErrInvalidTokenOrAmount
	}

	block.Data, err = definition.ABIToken.PackMethod(p.MethodName, param.TokenStandard, param.Amount, param.ReceiveAddress)
	return err
}
func (p *MintMethod) ReceiveBlock(context vm_context.AccountVmContext, sendBlock *nom.AccountBlock) ([]*nom.AccountBlock, error) {
	if err := p.ValidateSendBlock(sendBlock); err != nil {
		return nil, err
	}

	param := new(definition.MintParam)
	err := definition.ABIToken.UnpackMethod(param, p.MethodName, sendBlock.Data)
	common.DealWithErr(err)

	tokenInfo, err := definition.GetTokenInfo(context.Storage(), param.TokenStandard)
	if err == constants.ErrDataNonExistent {
		return nil, err
	}
	common.DealWithErr(err)

	if !tokenInfo.IsMintable {
		return nil, constants.ErrPermissionDenied
	}
	if new(big.Int).Sub(tokenInfo.MaxSupply, tokenInfo.TotalSupply).Cmp(param.Amount) < 0 {
		return nil, constants.ErrTokenInvalidAmount
	}

	// check owner, all embedded contracts for ZNN and QSR
	if param.TokenStandard == types.ZnnTokenStandard {
		if !types.IsEmbeddedAddress(sendBlock.Address) {
			return nil, constants.ErrPermissionDenied
		}
	} else if param.TokenStandard == types.QsrTokenStandard {
		if !types.IsEmbeddedAddress(sendBlock.Address) {
			return nil, constants.ErrPermissionDenied
		}
	} else if tokenInfo.Owner != sendBlock.Address {
		return nil, constants.ErrPermissionDenied
	}

	tokenInfo.TotalSupply.Add(tokenInfo.TotalSupply, param.Amount)
	common.DealWithErr(tokenInfo.Save(context.Storage()))

	// add minted token to TokenContract
	context.AddBalance(&param.TokenStandard, param.Amount)
	tokenLog.Debug("minted ZTS", "token", tokenInfo, "minted-amount", param.Amount, "to-address", param.ReceiveAddress)
	var data []byte
	if types.IsEmbeddedAddress(param.ReceiveAddress) {
		data, err = definition.ABICommon.PackMethod(definition.DonateMethodName)
		if err != nil {
			return nil, err
		}
	}
	return []*nom.AccountBlock{
		{
			Address:       types.TokenContract,
			ToAddress:     param.ReceiveAddress,
			BlockType:     nom.BlockTypeContractSend,
			Amount:        param.Amount,
			TokenStandard: param.TokenStandard,
			Data:          data,
		},
	}, nil
}

type BurnMethod struct {
	MethodName string
}

func (p *BurnMethod) GetPlasma(plasmaTable *constants.PlasmaTable) (uint64, error) {
	return plasmaTable.EmbeddedSimple, nil
}
func (p *BurnMethod) ValidateSendBlock(block *nom.AccountBlock) error {
	var err error

	if err := definition.ABIToken.UnpackEmptyMethod(p.MethodName, block.Data); err != nil {
		return constants.ErrUnpackError
	}

	if block.Amount.Sign() != 1 {
		return constants.ErrInvalidTokenOrAmount
	}

	block.Data, err = definition.ABIToken.PackMethod(p.MethodName)
	return err
}
func (p *BurnMethod) ReceiveBlock(context vm_context.AccountVmContext, sendBlock *nom.AccountBlock) ([]*nom.AccountBlock, error) {
	if err := p.ValidateSendBlock(sendBlock); err != nil {
		return nil, err
	}

	tokenInfo, err := definition.GetTokenInfo(context.Storage(), sendBlock.TokenStandard)
	if err == constants.ErrDataNonExistent {
		return nil, err
	}
	common.DealWithErr(err)

	if !tokenInfo.IsBurnable && tokenInfo.Owner != sendBlock.Address {
		return nil, constants.ErrPermissionDenied
	}

	// for non-mintable coins, drop MaxSupply as well
	if !tokenInfo.IsMintable {
		tokenInfo.MaxSupply.Sub(tokenInfo.MaxSupply, sendBlock.Amount)
	}
	tokenInfo.TotalSupply.Sub(tokenInfo.TotalSupply, sendBlock.Amount)
	common.DealWithErr(tokenInfo.Save(context.Storage()))

	// remove received token from TokenContract
	context.SubBalance(&sendBlock.TokenStandard, sendBlock.Amount)
	tokenLog.Debug("burned ZTS", "token", tokenInfo, "burned-amount", sendBlock.Amount)
	return nil, nil
}

type UpdateTokenMethod struct {
	MethodName string
}

func (p *UpdateTokenMethod) GetPlasma(plasmaTable *constants.PlasmaTable) (uint64, error) {
	return plasmaTable.EmbeddedSimple, nil
}
func (p *UpdateTokenMethod) ValidateSendBlock(block *nom.AccountBlock) error {
	var err error
	param := new(definition.UpdateTokenParam)

	if err := definition.ABIToken.UnpackMethod(param, p.MethodName, block.Data); err != nil {
		return constants.ErrUnpackError
	}

	if block.Amount.Sign() > 0 {
		return constants.ErrInvalidTokenOrAmount
	}

	block.Data, err = definition.ABIToken.PackMethod(p.MethodName, param.TokenStandard, param.Owner, param.IsMintable, param.IsBurnable)
	return err
}
func (p *UpdateTokenMethod) ReceiveBlock(context vm_context.AccountVmContext, sendBlock *nom.AccountBlock) ([]*nom.AccountBlock, error) {
	if err := p.ValidateSendBlock(sendBlock); err != nil {
		return nil, err
	}

	param := new(definition.UpdateTokenParam)
	err := definition.ABIToken.UnpackMethod(param, p.MethodName, sendBlock.Data)
	common.DealWithErr(err)

	tokenInfo, err := definition.GetTokenInfo(context.Storage(), param.TokenStandard)
	if err == constants.ErrDataNonExistent {
		return nil, err
	}
	common.DealWithErr(err)

	if tokenInfo.Owner != sendBlock.Address {
		return nil, constants.ErrPermissionDenied
	}

	if tokenInfo.IsMintable != param.IsMintable {
		if !tokenInfo.IsMintable {
			return nil, constants.ErrForbiddenParam
		}
		tokenLog.Debug("updating token IsMintable", "old", tokenInfo.IsMintable, "new", param.IsMintable)
		tokenInfo.IsMintable = param.IsMintable
		tokenInfo.MaxSupply = tokenInfo.TotalSupply
	}

	if tokenInfo.Owner != param.Owner {
		tokenLog.Debug("updating token owner", "old", tokenInfo.Owner, "new", param.Owner)
		tokenInfo.Owner = param.Owner
	}

	if tokenInfo.IsBurnable != param.IsBurnable {
		tokenLog.Debug("updating token IsBurnable", "old", tokenInfo.IsBurnable, "new", param.IsBurnable)
		tokenInfo.IsBurnable = param.IsBurnable
	}

	tokenLog.Debug("updated ZTS", "token", tokenInfo)
	common.DealWithErr(tokenInfo.Save(context.Storage()))
	return nil, nil
}
