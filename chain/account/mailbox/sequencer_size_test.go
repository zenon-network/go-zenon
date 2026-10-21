package mailbox

import (
	"strings"
	"testing"

	"github.com/zenon-network/go-zenon/common/db"
	"github.com/zenon-network/go-zenon/common/types"
)

// TestSequencerSizeNotFound verifies that SequencerSize returns (0, nil)
// when the counter has never been written.
func TestSequencerSizeNotFound(t *testing.T) {
	m := NewAccountMailbox(types.PillarContract, db.NewMemDB())
	size, err := m.SequencerSize()
	if err != nil {
		t.Fatalf("expected nil error for missing counter, got: %v", err)
	}
	if size != 0 {
		t.Fatalf("expected 0 for missing counter, got: %d", size)
	}
}

// TestSequencerSizeShortValue verifies that SequencerSize returns an error
// instead of panicking when the stored value is shorter than 8 bytes.
func TestSequencerSizeShortValue(t *testing.T) {
	mem := db.NewMemDB()
	m := NewAccountMailbox(types.PillarContract, mem)

	// Write a value that is too short for uint64 decoding.
	if err := mem.Put(sequencerNumInsertedKey, []byte{1, 2, 3}); err != nil {
		t.Fatal(err)
	}

	_, err := m.SequencerSize()
	if err == nil {
		t.Fatal("expected error for short stored value, got nil")
	}
	if !strings.Contains(err.Error(), "too short") {
		t.Fatalf("expected 'too short' in error, got: %v", err)
	}
}

// TestSequencerSizeNormal verifies that SequencerSize still works for a
// valid counter value.
func TestSequencerSizeNormal(t *testing.T) {
	mem := db.NewMemDB()
	m := NewAccountMailbox(types.PillarContract, mem).(*mailbox)

	// Push a header so the counter gets written.
	header := types.AccountHeader{
		Address: types.Address{1},
		HashHeight: types.HashHeight{
			Hash:   types.Hash{2},
			Height: 1,
		},
	}
	m.SequencerPushBack(header)

	size, err := m.SequencerSize()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if size != 1 {
		t.Fatalf("expected 1, got: %d", size)
	}
}
