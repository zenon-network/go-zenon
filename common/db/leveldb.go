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

func NewLevelDBSnapshotWrapper(ldb *leveldb.Snapshot) DB {
	return enableDelete(newMergedDb([]db{
		newMemDBInternal(),
		&levelDBROWrapper{
			db: ldb,
		},
	}), false)
}

func NewLevelDBWrapper(db *leveldb.DB) DB {
	return enableDelete(
		&levelDBWrapper{
			db: db,
		}, false)
}

func NewLevelDBWrapperWithFullDelete(db *leveldb.DB) DB {
	return enableDelete(
		&levelDBWrapper{
			db: db,
		}, true)
}

func NewLevelDB(dirname string) (DB, *leveldb.DB) {
	opts := &opt.Options{OpenFilesCacheCapacity: getConsensusOpenFilesCacheCapacity()}
	db, err := leveldb.OpenFile(dirname, opts)
	common.DealWithErr(err)
	return NewLevelDBWrapper(db), db
}
