package db

import (
	"github.com/syndtr/goleveldb/leveldb/comparer"
	"github.com/syndtr/goleveldb/leveldb/memdb"
	"github.com/syndtr/goleveldb/leveldb/util"
)

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

func NewMemDB() DB {
	return enableDelete(newMemDBInternal(), false)
}
