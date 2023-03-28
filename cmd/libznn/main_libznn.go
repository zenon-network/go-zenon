//go:build libznn && !detached

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
