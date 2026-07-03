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

// SignFunc is the callback a Generate* method uses to sign a freshly
// packed block or momentum: it receives the bytes to sign — the
// recomputed hash — and returns the signature and the public key it
// verifies against. The address return is ignored by the supervisor.
type SignFunc func(data []byte) (signedData []byte, addr *types.Address, pubkey []byte, err error)

// Supervisor is the entry point of the vm package: it pairs the chain
// with a verifier and turns raw account blocks and momentums into
// verified transactions ready for insertion. Apply* methods process
// blocks received from outside (RPC, network sync), Generate* methods
// build new ones locally (pillar momentum production, embedded
// auto-receives, node-internal sends). VM panics during execution are
// recovered and surfaced as constants.ErrVmRunPanic.
type Supervisor struct {
	log common.Logger

	chain     chain.Chain
	consensus consensus.Consensus
	verifier  verifier.Verifier
}

// ContractExecution is the result of GenerateAutoReceive: the
// ContractReceive transaction to insert plus the error returned by
// the embedded method itself (non-nil when the call failed and was
// rolled back, with a refund block if the send carried tokens; the
// transaction is still valid and records the failure status).
type ContractExecution struct {
	Transaction   *nom.AccountBlockTransaction
	ReturnedError error
}

// NewSupervisor returns a Supervisor over chain, creating its own
// verifier from chain and consensus.
func NewSupervisor(chain chain.Chain, consensus consensus.Consensus) *Supervisor {
	return &Supervisor{
		log:       common.SupervisorLogger,
		chain:     chain,
		consensus: consensus,
		verifier:  verifier.NewVerifier(chain, consensus),
	}
}

// newBlockContext builds the account VM context a block executes in:
// the momentum store at the block's MomentumAcknowledged, the account
// store at the block's predecessor and a pillar reader fixed at the
// acknowledged momentum. It panics when any of the three cannot be
// resolved (the verifier rejects such blocks first).
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

// newMomentumContext builds the momentum VM context a momentum
// executes in: the momentum store snapshot at its predecessor.
func (s *Supervisor) newMomentumContext(momentum *nom.Momentum) vm_context.MomentumVMContext {
	return vm_context.NewMomentumVMContext(
		s.chain.GetMomentumStore(momentum.Previous()),
	)
}

// ApplyBlock verifies and executes a fully-formed account block
// received from outside (RPC publish, chain bridge) and returns the
// transaction pairing it with the state patch it produces. The VM
// never signs the block and leaves the author's hashed and signature
// fields alone, but it does populate the plasma accounting fields
// (TotalPlasma from PoW plus fused plasma, BasePlasma from the
// block's cost) during execution. ChangesHash is validated only for
// ContractReceive blocks, whose regenerated counterpart must match;
// for user blocks it is recorded as authored, not checked against
// the produced patch. ContractSend blocks are rejected — they only
// exist as descendants of a ContractReceive.
func (s *Supervisor) ApplyBlock(block *nom.AccountBlock) (*nom.AccountBlockTransaction, error) {
	if block.BlockType == nom.BlockTypeContractSend {
		return nil, errors.Errorf("can't apply BlockTypeContractSend")
	}
	return s.applyBlock(block, nil)
}

// ApplyMomentum verifies and executes a momentum received from the
// network and returns the transaction pairing it with the
// momentum-store patch it produces. The momentum is not modified: its
// ChangesHash was set by the producing pillar and is checked against
// the recomputed patch by the momentum-transaction verifier.
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

// GenerateFromTemplate completes a partially filled account block and
// executes it: missing fields are defaulted from the chain frontier
// (MomentumAcknowledged, PreviousHash/Height, ChainIdentifier,
// Version, Amount) and, when neither PoW difficulty nor FusedPlasma
// is set, FusedPlasma is set to the block's base plasma. The block is
// then applied and signed twice via signFunc — once on the bare block
// and once after ChangesHash is filled in from the produced patch.
// The pillar's node uses it for embedded-contract update calls.
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

// GenerateAutoReceive builds the ContractReceive block for a send
// addressed to an embedded contract: it executes the contract method
// at the momentum that confirmed the send and returns the resulting
// transaction together with the method's own error (see
// ContractExecution). Embedded blocks are unsigned, so no SignFunc is
// involved. The pillar's contract generator drives this for every
// queued send.
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

// GenerateMomentum executes a momentum template produced by the
// pillar worker and finishes it: after the account-block patches are
// folded in, ChangesHash is set to the hash of the resulting patch,
// the momentum hash is recomputed and signed via signFunc, and the
// complete transaction is verified and returned.
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

// GenerateGenesisMomentum builds the height-1 momentum from the
// genesis template: it folds the genesis account-block patches from
// pool into an empty in-memory momentum store, sets ChangesHash and
// the momentum hash, and skips both the momentum verifier and the
// transaction verifier — the genesis momentum is unsigned and has no
// predecessor to verify against.
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

// applyBlock runs the shared verify-execute-pack pipeline for a
// single account block, recovering VM panics as
// constants.ErrVmRunPanic. signFunc is non-nil only on the
// generation path (GenerateFromTemplate).
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

// setAll fills every defaultable field of a template block: the
// acknowledged momentum, the previous hash/height and the static
// fields (chain identifier, version, amount normalization).
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

// packBlock pairs the executed block with the patch accumulated in
// its context and verifies the resulting transaction. With a non-nil
// signFunc (generation path) it also sets ChangesHash to
// db.PatchHash of the patch, recomputes the block hash and signs it;
// blocks received from outside are left untouched. (The two signing
// passes are equivalent because ChangesHash is not part of the block
// hash.)
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

// packMomentum pairs the executed momentum with the patch accumulated
// in its context. When generating (non-nil signFunc) or building
// genesis, ChangesHash is set to db.PatchHash of the patch and the
// momentum hash recomputed — unlike account blocks, ChangesHash is
// part of the momentum hash — and a signFunc additionally signs the
// hash. Except for genesis, the resulting transaction is verified
// before being returned.
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

// setBlockPlasma defaults FusedPlasma to the block's base plasma
// when the template specifies neither PoW difficulty nor fused
// plasma.
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

// setBlockFields normalizes static fields: chain identifier, version
// (defaulted to 1), a non-nil Amount for sends and a zero
// Amount/TokenStandard for receives.
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

// setBlockHH defaults PreviousHash and Height to continue the
// account's frontier (including unconfirmed blocks) when both are
// unset.
func (s *Supervisor) setBlockHH(block *nom.AccountBlock) error {
	if block.PreviousHash == types.ZeroHash && block.Height == 0 {
		store := s.chain.GetFrontierAccountStore(block.Address)
		frontier := store.Identifier()

		block.PreviousHash = frontier.Hash
		block.Height = frontier.Height + 1
	}
	return nil
}

// setBlockMomentum defaults MomentumAcknowledged: the momentum that
// confirmed the incoming send for an embedded contract block (so
// auto-receives are deterministic across nodes), the frontier
// momentum for everything else.
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
