package pillar

import (
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
