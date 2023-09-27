package consensus

import (
	"math/big"

	"github.com/zenon-network/go-zenon/chain/store"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/consensus/api"
)

type API struct {
	momentumStore store.Momentum
	er            ElectionReader
	points        Points
}

func (obj *API) GetPillarWeights() (map[string]*big.Int, error) {
	m, err := obj.momentumStore.GetFrontierMomentum()
	if err != nil {
		return nil, err
	}
	consensusTick := obj.points.GetPeriodPoints().ToTick(*m.Timestamp)
	if consensusTick != 0 {
		consensusTick = consensusTick - 1
	}
	point, err := obj.points.GetPeriodPoints().GetPoint(consensusTick)
	if err != nil {
		return nil, err
	}
	weights := make(map[string]*big.Int)
	for name, pillar := range point.Pillars {
		weights[name] = pillar.Weight
	}
	return weights, nil
}
func (obj *API) EpochTicker() common.Ticker {
	return obj.points.GetEpochPoints()
}
func (obj *API) EpochStats(epoch uint64) (*api.EpochStats, error) {
	point, err := obj.points.GetEpochPoints().GetPoint(epoch)
	if err != nil {
		return nil, err
	}

	if point == nil {
		return nil, nil
	}

	stats := &api.EpochStats{
		Pillars:     make(map[string]*api.EpochPillarStats),
		Epoch:       epoch,
		TotalWeight: point.TotalWeight,
	}
	for pillarName, v := range point.Pillars {
		stats.Pillars[pillarName] = &api.EpochPillarStats{
			Epoch:            epoch,
			BlockNum:         uint64(v.FactualNum),
			ExceptedBlockNum: uint64(v.ExpectedNum),
			Weight:           v.Weight,
			Name:             pillarName}
		stats.TotalBlocks += uint64(v.FactualNum)
	}
	return stats, nil
}
func (obj *API) GetPillarDelegationsByEpoch(epoch uint64) (map[string]*types.PillarDelegationDetail, error) {
	multiplier, err := obj.er.TickMultiplier(obj.EpochTicker())
	common.DealWithErr(err)

	result := make(map[string]*types.PillarDelegationDetail, 0)

	for i := uint64(0); i < multiplier; i += 1 {
		current, err := obj.er.DelegationsByTick(i + (epoch)*multiplier)
		if err != nil {
			return nil, err
		}

		// merge current into result
		for _, c := range current {
			existing, ok := result[c.Name]
			if !ok {
				result[c.Name] = c
			} else {
				existing.Merge(c)
			}
		}
	}

	for _, detail := range result {
		detail.Reduce(int64(multiplier))
	}
	return result, nil
}
