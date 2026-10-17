//go:build libznn && !detached

// Package main builds znnd as a C shared library (libznn): RunNode and
// StopNode are exported with cgo and wrap app.Run and app.Stop, so a
// host process can embed a full node.
package main

import (
	"github.com/zenon-network/go-zenon/app"
)

import "C"

//export RunNode
func RunNode() {
	app.Run()
}

//export StopNode
func StopNode() {
	app.Stop()
}

func main() {}
