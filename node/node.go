package node

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/prometheus/tsdb/fileutil"

	"github.com/zenon-network/go-zenon/chain"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/p2p"
	"github.com/zenon-network/go-zenon/p2p/libp2p"
	"github.com/zenon-network/go-zenon/p2p/switcher"
	api "github.com/zenon-network/go-zenon/rpc"
	rpc "github.com/zenon-network/go-zenon/rpc/server"
	"github.com/zenon-network/go-zenon/wallet"
	"github.com/zenon-network/go-zenon/zenon"
)

var (
	log = common.NodeLogger
)

// Node is chain container that manages p2p、rpc、zenon modules
type Node struct {
	config *Config

	walletManager *wallet.Manager
	server        p2p.Server

	z zenon.Zenon

	rpcAPIs []rpc.API   // List of APIs currently provided by the node
	http    *httpServer //
	ws      *httpServer //

	// Channel to wait for termination notifications
	stop        chan struct{}
	lock        sync.RWMutex
	dataDirLock fileutil.Releaser // prevents concurrent use of instance directory
}

func NewNode(conf *Config) (*Node, error) {
	var err error

	node := &Node{
		config:        conf,
		stop:          make(chan struct{}),
		walletManager: wallet.New(conf.makeWalletConfig()),
		http:          newHTTPServer(rpc.DefaultHTTPTimeouts),
		ws:            newHTTPServer(rpc.DefaultHTTPTimeouts),
	}

	// prepare node
	log.Info("preparing node ... ")
	if err = node.openDataDir(); err != nil {
		return nil, err
	}

	// start wallet
	if err = node.startWallet(); err != nil {
		log.Error("failed to start wallet", "reason", err)
		return nil, err
	}

	// Initialize the zenon rpc
	zenonConfig, err := node.config.makeZenonConfig(node.walletManager)
	if err != nil {
		return nil, err
	}
	node.z, err = zenon.NewZenon(zenonConfig)
	if err != nil {
		log.Error("failed to create zenon", "reason", err)
		return nil, err
	}

	netConfig := conf.makeNetConfig()

	// Parse both bootstrap lists up front so a malformed entry is caught
	// before the server starts. The switcher owns both: it picks which
	// list to use based on the spork's current state on this node's
	// local chain.

	// Legacy: enode:// URLs → []*discover.Node.
	legacyBootstrap, err := netConfig.Nodes()
	if err != nil {
		return nil, fmt.Errorf("parse legacy seeders: %w", err)
	}

	// libp2p: multiaddr strings → []peer.AddrInfo. ParseBootstrapPeers
	// tolerates accidentally-pasted enode:// entries by skipping them
	// with a warning, so this is robust to a partially-migrated config.
	libp2pBootstrap, err := libp2p.ParseBootstrapPeers(netConfig.BootstrapPeers)
	if err != nil {
		return nil, fmt.Errorf("parse libp2p bootstrap peers: %w", err)
	}

	node.server = &switcher.Server{
		PrivateKey:        netConfig.PrivateKey(),
		Name:              netConfig.Name,
		MaxPeers:          netConfig.MaxPeers,
		MinConnectedPeers: netConfig.MinConnectedPeers,
		MaxPendingPeers:   netConfig.MaxPendingPeers,
		ListenAddr:        fmt.Sprintf("%v:%v", netConfig.ListenAddr, netConfig.ListenPort),
		Protocols:         node.z.Protocol().SubProtocols,

		// Per-backend bootstrap material — the switcher uses whichever
		// matches the active backend at any given moment.
		LegacyBootstrapNodes: legacyBootstrap,
		NodeDatabase:         netConfig.NodeDatabase,
		Libp2pBootstrapPeers: libp2pBootstrap,
		NATPortMap:           netConfig.NATPortMap,
		PeerstoreDir:         netConfig.PeerstoreDir,

		// Activation gate. The switcher polls this on a 1s ticker; when
		// it returns true (the libp2p spork's EnforcementHeight has
		// passed on this node's local chain) the swap fires.
		Oracle: &sporkOracle{chain: node.z.Chain()},
	}
	return node, nil
}

// sporkOracle is the node-side adapter implementing switcher.SporkOracle
// over chain.Chain. Kept private to the node package since it's purely
// wiring — the switcher takes the narrow interface so it doesn't pull
// in the chain dependency.
type sporkOracle struct {
	chain chain.Chain
}

// IsLibp2pActive reports whether the libp2p activation spork has reached
// its EnforcementHeight on this node's local chain. Cheap: a single
// IsSporkActive lookup against the cached frontier momentum store.
func (s *sporkOracle) IsLibp2pActive() bool {
	active, _ := s.chain.GetFrontierMomentumStore().IsSporkActive(types.Libp2pSpork)
	return active
}

func (node *Node) Start() error {
	node.lock.Lock()
	defer node.lock.Unlock()

	if err := node.startZenon(); err != nil {
		return err
	}
	if err := node.server.Start(); err != nil {
		return err
	}
	node.rpcAPIs = api.GetPublicApis(node.z, node.server)
	if err := node.startRPC(); err != nil {
		log.Error("failed to start rpc", "reason", err)
		return err
	}
	node.warnRPCExposure()

	return nil
}
func (node *Node) Stop() error {
	node.lock.Lock()
	defer node.lock.Unlock()
	defer close(node.stop)

	log.Info("stopping p2p server ...")
	node.server.Stop()

	if err := node.stopWallet(); err != nil {
		log.Error("failed to stop wallet", "reason", err)
		return err
	}
	if err := node.stopZenon(); err != nil {
		log.Error("failed to stop zenon", "reason", err)
		return err
	}
	node.stopRPC()

	// Release instance directory lock.
	node.closeDataDir()

	return nil
}
func (node *Node) Wait() {
	<-node.stop
}

func (node *Node) Zenon() zenon.Zenon {
	return node.z
}
func (node *Node) Config() *Config {
	return node.config
}
func (node *Node) WalletManager() *wallet.Manager {
	return node.walletManager
}

func (node *Node) startWallet() error {
	if err := node.walletManager.Start(); err != nil {
		return err
	}
	return nil
}
func (node *Node) startZenon() error {
	if err := node.z.Init(); err != nil {
		log.Error("failed to init zenon", "reason", err)
		return err
	}
	if err := node.z.Start(); err != nil {
		log.Error("failed to start zenon", "reason", err)
		return err
	}
	return nil
}

func (node *Node) stopWallet() error {
	if node.walletManager == nil {
		return ErrNodeStopped
	}
	node.walletManager.Stop()
	return nil
}
func (node *Node) stopZenon() error {
	if node.z == nil {
		return ErrNodeStopped
	}
	return node.z.Stop()
}

func (node *Node) openDataDir() error {
	if node.config.DataPath == "" {
		return nil
	}

	if err := os.MkdirAll(node.config.DataPath, 0700); err != nil {
		return err
	}
	log.Info("successfully ensured DataPath exists", "data-path", node.config.DataPath)

	// Lock the instance directory to prevent concurrent use by another instance as well as
	// accidental use of the instance directory as a database.
	if fileLock, _, err := fileutil.Flock(filepath.Join(node.config.DataPath, ".lock")); err != nil {
		log.Info("unable to acquire file-lock", "reason", err)
		return convertFileLockError(err)
	} else {
		node.dataDirLock = fileLock
	}

	log.Info("successfully locked dataDir")
	return nil
}
func (node *Node) closeDataDir() {
	log.Info("releasing dataDir lock ... ")
	// Release instance directory lock.
	if node.dataDirLock != nil {
		if err := node.dataDirLock.Release(); err != nil {
			log.Error("can't release dataDir lock", "reason", err)
		}
		node.dataDirLock = nil
	}
}
