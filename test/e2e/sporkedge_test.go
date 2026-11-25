//go:build e2e

package e2e

import (
	"bytes"
	"strings"
	"testing"

	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/vm/embedded/definition"
)

// TestSporkEdge covers the acknowledgement-recency rule (issue #280,
// TEST-PLAN.md case S-7, edge class E7): once Dynamic Plasma is enforced, a
// user block must acknowledge a momentum at which every spork enforced at
// the frontier is already enforced. The rule compares sets, so the lowest
// acceptable acknowledgement height is the highest enforcement height on
// the chain.
func TestSporkEdge(t *testing.T) {
	group(t)
	if !live["dynamic-plasma"] {
		t.Skip("acknowledgement recency (#280) is enforced only once dynamic plasma is")
	}
	// enf is the highest enforcement height of a spork that is in force at
	// the frontier, and top the spork first enforced there whose id sorts
	// first: a block acknowledging one momentum below enf is refused, and
	// the refusal names top (the rule sorts the enforced set by id so every
	// node names the same spork). Sporks whose enforcement height is still
	// ahead (a staged run positioned below them) are not counted: the rule
	// compares against the sporks enforced at the frontier, and acking a
	// height that does not exist yet would fail for the wrong reason.
	enf := uint64(0)
	for name, h := range sporkHeights {
		if live[name] && h > enf {
			enf = h
		}
	}
	if enf == 0 {
		t.Skip("every spork is enforced from height 0; there is no momentum before them")
	}
	if enf < 2 {
		t.Skipf("the highest enforcement in force is %d; there is no momentum below it to acknowledge", enf)
	}
	var top types.Hash
	topName := ""
	for name, h := range sporkHeights {
		if !live[name] || h != enf {
			continue
		}
		id := sporkIDs[name]
		if top == types.ZeroHash || bytes.Compare(id.Bytes(), top.Bytes()) < 0 {
			top = id
			topName = sporkItem(t, name)
		}
	}

	// a is funded by hand and deliberately kept unreceived: the refused
	// block of S-7 must be the account's first block, because an
	// established account chain hits the older-than-previous rule before
	// the recency rule.
	a := net.Key(runBase + 120)
	b := newAccount(t, 121, 0, 0, 1500)
	funder := net.Key(IdxFunder)
	callOK(t, funder, types.PlasmaContract, definition.ABIPlasma.PackMethodPanic(definition.FuseMethodName, a.Address), qsr, coins(2000))
	funding := transfer(t, funder, a.Address, znn, coins(10), nil)
	if _, err := net.WaitConfirmed(funding.Hash, wait); err != nil {
		t.Fatal(err)
	}

	momentumHash := func(t *testing.T, height uint64) types.Hash {
		t.Helper()
		var list struct {
			List []struct {
				Hash   types.Hash `json:"hash"`
				Height uint64     `json:"height"`
			} `json:"list"`
		}
		get(t, &list, "ledger.getMomentumsByHeight", height, 1)
		if len(list.List) != 1 || list.List[0].Height != height {
			t.Fatalf("momentum at height %d: got %+v, want exactly that height", height, list.List)
		}
		return list.List[0].Hash
	}
	// acking builds a block that acknowledges the canonical momentum at the
	// given height, however far in the past that is.
	acking := func(height uint64) *Mutator {
		h := momentumHash(t, height)
		return &Mutator{Fields: func(blk *nom.AccountBlock) {
			blk.MomentumAcknowledged = types.HashHeight{Hash: h, Height: height}
		}}
	}

	t.Run("S-7_ack_below_the_highest_enforcement_refused", func(t *testing.T) {
		// The refusal wraps both sentinels: the verifier's ErrABMAStale
		// around the chain rule's ErrStaleMomentumAcknowledged.
		cover(t, "E7", "error", "chain.ErrStaleMomentumAcknowledged", "verifier.ErrABMAStale")
		cover(t, "E7", "verifier-rule", "accountBlockVerifier.acknowledgementRecency")
		cover(t, "E7", "spork", topName)
		err := tryTransfer(a, b.Address, znn, units(1), acking(enf-1))
		mustReject(t, err, "momentum-acknowledged")
		if msg := err.Error(); !strings.Contains(msg, top.String()) {
			t.Errorf("refusal %q does not name the spork first enforced at %d (%s)", msg, enf, top)
		}
	})

	t.Run("S-7b_fresh_ack_accepted_after_the_refusal", func(t *testing.T) {
		// The refused block consumed no account height: the same account
		// receives its funding with a fresh acknowledgement and the chain
		// advances.
		pending, err := net.Unreceived(a.Address)
		if err != nil || len(pending) != 1 {
			t.Fatalf("pending funding sends for %s: %v (%d)", a.Address, err, len(pending))
		}
		recv, err := net.Receive(a, pending[0].Hash, nil)
		if err != nil {
			t.Fatalf("fresh-ack receive after the refused stale ack: %v", err)
		}
		if _, err := net.WaitConfirmed(recv.Hash, wait); err != nil {
			t.Fatal(err)
		}
		wantBig(t, "ZNN funded", bal(t, a.Address, znn), coins(10))
	})

	// The acceptance edge "acknowledge the momentum at the enforcement
	// height" cannot be built on a chain that is already past it: a receive
	// must acknowledge a momentum at or above its from-block's confirmation
	// height, and any funding confirmed after the enforcement pins the
	// account to acknowledgements above it. That edge is exercised by a
	// driver run positioned at the activation instead (TEST-PLAN.md S-2);
	// the rule's acceptance side is pinned by the unit tests in
	// chain/acknowledgement_recency_test.go.
}
