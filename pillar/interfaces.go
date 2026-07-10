package pillar

import (
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/consensus"
	"github.com/zenon-network/go-zenon/wallet"
)

type Manager interface {
	Init() error
	Start() error
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
