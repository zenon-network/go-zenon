package vm_context

import (
	"math/big"
	"testing"

	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/types"
)

func expectBalance(t *testing.T, ctx AccountVmContext, want int64) {
	t.Helper()
	b, err := ctx.GetBalance(types.ZnnTokenStandard)
	common.FailIfErr(t, err)
	if b.Cmp(big.NewInt(want)) != 0 {
		t.Fatalf("balance %v, want %d", b, want)
	}
}

// noPanic fails the test, instead of crashing it, when f panics.
func noPanic(t *testing.T, what string, f func()) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("%s panicked: %v", what, r)
		}
	}()
	f()
}

// The VM's rollback calls Reset when a receive fails before Save (an
// embedded method that no longer resolves). With no snapshot there is
// nothing to discard: the live store must stay in place, and the refund the
// rollback adds after Reset must land in it.
func TestReset_BeforeSaveKeepsTheLiveStore(t *testing.T) {
	ctx := NewGenesisAccountContext(types.TokenContract)
	ctx.AddBalance(&types.ZnnTokenStandard, big.NewInt(5))

	noPanic(t, "Reset before Save", ctx.Reset)
	noPanic(t, "AddBalance after Reset", func() { ctx.AddBalance(&types.ZnnTokenStandard, big.NewInt(1)) })
	expectBalance(t, ctx, 6)

	// The normal cycle still works on the same context afterwards.
	ctx.Save()
	ctx.AddBalance(&types.ZnnTokenStandard, big.NewInt(10))
	expectBalance(t, ctx, 16)
	ctx.Reset()
	expectBalance(t, ctx, 6)

	ctx.Save()
	ctx.AddBalance(&types.ZnnTokenStandard, big.NewInt(2))
	ctx.Done()
	expectBalance(t, ctx, 8)
}

func TestDone_BeforeSaveKeepsTheLiveStore(t *testing.T) {
	ctx := NewGenesisAccountContext(types.TokenContract)
	ctx.AddBalance(&types.ZnnTokenStandard, big.NewInt(3))
	noPanic(t, "Done before Save", ctx.Done)
	expectBalance(t, ctx, 3)
}

// A second Reset or Done after the snapshot was consumed must not drop the
// store either.
func TestReset_TwiceKeepsTheRestoredStore(t *testing.T) {
	ctx := NewGenesisAccountContext(types.TokenContract)
	ctx.AddBalance(&types.ZnnTokenStandard, big.NewInt(4))
	ctx.Save()
	ctx.AddBalance(&types.ZnnTokenStandard, big.NewInt(1))
	ctx.Reset()
	noPanic(t, "second Reset", ctx.Reset)
	noPanic(t, "Done after Reset", ctx.Done)
	expectBalance(t, ctx, 4)
}
