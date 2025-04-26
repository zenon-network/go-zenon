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
	sporkLog = common.EmbeddedLogger.New("contract", "spork")
)

type CreateSporkMethod struct {
	MethodName string
}

func checkSporkMetaDataStatic(sporkInfo *definition.Spork) error {
	if len(sporkInfo.Name) < constants.SporkNameMinLength || len(sporkInfo.Name) > constants.SporkNameMaxLength {
		return constants.ErrForbiddenParam
	}
	if len(sporkInfo.Description) > constants.SporkDescriptionMaxLength {
		return constants.ErrForbiddenParam
	}
	return nil
}

func checkCommunitySporkAddressValidity(context vm_context.AccountVmContext) error {
	frontierMomentum, err := context.GetFrontierMomentum()
	common.DealWithErr(err)
	if frontierMomentum.Identifier().Height < definition.CommunitySporkAddressStartHeight {
		return constants.ErrPermissionDenied
	}
	if frontierMomentum.Identifier().Height >= definition.CommunitySporkAddressEndHeight {
		return constants.ErrPermissionDenied
	}
	return nil
}

func (p *CreateSporkMethod) GetPlasma(plasmaTable *constants.PlasmaTable) (uint64, error) {
	return plasmaTable.EmbeddedSimple, nil
}
func (p *CreateSporkMethod) ValidateSendBlock(block *nom.AccountBlock) error {
	if block.Address != *types.SporkAddress && block.Address != types.CommunitySporkAddress {
		return constants.ErrPermissionDenied
	}
	if block.Amount.Sign() != 0 {
		return constants.ErrInvalidTokenOrAmount
	}
	spork := new(definition.Spork)
	err := definition.ABISpork.UnpackMethod(spork, p.MethodName, block.Data)
	if err != nil {
		return constants.ErrForbiddenParam
	}

	// Repack for consistency
	block.Data, err = definition.ABISpork.PackMethod(p.MethodName, spork.Name, spork.Description)
	if err != nil {
		return constants.ErrForbiddenParam
	}

	// Check valid spork information
	err = checkSporkMetaDataStatic(spork)
	if err != nil {
		return err
	}

	return nil
}
func (p *CreateSporkMethod) ReceiveBlock(context vm_context.AccountVmContext, sendBlock *nom.AccountBlock) ([]*nom.AccountBlock, error) {
	if err := p.ValidateSendBlock(sendBlock); err != nil {
		sporkLog.Debug("invalid create - syntactic validation failed", "address", sendBlock.Address, "reason", err)
		return nil, err
	}

	if sendBlock.Address == types.CommunitySporkAddress {
		err := checkCommunitySporkAddressValidity(context)
		if err != nil {
			return nil, err
		}
	}

	spork := new(definition.Spork)
	err := definition.ABISpork.UnpackMethod(spork, p.MethodName, sendBlock.Data)
	if err != nil {
		return nil, constants.ErrForbiddenParam
	}

	// Check valid spork information
	err = checkSporkMetaDataStatic(spork)
	if err != nil {
		return nil, err
	}

	spork.Activated = false
	spork.EnforcementHeight = 0
	spork.Id = sendBlock.Hash
	spork.Save(context.Storage())

	sporkLog.Debug("created", "spork", spork)
	return nil, nil
}

type ActivateSporkMethod struct {
	MethodName string
}

func (p *ActivateSporkMethod) GetPlasma(plasmaTable *constants.PlasmaTable) (uint64, error) {
	return plasmaTable.EmbeddedSimple, nil
}
func (p *ActivateSporkMethod) ValidateSendBlock(block *nom.AccountBlock) error {
	var err error

	if block.Address != *types.SporkAddress && block.Address != types.CommunitySporkAddress {
		return constants.ErrPermissionDenied
	}
	id := new(types.Hash)
	if err := definition.ABISpork.UnpackMethod(id, p.MethodName, block.Data); err != nil {
		return constants.ErrUnpackError
	}

	if block.Amount.Sign() != 0 {
		return constants.ErrInvalidTokenOrAmount
	}

	block.Data, err = definition.ABISpork.PackMethod(p.MethodName, id)
	return err
}
func (p *ActivateSporkMethod) ReceiveBlock(context vm_context.AccountVmContext, sendBlock *nom.AccountBlock) ([]*nom.AccountBlock, error) {
	if err := p.ValidateSendBlock(sendBlock); err != nil {
		sporkLog.Debug("invalid spork activation - syntactic validation failed", "address", sendBlock.Address, "reason", err)
		return nil, err
	}

	if sendBlock.Address == types.CommunitySporkAddress {
		err := checkCommunitySporkAddressValidity(context)
		if err != nil {
			return nil, err
		}
	}

	id := new(types.Hash)
	err := definition.ABISpork.UnpackMethod(id, p.MethodName, sendBlock.Data)
	if err != nil {
		return nil, constants.ErrForbiddenParam
	}

	// Make sure spork exists
	spork := definition.GetSporkInfoById(context.Storage(), *id)
	if spork == nil {
		return nil, constants.ErrDataNonExistent
	}
	if spork.Activated {
		return nil, constants.ErrAlreadyActivated
	}

	spork.Activated = true
	frontierMomentum, err := context.GetFrontierMomentum()
	common.DealWithErr(err)
	spork.EnforcementHeight = frontierMomentum.Height + constants.SporkMinHeightDelay
	spork.Save(context.Storage())
	sporkLog.Debug("activated", "spork", spork)
	return nil, nil
}
