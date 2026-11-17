package consensus

import (
	"math/big"

	"github.com/pkg/errors"

	"github.com/zenon-network/go-zenon/chain/store"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/consensus/api"
)

// API implements api.PillarReader on top of a momentum store — the
// frontier or a fixed snapshot, depending on whether it came from
// FrontierPillarReader or FixedPillarReader — combined with the
// election reader and the points system. Every read is answered from
// momentumStore: the points and delegations are resolved against its
// chain, not against the live frontier, so a reader fixed at a momentum
// answers for that momentum even after the frontier has moved on or
// rolled back. The rpc embedded pillar API serves its results through
// a consensus cache refreshed at most every five minutes.
type API struct {
	momentumStore store.Momentum
	er            ElectionReader
	points        Points
}

// GetPillarWeights returns the delegated weight of every pillar,
// keyed by pillar name, read from the period point of the last
// completed election tick at the store's frontier momentum; during
// the very first tick, which has no completed predecessor, it reads
// the current tick instead.
func (obj *API) GetPillarWeights() (map[string]*big.Int, error) {
	if obj.momentumStore == nil {
		return nil, errNoMomentumStore
	}
	m, err := obj.momentumStore.GetFrontierMomentum()
	if err != nil {
		return nil, err
	}
	consensusTick := obj.points.GetPeriodPoints().ToTick(*m.Timestamp)
	if consensusTick != 0 {
		consensusTick = consensusTick - 1
	}
	point, err := obj.points.GetPeriodPoints().getPointAt(obj.momentumStore, consensusTick)
	if err != nil {
		return nil, err
	}
	if point == nil {
		return nil, errors.Errorf("no period point for tick %v at momentum %v", consensusTick, m.Identifier())
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
	point, err := obj.points.GetEpochPoints().getPointAt(obj.momentumStore, epoch)
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
			ExpectedBlockNum: uint64(v.ExpectedNum),
			Weight:           v.Weight,
			Name:             pillarName}
		stats.TotalBlocks += uint64(v.FactualNum)
	}
	return stats, nil
}

// FinishedPeriodStats returns the statistics of the last count election
// ticks that ended at or before the store's frontier momentum, oldest
// first. Every tick is read from its period point at momentumStore, and
// each pillar's eligibility from the jailed pillars at the tick's proof
// momentum on the same chain, so the result depends only on the chain
// up to that momentum.
func (obj *API) FinishedPeriodStats(count uint64) ([]*api.PeriodStats, error) {
	if obj.momentumStore == nil {
		return nil, errNoMomentumStore
	}
	frontier, err := obj.momentumStore.GetFrontierMomentum()
	if err != nil {
		return nil, err
	}
	period := obj.points.GetPeriodPoints()
	current := period.ToTick(*frontier.Timestamp)
	first := uint64(0)
	if current > count {
		first = current - count
	}
	stats := make([]*api.PeriodStats, 0, current-first)
	for tick := first; tick < current; tick++ {
		point, err := period.getPointAt(obj.momentumStore, tick)
		if err != nil {
			return nil, err
		}
		if point == nil {
			return nil, errors.Errorf("no period point for tick %v at momentum %v", tick, frontier.Identifier())
		}
		jailed, err := obj.er.jailedAtProof(obj.momentumStore, tick)
		if err != nil {
			return nil, err
		}
		start, _ := period.ToTime(tick)
		tickStats := &api.PeriodStats{
			Tick:      tick,
			StartTime: start.Unix(),
			Pillars:   make(map[string]*api.PeriodPillarStats, len(point.Pillars)),
		}
		for name, v := range point.Pillars {
			tickStats.Pillars[name] = &api.PeriodPillarStats{
				BlockNum:         uint64(v.FactualNum),
				ExpectedBlockNum: uint64(v.ExpectedNum),
				Eligible:         !jailed[name],
			}
		}
		stats = append(stats, tickStats)
	}
	return stats, nil
}

// GetPillarDelegationsByEpoch returns each pillar's delegations
// averaged over the epoch: the detailed delegations are sampled at
// every election tick of the epoch, merged per pillar and divided by
// the number of ticks (see PillarDelegationDetail Merge and Reduce).
func (obj *API) GetPillarDelegationsByEpoch(epoch uint64) (map[string]*types.PillarDelegationDetail, error) {
	multiplier, err := obj.er.TickMultiplier(obj.EpochTicker())
	if err != nil {
		return nil, err
	}

	result := make(map[string]*types.PillarDelegationDetail, 0)

	for i := uint64(0); i < multiplier; i += 1 {
		current, err := obj.er.delegationsByTickAt(obj.momentumStore, i+(epoch)*multiplier)
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
