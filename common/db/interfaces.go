// Package db implements the versioned key-value storage layer that
// backs every ledger in the node: the momentum chain, each account
// chain and the consensus caches all persist through the DB, Patch
// and Manager abstractions defined here, on top of goleveldb (an
// on-disk LevelDB or its in-memory memdb).
//
// The unit of change is the Patch: an ordered, replayable batch of
// Put/Delete operations. Writes made to a DB can be exported as a
// Patch (Changes), applied atomically to another DB (ApplyPatch),
// inverted against a base state (RollbackPatch), serialized (Dump)
// and hashed (PatchHash — this is how the ChangesHash field of
// account blocks and momentums is computed).
//
// The Manager maintains the versioned database: every version is
// identified by the hash-height of the Commit that produced it, and
// Add appends a Transaction (one or more commits plus their state
// patch) on top of the current frontier, while Pop rolls the frontier
// back one version. The LevelDB-backed manager persists only the
// frontier state plus per-height patch and rollback patches, and
// reconstructs historical versions on demand by folding rollback
// patches into an in-memory overlay.
//
// Missing keys are reported as leveldb.ErrNotFound by every DB
// flavour, including memory-backed ones; higher layers (for example
// the ledger stores) rely on this single sentinel. DisableNotFound
// wraps a DB to read missing keys as empty values instead, which is
// the behaviour embedded-contract storage expects.
package db

import (
	"github.com/zenon-network/go-zenon/common/types"
)

// PatchReplayer receives the operations of a Patch, in insertion
// order, during Patch.Replay. Implementations react to each operation
// to apply a patch to a database, invert it into a rollback patch,
// pretty-print it, and so on.
type PatchReplayer interface {
	Put(key []byte, value []byte)
	Delete(key []byte)
}

// Patch is an ordered, serializable batch of Put/Delete operations —
// the package's unit of atomic change (backed by a leveldb.Batch).
// Replay feeds the recorded operations, in order, to a PatchReplayer.
// Dump serializes the patch to bytes; NewPatchFromDump restores it
// and PatchHash hashes it.
type Patch interface {
	Put(key []byte, value []byte)
	Delete(key []byte)

	Replay(PatchReplayer) error
	Dump() []byte
}

// Commit is one ledger entry (an account block or a momentum) being
// committed to a Manager. Identifier returns the entry's hash-height
// — which becomes the identifier of the database version it produces
// — and Previous the identifier of the entry before it. Serialize
// returns the binary form stored as that version's entry (see
// SetFrontier / GetEntryByHeight).
type Commit interface {
	Identifier() types.HashHeight
	Previous() types.HashHeight
	Serialize() ([]byte, error)
}

// Transaction bundles the commits of one insertion together with the
// state Patch they produce; it is the unit accepted by Manager.Add.
// GetCommits returns the entries in chain order, oldest first (for
// account-block transactions, descendant blocks precede the main
// block). StealChanges returns the patch and transfers ownership:
// implementations nil out their internal reference, so it must be
// called at most once.
type Transaction interface {
	GetCommits() []Commit
	StealChanges() Patch
}

// StorageIterator is the minimal forward iterator over a key range,
// in ascending byte order of keys. Next advances to the next pair and
// must be called before the first Key/Value access; when it returns
// false the iteration is exhausted or failed, and Error distinguishes
// the two. Release frees the underlying resources and must always be
// called.
type StorageIterator interface {
	Next() bool

	Key() []byte
	Value() []byte
	Error() error
	Release()
}

// DB is a key-value store with byte-ordered keys. Get returns
// leveldb.ErrNotFound for missing keys; Has never does. NewIterator
// iterates all keys that start with prefix. Subset returns a view of
// the keys under prefix, with the prefix stripped from keys on both
// reads and writes.
//
// Apply replays a Patch onto the store. Changes exports the writes
// accumulated by this instance (since its creation or snapshot) as a
// Patch; it is supported only by memory-backed instances, including
// snapshot overlays — other flavours panic. Snapshot returns a
// copy-on-write view: writes to
// the returned DB land in a fresh in-memory layer and never reach the
// receiver.
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

// db is the raw backend interface implemented by the goleveldb
// wrappers. It has no Delete: deletion is layered on by enableDelete,
// which encodes a live value v as {0}+v and a deleted key as an empty
// record, so that deletions remain visible to overlays and patches.
type db interface {
	Get([]byte) ([]byte, error)
	Has([]byte) (bool, error)
	Put(key, value []byte) error

	NewIterator(prefix []byte) StorageIterator

	changesInternal(prefix []byte) (Patch, error)
}
