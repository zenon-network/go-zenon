package pillar

import (
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/consensus"
	"github.com/zenon-network/go-zenon/wallet"
)

// Manager is the node's block producer. It runs the producer
// lifecycle (Init, Start, Stop), listens for consensus ProducerEvents
// via the embedded EventListener, and produces a momentum when an
// event names its coinbase pillar. The coinbase key is supplied
// through SetCoinBase and reported by GetCoinBase.
type Manager interface {
	// Init prepares the producer's worker before it starts listening.
	Init() error
	// Start registers the Manager as a consensus listener and starts the
	// worker so it begins producing momentums for matching events.
	Start() error
	// Stop unregisters the Manager from consensus and stops the worker.
	Stop() error

	// EventListener ensures that we can subscribe to consensus events
	// in order to produce new momentums when necessary
	consensus.EventListener

	// Process is used by the testing environment to force-process an event
	// and be able to wait for it to finish.
	Process(e consensus.ProducerEvent) common.Task

	SetCoinBase(coinbase *wallet.KeyPair)
	GetCoinBase() *types.Address
}
