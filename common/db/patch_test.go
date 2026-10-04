package db

import (
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
	rp1 := RollbackPatch(db1, p1)

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
