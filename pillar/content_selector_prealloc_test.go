package pillar

import (
	"runtime"
	"testing"

	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/dp"
	"github.com/zenon-network/go-zenon/vm/embedded/definition"
)

// TestContent_MaxConfigAllocatesByInputNotCeiling pins that the content
// selector's memory use follows the number of pending blocks, not the
// configured MaxBasePlasmaInMomentum. The configuration is a fixed,
// deliberately absurd ceiling (10,000,000,000 base blocks, the limit the
// setter used to accept) rather than the current upper limit, so the test
// keeps discriminating no matter where that limit is set: at this ceiling
// the implied contract-block cap is four billion, and sizing a slice by it
// up front asks for about 32 GB on every producer at every momentum.
func TestContent_MaxConfigAllocatesByInputNotCeiling(t *testing.T) {
	config := &definition.PlasmaVariables{
		MaxBasePlasmaInMomentum: 10_000_000_000 * 21000,
	}
	previousMomentum := &nom.Momentum{NextFusionPrice: 1000, NextWorkPrice: 1000, Version: 2}
	cs := &contentSelector{
		plasma: dp.NewDynamicPlasma(previousMomentum, config),
	}

	cases := map[string][]*nom.AccountBlock{
		"no blocks": nil,
		"one user block": {
			{Height: 1, BlockType: nom.BlockTypeUserSend, BasePlasma: 21000, FusedPlasma: 21000},
		},
		"one contract batch": {
			{Height: 1, BlockType: nom.BlockTypeContractSend, Address: types.PillarContract},
			{Height: 2, BlockType: nom.BlockTypeContractReceive, Address: types.PillarContract},
		},
	}
	const allowance = 1 << 20 // a megabyte covers any per-call bookkeeping
	for name, blocks := range cases {
		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)
		got := cs.Content(blocks)
		runtime.ReadMemStats(&after)
		allocated := after.TotalAlloc - before.TotalAlloc
		if allocated > allowance {
			t.Fatalf("%s: Content allocated %d bytes for %d blocks", name, allocated, len(blocks))
		}
		common.Expect(t, len(got), len(blocks))
	}
}
