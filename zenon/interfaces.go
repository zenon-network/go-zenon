package zenon

import (
	"github.com/zenon-network/go-zenon/chain"
	"github.com/zenon-network/go-zenon/consensus"
	"github.com/zenon-network/go-zenon/pillar"
	"github.com/zenon-network/go-zenon/protocol"
	"github.com/zenon-network/go-zenon/verifier"
)

type Zenon interface {
	Init() error
	Start() error
	Stop() error

	Chain() chain.Chain
	Consensus() consensus.Consensus
	Verifier() verifier.Verifier
	Protocol() *protocol.ProtocolManager
	Producer() pillar.Manager
	Config() *Config
	Broadcaster() protocol.Broadcaster
}
