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

package protocol

import (
	"github.com/zenon-network/go-zenon/common/types"
)

// ProtocolVersions lists the supported versions of the eth protocol
// (first is primary).
var ProtocolVersions = []uint{61}

// ProtocolLengths holds the number of implemented message codes for
// each entry of ProtocolVersions.
var ProtocolLengths = []uint64{9}

// ProtocolMaxMsgSize is the maximum size accepted for a single
// protocol message; larger messages disconnect the sending peer.
const (
	ProtocolMaxMsgSize = 10 * 1024 * 1024 // Maximum cap on the size of a protocol message
)

// eth protocol message codes
const (
	StatusMsg = iota
	NewBlockHashesMsg
	TxMsg
	GetBlockHashesMsg
	BlockHashesMsg
	GetBlocksMsg
	BlocksMsg
	NewBlockMsg
	GetBlockHashesFromNumberMsg
)

type errCode int

// Protocol error codes, reported when a peer is disconnected for
// misbehaving: oversized or undecodable messages, handshake
// mismatches (protocol version, network id or genesis hash), a
// missing, repeated or out-of-order status message, or activity from
// a peer that has been suspended.
const (
	ErrMsgTooLarge = iota
	ErrDecode
	ErrInvalidMsgCode
	ErrProtocolVersionMismatch
	ErrNetworkIdMismatch
	ErrGenesisBlockMismatch
	ErrNoStatusMsg
	ErrExtraStatusMsg
	ErrSuspendedPeer
)

func (e errCode) String() string {
	return errorToString[int(e)]
}

// XXX change once legacy code is out
var errorToString = map[int]string{
	ErrMsgTooLarge:             "Message too long",
	ErrDecode:                  "Invalid message",
	ErrInvalidMsgCode:          "Invalid message code",
	ErrProtocolVersionMismatch: "Protocol version mismatch",
	ErrNetworkIdMismatch:       "NetworkId mismatch",
	ErrGenesisBlockMismatch:    "Genesis block mismatch",
	ErrNoStatusMsg:             "No status message",
	ErrExtraStatusMsg:          "Extra status message",
	ErrSuspendedPeer:           "Suspended peer",
}

// statusData is the network packet for the status message.
type statusData struct {
	ProtocolVersion uint32
	NetworkId       uint32
	TD              uint64
	CurrentBlock    types.Hash
	GenesisBlock    types.Hash
}

// getBlockHashesData is the network packet for the hash based block retrieval
// message.
type getBlockHashesData struct {
	Hash   types.Hash
	Amount uint64
}

// getBlockHashesFromNumberData is the network packet for the number based block
// retrieval message.
type getBlockHashesFromNumberData struct {
	Number uint64
	Amount uint64
}
