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

// CreateSporkMethod (CreateSpork) registers a new protocol-upgrade
// switch. Only types.SporkAddress and types.CommunitySporkAddress
// may call it, and the community address only inside its
// momentum-height window (checkCommunitySporkAddressValidity). The
// spork starts inactive; ActivateSpork schedules its enforcement.
type CreateSporkMethod struct {
	MethodName string
}

// checkSporkMetaDataStatic rejects names outside
// constants.SporkNameMinLength..constants.SporkNameMaxLength bytes
// and descriptions longer than constants.SporkDescriptionMaxLength
// bytes with constants.ErrForbiddenParam.
func checkSporkMetaDataStatic(sporkInfo *definition.Spork) error {
	if len(sporkInfo.Name) < constants.SporkNameMinLength || len(sporkInfo.Name) > constants.SporkNameMaxLength {
		return constants.ErrForbiddenParam
	}
	if len(sporkInfo.Description) > constants.SporkDescriptionMaxLength {
		return constants.ErrForbiddenParam
	}
	return nil
}

// checkCommunitySporkAddressValidity confines the community spork
// address to frontier heights from
// definition.CommunitySporkAddressStartHeight up to but excluding
// definition.CommunitySporkAddressEndHeight; outside the window it
// returns constants.ErrPermissionDenied. types.SporkAddress is not
// subject to the window.
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

// GetPlasma quotes the EmbeddedSimple tier; the call sends no
// response block.
func (p *CreateSporkMethod) GetPlasma(plasmaTable *constants.PlasmaTable) (uint64, error) {
	return plasmaTable.EmbeddedSimple, nil
}

// ValidateSendBlock accepts a packed name and description within the
// metadata bounds, sent with no tokens by one of the two spork
// addresses (constants.ErrPermissionDenied otherwise). Unlike most
// methods, malformed Data fails with constants.ErrForbiddenParam
// rather than constants.ErrUnpackError.
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

// ReceiveBlock applies the community-window gate when the sender is
// types.CommunitySporkAddress, then saves the spork: its Id is the
// send block's hash, Activated false and EnforcementHeight zero
// until ActivateSpork is called.
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

// ActivateSporkMethod (ActivateSpork) schedules an existing spork
// for enforcement, subject to the same caller gates as CreateSpork.
// Activation is one-way and takes effect
// constants.SporkMinHeightDelay (6) momentums past the frontier,
// giving every node the same enforcement height.
type ActivateSporkMethod struct {
	MethodName string
}

// GetPlasma quotes the EmbeddedSimple tier; the call sends no
// response block.
func (p *ActivateSporkMethod) GetPlasma(plasmaTable *constants.PlasmaTable) (uint64, error) {
	return plasmaTable.EmbeddedSimple, nil
}

// ValidateSendBlock accepts a packed spork id sent with no tokens by
// one of the two spork addresses (constants.ErrPermissionDenied
// otherwise).
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

// ReceiveBlock applies the community-window gate when the sender is
// types.CommunitySporkAddress, then activates the spork: it must
// exist (constants.ErrDataNonExistent) and not be activated already
// (constants.ErrAlreadyActivated). Activated is set and
// EnforcementHeight becomes the frontier height plus
// constants.SporkMinHeightDelay.
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
