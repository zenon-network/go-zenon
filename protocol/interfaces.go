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
)

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

type ChainBridge interface {
	txPool
	chainManager
}

type Broadcaster interface {
	SyncState() SyncState
	CreateMomentum(*nom.MomentumTransaction)
	CreateAccountBlock(*nom.AccountBlockTransaction)
}
