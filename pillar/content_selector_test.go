package pillar

import (
	"reflect"
	"testing"

	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/dp"
	"github.com/zenon-network/go-zenon/vm/embedded/definition"
)

func TestContent_filterBlocksToCommit(t *testing.T) {
	config := &definition.PlasmaVariables{
		MaxBasePlasmaInMomentum: 21000 * 5,
	}
	previousMomentum := &nom.Momentum{NextFusionPrice: 1000, NextWorkPrice: 1000, Version: 2}
	cs := &contentSelector{
		plasma: dp.NewDynamicPlasma(previousMomentum, config),
	}

	common.Expect(t, len(cs.filterBlocksToCommit([]*nom.AccountBlock{
		{Height: 1, BlockType: nom.BlockTypeUserSend, BasePlasma: 21000, FusedPlasma: 21000},
		{Height: 2, BlockType: nom.BlockTypeUserSend, BasePlasma: 21000, FusedPlasma: 21000},
		{Height: 3, BlockType: nom.BlockTypeUserSend, BasePlasma: 21000, FusedPlasma: 21000},
		{Height: 4, BlockType: nom.BlockTypeUserSend, BasePlasma: 21000, FusedPlasma: 21000},
		{Height: 5, BlockType: nom.BlockTypeUserSend, BasePlasma: 21000, FusedPlasma: 21000},
		{Height: 6, BlockType: nom.BlockTypeUserSend, BasePlasma: 21000, FusedPlasma: 21000},
	})), 5)

	common.Expect(t, len(cs.filterBlocksToCommit([]*nom.AccountBlock{
		{Height: 1, BlockType: nom.BlockTypeUserSend, BasePlasma: 21000, FusedPlasma: 21000},
		{Height: 2, BlockType: nom.BlockTypeUserSend, BasePlasma: 31500, FusedPlasma: 31500},
		{Height: 3, BlockType: nom.BlockTypeUserSend, BasePlasma: 21000, FusedPlasma: 21000},
		{Height: 4, BlockType: nom.BlockTypeUserSend, BasePlasma: 21000, FusedPlasma: 21000},
		{Height: 5, BlockType: nom.BlockTypeUserSend, BasePlasma: 21000, FusedPlasma: 21000},
	})), 4)

	common.Expect(t, len(cs.filterBlocksToCommit([]*nom.AccountBlock{
		{Height: 1, BlockType: nom.BlockTypeContractSend, Address: types.PillarContract},
		{Height: 2, BlockType: nom.BlockTypeContractReceive, Address: types.PillarContract},
	})), 2)

	common.Expect(t, len(cs.filterBlocksToCommit([]*nom.AccountBlock{
		{Height: 1, BlockType: nom.BlockTypeContractSend, Address: types.PillarContract},
		{Height: 2, BlockType: nom.BlockTypeContractSend, Address: types.PillarContract},
		{Height: 3, BlockType: nom.BlockTypeContractReceive, Address: types.PillarContract},
	})), 0)
}

func TestContent_filterBlocksToCommit_SkipsDescendantsOfSkippedAncestor(t *testing.T) {
	address1 := types.ParseAddressPanic("z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz")

	config := &definition.PlasmaVariables{
		MaxBasePlasmaInMomentum: 21000 * 5,
	}
	previousMomentum := &nom.Momentum{NextFusionPrice: 1000, NextWorkPrice: 1000, Version: 2}
	cs := &contentSelector{
		plasma: dp.NewDynamicPlasma(previousMomentum, config),
	}

	toCommit := cs.filterBlocksToCommit([]*nom.AccountBlock{
		// Ancestor is underpriced and gets skipped.
		{Height: 1, BlockType: nom.BlockTypeUserSend, BasePlasma: 21000, FusedPlasma: 0, Difficulty: 0, Address: address1},
		// Descendant is properly priced and would pass on its own, but must also be
		// skipped so the committed blocks don't have a gap in address1's chain.
		{Height: 2, BlockType: nom.BlockTypeUserSend, BasePlasma: 21000, FusedPlasma: 21000, Address: address1},
	})

	common.Expect(t, len(toCommit), 0)
}

// When an oversized contract batch spanning multiple addresses is dropped,
// every address in that batch must be marked skipped, not just the
// terminating block's address - otherwise a later, smaller batch for one of
// the dropped addresses could commit while earlier heights from the same
// drop are missing.
func TestContent_filterBlocksToCommit_DroppedContractBatchSkipsAllAddresses(t *testing.T) {
	config := &definition.PlasmaVariables{
		// MaxContractBlocksInMomentum() == 105000 / EmbeddedSimplePlasma(52500) == 2
		MaxBasePlasmaInMomentum: 105000,
	}
	previousMomentum := &nom.Momentum{NextFusionPrice: 1000, NextWorkPrice: 1000, Version: 2}
	cs := &contentSelector{
		plasma: dp.NewDynamicPlasma(previousMomentum, config),
	}

	toCommit := cs.filterBlocksToCommit([]*nom.AccountBlock{
		// Batch of 3 embedded blocks spanning two addresses exceeds the
		// limit of 2 and is dropped in its entirety.
		{Height: 1, BlockType: nom.BlockTypeContractSend, Address: types.PillarContract},
		{Height: 1, BlockType: nom.BlockTypeContractSend, Address: types.SentinelContract},
		{Height: 2, BlockType: nom.BlockTypeContractReceive, Address: types.SentinelContract},
		// A later, smaller batch for one of the dropped addresses would fit
		// within the limit on its own, but must still be skipped since an
		// earlier height for that address was already dropped.
		{Height: 2, BlockType: nom.BlockTypeContractReceive, Address: types.PillarContract},
	})

	common.Expect(t, len(toCommit), 0)
}

func TestContent_sortBlocksByPriority(t *testing.T) {
	address1 := types.ParseAddressPanic("z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz")
	address2 := types.ParseAddressPanic("z1qqfmjdays57w488sta69ykc2ey7r6d0q9wdvtj")
	address3 := types.ParseAddressPanic("z1qqdt06lnwz57x38rwlyutcx5wgrtl0ynkfe3kv")
	address4 := types.ParseAddressPanic("z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx")

	previousMomentum := &nom.Momentum{NextFusionPrice: 1000, NextWorkPrice: 1000, Version: 2}
	cs := &contentSelector{
		plasma: dp.NewDynamicPlasma(previousMomentum, nil),
	}

	// Contract blocks
	common.ExpectTrue(t, reflect.DeepEqual(cs.sortBlocksByPriority([]*nom.AccountBlock{
		{Height: 1, BlockType: nom.BlockTypeContractSend, Address: types.PillarContract},
		{Height: 2, BlockType: nom.BlockTypeContractSend, Address: types.SentinelContract},
		{Height: 3, BlockType: nom.BlockTypeContractSend, Address: types.AcceleratorContract},
		{Height: 4, BlockType: nom.BlockTypeContractSend, Address: types.PlasmaContract},
	}), []*nom.AccountBlock{
		{Height: 1, BlockType: nom.BlockTypeContractSend, Address: types.PillarContract},
		{Height: 2, BlockType: nom.BlockTypeContractSend, Address: types.SentinelContract},
		{Height: 3, BlockType: nom.BlockTypeContractSend, Address: types.AcceleratorContract},
		{Height: 4, BlockType: nom.BlockTypeContractSend, Address: types.PlasmaContract},
	}))

	// Contract and user blocks
	common.ExpectTrue(t, reflect.DeepEqual(cs.sortBlocksByPriority([]*nom.AccountBlock{
		{Height: 1, BlockType: nom.BlockTypeContractSend, Address: types.PillarContract},
		{Height: 2, BlockType: nom.BlockTypeUserSend, BasePlasma: 21000, FusedPlasma: 21000, Address: address1},
		{Height: 3, BlockType: nom.BlockTypeContractSend, Address: types.PillarContract},
		{Height: 4, BlockType: nom.BlockTypeContractSend, Address: types.PillarContract},
	}), []*nom.AccountBlock{
		{Height: 1, BlockType: nom.BlockTypeContractSend, Address: types.PillarContract},
		{Height: 3, BlockType: nom.BlockTypeContractSend, Address: types.PillarContract},
		{Height: 4, BlockType: nom.BlockTypeContractSend, Address: types.PillarContract},
		{Height: 2, BlockType: nom.BlockTypeUserSend, BasePlasma: 21000, FusedPlasma: 21000, Address: address1},
	}))

	// User blocks: same address
	common.ExpectTrue(t, reflect.DeepEqual(cs.sortBlocksByPriority([]*nom.AccountBlock{
		{Height: 1, BlockType: nom.BlockTypeUserSend, BasePlasma: 21000, FusedPlasma: 21000, Address: address1},
		{Height: 2, BlockType: nom.BlockTypeUserSend, BasePlasma: 21000, FusedPlasma: 31500, Address: address1},
		{Height: 3, BlockType: nom.BlockTypeUserSend, BasePlasma: 21000, FusedPlasma: 42000, Address: address1},
		{Height: 4, BlockType: nom.BlockTypeUserSend, BasePlasma: 21000, FusedPlasma: 21000, Address: address2},
	}), []*nom.AccountBlock{
		{Height: 1, BlockType: nom.BlockTypeUserSend, BasePlasma: 21000, FusedPlasma: 21000, Address: address1},
		{Height: 2, BlockType: nom.BlockTypeUserSend, BasePlasma: 21000, FusedPlasma: 31500, Address: address1},
		{Height: 3, BlockType: nom.BlockTypeUserSend, BasePlasma: 21000, FusedPlasma: 42000, Address: address1},
		{Height: 4, BlockType: nom.BlockTypeUserSend, BasePlasma: 21000, FusedPlasma: 21000, Address: address2},
	}))

	// User blocks: plasma amount
	common.ExpectTrue(t, reflect.DeepEqual(cs.sortBlocksByPriority([]*nom.AccountBlock{
		{Height: 1, BlockType: nom.BlockTypeUserSend, BasePlasma: 21000, FusedPlasma: 21000, Address: address1},
		{Height: 2, BlockType: nom.BlockTypeUserSend, BasePlasma: 21000, FusedPlasma: 21000, Address: address2},
		{Height: 3, BlockType: nom.BlockTypeUserSend, BasePlasma: 21000, FusedPlasma: 21002, Address: address3},
		{Height: 4, BlockType: nom.BlockTypeUserSend, BasePlasma: 21000, FusedPlasma: 21001, Address: address4},
	}), []*nom.AccountBlock{
		{Height: 3, BlockType: nom.BlockTypeUserSend, BasePlasma: 21000, FusedPlasma: 21002, Address: address3},
		{Height: 4, BlockType: nom.BlockTypeUserSend, BasePlasma: 21000, FusedPlasma: 21001, Address: address4},
		{Height: 1, BlockType: nom.BlockTypeUserSend, BasePlasma: 21000, FusedPlasma: 21000, Address: address1},
		{Height: 2, BlockType: nom.BlockTypeUserSend, BasePlasma: 21000, FusedPlasma: 21000, Address: address2},
	}))
}

// A cheap low-height block and an expensive high-height block from the
// same address must never be separated by a third address' block priced
// between them: a per-block comparator that mixes a same-address height
// rule with a cross-address price rule admits exactly this cycle
// (x1 < x2 by height, x2 < y by price, y < x1 by price), which a stable
// sort over individual blocks can resolve into an order with x2 before
// x1 - a gap in address X's chain that the producer's own chain-order
// check would then reject the whole momentum for.
func TestContent_sortBlocksByPriority_NoGapAcrossThirdAddress(t *testing.T) {
	addressX := types.ParseAddressPanic("z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz")
	addressY := types.ParseAddressPanic("z1qqfmjdays57w488sta69ykc2ey7r6d0q9wdvtj")

	previousMomentum := &nom.Momentum{NextFusionPrice: 1000, NextWorkPrice: 1000, Version: 2}
	cs := &contentSelector{
		plasma: dp.NewDynamicPlasma(previousMomentum, nil),
	}

	x1 := &nom.AccountBlock{Height: 1, BlockType: nom.BlockTypeUserSend, BasePlasma: 21000, FusedPlasma: 21000, Address: addressX}
	x2 := &nom.AccountBlock{Height: 2, BlockType: nom.BlockTypeUserSend, BasePlasma: 21000, FusedPlasma: 21002, Address: addressX}
	y := &nom.AccountBlock{Height: 1, BlockType: nom.BlockTypeUserSend, BasePlasma: 21000, FusedPlasma: 21001, Address: addressY}

	sorted := cs.sortBlocksByPriority([]*nom.AccountBlock{x2, y, x1})

	indexOf := func(target *nom.AccountBlock) int {
		for i, b := range sorted {
			if b == target {
				return i
			}
		}
		t.Fatalf("block at height %d for %v missing from sorted output", target.Height, target.Address)
		return -1
	}
	common.ExpectTrue(t, indexOf(x1) < indexOf(x2))
}

// A block outranks another address' block on its own price, while its own
// ancestors still precede it.
func TestContent_sortBlocksByPriority_PremiumHeadDoesNotEscortFloorPricedTail(t *testing.T) {
	addressX := types.ParseAddressPanic("z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz")
	addressY := types.ParseAddressPanic("z1qqfmjdays57w488sta69ykc2ey7r6d0q9wdvtj")

	previousMomentum := &nom.Momentum{NextFusionPrice: 1000, NextWorkPrice: 1000, Version: 2}
	cs := &contentSelector{
		plasma: dp.NewDynamicPlasma(previousMomentum, nil),
	}

	x1 := &nom.AccountBlock{Height: 1, BlockType: nom.BlockTypeUserSend, BasePlasma: 21000, FusedPlasma: 42000, Address: addressX}
	x2 := &nom.AccountBlock{Height: 2, BlockType: nom.BlockTypeUserSend, BasePlasma: 21000, FusedPlasma: 21000, Address: addressX}
	x3 := &nom.AccountBlock{Height: 3, BlockType: nom.BlockTypeUserSend, BasePlasma: 21000, FusedPlasma: 21000, Address: addressX}
	y := &nom.AccountBlock{Height: 1, BlockType: nom.BlockTypeUserSend, BasePlasma: 21000, FusedPlasma: 31500, Address: addressY}

	common.ExpectTrue(t, reflect.DeepEqual(cs.sortBlocksByPriority([]*nom.AccountBlock{x1, x2, x3, y}), []*nom.AccountBlock{x1, y, x2, x3}))
}
