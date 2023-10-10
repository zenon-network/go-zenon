package embedded

import (
	"math/big"
	"sync"
	"time"

	"github.com/zenon-network/go-zenon/chain"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/consensus"
	"github.com/zenon-network/go-zenon/consensus/api"
	"github.com/zenon-network/go-zenon/zenon"
)

type ConsensusCache interface {
	Get() (weights map[string]*big.Int, currentStats *api.EpochStats)
}

type consensusCache struct {
	testing   bool
	log       common.Logger
	chain     chain.Chain
	consensus consensus.Consensus

	updating     bool
	changes      sync.Mutex
	nextTime     *time.Time
	weights      map[string]*big.Int
	currentStats *api.EpochStats
}

func (cache *consensusCache) Get() (weights map[string]*big.Int, currentStats *api.EpochStats) {
	cache.changes.Lock()
	defer cache.changes.Unlock()

	// while testing serve only hot data
	if cache.testing {
		cache.changes.Unlock()
		cache.update()
		cache.changes.Lock()
	} else if cache.shouldUpdate() {
		cache.updating = true
		go cache.update()
	}

	weights = cache.weights
	currentStats = cache.currentStats
	return
}

func (cache *consensusCache) shouldUpdate() bool {
	if cache.updating {
		return false
	}
	return cache.nextTime == nil || common.Clock.Now().After(*cache.nextTime)
}
func (cache *consensusCache) releaseUpdate() {
	cache.changes.Lock()
	defer cache.changes.Unlock()
	cache.updating = false
}
func (cache *consensusCache) update() {
	defer cache.releaseUpdate()
	startTime := common.Clock.Now()

	frontierMomentum, err := cache.chain.GetFrontierMomentumStore().GetFrontierMomentum()
	if err != nil {
		cache.log.Error("failed to get frontier momentum", "reason", err)
		return
	}
	if frontierMomentum == nil {
		cache.log.Error("failed to get frontier momentum", "reason", "frontier-momentum is missing")
		return
	}

	reader := cache.consensus.FixedPillarReader(frontierMomentum.Identifier())
	epoch := reader.EpochTicker().ToTick(*frontierMomentum.Timestamp)

	cache.log.Debug("updating rpc consensus cache", "identifier", frontierMomentum.Identifier(), "epoch", epoch)

	weights, err := reader.GetPillarWeights()
	if err != nil {
		cache.log.Error("failed to get pillar weights", "reason", err, "momentum-identifier", frontierMomentum.Identifier())
		return
	}
	stats, err := reader.EpochStats(epoch)
	if err != nil {
		cache.log.Error("failed to get epoch stats", "reason", err, "momentum-identifier", frontierMomentum.Identifier())
		return
	}

	cache.changes.Lock()
	defer cache.changes.Unlock()
	nextTime := common.Clock.Now().Add(time.Minute * 5)
	cache.weights = weights
	cache.currentStats = stats
	cache.nextTime = &nextTime

	endTime := common.Clock.Now()
	cache.log.Debug("finish updating rpc consensus", "elapsed", endTime.Sub(startTime), "next-time", nextTime)
}

func NewConsensusCache(z zenon.Zenon, testing bool) ConsensusCache {
	return &consensusCache{
		testing:   testing,
		log:       common.RPCLogger.New("submodule", "consensus-cache"),
		chain:     z.Chain(),
		consensus: z.Consensus(),
	}
}
