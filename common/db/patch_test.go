package db

import (
	"errors"
	"testing"

	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/types"
)

func TestRollbackPatch(t *testing.T) {
	db1 := NewMemDB()

	db1.Put([]byte{1, 2, 3}, []byte{100})
	db1.Put([]byte{4, 1, 2}, []byte{0, 7, 1})
	db1.Put([]byte{7, 31}, []byte{0, 7, 1})

	db2 := db1.Snapshot()

	db2.Delete([]byte{7, 31})
	db2.Delete([]byte{4, 1, 2})
	db2.Put([]byte{1, 2, 3}, []byte{200})
	db2.Put([]byte{4, 1, 2}, []byte{1, 2, 3, 4, 5, 6})
	db2.Delete([]byte{4, 1, 2})

	p1, _ := db2.Changes()
	rp1, err := RollbackPatch(db1, p1)
	common.FailIfErr(t, err)

	common.ExpectString(t, DebugPatch(rp1), `
010203 - 64
040102 - 000701
071f - 000701`)
	common.ExpectString(t, DebugPatch(p1), `
010203 - c8
040102 - DELETE
071f - DELETE`)
}

func TestRemoveKeys(t *testing.T) {
	skipped := []byte{9, 9}
	kept := []byte{1, 2, 3}

	p := NewPatch()
	p.Put(skipped, []byte{1})
	p.Put(kept, []byte{100})
	p.Delete(skipped)
	p.Delete(kept)
	p.Put(kept, []byte{200})
	p.Put(skipped, []byte{2})

	filtered, err := RemoveKeys(p, [][]byte{skipped})
	common.FailIfErr(t, err)

	// Every put and delete on the skipped key is gone; the rest keeps its
	// order.
	common.ExpectString(t, DebugPatch(filtered), `
010203 - 64
010203 - DELETE
010203 - c8`)
	// The source patch is untouched.
	common.ExpectString(t, DebugPatch(p), `
0909 - 01
010203 - 64
0909 - DELETE
010203 - DELETE
010203 - c8
0909 - 02`)

	// Removing nothing yields an identical copy.
	same, err := RemoveKeys(p, nil)
	common.FailIfErr(t, err)
	common.ExpectString(t, string(same.Dump()), string(p.Dump()))
}

func TestFrontierWriteKeysAreCopies(t *testing.T) {
	version := types.HashHeight{Hash: types.NewHash([]byte("frontier")), Height: 7}
	keys := FrontierWriteKeys(version)
	common.Expect(t, len(keys), 3)
	keys[0][0] ^= 0xff
	again := FrontierWriteKeys(version)
	if string(again[0]) == string(keys[0]) {
		t.Fatal("modifying a returned key altered the key returned next time")
	}
	common.ExpectString(t, string(again[0]), string(frontierIdentifierKey))
}

// failingGetDB fails reads of one key with a store error other than
// ErrNotFound.
type failingGetDB struct {
	DB
	key []byte
	err error
}

func (f *failingGetDB) Get(key []byte) ([]byte, error) {
	if string(key) == string(f.key) {
		return nil, f.err
	}
	return f.DB.Get(key)
}

// RollbackPatch records, for each key the patch touches, the value the key
// has now. A read that fails for a reason other than the key being absent
// leaves that value unknown: the inverse cannot be built, and recording the
// key as empty would make a later rollback write the wrong value.
func TestRollbackPatchPropagatesReadError(t *testing.T) {
	base := NewMemDB()
	common.FailIfErr(t, base.Put([]byte{1}, []byte{10}))
	common.FailIfErr(t, base.Put([]byte{2}, []byte{20}))

	patch := NewPatch()
	patch.Put([]byte{1}, []byte{11})
	patch.Put([]byte{2}, []byte{21})
	patch.Delete([]byte{3})

	readErr := errors.New("injected read failure")
	rollback, err := RollbackPatch(&failingGetDB{DB: base, key: []byte{2}, err: readErr}, patch)
	if !errors.Is(err, readErr) {
		t.Fatalf("RollbackPatch with a failing read = (%v, %v), want the read error", rollback, err)
	}
	if rollback != nil {
		t.Fatalf("RollbackPatch returned an inverse beside its error:%s", DebugPatch(rollback))
	}

	// Absent keys are still recorded as deletes.
	rollback, err = RollbackPatch(base, patch)
	common.FailIfErr(t, err)
	common.ExpectString(t, DebugPatch(rollback), `
01 - 0a
02 - 14
03 - DELETE`)
}
