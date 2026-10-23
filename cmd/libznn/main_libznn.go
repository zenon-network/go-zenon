//go:build libznn && !detached

// Package main builds znnd as a C shared library (libznn): RunNode and
// StopNode are exported with cgo and wrap app.Run and app.Stop, so a
// host process can embed a full node.
package main

import (
	"encoding/json"

	"github.com/zenon-network/go-zenon/app"
	"github.com/zenon-network/go-zenon/node"
)

import "C"

//export RunNode
func RunNode() {
	app.Run()
}

// RunNodeWithConfig starts a node from a JSON-serialized node.Config
// passed as a C string. It returns 0 on success and 1 on error, without
// ever exiting the host process. The config JSON must be non-NULL and
// non-empty; an empty or malformed config is an error.
//
//export RunNodeWithConfig
func RunNodeWithConfig(configJSON *C.char) C.int {
	if configJSON == nil {
		return 1
	}
	raw := C.GoString(configJSON)
	if raw == "" {
		return 1
	}

	// Clone so the JSON overlay writes into fresh slice backing arrays
	// and can never mutate the package-level DefaultNodeConfig.
	cfg := node.DefaultNodeConfig.Clone()
	if err := json.Unmarshal([]byte(raw), cfg); err != nil {
		return 1
	}

	if err := app.RunWithConfig(cfg); err != nil {
		return 1
	}
	return 0
}

//export StopNode
func StopNode() {
	app.Stop()
}

func main() {}
