package definition

import (
	"testing"

	"github.com/zenon-network/go-zenon/common/db"
	"github.com/zenon-network/go-zenon/common/types"
)

// A tombstone deletion marker (empty value) left by a genesis rollback
// must be skipped instead of panicking ParseSporkInfo, matching
// GetSporkInfoById's treatment of empty values as absent.
func TestGetAllSporksSkipsTombstones(t *testing.T) {
	context := db.NewMemDB()

	real := &Spork{
		Id:   types.NewHash([]byte{1}),
		Name: "test-spork",
	}
	if err := context.Put(real.Key(), real.Data()); err != nil {
		t.Fatal(err)
	}

	// Simulate a tombstone: the key exists but the value is empty.
	tombstoneId := types.NewHash([]byte{2})
	tombstone := &Spork{Id: tombstoneId}
	if err := context.Put(tombstone.Key(), []byte{}); err != nil {
		t.Fatal(err)
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
