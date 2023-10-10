package db

import (
	"encoding/hex"
	"fmt"

	"github.com/syndtr/goleveldb/leveldb"

	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/types"
)

type patch struct {
	*leveldb.Batch
}

func (p *patch) Replay(pr PatchReplayer) error {
	return p.Batch.Replay(pr)
}

func NewPatch() Patch {
	return &patch{
		Batch: new(leveldb.Batch),
	}
}
func NewPatchFromDump(data []byte) (Patch, error) {
	p := &patch{
		Batch: new(leveldb.Batch),
	}
	err := p.Batch.Load(data)
	return p, err
}

type patchPrinter struct {
	dump string
}

func (pp *patchPrinter) Put(key []byte, value []byte) {
	pp.dump = pp.dump + fmt.Sprintf("%v - %v\n", hex.EncodeToString(key), hex.EncodeToString(value))
}
func (pp *patchPrinter) Delete(key []byte) {
	pp.dump = pp.dump + fmt.Sprintf("%v - DELETE\n", hex.EncodeToString(key))
}

type patchRollback struct {
	db DB
	rb Patch
}

func (pr *patchRollback) rollback(key []byte) {
	value, err := pr.db.Get(key)
	if err == leveldb.ErrNotFound {
		pr.rb.Delete(key)
	} else {
		pr.rb.Put(key, value)
	}
}
func (pr *patchRollback) Put(key []byte, _ []byte) {
	pr.rollback(key)
}
func (pr *patchRollback) Delete(key []byte) {
	pr.rollback(key)
}

type patchApplier struct {
	err error
	db  DB
}

func (pa *patchApplier) Put(key []byte, value []byte) {
	if pa.err != nil {
		return
	}
	pa.err = pa.db.Put(key, value)
}
func (pa *patchApplier) Delete(key []byte) {
	if pa.err != nil {
		return
	}
	pa.err = pa.db.Delete(key)
}

type patchValuePrefixer struct {
	prefix []byte
	Patch
}

func (pa *patchValuePrefixer) Put(key []byte, value []byte) {
	pa.Patch.Put(key, common.JoinBytes(pa.prefix, value))
}

type patchApplierWO struct {
	err error
	db  db
}

func (pa *patchApplierWO) Put(key []byte, value []byte) {
	if pa.err != nil {
		return
	}
	if ok, err := pa.db.Has(key); err != nil {
		pa.err = err
	} else if !ok {
		pa.err = pa.db.Put(key, common.JoinBytes([]byte{0}, value))
	}
}
func (pa *patchApplierWO) Delete(key []byte) {
	if pa.err != nil {
		return
	}
	if ok, err := pa.db.Has(key); err != nil {
		pa.err = err
	} else if !ok {
		pa.err = pa.db.Put(key, []byte{0})
	}
}

func DebugPatch(patch Patch) string {
	pp := new(patchPrinter)
	err := patch.Replay(pp)
	common.DealWithErr(err)
	return pp.dump
}
func PatchHash(patch Patch) types.Hash {
	return types.NewHash(patch.Dump())
}
func DebugDB(db DB) string {
	iterator := db.NewIterator([]byte{})
	defer iterator.Release()

	s := ""
	for {
		if !iterator.Next() {
			common.DealWithErr(iterator.Error())
			break
		}

		value := iterator.Value()
		if value == nil {
			continue
		}
		key := iterator.Key()
		s = s + fmt.Sprintf("%v - %v\n", hex.EncodeToString(key), hex.EncodeToString(value))
	}
	return s
}
func DumpDB(db DB) Patch {
	p := NewPatch()
	iterator := db.NewIterator(nil)
	defer iterator.Release()

	for {
		if !iterator.Next() {
			if iterator.Error() != nil {
				common.DealWithErr(iterator.Error())
			}
			break
		}

		value := iterator.Value()
		key := iterator.Key()

		p.Put(key, value)
	}

	return p
}

func PrefixPatchValues(patch Patch, prefix []byte) Patch {
	pa := &patchValuePrefixer{
		prefix: prefix,
		Patch:  NewPatch(),
	}
	common.DealWithErr(patch.Replay(pa))
	return pa.Patch
}

func ApplyPatch(db DB, patch Patch) error {
	pa := &patchApplier{
		db: db,
	}

	if err := patch.Replay(pa); err != nil {
		return err
	}
	return pa.err
}
func ApplyWithoutOverride(db db, patch Patch) error {
	pa := &patchApplierWO{
		db: db,
	}

	if err := patch.Replay(pa); err != nil {
		return err
	}
	return pa.err
}
func RollbackPatch(db DB, patch Patch) Patch {
	pr := &patchRollback{
		db: db,
		rb: NewPatch(),
	}

	err := patch.Replay(pr)
	common.DealWithErr(err)

	return pr.rb
}
