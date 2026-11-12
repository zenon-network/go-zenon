package verifier

import (
	"math/big"
	"math/bits"
	"testing"

	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/chain/store"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/vm/embedded/definition"
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
	burnData := definition.ABIToken.PackMethodPanic(definition.BurnMethodName)
	donateData := definition.ABICommon.PackMethodPanic(definition.DonateMethodName)
	userAddress := types.ParseAddressPanic("z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz")
	burn := &nom.AccountBlock{Address: types.BridgeContract, ToAddress: types.TokenContract, Amount: big.NewInt(1), Data: burnData}
	user := &nom.AccountBlock{Address: userAddress, ToAddress: types.PtlcContract, Amount: big.NewInt(1)}
	userBurn := &nom.AccountBlock{Address: userAddress, ToAddress: types.TokenContract, Amount: big.NewInt(1), Data: burnData}
	zero := &nom.AccountBlock{Address: types.BridgeContract, ToAddress: types.TokenContract, Amount: big.NewInt(0), Data: burnData}
	mintDonation := &nom.AccountBlock{Address: types.TokenContract, ToAddress: types.LiquidityContract, Amount: big.NewInt(1), Data: donateData}
	fundDonation := &nom.AccountBlock{Address: types.LiquidityContract, ToAddress: types.AcceleratorContract, Amount: big.NewInt(1), Data: donateData}
	refund := &nom.AccountBlock{Address: types.TokenContract, ToAddress: types.BridgeContract, Amount: big.NewInt(1)}
	notBurn := &nom.AccountBlock{Address: types.BridgeContract, ToAddress: types.TokenContract, Amount: big.NewInt(1), Data: donateData}

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
		{"user burn keeps its momentum", 20, 12, userBurn, 5, 0, false, 0},
		{"mint donation keeps its momentum", 20, 12, mintDonation, 5, 0, false, 0},
		{"fund donation keeps its momentum", 20, 12, fundDonation, 5, 0, false, 0},
		{"data-less refund keeps its momentum", 20, 12, refund, 5, 0, false, 0},
		{"other token method keeps its momentum", 20, 12, notBurn, 5, 0, false, 0},
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
	burn := &nom.AccountBlock{Address: types.BridgeContract, ToAddress: types.TokenContract, Amount: big.NewInt(1), Data: definition.ABIToken.PackMethodPanic(definition.BurnMethodName)}
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
