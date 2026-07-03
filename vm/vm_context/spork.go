package vm_context

import (
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/types"
)

// IsAcceleratorSporkEnforced reports whether the accelerator spork is
// active at the context's momentum.
func (ctx *accountVmContext) IsAcceleratorSporkEnforced() bool {
	active, err := ctx.momentumStore.IsSporkActive(types.AcceleratorSpork)
	common.DealWithErr(err)
	return active
}

// IsHtlcSporkEnforced reports whether the HTLC spork is active at the
// context's momentum.
func (ctx *accountVmContext) IsHtlcSporkEnforced() bool {
	active, err := ctx.momentumStore.IsSporkActive(types.HtlcSpork)
	common.DealWithErr(err)
	return active
}

// IsBridgeAndLiquiditySporkEnforced reports whether the
// bridge-and-liquidity spork is active at the context's momentum.
func (ctx *accountVmContext) IsBridgeAndLiquiditySporkEnforced() bool {
	active, err := ctx.momentumStore.IsSporkActive(types.BridgeAndLiquiditySpork)
	common.DealWithErr(err)
	return active
}
