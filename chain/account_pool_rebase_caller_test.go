package chain

import (
	"testing"

	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/chain/store"
	"github.com/zenon-network/go-zenon/common/db"
	"github.com/zenon-network/go-zenon/common/types"
)

// TestRebuildRebasesManagerInPlace pins the caller side of Rebase:
// rebuild rebinds the address's existing DB manager with
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
// forcing overlay reconstruction on every call.
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
// values are visible, pending values survive, and the retained version count
// equals the number of pending blocks — not the total number of blocks ever
// added.  The existing
// TestRebuildRebasesManagerInPlace uses fakeStable whose GetStableAccountDB
// returns a new empty DB on every call, so Rebase always sees a different DB
// object and runs the full rebuild path; this test proves the
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

		// Bounded version count: the number of versions still
		// accessible through the manager must be exactly the stable (1) plus
		// the pending blocks, not the total number of blocks ever added.
		// If Rebase failed to sever committed overlays the count would grow
		// by one per cycle instead of shrinking.
		pending := numBlocks - cycle
		accessible := 0
		for _, b := range blocks {
			id := types.HashHeight{Hash: b.Hash, Height: b.Height}
			if rebuilt.db.Get(id) != nil {
				accessible++
			}
		}
		// Accessible versions = pending blocks + the stable identifier itself.
		if uint64(accessible) != pending+1 {
			t.Fatalf("cycle %d: accessible versions = %d, want %d (1 stable + %d pending) — "+
				"version count is not bounded, committed versions were retained",
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

// TestRebuildNoUncommittedChanges covers the two normal end states where the
// stable store has caught up to (or passed) the manager's frontier, so
// rebuild has nothing to rebase.  Both cases must return nil, drop the
// manager, and not log an error.
//
// Case (a): the pool holds nothing above the new stable — the stable store
// has absorbed every pending block.
//
// Case (b): the pool holds blocks but on a different chain (fork) — the
// stable frontier is at or above the manager's frontier height even though
// the content differs.
func TestRebuildNoUncommittedChanges(t *testing.T) {
	t.Run("all pending committed", func(t *testing.T) {
		addr := types.Address{0, 44}
		stable := &advancingStable{stableDB: db.NewMemDB()}
		ap := newAccountPool(stable)

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

		// Advance the stable store to absorb the pending block.
		stable.stableDB = buildStableDB(t, []*nom.AccountBlock{block},
			[]map[string]string{{testAppKeyK1: "v1"}}, 1)

		if err := ap.rebuild(&nom.DetailedMomentum{
			Momentum: &nom.Momentum{Height: 1, Hash: types.Hash{1}},
		}); err != nil {
			t.Fatalf("rebuild returned error for all-committed case: %v", err)
		}
		if _, ok := ap.managers[addr]; ok {
			t.Fatal("manager should have been dropped when stable caught up to frontier")
		}
	})

	t.Run("stable above frontier", func(t *testing.T) {
		addr := types.Address{0, 46}
		stable := &advancingStable{stableDB: db.NewMemDB()}
		ap := newAccountPool(stable)

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

		// Advance the stable store past the manager's frontier.
		committed := &nom.AccountBlock{
			Address:     addr,
			BlockType:   nom.BlockTypeUserSend,
			Height:      1,
			TotalPlasma: 100,
			BasePlasma:  100,
		}
		committed.Hash = committed.ComputeHash()
		next := &nom.AccountBlock{
			Address:      addr,
			BlockType:    nom.BlockTypeUserSend,
			Height:       2,
			TotalPlasma:  100,
			BasePlasma:   100,
			PreviousHash: committed.Hash,
		}
		next.Hash = next.ComputeHash()
		stable.stableDB = buildStableDB(t, []*nom.AccountBlock{committed, next},
			[]map[string]string{{testAppKeyK1: "v1"}, {testAppKeyK2: "next"}}, 2)

		if err := ap.rebuild(&nom.DetailedMomentum{
			Momentum: &nom.Momentum{Height: 2, Hash: types.Hash{2}},
		}); err != nil {
			t.Fatalf("rebuild returned error for stable-above-frontier case: %v", err)
		}
		if _, ok := ap.managers[addr]; ok {
			t.Fatal("manager should have been dropped when stable passed frontier")
		}
	})

	t.Run("fork at same height", func(t *testing.T) {
		addr := types.Address{0, 45}
		stable := &advancingStable{stableDB: db.NewMemDB()}
		ap := newAccountPool(stable)

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

		// Build a different block at the same height on a different chain.
		// The stable frontier height equals the manager frontier height,
		// but the content is different (different hash).
		otherBlock := &nom.AccountBlock{
			Address:     addr,
			BlockType:   nom.BlockTypeUserSend,
			Height:      1,
			TotalPlasma: 200,
			BasePlasma:  200,
			Data:        []byte("fork"),
		}
		otherBlock.Hash = otherBlock.ComputeHash()
		if otherBlock.Hash == block.Hash {
			t.Fatal("otherBlock must differ from block in Data so ComputeHash produces a distinct hash")
		}
		stable.stableDB = buildStableDB(t, []*nom.AccountBlock{otherBlock},
			[]map[string]string{{testAppKeyK2: "other"}}, 1)

		if err := ap.rebuild(&nom.DetailedMomentum{
			Momentum: &nom.Momentum{Height: 1, Hash: types.Hash{1}},
		}); err != nil {
			t.Fatalf("rebuild returned error for fork-at-same-height case: %v", err)
		}
		if _, ok := ap.managers[addr]; ok {
			t.Fatal("manager should have been dropped when stable caught up to frontier on a fork")
		}
	})
}

// TestRebuildPreservesEmbeddedBlocks verifies that rebuild preserves
// DescendantBlocks on pending account blocks.  After rebuilds on an
// unchanged stable, the block returned by BlockByHeight must still
// contain its descendants.  This is a regression guard for the #82
// bugs where rebuild lost pending state.
func TestRebuildPreservesEmbeddedBlocks(t *testing.T) {
	addr := types.Address{0, 47}
	stable := &advancingStable{stableDB: db.NewMemDB()}
	ap := newAccountPool(stable)

	// h2: descendant at height 1 (added to DB first via GetCommits).
	// Its Previous must match the zero frontier.
	h2 := &nom.AccountBlock{
		Address:     addr,
		BlockType:   nom.BlockTypeUserReceive,
		Height:      1,
		TotalPlasma: 100,
		BasePlasma:  100,
	}
	h2.Hash = h2.ComputeHash()

	// h1: user send at height 2.  Added to DB after h2, so its Previous
	// must match h2's Identifier.
	h1 := &nom.AccountBlock{
		Address:      addr,
		BlockType:    nom.BlockTypeUserSend,
		Height:       2,
		TotalPlasma:  100,
		BasePlasma:   100,
		PreviousHash: h2.Hash,
	}
	h1.Hash = h1.ComputeHash()

	// Link h2 as a descendant of h1.
	h1.DescendantBlocks = []*nom.AccountBlock{h2}

	// Apply h1 (with descendants) to the manager.
	manager := ap.getAccountManager(addr)
	applyState(t, manager, h1, map[string]string{testAppKeyK1: "h1"})

	// Run three rebuilds on an unchanged stable.
	for i := 0; i < 3; i++ {
		if err := ap.rebuild(&nom.DetailedMomentum{
			Momentum: &nom.Momentum{Height: 1, Hash: types.Hash{1}},
		}); err != nil {
			t.Fatalf("rebuild %d returned error: %v", i+1, err)
		}
	}

	// Verify h1 survived with correct state.
	manager, ok := ap.managers[addr]
	if !ok {
		t.Fatal("manager missing after rebuilds")
	}
	if got := readValue(t, manager, testAppKeyK1); got != "h1" {
		t.Errorf("h1 value = %q, want %q", got, "h1")
	}

	// Verify h1 is in the blocks map at height 2 with its descendant.
	block, err := manager.BlockByHeight(2)
	if err != nil {
		t.Fatalf("BlockByHeight(2) after rebuilds: %v", err)
	}
	if len(block.DescendantBlocks) != 1 {
		t.Fatalf("h1 has %d descendants after rebuild, want 1", len(block.DescendantBlocks))
	}
	if block.DescendantBlocks[0].Hash != h2.Hash {
		t.Errorf("descendant block digest = %v, want %v", block.DescendantBlocks[0].Hash, h2.Hash)
	}

	// Verify h2 is also in the blocks map at height 1.
	block2, err := manager.BlockByHeight(1)
	if err != nil {
		t.Fatalf("BlockByHeight(1) after rebuilds: %v", err)
	}
	if block2.Hash != h2.Hash {
		t.Errorf("block2 hash = %v, want %v", block2.Hash, h2.Hash)
	}
}
