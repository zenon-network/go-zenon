package db

import (
	"encoding/hex"
	"fmt"

	"github.com/syndtr/goleveldb/leveldb"

	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/types"
)

// patch is the Patch implementation: a thin wrapper over
// leveldb.Batch, whose binary encoding doubles as the Dump format.
type patch struct {
	*leveldb.Batch
}

func (p *patch) Replay(pr PatchReplayer) error {
	return p.Batch.Replay(pr)
}

// NewPatch returns a new, empty Patch.
func NewPatch() Patch {
	return &patch{
		Batch: new(leveldb.Batch),
	}
}

// NewPatchFromDump restores a Patch from the bytes produced by
// Patch.Dump.
func NewPatchFromDump(data []byte) (Patch, error) {
	p := &patch{
		Batch: new(leveldb.Batch),
	}
	err := p.Batch.Load(data)
	return p, err
}

// patchPrinter is a PatchReplayer that renders each operation as a
// hex "key - value" (or "key - DELETE") line; used by DebugPatch.
type patchPrinter struct {
	dump string
}

func (pp *patchPrinter) Put(key []byte, value []byte) {
	pp.dump = pp.dump + fmt.Sprintf("%v - %v\n", hex.EncodeToString(key), hex.EncodeToString(value))
}
func (pp *patchPrinter) Delete(key []byte) {
	pp.dump = pp.dump + fmt.Sprintf("%v - DELETE\n", hex.EncodeToString(key))
}

// patchRollback is a PatchReplayer that records, for every key
// touched by the replayed patch, the value that key currently has in
// db — or its absence — producing the inverse patch for
// RollbackPatch.
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

// patchApplier is a PatchReplayer that forwards each operation to a
// DB, remembering the first error and dropping everything after it.
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

// patchValuePrefixer is a PatchReplayer that copies operations into
// another Patch, prepending prefix to every Put value; used by
// PrefixPatchValues.
type patchValuePrefixer struct {
	prefix []byte
	Patch
}

func (pa *patchValuePrefixer) Put(key []byte, value []byte) {
	pa.Patch.Put(key, common.JoinBytes(pa.prefix, value))
}

// patchApplierWO is the first-write-wins applier behind
// ApplyWithoutOverride. It writes to a raw backend in the
// enableDelete encoding: Put stores {0}+value and Delete stores the
// one-byte tombstone {0}; keys already present are left untouched.
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

// DebugPatch renders patch as one hex "key - value" (or
// "key - DELETE") line per operation, in insertion order. It is meant
// for tests and debugging and panics if the patch cannot be replayed.
func DebugPatch(patch Patch) string {
	pp := new(patchPrinter)
	err := patch.Replay(pp)
	common.DealWithErr(err)
	return pp.dump
}

// PatchHash returns the SHA3-256 hash of the patch's serialized form.
// The VM uses it to compute the ChangesHash committed in account
// blocks and momentums, so the hash covers both the operations and
// their order.
func PatchHash(patch Patch) types.Hash {
	return types.NewHash(patch.Dump())
}

// DebugDB renders the full contents of db as one hex "key - value"
// line per pair, in ascending key order, skipping deletion markers.
// It is meant for tests and debugging and panics on iteration errors.
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

// DumpDB copies the entire visible contents of db into a Patch of Put
// operations, in ascending key order. It panics on iteration errors.
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

// PrefixPatchValues returns a new Patch with every Put value of patch
// prefixed by prefix; Delete operations are copied unchanged. It
// panics if the patch cannot be replayed.
func PrefixPatchValues(patch Patch, prefix []byte) Patch {
	pa := &patchValuePrefixer{
		prefix: prefix,
		Patch:  NewPatch(),
	}
	common.DealWithErr(patch.Replay(pa))
	return pa.Patch
}

// ApplyPatch replays patch onto db in insertion order, stopping at
// the first failed operation and returning its error.
func ApplyPatch(db DB, patch Patch) error {
	pa := &patchApplier{
		db: db,
	}

	if err := patch.Replay(pa); err != nil {
		return err
	}
	return pa.err
}

// ApplyWithoutOverride replays patch onto a raw backend with
// first-write-wins semantics: keys that already exist in db are left
// untouched, new values are stored in the enableDelete encoding and
// deletions become explicit tombstones. The LevelDB-backed Manager
// relies on this to fold rollback patches in ascending height order —
// the first (oldest) write wins, which is equivalent to applying them
// newest-first with overwrites.
func ApplyWithoutOverride(db db, patch Patch) error {
	pa := &patchApplierWO{
		db: db,
	}

	if err := patch.Replay(pa); err != nil {
		return err
	}
	return pa.err
}

// RollbackPatch computes the inverse of patch relative to the current
// state of db: for every key the patch touches, the result restores
// the value the key has now (or deletes it if absent). Applying the
// returned patch after patch itself leaves db unchanged; the
// LevelDB-backed Manager persists these inverses to undo and to
// reconstruct historical versions. The patch must not have been
// applied to db yet. It panics if patch cannot be replayed.
func RollbackPatch(db DB, patch Patch) Patch {
	pr := &patchRollback{
		db: db,
		rb: NewPatch(),
	}

	err := patch.Replay(pr)
	common.DealWithErr(err)

	return pr.rb
}
