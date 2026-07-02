// Package vm executes account blocks and momentums against the
// ledger state. The Supervisor is the entry point: it builds a
// vm_context for the block or momentum being processed, runs the
// verifier, lets the VM (account blocks) or MomentumVM (momentums)
// apply the state changes, and packs the result into a
// nom.AccountBlockTransaction or nom.MomentumTransaction whose
// Changes patch hashes to the ChangesHash field.
//
// For account blocks the VM checks plasma (see
// GetBasePlasmaForAccountBlock and AvailablePlasma), adjusts
// balances, and — for sends addressed to an embedded contract —
// dispatches to the contract method selected by the block's Data via
// embedded.GetEmbeddedMethod. Receives addressed to embedded
// contracts are not signed by users: the VM auto-generates the
// ContractReceive block, executing the method inside a
// snapshot/rollback window so a failing method refunds the sent
// tokens instead of mutating contract state.
//
// For momentums the MomentumVM folds the patch of every confirmed
// account block into the momentum store, producing the momentum-level
// patch committed by the chain.
package vm

import (
	"math/big"

	"github.com/pkg/errors"

	"github.com/zenon-network/go-zenon/chain"
	"github.com/zenon-network/go-zenon/chain/nom"
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

// Execution status codes stored (big-endian, 8 bytes) in the Data
// field of a generated ContractReceive block: resultSuccess when the
// embedded method executed, resultFail when it returned an error and
// was rolled back. resultInvalid (0) is never produced by errToStatus.
const (
	resultInvalid uint64 = iota
	resultSuccess
	resultFail
)

// errToStatus maps an embedded-method execution error to the status
// code embedded in a ContractReceive block's Data.
func errToStatus(err error) uint64 {
	switch err {
	case nil:
		return resultSuccess
	default:
		return resultFail
	}
}

// VM applies a single account block on top of an account VM context.
// It is single-use: the supervisor creates one per block and reads
// the accumulated state changes from the context afterwards.
type VM struct {
	context vm_context.AccountVmContext
}

// NewVM returns a VM that applies one account block on top of
// context.
func NewVM(context vm_context.AccountVmContext) *VM {
	return &VM{
		context: context,
	}
}

// enoughPlasma checks the block's plasma against the account's
// resources and fills in the block's plasma accounting fields:
// FusedPlasma may not exceed AvailablePlasma, TotalPlasma (fused +
// PoW plasma from Difficulty) may not exceed MaxPlasmaForAccountBlock
// and must cover BasePlasma (recomputed here via
// GetBasePlasmaForAccountBlock). On success the consumed FusedPlasma
// is added to the account's chain-plasma counter. Embedded addresses
// have unlimited plasma and skip all checks.
func enoughPlasma(context vm_context.AccountVmContext, block *nom.AccountBlock) error {
	// embedded address have unlimited plasma
	if types.IsEmbeddedAddress(block.Address) {
		return nil
	}

	available, err := AvailablePlasma(context.MomentumStore(), context)
	common.DealWithErr(err)
	if available < block.FusedPlasma {
		return constants.ErrNotEnoughPlasma
	}

	powPlasma := DifficultyToPlasma(block.Difficulty)
	block.TotalPlasma = powPlasma + block.FusedPlasma
	if block.TotalPlasma > constants.MaxPlasmaForAccountBlock {
		return constants.ErrBlockPlasmaLimitReached
	}

	block.BasePlasma, err = GetBasePlasmaForAccountBlock(context, block)
	common.DealWithErr(err)

	if block.TotalPlasma < block.BasePlasma {
		return constants.ErrNotEnoughTotalPlasma
	}

	return context.AddChainPlasma(block.FusedPlasma)
}

// enoughFunds reports whether the account's balance covers the send
// block's Amount; a zero token standard means no transfer and always
// passes.
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
//
// The block must already have passed the verifier. After the plasma
// check it dispatches on BlockType; a ContractReceive block is not
// applied directly but re-generated from its FromBlockHash, and the
// claimed Hash and ChangesHash must match the re-generated block
// exactly.
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

// applySend applies a UserSend or ContractSend block: when the
// destination is an embedded contract the method selected by
// block.Data must validate the block (ValidateSendBlock), then the
// sent amount is subtracted from the account's balance.
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

// applyReceive applies a UserReceive block: it marks the referenced
// send block as received and credits its amount to the account's
// balance.
func (vm *VM) applyReceive(block *nom.AccountBlock) error {
	fromBlock, err := vm.context.MomentumStore().GetAccountBlockByHash(block.FromBlockHash)
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
//
// It pops the send from the embedded sequencer, snapshots the context
// (Save), credits the sent amount and runs the contract method
// selected by the send block's Data. The method's state changes and
// the descendant ContractSend blocks it returns are kept only if
// everything succeeds (Done); on a method error the context is rolled
// back and the sent tokens are refunded (see rollbackEmbedded). The
// three results are the generated block, the method's execution error
// (recorded in the block's Data as a status code, not a failure of
// generation) and an internal error that aborts generation.
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

// rollbackEmbedded discards the state changes of a failed embedded
// method call (Reset), re-credits the sent amount and, when the send
// carried tokens, emits a single ContractSend descendant refunding
// them to the sender. The resulting receive block records methodErr's
// status in its Data.
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

// finalizeEmbedded assembles the ContractReceive block: the
// descendant ContractSend blocks are chained at the heights
// immediately above the account's frontier (each with a zero
// ChangesHash — contract state changes are attributed to the receive
// block) and the receive block itself sits on top of them, carrying
// the execution status in Data and db.PatchHash of the context's
// accumulated changes as its ChangesHash. MomentumAcknowledged is the
// frontier of the context's momentum store, i.e. the momentum that
// confirmed the send block.
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

// MomentumVM applies a single momentum on top of a momentum VM
// context. Like VM it is single-use; the supervisor reads the
// momentum-level patch from the context afterwards.
type MomentumVM struct {
	context vm_context.MomentumVMContext
}

// NewMomentumVM returns a MomentumVM that applies one momentum on top
// of context.
func NewMomentumVM(context vm_context.MomentumVMContext) *MomentumVM {
	return &MomentumVM{
		context: context,
	}
}

// applyMomentum folds the state patch of every account block listed
// in the momentum's content into the momentum store (see
// store.Momentum.AddAccountBlockTransaction); the patches are fetched
// from the account pool by header. The context's resulting Changes
// patch becomes the momentum's ChangesHash material.
func (vm *MomentumVM) applyMomentum(pool chain.AccountPool, momentum *nom.Momentum) error {
	momentumStore := vm.context

	for _, header := range momentum.Content {
		if err := momentumStore.AddAccountBlockTransaction(*header, pool.GetPatch(header.Address, header.Identifier())); err != nil {
			return err
		}
	}

	return nil
}
