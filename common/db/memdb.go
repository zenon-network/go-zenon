package db

import (
	"github.com/syndtr/goleveldb/leveldb/comparer"
	"github.com/syndtr/goleveldb/leveldb/memdb"
	"github.com/syndtr/goleveldb/leveldb/util"
)

// memDBWrapper adapts goleveldb's in-memory memdb to the raw backend
// interface. It is the only backend that implements changesInternal,
// which makes memory-backed DBs (and snapshot overlays, whose top
// layer is a memdb) the only ones that support DB.Changes.
type memDBWrapper struct {
	*memdb.DB
}

func (mdbw *memDBWrapper) Has(key []byte) (bool, error) {
	return mdbw.Contains(key), nil
}
func (mdbw *memDBWrapper) NewIterator(prefix []byte) StorageIterator {
	return mdbw.DB.NewIterator(util.BytesPrefix(prefix))
}
func (mdbw *memDBWrapper) changesInternal(prefix []byte) (Patch, error) {
	p := NewPatch()
	iterator := mdbw.NewIterator(prefix)
	defer iterator.Release()

	for {
		if !iterator.Next() {
			if iterator.Error() != nil {
				return nil, iterator.Error()
			}
			break
		}

		value := iterator.Value()
		key := iterator.Key()
		p.Put(key, value)
	}

	return p, nil
}

func newMemDBInternal() db {
	return &memDBWrapper{
		DB: memdb.New(comparer.DefaultComparer, 0),
	}
}

// NewMemDB returns an empty in-memory DB. Unlike the LevelDB-backed
// flavours it fully supports Changes, so it serves as the scratch
// state for building patches (for example the frontier bookkeeping in
// Manager.Add), as the account pool's uncommitted state, and in
// tests.
func NewMemDB() DB {
	return enableDelete(newMemDBInternal())
}
