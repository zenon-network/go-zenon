// Copyright 2015 The go-ethereum Authors
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

package server

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
)

// API describes the set of methods offered over the RPC interface
type API struct {
	Namespace string      // namespace under which the rpc methods of Service are exposed
	Version   string      // api version for DApp's
	Service   interface{} // receiver instance which holds the methods
	Public    bool        // indication if the methods must be considered safe for public use
}

// ServerCodec implements reading, parsing and writing RPC messages for the server side of
// a RPC session. Implementations must be go-routine safe since the codec can be called in
// multiple go-routines concurrently.
type ServerCodec interface {
	readBatch() (msgs []*jsonrpcMessage, isBatch bool, err error)
	close()
	jsonWriter
}

// jsonWriter can write JSON messages to its underlying connection.
// Implementations must be safe for concurrent use.
type jsonWriter interface {
	writeJSON(context.Context, interface{}) error
	// Closed returns a channel which is closed when the connection is closed.
	closed() <-chan interface{}
	// RemoteAddr returns the peer address of the connection.
	remoteAddr() string
}

// BlockNumber is a height argument accepted by RPC methods. Non-
// negative values denote a concrete height; negative values denote
// the named positions below.
type BlockNumber int64

const (
	// PendingBlockNumber is the wire value "pending".
	PendingBlockNumber = BlockNumber(-2)
	// LatestBlockNumber is the wire value "latest".
	LatestBlockNumber = BlockNumber(-1)
	// EarliestBlockNumber is the wire value "earliest", height 0.
	EarliestBlockNumber = BlockNumber(0)
)

// UnmarshalJSON parses the given JSON fragment into a BlockNumber. It supports:
// - "latest", "earliest" or "pending" as string arguments
// - the block number
// Returned errors:
// - an invalid block number error when the given argument isn't a known strings
// - an out of range error when the given block number is either too little or too large
func (bn *BlockNumber) UnmarshalJSON(data []byte) error {
	input := strings.TrimSpace(string(data))
	if len(input) >= 2 && input[0] == '"' && input[len(input)-1] == '"' {
		input = input[1 : len(input)-1]
	}

	switch input {
	case "earliest":
		*bn = EarliestBlockNumber
		return nil
	case "latest":
		*bn = LatestBlockNumber
		return nil
	case "pending":
		*bn = PendingBlockNumber
		return nil
	}

	blckNum, err := hexutil.DecodeUint64(input)
	if err != nil {
		return err
	}
	if blckNum > math.MaxInt64 {
		return fmt.Errorf("block number larger than int64")
	}
	*bn = BlockNumber(blckNum)
	return nil
}

// Int64 returns the block number as a plain int64.
func (bn BlockNumber) Int64() int64 {
	return (int64)(bn)
}

// BlockNumberOrHash identifies a block by height or by hash. At most
// one of BlockNumber and BlockHash is set. RequireCanonical asks that
// a block referenced by hash also lie on the canonical chain.
type BlockNumberOrHash struct {
	BlockNumber      *BlockNumber `json:"blockNumber,omitempty"`
	BlockHash        *common.Hash `json:"blockHash,omitempty"`
	RequireCanonical bool         `json:"requireCanonical,omitempty"`
}

// UnmarshalJSON parses either the object form of BlockNumberOrHash or
// a bare string. A string is read as one of "earliest", "latest" or
// "pending", as a 66-character hex block hash, or as a hex height. It
// is an error to specify both a block number and a block hash.
func (bnh *BlockNumberOrHash) UnmarshalJSON(data []byte) error {
	type erased BlockNumberOrHash
	e := erased{}
	err := json.Unmarshal(data, &e)
	if err == nil {
		if e.BlockNumber != nil && e.BlockHash != nil {
			return fmt.Errorf("cannot specify both BlockHash and BlockNumber, choose one or the other")
		}
		bnh.BlockNumber = e.BlockNumber
		bnh.BlockHash = e.BlockHash
		bnh.RequireCanonical = e.RequireCanonical
		return nil
	}
	var input string
	err = json.Unmarshal(data, &input)
	if err != nil {
		return err
	}
	switch input {
	case "earliest":
		bn := EarliestBlockNumber
		bnh.BlockNumber = &bn
		return nil
	case "latest":
		bn := LatestBlockNumber
		bnh.BlockNumber = &bn
		return nil
	case "pending":
		bn := PendingBlockNumber
		bnh.BlockNumber = &bn
		return nil
	default:
		if len(input) == 66 {
			hash := common.Hash{}
			err := hash.UnmarshalText([]byte(input))
			if err != nil {
				return err
			}
			bnh.BlockHash = &hash
			return nil
		} else {
			blckNum, err := hexutil.DecodeUint64(input)
			if err != nil {
				return err
			}
			if blckNum > math.MaxInt64 {
				return fmt.Errorf("blocknumber too high")
			}
			bn := BlockNumber(blckNum)
			bnh.BlockNumber = &bn
			return nil
		}
	}
}

// Number returns the block number and true when this value identifies
// a block by height, or zero and false otherwise.
func (bnh *BlockNumberOrHash) Number() (BlockNumber, bool) {
	if bnh.BlockNumber != nil {
		return *bnh.BlockNumber, true
	}
	return BlockNumber(0), false
}

// Hash returns the block hash and true when this value identifies a
// block by hash, or the zero hash and false otherwise.
func (bnh *BlockNumberOrHash) Hash() (common.Hash, bool) {
	if bnh.BlockHash != nil {
		return *bnh.BlockHash, true
	}
	return common.Hash{}, false
}

// BlockNumberOrHashWithNumber builds a BlockNumberOrHash that
// identifies a block by the given height.
func BlockNumberOrHashWithNumber(blockNr BlockNumber) BlockNumberOrHash {
	return BlockNumberOrHash{
		BlockNumber:      &blockNr,
		BlockHash:        nil,
		RequireCanonical: false,
	}
}

// BlockNumberOrHashWithHash builds a BlockNumberOrHash that
// identifies a block by hash. When canonical is true the referenced
// block is also required to be on the canonical chain.
func BlockNumberOrHashWithHash(hash common.Hash, canonical bool) BlockNumberOrHash {
	return BlockNumberOrHash{
		BlockNumber:      nil,
		BlockHash:        &hash,
		RequireCanonical: canonical,
	}
}
