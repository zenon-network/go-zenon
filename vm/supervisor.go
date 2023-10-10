package vm

import (
	"fmt"
	"math/big"
	"runtime/debug"

	"github.com/pkg/errors"

	"github.com/zenon-network/go-zenon/chain"
	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/db"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/consensus"
	"github.com/zenon-network/go-zenon/verifier"
	"github.com/zenon-network/go-zenon/vm/constants"
	"github.com/zenon-network/go-zenon/vm/vm_context"
)

// SignFunc is the function type defining the callback when a block requires a
// method to sign the transaction in supervisor
type SignFunc func(data []byte) (signedData []byte, addr *types.Address, pubkey []byte, err error)

type Supervisor struct {
	log common.Logger

	chain     chain.Chain
	consensus consensus.Consensus
	verifier  verifier.Verifier
}

type ContractExecution struct {
	Transaction   *nom.AccountBlockTransaction
	ReturnedError error
}

func NewSupervisor(chain chain.Chain, consensus consensus.Consensus) *Supervisor {
	return &Supervisor{
		log:       common.SupervisorLogger,
		chain:     chain,
		consensus: consensus,
		verifier:  verifier.NewVerifier(chain, consensus),
	}
}

func (s *Supervisor) newBlockContext(block *nom.AccountBlock) vm_context.AccountVmContext {
	momentumStore := s.chain.GetMomentumStore(block.MomentumAcknowledged)
	accountStore := s.chain.GetAccountStore(block.Address, block.Previous())
	cache := s.consensus.FixedPillarReader(block.MomentumAcknowledged)
	if momentumStore == nil {
		panic(fmt.Sprintf("can't find momentumStore for %v", block.MomentumAcknowledged))
	}
	if accountStore == nil {
		panic(fmt.Sprintf("can't find accountStore for %v %v", block.Address, block.Previous()))
	}
	if cache == nil {
		panic(fmt.Sprintf("can't find cache for %v", block.MomentumAcknowledged))
	}
	return vm_context.NewAccountContext(
		momentumStore,
		accountStore,
		cache,
	)
}
func (s *Supervisor) newMomentumContext(momentum *nom.Momentum) vm_context.MomentumVMContext {
	return vm_context.NewMomentumVMContext(
		s.chain.GetMomentumStore(momentum.Previous()),
	)
}

func (s *Supervisor) ApplyBlock(block *nom.AccountBlock) (*nom.AccountBlockTransaction, error) {
	if block.BlockType == nom.BlockTypeContractSend {
		return nil, errors.Errorf("can't apply BlockTypeContractSend")
	}
	return s.applyBlock(block, nil)
}
func (s *Supervisor) ApplyMomentum(detailed *nom.DetailedMomentum) (result *nom.MomentumTransaction, internalErr error) {
	momentum := detailed.Momentum
	defer func() {
		if err := recover(); err != nil {
			s.log.Error("vm panic when applying momentum", "identifier", momentum.Identifier(), "reason", err, "stack", string(debug.Stack()))

			result = nil
			internalErr = constants.ErrVmRunPanic
		}
	}()

	if err := s.verifier.Momentum(detailed); err != nil {
		return nil, err
	}
	context := s.newMomentumContext(momentum)
	vm := NewMomentumVM(context)
	err := vm.applyMomentum(s.chain, momentum)
	if err != nil {
		return nil, err
	}
	transaction, err := s.packMomentum(context, momentum, nil, false)
	if err != nil {
		return nil, err
	}
	return transaction, nil
}

func (s *Supervisor) GenerateFromTemplate(template *nom.AccountBlock, signFunc SignFunc) (*nom.AccountBlockTransaction, error) {
	if err := s.setAll(template); err != nil {
		return nil, err
	}
	context := s.newBlockContext(template)
	if err := s.setBlockPlasma(context, template); err != nil {
		return nil, err
	}
	return s.applyBlock(template, signFunc)
}
func (s *Supervisor) GenerateAutoReceive(sendBlock *nom.AccountBlock) (*ContractExecution, error) {
	template := &nom.AccountBlock{
		BlockType:     nom.BlockTypeContractReceive,
		Address:       sendBlock.ToAddress,
		FromBlockHash: sendBlock.Hash,
	}
	if err := s.setAll(template); err != nil {
		return nil, err
	}

	if err := s.verifier.AccountBlock(template); err != nil {
		return nil, err
	}
	context := s.newBlockContext(template)
	if err := s.setBlockPlasma(context, template); err != nil {
		return nil, err
	}
	vm := NewVM(context)
	block, methodErr, err := vm.generateEmbeddedReceive(template.FromBlockHash)
	if err := s.verifier.AccountBlock(block); err != nil {
		return nil, err
	}

	if err != nil {
		return nil, err
	}
	transaction, err := s.packBlock(context, block, nil)
	if err != nil {
		return nil, err
	}

	return &ContractExecution{
		Transaction:   transaction,
		ReturnedError: methodErr,
	}, nil
}
func (s *Supervisor) GenerateMomentum(detailed *nom.DetailedMomentum, signFunc SignFunc) (result *nom.MomentumTransaction, internalErr error) {
	template := detailed.Momentum
	defer func() {
		if err := recover(); err != nil {
			s.log.Error("vm panic when applying momentum", "identifier", template.Identifier(), "reason", err, "stack", string(debug.Stack()))

			result = nil
			internalErr = constants.ErrVmRunPanic
		}
	}()

	if err := s.verifier.Momentum(detailed); err != nil {
		return nil, err
	}
	context := s.newMomentumContext(template)
	vm := NewMomentumVM(context)
	err := vm.applyMomentum(s.chain, template)
	if err != nil {
		return nil, err
	}
	transaction, err := s.packMomentum(context, template, signFunc, false)
	if err != nil {
		return nil, err
	}
	return transaction, nil
}
func (s *Supervisor) GenerateGenesisMomentum(template *nom.Momentum, pool chain.AccountPool) (result *nom.MomentumTransaction, internalErr error) {
	defer func() {
		if err := recover(); err != nil {
			s.log.Error("vm panic when applying momentum", "identifier", template.Identifier(), "reason", err, "stack", string(debug.Stack()))

			result = nil
			internalErr = constants.ErrVmRunPanic
		}
	}()

	context := vm_context.NewGenesisMomentumVMContext()
	vm := NewMomentumVM(context)
	err := vm.applyMomentum(pool, template)
	if err != nil {
		return nil, err
	}
	transaction, err := s.packMomentum(context, template, nil, true)
	if err != nil {
		return nil, err
	}
	return transaction, nil
}

func (s *Supervisor) applyBlock(block *nom.AccountBlock, signFunc SignFunc) (transaction *nom.AccountBlockTransaction, internalErr error) {
	defer func() {
		if err := recover(); err != nil {
			l := s.log.New("block", block.Header())
			l.Error("vm panic when applying block", "reason", err, "stack", string(debug.Stack()))

			transaction = nil
			internalErr = constants.ErrVmRunPanic
		}
	}()

	if err := s.verifier.AccountBlock(block); err != nil {
		return nil, err
	}
	context := s.newBlockContext(block)
	vm := NewVM(context)
	err := vm.applyBlock(block)
	if err != nil {
		return nil, err
	}

	transaction, err = s.packBlock(context, block, signFunc)
	if err != nil {
		return nil, err
	}

	return transaction, nil
}

func (s *Supervisor) setAll(template *nom.AccountBlock) error {
	if err := s.setBlockMomentum(template); err != nil {
		return err
	}
	if err := s.setBlockHH(template); err != nil {
		return err
	}
	s.setBlockFields(template)
	return nil
}
func (s *Supervisor) packBlock(context vm_context.AccountVmContext, block *nom.AccountBlock, signFunc SignFunc) (*nom.AccountBlockTransaction, error) {
	changes, err := context.Changes()
	if err != nil {
		return nil, err
	}

	if signFunc != nil {
		block.Hash = block.ComputeHash()
		signature, _, publicKey, err := signFunc(block.Hash.Bytes())
		if err != nil {
			return nil, err
		}
		block.Signature = signature
		block.PublicKey = publicKey
	}
	if signFunc != nil {
		block.ChangesHash = db.PatchHash(changes)
		block.Hash = block.ComputeHash()
		signature, _, publicKey, err := signFunc(block.Hash.Bytes())
		if err != nil {
			return nil, err
		}
		block.Signature = signature
		block.PublicKey = publicKey
	}

	transaction := &nom.AccountBlockTransaction{
		Block:   block,
		Changes: changes,
	}
	if err := s.verifier.AccountBlockTransaction(transaction); err != nil {
		return nil, err
	}

	return transaction, nil
}
func (s *Supervisor) packMomentum(context vm_context.MomentumVMContext, momentum *nom.Momentum, signFunc SignFunc, isGenesis bool) (*nom.MomentumTransaction, error) {
	changes, err := context.Changes()

	if err != nil {
		return nil, err
	}

	if signFunc != nil || isGenesis {
		momentum.ChangesHash = db.PatchHash(changes)
		momentum.Hash = momentum.ComputeHash()
	}
	if signFunc != nil {
		signature, _, publicKey, err := signFunc(momentum.Hash.Bytes())
		if err != nil {
			return nil, err
		}
		momentum.Signature = signature
		momentum.PublicKey = publicKey
	}

	transaction := &nom.MomentumTransaction{
		Momentum: momentum,
		Changes:  changes,
	}
	if !isGenesis {
		if err := s.verifier.MomentumTransaction(transaction); err != nil {
			return nil, err
		}
	}

	return transaction, nil
}

func (s *Supervisor) setBlockPlasma(context vm_context.AccountVmContext, block *nom.AccountBlock) error {
	if block.Difficulty == 0 && block.FusedPlasma == 0 {
		base, err := GetBasePlasmaForAccountBlock(context, block)
		if err != nil {
			return err
		}
		block.FusedPlasma = base
	}
	return nil
}
func (s *Supervisor) setBlockFields(block *nom.AccountBlock) {
	block.ChainIdentifier = s.chain.ChainIdentifier()
	if block.Version == 0 {
		block.Version = 1
	}
	switch block.BlockType {
	case nom.BlockTypeUserSend, nom.BlockTypeContractSend:
		if block.Amount == nil {
			block.Amount = big.NewInt(0)
		}
	case nom.BlockTypeUserReceive, nom.BlockTypeContractReceive:
		block.Amount = common.Big0
		block.TokenStandard = types.ZeroTokenStandard
	}
}
func (s *Supervisor) setBlockHH(block *nom.AccountBlock) error {
	if block.PreviousHash == types.ZeroHash && block.Height == 0 {
		store := s.chain.GetFrontierAccountStore(block.Address)
		frontier := store.Identifier()

		block.PreviousHash = frontier.Hash
		block.Height = frontier.Height + 1
	}
	return nil
}
func (s *Supervisor) setBlockMomentum(block *nom.AccountBlock) error {
	store := s.chain.GetFrontierMomentumStore()
	frontierMomentum, err := store.GetFrontierMomentum()
	if err != nil {
		return err
	}
	if block.MomentumAcknowledged.IsZero() {
		if types.IsEmbeddedAddress(block.Address) {
			confirmation, err := store.GetBlockConfirmationHeight(block.FromBlockHash)
			if err != nil {
				return err
			}
			if confirmation == 0 {
				return errors.Errorf("can't find block that confirms contract-receive")
			}
			momentum, err := store.GetMomentumByHeight(confirmation)
			if err != nil {
				return err
			}
			block.MomentumAcknowledged = momentum.Identifier()
		} else {
			block.MomentumAcknowledged = frontierMomentum.Identifier()
		}
	}
	return nil
}
