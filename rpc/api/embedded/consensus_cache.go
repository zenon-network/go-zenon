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

// ConsensusCache caches the consensus data served alongside pillar
// contract state by PillarApi: the voting weight of every pillar and
// the per-pillar momentum-production statistics of the current epoch,
// both computed at the frontier momentum. Recomputing them walks
// consensus state and is too slow to do on every RPC call, so results
// are cached and refreshed in the background at most every five
// minutes.
type ConsensusCache interface {
	// Get returns the cached pillar voting weights, keyed by pillar
	// name, and the statistics of the epoch containing the frontier
	// momentum. It returns immediately with the cached values; if the
	// cache is stale (last refreshed more than five minutes ago) and no
	// refresh is in flight, it also starts one asynchronously, so the
	// returned data can lag the chain by up to five minutes plus one
	// refresh. Until the first refresh completes both results are nil.
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

// NewConsensusCache returns a ConsensusCache bound to the given node's
// chain and consensus. The cache starts empty and fills on first use.
// With testing true, Get recomputes the data synchronously on every
// call instead of refreshing in the background, so tests always observe
// the current chain state. It is called when constructing PillarApi; it
// is not itself an RPC method.
func NewConsensusCache(z zenon.Zenon, testing bool) ConsensusCache {
	return &consensusCache{
		testing:   testing,
		log:       common.RPCLogger.New("submodule", "consensus-cache"),
		chain:     z.Chain(),
		consensus: z.Consensus(),
	}
}
