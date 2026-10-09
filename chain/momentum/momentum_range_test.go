package momentum

import (
	"testing"

	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common/db"
)

// A height that is missing at or below the recorded frontier is a store hole
// and must surface as an error, not as a silently truncated result.
func TestGetMomentumsByHeight_HoleBelowFrontierIsAnError(t *testing.T) {
	memDB := db.NewMemDB()
	frontier := &nom.Momentum{Height: 3}
	frontier.Hash = frontier.ComputeHash()
	data, err := frontier.Serialize()
	if err != nil {
		t.Fatal(err)
	}
	// only height 3 is stored; heights 1 and 2 are absent below the frontier
	if err := db.SetFrontier(memDB, frontier.Identifier(), data); err != nil {
		t.Fatal(err)
	}
	store := &momentumStore{DB: memDB}

	if _, err := store.GetMomentumsByHeight(1, true, 3); err == nil {
		t.Fatal("expected an error for a missing momentum below the frontier")
	}
}
