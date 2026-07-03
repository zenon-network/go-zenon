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

// checkPillarNameStatic performs basic static checks to determine if
// a pillar name is valid: non-empty, at most
// constants.PillarNameLengthMax bytes and made of alphanumeric runs
// separated by single dots, dashes or underscores; anything else
// fails with constants.ErrInvalidName.
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

// checkAvailableProducingAddress enforces the producing-address
// reuse rule: an address is acceptable when it has never been bound
// to a pillar, or when it is bound to this very pillar name (a past
// or current address of the same pillar); an address bound to any
// other pillar fails with constants.ErrNotUnique. Bindings are never
// deleted, so every address a pillar has ever used stays reserved
// for it.
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

// checkPillarPercentages rejects give-percentages above 100 with
// constants.ErrForbiddenParam.
func checkPillarPercentages(param *definition.RegisterParam) error {
	if param.GiveBlockRewardPercentage > 100 || param.GiveBlockRewardPercentage < 0 {
		return constants.ErrForbiddenParam
	}
	if param.GiveDelegateRewardPercentage > 100 || param.GiveDelegateRewardPercentage < 0 {
		return constants.ErrForbiddenParam
	}
	return nil
}

// checkAndRegisterPillar is used for registration
//   - checks the validity of the pillar information; a name already
//     registered (even by a since-revoked pillar) or a producing
//     address bound to another pillar fails with
//     constants.ErrNotUnique
//   - saves the PillarInfo — with the locked ZNN amount and the
//     frontier timestamp as registration time — and the producing
//     address binding
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

// GetQsrCostForNextPillar returns the QSR deposit the next Register
// call consumes: constants.PillarQsrStakeBaseAmount plus
// constants.PillarQsrStakeIncreaseAmount for every active
// normal-type pillar already registered. Legacy registrations are
// not counted and always pay the base amount.
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

// PillarGetRevokeStatus reports where the pillar stands in its
// revocation cycle at the momentum's timestamp. The cycle repeats
// from the registration time: constants.PillarEpochLockTime (83
// days) of lock followed by constants.PillarEpochRevokeTime (7 days)
// of revocability.
// It returns:
//   - true and the seconds the pillar can still be revoked for, when
//     revocation is currently possible
//   - false and the seconds until revocation opens, otherwise
func PillarGetRevokeStatus(old *definition.PillarInfo, m *nom.Momentum) (bool, int64) {
	epochTime := (m.Timestamp.Unix() - old.RegistrationTime) % (constants.PillarEpochLockTime + constants.PillarEpochRevokeTime)
	if epochTime < constants.PillarEpochLockTime {
		return false, constants.PillarEpochLockTime - epochTime
	} else {
		return true, (constants.PillarEpochLockTime + constants.PillarEpochRevokeTime) - epochTime
	}
}

// RegisterMethod (Register) creates a normal-type pillar. The send
// must carry exactly constants.PillarStakeAmount ZNN, locked until
// revocation, and the sender must have deposited the current QSR
// cost (GetQsrCostForNextPillar) via DepositQsr beforehand; the
// consumed QSR is burned.
type RegisterMethod struct {
	MethodName string
}

// GetPlasma quotes twice the EmbeddedSimple tier, covering the burn
// transaction the registration sends to the token contract.
func (p *RegisterMethod) GetPlasma(plasmaTable *constants.PlasmaTable) (uint64, error) {
	// include burn transaction
	return 2 * plasmaTable.EmbeddedSimple, nil
}

// ValidateSendBlock accepts a packed definition.RegisterParam with a
// valid pillar name and give-percentages of at most 100, carried by
// a send of exactly constants.PillarStakeAmount ZNN. The QSR cost is
// taken from the contract-side deposit and cannot be checked
// statically.
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

// ReceiveBlock registers the pillar with the sender as stake
// address: the name and producing address must be available
// (constants.ErrNotUnique) and the sender's QSR deposit must cover
// the current cost (constants.ErrNotEnoughDepositedQsr). It returns
// one descendant send burning the consumed QSR at the token
// contract.
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

// LegacyRegisterMethod (RegisterLegacy) creates a pillar against a
// legacy (swap-era) pillar slot. On top of the Register requirements
// the caller proves ownership of the legacy secp256k1 public key
// with a signature binding it to the sender's address; in exchange
// the QSR cost is the flat constants.PillarQsrStakeBaseAmount
// instead of the growing normal-type cost.
type LegacyRegisterMethod struct {
	MethodName string
}

// GetPlasma quotes twice the EmbeddedSimple tier, covering the burn
// transaction the registration sends to the token contract.
func (p *LegacyRegisterMethod) GetPlasma(plasmaTable *constants.PlasmaTable) (uint64, error) {
	// include burn transaction
	return 2 * plasmaTable.EmbeddedSimple, nil
}

// ValidateSendBlock accepts a packed definition.LegacyRegisterParam
// — the Register parameters plus a base64 public key and signature —
// carried by a send of exactly constants.PillarStakeAmount ZNN. The
// signature must recover to the public key over the legacy-pillar
// swap message for the sender's address (see CheckSwapSignature).
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

// ReceiveBlock consumes one slot of the LegacyPillarEntry stored
// under the public key's key-id hash — a missing entry fails with
// constants.ErrNotEnoughSlots; the count is decremented and the
// entry deleted at zero — then registers the pillar with
// definition.LegacyPillarType, consumes the flat base QSR deposit
// and returns one descendant send burning it at the token contract.
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

// RevokeMethod (Revoke) closes a pillar and returns its locked ZNN.
// Revocation is only possible inside the periodic revoke window —
// see PillarGetRevokeStatus — and is permanent: the name stays
// registered and can never be reused.
type RevokeMethod struct {
	MethodName string
}

// GetPlasma quotes the EmbeddedWWithdraw tier, covering the one
// refund block the revocation sends back.
func (p *RevokeMethod) GetPlasma(plasmaTable *constants.PlasmaTable) (uint64, error) {
	return plasmaTable.EmbeddedWWithdraw, nil
}

// ValidateSendBlock accepts a valid pillar name carried by a send
// with no tokens.
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

// ReceiveBlock revokes the named pillar: it must exist
// (constants.ErrDataNonExistent), still be active
// (constants.ErrNotActive), be owned by the sender — the stake
// address — (constants.ErrPermissionDenied) and sit inside its
// revoke window (constants.RevokeNotDue). The revoke time is
// stamped, the stored amount zeroed and one descendant send refunds
// constants.PillarStakeAmount ZNN to the stake address.
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

// pillarEpochReward is one pillar's reward breakdown for one epoch:
// the delegation share, the momentum-producing share, their sum and
// the produced/expected momentum counts and delegation weight they
// were computed from.
type pillarEpochReward struct {
	DelegationReward *big.Int
	BlockReward      *big.Int
	TotalReward      *big.Int
	ProducedBlockNum int32
	ExpectedBlockNum int32
	Weight           *big.Int
}

// computeDetailedPillarReward distributes one epoch's rewards across
// all pillars. Each pillar's raw reward is split per its
// give-percentages: the kept part is credited to its
// reward-withdraw address and the given part — GiveBlockReward% of
// the block reward plus GiveDelegateReward% of the delegation reward
// — is divided among that epoch's delegators pro-rata to their
// delegated amounts, falling back to the pillar's reward address
// when it had no delegation weight. A PillarEpochHistory entry is
// also saved per pillar for the RPC history endpoints.
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

// computePillarsRewardForEpoch computes the raw reward of every
// pillar present in the epoch's consensus stats, keyed by pillar
// name.
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

// computePillarRewardForEpoch computes one pillar's raw reward for
// one epoch from the consensus stats and the per-momentum rates of
// constants.PillarRewardPerMomentum; the formula is spelled out in
// the body. A pillar expected to produce no momentums earns nothing.
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

// updatePillarRewards distributes the rewards of every epoch that
// has become due, advancing the epoch marker one epoch at a time
// until checkAndPerformUpdateEpoch reports the next epoch is still
// too recent.
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

// UpdatePillarMethod (UpdatePillar) lets a pillar's stake address
// change its producing address, reward-withdraw address and
// give-percentages; the name and the locked deposit cannot change.
type UpdatePillarMethod struct {
	MethodName string
}

// GetPlasma quotes the EmbeddedSimple tier; the call sends no
// response block.
func (p *UpdatePillarMethod) GetPlasma(plasmaTable *constants.PlasmaTable) (uint64, error) {
	return plasmaTable.EmbeddedSimple, nil
}

// ValidateSendBlock accepts a packed definition.RegisterParam with a
// valid pillar name and give-percentages of at most 100, carried by
// a send with no tokens.
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

// ReceiveBlock applies the changed fields to the named pillar, which
// must exist (constants.ErrDataNonExistent), belong to the sender
// (constants.ErrPermissionDenied) and be active
// (constants.ErrNotActive). A new producing address must pass the
// same reuse rule as at registration
// (checkAvailableProducingAddress) and is bound to the pillar on top
// of its earlier bindings.
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

// DelegateMethod (Delegate) points the sender's delegation at the
// named pillar. The delegated weight is the sender's ZNN balance,
// tracked by the consensus layer; a new delegation replaces any
// earlier one.
type DelegateMethod struct {
	MethodName string
}

// GetPlasma quotes the EmbeddedSimple tier; the call sends no
// response block.
func (p *DelegateMethod) GetPlasma(plasmaTable *constants.PlasmaTable) (uint64, error) {
	return plasmaTable.EmbeddedSimple, nil
}

// ValidateSendBlock accepts a valid pillar name carried by a send
// with no tokens.
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

// ReceiveBlock saves the sender's DelegationInfo for the named
// pillar, which must exist (constants.ErrDataNonExistent) and be
// active (constants.ErrNotActive).
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

// UndelegateMethod (Undelegate) removes the sender's delegation.
type UndelegateMethod struct {
	MethodName string
}

// GetPlasma quotes the EmbeddedSimple tier; the call sends no
// response block.
func (p *UndelegateMethod) GetPlasma(plasmaTable *constants.PlasmaTable) (uint64, error) {
	return plasmaTable.EmbeddedSimple, nil
}

// ValidateSendBlock accepts an argument-less call carrying no
// tokens.
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

// ReceiveBlock deletes the sender's DelegationInfo, failing with
// constants.ErrDataNonExistent when no delegation exists.
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

// UpdateEmbeddedPillarMethod (Update) advances the pillar contract's
// reward bookkeeping. Anyone may call it; it is throttled to one run
// every constants.UpdateMinNumMomentums momentums.
type UpdateEmbeddedPillarMethod struct {
	MethodName string
}

// GetPlasma quotes the EmbeddedSimple tier; the call sends no
// response block.
func (p *UpdateEmbeddedPillarMethod) GetPlasma(plasmaTable *constants.PlasmaTable) (uint64, error) {
	return plasmaTable.EmbeddedSimple, nil
}

// ValidateSendBlock accepts an argument-less call carrying no
// tokens.
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

// ReceiveBlock runs the update throttle (constants.ErrUpdateTooRecent
// when called again too soon) and then distributes the pillar
// rewards of every epoch that has become due (updatePillarRewards),
// crediting RewardDeposit entries collectable via CollectReward.
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
