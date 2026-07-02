// Package pow implements the proof-of-work that an account block may
// offer in place of fused plasma, the anti-spam resource required to
// issue blocks.
//
// The work is a partial preimage search over the truncated SHA3-256
// digest (the protocol hash, common/crypto.Hash) of an 8-byte nonce
// concatenated with a 32-byte data hash. That data hash binds the work
// to a specific block: it is SHA3-256 of the issuer address followed by
// the previous-block hash (GetAccountBlockHash). A nonce is accepted
// when the first 8 bytes of the digest, read little-endian, meet a
// threshold derived from the block's difficulty.
//
// Difficulty maps to plasma through vm/constants.PoWDifficultyPerPlasma
// (1500 difficulty per unit of plasma): the higher the difficulty the
// more nonces a producer must try on average, and the more plasma the
// block is credited.
package pow

import (
	"encoding/binary"
	"math/big"

	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/crypto"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/wallet"
)

// GetAccountBlockHash returns the data hash that the block's proof-of-
// work is computed over: the SHA3-256 digest of the issuer address
// followed by the previous-block hash. Binding the work to the previous
// hash ties each nonce to one position in the account chain.
func GetAccountBlockHash(block *nom.AccountBlock) types.Hash {
	return types.NewHash(append(block.Address.Bytes(), block.PreviousHash.Bytes()...))
}

// CheckPoWNonce reports whether the block's Nonce is a valid proof of
// work for its Difficulty. It recomputes the block data hash, hashes it
// together with the nonce, and verifies the resulting digest meets the
// difficulty threshold. It is the verification counterpart of
// GetPoWNonce.
func CheckPoWNonce(block *nom.AccountBlock) bool {
	dataHash := GetAccountBlockHash(block)
	target := getTargetByDifficulty(block.Difficulty)
	calc := hashWithNonce(dataHash, block.Nonce.Serialize())
	return greaterDifficulty(calc, target[:])
}

// GetPoWNonce mines and returns an 8-byte nonce whose SHA3-256 digest,
// together with dataHash, meets the threshold for difficulty. It seeds
// the search from a cryptographically random start and increments until
// a digest passes, so the call blocks for a time that grows with
// difficulty. dataHash is normally GetAccountBlockHash of the block
// being issued. It panics if difficulty is nil.
func GetPoWNonce(difficulty *big.Int, dataHash types.Hash) []byte {
	rng := wallet.GetEntropyCSPRNG(8)
	calc, target := getTarget(difficulty, dataHash, rng)
	for {
		if greaterDifficulty(crypto.Hash(calc), target[:]) {
			break
		}
		calc = quickInc(calc)
	}
	var arr [8]byte
	copy(arr[:], calc[:8])
	return arr[:]
}

func getTarget(difficulty *big.Int, data types.Hash, nonce []byte) ([]byte, [8]byte) {
	threshold := GetThresholdByDifficulty(difficulty)
	calc := make([]byte, 40)
	l := copy(calc, nonce[:])
	copy(calc[l:], data[:])
	target := Uint64ToByteArray(threshold)
	return calc, target
}

// Uint64ToByteArray encodes i as 8 bytes in little-endian order, the
// byte ordering the proof-of-work threshold comparison expects.
func Uint64ToByteArray(i uint64) [8]byte {
	var n [8]byte
	binary.LittleEndian.PutUint64(n[:], i)
	return n
}

func quickInc(x []byte) []byte {
	for i := 0; i < len(x); i++ {
		x[i] = x[i] + 1
		if x[i] != 0 {
			return x
		}
	}
	return x
}

// GetThresholdByDifficulty returns the acceptance threshold for the
// given difficulty as 2^64 - (2^64 / difficulty). A candidate nonce is
// valid when the first 8 bytes of its digest, read as a little-endian
// uint64, meet or exceed this threshold, so a higher difficulty shrinks the
// accepting range and requires more attempts. It panics if difficulty
// is nil.
func GetThresholdByDifficulty(difficulty *big.Int) uint64 {
	if difficulty != nil {
		x := big.NewInt(2).Exp(big.NewInt(2), big.NewInt(64), nil)
		y := big.NewInt(0).Quo(x, difficulty)
		x.Sub(x, y)
		return x.Uint64()
	}
	panic("No difficulty supplied to compute PoW")
}

func hashWithNonce(dataHash types.Hash, nonce []byte) []byte {
	calc := make([]byte, 40)
	l := copy(calc, nonce[:])
	copy(calc[l:], dataHash[:])
	return crypto.Hash(calc)[:8]
}

func getTargetByDifficulty(difficulty uint64) [8]byte {
	if difficulty == 0 {
		return [8]byte{}
	}
	// 2^64 - (2^64 / difficulty)
	x := new(big.Int).Exp(common.Big2, common.Big64, nil)
	y := big.NewInt(0).Quo(x, big.NewInt(int64(difficulty)))
	x.Sub(x, y)
	var target [8]byte
	binary.LittleEndian.PutUint64(target[:], x.Uint64())
	return target
}

func greaterDifficulty(x, y []byte) bool {
	// Bytes are stored in LittleEndian ordering.
	for i := 7; i >= 0; i-- {
		if x[i] > y[i] {
			return true
		}
		if x[i] < y[i] {
			return false
		}
		if x[i] == y[i] {
			continue
		}
	}
	return true
}
