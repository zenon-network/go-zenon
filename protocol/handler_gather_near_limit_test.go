package protocol

import (
	"testing"

	"github.com/ethereum/go-ethereum/rlp"

	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common/types"
)

// TestGatherBlocks_NearSoftLimit verifies that gatherBlocksForReply returns
// blocks whose total encoded size is at most SoftResponseLimit plus one
// block's size (the function appends then checks), when the input set
// straddles the limit.  This is the "just below / just above" boundary test
// for the reply-size cap: it proves the accumulator does not stop early for
// a batch that fits, and does stop for a batch that would overflow.
func TestGatherBlocks_NearSoftLimit(t *testing.T) {
	// Build blocks of a controlled size so that N-1 fit under
	// SoftResponseLimit but N do not.
	//
	// A minimal DetailedMomentum with a Data field of size D encodes to
	// roughly D + ~80 bytes.  We binary-search for the Data size that puts
	// the cumulative total just over the limit with a known block count.

	const numBlocks = 5

	// First, measure a single block's overhead.
	probe := &nom.DetailedMomentum{
		Momentum:      &nom.Momentum{Hash: types.Hash{0x01}, Height: 100},
		AccountBlocks: []*nom.AccountBlock{},
	}
	probeEncoded, err := rlp.EncodeToBytes(probe)
	if err != nil {
		t.Fatalf("encode probe: %v", err)
	}
	overhead := len(probeEncoded) // ~80 bytes

	// Target: (numBlocks-1) blocks fit, numBlocks blocks overflow.
	// Per-block budget ≈ SoftResponseLimit / (numBlocks - 1) - overhead.
	perBlockData := SoftResponseLimit/(numBlocks-1) - overhead - 100 // margin

	// Build the blocks.
	blocks := make([]*nom.DetailedMomentum, numBlocks)
	hashes := make([]types.Hash, numBlocks)
	for i := 0; i < numBlocks; i++ {
		h := types.NewHash([]byte{'b', 'l', 'k', byte(i >> 24), byte(i >> 16), byte(i >> 8), byte(i)})
		hashes[i] = h
		blocks[i] = &nom.DetailedMomentum{
			Momentum: &nom.Momentum{
				Hash:   h,
				Height: uint64(i + 2),
				Data:   make([]byte, perBlockData),
			},
			AccountBlocks: []*nom.AccountBlock{},
		}
	}

	// Verify the fixture: (numBlocks-1) blocks fit, all numBlocks overflow.
	totalSize := 0
	for _, b := range blocks {
		enc, _ := rlp.EncodeToBytes(b)
		totalSize += len(enc)
	}
	fitSize := totalSize - func() int {
		enc, _ := rlp.EncodeToBytes(blocks[numBlocks-1])
		return len(enc)
	}()
	t.Logf("fixture: %d blocks total=%d, first %d blocks=%d, softLimit=%d",
		numBlocks, totalSize, numBlocks-1, fitSize, SoftResponseLimit)

	if fitSize >= SoftResponseLimit {
		t.Fatalf("test setup: %d blocks (%d bytes) should fit under soft limit %d, adjust perBlockData",
			numBlocks-1, fitSize, SoftResponseLimit)
	}
	if totalSize <= SoftResponseLimit {
		t.Fatalf("test setup: all %d blocks (%d bytes) should exceed soft limit %d, adjust perBlockData",
			numBlocks, totalSize, SoftResponseLimit)
	}

	// --- Case 1: first (numBlocks-1) blocks — all should be returned ---
	stream1 := streamFromHashes(t, hashes[:numBlocks-1])
	got1, hashCount1, err := gatherBlocksForReply(stream1, func(h types.Hash) *nom.DetailedMomentum {
		for _, b := range blocks[:numBlocks-1] {
			if b.Momentum.Hash == h {
				return b
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("gatherBlocksForReply (fit case): %v", err)
	}
	if len(got1) != numBlocks-1 {
		t.Errorf("fit case: got %d blocks, want %d", len(got1), numBlocks-1)
	}
	if hashCount1 != numBlocks-1 {
		t.Errorf("fit case: hashCount=%d, want %d", hashCount1, numBlocks-1)
	}

	// Verify total encoded size is under the limit.
	replySize1 := 0
	for _, b := range got1 {
		enc, _ := rlp.EncodeToBytes(b)
		replySize1 += len(enc)
	}
	if replySize1 > SoftResponseLimit {
		t.Errorf("fit case: reply size %d exceeds soft limit %d", replySize1, SoftResponseLimit)
	}

	// --- Case 2: all numBlocks + a trailing sentinel — the last real block
	// should trigger the stop; the sentinel must NOT be consumed.  Without a
	// sentinel, EOF masks removal of the soft-limit break (the stream ends
	// after the crossing hash either way). ---
	sentinelHash := types.NewHash([]byte{'s', 'e', 'n', 't', byte(0), byte(0), byte(0), byte(0), byte(0), byte(0), byte(0), byte(0), byte(0), byte(0), byte(0), byte(0), byte(0), byte(0), byte(0), byte(0), byte(0), byte(0), byte(0), byte(0), byte(0), byte(0), byte(0), byte(0), byte(0), byte(0)})
	allHashes := append(append([]types.Hash{}, hashes...), sentinelHash)
	stream2 := streamFromHashes(t, allHashes)
	got2, hashCount2, err := gatherBlocksForReply(stream2, func(h types.Hash) *nom.DetailedMomentum {
		for _, b := range blocks {
			if b.Momentum.Hash == h {
				return b
			}
		}
		if h == sentinelHash {
			t.Error("sentinel hash was looked up — the soft-limit stop is not working")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("gatherBlocksForReply (overflow case): %v", err)
	}

	// The function appends then checks, so it returns blocks until the
	// cumulative size exceeds the limit, then stops.  It should return at
	// most numBlocks blocks (the last one triggers the stop) and at least 1.
	if len(got2) == 0 {
		t.Error("overflow case: got 0 blocks, want at least 1")
	}
	if len(got2) > numBlocks {
		t.Errorf("overflow case: got %d blocks, want at most %d", len(got2), numBlocks)
	}
	if hashCount2 != len(got2) {
		t.Errorf("overflow case: hashCount=%d, want %d (all decoded hashes were looked up)", hashCount2, len(got2))
	}

	// Verify the reply-size invariant: total ≤ SoftResponseLimit + one block.
	replySize2 := 0
	maxBlockSize := 0
	for _, b := range got2 {
		enc, _ := rlp.EncodeToBytes(b)
		replySize2 += len(enc)
		if len(enc) > maxBlockSize {
			maxBlockSize = len(enc)
		}
	}
	if replySize2 > SoftResponseLimit+maxBlockSize {
		t.Errorf("overflow case: reply size %d exceeds SoftResponseLimit (%d) + one block (%d) = %d",
			replySize2, SoftResponseLimit, maxBlockSize, SoftResponseLimit+maxBlockSize)
	}
	t.Logf("overflow case: got %d blocks, replySize=%d, softLimit=%d, maxBlock=%d",
		len(got2), replySize2, SoftResponseLimit, maxBlockSize)
}
