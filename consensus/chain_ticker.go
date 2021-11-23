package consensus

import (
	"github.com/pkg/errors"

	"github.com/zenon-network/go-zenon/chain"
	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common"
)

type ChainTicker interface {
	common.Ticker
	IsFinished(tick uint64) bool
	HasStarted(tick uint64) bool
	GetEndBlock(tick uint64) (*nom.Momentum, error)
	GetContent(tick uint64) ([]*nom.Momentum, error)
}

type chainTicker struct {
	common.Ticker
	chain.Chain
}

func (ct *chainTicker) IsFinished(tick uint64) bool {
	if tick > (1<<62)-1 {
		panic("most probably an overflow error")
	}
	_, eTime := ct.ToTime(tick)
	block, err := ct.GetFrontierMomentumStore().GetFrontierMomentum()
	common.DealWithErr(err)
	if block.Timestamp.After(eTime) || block.Timestamp.Equal(eTime) {
		return true
	}
	return false
}

func (ct *chainTicker) HasStarted(tick uint64) bool {
	if tick > (1<<62)-1 {
		panic("most probably an overflow error")
	}
	sTime, _ := ct.ToTime(tick)
	block, err := ct.GetFrontierMomentumStore().GetFrontierMomentum()
	common.DealWithErr(err)
	if block.Timestamp.Before(sTime) {
		return false
	}
	return true
}

// Returns the head of the previous tick group
func (ct *chainTicker) GetEndBlock(tick uint64) (*nom.Momentum, error) {
	if tick > (1<<62)-1 {
		panic("most probably an overflow error")
	}
	_, eTime := ct.ToTime(tick)
	block, err := ct.GetFrontierMomentumStore().GetMomentumBeforeTime(&eTime)
	if err != nil {
		return nil, err
	}
	if block == nil {
		return nil, errors.Errorf("chainTicker.GetEndBlock failed to get block for tick %v endTime %v", tick, eTime.Unix())
	}
	return block, err
}

func (ct *chainTicker) GetContent(tick uint64) ([]*nom.Momentum, error) {
	if tick > (1<<62)-1 {
		panic("most probably an overflow error")
	}
	sTime, _ := ct.ToTime(tick)
	endBlock, err := ct.GetEndBlock(tick)
	if err != nil {
		return nil, err
	}

	if !endBlock.Timestamp.Before(sTime) {
		var startBlock *nom.Momentum
		if tick == 0 {
			startBlock = ct.GetGenesisMomentum()
		} else {
			startBlock, err = ct.GetEndBlock(tick - 1)
			if err != nil {
				return nil, err
			}
			if startBlock == nil {
				return nil, errors.Errorf("failed to get startBlock for content. Tick:%v", tick)
			}
		}

		if startBlock.Height == endBlock.Height {
			return []*nom.Momentum{}, nil
		}

		store := ct.GetFrontierMomentumStore()
		blocks, err := store.GetMomentumsByHeight(startBlock.Height+1, true, endBlock.Height-startBlock.Height)
		if err != nil {
			return nil, err
		}

		// empty genesis tick
		if len(blocks) == 0 {
			return []*nom.Momentum{}, nil
		}

		// make sure proof is right
		if endBlock.Hash != blocks[len(blocks)-1].Hash {
			return nil, errors.Errorf("chainTicker.GetContent failed expects %v but got %v", endBlock.Hash, blocks[0].Hash)
		}
		return blocks, nil
	} else {
		return []*nom.Momentum{}, nil
	}
}

func newChainTicker(chain chain.Chain, ticker common.Ticker) *chainTicker {
	return &chainTicker{
		Chain:  chain,
		Ticker: ticker,
	}
}
