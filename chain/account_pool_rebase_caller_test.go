package chain

import (
	"testing"

	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/chain/store"
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


// advancingStable returns a stable account DB whose frontier advances as
// blocks are committed, unlike fakeStable which always returns an empty DB.
// This exercises the Rebase path where the stable floor actually moves,
// forcing overlay reconstruction rather than taking the same-identifier
// fast path.
type advancingStable struct {
	stableDB db.DB
}

func (s *advancingStable) GetStableAccountDB(types.Address) db.DB {
	return s.stableDB
}

func (s *advancingStable) GetFrontierMomentumStore() store.Momentum {
	return nil
}

// TestRebuildAdvancingStableOverlayReconstruction adds a caller fixture that
// advances stable state across repeated commit cycles, checking retained
// values, overlay depth and Pop/replacement behavior (#120 review item 2).
// The existing TestRebuildRebasesManagerInPlace uses fakeStable which returns
// an empty DB, so Rebase takes its same-identifier fast path; this test
// proves the advancing-floor overlay reconstruction path.
func TestRebuildAdvancingStableOverlayReconstruction(t *testing.T) {
	addr := types.Address{0, 43}
	stable := &advancingStable{stableDB: db.NewMemDB()}
	ap := newAccountPool(stable)

	manager := ap.getAccountManager(addr)

	// Block at height 1 with real application state.
	block1 := &nom.AccountBlock{
		Address:     addr,
		BlockType:   nom.BlockTypeUserSend,
		Height:      1,
		TotalPlasma: 100,
		BasePlasma:  100,
	}
	block1.Hash = block1.ComputeHash()
	applyState(t, manager, block1, map[string]string{testAppKeyK1: "v1"})

	// Block at height 2 with real application state.
	block2 := &nom.AccountBlock{
		Address:      addr,
		BlockType:    nom.BlockTypeUserSend,
		Height:       2,
		PreviousHash: block1.Hash,
		TotalPlasma:  100,
		BasePlasma:   100,
	}
	block2.Hash = block2.ComputeHash()
	applyState(t, manager, block2, map[string]string{testAppKeyK2: "v2"})

	// Advance the stable DB to include block1's state, simulating a commit.
	// The stable frontier must match block1's identifier for Rebase to
	// accept it.
	stable.stableDB = db.NewMemDB()
	patch1 := db.NewPatch()
	patch1.Put([]byte(testAppKeyK1), []byte("v1"))
	if err := db.ApplyPatch(stable.stableDB, patch1); err != nil {
		t.Fatalf("failed to apply patch1 to stable: %v", err)
	}
	id1 := db.GetFrontierIdentifier(manager.db.Frontier())
	// We need block1's identifier, not block2's.  Get it from the manager's
	// previous chain: block2's previous is block1.
	// Actually, we can reconstruct it from the block itself.
	block1ID := types.HashHeight{Hash: block1.Hash, Height: block1.Height}
	data := block1ID.Serialize()
	_ = id1
	if err := db.SetFrontier(stable.stableDB, block1ID, data); err != nil {
		t.Fatalf("failed to set stable frontier: %v", err)
	}

	// Trigger rebuild: momentum at height 1 commits block1.
	ap.InsertMomentum(&nom.DetailedMomentum{
		Momentum: &nom.Momentum{Height: 1, Hash: types.Hash{1}},
	})

	rebuilt := ap.managers[addr]
	if rebuilt == nil {
		t.Fatal("address lost its manager across rebuild with advancing stable")
	}

	// Block1 is now committed (at or below stable height); block2 is pending.
	if _, err := rebuilt.BlockByHeight(1); err == nil {
		t.Fatal("block1 should have been committed (removed from blocks map)")
	}
	if _, err := rebuilt.BlockByHeight(2); err != nil {
		t.Fatalf("pending block2 did not survive the rebuild: %v", err)
	}

	// The DB frontier must be at height 2 (block2 pending above stable at h1).
	frontierID := db.GetFrontierIdentifier(rebuilt.db.Frontier())
	if frontierID.Height != 2 {
		t.Fatalf("DB frontier height = %d, want 2", frontierID.Height)
	}

	// Retained values: block1's committed write is visible through the
	// stable DB; block2's pending write is visible through the overlay.
	if got := readValue(t, rebuilt, testAppKeyK1); got != "v1" {
		t.Fatalf("%s = %q, want %q — committed prefix state lost", testAppKeyK1, got, "v1")
	}
	if got := readValue(t, rebuilt, testAppKeyK2); got != "v2" {
		t.Fatalf("%s = %q, want %q — pending overlay state lost", testAppKeyK2, got, "v2")
	}

	// Pop the pending block; its application writes must disappear.
	if err := rebuilt.Pop(); err != nil {
		t.Fatalf("Pop after rebuild failed: %v", err)
	}
	if got := readValue(t, rebuilt, testAppKeyK2); got != "" {
		t.Fatalf("%s = %q after Pop, want empty — pending write not removed", testAppKeyK2, got)
	}
	if got := readValue(t, rebuilt, testAppKeyK1); got != "v1" {
		t.Fatalf("%s = %q after Pop, want %q — committed state lost", testAppKeyK1, got, "v1")
	}

	// Replacement insertion on top of the popped manager must succeed.
	block2Replacement := &nom.AccountBlock{
		Address:      addr,
		BlockType:    nom.BlockTypeUserSend,
		Height:       2,
		PreviousHash: block1.Hash,
		TotalPlasma:  100,
		BasePlasma:   100,
	}
	block2Replacement.Hash = block2Replacement.ComputeHash()
	applyState(t, rebuilt, block2Replacement, map[string]string{testAppKeyK2: "v2-new"})

	if got := readValue(t, rebuilt, testAppKeyK2); got != "v2-new" {
		t.Fatalf("%s = %q after replacement, want %q", testAppKeyK2, got, "v2-new")
	}
	if got := readValue(t, rebuilt, testAppKeyK1); got != "v1" {
		t.Fatalf("%s = %q after replacement, want %q", testAppKeyK1, got, "v1")
	}
}
