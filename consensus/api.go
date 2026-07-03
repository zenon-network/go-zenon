package consensus

import (
	"math/big"

	"github.com/zenon-network/go-zenon/chain/store"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/consensus/api"
)

// API implements api.PillarReader on top of a momentum store — the
// frontier or a fixed snapshot, depending on whether it came from
// FrontierPillarReader or FixedPillarReader — combined with the
// election reader and the points system. The rpc embedded pillar API
// serves its results through a consensus cache refreshed at most
// every five minutes.
type API struct {
	momentumStore store.Momentum
	er            ElectionReader
	points        Points
}

// GetPillarWeights returns the delegated weight of every pillar,
// keyed by pillar name, read from the period point of the last
// completed election tick at the store's frontier momentum.
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

// EpochTicker returns the ticker of the epoch schedule: EpochDuration
// (24-hour) ticks anchored at the genesis timestamp.
func (obj *API) EpochTicker() common.Ticker {
	return obj.points.GetEpochPoints()
}

// EpochStats returns the production statistics of an epoch from its
// epoch point: per pillar, the momentums produced and expected and
// the average delegated weight, plus epoch-wide totals. It returns
// nil, nil for an epoch that has not started.
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

// GetPillarDelegationsByEpoch returns each pillar's delegations
// averaged over the epoch: the detailed delegations are sampled at
// every election tick of the epoch, merged per pillar and divided by
// the number of ticks (see PillarDelegationDetail Merge and Reduce).
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
