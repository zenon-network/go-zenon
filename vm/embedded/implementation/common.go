// Package implementation contains the executable methods of the
// embedded contracts. Each method is a small struct carrying its ABI
// method name (and occasionally a plasma cost) that satisfies
// embedded.Method with three calls: GetPlasma quotes the plasma cost
// of the call from the constants.PlasmaTable tiers; ValidateSendBlock
// statically checks a user's send block, unpacking and re-packing
// block.Data so only canonical encodings reach the chain and, where
// the method requires it, checking the sent token and amount (some
// methods — AddPhase, UpdatePhase, the liquidity Fund and BurnZnn —
// place no token or amount restriction); ReceiveBlock executes the
// method against the contract's state when the VM auto-generates the
// contract-receive block.
//
// ReceiveBlock re-runs ValidateSendBlock, applies state changes
// through the Save/Get/Delete helpers of vm/embedded/definition and
// returns the descendant contract-send blocks to emit (withdrawals,
// refunds, mint or burn calls to the token contract). Returning an
// error rolls the whole call back and refunds the sent tokens — see
// vm.generateEmbeddedReceive — while unexpected database failures
// panic via common.DealWithErr.
//
// common.go holds the machinery shared by several contracts: the
// update throttle (checkAndPerformUpdate, at most one Update per
// constants.UpdateMinNumMomentums momentums) and the epoch ratchet
// (checkAndPerformUpdateEpoch, one reward epoch at a time), reward
// bookkeeping (addReward, CollectRewardMethod), the QSR registration
// deposits (DepositQsrMethod, WithdrawQsrMethod, checkAndConsumeQsr),
// pillar voting (VoteByNameMethod, VoteByProdAddressMethod),
// donations (DonateMethod) and the time-challenge and guardian
// checks used by the bridge and liquidity contracts (TimeChallenge,
// CheckSecurityInitialized).
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

// CanPerformUpdate checks if embedded contract can be updated: at
// least constants.UpdateMinNumMomentums momentums must have passed
// since the height stored by the last update.
//   - returns constants.ErrUpdateTooRecent if not due
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

// checkAndPerformUpdate limits calls to the update method to one
// every constants.UpdateMinNumMomentums momentums
//   - automatically stores the current frontier height as the new
//     last-update marker
//   - returns constants.ErrUpdateTooRecent if not due
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

// CanPerformEpochUpdate checks if embedded contract can perform an
// epoch update, used most commonly to give rewards: the next epoch
// must have ended at least constants.RewardTimeLimit seconds before
// the frontier momentum.
//   - returns constants.ErrEpochUpdateTooRecent if not due
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

// checkAndPerformUpdateEpoch checks if the next epoch can be
// processed and, if so, advances and saves the marker
//   - automatically moves up epoch by one if possible
//   - returns constants.ErrEpochUpdateTooRecent if not due
func checkAndPerformUpdateEpoch(context vm_context.AccountVmContext, epoch *definition.LastEpochUpdate) error {
	if err := CanPerformEpochUpdate(context, epoch); err != nil {
		return err
	}

	epoch.LastEpoch += 1
	return epoch.Save(context.Storage())
}

// CollectRewardMethod (CollectReward) is a common embedded method
// used to issue tokens to users based on the RewardDeposit object.
// The reward-paying contracts (pillar, sentinel, stake, liquidity)
// credit definition.RewardDeposit entries from their Update runs and
// the depositors call this method to turn the balance into freshly
// minted coins. Plasma is a struct field because the cost tier
// differs per registration — see vm/embedded/embedded.go.
type CollectRewardMethod struct {
	MethodName string
	Plasma     uint64
}

// addReward credits the amounts to both the address's collectable
// RewardDeposit and its permanent per-epoch RewardDepositHistory.
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

// GetPlasma quotes the Plasma the method was registered with,
// ignoring the table.
func (p *CollectRewardMethod) GetPlasma(plasmaTable *constants.PlasmaTable) (uint64, error) {
	// in case of sentinels it issues 2 rewards, but it's not called enough to cause issues
	return p.Plasma, nil
}

// ValidateSendBlock accepts an argument-less call carrying no
// tokens: extra ABI arguments fail with constants.ErrUnpackError and
// a non-zero Amount with constants.ErrInvalidTokenOrAmount.
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

// ReceiveBlock pays out the sender's RewardDeposit. When both
// amounts are zero it fails with constants.ErrNothingToWithdraw;
// otherwise it deletes the deposit and returns up to two descendant
// sends instructing the token contract to mint the deposited ZNN and
// QSR amounts to the sender.
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

// checkAndConsumeQsr is used for registration
//   - checks if the owner has deposited enough QSR, failing with
//     constants.ErrNotEnoughDepositedQsr otherwise
//   - consumes the required amount, deleting the deposit entry when
//     it reaches zero
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

// DepositQsrMethod (DepositQsr) accumulates QSR toward a future
// registration on the pillar or sentinel contract; registering
// consumes the required amount from this deposit rather than from
// the registration send block itself.
type DepositQsrMethod struct {
	MethodName string
}

// GetPlasma quotes the EmbeddedSimple tier; the call sends no
// response block.
func (p *DepositQsrMethod) GetPlasma(plasmaTable *constants.PlasmaTable) (uint64, error) {
	return plasmaTable.EmbeddedSimple, nil
}

// ValidateSendBlock accepts an argument-less call that must carry a
// positive amount of QSR; any other token or a zero amount fails
// with constants.ErrInvalidTokenOrAmount.
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

// ReceiveBlock adds the sent QSR to the sender's QsrDeposit entry.
// It cannot fail past validation and emits no descendant blocks.
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

// WithdrawQsrMethod (WithdrawQsr) refunds the sender's entire
// QsrDeposit; partial withdrawals are not possible.
type WithdrawQsrMethod struct {
	MethodName string
}

// GetPlasma quotes the EmbeddedWWithdraw tier, covering the one
// refund block the call sends back.
func (p *WithdrawQsrMethod) GetPlasma(plasmaTable *constants.PlasmaTable) (uint64, error) {
	return plasmaTable.EmbeddedWWithdraw, nil
}

// ValidateSendBlock accepts an argument-less call carrying no
// tokens: extra ABI arguments fail with constants.ErrUnpackError and
// a non-zero Amount with constants.ErrInvalidTokenOrAmount.
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

// ReceiveBlock deletes the sender's QsrDeposit and returns one
// descendant send refunding the full deposited amount; an empty
// deposit fails with constants.ErrNothingToWithdraw.
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

// DonateMethod (Donate) accepts tokens as a plain donation to the
// receiving contract's balance; nothing is recorded in contract
// state and donations cannot be reclaimed.
type DonateMethod struct {
	MethodName string
}

// GetPlasma quotes the EmbeddedSimple tier; the call sends no
// response block.
func (p *DonateMethod) GetPlasma(plasmaTable *constants.PlasmaTable) (uint64, error) {
	return plasmaTable.EmbeddedSimple, nil
}

// ValidateSendBlock accepts an argument-less call carrying a
// positive amount of any token; a zero amount fails with
// constants.ErrInvalidTokenOrAmount.
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

// ReceiveBlock keeps the sent tokens (already credited to the
// contract by the VM) and only logs the donation; no state is
// written and no descendant blocks are emitted.
func (p *DonateMethod) ReceiveBlock(context vm_context.AccountVmContext, sendBlock *nom.AccountBlock) ([]*nom.AccountBlock, error) {
	if err := p.ValidateSendBlock(sendBlock); err != nil {
		return nil, err
	}
	commonLog.Info("received donation", "embedded", sendBlock.ToAddress, "from-address", sendBlock.Address, "zts", sendBlock.TokenStandard, "amount", sendBlock.Amount)
	return nil, nil
}

// VoteByNameMethod (VoteByName) records a pillar's vote on a votable
// hash, the pillar being identified by name; the sender must be the
// pillar's stake address. Registered on the accelerator contract,
// where project and phase ids are the votable hashes.
type VoteByNameMethod struct {
	MethodName string
}

// Fee returns a zero fee. It is not part of the embedded.Method
// interface and has no callers.
func (p *VoteByNameMethod) Fee() (*big.Int, error) {
	return big.NewInt(0), nil
}

// GetPlasma quotes the EmbeddedSimple tier; the call sends no
// response block.
func (p *VoteByNameMethod) GetPlasma(plasmaTable *constants.PlasmaTable) (uint64, error) {
	return plasmaTable.EmbeddedSimple, nil
}

// ValidateSendBlock accepts a packed definition.PillarVote (id,
// pillar name, vote) carrying no tokens; a vote value of
// definition.VoteNotValid or above fails with
// constants.ErrForbiddenParam.
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

// ReceiveBlock saves the vote, overwriting any earlier vote the
// pillar cast on the same id. The id must be open for voting
// (constants.ErrDataNonExistent otherwise) and the sender must be
// the stake address of the named pillar in the momentum store's
// active-pillar list, else constants.ErrForbiddenParam.
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

// VoteByProdAddressMethod (VoteByProdAddress) records a pillar's
// vote on a votable hash with the sender identified as a pillar's
// block-producing address, letting the producer node vote without
// the stake address's keys. Registered on the accelerator contract.
type VoteByProdAddressMethod struct {
	MethodName string
}

// Fee returns a zero fee. It is not part of the embedded.Method
// interface and has no callers.
func (p *VoteByProdAddressMethod) Fee() (*big.Int, error) {
	return big.NewInt(0), nil
}

// GetPlasma quotes the EmbeddedSimple tier; the call sends no
// response block.
func (p *VoteByProdAddressMethod) GetPlasma(plasmaTable *constants.PlasmaTable) (uint64, error) {
	return plasmaTable.EmbeddedSimple, nil
}

// ValidateSendBlock accepts a packed definition.PillarVote without
// the name field (id, vote) carrying no tokens; a vote value of
// definition.VoteNotValid or above fails with
// constants.ErrForbiddenParam.
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

// ReceiveBlock resolves the sender to the active pillar whose
// block-producing address it is — failing with
// constants.ErrForbiddenParam when there is none — and saves the
// vote under that pillar's name, overwriting any earlier vote on the
// same id. The id must be open for voting
// (constants.ErrDataNonExistent otherwise).
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

// TimeChallenge advances the two-step submission scheme protecting
// sensitive bridge and liquidity actions (see
// definition.TimeChallengeInfo); hash is the hash of the call's
// parameters. When it differs from the stored one, a new challenge
// is recorded starting at the current frontier height and the saved
// info — ParamsHash still set — is returned, the caller being
// expected to stop there. When it matches a pending challenge, the
// call fails with constants.ErrTimeChallengeNotDue until strictly
// more than delay momentums have passed since the challenge started;
// once due, the challenge is reset and saved with a zeroed
// ParamsHash, which is how callers recognize a satisfied challenge
// and proceed with the action.
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

// CheckSecurityInitialized returns the contract's security
// configuration, failing with constants.ErrSecurityNotInitialized
// while fewer than constants.MinGuardians guardians have been
// nominated; guarded bridge and liquidity actions call it before
// anything else.
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
