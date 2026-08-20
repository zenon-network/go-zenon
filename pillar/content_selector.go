package pillar

import (
	"bytes"
	"container/heap"
	"sort"

	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/dp"
)

type ContentSelector interface {
	Content(blocks []*nom.AccountBlock) []*nom.AccountBlock
}

type contentSelector struct {
	plasma dp.DynamicPlasma
}

func (cs *contentSelector) Content(blocks []*nom.AccountBlock) []*nom.AccountBlock {
	return cs.filterBlocksToCommit(cs.sortBlocksByPriority(blocks))
}

// sortBlocksByPriority orders blocks by grouping them by address first
// (each group internally ordered by height, lowest first, so a block
// never precedes its own ancestor — a gap would make the producer's own
// chain-order check reject the whole momentum). Embedded-address groups
// are emitted first, whole and contiguous, in first-seen order, which is
// what filterBlocksToCommit's contractBatch accumulation requires: it
// would mix addresses into a single batch if contract groups
// interleaved. The remaining (user) groups are merged by price across
// addresses via a k-way merge over each group's current head, so a
// block's price is compared only against other addresses' current heads
// — never against a lower-priority block from its own chain — while the
// per-address ancestor-before-descendant order is preserved by
// construction.
func (cs *contentSelector) sortBlocksByPriority(blocks []*nom.AccountBlock) []*nom.AccountBlock {
	groups := make(map[types.Address][]*nom.AccountBlock, len(blocks))
	order := make([]types.Address, 0, len(blocks))
	for _, block := range blocks {
		if _, ok := groups[block.Address]; !ok {
			order = append(order, block.Address)
		}
		groups[block.Address] = append(groups[block.Address], block)
	}

	for _, address := range order {
		group := groups[address]
		sort.SliceStable(group, func(i, j int) bool {
			return group[i].Height < group[j].Height
		})
	}

	result := make([]*nom.AccountBlock, 0, len(blocks))

	var userGroups []*addressGroup
	for _, address := range order {
		group := groups[address]
		if types.IsEmbeddedAddress(address) {
			result = append(result, group...)
			continue
		}
		userGroups = append(userGroups, &addressGroup{blocks: group, order: len(userGroups)})
	}

	gh := &groupHeap{cs: cs, groups: userGroups}
	heap.Init(gh)
	for gh.Len() > 0 {
		group := gh.groups[0]
		result = append(result, group.blocks[group.pos])
		group.pos++
		if group.pos == len(group.blocks) {
			heap.Pop(gh)
		} else {
			heap.Fix(gh, 0)
		}
	}

	return result
}

// addressGroup is a single address' height-ordered blocks, tracked by the
// k-way merge in sortBlocksByPriority.
type addressGroup struct {
	blocks []*nom.AccountBlock
	pos    int
	order  int // first-seen index; the stable tie-break
}

// groupHeap is a min-heap keyed by each group's current head price (via
// compareBlockPriority), so the highest-priced head is always at the root.
type groupHeap struct {
	cs     *contentSelector
	groups []*addressGroup
}

func (h *groupHeap) Len() int { return len(h.groups) }
func (h *groupHeap) Less(i, j int) bool {
	a, b := h.groups[i], h.groups[j]
	if c := h.cs.compareBlockPriority(a.blocks[a.pos], b.blocks[b.pos]); c != 0 {
		return c > 0
	}
	return a.order < b.order
}
func (h *groupHeap) Swap(i, j int) { h.groups[i], h.groups[j] = h.groups[j], h.groups[i] }
func (h *groupHeap) Push(x interface{}) {
	h.groups = append(h.groups, x.(*addressGroup))
}
func (h *groupHeap) Pop() interface{} {
	old := h.groups
	n := len(old)
	item := old[n-1]
	h.groups = old[:n-1]
	return item
}

func (cs *contentSelector) filterBlocksToCommit(blocks []*nom.AccountBlock) []*nom.AccountBlock {
	contractBlockCount := 0
	basePlasma := uint64(0)
	toCommit := make([]*nom.AccountBlock, 0, len(blocks))
	contractBatch := make([]*nom.AccountBlock, 0, int(cs.plasma.MaxContractBlocksInMomentum()))
	skippedAddresses := make(map[types.Address]bool)
	for _, block := range blocks {
		if types.IsEmbeddedAddress(block.Address) {
			if skippedAddresses[block.Address] {
				continue
			}
			contractBatch = append(contractBatch, block)
			// Can't end in BlockTypeContractSend because otherwise the embedded send blocks would
			// be included but not the embedded receive block, since the embedded receive block
			// always has a greater height than the descendant send blocks.
			if block.BlockType == nom.BlockTypeContractSend {
				continue
			}
			if len(contractBatch)+contractBlockCount > int(cs.plasma.MaxContractBlocksInMomentum()) {
				for _, dropped := range contractBatch {
					skippedAddresses[dropped.Address] = true
				}
				contractBatch = contractBatch[:0]
				continue
			}
			toCommit = append(toCommit, contractBatch...)
			contractBlockCount += len(contractBatch)
			contractBatch = contractBatch[:0]
		} else {
			// Blocks are sorted so that a lower-height block from the same address is always
			// processed first. Once a block is skipped, every descendant from that address must
			// also be skipped, otherwise the momentum would contain a gap in that address's chain.
			if skippedAddresses[block.Address] {
				continue
			}
			if basePlasma+block.BasePlasma > cs.plasma.Config().MaxBasePlasmaInMomentum {
				skippedAddresses[block.Address] = true
				continue
			}
			if !cs.plasma.ValidPrice(block) {
				skippedAddresses[block.Address] = true
				continue
			}
			basePlasma += block.BasePlasma
			toCommit = append(toCommit, block)
		}
	}
	return toCommit
}

// compareBlockPriority returns a positive value when a should be emitted
// before b, negative when after, and zero when the two are
// indistinguishable. Higher-priced blocks come first; an exact price tie is
// broken by the larger block hash.
func (cs *contentSelector) compareBlockPriority(a, b *nom.AccountBlock) int {
	switch err := cs.plasma.HigherPrice(a, b); err {
	case nil:
		return 1
	case dp.ErrBlockPriceSame:
		return bytes.Compare(a.Hash.Bytes()[:], b.Hash.Bytes()[:])
	default:
		return -1
	}
}

func NewMomentumContentSelector(plasma dp.DynamicPlasma) ContentSelector {
	return &contentSelector{
		plasma: plasma,
	}
}
