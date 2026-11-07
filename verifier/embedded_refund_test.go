package verifier

import (
	"math/big"
	"math/bits"
	"testing"

	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/chain/store"
	"github.com/zenon-network/go-zenon/common/types"
)

// sporkCache is a cache store at one height that only answers
// IsSporkActive. The embedded interface is nil, so any other call panics.
type sporkCache struct {
	store.Cache
	inForce bool
}

func (s *sporkCache) IsSporkActive(*types.ImplementedSpork) (bool, error) {
	return s.inForce, nil
}

// refundHistory returns cache stores in which the refund spork is in force
// from inForceAt on (never when inForceAt is 0), and counts the lookups.
func refundHistory(inForceAt uint64, lookups *int) CacheStoreAt {
	return func(height uint64) (store.Cache, error) {
		*lookups++
		return &sporkCache{inForce: inForceAt != 0 && height >= inForceAt}, nil
	}
}

func TestEmbeddedRefundRecoveryHeight(t *testing.T) {
	burn := &nom.AccountBlock{Address: types.BridgeContract, ToAddress: types.TokenContract, Amount: big.NewInt(1)}
	user := &nom.AccountBlock{Address: types.ParseAddressPanic("z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz"), ToAddress: types.PtlcContract, Amount: big.NewInt(1)}
	zero := &nom.AccountBlock{Address: types.BridgeContract, ToAddress: types.TokenContract, Amount: big.NewInt(0)}

	cases := []struct {
		name                string
		frontier, inForceAt uint64
		send                *nom.AccountBlock
		confirmation        uint64
		height              uint64
		ok                  bool
		maxLookups          int
	}{
		{"not in force", 20, 0, burn, 5, 0, false, 1},
		{"not yet in force at the frontier", 20, 25, burn, 5, 0, false, 1},
		{"stalled before it is in force", 20, 12, burn, 5, 12, true, 2 + 4},
		{"in force only from the frontier", 20, 20, burn, 5, 20, true, 2 + 4},
		{"activation receive confirmed late", 20, 15, burn, 13, 15, true, 2 + 3},
		{"confirmed once in force", 20, 12, burn, 12, 0, false, 2},
		{"confirmed long after", 1000000, 12, burn, 999999, 0, false, 2},
		{"confirmed at the frontier", 20, 12, burn, 20, 0, false, 0},
		{"user send keeps its momentum", 20, 12, user, 5, 0, false, 0},
		{"zero amount keeps its momentum", 20, 12, zero, 5, 0, false, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			lookups := 0
			height, ok, err := EmbeddedRefundRecoveryHeight(c.frontier, refundHistory(c.inForceAt, &lookups), c.send, c.confirmation)
			if err != nil {
				t.Fatal(err)
			}
			if height != c.height || ok != c.ok {
				t.Fatalf("got (%d, %v), want (%d, %v)", height, ok, c.height, c.ok)
			}
			if lookups > c.maxLookups {
				t.Fatalf("%d cache-store lookups, want at most %d", lookups, c.maxLookups)
			}
		})
	}
}

// A queue stalled for a long time recovers with a logarithmic number of
// lookups, not one per momentum since the stall.
func TestEmbeddedRefundRecoveryHeight_LongStallIsLogarithmic(t *testing.T) {
	burn := &nom.AccountBlock{Address: types.BridgeContract, ToAddress: types.TokenContract, Amount: big.NewInt(1)}
	const frontier, confirmation, inForceAt = 10000000, 100, 9000000
	lookups := 0
	height, ok, err := EmbeddedRefundRecoveryHeight(frontier, refundHistory(inForceAt, &lookups), burn, confirmation)
	if err != nil {
		t.Fatal(err)
	}
	if height != inForceAt || !ok {
		t.Fatalf("got (%d, %v), want (%d, true)", height, ok, inForceAt)
	}
	if limit := 2 + bits.Len64(frontier-confirmation); lookups > limit {
		t.Fatalf("%d cache-store lookups, want at most %d", lookups, limit)
	}
}
