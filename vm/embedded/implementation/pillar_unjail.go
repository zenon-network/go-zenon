package implementation

import (
	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/vm/constants"
	"github.com/zenon-network/go-zenon/vm/embedded/definition"
	"github.com/zenon-network/go-zenon/vm/vm_context"
)

// UnjailMethod (Unjail) clears the jail of a pillar. Only the pillar's
// owner (stake address) may call it, with exactly
// constants.PillarUnjailAmount ZNN, which is burned. The method exists
// only once types.PillarJailSpork is enforced.
type UnjailMethod struct {
	MethodName string
}

// GetPlasma quotes twice the EmbeddedSimple tier, covering the burn
// transaction the unjail sends to the token contract.
func (p *UnjailMethod) GetPlasma(plasmaTable *constants.PlasmaTable) (uint64, error) {
	// include burn transaction
	return 2 * plasmaTable.EmbeddedSimple, nil
}

// ValidateSendBlock accepts a valid pillar name carried by a send of
// exactly constants.PillarUnjailAmount ZNN.
func (p *UnjailMethod) ValidateSendBlock(block *nom.AccountBlock) error {
	var err error
	param := new(string)

	if err := definition.ABIPillars.UnpackMethod(param, p.MethodName, block.Data); err != nil {
		return constants.ErrUnpackError
	}

	if err := checkPillarNameStatic(*param); err != nil {
		return err
	}
	if block.TokenStandard != types.ZnnTokenStandard || block.Amount.Cmp(constants.PillarUnjailAmount) != 0 {
		return constants.ErrInvalidTokenOrAmount
	}

	block.Data, err = definition.ABIPillars.PackMethod(p.MethodName, param)
	return err
}

// ReceiveBlock clears the jail of the named pillar, which must exist
// (constants.ErrDataNonExistent), be active (constants.ErrNotActive),
// be owned by the sender, its stake address
// (constants.ErrPermissionDenied), and be jailed
// (constants.ErrPillarNotJailed). The jail record is saved with Jailed
// false and Since set to the frontier momentum time, and one
// descendant send burns the fee at the token contract. ZNN is
// burnable, so the burn is accepted: on mainnet the token contract
// owns ZNN and its isBurnable flag is set at genesis.
func (p *UnjailMethod) ReceiveBlock(context vm_context.AccountVmContext, sendBlock *nom.AccountBlock) ([]*nom.AccountBlock, error) {
	if err := p.ValidateSendBlock(sendBlock); err != nil {
		return nil, err
	}

	name := new(string)
	err := definition.ABIPillars.UnpackMethod(name, p.MethodName, sendBlock.Data)
	common.DealWithErr(err)

	pillar, err := definition.GetPillarInfo(context.Storage(), *name)
	if err == constants.ErrDataNonExistent {
		return nil, err
	} else {
		common.DealWithErr(err)
	}
	if !pillar.IsActive() {
		return nil, constants.ErrNotActive
	}
	if pillar.StakeAddress != sendBlock.Address {
		return nil, constants.ErrPermissionDenied
	}

	jail, err := definition.GetPillarJail(context.Storage(), *name)
	if err == constants.ErrDataNonExistent {
		return nil, constants.ErrPillarNotJailed
	} else {
		common.DealWithErr(err)
	}
	if !jail.Jailed {
		return nil, constants.ErrPillarNotJailed
	}

	momentum, err := context.GetFrontierMomentum()
	common.DealWithErr(err)
	jail.Jailed = false
	jail.Since = momentum.Timestamp.Unix()
	common.DealWithErr(jail.Save(context.Storage()))

	return []*nom.AccountBlock{
		{
			Address:       types.PillarContract,
			ToAddress:     types.TokenContract,
			BlockType:     nom.BlockTypeContractSend,
			Amount:        constants.PillarUnjailAmount,
			TokenStandard: types.ZnnTokenStandard,
			Data:          definition.ABIToken.PackMethodPanic(definition.BurnMethodName),
		},
	}, nil
}
