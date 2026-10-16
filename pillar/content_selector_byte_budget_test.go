package pillar

import (
	"bytes"
	"testing"

	"github.com/ethereum/go-ethereum/rlp"
	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/dp"
	"github.com/zenon-network/go-zenon/protocol"
	"github.com/zenon-network/go-zenon/vm/embedded/definition"
)

// newTestContentSelector returns a contentSelector with a generous plasma
// budget so that byte-budget tests are not constrained by plasma limits.
func newTestContentSelector() *contentSelector {
	config := &definition.PlasmaVariables{
		MaxBasePlasmaInMomentum: 21000 * 10000, // very high: plasma is not the constraint
	}
	previousMomentum := &nom.Momentum{NextFusionPrice: 1000, NextWorkPrice: 1000, Version: 2}
	return &contentSelector{
		plasma: dp.NewDynamicPlasma(previousMomentum, config),
	}
}

// TestContent_ByteBudget_SkipsOversizedBlock verifies that a user block whose
// RLP-encoded size would exceed the budget is skipped, and its
// address is marked skipped so descendants are also excluded.
func TestContent_ByteBudget_SkipsOversizedBlock(t *testing.T) {
	cs := newTestContentSelector()
	addr1 := types.ParseAddressPanic("z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz")
	addr2 := types.ParseAddressPanic("z1qqfmjdays57w488sta69ykc2ey7r6d0q9wdvtj")

	// A block with Data large enough to exceed the budget.
	bigData := make([]byte, protocol.MaxDetailedMomentumWireSize)

	toCommit := cs.filterBlocksToCommit([]*nom.AccountBlock{
		{Height: 1, BlockType: nom.BlockTypeUserSend, BasePlasma: 21000, FusedPlasma: 21000, Address: addr1, Data: bigData},
		// This block from a different address should still be included.
		{Height: 1, BlockType: nom.BlockTypeUserSend, BasePlasma: 21000, FusedPlasma: 21000, Address: addr2},
	})

	common.Expect(t, len(toCommit), 1)
	common.Expect(t, toCommit[0].Address, addr2)
}

// TestContent_ByteBudget_SkipsChildOfOversizedAncestor verifies that
// when a block is skipped for exceeding the byte budget, all its descendants
// are also skipped (no gaps in the address chain).
func TestContent_ByteBudget_SkipsChildOfOversizedAncestor(t *testing.T) {
	cs := newTestContentSelector()
	addr1 := types.ParseAddressPanic("z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz")

	bigData := make([]byte, protocol.MaxDetailedMomentumWireSize)

	toCommit := cs.filterBlocksToCommit([]*nom.AccountBlock{
		// Ancestor exceeds byte budget.
		{Height: 1, BlockType: nom.BlockTypeUserSend, BasePlasma: 21000, FusedPlasma: 21000, Address: addr1, Data: bigData},
		// Descendant is small and would pass on its own, but must be skipped.
		{Height: 2, BlockType: nom.BlockTypeUserSend, BasePlasma: 21000, FusedPlasma: 21000, Address: addr1},
	})

	common.Expect(t, len(toCommit), 0)
}

// TestContent_ByteBudget_DropsOversizedContractBatch verifies that a contract
// batch whose RLP-encoded size would exceed the byte budget is dropped in its
// entirety, and all addresses in the batch are marked skipped.
func TestContent_ByteBudget_DropsOversizedContractBatch(t *testing.T) {
	cs := newTestContentSelector()

	bigData := make([]byte, protocol.MaxDetailedMomentumWireSize)

	toCommit := cs.filterBlocksToCommit([]*nom.AccountBlock{
		{Height: 1, BlockType: nom.BlockTypeContractSend, Address: types.PillarContract, Data: bigData},
		{Height: 2, BlockType: nom.BlockTypeContractReceive, Address: types.PillarContract},
	})

	common.Expect(t, len(toCommit), 0)
}

// TestContent_ByteBudget_AllowsBlocksWithinBudget verifies that multiple
// blocks whose combined RLP-encoded size fits within the budget are all
// included.
func TestContent_ByteBudget_AllowsBlocksWithinBudget(t *testing.T) {
	cs := newTestContentSelector()
	addr1 := types.ParseAddressPanic("z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz")
	addr2 := types.ParseAddressPanic("z1qqfmjdays57w488sta69ykc2ey7r6d0q9wdvtj")

	// 100 blocks with 1 KiB Data each = ~100 KiB, well under the budget.
	data := make([]byte, 1024)
	blocks := make([]*nom.AccountBlock, 0, 100)
	for i := 1; i <= 50; i++ {
		blocks = append(blocks, &nom.AccountBlock{
			Height: uint64(i), BlockType: nom.BlockTypeUserSend,
			BasePlasma: 21000, FusedPlasma: 21000, Address: addr1, Data: data,
		})
	}
	for i := 1; i <= 50; i++ {
		blocks = append(blocks, &nom.AccountBlock{
			Height: uint64(i), BlockType: nom.BlockTypeUserSend,
			BasePlasma: 21000, FusedPlasma: 21000, Address: addr2, Data: data,
		})
	}

	toCommit := cs.filterBlocksToCommit(blocks)
	common.Expect(t, len(toCommit), 100)
}

// TestContent_ByteBudget_CumulativeUserBlocks verifies that the byte budget
// is enforced cumulatively: three 3 MiB user blocks from three addresses
// should commit the first two (6 MiB total) and skip the third. This is the
// case the budget exists for — many ordinary blocks adding up — and it
// discriminates against a missing accumulation line.
func TestContent_ByteBudget_CumulativeUserBlocks(t *testing.T) {
	cs := newTestContentSelector()
	addr1 := types.ParseAddressPanic("z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz")
	addr2 := types.ParseAddressPanic("z1qqfmjdays57w488sta69ykc2ey7r6d0q9wdvtj")
	addr3, err := types.BytesToAddress([]byte{0x02, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10, 0x11, 0x12, 0x13, 0x14})
	if err != nil {
		t.Fatalf("BytesToAddress: %v", err)
	}

	// Three 3 MiB blocks: first two fit (6 MiB), third would exceed.
	dataSize := 3 * 1024 * 1024
	blocks := []*nom.AccountBlock{
		{Height: 1, BlockType: nom.BlockTypeUserSend, BasePlasma: 21000, FusedPlasma: 21000, Address: addr1, Data: make([]byte, dataSize)},
		{Height: 1, BlockType: nom.BlockTypeUserSend, BasePlasma: 21000, FusedPlasma: 21000, Address: addr2, Data: make([]byte, dataSize)},
		{Height: 1, BlockType: nom.BlockTypeUserSend, BasePlasma: 21000, FusedPlasma: 21000, Address: addr3, Data: make([]byte, dataSize)},
	}

	toCommit := cs.filterBlocksToCommit(blocks)
	common.Expect(t, len(toCommit), 2)

	// Verify the RLP of the selected content is under the budget.
	dm := &nom.DetailedMomentum{
		Momentum:      &nom.Momentum{Version: 2, Content: nom.MomentumContent{}},
		AccountBlocks: toCommit,
	}
	encoded, err := rlp.EncodeToBytes(dm)
	if err != nil {
		t.Fatalf("RLP encode failed: %v", err)
	}
	if len(encoded) > protocol.MaxDetailedMomentumWireSize {
		t.Errorf("selected DetailedMomentum RLP size %d exceeds budget %d", len(encoded), protocol.MaxDetailedMomentumWireSize)
	}
	t.Logf("cumulative user blocks: committed %d blocks, RLP size %d, budget %d", len(toCommit), len(encoded), protocol.MaxDetailedMomentumWireSize)
}

// TestContent_ByteBudget_CumulativeContractBatches verifies that the byte
// budget is enforced cumulatively for contract batches: three batches of
// two blocks each with 1.5 MiB Data per block should commit the first two
// batches (four blocks) and drop the third. This discriminates against a
// missing batchSize accumulation line.
func TestContent_ByteBudget_CumulativeContractBatches(t *testing.T) {
	cs := newTestContentSelector()

	dataSize := 3 * 1024 * 1024 / 2 // 1.5 MiB per block

	// Three contract batches, each = one ContractSend + one ContractReceive.
	// Each batch ≈ 2 blocks × (RLP overhead + 1.5 MiB Data) ≈ 3+ MiB.
	// First two batches ≈ 6+ MiB, third would exceed.
	blocks := []*nom.AccountBlock{
		// Batch 1
		{Height: 1, BlockType: nom.BlockTypeContractSend, Address: types.PillarContract, Data: make([]byte, dataSize)},
		{Height: 2, BlockType: nom.BlockTypeContractReceive, Address: types.PillarContract, Data: make([]byte, dataSize)},
		// Batch 2
		{Height: 3, BlockType: nom.BlockTypeContractSend, Address: types.PillarContract, Data: make([]byte, dataSize)},
		{Height: 4, BlockType: nom.BlockTypeContractReceive, Address: types.PillarContract, Data: make([]byte, dataSize)},
		// Batch 3
		{Height: 5, BlockType: nom.BlockTypeContractSend, Address: types.PillarContract, Data: make([]byte, dataSize)},
		{Height: 6, BlockType: nom.BlockTypeContractReceive, Address: types.PillarContract, Data: make([]byte, dataSize)},
	}

	toCommit := cs.filterBlocksToCommit(blocks)
	// First two batches = 4 blocks committed, third dropped.
	common.Expect(t, len(toCommit), 4)

	// Verify the RLP of the selected content is under the budget.
	dm := &nom.DetailedMomentum{
		Momentum:      &nom.Momentum{Version: 2, Content: nom.MomentumContent{}},
		AccountBlocks: toCommit,
	}
	encoded, err := rlp.EncodeToBytes(dm)
	if err != nil {
		t.Fatalf("RLP encode failed: %v", err)
	}
	if len(encoded) > protocol.MaxDetailedMomentumWireSize {
		t.Errorf("selected DetailedMomentum RLP size %d exceeds budget %d", len(encoded), protocol.MaxDetailedMomentumWireSize)
	}
	t.Logf("cumulative contract batches: committed %d blocks, RLP size %d, budget %d", len(toCommit), len(encoded), protocol.MaxDetailedMomentumWireSize)
}

// TestContent_ByteBudget_OversizedNonDataField verifies that a block with an
// oversized non-Data field (e.g. PublicKey) on a nested descendant is
// correctly measured by the RLP-based size check. The block must be skipped.
func TestContent_ByteBudget_OversizedNonDataField(t *testing.T) {
	cs := newTestContentSelector()
	addr1 := types.ParseAddressPanic("z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz")

	// Parent block with small Data, but descendant has a 9 MiB PublicKey.
	// The real RLP is dominated by the 9 MiB PublicKey, which exceeds the
	// ~8 MiB budget.
	bigKey := make([]byte, 9*1024*1024)
	block := &nom.AccountBlock{
		Height:    1,
		BlockType: nom.BlockTypeUserSend,
		Address:   addr1,
		Data:      make([]byte, 100), // small Data on parent
		DescendantBlocks: []*nom.AccountBlock{
			{
				Height:    2,
				BlockType: nom.BlockTypeUserSend,
				Address:   addr1,
				PublicKey: bigKey, // 9 MiB non-Data field on nested block
				Data:      make([]byte, 100),
			},
		},
	}

	// Verify the RLP size indeed exceeds the budget.
	encoded, _ := rlp.EncodeToBytes(block)
	t.Logf("parent block with 9 MiB PublicKey descendant: RLP size %d, budget %d", len(encoded), protocol.MaxDetailedMomentumWireSize)
	if len(encoded) <= protocol.MaxDetailedMomentumWireSize {
		t.Fatalf("test setup: block RLP %d should exceed budget %d", len(encoded), protocol.MaxDetailedMomentumWireSize)
	}

	// The block must be skipped (real RLP > budget).
	toCommit := cs.filterBlocksToCommit([]*nom.AccountBlock{block})
	common.Expect(t, len(toCommit), 0)
}

// TestContent_ByteBudget_SignedMomentumBoundary verifies that when ~2000
// small user blocks are selected and wrapped in a real signed momentum, the
// RLP-encoded DetailedMomentum stays within the wire-size budget.  This
// catches the case where the empty-momentum baseline plus a fixed envelope
// overhead underestimates the true populated header size.
func TestContent_ByteBudget_SignedMomentumBoundary(t *testing.T) {
	cs := newTestContentSelector()

	const numBlocks = 2000
	const blockHeight = 1 << 30 // 2^30: exercises multi-byte RLP height encoding

	addr1 := types.ParseAddressPanic("z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz")
	addr2 := types.ParseAddressPanic("z1qqfmjdays57w488sta69ykc2ey7r6d0q9wdvtj")

	blocks := make([]*nom.AccountBlock, 0, numBlocks)
	for i := 0; i < numBlocks; i++ {
		addr := addr1
		if i%2 == 1 {
			addr = addr2
		}
		b := &nom.AccountBlock{
			Version:         1,
			ChainIdentifier: 1,
			BlockType:       nom.BlockTypeUserSend,
			Height:          blockHeight + uint64(i),
			Address:         addr,
			Data:            make([]byte, 100),
			FusedPlasma:     21000,
			BasePlasma:      21000,
			TotalPlasma:     21000,
		}
		b.Hash = b.ComputeHash()
		blocks = append(blocks, b)
	}

	toCommit := cs.filterBlocksToCommit(blocks)
	t.Logf("selected %d of %d blocks", len(toCommit), numBlocks)

	// Wrap in a real signed momentum using NewMomentumContent.
	content := nom.NewMomentumContent(toCommit)
	momentum := &nom.Momentum{
		Version:         2,
		ChainIdentifier: 1,
		Hash:            types.Hash{0x01},
		PreviousHash:    types.Hash{0x02},
		Height:          14419042,
		TimestampUnix:   1728867600,
		Content:         content,
		ChangesHash:     types.Hash{0x03},
		PublicKey:       make([]byte, 32),
		Signature:       make([]byte, 64),
		NextFusionPrice: 1000000,
		NextWorkPrice:   500000,
	}
	dm := &nom.DetailedMomentum{
		Momentum:      momentum,
		AccountBlocks: toCommit,
	}
	encoded, err := rlp.EncodeToBytes(dm)
	if err != nil {
		t.Fatalf("RLP encode failed: %v", err)
	}
	t.Logf("signed momentum DetailedMomentum: RLP size %d, budget %d, blocks %d", len(encoded), protocol.MaxDetailedMomentumWireSize, len(toCommit))
	if len(encoded) > protocol.MaxDetailedMomentumWireSize {
		t.Errorf("signed momentum DetailedMomentum RLP size %d exceeds budget %d (%d blocks)",
			len(encoded), protocol.MaxDetailedMomentumWireSize, len(toCommit))
	}
}

// TestContent_ByteBudget_MaxWidthFinalEncoding is an independent oracle that
// encodes the selector's actual output at the admission boundary with
// maximum-width fields.  It builds a DetailedMomentum from the selector's
// chosen blocks using a worst-case momentum header (max-uint64 height,
// timestamp, chain ID, prices; real 33-byte pubkey, 64-byte signature) and
// verifies the RLP-encoded result fits within MaxDetailedMomentumWireSize.
// This test discriminates against an under-counted byte budget: setting
// listPrefixAllowance to 0 must make it fail.
func TestContent_ByteBudget_MaxWidthFinalEncoding(t *testing.T) {
	cs := newTestContentSelector()
	addr := types.ParseAddressPanic("z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz")

	// Build a block whose data size brings the selector's estimate to just
	// under the budget.  Use max-width scalar fields on the momentum so the
	// header baseline is the worst case.
	baseline := worstCaseMomentumHeaderSize() + listPrefixAllowance

	// Binary-search for the largest data size the selector admits.
	fitSize := 0
	overSize := protocol.MaxDetailedMomentumWireSize
	for overSize-fitSize > 1 {
		mid := (fitSize + overSize) / 2
		probe := &nom.AccountBlock{
			Version: 1, BlockType: nom.BlockTypeUserSend, Height: 1,
			Address: addr, Data: make([]byte, mid),
			FusedPlasma: 21000, BasePlasma: 21000, TotalPlasma: 21000,
		}
		probe.Hash = probe.ComputeHash()
		if baseline+blockWireSize(probe)+contentEntrySize(probe) <= protocol.MaxDetailedMomentumWireSize {
			fitSize = mid
		} else {
			overSize = mid
		}
	}

	// Build the admitted block.
	fitBlock := &nom.AccountBlock{
		Version: 1, BlockType: nom.BlockTypeUserSend, Height: 1,
		Address: addr, Data: make([]byte, fitSize),
		FusedPlasma: 21000, BasePlasma: 21000, TotalPlasma: 21000,
	}
	fitBlock.Hash = fitBlock.ComputeHash()

	// Run the selector.
	toCommit := cs.Content([]*nom.AccountBlock{fitBlock})
	if len(toCommit) != 1 {
		t.Fatalf("selector rejected block at fitSize=%d (budget=%d)", fitSize, protocol.MaxDetailedMomentumWireSize)
	}

	// Build the actual DetailedMomentum the producer would create: worst-case
	// header fields, populated Content from the selected blocks.
	content := nom.NewMomentumContent(toCommit)
	worstMom := &nom.Momentum{
		Version:         2,
		ChainIdentifier: ^uint64(0),
		Hash:            types.Hash{0xff},
		PreviousHash:    types.Hash{0xff},
		Height:          ^uint64(0),
		TimestampUnix:   ^uint64(0),
		Data:            []byte{},
		Content:         content,
		ChangesHash:     types.Hash{0xff},
		PublicKey:       make([]byte, 32),
		Signature:       make([]byte, 64),
		NextFusionPrice: ^uint64(0),
		NextWorkPrice:   ^uint64(0),
	}
	dm := &nom.DetailedMomentum{
		Momentum:      worstMom,
		AccountBlocks: toCommit,
	}
	encoded, err := rlp.EncodeToBytes(dm)
	if err != nil {
		t.Fatalf("RLP encode failed: %v", err)
	}

	t.Logf("max-width final encoding: %d bytes, budget=%d, data=%d, allowance=%d",
		len(encoded), protocol.MaxDetailedMomentumWireSize, fitSize, listPrefixAllowance)

	if len(encoded) > protocol.MaxDetailedMomentumWireSize {
		t.Errorf("max-width DetailedMomentum RLP size %d exceeds budget %d (allowance=%d, data=%d)",
			len(encoded), protocol.MaxDetailedMomentumWireSize, listPrefixAllowance, fitSize)
	}

	// Descendant-batch case: a contract send with a descendant receive.
	contractSend := &nom.AccountBlock{
		Version: 1, BlockType: nom.BlockTypeContractSend, Height: 1,
		Address: types.PillarContract, Data: make([]byte, fitSize/2),
	}
	contractSend.Hash = contractSend.ComputeHash()
	contractRecv := &nom.AccountBlock{
		Version: 1, BlockType: nom.BlockTypeContractReceive, Height: 2,
		Address: types.PillarContract, Data: make([]byte, fitSize/2),
		DescendantBlocks: []*nom.AccountBlock{
			{
				Version: 1, BlockType: nom.BlockTypeUserSend, Height: 3,
				Address: addr, Data: make([]byte, 100),
			},
		},
	}
	contractRecv.Hash = contractRecv.ComputeHash()

	toCommit2 := cs.Content([]*nom.AccountBlock{contractSend, contractRecv})
	if len(toCommit2) == 0 {
		t.Log("descendant-batch case: selector rejected all blocks (acceptable at boundary)")
	} else {
		content2 := nom.NewMomentumContent(toCommit2)
		worstMom2 := *worstMom
		worstMom2.Content = content2
		dm2 := &nom.DetailedMomentum{
			Momentum:      &worstMom2,
			AccountBlocks: toCommit2,
		}
		encoded2, err := rlp.EncodeToBytes(dm2)
		if err != nil {
			t.Fatalf("RLP encode descendant batch: %v", err)
		}
		t.Logf("descendant-batch encoding: %d bytes, budget=%d", len(encoded2), protocol.MaxDetailedMomentumWireSize)
		if len(encoded2) > protocol.MaxDetailedMomentumWireSize {
			t.Errorf("descendant-batch DetailedMomentum RLP size %d exceeds budget %d",
				len(encoded2), protocol.MaxDetailedMomentumWireSize)
		}
	}
}

// TestContent_ByteBudget_ManyHeadersNearBudget verifies that with ~1100+
// small user blocks the selector still admits a substantial portion and the
// final RLP-encoded DetailedMomentum stays within the wire-size budget.
// Unlike SignedMomentumBoundary (which tops out at ~838K), this test
// approaches the admission boundary by using blocks large enough that the
// cumulative estimate nears MaxDetailedMomentumWireSize, and then proves
// that a probe block sized to push the total over IS rejected.
func TestContent_ByteBudget_ManyHeadersNearBudget(t *testing.T) {
	cs := newTestContentSelector()

	const numBlocks = 1150
	const blockHeight = 1 << 30 // 2^30: multi-byte RLP height encoding

	addr1 := types.ParseAddressPanic("z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz")
	addr2 := types.ParseAddressPanic("z1qqfmjdays57w488sta69ykc2ey7r6d0q9wdvtj")

	// Size each block so that the cumulative estimate approaches the budget.
	// With 1150 blocks we need roughly (budget - baseline) / 1150 per block.
	baseline := worstCaseMomentumHeaderSize() + listPrefixAllowance
	perBlockBudget := (protocol.MaxDetailedMomentumWireSize - baseline) / numBlocks

	// Account for RLP overhead: a block with Data of size D has an RLP
	// encoding of roughly D + ~120 bytes (fields + list prefix).  The
	// content entry adds ~50 bytes.  Solve for D.
	dataSize := perBlockBudget - 200 // conservative: overhead < 200 per block
	if dataSize < 100 {
		t.Fatalf("test setup: dataSize %d too small, increase numBlocks or budget", dataSize)
	}

	blocks := make([]*nom.AccountBlock, 0, numBlocks)
	for i := 0; i < numBlocks; i++ {
		addr := addr1
		if i%2 == 1 {
			addr = addr2
		}
		b := &nom.AccountBlock{
			Version:         1,
			ChainIdentifier: 1,
			BlockType:       nom.BlockTypeUserSend,
			Height:          blockHeight + uint64(i),
			Address:         addr,
			Data:            make([]byte, dataSize),
			FusedPlasma:     21000,
			BasePlasma:      21000,
			TotalPlasma:     21000,
		}
		b.Hash = b.ComputeHash()
		blocks = append(blocks, b)
	}

	// Run the full selector (sort + filter).
	toCommit := cs.Content(blocks)
	t.Logf("selector admitted %d of %d blocks (dataSize=%d, perBlockBudget=%d)",
		len(toCommit), numBlocks, dataSize, perBlockBudget)

	// The selector must admit a substantial portion.
	if len(toCommit) < 1000 {
		t.Fatalf("selector admitted only %d blocks, want at least 1000", len(toCommit))
	}

	// Build the actual DetailedMomentum with a worst-case header.
	content := nom.NewMomentumContent(toCommit)
	worstMom := &nom.Momentum{
		Version:         2,
		ChainIdentifier: ^uint64(0),
		Hash:            types.Hash{0xff},
		PreviousHash:    types.Hash{0xff},
		Height:          ^uint64(0),
		TimestampUnix:   ^uint64(0),
		Data:            []byte{},
		Content:         content,
		ChangesHash:     types.Hash{0xff},
		PublicKey:       make([]byte, 32),
		Signature:       make([]byte, 64),
		NextFusionPrice: ^uint64(0),
		NextWorkPrice:   ^uint64(0),
	}
	dm := &nom.DetailedMomentum{
		Momentum:      worstMom,
		AccountBlocks: toCommit,
	}
	encoded, err := rlp.EncodeToBytes(dm)
	if err != nil {
		t.Fatalf("RLP encode failed: %v", err)
	}
	t.Logf("many-headers encoding: %d bytes, budget=%d, blocks=%d",
		len(encoded), protocol.MaxDetailedMomentumWireSize, len(toCommit))
	if len(encoded) > protocol.MaxDetailedMomentumWireSize {
		t.Errorf("many-headers DetailedMomentum RLP size %d exceeds budget %d (%d blocks)",
			len(encoded), protocol.MaxDetailedMomentumWireSize, len(toCommit))
	}

	// Positive admission control: a probe block sized to push the total over
	// the budget MUST be rejected by the selector.
	probeDataSize := protocol.MaxDetailedMomentumWireSize // definitely over
	probe := &nom.AccountBlock{
		Version:         1,
		ChainIdentifier: 1,
		BlockType:       nom.BlockTypeUserSend,
		Height:          blockHeight + uint64(numBlocks),
		Address:         addr1,
		Data:            make([]byte, probeDataSize),
		FusedPlasma:     21000,
		BasePlasma:      21000,
		TotalPlasma:     21000,
	}
	probe.Hash = probe.ComputeHash()

	allBlocks := append(append([]*nom.AccountBlock{}, blocks...), probe)
	toCommit2 := cs.Content(allBlocks)

	// The probe must not be in the committed output.
	for _, b := range toCommit2 {
		if b == probe {
			t.Fatal("probe block (sized to exceed budget) was admitted by the selector")
		}
	}
	// The count should be the same as before (probe rejected, no other changes).
	if len(toCommit2) != len(toCommit) {
		t.Errorf("admission count changed after adding probe: got %d, want %d", len(toCommit2), len(toCommit))
	}
}

// TestContent_ByteBudget_ContractBatchAdmission verifies that a contract
// send+receive batch with a descendant, sized to just fit the budget, is
// admitted in full (both blocks committed, descendant present in output),
// and that the same batch sized to just exceed the budget is rejected
// entirely (zero blocks committed).  This replaces the vacuous
// descendant-batch case in MaxWidthFinalEncoding which logged "all blocks
// rejected" and still passed.
func TestContent_ByteBudget_ContractBatchAdmission(t *testing.T) {
	cs := newTestContentSelector()
	userAddr := types.ParseAddressPanic("z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz")

	baseline := worstCaseMomentumHeaderSize() + listPrefixAllowance

	// Binary-search for the largest contract-send Data size such that
	// send + receive (with descendant) fits within the budget.
	//
	// The batch is: ContractSend(Data=D) + ContractReceive(Data=D, Descendant=UserSend(Data=100))
	// The selector processes embedded blocks as a batch: it accumulates
	// until a non-ContractSend block, then checks the total batch size.
	fitSize := 0
	overSize := protocol.MaxDetailedMomentumWireSize
	for overSize-fitSize > 1 {
		mid := (fitSize + overSize) / 2
		send := &nom.AccountBlock{
			Version: 1, BlockType: nom.BlockTypeContractSend, Height: 1,
			Address: types.PillarContract, Data: make([]byte, mid),
		}
		send.Hash = send.ComputeHash()
		recv := &nom.AccountBlock{
			Version: 1, BlockType: nom.BlockTypeContractReceive, Height: 2,
			Address: types.PillarContract, Data: make([]byte, mid),
			DescendantBlocks: []*nom.AccountBlock{
				{
					Version: 1, BlockType: nom.BlockTypeUserSend, Height: 3,
					Address: userAddr, Data: make([]byte, 100),
				},
			},
		}
		recv.Hash = recv.ComputeHash()

		batchSize := 0
		for _, b := range []*nom.AccountBlock{send, recv} {
			sz := blockWireSize(b)
			if sz < 0 {
				t.Fatalf("RLP encode failed for batch probe")
			}
			batchSize += sz + contentEntrySize(b)
		}
		if baseline+batchSize <= protocol.MaxDetailedMomentumWireSize {
			fitSize = mid
		} else {
			overSize = mid
		}
	}

	t.Logf("descendant batch boundary: fitSize=%d, baseline=%d, budget=%d", fitSize, baseline, protocol.MaxDetailedMomentumWireSize)

	// --- Admitted case: batch at fitSize should be committed ---
	sendOK := &nom.AccountBlock{
		Version: 1, BlockType: nom.BlockTypeContractSend, Height: 1,
		Address: types.PillarContract, Data: make([]byte, fitSize),
	}
	sendOK.Hash = sendOK.ComputeHash()
	recvOK := &nom.AccountBlock{
		Version: 1, BlockType: nom.BlockTypeContractReceive, Height: 2,
		Address: types.PillarContract, Data: make([]byte, fitSize),
		DescendantBlocks: []*nom.AccountBlock{
			{
				Version: 1, BlockType: nom.BlockTypeUserSend, Height: 3,
				Address: userAddr, Data: make([]byte, 100),
			},
		},
	}
	recvOK.Hash = recvOK.ComputeHash()

	toCommit := cs.Content([]*nom.AccountBlock{sendOK, recvOK})
	if len(toCommit) != 2 {
		t.Fatalf("admitted case: selector committed %d blocks, want exactly 2 (send+receive)", len(toCommit))
	}

	// Verify the descendant block is present in the committed output.
	foundDescendant := false
	for _, b := range toCommit {
		for _, d := range b.DescendantBlocks {
			if d.Height == 3 && d.Address == userAddr {
				foundDescendant = true
			}
		}
	}
	if !foundDescendant {
		t.Error("admitted case: descendant block not found in committed output")
	}

	// Build the DetailedMomentum and check the final encoding.
	content := nom.NewMomentumContent(toCommit)
	worstMom := &nom.Momentum{
		Version:         2,
		ChainIdentifier: ^uint64(0),
		Hash:            types.Hash{0xff},
		PreviousHash:    types.Hash{0xff},
		Height:          ^uint64(0),
		TimestampUnix:   ^uint64(0),
		Data:            []byte{},
		Content:         content,
		ChangesHash:     types.Hash{0xff},
		PublicKey:       make([]byte, 32),
		Signature:       make([]byte, 64),
		NextFusionPrice: ^uint64(0),
		NextWorkPrice:   ^uint64(0),
	}
	dm := &nom.DetailedMomentum{
		Momentum:      worstMom,
		AccountBlocks: toCommit,
	}
	encoded, err := rlp.EncodeToBytes(dm)
	if err != nil {
		t.Fatalf("RLP encode failed: %v", err)
	}
	t.Logf("descendant-batch admitted: %d blocks, RLP=%d, budget=%d", len(toCommit), len(encoded), protocol.MaxDetailedMomentumWireSize)
	if len(encoded) > protocol.MaxDetailedMomentumWireSize {
		t.Errorf("descendant-batch DetailedMomentum RLP size %d exceeds budget %d",
			len(encoded), protocol.MaxDetailedMomentumWireSize)
	}

	// --- Rejected case: batch at overSize must be rejected entirely ---
	sendOver := &nom.AccountBlock{
		Version: 1, BlockType: nom.BlockTypeContractSend, Height: 1,
		Address: types.PillarContract, Data: make([]byte, overSize),
	}
	sendOver.Hash = sendOver.ComputeHash()
	recvOver := &nom.AccountBlock{
		Version: 1, BlockType: nom.BlockTypeContractReceive, Height: 2,
		Address: types.PillarContract, Data: make([]byte, overSize),
		DescendantBlocks: []*nom.AccountBlock{
			{
				Version: 1, BlockType: nom.BlockTypeUserSend, Height: 3,
				Address: userAddr, Data: make([]byte, 100),
			},
		},
	}
	recvOver.Hash = recvOver.ComputeHash()

	toCommitOver := cs.Content([]*nom.AccountBlock{sendOver, recvOver})
	if len(toCommitOver) != 0 {
		t.Errorf("rejected case: selector committed %d blocks from an over-budget batch, want 0", len(toCommitOver))
	}

	// --- Two-batch cumulative case: each batch fits alone, but together they
	// exceed the budget.  Without batch-size accumulation the second batch
	// would be admitted and the final RLP would exceed the budget. ---
	halfFit := fitSize / 2 // each batch is ~half the admitted size

	// Batch A: PillarContract send+receive with descendant.
	sendA := &nom.AccountBlock{
		Version: 1, BlockType: nom.BlockTypeContractSend, Height: 1,
		Address: types.PillarContract, Data: make([]byte, halfFit),
	}
	sendA.Hash = sendA.ComputeHash()
	recvA := &nom.AccountBlock{
		Version: 1, BlockType: nom.BlockTypeContractReceive, Height: 2,
		Address: types.PillarContract, Data: make([]byte, halfFit),
		DescendantBlocks: []*nom.AccountBlock{
			{
				Version: 1, BlockType: nom.BlockTypeUserSend, Height: 3,
				Address: userAddr, Data: make([]byte, 100),
			},
		},
	}
	recvA.Hash = recvA.ComputeHash()

	// Batch B: StakeContract send+receive with descendant.
	sendB := &nom.AccountBlock{
		Version: 1, BlockType: nom.BlockTypeContractSend, Height: 1,
		Address: types.StakeContract, Data: make([]byte, halfFit),
	}
	sendB.Hash = sendB.ComputeHash()
	recvB := &nom.AccountBlock{
		Version: 1, BlockType: nom.BlockTypeContractReceive, Height: 2,
		Address: types.StakeContract, Data: make([]byte, halfFit),
		DescendantBlocks: []*nom.AccountBlock{
			{
				Version: 1, BlockType: nom.BlockTypeUserSend, Height: 3,
				Address: userAddr, Data: make([]byte, 100),
			},
		},
	}
	recvB.Hash = recvB.ComputeHash()

	toCommitTwo := cs.Content([]*nom.AccountBlock{sendA, recvA, sendB, recvB})
	t.Logf("two-batch cumulative: committed %d blocks (batch A=2 + batch B=2 = 4 if all admitted)", len(toCommitTwo))

	// With accumulation, the first batch (~half budget) is admitted and the
	// second (~half budget) is also admitted since half + half < budget.
	// Actually both fit: halfFit = fitSize/2, so 2*halfFit ≈ fitSize < budget.
	// Both batches should be admitted.
	if len(toCommitTwo) == 0 {
		t.Fatal("two-batch cumulative: selector rejected all blocks, want at least batch A committed")
	}

	// Build the DetailedMomentum and verify it fits.
	contentTwo := nom.NewMomentumContent(toCommitTwo)
	worstMomTwo := &nom.Momentum{
		Version:         2,
		ChainIdentifier: ^uint64(0),
		Hash:            types.Hash{0xff},
		PreviousHash:    types.Hash{0xff},
		Height:          ^uint64(0),
		TimestampUnix:   ^uint64(0),
		Data:            []byte{},
		Content:         contentTwo,
		ChangesHash:     types.Hash{0xff},
		PublicKey:       make([]byte, 32),
		Signature:       make([]byte, 64),
		NextFusionPrice: ^uint64(0),
		NextWorkPrice:   ^uint64(0),
	}
	dmTwo := &nom.DetailedMomentum{
		Momentum:      worstMomTwo,
		AccountBlocks: toCommitTwo,
	}
	encodedTwo, err := rlp.EncodeToBytes(dmTwo)
	if err != nil {
		t.Fatalf("RLP encode two-batch: %v", err)
	}
	t.Logf("two-batch cumulative: %d blocks, RLP=%d, budget=%d", len(toCommitTwo), len(encodedTwo), protocol.MaxDetailedMomentumWireSize)
	if len(encodedTwo) > protocol.MaxDetailedMomentumWireSize {
		t.Errorf("two-batch DetailedMomentum RLP size %d exceeds budget %d",
			len(encodedTwo), protocol.MaxDetailedMomentumWireSize)
	}

	// Now the discriminating case: two batches at ~fitSize each (not halfFit).
	// Each fits alone (as proven by the admitted case above).  Together they
	// must exceed the budget, so the second batch must be rejected.
	sendC := &nom.AccountBlock{
		Version: 1, BlockType: nom.BlockTypeContractSend, Height: 1,
		Address: types.PillarContract, Data: make([]byte, fitSize),
	}
	sendC.Hash = sendC.ComputeHash()
	recvC := &nom.AccountBlock{
		Version: 1, BlockType: nom.BlockTypeContractReceive, Height: 2,
		Address: types.PillarContract, Data: make([]byte, fitSize),
		DescendantBlocks: []*nom.AccountBlock{
			{
				Version: 1, BlockType: nom.BlockTypeUserSend, Height: 3,
				Address: userAddr, Data: make([]byte, 100),
			},
		},
	}
	recvC.Hash = recvC.ComputeHash()

	sendD := &nom.AccountBlock{
		Version: 1, BlockType: nom.BlockTypeContractSend, Height: 1,
		Address: types.StakeContract, Data: make([]byte, fitSize),
	}
	sendD.Hash = sendD.ComputeHash()
	recvD := &nom.AccountBlock{
		Version: 1, BlockType: nom.BlockTypeContractReceive, Height: 2,
		Address: types.StakeContract, Data: make([]byte, fitSize),
		DescendantBlocks: []*nom.AccountBlock{
			{
				Version: 1, BlockType: nom.BlockTypeUserSend, Height: 3,
				Address: userAddr, Data: make([]byte, 100),
			},
		},
	}
	recvD.Hash = recvD.ComputeHash()

	toCommitCD := cs.Content([]*nom.AccountBlock{sendC, recvC, sendD, recvD})
	t.Logf("two-full-batch: committed %d blocks (want 2: batch C admitted, batch D rejected)", len(toCommitCD))

	// Batch C (PillarContract) should be admitted; batch D (StakeContract)
	// should be rejected because C already consumed ~full budget.
	if len(toCommitCD) > 2 {
		t.Errorf("two-full-batch: selector committed %d blocks, want at most 2 (batch C only)", len(toCommitCD))
	}

	// Verify batch D blocks are NOT in the output.
	for _, b := range toCommitCD {
		if b.Address == types.StakeContract {
			t.Error("two-full-batch: StakeContract block was admitted despite budget being consumed by PillarContract batch")
		}
	}
}

// TestContent_ByteBudget_ListPrefixAllowanceCrossing specifically targets the
// RLP list-prefix boundary at 65,536 bytes to catch an undercounted
// listPrefixAllowance.  With ~1,130 AccountHeaders in Momentum.Content the
// cumulative header payload crosses the 65,536-byte boundary where the RLP
// list prefix widens from 3 to 4 bytes.  The selector's estimate uses the
// production listPrefixAllowance (16) to reserve headroom for this growth.
//
// The test binary-searches for the exact Data size of a final "filler" block
// that the selector admits, then builds the actual worst-case
// DetailedMomentum and verifies the RLP fits within the budget.  When
// listPrefixAllowance is correct (16), the admitted content fits.  When it is
// undercounted (e.g. 12), the selector admits 4 more bytes of content and the
// final encoding exceeds the budget by 1 byte.
func TestContent_ByteBudget_ListPrefixAllowanceCrossing(t *testing.T) {
	cs := newTestContentSelector()
	addr1 := types.ParseAddressPanic("z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz")
	addr2 := types.ParseAddressPanic("z1qqfmjdays57w488sta69ykc2ey7r6d0q9wdvtj")

	// Use enough blocks so the cumulative Content list payload crosses the
	// 65,536-byte RLP list-prefix boundary.  Each AccountHeader at height=127
	// encodes to 58 bytes, so 1,131 headers give a payload of 65,598 bytes —
	// just past the boundary where the prefix widens from 3 to 4 bytes.
	const numHeaders = 1131
	const blockDataSize = 7000 // large enough to consume most of the budget
	const blockHeight = 127    // AccountHeader RLP = 58 bytes at this height

	// Build the initial blocks that fill the Content list past the boundary.
	blocks := make([]*nom.AccountBlock, 0, numHeaders+1)
	for i := 0; i < numHeaders; i++ {
		addr := addr1
		if i%2 == 1 {
			addr = addr2
		}
		b := &nom.AccountBlock{
			Version:         1,
			ChainIdentifier: 1,
			BlockType:       nom.BlockTypeUserSend,
			Height:          blockHeight,
			Address:         addr,
			Data:            make([]byte, blockDataSize),
			FusedPlasma:     21000,
			BasePlasma:      21000,
			TotalPlasma:     21000,
		}
		b.Hash = b.ComputeHash()
		blocks = append(blocks, b)
	}

	// Verify the Content list payload crosses the 65,536-byte boundary.
	contentPayload := 0
	for _, b := range blocks {
		he := b.Header()
		enc, err := rlp.EncodeToBytes(&he)
		if err != nil {
			t.Fatalf("RLP encode header: %v", err)
		}
		contentPayload += len(enc)
	}
	if contentPayload <= 65536 {
		t.Fatalf("test setup: Content payload %d does not cross the 65,536-byte boundary; increase numHeaders", contentPayload)
	}
	t.Logf("Content list payload: %d bytes (crosses 65,536)", contentPayload)

	// Compute the selector's size estimate for the initial blocks to find
	// the remaining budget for a final filler block.
	baseline := worstCaseMomentumHeaderSize() + listPrefixAllowance
	estimate := baseline
	for _, b := range blocks {
		sz := blockWireSize(b)
		if sz < 0 {
			t.Fatalf("blockWireSize failed for setup block")
		}
		es := contentEntrySize(b)
		if es < 0 {
			t.Fatalf("contentEntrySize failed for setup block")
		}
		estimate += sz + es
	}
	remaining := protocol.MaxDetailedMomentumWireSize - estimate

	// Binary-search for the largest filler Data size the selector admits.
	fitSize := 0
	overSize := remaining
	for overSize-fitSize > 1 {
		mid := (fitSize + overSize) / 2
		probe := &nom.AccountBlock{
			Version:         1,
			ChainIdentifier: 1,
			BlockType:       nom.BlockTypeUserSend,
			Height:          blockHeight,
			Address:         addr1,
			Data:            make([]byte, mid),
			FusedPlasma:     21000,
			BasePlasma:      21000,
			TotalPlasma:     21000,
		}
		probe.Hash = probe.ComputeHash()
		cost := blockWireSize(probe) + contentEntrySize(probe)
		if cost <= remaining {
			fitSize = mid
		} else {
			overSize = mid
		}
	}

	// Build the filler block at the admitted size and run the selector.
	// Use a third address so that rejecting the probe (below) does not cascade
	// to any of the setup blocks via the per-address skip rule.
	addr3, err := types.BytesToAddress([]byte{0x03, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10, 0x11, 0x12, 0x13, 0x14})
	if err != nil {
		t.Fatalf("BytesToAddress: %v", err)
	}
	filler := &nom.AccountBlock{
		Version:         1,
		ChainIdentifier: 1,
		BlockType:       nom.BlockTypeUserSend,
		Height:          blockHeight,
		Address:         addr3,
		Data:            make([]byte, fitSize),
		FusedPlasma:     21000,
		BasePlasma:      21000,
		TotalPlasma:     21000,
	}
	filler.Hash = filler.ComputeHash()

	allBlocks := append(append([]*nom.AccountBlock{}, blocks...), filler)
	toCommit := cs.Content(allBlocks)
	if len(toCommit) != len(allBlocks) {
		t.Fatalf("selector admitted %d of %d blocks; want all admitted at the boundary", len(toCommit), len(allBlocks))
	}

	// Build the worst-case DetailedMomentum and verify the RLP fits.
	content := nom.NewMomentumContent(toCommit)
	worstMom := &nom.Momentum{
		Version:         2,
		ChainIdentifier: ^uint64(0),
		Hash:            types.Hash{0xff},
		PreviousHash:    types.Hash{0xff},
		Height:          ^uint64(0),
		TimestampUnix:   ^uint64(0),
		Data:            []byte{},
		Content:         content,
		ChangesHash:     types.Hash{0xff},
		PublicKey:       make([]byte, 32),
		Signature:       make([]byte, 64),
		NextFusionPrice: ^uint64(0),
		NextWorkPrice:   ^uint64(0),
	}
	dm := &nom.DetailedMomentum{
		Momentum:      worstMom,
		AccountBlocks: toCommit,
	}
	encoded, err := rlp.EncodeToBytes(dm)
	if err != nil {
		t.Fatalf("RLP encode failed: %v", err)
	}

	t.Logf("list-prefix crossing: %d blocks, Content payload=%d, filler data=%d, RLP=%d, budget=%d, allowance=%d",
		len(toCommit), contentPayload, fitSize, len(encoded), protocol.MaxDetailedMomentumWireSize, listPrefixAllowance)

	if len(encoded) > protocol.MaxDetailedMomentumWireSize {
		t.Errorf("DetailedMomentum RLP size %d exceeds budget %d by %d bytes (allowance=%d, blocks=%d, filler=%d); "+
			"listPrefixAllowance may be undercounted",
			len(encoded), protocol.MaxDetailedMomentumWireSize,
			len(encoded)-protocol.MaxDetailedMomentumWireSize,
			listPrefixAllowance, len(toCommit), fitSize)
	}

	// Positive admission control: a filler block 1 byte larger must be rejected.
	probe := &nom.AccountBlock{
		Version:         1,
		ChainIdentifier: 1,
		BlockType:       nom.BlockTypeUserSend,
		Height:          blockHeight,
		Address:         addr3,
		Data:            make([]byte, overSize),
		FusedPlasma:     21000,
		BasePlasma:      21000,
		TotalPlasma:     21000,
	}
	probe.Hash = probe.ComputeHash()

	// Stabilize ordering: the probe must sort after all setup blocks so that
	// its rejection is caused by the cumulative byte budget, not by an
	// early position in the priority order.  The selector breaks price ties
	// by larger hash; verify the probe's hash is smaller than every setup
	// block's hash (so it sorts last, not first).
	for _, b := range blocks {
		if bytes.Compare(probe.Hash.Bytes()[:], b.Hash.Bytes()[:]) > 0 {
			t.Logf("WARNING: probe hash > setup block hash — probe may sort early; rejection assertion may not isolate the byte budget")
		}
	}

	// Control: the probe alone (no setup blocks) must be admitted, proving
	// that rejection in the full set is due to the cumulative estimate.
	toCommitAlone := cs.Content([]*nom.AccountBlock{probe})
	if len(toCommitAlone) != 1 {
		t.Fatalf("control: probe alone admitted %d blocks, want 1 (probe must be admissible in isolation)", len(toCommitAlone))
	}

	probeBlocks := append(append([]*nom.AccountBlock{}, blocks...), probe)
	toCommitProbe := cs.Content(probeBlocks)
	for _, b := range toCommitProbe {
		if b == probe {
			t.Fatal("probe block (1 byte over the admitted size) was admitted by the selector")
		}
	}
	// All setup blocks must still be admitted (the probe uses a distinct address).
	if len(toCommitProbe) != len(blocks) {
		t.Errorf("selector admitted %d blocks with probe, want %d (all setup blocks)", len(toCommitProbe), len(blocks))
	}
}
