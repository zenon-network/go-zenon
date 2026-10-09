package tests

import (
	"testing"

	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/zenon/mock"
)

// A height range that extends beyond the frontier is capped at the frontier
// rather than padded with nil entries.
func TestGetMomentumsByHeight_CappedAtFrontier(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()
	z.InsertNewMomentum()
	z.InsertNewMomentum()

	store := z.Chain().GetFrontierMomentumStore()
	frontier, err := store.GetFrontierMomentum()
	common.FailIfErr(t, err)

	momentums, err := store.GetMomentumsByHeight(frontier.Height-1, true, 10)
	common.FailIfErr(t, err)
	common.Expect(t, len(momentums), 2)
	for _, m := range momentums {
		if m == nil {
			t.Fatal("nil momentum in range")
		}
	}
	common.Expect(t, momentums[len(momentums)-1].Height, frontier.Height)

	momentums, err = store.GetMomentumsByHeight(frontier.Height+5, true, 10)
	common.FailIfErr(t, err)
	common.Expect(t, len(momentums), 0)
}
