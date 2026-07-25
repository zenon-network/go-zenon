package verifier

import (
	"fmt"

	"github.com/pkg/errors"

	"github.com/zenon-network/go-zenon/chain"
	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/chain/store"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/consensus"
	"github.com/zenon-network/go-zenon/pow"
	"github.com/zenon-network/go-zenon/vm/constants"
	"github.com/zenon-network/go-zenon/vm/embedded/definition"
	"github.com/zenon-network/go-zenon/wallet"
)

var (
	ReceiverMismatchEnforcementHeight uint64 = 10109240 // Targeting 2025-04-16 12:00:00 UTC
)

func isBatched(block *nom.AccountBlock) bool {
	return block.IsSendBlock() && types.IsEmbeddedAddress(block.Address)
}
func isContractReceive(block *nom.AccountBlock) bool {
	return block.IsReceiveBlock() && types.IsEmbeddedAddress(block.Address)
}

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
	if types.IsEmbeddedAddress(block.Address) || types.IsMultisigAddress(block.Address) {
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
	if transaction.Block.BlockType == nom.BlockTypeContractSend {
		return ErrABTypeInvalidExternal
	}

	accountStore, momentumStore, err := av.getContext(transaction.Block)
	if err != nil {
		return err
	}

	isMultisigActive := false
	if types.IsMultisigAddress(transaction.Block.Address) {
		isMultisigActive, err = momentumStore.IsSporkActive(types.MultisigSpork)
		if err != nil {
			return err
		}
	}

	return (&accountBlockTransactionVerifier{
		transaction:      transaction,
		accountStore:     accountStore,
		momentumStore:    momentumStore,
		frontierStore:    av.chain.GetFrontierMomentumStore(),
		isMultisigActive: isMultisigActive,
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

	// Recency floor (best-effort, node-local pre-filter): a multisig block may not acknowledge a
	// momentum more than MultisigMaxMaLag below this node's current frontier. This rejects most
	// stale blocks early and cheaply, but it is NOT consensus-authoritative -- the frontier is
	// node-local and can differ across sync states, so it serves only as a backlog/hygiene bound.
	// content() (rawMomentumVerifier.content(), verifier/momentum.go) enforces no MA-recency floor
	// at all -- it verifies live signatures/policy against the block's actual inclusion height
	// instead.
	// Outside the `previous != ZeroHashHeight` guard above so it also gates a multisig account's
	// very first block (height 1, no previous block). Additive arithmetic only, to avoid uint
	// underflow at low/genesis frontier height.
	if types.IsMultisigAddress(abv.block.Address) {
		frontierH := abv.frontierStore.Identifier().Height
		if definition.IsMultisigMAStale(abv.block.MomentumAcknowledged.Height, frontierH) {
			return ErrABMATooOld
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
		// Use the momentum ledger's true frontier height when comparing
		if abv.frontierStore.Identifier().Height >= ReceiverMismatchEnforcementHeight {
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
	frontierStore store.Momentum

	// isMultisigActive is set per-call (not shared/cached) by AccountBlockTransaction, true iff
	// types.MultisigSpork is active at the block's MomentumAcknowledged. Only meaningful for
	// multisig blocks.
	isMultisigActive bool
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
	if types.IsMultisigAddress(block.Address) {
		if !abvt.isMultisigActive {
			return constants.ErrMultisigNotActivated
		}
		if len(block.PublicKey) != 0 {
			return ErrABPublicKeyMustBeZero
		}
		if len(block.Signature) != 0 {
			return ErrABSignatureMustBeZero
		}
		auth := block.MultisigAuth
		if auth == nil {
			return ErrABMultisigAuthMissing
		}

		// Read the active policy from the registry at the local frontier's current height. Uses the
		// single canonical definition-package codec/Promote() — same as the write path. This is a
		// best-effort pre-filter, not the authoritative gate: the block's actual momentum-inclusion
		// height may differ from this node's frontier at admission time, so the authoritative check
		// is the live re-check in content() against the block's real inclusion height.
		rec, err := definition.GetMultisigRecord(
			abvt.frontierStore.GetAccountStore(types.MultisigContract).Storage(), block.Address)
		common.DealWithErr(err)
		if rec == nil {
			return constants.ErrMultisigNoPolicy
		}
		policy := definition.ActivePolicyAtHeight(rec, abvt.frontierStore.Identifier().Height)

		if len(auth.Signatures) != int(policy.Threshold) {
			return constants.ErrMultisigThresholdNotMet
		}

		if !definition.VerifyThresholdSignatures(policy, block.Hash.Bytes(), auth.Signatures) {
			return ErrABSignatureInvalid
		}
		return nil
	}
	if types.IsEmbeddedAddress(block.Address) {
		if len(block.PublicKey) != 0 {
			return ErrABPublicKeyMustBeZero
		}
		if len(block.Signature) != 0 {
			return ErrABSignatureMustBeZero
		}
		if block.MultisigAuth != nil {
			return ErrABMultisigAuthMustBeZero
		}
		return nil
	}

	if len(block.Signature) == 0 {
		return ErrABSignatureMissing
	}
	if len(block.PublicKey) == 0 {
		return ErrABPublicKeyMissing
	}
	if block.MultisigAuth != nil {
		return ErrABMultisigAuthMustBeZero
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

	if types.IsMultisigAddress(block.Address) {
		// No-op: authority is the in-state policy checked in signature(); the address↔creation
		// binding was enforced once, at creation, by CreateMultisig. The address does not commit
		// to the policy (unlike phase-1's fixed policy-derived address), so there is no pubkey to
		// rederive here.
		return nil
	}
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
		}).all(); err != nil {
			return DescendantVerifyError(err)
		}
	}
	return nil
}
