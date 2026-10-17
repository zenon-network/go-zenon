// Package crypto provides the hash primitives shared across the node.
//
// The protocol's canonical hash is SHA3-256 (FIPS-202), exposed as
// Hash; it produces account-block and momentum hashes (types.NewHash),
// address and token-standard derivation, and proof-of-work digests.
// Keccak256 is the pre-standardization Keccak variant used by Ethereum
// — it yields different digests than Hash and exists only for
// compatibility with the devp2p discovery/RLPx protocols and for
// messages verified by EVM-based bridge networks. HashSHA256 covers
// the places that need classic SHA-256, such as Bitcoin-compatible
// HTLC hash locks.
//
// All three helpers hash the plain concatenation of their arguments,
// with no length prefixes or domain separation: hashing a then b
// equals hashing their concatenation. Callers that hash multiple
// variable-length items must
// ensure unambiguous encodings themselves.
package crypto

import (
	"crypto/sha256"

	"golang.org/x/crypto/sha3"
)

// Hash returns the SHA3-256 (FIPS-202) digest of the concatenation of
// the given byte slices. This is the canonical hash of the protocol;
// types.NewHash wraps it to produce typed 32-byte hashes.
func Hash(data ...[]byte) []byte {
	d := sha3.New256()
	for _, item := range data {
		d.Write(item)
	}
	return d.Sum(nil)
}

// HashSHA256 returns the SHA-256 digest of the concatenation of the
// given byte slices. It is used where compatibility with external
// SHA-256 schemes is required, such as the SHA-256 hash-lock type of
// the HTLC embedded contract.
func HashSHA256(data ...[]byte) []byte {
	d := sha256.New()
	for _, item := range data {
		d.Write(item)
	}
	return d.Sum(nil)
}

// Keccak256 returns the legacy (pre-FIPS-202) Keccak-256 digest of the
// concatenation of the given byte slices. This is the hash used by
// Ethereum; it is NOT interchangeable with Hash. It serves the devp2p
// node-discovery and RLPx handshake code and the bridge messages that
// must be verifiable on EVM networks.
func Keccak256(data ...[]byte) []byte {
	d := sha3.NewLegacyKeccak256()
	for _, item := range data {
		d.Write(item)
	}

	return d.Sum(nil)
}
