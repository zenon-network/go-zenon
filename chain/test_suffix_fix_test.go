package chain

import (
	"testing"

	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common/db"
	"github.com/zenon-network/go-zenon/common/types"
)

// Verify: DeleteMomentum truncates only the stale suffix, not the whole address.
func TestDeleteMomentumSuffixTruncation(t *testing.T) {
	ap := newAccountPool(fakeStable{})
	addr := types.Address{0, 1}
	manager := ap.getAccountManager(addr)
	b1 := &nom.AccountBlock{Address: addr, BlockType: nom.BlockTypeUserSend, Height: 1,
		MomentumAcknowledged: types.HashHeight{Hash: types.Hash{1}, Height: 5}}
	b1.Hash = b1.ComputeHash()
	b2 := &nom.AccountBlock{Address: addr, BlockType: nom.BlockTypeUserSend, Height: 2, PreviousHash: b1.Hash,
		MomentumAcknowledged: types.HashHeight{Hash: types.Hash{2}, Height: 10}}
	b2.Hash = b2.ComputeHash()
	for _, b := range []*nom.AccountBlock{b1, b2} {
		if err := manager.Add(&nom.AccountBlockTransaction{Block: b, Changes: db.NewPatch()}); err != nil {
			t.Fatal(err)
		}
	}
	// pop momentum 10 (contains no blocks of addr)
	ap.DeleteMomentum(&nom.DetailedMomentum{Momentum: &nom.Momentum{Height: 10, Hash: types.Hash{10}}})
	m := ap.managers[addr]
	if m == nil {
		t.Fatalf("whole address dropped: b1 (MA=5, still canonical) evicted together with b2 (MA=10)")
	}
	if len(m.blocks) != 1 {
		t.Fatalf("expected 1 block after truncation, got %d", len(m.blocks))
	}
	if _, ok := m.blocks[1]; !ok {
		t.Fatalf("block at height 1 (MA=5) should be kept")
	}
	if _, ok := m.blocks[2]; ok {
		t.Fatalf("block at height 2 (MA=10) should be evicted")
	}
	t.Logf("kept %d blocks (correct suffix truncation)", len(m.blocks))
}

// Verify: when ALL blocks are stale, the manager is dropped entirely.
func TestDeleteMomentumAllStale(t *testing.T) {
	ap := newAccountPool(fakeStable{})
	addr := types.Address{0, 2}
	manager := ap.getAccountManager(addr)
	b1 := &nom.AccountBlock{Address: addr, BlockType: nom.BlockTypeUserSend, Height: 1,
		MomentumAcknowledged: types.HashHeight{Hash: types.Hash{1}, Height: 10}}
	b1.Hash = b1.ComputeHash()
	if err := manager.Add(&nom.AccountBlockTransaction{Block: b1, Changes: db.NewPatch()}); err != nil {
		t.Fatal(err)
	}
	ap.DeleteMomentum(&nom.DetailedMomentum{Momentum: &nom.Momentum{Height: 10, Hash: types.Hash{10}}})
	m := ap.managers[addr]
	if m != nil {
		t.Fatalf("expected manager to be dropped when all blocks are stale, got %d blocks", len(m.blocks))
	}
}
