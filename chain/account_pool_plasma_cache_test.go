package chain

import (
	"testing"

	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/chain/store"
	"github.com/zenon-network/go-zenon/common/db"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/dp"
	"github.com/zenon-network/go-zenon/vm/embedded/definition"
)

// scriptedStable is a Stable whose frontier momentum the test moves between
// events, standing in for the momentum pool during an insert burst or a
// rollback burst. Its momentum store answers only what computeDynamicPlasma
// asks: the dynamic-plasma spork is active, the frontier is whatever the
// test last set, and the plasma variables are the defaults.
type scriptedStable struct {
	frontier *nom.Momentum
}

func (s *scriptedStable) GetStableAccountDB(types.Address) db.DB { return db.NewMemDB() }
func (s *scriptedStable) GetFrontierMomentumStore() store.Momentum {
	return &scriptedMomentumStore{frontier: s.frontier}
}

type scriptedMomentumStore struct {
	store.Momentum
	frontier *nom.Momentum
}

func (m *scriptedMomentumStore) IsSporkActive(*types.ImplementedSpork) (bool, error) {
	return true, nil
}
func (m *scriptedMomentumStore) GetFrontierMomentum() (*nom.Momentum, error) { return m.frontier, nil }
func (m *scriptedMomentumStore) GetPlasmaVariables() (*definition.PlasmaVariables, error) {
	return &definition.PlasmaVariables{
		MaxBasePlasmaInMomentum: definition.DefaultMaxBasePlasmaInMomentum,
		FusedPlasmaTarget:       definition.DefaultFusedPlasmaTarget,
		PowPlasmaTarget:         definition.DefaultPowPlasmaTarget,
		MaxPriceChangePercent:   definition.DefaultMaxPriceChangePercent,
		PriceChangeDenominator:  definition.DefaultPriceChangeDenominator,
	}, nil
}

// pricedMomentum builds a frontier with independent fusion and work prices,
// so that a cache that kept the old value of either one is detected.
func pricedMomentum(height, fusionPrice, workPrice uint64) *nom.Momentum {
	return &nom.Momentum{Version: nom.DynamicPlasmaMomentumVersion, Height: height, NextFusionPrice: fusionPrice, NextWorkPrice: workPrice}
}

// pricingMatchesFrontier reports whether the pool's cached pricing context
// gives the same verdicts as a context computed from the frontier now.
// ValidPrice and HigherPrice are the observables, since the context does not
// expose its momentum. The fused-only probes sit at the floor of each fusion
// price the test uses; for them the work price cancels out of ValidPrice, so
// PoW-only probes at the floor of each work price cover that side, and a
// mixed pair whose HigherPrice ordering depends on the ratio of the two
// prices covers the comparator.
func pricingMatchesFrontier(t *testing.T, ap *accountPool, frontier *nom.Momentum) bool {
	t.Helper()
	ap.plasmaMu.Lock()
	cached := ap.plasma
	ap.plasmaMu.Unlock()
	if cached == nil {
		t.Fatalf("pricing context is empty with the spork active and frontier %d", frontier.Height)
	}
	fresh := dp.NewDynamicPlasma(frontier, cached.Config())
	// A user address: the first byte is not the embedded-contract marker,
	// for which ValidPrice is always true.
	user := types.Address{0x00, 0x25}
	for _, fused := range []uint64{21000, 22050, 23100, 24150} {
		probe := &nom.AccountBlock{Address: user, BasePlasma: 21000, FusedPlasma: fused}
		if cached.ValidPrice(probe) != fresh.ValidPrice(probe) {
			return false
		}
	}
	for _, workPlasma := range []uint64{21000, 25200, 29400, 33600} {
		difficulty, err := dp.GetDifficultyForPlasma(workPlasma)
		if err != nil {
			t.Fatal(err)
		}
		probe := &nom.AccountBlock{Address: user, BasePlasma: 21000, Difficulty: difficulty}
		if cached.ValidPrice(probe) != fresh.ValidPrice(probe) {
			return false
		}
	}
	fusedOnly := &nom.AccountBlock{Address: user, BasePlasma: 21000, FusedPlasma: 25200}
	powDifficulty, err := dp.GetDifficultyForPlasma(29400) // beats 25,200 fused only when work is priced well above fusion
	if err != nil {
		t.Fatal(err)
	}
	powOnly := &nom.AccountBlock{Address: user, BasePlasma: 21000, Difficulty: powDifficulty}
	if cached.HigherPrice(fusedOnly, powOnly) != fresh.HigherPrice(fusedOnly, powOnly) {
		return false
	}
	return true
}

// TestAccountPool_PricingContextTracksFrontierAcrossBursts pins the bound the
// pool's pricing cache gives: after every momentum event the cache equals a
// context computed from the frontier the momentum pool has at that moment.
// The momentum pool releases its lock before notifying listeners and the
// account pool reads the frontier inside the listener, so during an insert
// burst or a rollback burst the cache is exact at every event boundary; it
// can be stale only between an event and the next listener call, a window no
// production reader observes because every reader and every momentum change
// run under the chain insert lock.
func TestAccountPool_PricingContextTracksFrontierAcrossBursts(t *testing.T) {
	stable := &scriptedStable{frontier: pricedMomentum(10, 1000, 1000)}
	ap := newAccountPool(stable)
	ap.refreshDynamicPlasma() // as chain Init does
	if !pricingMatchesFrontier(t, ap, stable.frontier) {
		t.Fatalf("after priming, the cache does not match the frontier")
	}

	// Insert burst: the frontier advances three times, moving the fusion
	// price and the work price independently, and the listener fires after
	// each advance.
	for _, m := range []*nom.Momentum{pricedMomentum(11, 1050, 1000), pricedMomentum(12, 1100, 1200), pricedMomentum(13, 1150, 1600)} {
		stable.frontier = m
		ap.InsertMomentum(&nom.DetailedMomentum{Momentum: m})
		if !pricingMatchesFrontier(t, ap, m) {
			t.Fatalf("after inserting momentum %d, the cache does not match the frontier", m.Height)
		}
	}

	// Rollback burst: the frontier retreats to 10 one pop at a time, and the
	// listener fires after each pop. The deleted momentum is the one above
	// the new frontier, as RollbackTo passes it.
	deleted := []*nom.Momentum{pricedMomentum(13, 1150, 1600), pricedMomentum(12, 1100, 1200), pricedMomentum(11, 1050, 1000)}
	after := []*nom.Momentum{pricedMomentum(12, 1100, 1200), pricedMomentum(11, 1050, 1000), pricedMomentum(10, 1000, 1000)}
	for i := range deleted {
		stable.frontier = after[i]
		ap.DeleteMomentum(&nom.DetailedMomentum{Momentum: deleted[i]})
		if !pricingMatchesFrontier(t, ap, after[i]) {
			t.Fatalf("after deleting momentum %d, the cache does not match the frontier at %d", deleted[i].Height, after[i].Height)
		}
	}

	// A cache left over from an unrelated frontier is replaced by the first
	// event, whichever kind it is.
	ap.plasmaMu.Lock()
	ap.plasma = dp.NewDynamicPlasma(pricedMomentum(99, 1500, 1500), ap.plasma.Config())
	ap.plasmaMu.Unlock()
	if pricingMatchesFrontier(t, ap, stable.frontier) {
		t.Fatalf("test premise broken: the planted stale context matches the frontier")
	}
	ap.InsertMomentum(&nom.DetailedMomentum{Momentum: stable.frontier})
	if !pricingMatchesFrontier(t, ap, stable.frontier) {
		t.Fatalf("an insert event did not replace a stale context")
	}
}
