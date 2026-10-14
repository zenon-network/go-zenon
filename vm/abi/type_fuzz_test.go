package abi

import "testing"

// TestNewType_EmptyString verifies that NewType returns an error instead of
// panicking on an empty type string. Previously this caused an
// index-out-of-range panic at type.go:100 because
// typeRegex.FindAllStringSubmatch("", -1) returns an empty slice.
func TestNewType_EmptyString(t *testing.T) {
	_, err := NewType("")
	if err == nil {
		t.Fatal("expected error for empty type string, got nil")
	}
}

// TestNewType_Valid verifies that NewType still works for valid type strings.
func TestNewType_Valid(t *testing.T) {
	_, err := NewType("uint256")
	if err != nil {
		t.Fatalf("unexpected error for valid type: %v", err)
	}
}
