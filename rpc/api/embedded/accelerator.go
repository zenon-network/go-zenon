package embedded

import (
	"encoding/json"
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

// AcceleratorApi implements the "embedded.accelerator" JSON-RPC
// namespace, which reads the state of the accelerator embedded contract
// as of the frontier momentum: funding projects, their phases and the
// pillar votes cast on both. Projects request funding denominated in
// ZNN and QSR (at most 5,000 ZNN and 50,000 QSR per project) and move
// through the statuses voting (0), active (1), paid (2), closed (3) and
// completed (4); phases use the same status values. Every exported
// method is served as embedded.accelerator.<lowerCamelMethodName>.
type AcceleratorApi struct {
	chain chain.Chain
	log   log15.Logger
}

// NewAcceleratorApi returns an AcceleratorApi bound to the given node's
// chain. It is called by the RPC server when the "embedded" namespace
// is enabled; it is not itself an RPC method.
func NewAcceleratorApi(z zenon.Zenon) *AcceleratorApi {
	return &AcceleratorApi{
		chain: z.Chain(),
		log:   common.RPCLogger.New("module", "embedded_accelerator_api"),
	}
}

// toProject enriches a project read from contract state with its vote
// breakdown and its phases (each with their own vote breakdown). A
// phase id whose entry cannot be read leaves a nil slot in Phases.
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

// Phase pairs one project phase read from contract state with the
// tally of the pillar votes cast on it.
type Phase struct {
	Phase *definition.Phase         `json:"phase"`
	Votes *definition.VoteBreakdown `json:"votes"`
}

// Project is one accelerator funding project: the contract-state fields
// (owner, descriptive metadata, the requested ZNN and QSR amounts in
// smallest units, unix-second timestamps and the status code described
// on AcceleratorApi) enriched with the tally of pillar votes on the
// project itself and with its phases, in PhaseIds order.
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

// ProjectMarshal is the JSON wire form of Project, with the requested
// ZNN and QSR amounts rendered as base-10 strings. It exists so the
// custom MarshalJSON/UnmarshalJSON of Project can round-trip amounts
// without precision loss.
type ProjectMarshal struct {
	Id                  types.Hash                `json:"id"`
	Owner               types.Address             `json:"owner"`
	Name                string                    `json:"name"`
	Description         string                    `json:"description"`
	Url                 string                    `json:"url"`
	ZnnFundsNeeded      string                    `json:"znnFundsNeeded"`
	QsrFundsNeeded      string                    `json:"qsrFundsNeeded"`
	CreationTimestamp   int64                     `json:"creationTimestamp"`
	LastUpdateTimestamp int64                     `json:"lastUpdateTimestamp"`
	Status              uint8                     `json:"status"`
	PhaseIds            []types.Hash              `json:"phaseIds"`
	Votes               *definition.VoteBreakdown `json:"votes"`
	Phases              []*Phase                  `json:"phases"`
}

// ToProjectMarshal converts the project to its JSON wire
// representation, rendering the requested ZNN and QSR amounts as
// base-10 strings.
func (p *Project) ToProjectMarshal() *ProjectMarshal {
	aux := &ProjectMarshal{
		Id:                  p.Id,
		Owner:               p.Owner,
		Name:                p.Name,
		Description:         p.Description,
		Url:                 p.Url,
		ZnnFundsNeeded:      p.ZnnFundsNeeded.String(),
		QsrFundsNeeded:      p.QsrFundsNeeded.String(),
		CreationTimestamp:   p.CreationTimestamp,
		LastUpdateTimestamp: p.LastUpdateTimestamp,
		Status:              p.Status,
		PhaseIds:            nil,
		Votes:               p.Votes,
		Phases:              nil,
	}
	aux.PhaseIds = make([]types.Hash, len(p.PhaseIds))
	for idx, phaseId := range p.PhaseIds {
		aux.PhaseIds[idx] = phaseId
	}

	aux.Phases = make([]*Phase, len(p.Phases))
	for idx, phase := range p.Phases {
		aux.Phases[idx] = phase
	}
	return aux
}

// MarshalJSON encodes the project via its ProjectMarshal wire form, so
// the requested amounts appear as base-10 JSON strings.
func (p *Project) MarshalJSON() ([]byte, error) {
	return json.Marshal(p.ToProjectMarshal())
}

// UnmarshalJSON decodes the ProjectMarshal wire form produced by
// MarshalJSON. Amount strings that are not valid base-10 integers
// decode to 0 rather than producing an error. The Phases slice is sized
// from the receiver's previous length rather than from the decoded
// data, so Phases does not round-trip reliably (decoding a non-empty
// phase list into a fresh Project panics); the other fields round-trip.
func (p *Project) UnmarshalJSON(data []byte) error {
	aux := new(ProjectMarshal)
	if err := json.Unmarshal(data, aux); err != nil {
		return err
	}
	p.Id = aux.Id
	p.Owner = aux.Owner
	p.Name = aux.Name
	p.Description = aux.Description
	p.Url = aux.Url
	p.ZnnFundsNeeded = common.StringToBigInt(aux.ZnnFundsNeeded)
	p.QsrFundsNeeded = common.StringToBigInt(aux.QsrFundsNeeded)
	p.CreationTimestamp = aux.CreationTimestamp
	p.LastUpdateTimestamp = aux.LastUpdateTimestamp
	p.Status = aux.Status
	p.PhaseIds = make([]types.Hash, len(aux.PhaseIds))
	for idx, phaseId := range aux.PhaseIds {
		p.PhaseIds[idx] = phaseId
	}
	p.Votes = aux.Votes
	p.Phases = make([]*Phase, len(p.Phases))
	for idx, phase := range aux.Phases {
		p.Phases[idx] = phase
	}
	return nil
}

// ProjectList is one page of projects. Count is the total number of
// projects in contract state, not the number of entries in List.
type ProjectList struct {
	Count int        `json:"count"`
	List  []*Project `json:"list"`
}

// === Getters for projects ===

// GetAll returns one page of every accelerator project, in any status,
// read from contract state at the frontier momentum and sorted by
// descending last-update timestamp, most recently updated first (the
// sort is stable, so projects with equal timestamps keep their storage
// order). Unlike most paged methods in this package, pageSize is not
// capped.
//
// JSON-RPC: embedded.accelerator.getAll
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

// GetProjectById returns the project with the given id, with its vote
// breakdown and phases, read from contract state at the frontier
// momentum. An id with no project entry produces an error.
//
// JSON-RPC: embedded.accelerator.getProjectById
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

// GetPhaseById returns the phase with the given id together with its
// vote breakdown, read from contract state at the frontier momentum. An
// id with no phase entry produces an error.
//
// JSON-RPC: embedded.accelerator.getPhaseById
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

// GetVoteBreakdown returns the tally of the pillar votes cast on the
// project or phase with the given id, as of the frontier momentum:
// Total counts every vote including abstentions, while Yes and No count
// only those choices. The id itself is not validated, so an id with no
// votes yields zero tallies rather than an error.
//
// JSON-RPC: embedded.accelerator.getVoteBreakdown
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

// GetPillarVotes returns the votes the named pillar has cast on the
// given project or phase ids, as of the frontier momentum, one result
// per requested hash in the same order. A hash the pillar has not voted
// on yields a nil entry instead of an error; vote values are 0 for yes, 1 for
// no and 2 for abstain.
//
// JSON-RPC: embedded.accelerator.getPillarVotes
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
