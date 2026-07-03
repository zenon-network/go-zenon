package db

import (
	"github.com/syndtr/goleveldb/leveldb"

	"github.com/zenon-network/go-zenon/common"
)

var (
	// existsByte is prepended to every stored value by enableDeleteDB
	// so that a deleted key (stored as an empty record) can be told
	// apart from a live one.
	existsByte = []byte{0}
)

// enableDeletePatch is a PatchReplayer that translates a patch from
// the encoded representation back to the logical one: empty records
// become Delete operations and the exists byte is stripped from
// values. Encoded patches never contain Delete operations, so Delete
// panics.
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

// enableDeleteDB implements the full DB interface on top of a raw
// backend that has no Delete. Every logical value v is stored as
// existsByte+v and Delete stores an empty record, so deletions stay
// visible to overlays, change patches and historical reconstruction
// instead of silently unmasking older values. Get translates both a
// missing key and an empty record into leveldb.ErrNotFound.
type enableDeleteDB struct {
	db db
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
	return d.db.Put(key, []byte{})
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
	return enableDelete(newMergedDb([]db{newMemDBInternal(), d.db}))
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
	return enableDelete(newSubDB(prefix, d.db))
}

// enableDeleteIterator decodes iterated values: deletion markers are
// reported as nil values (callers such as DebugDB skip them) and live
// values have the exists byte stripped.
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

// enableDelete upgrades a raw backend to the full DB interface; every
// public DB constructor in this package goes through it.
func enableDelete(db db) DB {
	return &enableDeleteDB{
		db: db,
	}
}
