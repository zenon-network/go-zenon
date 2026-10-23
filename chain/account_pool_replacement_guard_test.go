package chain

import (
	"errors"
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
	t.Run("equal heights", func(t *testing.T) {
		assertReplacementWalkRejects(t, 2, 2)
	})
	t.Run("strictly greater previous height", func(t *testing.T) {
		assertReplacementWalkRejects(t, 2, 3)
	})
}

// assertReplacementWalkRejects builds a pool with a normal block at
// height 1, a normal block at height 2, and a malformed batch head at
// batchHeight whose descendant sits at batchHeight+1 with previous
// pointing to the block at height 2. The batch head's Previous()
// returns {block2.Hash, prevHeight}. When prevHeight >= batchHeight the
// replacement walk would loop forever without the guard. Wrapping
// manager.db with nilPatchManager after the fixture is built bounds the
// failure path: a regression hits "missing later pending patch" at once
// instead of spinning, and with the guard present no patch is read so
// the positive path is unchanged.
func assertReplacementWalkRejects(t *testing.T, batchHeight, prevHeight uint64) {
	t.Helper()
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

	// Malformed batch head whose descendant chains onto block2. The DB
	// accepts this because it only checks the first commit's previous
	// link against the frontier. The batch head's Previous() returns the
	// descendant's previous: {block2.Hash, prevHeight}. Since
	// prevHeight >= batchHeight, the replacement walk would loop forever
	// without the guard.
	descendant := &nom.AccountBlock{
		Address:      address,
		BlockType:    nom.BlockTypeUserSend,
		Height:       batchHeight + 1,
		PreviousHash: block2.Hash,
	}
	descendant.Hash = descendant.ComputeHash()

	batchHead := &nom.AccountBlock{
		Address:          address,
		BlockType:        nom.BlockTypeUserSend,
		Height:           batchHeight,
		PreviousHash:     block2.Hash,
		DescendantBlocks: []*nom.AccountBlock{descendant},
	}
	batchHead.Hash = batchHead.ComputeHash()
	common.FailIfErr(t, manager.Add(&nom.AccountBlockTransaction{
		Block:   batchHead,
		Changes: db.NewPatch(),
	}))

	// Bound the failure path: without the guard the walk spins, growing
	// keep until the test binary exits. nilPatchManager makes a
	// regression fail at once with "missing later pending patch", which
	// the message assertion rejects. With the guard present no patch is
	// read, so the positive path is unchanged.
	manager.db = &nilPatchManager{manager.db}

	// Snapshot the frontier and stored blocks so the rejection can be
	// checked to leave the pool untouched.
	frontierBefore := ap.GetFrontierAccountStore(address)
	frontierIDBefore := frontierBefore.Identifier()
	stored2Before, err := frontierBefore.ByHeight(2)
	common.FailIfErr(t, err)

	// Now attempt a forced same-identifier replacement of block1.
	// Same hash (all hashed fields identical), different bytes via
	// BasePlasma (not covered by the hash). The walk starts at the
	// frontier (the batch head), reads it, and the guard fires because
	// head.Previous().Height (prevHeight) >= head.Height (batchHeight).
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
		if !errors.Is(err, ErrFailedToAddAccountBlockTransaction) {
			t.Fatalf("error = %v, want ErrFailedToAddAccountBlockTransaction", err)
		}
		if !strings.Contains(err.Error(), "non-descending previous height") {
			t.Fatalf("error = %v, want non-descending previous height", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("replacement walk hung — non-descending previous guard is missing")
	}

	// The rejection must leave the pool untouched.
	frontierAfter := ap.GetFrontierAccountStore(address)
	common.Expect(t, frontierAfter.Identifier(), frontierIDBefore)
	stored2After, err := frontierAfter.ByHeight(2)
	common.FailIfErr(t, err)
	if stored2Before == nil || stored2After == nil {
		t.Fatal("expected stored block at height 2 before and after")
	}
	if !stored2After.EqualBytes(stored2Before) {
		t.Fatal("stored block at height 2 changed after rejection")
	}
}
