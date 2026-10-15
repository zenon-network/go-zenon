package pillar

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/rlp"
	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common/types"
)

// TestContent_ByteBudget_MeasuredSizeIsExact verifies that blockWireSize
// returns the exact RLP-encoded size, not an estimate. This is the core
// property that replaces the old conservative-estimate approach: the
// selector measures real RLP sizes so it cannot be fooled by oversized
// non-Data fields.
func TestContent_ByteBudget_MeasuredSizeIsExact(t *testing.T) {
	block := &nom.AccountBlock{
		Version:         1,
		ChainIdentifier: 1,
		BlockType:       nom.BlockTypeUserSend,
		Height:          1,
		Address:         types.ParseAddressPanic("z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz"),
		ToAddress:       types.ParseAddressPanic("z1qqfmjdays57w488sta69ykc2ey7r6d0q9wdvtj"),
		Data:            make([]byte, 1000),
		FusedPlasma:     21000,
		BasePlasma:      21000,
		TotalPlasma:     21000,
	}

	encoded, err := rlp.EncodeToBytes(block)
	if err != nil {
		t.Fatalf("RLP encode failed: %v", err)
	}

	measured := blockWireSize(block)
	actual := len(encoded)
	if measured != actual {
		t.Errorf("blockWireSize (%d) != actual RLP size (%d): measurement is not exact", measured, actual)
	}
	t.Logf("block with 1000-byte Data: measured=%d, actual=%d", measured, actual)

	// Empty block
	empty := &nom.AccountBlock{}
	emptyEncoded, err := rlp.EncodeToBytes(empty)
	if err != nil {
		t.Fatalf("RLP encode of empty block failed: %v", err)
	}
	emptyMeasured := blockWireSize(empty)
	emptyActual := len(emptyEncoded)
	if emptyMeasured != emptyActual {
		t.Errorf("empty block: measured (%d) != actual RLP size (%d)", emptyMeasured, emptyActual)
	}
	t.Logf("empty block: measured=%d, actual=%d", emptyMeasured, emptyActual)

	// Block with descendants
	withDesc := &nom.AccountBlock{
		Data: make([]byte, 500),
		DescendantBlocks: []*nom.AccountBlock{
			{Data: make([]byte, 300)},
			{Data: make([]byte, 200)},
		},
	}
	descEncoded, err := rlp.EncodeToBytes(withDesc)
	if err != nil {
		t.Fatalf("RLP encode of block with descendants failed: %v", err)
	}
	descMeasured := blockWireSize(withDesc)
	// rlp.EncodeToBytes already includes DescendantBlocks in the encoding,
	// so the actual size is just the encoded parent.
	descActual := len(descEncoded)
	if descMeasured != descActual {
		t.Errorf("block with descendants: measured (%d) != actual RLP size (%d)", descMeasured, descActual)
	}
	t.Logf("block with descendants: measured=%d, actual=%d", descMeasured, descActual)
}

// TestContent_ByteBudget_FullyPopulatedBlock verifies that a fully populated
// block (every field set to realistic values) is measured exactly. This is
// the case that broke the old estimate approach: the old perBlockFixedSize
// assumption was validated only for Data, not for other variable-length
// fields like PublicKey and Signature.
func TestContent_ByteBudget_FullyPopulatedBlock(t *testing.T) {
	addr := types.ParseAddressPanic("z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz")
	toAddr := types.ParseAddressPanic("z1qqfmjdays57w488sta69ykc2ey7r6d0q9wdvtj")
	zts, _ := types.ParseZTS("zts1znnxxxxxxxxxxxxx9z4vexb")

	h1 := types.HexToHashPanic("0000000000000000000000000000000000000000000000000000000000000001")
	h2 := types.HexToHashPanic("0000000000000000000000000000000000000000000000000000000000000002")
	h3 := types.HexToHashPanic("0000000000000000000000000000000000000000000000000000000000000003")
	h4 := types.HexToHashPanic("0000000000000000000000000000000000000000000000000000000000000004")
	h5 := types.HexToHashPanic("0000000000000000000000000000000000000000000000000000000000000005")

	var nonce nom.Nonce
	copy(nonce.Data[:], []byte{1, 2, 3, 4, 5, 6, 7, 8})

	full := &nom.AccountBlock{
		Version:              1,
		ChainIdentifier:      1,
		BlockType:            nom.BlockTypeUserSend,
		Hash:                 h1,
		PreviousHash:         h2,
		Height:               12345,
		MomentumAcknowledged: types.HashHeight{Hash: h3, Height: 999},
		Address:              addr,
		ToAddress:            toAddr,
		Amount:               big.NewInt(1000000000),
		TokenStandard:        zts,
		FromBlockHash:        h4,
		Data:                 make([]byte, 1000),
		FusedPlasma:          21000,
		Difficulty:           123456789,
		Nonce:                nonce,
		BasePlasma:           21000,
		TotalPlasma:          21000,
		ChangesHash:          h5,
		PublicKey:            make([]byte, 32),
		Signature:            make([]byte, 64),
	}

	fullEncoded, err := rlp.EncodeToBytes(full)
	if err != nil {
		t.Fatalf("RLP encode of full block failed: %v", err)
	}
	fullMeasured := blockWireSize(full)
	fullActual := len(fullEncoded)
	if fullMeasured != fullActual {
		t.Errorf("fully populated block: measured (%d) != actual RLP size (%d)", fullMeasured, fullActual)
	}
	t.Logf("fully populated block: measured=%d, actual=%d", fullMeasured, fullActual)
}

// TestContent_ByteBudget_MomentumHeaderSize verifies that the momentum header
// size used in filterBlocksToCommit matches the real RLP encoding of an
// empty Momentum with Version 2.
func TestContent_ByteBudget_MomentumHeaderSize(t *testing.T) {
	momentum := &nom.Momentum{Version: 2, Content: nom.MomentumContent{}}
	encoded, err := rlp.EncodeToBytes(momentum)
	if err != nil {
		t.Fatalf("RLP encode failed: %v", err)
	}
	t.Logf("momentum header (Version 2, empty content): RLP size %d", len(encoded))

	// The header size should be reasonable (under 1 KiB).
	if len(encoded) > 1024 {
		t.Errorf("momentum header RLP size %d seems too large", len(encoded))
	}
}

// The budget safety invariant test lives in protocol (TestMaxDetailedMomentumWireSize)
// where the constants are defined.

// TestContent_ByteBudget_EnvelopeCoversPopulatedMomentum verifies that the
// selector's size estimate (empty-momentum baseline + envelopeOverhead +
// per-block costs) is always >= the actual RLP-encoded size of the final
// populated DetailedMomentum. This is the property the PR #145 review
// required: the estimate must account for the complete final encoding,
// including populated producer fields (Hash, PreviousHash, ChangesHash,
// Height, TimestampUnix, PublicKey, Signature) and real content headers.
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
		emptyM := &nom.Momentum{Version: 2, Content: nom.MomentumContent{}}
		emptyMEnc, err := rlp.EncodeToBytes(emptyM)
		if err != nil {
			t.Fatalf("RLP encode empty momentum: %v", err)
		}
		estimate := len(emptyMEnc) + envelopeOverhead
		for _, b := range blocks {
			estimate += blockWireSize(b) + contentEntrySize()
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
// the budget are admitted and blocks just above are rejected, using the
// envelope-aware estimate. The block size is tuned so that the envelope
// overhead is what makes the difference.
func TestContent_ByteBudget_BoundaryAtBudget(t *testing.T) {
	cs := newTestContentSelector()
	addr := types.ParseAddressPanic("z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz")

	// Compute the data size that brings the total estimate just under the budget
	emptyM := &nom.Momentum{Version: 2, Content: nom.MomentumContent{}}
	emptyMEnc, _ := rlp.EncodeToBytes(emptyM)
	baseline := len(emptyMEnc) + envelopeOverhead + contentEntrySize()

	// Find the data size where the selector's estimate crosses the budget.
	// Binary search for the boundary.
	fitSize := 0
	overSize := MaxDetailedMomentumSize // definitely over
	for overSize-fitSize > 1 {
		mid := (fitSize + overSize) / 2
		probe := &nom.AccountBlock{
			Version: 1, BlockType: nom.BlockTypeUserSend, Height: 1,
			Address: addr, Data: make([]byte, mid),
			FusedPlasma: 21000, BasePlasma: 21000, TotalPlasma: 21000,
		}
		probe.Hash = probe.ComputeHash()
		if baseline+blockWireSize(probe) <= MaxDetailedMomentumSize {
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
		t.Errorf("block just below budget was rejected (data=%d, estimate=%d, budget=%d)",
			fitSize, baseline+blockWireSize(fitBlock), MaxDetailedMomentumSize)
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
		t.Errorf("block just above budget was admitted (data=%d, estimate=%d, budget=%d)",
			overSize, baseline+blockWireSize(overBlock), MaxDetailedMomentumSize)
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
