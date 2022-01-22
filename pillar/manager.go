package pillar

import (
	"fmt"
	"time"

	"github.com/inconshreveable/log15"

	"github.com/zenon-network/go-zenon/chain"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/consensus"
	"github.com/zenon-network/go-zenon/protocol"
	"github.com/zenon-network/go-zenon/vm"
	"github.com/zenon-network/go-zenon/wallet"
)

type manager struct {
	log      log15.Logger
	coinbase *wallet.KeyPair

	worker *worker

	consensus   consensus.Consensus
	broadcaster protocol.Broadcaster
}

func NewPillar(chain chain.Chain, consensus consensus.Consensus, broadcaster protocol.Broadcaster) Manager {
	supervisor := vm.NewSupervisor(chain, consensus)
	return &manager{
		consensus:   consensus,
		broadcaster: broadcaster,
		worker:      newWorker(chain, supervisor, broadcaster),
		log:         common.PillarLogger.New("submodule", "manager"),
	}
}

func (m *manager) Init() error {
	m.log.Info("initializing ...")
	defer m.log.Info("initialized")

	if err := m.worker.Init(); err != nil {
		return err
	}

	return nil
}
func (m *manager) Start() error {
	m.log.Info("starting ...")
	defer m.log.Info("started")

	m.consensus.Register(m)
	if err := m.worker.Start(); err != nil {
		m.log.Error("failed to produce contracts", "reason", err)
	}

	return nil
}
func (m *manager) Stop() error {
	m.log.Info("stopping ...")
	defer m.log.Info("stopped")

	m.consensus.UnRegister(m)
	if err := m.worker.Stop(); err != nil {
		return err
	}

	return nil
}

// NewProducerEvent subscribes to consensus events which trigger
func (m *manager) NewProducerEvent(e consensus.ProducerEvent) {
	go m.processSupervised(e)
}

func (m *manager) shouldProcess(e consensus.ProducerEvent) error {
	if m.broadcaster.SyncInfo().State != protocol.SyncDone {
		return ErrSyncNotDone
	}
	if m.coinbase == nil {
		return ErrPillarNotDefined
	}
	if m.coinbase.Address != e.Producer {
		return ErrNotOurEvent
	}
	if common.Clock.Now().Before(e.StartTime) {
		return ErrEventHasNotStarted
	}
	if common.Clock.Now().After(e.EndTime) {
		return ErrEventEnded
	}
	return nil
}
func (m *manager) processSupervised(e consensus.ProducerEvent) {
	if err := m.shouldProcess(e); err != nil {
		m.log.Info("do not process current event", "event", e, "reason", err)
		return
	}

	fmt.Printf("Producing momentum ...\n")
	m.log.Info("momentum producer triggered", "event", e)
	defer m.log.Info("momentum producer trigger finished", "event", e)

	endTime := e.EndTime.Add(time.Millisecond * -250)
	task := m.worker.Process(e)
	for {
		select {
		case <-task.Finished():
			return
		case <-time.After(time.Millisecond * 100):
		}

		// Check for work expiration period
		if currentTime := time.Now(); currentTime.After(endTime) {
			m.log.Info("force-stopping producer task")
			task.ForceStop()
			break
		}
	}
}
func (m *manager) Process(e consensus.ProducerEvent) common.Task {
	// keep this section commented since it's used by the testing environment
	// when we find a nice way to move the clock in the future consider de-commenting this

	//if err := m.shouldProcess(e); err != nil {
	//	m.log.Error("do not process current event", "event", e, "reason", err)
	//	return nil
	//}
	return m.worker.Process(e)
}

func (m *manager) SetCoinBase(coinbase *wallet.KeyPair) {
	m.coinbase = coinbase
	m.worker.coinbase = coinbase
}
func (m *manager) GetCoinBase() *types.Address {
	if m.coinbase == nil {
		return nil
	}
	return &m.coinbase.Address
}
