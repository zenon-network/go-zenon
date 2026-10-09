package definition

import "testing"

// The prefixes are baked into on-chain contract-storage keys; lock in the
// historical values (from the original shared iota block, where iota was 11).
func TestAcceleratorStoragePrefixValues(t *testing.T) {
	if projectKeyPrefix != 12 {
		t.Fatalf("projectKeyPrefix = %d, want 12", projectKeyPrefix)
	}
	if phaseKeyPrefix != 13 {
		t.Fatalf("phaseKeyPrefix = %d, want 13", phaseKeyPrefix)
	}
}
