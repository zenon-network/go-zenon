package db

import (
	"github.com/syndtr/goleveldb/leveldb"

	"github.com/zenon-network/go-zenon/common"
)

var (
	existsByte = []byte{0}
)

type enableDeletePatch struct {
	p Patch
}

func (p *enableDeletePatch) Put(key []byte, value []byte) {
	if len(value) == 0 {
		p.p.Delete(key)
	} else {
		p.p.Put(key, value[1:])
	}
}
func (p *enableDeletePatch) Delete(key []byte) {
	panic("impossible")
}

type enableDeleteDB struct {
	db         db
	fullDelete bool
}

func (d *enableDeleteDB) Has(key []byte) (bool, error) {
	data, err := d.db.Get(key)
	if err == leveldb.ErrNotFound {
		return false, nil
	} else if err != nil {
		return false, err
	}
	if len(data) == 0 {
		return false, nil
	}
	return true, nil
}
func (d *enableDeleteDB) Get(key []byte) ([]byte, error) {
	data, err := d.db.Get(key)
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return nil, leveldb.ErrNotFound
	}
	return data[1:], nil
}
func (d *enableDeleteDB) Put(key, value []byte) error {
	return d.db.Put(key, common.JoinBytes(existsByte, value))
}
func (d *enableDeleteDB) Delete(key []byte) error {
	if d.fullDelete {
		return d.db.Delete(key)
	} else {
		return d.db.Put(key, []byte{})
	}
}
func (d *enableDeleteDB) NewIterator(prefix []byte) StorageIterator {
	return newEnableDeleteIterator(d.db.NewIterator(prefix))
}

func (d *enableDeleteDB) Changes() (Patch, error) {
	p, err := d.db.changesInternal([]byte{})
	if err != nil {
		return nil, err
	}
	edp := &enableDeletePatch{
		p: NewPatch(),
	}
	err = p.Replay(edp)
	if err != nil {
		return nil, err
	}
	return edp.p, nil
}
func (d *enableDeleteDB) Snapshot() DB {
	return enableDelete(newMergedDb([]db{newMemDBInternal(), d.db}), d.fullDelete)
}
func (d *enableDeleteDB) Apply(patch Patch) error {
	pa := &patchApplier{
		err: nil,
		db:  d,
	}
	if err := patch.Replay(pa); err != nil {
		return err
	}
	return pa.err
}
func (d *enableDeleteDB) Subset(prefix []byte) DB {
	return enableDelete(newSubDB(prefix, d.db), d.fullDelete)
}

type enableDeleteIterator struct {
	StorageIterator
}

func (i *enableDeleteIterator) Value() []byte {
	val := i.StorageIterator.Value()
	if len(val) == 0 {
		return nil
	}
	return val[1:]
}

func newEnableDeleteIterator(iterator StorageIterator) StorageIterator {
	return &enableDeleteIterator{
		StorageIterator: iterator,
	}
}
func enableDelete(db db, fullDelete bool) DB {
	return &enableDeleteDB{
		db:         db,
		fullDelete: fullDelete,
	}
}
