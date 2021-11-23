package vm_context

import (
	"math/big"

	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/types"
)

func (ctx *accountVmContext) AddBalance(ts *types.ZenonTokenStandard, amount *big.Int) {
	b, err := ctx.GetBalance(*ts)
	common.DealWithErr(err)
	b.Add(b, amount)
	common.DealWithErr(ctx.SetBalance(*ts, b))
}
func (ctx *accountVmContext) SubBalance(ts *types.ZenonTokenStandard, amount *big.Int) {
	b, err := ctx.GetBalance(*ts)
	common.DealWithErr(err)
	if b.Cmp(amount) >= 0 {
		b.Sub(b, amount)
		common.DealWithErr(ctx.SetBalance(*ts, b))
	} else {
		panic("negative balance after sub")
	}
}
