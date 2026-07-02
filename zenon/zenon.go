// Package zenon is the node's central orchestrator: it owns the
// chain, consensus, verifier, protocol, pillar producer and
// broadcaster and drives their shared lifecycle.
//
// NewZenon wires the subsystems together — the chain over its nom
// db.Manager, consensus over its own leveldb, the verifier over both,
// then the protocol manager (fed by a chain bridge and VM
// supervisor), the broadcaster, the event printer, the RPC
// subscription server and the pillar. Init and Start then run the
// subsystems in dependency order — chain, consensus, event printer,
// subscription server, pillar — with the protocol manager started
// last (its Init call is commented out); Stop tears them down in
// reverse,
// closing the consensus leveldb at the end. The Zenon interface
// exposes each subsystem to the node shell and the RPC APIs.
package zenon

import (
	"github.com/syndtr/goleveldb/leveldb"

	"github.com/zenon-network/go-zenon/chain"
	"github.com/zenon-network/go-zenon/consensus"
	"github.com/zenon-network/go-zenon/pillar"
	"github.com/zenon-network/go-zenon/protocol"
	"github.com/zenon-network/go-zenon/rpc/api/subscribe"
	"github.com/zenon-network/go-zenon/verifier"
	"github.com/zenon-network/go-zenon/vm"
)

type zenon struct {
	config *Config

	protocol    *protocol.ProtocolManager
	subscribe   *subscribe.Server
	verifier    verifier.Verifier
	chain       chain.Chain
	pillar      pillar.Manager
	consensus   consensus.Consensus
	evPrinter   EventPrinter
	broadcaster protocol.Broadcaster
	levelDb     *leveldb.DB
}

// NewZenon constructs a Zenon from cfg, wiring every subsystem but
// starting none: the chain, consensus, verifier, protocol manager
// (with its chain bridge and VM supervisor), broadcaster, event
// printer, RPC subscription server and pillar. When the config names
// a producing key pair it is set as the pillar's coinbase. Call Init
// then Start to bring the node online.
func NewZenon(cfg *Config) (Zenon, error) {
	z := &zenon{
		config: cfg,
	}

	z.chain = chain.NewChain(cfg.NewDBManager("nom"), cfg.GenesisConfig)
	db, levelDb := cfg.NewLevelDB("consensus")
	z.consensus = consensus.NewConsensus(db, z.chain, false)
	z.verifier = verifier.NewVerifier(z.chain, z.consensus)
	z.levelDb = levelDb

	chainBridge := protocol.NewChainBridge(z.chain, z.consensus, z.verifier, vm.NewSupervisor(z.chain, z.consensus))
	z.protocol = protocol.NewProtocolManager(cfg.MinPeers, z.chain.ChainIdentifier(), chainBridge)
	z.broadcaster = protocol.NewBroadcaster(z.chain, z.protocol)

	z.evPrinter = NewEventPrinter(z.chain, z.broadcaster)
	z.subscribe = subscribe.GetSubscribeServer(z.chain)
	z.pillar = pillar.NewPillar(z.chain, z.consensus, z.broadcaster)

	if cfg.ProducingKeyPair != nil {
		z.pillar.SetCoinBase(cfg.ProducingKeyPair)
	}

	return z, nil
}

func (z *zenon) Init() error {
	if err := z.chain.Init(); err != nil {
		return err
	}
	if err := z.consensus.Init(); err != nil {
		return err
	}
	if err := z.evPrinter.Init(); err != nil {
		return err
	}
	if err := z.subscribe.Init(); err != nil {
		return err
	}
	//z.protocol.Init()
	if err := z.pillar.Init(); err != nil {
		return err
	}

	return nil
}
func (z *zenon) Start() error {
	if err := z.chain.Start(); err != nil {
		return err
	}
	if err := z.consensus.Start(); err != nil {
		return err
	}
	if err := z.evPrinter.Start(); err != nil {
		return err
	}
	if err := z.subscribe.Start(); err != nil {
		return err
	}
	if err := z.pillar.Start(); err != nil {
		return err
	}
	z.protocol.Start()

	return nil
}
func (z *zenon) Stop() error {
	z.protocol.Stop()
	if err := z.pillar.Stop(); err != nil {
		return err
	}
	if err := z.subscribe.Stop(); err != nil {
		return err
	}
	if err := z.evPrinter.Stop(); err != nil {
		return err
	}
	if err := z.consensus.Stop(); err != nil {
		return err
	}
	if err := z.chain.Stop(); err != nil {
		return err
	}
	if err := z.levelDb.Close(); err != nil {
		return err
	}

	return nil
}

func (z *zenon) Chain() chain.Chain {
	return z.chain
}
func (z *zenon) Producer() pillar.Manager {
	return z.pillar
}
func (z *zenon) Consensus() consensus.Consensus {
	return z.consensus
}
func (z *zenon) Verifier() verifier.Verifier {
	return z.verifier
}
func (z *zenon) Protocol() *protocol.ProtocolManager {
	return z.protocol
}
func (z *zenon) Config() *Config {
	return z.config
}
func (z *zenon) Broadcaster() protocol.Broadcaster {
	return z.broadcaster
}
