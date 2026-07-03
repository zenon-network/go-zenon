package vm_context

import (
	"math/big"

	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/types"
)

// AddBalance credits amount of token standard ts to the account's
// balance in the underlying account store.
func (ctx *accountVmContext) AddBalance(ts *types.ZenonTokenStandard, amount *big.Int) {
	b, err := ctx.GetBalance(*ts)
	common.DealWithErr(err)
	b.Add(b, amount)
	common.DealWithErr(ctx.SetBalance(*ts, b))
}

// SubBalance debits amount of token standard ts, panicking if the
// balance would go negative — callers (vm.applySend via enoughFunds)
// check funds beforehand.
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
