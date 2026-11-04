package vm_context

import (
	"github.com/zenon-network/go-zenon/common"
)

func (ctx *accountVmContext) Save() {
	s := ctx.Account.Snapshot()
	ctx.accountStoreSnapshot = ctx.Account
	ctx.Account = s
}

// Reset discards the changes made since Save. Without a snapshot there is
// nothing to discard, and the live store stays in place: the VM's rollback
// can reach Reset before Save, and then adds the refund to this store.
func (ctx *accountVmContext) Reset() {
	if ctx.accountStoreSnapshot == nil {
		return
	}
	ctx.Account = ctx.accountStoreSnapshot
	ctx.accountStoreSnapshot = nil
}

// Done applies the changes made since Save to the store Save replaced.
// Without a snapshot the changes are already in the live store.
func (ctx *accountVmContext) Done() {
	if ctx.accountStoreSnapshot == nil {
		return
	}
	changes, _ := ctx.Account.Changes()
	ctx.Account = ctx.accountStoreSnapshot
	ctx.accountStoreSnapshot = nil
	common.DealWithErr(ctx.Account.Apply(changes))
}
