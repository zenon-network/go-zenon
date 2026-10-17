package consensus

import (
	"time"

	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/consensus/api"
)

// Verifier is the interface that can verify block consensus.
type Verifier interface {
	// VerifyMomentumProducer checks the momentum against the election the
	// current frontier implies.
	VerifyMomentumProducer(momentum *nom.Momentum) (bool, error)
	// VerifyMomentumProducerAt checks the momentum against the election a
	// chain ending at frontier implies. The election for a slot is seeded
	// by the last momentum before a proof time two ticks earlier, so a
	// momentum that extends a momentum below the current frontier has to be
	// judged by the chain it belongs to, which ends at its previous
	// momentum, not by the momentums above that fork point.
	VerifyMomentumProducerAt(frontier types.HashHeight, momentum *nom.Momentum) (bool, error)
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

// Consensus include all interface for consensus
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
