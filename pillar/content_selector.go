package pillar

import (
	"bytes"
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
// (each group internally ordered by height, lowest first), then ordering
// the groups themselves by the price of each group's lowest-height
// block. Comparing whole groups rather than individual blocks pairwise
// keeps the comparator a total order: a per-block comparator that mixes
// a same-address height rule with a cross-address price rule is not
// transitive (address X's cheap low-height block and expensive
// high-height block can each individually out- or under-rank a third
// address' block priced between them), and sort.SliceStable can resolve
// that into an order that separates a block from its own ancestor —
// leaving a gap in that address's chain that the producer's own
// chain-order check then rejects the whole momentum for.
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

	sort.SliceStable(order, func(i, j int) bool {
		return cs.higherGroupPriority(groups[order[i]][0], groups[order[j]][0])
	})

	result := make([]*nom.AccountBlock, 0, len(blocks))
	for _, address := range order {
		result = append(result, groups[address]...)
	}
	return result
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

// higherGroupPriority determines whether the address group led by block
// a should be ordered before the address group led by block b (a and b
// are always each group's lowest-height block, per sortBlocksByPriority).
// The following rules are applied:
// 1. Contract-address groups always have the higher priority.
// 2. Otherwise, the group whose leading block has a higher block price has higher priority.
// 3. If leading blocks are of equal price then a block hash comparison determines priority.
func (cs *contentSelector) higherGroupPriority(a, b *nom.AccountBlock) bool {
	if types.IsEmbeddedAddress(b.Address) {
		return false
	}

	if types.IsEmbeddedAddress(a.Address) {
		return true
	}

	err := cs.plasma.HigherPrice(a, b)
	if err == dp.ErrBlockPriceSame && bytes.Compare(a.Hash.Bytes()[:], b.Hash.Bytes()[:]) > 0 {
		return true
	}
	return err == nil
}

func NewMomentumContentSelector(plasma dp.DynamicPlasma) ContentSelector {
	return &contentSelector{
		plasma: plasma,
	}
}
