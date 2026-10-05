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

// Supported versions of the eth protocol (first is primary).
var ProtocolVersions = []uint{61}

// Number of implemented message corresponding to different protocol versions.
var ProtocolLengths = []uint64{9}

const (
	ProtocolMaxMsgSize = 10 * 1024 * 1024 // Maximum cap on the size of a protocol message

	// MaxBlocksRequest is the most hashes the receiver of one GetBlocksMsg
	// looks up. It bounds hash lookups only: every named hash costs one
	// store lookup whether or not the block exists, and the receiver stops
	// looking after this many and answers with what it found, as it already
	// does once the reply holds downloader.MaxBlockFetch blocks. It does not
	// bound the work a found block costs (a momentum read plus one read per
	// account block). The size of the reply is capped separately by
	// softResponseLimit (2 MB), which stops the handler once the encoded
	// reply exceeds that limit.
	//
	// The value is twice the reply cap downloader.MaxBlockFetch, which
	// TestMaxBlocksRequest_CoversEveryHonestRequester pins, so a request
	// that mixes misses and hits can still fill a reply. It is independent
	// of the fetcher's per-peer announce limit, which happens to be the same
	// number. peer.RequestBlocks, the only sender, splits larger batches
	// into requests of downloader.MaxBlockFetch hashes, so a node running
	// this code never names more than the reply cap in one message.
	MaxBlocksRequest = 256

	// softResponseLimit is the target maximum cumulative size of a GetBlocks
	// reply. The handler stops appending blocks once the encoded reply
	// exceeds this limit, which bounds the read and encode work a single
	// request can cause. It is not a hard wire cap: the reply is one message,
	// and both transports reject frames above their own limit (10 MiB in
	// libp2p; 10 MiB plus a frame header on legacy RLPx).
	softResponseLimit = 2 * 1024 * 1024 // 2 MB, matching go-ethereum
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
