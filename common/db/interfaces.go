package db

import (
	"github.com/zenon-network/go-zenon/common/types"
)

type PatchReplayer interface {
	Put(key []byte, value []byte)
	Delete(key []byte)
}

type Patch interface {
	Put(key []byte, value []byte)
	Delete(key []byte)

	Replay(PatchReplayer) error
	Dump() []byte
}
type Commit interface {
	Identifier() types.HashHeight
	Previous() types.HashHeight
	Serialize() ([]byte, error)
}
type Transaction interface {
	GetCommits() []Commit
	StealChanges() Patch
}

type StorageIterator interface {
	Next() bool
	Prev() bool
	Last() bool

	Key() []byte
	Value() []byte
	Error() error
	Release()
}

type DB interface {
	Get([]byte) ([]byte, error)
	Has([]byte) (bool, error)
	Put(key, value []byte) error
	Delete(key []byte) error

	NewIterator(prefix []byte) StorageIterator
	Subset(prefix []byte) DB

	Apply(Patch) error
	Changes() (Patch, error)
	Snapshot() DB
}

type db interface {
	Get([]byte) ([]byte, error)
	Has([]byte) (bool, error)
	Put(key, value []byte) error
	Delete(key []byte) error

	NewIterator(prefix []byte) StorageIterator

	changesInternal(prefix []byte) (Patch, error)
}
