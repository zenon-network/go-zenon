package pow

import (
	"math"
	"math/big"
	"testing"

	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/vm/constants"
)

// getTargetByDifficulty must agree with GetThresholdByDifficulty, which is the
// producer-side computation and already takes an unsigned value.
func TestGetTargetByDifficulty_MatchesThreshold(t *testing.T) {
	difficulties := []uint64{
		1,
		2,
		1000,
		constants.MaxDifficultyForAccountBlock,
		math.MaxInt64,
		math.MaxInt64 + 1,
		math.MaxUint64 / 2,
		math.MaxUint64 - 1,
		math.MaxUint64,
	}

	for _, difficulty := range difficulties {
		target := getTargetByDifficulty(difficulty)
		expected := GetThresholdByDifficulty(new(big.Int).SetUint64(difficulty))

		var got uint64
		for i := 7; i >= 0; i-- {
			got = got<<8 | uint64(target[i])
		}

		if got != expected {
			t.Errorf("difficulty %d: target %d, threshold %d", difficulty, got, expected)
		}
	}
}

// The target is monotonic in difficulty over the whole uint64 range: a larger
// difficulty must never be easier to satisfy than a smaller one.
func TestGetTargetByDifficulty_Monotonic(t *testing.T) {
	difficulties := []uint64{
		1,
		1000,
		constants.MaxDifficultyForAccountBlock,
		math.MaxInt64 - 1,
		math.MaxInt64,
		math.MaxInt64 + 1,
		math.MaxUint64,
	}

	previous := uint64(0)
	for _, difficulty := range difficulties {
		target := getTargetByDifficulty(difficulty)
		var got uint64
		for i := 7; i >= 0; i-- {
			got = got<<8 | uint64(target[i])
		}

		if got < previous {
			t.Fatalf("difficulty %d produced target %d, lower than %d for the preceding difficulty", difficulty, got, previous)
		}
		previous = got
	}
}

// A difficulty far above what the protocol allows must still require work.
func TestCheckPoWNonce_HighDifficultyRejectsArbitraryNonces(t *testing.T) {
	difficulties := []uint64{
		math.MaxInt64 + 1,
		math.MaxUint64 / 2,
		math.MaxUint64,
	}

	for _, difficulty := range difficulties {
		accepted := 0
		for i := 0; i < 512; i++ {
			block := &nom.AccountBlock{Difficulty: difficulty}
			block.Nonce.Data[0] = byte(i)
			block.Nonce.Data[1] = byte(i >> 8)
			if CheckPoWNonce(block) {
				accepted++
			}
		}
		if accepted != 0 {
			t.Errorf("difficulty %d accepted %d/512 arbitrary nonces", difficulty, accepted)
		}
	}
}
