package definition

import (
	"encoding/json"
	"math/big"
	"strings"

	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/db"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/vm/abi"
	"github.com/zenon-network/go-zenon/vm/constants"
)

const (
	// VotingStatus (0) marks a project or phase still collecting
	// pillar votes.
	VotingStatus uint8 = iota
	// ActiveStatus (1) marks a project accepted by vote, eligible to
	// submit phases; phases never carry this status.
	ActiveStatus
	// PaidStatus (2) marks a phase whose requested funds have been
	// paid out to the project owner.
	PaidStatus
	// ClosedStatus (3) marks a project whose voting period ended
	// without acceptance; phases never carry this status.
	ClosedStatus
	// CompletedStatus (4) marks a project all of whose requested
	// funds have been paid.
	CompletedStatus

	// jsonAccelerator is the ABI JSON of the accelerator embedded
	// contract: project creation, phase submission and update, the
	// shared pillar-vote methods and the stored project and phase
	// variables. Parsed into ABIAccelerator.
	jsonAccelerator = `
	[
		{"type":"function","name":"Update", "inputs":[]},
		{"type":"function","name":"Donate", "inputs":[]},
		
		{"type":"function","name":"CreateProject", "inputs":[
			{"name":"name","type":"string"},
			{"name":"description","type":"string"},
			{"name":"url","type":"string"},
			{"name":"znnFundsNeeded","type":"uint256"},
			{"name":"qsrFundsNeeded","type":"uint256"}
		]},
		{"type":"function","name":"AddPhase", "inputs":[
			{"name":"id","type":"hash"},
			{"name":"name","type":"string"},
			{"name":"description","type":"string"},
			{"name":"url","type":"string"},
			{"name":"znnFundsNeeded","type":"uint256"},
			{"name":"qsrFundsNeeded","type":"uint256"}
		]},
		{"type":"function","name":"UpdatePhase", "inputs":[
			{"name":"id","type":"hash"},
			{"name":"name","type":"string"},
			{"name":"description","type":"string"},
			{"name":"url","type":"string"},
			{"name":"znnFundsNeeded","type":"uint256"},
			{"name":"qsrFundsNeeded","type":"uint256"}
		]},
		{"type":"function","name":"VoteByName","inputs":[
			{"name":"id","type":"hash"},
			{"name":"name","type":"string"},
			{"name":"vote","type":"uint8"}
		]},
		{"type":"function","name":"VoteByProdAddress","inputs":[
			{"name":"id","type":"hash"},
			{"name":"vote","type":"uint8"}
		]},

		{"type":"variable","name":"project","inputs":[
			{"name":"id", "type":"hash"},
			{"name":"owner","type":"address"},
			{"name":"name","type":"string"},
			{"name":"description","type":"string"},
			{"name":"url","type":"string"},
			{"name":"znnFundsNeeded","type":"uint256"},
			{"name":"qsrFundsNeeded","type":"uint256"},
			{"name":"creationTimestamp","type":"int64"},
			{"name":"lastUpdateTimestamp","type":"int64"},
			{"name":"status","type":"uint8"},
			{"name":"phaseIds","type":"hash[]"}
		]},

		{"type":"variable","name":"phase","inputs":[
			{"name":"id", "type":"hash"},
			{"name":"projectId", "type":"hash"},
			{"name":"name","type":"string"},
			{"name":"description","type":"string"},
			{"name":"url","type":"string"},
			{"name":"znnFundsNeeded","type":"uint256"},
			{"name":"qsrFundsNeeded","type":"uint256"},
			{"name":"creationTimestamp","type":"int64"},
			{"name":"acceptedTimestamp","type":"int64"},
			{"name":"status","type":"uint8"}
		]}
	]`

	// CreateProjectMethodName names the method that submits a new
	// funding project, paying the constants.ProjectCreationAmount ZNN
	// fee and opening the project for pillar voting.
	CreateProjectMethodName = "CreateProject"
	// AddPhaseMethodName names the method by which a project's owner
	// submits the next phase of an active project for voting.
	AddPhaseMethodName = "AddPhase"
	// UpdatePhaseMethodName names the method by which a project's
	// owner replaces the current phase while it is still in voting.
	UpdatePhaseMethodName = "UpdatePhase"

	// ProjectVariableName is the ABI variable holding a project.
	ProjectVariableName = "project"
	// PhaseVariableName is the ABI variable holding a phase.
	PhaseVariableName = "phase"

	// The key prefixes are declared with iota, which has been
	// counting the const specs above (the statuses, the ABI JSON and
	// the name constants), so projectKeyPrefix is byte 12 and
	// phaseKeyPrefix byte 13 — not 1 and 2. Writers and readers share
	// the constants, so the values only matter for raw storage
	// inspection.
	_ byte = iota
	projectKeyPrefix
	phaseKeyPrefix
)

var (
	// ABIAccelerator is the parsed ABI of the accelerator embedded
	// contract.
	ABIAccelerator = abi.JSONToABIContract(strings.NewReader(jsonAccelerator))
)

// Project is one stored accelerator funding project. Id is the hash
// of the CreateProject send block and doubles as the VotableHash id
// pillars vote on; ZnnFundsNeeded and QsrFundsNeeded (smallest units)
// are the project's total budget, which its phases collectively draw
// down. CreationTimestamp and LastUpdateTimestamp are unix seconds,
// Status one of the five status
// constants and PhaseIds the project's phase ids in submission order
// (the last one is the current phase). Entries are stored under
// projectKeyPrefix (12) followed by the 32-byte id.
type Project struct {
	Id                  types.Hash    `json:"id"`
	Owner               types.Address `json:"owner"`
	Name                string        `json:"name"`
	Description         string        `json:"description"`
	Url                 string        `json:"url"`
	ZnnFundsNeeded      *big.Int      `json:"znnFundsNeeded"`
	QsrFundsNeeded      *big.Int      `json:"qsrFundsNeeded"`
	CreationTimestamp   int64         `json:"creationTimestamp"`
	LastUpdateTimestamp int64         `json:"lastUpdateTimestamp"`
	Status              uint8         `json:"status"`
	PhaseIds            []types.Hash
}

// AcceleratorParam carries the arguments of CreateProject, AddPhase
// and UpdatePhase: name, description, url and the requested ZNN and
// QSR (smallest units). Id is the project id for AddPhase and
// UpdatePhase (which always operates on the project's current phase);
// CreateProject has no id parameter.
type AcceleratorParam struct {
	Id             types.Hash
	Name           string
	Description    string
	Url            string
	ZnnFundsNeeded *big.Int
	QsrFundsNeeded *big.Int
}

// Save stores the project under its id key, panicking via
// common.DealWithErr on database errors.
func (project *Project) Save(context db.DB) {
	common.DealWithErr(context.Put(project.Key(), project.Data()))
}

// Delete removes the project, panicking via common.DealWithErr on
// database errors.
func (project *Project) Delete(context db.DB) {
	common.DealWithErr(context.Delete(project.Key()))
}

// Key is projectKeyPrefix (12) followed by the 32-byte id.
func (project *Project) Key() []byte {
	return common.JoinBytes([]byte{projectKeyPrefix}, project.Id.Bytes())
}

// Data packs the full project state including the phase id list;
// packing failures panic.
func (project *Project) Data() []byte {
	return ABIAccelerator.PackVariablePanic(
		ProjectVariableName,
		project.Id,
		project.Owner,
		project.Name,
		project.Description,
		project.Url,
		project.ZnnFundsNeeded,
		project.QsrFundsNeeded,
		project.CreationTimestamp,
		project.LastUpdateTimestamp,
		project.Status,
		project.PhaseIds,
	)
}

// GetCurrentPhase returns the project's latest phase (the last entry
// of PhaseIds), or constants.ErrDataNonExistent if the project has no
// phases or the phase entry is missing.
func (project *Project) GetCurrentPhase(context db.DB) (*Phase, error) {
	if len(project.PhaseIds) > 0 {
		currentActivePhaseId := project.PhaseIds[len(project.PhaseIds)-1]
		return GetPhaseEntry(context, currentActivePhaseId)
	} else {
		return nil, constants.ErrDataNonExistent
	}
}

func parseProject(data []byte) *Project {
	project := new(Project)
	ABIAccelerator.UnpackVariablePanic(project, ProjectVariableName, data)
	return project
}

// Phase is one stored funding phase of a project. Id is the hash of
// the AddPhase (or latest UpdatePhase) send block and doubles as the
// VotableHash id pillars vote on — updating a phase therefore changes
// its id and discards the votes cast so far. ZnnFundsNeeded and
// QsrFundsNeeded (smallest units) are paid to the project owner when
// the phase passes its vote. CreationTimestamp and AcceptedTimestamp
// are unix seconds; AcceptedTimestamp is zero until the phase is
// paid. Entries are stored under phaseKeyPrefix (13) followed by the
// 32-byte id.
type Phase struct {
	Id                types.Hash `json:"id"`
	ProjectId         types.Hash `json:"projectID"`
	Name              string     `json:"name"`
	Description       string     `json:"description"`
	Url               string     `json:"url"`
	ZnnFundsNeeded    *big.Int   `json:"znnFundsNeeded"`
	QsrFundsNeeded    *big.Int   `json:"qsrFundsNeeded"`
	CreationTimestamp int64      `json:"creationTimestamp"`
	AcceptedTimestamp int64      `json:"acceptedTimestamp"`
	Status            uint8      `json:"status"`
}

// PhaseMarshal is the JSON form of Phase, with the amounts rendered
// as base-10 strings to survive clients that parse numbers as 64-bit
// floats.
type PhaseMarshal struct {
	Id                types.Hash `json:"id"`
	ProjectId         types.Hash `json:"projectID"`
	Name              string     `json:"name"`
	Description       string     `json:"description"`
	Url               string     `json:"url"`
	ZnnFundsNeeded    string     `json:"znnFundsNeeded"`
	QsrFundsNeeded    string     `json:"qsrFundsNeeded"`
	CreationTimestamp int64      `json:"creationTimestamp"`
	AcceptedTimestamp int64      `json:"acceptedTimestamp"`
	Status            uint8      `json:"status"`
}

// ToProjectMarshal converts the phase to its JSON form with
// string-encoded amounts. Despite the name it marshals a phase, not a
// project.
func (phase *Phase) ToProjectMarshal() *PhaseMarshal {
	aux := &PhaseMarshal{
		Id:                phase.Id,
		ProjectId:         phase.ProjectId,
		Name:              phase.Name,
		Description:       phase.Description,
		Url:               phase.Url,
		ZnnFundsNeeded:    phase.ZnnFundsNeeded.String(),
		QsrFundsNeeded:    phase.QsrFundsNeeded.String(),
		CreationTimestamp: phase.CreationTimestamp,
		AcceptedTimestamp: phase.AcceptedTimestamp,
		Status:            phase.Status,
	}
	return aux
}

// MarshalJSON encodes the phase through PhaseMarshal.
func (phase *Phase) MarshalJSON() ([]byte, error) {
	return json.Marshal(phase.ToProjectMarshal())
}

// UnmarshalJSON decodes the phase from its PhaseMarshal form, parsing
// the string amounts back into big.Int values.
func (phase *Phase) UnmarshalJSON(data []byte) error {
	aux := new(PhaseMarshal)
	if err := json.Unmarshal(data, aux); err != nil {
		return err
	}
	phase.Id = aux.Id
	phase.ProjectId = aux.ProjectId
	phase.Name = aux.Name
	phase.Description = aux.Description
	phase.Url = aux.Url
	phase.ZnnFundsNeeded = common.StringToBigInt(aux.ZnnFundsNeeded)
	phase.QsrFundsNeeded = common.StringToBigInt(aux.QsrFundsNeeded)
	phase.CreationTimestamp = aux.CreationTimestamp
	phase.AcceptedTimestamp = aux.AcceptedTimestamp
	phase.Status = aux.Status
	return nil
}

// Save stores the phase under its id key, panicking via
// common.DealWithErr on database errors.
func (phase *Phase) Save(context db.DB) {
	common.DealWithErr(context.Put(phase.Key(), phase.Data()))
}

// Delete removes the phase, panicking via common.DealWithErr on
// database errors.
func (phase *Phase) Delete(context db.DB) {
	common.DealWithErr(context.Delete(phase.Key()))
}

// Key is phaseKeyPrefix (13) followed by the 32-byte id.
func (phase *Phase) Key() []byte {
	return common.JoinBytes([]byte{phaseKeyPrefix}, phase.Id.Bytes())
}

// Data packs the full phase state; packing failures panic.
func (phase *Phase) Data() []byte {
	return ABIAccelerator.PackVariablePanic(
		PhaseVariableName,
		phase.Id,
		phase.ProjectId,
		phase.Name,
		phase.Description,
		phase.Url,
		phase.ZnnFundsNeeded,
		phase.QsrFundsNeeded,
		phase.CreationTimestamp,
		phase.AcceptedTimestamp,
		phase.Status,
	)
}

func parsePhase(data []byte) *Phase {
	phase := new(Phase)
	ABIAccelerator.UnpackVariablePanic(phase, PhaseVariableName, data)
	return phase
}

// GetProjectList returns every stored project, in storage-key (id
// byte) order; database errors panic via common.DealWithErr, so the
// returned error is always nil.
func GetProjectList(context db.DB) ([]*Project, error) {
	iterator := context.NewIterator([]byte{projectKeyPrefix})
	defer iterator.Release()
	projectList := make([]*Project, 0)

	for {
		if !iterator.Next() {
			common.DealWithErr(iterator.Error())
			break
		}
		projectList = append(projectList, parseProject(iterator.Value()))
	}

	return projectList, nil
}

// GetProjectEntry returns the project stored under id, or
// constants.ErrDataNonExistent if none exists; database errors panic
// via common.DealWithErr.
func GetProjectEntry(context db.DB, id types.Hash) (*Project, error) {
	key := (&Project{Id: id}).Key()
	data, err := context.Get(key)
	common.DealWithErr(err)
	if len(data) == 0 {
		return nil, constants.ErrDataNonExistent
	} else {
		return parseProject(data), nil
	}
}

// GetPhaseEntry returns the phase stored under id, or
// constants.ErrDataNonExistent if none exists; database errors panic
// via common.DealWithErr.
func GetPhaseEntry(context db.DB, id types.Hash) (*Phase, error) {
	key := (&Phase{Id: id}).Key()
	data, err := context.Get(key)
	common.DealWithErr(err)
	if len(data) == 0 {
		return nil, constants.ErrDataNonExistent
	} else {
		return parsePhase(data), nil
	}
}
