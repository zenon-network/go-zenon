package vm_context

import (
	"github.com/zenon-network/go-zenon/common"
)

// Save opens a snapshot window: the current account store is set
// aside and a snapshot of it becomes the working store, so subsequent
// writes can still be discarded.
func (ctx *accountVmContext) Save() {
	s := ctx.Account.Snapshot()
	ctx.accountStoreSnapshot = ctx.Account
	ctx.Account = s
}

// Reset closes the snapshot window opened by Save, discarding every
// write made inside it.
func (ctx *accountVmContext) Reset() {
	ctx.Account = ctx.accountStoreSnapshot
	ctx.accountStoreSnapshot = nil
}

// Done closes the snapshot window opened by Save, applying the
// changes accumulated in the working snapshot onto the original
// store.
func (ctx *accountVmContext) Done() {
	changes, _ := ctx.Account.Changes()
	ctx.Account = ctx.accountStoreSnapshot
	common.DealWithErr(ctx.Account.Apply(changes))
}
