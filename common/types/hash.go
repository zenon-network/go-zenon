package types

import (
	"encoding/hex"
	"fmt"

	"github.com/zenon-network/go-zenon/common/crypto"
)

const (
	// HashSize is the byte length of a Hash.
	HashSize = 32
)

// Hash is a 32-byte digest used for all chain identifiers: account
// block hashes, momentum hashes, transaction data hashes and spork
// IDs. Hashes are produced with SHA3-256 (see NewHash) and rendered as
// bare lowercase hex without a 0x prefix.
type Hash [HashSize]byte

// ZeroHash is the all-zero Hash, used as the "no hash" sentinel, for
// example as the previous-hash of the first block in a chain.
var ZeroHash = Hash{}

// NewHash returns the SHA3-256 digest of data.
func NewHash(data []byte) Hash {
	h, _ := BytesToHash(crypto.Hash(data))
	return h
}

// SetBytes overwrites the hash with b, which must be exactly HashSize
// (32) bytes; otherwise the hash is left unchanged and an error is
// returned.
func (h *Hash) SetBytes(b []byte) error {
	if len(b) != HashSize {
		return fmt.Errorf("error hash size %v", len(b))
	}
	copy(h[:], b)
	return nil
}

// Bytes returns the 32 raw bytes of the hash.
func (h Hash) Bytes() []byte {
	return h[:]
}

// IsZero reports whether the hash equals ZeroHash.
func (h Hash) IsZero() bool {
	return h == ZeroHash
}

// String renders the hash as 64 lowercase hex characters, without a
// 0x prefix. It is the inverse of HexToHash.
func (h Hash) String() string {
	return hex.EncodeToString(h[:])
}

// BytesToHash builds a Hash from its 32 raw bytes. It returns the zero
// Hash and an error if b is not exactly HashSize bytes.
func BytesToHash(b []byte) (Hash, error) {
	var h Hash
	err := h.SetBytes(b)
	return h, err
}

// BytesToHashPanic is like BytesToHash but panics on invalid input.
func BytesToHashPanic(b []byte) Hash {
	h, err := BytesToHash(b)
	if err != nil {
		panic(err)
	}
	return h
}

// HexToHash parses a hash from its hex form. The input must be
// exactly 64 hex characters with no 0x prefix; both letter cases are
// accepted. On failure it returns the zero Hash and an error.
func HexToHash(hexStr string) (Hash, error) {
	if len(hexStr) != 2*HashSize {
		return Hash{}, fmt.Errorf("error hex hash size %v", len(hexStr))
	}
	b, err := hex.DecodeString(hexStr)
	if err != nil {
		return Hash{}, err
	}
	return BytesToHash(b)
}

// HexToHashPanic is like HexToHash but panics on invalid input. It is
// meant for hard-coded hashes known to be valid.
func HexToHashPanic(hexStr string) Hash {
	h, err := HexToHash(hexStr)
	if err != nil {
		panic(err)
	}
	return h
}

// UnmarshalText implements encoding.TextUnmarshaler by parsing a
// 64-character hex string with HexToHash. On error the hash is left
// unchanged.
func (h *Hash) UnmarshalText(input []byte) error {
	hash, e := HexToHash(string(input))
	if e != nil {
		return e
	}
	return h.SetBytes(hash.Bytes())
}

// MarshalText implements encoding.TextMarshaler. The hash is rendered
// as bare lowercase hex, so it appears in JSON as a quoted string.
func (h Hash) MarshalText() ([]byte, error) {
	return []byte(h.String()), nil
}

// Proto wraps the raw hash bytes in their protobuf message, used when
// embedding hashes in serialized chain structures.
func (h *Hash) Proto() *HashProto {
	return &HashProto{
		Hash: h[:],
	}
}

// DeProtoHash is the inverse of Proto. It panics if the protobuf
// payload is not exactly HashSize bytes.
func DeProtoHash(pb *HashProto) *Hash {
	if len(pb.Hash) != HashSize {
		panic(fmt.Sprintf("invalid DeProto - wanted hash size %v but got %v", HashSize, len(pb.Hash)))
	}
	h := new(Hash)
	copy(h[:], pb.Hash)
	return h
}
