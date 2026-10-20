package account

import (
	"math"
	"testing"

	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/db"
	"github.com/zenon-network/go-zenon/common/types"
)

// twoBlockStore is an account chain with blocks at heights 1 and 2.
func twoBlockStore(t *testing.T) (*accountStore, []*nom.AccountBlock) {
	t.Helper()
	address := types.ParseAddressPanic("z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz")
	memdb := db.NewMemDB()
	blocks := make([]*nom.AccountBlock, 0, 2)
	for height := uint64(1); height <= 2; height++ {
		block := &nom.AccountBlock{Version: 1, ChainIdentifier: 100, BlockType: nom.BlockTypeUserSend, Address: address, Height: height, Amount: common.Big0}
		block.Hash = block.ComputeHash()
		data, err := block.Serialize()
		common.FailIfErr(t, err)
		common.FailIfErr(t, db.SetFrontier(memdb, block.Identifier(), data))
		blocks = append(blocks, block)
	}
	return NewAccountStore(address, memdb).(*accountStore), blocks
}

func heights(blocks []*nom.AccountBlock) []uint64 {
	out := make([]uint64, 0, len(blocks))
	for _, b := range blocks {
		if b == nil {
			out = append(out, 0)
			continue
		}
		out = append(out, b.Height)
	}
	return out
}

// A start height above the frontier yields an empty range, including at
// the top of the uint64 range, where height+i used to wrap to 0, 1, ...
// and return the chain's first blocks under heights they do not have.
func TestMoreByHeight_AboveTheFrontierIsEmpty(t *testing.T) {
	store, _ := twoBlockStore(t)
	for _, c := range []struct{ height, count uint64 }{
		{math.MaxUint64, 3},
		{math.MaxUint64 - 1, 5},
		{math.MaxUint64, 0},
		{3, 1},
		{3, 1024},
	} {
		got, err := store.MoreByHeight(c.height, c.count)
		common.FailIfErr(t, err)
		if len(got) != 0 {
			t.Fatalf("MoreByHeight(%d, %d) = heights %v, want none", c.height, c.count, heights(got))
		}
	}
}

// Within the chain the range is ascending from height, cut at the frontier,
// with no nil entries.
func TestMoreByHeight_RangeIsCutAtTheFrontier(t *testing.T) {
	store, blocks := twoBlockStore(t)
	for _, c := range []struct {
		height, count uint64
		want          []uint64
	}{
		{1, 1, []uint64{1}},
		{1, 2, []uint64{1, 2}},
		{1, 10, []uint64{1, 2}},
		{2, 1, []uint64{2}},
		{2, math.MaxUint64, []uint64{2}},
		{1, 0, []uint64{}},
	} {
		got, err := store.MoreByHeight(c.height, c.count)
		common.FailIfErr(t, err)
		if h := heights(got); len(h) != len(c.want) || (len(h) > 0 && !equalU64(h, c.want)) {
			t.Fatalf("MoreByHeight(%d, %d) = heights %v, want %v", c.height, c.count, h, c.want)
		}
		for i, b := range got {
			if b == nil || b.Hash != blocks[b.Height-1].Hash {
				t.Fatalf("MoreByHeight(%d, %d)[%d] is not the stored block", c.height, c.count, i)
			}
		}
	}

	empty := NewAccountStore(types.ParseAddressPanic("z1qqfmjdays57w488sta69ykc2ey7r6d0q9wdvtj"), db.NewMemDB())
	got, err := empty.MoreByHeight(1, 5)
	common.FailIfErr(t, err)
	if len(got) != 0 {
		t.Fatalf("empty account returned %d entries", len(got))
	}
}

func equalU64(a, b []uint64) bool {
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
