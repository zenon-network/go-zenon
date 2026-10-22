package account

import (
	"strings"
	"testing"

	"github.com/zenon-network/go-zenon/common/db"
	"github.com/zenon-network/go-zenon/common/types"
)

// TestSequencerFrontIndexNotFound verifies that sequencerFrontIndex returns
// (0, nil) when the counter has never been written.
func TestSequencerFrontIndexNotFound(t *testing.T) {
	address := types.ParseAddressPanic("z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz")
	as := NewAccountStore(address, db.NewMemDB()).(*accountStore)

	idx, err := as.sequencerFrontIndex()
	if err != nil {
		t.Fatalf("expected nil error for missing counter, got: %v", err)
	}
	if idx != 0 {
		t.Fatalf("expected 0 for missing counter, got: %d", idx)
	}
}

// TestSequencerFrontIndexShortValue verifies that sequencerFrontIndex returns
// an error instead of panicking when the stored value is shorter than 8 bytes.
func TestSequencerFrontIndexShortValue(t *testing.T) {
	address := types.ParseAddressPanic("z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz")
	memdb := db.NewMemDB()
	as := NewAccountStore(address, memdb).(*accountStore)

	// Write a value that is too short for uint64 decoding.
	if err := memdb.Put(sequencerLastReceivedKey, []byte{1, 2, 3}); err != nil {
		t.Fatal(err)
	}

	_, err := as.sequencerFrontIndex()
	if err == nil {
		t.Fatal("expected error for short stored value, got nil")
	}
	if !strings.Contains(err.Error(), "too short") {
		t.Fatalf("expected 'too short' in error, got: %v", err)
	}
}

// TestSequencerFrontIndexNormal verifies that sequencerFrontIndex still works
// for a valid counter value.
func TestSequencerFrontIndexNormal(t *testing.T) {
	address := types.ParseAddressPanic("z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz")
	memdb := db.NewMemDB()
	as := NewAccountStore(address, memdb).(*accountStore)

	// Write a valid counter value.
	if err := memdb.Put(sequencerLastReceivedKey, []byte{0, 0, 0, 0, 0, 0, 0, 42}); err != nil {
		t.Fatal(err)
	}

	idx, err := as.sequencerFrontIndex()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if idx != 42 {
		t.Fatalf("expected 42, got: %d", idx)
	}
}
