package db

import (
	"testing"

	"github.com/zenon-network/go-zenon/common"
)

// TestEnableDeleteIteratorSkipsTombstones verifies that the enableDelete
// iterator skips tombstone entries (keys deleted via enableDeleteDB.Delete,
// which writes an empty value). Before the fix, Next() did not skip these,
// so Value() returned nil for tombstoned keys, causing
// UnpackVariablePanic to panic on errEmptyInput in collection readers.
func TestEnableDeleteIteratorSkipsTombstones(t *testing.T) {
	db := NewMemDB()

	prefix := []byte{1}
	key1 := common.JoinBytes(prefix, []byte{0x01})
	key2 := common.JoinBytes(prefix, []byte{0x02})
	key3 := common.JoinBytes(prefix, []byte{0x03})

	common.FailIfErr(t, db.Put(key1, []byte{0xAA}))
	common.FailIfErr(t, db.Put(key2, []byte{0xBB}))
	common.FailIfErr(t, db.Put(key3, []byte{0xCC}))

	// Delete the middle entry (writes a tombstone via enableDeleteDB.Delete)
	common.FailIfErr(t, db.Delete(key2))

	// Iterate forward: should see key1 and key3, skipping the tombstoned key2
	iterator := db.NewIterator(prefix)
	defer iterator.Release()

	var keys [][]byte
	for iterator.Next() {
		val := iterator.Value()
		if len(val) == 0 {
			t.Fatalf("iterator returned empty value for key %x (tombstone not skipped)", iterator.Key())
		}
		keys = append(keys, iterator.Key())
	}
	common.FailIfErr(t, iterator.Error())

	if len(keys) != 2 {
		t.Fatalf("expected 2 keys, got %d", len(keys))
	}
	common.ExpectBytes(t, keys[0], "0x0101")
	common.ExpectBytes(t, keys[1], "0x0103")
}

// TestEnableDeleteIteratorPrevSkipsTombstones verifies that reverse
// iteration also skips tombstones.
func TestEnableDeleteIteratorPrevSkipsTombstones(t *testing.T) {
	db := NewMemDB()

	prefix := []byte{1}
	key1 := common.JoinBytes(prefix, []byte{0x01})
	key2 := common.JoinBytes(prefix, []byte{0x02})
	key3 := common.JoinBytes(prefix, []byte{0x03})

	common.FailIfErr(t, db.Put(key1, []byte{0xAA}))
	common.FailIfErr(t, db.Put(key2, []byte{0xBB}))
	common.FailIfErr(t, db.Put(key3, []byte{0xCC}))
	common.FailIfErr(t, db.Delete(key2))

	iterator := db.NewIterator(prefix)
	defer iterator.Release()

	// Start from the last entry and walk backwards
	if !iterator.Last() {
		t.Fatal("Last() returned false, expected true")
	}
	if len(iterator.Value()) == 0 {
		t.Fatal("Last() returned a tombstone value")
	}
	common.ExpectBytes(t, iterator.Key(), "0x0103")

	if !iterator.Prev() {
		t.Fatal("Prev() returned false, expected true")
	}
	if len(iterator.Value()) == 0 {
		t.Fatal("Prev() returned a tombstone value")
	}
	common.ExpectBytes(t, iterator.Key(), "0x0101")

	if iterator.Prev() {
		t.Fatal("Prev() returned true after the first entry, expected false")
	}
}

// TestEnableDeleteIteratorLastSkipsTombstones verifies that Last() skips
// tombstones when the final entry is tombstoned.
func TestEnableDeleteIteratorLastSkipsTombstones(t *testing.T) {
	db := NewMemDB()

	prefix := []byte{1}
	key1 := common.JoinBytes(prefix, []byte{0x01})
	key2 := common.JoinBytes(prefix, []byte{0x02})

	common.FailIfErr(t, db.Put(key1, []byte{0xAA}))
	common.FailIfErr(t, db.Put(key2, []byte{0xBB}))
	common.FailIfErr(t, db.Delete(key2))

	iterator := db.NewIterator(prefix)
	defer iterator.Release()

	// Last entry (key2) is a tombstone; Last() should skip to key1
	if !iterator.Last() {
		t.Fatal("Last() returned false, expected true")
	}
	if len(iterator.Value()) == 0 {
		t.Fatal("Last() returned a tombstone value")
	}
	common.ExpectBytes(t, iterator.Key(), "0x0101")
}

// TestEnableDeleteIteratorAllTombstones verifies that iterating a prefix
// where every entry is a tombstone returns no results and does not panic.
func TestEnableDeleteIteratorAllTombstones(t *testing.T) {
	db := NewMemDB()

	prefix := []byte{1}
	key1 := common.JoinBytes(prefix, []byte{0x01})
	key2 := common.JoinBytes(prefix, []byte{0x02})

	common.FailIfErr(t, db.Put(key1, []byte{0xAA}))
	common.FailIfErr(t, db.Put(key2, []byte{0xBB}))
	common.FailIfErr(t, db.Delete(key1))
	common.FailIfErr(t, db.Delete(key2))

	iterator := db.NewIterator(prefix)
	defer iterator.Release()

	count := 0
	for iterator.Next() {
		count++
	}
	common.FailIfErr(t, iterator.Error())

	if count != 0 {
		t.Fatalf("expected 0 keys (all tombstoned), got %d", count)
	}
}

// TestLevelDBManagerPopTombstoneIterator verifies that after a Pop on a
// LevelDB-backed manager, iterating the frontier skips tombstones left by
// the rollback. Before the fix, the iterator would return empty values for
// tombstoned keys, causing panics in collection readers.
func TestLevelDBManagerPopTombstoneIterator(t *testing.T) {
	dir := t.TempDir()
	m := mustNewLevelDBManager(t, dir)
	t.Cleanup(func() { common.FailIfErr(t, m.Stop()) })

	// Add two commits: k1 then k2
	common.FailIfErr(t, m.Add(keyedTransaction(m.Frontier(), "k1", "v1")))
	common.FailIfErr(t, m.Add(keyedTransaction(m.Frontier(), "k2", "v2")))

	// Pop the second commit; this writes a tombstone for k2
	common.FailIfErr(t, m.Pop())

	// Iterate the frontier: should see only k1, not the tombstoned k2
	iterator := m.Frontier().NewIterator([]byte("k"))
	defer iterator.Release()

	var keys []string
	for iterator.Next() {
		val := iterator.Value()
		if len(val) == 0 {
			t.Fatalf("iterator returned empty value for key %q (tombstone not skipped)", iterator.Key())
		}
		keys = append(keys, string(iterator.Key()))
	}
	common.FailIfErr(t, iterator.Error())

	if len(keys) != 1 {
		t.Fatalf("expected 1 key after Pop, got %d: %v", len(keys), keys)
	}
	common.Expect(t, keys[0], "k1")
}

// TestLevelDBManagerPopTombstoneIteratorReopen verifies that tombstone
// skipping works after stopping and reopening the manager.
func TestLevelDBManagerPopTombstoneIteratorReopen(t *testing.T) {
	dir := t.TempDir()

	m := mustNewLevelDBManager(t, dir)
	common.FailIfErr(t, m.Add(keyedTransaction(m.Frontier(), "k1", "v1")))
	common.FailIfErr(t, m.Add(keyedTransaction(m.Frontier(), "k2", "v2")))
	common.FailIfErr(t, m.Pop())
	common.FailIfErr(t, m.Stop())

	// Reopen and iterate
	m2 := mustNewLevelDBManager(t, dir)
	t.Cleanup(func() { common.FailIfErr(t, m2.Stop()) })

	iterator := m2.Frontier().NewIterator([]byte("k"))
	defer iterator.Release()

	var keys []string
	for iterator.Next() {
		val := iterator.Value()
		if len(val) == 0 {
			t.Fatalf("iterator returned empty value for key %q (tombstone not skipped after reopen)", iterator.Key())
		}
		keys = append(keys, string(iterator.Key()))
	}
	common.FailIfErr(t, iterator.Error())

	if len(keys) != 1 {
		t.Fatalf("expected 1 key after Pop and reopen, got %d: %v", len(keys), keys)
	}
	common.Expect(t, keys[0], "k1")
}
