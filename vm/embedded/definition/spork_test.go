package definition

import (
	"testing"

	"github.com/syndtr/goleveldb/leveldb"
	"github.com/zenon-network/go-zenon/common/db"
	"github.com/zenon-network/go-zenon/common/types"
)

// A tombstone deletion marker (empty value) left by a genesis rollback
// must be skipped instead of panicking ParseSporkInfo, matching
// GetSporkInfoById's treatment of empty values as absent.
//
// The tombstone encoding is produced by enableDeleteDB.Delete(false),
// which writes Put(key, []byte{}) — the same encoding that
// AppendPatchToLevelDBBatch(..., false) writes during a Pop. Using
// Delete() through the wrapper pins the encoding rather than assuming
// it.
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
	// wrapper (fullDelete=false), which writes an empty-value tombstone
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
