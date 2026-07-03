package mailbox

import (
	"testing"

	"github.com/zenon-network/go-zenon/common/db"
	"github.com/zenon-network/go-zenon/common/types"
)

// atMost == 0 used to underflow to effectively unlimited because the counter
// was decremented after the first append.
func TestGetUnreceivedAccountBlockHashesAtMost(t *testing.T) {
	m := NewAccountMailbox(types.PillarContract, db.NewMemDB())
	for i := byte(1); i <= 3; i++ {
		if err := m.MarkAsUnreceived(types.NewHash([]byte{i})); err != nil {
			t.Fatal(err)
		}
	}

	for _, tc := range []struct {
		atMost uint64
		want   int
	}{{0, 0}, {1, 1}, {2, 2}, {10, 3}} {
		hashes, err := m.GetUnreceivedAccountBlockHashes(tc.atMost)
		if err != nil {
			t.Fatal(err)
		}
		if len(hashes) != tc.want {
			t.Fatalf("atMost=%d: got %d hashes, want %d", tc.atMost, len(hashes), tc.want)
		}
	}
}
