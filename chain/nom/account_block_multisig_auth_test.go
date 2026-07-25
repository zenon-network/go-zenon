package nom

import (
	"crypto/ed25519"
	"math/big"
	"reflect"
	"testing"

	"github.com/ethereum/go-ethereum/rlp"

	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/types"
)

// legacyAccountBlock mirrors the pre-multisig exported RLP layout of
// AccountBlock: every exported field, in order, except MultisigAuth.
type legacyAccountBlock struct {
	Version         uint64
	ChainIdentifier uint64
	BlockType       uint64

	Hash                 types.Hash
	PreviousHash         types.Hash
	Height               uint64
	MomentumAcknowledged types.HashHeight

	Address types.Address

	ToAddress     types.Address
	Amount        *big.Int
	TokenStandard types.ZenonTokenStandard

	FromBlockHash types.Hash

	DescendantBlocks []*AccountBlock

	Data []byte

	FusedPlasma uint64
	Difficulty  uint64
	Nonce       Nonce
	BasePlasma  uint64
	TotalPlasma uint64

	ChangesHash types.Hash

	PublicKey ed25519.PublicKey
	Signature []byte
}

func sampleAccountBlockWithMultisigAuth() *AccountBlock {
	return &AccountBlock{
		Version:         1,
		ChainIdentifier: 1,
		BlockType:       BlockTypeUserSend,
		Height:          1,
		MultisigAuth: &MultisigAuth{
			Signatures: [][]byte{
				{1, 2, 3, 4},
				{5, 6, 7, 8, 9},
			},
		},
	}
}

func TestAccountBlock_MultisigAuth_ProtoRoundTrip(t *testing.T) {
	ab := sampleAccountBlockWithMultisigAuth()

	pb := ab.Proto()
	dab := DeProtoAccountBlock(pb)

	common.ExpectTrue(t, dab.MultisigAuth != nil)
	common.ExpectTrue(t, reflect.DeepEqual(ab.MultisigAuth.Signatures, dab.MultisigAuth.Signatures))
}

func TestAccountBlock_MultisigAuth_ProtoRoundTrip_Nil(t *testing.T) {
	ab := &AccountBlock{Version: 1}

	pb := ab.Proto()
	dab := DeProtoAccountBlock(pb)

	common.ExpectTrue(t, dab.MultisigAuth == nil)
}

func TestAccountBlock_MultisigAuth_JsonRoundTrip(t *testing.T) {
	ab := sampleAccountBlockWithMultisigAuth()
	ab.Amount = common.StringToBigInt("0")

	data, err := ab.MarshalJSON()
	common.FailIfErr(t, err)

	dab := &AccountBlock{}
	common.FailIfErr(t, dab.UnmarshalJSON(data))

	common.ExpectTrue(t, dab.MultisigAuth != nil)
	common.ExpectTrue(t, reflect.DeepEqual(ab.MultisigAuth.Signatures, dab.MultisigAuth.Signatures))
}

// TestAccountBlock_MultisigAuth_RlpRoundTrip_Nil reproduces the fetcher's
// peer-to-peer transaction relay path: SendTransactions RLP-encodes
// []*nom.AccountBlock via p2p.Send. MultisigAuth uses rlp:"optional" so a nil
// pointer is omitted entirely on encode, and decodes back to nil - this is
// what keeps the wire format compatible with pre-multisig binaries.
func TestAccountBlock_MultisigAuth_RlpRoundTrip_Nil(t *testing.T) {
	ab := &AccountBlock{
		Version:         1,
		ChainIdentifier: 1,
		BlockType:       BlockTypeUserSend,
		Height:          1,
		Amount:          common.StringToBigInt("0"),
	}

	data, err := rlp.EncodeToBytes([]*AccountBlock{ab})
	common.FailIfErr(t, err)

	var decoded []*AccountBlock
	common.FailIfErr(t, rlp.DecodeBytes(data, &decoded))

	common.ExpectTrue(t, len(decoded) == 1)
	common.ExpectTrue(t, decoded[0].MultisigAuth == nil)
}

func TestAccountBlock_MultisigAuth_RlpRoundTrip(t *testing.T) {
	ab := sampleAccountBlockWithMultisigAuth()
	ab.Amount = common.StringToBigInt("0")

	data, err := rlp.EncodeToBytes([]*AccountBlock{ab})
	common.FailIfErr(t, err)

	var decoded []*AccountBlock
	common.FailIfErr(t, rlp.DecodeBytes(data, &decoded))

	common.ExpectTrue(t, len(decoded) == 1)
	common.ExpectTrue(t, decoded[0].MultisigAuth != nil)
	common.ExpectTrue(t, reflect.DeepEqual(ab.MultisigAuth.Signatures, decoded[0].MultisigAuth.Signatures))
}

func TestAccountBlock_MultisigAuth_ComputeHashUnaffected(t *testing.T) {
	ab := &AccountBlock{
		Version:         1,
		ChainIdentifier: 1,
		BlockType:       BlockTypeUserSend,
		Height:          1,
		Amount:          common.StringToBigInt("0"),
	}

	hashWithoutAuth := ab.ComputeHash()

	ab.MultisigAuth = &MultisigAuth{
		Signatures: [][]byte{{1, 2, 3}},
	}
	hashWithAuth := ab.ComputeHash()

	common.Expect(t, hashWithAuth, hashWithoutAuth)

	// mutate MultisigAuth after the first hash was computed - hash must stay identical
	ab.MultisigAuth.Signatures = append(ab.MultisigAuth.Signatures, []byte{9, 9, 9})
	hashAfterMutation := ab.ComputeHash()

	common.Expect(t, hashAfterMutation, hashWithoutAuth)
}

func TestAccountBlock_MultisigAuth_CopyIsDeep(t *testing.T) {
	ab := sampleAccountBlockWithMultisigAuth()
	ab.Amount = common.StringToBigInt("0")

	cBlock := ab.Copy()

	common.ExpectTrue(t, cBlock.MultisigAuth != nil)
	common.ExpectTrue(t, reflect.DeepEqual(ab.MultisigAuth.Signatures, cBlock.MultisigAuth.Signatures))

	// mutating the copy must not affect the original
	cBlock.MultisigAuth.Signatures[0][0] = 0xff
	common.ExpectTrue(t, ab.MultisigAuth.Signatures[0][0] != 0xff)

	// mutating the original must not affect the copy
	ab.MultisigAuth.Signatures[1][0] = 0xee
	common.ExpectTrue(t, cBlock.MultisigAuth.Signatures[1][0] != 0xee)
}

// TestAccountBlock_RlpInterop_OldToNew ensures a block produced by a
// pre-multisig binary (no MultisigAuth field on the wire) decodes cleanly
// into the current AccountBlock, with MultisigAuth left nil.
func TestAccountBlock_RlpInterop_OldToNew(t *testing.T) {
	legacy := &legacyAccountBlock{
		Version:         1,
		ChainIdentifier: 1,
		BlockType:       BlockTypeUserSend,
		Height:          1,
		Amount:          common.StringToBigInt("0"),
	}

	data, err := rlp.EncodeToBytes(legacy)
	common.FailIfErr(t, err)

	decoded := &AccountBlock{}
	common.FailIfErr(t, rlp.DecodeBytes(data, decoded))

	common.ExpectTrue(t, decoded.MultisigAuth == nil)
}

// TestAccountBlock_RlpInterop_NewToOld ensures a block produced by the
// current binary with a nil MultisigAuth decodes cleanly into the
// pre-multisig layout (an old binary would see the same bytes).
func TestAccountBlock_RlpInterop_NewToOld(t *testing.T) {
	ab := &AccountBlock{
		Version:         1,
		ChainIdentifier: 1,
		BlockType:       BlockTypeUserSend,
		Height:          1,
		Amount:          common.StringToBigInt("0"),
	}
	common.ExpectTrue(t, ab.MultisigAuth == nil)

	data, err := rlp.EncodeToBytes(ab)
	common.FailIfErr(t, err)

	legacy := &legacyAccountBlock{}
	common.FailIfErr(t, rlp.DecodeBytes(data, legacy))
}
