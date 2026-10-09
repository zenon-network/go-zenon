// Copyright 2014 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

package p2p

import "github.com/zenon-network/go-zenon/p2p/discover"

// Server is the application-level view of the p2p layer. Both the legacy
// (devp2p/RLPX) and libp2p backends implement this interface; the rest of
// the codebase (node, rpc) holds a Server interface and does not care
// which transport is active.
//
// In production, node.go constructs a switcher.Server (p2p/switcher/)
// which owns both backends and swaps between them when the libp2p
// activation spork's EnforcementHeight is reached on the local chain.
type Server interface {
	// Start launches the server. Returns when the listener is ready or
	// with an error describing why startup failed.
	Start() error

	// Stop terminates the server and all active peer connections. Blocks
	// until all goroutines have exited.
	Stop()

	// Peers returns a snapshot of currently connected peers.
	Peers() []Peer

	// PeerCount returns the number of currently connected peers.
	PeerCount() int

	// AddPeer attempts to dial the given node and keep it connected.
	AddPeer(node *discover.Node)

	// Self returns the local node's endpoint information.
	Self() *discover.Node
}
