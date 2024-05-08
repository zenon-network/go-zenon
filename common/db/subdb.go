package db

import (
	"github.com/zenon-network/go-zenon/common"
)

type removePatchKeyPrefix struct {
	prefixLength int
	Patch
}

func (edp *removePatchKeyPrefix) Put(key []byte, value []byte) {
	edp.Patch.Put(key[edp.prefixLength:], value)
}

type subDB struct {
	prefix []byte
	db     db
}

func newSubDB(prefix []byte, db db) db {
	return &subDB{
		prefix: prefix,
		db:     db,
	}
}

func (u *subDB) Get(key []byte) ([]byte, error) {
	return u.db.Get(common.JoinBytes(u.prefix, key))
}
func (u *subDB) Has(key []byte) (bool, error) {
	return u.db.Has(common.JoinBytes(u.prefix, key))
}
func (u *subDB) Put(key, value []byte) error {
	return u.db.Put(common.JoinBytes(u.prefix, key), value)
}
func (u *subDB) Delete(key []byte) error {
	return u.db.Delete(common.JoinBytes(u.prefix, key))
}
func (u *subDB) NewIterator(prefix []byte) StorageIterator {
	return newSubIterator(len(u.prefix), u.db.NewIterator(common.JoinBytes(u.prefix, prefix)))
}

func (u *subDB) changesInternal(prefix []byte) (Patch, error) {
	changes, err := u.db.changesInternal(common.JoinBytes(u.prefix, prefix))
	if err != nil {
		return nil, err
	}

	p := &removePatchKeyPrefix{
		prefixLength: len(u.prefix),
		Patch:        NewPatch(),
	}

	if err := changes.Replay(p); err != nil {
		return nil, err
	}
	return p.Patch, nil
}

type subIterator struct {
	prefixLen int
	StorageIterator
}

func (si *subIterator) Key() []byte {
	return si.StorageIterator.Key()[si.prefixLen:]
}

func newSubIterator(prefixLen int, iterator StorageIterator) StorageIterator {
	return &subIterator{
		prefixLen:       prefixLen,
		StorageIterator: iterator,
	}
}
