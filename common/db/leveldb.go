package db

import (
	"runtime"

	"github.com/syndtr/goleveldb/leveldb"
	"github.com/syndtr/goleveldb/leveldb/iterator"
	"github.com/syndtr/goleveldb/leveldb/opt"
	"github.com/syndtr/goleveldb/leveldb/util"

	"github.com/zenon-network/go-zenon/common"
)

// getConsensusOpenFilesCacheCapacity returns the LevelDB open-files
// cache size used by NewLevelDB (which backs the consensus database).
// Darwin gets a much smaller cache because macOS default per-process
// file-descriptor limits are low.
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

// levelDBROWrapper adapts a read-only goleveldb handle (typically a
// snapshot) to the raw backend interface; Put and changesInternal
// panic. It is always used as the bottom layer of a merged db with a
// writable memdb on top.
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
}

// levelDBWrapper adapts a writable goleveldb handle to the raw
// backend interface. It cannot report its accumulated writes, so
// changesInternal (and therefore DB.Changes) panics.
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
	}))
}

// NewLevelDBWrapper exposes an open LevelDB handle as a DB. Writes go
// directly to the database; Changes is not supported and panics. Use
// Snapshot to obtain a write-isolated view.
func NewLevelDBWrapper(db *leveldb.DB) DB {
	return enableDelete(
		&levelDBWrapper{
			db: db,
		})
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
