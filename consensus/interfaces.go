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

type ProducerEvent struct {
	StartTime time.Time
	EndTime   time.Time
	Producer  types.Address
	Name      string
}

type EventListener interface {
	NewProducerEvent(ProducerEvent)
}

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
