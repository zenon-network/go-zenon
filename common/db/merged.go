package db

import (
	"github.com/syndtr/goleveldb/leveldb"
	"github.com/syndtr/goleveldb/leveldb/comparer"
)

func newMergedDb(dbs []db) db {
	return &mergedDB{
		dbs: dbs,
	}
}

type mergedDB struct {
	dbs []db
}

func (u *mergedDB) Get(key []byte) ([]byte, error) {
	for _, db := range u.dbs {
		if ok, err := db.Has(key); err != nil {
			return nil, err
		} else if ok {
			return db.Get(key)
		}
	}
	return nil, leveldb.ErrNotFound
}
func (u *mergedDB) Has(key []byte) (bool, error) {
	for _, db := range u.dbs {
		if ok, err := db.Has(key); err != nil {
			return false, err
		} else if ok {
			return true, nil
		}
	}
	return false, nil
}
func (u *mergedDB) Put(key, value []byte) error {
	return u.dbs[0].Put(key, value)
}
func (u *mergedDB) NewIterator(prefix []byte) StorageIterator {
	iterators := make([]StorageIterator, len(u.dbs))
	for i := range u.dbs {
		iterators[i] = u.dbs[i].NewIterator(prefix)
	}
	return newMergedIterator(iterators)
}

func (u *mergedDB) changesInternal(prefix []byte) (Patch, error) {
	return u.dbs[0].changesInternal(prefix)
}

const (
	noCurrent        = -1
	iteratorFinished = 1
)

type mergedIterator struct {
	cmp comparer.BasicComparer

	iterators []StorageIterator
	current   int
	status    []byte

	err error
}

func newMergedIterator(iterators []StorageIterator) StorageIterator {
	mi := &mergedIterator{
		cmp:       comparer.DefaultComparer,
		iterators: iterators,
		status:    make([]byte, len(iterators)),
		current:   noCurrent,
	}

	for index, i := range iterators {
		if !i.Next() {
			if err := i.Error(); err != nil && err != leveldb.ErrNotFound {
				mi.err = err
			}
			mi.status[index] = iteratorFinished
		}
	}
	return mi
}

func (mi *mergedIterator) Next() bool {
	return mi.step()
}
func (mi *mergedIterator) Key() []byte {
	if mi.current == noCurrent || mi.err != nil {
		return nil
	}
	return mi.iterators[mi.current].Key()
}
func (mi *mergedIterator) Value() []byte {
	if mi.current == noCurrent || mi.err != nil {
		return nil
	}
	return mi.iterators[mi.current].Value()
}
func (mi *mergedIterator) Error() error {
	return mi.err
}
func (mi *mergedIterator) Release() {
	for _, iter := range mi.iterators {
		iter.Release()
	}
}
func (mi *mergedIterator) step() bool {
	if mi.err != nil {
		return false
	}

	// call next on all iterators which have the key equal to current iterator before going forward
	if mi.current != noCurrent {
		i := mi.iterators[mi.current]
		key := make([]byte, len(i.Key()))
		copy(key, i.Key())

		for index, i := range mi.iterators {
			if mi.status[index] == iteratorFinished {
				continue
			}
			if mi.cmp.Compare(key, i.Key()) == 0 {
				if !i.Next() {
					if err := i.Error(); err != nil && err != leveldb.ErrNotFound {
						mi.err = err
						return false
					}
					mi.status[index] = iteratorFinished
				}
			}
		}
	}

	bestIndex := noCurrent
	var bestKey []byte
	for index, iterator := range mi.iterators {
		if mi.status[index] == iteratorFinished {
			continue
		}

		key := make([]byte, len(iterator.Key()))
		copy(key, iterator.Key())

		if bestIndex == noCurrent || bestKey == nil || mi.cmp.Compare(key, bestKey) == -1 {
			bestIndex = index
			bestKey = key
		}
	}

	mi.current = bestIndex
	if bestIndex == noCurrent {
		return false
	}
	return true
}
