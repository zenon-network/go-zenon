// Package nom defines the core ledger types of the Network of
// Momentum, a dual-ledger design. The first ledger is the block
// lattice of AccountBlocks: every address has its own chain of
// blocks, where send blocks debit the sender and receive blocks
// accept a specific send by referencing its hash (FromBlockHash).
// Embedded-contract execution extends the same model: a contract
// receive block carries the ContractSend blocks it spawned as
// DescendantBlocks, committed atomically with it.
//
// The second ledger is the momentum chain: Momentums are consensus
// blocks produced by elected pillars at scheduled times, each
// confirming a batch of account blocks by listing their headers in
// Content. Both ledger entries implement db.Commit and are inserted
// together with the db.Patch of state changes they produce, wrapped
// in AccountBlockTransaction and MomentumTransaction respectively.
package nom

import (
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"

	"github.com/pkg/errors"
	"google.golang.org/protobuf/proto"

	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/db"
	"github.com/zenon-network/go-zenon/common/types"
)

// The BlockType constants classify account blocks. User types are
// signed by the address owner; contract types belong to
// embedded-contract addresses and carry no public key or signature.
// BlockTypeGenesisReceive is reserved for the blocks that seed
// account state in the genesis momentum — the verifier rejects it in
// any later block.
const (
	BlockTypeGenesisReceive = 1 // receive

	BlockTypeUserSend    = 2 // send
	BlockTypeUserReceive = 3 // receive

	BlockTypeContractSend    = 4 // send
	BlockTypeContractReceive = 5 // receive
)

// AccountBlockTransaction pairs an account block with the db.Patch
// of account-store changes its insertion produces. It implements
// db.Transaction and is the unit accepted by the account pool.
type AccountBlockTransaction struct {
	Block   *AccountBlock
	Changes db.Patch
}

// GetCommits returns the block's descendant blocks followed by the
// block itself — chain order, since descendants occupy the heights
// immediately below the main block.
func (t *AccountBlockTransaction) GetCommits() []db.Commit {
	list := make([]db.Commit, len(t.Block.DescendantBlocks)+1)
	for index := range t.Block.DescendantBlocks {
		list[index] = t.Block.DescendantBlocks[index]
	}
	list[len(list)-1] = t.Block
	return list
}

// StealChanges returns the transaction's patch and transfers
// ownership, nilling out the internal reference; it must be called at
// most once.
func (t *AccountBlockTransaction) StealChanges() db.Patch {
	changes := t.Changes
	t.Changes = nil
	return changes
}

// Nonce is the 8-byte proof-of-work nonce of an account block, found
// so that the block satisfies its Difficulty (see pow.CheckPoWNonce).
// It is all zeros when the block relies on fused plasma alone.
type Nonce struct {
	Data [8]byte
}

// Copy returns an independent copy of the nonce.
func (n *Nonce) Copy() Nonce {
	dn := Nonce{}
	copy(dn.Data[:], n.Data[:])
	return dn
}

// MarshalText encodes the nonce as 16 lowercase hex characters. The
// error is always nil.
func (n *Nonce) MarshalText() ([]byte, error) {
	return []byte(hex.EncodeToString(n.Data[:])), nil
}

// UnmarshalText decodes a 16-character hex string into the nonce,
// reversing MarshalText. It returns an error for invalid hex or any
// length other than 8 bytes, leaving the nonce unchanged.
func (n *Nonce) UnmarshalText(input []byte) error {
	bytes, err := hex.DecodeString(string(input))
	if err != nil {
		return fmt.Errorf("failed to decode nonce:%v", err)
	}
	if len(bytes) != 8 {
		return errors.Errorf("invalid nonce length")
	}
	copy(n.Data[:], bytes)
	return nil
}

// Serialize returns the raw 8 nonce bytes; the returned slice aliases
// the nonce's array.
func (n *Nonce) Serialize() []byte {
	return n.Data[:]
}

// DeSerializeNonce reverses Nonce.Serialize, copying the input. It
// panics if bytes is not exactly 8 bytes long.
func DeSerializeNonce(bytes []byte) Nonce {
	if len(bytes) != 8 {
		panic("invalid nonce length")
	}
	var n Nonce
	copy(n.Data[:], bytes)
	return n
}

// AccountBlock is one entry of an account chain in the block
// lattice. Send blocks (see IsSendBlock) populate the send fields;
// receive blocks populate FromBlockHash; a contract receive
// additionally carries the ContractSend blocks it spawned as
// DescendantBlocks. Hash covers the fields enumerated in ComputeHash
// and is what the owner signs; custom JSON marshalling renders Amount
// as a base-10 string and Nonce as hex (see AccountBlockMarshal).
type AccountBlock struct {
	// Version is the block format version; the verifier accepts only 1.
	Version uint64 `json:"version"`
	// ChainIdentifier is the network identifier from genesis; blocks
	// whose identifier does not match the node's chain are rejected.
	ChainIdentifier uint64 `json:"chainIdentifier"`
	// BlockType is one of the BlockType constants.
	BlockType uint64 `json:"blockType"`

	// Hash is the block's identity, expected to equal ComputeHash.
	Hash types.Hash `json:"hash"`
	// PreviousHash is the hash of the previous block in this account's
	// chain; it is zero for the first block (Height 1).
	PreviousHash types.Hash `json:"previousHash"`
	// Height is the 1-based position in the account chain.
	Height uint64 `json:"height"`
	// MomentumAcknowledged anchors the block to a momentum the author
	// has observed, protecting it from being replayed on a fork.
	MomentumAcknowledged types.HashHeight `json:"momentumAcknowledged"`

	// Address is the account chain this block belongs to.
	Address types.Address `json:"address"`

	// Send information
	ToAddress     types.Address            `json:"toAddress"`
	Amount        *big.Int                 `json:"amount"`
	TokenStandard types.ZenonTokenStandard `json:"tokenStandard"`

	// Receive information; FromBlockHash is the hash of the send block
	// being received.
	FromBlockHash types.Hash `json:"fromBlockHash"`

	// DescendantBlocks holds the ContractSend blocks spawned by
	// executing a ContractReceive block, at the heights immediately
	// below it; the verifier requires it to be empty for every other
	// block type. The hash of DescendantBlocks is included in Hash.
	DescendantBlocks []*AccountBlock `json:"descendantBlocks"`

	// Data is the method call or payload carried by the block; the
	// SHA3-256 hash of Data, not Data itself, is included in Hash.
	Data []byte `json:"data"`

	// FusedPlasma is the plasma drawn from QSR fused to the account.
	FusedPlasma uint64 `json:"fusedPlasma"`
	// Difficulty is the proof-of-work difficulty target met by Nonce;
	// 0 when no PoW was performed.
	Difficulty uint64 `json:"difficulty"`
	// Nonce is the proof-of-work solution for Difficulty.
	Nonce Nonce `json:"nonce"`
	// BasePlasma is the smallest TotalPlasma the block requires; it is
	// not included in Hash.
	BasePlasma uint64 `json:"basePlasma"`
	// TotalPlasma is FusedPlasma plus PoW plasma, serialized as
	// "usedPlasma"; it is not included in Hash.
	TotalPlasma uint64 `json:"usedPlasma"`

	// ChangesHash is db.PatchHash of the state patch produced by
	// applying the block; it is not included in Hash.
	ChangesHash types.Hash `json:"changesHash"`

	producer *types.Address // not included in hash, for caching purpose only
	// PublicKey is the Ed25519 public key of the address owner and
	// Signature its signature of Hash. Neither is included in Hash, and
	// both must be empty on embedded-contract blocks.
	PublicKey ed25519.PublicKey `json:"publicKey"`
	Signature []byte            `json:"signature"`
}

// Identifier returns the block's hash-height, which also names the
// account-store version it produces (see db.Commit).
func (ab *AccountBlock) Identifier() types.HashHeight {
	return types.HashHeight{
		Hash:   ab.Hash,
		Height: ab.Height,
	}
}

// Previous returns the identifier of the block that precedes this
// block's whole transaction in the account chain: when descendant
// blocks are present they occupy the heights immediately below, so
// the predecessor is the one before the first descendant.
func (ab *AccountBlock) Previous() types.HashHeight {
	if len(ab.DescendantBlocks) != 0 {
		return ab.DescendantBlocks[0].Previous()
	}
	return types.HashHeight{
		Hash:   ab.PreviousHash,
		Height: ab.Height - 1,
	}
}

// Header returns the block's account header (address plus
// hash-height), the form in which momentum Content references it.
func (ab *AccountBlock) Header() types.AccountHeader {
	return types.AccountHeader{
		Address: ab.Address,
		HashHeight: types.HashHeight{
			Hash:   ab.Hash,
			Height: ab.Height,
		},
	}
}

// Copy returns a mostly deep copy of the block: Amount, Data, Nonce,
// Signature and DescendantBlocks (recursively) get fresh storage. The
// PublicKey backing array is shared with the original, as is the
// cached producer pointer.
func (ab *AccountBlock) Copy() *AccountBlock {
	cBlock := *ab

	if ab.Amount != nil {
		cBlock.Amount = new(big.Int).Set(ab.Amount)
	}

	cBlock.Data = make([]byte, len(ab.Data))
	copy(cBlock.Data, ab.Data)

	cBlock.Nonce = ab.Nonce.Copy()

	if len(ab.Signature) > 0 {
		cBlock.Signature = make([]byte, len(ab.Signature))
		copy(cBlock.Signature, ab.Signature)
	}

	cBlock.DescendantBlocks = make([]*AccountBlock, 0, len(ab.DescendantBlocks))
	for _, dBlock := range ab.DescendantBlocks {
		cBlock.DescendantBlocks = append(cBlock.DescendantBlocks, dBlock.Copy())
	}
	return &cBlock
}

// DescendantBlocksHash returns the SHA3-256 hash of the concatenated
// hashes of the descendant blocks, in order; this digest is what
// ComputeHash commits to. With no descendants it is the hash of the
// empty string, not the zero hash.
func (ab *AccountBlock) DescendantBlocksHash() types.Hash {
	source := make([]byte, 0, types.HashSize*len(ab.DescendantBlocks))
	for _, dBlock := range ab.DescendantBlocks {
		source = append(source, dBlock.Hash.Bytes()...)
	}
	return types.NewHash(source)
}

// ComputeHash recomputes the block hash: a SHA3-256 digest over the
// following fields, in order, with uint64s encoded big-endian:
//   - Version
//   - ChainIdentifier
//   - BlockType
//   - PreviousHash
//   - Height
//   - MomentumAcknowledged
//   - Address
//   - ToAddress
//   - Amount (32-byte big-endian)
//   - TokenStandard
//   - FromBlockHash
//   - DescendantBlocksHash
//   - the SHA3-256 hash of Data
//   - FusedPlasma
//   - Difficulty
//   - Nonce
//
// BasePlasma, TotalPlasma, ChangesHash, PublicKey and Signature are
// not covered. It neither reads nor writes ab.Hash.
func (ab *AccountBlock) ComputeHash() types.Hash {
	return types.NewHash(common.JoinBytes(
		common.Uint64ToBytes(ab.Version),
		common.Uint64ToBytes(ab.ChainIdentifier),
		common.Uint64ToBytes(ab.BlockType),
		ab.PreviousHash.Bytes(),
		common.Uint64ToBytes(ab.Height),
		ab.MomentumAcknowledged.Bytes(),
		ab.Address.Bytes(),
		ab.ToAddress.Bytes(),
		common.BigIntToBytes(ab.Amount),
		ab.TokenStandard.Bytes(),
		ab.FromBlockHash.Bytes(),
		ab.DescendantBlocksHash().Bytes(),
		types.NewHash(ab.Data).Bytes(),
		common.Uint64ToBytes(ab.FusedPlasma),
		common.Uint64ToBytes(ab.Difficulty),
		ab.Nonce.Data[:],
	))
}

// Producer returns the address derived from the block's PublicKey
// (types.PubKeyToAddress), caching the result on first call; for a
// valid user block it equals Address. Do not call it before PublicKey
// is set, or the empty-key address will be cached.
func (ab *AccountBlock) Producer() types.Address {
	if ab.producer == nil {
		producer := types.PubKeyToAddress(ab.PublicKey)
		ab.producer = &producer
	}

	return *ab.producer
}

// IsSendBlock reports whether the block's type is a send type; see
// the package-level IsSendBlock.
func (ab *AccountBlock) IsSendBlock() bool {
	return IsSendBlock(ab.BlockType)
}

// IsReceiveBlock reports whether the block's type is a receive type;
// see the package-level IsReceiveBlock.
func (ab *AccountBlock) IsReceiveBlock() bool {
	return IsReceiveBlock(ab.BlockType)
}

// IsSendBlock reports whether blockType is BlockTypeUserSend or
// BlockTypeContractSend.
func IsSendBlock(blockType uint64) bool {
	return blockType == BlockTypeUserSend || blockType == BlockTypeContractSend
}

// IsReceiveBlock reports whether blockType is BlockTypeUserReceive,
// BlockTypeContractReceive or BlockTypeGenesisReceive.
func IsReceiveBlock(blockType uint64) bool {
	return blockType == BlockTypeUserReceive || blockType == BlockTypeContractReceive || blockType == BlockTypeGenesisReceive
}

// Proto converts the block (including descendants, recursively) to
// its protobuf message. Amount is encoded via common.BigIntToBytes
// (32-byte big-endian; nil encodes the same as zero), so nil does not
// round-trip: DeProtoAccountBlock decodes it as 0.
func (ab *AccountBlock) Proto() *AccountBlockProto {
	pb := &AccountBlockProto{
		Version:              ab.Version,
		ChainIdentifier:      ab.ChainIdentifier,
		BlockType:            ab.BlockType,
		Hash:                 ab.Hash.Proto(),
		PreviousHash:         ab.PreviousHash.Proto(),
		Height:               ab.Height,
		MomentumAcknowledged: ab.MomentumAcknowledged.Proto(),
		Address:              ab.Address.Proto(),

		ToAddress:     ab.ToAddress.Proto(),
		Amount:        common.BigIntToBytes(ab.Amount),
		TokenStandard: ab.TokenStandard.Bytes(),

		FromBlockHash: ab.FromBlockHash.Proto(),

		DescendantBlocks: nil,

		Data: ab.Data,

		FusedPlasma: ab.FusedPlasma,
		Difficulty:  ab.Difficulty,
		Nonce:       ab.Nonce.Serialize(),
		BasePlasma:  ab.BasePlasma,
		TotalPlasma: ab.TotalPlasma,

		ChangesHash: ab.ChangesHash.Proto(),

		PublicKey: ab.PublicKey,
		Signature: ab.Signature,
	}

	pb.DescendantBlocks = make([]*AccountBlockProto, 0, len(ab.DescendantBlocks))
	for _, dBlock := range ab.DescendantBlocks {
		pb.DescendantBlocks = append(pb.DescendantBlocks, dBlock.Proto())
	}

	return pb
}

// DeProtoAccountBlock is the inverse of AccountBlock.Proto. It panics
// on malformed payloads: hash and address fields of the wrong size
// (via types.DeProtoHash and types.DeProtoAddress), an invalid token
// standard (types.BytesToZTSPanic) or a nonce that is not 8 bytes
// (DeSerializeNonce).
func DeProtoAccountBlock(pb *AccountBlockProto) *AccountBlock {
	ab := &AccountBlock{
		Version:              pb.Version,
		ChainIdentifier:      pb.ChainIdentifier,
		BlockType:            pb.BlockType,
		Hash:                 *types.DeProtoHash(pb.Hash),
		PreviousHash:         *types.DeProtoHash(pb.PreviousHash),
		Height:               pb.Height,
		MomentumAcknowledged: *types.DeProtoHashHeight(pb.MomentumAcknowledged),
		Address:              *types.DeProtoAddress(pb.Address),
		ToAddress:            *types.DeProtoAddress(pb.ToAddress),
		Amount:               common.BytesToBigInt(pb.Amount),
		TokenStandard:        types.BytesToZTSPanic(pb.TokenStandard),
		FromBlockHash:        *types.DeProtoHash(pb.FromBlockHash),
		DescendantBlocks:     make([]*AccountBlock, len(pb.DescendantBlocks)),
		Data:                 pb.Data,
		FusedPlasma:          pb.FusedPlasma,
		Difficulty:           pb.Difficulty,
		Nonce:                DeSerializeNonce(pb.Nonce),
		BasePlasma:           pb.BasePlasma,
		TotalPlasma:          pb.TotalPlasma,

		ChangesHash: *types.DeProtoHash(pb.ChangesHash),

		PublicKey: pb.PublicKey,
		Signature: pb.Signature,
	}

	for index, dBlockProto := range pb.DescendantBlocks {
		ab.DescendantBlocks[index] = DeProtoAccountBlock(dBlockProto)
	}
	return ab
}

// Serialize encodes the block as protobuf bytes — the binary form
// stored by the ledger (see db.Commit). DeserializeAccountBlock is
// the inverse.
func (ab *AccountBlock) Serialize() ([]byte, error) {
	return proto.Marshal(ab.Proto())
}

// DeserializeAccountBlock decodes protobuf bytes produced by
// Serialize. It returns an error for invalid protobuf but panics, as
// DeProtoAccountBlock does, on malformed field payloads.
func DeserializeAccountBlock(data []byte) (*AccountBlock, error) {
	pb := &AccountBlockProto{}
	if err := proto.Unmarshal(data, pb); err != nil {
		return nil, err
	}
	return DeProtoAccountBlock(pb), nil
}

// AccountBlockMarshal is the JSON wire representation of
// AccountBlock: identical except that Amount is a base-10 string and
// Nonce a hex string, so amounts round-trip without precision loss.
// TotalPlasma is serialized as "usedPlasma" (matching the tag on
// AccountBlock itself), and DescendantBlocks remain full AccountBlock
// values whose own custom marshalling applies recursively.
type AccountBlockMarshal struct {
	Version         uint64 `json:"version"`
	ChainIdentifier uint64 `json:"chainIdentifier"`
	BlockType       uint64 `json:"blockType"`

	Hash                 types.Hash       `json:"hash"`
	PreviousHash         types.Hash       `json:"previousHash"`
	Height               uint64           `json:"height"`
	MomentumAcknowledged types.HashHeight `json:"momentumAcknowledged"`

	Address types.Address `json:"address"`

	// Send information
	ToAddress     types.Address            `json:"toAddress"`
	Amount        string                   `json:"amount"`
	TokenStandard types.ZenonTokenStandard `json:"tokenStandard"`

	// Receive information
	FromBlockHash types.Hash `json:"fromBlockHash"`

	// Batch information
	DescendantBlocks []*AccountBlock `json:"descendantBlocks"` // hash of DescendantBlocks is included in hash

	Data []byte `json:"data"` // hash of Data is included in hash

	FusedPlasma uint64 `json:"fusedPlasma"`
	Difficulty  uint64 `json:"difficulty"`
	Nonce       string `json:"nonce"`
	BasePlasma  uint64 `json:"basePlasma"` // not included in hash, the smallest value of TotalPlasma required for block
	TotalPlasma uint64 `json:"usedPlasma"` // not included in hash, TotalPlasma = FusedPlasma + PowPlasma

	ChangesHash types.Hash `json:"changesHash"` // not included in hash

	producer  *types.Address    // not included in hash, for caching purpose only
	PublicKey ed25519.PublicKey `json:"publicKey"` // not included in hash
	Signature []byte            `json:"signature"` // not included in hash
}

// ToNomMarshalJson converts the block to its JSON wire
// representation, rendering Amount as a base-10 string and Nonce as
// hex. The descendant block pointers are shared, not copied. It
// panics if Amount is nil.
func (ab *AccountBlock) ToNomMarshalJson() *AccountBlockMarshal {
	aux := &AccountBlockMarshal{
		Version:              ab.Version,
		ChainIdentifier:      ab.ChainIdentifier,
		BlockType:            ab.BlockType,
		Hash:                 ab.Hash,
		PreviousHash:         ab.PreviousHash,
		Height:               ab.Height,
		MomentumAcknowledged: ab.MomentumAcknowledged,
		Address:              ab.Address,
		ToAddress:            ab.ToAddress,
		Amount:               ab.Amount.String(),
		TokenStandard:        ab.TokenStandard,
		FromBlockHash:        ab.FromBlockHash,
		Data:                 ab.Data,
		FusedPlasma:          ab.FusedPlasma,
		Difficulty:           ab.Difficulty,
		Nonce:                hex.EncodeToString(ab.Nonce.Data[:]),
		BasePlasma:           ab.BasePlasma,
		TotalPlasma:          ab.TotalPlasma,
		ChangesHash:          ab.ChangesHash,
		PublicKey:            ab.PublicKey,
		Signature:            ab.Signature,
	}

	aux.DescendantBlocks = make([]*AccountBlock, 0, len(ab.DescendantBlocks))
	for _, dBlock := range ab.DescendantBlocks {
		aux.DescendantBlocks = append(aux.DescendantBlocks, dBlock)
	}
	return aux
}

// FromNomMarshalJson converts the wire representation back to an
// AccountBlock. An Amount string that is not a valid base-10 integer
// silently decodes to 0 (common.StringToBigInt), and an invalid Nonce
// string is silently ignored, leaving the nonce zero — unlike
// AccountBlock.UnmarshalJSON, which reports a nonce error.
func (ab *AccountBlockMarshal) FromNomMarshalJson() *AccountBlock {
	aux := &AccountBlock{
		Version:              ab.Version,
		ChainIdentifier:      ab.ChainIdentifier,
		BlockType:            ab.BlockType,
		Hash:                 ab.Hash,
		PreviousHash:         ab.PreviousHash,
		Height:               ab.Height,
		MomentumAcknowledged: ab.MomentumAcknowledged,
		Address:              ab.Address,
		ToAddress:            ab.ToAddress,
		Amount:               common.StringToBigInt(ab.Amount),
		TokenStandard:        ab.TokenStandard,
		FromBlockHash:        ab.FromBlockHash,
		Data:                 ab.Data,
		FusedPlasma:          ab.FusedPlasma,
		Difficulty:           ab.Difficulty,
		BasePlasma:           ab.BasePlasma,
		TotalPlasma:          ab.TotalPlasma,
		ChangesHash:          ab.ChangesHash,
		PublicKey:            ab.PublicKey,
		Signature:            ab.Signature,
	}
	// ignore the error, it will just not set the nonce
	aux.Nonce.UnmarshalText([]byte(ab.Nonce))

	aux.DescendantBlocks = make([]*AccountBlock, 0, len(ab.DescendantBlocks))
	for _, dBlock := range ab.DescendantBlocks {
		aux.DescendantBlocks = append(aux.DescendantBlocks, dBlock)
	}
	return aux
}

// MarshalJSON encodes the block via its AccountBlockMarshal wire
// form, so Amount appears as a base-10 JSON string and Nonce as hex.
func (ab *AccountBlock) MarshalJSON() ([]byte, error) {
	return json.Marshal(ab.ToNomMarshalJson())
}

// UnmarshalJSON decodes the AccountBlockMarshal wire form produced by
// MarshalJSON. An invalid Amount string silently decodes to 0, while
// an invalid Nonce string is reported as an error.
func (ab *AccountBlock) UnmarshalJSON(data []byte) error {
	aux := new(AccountBlockMarshal)
	if err := json.Unmarshal(data, aux); err != nil {
		return err
	}

	ab.Version = aux.Version
	ab.ChainIdentifier = aux.ChainIdentifier
	ab.BlockType = aux.BlockType
	ab.Hash = aux.Hash
	ab.PreviousHash = aux.PreviousHash
	ab.Height = aux.Height
	ab.MomentumAcknowledged = aux.MomentumAcknowledged
	ab.Address = aux.Address
	ab.ToAddress = aux.ToAddress
	ab.Amount = common.StringToBigInt(aux.Amount)
	ab.TokenStandard = aux.TokenStandard
	ab.FromBlockHash = aux.FromBlockHash
	ab.DescendantBlocks = make([]*AccountBlock, len(aux.DescendantBlocks))
	ab.Data = aux.Data
	ab.FusedPlasma = aux.FusedPlasma
	ab.Difficulty = aux.Difficulty
	if err := ab.Nonce.UnmarshalText([]byte(aux.Nonce)); err != nil {
		return err
	}
	ab.BasePlasma = aux.BasePlasma
	ab.TotalPlasma = aux.TotalPlasma
	ab.ChangesHash = aux.ChangesHash
	ab.PublicKey = aux.PublicKey
	ab.Signature = aux.Signature
	for index, dBlock := range aux.DescendantBlocks {
		ab.DescendantBlocks[index] = dBlock
	}

	return nil
}
