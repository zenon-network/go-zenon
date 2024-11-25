package embedded

import (
	"github.com/inconshreveable/log15"
	"github.com/zenon-network/go-zenon/chain"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/rpc/api"
	"github.com/zenon-network/go-zenon/vm/constants"
	"github.com/zenon-network/go-zenon/vm/embedded/definition"
	"github.com/zenon-network/go-zenon/zenon"
)

type GovernanceApi struct {
	chain chain.Chain
	log   log15.Logger
}

func NewGovernanceApi(z zenon.Zenon) *GovernanceApi {
	return &GovernanceApi{
		chain: z.Chain(),
		log:   common.RPCLogger.New("module", "embedded_governance_api"),
	}
}

type Action struct {
	*definition.ActionVariable
	Expired bool                      `json:"Expired"`
	Votes   *definition.VoteBreakdown `json:"Votes"`
}

func (a *GovernanceApi) GetActionById(id types.Hash) (*Action, error) {
	_, context, err := api.GetFrontierContext(a.chain, types.GovernanceContract)
	if err != nil {
		return nil, err
	}

	actionVariable, err := definition.GetActionById(context.Storage(), id)
	if err != nil {
		return nil, err
	}

	expired := false
	expireTimestamp := actionVariable.CreationTimestamp
	if actionVariable.Type == constants.Type1Action {
		expireTimestamp += constants.Type1ActionVotingPeriod
	} else if actionVariable.Type == constants.Type2Action {
		expireTimestamp += constants.Type2ActionVotingPeriod
	} else {
		// todo just return the action?
		return nil, constants.ErrUnkownActionType
	}
	momentum, err := context.GetFrontierMomentum()
	if err != nil {
		return nil, err
	}
	if expireTimestamp < momentum.Timestamp.Unix() {
		expired = true
	}
	action := &Action{
		ActionVariable: actionVariable,
		Expired:        expired,
		Votes:          definition.GetVoteBreakdown(context.Storage(), id),
	}

	return action, nil
}

type ActionList struct {
	Count int       `json:"count"`
	List  []*Action `json:"list"`
}

func (a *GovernanceApi) GetAllActions(pageIndex, pageSize uint32) (*ActionList, error) {
	_, context, err := api.GetFrontierContext(a.chain, types.GovernanceContract)
	if err != nil {
		return nil, err
	}

	actions, err := definition.GetActions(context.Storage())
	if err != nil {
		return nil, err
	}

	result := &ActionList{
		Count: len(actions),
		List:  make([]*Action, 0),
	}

	start, end := api.GetRange(pageIndex, pageSize, uint32(len(actions)))
	momentum, err := context.GetFrontierMomentum()
	if err != nil {
		return nil, err
	}
	for i := start; i < end; i++ {
		expired := false
		expireTimestamp := actions[i].CreationTimestamp
		if actions[i].Type == constants.Type1Action {
			expireTimestamp += constants.Type1ActionVotingPeriod
		} else if actions[i].Type == constants.Type2Action {
			expireTimestamp += constants.Type2ActionVotingPeriod
		} else {
			// todo just return the action?
			return nil, constants.ErrUnkownActionType
		}

		if expireTimestamp < momentum.Timestamp.Unix() {
			expired = true
		}
		action := &Action{
			ActionVariable: actions[i],
			Expired:        expired,
			Votes:          definition.GetVoteBreakdown(context.Storage(), actions[i].Id),
		}

		result.List = append(result.List, action)
	}

	return result, nil
}
