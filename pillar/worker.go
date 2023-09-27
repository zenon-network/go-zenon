package pillar

import (
	"sync"
	"time"

	"github.com/zenon-network/go-zenon/chain"
	"github.com/zenon-network/go-zenon/chain/store"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/consensus"
	"github.com/zenon-network/go-zenon/protocol"
	"github.com/zenon-network/go-zenon/vm"
	"github.com/zenon-network/go-zenon/wallet"
)

// worker takes care of generating receive blocks for contracts.
// Init Start Stop endpoints are used to control the worker.
// The worker needs to be started for every event and it automatically stops when the event ends.
type worker struct {
	log      common.Logger
	closed   chan struct{}
	working  sync.Mutex
	children sync.WaitGroup

	contracts []types.Address
	coinbase  *wallet.KeyPair

	// modules
	chain       chain.Chain
	supervisor  *vm.Supervisor
	broadcaster protocol.Broadcaster
}

func newWorker(chain chain.Chain, supervisor *vm.Supervisor, broadcaster protocol.Broadcaster) *worker {
	return &worker{
		log:         common.PillarLogger.New("submodule", "worker"),
		contracts:   types.EmbeddedContracts,
		supervisor:  supervisor,
		chain:       chain,
		broadcaster: broadcaster,
	}
}

func (w *worker) Init() error {
	return nil
}
func (w *worker) Start() error {
	w.closed = make(chan struct{})
	w.log.Info("start contract worker")

	return nil
}
func (w *worker) Stop() error {
	close(w.closed)

	w.log.Info("stop all task")
	w.children.Wait()
	w.log.Info("end stop all task")
	w.log.Info("stopped")

	return nil
}
func (w *worker) shouldStop() bool {
	select {
	case <-w.closed:
		return true
	default:
	}
	return false
}

func (w *worker) Process(e consensus.ProducerEvent) common.Task {
	w.children.Add(1)
	w.working.Lock()

	if w.shouldStop() {
		w.children.Done()
		w.working.Unlock()
		return nil
	}

	task := common.NewTask(func(task common.TaskResolver) {
		defer common.RecoverStack()
		w.work(task, e)
		w.children.Done()
		w.working.Unlock()
	})

	return task
}

func (w *worker) work(task common.TaskResolver, e consensus.ProducerEvent) {
	var momentumStore store.Momentum

	w.log.Info("producing momentum", "event", e)
	momentum, err := w.generateMomentum(e)
	if err != nil {
		w.log.Error("failed to generate momentum", "reason", err)
		return
	}

	if task.ShouldStop() {
		return
	}
	if w.shouldStop() {
		return
	}
	if common.Clock.Now().After(e.StartTime.Add(3 * time.Second)) {
		w.log.Error("do not broadcast own momentum", "identifier", momentum.Momentum.Identifier(), "reason", "too-late")
	} else {
		w.log.Info("broadcasting own momentum", "identifier", momentum.Momentum.Identifier())
		w.broadcaster.CreateMomentum(momentum)
	}

	if task.ShouldStop() {
		return
	}
	if w.shouldStop() {
		return
	}
	w.log.Info("start creating autoreceive blocks")
	momentumStore = w.chain.GetFrontierMomentumStore()
	for {
		one := false
		for _, contractAddress := range w.contracts {
			if task.ShouldStop() {
				return
			}
			if w.shouldStop() {
				return
			}

			transaction, err := w.generateNext(momentumStore, contractAddress)
			if err == ErrNothingToGenerate {
				continue
			}
			if err != nil {
				w.log.Error("unable to generate receive block for contract", "reason", err)
				return
			}
			w.broadcaster.CreateAccountBlock(transaction)
			w.log.Info("created autoreceive-block", "identifier", transaction.Block.Header())

			one = true
		}
		if !one {
			break
		}
	}

	if task.ShouldStop() {
		return
	}
	if w.shouldStop() {
		return
	}
	w.log.Info("checking if can update contracts")
	momentumStore = w.chain.GetFrontierMomentumStore()
	if err := w.updateContracts(momentumStore); err != nil {
		w.log.Error("failed to update contracts", "reason", err)
		return
	}
}
