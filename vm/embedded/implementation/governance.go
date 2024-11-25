package implementation

import (
	"encoding/base64"
	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/vm/constants"
	"github.com/zenon-network/go-zenon/vm/embedded/definition"
	"github.com/zenon-network/go-zenon/vm/vm_context"
	"regexp"
)

var (
	governanceLog = common.EmbeddedLogger.New("contract", "governance")
)

type ProposeActionMethod struct {
	MethodName string
}

func checkActionStatic(param *definition.ActionVariable) error {
	if len(param.Name) == 0 ||
		len(param.Name) > constants.ProjectNameLengthMax {
		governanceLog.Debug("governance-check-action-static", "reason", "malformed-name")
		return constants.ErrInvalidName
	}

	if len(param.Description) == 0 || len(param.Description) > constants.ProjectDescriptionLengthMax {
		governanceLog.Debug("governance-check-action-static", "reason", "malformed-description")
		return constants.ErrInvalidDescription
	}

	if ok, _ := regexp.MatchString("^([Hh][Tt][Tt][Pp][Ss]?://)?[a-zA-Z0-9]{2,60}\\.[a-zA-Z]{1,6}([-a-zA-Z0-9()@:%_+.~#?&/=]{0,100})$", param.Url); !ok || len(param.Url) == 0 {
		governanceLog.Debug("governance-check-action-static", "reason", "malformed-url")
		return constants.ErrForbiddenParam
	}

	if param.Destination.String() == types.TokenContract.String() {
		governanceLog.Debug("governance-check-action-static", "reason", "forbidden-destination")
		return constants.ErrPermissionDenied
	}

	_, err := base64.StdEncoding.DecodeString(param.Data)
	if err != nil {
		governanceLog.Debug("governance-check-action-static", "reason", "malformed-data")
		return constants.ErrInvalidB64Decode
	}
	return nil
}

func (p *ProposeActionMethod) GetPlasma(plasmaTable *constants.PlasmaTable) (uint64, error) {
	return plasmaTable.EmbeddedWDoubleWithdraw, nil
}

func (p *ProposeActionMethod) ValidateSendBlock(block *nom.AccountBlock) error {
	var err error
	param := new(definition.ActionVariable)

	if err := definition.ABIGovernance.UnpackMethod(param, p.MethodName, block.Data); err != nil {
		return constants.ErrUnpackError
	}

	if err := checkActionStatic(param); err != nil {
		return err
	}

	if block.TokenStandard != types.ZnnTokenStandard || block.Amount.Cmp(constants.ProjectCreationAmount) != 0 {
		return constants.ErrInvalidTokenOrAmount
	}

	block.Data, err = definition.ABIGovernance.PackMethod(p.MethodName, param.Name, param.Description, param.Url, param.Destination, param.Data)
	return err
}
func (p *ProposeActionMethod) ReceiveBlock(context vm_context.AccountVmContext, sendBlock *nom.AccountBlock) ([]*nom.AccountBlock, error) {
	if err := p.ValidateSendBlock(sendBlock); err != nil {
		return nil, err
	}

	proposedAction := new(definition.ActionVariable)
	err := definition.ABIGovernance.UnpackMethod(proposedAction, p.MethodName, sendBlock.Data)
	common.DealWithErr(err)

	frontierMomentum, err := context.GetFrontierMomentum()
	if err != nil {
		return nil, err
	}

	proposedAction.Id = sendBlock.Hash
	proposedAction.Owner = sendBlock.Address
	proposedAction.CreationTimestamp = frontierMomentum.Timestamp.Unix()
	proposedAction.Executed = false
	// Only account-blocks to spork are considered type1 for now
	if proposedAction.Destination.String() == types.SporkContract.String() {
		proposedAction.Type = constants.Type1Action
	} else {
		proposedAction.Type = constants.Type2Action
	}

	proposedAction.Save(context.Storage())

	// Add hash to votable hashes
	(&definition.VotableHash{Id: sendBlock.Hash}).Save(context.Storage())

	governanceLog.Debug("successfully created action proposal", "action", proposedAction)
	return nil, nil
}

type ExecuteActionMethod struct {
	MethodName string
}

func (p *ExecuteActionMethod) GetPlasma(plasmaTable *constants.PlasmaTable) (uint64, error) {
	return plasmaTable.EmbeddedSimple, nil
}

func (p *ExecuteActionMethod) ValidateSendBlock(block *nom.AccountBlock) error {
	var err error
	id := new(types.Hash)

	if err := definition.ABIGovernance.UnpackMethod(id, p.MethodName, block.Data); err != nil {
		return constants.ErrUnpackError
	}

	if block.TokenStandard != types.ZnnTokenStandard || block.Amount.Sign() != 0 {
		return constants.ErrInvalidTokenOrAmount
	}

	block.Data, err = definition.ABIGovernance.PackMethod(p.MethodName, id)
	return err
}
func (p *ExecuteActionMethod) ReceiveBlock(context vm_context.AccountVmContext, sendBlock *nom.AccountBlock) ([]*nom.AccountBlock, error) {
	if err := p.ValidateSendBlock(sendBlock); err != nil {
		return nil, err
	}

	id := new(types.Hash)
	err := definition.ABIGovernance.UnpackMethod(id, p.MethodName, sendBlock.Data)
	common.DealWithErr(err)

	action, err := definition.GetActionById(context.Storage(), *id)
	if err != nil {
		return nil, err
	}

	frontierMomentum, err := context.GetFrontierMomentum()
	if err != nil {
		return nil, err
	}

	expirationTime := action.CreationTimestamp
	if action.Type == constants.Type1Action {
		expirationTime += constants.Type1ActionVotingPeriod
	} else if action.Type == constants.Type2Action {
		expirationTime += constants.Type2ActionVotingPeriod
	} else {
		return nil, constants.ErrUnkownActionType
	}

	expired := expirationTime < frontierMomentum.Timestamp.Unix()
	if action.Executed || expired {
		governanceLog.Debug("action-executed-or-expired", "executedValue", action.Executed, "expiredValue", expired)
		return nil, nil
	}

	pillarList, err := context.MomentumStore().GetActivePillars()
	if err != nil {
		return nil, err
	}
	numPillars := uint32(len(pillarList))
	ok := checkActionVotes(context, action.Id, numPillars, action.Type)
	if !ok {
		return nil, nil
	}

	data, err := base64.StdEncoding.DecodeString(action.Data)
	if err != nil {
		governanceLog.Debug("execute-action", "reason", "malformed-data")
		return nil, constants.ErrInvalidB64Decode
	}

	action.Executed = true
	action.Save(context.Storage())

	governanceLog.Debug("action passed voting and is being executed", "action-id", action.Id, "passed-votes", ok)
	return []*nom.AccountBlock{
		{
			Address:       types.GovernanceContract,
			ToAddress:     action.Destination,
			BlockType:     nom.BlockTypeContractSend,
			Amount:        common.Big0,
			TokenStandard: types.ZnnTokenStandard,
			Data:          data,
		},
	}, nil
}

func checkActionVotes(context vm_context.AccountVmContext, id types.Hash, numPillars uint32, actionType uint8) bool {
	breakdown := definition.GetVoteBreakdown(context.Storage(), id)

	ok := true
	// Test majority
	if breakdown.Yes <= breakdown.No {
		ok = false
	}
	threshold := constants.Type1ActionAcceptanceThreshold
	if actionType == uint8(2) {
		threshold = constants.Type2ActionAcceptanceThreshold
	}

	// Test enough yes votes
	if breakdown.Yes*100 <= numPillars*threshold {
		ok = false
	}

	governanceLog.Debug("check action votes", "votes", breakdown, "status", ok)
	return ok
}
