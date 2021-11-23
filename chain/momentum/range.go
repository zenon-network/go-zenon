package momentum

import (
	"sort"
	"time"

	"github.com/pkg/errors"

	"github.com/zenon-network/go-zenon/chain/nom"
)

func (ms *momentumStore) GetMomentumBeforeTime(timestamp *time.Time) (*nom.Momentum, error) {
	// normal logic
	genesis := ms.GetGenesisMomentum()
	frontierMomentum, err := ms.GetFrontierMomentum()
	if err != nil {
		return nil, err
	}

	timeNanosecond := timestamp.UnixNano()
	if genesis.Timestamp.UnixNano() >= timeNanosecond {
		return nil, nil
	}
	if frontierMomentum.Timestamp.UnixNano() < timeNanosecond {
		return frontierMomentum, nil
	}

	endSec := frontierMomentum.Timestamp.Unix()
	timeSec := timestamp.Unix()

	gap := uint64(endSec - timeSec)
	estimateHeight := uint64(1)
	if frontierMomentum.Height > gap {
		estimateHeight = frontierMomentum.Height - gap
	}

	var highBoundary, lowBoundary *nom.Momentum

	for highBoundary == nil || lowBoundary == nil {
		block, err := ms.GetMomentumByHeight(estimateHeight)
		if err != nil {
			return nil, errors.Errorf("GetMomentumByHeight failed; reason: %v; height: %v", err, estimateHeight)
		}

		if block == nil {
			return nil, errors.Errorf("GetMomentumByHeight failed; reason: block is nil;  height: %d", estimateHeight)
		}

		if block.Timestamp.UnixNano() >= timeNanosecond {
			highBoundary = block
			gap := uint64(block.Timestamp.Unix() - timeSec)
			if gap <= 0 {
				gap = 1
			}

			if block.Height <= gap {
				lowBoundary = genesis
				break
			} else {
				estimateHeight = block.Height - gap
			}

		} else {
			lowBoundary = block
			estimateHeight = block.Height + uint64(timeSec-block.Timestamp.Unix())
			if estimateHeight > frontierMomentum.Height {
				highBoundary = frontierMomentum
			}
		}
	}

	if highBoundary.Height == lowBoundary.Height+1 {
		return lowBoundary, nil
	}
	block, err := ms.binarySearchBeforeTime(lowBoundary, highBoundary, timeNanosecond)
	if err != nil {
		cErr := errors.Errorf("binarySearchBeforeTime failed; reason: %v; lowBoundary: %v, highBoundary: %v; timeNanosecond:  %d", err, lowBoundary, highBoundary, timeNanosecond)
		return nil, cErr
	}
	return block, nil
}
func (ms *momentumStore) binarySearchBeforeTime(start, end *nom.Momentum, timeNanosecond int64) (*nom.Momentum, error) {
	n := int(end.Height - start.Height + 1)

	var err error
	blockMap := make(map[int]*nom.Momentum, n)

	i := sort.Search(n, func(i int) bool {
		if err != nil {
			return true
		}
		height := start.Height + uint64(i)

		blockMap[i], err = ms.GetMomentumByHeight(height)
		if err != nil {
			return true
		}
		if blockMap[i] == nil {
			err = errors.Errorf("GetMomentumByHeight failed; reason: block is nill")
			return true
		}

		return blockMap[i].Timestamp.UnixNano() >= timeNanosecond

	})

	if err != nil {
		return nil, err
	}
	if i >= n {
		return nil, nil
	}

	if block, ok := blockMap[i-1]; ok {
		return block, nil
	}
	return ms.GetMomentumByHeight(start.Height + uint64(i-1))
}
