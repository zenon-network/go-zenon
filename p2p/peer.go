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

import (
	"net"

	"github.com/zenon-network/go-zenon/p2p/discover"
)

// Peer is the application-level view of a remote node. The legacy
// (devp2p/RLPX) and libp2p backends each provide their own concrete
// implementation; the subprotocol code in protocol/ only sees this
// interface, so it works unchanged against either backend.
type Peer interface {
	// ID returns the peer's Zenon node identity. The same secp256k1 key
	// is used regardless of which transport is active.
	ID() discover.NodeID

	// Name returns the peer-advertised node name (from the protocol
	// handshake).
	Name() string

	// Caps returns the capabilities (subprotocol name + version) the
	// peer advertised during the handshake.
	Caps() []Cap

	// RemoteAddr is the remote endpoint of the connection. The concrete
	// net.Addr type is backend-specific (TCP/UDP for legacy, a
	// multiaddr-backed wrapper for libp2p).
	RemoteAddr() net.Addr

	// Disconnect requests termination of the connection with the given
	// reason. Returns immediately; the backend handles the teardown
	// asynchronously.
	Disconnect(reason DiscReason)
}
