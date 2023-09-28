package db

import (
	"math/rand"
	"sync"
	"testing"

	"github.com/zenon-network/go-zenon/common"
)

func TestStressMemDB(t *testing.T) {
	simpleMemDBOperations(t, NewMemDB())
	r := rand.New(rand.NewSource(rand.Int63()))
	stressTestConcurrentUse(t, NewMemDB(), 1e5, 20, r)
	stressTestConcurrentUse(t, NewMemDB().Subset([]byte{10, 11, 12, 13}), 1e5, 10, r)
}

func TestChanges(t *testing.T) {
	db := NewMemDB()
	simpleMemDBOperations(t, db)
	simpleMemDBOperations(t, db.Subset([]byte{16}))

	changes, err := db.Changes()
	common.FailIfErr(t, err)
	common.ExpectString(t, DebugPatch(changes), `
010203 - 0102030405
01020304 - DELETE
10010203 - 0102030405
1001020304 - DELETE
`)

	changes, err = db.Subset([]byte{1, 2}).Changes()
	common.FailIfErr(t, err)
	common.ExpectString(t, DebugPatch(changes), `
03 - 0102030405
0304 - DELETE
`)

	changes, err = db.Snapshot().Changes()
	common.FailIfErr(t, err)
	common.ExpectString(t, DebugPatch(changes), `
`)
}

func TestMergedIterators(t *testing.T) {
	db1 := newMemDBInternal()
	db1.Put([]byte{0, 1, 1}, []byte{0, 16, 32, 1})
	db1.Put([]byte{0, 1, 2}, []byte{0})
	db1.Put([]byte{0, 2, 1}, []byte{0, 16, 32, 3})
	db1.Put([]byte{0, 2, 2}, []byte{0})
	db1.Put([]byte{0, 3, 1}, []byte{0, 16, 32, 5})
	db1.Put([]byte{0, 3, 2}, []byte{0})

	db2 := newMemDBInternal()
	db2.Put([]byte{0, 1, 1}, []byte{0, 16, 32, 17})
	db2.Put([]byte{0, 1, 2}, []byte{0, 16, 32, 18})
	db2.Put([]byte{0, 1, 3}, []byte{0, 16, 32, 19})
	db2.Put([]byte{0, 2, 1}, []byte{0})
	db2.Put([]byte{0, 2, 2}, []byte{0})
	db2.Put([]byte{0, 2, 3}, []byte{0})

	common.ExpectString(t, DebugDB(enableDelete(newMergedDb([]db{
		db1, db2,
	}))), `
000101 - 102001
000102 - 
000103 - 102013
000201 - 102003
000202 - 
000203 - 
000301 - 102005
000302 - `)
	common.ExpectString(t, DebugDB(enableDelete(newMergedDb([]db{
		db1, newSkipDelete(db2),
	}))), `
000101 - 102001
000102 - 
000103 - 102013
000201 - 102003
000202 - 
000301 - 102005
000302 - `)
	common.ExpectString(t, DebugDB(enableDelete(newSkipDelete(newMergedDb([]db{
		db1, db2,
	})))), `
000101 - 102001
000103 - 102013
000201 - 102003
000301 - 102005`)
}

func simpleMemDBOperations(t *testing.T, db DB) {
	common.FailIfErr(t, db.Put([]byte{1, 2, 3}, []byte{1, 2, 3, 4, 5}))
	common.FailIfErr(t, db.Put([]byte{1, 2, 3, 4}, []byte{1, 2, 3, 4, 5}))
	common.FailIfErr(t, db.Put([]byte{1, 2, 3, 4}, []byte{1, 2, 3, 4, 5, 6}))
	bytes, err := db.Get([]byte{1, 2, 3, 4})
	common.FailIfErr(t, err)
	common.ExpectBytes(t, bytes, "0x010203040506")
	common.FailIfErr(t, db.Delete([]byte{1, 2, 3, 4}))
	common.FailIfErr(t, db.Delete([]byte{1, 2, 3, 4}))
}

func stressTestConcurrentUse(t *testing.T, db DB, numInserts int, numThreads int, r *rand.Rand) {
	inputs := make([][][2][]byte, 0, numThreads)
	dbs := make([]DB, 0, numThreads)

	for i := 0; i < numThreads; i += 1 {
		dbs = append(dbs, db)
		currentInputs := make([][2][]byte, numInserts)
		for i := 0; i < numInserts; i += 1 {
			currentInputs[i][0] = common.Uint64ToBytes(r.Uint64())
			currentInputs[i][1] = common.Uint64ToBytes(r.Uint64())
		}
		inputs = append(inputs, currentInputs)
	}

	wg := new(sync.WaitGroup)
	wg.Add(numThreads)
	for i := 0; i < numThreads; i += 1 {
		go func(number int) {
			db := dbs[number]
			input := inputs[number]
			for j := 0; j < numInserts; j += 1 {
				common.FailIfErr(t, db.Put(input[j][0], input[j][1]))
			}
			wg.Done()
		}(i)
	}

	wg.Wait()
}
