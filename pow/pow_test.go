package pow

import (
	"encoding/binary"
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

// Golden vectors pinning the exact bit-level target values for unsigned
// getTargetByDifficulty and v0.0.10-compatible getTargetByDifficultyLegacy,
// and verifying CheckPoWNonce vs CheckPoWNonceLegacy behavior at each point.
func TestGetTargetByDifficulty_GoldenVectors(t *testing.T) {
	cases := []struct {
		name         string
		difficulty   uint64
		wantUnsigned uint64
		wantLegacy   uint64
	}{
		{
			name:         "zero",
			difficulty:   0,
			wantUnsigned: 0,
			wantLegacy:   0,
		},
		{
			name:         "one",
			difficulty:   1,
			wantUnsigned: 0,
			wantLegacy:   0,
		},
		{
			name:         "legacy_max_difficulty",
			difficulty:   141750000,
			wantUnsigned: 18446743943573790955,
			wantLegacy:   18446743943573790955,
		},
		{
			name:         "max_int64",
			difficulty:   (1 << 63) - 1,
			wantUnsigned: math.MaxUint64 - 1,
			wantLegacy:   math.MaxUint64 - 1,
		},
		{
			name:         "two_pow_63",
			difficulty:   1 << 63,
			wantUnsigned: math.MaxUint64 - 1,
			wantLegacy:   2,
		},
		{
			name:         "max_uint64_minus_1",
			difficulty:   math.MaxUint64 - 1,
			wantUnsigned: math.MaxUint64,
			wantLegacy:   1 << 63,
		},
		{
			name:         "max_uint64",
			difficulty:   math.MaxUint64,
			wantUnsigned: math.MaxUint64,
			wantLegacy:   0,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			unsignedTarget := getTargetByDifficulty(tc.difficulty)
			gotUnsigned := binary.LittleEndian.Uint64(unsignedTarget[:])
			if gotUnsigned != tc.wantUnsigned {
				t.Errorf("unsigned target(%d) = %d, want %d", tc.difficulty, gotUnsigned, tc.wantUnsigned)
			}
			legacyTarget := getTargetByDifficultyLegacy(tc.difficulty)
			gotLegacy := binary.LittleEndian.Uint64(legacyTarget[:])
			if gotLegacy != tc.wantLegacy {
				t.Errorf("legacy target(%d) = %d, want %d", tc.difficulty, gotLegacy, tc.wantLegacy)
			}
		})
	}
}

func TestCheckPoWNonce_VsLegacy_GoldenVectors(t *testing.T) {
	// Below 2^63, CheckPoWNonce and CheckPoWNonceLegacy must agree identically.
	for _, difficulty := range []uint64{0, 1, 141750000, (1 << 63) - 1} {
		for i := uint64(0); i < 64; i++ {
			block := &nom.AccountBlock{Difficulty: difficulty}
			binary.LittleEndian.PutUint64(block.Nonce.Data[:], i)
			gotUnsigned := CheckPoWNonce(block)
			gotLegacy := CheckPoWNonceLegacy(block)
			if gotUnsigned != gotLegacy {
				t.Fatalf("difficulty %d nonce %d: unsigned=%v != legacy=%v", difficulty, i, gotUnsigned, gotLegacy)
			}
		}
	}

	// At 2^63 (legacy target = 2): a digest in [2, 2^64-3] fails unsigned and passes legacy.
	{
		block := &nom.AccountBlock{Difficulty: 1 << 63}
		dataHash := GetAccountBlockHash(block)
		found := false
		for i := uint64(0); i < 1024; i++ {
			binary.LittleEndian.PutUint64(block.Nonce.Data[:], i)
			digest := binary.LittleEndian.Uint64(hashWithNonce(dataHash, block.Nonce.Serialize()))
			if digest >= 2 && digest < math.MaxUint64-1 {
				found = true
				break
			}
		}
		if !found {
			t.Fatal("failed to find nonce for 2^63 test")
		}
		if CheckPoWNonce(block) {
			t.Fatal("difficulty 2^63: expected unsigned CheckPoWNonce to reject")
		}
		if !CheckPoWNonceLegacy(block) {
			t.Fatal("difficulty 2^63: expected CheckPoWNonceLegacy to accept digest >= 2")
		}
	}

	// At 2^64 - 2 (legacy target = 2^63): digests >= 2^63 pass legacy (and fail unsigned),
	// while digests < 2^63 fail both.
	{
		block := &nom.AccountBlock{Difficulty: math.MaxUint64 - 1}
		dataHash := GetAccountBlockHash(block)
		var foundHigh, foundLow bool
		for i := uint64(0); i < 1024 && (!foundHigh || !foundLow); i++ {
			binary.LittleEndian.PutUint64(block.Nonce.Data[:], i)
			digest := binary.LittleEndian.Uint64(hashWithNonce(dataHash, block.Nonce.Serialize()))
			if digest >= (1<<63) && digest < math.MaxUint64 && !foundHigh {
				foundHigh = true
				if CheckPoWNonce(block) {
					t.Fatal("difficulty 2^64-2 (digest >= 2^63): expected unsigned CheckPoWNonce to reject")
				}
				if !CheckPoWNonceLegacy(block) {
					t.Fatal("difficulty 2^64-2 (digest >= 2^63): expected CheckPoWNonceLegacy to accept")
				}
			}
			if digest < (1<<63) && !foundLow {
				foundLow = true
				if CheckPoWNonce(block) {
					t.Fatal("difficulty 2^64-2 (digest < 2^63): expected unsigned CheckPoWNonce to reject")
				}
				if CheckPoWNonceLegacy(block) {
					t.Fatal("difficulty 2^64-2 (digest < 2^63): expected CheckPoWNonceLegacy to reject")
				}
			}
		}
		if !foundHigh || !foundLow {
			t.Fatalf("failed to find both high (%v) and low (%v) digests for 2^64-2", foundHigh, foundLow)
		}
	}

	// At 2^64 - 1 (legacy target = 0): every digest passes legacy while failing unsigned
	// (except the astronomically unlikely all-ones digest).
	{
		block := &nom.AccountBlock{Difficulty: math.MaxUint64}
		dataHash := GetAccountBlockHash(block)
		found := false
		for i := uint64(0); i < 1024; i++ {
			binary.LittleEndian.PutUint64(block.Nonce.Data[:], i)
			digest := binary.LittleEndian.Uint64(hashWithNonce(dataHash, block.Nonce.Serialize()))
			if digest < math.MaxUint64 {
				found = true
				break
			}
		}
		if !found {
			t.Fatal("failed to find nonce for 2^64-1 test")
		}
		if CheckPoWNonce(block) {
			t.Fatal("difficulty 2^64-1: expected unsigned CheckPoWNonce to reject")
		}
		if !CheckPoWNonceLegacy(block) {
			t.Fatal("difficulty 2^64-1: expected CheckPoWNonceLegacy to accept digest >= 0")
		}
	}
}
