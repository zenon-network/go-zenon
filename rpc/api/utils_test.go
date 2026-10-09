package api

import (
	"math"
	"testing"
)

func TestGetRangeLargeIndexDoesNotWrap(t *testing.T) {
	// index*count would wrap around uint32 into a small, valid-looking offset
	start, end := GetRange(math.MaxUint32/2+1, 4, 10)
	if start != 10 || end != 10 {
		t.Fatalf("expected empty range past the end, got [%d, %d)", start, end)
	}
	if start, end := GetRange(2, 4, 10); start != 8 || end != 10 {
		t.Fatalf("expected [8, 10), got [%d, %d)", start, end)
	}
	if start, end := GetRange(3, 4, 10); start != 10 || end != 10 {
		t.Fatalf("expected empty range, got [%d, %d)", start, end)
	}
}
