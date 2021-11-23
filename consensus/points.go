package consensus

import (
	"math/big"

	"github.com/zenon-network/go-zenon/chain"
	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/consensus/storage"
)

type Points interface {
	// MomentumEventListener is used to precompute points as momentums come, so API calls have hot data
	chain.MomentumEventListener
	GetPeriodPoints() PointsReader
	GetEpochPoints() PointsReader
}

type points struct {
	log          common.Logger
	epochPoints  PointsReader
	periodPoints PointsReader

	lastCompletedPeriod int64
	lastCompletedEpoch  int64
	epochTickMultiplier int64
}

func newPoints(electionReader ElectionReader, epochTicker common.Ticker, ch chain.Chain, db *storage.DB) Points {
	periodPoints := newPeriodPoints(electionReader, newChainTicker(ch, electionReader), db)
	epochPoints := newCompoundPoints(periodPoints, newChainTicker(ch, epochTicker), db, storage.PrefixEpochPoint)

	var lastCompletedPeriod int64 = -1
	// Do a binary search to determine the last completed period based on DB
	for i := 30; i >= 0; i -= 1 {
		now := lastCompletedPeriod + (1 << i)
		p, err := db.GetPointByHeight(storage.PrefixPeriodPoint, uint64(now))
		if err != nil {
			panic(err)
		}
		if p != nil {
			lastCompletedPeriod = now
		}
	}

	epochTickMultiplier, err := periodPoints.TickMultiplier(epochPoints)
	if err != nil {
		panic(err)
	}

	lastCompletedEpoch := (lastCompletedPeriod / int64(epochTickMultiplier)) - 1

	return &points{
		log:                 common.ConsensusLogger.New("submodule", "points"),
		periodPoints:        periodPoints,
		epochPoints:         epochPoints,
		lastCompletedPeriod: lastCompletedPeriod,
		lastCompletedEpoch:  lastCompletedEpoch,
		epochTickMultiplier: int64(epochTickMultiplier),
	}
}

func (p *points) GetPeriodPoints() PointsReader {
	return p.periodPoints
}
func (p *points) GetEpochPoints() PointsReader {
	return p.epochPoints
}

func (p *points) InsertMomentum(detailed *nom.DetailedMomentum) {
	block := detailed.Momentum

	tick := int64(p.periodPoints.ToTick(*block.Timestamp))
	epochTick := tick / p.epochTickMultiplier

	// update period ticks
	for i := p.lastCompletedPeriod + 1; i < tick; i += 1 {
		p.log.Debug("create period point", "tick", i)
		_, err := p.periodPoints.GetPoint(uint64(i))
		if err != nil {
			p.log.Error("failed to get point", "tick", i, "reason", err)
			return
		}
	}
	if p.lastCompletedPeriod < tick-1 {
		p.lastCompletedPeriod = tick - 1
	}

	// update epoch ticks
	for i := p.lastCompletedEpoch + 1; i < epochTick; i += 1 {
		p.log.Info("create epoch point", "tick", i)
		_, err := p.epochPoints.GetPoint(uint64(i))
		if err != nil {
			p.log.Error("failed to get point", "tick", i, "reason", err)
			return
		}
	}
	if p.lastCompletedEpoch < epochTick-1 {
		p.lastCompletedEpoch = epochTick - 1
	}
}
func (p *points) DeleteMomentum(*nom.DetailedMomentum) {
}

// PointsReader can read pillar statistics of epoch or period
type PointsReader interface {
	common.Ticker
	// Returns nil, nil for points which are in the future
	GetPoint(tick uint64) (*storage.Point, error)
}

func newCompoundPoints(lower PointsReader, chainTicker ChainTicker, db *storage.DB, prefix byte) PointsReader {
	lowerMultiplier, err := lower.TickMultiplier(chainTicker)
	if err != nil {
		panic(err)
	}

	return &compoundPoints{
		ChainTicker:     chainTicker,
		db:              db,
		log:             common.ConsensusLogger.New("submodule", "compound-points", "compound-prefix", prefix),
		prefix:          prefix,
		lowerMultiplier: lowerMultiplier,
		lower:           lower,
	}
}

type compoundPoints struct {
	ChainTicker
	db     *storage.DB
	log    common.Logger
	prefix byte

	// number of lower ticks that make a current tick
	lowerMultiplier uint64
	lower           PointsReader
}

func (compound *compoundPoints) GetPoint(tick uint64) (*storage.Point, error) {
	if !compound.HasStarted(tick) {
		return nil, nil
	}

	endBlock, err := compound.GetEndBlock(tick)
	if err != nil {
		return nil, err
	}

	dbPoint, err := compound.db.GetPointByHeight(compound.prefix, tick)
	if err != nil {
		return nil, err
	}
	if dbPoint != nil {
		if dbPoint.EndHash != endBlock.Hash {
			// invalidate DB & cache
			err := compound.db.DeletePointByHeight(compound.prefix, tick)
			if err != nil {
				return nil, err
			}
		} else {
			return dbPoint, nil
		}
	}

	point, err := compound.generatePointFromLower(tick, endBlock)
	if err != nil {
		return nil, err
	}

	// Store point in DB if finished
	if compound.IsFinished(tick) {
		err := compound.db.StorePointByHeight(compound.prefix, tick, point)
		if err != nil {
			return nil, err
		}
	}

	return point, nil
}
func (compound *compoundPoints) generatePointFromLower(tick uint64, endBlock *nom.Momentum) (*storage.Point, error) {
	result := storage.NewEmptyPoint(endBlock.Hash)
	start := tick * compound.lowerMultiplier
	end := start + compound.lowerMultiplier
	var numPresent int64 = 0

	for i := end - 1; ; i-- {
		p, err := compound.lower.GetPoint(i)
		if err != nil {
			return nil, err
		}
		// Future point
		if p == nil {
			continue
		}

		numPresent += 1
		if err := result.LeftAppend(p); err != nil {
			return nil, err
		}
		if i == start {
			break
		}
	}

	// Divide weight by lowerMultiplier
	result.TotalWeight.Set(big.NewInt(0))
	bigRate := big.NewInt(numPresent)
	for _, p := range result.Pillars {
		p.Weight.Quo(p.Weight, bigRate)
		result.TotalWeight.Add(result.TotalWeight, p.Weight)
	}

	return result, nil
}

type periodPoints struct {
	ChainTicker
	db  *storage.DB
	log common.Logger

	electionReader ElectionReader
}

func newPeriodPoints(electionReader ElectionReader, ticker ChainTicker, db *storage.DB) PointsReader {
	return &periodPoints{
		ChainTicker:    ticker,
		db:             db,
		electionReader: electionReader,
		log:            common.ConsensusLogger.New("submodule", "period-points"),
	}
}

func (period *periodPoints) GetPoint(tick uint64) (*storage.Point, error) {
	if !period.HasStarted(tick) {
		return nil, nil
	}

	endBlock, err := period.GetEndBlock(tick)
	if err != nil {
		return nil, err
	}

	dbPoint, err := period.db.GetPointByHeight(storage.PrefixPeriodPoint, tick)
	if err != nil {
		return nil, err
	}
	if dbPoint != nil {
		if dbPoint.EndHash != endBlock.Hash {
			// invalidate DB & cache
			err := period.db.DeletePointByHeight(storage.PrefixPeriodPoint, tick)
			if err != nil {
				return nil, err
			}
		} else {
			return dbPoint, nil
		}
	}

	point, err := period.generatePointFromChain(tick)
	if err != nil {
		return nil, err
	}

	// Store point in DB if finished
	if period.IsFinished(tick) {
		err := period.db.StorePointByHeight(storage.PrefixPeriodPoint, tick, point)
		if err != nil {
			return nil, err
		}
	}

	return point, nil
}
func (period *periodPoints) generatePointFromChain(tick uint64) (*storage.Point, error) {
	election, err := period.electionReader.ElectionByTick(tick)
	if err != nil {
		return nil, err
	}

	endBlock, err := period.GetEndBlock(tick)
	if err != nil {
		return nil, err
	}
	point := storage.NewEmptyPoint(endBlock.Hash)

	blocks, err := period.GetContent(tick)
	if err != nil {
		return nil, err
	}

	if len(blocks) != 0 {
		// set up hashes in point
		point.PrevHash = blocks[0].PreviousHash
		point.EndHash = blocks[len(blocks)-1].Hash

		// set up name-lookup
		nameLookup := make(map[types.Address]string)
		for _, el := range election.Producers {
			nameLookup[el.Producer] = el.Name
		}

		// add produced blocks to count
		for _, v := range blocks {
			pillar, ok := point.Pillars[nameLookup[v.Producer()]]
			if !ok {
				point.Pillars[nameLookup[v.Producer()]] = &storage.ProducerDetail{ExpectedNum: 0, FactualNum: 1, Weight: big.NewInt(0)}
			} else {
				pillar.AddNum(0, 1)
			}
		}
	}

	// Add expected blocks
	for _, v := range election.Producers {
		pillar, ok := point.Pillars[v.Name]
		if !ok {
			point.Pillars[v.Name] = &storage.ProducerDetail{ExpectedNum: 1, FactualNum: 0, Weight: big.NewInt(0)}
		} else {
			pillar.AddNum(1, 0)
		}
	}

	// Add weight
	for _, delegation := range election.Delegations {
		point.TotalWeight.Add(point.TotalWeight, delegation.Weight)
		pillar, ok := point.Pillars[delegation.Name]
		if !ok {
			point.Pillars[delegation.Name] = &storage.ProducerDetail{ExpectedNum: 0, FactualNum: 0, Weight: big.NewInt(0).Set(delegation.Weight)}
		} else {
			pillar.Weight.Add(pillar.Weight, delegation.Weight)
		}
	}
	return point, nil
}
