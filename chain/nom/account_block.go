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
	"bytes"
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
// A block that relies on fused plasma alone normally leaves it all
// zeros; the nonce is only checked when Difficulty is non-zero.
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
// returns an error if bytes is not exactly 8 bytes long.
func DeSerializeNonce(bytes []byte) (Nonce, error) {
	if len(bytes) != 8 {
		return Nonce{}, fmt.Errorf("invalid nonce length %d (expect 8)", len(bytes))
	}
	var n Nonce
	copy(n.Data[:], bytes)
	return n, nil
}

// AccountBlock is one entry of an account chain in the block
// lattice. Send blocks (see IsSendBlock) populate the send fields;
// receive blocks populate FromBlockHash; a contract receive
// additionally carries the ContractSend blocks it spawned as
// DescendantBlocks. Hash covers the fields enumerated in ComputeHash
// and is what the owner signs; custom JSON marshalling renders Amount
// as a base-10 string and Nonce as hex (see AccountBlockMarshal).
// FusedPlasma, Difficulty and Nonce are the block's plasma payment
// and are hashed; BasePlasma (the price the block must meet) and
// TotalPlasma (FusedPlasma plus the PoW plasma) are derived at
// verification and are not.
type AccountBlock struct {
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
	Amount        *big.Int                 `json:"amount"`
	TokenStandard types.ZenonTokenStandard `json:"tokenStandard"`

	// Receive information
	FromBlockHash types.Hash `json:"fromBlockHash"`

	// Batch information
	DescendantBlocks []*AccountBlock `json:"descendantBlocks"` // hash of DescendantBlocks is included in hash

	Data []byte `json:"data"` // hash of Data is included in hash

	FusedPlasma uint64 `json:"fusedPlasma"`
	Difficulty  uint64 `json:"difficulty"`
	Nonce       Nonce  `json:"nonce"`
	BasePlasma  uint64 `json:"basePlasma"` // not included in hash, the smallest value of TotalPlasma required for block
	TotalPlasma uint64 `json:"usedPlasma"` // not included in hash, TotalPlasma = FusedPlasma + PowPlasma

	ChangesHash types.Hash `json:"changesHash"` // not included in hash

	producer  *types.Address    // not included in hash, for caching purpose only
	PublicKey ed25519.PublicKey `json:"publicKey"` // not included in hash
	Signature []byte            `json:"signature"` // not included in hash
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

// Copy returns a deep copy of the block: Amount, Data, Nonce,
// PublicKey, Signature and DescendantBlocks (recursively) get fresh
// storage; only the cached producer pointer is shared with the
// original.
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
	if len(ab.PublicKey) > 0 {
		cBlock.PublicKey = make([]byte, len(ab.PublicKey))
		copy(cBlock.PublicKey, ab.PublicKey)
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

// DeProtoAccountBlock is the inverse of AccountBlock.Proto. A nil
// proto or a malformed payload — a hash, address or token standard of
// the wrong size, a nonce that is not 8 bytes, or a bad descendant —
// returns an error naming the field; nothing in the decode path
// panics, so untrusted wire data is safe to pass in.
func DeProtoAccountBlock(pb *AccountBlockProto) (*AccountBlock, error) {
	if pb == nil {
		return nil, fmt.Errorf("invalid DeProto - nil account block proto")
	}
	hash, err := types.DeProtoHash(pb.Hash)
	if err != nil {
		return nil, fmt.Errorf("invalid hash: %w", err)
	}
	prevHash, err := types.DeProtoHash(pb.PreviousHash)
	if err != nil {
		return nil, fmt.Errorf("invalid previous hash: %w", err)
	}
	momentumAck, err := types.DeProtoHashHeight(pb.MomentumAcknowledged)
	if err != nil {
		return nil, fmt.Errorf("invalid momentum acknowledged: %w", err)
	}
	fromBlockHash, err := types.DeProtoHash(pb.FromBlockHash)
	if err != nil {
		return nil, fmt.Errorf("invalid from block hash: %w", err)
	}
	changesHash, err := types.DeProtoHash(pb.ChangesHash)
	if err != nil {
		return nil, fmt.Errorf("invalid changes hash: %w", err)
	}
	address, err := types.DeProtoAddress(pb.Address)
	if err != nil {
		return nil, fmt.Errorf("invalid address: %w", err)
	}
	toAddress, err := types.DeProtoAddress(pb.ToAddress)
	if err != nil {
		return nil, fmt.Errorf("invalid to address: %w", err)
	}
	tokenStandard, err := types.BytesToZTS(pb.TokenStandard)
	if err != nil {
		return nil, fmt.Errorf("invalid token standard: %w", err)
	}
	nonce, err := DeSerializeNonce(pb.Nonce)
	if err != nil {
		return nil, fmt.Errorf("invalid nonce: %w", err)
	}
	ab := &AccountBlock{
		Version:              pb.Version,
		ChainIdentifier:      pb.ChainIdentifier,
		BlockType:            pb.BlockType,
		Hash:                 *hash,
		PreviousHash:         *prevHash,
		Height:               pb.Height,
		MomentumAcknowledged: *momentumAck,
		Address:              *address,
		ToAddress:            *toAddress,
		Amount:               common.BytesToBigInt(pb.Amount),
		TokenStandard:        tokenStandard,
		FromBlockHash:        *fromBlockHash,
		DescendantBlocks:     make([]*AccountBlock, len(pb.DescendantBlocks)),
		Data:                 pb.Data,
		FusedPlasma:          pb.FusedPlasma,
		Difficulty:           pb.Difficulty,
		Nonce:                nonce,
		BasePlasma:           pb.BasePlasma,
		TotalPlasma:          pb.TotalPlasma,

		ChangesHash: *changesHash,

		PublicKey: pb.PublicKey,
		Signature: pb.Signature,
	}

	for index, dBlockProto := range pb.DescendantBlocks {
		dBlock, err := DeProtoAccountBlock(dBlockProto)
		if err != nil {
			return nil, fmt.Errorf("invalid descendant block at index %d: %w", index, err)
		}
		ab.DescendantBlocks[index] = dBlock
	}
	return ab, nil
}

// EqualBytes reports whether both blocks serialize to identical bytes. This is
// stricter than comparing identifiers, because some stored fields are not part
// of the hash.
func (ab *AccountBlock) EqualBytes(other *AccountBlock) bool {
	if ab == nil || other == nil {
		return ab == other
	}
	abBytes, err := ab.Serialize()
	if err != nil {
		return false
	}
	otherBytes, err := other.Serialize()
	if err != nil {
		return false
	}
	return bytes.Equal(abBytes, otherBytes)
}

// Serialize encodes the block as protobuf bytes — the binary form
// stored by the ledger (see db.Commit). DeserializeAccountBlock is
// the inverse.
func (ab *AccountBlock) Serialize() ([]byte, error) {
	return proto.Marshal(ab.Proto())
}

// DeserializeAccountBlock decodes protobuf bytes produced by
// Serialize. It returns an error for invalid protobuf and, through
// DeProtoAccountBlock, for malformed field payloads.
func DeserializeAccountBlock(data []byte) (*AccountBlock, error) {
	pb := &AccountBlockProto{}
	if err := proto.Unmarshal(data, pb); err != nil {
		return nil, err
	}
	return DeProtoAccountBlock(pb)
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

	PublicKey ed25519.PublicKey `json:"publicKey"` // not included in hash
	Signature []byte            `json:"signature"` // not included in hash
}

// ToNomMarshalJson converts the block to its JSON wire
// representation, rendering Amount as a base-10 string and Nonce as
// hex. The descendant block pointers are shared, not copied. A nil
// Amount renders as the string "<nil>".
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
