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
	"github.com/zenon-network/go-zenon/vm/embedded/definition"
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

type AccountBlockVerifier interface {
	AccountBlock(block *nom.AccountBlock) error
	// AccountBlockTransaction verifies a transaction that is being admitted
	// to the account pool or generated locally. Every rule applies.
	AccountBlockTransaction(transaction *nom.AccountBlockTransaction) error
	// AccountBlockTransactionInMomentum verifies a transaction as part of
	// the momentum with the given height being inserted. Rules that were
	// introduced at a height apply only when the momentum is at or above
	// it, so that every node judges a momentum the same way.
	AccountBlockTransactionInMomentum(transaction *nom.AccountBlockTransaction, momentumHeight uint64) error
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
	momentumStore := av.chain.GetMomentumStore(block.MomentumAcknowledged)
	if momentumStore == nil {
		return nil, nil, ErrABMAMissing
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
	}).all()
}
func (av *accountVerifier) AccountBlockTransaction(transaction *nom.AccountBlockTransaction) error {
	return av.verifyTransaction(transaction, true)
}
func (av *accountVerifier) AccountBlockTransactionInMomentum(transaction *nom.AccountBlockTransaction, momentumHeight uint64) error {
	return av.verifyTransaction(transaction, momentumHeight >= definition.CommunitySporkAddressRenewalStartHeight)
}
func (av *accountVerifier) verifyTransaction(transaction *nom.AccountBlockTransaction, contractReceiveRules bool) error {
	if transaction.Block.BlockType == nom.BlockTypeContractSend {
		return ErrABTypeInvalidExternal
	}

	accountStore, momentumStore, err := av.getContext(transaction.Block)
	if err != nil {
		return err
	}

	return (&accountBlockTransactionVerifier{
		transaction:          transaction,
		accountStore:         accountStore,
		momentumStore:        momentumStore,
		contractReceiveRules: contractReceiveRules,
	}).all()
}

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
	if abv.block.ChainIdentifier != abv.momentumStore.ChainIdentifier() {
		return fmt.Errorf("%w - expected %v but received %v", ErrMChainIdentifierMismatch, abv.momentumStore.ChainIdentifier(), abv.block.ChainIdentifier)
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
	momentum, err := abv.momentumStore.GetFrontierMomentum()
	if err != nil {
		return InternalError(err)
	}
	if momentum.Identifier() != abv.block.MomentumAcknowledged {
		return InternalError(errors.Errorf("impossible scenario. verifier momentum-store exists but frontier is different. Expected MomentumAcknowledged %v but got %v from MomentumStore", abv.block.MomentumAcknowledged, momentum.Identifier()))
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

		height, err := abv.momentumStore.GetBlockConfirmationHeight(abv.block.FromBlockHash)
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
	sendBlock, err := abv.momentumStore.GetAccountBlockByHash(abv.block.FromBlockHash)
	if err != nil {
		return InternalError(err)
	} else if sendBlock == nil {
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

	nextInLine := abv.accountStore.SequencerFront(abv.momentumStore.GetAccountMailbox(abv.block.Address))
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

type accountBlockTransactionVerifier struct {
	transaction   *nom.AccountBlockTransaction
	accountStore  store.Account
	momentumStore store.Momentum
	// contractReceiveRules says whether the contract-receive rules apply
	// to this verification. They always apply when a block is admitted to
	// the pool through publication or gossip, or generated locally: that
	// is not a consensus decision, and it keeps a block that fails them out
	// of the uncommitted state an upgraded node could later reuse or
	// produce from. When a block is verified as part of a momentum being
	// inserted they apply from the community spork renewal height, so that
	// every node judges a momentum the same way; the caller decides from
	// the momentum's height. A momentum below that height is committed as
	// every node committed it before the rules existed, blocks included.
	contractReceiveRules bool
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
	if err := abvt.plasmaFields(); err != nil {
		return err
	}
	if err := abvt.descendantBlocks(); err != nil {
		return err
	}

	return nil
}

func (abvt *accountBlockTransactionVerifier) contractReceiveRulesActive() bool {
	return abvt.contractReceiveRules
}
func (abvt *accountBlockTransactionVerifier) plasmaFields() error {
	if !abvt.contractReceiveRulesActive() {
		return nil
	}
	block := abvt.transaction.Block
	// BasePlasma and TotalPlasma are serialized but excluded from ComputeHash,
	// so the hash check above does not bind them. A contract receive is produced
	// by the VM, which never sets either field; a peer could raise them under an
	// honest hash, and a node that stored the altered copy would then compute a
	// divergent momentum changes-hash. Require both to be zero. The parent's
	// ChangesHash is legitimately non-zero and is already revalidated by the VM.
	if isContractReceive(block) && (block.BasePlasma != 0 || block.TotalPlasma != 0) {
		return ErrABPlasmaMustBeZero
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
		}).all(); err != nil {
			return DescendantVerifyError(err)
		}
		if !abvt.contractReceiveRulesActive() {
			continue
		}
		// Bind the descendant's content and credentials to its hash, the same
		// transaction-level checks the top-level block gets. The parent only
		// commits to each descendant's hash, not its content, so without this a
		// peer can change a descendant's amount, recipient, or data (or pad its
		// signature/public key) while keeping its hash.
		child := &accountBlockTransactionVerifier{
			transaction:          &nom.AccountBlockTransaction{Block: dBlock},
			accountStore:         abvt.accountStore,
			momentumStore:        abvt.momentumStore,
			contractReceiveRules: abvt.contractReceiveRules,
		}
		if err := child.hash(); err != nil {
			return DescendantVerifyError(err)
		}
		if err := child.signature(); err != nil {
			return DescendantVerifyError(err)
		}
		if err := child.producer(); err != nil {
			return DescendantVerifyError(err)
		}
		// ComputeHash excludes ChangesHash, BasePlasma and TotalPlasma, so the
		// hash check above does not bind them. A descendant is always a contract
		// send the VM generates with these zero, so require that; otherwise a
		// peer could alter these serialized fields under an honest hash and
		// poison the stored patch.
		if dBlock.ChangesHash != types.ZeroHash || dBlock.BasePlasma != 0 || dBlock.TotalPlasma != 0 {
			return DescendantVerifyError(errors.Errorf("descendant carries a non-canonical changes-hash or plasma field"))
		}
		if err := child.descendantBlocks(); err != nil {
			return DescendantVerifyError(err)
		}
	}
	return nil
}
