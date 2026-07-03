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
	plasmaLog = common.EmbeddedLogger.New("contract", "plasma")
)

// FuseMethod (Fuse) locks the sent QSR to generate plasma for a
// beneficiary address; the QSR stays locked until the entry is
// cancelled via CancelFuse. The plasma granted per account block is
// proportional to the total amount fused for the beneficiary — see
// vm/constants/plasma.go for the fusion-unit arithmetic.
type FuseMethod struct {
	MethodName string
}

// GetPlasma quotes the EmbeddedSimple tier; the call sends no
// response block.
func (p *FuseMethod) GetPlasma(plasmaTable *constants.PlasmaTable) (uint64, error) {
	return plasmaTable.EmbeddedSimple, nil
}

// ValidateSendBlock accepts a packed beneficiary address carried by
// a send of QSR of at least constants.FuseMinAmount and a whole
// multiple of constants.CostPerFusionUnit; anything else fails with
// constants.ErrInvalidTokenOrAmount.
func (p *FuseMethod) ValidateSendBlock(block *nom.AccountBlock) error {
	var err error
	param := new(types.Address)

	if err := definition.ABIPlasma.UnpackMethod(param, p.MethodName, block.Data); err != nil {
		return constants.ErrUnpackError
	}

	// make sure users send QSR and more than min amount
	if block.TokenStandard != types.QsrTokenStandard || block.Amount.Cmp(constants.FuseMinAmount) < 0 {
		return constants.ErrInvalidTokenOrAmount
	}

	// make sure users send multiple of constants.CostPerFusionUnit
	mod := new(big.Int).Mod(block.Amount, big.NewInt(constants.CostPerFusionUnit))
	if mod.Sign() != 0 {
		return constants.ErrInvalidTokenOrAmount
	}

	block.Data, err = definition.ABIPlasma.PackMethod(p.MethodName, param)
	return err
}

// ReceiveBlock saves a FusionInfo owned by the sender — its id is
// the send block's hash, its expiration the frontier height plus
// constants.FuseExpiration — and adds the amount to the
// beneficiary's running FusedAmount. It cannot fail past validation
// and emits no descendant blocks.
func (p *FuseMethod) ReceiveBlock(context vm_context.AccountVmContext, sendBlock *nom.AccountBlock) ([]*nom.AccountBlock, error) {
	if err := p.ValidateSendBlock(sendBlock); err != nil {
		return nil, err
	}

	beneficiary := new(types.Address)
	err := definition.ABIPlasma.UnpackMethod(beneficiary, p.MethodName, sendBlock.Data)
	common.DealWithErr(err)

	momentum, err := context.GetFrontierMomentum()
	common.DealWithErr(err)

	fusionInfo := definition.FusionInfo{
		Owner:            sendBlock.Address,
		Id:               sendBlock.Hash,
		Amount:           sendBlock.Amount,
		Beneficiary:      *beneficiary,
		ExpirationHeight: momentum.Height + constants.FuseExpiration,
	}
	common.DealWithErr(fusionInfo.Save(context.Storage()))

	fused, err := definition.GetFusedAmount(context.Storage(), *beneficiary)
	common.DealWithErr(err)
	fused.Amount.Add(fused.Amount, sendBlock.Amount)
	common.DealWithErr(fused.Save(context.Storage()))

	plasmaLog.Debug("fused new entry", "fusionInfo", fusionInfo, "beneficiary", fused)
	return nil, nil
}

// CancelFuseMethod (CancelFuse) dissolves one of the sender's fusion
// entries and refunds its QSR, once the entry has aged past its
// expiration height.
type CancelFuseMethod struct {
	MethodName string
}

// GetPlasma quotes the EmbeddedWWithdraw tier, covering the one
// refund block the cancellation sends back.
func (p *CancelFuseMethod) GetPlasma(plasmaTable *constants.PlasmaTable) (uint64, error) {
	return plasmaTable.EmbeddedWWithdraw, nil
}

// ValidateSendBlock accepts a packed fusion id — the hash of the
// original Fuse send block — carried by a send with no tokens.
func (p *CancelFuseMethod) ValidateSendBlock(block *nom.AccountBlock) error {
	var err error
	param := new(types.Hash)

	if err := definition.ABIPlasma.UnpackMethod(param, p.MethodName, block.Data); err != nil {
		return constants.ErrUnpackError
	}

	if block.Amount.Sign() > 0 {
		return constants.ErrInvalidTokenOrAmount
	}

	block.Data, err = definition.ABIPlasma.PackMethod(p.MethodName, param)
	return err
}

// ReceiveBlock cancels the fusion entry stored under the sender and
// id — only the owner can find it, anyone else gets
// constants.ErrDataNonExistent — once it has expired; an entry whose
// expiration height is still above the frontier fails with
// constants.RevokeNotDue. The amount is subtracted from the
// beneficiary's FusedAmount (deleted at zero), the entry is deleted
// and one descendant send refunds the QSR to the sender.
func (p *CancelFuseMethod) ReceiveBlock(context vm_context.AccountVmContext, sendBlock *nom.AccountBlock) ([]*nom.AccountBlock, error) {
	if err := p.ValidateSendBlock(sendBlock); err != nil {
		return nil, err
	}

	id := new(types.Hash)
	err := definition.ABIPlasma.UnpackMethod(id, p.MethodName, sendBlock.Data)
	common.DealWithErr(err)

	momentum, err := context.GetFrontierMomentum()
	common.DealWithErr(err)

	fusionInfo, err := definition.GetFusionInfo(context.Storage(), sendBlock.Address, *id)
	if err == constants.ErrDataNonExistent {
		return nil, err
	}
	common.DealWithErr(err)

	if fusionInfo.ExpirationHeight > momentum.Height {
		return nil, constants.RevokeNotDue
	}

	fused, err := definition.GetFusedAmount(context.Storage(), fusionInfo.Beneficiary)
	common.DealWithErr(err)
	fused.Amount.Sub(fused.Amount, fusionInfo.Amount)

	plasmaLog.Debug("canceled fusion entry", "fusionInfo", fusionInfo, "beneficiary-remaining", fused)

	if fused.Amount.Sign() == 0 {
		common.DealWithErr(fused.Delete(context.Storage()))
	} else {
		common.DealWithErr(fused.Save(context.Storage()))
	}
	common.DealWithErr(fusionInfo.Delete(context.Storage()))

	return []*nom.AccountBlock{
		{
			Address:       types.PlasmaContract,
			ToAddress:     sendBlock.Address,
			BlockType:     nom.BlockTypeContractSend,
			Amount:        fusionInfo.Amount,
			TokenStandard: types.QsrTokenStandard,
			Data:          []byte{},
		},
	}, nil
}
