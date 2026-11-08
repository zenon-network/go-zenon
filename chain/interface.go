package chain

import (
	"sync"

	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/chain/store"
	"github.com/zenon-network/go-zenon/common/db"
	"github.com/zenon-network/go-zenon/common/types"
)

// Chain is the full chain module: the genesis view, both ledger
// pools, the chain cache and the momentum event manager behind one
// interface, plus the Init/Start/Stop lifecycle. It is implemented by NewChain and
// consumed by virtually every other module (consensus, verifier,
// supervisor, pillar, protocol, rpc).
type Chain interface {
	Init() error
	Start() error
	Stop() error

	// AcquireInsert is used to limit insert operations in a global way inside the chain module.
	// The actual sync.Locker object returned is used for logging purposes and any method receiving such argument
	// does not enforce in any way the validity, only the fact that is non-nil.
	AcquireInsert(reason string) sync.Locker

	store.Genesis
	AccountPool
	MomentumPool
	MomentumEventManager
	ChainCache
}

// MomentumEventListener receives ledger-change notifications:
// InsertMomentum after a momentum is committed and DeleteMomentum for
// every momentum popped during a rollback (most recent first).
// Callbacks run synchronously inside the insertion or rollback, on
// the inserter's goroutine, while the chain insert lock is held and
// the momentum pool's own lock is released for the duration of the
// callback.
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
// existing momentum), broadcasting DeleteMomentum per pop;
// CaptureBranchAbove reads a branch back out so it can be re-inserted
// after a rollback. All three require the chain insert lock to be
// held and only check that the locker is non-nil.
//
// GetFrontierMomentumStore returns a store over the latest committed
// momentum version and GetMomentumStore over an arbitrary committed
// version (nil if unknown); both wrap copy-on-write snapshots, so
// they remain consistent while the chain advances.
type MomentumPool interface {
	// CaptureBranchAbove returns every momentum above identifier, lowest first,
	// together with its stored state patch, so the branch can be re-inserted
	// after a rollback. It fails, without side effects, if any patch is missing.
	CaptureBranchAbove(insertLocker sync.Locker, identifier types.HashHeight) ([]*RemovedMomentum, error)
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
// produced. GetNewMomentumContent is the content selection used
// before the dynamic-plasma spork (pillar.ContentSelector replaces it
// after): it returns the uncommitted blocks a new momentum should
// confirm — at most MaxAccountBlocksInMomentum and within the detailed
// momentum's wire-size budget, keeping contract-send batches whole and
// skipping a batch that does not fit along with the rest of its
// account's chain. SnapshotUncommitted and
// RestoreUncommitted save and put back the pending state of given
// addresses across a rollback.
type AccountPool interface {
	// AddAccountBlockTransaction implements the whole logic required to manage an account-chain.
	// When inserting a new account-block-transaction, is possible to trigger rollbacks of other unconfirmed account-blocks.
	//
	// Note: A confirmed account-block will never be rollback.
	//
	// In case a fork is detected, there is a deterministic way to find the longest chain, as follows:
	//  - the account-block with the biggest TotalPlasma/BasePlasma is selected
	//  - the account-block with the smallest hash
	AddAccountBlockTransaction(insertLocker sync.Locker, transaction *nom.AccountBlockTransaction) error
	ForceAddAccountBlockTransaction(insertLocker sync.Locker, transaction *nom.AccountBlockTransaction) error

	GetPatch(address types.Address, identifier types.HashHeight) db.Patch
	GetAccountStore(address types.Address, identifier types.HashHeight) store.Account
	GetFrontierAccountStore(address types.Address) store.Account

	GetNewMomentumContent() []*nom.AccountBlock
	GetAllUncommittedAccountBlocks() []*nom.AccountBlock
	GetUncommittedAccountBlocksByAddress(address types.Address) []*nom.AccountBlock

	// SnapshotUncommitted and RestoreUncommitted bracket a sequence of pool
	// mutations so they can be undone if a later validation step fails.
	SnapshotUncommitted(insertLocker sync.Locker, addresses []types.Address) *UncommittedSnapshot
	RestoreUncommitted(insertLocker sync.Locker, snapshot *UncommittedSnapshot) error
}

// ChainCache is the derived-state cache the chain keeps beside the
// ledger (chain/cache): fused plasma per beneficiary, chain plasma per
// address and spork activation, indexed by momentum height. UpdateCache
// and RollbackCacheTo follow momentum insertion and rollback and require
// the chain insert lock; GetCacheStore returns a read view at a committed
// identifier (nil if above the cache frontier or the manager is stopped)
// and GetFrontierCacheStore one at the cache frontier (nil if stopped).
type ChainCache interface {
	UpdateCache(insertLocker sync.Locker, detailed *nom.DetailedMomentum, changes db.Patch) error
	RollbackCacheTo(insertLocker sync.Locker, identifier types.HashHeight) error
	GetCacheStore(identifier types.HashHeight) store.Cache
	GetFrontierCacheStore() store.Cache
}
