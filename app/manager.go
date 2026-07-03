//go:build !libznn

package app

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/urfave/cli/v2"

	"github.com/zenon-network/go-zenon/node"
)

// Manager owns the node lifecycle for the cli. It holds the cli context and
// the constructed node.Node, and drives the node's start, run-until-signal
// and stop sequence.
type Manager struct {
	ctx  *cli.Context
	node *node.Node
}

// NewNodeManager builds the node configuration from ctx and
// constructs the node, returning a Manager ready to Start. It returns
// an error if the config cannot be built or the node cannot be created.
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

// Start starts the node and blocks until it stops. It reports the
// configured producer (pillar) address, installs a SIGINT/SIGTERM
// handler that triggers a graceful Stop, and waits for the node to
// finish. If the node fails to start it logs and exits the process.
func (nodeManager *Manager) Start() error {
	// Start up the node
	log.Info("starting znnd")
	if err := nodeManager.node.Start(); err != nil {
		fmt.Printf("failed to start node; reason:%v\n", err)
		log.Crit("failed to start node", "reason", err)
		os.Exit(1)
	} else {
		fmt.Println("znnd successfully started")
		fmt.Println("*** Node status ***")
		address := nodeManager.node.Zenon().Producer().GetCoinBase()
		if address == nil {
			fmt.Println("* No Pillar configured for current node")
		} else {
			fmt.Printf("* Producer address detected: %v\n", address)
		}
	}

	// Listening event closes the node
	go func() {
		c := make(chan os.Signal, 1)
		signal.Notify(c, syscall.SIGINT, syscall.SIGTERM, syscall.SIGKILL)
		defer signal.Stop(c)
		<-c
		fmt.Println("Shutting down znnd")

		go func() {
			nodeManager.Stop()
		}()

		for i := 10; i > 0; i-- {
			<-c
			if i > 1 {
				log.Warn("Please DO NOT interrupt the shutdown process, panic may occur.", "times", i-1)
			}
		}
	}()

	// Waiting for node to close
	nodeManager.node.Wait()

	return nil
}

// Stop shuts the node down gracefully, logging any failure. It always
// returns nil so that callers can treat shutdown as best-effort.
func (nodeManager *Manager) Stop() error {
	log.Warn("Stopping znnd ...")

	if err := nodeManager.node.Stop(); err != nil {
		log.Error("Failed to stop node", "reason", err)
	}
	return nil
}
