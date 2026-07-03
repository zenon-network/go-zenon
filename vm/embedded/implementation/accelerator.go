package implementation

import (
	"math/big"
	"regexp"
	"sort"

	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/vm/constants"
	"github.com/zenon-network/go-zenon/vm/embedded/definition"
	"github.com/zenon-network/go-zenon/vm/vm_context"
)

var (
	acceleratorLog = common.EmbeddedLogger.New("contract", "accelerator")
)

// IsAcceleratorRunning fails with constants.ErrAcceleratorEnded once
// more than constants.AcceleratorDuration seconds have elapsed since
// the genesis momentum; every accelerator method is gated on it.
func IsAcceleratorRunning(context vm_context.AccountVmContext) error {
	frontierMomentum, err := context.GetFrontierMomentum()
	genesisMomentum := context.GetGenesisMomentum()
	if err != nil {
		return err
	}
	if genesisMomentum.Timestamp.Unix()+constants.AcceleratorDuration < frontierMomentum.Timestamp.Unix() {
		return constants.ErrAcceleratorEnded
	}
	return nil
}

// checkMetaDataStatic validates the static fields shared by project
// and phase submissions:
//   - the name is 1 to constants.ProjectNameLengthMax bytes
//     (constants.ErrInvalidName)
//   - the description is 1 to constants.ProjectDescriptionLengthMax
//     bytes (constants.ErrInvalidDescription)
//   - the url is non-empty and matches a permissive
//     domain-with-optional-scheme pattern
//     (constants.ErrForbiddenParam)
//   - the requested funds are at most
//     constants.ProjectZnnMaximumFunds /
//     constants.ProjectQsrMaximumFunds
//     (constants.ErrAcceleratorInvalidFunds)
func checkMetaDataStatic(param *definition.AcceleratorParam) error {
	if len(param.Name) == 0 ||
		len(param.Name) > constants.ProjectNameLengthMax {
		return constants.ErrInvalidName
	}

	if len(param.Description) == 0 || len(param.Description) > constants.ProjectDescriptionLengthMax {
		return constants.ErrInvalidDescription
	}

	if ok, _ := regexp.MatchString("^([Hh][Tt][Tt][Pp][Ss]?://)?[a-zA-Z0-9]{2,60}\\.[a-zA-Z]{1,6}([-a-zA-Z0-9()@:%_+.~#?&/=]{0,100})$", param.Url); !ok || len(param.Url) == 0 {
		return constants.ErrForbiddenParam
	}

	if param.ZnnFundsNeeded.Cmp(constants.ProjectZnnMaximumFunds) > 0 || param.QsrFundsNeeded.Cmp(constants.ProjectQsrMaximumFunds) > 0 {
		return constants.ErrAcceleratorInvalidFunds
	}

	return nil
}

// checkReceivedFunds reports whether the project's phases together
// request exactly the project's needed ZNN and QSR — the condition on
// which a project counts as fully funded and is marked completed.
func checkReceivedFunds(context vm_context.AccountVmContext, project *definition.Project) bool {
	znnFunds := new(big.Int).Set(project.ZnnFundsNeeded)
	qsrFunds := new(big.Int).Set(project.QsrFundsNeeded)
	for _, phaseId := range project.PhaseIds {
		phase, err := definition.GetPhaseEntry(context.Storage(), phaseId)
		if err != nil {
			continue
		}
		znnFunds.Sub(znnFunds, phase.ZnnFundsNeeded)
		qsrFunds.Sub(qsrFunds, phase.QsrFundsNeeded)
	}
	if znnFunds.Sign() != 0 || qsrFunds.Sign() != 0 {
		return false
	}
	return true
}

// checkPhaseFunds fails with constants.ErrAcceleratorInvalidFunds
// when the project's phases are inconsistent with its budget: no
// single phase and no sum of phases may request more ZNN or QSR than
// the project needs, and the project's needs must stay within the
// constants.Project*MaximumFunds caps.
func checkPhaseFunds(context vm_context.AccountVmContext, project *definition.Project) error {
	znnPhaseFunds := big.NewInt(0)
	for _, phaseId := range project.PhaseIds {
		phase, err := definition.GetPhaseEntry(context.Storage(), phaseId)
		if err != nil {
			continue
		}
		if phase.ZnnFundsNeeded.Cmp(project.ZnnFundsNeeded) == +1 {
			return constants.ErrAcceleratorInvalidFunds
		}
		znnPhaseFunds.Add(znnPhaseFunds, phase.ZnnFundsNeeded)
	}
	if znnPhaseFunds.Cmp(project.ZnnFundsNeeded) == +1 || project.ZnnFundsNeeded.Cmp(constants.ProjectZnnMaximumFunds) == +1 {
		return constants.ErrAcceleratorInvalidFunds
	}

	qsrPhaseFunds := big.NewInt(0)
	for _, phaseId := range project.PhaseIds {
		phase, err := definition.GetPhaseEntry(context.Storage(), phaseId)
		if err != nil {
			continue
		}
		if phase.QsrFundsNeeded.Cmp(project.QsrFundsNeeded) == +1 {
			return constants.ErrAcceleratorInvalidFunds
		}
		qsrPhaseFunds.Add(qsrPhaseFunds, phase.QsrFundsNeeded)
	}
	if qsrPhaseFunds.Cmp(project.QsrFundsNeeded) == +1 || project.QsrFundsNeeded.Cmp(constants.ProjectQsrMaximumFunds) == +1 {
		return constants.ErrAcceleratorInvalidFunds
	}
	return nil
}

// CreateProjectMethod (CreateProject) submits a new accelerator
// funding project against the non-refundable
// constants.ProjectCreationAmount ZNN fee (1 ZNN), kept by the
// contract. The project starts in voting status with its id open for
// pillar votes.
type CreateProjectMethod struct {
	MethodName string
}

// GetPlasma quotes the EmbeddedSimple tier; the call sends no
// response block.
func (p *CreateProjectMethod) GetPlasma(plasmaTable *constants.PlasmaTable) (uint64, error) {
	return plasmaTable.EmbeddedSimple, nil
}

// ValidateSendBlock accepts a packed definition.AcceleratorParam
// passing the checkMetaDataStatic rules, carried by a send of exactly
// constants.ProjectCreationAmount ZNN; any other token or amount
// fails with constants.ErrInvalidTokenOrAmount.
func (p *CreateProjectMethod) ValidateSendBlock(block *nom.AccountBlock) error {
	var err error
	param := new(definition.AcceleratorParam)

	if err := definition.ABIAccelerator.UnpackMethod(param, p.MethodName, block.Data); err != nil {
		return constants.ErrUnpackError
	}

	if err := checkMetaDataStatic(param); err != nil {
		return err
	}

	// the cost to create an accelerated project is 1 znn
	if block.TokenStandard != types.ZnnTokenStandard || block.Amount.Cmp(constants.ProjectCreationAmount) != 0 {
		return constants.ErrInvalidTokenOrAmount
	}

	block.Data, err = definition.ABIAccelerator.PackMethod(p.MethodName, param.Name, param.Description, param.Url, param.ZnnFundsNeeded, param.QsrFundsNeeded)
	return err
}

// ReceiveBlock creates the project — gated on IsAcceleratorRunning —
// with the send block's hash as id, the sender as owner, the frontier
// timestamp as creation time, definition.VotingStatus and no phases,
// and registers the id as a votable hash so pillars can vote on it
// via VoteByName / VoteByProdAddress. No descendant blocks are
// emitted.
func (p *CreateProjectMethod) ReceiveBlock(context vm_context.AccountVmContext, sendBlock *nom.AccountBlock) ([]*nom.AccountBlock, error) {
	if err := p.ValidateSendBlock(sendBlock); err != nil {
		return nil, err
	}
	if err := IsAcceleratorRunning(context); err != nil {
		return nil, err
	}

	param := new(definition.AcceleratorParam)
	err := definition.ABIAccelerator.UnpackMethod(param, p.MethodName, sendBlock.Data)
	common.DealWithErr(err)

	frontierMomentum, err := context.GetFrontierMomentum()
	common.DealWithErr(err)

	project := new(definition.Project)
	project.Id = sendBlock.Hash
	project.Owner = sendBlock.Address
	project.Name = param.Name
	project.Description = param.Description
	project.Url = param.Url
	project.ZnnFundsNeeded = param.ZnnFundsNeeded
	project.QsrFundsNeeded = param.QsrFundsNeeded
	project.CreationTimestamp = frontierMomentum.Timestamp.Unix()
	project.LastUpdateTimestamp = frontierMomentum.Timestamp.Unix()
	project.Status = definition.VotingStatus
	project.PhaseIds = make([]types.Hash, 0)

	project.Save(context.Storage())

	// Add hash to votable hashes
	(&definition.VotableHash{Id: sendBlock.Hash}).Save(context.Storage())

	acceleratorLog.Debug("successfully create project", "project", project)
	return nil, nil
}

// AddPhaseMethod (AddPhase) lets a project's owner submit the next
// funding phase of an active project; each phase needs its own pillar
// vote before the Update run pays it out.
type AddPhaseMethod struct {
	MethodName string
}

// Fee returns a zero fee. It is not part of the embedded.Method
// interface and has no callers.
func (p *AddPhaseMethod) Fee() (*big.Int, error) {
	return big.NewInt(0), nil
}

// GetPlasma quotes the EmbeddedSimple tier; the call sends no
// response block.
func (p *AddPhaseMethod) GetPlasma(plasmaTable *constants.PlasmaTable) (uint64, error) {
	return plasmaTable.EmbeddedSimple, nil
}

// ValidateSendBlock accepts a packed definition.AcceleratorParam —
// the project id plus the phase metadata, passing the
// checkMetaDataStatic rules. Unlike most methods it places no
// restriction on the sent token or amount; anything sent along stays
// with the contract.
func (p *AddPhaseMethod) ValidateSendBlock(block *nom.AccountBlock) error {
	var err error
	param := new(definition.AcceleratorParam)

	if err := definition.ABIAccelerator.UnpackMethod(param, p.MethodName, block.Data); err != nil {
		return constants.ErrUnpackError
	}

	if err := checkMetaDataStatic(param); err != nil {
		return err
	}

	block.Data, err = definition.ABIAccelerator.PackMethod(p.MethodName, param.Id, param.Name, param.Description, param.Url, param.ZnnFundsNeeded, param.QsrFundsNeeded)
	return err
}

// ReceiveBlock appends a new phase to the project — gated on
// IsAcceleratorRunning. The project must exist
// (constants.ErrDataNonExistent), be owned by the sender, be in
// definition.ActiveStatus and have no unpaid current phase (all
// constants.ErrPermissionDenied). The phase is created with the send
// block's hash as id and definition.VotingStatus, appended to the
// project's phase list and registered as a votable hash; phase
// budgets exceeding the project's remaining needs roll the call back
// via checkPhaseFunds.
func (p *AddPhaseMethod) ReceiveBlock(context vm_context.AccountVmContext, sendBlock *nom.AccountBlock) ([]*nom.AccountBlock, error) {
	if err := p.ValidateSendBlock(sendBlock); err != nil {
		return nil, err
	}
	if err := IsAcceleratorRunning(context); err != nil {
		return nil, err
	}

	param := new(definition.AcceleratorParam)
	err := definition.ABIAccelerator.UnpackMethod(param, p.MethodName, sendBlock.Data)
	common.DealWithErr(err)

	// Check project exists and block is sent by owner
	project, err := definition.GetProjectEntry(context.Storage(), param.Id)
	if err != nil {
		return nil, constants.ErrDataNonExistent
	}
	if project.Owner != sendBlock.Address {
		return nil, constants.ErrPermissionDenied
	}
	if project.Status != definition.ActiveStatus {
		return nil, constants.ErrPermissionDenied
	}

	currentPhase, err := project.GetCurrentPhase(context.Storage())
	if currentPhase != nil && currentPhase.Status != definition.PaidStatus {
		// last phase not finalized
		return nil, constants.ErrPermissionDenied
	}

	frontierMomentum, err := context.GetFrontierMomentum()
	common.DealWithErr(err)

	phase := new(definition.Phase)
	phase.Id = sendBlock.Hash
	phase.ProjectId = project.Id
	phase.Name = param.Name
	phase.Description = param.Description
	phase.Url = param.Url
	phase.ZnnFundsNeeded = param.ZnnFundsNeeded
	phase.QsrFundsNeeded = param.QsrFundsNeeded
	phase.CreationTimestamp = frontierMomentum.Timestamp.Unix()
	phase.Status = definition.VotingStatus
	phase.Save(context.Storage())

	// Add phase ID to project & update project in storage
	project.PhaseIds = append(project.PhaseIds, phase.Id)
	project.LastUpdateTimestamp = frontierMomentum.Timestamp.Unix()
	project.Save(context.Storage())

	// Add hash to votable hashes
	(&definition.VotableHash{Id: sendBlock.Hash}).Save(context.Storage())

	if err := checkPhaseFunds(context, project); err != nil {
		return nil, err
	}

	acceleratorLog.Debug("successfully created phase", "phase", phase)
	return nil, nil
}

// checkAcceleratorVotes tallies the pillar votes on a project or
// phase id: it passes when the yes votes strictly outnumber the no
// votes and the total turnout strictly exceeds
// constants.VoteAcceptanceThreshold percent of the active pillars.
func checkAcceleratorVotes(context vm_context.AccountVmContext, id types.Hash, numPillars uint32) bool {
	breakdown := definition.GetVoteBreakdown(context.Storage(), id)

	ok := true
	// Test majority
	if breakdown.Yes <= breakdown.No {
		ok = false
	}
	// Test enough votes
	if breakdown.Total*100 <= numPillars*constants.VoteAcceptanceThreshold {
		ok = false
	}

	acceleratorLog.Debug("check accelerator votes", "votes", breakdown, "status", ok)
	return ok
}

// UpdateEmbeddedAcceleratorMethod (Update) advances the accelerator
// state machine: it resolves project votes and pays out accepted
// phases from the contract's balance. Anyone may call it; it is
// throttled to one run every constants.UpdateMinNumMomentums
// momentums.
type UpdateEmbeddedAcceleratorMethod struct {
	MethodName string
}

// GetPlasma quotes the EmbeddedWWithdraw tier, covering the phase
// payout blocks an update may send.
func (p *UpdateEmbeddedAcceleratorMethod) GetPlasma(plasmaTable *constants.PlasmaTable) (uint64, error) {
	return plasmaTable.EmbeddedWWithdraw, nil
}

// ValidateSendBlock accepts an argument-less call carrying no
// tokens.
func (p *UpdateEmbeddedAcceleratorMethod) ValidateSendBlock(block *nom.AccountBlock) error {
	var err error

	if err := definition.ABIAccelerator.UnpackEmptyMethod(p.MethodName, block.Data); err != nil {
		return constants.ErrUnpackError
	}

	if block.Amount.Sign() != 0 {
		return constants.ErrInvalidTokenOrAmount
	}

	block.Data, err = definition.ABIAccelerator.PackMethod(p.MethodName)
	return err
}

// ReceiveBlock runs the accelerator update — gated on
// IsAcceleratorRunning and the update throttle
// (constants.ErrUpdateTooRecent). Projects are processed oldest
// current phase first (projects without phases last):
//   - a project in voting whose constants.AcceleratorProjectVotingPeriod
//     window is still open becomes active when its votes pass
//     checkAcceleratorVotes; once the window has passed it is closed
//   - an active project's current phase in voting is paid when its
//     votes pass, the phase budget is consistent, the contract's
//     balance covers both the ZNN and QSR needs and fewer than
//     constants.MaxBlocksPerUpdate payout blocks have accumulated:
//     the phase is marked paid and two descendant sends transfer the
//     requested ZNN and QSR to the project owner, with the phase id
//     as block data; phases that cannot be paid stay in voting for a
//     later update
//
// A project whose phases have received everything it asked for is
// marked completed.
func (p *UpdateEmbeddedAcceleratorMethod) ReceiveBlock(context vm_context.AccountVmContext, sendBlock *nom.AccountBlock) ([]*nom.AccountBlock, error) {
	if err := p.ValidateSendBlock(sendBlock); err != nil {
		return nil, err
	}

	if err := IsAcceleratorRunning(context); err != nil {
		return nil, err
	}

	if err := checkAndPerformUpdate(context); err != nil {
		return nil, err
	}

	projectList, err := definition.GetProjectList(context.Storage())
	if err != nil {
		return nil, err
	}

	pillarList, err := context.MomentumStore().GetActivePillars()
	if err != nil {
		return nil, err
	}
	numPillars := uint32(len(pillarList))
	frontierMomentum, err := context.GetFrontierMomentum()
	if err != nil {
		return nil, err
	}

	blocks := make([]*nom.AccountBlock, 0)
	balanceZnn, err := context.GetBalance(types.ZnnTokenStandard)
	if err != nil {
		return nil, err
	}
	znnBalance := new(big.Int).Set(balanceZnn)
	balanceQsr, err := context.GetBalance(types.QsrTokenStandard)
	if err != nil {
		return nil, err
	}
	qsrBalance := new(big.Int).Set(balanceQsr)

	sort.SliceStable(projectList, func(i, j int) bool {
		var phaseITimestamp, phaseJTimestamp int64
		phaseI, err := projectList[i].GetCurrentPhase(context.Storage())
		if err != nil {
			phaseITimestamp = frontierMomentum.Timestamp.Unix()
		} else {
			phaseITimestamp = phaseI.CreationTimestamp
		}
		phaseJ, err := projectList[j].GetCurrentPhase(context.Storage())
		if err != nil {
			phaseJTimestamp = frontierMomentum.Timestamp.Unix()
		} else {
			phaseJTimestamp = phaseJ.CreationTimestamp
		}
		return phaseITimestamp < phaseJTimestamp
	})
	for _, project := range projectList {
		if project.Status == definition.VotingStatus {
			// Check if project voting period has ended
			if project.CreationTimestamp+constants.AcceleratorProjectVotingPeriod >= frontierMomentum.Timestamp.Unix() {
				ok := checkAcceleratorVotes(context, project.Id, numPillars)
				acceleratorLog.Debug("project passed voting period", "project-id", project.Id, "passed-votes", ok)
				if ok {
					project.Status = definition.ActiveStatus
					project.LastUpdateTimestamp = frontierMomentum.Timestamp.Unix()
					project.Save(context.Storage())
				}
			} else {
				project.Status = definition.ClosedStatus
				project.Save(context.Storage())
			}
		} else if project.Status == definition.ActiveStatus {
			phase, err := project.GetCurrentPhase(context.Storage())
			if err != nil {
				continue
			}
			// Mark current phase as Paid if possible
			if phase.Status == definition.VotingStatus {
				if checkAcceleratorVotes(context, phase.Id, numPillars) && len(blocks) < constants.MaxBlocksPerUpdate {
					if err := checkPhaseFunds(context, project); err != nil {
						continue
					}
					var znnBlock, qsrBlock *nom.AccountBlock = nil, nil

					if znnBalance.Cmp(phase.ZnnFundsNeeded) != -1 {
						znnBlock = &nom.AccountBlock{
							Address:       types.AcceleratorContract,
							ToAddress:     project.Owner,
							BlockType:     nom.BlockTypeContractSend,
							Amount:        phase.ZnnFundsNeeded,
							TokenStandard: types.ZnnTokenStandard,
							Data:          phase.Id.Bytes(),
						}
					} else {
						continue
					}

					if qsrBalance.Cmp(phase.QsrFundsNeeded) != -1 {
						qsrBlock = &nom.AccountBlock{
							Address:       types.AcceleratorContract,
							ToAddress:     project.Owner,
							BlockType:     nom.BlockTypeContractSend,
							Amount:        phase.QsrFundsNeeded,
							TokenStandard: types.QsrTokenStandard,
							Data:          phase.Id.Bytes(),
						}
					} else {
						continue
					}

					znnBalance.Sub(znnBalance, phase.ZnnFundsNeeded)
					blocks = append(blocks, znnBlock)

					qsrBalance.Sub(qsrBalance, phase.QsrFundsNeeded)
					blocks = append(blocks, qsrBlock)

					phase.Status = definition.PaidStatus
					phase.AcceptedTimestamp = frontierMomentum.Timestamp.Unix()
					phase.Save(context.Storage())

					project.LastUpdateTimestamp = frontierMomentum.Timestamp.Unix()
					if checkReceivedFunds(context, project) {
						project.Status = definition.CompletedStatus
					}
					project.Save(context.Storage())
					acceleratorLog.Debug("finishing and paying phase", "project-id", project.Id, "phase-id", phase.Id, "znn-amount", phase.ZnnFundsNeeded, "qsr-amount", phase.QsrFundsNeeded)
				} else {
					acceleratorLog.Debug("not enough votes to finish phase", "project-id", project.Id, "phase-id", phase.Id)
				}
			}
		}
	}

	return blocks, nil
}

// UpdatePhaseMethod (UpdatePhase) lets a project's owner replace the
// current phase while it is still in voting, restarting the vote from
// scratch.
type UpdatePhaseMethod struct {
	MethodName string
}

// GetPlasma quotes the EmbeddedSimple tier; the call sends no
// response block.
func (p *UpdatePhaseMethod) GetPlasma(plasmaTable *constants.PlasmaTable) (uint64, error) {
	return plasmaTable.EmbeddedSimple, nil
}

// ValidateSendBlock accepts a packed definition.AcceleratorParam —
// the project id plus the replacement phase metadata, passing the
// checkMetaDataStatic rules. Like AddPhase it places no restriction
// on the sent token or amount; anything sent along stays with the
// contract.
func (p *UpdatePhaseMethod) ValidateSendBlock(block *nom.AccountBlock) error {
	var err error
	param := new(definition.AcceleratorParam)

	if err := definition.ABIAccelerator.UnpackMethod(param, p.MethodName, block.Data); err != nil {
		return constants.ErrUnpackError
	}

	if err := checkMetaDataStatic(param); err != nil {
		return err
	}

	block.Data, err = definition.ABIAccelerator.PackMethod(p.MethodName, param.Id, param.Name, param.Description, param.Url, param.ZnnFundsNeeded, param.QsrFundsNeeded)
	return err
}

// ReceiveBlock replaces the project's current phase — gated on
// IsAcceleratorRunning. The project must exist
// (constants.ErrDataNonExistent) and be owned by the sender
// (constants.ErrPermissionDenied); the current phase must exist
// (constants.ErrDataNonExistent) and still be in
// definition.VotingStatus (constants.ErrPermissionDenied). A new
// phase with the send block's hash as id and a fresh creation
// timestamp takes the old one's slot in the project's phase list —
// checked against the budget via checkPhaseFunds — and the vote
// restarts: all pillar votes on the old phase and its votable hash
// are deleted, the new id is registered and the old phase entry
// removed.
func (p *UpdatePhaseMethod) ReceiveBlock(context vm_context.AccountVmContext, sendBlock *nom.AccountBlock) ([]*nom.AccountBlock, error) {
	if err := p.ValidateSendBlock(sendBlock); err != nil {
		return nil, err
	}
	if err := IsAcceleratorRunning(context); err != nil {
		return nil, err
	}

	param := new(definition.AcceleratorParam)
	err := definition.ABIAccelerator.UnpackMethod(param, p.MethodName, sendBlock.Data)
	common.DealWithErr(err)

	// Check project exists and block is send by owner
	project, err := definition.GetProjectEntry(context.Storage(), param.Id)
	if err != nil {
		return nil, constants.ErrDataNonExistent
	}
	if project.Owner != sendBlock.Address {
		return nil, constants.ErrPermissionDenied
	}

	phase, err := project.GetCurrentPhase(context.Storage())
	if err != nil {
		return nil, constants.ErrDataNonExistent
	}
	if phase.Status != definition.VotingStatus {
		return nil, constants.ErrPermissionDenied
	}

	frontierMomentum, err := context.GetFrontierMomentum()
	common.DealWithErr(err)

	newPhase := new(definition.Phase)
	newPhase.Id = sendBlock.Hash
	newPhase.Name = param.Name
	newPhase.Description = param.Description
	newPhase.Url = param.Url
	newPhase.ZnnFundsNeeded = param.ZnnFundsNeeded
	newPhase.QsrFundsNeeded = param.QsrFundsNeeded
	newPhase.CreationTimestamp = frontierMomentum.Timestamp.Unix()
	newPhase.Status = definition.VotingStatus
	newPhase.ProjectId = project.Id
	newPhase.Save(context.Storage())

	project.PhaseIds[len(project.PhaseIds)-1] = newPhase.Id
	project.Save(context.Storage())

	if err := checkPhaseFunds(context, project); err != nil {
		return nil, err
	}

	// reset votes
	votes := definition.GetAllPillarVotes(context.Storage(), phase.Id)
	for _, vote := range votes {
		acceleratorLog.Debug("delete pillar vote due to phase update", "old-pillar-vote", vote)
		vote.Delete(context.Storage())
	}

	// Remove prev hash from votable hashes
	(&definition.VotableHash{Id: phase.Id}).Delete(context.Storage())
	acceleratorLog.Debug("delete phase hash due to phase update", "old-phase-hash", phase.Id)

	// Add hash to votable hashes
	(&definition.VotableHash{Id: sendBlock.Hash}).Save(context.Storage())

	phase.Delete(context.Storage())

	acceleratorLog.Debug("successfully updated phase", "old-phase", phase, "new-phase", newPhase)
	return nil, nil
}
