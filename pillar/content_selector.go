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
	plasma            dp.DynamicPlasma
	priorityAddresses map[types.Address]bool
}

func (cs *contentSelector) Content(blocks []*nom.AccountBlock) []*nom.AccountBlock {
	return cs.filterBlocksToCommit(cs.sortBlocksByPriority(blocks))
}

func (cs *contentSelector) sortBlocksByPriority(blocks []*nom.AccountBlock) []*nom.AccountBlock {
	sort.SliceStable(blocks, func(i, j int) bool {
		return cs.higherPriority(blocks[i], blocks[j])
	})
	return blocks
}

func (cs *contentSelector) filterBlocksToCommit(blocks []*nom.AccountBlock) []*nom.AccountBlock {
	contractBlockCount := 0
	basePlasma := uint64(0)
	toCommit := make([]*nom.AccountBlock, 0, len(blocks))
	contractBatch := make([]*nom.AccountBlock, 0, int(cs.plasma.MaxContractBlocksInMomentum()))
	for _, block := range blocks {
		if types.IsEmbeddedAddress(block.Address) {
			contractBatch = append(contractBatch, block)
			// Can't end in BlockTypeContractSend because otherwise the embedded send blocks would
			// be included but not the embedded receive block, since the embedded receive block
			// always has a greater height than the descendant send blocks.
			if block.BlockType == nom.BlockTypeContractSend {
				continue
			}
			if len(contractBatch)+contractBlockCount > int(cs.plasma.MaxContractBlocksInMomentum()) {
				continue
			}
			toCommit = append(toCommit, contractBatch...)
			contractBlockCount += len(contractBatch)
			contractBatch = contractBatch[:0]
		} else {
			basePlasma += block.BasePlasma
			if basePlasma > cs.plasma.Config().MaxBasePlasmaInMomentum {
				break
			}
			if !cs.plasma.ValidPrice(block) {
				break
			}
			toCommit = append(toCommit, block)
		}
	}
	return toCommit
}

// Determines the higher priority between two account blocks. Anwsers the question:
// "Does account block A have a higher priority than account block B?"
// The following rules are applied:
// 1. Contract blocks always have the higher priority.
// 2. Priority address blocks have higher priority over other user blocks.
// 3. When comparing two user blocks from the same address, the block with a lower height has higher priority.
// 4. When comparing two user blocks from different addresses, the block with a higher block price has higher priority.
// 5. If blocks are of equal priority price-wise then a block hash comparison will determine which block gets higher priority.
func (cs *contentSelector) higherPriority(a, b *nom.AccountBlock) bool {
	if types.IsEmbeddedAddress(b.Address) {
		return false
	}

	if types.IsEmbeddedAddress(a.Address) {
		return true
	}

	if _, ok := cs.priorityAddresses[b.Address]; ok {
		return false
	}

	if _, ok := cs.priorityAddresses[a.Address]; ok {
		return true
	}

	if a.Address == b.Address {
		return a.Height < b.Height
	}

	err := cs.plasma.HigherPrice(a, b)
	if err == dp.ErrBlockPriceSame && bytes.Compare(a.Hash.Bytes()[:], b.Hash.Bytes()[:]) > 1 {
		return true
	}
	return err == nil
}

func NewMomentumContentSelector(plasma dp.DynamicPlasma, priorityAddresses map[types.Address]bool) ContentSelector {
	return &contentSelector{
		plasma:            plasma,
		priorityAddresses: priorityAddresses,
	}
}
