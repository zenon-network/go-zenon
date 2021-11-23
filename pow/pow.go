package pow

import (
	"encoding/binary"
	"math/big"

	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/crypto"
	"github.com/zenon-network/go-zenon/common/types"
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
