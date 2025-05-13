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
		plasma:            dp.NewDynamicPlasma(previousMomentum, config),
		priorityAddresses: make(map[types.Address]bool),
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

func TestContent_sortBlocksByPriority(t *testing.T) {
	address1 := types.ParseAddressPanic("z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz")
	address2 := types.ParseAddressPanic("z1qqfmjdays57w488sta69ykc2ey7r6d0q9wdvtj")
	address3 := types.ParseAddressPanic("z1qqdt06lnwz57x38rwlyutcx5wgrtl0ynkfe3kv")
	address4 := types.ParseAddressPanic("z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx")

	previousMomentum := &nom.Momentum{NextFusionPrice: 1000, NextWorkPrice: 1000, Version: 2}
	cs := &contentSelector{
		plasma:            dp.NewDynamicPlasma(previousMomentum, nil),
		priorityAddresses: make(map[types.Address]bool),
	}

	cs.priorityAddresses[address3] = true

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

	// Priority address
	common.ExpectTrue(t, reflect.DeepEqual(cs.sortBlocksByPriority([]*nom.AccountBlock{
		{Height: 1, BlockType: nom.BlockTypeUserSend, BasePlasma: 21000, FusedPlasma: 21001, Address: address1},
		{Height: 2, BlockType: nom.BlockTypeUserSend, BasePlasma: 21000, FusedPlasma: 21002, Address: address2},
		{Height: 3, BlockType: nom.BlockTypeUserSend, BasePlasma: 21000, FusedPlasma: 21000, Address: address3},
		{Height: 4, BlockType: nom.BlockTypeUserSend, BasePlasma: 21000, FusedPlasma: 21004, Address: address4},
	}), []*nom.AccountBlock{
		{Height: 3, BlockType: nom.BlockTypeUserSend, BasePlasma: 21000, FusedPlasma: 21000, Address: address3},
		{Height: 4, BlockType: nom.BlockTypeUserSend, BasePlasma: 21000, FusedPlasma: 21004, Address: address4},
		{Height: 2, BlockType: nom.BlockTypeUserSend, BasePlasma: 21000, FusedPlasma: 21002, Address: address2},
		{Height: 1, BlockType: nom.BlockTypeUserSend, BasePlasma: 21000, FusedPlasma: 21001, Address: address1},
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
