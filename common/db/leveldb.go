package db

import (
	"runtime"

	"github.com/syndtr/goleveldb/leveldb"
	"github.com/syndtr/goleveldb/leveldb/iterator"
	"github.com/syndtr/goleveldb/leveldb/opt"
	"github.com/syndtr/goleveldb/leveldb/util"

	"github.com/zenon-network/go-zenon/common"
)

func getConsensusOpenFilesCacheCapacity() int {
	switch runtime.GOOS {
	case "darwin":
		return 20
	case "windows":
		return 200
	default:
		return 200
	}
}

// LevelDBLikeRO is the read-only subset of the goleveldb API that the
// wrappers in this package need; both *leveldb.DB and
// *leveldb.Snapshot satisfy it.
type LevelDBLikeRO interface {
	Get(key []byte, ro *opt.ReadOptions) (value []byte, err error)
	Has(key []byte, ro *opt.ReadOptions) (ret bool, err error)
	NewIterator(slice *util.Range, ro *opt.ReadOptions) iterator.Iterator
}

type levelDBROWrapper struct {
	db LevelDBLikeRO
}

func (ro *levelDBROWrapper) Get(key []byte) ([]byte, error) {
	return ro.db.Get(key, nil)
}
func (ro *levelDBROWrapper) Has(key []byte) (bool, error) {
	return ro.db.Has(key, nil)
}
func (ro *levelDBROWrapper) Put(key []byte, value []byte) error {
	panic("unimplemented")
}
func (ro *levelDBROWrapper) Delete(key []byte) error {
	panic("unimplemented")
}
func (ro *levelDBROWrapper) changesInternal(prefix []byte) (Patch, error) {
	panic("unimplemented")
}
func (ro *levelDBROWrapper) NewIterator(prefix []byte) StorageIterator {
	return ro.db.NewIterator(util.BytesPrefix(prefix), nil)
}

// LevelDBLike extends LevelDBLikeRO with writes; *leveldb.DB
// satisfies it.
type LevelDBLike interface {
	LevelDBLikeRO
	Put(key []byte, value []byte, wo *opt.WriteOptions) error
	Delete(key []byte, wo *opt.WriteOptions) error
}

type levelDBWrapper struct {
	db LevelDBLike
}

func (ldbw *levelDBWrapper) Get(key []byte) ([]byte, error) {
	return ldbw.db.Get(key, nil)
}
func (ldbw *levelDBWrapper) Has(key []byte) (bool, error) {
	return ldbw.db.Has(key, nil)
}
func (ldbw *levelDBWrapper) Put(key, value []byte) error {
	return ldbw.db.Put(key, value, nil)
}
func (ldbw *levelDBWrapper) Delete(key []byte) error {
	return ldbw.db.Delete(key, nil)
}
func (ldbw *levelDBWrapper) NewIterator(prefix []byte) StorageIterator {
	return ldbw.db.NewIterator(util.BytesPrefix(prefix), nil)
}

func (ldbw *levelDBWrapper) changesInternal(prefix []byte) (Patch, error) {
	panic("unimplemented")
}

func newLevelDBSnapshotWrapper(ldb *leveldb.Snapshot) db {
	return newMergedDb([]db{
		newMemDBInternal(),
		&levelDBROWrapper{
			db: ldb,
		},
	})
}

// NewLevelDBSnapshotWrapper exposes a point-in-time LevelDB snapshot
// as a DB. Reads fall through to the frozen snapshot; writes land in
// an in-memory overlay and never touch the underlying database, and
// Changes reports exactly those overlay writes. The Manager hands out
// such wrappers as its version views.
func NewLevelDBSnapshotWrapper(ldb *leveldb.Snapshot) DB {
	return enableDelete(newMergedDb([]db{
		newMemDBInternal(),
		&levelDBROWrapper{
			db: ldb,
		},
	}), false)
}

// NewLevelDBWrapper exposes an open LevelDB handle as a DB. Writes go
// directly to the database; Changes is not supported and panics. Use
// Snapshot to obtain a write-isolated view.
func NewLevelDBWrapper(db *leveldb.DB) DB {
	return enableDelete(
		&levelDBWrapper{
			db: db,
		}, false)
}

// NewLevelDBWrapperWithFullDelete is NewLevelDBWrapper with physical
// deletion: Delete removes the key from LevelDB instead of writing the
// tombstone the default wrapper uses, and that mode is inherited by its
// snapshots and subsets. Has and Get behave the same in both modes.
// The chain cache manager uses it when it computes rollback patches.
func NewLevelDBWrapperWithFullDelete(db *leveldb.DB) DB {
	return enableDelete(
		&levelDBWrapper{
			db: db,
		}, true)
}

// NewLevelDB opens (or creates) the LevelDB database at dirname and
// returns it both wrapped as a DB and as the raw goleveldb handle —
// the caller is responsible for closing the latter. It panics if the
// database cannot be opened. The consensus layer uses it for its
// election cache.
func NewLevelDB(dirname string) (DB, *leveldb.DB) {
	opts := &opt.Options{OpenFilesCacheCapacity: getConsensusOpenFilesCacheCapacity()}
	db, err := leveldb.OpenFile(dirname, opts)
	common.DealWithErr(err)
	return NewLevelDBWrapper(db), db
}
