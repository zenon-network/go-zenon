package zenon

import (
	"fmt"
	"time"

	"github.com/zenon-network/go-zenon/chain"
	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/protocol"
)

type EventPrinter interface {
	chain.MomentumEventListener

	Init() error
	Start() error
	Stop() error
}

type eventPrinter struct {
	chain       chain.Chain
	broadcaster protocol.Broadcaster
}

func NewEventPrinter(chain chain.Chain, broadcaster protocol.Broadcaster) EventPrinter {
	return &eventPrinter{
		chain:       chain,
		broadcaster: broadcaster,
	}
}

func (ep *eventPrinter) Init() error {
	return nil
}
func (ep *eventPrinter) Start() error {
	ep.chain.Register(ep)
	return nil
}
func (ep *eventPrinter) Stop() error {
	ep.chain.UnRegister(ep)
	return nil
}

func (ep *eventPrinter) InsertMomentum(detailed *nom.DetailedMomentum) {
	block := detailed.Momentum
	txs := 0
	if detailed.AccountBlocks != nil {
		txs = len(detailed.AccountBlocks)
	}
	if ep.broadcaster.SyncInfo().State == protocol.SyncDone || block.Height%50 == 0 {
		fmt.Printf("[Momentum inserted] Height: %d, Hash: %s, Timestamp: %d, Pillar producer address: %s, Current time: %s, Txs: %d\n", block.Height, block.Hash, block.Timestamp.Unix(), block.Producer(), time.Now().Format("2006-01-02 15:04:05"), txs)
	}
}
func (ep *eventPrinter) DeleteMomentum(detailed *nom.DetailedMomentum) {
	fmt.Printf("[Momentum deleted] Height: %v, Hash: %v\n", detailed.Momentum.Height, detailed.Momentum.Hash)
}
