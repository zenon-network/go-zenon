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
		assertReplacementWalkRejectsEqual(t)
	})
	t.Run("strictly greater previous height", func(t *testing.T) {
		assertReplacementWalkRejectsGreater(t)
	})
}

// assertReplacementWalkRejectsEqual builds a pool with a normal block at
// height 1, a normal block at height 2, and a malformed batch head also
// at height 2 whose descendant sits at height 3 with previous pointing
// to block2. The batch head's Previous() returns {block2.Hash, 2}, so
// Previous().Height == batchHead.Height. The replacement walk would loop
// forever without the guard.
func assertReplacementWalkRejectsEqual(t *testing.T) {
	t.Helper()
	address := types.Address{0, 1}
	ap := newAccountPool(fakeStable{})
	manager := ap.getAccountManager(address)

	block1 := addBlockAtHeight(t, manager, address, 1, types.ZeroHash)
	block2 := addBlockAtHeight(t, manager, address, 2, block1.Hash)

	// Malformed batch head at height 2 with descendant at height 3
	// chaining onto block2. Previous().Height (2) == batchHead.Height (2).
	addBatchHead(t, manager, address, 2, 3, block2.Hash)

	assertReplacementFails(t, ap, manager, address, block1)
}

// assertReplacementWalkRejectsGreater builds a pool with normal blocks at
// heights 1, 2 and 3, and a malformed batch head at height 2 whose
// descendant sits at height 4 with previous pointing to block3. The batch
// head's Previous() returns {block3.Hash, 3}, so Previous().Height (3) >
// batchHead.Height (2). The replacement walk would loop forever without
// the guard.
func assertReplacementWalkRejectsGreater(t *testing.T) {
	t.Helper()
	address := types.Address{0, 1}
	ap := newAccountPool(fakeStable{})
	manager := ap.getAccountManager(address)

	block1 := addBlockAtHeight(t, manager, address, 1, types.ZeroHash)
	block2 := addBlockAtHeight(t, manager, address, 2, block1.Hash)
	block3 := addBlockAtHeight(t, manager, address, 3, block2.Hash)

	// Malformed batch head at height 2 with descendant at height 4
	// chaining onto block3. Previous().Height (3) > batchHead.Height (2).
	addBatchHead(t, manager, address, 2, 4, block3.Hash)

	assertReplacementFails(t, ap, manager, address, block1)
}

// addBlockAtHeight creates and adds a simple user-send block at the given
// height chaining onto previousHash.
func addBlockAtHeight(t *testing.T, manager *accountManager, address types.Address, height uint64, previousHash types.Hash) *nom.AccountBlock {
	t.Helper()
	block := &nom.AccountBlock{
		Address:      address,
		BlockType:    nom.BlockTypeUserSend,
		Height:       height,
		PreviousHash: previousHash,
	}
	block.Hash = block.ComputeHash()
	common.FailIfErr(t, manager.Add(&nom.AccountBlockTransaction{
		Block:   block,
		Changes: db.NewPatch(),
	}))
	return block
}

// addBatchHead creates and adds a malformed batch head at batchHeight
// with a single descendant at descendantHeight chaining onto
// descendantPrevHash. The batch head's Previous() returns the
// descendant's previous: {descendantPrevHash, descendantHeight - 1}.
func addBatchHead(t *testing.T, manager *accountManager, address types.Address, batchHeight, descendantHeight uint64, descendantPrevHash types.Hash) {
	t.Helper()
	descendant := &nom.AccountBlock{
		Address:      address,
		BlockType:    nom.BlockTypeUserSend,
		Height:       descendantHeight,
		PreviousHash: descendantPrevHash,
	}
	descendant.Hash = descendant.ComputeHash()

	batchHead := &nom.AccountBlock{
		Address:          address,
		BlockType:        nom.BlockTypeUserSend,
		Height:           batchHeight,
		PreviousHash:     descendantPrevHash,
		DescendantBlocks: []*nom.AccountBlock{descendant},
	}
	batchHead.Hash = batchHead.ComputeHash()
	common.FailIfErr(t, manager.Add(&nom.AccountBlockTransaction{
		Block:   batchHead,
		Changes: db.NewPatch(),
	}))
}

// assertReplacementFails attempts a forced same-identifier replacement of
// block1 and asserts the guard rejects it with
// ErrFailedToAddAccountBlockTransaction wrapping "non-descending previous
// height". It also asserts the pool is left untouched.
func assertReplacementFails(t *testing.T, ap *accountPool, manager *accountManager, address types.Address, block1 *nom.AccountBlock) {
	t.Helper()

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

	// Attempt a forced same-identifier replacement of block1. Same hash
	// (all hashed fields identical), different bytes via BasePlasma (not
	// covered by the hash). The walk starts at the frontier (the batch
	// head), reads it, and the guard fires because
	// head.Previous().Height >= head.Height. Run in a goroutine with a
	// timeout so a regression fails fast instead of hanging the test run.
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
