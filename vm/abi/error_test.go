package abi

import (
	"fmt"
	"testing"
)

// The offset/length arguments were swapped, and the buffer itself was
// formatted with %d instead of its length.
func TestErrorMessagesRenderSaneValues(t *testing.T) {
	output := make([]byte, 8)

	got := errArrayOffsetOverflow(output, 32, 2).Error()
	want := fmt.Sprintf("abi: cannot marshal in to go array: offset %d would go over slice boundary (len=%d)", 32+WordSize*2, len(output))
	if got != want {
		t.Fatalf("errArrayOffsetOverflow:\n got  %q\n want %q", got, want)
	}

	got = errInsufficientLength(output, 4).Error()
	want = fmt.Sprintf("abi: cannot marshal in to go type: length insufficient %d require %d", len(output), 4+WordSize)
	if got != want {
		t.Fatalf("errInsufficientLength:\n got  %q\n want %q", got, want)
	}
}
