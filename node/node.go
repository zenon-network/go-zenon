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
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/pkg/errors"
	"github.com/prometheus/tsdb/fileutil"

	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/p2p"
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
	server        *p2p.Server

	z zenon.Zenon

	rpcAPIs []rpc.API   // List of APIs currently provided by the node
	http    *httpServer //
	ws      *httpServer //

	// Channel to wait for termination notifications
	stop        chan struct{}
	lock        sync.RWMutex
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
	nodes, err := netConfig.Nodes()
	if err != nil {
		return nil, errors.Errorf("Unable to parse seeders. Reason: %v", err)
	}

	node.server = &p2p.Server{
		PrivateKey:        netConfig.PrivateKey(),
		Name:              netConfig.Name,
		MaxPeers:          netConfig.MaxPeers,
		MinConnectedPeers: netConfig.MinConnectedPeers,
		MaxPendingPeers:   netConfig.MaxPendingPeers,
		Discovery:         true,
		NoDial:            false,
		StaticNodes:       nil,
		BootstrapNodes:    nodes,
		TrustedNodes:      nil,
		NodeDatabase:      netConfig.NodeDatabase,
		ListenAddr:        fmt.Sprintf("%v:%v", netConfig.ListenAddr, netConfig.ListenPort),
		Protocols:         node.z.Protocol().SubProtocols,
	}
	return node, nil
}

// Start brings the node online: it inits and starts the Zenon core,
// starts the p2p server and serves the public RPC APIs over the
// configured HTTP and WebSocket endpoints.
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

	return nil
}

// Stop shuts the node down in reverse of Start: it stops the p2p
// server, the wallet and the Zenon core, shuts down the RPC servers,
// releases the data-directory lock and unblocks Wait.
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
