package db

import "testing"

// TestNewPatchFromDump_Malformed verifies that NewPatchFromDump returns an
// error instead of panicking on malformed batch dumps. The input is an
// 11-byte leveldb batch with a crafted uvarint key length that triggers a
// uint64→int truncation in vendored goleveldb, producing negative slice
// bounds.
func TestNewPatchFromDump_Malformed(t *testing.T) {
	malformed := []byte{0x01, 0xd4, 0xc7, 0xd4, 0xd4, 0xd4, 0xff, 0xff, 0xfe, 0xed, 0x01}
	_, err := NewPatchFromDump(malformed)
	if err == nil {
		t.Fatal("expected error for malformed batch dump, got nil")
	}
}

// TestNewPatchFromDump_Valid verifies that NewPatchFromDump succeeds on
// a valid batch produced by Dump().
func TestNewPatchFromDump_Valid(t *testing.T) {
	p := NewPatch()
	dump := p.Dump()
	_, err := NewPatchFromDump(dump)
	if err != nil {
		t.Fatalf("unexpected error for valid batch: %v", err)
	}
}
