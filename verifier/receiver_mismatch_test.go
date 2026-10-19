package verifier

import "testing"

// The receiver rule is relaxed only for mainnet's own history: below the
// enforcement height on the mainnet chain identifier. Any other chain, and
// mainnet from the enforcement height on, must reject a receiver that is not
// the send's recipient.
func TestReceiverMismatchTolerated(t *testing.T) {
	cases := []struct {
		name            string
		chainIdentifier uint64
		frontierHeight  uint64
		want            bool
	}{
		{"mainnet below the height", mainnetChainIdentifier, ReceiverMismatchEnforcementHeight - 1, true},
		{"mainnet at the height", mainnetChainIdentifier, ReceiverMismatchEnforcementHeight, false},
		{"mainnet above the height", mainnetChainIdentifier, ReceiverMismatchEnforcementHeight + 1, false},
		{"mainnet at genesis", mainnetChainIdentifier, 0, true},
		{"other chain at genesis", 100, 0, false},
		{"other chain below the height", 100, ReceiverMismatchEnforcementHeight - 1, false},
		{"other chain above the height", 100, ReceiverMismatchEnforcementHeight + 1, false},
		{"zero chain identifier", 0, 0, false},
	}
	for _, c := range cases {
		if got := receiverMismatchTolerated(c.chainIdentifier, c.frontierHeight); got != c.want {
			t.Errorf("%s: receiverMismatchTolerated(%d, %d) = %v, want %v", c.name, c.chainIdentifier, c.frontierHeight, got, c.want)
		}
	}
}
