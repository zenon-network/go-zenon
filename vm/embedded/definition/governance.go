package definition

import (
	"strings"

	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/crypto"
	"github.com/zenon-network/go-zenon/common/db"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/vm/abi"
	"github.com/zenon-network/go-zenon/vm/constants"
)

const (
	GovernanceProposalActive   uint8 = 0
	GovernanceProposalPassed   uint8 = 1
	GovernanceProposalRejected uint8 = 2
	GovernanceProposalExecuted uint8 = 3

	jsonGovernance = `
	[
		{"type":"function","name":"CreateProposal","inputs":[
			{"name":"title","type":"string"},
			{"name":"description","type":"string"},
			{"name":"url","type":"string"},
			{"name":"votingPeriod","type":"uint64"}
		]},
		{"type":"function","name":"CastVote","inputs":[
			{"name":"id","type":"hash"},
			{"name":"vote","type":"uint8"}
		]},
		{"type":"function","name":"Execute","inputs":[
			{"name":"id","type":"hash"}
		]},

		{"type":"variable","name":"governanceProposal","inputs":[
			{"name":"id","type":"hash"},
			{"name":"creator","type":"address"},
			{"name":"title","type":"string"},
			{"name":"description","type":"string"},
			{"name":"url","type":"string"},
			{"name":"creationMomentum","type":"uint64"},
			{"name":"votingEndMomentum","type":"uint64"},
			{"name":"yesVotes","type":"uint32"},
			{"name":"noVotes","type":"uint32"},
			{"name":"abstainVotes","type":"uint32"},
			{"name":"status","type":"uint8"}
		]},
		{"type":"variable","name":"governanceVote","inputs":[
			{"name":"id","type":"hash"},
			{"name":"voter","type":"address"},
			{"name":"vote","type":"uint8"}
		]}
	]`

	CreateProposalMethodName = "CreateProposal"
	CastVoteMethodName       = "CastVote"
	ExecuteMethodName        = "Execute"

	GovernanceProposalVariableName = "governanceProposal"
	GovernanceVoteVariableName     = "governanceVote"
)

const (
	_ byte = iota
	governanceProposalKeyPrefix
	governanceVoteKeyPrefix
)

var (
	ABIGovernance = abi.JSONToABIContract(strings.NewReader(jsonGovernance))
)

// GovernanceProposal stores a governance proposal.
type GovernanceProposal struct {
	Id                types.Hash    `json:"id"`
	Creator           types.Address `json:"creator"`
	Title             string        `json:"title"`
	Description       string        `json:"description"`
	Url               string        `json:"url"`
	CreationMomentum  uint64        `json:"creationMomentum"`
	VotingEndMomentum uint64        `json:"votingEndMomentum"`
	YesVotes          uint32        `json:"yesVotes"`
	NoVotes           uint32        `json:"noVotes"`
	AbstainVotes      uint32        `json:"abstainVotes"`
	Status            uint8         `json:"status"`
}

func (p *GovernanceProposal) Save(context db.DB) {
	common.DealWithErr(context.Put(p.Key(), p.Data()))
}
func (p *GovernanceProposal) Delete(context db.DB) {
	common.DealWithErr(context.Delete(p.Key()))
}
func (p *GovernanceProposal) Key() []byte {
	return common.JoinBytes([]byte{governanceProposalKeyPrefix}, p.Id.Bytes())
}
func (p *GovernanceProposal) Data() []byte {
	return ABIGovernance.PackVariablePanic(
		GovernanceProposalVariableName,
		p.Id,
		p.Creator,
		p.Title,
		p.Description,
		p.Url,
		p.CreationMomentum,
		p.VotingEndMomentum,
		p.YesVotes,
		p.NoVotes,
		p.AbstainVotes,
		p.Status,
	)
}

func parseGovernanceProposal(data []byte) *GovernanceProposal {
	proposal := new(GovernanceProposal)
	ABIGovernance.UnpackVariablePanic(proposal, GovernanceProposalVariableName, data)
	return proposal
}

func GetGovernanceProposalEntry(context db.DB, id types.Hash) (*GovernanceProposal, error) {
	key := (&GovernanceProposal{Id: id}).Key()
	data, err := context.Get(key)
	common.DealWithErr(err)
	if len(data) == 0 {
		return nil, constants.ErrDataNonExistent
	}
	return parseGovernanceProposal(data), nil
}

func GetAllGovernanceProposals(context db.DB) ([]*GovernanceProposal, error) {
	iterator := context.NewIterator([]byte{governanceProposalKeyPrefix})
	defer iterator.Release()
	proposals := make([]*GovernanceProposal, 0)
	for {
		if !iterator.Next() {
			common.DealWithErr(iterator.Error())
			break
		}
		proposals = append(proposals, parseGovernanceProposal(iterator.Value()))
	}
	return proposals, nil
}

// GovernanceVote stores a single Pillar's vote on a proposal.
type GovernanceVote struct {
	Id    types.Hash    `json:"id"`
	Voter types.Address `json:"voter"`
	Vote  uint8         `json:"vote"`
}

func (v *GovernanceVote) Save(context db.DB) {
	common.DealWithErr(context.Put(v.Key(), v.Data()))
}
func (v *GovernanceVote) Delete(context db.DB) {
	common.DealWithErr(context.Delete(v.Key()))
}
func (v *GovernanceVote) Key() []byte {
	voterHash := crypto.Hash(v.Voter.Bytes())[:20]
	return common.JoinBytes([]byte{governanceVoteKeyPrefix}, v.Id.Bytes(), voterHash)
}
func (v *GovernanceVote) Data() []byte {
	return ABIGovernance.PackVariablePanic(
		GovernanceVoteVariableName,
		v.Id,
		v.Voter,
		v.Vote,
	)
}

func parseGovernanceVote(data []byte) *GovernanceVote {
	vote := new(GovernanceVote)
	ABIGovernance.UnpackVariablePanic(vote, GovernanceVoteVariableName, data)
	return vote
}

// GetGovernanceVote returns a Pillar's vote on a specific proposal.
func GetGovernanceVote(context db.DB, id types.Hash, voter types.Address) (*GovernanceVote, error) {
	key := (&GovernanceVote{Id: id, Voter: voter}).Key()
	data, err := context.Get(key)
	common.DealWithErr(err)
	if len(data) == 0 {
		return nil, constants.ErrDataNonExistent
	}
	return parseGovernanceVote(data), nil
}

// GetAllGovernanceVotes returns all votes for a given proposal.
func GetAllGovernanceVotes(context db.DB, id types.Hash) []*GovernanceVote {
	iterator := context.NewIterator([]byte{governanceVoteKeyPrefix})
	defer iterator.Release()
	votes := make([]*GovernanceVote, 0)
	for {
		if !iterator.Next() {
			common.DealWithErr(iterator.Error())
			break
		}
		vote := parseGovernanceVote(iterator.Value())
		if vote.Id == id {
			votes = append(votes, vote)
		}
	}
	return votes
}

// CreateProposalParam is the parameter struct for CreateProposal method.
type CreateProposalParam struct {
	Title        string
	Description  string
	Url          string
	VotingPeriod uint64
}

// CastVoteParam is the parameter struct for CastVote method.
type CastVoteParam struct {
	Id   types.Hash
	Vote uint8
}

// ExecuteParam is the parameter struct for Execute method.
type ExecuteParam struct {
	Id types.Hash
}
