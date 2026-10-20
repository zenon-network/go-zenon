package db

import (
	"testing"

	"github.com/zenon-network/go-zenon/common/types"
)

// TestGetFrontierIdentifierCorrupted verifies that GetFrontierIdentifier
// panics on corrupted data, but only after logging the key, store and error.
// The panic is the intended fail-fast behaviour; the test pins that it still
// happens.
func TestGetFrontierIdentifierCorrupted(t *testing.T) {
	mem := NewMemDB()
	// Write garbage to the frontier identifier key.
	if err := mem.Put(getFrontierIdentifierKey(), []byte{0xff, 0xfe, 0xfd}); err != nil {
		t.Fatal(err)
	}

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic on corrupted frontier identifier, got none")
		}
	}()
	GetFrontierIdentifier(mem)
}

// TestGetFrontierIdentifierNotFound verifies that GetFrontierIdentifier
// returns ZeroHashHeight when the key is absent.
func TestGetFrontierIdentifierNotFound(t *testing.T) {
	mem := NewMemDB()
	hh := GetFrontierIdentifier(mem)
	if hh != types.ZeroHashHeight {
		t.Fatalf("expected ZeroHashHeight for missing key, got %v", hh)
	}
}
