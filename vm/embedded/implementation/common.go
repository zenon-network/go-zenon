package implementation

import (
	"math/big"
	"reflect"

	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/vm/constants"
	"github.com/zenon-network/go-zenon/vm/embedded/definition"
	"github.com/zenon-network/go-zenon/vm/vm_context"
)

var (
	commonLog = common.EmbeddedLogger.New("contract", "common")
)

// CanPerformUpdate checks if embedded contract can be updated
//   - returns util.ErrUpdateTooRecent if not due
func CanPerformUpdate(context vm_context.AccountVmContext) error {
	momentum, err := context.GetFrontierMomentum()
	if err != nil {
		return err
	}

	currentHeight := momentum.Height
	lastUpdate, err := definition.GetLastUpdate(context.Storage())
	if err != nil {
		return err
	}

	if lastUpdate.Height+constants.UpdateMinNumMomentums <= currentHeight {
		return nil
	} else {
		return constants.ErrUpdateTooRecent
	}
}

// Generic function, used to limits calls to the update method once every UpdateMinNumMomentums blocks
//   - automatically stores new height
//   - returns util.ErrUpdateTooRecent if not due
func checkAndPerformUpdate(context vm_context.AccountVmContext) error {
	if err := CanPerformUpdate(context); err != nil {
		return err
	}

	lastUpdate, _ := definition.GetLastUpdate(context.Storage())
	momentum, _ := context.GetFrontierMomentum()
	commonLog.Debug("updating contract state", "contract", *context.Address(), "current-height", momentum.Height, "last-update-height", lastUpdate.Height)

	lastUpdate.Height = momentum.Height
	common.DealWithErr(lastUpdate.Save(context.Storage()))
	return nil
}

// CanPerformEpochUpdate checks if embedded contract can perform an epoch update, used most commonly to give rewards
//   - returns util.EpochUpdateNotDue if not due
func CanPerformEpochUpdate(context vm_context.AccountVmContext, epoch *definition.LastEpochUpdate) error {
	_, currentEpochEndTime := context.EpochTicker().ToTime(uint64(epoch.LastEpoch + 1))
	frontierMomentum, err := context.GetFrontierMomentum()
	if err != nil {
		return err
	}

	if frontierMomentum.Timestamp.Unix() < currentEpochEndTime.Unix()+constants.RewardTimeLimit {
		return constants.ErrEpochUpdateTooRecent
	}
	return nil
}

// Generic function to check if epoch can be updated, if true, update it and save
//   - automatically moves up epoch by one if possible
//   - returns util.EpochUpdateNotDue if not due
func checkAndPerformUpdateEpoch(context vm_context.AccountVmContext, epoch *definition.LastEpochUpdate) error {
	if err := CanPerformEpochUpdate(context, epoch); err != nil {
		return err
	}

	epoch.LastEpoch += 1
	return epoch.Save(context.Storage())
}

// CollectRewardMethod is a common embedded.method used to issue tokens to users based on RewardDeposit object.
// When issuing rewards, the embedded adds the respected value in the RewardDeposit object in the DB and afterwards,
// the users will call this method to receive the tokens.
type CollectRewardMethod struct {
	MethodName string
	Plasma     uint64
}

func addReward(context vm_context.AccountVmContext, epoch uint64, reward definition.RewardDeposit) {
	deposit, err := definition.GetRewardDeposit(context.Storage(), reward.Address)
	common.DealWithErr(err)

	deposit.Znn.Add(deposit.Znn, reward.Znn)
	deposit.Qsr.Add(deposit.Qsr, reward.Qsr)
	common.DealWithErr(deposit.Save(context.Storage()))

	hisDeposit, err := definition.GetRewardDepositHistory(context.Storage(), epoch, reward.Address)
	common.DealWithErr(err)
	hisDeposit.Znn.Add(hisDeposit.Znn, reward.Znn)
	hisDeposit.Qsr.Add(hisDeposit.Qsr, reward.Qsr)
	common.DealWithErr(hisDeposit.Save(context.Storage()))
}

func (p *CollectRewardMethod) GetPlasma(plasmaTable *constants.PlasmaTable) (uint64, error) {
	// in case of sentinels it issues 2 rewards, but it's not called enough to cause issues
	return p.Plasma, nil
}
func (p *CollectRewardMethod) ValidateSendBlock(block *nom.AccountBlock) error {
	var err error

	if err := definition.ABICommon.UnpackEmptyMethod(p.MethodName, block.Data); err != nil {
		return constants.ErrUnpackError
	}

	if block.Amount.Sign() != 0 {
		return constants.ErrInvalidTokenOrAmount
	}

	block.Data, err = definition.ABICommon.PackMethod(p.MethodName)
	return err
}
func (p *CollectRewardMethod) ReceiveBlock(context vm_context.AccountVmContext, sendBlock *nom.AccountBlock) ([]*nom.AccountBlock, error) {
	if err := p.ValidateSendBlock(sendBlock); err != nil {
		return nil, err
	}

	deposit, err := definition.GetRewardDeposit(context.Storage(), &sendBlock.Address)
	common.DealWithErr(err)

	if deposit.Znn.Sign() == 0 && deposit.Qsr.Sign() == 0 {
		return nil, constants.ErrNothingToWithdraw
	}

	result := make([]*nom.AccountBlock, 0, 2)
	if deposit.Znn.Sign() == +1 {
		result = append(result, &nom.AccountBlock{
			Address:       sendBlock.ToAddress,
			ToAddress:     types.TokenContract,
			BlockType:     nom.BlockTypeContractSend,
			Amount:        big.NewInt(0),
			TokenStandard: types.ZnnTokenStandard,
			Data: definition.ABIToken.PackMethodPanic(
				definition.MintMethodName,
				types.ZnnTokenStandard,
				deposit.Znn,
				sendBlock.Address,
			),
		})
	}
	if deposit.Qsr.Sign() == +1 {
		result = append(result, &nom.AccountBlock{
			Address:       sendBlock.ToAddress,
			ToAddress:     types.TokenContract,
			BlockType:     nom.BlockTypeContractSend,
			Amount:        big.NewInt(0),
			TokenStandard: types.ZnnTokenStandard,
			Data: definition.ABIToken.PackMethodPanic(
				definition.MintMethodName,
				types.QsrTokenStandard,
				deposit.Qsr,
				sendBlock.Address,
			),
		})
	}

	common.DealWithErr(deposit.Delete(context.Storage()))

	return result, nil
}

// Used for registration
//   - checks if user has deposited enough QSR
//   - consumes the required amount
func checkAndConsumeQsr(context vm_context.AccountVmContext, ownerAddress types.Address, requiredAmount *big.Int) error {
	// check that sender has enough Qsr deposited for this operation
	qsrDeposit, err := definition.GetQsrDeposit(context.Storage(), &ownerAddress)
	common.DealWithErr(err)

	if qsrDeposit.Qsr.Cmp(requiredAmount) == -1 {
		return constants.ErrNotEnoughDepositedQsr
	}
	qsrDeposit.Qsr.Sub(qsrDeposit.Qsr, requiredAmount)

	if qsrDeposit.Qsr.Cmp(common.Big0) == 0 {
		common.DealWithErr(qsrDeposit.Delete(context.Storage()))
	} else {
		common.DealWithErr(qsrDeposit.Save(context.Storage()))
	}

	return nil
}

type DepositQsrMethod struct {
	MethodName string
}

func (p *DepositQsrMethod) GetPlasma(plasmaTable *constants.PlasmaTable) (uint64, error) {
	return plasmaTable.EmbeddedSimple, nil
}
func (p *DepositQsrMethod) ValidateSendBlock(block *nom.AccountBlock) error {
	var err error

	if err := definition.ABICommon.UnpackEmptyMethod(p.MethodName, block.Data); err != nil {
		return constants.ErrUnpackError
	}

	if block.TokenStandard != types.QsrTokenStandard || block.Amount.Sign() != 1 {
		return constants.ErrInvalidTokenOrAmount
	}

	block.Data, err = definition.ABICommon.PackMethod(p.MethodName)
	return err
}
func (p *DepositQsrMethod) ReceiveBlock(context vm_context.AccountVmContext, sendBlock *nom.AccountBlock) ([]*nom.AccountBlock, error) {
	if err := p.ValidateSendBlock(sendBlock); err != nil {
		return nil, err
	}

	qsrDeposit, err := definition.GetQsrDeposit(context.Storage(), &sendBlock.Address)
	common.DealWithErr(err)

	qsrDeposit.Qsr.Add(qsrDeposit.Qsr, sendBlock.Amount)
	common.DealWithErr(qsrDeposit.Save(context.Storage()))
	return nil, nil
}

type WithdrawQsrMethod struct {
	MethodName string
}

func (p *WithdrawQsrMethod) GetPlasma(plasmaTable *constants.PlasmaTable) (uint64, error) {
	return plasmaTable.EmbeddedWWithdraw, nil
}
func (p *WithdrawQsrMethod) ValidateSendBlock(block *nom.AccountBlock) error {
	var err error

	if err := definition.ABICommon.UnpackEmptyMethod(p.MethodName, block.Data); err != nil {
		return constants.ErrUnpackError
	}

	if block.Amount.Sign() != 0 {
		return constants.ErrInvalidTokenOrAmount
	}

	block.Data, err = definition.ABICommon.PackMethod(p.MethodName)
	return err
}
func (p *WithdrawQsrMethod) ReceiveBlock(context vm_context.AccountVmContext, sendBlock *nom.AccountBlock) ([]*nom.AccountBlock, error) {
	if err := p.ValidateSendBlock(sendBlock); err != nil {
		return nil, err
	}

	qsrDeposit, err := definition.GetQsrDeposit(context.Storage(), &sendBlock.Address)
	common.DealWithErr(err)

	// check for 0 deposited QSR
	if qsrDeposit.Qsr.Sign() == 0 {
		return nil, constants.ErrNothingToWithdraw
	}
	common.DealWithErr(qsrDeposit.Delete(context.Storage()))

	return []*nom.AccountBlock{
		{
			Address:       sendBlock.ToAddress,
			ToAddress:     *qsrDeposit.Address,
			BlockType:     nom.BlockTypeContractSend,
			Amount:        qsrDeposit.Qsr,
			TokenStandard: types.QsrTokenStandard,
			Data:          []byte{},
		},
	}, nil
}

type DonateMethod struct {
	MethodName string
}

func (p *DonateMethod) GetPlasma(plasmaTable *constants.PlasmaTable) (uint64, error) {
	return plasmaTable.EmbeddedSimple, nil
}
func (p *DonateMethod) ValidateSendBlock(block *nom.AccountBlock) error {
	var err error

	if err := definition.ABICommon.UnpackEmptyMethod(p.MethodName, block.Data); err != nil {
		return constants.ErrUnpackError
	}

	if block.Amount.Sign() == 0 {
		return constants.ErrInvalidTokenOrAmount
	}

	block.Data, err = definition.ABICommon.PackMethod(p.MethodName)
	return err
}
func (p *DonateMethod) ReceiveBlock(context vm_context.AccountVmContext, sendBlock *nom.AccountBlock) ([]*nom.AccountBlock, error) {
	if err := p.ValidateSendBlock(sendBlock); err != nil {
		return nil, err
	}
	commonLog.Info("received donation", "embedded", sendBlock.ToAddress, "from-address", sendBlock.Address, "zts", sendBlock.TokenStandard, "amount", sendBlock.Amount)
	return nil, nil
}

type VoteByNameMethod struct {
	MethodName string
}

func (p *VoteByNameMethod) Fee() (*big.Int, error) {
	return big.NewInt(0), nil
}
func (p *VoteByNameMethod) GetPlasma(plasmaTable *constants.PlasmaTable) (uint64, error) {
	return plasmaTable.EmbeddedSimple, nil
}
func (p *VoteByNameMethod) ValidateSendBlock(block *nom.AccountBlock) error {
	var err error

	param := new(definition.PillarVote)
	if err := definition.ABICommon.UnpackMethod(param, p.MethodName, block.Data); err != nil {
		return constants.ErrUnpackError
	}

	if param.Vote >= definition.VoteNotValid {
		return constants.ErrForbiddenParam
	}

	if block.Amount.Sign() != 0 {
		return constants.ErrInvalidTokenOrAmount
	}

	block.Data, err = definition.ABICommon.PackMethod(p.MethodName, param.Id, param.Name, param.Vote)
	return err
}
func (p *VoteByNameMethod) ReceiveBlock(context vm_context.AccountVmContext, sendBlock *nom.AccountBlock) ([]*nom.AccountBlock, error) {
	if err := p.ValidateSendBlock(sendBlock); err != nil {
		return nil, err
	}

	param := new(definition.PillarVote)
	if err := definition.ABICommon.UnpackMethod(param, p.MethodName, sendBlock.Data); err != nil {
		return nil, constants.ErrUnpackError
	}

	if _, err := definition.GetVotableHash(context.Storage(), param.Id); err == constants.ErrDataNonExistent {
		return nil, err
	} else {
		common.DealWithErr(err)
	}

	pillarList, err := context.MomentumStore().GetActivePillars()
	common.DealWithErr(err)

	ok := false
	for _, pillar := range pillarList {
		if pillar.Name == param.Name && pillar.StakeAddress == sendBlock.Address {
			ok = true
			break
		}
	}
	if !ok {
		commonLog.Debug("unable to find pillar", "param", param, "send-block-address", sendBlock.Address)
		return nil, constants.ErrForbiddenParam
	}

	param.Save(context.Storage())

	commonLog.Debug("voted for hash", "pillar-vote", param)
	return nil, nil
}

type VoteByProdAddressMethod struct {
	MethodName string
}

func (p *VoteByProdAddressMethod) Fee() (*big.Int, error) {
	return big.NewInt(0), nil
}
func (p *VoteByProdAddressMethod) GetPlasma(plasmaTable *constants.PlasmaTable) (uint64, error) {
	return plasmaTable.EmbeddedSimple, nil
}
func (p *VoteByProdAddressMethod) ValidateSendBlock(block *nom.AccountBlock) error {
	var err error

	param := new(definition.PillarVote)
	if err := definition.ABICommon.UnpackMethod(param, p.MethodName, block.Data); err != nil {
		return constants.ErrUnpackError
	}

	if param.Vote >= definition.VoteNotValid {
		return constants.ErrForbiddenParam
	}

	if block.Amount.Sign() != 0 {
		return constants.ErrInvalidTokenOrAmount
	}

	block.Data, err = definition.ABICommon.PackMethod(p.MethodName, param.Id, param.Vote)
	return err
}
func (p *VoteByProdAddressMethod) ReceiveBlock(context vm_context.AccountVmContext, sendBlock *nom.AccountBlock) ([]*nom.AccountBlock, error) {
	if err := p.ValidateSendBlock(sendBlock); err != nil {
		return nil, err
	}

	param := new(definition.PillarVote)
	if err := definition.ABICommon.UnpackMethod(param, p.MethodName, sendBlock.Data); err != nil {
		return nil, constants.ErrUnpackError
	}

	if _, err := definition.GetVotableHash(context.Storage(), param.Id); err == constants.ErrDataNonExistent {
		return nil, err
	} else {
		common.DealWithErr(err)
	}

	pillarList, err := context.MomentumStore().GetActivePillars()
	common.DealWithErr(err)

	ok := false
	for _, pillar := range pillarList {
		if pillar.BlockProducingAddress == sendBlock.Address {
			param.Name = pillar.Name
			ok = true
			break
		}
	}
	if !ok {
		commonLog.Debug("unable to find pillar", "param", param, "send-block-address", sendBlock.Address)
		return nil, constants.ErrForbiddenParam
	}

	param.Save(context.Storage())

	commonLog.Debug("voted for hash", "pillar-vote", param)
	return nil, nil
}

func TimeChallenge(context vm_context.AccountVmContext, methodName string, hash []byte, delay uint64) (*definition.TimeChallengeInfo, error) {
	timeChallengeInfo, err := definition.GetTimeChallengeInfoVariable(context.Storage(), methodName)
	if err != nil {
		return nil, err
	}
	if timeChallengeInfo == nil {
		timeChallengeInfo = &definition.TimeChallengeInfo{
			MethodName:           methodName,
			ParamsHash:           types.Hash{},
			ChallengeStartHeight: 0,
		}
	}
	paramsHash, err := types.BytesToHash(hash)
	if err != nil {
		return nil, err
	}

	momentum, err := context.GetFrontierMomentum()
	common.DealWithErr(err)
	// if true then we need to check the time challenge, otherwise we start a new challenge
	if reflect.DeepEqual(timeChallengeInfo.ParamsHash, paramsHash) {
		if timeChallengeInfo.ChallengeStartHeight+delay >= momentum.Height {
			return nil, constants.ErrTimeChallengeNotDue
		} else {
			// challenge is ok, we can reset it
			timeChallengeInfo.ParamsHash = types.Hash{}
		}
	} else {
		if errSet := timeChallengeInfo.ParamsHash.SetBytes(paramsHash.Bytes()); errSet != nil {
			return nil, errSet
		}
		timeChallengeInfo.ChallengeStartHeight = momentum.Height
	}
	common.DealWithErr(timeChallengeInfo.Save(context.Storage()))
	return timeChallengeInfo, nil
}

func CheckSecurityInitialized(context vm_context.AccountVmContext) (*definition.SecurityInfoVariable, error) {
	securityInfo, err := definition.GetSecurityInfoVariable(context.Storage())
	if err != nil {
		return nil, err
	}
	if len(securityInfo.Guardians) < constants.MinGuardians {
		return nil, constants.ErrSecurityNotInitialized
	}

	return securityInfo, nil
}
