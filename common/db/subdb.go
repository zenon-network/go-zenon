package db

import (
	"github.com/zenon-network/go-zenon/common"
)

// removePatchKeyPrefix is a PatchReplayer that copies operations into
// another Patch with the first prefixLength bytes stripped from every
// key; subDB uses it to report changes in subset key space.
type removePatchKeyPrefix struct {
	prefixLength int
	Patch
}

func (edp *removePatchKeyPrefix) Put(key []byte, value []byte) {
	edp.Patch.Put(key[edp.prefixLength:], value)
}

// subDB implements DB.Subset on a raw backend: every key is
// transparently prefixed on writes and reads, and stripped of the
// prefix in iteration and change reporting, so callers operate in
// their own key space while sharing the parent's storage.
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

// subIterator strips the subset prefix from iterated keys so they
// appear in the subset's own key space.
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
