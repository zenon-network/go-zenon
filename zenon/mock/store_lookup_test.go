package mock

import (
	"testing"

	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/types"
)

// Lookups keyed by a value the caller supplies report an unknown key as
// (nil, nil) rather than an error, so every caller has to handle the nil. These
// cover the two lookups whose keys originate outside the node.
func TestMomentumStore_UnknownHashLookup(t *testing.T) {
	z := NewMockZenon(t)
	defer z.StopPanic()

	z.InsertMomentumsTo(10)
	store := z.Chain().GetFrontierMomentumStore()

	unknown := types.NewHash([]byte("no momentum has this hash"))

	momentum, err := store.GetMomentumByHash(unknown)
	common.FailIfErr(t, err)
	if momentum != nil {
		t.Fatalf("expected no momentum for an unknown hash, got %v", momentum.Identifier())
	}

	momentums, err := store.GetMomentumsByHash(unknown, true, 10)
	common.FailIfErr(t, err)
	if len(momentums) != 0 {
		t.Fatalf("expected no momentums for an unknown hash, got %d", len(momentums))
	}
}

func TestMomentumStore_AbsentHeightLookup(t *testing.T) {
	z := NewMockZenon(t)
	defer z.StopPanic()

	z.InsertMomentumsTo(10)
	store := z.Chain().GetFrontierMomentumStore()

	frontier := store.Identifier().Height

	// Above the frontier, and the underflow value a caller reaches by
	// subtracting one from a height of zero.
	for _, height := range []uint64{frontier + 1, frontier + 1000, ^uint64(0)} {
		momentum, err := store.GetMomentumByHeight(height)
		common.FailIfErr(t, err)
		if momentum != nil {
			t.Fatalf("expected no momentum at height %d, got %v", height, momentum.Identifier())
		}
	}
}
