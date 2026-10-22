package abi

import (
	"fmt"
	"math"
	"strconv"
	"testing"
)

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

// TestNewType_ArrayOverflow verifies that NewType returns an error instead
// of panicking when a fixed-array declaration's size times the element size
// overflows int (issue #197). The test value is derived from math.MaxInt so
// it exercises the overflow guard (not the Atoi guard) on both 32-bit and
// 64-bit architectures.
func TestNewType_ArrayOverflow(t *testing.T) {
	// On 64-bit: uint64[2^61] fits in int but 2^61 * 8 overflows MaxInt64.
	// On 32-bit: uint64[2^29] fits in int but 2^29 * 8 overflows MaxInt32.
	size := math.MaxInt/8 + 1
	_, err := NewType(fmt.Sprintf("uint64[%d]", size))
	if err == nil {
		t.Fatal("expected error for overflowing array size, got nil")
	}
}

// TestNewType_ArrayAtoiOverflow verifies that NewType returns an error for
// array sizes that overflow strconv.Atoi (issue #197).
func TestNewType_ArrayAtoiOverflow(t *testing.T) {
	_, err := NewType("uint64[99999999999999999999999999]")
	if err == nil {
		t.Fatal("expected error for Atoi-overflowing array size, got nil")
	}
}

// TestNewType_ArrayNormal verifies that normal fixed arrays still work.
func TestNewType_ArrayNormal(t *testing.T) {
	_, err := NewType("uint64[10]")
	if err != nil {
		t.Fatalf("unexpected error for normal array: %v", err)
	}
	_, err = NewType("address[5]")
	if err != nil {
		t.Fatalf("unexpected error for address array: %v", err)
	}
}

// TestNewType_ArrayBoundary verifies the exact overflow boundary on 64-bit:
// uint64[2^60-1] is accepted (size * 8 == MaxInt64), uint64[2^60] is rejected.
func TestNewType_ArrayBoundary(t *testing.T) {
	if strconv.IntSize != 64 {
		t.Skip("boundary values are 64-bit specific")
	}
	_, err := NewType("uint64[1152921504606846975]") // 2^60 - 1
	if err != nil {
		t.Fatalf("unexpected error at exact boundary: %v", err)
	}
	_, err = NewType("uint64[1152921504606846976]") // 2^60
	if err == nil {
		t.Fatal("expected error above the boundary, got nil")
	}
}
