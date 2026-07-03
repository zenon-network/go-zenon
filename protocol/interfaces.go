package protocol

import (
	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/types"
)

// SyncState describes how far momentum synchronisation with the
// network has progressed. It is exposed over RPC (stats.syncInfo), so
// the numeric values are part of the API.
type SyncState int

var (
	log = common.ProtocolLogger
)

// The sync states, in the order reported over RPC: 1 while the local
// frontier is behind the best peer's advertised height, 2 once it has
// caught up, and 3 — overriding both — when fewer peers are connected
// than the configured minimum. Unknown (0) is the zero value and is
// never reported.
const (
	Unknown SyncState = iota
	Syncing
	SyncDone
	NotEnoughPeers
)

// SyncInfo is a snapshot of momentum synchronisation progress.
// CurrentHeight is the local frontier momentum height and
// TargetHeight is the frontier height advertised by the best-known
// peer, or 0 when no peers are connected.
type SyncInfo struct {
	State         SyncState `json:"state"`
	CurrentHeight uint64    `json:"currentHeight"`
	TargetHeight  uint64    `json:"targetHeight"`
}

type txPool interface {
	AddAccountBlocks([]*nom.AccountBlock) error

	// GetTransactions should return pending transactions.
	// The slice should be modifiable by the caller.
	GetTransactions() []*nom.AccountBlock
}

type chainManager interface {
	HasBlock(hash types.Hash) bool
	GetBlockHashesFromHash(hash types.Hash, amount uint64) ([]types.Hash, error)
	GetBlock(hash types.Hash) (block *nom.DetailedMomentum)
	GetBlockByNumber(num uint64) (*nom.Momentum, error)
	CurrentBlock() *nom.Momentum
	Status() (td uint64, currentBlock types.Hash, genesisBlock types.Hash)

	InsertChain(chain []*nom.DetailedMomentum) (int, error)
}

// ChainBridge adapts the chain for the protocol layer. The
// chainManager side serves and inserts momentums — in eth/61 terms a
// momentum is a block and Status reports the frontier momentum height
// as the total difficulty — while the txPool side exchanges
// uncommitted account blocks, the protocol's transactions.
type ChainBridge interface {
	txPool
	chainManager
}

// Broadcaster is how the rest of the node hands locally produced
// blocks to the network: blocks are inserted into the chain first and
// relayed to peers only if the insertion succeeds. It also reports
// sync progress, since producers must not act on a stale frontier.
type Broadcaster interface {
	SyncInfo() *SyncInfo
	CreateMomentum(*nom.MomentumTransaction)
	CreateAccountBlock(*nom.AccountBlockTransaction)
}
