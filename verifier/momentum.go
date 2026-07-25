package verifier

import (
	"fmt"
	"time"

	"github.com/inconshreveable/log15"
	"github.com/pkg/errors"

	"github.com/zenon-network/go-zenon/chain"
	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/chain/store"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/db"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/consensus"
	"github.com/zenon-network/go-zenon/dp"
	"github.com/zenon-network/go-zenon/vm/embedded/definition"
	"github.com/zenon-network/go-zenon/wallet"
)

type MomentumVerifier interface {
	Momentum(momentum *nom.DetailedMomentum) error
	MomentumTransaction(transaction *nom.MomentumTransaction) error
}

type momentumVerifier struct {
	log                 log15.Logger
	chain               chain.Chain
	consensus           consensus.Consensus
	canonicalBasePlasma CanonicalBasePlasmaFunc
}

func (mv *momentumVerifier) getContext(momentum *nom.Momentum) (store.Momentum, error) {
	if momentum.Height == 1 {
		return nil, ErrMNotGenesis
	}
	if momentum.PreviousHash.IsZero() {
		return nil, ErrMPrevHashMissing
	}

	momentumStore := mv.chain.GetMomentumStore(momentum.Previous())
	if momentumStore == nil {
		return nil, ErrMPreviousMissing
	}
	return momentumStore, nil
}
func (mv *momentumVerifier) Momentum(detailed *nom.DetailedMomentum) error {
	momentumStore, err := mv.getContext(detailed.Momentum)
	if err != nil {
		return err
	}

	return (&rawMomentumVerifier{
		momentum:            detailed.Momentum,
		accountBlocks:       detailed.AccountBlocks,
		momentumStore:       momentumStore,
		chain:               mv.chain,
		canonicalBasePlasma: mv.canonicalBasePlasma,
	}).all()
}
func (mv *momentumVerifier) MomentumTransaction(transaction *nom.MomentumTransaction) error {
	return (&momentumTransactionVerifier{
		transaction: transaction,
		consensus:   mv.consensus,
	}).all()
}

func NewMomentumVerifier(chain chain.Chain, consensus consensus.Consensus, canonicalBasePlasma CanonicalBasePlasmaFunc) MomentumVerifier {
	return &momentumVerifier{
		log:                 common.VerifierLogger.New("type", "momentum"),
		chain:               chain,
		consensus:           consensus,
		canonicalBasePlasma: canonicalBasePlasma,
	}
}

type rawMomentumVerifier struct {
	momentum            *nom.Momentum
	accountBlocks       []*nom.AccountBlock
	momentumStore       store.Momentum
	chain               chain.Chain
	canonicalBasePlasma CanonicalBasePlasmaFunc
}

func (rmv *rawMomentumVerifier) all() error {
	isDynamicPlasmaActive, err := rmv.momentumStore.IsSporkActive(types.DynamicPlasmaSpork)
	if err != nil {
		return err
	}
	if err := rmv.chainIdentifier(); err != nil {
		return err
	}
	if err := rmv.version(isDynamicPlasmaActive); err != nil {
		return err
	}
	if err := rmv.timestamp(); err != nil {
		return err
	}
	if err := rmv.previous(); err != nil {
		return err
	}
	if err := rmv.data(); err != nil {
		return err
	}
	if err := rmv.nextFusionPrice(); err != nil {
		return err
	}
	if err := rmv.nextWorkPrice(); err != nil {
		return err
	}
	if err := rmv.content(isDynamicPlasmaActive); err != nil {
		return err
	}
	return nil
}
func (rmv *rawMomentumVerifier) chainIdentifier() error {
	if rmv.momentum.ChainIdentifier == 0 {
		return ErrABChainIdentifierMissing
	}
	if rmv.momentum.ChainIdentifier != rmv.momentumStore.ChainIdentifier() {
		return fmt.Errorf("%w - expected %v but received %v", ErrABChainIdentifierMismatch, rmv.momentumStore.ChainIdentifier(), rmv.momentum.ChainIdentifier)
	}
	return nil
}
func (rmv *rawMomentumVerifier) version(isDynamicPlasmaActive bool) error {
	if rmv.momentum.Version == 0 {
		return ErrMVersionMissing
	}
	if isDynamicPlasmaActive {
		if rmv.momentum.Version != 2 {
			return ErrMVersionInvalid
		}
	} else {
		if rmv.momentum.Version != 1 {
			return ErrMVersionInvalid
		}
	}
	return nil
}
func (rmv *rawMomentumVerifier) timestamp() error {
	if rmv.momentum.Timestamp.Unix() == 0 {
		return ErrMTimestampMissing
	}
	if rmv.momentum.Timestamp.After(time.Now().Add(time.Second * 10)) {
		return ErrMTimestampInTheFuture
	}

	previous, err := rmv.momentumStore.GetFrontierMomentum()
	if err != nil {
		return InternalError(err)
	}
	if previous.TimestampUnix >= rmv.momentum.TimestampUnix {
		return ErrMTimestampNotIncreasing
	}
	return nil
}
func (rmv *rawMomentumVerifier) previous() error {
	// for consistency, check again
	if rmv.momentum.Height == 1 {
		return ErrMNotGenesis
	}
	if rmv.momentum.PreviousHash.IsZero() {
		return ErrMPrevHashMissing
	}

	previous, err := rmv.momentumStore.GetFrontierMomentum()
	if err != nil {
		return InternalError(err)
	}
	if rmv.momentum.Previous() != previous.Identifier() {
		return ErrMPreviousMissing
	}
	return nil
}
func (rmv *rawMomentumVerifier) data() error {
	if len(rmv.momentum.Data) != 0 {
		return ErrMDataMustBeZero
	}
	return nil
}
func (rmv *rawMomentumVerifier) nextFusionPrice() error {
	if rmv.momentum.Version == 1 && rmv.momentum.NextFusionPrice != 0 {
		return ErrMDataMustBeZero
	} else if rmv.momentum.Version >= 2 && rmv.momentum.NextFusionPrice < dp.MinResourcePrice {
		return ErrMMinimumValueInvalid
	}
	return nil
}
func (rmv *rawMomentumVerifier) nextWorkPrice() error {
	if rmv.momentum.Version == 1 && rmv.momentum.NextWorkPrice != 0 {
		return ErrMDataMustBeZero
	} else if rmv.momentum.Version >= 2 && rmv.momentum.NextWorkPrice < dp.MinResourcePrice {
		return ErrMMinimumValueInvalid
	}
	return nil
}
func (rmv *rawMomentumVerifier) content(isDynamicPlasmaActive bool) error {
	// Multisig authorisation is verified LIVE against the policy active at this momentum's inclusion
	// height. momentumStore is the committed state as of the previous momentum, so the read is
	// deterministic and identical on every node. This is the single authoritative authorisation gate
	// for multisig account blocks.
	var registry db.DB
	policyCache := make(map[types.Address]*definition.MultisigPolicy)
	for _, block := range rmv.accountBlocks {
		if types.IsMultisigAddress(block.Address) {
			if registry == nil {
				registry = rmv.momentumStore.GetAccountStore(types.MultisigContract).Storage()
			}
			var sigs [][]byte
			if block.MultisigAuth != nil {
				sigs = block.MultisigAuth.Signatures
			}
			policy, ok := policyCache[block.Address]
			if !ok {
				rec, err := definition.GetMultisigRecord(registry, block.Address)
				common.DealWithErr(err)
				policy = definition.ActivePolicyAtHeight(rec, rmv.momentum.Height)
				policyCache[block.Address] = policy
			}
			if !definition.VerifyThresholdSignatures(policy, block.Hash.Bytes(), sigs) {
				return errors.Errorf("multisig block authorization does not satisfy policy active at inclusion height")
			}
		}
	}

	blocksLookup := make(map[types.HashHeight]*nom.AccountBlock, len(rmv.accountBlocks))

	// insert all account-blocks in lookup map, rejecting duplicates so a repeated block can't be
	// counted twice for dynamic-plasma price accounting while collapsing back to one entry here
	for _, block := range rmv.accountBlocks {
		if block == nil {
			return errors.Errorf("prefetched account-block is nil")
		}
		identifier := block.Identifier()
		if _, ok := blocksLookup[identifier]; ok {
			return errors.Errorf("duplicate prefetched account-block: %v", identifier)
		}
		blocksLookup[identifier] = block
	}

	// sizes are the same
	if len(blocksLookup) != len(rmv.momentum.Content) {
		return errors.Errorf("momentum content size is different than the size of the prefetched account-blocks")
	}

	// resolve each content header to its account-block once, in content order, consuming the lookup
	// entry as it's matched so a duplicate content header can't resolve to the same block twice
	orderedBlocks := make([]*nom.AccountBlock, len(rmv.momentum.Content))
	for index, header := range rmv.momentum.Content {
		identifier := header.Identifier()
		block, ok := blocksLookup[identifier]
		if !ok || block.Address != header.Address {
			return errors.Errorf("content header has no matching account-block")
		}
		delete(blocksLookup, identifier)
		orderedBlocks[index] = block
	}
	if len(blocksLookup) != 0 {
		return errors.Errorf("prefetched account-blocks contain entries not referenced by momentum content")
	}

	if isDynamicPlasmaActive {
		previousMomentum, err := rmv.momentumStore.GetMomentumByHash(rmv.momentum.PreviousHash)
		if err != nil {
			return err
		}

		config, err := rmv.momentumStore.GetPlasmaVariables()
		if err != nil {
			return err
		}

		plasma := dp.NewDynamicPlasma(previousMomentum, config)
		contractBlockCount := uint64(0)
		basePlasma := types.BasePlasma{Fusion: 0, Pow: 0}
		for _, block := range orderedBlocks {
			if types.IsEmbeddedAddress(block.Address) {
				contractBlockCount++
				if contractBlockCount > plasma.MaxContractBlocksInMomentum() {
					return errors.Errorf("exceeded maximum allowed contract account blocks in momentum")
				}
			} else {
				canonical, err := rmv.canonicalBasePlasma(rmv.chain, block)
				if err != nil {
					return err
				}
				block.BasePlasma = canonical
				if !plasma.ValidPrice(block) {
					return errors.Errorf("block price is too small")
				}
				basePlasma.Add(plasma.ComputeBasePlasma(block))
				if basePlasma.Total() > config.MaxBasePlasmaInMomentum {
					return ErrMContentTooBig
				}
			}
		}

		nextFusionPrice := plasma.NextFusionPrice(basePlasma.Fusion)
		if nextFusionPrice != rmv.momentum.NextFusionPrice {
			return errors.Errorf("mismatch in momentum fusion price: have %d, want %d", nextFusionPrice, rmv.momentum.NextFusionPrice)
		}

		nextWorkPrice := plasma.NextWorkPrice(basePlasma.Pow)
		if nextWorkPrice != rmv.momentum.NextWorkPrice {
			return errors.Errorf("mismatch in momentum work price: have %d, want %d", nextWorkPrice, rmv.momentum.NextWorkPrice)
		}
	} else {
		if len(rmv.momentum.Content) > chain.MaxAccountBlocksInMomentum {
			return ErrMContentTooBig
		}
	}

	// account identifiers make sense when 'applying' blocks; i.e: all pairs of (previous, identifier) match
	// Note: use prefetched blocks to get block.previous
	// Note: at this point, we don't care if account-blocks are valid or not, just that the momentum contains all the
	// blocks and the headers are put in a valid order, since the pillar selects which blocks and in which order
	// are inserted in the momentum
	heads := make(map[types.Address]types.HashHeight)
	for index, header := range rmv.momentum.Content {
		previous, ok := heads[header.Address]
		if !ok {
			pastFrontier, err := rmv.momentumStore.GetFrontierAccountBlock(header.Address)
			if err != nil {
				return InternalError(err)
			}
			if pastFrontier == nil {
				previous = types.ZeroHashHeight
			} else {
				previous = pastFrontier.Identifier()
			}
		}

		block := orderedBlocks[index]
		if isBatched(block) {
			continue
		}

		if block.Previous() != previous {
			return errors.Errorf("gap in previous Expected %v but got %v", previous, block.Previous())
		}

		heads[header.Address] = block.Identifier()
	}

	return nil
}

type momentumTransactionVerifier struct {
	transaction *nom.MomentumTransaction
	consensus   consensus.Consensus
}

func (mv *momentumTransactionVerifier) all() error {
	if err := mv.changesHash(mv.transaction); err != nil {
		return err
	}
	if err := mv.hash(mv.transaction); err != nil {
		return err
	}
	if err := mv.signature(mv.transaction); err != nil {
		return err
	}
	if err := mv.producer(mv.transaction); err != nil {
		return err
	}
	return nil
}
func (mv *momentumTransactionVerifier) signature(transaction *nom.MomentumTransaction) error {
	momentum := transaction.Momentum

	if len(momentum.Signature) == 0 {
		return ErrMSignatureMissing
	}
	if len(momentum.PublicKey) == 0 {
		return ErrMPublicKeyMissing
	}
	isVerified, err := wallet.VerifySignature(momentum.PublicKey, momentum.Hash.Bytes(), momentum.Signature)
	if err != nil {
		return InternalError(err)
	}
	if !isVerified {
		return ErrMSignatureInvalid
	}
	return nil
}
func (mv *momentumTransactionVerifier) changesHash(transaction *nom.MomentumTransaction) error {
	computedHash := db.PatchHash(transaction.Changes)
	if computedHash != transaction.Momentum.ChangesHash {
		log.Info("changes-hash differ", "expected", computedHash, "got-instead", transaction.Momentum.ChangesHash)
		return ErrMChangesHashInvalid
	}
	return nil
}
func (mv *momentumTransactionVerifier) hash(transaction *nom.MomentumTransaction) error {
	momentum := transaction.Momentum
	computedHash := momentum.ComputeHash()
	if computedHash != momentum.Hash {
		return ErrMHashInvalid
	}
	return nil
}
func (mv *momentumTransactionVerifier) producer(transaction *nom.MomentumTransaction) error {
	// MomentumTransaction producer
	result, err := mv.consensus.VerifyMomentumProducer(transaction.Momentum)
	if err != nil {
		return InternalError(err)
	} else if !result {
		return ErrMProducerInvalid
	}
	return nil
}
