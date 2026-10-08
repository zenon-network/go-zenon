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

// buildStableDB constructs a stable DB that includes the state of blocks
// 1..upToHeight and carries the identifier of the block at upToHeight as its
// frontier, simulating a commit of all blocks up to and including that height.
func buildStableDB(t *testing.T, blocks []*nom.AccountBlock, patches []map[string]string, upToHeight uint64) db.DB {
	t.Helper()
	stableDB := db.NewMemDB()
	for h := uint64(1); h <= upToHeight; h++ {
		patch := db.NewPatch()
		for k, v := range patches[h-1] {
			patch.Put([]byte(k), []byte(v))
		}
		if err := db.ApplyPatch(stableDB, patch); err != nil {
			t.Fatalf("failed to apply patch at height %d to stable DB: %v", h, err)
		}
	}
	id := types.HashHeight{Hash: blocks[upToHeight-1].Hash, Height: upToHeight}
	if err := db.SetFrontier(stableDB, id, id.Serialize()); err != nil {
		t.Fatalf("failed to set stable frontier at height %d: %v", upToHeight, err)
	}
	return stableDB
}

// TestRebuildAdvancingStableOverlayReconstruction drives three successive
// advancing-floor commit cycles and, after each one, checks that committed
// values are visible, pending values survive, and the retained overlay depth
// equals the number of pending blocks — not the total number of blocks ever
// added (#120 review item 2).  The existing
// TestRebuildRebasesManagerInPlace uses fakeStable which returns an empty
// DB, so Rebase takes its same-identifier fast path; this test proves the
// advancing-floor overlay reconstruction path stays bounded across repeated
// commits.
func TestRebuildAdvancingStableOverlayReconstruction(t *testing.T) {
	addr := types.Address{0, 43}
	stable := &advancingStable{stableDB: db.NewMemDB()}
	ap := newAccountPool(stable)

	manager := ap.getAccountManager(addr)

	// Build four blocks, each writing a distinct key.
	const numBlocks = 4
	keys := []string{testAppKeyK1, testAppKeyK2, testAppKeyShared, "testapp/k4"}
	vals := []string{"v1", "v2", "v3", "v4"}
	patches := make([]map[string]string, numBlocks)
	var blocks []*nom.AccountBlock
	for i := 0; i < numBlocks; i++ {
		b := &nom.AccountBlock{
			Address:     addr,
			BlockType:   nom.BlockTypeUserSend,
			Height:      uint64(i + 1),
			TotalPlasma: 100,
			BasePlasma:  100,
		}
		if i > 0 {
			b.PreviousHash = blocks[i-1].Hash
		}
		b.Hash = b.ComputeHash()
		patches[i] = map[string]string{keys[i]: vals[i]}
		applyState(t, manager, b, patches[i])
		blocks = append(blocks, b)
	}

	// Three commit cycles: advance the stable floor from h0 to h1, h2, h3.
	// After cycle N the stable includes blocks 1..N; blocks N+1..4 are pending.
	for cycle := uint64(1); cycle <= 3; cycle++ {
		stable.stableDB = buildStableDB(t, blocks, patches, cycle)
		ap.InsertMomentum(&nom.DetailedMomentum{
			Momentum: &nom.Momentum{Height: cycle, Hash: types.Hash{byte(cycle)}},
		})

		rebuilt := ap.managers[addr]
		if rebuilt == nil {
			t.Fatalf("cycle %d: address lost its manager across rebuild", cycle)
		}

		// Committed blocks must be removed from the blocks map.
		for h := uint64(1); h <= cycle; h++ {
			if _, err := rebuilt.BlockByHeight(h); err == nil {
				t.Fatalf("cycle %d: block at height %d should have been committed (removed from blocks map)",
					cycle, h)
			}
		}

		// Pending blocks must survive.
		for h := cycle + 1; h <= numBlocks; h++ {
			if _, err := rebuilt.BlockByHeight(h); err != nil {
				t.Fatalf("cycle %d: pending block at height %d did not survive the rebuild: %v",
					cycle, h, err)
			}
		}

		// Frontier must sit at the last pending block.
		frontierID := db.GetFrontierIdentifier(rebuilt.db.Frontier())
		if frontierID.Height != numBlocks {
			t.Fatalf("cycle %d: DB frontier height = %d, want %d", cycle, frontierID.Height, numBlocks)
		}

		// Committed values must be visible through the stable DB.
		for h := uint64(1); h <= cycle; h++ {
			if got := readValue(t, rebuilt, keys[h-1]); got != vals[h-1] {
				t.Fatalf("cycle %d: committed %s = %q, want %q — committed prefix state lost",
					cycle, keys[h-1], got, vals[h-1])
			}
		}

		// Pending values must be visible through the overlay.
		for h := cycle + 1; h <= numBlocks; h++ {
			if got := readValue(t, rebuilt, keys[h-1]); got != vals[h-1] {
				t.Fatalf("cycle %d: pending %s = %q, want %q — pending overlay state lost",
					cycle, keys[h-1], got, vals[h-1])
			}
		}

		// Retained version-ID count: the number of block identifiers still
		// resolvable through the manager's version map must be exactly the
		// stable identifier (1) plus the pending blocks, not the total
		// blocks ever added. If Rebase failed to remove committed map entries
		// the count would grow by one per cycle instead of shrinking.
		// Note: this measures version-map membership (Get hit), not overlay
		// ancestry. The DB-layer TestRebase* tests cover ancestry retention
		// independently.
		pending := numBlocks - cycle
		accessible := 0
		for _, b := range blocks {
			id := types.HashHeight{Hash: b.Hash, Height: b.Height}
			if rebuilt.db.Get(id) != nil {
				accessible++
			}
		}
		// Accessible version-IDs = pending blocks + the stable identifier itself.
		if uint64(accessible) != pending+1 {
			t.Fatalf("cycle %d: accessible version-IDs = %d, want %d (1 stable + %d pending) — "+
				"version-ID count exceeds pending+stable, committed entries were retained in the map",
				cycle, accessible, pending+1, pending)
		}
	}

	// After three cycles only block 4 is pending.  Pop it; its write must
	// disappear while committed state survives.
	rebuilt := ap.managers[addr]
	if err := rebuilt.Pop(); err != nil {
		t.Fatalf("Pop after rebuild failed: %v", err)
	}
	if got := readValue(t, rebuilt, keys[3]); got != "" {
		t.Fatalf("%s = %q after Pop, want empty — pending write not removed", keys[3], got)
	}
	for h := uint64(1); h <= 3; h++ {
		if got := readValue(t, rebuilt, keys[h-1]); got != vals[h-1] {
			t.Fatalf("%s = %q after Pop, want %q — committed state lost", keys[h-1], got, vals[h-1])
		}
	}

	// Replacement insertion on top of the popped manager must succeed.
	replacement := &nom.AccountBlock{
		Address:      addr,
		BlockType:    nom.BlockTypeUserSend,
		Height:       4,
		PreviousHash: blocks[2].Hash,
		TotalPlasma:  100,
		BasePlasma:   100,
	}
	replacement.Hash = replacement.ComputeHash()
	applyState(t, rebuilt, replacement, map[string]string{keys[3]: "v4-new"})

	if got := readValue(t, rebuilt, keys[3]); got != "v4-new" {
		t.Fatalf("%s = %q after replacement, want %q", keys[3], got, "v4-new")
	}
	if got := readValue(t, rebuilt, keys[0]); got != "v1" {
		t.Fatalf("%s = %q after replacement, want %q", keys[0], got, "v1")
	}
}
