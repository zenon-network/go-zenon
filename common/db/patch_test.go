package db

import (
	"testing"

	"github.com/zenon-network/go-zenon/common"
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
