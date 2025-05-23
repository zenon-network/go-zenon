package chain

import (
	"sync"

	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/chain/store"
	"github.com/zenon-network/go-zenon/common/db"
	"github.com/zenon-network/go-zenon/common/types"
)

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

type MomentumEventListener interface {
	InsertMomentum(*nom.DetailedMomentum)
	DeleteMomentum(*nom.DetailedMomentum)
}

type MomentumEventManager interface {
	Register(MomentumEventListener)
	UnRegister(MomentumEventListener)
}

type MomentumPool interface {
	AddMomentumTransaction(insertLocker sync.Locker, transaction *nom.MomentumTransaction) error
	RollbackTo(insertLocker sync.Locker, identifier types.HashHeight) error

	GetFrontierMomentumStore() store.Momentum
	GetMomentumStore(identifier types.HashHeight) store.Momentum
}

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
}

type ChainCache interface {
	UpdateCache(insertLocker sync.Locker, detailed *nom.DetailedMomentum, changes db.Patch) error
	RollbackCacheTo(insertLocker sync.Locker, identifier types.HashHeight) error
	GetCacheStore(identifier types.HashHeight) store.Cache
	GetFrontierCacheStore() store.Cache
}
