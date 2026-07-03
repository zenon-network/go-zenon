package consensus

import (
	"time"

	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/consensus/api"
)

// Verifier checks momentum production rights:
// VerifyMomentumProducer reports whether a momentum was produced by
// the pillar elected for its timestamp. The momentum verifier calls
// it on every momentum before insertion.
type Verifier interface {
	VerifyMomentumProducer(momentum *nom.Momentum) (bool, error)
}

// ProducerEvent is one momentum-production slot of an election tick:
// the pillar named Name, producing from address Producer, is expected
// to produce exactly one momentum in the BlockTime-second window
// [StartTime, EndTime).
type ProducerEvent struct {
	StartTime time.Time
	EndTime   time.Time
	Producer  types.Address
	Name      string
}

// EventListener receives every ProducerEvent at its start time. The
// consensus module invokes listeners synchronously on its own
// event-broadcasting goroutine — not under the chain insert lock —
// while holding the event manager's mutex, so callbacks must return
// quickly; the pillar manager (the only production listener) hands
// the event off to a fresh goroutine and produces a momentum if the
// slot belongs to its coinbase.
type EventListener interface {
	NewProducerEvent(ProducerEvent)
}

// EventManager registers and unregisters producer-event listeners;
// Consensus embeds it.
type EventManager interface {
	Register(callback EventListener)
	UnRegister(callback EventListener)
}

// Consensus is the consensus module's public surface, implemented by
// NewConsensus and consumed by the zenon orchestrator. Init does
// nothing; Start launches the producer-event goroutine and registers
// the points system and election manager as chain listeners; Stop
// unregisters them and waits for the goroutine to drain.
//
// GetMomentumProducer returns the address elected to produce the
// momentum slot starting exactly at timestamp. FrontierPillarReader
// reads pillar statistics at the current frontier momentum, while
// FixedPillarReader pins them to the chain state identified by the
// given hash-height — the VM uses the latter to evaluate
// embedded-contract logic at a block's acknowledged momentum.
type Consensus interface {
	Verifier
	EventManager

	Init() error
	Start() error
	Stop() error

	GetMomentumProducer(timestamp time.Time) (*types.Address, error)

	FrontierPillarReader() api.PillarReader
	FixedPillarReader(types.HashHeight) api.PillarReader
}
