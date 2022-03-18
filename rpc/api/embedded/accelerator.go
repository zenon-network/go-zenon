package embedded

import (
	"math/big"
	"sort"

	"github.com/inconshreveable/log15"

	"github.com/zenon-network/go-zenon/chain"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/rpc/api"
	"github.com/zenon-network/go-zenon/vm/constants"
	"github.com/zenon-network/go-zenon/vm/embedded/definition"
	"github.com/zenon-network/go-zenon/vm/vm_context"
	"github.com/zenon-network/go-zenon/zenon"
)

type AcceleratorApi struct {
	chain chain.Chain
	log   log15.Logger
}

func NewAcceleratorApi(z zenon.Zenon) *AcceleratorApi {
	return &AcceleratorApi{
		chain: z.Chain(),
		log:   common.RPCLogger.New("module", "embedded_accelerator_api"),
	}
}

func (a *AcceleratorApi) toProject(context vm_context.AccountVmContext, abiProject *definition.Project) *Project {
	project := &Project{
		Id:                  abiProject.Id,
		Owner:               abiProject.Owner,
		Name:                abiProject.Name,
		Description:         abiProject.Description,
		Url:                 abiProject.Url,
		ZnnFundsNeeded:      abiProject.ZnnFundsNeeded,
		QsrFundsNeeded:      abiProject.QsrFundsNeeded,
		CreationTimestamp:   abiProject.CreationTimestamp,
		LastUpdateTimestamp: abiProject.LastUpdateTimestamp,
		Status:              abiProject.Status,
		PhaseIds:            abiProject.PhaseIds,
		Votes:               definition.GetVoteBreakdown(context.Storage(), abiProject.Id),
		Phases:              make([]*Phase, len(abiProject.PhaseIds)),
	}

	for index, id := range abiProject.PhaseIds {
		phase, err := definition.GetPhaseEntry(context.Storage(), id)
		if err != nil {
			continue
		}
		project.Phases[index] = &Phase{
			Phase: phase,
			Votes: definition.GetVoteBreakdown(context.Storage(), phase.Id),
		}
	}

	return project
}

type Phase struct {
	Phase *definition.Phase         `json:"phase"`
	Votes *definition.VoteBreakdown `json:"votes"`
}

type Project struct {
	Id                  types.Hash                `json:"id"`
	Owner               types.Address             `json:"owner"`
	Name                string                    `json:"name"`
	Description         string                    `json:"description"`
	Url                 string                    `json:"url"`
	ZnnFundsNeeded      *big.Int                  `json:"znnFundsNeeded"`
	QsrFundsNeeded      *big.Int                  `json:"qsrFundsNeeded"`
	CreationTimestamp   int64                     `json:"creationTimestamp"`
	LastUpdateTimestamp int64                     `json:"lastUpdateTimestamp"`
	Status              uint8                     `json:"status"`
	PhaseIds            []types.Hash              `json:"phaseIds"`
	Votes               *definition.VoteBreakdown `json:"votes"`
	Phases              []*Phase                  `json:"phases"`
}

type ProjectList struct {
	Count int        `json:"count"`
	List  []*Project `json:"list"`
}

// === Getters for projects ===

func (a *AcceleratorApi) GetAll(pageIndex, pageSize uint32) (*ProjectList, error) {
	_, context, err := api.GetFrontierContext(a.chain, types.AcceleratorContract)
	if err != nil {
		return nil, err
	}

	projects, err := definition.GetProjectList(context.Storage())
	if err != nil {
		return nil, err
	}

	sort.SliceStable(projects, func(i, j int) bool {
		return projects[i].LastUpdateTimestamp > projects[j].LastUpdateTimestamp
	})

	result := &ProjectList{
		Count: len(projects),
		List:  make([]*Project, len(projects)),
	}

	for index, project := range projects {
		result.List[index] = a.toProject(context, project)
	}

	start, end := api.GetRange(pageIndex, pageSize, uint32(len(result.List)))
	result.List = result.List[start:end]

	return result, nil
}
func (a *AcceleratorApi) GetProjectById(id types.Hash) (*Project, error) {
	_, context, err := api.GetFrontierContext(a.chain, types.AcceleratorContract)
	if err != nil {
		return nil, err
	}

	project, err := definition.GetProjectEntry(context.Storage(), id)
	if err != nil {
		return nil, err
	}
	return a.toProject(context, project), nil
}
func (a *AcceleratorApi) GetPhaseById(id types.Hash) (*Phase, error) {
	_, context, err := api.GetFrontierContext(a.chain, types.AcceleratorContract)
	if err != nil {
		return nil, err
	}

	phase, err := definition.GetPhaseEntry(context.Storage(), id)
	if err != nil {
		return nil, err
	}
	return &Phase{
		Phase: phase,
		Votes: definition.GetVoteBreakdown(context.Storage(), phase.Id),
	}, nil
}
func (a *AcceleratorApi) GetVoteBreakdown(id types.Hash) (*definition.VoteBreakdown, error) {
	_, context, err := api.GetFrontierContext(a.chain, types.AcceleratorContract)
	if err != nil {
		return nil, err
	}
	voteBreakdown := definition.GetVoteBreakdown(context.Storage(), id)
	if voteBreakdown == nil {
		return nil, constants.ErrDataNonExistent
	}
	return voteBreakdown, nil
}
func (a *AcceleratorApi) GetPillarVotes(name string, hashes []types.Hash) ([]*definition.PillarVote, error) {
	_, context, err := api.GetFrontierContext(a.chain, types.AcceleratorContract)
	if err != nil {
		return nil, err
	}
	result := make([]*definition.PillarVote, len(hashes))
	for index := range hashes {
		vote, err := definition.GetPillarVote(context.Storage(), hashes[index], name)
		if err == constants.ErrDataNonExistent {
			result[index] = nil
		} else if err != nil {
			return nil, err
		} else {
			result[index] = vote
		}
	}
	return result, nil
}
