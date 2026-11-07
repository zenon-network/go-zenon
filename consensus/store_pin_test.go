package consensus

import (
	"crypto/ed25519"
	"fmt"
	"math/big"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/zenon-network/go-zenon/chain"
	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/chain/store"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/db"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/consensus/api"
	"github.com/zenon-network/go-zenon/consensus/storage"
)

// The fixtures below model a reorg racing a consensus read. Two
// branches, A and B, share momentums h1..h3 and then diverge: each holds
// its own h4..h7, produced by its own pillar. A read starts on one
// branch's store; a hook moves the live frontier to the other branch
// part-way through, which is what a rollback and re-extension does to
// a concurrent, unlocked read (the RPC consensus-cache refresh).
//
// Schedule: genesis h1 at T0; period ticks of 10 s; epochs of 40 s
// (period ticks 4..7 make epoch 1). Momentum hN (N >= 2) sits mid-tick
// at T0 + 10(N-1) + 5 s, in period tick N-1. The election of tick t is
// seeded by the proof momentum before the end of tick t-2, so ticks 5
// and later are elected from branch momentums and differ by branch.

var (
	pinTestGenesisTime = time.Unix(1700000000, 0)
	pinTestPeriod      = common.NewTicker(pinTestGenesisTime, 10*time.Second)
	pinTestEpoch       = common.NewTicker(pinTestGenesisTime, 40*time.Second)
)

type pinTestPillar struct {
	name   string
	key    ed25519.PublicKey
	weight int64
}

var (
	pinTestCommon  = pinTestPillar{name: "pillar-c", key: pinTestKey(0xc0), weight: 5}
	pinTestBranchA = pinTestPillar{name: "pillar-a", key: pinTestKey(0xa0), weight: 10}
	pinTestBranchB = pinTestPillar{name: "pillar-b", key: pinTestKey(0xb0), weight: 20}
	pinTestPillars = []pinTestPillar{pinTestCommon, pinTestBranchA, pinTestBranchB}
)

func pinTestKey(b byte) ed25519.PublicKey {
	key := make(ed25519.PublicKey, ed25519.PublicKeySize)
	key[0] = b
	return key
}

func pinTestPillarOf(producer types.Address) pinTestPillar {
	for _, p := range pinTestPillars {
		if types.PubKeyToAddress(p.key) == producer {
			return p
		}
	}
	panic(fmt.Sprintf("unknown producer %v", producer))
}

// pinTestMomentum builds momentum hN of a branch; hashes are unique per
// (branch, height).
func pinTestMomentum(branch byte, height uint64, producer pinTestPillar) *nom.Momentum {
	ts := pinTestGenesisTime
	if height > 1 {
		ts = pinTestGenesisTime.Add(time.Duration(10*(height-1)+5) * time.Second)
	}
	var hash types.Hash
	hash[0] = branch
	hash[1] = byte(height)
	return &nom.Momentum{
		Hash:          hash,
		Height:        height,
		Timestamp:     &ts,
		TimestampUnix: uint64(ts.Unix()),
		PublicKey:     producer.key,
	}
}

// pinTestStore is an immutable momentum store whose frontier is its
// last momentum, like a snapshot of the real frontier store.
type pinTestStore struct {
	store.Momentum
	momentums []*nom.Momentum // momentums[i] is height i+1
	// dropRange makes GetMomentumsByHeight return nothing, so the store
	// reports an end block whose range it then does not hold.
	dropRange bool
}

func pinTestCopy(m *nom.Momentum) *nom.Momentum {
	cp := *m
	return &cp
}

func (s *pinTestStore) GetFrontierMomentum() (*nom.Momentum, error) {
	return pinTestCopy(s.momentums[len(s.momentums)-1]), nil
}

func (s *pinTestStore) GetMomentumBeforeTime(t *time.Time) (*nom.Momentum, error) {
	var best *nom.Momentum
	for _, m := range s.momentums {
		if m.Timestamp.Before(*t) {
			best = m
		}
	}
	if best == nil {
		return nil, nil
	}
	return pinTestCopy(best), nil
}

func (s *pinTestStore) GetMomentumsByHeight(height uint64, higher bool, count uint64) ([]*nom.Momentum, error) {
	if !higher {
		panic("pinTestStore supports ascending reads only")
	}
	result := []*nom.Momentum{}
	if s.dropRange {
		return result, nil
	}
	for h := height; h < height+count && h <= uint64(len(s.momentums)); h++ {
		result = append(result, pinTestCopy(s.momentums[h-1]))
	}
	return result, nil
}

// pinTestBranch returns the store of a branch truncated at frontier
// height top: h1..h3 are common, h4.. belong to the branch.
func pinTestBranch(branch byte, producer pinTestPillar, top uint64) *pinTestStore {
	s := &pinTestStore{}
	for h := uint64(1); h <= top; h++ {
		var m *nom.Momentum
		if h <= 3 {
			m = pinTestMomentum(0, h, pinTestCommon)
		} else {
			m = pinTestMomentum(branch, h, producer)
		}
		if h > 1 {
			m.PreviousHash = s.momentums[h-2].Hash
		}
		s.momentums = append(s.momentums, m)
	}
	return s
}

type pinTestChain struct {
	chain.Chain
	live store.Momentum
}

func (c *pinTestChain) GetFrontierMomentumStore() store.Momentum { return c.live }
func (c *pinTestChain) GetGenesisMomentum() *nom.Momentum {
	return pinTestMomentum(0, 1, pinTestCommon)
}

// pinTestElection elects, for every tick, the pillar that produced the
// tick's proof momentum, with that pillar's weight as the only
// delegation, so an election computed on one branch is told apart from
// one computed on the other. afterElection, if set, runs once after
// the first election has been computed: the reorg lands there.
type pinTestElection struct {
	common.Ticker
	chain         *pinTestChain
	afterElection func()
}

func (e *pinTestElection) proofPillar(momentumStore store.Momentum, tick uint64) (pinTestPillar, error) {
	proofTime := pinTestGenesisTime.Add(time.Second)
	if tick >= 2 {
		_, proofTime = e.ToTime(tick - 2)
	}
	proof, err := getMomentumBeforeTime(momentumStore, proofTime)
	if err != nil {
		return pinTestPillar{}, err
	}
	return pinTestPillarOf(proof.Producer()), nil
}

func (e *pinTestElection) ElectionByTime(t time.Time) (*electionResult, error) {
	return e.ElectionByTick(e.ToTick(t))
}

func (e *pinTestElection) ElectionByTick(tick uint64) (*electionResult, error) {
	return e.electionByTickAt(e.chain.GetFrontierMomentumStore(), tick)
}

func (e *pinTestElection) electionByTickAt(momentumStore store.Momentum, tick uint64) (*electionResult, error) {
	pillar, err := e.proofPillar(momentumStore, tick)
	if err != nil {
		return nil, err
	}
	producer := types.PubKeyToAddress(pillar.key)
	result := &electionResult{
		Tick:      tick,
		Producers: []*ProducerEvent{{Producer: producer, Name: pillar.name}},
		Delegations: []*types.PillarDelegation{{
			Name:      pillar.name,
			Producing: producer,
			Weight:    big.NewInt(pillar.weight),
		}},
	}
	result.STime, result.ETime = e.ToTime(tick)
	if hook := e.afterElection; hook != nil {
		e.afterElection = nil
		hook()
	}
	return result, nil
}

func (e *pinTestElection) DelegationsByTick(tick uint64) ([]*types.PillarDelegationDetail, error) {
	return e.delegationsByTickAt(e.chain.GetFrontierMomentumStore(), tick)
}

func (e *pinTestElection) delegationsByTickAt(momentumStore store.Momentum, tick uint64) ([]*types.PillarDelegationDetail, error) {
	pillar, err := e.proofPillar(momentumStore, tick)
	if err != nil {
		return nil, err
	}
	producer := types.PubKeyToAddress(pillar.key)
	return []*types.PillarDelegationDetail{{
		PillarDelegation: types.PillarDelegation{
			Name:      pillar.name,
			Producing: producer,
			Weight:    big.NewInt(pillar.weight),
		},
		Backers: map[types.Address]*big.Int{producer: big.NewInt(pillar.weight)},
	}}, nil
}

type pinTestFixture struct {
	chain    *pinTestChain
	election *pinTestElection
	db       *storage.DB
	period   PointsReader
	epoch    PointsReader
}

func newPinTestFixture(live store.Momentum) *pinTestFixture {
	ch := &pinTestChain{live: live}
	election := &pinTestElection{Ticker: pinTestPeriod, chain: ch}
	consensusDB := storage.NewConsensusDB(db.NewMemDB(), 16, 16)
	period := newPeriodPoints(election, newChainTicker(ch, pinTestPeriod), consensusDB)
	epoch := newCompoundPoints(period, newChainTicker(ch, pinTestEpoch), consensusDB, storage.PrefixEpochPoint)
	return &pinTestFixture{chain: ch, election: election, db: consensusDB, period: period, epoch: epoch}
}

// reader returns a pillar reader fixed at momentumStore, as
// FixedPillarReader builds it, over the fixture's shared points.
func (f *pinTestFixture) reader(momentumStore store.Momentum) *API {
	return &API{
		momentumStore: momentumStore,
		er:            f.election,
		points:        &points{periodPoints: f.period, epochPoints: f.epoch},
	}
}

func describePoint(p *storage.Point) string {
	if p == nil {
		return "<nil>"
	}
	names := make([]string, 0, len(p.Pillars))
	for name := range p.Pillars {
		names = append(names, name)
	}
	sort.Strings(names)
	parts := make([]string, 0, len(names))
	for _, name := range names {
		d := p.Pillars[name]
		parts = append(parts, fmt.Sprintf("%q:%d/%d/%v", name, d.ExpectedNum, d.FactualNum, d.Weight))
	}
	return fmt.Sprintf("(%x, %x] total %v {%s}", p.PrevHash[:2], p.EndHash[:2], p.TotalWeight, strings.Join(parts, " "))
}

func describeWeights(weights map[string]*big.Int) string {
	names := make([]string, 0, len(weights))
	for name := range weights {
		names = append(names, name)
	}
	sort.Strings(names)
	parts := make([]string, 0, len(names))
	for _, name := range names {
		parts = append(parts, fmt.Sprintf("%q:%v", name, weights[name]))
	}
	return strings.Join(parts, " ")
}

func describeDelegations(details map[string]*types.PillarDelegationDetail) string {
	names := make([]string, 0, len(details))
	for name := range details {
		names = append(names, name)
	}
	sort.Strings(names)
	parts := make([]string, 0, len(names))
	for _, name := range names {
		d := details[name]
		backers := make([]string, 0, len(d.Backers))
		for address, amount := range d.Backers {
			backers = append(backers, fmt.Sprintf("%v=%v", address, amount))
		}
		sort.Strings(backers)
		parts = append(parts, fmt.Sprintf("%q:%v[%s]", name, d.Weight, strings.Join(backers, ",")))
	}
	return strings.Join(parts, " ")
}

// A period point is assembled from its election, its end block and its
// content. If the frontier moves from branch A to branch B after the
// election was read, the point must still be A's point, not A's weights
// and expected slots combined with B's produced momentums and end hash;
// and it must not be stored for later reads as such.
func TestPeriodPoint_FrontierMovedMidComputationIsNotMixed(t *testing.T) {
	storeA := pinTestBranch(0xa, pinTestBranchA, 7)
	storeB := pinTestBranch(0xb, pinTestBranchB, 7)

	want, err := newPinTestFixture(storeA).period.GetPoint(5)
	if err != nil || want == nil {
		t.Fatalf("GetPoint(5) on a stable branch A = %v, %v", want, err)
	}

	f := newPinTestFixture(storeA)
	f.election.afterElection = func() { f.chain.live = storeB }
	got, err := f.period.GetPoint(5)
	if err != nil {
		t.Fatalf("GetPoint(5) with the frontier moving to B: %v", err)
	}
	if describePoint(got) != describePoint(want) {
		t.Fatalf("GetPoint(5) with the frontier moving to B after the election\n got %s\nwant %s (branch A's point)", describePoint(got), describePoint(want))
	}

	stored, err := f.db.GetPointByHeight(storage.PrefixPeriodPoint, 5)
	if err != nil {
		t.Fatalf("GetPointByHeight(5): %v", err)
	}
	if stored != nil && describePoint(stored) != describePoint(want) {
		t.Fatalf("stored period point 5\n got %s\nwant %s or nothing", describePoint(stored), describePoint(want))
	}
}

// The same race one level up: the walk of an epoch point reads its
// lower period points one at a time, and must read all of them from
// the branch the epoch point was resolved against.
func TestEpochPoint_FrontierMovedMidWalkIsNotMixed(t *testing.T) {
	storeA := pinTestBranch(0xa, pinTestBranchA, 7)
	storeB := pinTestBranch(0xb, pinTestBranchB, 7)

	want, err := newPinTestFixture(storeA).epoch.GetPoint(1)
	if err != nil || want == nil {
		t.Fatalf("epoch GetPoint(1) on a stable branch A = %v, %v", want, err)
	}

	f := newPinTestFixture(storeA)
	f.election.afterElection = func() { f.chain.live = storeB }
	got, err := f.epoch.GetPoint(1)
	if err != nil {
		t.Fatalf("epoch GetPoint(1) with the frontier moving to B: %v", err)
	}
	if describePoint(got) != describePoint(want) {
		t.Fatalf("epoch GetPoint(1) with the frontier moving to B mid-walk\n got %s\nwant %s (branch A's point)", describePoint(got), describePoint(want))
	}
}

// GetPillarWeights through a reader fixed at branch A must not serve
// the period point the live branch B has cached for the same tick.
func TestFixedReader_PillarWeightsIgnoreOtherBranchCache(t *testing.T) {
	storeA := pinTestBranch(0xa, pinTestBranchA, 7)
	storeB := pinTestBranch(0xb, pinTestBranchB, 7)

	want, err := newPinTestFixture(storeA).reader(storeA).GetPillarWeights()
	if err != nil {
		t.Fatalf("GetPillarWeights on a stable branch A: %v", err)
	}

	f := newPinTestFixture(storeB)
	// The live branch B computes and caches its own point for tick 5.
	if p, err := f.period.GetPoint(5); err != nil || p == nil {
		t.Fatalf("GetPoint(5) on live branch B = %v, %v", p, err)
	}
	got, err := f.reader(storeA).GetPillarWeights()
	if err != nil {
		t.Fatalf("GetPillarWeights fixed at A with B live: %v", err)
	}
	if describeWeights(got) != describeWeights(want) {
		t.Fatalf("GetPillarWeights fixed at A with B live\n got %s\nwant %s (branch A's weights)", describeWeights(got), describeWeights(want))
	}
}

// A reader fixed at an older momentum of the live branch must report
// the epoch as that momentum sees it: epoch 1 has only tick 4 at h5,
// though the live frontier h7 has reached tick 6.
func TestFixedReader_EpochStatsAnswerFromOlderMomentum(t *testing.T) {
	older := pinTestBranch(0xa, pinTestBranchA, 5)
	live := pinTestBranch(0xa, pinTestBranchA, 7)

	wantStats, err := newPinTestFixture(older).reader(older).EpochStats(1)
	if err != nil || wantStats == nil {
		t.Fatalf("EpochStats(1) with h5 live = %v, %v", wantStats, err)
	}
	wantPoint, err := newPinTestFixture(older).epoch.GetPoint(1)
	if err != nil {
		t.Fatalf("epoch GetPoint(1) with h5 live: %v", err)
	}

	f := newPinTestFixture(live)
	got, err := f.reader(older).EpochStats(1)
	if err != nil || got == nil {
		t.Fatalf("EpochStats(1) fixed at h5 with h7 live = %v, %v", got, err)
	}
	assertSameEpochStats(t, fmt.Sprintf("EpochStats(1) fixed at h5 with h7 live (want point %s)", describePoint(wantPoint)), got, wantStats)
}

// GetPillarDelegationsByEpoch through a reader fixed at branch A must
// sample every tick's proof momentum on branch A, not on live branch B.
func TestFixedReader_DelegationsByEpochAnswerFromFixedBranch(t *testing.T) {
	storeA := pinTestBranch(0xa, pinTestBranchA, 7)
	storeB := pinTestBranch(0xb, pinTestBranchB, 7)

	want, err := newPinTestFixture(storeA).reader(storeA).GetPillarDelegationsByEpoch(1)
	if err != nil {
		t.Fatalf("GetPillarDelegationsByEpoch(1) on a stable branch A: %v", err)
	}
	got, err := newPinTestFixture(storeB).reader(storeA).GetPillarDelegationsByEpoch(1)
	if err != nil {
		t.Fatalf("GetPillarDelegationsByEpoch(1) fixed at A with B live: %v", err)
	}
	if describeDelegations(got) != describeDelegations(want) {
		t.Fatalf("GetPillarDelegationsByEpoch(1) fixed at A with B live\n got %s\nwant %s (branch A's delegations)", describeDelegations(got), describeDelegations(want))
	}
}

// GetContent must not report a tick as empty when its end block lies
// above its start block but the store returns no momentums in between:
// that is a store inconsistent with the end block it reported (on dev,
// a frontier that fell below the tick between the reads), not a tick
// without momentums.
func TestChainTicker_EmptyRangeBetweenDifferentHeightsIsAnError(t *testing.T) {
	st := pinTestBranch(0xa, pinTestBranchA, 7)
	ct := newChainTicker(&pinTestChain{live: st}, pinTestPeriod)

	blocks, err := ct.GetContent(5)
	if err != nil || len(blocks) != 1 {
		t.Fatalf("GetContent(5) on a consistent store = %d momentums, %v; want 1, nil", len(blocks), err)
	}
	// A genuinely empty tick (no momentum between the end blocks) still
	// reads as empty: tick 0 holds only genesis.
	if blocks, err := ct.GetContent(0); err != nil || len(blocks) != 0 {
		t.Fatalf("GetContent(0) = %d momentums, %v; want 0, nil", len(blocks), err)
	}

	st.dropRange = true
	blocks, err = ct.GetContent(5)
	if err == nil {
		t.Fatalf("GetContent(5) with heights 5 to 6 and no momentums in range = %d momentums, nil; want an error", len(blocks))
	}
	if blocks != nil {
		t.Fatalf("GetContent(5) returned %d momentums alongside the error %v; want nil", len(blocks), err)
	}
	t.Logf("GetContent(5) returned the expected error: %v", err)
}

// A completed epoch point in the cache must not answer for a reader
// pinned at a momentum where that epoch has not finished, even when
// both resolve the same end block. Here the reader's frontier is h5
// (tick 4, T0+45s) and the live chain's next momentum lands at T0+85s,
// after epoch 1 (ticks 4..7) ends: epoch 1 ends at h5 in both views,
// but at h5 only tick 4 has started, while the cached completed point
// also counts the expected slots of the empty ticks 5..7.
func TestFixedReader_EpochStatsIgnoreCompletedCacheBeyondMomentum(t *testing.T) {
	older := pinTestBranch(0xa, pinTestBranchA, 5)
	live := pinTestBranch(0xa, pinTestBranchA, 5)
	next := pinTestMomentum(0xa, 6, pinTestBranchA)
	ts := pinTestGenesisTime.Add(85 * time.Second)
	next.Timestamp, next.TimestampUnix = &ts, uint64(ts.Unix())
	next.PreviousHash = live.momentums[4].Hash
	live.momentums = append(live.momentums, next)

	wantStats, err := newPinTestFixture(older).reader(older).EpochStats(1)
	if err != nil || wantStats == nil {
		t.Fatalf("EpochStats(1) with h5 live = %v, %v", wantStats, err)
	}

	f := newPinTestFixture(live)
	// The live chain finishes epoch 1 and caches its completed point.
	completed, err := f.epoch.GetPoint(1)
	if err != nil || completed == nil {
		t.Fatalf("epoch GetPoint(1) on the live chain = %v, %v", completed, err)
	}
	if completed.EndHash != older.momentums[4].Hash {
		t.Fatalf("completed epoch 1 ends at %x, want h5 so both views share the end block", completed.EndHash[:2])
	}

	got, err := f.reader(older).EpochStats(1)
	if err != nil || got == nil {
		t.Fatalf("EpochStats(1) fixed at h5 with the completed epoch cached = %v, %v", got, err)
	}
	assertSameEpochStats(t, "EpochStats(1) fixed at h5 with the completed epoch cached", got, wantStats)

	// The completed entry stays cached for the views that finished it.
	again, err := f.epoch.GetPoint(1)
	if err != nil || describePoint(again) != describePoint(completed) {
		t.Fatalf("epoch GetPoint(1) on the live chain after the pinned read = %s, %v; want %s", describePoint(again), err, describePoint(completed))
	}
}

func assertSameEpochStats(t *testing.T, what string, got, want *api.EpochStats) {
	t.Helper()
	if got.TotalBlocks != want.TotalBlocks || got.TotalWeight.Cmp(want.TotalWeight) != 0 || len(got.Pillars) != len(want.Pillars) {
		t.Fatalf("%s: blocks %v weight %v pillars %v, want blocks %v weight %v pillars %v",
			what, got.TotalBlocks, got.TotalWeight, len(got.Pillars), want.TotalBlocks, want.TotalWeight, len(want.Pillars))
	}
	for name, w := range want.Pillars {
		g := got.Pillars[name]
		if g == nil || g.BlockNum != w.BlockNum || g.ExpectedBlockNum != w.ExpectedBlockNum || g.Weight.Cmp(w.Weight) != 0 {
			t.Fatalf("%s: pillar %q = %+v, want %+v", what, name, g, w)
		}
	}
}
