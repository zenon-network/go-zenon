package vm_context

import (
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/types"
)

func (ctx *accountVmContext) IsAcceleratorSporkEnforced() bool {
	active, err := ctx.cacheStore.IsSporkActive(types.AcceleratorSpork)
	common.DealWithErr(err)
	return active
}

func (ctx *accountVmContext) IsHtlcSporkEnforced() bool {
	active, err := ctx.cacheStore.IsSporkActive(types.HtlcSpork)
	common.DealWithErr(err)
	return active
}

func (ctx *accountVmContext) IsBridgeAndLiquiditySporkEnforced() bool {
	active, err := ctx.cacheStore.IsSporkActive(types.BridgeAndLiquiditySpork)
	common.DealWithErr(err)
	return active
}
