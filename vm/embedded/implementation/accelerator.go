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

type CreateProjectMethod struct {
	MethodName string
}

func (p *CreateProjectMethod) GetPlasma(plasmaTable *constants.PlasmaTable) (uint64, error) {
	return plasmaTable.EmbeddedSimple, nil
}
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

type AddPhaseMethod struct {
	MethodName string
}

func (p *AddPhaseMethod) Fee() (*big.Int, error) {
	return big.NewInt(0), nil
}
func (p *AddPhaseMethod) GetPlasma(plasmaTable *constants.PlasmaTable) (uint64, error) {
	return plasmaTable.EmbeddedSimple, nil
}
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

type UpdateEmbeddedAcceleratorMethod struct {
	MethodName string
}

func (p *UpdateEmbeddedAcceleratorMethod) GetPlasma(plasmaTable *constants.PlasmaTable) (uint64, error) {
	return plasmaTable.EmbeddedWWithdraw, nil
}
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

type UpdatePhaseMethod struct {
	MethodName string
}

func (p *UpdatePhaseMethod) GetPlasma(plasmaTable *constants.PlasmaTable) (uint64, error) {
	return plasmaTable.EmbeddedSimple, nil
}
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
