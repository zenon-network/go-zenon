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
	EpochDuration = time.Hour * 24
)

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

func (cs *consensus) FrontierPillarReader() api.PillarReader {
	return &API{
		momentumStore: cs.chain.GetFrontierMomentumStore(),
		er:            cs.electionManager,
		points:        cs.points,
	}
}
func (cs *consensus) FixedPillarReader(identifier types.HashHeight) api.PillarReader {
	return &API{
		momentumStore: cs.chain.GetMomentumStore(identifier),
		er:            cs.electionManager,
		points:        cs.points,
	}
}

// NewConsensus instantiates a new consensus object
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

func (cs *consensus) Init() error {
	return nil
}
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
func (cs *consensus) Stop() error {
	cs.log.Info("stopping ...")
	defer cs.log.Info("stopped")

	cs.chain.UnRegister(cs.points)
	cs.chain.UnRegister(cs.electionManager)

	close(cs.closed)
	cs.wg.Wait()
	return nil
}

// work runs in a different go routine and broadcasts ProducerEvent to all modules which called Register on EventManager
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
