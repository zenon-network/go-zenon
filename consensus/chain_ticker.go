package consensus

import (
	"github.com/pkg/errors"

	"github.com/zenon-network/go-zenon/chain"
	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/chain/store"
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

	// frontierStore returns one snapshot of the frontier momentum store.
	// The exported methods above each take their own snapshot; the At
	// variants below answer from the snapshot they are given, so a
	// computation that needs several reads can take one snapshot and
	// pass it to every read, and a frontier that moves meanwhile (a
	// rollback and re-extension) cannot mix two branches into one
	// result.
	frontierStore() store.Momentum
	isFinishedAt(momentumStore store.Momentum, tick uint64) bool
	hasStartedAt(momentumStore store.Momentum, tick uint64) bool
	getEndBlockAt(momentumStore store.Momentum, tick uint64) (*nom.Momentum, error)
	getContentAt(momentumStore store.Momentum, tick uint64) ([]*nom.Momentum, error)
}

type chainTicker struct {
	common.Ticker
	chain.Chain
}

func (ct *chainTicker) frontierStore() store.Momentum {
	return ct.GetFrontierMomentumStore()
}

func (ct *chainTicker) IsFinished(tick uint64) bool {
	return ct.isFinishedAt(ct.frontierStore(), tick)
}

func (ct *chainTicker) HasStarted(tick uint64) bool {
	return ct.hasStartedAt(ct.frontierStore(), tick)
}

// GetEndBlock returns the head of the previous tick group.
func (ct *chainTicker) GetEndBlock(tick uint64) (*nom.Momentum, error) {
	return ct.getEndBlockAt(ct.frontierStore(), tick)
}

func (ct *chainTicker) GetContent(tick uint64) ([]*nom.Momentum, error) {
	return ct.getContentAt(ct.frontierStore(), tick)
}

func (ct *chainTicker) isFinishedAt(momentumStore store.Momentum, tick uint64) bool {
	if tick > (1<<62)-1 {
		panic("most probably an overflow error")
	}
	_, eTime := ct.ToTime(tick)
	block, err := momentumStore.GetFrontierMomentum()
	common.DealWithErr(err)
	if block.Timestamp.After(eTime) || block.Timestamp.Equal(eTime) {
		return true
	}
	return false
}

func (ct *chainTicker) hasStartedAt(momentumStore store.Momentum, tick uint64) bool {
	if tick > (1<<62)-1 {
		panic("most probably an overflow error")
	}
	sTime, _ := ct.ToTime(tick)
	block, err := momentumStore.GetFrontierMomentum()
	common.DealWithErr(err)
	if block.Timestamp.Before(sTime) {
		return false
	}
	return true
}

func (ct *chainTicker) getEndBlockAt(momentumStore store.Momentum, tick uint64) (*nom.Momentum, error) {
	if tick > (1<<62)-1 {
		panic("most probably an overflow error")
	}
	_, eTime := ct.ToTime(tick)
	block, err := momentumStore.GetMomentumBeforeTime(&eTime)
	if err != nil {
		return nil, err
	}
	if block == nil {
		return nil, errors.Errorf("chainTicker.GetEndBlock failed to get block for tick %v endTime %v", tick, eTime.Unix())
	}
	return block, err
}

func (ct *chainTicker) getContentAt(momentumStore store.Momentum, tick uint64) ([]*nom.Momentum, error) {
	if tick > (1<<62)-1 {
		panic("most probably an overflow error")
	}
	sTime, _ := ct.ToTime(tick)
	endBlock, err := ct.getEndBlockAt(momentumStore, tick)
	if err != nil {
		return nil, err
	}

	if !endBlock.Timestamp.Before(sTime) {
		var startBlock *nom.Momentum
		if tick == 0 {
			startBlock = ct.GetGenesisMomentum()
		} else {
			startBlock, err = ct.getEndBlockAt(momentumStore, tick-1)
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

		blocks, err := momentumStore.GetMomentumsByHeight(startBlock.Height+1, true, endBlock.Height-startBlock.Height)
		if err != nil {
			return nil, err
		}

		// The equal-height case, which includes the empty genesis tick,
		// returned above. endBlock was read from this store, so the
		// range up to it cannot be empty on a consistent store: an
		// empty result means the store does not hold the momentums it
		// reported, and must not be served as a tick without momentums.
		if len(blocks) == 0 {
			return nil, errors.Errorf("chainTicker.GetContent found no momentums for tick %v between heights %v and %v", tick, startBlock.Height, endBlock.Height)
		}

		// make sure proof is right
		if endBlock.Hash != blocks[len(blocks)-1].Hash {
			return nil, errors.Errorf("chainTicker.GetContent failed expects %v but got %v", endBlock.Hash, blocks[len(blocks)-1].Hash)
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
