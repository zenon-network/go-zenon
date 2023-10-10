package implementation

import (
	"encoding/base64"
	"math/big"
	"regexp"
	"sort"

	"github.com/pkg/errors"

	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/consensus/api"
	"github.com/zenon-network/go-zenon/vm/constants"
	"github.com/zenon-network/go-zenon/vm/embedded/definition"
	"github.com/zenon-network/go-zenon/vm/vm_context"
)

var (
	pillarLog = common.EmbeddedLogger.New("contract", "pillar")
)

// Performs basic static checks to determine if a pillar name is valid
func checkPillarNameStatic(name string) error {
	if len(name) == 0 ||
		len(name) > constants.PillarNameLengthMax {
		return constants.ErrInvalidName
	}
	if ok, _ := regexp.MatchString("^([a-zA-Z0-9]+[-._]?)*[a-zA-Z0-9]$", name); !ok {
		return constants.ErrInvalidName
	}
	return nil
}

// returns true if producing address is not used or belonged to this pillar in the past
func checkAvailableProducingAddress(context vm_context.AccountVmContext, producing types.Address, name string) error {
	// return true if addr is unused
	prodName, err := definition.GetProducingPillarName(context.Storage(), producing)
	if err == constants.ErrDataNonExistent {
		return nil
	} else if err != nil {
		common.DealWithErr(err)
	}

	// return true if past address
	if prodName.Name == name {
		return nil
	}
	return constants.ErrNotUnique
}

func checkPillarPercentages(param *definition.RegisterParam) error {
	if param.GiveBlockRewardPercentage > 100 || param.GiveBlockRewardPercentage < 0 {
		return constants.ErrForbiddenParam
	}
	if param.GiveDelegateRewardPercentage > 100 || param.GiveDelegateRewardPercentage < 0 {
		return constants.ErrForbiddenParam
	}
	return nil
}

// Used for registration
// - checks the validity of pillar information
// - registers pillar and producing address in DB
func checkAndRegisterPillar(context vm_context.AccountVmContext, param *definition.RegisterParam, ownerAddress types.Address, pillarType uint8) error {
	// check pillar param
	if err := checkPillarNameStatic(param.Name); err != nil {
		return err
	}
	if err := checkPillarPercentages(param); err != nil {
		return err
	}

	// check if pillar name is used
	_, err := definition.GetPillarInfo(context.Storage(), param.Name)
	if err == constants.ErrDataNonExistent {
		// ok, does not exist
	} else if err == nil {
		return constants.ErrNotUnique
	} else {
		common.DealWithErr(err)
	}

	if err = checkAvailableProducingAddress(context, param.ProducerAddress, param.Name); err != nil {
		return err
	}

	momentum, err := context.GetFrontierMomentum()
	common.DealWithErr(err)

	pillar := new(definition.PillarInfo)
	pillar.Name = param.Name
	pillar.BlockProducingAddress = param.ProducerAddress
	pillar.RewardWithdrawAddress = param.RewardAddress
	pillar.StakeAddress = ownerAddress
	pillar.Amount = constants.PillarStakeAmount
	pillar.RegistrationTime = momentum.Timestamp.Unix()
	pillar.GiveBlockRewardPercentage = param.GiveBlockRewardPercentage
	pillar.GiveDelegateRewardPercentage = param.GiveDelegateRewardPercentage
	pillar.PillarType = pillarType
	common.DealWithErr(pillar.Save(context.Storage()))

	producing := new(definition.ProducingPillar)
	producing.Name = param.Name
	producing.Producing = &param.ProducerAddress
	common.DealWithErr(producing.Save(context.Storage()))
	return nil
}

// GetQsrCostForNextPillar returns PillarQsrStakeBaseAmount * PillarQsrStakeIncreaseAmount * len(definition.NormalPillarType)
func GetQsrCostForNextPillar(context vm_context.AccountVmContext) (*big.Int, error) {
	pillarsList, err := definition.GetPillarsList(context.Storage(), true, definition.NormalPillarType)
	if err != nil {
		return nil, err
	}
	numPillars := len(pillarsList)

	currentCost := new(big.Int)
	currentCost.Set(constants.PillarQsrStakeIncreaseAmount)
	currentCost.Mul(currentCost, big.NewInt(int64(numPillars)))
	currentCost.Add(currentCost, constants.PillarQsrStakeBaseAmount)
	return currentCost, nil
}

// PillarGetRevokeStatus returns status and cooldown.
// If Pillar *can* be revoked, returns
// - true, timeWhileCanRevoke
// If Pillar *can't* be revoked, returns
// - false, timeUntilCanRevoke
func PillarGetRevokeStatus(old *definition.PillarInfo, m *nom.Momentum) (bool, int64) {
	epochTime := (m.Timestamp.Unix() - old.RegistrationTime) % (constants.PillarEpochLockTime + constants.PillarEpochRevokeTime)
	if epochTime < constants.PillarEpochLockTime {
		return false, constants.PillarEpochLockTime - epochTime
	} else {
		return true, (constants.PillarEpochLockTime + constants.PillarEpochRevokeTime) - epochTime
	}
}

type RegisterMethod struct {
	MethodName string
}

func (p *RegisterMethod) GetPlasma(plasmaTable *constants.PlasmaTable) (uint64, error) {
	// include burn transaction
	return 2 * plasmaTable.EmbeddedSimple, nil
}
func (p *RegisterMethod) ValidateSendBlock(block *nom.AccountBlock) error {
	var err error
	param := new(definition.RegisterParam)

	if err := definition.ABIPillars.UnpackMethod(param, p.MethodName, block.Data); err != nil {
		return constants.ErrUnpackError
	}

	if err := checkPillarNameStatic(param.Name); err != nil {
		return err
	}
	if err := checkPillarPercentages(param); err != nil {
		return err
	}
	// check amount of znn in block required for registration
	// qsr amount is deposited in the embedded and it cannot be checked static
	if block.TokenStandard != types.ZnnTokenStandard || block.Amount.Cmp(constants.PillarStakeAmount) != 0 {
		return constants.ErrInvalidTokenOrAmount
	}

	block.Data, err = definition.ABIPillars.PackMethod(p.MethodName, param.Name, param.ProducerAddress, param.RewardAddress, param.GiveBlockRewardPercentage, param.GiveDelegateRewardPercentage)
	return err
}
func (p *RegisterMethod) ReceiveBlock(context vm_context.AccountVmContext, sendBlock *nom.AccountBlock) ([]*nom.AccountBlock, error) {
	if err := p.ValidateSendBlock(sendBlock); err != nil {
		return nil, err
	}

	param := new(definition.RegisterParam)
	err := definition.ABIPillars.UnpackMethod(param, p.MethodName, sendBlock.Data)
	common.DealWithErr(err)

	requiredPillarQsrAmount, err := GetQsrCostForNextPillar(context)
	common.DealWithErr(err)

	if err := checkAndRegisterPillar(context, param, sendBlock.Address, definition.NormalPillarType); err != nil {
		return nil, err
	}
	if err := checkAndConsumeQsr(context, sendBlock.Address, requiredPillarQsrAmount); err != nil {
		return nil, err
	}

	return []*nom.AccountBlock{
		{
			Address:       types.PillarContract,
			ToAddress:     types.TokenContract,
			BlockType:     nom.BlockTypeContractSend,
			Amount:        requiredPillarQsrAmount,
			TokenStandard: types.QsrTokenStandard,
			Data:          definition.ABIToken.PackMethodPanic(definition.BurnMethodName),
		},
	}, nil
}

type LegacyRegisterMethod struct {
	MethodName string
}

func (p *LegacyRegisterMethod) GetPlasma(plasmaTable *constants.PlasmaTable) (uint64, error) {
	// include burn transaction
	return 2 * plasmaTable.EmbeddedSimple, nil
}
func (p *LegacyRegisterMethod) ValidateSendBlock(block *nom.AccountBlock) error {
	var err error
	param := new(definition.LegacyRegisterParam)

	if err := definition.ABIPillars.UnpackMethod(param, p.MethodName, block.Data); err != nil {
		return constants.ErrUnpackError
	}

	if err := checkPillarNameStatic(param.Name); err != nil {
		return err
	}
	if err := checkPillarPercentages(&param.RegisterParam); err != nil {
		return err
	}
	// check signature - no errors means it's valid
	if _, err := CheckSwapSignature(SwapRetrieveLegacyPillar, block.Address, param.PublicKey, param.Signature); err != nil {
		return err
	}
	// check amount of znn in block required for registration
	// qsr amount is deposited in the embedded and it cannot be checked static
	if block.TokenStandard != types.ZnnTokenStandard || block.Amount.Cmp(constants.PillarStakeAmount) != 0 {
		return constants.ErrInvalidTokenOrAmount
	}

	block.Data, err = definition.ABIPillars.PackMethod(p.MethodName, param.Name, param.ProducerAddress, param.RewardAddress, param.GiveBlockRewardPercentage, param.GiveDelegateRewardPercentage, param.PublicKey, param.Signature)
	return err
}
func (p *LegacyRegisterMethod) ReceiveBlock(context vm_context.AccountVmContext, sendBlock *nom.AccountBlock) ([]*nom.AccountBlock, error) {
	if err := p.ValidateSendBlock(sendBlock); err != nil {
		return nil, err
	}

	param := new(definition.LegacyRegisterParam)
	err := definition.ABIPillars.UnpackMethod(param, p.MethodName, sendBlock.Data)
	common.DealWithErr(err)

	// check legacy entry exists
	publicKey, err := base64.StdEncoding.DecodeString(param.PublicKey)
	if err != nil {
		return nil, constants.ErrInvalidB64Decode
	}

	legacyEntry, err := definition.GetLegacyPillarEntry(context.Storage(), PubKeyToKeyIdHash(publicKey))
	if err == constants.ErrDataNonExistent {
		return nil, constants.ErrNotEnoughSlots
	} else {
		common.DealWithErr(err)
	}

	legacyEntry.PillarCount -= 1
	if legacyEntry.PillarCount == 0 {
		common.DealWithErr(legacyEntry.Delete(context.Storage()))
	} else {
		common.DealWithErr(legacyEntry.Save(context.Storage()))
	}

	requiredPillarQsrAmount := constants.PillarQsrStakeBaseAmount

	if err := checkAndRegisterPillar(context, &param.RegisterParam, sendBlock.Address, definition.LegacyPillarType); err != nil {
		return nil, err
	}
	if err := checkAndConsumeQsr(context, sendBlock.Address, requiredPillarQsrAmount); err != nil {
		return nil, err
	}

	return []*nom.AccountBlock{
		{
			Address:       types.PillarContract,
			ToAddress:     types.TokenContract,
			BlockType:     nom.BlockTypeContractSend,
			Amount:        requiredPillarQsrAmount,
			TokenStandard: types.QsrTokenStandard,
			Data:          definition.ABIToken.PackMethodPanic(definition.BurnMethodName),
		},
	}, nil
}

type RevokeMethod struct {
	MethodName string
}

func (p *RevokeMethod) GetPlasma(plasmaTable *constants.PlasmaTable) (uint64, error) {
	return plasmaTable.EmbeddedWWithdraw, nil
}
func (p *RevokeMethod) ValidateSendBlock(block *nom.AccountBlock) error {
	var err error
	param := new(string)

	if err := definition.ABIPillars.UnpackMethod(param, p.MethodName, block.Data); err != nil {
		return constants.ErrUnpackError
	}

	if err := checkPillarNameStatic(*param); err != nil {
		return err
	}
	if block.Amount.Sign() != 0 {
		return constants.ErrInvalidTokenOrAmount
	}

	block.Data, err = definition.ABIPillars.PackMethod(p.MethodName, param)
	return err
}
func (p *RevokeMethod) ReceiveBlock(context vm_context.AccountVmContext, sendBlock *nom.AccountBlock) ([]*nom.AccountBlock, error) {
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

	momentum, err := context.GetFrontierMomentum()
	common.DealWithErr(err)
	if status, _ := PillarGetRevokeStatus(pillar, momentum); !status {
		return nil, constants.RevokeNotDue
	}

	pillar.RevokeTime = momentum.Timestamp.Unix()
	pillar.Amount = big.NewInt(0)
	common.DealWithErr(pillar.Save(context.Storage()))

	return []*nom.AccountBlock{
		{
			Address:       types.PillarContract,
			ToAddress:     pillar.StakeAddress,
			BlockType:     nom.BlockTypeContractSend,
			Amount:        constants.PillarStakeAmount,
			TokenStandard: types.ZnnTokenStandard,
			Data:          []byte{},
		},
	}, nil
}

// Reward defines momentum reward details
type pillarEpochReward struct {
	DelegationReward *big.Int
	BlockReward      *big.Int
	TotalReward      *big.Int
	ProducedBlockNum int32
	ExpectedBlockNum int32
	Weight           *big.Int
}

// distributed reward for all pillars in one epoch
func computeDetailedPillarReward(context vm_context.AccountVmContext, epoch uint64) error {
	pillarReward, err := computePillarsRewardForEpoch(context, epoch)
	if err != nil {
		return err
	}

	distributed := make(map[types.Address]*big.Int)
	toGive := make(map[string]*big.Int)
	pillarInfos, err := definition.GetPillarsList(context.Storage(), false, definition.AnyPillarType)

	// set pillar percentages in DB for historic reasons
	for _, pillar := range pillarInfos {
		reward, ok := pillarReward[pillar.Name]
		// pillar registered in later epochs
		if !ok {
			continue
		}

		err = (&definition.PillarEpochHistory{
			Epoch:                        epoch,
			Name:                         pillar.Name,
			GiveBlockRewardPercentage:    pillar.GiveBlockRewardPercentage,
			GiveDelegateRewardPercentage: pillar.GiveDelegateRewardPercentage,
			ProducedBlockNum:             reward.ProducedBlockNum,
			ExpectedBlockNum:             reward.ExpectedBlockNum,
			Weight:                       reward.Weight,
		}).Save(context.Storage())
		if err != nil {
			return err
		}
	}

	for _, pillar := range pillarInfos {
		reward, ok := pillarReward[pillar.Name]
		// pillar registered in later epochs
		if !ok {
			continue
		}

		toGiveN := big.NewInt(0)
		// toGive = (pillar.GiveBlockRewardPercentage * reward.BlockReward + pillar.GiveDelegateRewardPercentage * reward.DelegationReward) / 100
		tmp := big.NewInt(int64(pillar.GiveBlockRewardPercentage))
		tmp.Mul(tmp, reward.BlockReward)
		toGiveN.Add(toGiveN, tmp)

		tmp.SetInt64(int64(pillar.GiveDelegateRewardPercentage))
		tmp.Mul(tmp, reward.DelegationReward)
		toGiveN.Add(toGiveN, tmp)

		toGiveN.Quo(toGiveN, common.Big100)
		toGive[pillar.Name] = toGiveN

		// rewards to pillar, total - toGive
		addReward(context, epoch, definition.RewardDeposit{
			Address: &pillar.RewardWithdrawAddress,
			Znn:     new(big.Int).Sub(reward.TotalReward, toGiveN),
			Qsr:     common.Big0,
		})
	}

	if len(toGive) != len(pillarReward) {
		return errors.Errorf("some pillar rewards were not distributed. toGive %v all %v registered pillars %v", len(toGive), len(pillarReward), len(pillarInfos))
	}

	// add rewards to backers
	details, err := context.GetPillarDelegationsByEpoch(epoch)
	common.DealWithErr(err)
	for _, pillarDetail := range details {
		toBackers, ok := toGive[pillarDetail.Name]
		if !ok {
			return errors.Errorf("can't find amount to backers for pillar %v", pillarDetail.Name)
		}

		backersAmount := big.NewInt(0)
		for _, amount := range pillarDetail.Backers {
			backersAmount.Add(backersAmount, amount)
		}

		// no weight, all rewards go to pillar reward address
		if backersAmount.Cmp(common.Big0) == 0 {
			for _, pillar := range pillarInfos {
				if pillar.Name == pillarDetail.Name {
					addReward(context, epoch, definition.RewardDeposit{
						Address: &pillar.RewardWithdrawAddress,
						Znn:     toBackers,
						Qsr:     common.Big0,
					})
					break
				}
			}
			continue
		}

		// distribute evenly to backers
		for address, amount := range pillarDetail.Backers {
			toBacker := new(big.Int).Set(toBackers)
			toBacker.Mul(toBacker, amount)
			toBacker.Quo(toBacker, backersAmount)
			addReward(context, epoch, definition.RewardDeposit{
				Address: &address,
				Znn:     toBacker,
				Qsr:     common.Big0,
			})
		}
	}

	// for debug, sort keys and print distributed map
	distributedAddresses := make([]string, 0, len(distributed))
	for address := range distributed {
		distributedAddresses = append(distributedAddresses, address.String())
	}
	sort.Strings(distributedAddresses)
	for _, address := range distributedAddresses {
		raw, _ := types.ParseAddress(address)
		amount, _ := distributed[raw]
		pillarLog.Debug("distribute pillar rewards", "epoch", epoch, "address", address, "amount", amount)
	}

	return nil
}

// raw reward for all pillars in one epoch
func computePillarsRewardForEpoch(context vm_context.AccountVmContext, epoch uint64) (m map[string]*pillarEpochReward, err error) {
	detailList, err := context.EpochStats(epoch)
	if err != nil {
		return nil, err
	}

	rewardMap := make(map[string]*pillarEpochReward, len(detailList.Pillars))

	// sort pillar names for debug purposes only, so that the output is deterministic
	pillarNames := make([]string, 0, len(detailList.Pillars))
	for name := range detailList.Pillars {
		pillarNames = append(pillarNames, name)
	}
	sort.Strings(pillarNames)
	for _, name := range pillarNames {
		rewardMap[name] = computePillarRewardForEpoch(detailList, name)
	}
	return rewardMap, nil
}

// raw reward for one pillar in one epoch
func computePillarRewardForEpoch(detail *api.EpochStats, name string) *pillarEpochReward {
	selfDetail, ok := detail.Pillars[name]
	reward := &pillarEpochReward{
		DelegationReward: big.NewInt(0),
		BlockReward:      big.NewInt(0),
		TotalReward:      big.NewInt(0),
		ProducedBlockNum: 0,
		ExpectedBlockNum: 0,
		Weight:           new(big.Int).Set(detail.Pillars[name].Weight),
	}
	if !ok || selfDetail.ExceptedBlockNum == 0 {
		return reward
	}

	var totalExpectedBlockNum uint64 = 0
	for _, detail := range detail.Pillars {
		totalExpectedBlockNum += detail.ExceptedBlockNum
	}

	// reward = DelegationRewardsPerBlock * totalExpectedBlocks * (selfProducedBlockNum / expectedBlockNum) * (weight / totalWeight)
	//	      + BlockProducingRewardsPerBlock * selfProducesBlocksNum

	tmp := new(big.Int)
	delegationRewardsPerBlock, blockProducingRewardsPerBlock := constants.PillarRewardPerMomentum(detail.Epoch)

	if detail.TotalWeight.Sign() != 0 {
		reward.DelegationReward.Set(delegationRewardsPerBlock)
		tmp.SetUint64(selfDetail.BlockNum)
		reward.DelegationReward.Mul(reward.DelegationReward, tmp)
		reward.DelegationReward.Mul(reward.DelegationReward, selfDetail.Weight)
		tmp.SetUint64(totalExpectedBlockNum)
		reward.DelegationReward.Mul(reward.DelegationReward, tmp)
		tmp.SetUint64(selfDetail.ExceptedBlockNum)
		reward.DelegationReward.Quo(reward.DelegationReward, tmp)
		reward.DelegationReward.Quo(reward.DelegationReward, detail.TotalWeight)
	}

	reward.BlockReward.Set(blockProducingRewardsPerBlock)
	tmp.SetUint64(selfDetail.BlockNum)
	reward.BlockReward.Mul(reward.BlockReward, tmp)

	reward.TotalReward.Add(reward.BlockReward, reward.DelegationReward)
	reward.ProducedBlockNum = int32(selfDetail.BlockNum)
	reward.ExpectedBlockNum = int32(selfDetail.ExceptedBlockNum)

	pillarLog.Debug("computer pillar-reward", "epoch", detail.Epoch, "pillar-name", name, "reward", reward, "total-weight", detail.TotalWeight, "self-weight", selfDetail.Weight)
	return reward
}

func updatePillarRewards(context vm_context.AccountVmContext) error {
	lastEpoch, err := definition.GetLastEpochUpdate(context.Storage())
	if err != nil {
		return err
	}
	for {
		if err := checkAndPerformUpdateEpoch(context, lastEpoch); err == constants.ErrEpochUpdateTooRecent {
			pillarLog.Debug("invalid update - rewards not due yet", "epoch", lastEpoch.LastEpoch+1)
			return nil
		} else if err != nil {
			pillarLog.Error("unknown panic", "reason", err)
			return err
		}
		if err := computeDetailedPillarReward(context, uint64(lastEpoch.LastEpoch)); err != nil {
			return err
		}
	}
}

type UpdatePillarMethod struct {
	MethodName string
}

func (p *UpdatePillarMethod) GetPlasma(plasmaTable *constants.PlasmaTable) (uint64, error) {
	return plasmaTable.EmbeddedSimple, nil
}
func (p *UpdatePillarMethod) ValidateSendBlock(block *nom.AccountBlock) error {
	var err error
	param := new(definition.RegisterParam)

	if err := definition.ABIPillars.UnpackMethod(param, p.MethodName, block.Data); err != nil {
		return constants.ErrUnpackError
	}

	if err := checkPillarNameStatic(param.Name); err != nil {
		return err
	}
	if err := checkPillarPercentages(param); err != nil {
		return err
	}
	if block.Amount.Sign() != 0 {
		return constants.ErrInvalidTokenOrAmount
	}

	block.Data, err = definition.ABIPillars.PackMethod(p.MethodName, param.Name, param.ProducerAddress, param.RewardAddress, param.GiveBlockRewardPercentage, param.GiveDelegateRewardPercentage)
	return err
}
func (p *UpdatePillarMethod) ReceiveBlock(context vm_context.AccountVmContext, sendBlock *nom.AccountBlock) ([]*nom.AccountBlock, error) {
	if err := p.ValidateSendBlock(sendBlock); err != nil {
		return nil, err
	}

	param := new(definition.RegisterParam)
	err := definition.ABIPillars.UnpackMethod(param, p.MethodName, sendBlock.Data)
	common.DealWithErr(err)

	pillar, err := definition.GetPillarInfo(context.Storage(), param.Name)
	if err == constants.ErrDataNonExistent {
		return nil, err
	} else {
		common.DealWithErr(err)
	}

	if pillar.StakeAddress != sendBlock.Address {
		return nil, constants.ErrPermissionDenied
	}

	if !pillar.IsActive() {
		return nil, constants.ErrNotActive
	}

	if param.ProducerAddress != pillar.BlockProducingAddress {
		pillarLog.Info("Updating pillar producer address", "pillar-name", param.Name, "old-address", pillar.BlockProducingAddress, "new-address", param.ProducerAddress)
		if err := checkAvailableProducingAddress(context, param.ProducerAddress, param.Name); err != nil {
			return nil, err
		}

		pillar.BlockProducingAddress = param.ProducerAddress

		producing := new(definition.ProducingPillar)
		producing.Name = param.Name
		producing.Producing = &param.ProducerAddress
		common.DealWithErr(producing.Save(context.Storage()))
	}

	if param.RewardAddress != pillar.RewardWithdrawAddress {
		pillarLog.Info("Updating pillar reward address", "pillar-name", param.Name, "old-address", pillar.RewardWithdrawAddress, "new-address", param.RewardAddress)
		pillar.RewardWithdrawAddress = param.RewardAddress
	}

	if param.GiveBlockRewardPercentage != pillar.GiveBlockRewardPercentage {
		pillarLog.Info("Updating pillar give-block-reward-percentage", "pillar-name", param.Name, "old", pillar.GiveBlockRewardPercentage, "new", param.GiveBlockRewardPercentage)
		pillar.GiveBlockRewardPercentage = param.GiveBlockRewardPercentage
	}

	if param.GiveDelegateRewardPercentage != pillar.GiveDelegateRewardPercentage {
		pillarLog.Info("Updating pillar give-delegate-reward-percentage", "pillar-name", param.Name, "old", pillar.GiveDelegateRewardPercentage, "new", param.GiveDelegateRewardPercentage)
		pillar.GiveDelegateRewardPercentage = param.GiveDelegateRewardPercentage
	}

	common.DealWithErr(pillar.Save(context.Storage()))
	return nil, nil
}

type DelegateMethod struct {
	MethodName string
}

func (p *DelegateMethod) GetPlasma(plasmaTable *constants.PlasmaTable) (uint64, error) {
	return plasmaTable.EmbeddedSimple, nil
}
func (p *DelegateMethod) ValidateSendBlock(block *nom.AccountBlock) error {
	var err error
	param := new(string)

	if err := definition.ABIPillars.UnpackMethod(param, p.MethodName, block.Data); err != nil {
		return constants.ErrUnpackError
	}

	if err := checkPillarNameStatic(*param); err != nil {
		return err
	}
	if block.Amount.Sign() != 0 {
		return constants.ErrInvalidTokenOrAmount
	}

	block.Data, err = definition.ABIPillars.PackMethod(p.MethodName, param)
	return err
}
func (p *DelegateMethod) ReceiveBlock(context vm_context.AccountVmContext, sendBlock *nom.AccountBlock) ([]*nom.AccountBlock, error) {
	if err := p.ValidateSendBlock(sendBlock); err != nil {
		return nil, err
	}

	name := new(string)
	err := definition.ABIPillars.UnpackMethod(name, p.MethodName, sendBlock.Data)
	common.DealWithErr(err)

	// check pillar exists
	pillar, err := definition.GetPillarInfo(context.Storage(), *name)
	if err == constants.ErrDataNonExistent {
		return nil, err
	} else {
		common.DealWithErr(err)
	}

	// check pillar is active
	if !pillar.IsActive() {
		return nil, constants.ErrNotActive
	}

	common.DealWithErr((&definition.DelegationInfo{
		Backer: sendBlock.Address,
		Name:   *name,
	}).Save(context.Storage()))
	momentum, err := context.GetFrontierMomentum()
	common.DealWithErr(err)
	pillarLog.Info("delegating to pillar", "address", sendBlock.Address.String(), "pillar-name", *name, "height", momentum.Height)
	return nil, nil
}

type UndelegateMethod struct {
	MethodName string
}

func (p *UndelegateMethod) GetPlasma(plasmaTable *constants.PlasmaTable) (uint64, error) {
	return plasmaTable.EmbeddedSimple, nil
}
func (p *UndelegateMethod) ValidateSendBlock(block *nom.AccountBlock) error {
	var err error

	if err := definition.ABIPillars.UnpackEmptyMethod(p.MethodName, block.Data); err != nil {
		return constants.ErrUnpackError
	}

	if block.Amount.Sign() != 0 {
		return constants.ErrInvalidTokenOrAmount
	}

	block.Data, err = definition.ABIPillars.PackMethod(p.MethodName)
	return err
}
func (p *UndelegateMethod) ReceiveBlock(context vm_context.AccountVmContext, sendBlock *nom.AccountBlock) ([]*nom.AccountBlock, error) {
	if err := p.ValidateSendBlock(sendBlock); err != nil {
		return nil, err
	}

	if delegation, err := definition.GetDelegationInfo(context.Storage(), sendBlock.Address); err == nil {
		common.DealWithErr(delegation.Delete(context.Storage()))
		momentum, err := context.GetFrontierMomentum()
		common.DealWithErr(err)
		pillarLog.Info("undelegating to pillar", "address", sendBlock.Address.String(), "height", momentum.Height)
	} else if err == constants.ErrDataNonExistent {
		return nil, err
	} else {
		common.DealWithErr(err)
	}
	return nil, nil
}

type UpdateEmbeddedPillarMethod struct {
	MethodName string
}

func (p *UpdateEmbeddedPillarMethod) GetPlasma(plasmaTable *constants.PlasmaTable) (uint64, error) {
	return plasmaTable.EmbeddedSimple, nil
}
func (p *UpdateEmbeddedPillarMethod) ValidateSendBlock(block *nom.AccountBlock) error {
	var err error

	if err := definition.ABIPillars.UnpackEmptyMethod(p.MethodName, block.Data); err != nil {
		return constants.ErrUnpackError
	}

	if block.Amount.Sign() != 0 {
		return constants.ErrInvalidTokenOrAmount
	}

	block.Data, err = definition.ABIPillars.PackMethod(p.MethodName)
	return err
}
func (p *UpdateEmbeddedPillarMethod) ReceiveBlock(context vm_context.AccountVmContext, sendBlock *nom.AccountBlock) ([]*nom.AccountBlock, error) {
	if err := p.ValidateSendBlock(sendBlock); err != nil {
		return nil, err
	}

	if err := checkAndPerformUpdate(context); err != nil {
		return nil, err
	}

	if err := updatePillarRewards(context); err != nil {
		return nil, err
	}
	return nil, nil
}
