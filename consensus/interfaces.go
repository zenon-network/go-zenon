package consensus

import (
	"time"

	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/consensus/api"
)

// Verifier is the interface that can verify block consensus.
type Verifier interface {
	VerifyMomentumProducer(momentum *nom.Momentum) (bool, error)
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
