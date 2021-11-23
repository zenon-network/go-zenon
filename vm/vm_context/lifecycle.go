package vm_context

import (
	"github.com/zenon-network/go-zenon/common"
)

func (ctx *accountVmContext) Save() {
	s := ctx.Account.Snapshot()
	ctx.accountStoreSnapshot = ctx.Account
	ctx.Account = s
}
func (ctx *accountVmContext) Reset() {
	ctx.Account = ctx.accountStoreSnapshot
	ctx.accountStoreSnapshot = nil
}
func (ctx *accountVmContext) Done() {
	changes, _ := ctx.Account.Changes()
	ctx.Account = ctx.accountStoreSnapshot
	common.DealWithErr(ctx.Account.Apply(changes))
}
