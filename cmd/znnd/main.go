//go:build !libznn

package main

import (
	"github.com/zenon-network/go-zenon/app"
)

// znnd is the official command-line client
func main() {
	app.Run()
}
