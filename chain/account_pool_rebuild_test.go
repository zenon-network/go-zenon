package chain

import (
	"math/big"
	"sync"
	"testing"

	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/chain/store"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/db"
	"github.com/zenon-network/go-zenon/common/types"
)

// advancingStable is a Stable implementation whose per-address stable DB can
// be advanced to a new frontier, mimicking a momentum commit.
type advancingStable struct {
	mu  sync.Mutex
	dbs map[types.Address]db.DB
}

func newAdvancingStable() *advancingStable {
	return &advancingStable{dbs: map[types.Address]db.DB{}}
}

func (s *advancingStable) GetStableAccountDB(address types.Address) db.DB {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.dbs[address] == nil {
		s.dbs[address] = db.NewMemDB()
	}
	return s.dbs[address]
}

func (s *advancingStable) GetFrontierMomentumStore() store.Momentum {
	return nil
}

// advanceTo copies the given blocks into the stable DB and sets the frontier
// to the last block's identifier, simulating a momentum commit.
func (s *advancingStable) advanceTo(blocks ...*nom.AccountBlock) {
	if len(blocks) == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	addr := blocks[0].Address
	if s.dbs[addr] == nil {
		s.dbs[addr] = db.NewMemDB()
	}
	for _, block := range blocks {
		data, err := block.Serialize()
		common.DealWithErr(err)
		common.DealWithErr(db.SetFrontier(s.dbs[addr], block.Identifier(), data))
	}
}

// rebuildTestBlocks creates a simple chain of user blocks at the given
// address starting at height 1.
func rebuildTestBlocks(address types.Address, count int) []*nom.AccountBlock {
	blocks := make([]*nom.AccountBlock, 0, count)
	prevHash := types.ZeroHash
	for height := uint64(1); height <= uint64(count); height++ {
		b := &nom.AccountBlock{
			Version:         1,
			ChainIdentifier: 1,
			BlockType:       nom.BlockTypeUserSend,
			Address:         address,
			Height:          height,
			PreviousHash:    prevHash,
			Amount:          big.NewInt(0),
		}
		b.Hash = b.ComputeHash()
		blocks = append(blocks, b)
		prevHash = b.Hash
	}
	return blocks
}

// TestRebuild_KeepsManagerWhenBlocksPending verifies that rebuild does not
// destroy the manager when uncommitted blocks remain after a momentum insert.
func TestRebuild_KeepsManagerWhenBlocksPending(t *testing.T) {
	address := types.Address{0, 1}
	stable := newAdvancingStable()
	ap := newAccountPool(stable)
	locker := &sync.Mutex{}

	blocks := rebuildTestBlocks(address, 3)
	for _, block := range blocks {
		common.FailIfErr(t, ap.AddAccountBlockTransaction(locker, &nom.AccountBlockTransaction{
			Block:   block,
			Changes: db.NewPatch(),
		}))
	}

	// Advance stable to height 1 (commit the first block).
	stable.advanceTo(blocks[0])
	ap.InsertMomentum(&nom.DetailedMomentum{Momentum: &nom.Momentum{Height: 1}})

	// Manager should still exist — blocks 2 and 3 are still pending.
	manager := ap.managers[address]
	if manager == nil {
		t.Fatal("manager removed despite pending blocks")
	}

	// Frontier should still be block 3.
	frontier := db.GetFrontierIdentifier(manager.db.Frontier())
	if frontier.Height != 3 {
		t.Fatalf("frontier height = %d, want 3", frontier.Height)
	}

	// Block 2 and 3 should be accessible.
	for _, h := range []uint64{2, 3} {
		if _, err := manager.BlockByHeight(h); err != nil {
			t.Fatalf("BlockByHeight(%d) error: %v", h, err)
		}
	}
}

// TestRebuild_RemovesManagerWhenAllCommitted verifies that rebuild removes
// the manager when every pending block has been committed.
func TestRebuild_RemovesManagerWhenAllCommitted(t *testing.T) {
	address := types.Address{0, 2}
	stable := newAdvancingStable()
	ap := newAccountPool(stable)
	locker := &sync.Mutex{}

	blocks := rebuildTestBlocks(address, 2)
	for _, block := range blocks {
		common.FailIfErr(t, ap.AddAccountBlockTransaction(locker, &nom.AccountBlockTransaction{
			Block:   block,
			Changes: db.NewPatch(),
		}))
	}

	// Advance stable to height 2 (commit everything).
	stable.advanceTo(blocks...)
	ap.InsertMomentum(&nom.DetailedMomentum{Momentum: &nom.Momentum{Height: 1}})

	if ap.managers[address] != nil {
		t.Fatal("manager kept despite all blocks committed")
	}
}

// TestRebuild_PatchDoesNotGrow verifies that calling rebuild (via
// InsertMomentum) does not grow the stored patches.  Before the fix, each
// rebuild re-added every uncommitted block together with its stored patch;
// memdbManager.Add replays the frontier writes into that patch, so the patch
// grew by one serialized block copy per rebuild.
func TestRebuild_PatchDoesNotGrow(t *testing.T) {
	address := types.Address{0, 3}
	stable := newAdvancingStable()
	ap := newAccountPool(stable)
	locker := &sync.Mutex{}

	// Add one block that will stay pending.
	block := rebuildTestBlocks(address, 1)[0]
	common.FailIfErr(t, ap.AddAccountBlockTransaction(locker, &nom.AccountBlockTransaction{
		Block:   block,
		Changes: db.NewPatch(),
	}))

	// Commit a momentum that does NOT include this address's block.
	// The stable store stays at height 0, so the block remains pending.
	otherAddr := types.Address{0, 99}
	otherBlocks := rebuildTestBlocks(otherAddr, 1)
	stable.advanceTo(otherBlocks[0])

	// Measure patch size before rebuild.
	patchBefore := ap.managers[address].db.GetPatch(block.Identifier())
	sizeBefore := len(patchBefore.Dump())
	if sizeBefore == 0 {
		t.Fatal("initial patch is empty")
	}

	// Trigger several rebuilds.
	for i := 0; i < 5; i++ {
		ap.InsertMomentum(&nom.DetailedMomentum{Momentum: &nom.Momentum{Height: uint64(i + 1)}})
	}

	patchAfter := ap.managers[address].db.GetPatch(block.Identifier())
	sizeAfter := len(patchAfter.Dump())

	if sizeAfter != sizeBefore {
		t.Fatalf("patch grew: %d -> %d bytes after 5 rebuilds", sizeBefore, sizeAfter)
	}
}

// TestRebuild_BatchedEmbeddedSurvives verifies that a batched embedded
// transaction (a contract receive whose DescendantBlocks carry the contract
// sends it triggered) survives rebuild intact.  Before the fix, rebuild
// re-added each height individually; re-adding the parent replayed its
// descendants a second time and Add rejected it with "previous doesn't
// match", destroying the account's pending chain.
func TestRebuild_BatchedEmbeddedSurvives(t *testing.T) {
	address := types.PillarContract
	stable := newAdvancingStable()
	ap := newAccountPool(stable)
	locker := &sync.Mutex{}

	// Build: base block at height 1, then a batched receive at height 4
	// whose descendants are contract sends at heights 2 and 3.
	base := &nom.AccountBlock{
		Version:         1,
		ChainIdentifier: 1,
		BlockType:       nom.BlockTypeContractReceive,
		Address:         address,
		Height:          1,
		Amount:          big.NewInt(0),
	}
	base.Hash = base.ComputeHash()

	prev := base.Hash
	descendants := make([]*nom.AccountBlock, 0, 2)
	for h := uint64(2); h <= 3; h++ {
		d := &nom.AccountBlock{
			Version:         1,
			ChainIdentifier: 1,
			BlockType:       nom.BlockTypeContractSend,
			Address:         address,
			Height:          h,
			PreviousHash:    prev,
			Amount:          big.NewInt(0),
		}
		d.Hash = d.ComputeHash()
		prev = d.Hash
		descendants = append(descendants, d)
	}
	receive := &nom.AccountBlock{
		Version:          1,
		ChainIdentifier:  1,
		BlockType:        nom.BlockTypeContractReceive,
		Address:          address,
		Height:           4,
		PreviousHash:     prev,
		Amount:           big.NewInt(0),
		DescendantBlocks: descendants,
	}
	receive.Hash = receive.ComputeHash()

	common.FailIfErr(t, ap.AddAccountBlockTransaction(locker, &nom.AccountBlockTransaction{
		Block:   base,
		Changes: db.NewPatch(),
	}))
	common.FailIfErr(t, ap.AddAccountBlockTransaction(locker, &nom.AccountBlockTransaction{
		Block:   receive,
		Changes: db.NewPatch(),
	}))

	// Verify frontier is the receive.
	frontier := ap.GetFrontierAccountStore(address)
	common.Expect(t, frontier.Identifier(), receive.Identifier())

	// Commit a momentum for a different address, triggering rebuild.
	otherAddr := types.Address{0, 98}
	otherBlocks := rebuildTestBlocks(otherAddr, 1)
	stable.advanceTo(otherBlocks[0])
	ap.InsertMomentum(&nom.DetailedMomentum{Momentum: &nom.Momentum{Height: 1}})

	// After rebuild, the manager should still exist and the frontier should
	// still be the receive.
	frontier = ap.GetFrontierAccountStore(address)
	if frontier.Identifier() != receive.Identifier() {
		t.Fatalf("frontier = %v, want %v", frontier.Identifier(), receive.Identifier())
	}

	// All blocks should be accessible.
	manager := ap.managers[address]
	if manager == nil {
		t.Fatal("manager removed despite pending batched embedded transaction")
	}
	for _, h := range []uint64{1, 2, 3, 4} {
		if _, err := manager.BlockByHeight(h); err != nil {
			t.Fatalf("BlockByHeight(%d) error after rebuild: %v", h, err)
		}
	}
}

// TestRebuild_MultipleRebuildsPreserveState verifies that repeated rebuilds
// do not corrupt the manager state.
func TestRebuild_MultipleRebuildsPreserveState(t *testing.T) {
	address := types.Address{0, 4}
	stable := newAdvancingStable()
	ap := newAccountPool(stable)
	locker := &sync.Mutex{}

	blocks := rebuildTestBlocks(address, 3)
	for _, block := range blocks {
		common.FailIfErr(t, ap.AddAccountBlockTransaction(locker, &nom.AccountBlockTransaction{
			Block:   block,
			Changes: db.NewPatch(),
		}))
	}

	// Commit block 1.
	stable.advanceTo(blocks[0])

	// Trigger several rebuilds.
	for i := 0; i < 3; i++ {
		ap.InsertMomentum(&nom.DetailedMomentum{Momentum: &nom.Momentum{Height: uint64(i + 1)}})
	}

	// Blocks 2 and 3 should still be accessible.
	manager := ap.managers[address]
	if manager == nil {
		t.Fatal("manager removed despite pending blocks")
	}
	frontier := db.GetFrontierIdentifier(manager.db.Frontier())
	if frontier.Height != 3 {
		t.Fatalf("frontier height = %d, want 3", frontier.Height)
	}

	// The pool should still accept new blocks on top.
	block4 := &nom.AccountBlock{
		Version:         1,
		ChainIdentifier: 1,
		BlockType:       nom.BlockTypeUserSend,
		Address:         address,
		Height:          4,
		PreviousHash:    blocks[2].Hash,
		Amount:          big.NewInt(0),
	}
	block4.Hash = block4.ComputeHash()
	common.FailIfErr(t, ap.AddAccountBlockTransaction(locker, &nom.AccountBlockTransaction{
		Block:   block4,
		Changes: db.NewPatch(),
	}))

	frontier = db.GetFrontierIdentifier(manager.db.Frontier())
	if frontier.Height != 4 {
		t.Fatalf("frontier height after add = %d, want 4", frontier.Height)
	}
}
