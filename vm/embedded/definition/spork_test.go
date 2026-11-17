package definition

import (
	"testing"

	"github.com/syndtr/goleveldb/leveldb"
	"github.com/zenon-network/go-zenon/common/db"
	"github.com/zenon-network/go-zenon/common/types"
)

// A tombstone deletion marker (empty value) left by a genesis rollback
// must be skipped instead of panicking parseSporkInfo, matching
// GetSporkInfoById's treatment of empty values as absent.
//
// The tombstone encoding is produced by enableDeleteDB.Delete, which
// writes Put(key, []byte{}) — the same encoding Pop leaves behind when
// it applies the rollback patch through the delete-aware wrapper. Using
// Delete() through the wrapper pins the encoding rather than assuming
// it.
//
// Two layers skip such entries: the delete-aware iterator no longer
// yields a tombstone at all, and the reader ignores an empty value it is
// handed. The tombstone fixture exercises the first; it never reaches the
// reader. An entry stored with an empty value exercises the second: its
// existence marker survives the iterator's filter and Value() hands the
// reader empty data, which the reader guard alone turns into "absent".
func TestGetAllSporksSkipsTombstones(t *testing.T) {
	context := db.NewMemDB()

	real := &Spork{
		Id:   types.NewHash([]byte{1}),
		Name: "test-spork",
	}
	if err := context.Put(real.Key(), real.Data()); err != nil {
		t.Fatal(err)
	}

	// Insert a second spork, then delete it through the enableDelete
	// wrapper, which writes an empty-value tombstone
	// — the same encoding Pop leaves behind.
	tombstone := &Spork{
		Id:   types.NewHash([]byte{2}),
		Name: "rolled-back-spork",
	}
	if err := context.Put(tombstone.Key(), tombstone.Data()); err != nil {
		t.Fatal(err)
	}
	if err := context.Delete(tombstone.Key()); err != nil {
		t.Fatal(err)
	}

	// Verify the tombstone encoding: the key exists but reads as
	// not-found through the enableDelete wrapper (empty value → nil).
	rawVal, err := context.Get(tombstone.Key())
	if err != leveldb.ErrNotFound {
		t.Fatalf("expected tombstone key to read as not-found, got val=%v err=%v", rawVal, err)
	}

	// An entry stored with an empty value reaches the reader through the
	// iterator; the reader guard is what skips it.
	empty := &Spork{
		Id:   types.NewHash([]byte{3}),
		Name: "empty-value-spork",
	}
	if err := context.Put(empty.Key(), nil); err != nil {
		t.Fatal(err)
	}
	if data, err := context.Get(empty.Key()); err != nil || len(data) != 0 {
		t.Fatalf("expected the empty-value entry to read as present and empty, got val=%v err=%v", data, err)
	}

	sporks := GetAllSporks(context)
	if len(sporks) != 1 {
		t.Fatalf("expected 1 spork, got %d", len(sporks))
	}
	if sporks[0].Id != real.Id {
		t.Fatalf("expected spork id %v, got %v", real.Id, sporks[0].Id)
	}
	if sporks[0].Name != "test-spork" {
		t.Fatalf("expected spork name %q, got %q", "test-spork", sporks[0].Name)
	}
}
