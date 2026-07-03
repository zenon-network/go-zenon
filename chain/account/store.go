// Package account implements store.Account: the state of a single
// account chain backed by one db.DB version. The blocks themselves
// are stored through the common/db version helpers, while balances,
// embedded-contract storage, the chain-plasma counter, received-send
// markers and the embedded sequencer cursor live under dedicated key
// prefixes of the same database. Depending on which database it
// wraps, the same store serves the momentum-confirmed ("stable")
// state or the account pool's unconfirmed frontier.
package account

import (
	"github.com/zenon-network/go-zenon/chain/store"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/db"
	"github.com/zenon-network/go-zenon/common/types"
)

func getStorageIterator() []byte {
	return storageKeyPrefix
}

// accountStore implements store.Account over one version of an
// account-chain database; Apply, Changes and Snapshot come from the
// embedded db.DB.
type accountStore struct {
	address types.Address
	db.DB
}

func (as *accountStore) Address() *types.Address {
	return &as.address
}

// Storage returns the embedded-contract key-value storage: the
// storage subset of the account database wrapped with
// db.DisableNotFound, so contract code reads missing keys as empty
// values instead of leveldb.ErrNotFound.
func (as *accountStore) Storage() db.DB {
	return db.DisableNotFound(as.Subset(getStorageIterator()))
}
func (as *accountStore) Snapshot() store.Account {
	return NewAccountStore(as.address, as.DB.Snapshot())
}

// Identifier returns the hash-height of the frontier account block,
// or the zero hash-height if the account chain is empty.
func (as *accountStore) Identifier() types.HashHeight {
	frontier, err := as.Frontier()
	common.DealWithErr(err)
	if frontier == nil {
		return types.ZeroHashHeight
	}
	return frontier.Identifier()
}

// NewAccountStore returns a store.Account for address that reads and
// writes through db, which must be one version of that account's
// chain database — either the momentum store's per-address subset or
// a version of the account pool's in-memory manager. It panics if db
// is nil.
func NewAccountStore(address types.Address, db db.DB) store.Account {
	if db == nil {
		panic("account store can't operate with nil db")
	}
	return &accountStore{
		address: address,
		DB:      db,
	}
}
