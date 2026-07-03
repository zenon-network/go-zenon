//go:build !libznn

// Command znnd is the official Zenon Network of Momentum node client.
// It delegates to package app, which parses the command line and runs
// the node.
package main

import (
	"github.com/zenon-network/go-zenon/app"
)

// znnd is the official command-line client
func main() {
	app.Run()
}
