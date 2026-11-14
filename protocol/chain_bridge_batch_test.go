package protocol_test

import (
	"strings"
	"testing"

	"github.com/pkg/errors"

	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/protocol"
	"github.com/zenon-network/go-zenon/protocol/downloader"
	"github.com/zenon-network/go-zenon/verifier"
	"github.com/zenon-network/go-zenon/zenon/mock"
)

// batchVerdicts are the downloader's sentinels for a verdict on a batch.
var batchVerdicts = []error{downloader.ErrBatchRejected, downloader.ErrBatchMisplaced, downloader.ErrBatchInconclusive}

// expectBatchVerdict asserts err is a verdict on the batch: typed as such,
// carrying the text of what was noticed, recognised by the downloader as
// verdict and as no other kind, and neither a consensus rejection nor a
// reason to quarantine anybody.
func expectBatchVerdict(t *testing.T, bridge protocol.ChainBridge, err error, text string, verdict error) {
	t.Helper()
	var batch *protocol.BatchRejected
	if !errors.As(err, &batch) {
		t.Fatalf("expected a BatchRejected, got %v", err)
	}
	for _, kind := range batchVerdicts {
		if errors.Is(err, kind) != (kind == verdict) {
			t.Fatalf("expected the refusal to match %q and no other kind; it carries %q", verdict, batch.Verdict)
		}
	}
	if !strings.Contains(err.Error(), text) {
		t.Fatalf("expected the refusal to say %q, got %v", text, err)
	}
	var rejected *protocol.SideChainRejected
	if errors.As(err, &rejected) {
		t.Fatalf("a batch verdict is not a verdict on a candidate: %v", err)
	}
	expectQuarantinedBy(t, bridge, err, false)
}

// expectNotItsHash asserts err blames a candidate for not being the
// momentum its hash names, as an ordinary import failure: not a verdict on
// the batch, not a consensus rejection, nobody quarantined.
func expectNotItsHash(t *testing.T, bridge protocol.ChainBridge, err error) {
	t.Helper()
	if !errors.Is(err, verifier.ErrMHashInvalid) {
		t.Fatalf("expected the candidate to be refused for its hash, got %v", err)
	}
	for _, kind := range batchVerdicts {
		if errors.Is(err, kind) {
			t.Fatalf("a candidate that is not its hash is its deliverer's doing, not a batch verdict: %v", err)
		}
	}
	var rejected *protocol.SideChainRejected
	if errors.As(err, &rejected) {
		t.Fatalf("a batch refused for its shape must not be typed as a consensus rejection: %v", err)
	}
	expectQuarantinedBy(t, bridge, err, false)
}

// altered returns a copy of detailed whose momentum went through change and
// still carries the original hash: what a peer can answer a request for
// that hash with, since the hash on the wire is not recomputed on receipt.
func altered(detailed *nom.DetailedMomentum, change func(*nom.Momentum)) *nom.DetailedMomentum {
	momentum := *detailed.Momentum
	change(&momentum)
	return &nom.DetailedMomentum{Momentum: &momentum, AccountBlocks: detailed.AccountBlocks}
}

func otherPrevious(momentum *nom.Momentum) {
	momentum.PreviousHash = types.NewHash([]byte("not the previous momentum"))
}

func otherData(momentum *nom.Momentum) {
	momentum.Data = []byte("not the data this momentum was hashed with")
}

// A batch whose candidates do not chain is refused before anything is
// validated. The index it is refused with is the index the downloader drops
// the deliverer of, so it must name the candidate that is at fault when
// there is one, and must say so when there is none.
func TestInsertChain_BatchThatDoesNotChainIsBlamedOnWhatIsAtFault(t *testing.T) {
	type fixture struct {
		batch     []*nom.DetailedMomentum
		wantIndex int
		// batchVerdict: every candidate is what its hash names, so the
		// refusal is a verdict on the batch.
		batchVerdict bool
	}
	cases := map[string]func(t *testing.T, z mock.MockZenon, genuine []*nom.DetailedMomentum) fixture{
		// The case of the bug: the candidate that does not link is the
		// genuine one, because the one before it is not the momentum the
		// genuine one extends.
		"bytes that are not their hash, before a genuine momentum": func(t *testing.T, z mock.MockZenon, genuine []*nom.DetailedMomentum) fixture {
			garbage := unsignedMomentum(momentumAt(t, z.Chain(), 6), 7)
			garbage.Hash = types.NewHash([]byte("a hash no momentum has"))
			return fixture{batch: []*nom.DetailedMomentum{detailedOf(garbage)[0], genuine[1]}, wantIndex: 0}
		},
		"bytes that are not their hash, after a genuine momentum": func(t *testing.T, z mock.MockZenon, genuine []*nom.DetailedMomentum) fixture {
			return fixture{batch: []*nom.DetailedMomentum{genuine[0], altered(genuine[1], otherPrevious)}, wantIndex: 1}
		},
		"two momentums that are each their hash": func(t *testing.T, z mock.MockZenon, genuine []*nom.DetailedMomentum) fixture {
			own := attackerMomentum(momentumAt(t, z.Chain(), 6), 7)
			return fixture{batch: []*nom.DetailedMomentum{detailedOf(own)[0], genuine[1]}, wantIndex: 1, batchVerdict: true}
		},
	}
	for name, build := range cases {
		t.Run(name, func(t *testing.T) {
			z := mock.NewMockZenon(t)
			defer z.StopPanic()
			z.InsertMomentumsTo(6)
			genuine := buildFork(t, z, 6, 8, 1)
			f := build(t, z, genuine)
			counting, bridge := newSideChainBridge(z)
			before := snapshotChain(t, z.Chain())

			index, err := bridge.InsertChain(f.batch)
			if err == nil {
				t.Fatal("expected the batch to be refused")
			}
			common.Expect(t, index, f.wantIndex)
			if f.batchVerdict {
				expectBatchVerdict(t, bridge, err, "do not chain", downloader.ErrBatchRejected)
			} else {
				expectNotItsHash(t, bridge, err)
			}
			common.Expect(t, counting.rollbacks, 0)
			expectChainEquals(t, z.Chain(), before)

			// Nothing was remembered: the genuine momentums still insert.
			_, err = bridge.InsertChain(genuine)
			common.FailIfErr(t, err)
			expectFrontierAt(t, z.Chain(), genuine[1].Momentum)
		})
	}
}

// A batch refused for how it sits against the local chain (it does not
// attach, attaches too deep, or is not longer) is refused with the index of
// its first momentum the node does not have, never with the index of a
// momentum the node already has, and as a verdict on the batch when every
// candidate is what its hash names. Where it attaches and how deep depend
// on the local chain as well as on its hashes, so those refusals are
// reported as misplaced; how long it is depends on how much of the chain
// was handed over, so that one is inconclusive.
func TestInsertChain_ShapeRefusalIsAVerdictOnTheBatch(t *testing.T) {
	type fixture struct {
		batch     []*nom.DetailedMomentum
		wantIndex int
		text      string
		verdict   error
	}
	cases := map[string]struct {
		height uint64
		build  func(t *testing.T, z mock.MockZenon) fixture
	}{
		"not longer, behind a momentum the node has": {height: 6, build: func(t *testing.T, z mock.MockZenon) fixture {
			known := prefetchAbove(t, z, 4, 5)
			competing := buildFork(t, z, 5, 6, 2)
			return fixture{batch: append(known, competing...), wantIndex: 1, text: "not longer", verdict: downloader.ErrBatchInconclusive}
		}},
		"too deep, behind momentums the node has": {height: 40, build: func(t *testing.T, z mock.MockZenon) fixture {
			known := prefetchAbove(t, z, 3, 5)
			fork := buildFork(t, z, 5, 41, 2)
			return fixture{batch: append(known, fork...), wantIndex: 2, text: "Too far", verdict: downloader.ErrBatchMisplaced}
		}},
		"head extends a momentum the node does not have at that height": {height: 6, build: func(t *testing.T, z mock.MockZenon) fixture {
			fork := buildFork(t, z, 4, 6, 2)
			return fixture{batch: fork[1:], wantIndex: 0, text: "can't link", verdict: downloader.ErrBatchMisplaced}
		}},
		"head is beyond the frontier": {height: 6, build: func(t *testing.T, z mock.MockZenon) fixture {
			genuine := buildFork(t, z, 6, 8, 1)
			return fixture{batch: genuine[1:], wantIndex: 0, text: "can't link", verdict: downloader.ErrBatchMisplaced}
		}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			z := mock.NewMockZenon(t)
			defer z.StopPanic()
			z.InsertMomentumsTo(c.height)
			f := c.build(t, z)
			counting, bridge := newSideChainBridge(z)
			before := snapshotChain(t, z.Chain())

			index, err := bridge.InsertChain(f.batch)
			if err == nil {
				t.Fatal("expected the batch to be refused")
			}
			common.Expect(t, index, f.wantIndex)
			expectBatchVerdict(t, bridge, err, f.text, f.verdict)
			common.Expect(t, counting.rollbacks, 0)
			expectChainEquals(t, z.Chain(), before)
		})
	}
}

// The same refusals, when one candidate is not the momentum its hash names:
// the refusal is that candidate's, wherever in the batch it sits, including
// under the hash of a momentum the node already has, and it is not a
// verdict on the batch.
func TestInsertChain_ShapeRefusalIsBlamedOnACandidateThatIsNotItsHash(t *testing.T) {
	type fixture struct {
		batch     []*nom.DetailedMomentum
		wantIndex int
	}
	cases := map[string]func(t *testing.T, z mock.MockZenon) fixture{
		"not longer, the head is not its hash": func(t *testing.T, z mock.MockZenon) fixture {
			known := prefetchAbove(t, z, 4, 5)
			competing := buildFork(t, z, 5, 6, 2)
			return fixture{batch: []*nom.DetailedMomentum{known[0], altered(competing[0], otherData)}, wantIndex: 1}
		},
		"not longer, the momentum under a hash the node has is not its hash": func(t *testing.T, z mock.MockZenon) fixture {
			known := prefetchAbove(t, z, 4, 5)
			competing := buildFork(t, z, 5, 6, 2)
			return fixture{batch: []*nom.DetailedMomentum{altered(known[0], otherData), competing[0]}, wantIndex: 0}
		},
		"does not link, because the head's previous hash is not the one it was hashed with": func(t *testing.T, z mock.MockZenon) fixture {
			genuine := buildFork(t, z, 6, 8, 1)
			return fixture{batch: []*nom.DetailedMomentum{altered(genuine[0], otherPrevious), genuine[1]}, wantIndex: 0}
		},
	}
	for name, build := range cases {
		t.Run(name, func(t *testing.T) {
			z := mock.NewMockZenon(t)
			defer z.StopPanic()
			z.InsertMomentumsTo(6)
			f := build(t, z)
			counting, bridge := newSideChainBridge(z)
			before := snapshotChain(t, z.Chain())

			index, err := bridge.InsertChain(f.batch)
			if err == nil {
				t.Fatal("expected the batch to be refused")
			}
			common.Expect(t, index, f.wantIndex)
			expectNotItsHash(t, bridge, err)
			common.Expect(t, counting.rollbacks, 0)
			expectChainEquals(t, z.Chain(), before)
		})
	}
}
