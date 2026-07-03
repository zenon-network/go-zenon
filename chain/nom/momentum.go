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
// block-producing Ed25519 key.
type Momentum struct {
	// Version is the momentum format version; the verifier accepts
	// only 1.
	Version uint64 `json:"version"`
	// ChainIdentifier is the network identifier from genesis; momentums
	// whose identifier does not match the node's chain are rejected.
	ChainIdentifier uint64 `json:"chainIdentifier"`

	// Hash is the momentum's identity, expected to equal ComputeHash.
	Hash types.Hash `json:"hash"`
	// PreviousHash links to the momentum at Height-1.
	PreviousHash types.Hash `json:"previousHash"`
	// Height is the 1-based position in the momentum chain; the genesis
	// momentum has height 1.
	Height uint64 `json:"height"`

	// TimestampUnix is the production time in Unix seconds; it is the
	// form included in the hash and must strictly increase along the
	// chain.
	TimestampUnix uint64 `json:"timestamp"`
	// Timestamp caches TimestampUnix as a time.Time (see EnsureCache);
	// it is not included in the hash.
	Timestamp *time.Time `json:"-" rlp:"-"`

	// Data is hashed into the momentum hash but is currently required
	// by the verifier to be empty.
	Data []byte `json:"data"`
	// Content lists the headers of the account blocks this momentum
	// confirms; its hash is included in the momentum hash.
	Content MomentumContent `json:"content"`

	// ChangesHash is db.PatchHash of the state patch produced by
	// applying the momentum; unlike its account-block counterpart it is
	// included in the momentum hash.
	ChangesHash types.Hash `json:"changesHash"`

	producer *types.Address `rlp:"-"` // not included in hash, for caching purpose only
	// PublicKey is the producing pillar's block-producing Ed25519 key
	// and Signature its signature of Hash; neither is included in the
	// hash. Consensus additionally checks that the derived producer
	// address matches the pillar elected for the tick.
	PublicKey ed25519.PublicKey `json:"publicKey"`
	Signature []byte            `json:"signature"`
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
//
// PublicKey and Signature are not covered. It neither reads nor
// writes m.Hash.
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
	}
}

// DeProtoMomentum is the inverse of Momentum.Proto; it also rebuilds
// the caches via EnsureCache. It panics (via types.DeProtoHash) if a
// hash payload is not exactly HashSize bytes.
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
// It returns an error for invalid protobuf but panics, as
// DeProtoMomentum does, on hash payloads of the wrong size.
func DeserializeMomentum(data []byte) (*Momentum, error) {
	pb := &MomentumProto{}
	if err := proto.Unmarshal(data, pb); err != nil {
		return nil, err
	}
	return DeProtoMomentum(pb), nil
}
