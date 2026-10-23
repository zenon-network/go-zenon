package chain

import (
	"strings"
	"testing"
	"time"

	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/db"
	"github.com/zenon-network/go-zenon/common/types"
)

// The replacement walk in addAccountBlockTransaction descends from the
// pool frontier by previous links, collecting blocks to re-add after a
// same-identifier replacement. The loop terminates only when each
// block's previous sits strictly below the block itself — an invariant
// the VM enforces for well-formed batches but the loop must not rely
// on. A block whose previous is at or above its own height would spin
// forever while holding ap.changes. The guard must fail closed instead.
func TestAccountPool_ReplacementWalkRejectsNonDescendingPrevious(t *testing.T) {
	address := types.Address{0, 1}
	ap := newAccountPool(fakeStable{})
	manager := ap.getAccountManager(address)

	// Block 1: a normal block at height 1.
	block1 := &nom.AccountBlock{
		Address:      address,
		BlockType:    nom.BlockTypeUserSend,
		Height:       1,
		PreviousHash: types.ZeroHash,
	}
	block1.Hash = block1.ComputeHash()
	common.FailIfErr(t, manager.Add(&nom.AccountBlockTransaction{
		Block:   block1,
		Changes: db.NewPatch(),
	}))

	// Block 2: a normal block at height 2 on top of block1.
	block2 := &nom.AccountBlock{
		Address:      address,
		BlockType:    nom.BlockTypeUserSend,
		Height:       2,
		PreviousHash: block1.Hash,
	}
	block2.Hash = block2.ComputeHash()
	common.FailIfErr(t, manager.Add(&nom.AccountBlockTransaction{
		Block:   block2,
		Changes: db.NewPatch(),
	}))

	// Malformed batch head at height 2 (same as block2!) whose
	// descendant sits at height 3 with previous pointing to block2.
	// The DB accepts this because it only checks the first commit's
	// previous link against the frontier. The batch head's Previous()
	// returns the descendant's previous: {block2.Hash, 2}. Since
	// head.Previous().Height (2) >= head.Height (2), the replacement
	// walk would loop forever without the guard.
	descendant := &nom.AccountBlock{
		Address:      address,
		BlockType:    nom.BlockTypeUserSend,
		Height:       3,
		PreviousHash: block2.Hash,
	}
	descendant.Hash = descendant.ComputeHash()

	batchHead := &nom.AccountBlock{
		Address:          address,
		BlockType:        nom.BlockTypeUserSend,
		Height:           2, // same height as block2 — malformed
		PreviousHash:     block2.Hash,
		DescendantBlocks: []*nom.AccountBlock{descendant},
	}
	batchHead.Hash = batchHead.ComputeHash()
	common.FailIfErr(t, manager.Add(&nom.AccountBlockTransaction{
		Block:   batchHead,
		Changes: db.NewPatch(),
	}))

	// Now attempt a forced same-identifier replacement of block1.
	// Same hash (all hashed fields identical), different bytes via
	// BasePlasma (not covered by the hash). The walk starts at the
	// frontier (the batch head at height 2), reads it, and the guard
	// fires because head.Previous().Height (2) >= head.Height (2).
	// Run in a goroutine with a timeout so a regression fails fast
	// instead of hanging the test run.
	replacement := &nom.AccountBlock{
		Address:      address,
		BlockType:    nom.BlockTypeUserSend,
		Height:       1,
		PreviousHash: types.ZeroHash,
		BasePlasma:   999, // different bytes, same hash
	}
	replacement.Hash = block1.Hash // same identifier

	errCh := make(chan error, 1)
	go func() {
		errCh <- ap.addAccountBlockTransaction(&nom.AccountBlockTransaction{
			Block:   replacement,
			Changes: db.NewPatch(),
		}, true)
	}()

	select {
	case err := <-errCh:
		if err == nil {
			t.Fatal("expected error for non-descending previous in replacement walk, got nil")
		}
		if !strings.Contains(err.Error(), "non-descending previous height") {
			t.Fatalf("error = %v, want non-descending previous height", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("replacement walk hung — non-descending previous guard is missing")
	}
}
