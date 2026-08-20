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

func GetAccountBlockHash(block *nom.AccountBlock) types.Hash {
	return types.NewHash(append(block.Address.Bytes(), block.PreviousHash.Bytes()...))
}

func CheckPoWNonce(block *nom.AccountBlock) bool {
	dataHash := GetAccountBlockHash(block)
	target := getTargetByDifficulty(block.Difficulty)
	calc := hashWithNonce(dataHash, block.Nonce.Serialize())
	return greaterDifficulty(calc, target[:])
}

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
