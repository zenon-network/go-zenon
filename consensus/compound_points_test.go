package consensus

import (
	"encoding/binary"
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/zenon-network/go-zenon/chain"
	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/chain/store"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/db"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/consensus/storage"
)

// The fixtures below drive the real periodPoints, compoundPoints and
// chainTicker code over a momentum store whose frontier can move
// backwards while a compound walk is in flight, which is what a
// rollback does to a concurrent epoch-point read.
//
// Schedule: genesis T0, period ticks of 10 s, epochs of 40 s
// (lowerMultiplier 4; epoch 1 = period ticks 4..7). Momentums m0..m5
// sit mid-tick at T0+5s, T0+15s, ..., T0+55s. The frontier starts at
// m5 (tick 5, epoch 1 in progress).

// walkTestStore moves its frontier from m5 (tick 5) to m2 (tick 2),
// below the start of epoch 1 (tick 4, T0+40s), at one of two moments
// of an epoch-1 read:
//
//   - moveOnFirstEndBlock: during the first GetMomentumBeforeTime, i.e.
//     after HasStarted(1) accepted the epoch and before the walk
//     resolved its end block, so no lower tick is ever merged;
//   - moveAfterEndBlockCalls N: on the first frontier read after N
//     GetMomentumBeforeTime calls. With N = 2 (epoch end block, then
//     the end block of lower tick 5) the move lands after tick 5 has
//     merged and before tick 4 is read.
type walkTestStore struct {
	store.Momentum
	momentums []*nom.Momentum
	frontier  int

	moveOnFirstEndBlock    bool
	moveAfterEndBlockCalls int
	endBlockCalls          int
	moved                  bool
}

func (s *walkTestStore) GetFrontierMomentum() (*nom.Momentum, error) {
	if s.moveAfterEndBlockCalls > 0 && !s.moved && s.endBlockCalls >= s.moveAfterEndBlockCalls {
		s.moved = true
		s.frontier = 2
	}
	cp := *s.momentums[s.frontier]
	return &cp, nil
}

func (s *walkTestStore) GetMomentumBeforeTime(t *time.Time) (*nom.Momentum, error) {
	s.endBlockCalls++
	if s.moveOnFirstEndBlock && !s.moved {
		s.moved = true
		s.frontier = 2
	}
	var best *nom.Momentum
	for i := 0; i <= s.frontier; i++ {
		if s.momentums[i].Timestamp.Before(*t) {
			best = s.momentums[i]
		}
	}
	if best == nil {
		return nil, nil
	}
	cp := *best
	return &cp, nil
}

type walkTestChain struct {
	chain.Chain
	store *walkTestStore
}

func (c *walkTestChain) GetFrontierMomentumStore() store.Momentum { return c.store }

func newWalkTestCompoundPoints(t *testing.T, st *walkTestStore) (PointsReader, []types.Hash) {
	t.Helper()
	genesis := time.Unix(1700000000, 0)
	periodTicker := common.NewTicker(genesis, 10*time.Second)
	epochTicker := common.NewTicker(genesis, 40*time.Second)

	momentums := make([]*nom.Momentum, 6)
	hashes := make([]types.Hash, 6)
	for k := 0; k < 6; k++ {
		binary.BigEndian.PutUint32(hashes[k][:4], uint32(k)+1)
		ts := genesis.Add(time.Duration(10*k+5) * time.Second)
		momentums[k] = &nom.Momentum{
			Hash:          hashes[k],
			Height:        uint64(k),
			Timestamp:     &ts,
			TimestampUnix: uint64(ts.Unix()),
		}
	}

	st.momentums = momentums
	st.frontier = 5
	ch := &walkTestChain{store: st}
	consensusDB := storage.NewConsensusDB(db.NewMemDB(), 16, 16)

	// A warm node: the period points of every finished tick are stored
	// and hash-chained (point k spans (H(k-1), H(k)]).
	for k := 0; k < 6; k++ {
		p := storage.NewEmptyPoint(hashes[k])
		if k > 0 {
			p.PrevHash = hashes[k-1]
		} else {
			p.PrevHash = types.ZeroHash
		}
		p.Pillars["pillar-a"] = &storage.ProducerDetail{ExpectedNum: 1, FactualNum: 1, Weight: big.NewInt(1)}
		p.TotalWeight = big.NewInt(1)
		if err := consensusDB.StorePointByHeight(storage.PrefixPeriodPoint, uint64(k), p); err != nil {
			t.Fatalf("store period point %d: %v", k, err)
		}
	}

	period := newPeriodPoints(nil, newChainTicker(ch, periodTicker), consensusDB)
	return newCompoundPoints(period, newChainTicker(ch, epochTicker), consensusDB, storage.PrefixEpochPoint), hashes
}

// On a stable frontier the epoch walk merges exactly the present lower
// ticks (4 and 5) and stops at the start of the range.
func TestCompoundPoints_WalkStopsAtRangeStartOnStableFrontier(t *testing.T) {
	compound, hashes := newWalkTestCompoundPoints(t, &walkTestStore{})

	// Finished epoch 0: all four lower ticks present, range (zero, H3].
	point, err := compound.GetPoint(0)
	if err != nil {
		t.Fatalf("GetPoint(0): %v", err)
	}
	if point == nil {
		t.Fatalf("GetPoint(0): expected a point for a finished epoch")
	}
	if d := point.Pillars["pillar-a"]; d == nil || d.ExpectedNum != 4 || d.FactualNum != 4 || d.Weight.Cmp(big.NewInt(1)) != 0 {
		t.Fatalf("epoch 0 point = %+v, want counts 4/4 and weight 1 (sum 4 over 4 ticks)", d)
	}
	if !point.PrevHash.IsZero() || point.EndHash != hashes[3] || point.TotalWeight.Cmp(big.NewInt(1)) != 0 {
		t.Fatalf("epoch 0 point range/total = (%v, %v] %v, want (zero, H3] and total 1", point.PrevHash, point.EndHash, point.TotalWeight)
	}

	// Epoch 1 in progress: ticks 4 and 5 present, 6 and 7 not started.
	point, err = compound.GetPoint(1)
	if err != nil {
		t.Fatalf("GetPoint(1): %v", err)
	}
	if point == nil {
		t.Fatalf("GetPoint(1): expected a point for an epoch in progress")
	}
	d := point.Pillars["pillar-a"]
	if d == nil {
		t.Fatalf("point is missing pillar-a")
	}
	if d.ExpectedNum != 2 || d.FactualNum != 2 {
		t.Fatalf("point counts = %v/%v, want 2/2 (ticks 4 and 5)", d.ExpectedNum, d.FactualNum)
	}
	if d.Weight.Cmp(big.NewInt(1)) != 0 || point.TotalWeight.Cmp(big.NewInt(1)) != 0 {
		t.Fatalf("weight = %v total = %v, want 1 and 1 (sum 2 over 2 present ticks)", d.Weight, point.TotalWeight)
	}
	if point.PrevHash != hashes[3] || point.EndHash != hashes[5] {
		t.Fatalf("epoch 1 point range = (%v, %v], want (H3, H5]", point.PrevHash, point.EndHash)
	}
}

// When the frontier moves below the start of the range while the walk
// is in flight, every lower tick of the range reads as not started.
// The walk must stop at the range start and report an error; it must
// not keep descending below the range (the index would wrap and the
// ticker's overflow guard would stop the process).
func TestCompoundPoints_FrontierMovedBelowRangeReturnsError(t *testing.T) {
	compound, _ := newWalkTestCompoundPoints(t, &walkTestStore{moveOnFirstEndBlock: true})
	assertWalkReturnsError(t, compound, "no lower points for tick 1")
}

// When the frontier moves below the range after a lower tick has
// already been merged (here after tick 5, before tick 4 is read), the
// walk must not report the range with its first ticks missing: a nil
// tick below a merged one is an error.
func TestCompoundPoints_FrontierMovedAfterFirstMergeReturnsError(t *testing.T) {
	compound, _ := newWalkTestCompoundPoints(t, &walkTestStore{moveAfterEndBlockCalls: 2})
	assertWalkReturnsError(t, compound, "lower tick 4 of tick 1 reads as not started after tick 5 was merged")
}

// assertWalkReturnsError reads epoch 1 and requires a nil point and an
// error whose message contains cause, without a panic and within a
// bounded time.
func assertWalkReturnsError(t *testing.T, compound PointsReader, cause string) {
	t.Helper()
	done := make(chan struct{})
	var point *storage.Point
	var err error
	go func() {
		defer close(done)
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("GetPoint(1) panicked: %v", r)
			}
		}()
		point, err = compound.GetPoint(1)
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatalf("GetPoint(1) did not return within 10s")
	}
	if err == nil {
		t.Fatalf("GetPoint(1) returned %v with no error; want an error: %s", point, cause)
	}
	if point != nil {
		t.Fatalf("GetPoint(1) returned a point %v alongside the error %v; want nil", point, err)
	}
	if !strings.Contains(err.Error(), cause) {
		t.Fatalf("GetPoint(1) error = %q, want it to contain %q", err.Error(), cause)
	}
	t.Logf("GetPoint(1) returned the expected error: %v", err)
}
