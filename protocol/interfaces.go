package protocol

import (
	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/types"
)

type SyncState int

var (
	log = common.ProtocolLogger
)

const (
	Unknown SyncState = iota
	Syncing
	SyncDone
	NotEnoughPeers
)

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
	// InsertChainMoved is InsertChain that also reports whether the
	// canonical frontier is different after the call, read under the
	// insert lock the call holds: false for a batch the node already had
	// and for a replacement that was rejected and left the frontier where it
	// was, true when the frontier changed, with or without a later error.
	InsertChainMoved(chain []*nom.DetailedMomentum) (int, bool, error)
}

type ChainBridge interface {
	txPool
	chainManager
}

type Broadcaster interface {
	SyncInfo() *SyncInfo
	// ReadyToProduce is the producer's gate: false while too few peers are
	// connected or while a download session towards a higher advertised
	// height is running, draining, or imported recently. See
	// ProtocolManager.ReadyToProduce.
	ReadyToProduce() bool
	CreateMomentum(*nom.MomentumTransaction)
	CreateAccountBlock(*nom.AccountBlockTransaction)
}
