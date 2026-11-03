package verifier

import (
	"fmt"

	"github.com/pkg/errors"

	"github.com/zenon-network/go-zenon/chain"
	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/chain/store"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/consensus"
	"github.com/zenon-network/go-zenon/pow"
	"github.com/zenon-network/go-zenon/wallet"
)

var (
	// ReceiverMismatchEnforcementHeight is the mainnet momentum height from
	// which a receive block must name the account the referenced send was
	// addressed to. Mainnet carries earlier blocks that do not, so the rule
	// is relaxed below this height on mainnet only; every other chain
	// enforces it from genesis. See receiverMismatchTolerated.
	ReceiverMismatchEnforcementHeight uint64 = 10109240 // Targeting 2025-04-16 12:00:00 UTC
)

// mainnetChainIdentifier is the chain identifier of the embedded alphanet
// genesis, the only chain whose history predates the receiver rule. The
// relaxation below is keyed on this value alone, so a custom network must
// not reuse identifier 1 in its genesis, or it inherits mainnet's historical
// exception until its own frontier passes ReceiverMismatchEnforcementHeight.
const mainnetChainIdentifier uint64 = 1

// receiverMismatchTolerated reports whether a receive block whose address is
// not the referenced send's recipient is accepted on the chain identified by
// chainIdentifier when the momentum frontier is at frontierHeight. Only
// mainnet below ReceiverMismatchEnforcementHeight tolerates it, so that its
// history can be replayed; a chain with any other identifier rejects it from
// its first momentum.
func receiverMismatchTolerated(chainIdentifier, frontierHeight uint64) bool {
	return chainIdentifier == mainnetChainIdentifier && frontierHeight < ReceiverMismatchEnforcementHeight
}

func isBatched(block *nom.AccountBlock) bool {
	return block.IsSendBlock() && types.IsEmbeddedAddress(block.Address)
}
func isContractReceive(block *nom.AccountBlock) bool {
	return block.IsReceiveBlock() && types.IsEmbeddedAddress(block.Address)
}

// AccountBlockVerifier validates account-blocks. AccountBlock checks a
// block's intrinsic fields and its position in the ledger; the heavier
// AccountBlockTransaction additionally validates the hash, signature and
// producer once the block has been embedded in a transaction.
type AccountBlockVerifier interface {
	AccountBlock(block *nom.AccountBlock) error
	AccountBlockTransaction(transaction *nom.AccountBlockTransaction) error
}

type accountVerifier struct {
	chain     chain.Chain
	consensus consensus.Consensus
}

func (av *accountVerifier) getContext(block *nom.AccountBlock) (store.Account, store.Momentum, error) {
	if block.Height == 0 {
		return nil, nil, ErrABMHeightMissing
	}
	if block.Height == 1 && !block.PreviousHash.IsZero() {
		return nil, nil, ErrABPrevHashMustBeZero
	}
	if block.Height != 1 && block.PreviousHash.IsZero() {
		return nil, nil, ErrABPrevHashMissing
	}

	if block.MomentumAcknowledged.IsZero() {
		return nil, nil, ErrABMAMustNotBeZero
	}

	var momentumStore store.Momentum
	if types.IsEmbeddedAddress(block.Address) {
		momentumStore = av.chain.GetMomentumStore(block.MomentumAcknowledged)
		if momentumStore == nil {
			return nil, nil, ErrABMAMissing
		}
	}

	accountStore := av.chain.GetAccountStore(block.Address, block.Previous())

	if accountStore == nil {
		// try to give a better error in case we are not able to give a better error
		globalStore := av.chain.GetFrontierMomentumStore().GetAccountStore(block.Address)
		globalFrontier, err := globalStore.Frontier()
		if err != nil {
			return nil, nil, InternalError(err)
		}
		if globalFrontier == nil {
			// the account has no chain at all, so no previous block
			return nil, nil, ErrABPreviousMissing
		}

		if globalFrontier.Height > block.Height-1 {
			block, err := globalStore.ByHash(block.PreviousHash)
			if err != nil {
				return nil, nil, InternalError(err)
			}
			if block != nil {
				return nil, nil, ErrABPrevHasCementedOnTop
			}
			return nil, nil, ErrABPrevHeightExists
		} else {
			return nil, nil, ErrABPreviousMissing
		}
	}

	return accountStore, momentumStore, nil
}
func (av *accountVerifier) AccountBlock(block *nom.AccountBlock) error {
	if block.BlockType == nom.BlockTypeContractSend {
		return ErrABTypeInvalidExternal
	}

	accountStore, momentumStore, err := av.getContext(block)
	if err != nil {
		return err
	}

	return (&accountBlockVerifier{
		block:         block,
		accountStore:  accountStore,
		momentumStore: momentumStore,
		frontierStore: av.chain.GetFrontierMomentumStore(),
		chain:         av.chain,
	}).all()
}
func (av *accountVerifier) AccountBlockTransaction(transaction *nom.AccountBlockTransaction) error {
	if transaction.Block.BlockType == nom.BlockTypeContractSend {
		return ErrABTypeInvalidExternal
	}

	accountStore, momentumStore, err := av.getContext(transaction.Block)
	if err != nil {
		return err
	}

	return (&accountBlockTransactionVerifier{
		transaction:   transaction,
		accountStore:  accountStore,
		momentumStore: momentumStore,
		frontierStore: av.chain.GetFrontierMomentumStore(),
		chain:         av.chain,
	}).all()
}

// NewAccountBlockVerifier returns an AccountBlockVerifier that
// resolves the account and momentum stores it needs from chain. The
// consensus argument is retained for symmetry with the momentum
// verifier and to support future account-level consensus checks.
func NewAccountBlockVerifier(chain chain.Chain, consensus consensus.Consensus) AccountBlockVerifier {
	return &accountVerifier{
		chain:     chain,
		consensus: consensus,
	}
}

type accountBlockVerifier struct {
	block         *nom.AccountBlock
	accountStore  store.Account
	momentumStore store.Momentum
	frontierStore store.Momentum
	chain         chain.Chain
}

func (abv *accountBlockVerifier) all() error {
	if err := abv.version(); err != nil {
		return err
	}
	if err := abv.chainIdentifier(); err != nil {
		return err
	}
	if err := abv.blockType(); err != nil {
		return err
	}
	if err := abv.amounts(); err != nil {
		return err
	}
	if err := abv.pow(); err != nil {
		return err
	}
	if err := abv.previous(); err != nil {
		return err
	}
	if err := abv.momentumAcknowledged(); err != nil {
		return err
	}
	if err := abv.fromHash(); err != nil {
		return err
	}
	if err := abv.sequencer(); err != nil {
		return err
	}
	if err := abv.receiveData(); err != nil {
		return err
	}
	return nil
}
func (abv *accountBlockVerifier) version() error {
	if abv.block.Version == 0 {
		return ErrABVersionMissing
	}
	if abv.block.Version != 1 {
		return ErrABVersionInvalid
	}
	return nil
}
func (abv *accountBlockVerifier) chainIdentifier() error {
	if abv.block.ChainIdentifier == 0 {
		return ErrMChainIdentifierMissing
	}
	if abv.block.ChainIdentifier != abv.frontierStore.ChainIdentifier() {
		return fmt.Errorf("%w - expected %v but received %v", ErrMChainIdentifierMismatch, abv.frontierStore.ChainIdentifier(), abv.block.ChainIdentifier)
	}
	return nil
}
func (abv *accountBlockVerifier) blockType() error {
	if abv.block.BlockType == 0 {
		return ErrABTypeMissing
	}
	if abv.block.BlockType == nom.BlockTypeGenesisReceive {
		return ErrABTypeMustNotBeGenesis
	}
	if abv.block.IsSendBlock() || abv.block.IsReceiveBlock() {
	} else {
		return ErrABTypeUnsupported
	}

	if types.IsEmbeddedAddress(abv.block.Address) {
		if abv.block.BlockType == nom.BlockTypeContractReceive || abv.block.BlockType == nom.BlockTypeContractSend {
		} else {
			return ErrABTypeMustBeContract
		}
	} else {
		if abv.block.BlockType == nom.BlockTypeUserReceive || abv.block.BlockType == nom.BlockTypeUserSend {
		} else {
			return ErrABTypeMustBeUser
		}
	}
	return nil
}
func (abv *accountBlockVerifier) amounts() error {
	if abv.block.IsSendBlock() {
		if abv.block.Amount.Sign() == -1 {
			return ErrABAmountNegative
		}
		if abv.block.Amount.BitLen() > 255 {
			return ErrABAmountTooBig
		}
		if abv.block.Amount.Sign() == +1 && abv.block.TokenStandard == types.ZeroTokenStandard {
			return ErrABZtsMissing
		}
		// ToAddress can be null

		if !abv.block.FromBlockHash.IsZero() {
			return ErrABFromBlockHashMustBeZero
		}
	} else {
		if abv.block.Amount != nil && abv.block.Amount.Sign() != 0 {
			return ErrABAmountMustBeZero
		}
		if abv.block.TokenStandard != types.ZeroTokenStandard {
			return ErrABZtsMustBeZero
		}
		if abv.block.ToAddress != types.ZeroAddress {
			return ErrABToAddressMustBeZero
		}

		if abv.block.FromBlockHash.IsZero() {
			return ErrABFromBlockHashMissing
		}
	}
	return nil
}
func (abv *accountBlockVerifier) pow() error {
	if abv.block.Difficulty != 0 {
		if types.IsEmbeddedAddress(abv.block.Address) {
			return ErrABPoWInvalid
		}
		if !pow.CheckPoWNonce(abv.block) {
			return ErrABPoWInvalid
		}
	}
	return nil
}
func (abv *accountBlockVerifier) previous() error {
	// for consistency, check again
	if abv.block.Height == 0 {
		return ErrABMHeightMissing
	}
	if abv.block.Height == 1 && !abv.block.PreviousHash.IsZero() {
		return ErrABPrevHashMustBeZero
	}
	if abv.block.Height != 1 && abv.block.PreviousHash.IsZero() {
		return ErrABPrevHashMissing
	}

	// start blocks don't expect previous
	if abv.block.Height == 1 {
		return nil
	}

	// don't check previous on contract
	if types.IsEmbeddedAddress(abv.block.Address) {
		return nil
	}

	block, err := abv.accountStore.Frontier()
	if err != nil {
		return InternalError(err)
	}
	if block == nil {
		return InternalError(errors.Errorf("empty frontier account-block"))
	}
	if block.Identifier() != abv.block.Previous() {
		return ErrABPreviousMissing
	}
	return nil
}
func (abv *accountBlockVerifier) momentumAcknowledged() error {
	if abv.momentumStore == nil {
		// When the momentumStore is not used, explicitly verify that the block's acknowledgement momentum exists
		m, err := abv.frontierStore.GetMomentumByHeight(abv.block.MomentumAcknowledged.Height)
		if err != nil {
			return InternalError(err)
		}
		if m == nil || m.Hash != abv.block.MomentumAcknowledged.Hash {
			return ErrABMAMissing
		}
	} else {
		momentum, err := abv.momentumStore.GetFrontierMomentum()
		if err != nil {
			return InternalError(err)
		}
		identifier := momentum.Identifier()
		if identifier != abv.block.MomentumAcknowledged {
			return InternalError(errors.Errorf("impossible scenario. momentum store exists but frontier is different. Expected MomentumAcknowledged %v but got %v from momentum store", abv.block.MomentumAcknowledged, identifier))
		}
	}

	// all checks are done by the parent
	if isBatched(abv.block) {
		return nil
	}

	// MomentumAcknowledged is the same as all the ones in dBlocks
	if isContractReceive(abv.block) {
		for _, dBlock := range abv.block.DescendantBlocks {
			if dBlock.MomentumAcknowledged != abv.block.MomentumAcknowledged {
				return ErrABMAMustBeTheSame
			}
		}

		height, err := abv.frontierStore.GetBlockConfirmationHeight(abv.block.FromBlockHash)
		if err != nil {
			return InternalError(err)
		}
		if height != abv.block.MomentumAcknowledged.Height {
			return ErrABMAInvalidForAutoGenerated
		}
		return nil
	}

	// current MomentumAcknowledged is bigger than previous
	if previous := abv.block.Previous(); previous != types.ZeroHashHeight {
		previousBlock, err := abv.accountStore.ByHeight(previous.Height)
		if err != nil {
			return InternalError(err)
		}
		if previousBlock.MomentumAcknowledged.Height > abv.block.MomentumAcknowledged.Height {
			return ErrABMAGap
		}
	}

	return nil
}
func (abv *accountBlockVerifier) fromHash() error {
	if abv.block.IsSendBlock() {
		return nil
	}

	// check that from-hash is a valid hash
	sendBlock, err := abv.frontierStore.GetAccountBlockByHash(abv.block.FromBlockHash)
	if err != nil {
		return InternalError(err)
	} else if sendBlock == nil {
		return ErrABFromBlockMissing
	}

	// the referenced send must have been confirmed at or before the
	// momentum this block acknowledges
	confirmationHeight, err := abv.frontierStore.GetBlockConfirmationHeight(abv.block.FromBlockHash)
	if err != nil {
		return InternalError(err)
	}
	if confirmationHeight > abv.block.MomentumAcknowledged.Height {
		return ErrABFromBlockMissing
	}

	if abv.block.Address != sendBlock.ToAddress {
		// Decided against the momentum ledger's true frontier and the chain
		// it belongs to; see receiverMismatchTolerated.
		if !receiverMismatchTolerated(abv.frontierStore.ChainIdentifier(), abv.frontierStore.Identifier().Height) {
			return ErrABFromBlockReceiverMismatch
		}
	}

	// check if abv.block was already received
	status := abv.accountStore.IsReceived(abv.block.FromBlockHash)
	if status {
		return ErrABFromBlockAlreadyReceived
	}

	return nil
}
func (abv *accountBlockVerifier) sequencer() error {
	if types.IsEmbeddedAddress(abv.block.Address) && abv.block.IsReceiveBlock() {
	} else {
		return nil
	}

	nextInLine, err := abv.accountStore.SequencerFront(abv.momentumStore.GetAccountMailbox(abv.block.Address))
	if err != nil {
		return InternalError(err)
	}
	if nextInLine == nil {
		return ErrABSequencerNothing
	}

	sendBlock, err := abv.momentumStore.GetAccountBlockByHash(abv.block.FromBlockHash)
	if err != nil {
		return InternalError(err)
	}
	if sendBlock.Header() != *nextInLine {
		return ErrABSequencerNotNext
	}

	return nil
}

// isDynamicPlasmaActiveAtMA reports whether the Dynamic Plasma spork is
// enforced at the block's acknowledged momentum height. It prefers the
// momentum store already attached to the verifier; when that is nil (user
// blocks), it reads from the cache store at the MA height, which answers
// spork queries from the momentum's patch set without replaying rollbacks.
func (abv *accountBlockVerifier) isDynamicPlasmaActiveAtMA() (bool, error) {
	if abv.momentumStore != nil {
		return abv.momentumStore.IsSporkActive(types.DynamicPlasmaSpork)
	}
	if abv.chain == nil {
		return false, ErrABMAMissing
	}
	cacheStore := abv.chain.GetCacheStore(abv.block.MomentumAcknowledged)
	if cacheStore == nil {
		return false, InternalError(fmt.Errorf("no cache store at momentum %s", abv.block.MomentumAcknowledged.Hash))
	}
	return cacheStore.IsSporkActive(types.DynamicPlasmaSpork)
}

// receiveData rejects user receive blocks that carry non-empty Data once the
// Dynamic Plasma spork is enforced at the block's acknowledged momentum height.
// Before the spork, receive blocks always get flat plasma regardless of Data
// size, which lets anyone commit large data at minimum cost.
func (abv *accountBlockVerifier) receiveData() error {
	if !abv.block.IsReceiveBlock() {
		return nil
	}
	if types.IsEmbeddedAddress(abv.block.Address) {
		return nil
	}
	if len(abv.block.Data) == 0 {
		return nil
	}
	active, err := abv.isDynamicPlasmaActiveAtMA()
	if err != nil {
		return err
	}
	if active {
		return ErrABDataMustBeEmpty
	}
	return nil
}

type accountBlockTransactionVerifier struct {
	transaction   *nom.AccountBlockTransaction
	accountStore  store.Account
	momentumStore store.Momentum
	frontierStore store.Momentum
	chain         chain.Chain
}

func (abvt *accountBlockTransactionVerifier) all() error {
	if err := abvt.hash(); err != nil {
		return err
	}
	if err := abvt.signature(); err != nil {
		return err
	}
	if err := abvt.producer(); err != nil {
		return err
	}
	if err := abvt.descendantBlocks(); err != nil {
		return err
	}

	return nil
}
func (abvt *accountBlockTransactionVerifier) signature() error {
	block := abvt.transaction.Block
	if types.IsEmbeddedAddress(block.Address) {
		if len(block.PublicKey) != 0 {
			return ErrABPublicKeyMustBeZero
		}
		if len(block.Signature) != 0 {
			return ErrABSignatureMustBeZero
		}
		return nil
	}

	if len(block.Signature) == 0 {
		return ErrABSignatureMissing
	}
	if len(block.PublicKey) == 0 {
		return ErrABPublicKeyMissing
	}
	isVerified, err := wallet.VerifySignature(block.PublicKey, block.Hash.Bytes(), block.Signature)
	if err != nil {
		return ErrABSignatureInvalid
	}
	if !isVerified {
		return ErrABSignatureInvalid
	}
	return nil
}
func (abvt *accountBlockTransactionVerifier) hash() error {
	block := abvt.transaction.Block

	// check expected hash matches
	computedHash := block.ComputeHash()
	if block.Hash.IsZero() {
		return ErrABHashMissing
	}
	if computedHash != block.Hash {
		return ErrABHashInvalid
	}
	return nil
}
func (abvt *accountBlockTransactionVerifier) producer() error {
	block := abvt.transaction.Block

	if types.IsEmbeddedAddress(block.Address) {
		return nil
	}
	if types.PubKeyToAddress(block.PublicKey) != block.Address {
		return ErrABPublicKeyWrongAddress
	}

	return nil
}
func (abvt *accountBlockTransactionVerifier) descendantBlocks() error {
	block := abvt.transaction.Block
	if !isContractReceive(block) && len(block.DescendantBlocks) > 0 {
		return ErrABDescendantMustBeZero
	}
	for _, dBlock := range block.DescendantBlocks {
		if err := (&accountBlockVerifier{
			block:         dBlock,
			accountStore:  abvt.accountStore,
			momentumStore: abvt.momentumStore,
			frontierStore: abvt.frontierStore,
			chain:         abvt.chain,
		}).all(); err != nil {
			return DescendantVerifyError(err)
		}
	}
	return nil
}
