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
	"github.com/zenon-network/go-zenon/wallet"
)

type MomentumVerifier interface {
	Momentum(momentum *nom.DetailedMomentum) error
	MomentumTransaction(transaction *nom.MomentumTransaction) error
}

type momentumVerifier struct {
	log       log15.Logger
	chain     chain.Chain
	consensus consensus.Consensus
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
		momentum:      detailed.Momentum,
		accountBlocks: detailed.AccountBlocks,
		momentumStore: momentumStore,
	}).all()
}
func (mv *momentumVerifier) MomentumTransaction(transaction *nom.MomentumTransaction) error {
	return (&momentumTransactionVerifier{
		transaction: transaction,
		consensus:   mv.consensus,
	}).all()
}

func NewMomentumVerifier(chain chain.Chain, consensus consensus.Consensus) MomentumVerifier {
	return &momentumVerifier{
		log:       common.VerifierLogger.New("type", "momentum"),
		chain:     chain,
		consensus: consensus,
	}
}

type rawMomentumVerifier struct {
	momentum      *nom.Momentum
	accountBlocks []*nom.AccountBlock
	momentumStore store.Momentum
}

func (rmv *rawMomentumVerifier) all() error {
	if err := rmv.chainIdentifier(); err != nil {
		return err
	}
	if err := rmv.version(); err != nil {
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
	if err := rmv.content(); err != nil {
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
func (rmv *rawMomentumVerifier) version() error {
	if rmv.momentum.Version == 0 {
		return ErrMVersionMissing
	}
	if rmv.momentum.Version != 1 {
		return ErrMVersionInvalid
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
func (rmv *rawMomentumVerifier) content() error {
	if len(rmv.momentum.Content) > chain.MaxAccountBlocksInMomentum {
		return ErrMContentTooBig
	}
	blocksLookup := make(map[types.HashHeight]*nom.AccountBlock)

	// insert all account-blocks in lookup map
	for _, block := range rmv.accountBlocks {
		blocksLookup[block.Identifier()] = block
	}

	// sizes are the same
	if len(blocksLookup) != len(rmv.momentum.Content) {
		return errors.Errorf("momentum content size is different than the size of the prefetched account-blocks")
	}

	// account identifiers make sense when 'applying' blocks; i.e: all pairs of (previous, identifier) match
	// Note: use prefetched blocks to get block.previous
	// Note: at this point, we don't care if account-blocks are valid or not, just that the momentum contains all the
	// blocks and the headers are put in a valid order, since the pillar selects which blocks and in which order
	// are inserted in the momentum
	heads := make(map[types.Address]types.HashHeight)
	for _, header := range rmv.momentum.Content {
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

		block, ok := blocksLookup[header.Identifier()]
		if isBatched(block) {
			continue
		}
		if !ok {
			return errors.Errorf("momentum content header is not present in prefetched account-blocks")
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
