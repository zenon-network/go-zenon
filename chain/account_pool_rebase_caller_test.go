package chain

import (
	"testing"

	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common/db"
	"github.com/zenon-network/go-zenon/common/types"
)

// TestRebuildRebasesManagerInPlace pins the caller side of Rebase (#120
// review item 3): rebuild rebinds the address's existing DB manager with
// Rebase instead of throwing it away and replaying every pending block into
// a freshly constructed manager.
//
// The DB manager is behind an interface holding a pointer, so identity is
// observable: a caller that went back to db.NewMemDBManager would hand out a
// different instance. The accountManager wrapper around it is deliberately a
// new value — that is what TestRebuildFailureIsolatesFailingAddress uses to
// tell "rebuilt" from "left untouched" — so this test asserts on the DB
// underneath, not on the wrapper.
func TestRebuildRebasesManagerInPlace(t *testing.T) {
	addr := types.Address{0, 42}
	ap := newAccountPool(fakeStable{})

	manager := ap.getAccountManager(addr)
	block := &nom.AccountBlock{
		Address:     addr,
		BlockType:   nom.BlockTypeUserSend,
		Height:      1,
		TotalPlasma: 100,
		BasePlasma:  100,
	}
	block.Hash = block.ComputeHash()
	applyState(t, manager, block, map[string]string{testAppKeyK1: "v1"})

	dbBefore := manager.db

	// A momentum insert that changes nothing about this account's chain:
	// rebuild must run and leave the pending block in place.
	ap.InsertMomentum(&nom.DetailedMomentum{
		Momentum: &nom.Momentum{Height: 1, Hash: types.Hash{1}},
	})

	rebuilt := ap.managers[addr]
	if rebuilt == nil {
		t.Fatal("address lost its manager across a rebuild that kept its only block")
	}
	if rebuilt.db != dbBefore {
		t.Fatal("rebuild replaced the DB manager instead of rebasing it in place — " +
			"Rebase exists so the pending versions are moved, not reconstructed block by block")
	}
	if _, err := rebuilt.BlockByHeight(1); err != nil {
		t.Fatalf("pending block did not survive the rebuild: %v", err)
	}
	if got := db.GetFrontierIdentifier(rebuilt.db.Frontier()).Height; got != 1 {
		t.Fatalf("frontier height = %d, want 1 — the rebased manager lost its pending head", got)
	}
	if got := readValue(t, rebuilt, testAppKeyK1); got != "v1" {
		t.Fatalf("pending patch lost across rebuild: %s = %q, want %q", testAppKeyK1, got, "v1")
	}
}
