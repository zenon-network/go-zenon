package vm

import (
	"math/big"

	"github.com/pkg/errors"

	"github.com/zenon-network/go-zenon/chain"
	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/chain/store"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/db"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/vm/constants"
	"github.com/zenon-network/go-zenon/vm/embedded"
	"github.com/zenon-network/go-zenon/vm/vm_context"
)

var (
	log = common.VmLogger
)

const (
	resultInvalid uint64 = iota
	resultSuccess
	resultFail
)

func errToStatus(err error) uint64 {
	switch err {
	case nil:
		return resultSuccess
	default:
		return resultFail
	}
}

type VM struct {
	context       vm_context.AccountVmContext
	frontierStore store.Momentum
}

func NewVM(context vm_context.AccountVmContext, frontierStore store.Momentum) *VM {
	return &VM{
		context:       context,
		frontierStore: frontierStore,
	}
}

func enoughPlasma(context vm_context.AccountVmContext, block *nom.AccountBlock) error {
	// embedded address have unlimited plasma
	if types.IsEmbeddedAddress(block.Address) {
		return nil
	}

	// Prevent potentially expensive database read operations by only
	// checking available plasma for blocks with fused plasma
	if block.FusedPlasma > 0 {
		available, err := AvailablePlasma(context.CacheStore(), context)
		common.DealWithErr(err)
		if available < block.FusedPlasma {
			return constants.ErrNotEnoughPlasma
		}
	}

	powPlasma := DifficultyToPlasma(block.Difficulty)
	block.TotalPlasma = powPlasma + block.FusedPlasma
	if block.TotalPlasma > constants.MaxPlasmaForAccountBlock {
		return constants.ErrBlockPlasmaLimitReached
	}

	basePlasma, err := GetBasePlasmaForAccountBlock(context, block)
	common.DealWithErr(err)

	block.BasePlasma = basePlasma

	if block.TotalPlasma < block.BasePlasma {
		return constants.ErrNotEnoughTotalPlasma
	}

	return context.AddChainPlasma(block.FusedPlasma)
}
func enoughFunds(context vm_context.AccountVmContext, block *nom.AccountBlock) bool {
	if block.TokenStandard == types.ZeroTokenStandard {
		return true
	}

	balance, err := context.GetBalance(block.TokenStandard)
	common.DealWithErr(err)
	if balance.Cmp(block.Amount) == -1 {
		return false
	}

	return true
}

// applyBlock is used to apply the block on top of the vm.context
// After calling applyBlock vm.context.Changes() has all the changes necessary to create a nom.AccountBlockTransaction
func (vm *VM) applyBlock(block *nom.AccountBlock) error {
	if err := enoughPlasma(vm.context, block); err != nil {
		return err
	}

	// In case vm will update some fields of block, make a copy of block.
	switch block.BlockType {
	case nom.BlockTypeUserSend, nom.BlockTypeContractSend:
		return vm.applySend(block)
	case nom.BlockTypeUserReceive:
		return vm.applyReceive(block)
	case nom.BlockTypeContractReceive:
		generated, _, err := vm.generateEmbeddedReceive(block.FromBlockHash)
		if err != nil {
			return err
		}
		if generated.ChangesHash != block.ChangesHash {
			return errors.Errorf("auto-received block has different changes-hash expected %v but got %v", generated.ChangesHash, block.ChangesHash)
		}
		computed := generated.ComputeHash()
		if computed != block.Hash {
			return errors.Errorf("auto-received block has different hash expected %v but got %v", computed, generated)
		}
		return nil
	default:
		panic("unknown block type")
	}
}
func (vm *VM) applySend(block *nom.AccountBlock) error {
	// check can make transaction
	if method, err := embedded.GetEmbeddedMethod(vm.context, block.ToAddress, block.Data); err != constants.ErrNotContractAddress {
		if err != nil {
			return err
		}

		// validate block
		err = method.ValidateSendBlock(block)
		if err != nil {
			return err
		}
	}

	// affect balance
	if !enoughFunds(vm.context, block) {
		return constants.ErrInsufficientBalance
	}

	vm.context.SubBalance(&block.TokenStandard, block.Amount)

	return nil
}
func (vm *VM) applyReceive(block *nom.AccountBlock) error {
	fromBlock, err := vm.frontierStore.GetAccountBlockByHash(block.FromBlockHash)
	if err != nil {
		return err
	}

	err = vm.context.MarkAsReceived(block.FromBlockHash)
	if err != nil {
		return err
	}

	vm.context.AddBalance(&fromBlock.TokenStandard, fromBlock.Amount)
	return nil
}

// generateEmbeddedReceive is used to generate the embedded receive nom.AccountBlock from an fromBlockHash
// Since the receive-block is auto-generated, we don't actually need the whole block (just the fromBlockHash)
// After calling applyBlock vm.context.Changes() has all the changes necessary to create a nom.AccountBlockTransaction
func (vm *VM) generateEmbeddedReceive(fromBlockHash types.Hash) (*nom.AccountBlock, error, error) {
	// mark block as received (only for contracts, using sequencer)
	vm.context.SequencerPopFront()

	sendBlock, err := vm.context.MomentumStore().GetAccountBlockByHash(fromBlockHash)
	if err != nil {
		return nil, nil, err
	}
	method, err := embedded.GetEmbeddedMethod(vm.context, sendBlock.ToAddress, sendBlock.Data)

	// can happen when a method is deleted in a spork (height 100) and someone calls it before the spork (height 95)
	// and the autoReceive uses momentum height 105 for various reasons
	if err == constants.ErrContractMethodNotFound {
		return vm.rollbackEmbedded(fromBlockHash, err)
	}

	vm.context.Save()
	// balance
	vm.context.AddBalance(&sendBlock.TokenStandard, sendBlock.Amount)
	// call code
	descendantBlocks, err := method.ReceiveBlock(vm.context, sendBlock)
	if err != nil {
		return vm.rollbackEmbedded(fromBlockHash, err)
	}
	// apply send-descendant-blocks
	for _, dblock := range descendantBlocks {
		err := vm.applySend(dblock)
		if err != nil {
			return vm.rollbackEmbedded(fromBlockHash, err)
		}
	}

	// everything went right, no rollback required
	vm.context.Done()
	return vm.finalizeEmbedded(fromBlockHash, descendantBlocks, nil)
}
func (vm *VM) rollbackEmbedded(fromBlockHash types.Hash, methodErr error) (*nom.AccountBlock, error, error) {
	sendBlock, err := vm.context.MomentumStore().GetAccountBlockByHash(fromBlockHash)
	common.DealWithErr(err) // impossible to not find send-block at rollback

	vm.context.Reset()
	// If sendBlock contains amount, add current amount to embedded to be able to refund it
	// This operation was rollbacked with vm.context.Reset()
	vm.context.AddBalance(&sendBlock.TokenStandard, sendBlock.Amount)
	descendantBlocks := make([]*nom.AccountBlock, 0, 1)

	// If sendBlock contained tokens, refund them
	if sendBlock.Amount.Sign() > 0 {
		dBlock := &nom.AccountBlock{
			BlockType:     nom.BlockTypeContractSend,
			Address:       sendBlock.ToAddress,
			ToAddress:     sendBlock.Address,
			Amount:        new(big.Int).Set(sendBlock.Amount),
			TokenStandard: sendBlock.TokenStandard,
		}

		err := vm.applySend(dBlock)
		if err != nil {
			log.Error("Unable to apply descendant blocks for refund", "reason", err, "send-block-hash", sendBlock.Hash)
			return nil, nil, err
		}

		descendantBlocks = append(descendantBlocks, dBlock)
	}

	return vm.finalizeEmbedded(fromBlockHash, descendantBlocks, methodErr)
}
func (vm *VM) finalizeEmbedded(fromBlockHash types.Hash, descendantBlocks []*nom.AccountBlock, executionError error) (*nom.AccountBlock, error, error) {
	var err error

	prevFrontier, err := vm.context.Frontier()
	common.DealWithErr(err)
	prevHash := types.ZeroHash
	height := uint64(1)
	if prevFrontier != nil {
		prevHash = prevFrontier.Hash
		height = prevFrontier.Height + 1
	}

	momentum, err := vm.context.MomentumStore().GetFrontierMomentum()
	common.DealWithErr(err)

	for _, dblock := range descendantBlocks {
		dblock.Version = 1
		dblock.ChainIdentifier = vm.context.MomentumStore().ChainIdentifier()
		dblock.BlockType = nom.BlockTypeContractSend
		dblock.Address = *vm.context.Address()
		dblock.MomentumAcknowledged = momentum.Identifier()
		dblock.PreviousHash = prevHash
		dblock.Height = height
		dblock.ChangesHash = types.ZeroHash
		dblock.Hash = dblock.ComputeHash()
		prevHash = dblock.Hash
		height = height + 1
	}

	changes, err := vm.context.Changes()
	common.DealWithErr(err)
	block := &nom.AccountBlock{
		Version:              1,
		ChainIdentifier:      vm.context.MomentumStore().ChainIdentifier(),
		BlockType:            nom.BlockTypeContractReceive,
		Address:              *vm.context.Address(),
		FromBlockHash:        fromBlockHash,
		MomentumAcknowledged: momentum.Identifier(),
		PreviousHash:         prevHash,
		Height:               height,
		Data:                 common.Uint64ToBytes(errToStatus(executionError)),
		DescendantBlocks:     descendantBlocks,
		ChangesHash:          db.PatchHash(changes),
	}

	block.Hash = block.ComputeHash()
	return block, executionError, nil
}

type MomentumVM struct {
	context vm_context.MomentumVMContext
}

func NewMomentumVM(context vm_context.MomentumVMContext) *MomentumVM {
	return &MomentumVM{
		context: context,
	}
}

func (vm *MomentumVM) applyMomentum(pool chain.AccountPool, momentum *nom.Momentum) error {
	momentumStore := vm.context

	for _, header := range momentum.Content {
		if err := momentumStore.AddAccountBlockTransaction(*header, pool.GetPatch(header.Address, header.Identifier())); err != nil {
			return err
		}
	}

	return nil
}
