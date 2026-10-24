//go:build libznn

package app

import (
	"fmt"

	"github.com/urfave/cli/v2"

	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/node"
)

// Manager owns the node for the lifetime of the process: it builds the
// node from the cli context and starts and stops it. Run creates one
// and Stop (exported for libznn) stops it.
type Manager struct {
	ctx  *cli.Context
	node *node.Node
}

// NewNodeManager builds the node configuration from the cli context
// (MakeConfig) and constructs the node, which locks the data directory
// and opens the wallet but starts nothing yet.
func NewNodeManager(ctx *cli.Context) (*Manager, error) {
	// make config
	nodeConfig, err := MakeConfig(ctx)
	if err != nil {
		return nil, err
	}

	// make node
	newNode, err := node.NewNode(nodeConfig)
	if err != nil {
		log.Error("failed to create the node", "reason", err)
		return nil, err
	}

	return &Manager{
		ctx:  ctx,
		node: newNode,
	}, nil
}

// NewNodeManagerFromConfig builds a Manager directly from an explicit
// node.Config, bypassing the CLI flag-parsing path entirely. This is
// the entry point for library consumers (libznn) that need to pass
// configuration programmatically rather than via os.Args. It performs
// the same config initialisation as MakeConfig (absolute paths, logging)
// so that a bare DefaultNodeConfig with an empty WalletPath works.
func NewNodeManagerFromConfig(nodeConfig *node.Config) (*Manager, error) {
	if err := nodeConfig.MakePathsAbsolute(); err != nil {
		return nil, err
	}
	common.InitLogging(nodeConfig.DataPath, nodeConfig.LogLevel)

	newNode, err := node.NewNode(nodeConfig)
	if err != nil {
		log.Error("failed to create the node", "reason", err)
		return nil, err
	}

	return &Manager{
		node: newNode,
	}, nil
}

// Start brings the node online and prints a short status to stdout: the
// producer address if a pillar is configured and any RPC exposure
// warnings. A start failure is returned as an error; the process is not
// exited. In the libznn build Start does not install a signal handler
// or block — the library caller drives the lifecycle via Stop.
func (nodeManager *Manager) Start() error {
	return nodeManager.startNode()
}

// startNode does the actual start work — node.Start, print status, RPC
// warnings — and returns. It never blocks.
func (nodeManager *Manager) startNode() error {
	// Start up the node
	log.Info("starting znnd")
	if err := nodeManager.node.Start(); err != nil {
		fmt.Printf("failed to start node; reason:%v\n", err)
		log.Error("failed to start node", "reason", err)
		return fmt.Errorf("failed to start node: %w", err)
	}

	fmt.Println("znnd successfully started")
	fmt.Println("*** Node status ***")
	address := nodeManager.node.Zenon().Producer().GetCoinBase()
	if address == nil {
		fmt.Println("* No Pillar configured for current node")
	} else {
		fmt.Printf("* Producer address detected: %v\n", address)
	}
	for _, warning := range nodeManager.node.RPCExposureWarnings() {
		fmt.Printf("* WARNING: %s\n", warning)
	}

	return nil
}

// Stop shuts the node down. A failure to stop is logged, not returned;
// the method always returns nil. In the libznn build Stop also waits for
// the node to finish shutting down. A Manager whose node was never
// constructed (nil node) is a no-op.
func (nodeManager *Manager) Stop() error {
	log.Warn("Stopping znnd ...")

	if nodeManager.node == nil {
		return nil
	}
	if err := nodeManager.node.Stop(); err != nil {
		log.Error("Failed to stop node", "reason", err)
	}

	// Waiting for node to close
	nodeManager.node.Wait()

	return nil
}
