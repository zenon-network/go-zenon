package chain

import (
	"sync"

	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/chain/store"
	"github.com/zenon-network/go-zenon/common/db"
	"github.com/zenon-network/go-zenon/common/types"
)

// Chain is the full chain module: the genesis view, both ledger
// pools and the momentum event manager behind one interface, plus the
// Init/Start/Stop lifecycle. It is implemented by NewChain and
// consumed by virtually every other module (consensus, verifier,
// supervisor, pillar, protocol, rpc).
type Chain interface {
	Init() error
	Start() error
	Stop() error

	// AcquireInsert blocks until the chain-wide insert lock is free
	// and returns a single-use token that is already held: calling
	// Lock on it panics, and Unlock releases the chain lock exactly
	// once. The lock is not reentrant — acquiring it twice from the
	// same goroutine deadlocks. Methods that take the token as an
	// insertLocker argument check only that it is non-nil; reason is
	// used for logging.
	AcquireInsert(reason string) sync.Locker

	store.Genesis
	AccountPool
	MomentumPool
	MomentumEventManager
}

// MomentumEventListener receives ledger-change notifications:
// InsertMomentum after a momentum is committed and DeleteMomentum for
// every momentum popped during a rollback (most recent first).
// Callbacks run synchronously inside the insertion or rollback, on
// the inserter's goroutine, while the chain insert lock is held.
// Listeners are the account pool, the consensus points and election
// caches, the event printer and the RPC subscription server.
type MomentumEventListener interface {
	InsertMomentum(*nom.DetailedMomentum)
	DeleteMomentum(*nom.DetailedMomentum)
}

// MomentumEventManager registers and unregisters momentum event
// listeners; the chain embeds it and broadcasts to all registered
// listeners in registration order.
type MomentumEventManager interface {
	Register(MomentumEventListener)
	UnRegister(MomentumEventListener)
}

// MomentumPool manages the persistent momentum chain.
// AddMomentumTransaction commits a verified momentum on top of the
// frontier and broadcasts the InsertMomentum event; RollbackTo pops
// momentums down to the given identifier (which must match an
// existing momentum), broadcasting DeleteMomentum per pop. Both
// require the chain insert lock to be held and only check that the
// locker is non-nil.
//
// GetFrontierMomentumStore returns a store over the latest committed
// momentum version and GetMomentumStore over an arbitrary committed
// version (nil if unknown); both wrap copy-on-write snapshots, so
// they remain consistent while the chain advances.
type MomentumPool interface {
	AddMomentumTransaction(insertLocker sync.Locker, transaction *nom.MomentumTransaction) error
	RollbackTo(insertLocker sync.Locker, identifier types.HashHeight) error

	GetFrontierMomentumStore() store.Momentum
	GetMomentumStore(identifier types.HashHeight) store.Momentum
}

// AccountPool manages the unconfirmed tips of all account chains,
// layered in memory over the momentum-confirmed state. Methods that
// take an insertLocker require the chain insert lock to be held and
// only check that the locker is non-nil.
//
// GetFrontierAccountStore returns the account's state including
// unconfirmed blocks; GetAccountStore returns the state at an exact
// identifier — the stable (momentum-confirmed) version or any pooled
// one, nil otherwise — and GetPatch the state patch a pooled block
// produced. GetNewMomentumContent returns the uncommitted blocks a
// new momentum should confirm — at most MaxAccountBlocksInMomentum,
// keeping contract-send batches whole.
type AccountPool interface {
	// AddAccountBlockTransaction inserts an account-block transaction
	// into the pool, which may roll back conflicting unconfirmed
	// blocks of the same account chain; momentum-confirmed blocks are
	// never rolled back. Forks resolve by the fork-priority rule: the
	// block with the higher TotalPlasma/BasePlasma ratio wins, with
	// ties broken by the smaller hash.
	AddAccountBlockTransaction(insertLocker sync.Locker, transaction *nom.AccountBlockTransaction) error
	// ForceAddAccountBlockTransaction inserts like
	// AddAccountBlockTransaction but skips the fork-priority
	// comparison, so the new block always replaces a conflicting
	// unconfirmed one; the chain bridge uses it for blocks already
	// confirmed by momentums being synced.
	ForceAddAccountBlockTransaction(insertLocker sync.Locker, transaction *nom.AccountBlockTransaction) error

	GetPatch(address types.Address, identifier types.HashHeight) db.Patch
	GetAccountStore(address types.Address, identifier types.HashHeight) store.Account
	GetFrontierAccountStore(address types.Address) store.Account

	GetNewMomentumContent() []*nom.AccountBlock
	GetAllUncommittedAccountBlocks() []*nom.AccountBlock
	GetUncommittedAccountBlocksByAddress(address types.Address) []*nom.AccountBlock
}
