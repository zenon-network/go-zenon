package nom

import (
	"crypto/ed25519"
	"time"

	"google.golang.org/protobuf/proto"

	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/db"
	"github.com/zenon-network/go-zenon/common/types"
)

var (
	emptyEd25519PublicKey ed25519.PublicKey
)

type MomentumTransaction struct {
	Momentum *Momentum
	Changes  db.Patch
}

func (t *MomentumTransaction) GetCommits() []db.Commit {
	return []db.Commit{t.Momentum}
}
func (t *MomentumTransaction) StealChanges() db.Patch {
	changes := t.Changes
	t.Changes = nil
	return changes
}

type Momentum struct {
	Version         uint64 `json:"version"`
	ChainIdentifier uint64 `json:"chainIdentifier"`

	Hash         types.Hash `json:"hash"`
	PreviousHash types.Hash `json:"previousHash"`
	Height       uint64     `json:"height"`

	TimestampUnix uint64     `json:"timestamp"` // hash item 3
	Timestamp     *time.Time `json:"-" rlp:"-"` // not included in hash, for caching purpose only

	Data    []byte          `json:"data"`    // hash of Data is included in hash
	Content MomentumContent `json:"content"` // hash of Content is included in hash

	ChangesHash types.Hash `json:"changesHash"`

	producer  *types.Address    `rlp:"-"`          // not included in hash, for caching purpose only
	PublicKey ed25519.PublicKey `json:"publicKey"` // not included in hash
	Signature []byte            `json:"signature"` // not included in hash
}

type DetailedMomentum struct {
	Momentum      *Momentum       `json:"momentum"`
	AccountBlocks []*AccountBlock `json:"accountBlocks"`
}

func (m *Momentum) ComputeHash() types.Hash {
	return types.NewHash(common.JoinBytes(
		common.Uint64ToBytes(m.Version),
		common.Uint64ToBytes(m.ChainIdentifier),
		m.PreviousHash.Bytes(),
		common.Uint64ToBytes(m.Height),
		common.Uint64ToBytes(m.TimestampUnix),
		types.NewHash(m.Data).Bytes(),
		m.Content.Hash().Bytes(),
		m.ChangesHash.Bytes(),
	))
}

func (m *Momentum) Identifier() types.HashHeight {
	return types.HashHeight{
		Height: m.Height,
		Hash:   m.Hash,
	}
}
func (m *Momentum) Previous() types.HashHeight {
	return types.HashHeight{
		Hash:   m.PreviousHash,
		Height: m.Height - 1,
	}
}

func (m *Momentum) Producer() types.Address {
	if m.producer == nil {
		producer := types.PubKeyToAddress(m.PublicKey)
		m.producer = &producer
	}
	return *m.producer
}
func (m *Momentum) EnsureCache() {
	if m.Timestamp == nil {
		timestamp := time.Unix(int64(m.TimestampUnix), 0)
		m.Timestamp = &timestamp
	}
	// don't call producer before publicKey is set
	if !m.PublicKey.Equal(emptyEd25519PublicKey) {
		m.Producer()
	}
}

func (m *Momentum) Proto() *MomentumProto {
	return &MomentumProto{
		Version:         m.Version,
		ChainIdentifier: m.ChainIdentifier,
		Hash:            m.Hash.Proto(),
		PreviousHash:    m.PreviousHash.Proto(),
		Height:          m.Height,
		Timestamp:       m.TimestampUnix,
		Data:            m.Data,
		Content:         m.Content.Proto(),
		ChangesHash:     m.ChangesHash.Proto(),
		PublicKey:       m.PublicKey,
		Signature:       m.Signature,
	}
}
func DeProtoMomentum(pb *MomentumProto) *Momentum {
	m := &Momentum{
		Version:         pb.Version,
		ChainIdentifier: pb.ChainIdentifier,
		Hash:            *types.DeProtoHash(pb.Hash),
		PreviousHash:    *types.DeProtoHash(pb.PreviousHash),
		Height:          pb.Height,
		TimestampUnix:   pb.Timestamp,
		Data:            pb.Data,
		Content:         DeProtoMomentumContent(pb.Content),
		ChangesHash:     *types.DeProtoHash(pb.ChangesHash),
		PublicKey:       pb.PublicKey,
		Signature:       pb.Signature,
	}
	m.EnsureCache()
	return m
}
func (m *Momentum) Serialize() ([]byte, error) {
	pb := m.Proto()
	buf, err := proto.Marshal(pb)
	if err != nil {
		return nil, err
	}
	return buf, nil
}
func DeserializeMomentum(data []byte) (*Momentum, error) {
	pb := &MomentumProto{}
	if err := proto.Unmarshal(data, pb); err != nil {
		return nil, err
	}
	return DeProtoMomentum(pb), nil
}
