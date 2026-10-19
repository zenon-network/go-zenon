package nom

import (
	"crypto/ed25519"
	"fmt"
	"time"

	"google.golang.org/protobuf/proto"

	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/db"
	"github.com/zenon-network/go-zenon/common/types"
)

const (
	// DynamicPlasmaMomentumVersion is the momentum Version produced once
	// the Dynamic Plasma spork is enforced; such momentums carry
	// NextFusionPrice and NextWorkPrice.
	DynamicPlasmaMomentumVersion = uint64(2)
)

var (
	emptyEd25519PublicKey ed25519.PublicKey
)

// MomentumTransaction pairs a momentum with the db.Patch of
// momentum-store changes its insertion produces. It implements
// db.Transaction and is the unit accepted by the momentum pool.
type MomentumTransaction struct {
	Momentum *Momentum
	Changes  db.Patch
}

// GetCommits returns the momentum as the transaction's single commit.
func (t *MomentumTransaction) GetCommits() []db.Commit {
	return []db.Commit{t.Momentum}
}

// StealChanges returns the transaction's patch and transfers
// ownership, nilling out the internal reference; it must be called at
// most once.
func (t *MomentumTransaction) StealChanges() db.Patch {
	changes := t.Changes
	t.Changes = nil
	return changes
}

// Momentum is one block of the momentum chain — the consensus ledger
// that confirms account blocks. An elected pillar produces one
// momentum per scheduled tick, listing the headers of the confirmed
// account blocks in Content and signing the momentum's Hash with its
// block-producing Ed25519 key. From the Dynamic Plasma spork,
// momentums carry Version 2 and publish NextFusionPrice and
// NextWorkPrice, the plasma prices every block confirmed by the next
// momentum must meet; both fields are hashed when present.
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

	NextFusionPrice uint64 `json:"nextFusionPrice" rlp:"optional"` // NextFusionPrice was added with the Dynamic Plasma spork
	NextWorkPrice   uint64 `json:"nextWorkPrice" rlp:"optional"`   // NextWorkPrice was added with the Dynamic Plasma spork
}

// DetailedMomentum pairs a momentum with the full account blocks it
// confirms, expanding the headers of Content. (The rpc/api package
// defines its own DetailedMomentum that serializes the blocks under
// "blocks" instead of "accountBlocks".)
type DetailedMomentum struct {
	Momentum      *Momentum       `json:"momentum"`
	AccountBlocks []*AccountBlock `json:"accountBlocks"`
}

// ComputeHash recomputes the momentum hash: a SHA3-256 digest over the
// following fields, in order, with uint64s encoded big-endian:
//   - Version
//   - ChainIdentifier
//   - PreviousHash
//   - Height
//   - TimestampUnix
//   - the SHA3-256 hash of Data
//   - the hash of Content
//   - ChangesHash
//   - NextFusionPrice and NextWorkPrice, in that order, only when
//     Version is at least DynamicPlasmaMomentumVersion
//
// PublicKey and Signature are not covered. It neither reads nor
// writes m.Hash.
func (m *Momentum) ComputeHash() types.Hash {
	bytes := common.JoinBytes(
		common.Uint64ToBytes(m.Version),
		common.Uint64ToBytes(m.ChainIdentifier),
		m.PreviousHash.Bytes(),
		common.Uint64ToBytes(m.Height),
		common.Uint64ToBytes(m.TimestampUnix),
		types.NewHash(m.Data).Bytes(),
		m.Content.Hash().Bytes(),
		m.ChangesHash.Bytes(),
	)
	if m.Version >= DynamicPlasmaMomentumVersion {
		bytes = common.JoinBytes(
			bytes,
			common.Uint64ToBytes(m.NextFusionPrice),
			common.Uint64ToBytes(m.NextWorkPrice),
		)
	}
	return types.NewHash(bytes)
}

// Identifier returns the momentum's hash-height, which also names the
// momentum-store version it produces (see db.Commit).
func (m *Momentum) Identifier() types.HashHeight {
	return types.HashHeight{
		Height: m.Height,
		Hash:   m.Hash,
	}
}

// Previous returns the identifier of the preceding momentum
// (PreviousHash at Height-1).
func (m *Momentum) Previous() types.HashHeight {
	return types.HashHeight{
		Hash:   m.PreviousHash,
		Height: m.Height - 1,
	}
}

// Producer returns the pillar address derived from the momentum's
// PublicKey (types.PubKeyToAddress), caching the result on first
// call. Do not call it before PublicKey is set, or the empty-key
// address will be cached.
func (m *Momentum) Producer() types.Address {
	if m.producer == nil {
		producer := types.PubKeyToAddress(m.PublicKey)
		m.producer = &producer
	}
	return *m.producer
}

// EnsureCache populates the derived fields: the Timestamp cache from
// TimestampUnix and, if PublicKey is set, the producer address cache.
// It is idempotent.
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

// Proto converts the momentum to its protobuf message; the cached
// Timestamp and producer fields are not part of the encoding.
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
		NextFusionPrice: m.NextFusionPrice,
		NextWorkPrice:   m.NextWorkPrice,
	}
}

// DeProtoMomentum is the inverse of Momentum.Proto; it also rebuilds
// the caches via EnsureCache. A nil proto, a hash payload that is not
// exactly HashSize bytes or a malformed content entry returns an
// error naming the field; nothing in the decode path panics.
func DeProtoMomentum(pb *MomentumProto) (*Momentum, error) {
	if pb == nil {
		return nil, fmt.Errorf("invalid DeProto - nil momentum proto")
	}
	hash, err := types.DeProtoHash(pb.Hash)
	if err != nil {
		return nil, fmt.Errorf("invalid hash: %w", err)
	}
	prevHash, err := types.DeProtoHash(pb.PreviousHash)
	if err != nil {
		return nil, fmt.Errorf("invalid previous hash: %w", err)
	}
	changesHash, err := types.DeProtoHash(pb.ChangesHash)
	if err != nil {
		return nil, fmt.Errorf("invalid changes hash: %w", err)
	}
	content, err := DeProtoMomentumContent(pb.Content)
	if err != nil {
		return nil, fmt.Errorf("invalid content: %w", err)
	}
	m := &Momentum{
		Version:         pb.Version,
		ChainIdentifier: pb.ChainIdentifier,
		Hash:            *hash,
		PreviousHash:    *prevHash,
		Height:          pb.Height,
		TimestampUnix:   pb.Timestamp,
		Data:            pb.Data,
		Content:         content,
		ChangesHash:     *changesHash,
		PublicKey:       pb.PublicKey,
		Signature:       pb.Signature,
		NextFusionPrice: pb.NextFusionPrice,
		NextWorkPrice:   pb.NextWorkPrice,
	}
	m.EnsureCache()
	return m, nil
}

// Serialize encodes the momentum as protobuf bytes — the binary form
// stored by the ledger (see db.Commit). DeserializeMomentum is the
// inverse.
func (m *Momentum) Serialize() ([]byte, error) {
	pb := m.Proto()
	buf, err := proto.Marshal(pb)
	if err != nil {
		return nil, err
	}
	return buf, nil
}

// DeserializeMomentum decodes protobuf bytes produced by Serialize.
// It returns an error for invalid protobuf and, through DeProtoMomentum,
// for hash payloads of the wrong size.
func DeserializeMomentum(data []byte) (*Momentum, error) {
	pb := &MomentumProto{}
	if err := proto.Unmarshal(data, pb); err != nil {
		return nil, err
	}
	return DeProtoMomentum(pb)
}
