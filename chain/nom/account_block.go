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

const (
	BlockTypeGenesisReceive = 1 // receive

	BlockTypeUserSend    = 2 // send
	BlockTypeUserReceive = 3 // receive

	BlockTypeContractSend    = 4 // send
	BlockTypeContractReceive = 5 // receive
)

type AccountBlockTransaction struct {
	Block   *AccountBlock
	Changes db.Patch
}

func (t *AccountBlockTransaction) GetCommits() []db.Commit {
	list := make([]db.Commit, len(t.Block.DescendantBlocks)+1)
	for index := range t.Block.DescendantBlocks {
		list[index] = t.Block.DescendantBlocks[index]
	}
	list[len(list)-1] = t.Block
	return list
}
func (t *AccountBlockTransaction) StealChanges() db.Patch {
	changes := t.Changes
	t.Changes = nil
	return changes
}

type Nonce struct {
	Data [8]byte
}

func (n *Nonce) Copy() Nonce {
	dn := Nonce{}
	copy(dn.Data[:], n.Data[:])
	return dn
}
func (n *Nonce) MarshalText() ([]byte, error) {
	return []byte(hex.EncodeToString(n.Data[:])), nil
}
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

func (n *Nonce) Serialize() []byte {
	return n.Data[:]
}
func DeSerializeNonce(bytes []byte) Nonce {
	if len(bytes) != 8 {
		panic("invalid nonce length")
	}
	var n Nonce
	copy(n.Data[:], bytes)
	return n
}

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

func (ab *AccountBlock) Identifier() types.HashHeight {
	return types.HashHeight{
		Hash:   ab.Hash,
		Height: ab.Height,
	}
}
func (ab *AccountBlock) Previous() types.HashHeight {
	if len(ab.DescendantBlocks) != 0 {
		return ab.DescendantBlocks[0].Previous()
	}
	return types.HashHeight{
		Hash:   ab.PreviousHash,
		Height: ab.Height - 1,
	}
}
func (ab *AccountBlock) Header() types.AccountHeader {
	return types.AccountHeader{
		Address: ab.Address,
		HashHeight: types.HashHeight{
			Hash:   ab.Hash,
			Height: ab.Height,
		},
	}
}
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

func (ab *AccountBlock) DescendantBlocksHash() types.Hash {
	source := make([]byte, 0, types.HashSize*len(ab.DescendantBlocks))
	for _, dBlock := range ab.DescendantBlocks {
		source = append(source, dBlock.Hash.Bytes()...)
	}
	return types.NewHash(source)
}
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

func (ab *AccountBlock) Producer() types.Address {
	if ab.producer == nil {
		producer := types.PubKeyToAddress(ab.PublicKey)
		ab.producer = &producer
	}

	return *ab.producer
}

func (ab *AccountBlock) IsSendBlock() bool {
	return IsSendBlock(ab.BlockType)
}
func (ab *AccountBlock) IsReceiveBlock() bool {
	return IsReceiveBlock(ab.BlockType)
}
func IsSendBlock(blockType uint64) bool {
	return blockType == BlockTypeUserSend || blockType == BlockTypeContractSend
}
func IsReceiveBlock(blockType uint64) bool {
	return blockType == BlockTypeUserReceive || blockType == BlockTypeContractReceive || blockType == BlockTypeGenesisReceive
}

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
func (ab *AccountBlock) Serialize() ([]byte, error) {
	return proto.Marshal(ab.Proto())
}
func DeserializeAccountBlock(data []byte) (*AccountBlock, error) {
	pb := &AccountBlockProto{}
	if err := proto.Unmarshal(data, pb); err != nil {
		return nil, err
	}
	return DeProtoAccountBlock(pb), nil
}

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

func (ab *AccountBlock) MarshalJSON() ([]byte, error) {
	return json.Marshal(ab.ToNomMarshalJson())
}
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
