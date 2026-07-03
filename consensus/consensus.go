// Package consensus elects which pillar produces each momentum and
// tracks how well pillars keep their production schedule.
//
// Time is divided into fixed ticks anchored at the genesis timestamp
// (common.Ticker): an election tick lasts BlockTime * NodeCount
// seconds (300 on Alphanet) and is split into NodeCount slots of
// BlockTime seconds, one momentum each; an epoch lasts EpochDuration
// (24 hours). For every tick the election manager (election.go) seeds
// a deterministic algorithm (election_algorithm.go) with a proof
// momentum taken two ticks back and assigns each slot to a pillar —
// part by delegated weight, part at random. The points system
// (points.go) records, per pillar and per tick or epoch, how many
// momentums were expected versus actually produced, along with the
// delegated weight backing the pillar.
//
// The Consensus object wires these together: it registers the
// election and points caches as chain listeners so they stay warm as
// momentums are committed, runs a goroutine that broadcasts a
// ProducerEvent at the start of every slot so the local pillar can
// produce, and verifies the producer of incoming momentums for the
// momentum verifier.
package consensus

import (
	"sync"
	"time"

	"github.com/pkg/errors"

	"github.com/zenon-network/go-zenon/chain"
	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/db"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/consensus/api"
	"github.com/zenon-network/go-zenon/consensus/storage"
	"github.com/zenon-network/go-zenon/vm/constants"
)

var (
	// EpochDuration is the length of one consensus epoch. Epochs are
	// counted from the genesis timestamp by the epoch ticker; pillar
	// production statistics (EpochStats) and the embedded contracts'
	// reward computations are aggregated per epoch.
	EpochDuration = time.Hour * 24
)

// consensus implements Consensus by composing the event manager
// (producer-event fan-out), the election manager and the points
// system, all sharing one consensus database.
type consensus struct {
	log     common.Logger
	genesis time.Time
	chain   chain.Chain
	testing bool

	*eventManager
	electionManager *electionManager
	points          Points

	wg     sync.WaitGroup
	closed chan struct{}
}

// FrontierPillarReader returns a PillarReader that follows the
// frontier momentum store, so each call observes the current chain
// head.
func (cs *consensus) FrontierPillarReader() api.PillarReader {
	return &API{
		momentumStore: cs.chain.GetFrontierMomentumStore(),
		er:            cs.electionManager,
		points:        cs.points,
	}
}

// FixedPillarReader returns a PillarReader pinned to the chain state
// identified by identifier, for reads that must be reproducible —
// e.g. the VM evaluating embedded-contract logic at a block's
// acknowledged momentum.
func (cs *consensus) FixedPillarReader(identifier types.HashHeight) api.PillarReader {
	return &API{
		momentumStore: cs.chain.GetMomentumStore(identifier),
		er:            cs.electionManager,
		points:        cs.points,
	}
}

// NewConsensus wires up the consensus module for the given chain: an
// election manager and points system backed by a consensus database
// whose caches hold roughly one week of election ticks, an epoch
// ticker anchored at the genesis timestamp, and an event manager for
// producer events. With testing true, Start skips the producer-event
// goroutine.
func NewConsensus(db db.DB, chain chain.Chain, testing bool) Consensus {
	genesisTimestamp := chain.GetGenesisMomentum().Timestamp
	epochTicker := common.NewTicker(*genesisTimestamp, EpochDuration)
	cacheSize := 7 * 24 * 60 * 60 / (constants.ConsensusConfig.BlockTime * int64(constants.ConsensusConfig.NodeCount))

	dbCache := storage.NewConsensusDB(db, int(cacheSize), int(cacheSize))
	electionManager := newElectionManager(chain, dbCache)

	return &consensus{
		log:             common.ConsensusLogger,
		genesis:         *genesisTimestamp,
		chain:           chain,
		testing:         testing,
		eventManager:    newEventManager(),
		electionManager: electionManager,
		points:          newPoints(electionManager, epochTicker, chain, dbCache),
		closed:          make(chan struct{}),
	}
}

// GetMomentumProducer returns the address elected to produce the
// momentum slot starting exactly at timestamp; it errors if timestamp
// is not a slot start time of its election tick.
func (cs *consensus) GetMomentumProducer(timestamp time.Time) (*types.Address, error) {
	election, err := cs.electionManager.ElectionByTime(timestamp)
	if err != nil {
		return nil, err
	}
	for _, plan := range election.Producers {
		if plan.StartTime == timestamp {
			return &plan.Producer, nil
		}
	}
	return nil, errors.Errorf("couldn't find producer for timestamp")
}

// VerifyMomentumProducer reports whether the momentum's producer
// (the address of its signing key) is the pillar elected for the
// momentum's timestamp. The momentum verifier rejects momentums for
// which this returns false.
func (cs *consensus) VerifyMomentumProducer(momentum *nom.Momentum) (bool, error) {
	expected, err := cs.GetMomentumProducer(*momentum.Timestamp)
	if err != nil {
		return false, err
	}
	if momentum.Producer() == *expected {
		return true, nil
	}
	return false, nil
}

// Init implements Consensus; it has nothing to initialize.
func (cs *consensus) Init() error {
	return nil
}

// Start launches the producer-event goroutine (unless in testing
// mode) and registers the points system and the election manager as
// chain momentum-event listeners.
func (cs *consensus) Start() error {
	cs.log.Info("starting ...")
	defer cs.log.Info("started")

	// enable
	if !cs.testing {
		cs.wg.Add(1)
		go func() {
			defer common.RecoverStack()
			cs.work()
			cs.wg.Done()
		}()
	}

	cs.chain.Register(cs.points)
	cs.chain.Register(cs.electionManager)
	return nil
}

// Stop unregisters the chain listeners and waits for the
// producer-event goroutine to finish.
func (cs *consensus) Stop() error {
	cs.log.Info("stopping ...")
	defer cs.log.Info("stopped")

	cs.chain.UnRegister(cs.points)
	cs.chain.UnRegister(cs.electionManager)

	close(cs.closed)
	cs.wg.Wait()
	return nil
}

// work runs in its own goroutine and broadcasts ProducerEvents to
// all listeners registered on the EventManager. After waiting for the
// genesis timestamp it loops over election ticks: it computes the
// election for the current tick, sleeps until each slot's start time
// and broadcasts that slot's event (skipping slots that already
// ended), then sleeps until the tick ends and moves to the next one.
// Events are therefore delivered at slot start, from this goroutine —
// not under the chain insert lock.
func (cs *consensus) work() {
	// wait for genesis to begin
	for (cs.chain.GetGenesisMomentum().Timestamp).After(time.Now()) {
		select {
		case <-time.After(time.Millisecond * 100):
		case <-cs.closed:
			return
		}
	}

	for {
		select {
		case <-cs.closed:
			return
		default:
		}

		tick := cs.electionManager.ToTick(time.Now())
		election, err := cs.electionManager.ElectionByTick(tick)
		if err != nil {
			cs.log.Error("can't get election result", "reason", err, "time", time.Now().Format(time.RFC3339Nano))
			select {
			case <-cs.closed:
				return
			case <-time.After(time.Second):
			}
			continue
		}

		if election.Tick != tick {
			cs.log.Error("can't get Tick election result", "tick", tick)
			continue
		}

		for _, event := range election.Producers {
			// event already ended
			if common.Clock.Now().After(event.EndTime) {
				continue
			}

			// wait for event to start
			select {
			case <-cs.closed:
				return
			case <-time.After(event.StartTime.Sub(time.Now())):
			}

			// broadcast event
			cs.eventManager.broadcastNewProducerEvent(*event)
		}

		// wait for current election to end
		select {
		case <-cs.closed:
			return
		case <-time.After(election.ETime.Sub(common.Clock.Now())):
		}
	}
}
