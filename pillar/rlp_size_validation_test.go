package pillar

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/rlp"
	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/protocol"
)

// TestContent_ByteBudget_MomentumHeaderSize verifies that the worst-case
// momentum header size used in filterBlocksToCommit is large enough to
// cover any real populated momentum header.
func TestContent_ByteBudget_MomentumHeaderSize(t *testing.T) {
	worstCase := worstCaseMomentumHeaderSize()
	if worstCase <= 0 {
		t.Fatalf("worstCaseMomentumHeaderSize() = %d, expected positive", worstCase)
	}

	// A real populated momentum header must never exceed the worst case.
	realM := &nom.Momentum{
		Version:         2,
		ChainIdentifier: 1,
		Hash:            types.Hash{1, 2, 3},
		PreviousHash:    types.Hash{4, 5, 6},
		Height:          14419042,
		TimestampUnix:   1728867600,
		Content:         nom.MomentumContent{},
		ChangesHash:     types.Hash{7, 8, 9},
		PublicKey:       make([]byte, 32),
		Signature:       make([]byte, 64),
		NextFusionPrice: 1000000,
		NextWorkPrice:   500000,
	}
	realEncoded, err := rlp.EncodeToBytes(realM)
	if err != nil {
		t.Fatalf("RLP encode failed: %v", err)
	}
	t.Logf("worst-case header: %d bytes, real populated header: %d bytes", worstCase, len(realEncoded))

	if len(realEncoded) > worstCase {
		t.Errorf("real populated header (%d) exceeds worst-case estimate (%d)", len(realEncoded), worstCase)
	}
}

// The budget safety invariant test lives in protocol (TestMaxDetailedMomentumWireSize)
// where the constants are defined.

// TestContent_ByteBudget_EnvelopeCoversPopulatedMomentum verifies that the
// selector's size estimate (worst-case header + per-block costs) is always
// >= the actual RLP-encoded size of the final populated DetailedMomentum.
func TestContent_ByteBudget_EnvelopeCoversPopulatedMomentum(t *testing.T) {
	addr := types.ParseAddressPanic("z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz")

	// Build blocks of realistic size and count
	for _, n := range []int{1, 10, 50} {
		blocks := make([]*nom.AccountBlock, n)
		content := make(nom.MomentumContent, n)
		for i := 0; i < n; i++ {
			b := &nom.AccountBlock{
				Version:         1,
				ChainIdentifier: 1,
				BlockType:       nom.BlockTypeUserSend,
				Height:          uint64(i + 1),
				Address:         addr,
				Data:            make([]byte, 1000),
				FusedPlasma:     21000,
				BasePlasma:      21000,
				TotalPlasma:     21000,
			}
			b.Hash = b.ComputeHash()
			blocks[i] = b
			content[i] = &types.AccountHeader{
				Address:    b.Address,
				HashHeight: types.HashHeight{Hash: b.Hash, Height: b.Height},
			}
		}

		// Selector's estimate (replicates filterBlocksToCommit's arithmetic)
		estimate := worstCaseMomentumHeaderSize() + listPrefixAllowance
		for _, b := range blocks {
			estimate += blockWireSize(b) + contentEntrySize(b)
		}

		// Actual populated DetailedMomentum
		realM := &nom.Momentum{
			Version:         2,
			ChainIdentifier: 1,
			Hash:            types.Hash{1, 2, 3},
			PreviousHash:    types.Hash{4, 5, 6},
			Height:          14419042,
			TimestampUnix:   1728867600,
			Content:         content,
			ChangesHash:     types.Hash{7, 8, 9},
			PublicKey:       make([]byte, 32),
			Signature:       make([]byte, 64),
			NextFusionPrice: 1000000,
			NextWorkPrice:   500000,
		}
		dm := &nom.DetailedMomentum{Momentum: realM, AccountBlocks: blocks}
		encDM, err := rlp.EncodeToBytes(dm)
		if err != nil {
			t.Fatalf("RLP encode DetailedMomentum: %v", err)
		}

		margin := estimate - len(encDM)
		t.Logf("N=%d: estimate=%d actual=%d margin=%d", n, estimate, len(encDM), margin)
		if margin < 0 {
			t.Errorf("N=%d: estimate (%d) < actual (%d): envelope does not cover populated momentum (undercount %d bytes)",
				n, estimate, len(encDM), -margin)
		}
	}
}

// TestContent_ByteBudget_BoundaryAtBudget verifies that blocks just below
// the budget are admitted and blocks just above are rejected.
func TestContent_ByteBudget_BoundaryAtBudget(t *testing.T) {
	cs := newTestContentSelector()
	addr := types.ParseAddressPanic("z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz")

	// Compute the data size that brings the total estimate just under the budget.
	// The selector starts from worstCaseMomentumHeaderSize() + listPrefixAllowance.
	baseline := worstCaseMomentumHeaderSize() + listPrefixAllowance

	// Find the data size where the selector's estimate crosses the budget.
	// Binary search for the boundary.
	fitSize := 0
	overSize := protocol.MaxDetailedMomentumWireSize // definitely over
	for overSize-fitSize > 1 {
		mid := (fitSize + overSize) / 2
		probe := &nom.AccountBlock{
			Version: 1, BlockType: nom.BlockTypeUserSend, Height: 1,
			Address: addr, Data: make([]byte, mid),
			FusedPlasma: 21000, BasePlasma: 21000, TotalPlasma: 21000,
		}
		probe.Hash = probe.ComputeHash()
		entrySize := contentEntrySize(probe)
		if baseline+blockWireSize(probe)+entrySize <= protocol.MaxDetailedMomentumWireSize {
			fitSize = mid
		} else {
			overSize = mid
		}
	}

	// Just below budget: should be admitted
	fitBlock := &nom.AccountBlock{
		Version: 1, BlockType: nom.BlockTypeUserSend, Height: 1,
		Address: addr, Data: make([]byte, fitSize),
		FusedPlasma: 21000, BasePlasma: 21000, TotalPlasma: 21000,
	}
	fitBlock.Hash = fitBlock.ComputeHash()
	fitResult := cs.Content([]*nom.AccountBlock{fitBlock})
	if len(fitResult) != 1 {
		t.Errorf("block just below budget was rejected (data=%d, budget=%d)",
			fitSize, protocol.MaxDetailedMomentumWireSize)
	}

	// Just above budget: should be rejected
	overBlock := &nom.AccountBlock{
		Version: 1, BlockType: nom.BlockTypeUserSend, Height: 1,
		Address: addr, Data: make([]byte, overSize),
		FusedPlasma: 21000, BasePlasma: 21000, TotalPlasma: 21000,
	}
	overBlock.Hash = overBlock.ComputeHash()
	overResult := cs.Content([]*nom.AccountBlock{overBlock})
	if len(overResult) != 0 {
		t.Errorf("block just above budget was admitted (data=%d, budget=%d)",
			overSize, protocol.MaxDetailedMomentumWireSize)
	}
}

// TestContent_ByteBudget_UnencodableBlockRejected verifies that a block
// whose RLP encoding fails is rejected rather than treated as zero-size.
func TestContent_ByteBudget_UnencodableBlockRejected(t *testing.T) {
	cs := newTestContentSelector()
	addr := types.ParseAddressPanic("z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz")

	// A block with a negative Amount causes RLP encoding to fail
	// ("rlp: cannot encode negative big.Int").
	badBlock := &nom.AccountBlock{
		Version: 1, BlockType: nom.BlockTypeUserSend, Height: 1,
		Address: addr, Data: make([]byte, 100),
		Amount:      big.NewInt(-1),
		FusedPlasma: 21000, BasePlasma: 21000, TotalPlasma: 21000,
	}
	badBlock.Hash = badBlock.ComputeHash()

	sz := blockWireSize(badBlock)
	if sz >= 0 {
		t.Fatalf("expected encoding failure for negative Amount, got size %d", sz)
	}

	result := cs.Content([]*nom.AccountBlock{badBlock})
	if len(result) != 0 {
		t.Errorf("unencodable block was admitted to the momentum")
	}
}
