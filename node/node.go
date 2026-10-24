// Package node is the top-level container that runs a Zenon node: it
// locks the data directory, starts the wallet, builds the Zenon core
// and the p2p server, and exposes the public RPC over HTTP and
// WebSocket.
//
// NewNode prepares the instance — acquiring the data-directory file
// lock, starting the wallet manager, constructing the Zenon core from
// the derived config and assembling the p2p server from the network
// config and the protocol's sub-protocols. Start then brings the
// Zenon core online, starts the p2p server and serves the public RPC
// APIs; Stop reverses that, stopping the p2p server, wallet and Zenon
// core, shutting down RPC and releasing the data-directory lock. Wait
// blocks until the node is stopped.
package node

import (
	stderrors "errors"
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
	stopped     bool
	dataDirLock fileutil.Releaser // prevents concurrent use of instance directory
}

// NewNode prepares a Node from conf without starting it: it locks the
// data directory, starts the wallet manager, builds the Zenon core
// from the derived zenon config and assembles the p2p server from the
// network config and the protocol's sub-protocols. It fails if the
// data directory is already in use by another instance. Call Start to
// bring the node online.
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

	// From this point on, partially-initialised resources must be
	// released when an error aborts the build. Before makeGenesisConfig
	// returned an error (instead of calling os.Exit) this was impossible
	// to reach, but a library caller that receives an error from NewNode
	// expects to be able to retry with a different config.
	success := false
	defer func() {
		if !success {
			node.cleanupOnError()
		}
	}()

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

	listenAddr, err := joinHostPort(netConfig.ListenAddr, netConfig.ListenPort)
	if err != nil {
		return nil, fmt.Errorf("invalid p2p listen address: %w", err)
	}

	node.server = &switcher.Server{
		PrivateKey:        netConfig.PrivateKey(),
		Name:              netConfig.Name,
		MaxPeers:          netConfig.MaxPeers,
		MinConnectedPeers: netConfig.MinConnectedPeers,
		MaxPendingPeers:   netConfig.MaxPendingPeers,
		ListenAddr:        listenAddr,
		Protocols:         node.z.Protocol().SubProtocols,

		// Per-backend bootstrap material — the switcher uses whichever
		// matches the active backend at any given moment.
		LegacyBootstrapNodes: legacyBootstrap,
		NodeDatabase:         netConfig.NodeDatabase,
		Libp2pBootstrapPeers: libp2pBootstrap,
		NATPortMap:           netConfig.NATPortMap,
		PeerstoreDir:         netConfig.PeerstoreDir,

		// Backend override (issue #105). "libp2p" lets a fresh node
		// bootstrap from a post-activation network where no legacy
		// peers remain. Empty string defaults to auto (spork-gated).
		P2PBackend: netConfig.P2PBackend,

		// Activation gate. The switcher polls this on a 1s ticker; when
		// it returns true (the libp2p spork's EnforcementHeight has
		// passed on this node's local chain) the swap fires.
		Oracle: &sporkOracle{chain: node.z.Chain()},
	}
	success = true
	return node, nil
}

// cleanupOnError releases resources acquired partway through NewNode when
// an error aborts the build. It stops the wallet manager, stops the zenon
// core if one was created, and releases the data-directory lock. Each step
// is nil-guarded so it is safe to call from any failure point after
// openDataDir has run.
func (node *Node) cleanupOnError() {
	if node.walletManager != nil {
		node.walletManager.Stop()
	}
	if node.z != nil {
		if err := node.z.Stop(); err != nil {
			log.Error("cleanup: failed to stop zenon", "reason", err)
		}
	}
	node.closeDataDir()
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

// Start brings the node online: it inits and starts the Zenon core,
// starts the p2p server and serves the public RPC APIs over the
// configured HTTP and WebSocket endpoints.
//
// If any step after startZenon fails, Start unwinds what was started —
// the p2p server, RPC endpoints, Zenon core, wallet and data-dir lock —
// so the caller can retry with corrected config or shut down cleanly.
// The unwind calls the individual stop methods directly (not Stop) to
// avoid re-acquiring the lock that Start already holds.
//
// A failure inside startZenon itself (zenon Init or Start error) is not
// unwound here: the partially-initialised zenon core is left as-is and
// the caller should call Stop to release it.
//
// Before this change, chain.Init called os.Exit(2) on an unsupported
// active spork and a DB open failure in zenon.NewZenon panicked via
// common.DealWithErr. Both now return errors through the API (issue
// #196, item 4).
func (node *Node) Start() error {
	node.lock.Lock()
	defer node.lock.Unlock()

	if err := node.startZenon(); err != nil {
		return err
	}

	success := false
	defer func() {
		if !success {
			node.unwindStart()
		}
	}()

	if err := node.server.Start(); err != nil {
		return err
	}

	node.rpcAPIs = api.GetPublicApis(node.z, node.server)
	if err := node.startRPC(); err != nil {
		log.Error("failed to start rpc", "reason", err)
		return err
	}

	node.warnRPCExposure()

	success = true
	return nil
}

// unwindStart releases resources acquired by a failed Start. It is
// called from Start's rollback defer while node.lock is held, so it
// must not call Stop (which acquires the same lock). Each step is
// guarded: a resource that was never started must not be stopped.
func (node *Node) unwindStart() {
	log.Info("unwinding failed start ...")
	if node.server != nil {
		node.server.Stop()
	}
	node.stopRPC()
	if node.z != nil {
		if err := node.stopZenon(); err != nil {
			log.Error("unwind: failed to stop zenon", "reason", err)
		}
	}
	if err := node.stopWallet(); err != nil {
		log.Error("unwind: failed to stop wallet", "reason", err)
	}
	node.closeDataDir()
	node.stopped = true
	close(node.stop)
}

// Stop shuts the node down in reverse of Start: it stops the p2p
// server, the wallet and the Zenon core, shuts down the RPC servers,
// releases the data-directory lock and unblocks Wait. Stop is
// idempotent: a second call returns nil immediately.
//
// Every cleanup step runs even when an earlier one fails; the returned
// error is the join of the wallet and core stop errors (stopRPC and
// closeDataDir log their own), matching zenon.Stop.
func (node *Node) Stop() error {
	node.lock.Lock()
	defer node.lock.Unlock()

	if node.stopped {
		return nil
	}
	node.stopped = true
	defer close(node.stop)

	var errs []error

	log.Info("stopping p2p server ...")
	node.server.Stop()

	if err := node.stopWallet(); err != nil {
		log.Error("failed to stop wallet", "reason", err)
		errs = append(errs, err)
	}
	if err := node.stopZenon(); err != nil {
		log.Error("failed to stop zenon", "reason", err)
		errs = append(errs, err)
	}
	node.stopRPC()

	// Release instance directory lock.
	node.closeDataDir()

	return stderrors.Join(errs...)
}

// Wait blocks until the node is stopped.
func (node *Node) Wait() {
	<-node.stop
}

// Zenon returns the node's Zenon core.
func (node *Node) Zenon() zenon.Zenon {
	return node.z
}

// Config returns the node's configuration.
func (node *Node) Config() *Config {
	return node.config
}

// WalletManager returns the node's wallet manager.
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
