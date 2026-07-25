package wallet

import (
	"crypto/ed25519"
	"math/big"

	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/vm/constants"
	"github.com/zenon-network/go-zenon/vm/embedded/definition"
)

// DeriveMultisigAddress derives the address of a mutable multisig account from its creation
// event (creatorPubKey, nonce). Thin wrapper around the single canonical derivation,
// types.MultisigCreationToAddress -- not reimplemented here.
func DeriveMultisigAddress(creatorPubKey ed25519.PublicKey, nonce uint64) types.Address {
	return types.MultisigCreationToAddress(creatorPubKey, nonce)
}

// signersToBytes re-types a signer list as the raw [][]byte the ABI codec expects.
func signersToBytes(signers []ed25519.PublicKey) [][]byte {
	raw := make([][]byte, len(signers))
	for i, s := range signers {
		raw[i] = s
	}
	return raw
}

// NewCreateMultisigTemplate builds an unsigned CreateMultisig send-block template. creator is
// the funded single-sig account submitting the call; it is signed normally, like any
// other embedded-contract send -- no multisig verification is involved at this step. The
// resulting multisig account's address can be computed offline ahead of time with
// DeriveMultisigAddress(creatorPubKey, nonce). The call burns constants.MultisigCreationBurnAmount
// of ZNN; creator must be funded accordingly.
func NewCreateMultisigTemplate(creator types.Address, nonce uint64, threshold uint8, signers []ed25519.PublicKey) *nom.AccountBlock {
	return &nom.AccountBlock{
		Address:       creator,
		ToAddress:     types.MultisigContract,
		BlockType:     nom.BlockTypeUserSend,
		Amount:        new(big.Int).Set(constants.MultisigCreationBurnAmount),
		TokenStandard: types.ZnnTokenStandard,
		Data: definition.ABIMultisig.PackMethodPanic(definition.CreateMultisigMethodName,
			nonce, threshold, signersToBytes(signers)),
	}
}

// NewChangePolicyTemplate builds an unsigned ChangePolicy send-block template. ChangePolicy is
// called BY the multisig account itself (Address = multisigAddr) TO the
// registry contract (types.MultisigContract), so the resulting template still needs threshold
// signatures collected under multisigAddr's CURRENTLY effective policy (SignMultisigBlock /
// AssembleMultisigAuth below) before it can be submitted.
func NewChangePolicyTemplate(multisigAddr types.Address, threshold uint8, signers []ed25519.PublicKey, lock bool) *nom.AccountBlock {
	return &nom.AccountBlock{
		Address:       multisigAddr,
		ToAddress:     types.MultisigContract,
		BlockType:     nom.BlockTypeUserSend,
		Amount:        big.NewInt(0),
		TokenStandard: types.ZeroTokenStandard,
		Data: definition.ABIMultisig.PackMethodPanic(definition.ChangePolicyMethodName,
			threshold, signersToBytes(signers), lock),
	}
}

// SignMultisigBlock produces one ed25519 signature from signer over a frozen block's hash
// (block.Hash must already be computed -- i.e. the block has been PoW'd/finalised). The verifier
// trial-matches signatures against the active policy's signers in any order, so callers may
// collect signatures from multiple signers independently, in any order, without coordinating who
// signs first.
func SignMultisigBlock(block *nom.AccountBlock, signer *KeyPair) []byte {
	return signer.Sign(block.Hash.Bytes())
}

// AssembleMultisigAuth attaches a collected list of signatures to a frozen block and clears the
// singular auth fields, which the verifier requires to be empty for multisig blocks
// (ErrABPublicKeyMustBeZero/ErrABSignatureMustBeZero). The verifier requires EXACTLY the active
// policy's threshold number of signatures, not more -- the caller must collect exactly that many
// before calling this.
func AssembleMultisigAuth(block *nom.AccountBlock, signatures [][]byte) {
	block.PublicKey = nil
	block.Signature = nil
	block.MultisigAuth = &nom.MultisigAuth{Signatures: signatures}
}
