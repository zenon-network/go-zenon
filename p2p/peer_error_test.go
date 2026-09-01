package p2p

import (
	"strings"
	"testing"
)

// DiscReason is decoded from the wire and is not constrained to the declared
// reasons, so String must be total over the whole range.
func TestDiscReasonString_OutOfRange(t *testing.T) {
	for d := 0; d < len(discReasonToString); d++ {
		if got := DiscReason(d).String(); got == "" || strings.HasPrefix(got, "Unknown Reason") {
			t.Errorf("DiscReason(%d) should name a declared reason, got %q", d, got)
		}
	}

	// The boundary value and beyond, including the values a wire-decoded
	// reason can take above the declared set.
	for _, d := range []int{len(discReasonToString), len(discReasonToString) + 1, 255, 1 << 16} {
		got := DiscReason(d).String()
		if !strings.HasPrefix(got, "Unknown Reason") {
			t.Errorf("DiscReason(%d) should be reported as unknown, got %q", d, got)
		}
	}
}
