package consensus

import (
	"github.com/pkg/errors"

	"github.com/zenon-network/go-zenon/chain"
	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common"
)

// ChainTicker couples a tick schedule (common.Ticker, anchored at
// genesis) with the momentum chain, so a tick can be resolved to the
// momentums produced during it. The points system uses chain tickers
// to map period and epoch ticks onto chain segments.
//
//   - IsFinished reports whether the frontier momentum's timestamp
//     has reached the tick's end time.
//   - HasStarted reports whether the frontier momentum's timestamp
//     has reached the tick's start time.
//   - GetEndBlock returns the latest momentum with a timestamp before
//     the tick's end time: the tick's last momentum, or one from an
//     earlier tick if this one is empty.
//   - GetContent returns the momentums whose timestamps fall inside
//     the tick, in ascending height order; empty if none were
//     produced during it. For tick 0 the genesis momentum itself is
//     excluded, even though its timestamp falls in the tick.
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

// GetEndBlock returns the latest momentum produced before the tick's
// end time; GetContent uses it (paired with the previous tick's end
// block) to delimit the tick's chain segment.
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
