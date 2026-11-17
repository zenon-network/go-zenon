package consensus

import (
	"math/big"
	"sort"
	"testing"
	"time"

	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/chain/store"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/db"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/consensus/storage"
	"github.com/zenon-network/go-zenon/vm/constants"
	"github.com/zenon-network/go-zenon/vm/embedded/definition"
)

// useJailTestConsensus sets a 5-slot election with 50 s ticks and
// restores the previous configuration when the test ends.
func useJailTestConsensus(t *testing.T) {
	t.Helper()
	saved := constants.ConsensusConfig
	constants.ConsensusConfig = &constants.Consensus{
		BlockTime:   10,
		NodeCount:   5,
		RandCount:   2,
		CountingZTS: types.ZnnTokenStandard,
	}
	t.Cleanup(func() { constants.ConsensusConfig = saved })
}

type jailTestPillar struct {
	name   string
	weight int64
	addr   types.Address
}

var jailTestPillars = []jailTestPillar{
	{"pillar-a", 30, types.PubKeyToAddress(pinTestKey(0x1a))},
	{"pillar-b", 20, types.PubKeyToAddress(pinTestKey(0x1b))},
	{"pillar-c", 10, types.PubKeyToAddress(pinTestKey(0x1c))},
}

// jailTestStore is a momentum store that reports a fixed jailed set and
// three pillars as delegations. delegationCalls, if set, counts the
// delegation computations.
type jailTestStore struct {
	*pinTestStore
	jailed          map[string]bool
	delegationCalls *int
}

func (s *jailTestStore) GetActivePillars() ([]*definition.PillarInfo, error) {
	result := make([]*definition.PillarInfo, 0, len(jailTestPillars))
	for _, p := range jailTestPillars {
		result = append(result, &definition.PillarInfo{Name: p.name, BlockProducingAddress: p.addr})
	}
	return result, nil
}

func (s *jailTestStore) GetMomentumByHash(hash types.Hash) (*nom.Momentum, error) {
	for _, m := range s.momentums {
		if m.Hash == hash {
			return pinTestCopy(m), nil
		}
	}
	return nil, nil
}

func (s *jailTestStore) ComputePillarDelegations() ([]*types.PillarDelegationDetail, error) {
	if s.delegationCalls != nil {
		*s.delegationCalls++
	}
	result := make([]*types.PillarDelegationDetail, 0, len(jailTestPillars))
	for _, p := range jailTestPillars {
		result = append(result, &types.PillarDelegationDetail{
			PillarDelegation: types.PillarDelegation{
				Name:      p.name,
				Producing: p.addr,
				Weight:    big.NewInt(p.weight),
			},
			Backers: map[types.Address]*big.Int{},
		})
	}
	return result, nil
}

func (s *jailTestStore) GetJailedPillars() (map[string]bool, error) {
	if s.jailed == nil {
		return map[string]bool{}, nil
	}
	return s.jailed, nil
}

// jailTestChain holds one store per height; the live store is the top.
type jailTestChain struct {
	*pinTestChain
	stores []*jailTestStore // stores[i] is the store at height i+1
}

func (c *jailTestChain) GetMomentumStore(id types.HashHeight) store.Momentum {
	if id.Height == 0 || id.Height > uint64(len(c.stores)) {
		return nil
	}
	s := c.stores[id.Height-1]
	if s.momentums[len(s.momentums)-1].Hash != id.Hash {
		return nil
	}
	return s
}

// jailTestMomentum builds momentum hN at the given offset from genesis,
// produced by pillar-a.
func jailTestMomentum(height uint64, offset time.Duration) *nom.Momentum {
	return jailTestMomentumBy(height, offset, pinTestKey(0x1a))
}

// jailTestMomentumBy builds momentum hN at the given offset from
// genesis, signed with key.
func jailTestMomentumBy(height uint64, offset time.Duration, key []byte) *nom.Momentum {
	ts := pinTestGenesisTime.Add(offset)
	var hash types.Hash
	hash[0] = 0x77
	hash[1] = byte(height)
	return &nom.Momentum{
		Hash:          hash,
		Height:        height,
		Timestamp:     &ts,
		TimestampUnix: uint64(ts.Unix()),
		PublicKey:     key,
	}
}

// newJailTestChain builds h1 (tick 0), h2 (tick 1), h3 (tick 2), h4
// (tick 4) and h5 (tick 5), with no momentum in tick 3, all produced by
// pillar-a. Stores at heights from jailedFrom up report the jailed
// names.
func newJailTestChain(jailedFrom uint64, jailed ...string) *jailTestChain {
	offsets := []time.Duration{0, 55 * time.Second, 105 * time.Second, 205 * time.Second, 255 * time.Second}
	return newJailTestChainOf(offsets, nil, jailedFrom, jailed...)
}

// newJailTestChainOf builds one momentum per offset. keys[i], when
// given, signs the momentum at offsets[i]; pillar-a signs the others.
func newJailTestChainOf(offsets []time.Duration, keys [][]byte, jailedFrom uint64, jailed ...string) *jailTestChain {
	all := make([]*nom.Momentum, 0, len(offsets))
	ch := &jailTestChain{}
	for i, offset := range offsets {
		height := uint64(i + 1)
		m := jailTestMomentum(height, offset)
		if i < len(keys) && keys[i] != nil {
			m = jailTestMomentumBy(height, offset, keys[i])
		}
		if i > 0 {
			m.PreviousHash = all[i-1].Hash
		}
		all = append(all, m)
		s := &jailTestStore{pinTestStore: &pinTestStore{momentums: append([]*nom.Momentum(nil), all...)}}
		if height >= jailedFrom {
			s.jailed = map[string]bool{}
			for _, name := range jailed {
				s.jailed[name] = true
			}
		}
		ch.stores = append(ch.stores, s)
	}
	ch.pinTestChain = &pinTestChain{live: ch.stores[len(ch.stores)-1]}
	return ch
}

func (c *jailTestChain) momentum(height uint64) *nom.Momentum {
	return c.stores[height-1].momentums[height-1]
}

func newJailTestElection(ch *jailTestChain) (*electionManager, *storage.DB) {
	consensusDB := storage.NewConsensusDB(db.NewMemDB(), 16, 16)
	return newElectionManager(ch, consensusDB), consensusDB
}

func jailTestProducers(t *testing.T, r *electionResult) []types.Address {
	t.Helper()
	addrs := make([]types.Address, 0, len(r.Producers))
	for _, p := range r.Producers {
		addrs = append(addrs, p.Producer)
	}
	return addrs
}

func jailTestHas(addrs []types.Address, want types.Address) bool {
	for _, a := range addrs {
		if a == want {
			return true
		}
	}
	return false
}

var jailTestPillarC = jailTestPillars[2].addr

func jailTestElect(t *testing.T, em *electionManager, tick uint64) *electionResult {
	t.Helper()
	r, err := em.ElectionByTick(tick)
	common.FailIfErr(t, err)
	if len(r.Delegations) != 3 {
		t.Fatalf("tick %d lists %d delegations, want 3", tick, len(r.Delegations))
	}
	return r
}

func expectPillarC(t *testing.T, em *electionManager, tick uint64, want bool) {
	t.Helper()
	got := jailTestHas(jailTestProducers(t, jailTestElect(t, em, tick)), jailTestPillarC)
	if got != want {
		t.Fatalf("tick %d includes pillar-c: %v, want %v", tick, got, want)
	}
}

func TestExcludeJailed(t *testing.T) {
	mk := func(names ...string) []*types.PillarDelegation {
		list := make([]*types.PillarDelegation, 0, len(names))
		for _, n := range names {
			list = append(list, &types.PillarDelegation{Name: n, Weight: big.NewInt(1)})
		}
		return list
	}
	names := func(list []*types.PillarDelegation) []string {
		out := make([]string, 0, len(list))
		for _, d := range list {
			out = append(out, d.Name)
		}
		return out
	}
	cases := []struct {
		name      string
		input     []string
		jailed    map[string]bool
		want      []string
		sameSlice bool
	}{
		{"none jailed", []string{"a", "b", "c"}, map[string]bool{}, []string{"a", "b", "c"}, true},
		{"jailed name absent", []string{"a", "b", "c"}, map[string]bool{"z": true}, []string{"a", "b", "c"}, true},
		{"middle jailed", []string{"a", "b", "c"}, map[string]bool{"b": true}, []string{"a", "c"}, false},
		{"all jailed", []string{"a", "b", "c"}, map[string]bool{"a": true, "b": true, "c": true}, []string{"a", "b", "c"}, true},
		{"empty input", []string{}, map[string]bool{"a": true}, []string{}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			input := mk(c.input...)
			got := excludeJailed(input, c.jailed)
			if g, w := names(got), c.want; len(g) != len(w) {
				t.Fatalf("got %v, want %v", g, w)
			} else {
				for i := range w {
					if g[i] != w[i] {
						t.Fatalf("got %v, want %v", g, w)
					}
				}
			}
			if c.sameSlice && len(input) > 0 && &got[0] != &input[0] {
				t.Fatal("expected the input slice itself")
			}
			if !c.sameSlice && &got[0] == &input[0] {
				t.Fatal("expected a newly allocated slice")
			}
			orig := names(input)
			for i := range c.input {
				if orig[i] != c.input[i] {
					t.Fatalf("input changed to %v, want %v", orig, c.input)
				}
			}
		})
	}
}

func TestJailFallback(t *testing.T) {
	useJailTestConsensus(t)
	keyA, keyC := pinTestKey(0x1a), pinTestKey(0x1c)
	// h1 tick 0 (a), h2 tick 1 (a), h3 tick 2 (c), h4 tick 2 (a),
	// h5 tick 4 (c), h6 tick 4 (c); tick 3 is empty.
	offsets := []time.Duration{0, 55 * time.Second, 101 * time.Second, 105 * time.Second, 201 * time.Second, 205 * time.Second}
	keys := [][]byte{keyA, keyA, keyC, keyA, keyC, keyC}
	cases := []struct {
		name   string
		jailed []string
		tick   uint64
		want   bool
	}{
		{"no jailed pillar", nil, 5, false},
		{"tick 1 never falls back", []string{"pillar-a", "pillar-b"}, 1, false},
		{"kept pillar produced in tick-2", []string{"pillar-c"}, 4, false},
		{"tick-2 empty", []string{"pillar-c"}, 5, true},
		{"only jailed pillars produced in tick-2", []string{"pillar-c"}, 6, true},
		{"a kept pillar produced earlier in tick-2", []string{"pillar-b"}, 4, false},
		{"every producer of tick-2 jailed", []string{"pillar-a", "pillar-c"}, 4, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ch := newJailTestChainOf(offsets, keys, 1, c.jailed...)
			em, _ := newJailTestElection(ch)
			proofBlock, err := getMomentumBeforeTime(ch.live, em.genProofTime(c.tick))
			common.FailIfErr(t, err)
			proofStore := ch.GetMomentumStore(proofBlock.Identifier())
			jailed, err := proofStore.GetJailedPillars()
			common.FailIfErr(t, err)
			got, err := em.jailFallback(proofStore, c.tick, proofBlock, jailed)
			common.FailIfErr(t, err)
			if got != c.want {
				t.Fatalf("got %v, want %v", got, c.want)
			}
		})
	}
}

// When every kept pillar is offline, the fallback lets the jailed
// pillars produce, and their momentums keep the fallback on: no tick
// goes back to the jail-filtered schedule while only jailed pillars
// produce.
func TestElection_FallbackKeepsProducingThroughJailedPillars(t *testing.T) {
	useJailTestConsensus(t)
	keyC := pinTestKey(0x1c)
	// pillar-a produces through tick 2, then only jailed pillar-c
	// produces, in ticks 6 to 9.
	offsets := []time.Duration{0, 55 * time.Second, 105 * time.Second, 305 * time.Second, 355 * time.Second, 405 * time.Second, 455 * time.Second}
	keys := [][]byte{nil, nil, nil, keyC, keyC, keyC, keyC}
	ch := newJailTestChainOf(offsets, keys, 1, "pillar-c")
	em, _ := newJailTestElection(ch)

	expectPillarC(t, em, 4, false)
	for tick := uint64(6); tick <= 11; tick++ {
		expectPillarC(t, em, tick, true)
	}
}

func TestElection_OneProofHashServesFallbackAndNormalTicks(t *testing.T) {
	useJailTestConsensus(t)
	ch := newJailTestChain(3, "pillar-c")
	h3 := ch.momentum(3)

	checkStored := func(consensusDB *storage.DB) {
		t.Helper()
		stored, err := consensusDB.GetElectionResultByHash(h3.Hash)
		common.FailIfErr(t, err)
		if stored == nil {
			t.Fatal("no result stored under h3")
		}
		if jailTestHas(stored.Producers, jailTestPillarC) {
			t.Fatal("stored result includes pillar-c")
		}
		if len(stored.Delegations) != 3 {
			t.Fatalf("stored result lists %d delegations, want 3", len(stored.Delegations))
		}
	}

	em, consensusDB := newJailTestElection(ch)
	expectPillarC(t, em, 4, false)
	expectPillarC(t, em, 5, true)
	expectPillarC(t, em, 4, false)
	checkStored(consensusDB)

	em, consensusDB = newJailTestElection(ch)
	expectPillarC(t, em, 5, true)
	stored, err := consensusDB.GetElectionResultByHash(h3.Hash)
	common.FailIfErr(t, err)
	if stored != nil {
		t.Fatal("fallback tick wrote a result under h3")
	}
	fallback, err := consensusDB.GetFallbackElectionResult(h3.Hash, 5)
	common.FailIfErr(t, err)
	if fallback == nil || !jailTestHas(fallback.Producers, jailTestPillarC) {
		t.Fatalf("fallback result under (h3, 5) is %+v, want one with pillar-c", fallback)
	}
	expectPillarC(t, em, 4, false)
	checkStored(consensusDB)
}

// Repeated fallback elections of one tick compute the delegations
// once.
func TestElection_FallbackComputedOnce(t *testing.T) {
	useJailTestConsensus(t)
	ch := newJailTestChain(3, "pillar-c")
	calls := 0
	for _, s := range ch.stores {
		s.delegationCalls = &calls
	}
	em, _ := newJailTestElection(ch)
	for i := 0; i < 20; i++ {
		expectPillarC(t, em, 5, true)
	}
	if calls != 1 {
		t.Fatalf("20 fallback elections computed the delegations %d times, want 1", calls)
	}
}

// While the proof momentum stops moving, each fallback tick draws its
// own schedule, so every candidate is eventually elected.
func TestElection_FallbackScheduleRotatesWhileProofIsStuck(t *testing.T) {
	saved := constants.ConsensusConfig
	constants.ConsensusConfig = &constants.Consensus{BlockTime: 10, NodeCount: 2, RandCount: 1, CountingZTS: types.ZnnTokenStandard}
	t.Cleanup(func() { constants.ConsensusConfig = saved })

	offsets := []time.Duration{0, 25 * time.Second, 45 * time.Second}
	stuck := newJailTestChainOf(offsets, nil, 1, "pillar-b")
	em, _ := newJailTestElection(stuck)

	// Tick 4 is proved by h3, a kept pillar's momentum in tick 2; from
	// tick 5 on tick-2 is empty and the proof stays h3.
	seen := map[types.Address]bool{}
	schedules := map[string]bool{}
	for tick := uint64(5); tick <= 40; tick++ {
		r, err := em.ElectionByTick(tick)
		common.FailIfErr(t, err)
		key := ""
		for _, p := range r.Producers {
			seen[p.Producer] = true
			key += p.Producer.String()
		}
		schedules[key] = true
	}
	for _, p := range jailTestPillars {
		if !seen[p.addr] {
			t.Fatalf("%s was never elected over 36 stuck fallback ticks", p.name)
		}
	}
	if len(schedules) < 2 {
		t.Fatal("every stuck fallback tick drew the same schedule")
	}
}

func TestElection_PrecomputeDoesNotLeakIntoStallPath(t *testing.T) {
	useJailTestConsensus(t)
	ch := newJailTestChain(3, "pillar-c")
	em, consensusDB := newJailTestElection(ch)

	// h4 completes tick 3, so the precompute elects tick 5 from h3, and
	// tick 3 is empty: a fallback election.
	em.InsertMomentum(&nom.DetailedMomentum{Momentum: ch.momentum(4)})
	stored, err := consensusDB.GetFallbackElectionResult(ch.momentum(3).Hash, 5)
	common.FailIfErr(t, err)
	if stored == nil || !jailTestHas(stored.Producers, jailTestPillarC) {
		t.Fatalf("precomputed fallback result under (h3, 5) is %+v, want one with pillar-c", stored)
	}
	normal, err := consensusDB.GetElectionResultByHash(ch.momentum(3).Hash)
	common.FailIfErr(t, err)
	if normal != nil {
		t.Fatal("the fallback precompute wrote a result under h3")
	}

	expectPillarC(t, em, 5, true)
	expectPillarC(t, em, 4, false)
}

func TestElection_ColdAndWarmCacheAgree(t *testing.T) {
	useJailTestConsensus(t)
	ch := newJailTestChain(3, "pillar-c")

	warm, _ := newJailTestElection(ch)
	for h := uint64(2); h <= 5; h++ {
		warm.InsertMomentum(&nom.DetailedMomentum{Momentum: ch.momentum(h)})
	}
	for tick := uint64(7); ; tick-- {
		jailTestElect(t, warm, tick)
		if tick == 0 {
			break
		}
	}
	cold, _ := newJailTestElection(ch)

	for tick := uint64(0); tick <= 7; tick++ {
		w := jailTestElect(t, warm, tick)
		c := jailTestElect(t, cold, tick)
		wp, cp := jailTestProducers(t, w), jailTestProducers(t, c)
		if len(wp) != len(cp) {
			t.Fatalf("tick %d: warm has %d producers, cold has %d", tick, len(wp), len(cp))
		}
		for i := range wp {
			if wp[i] != cp[i] {
				t.Fatalf("tick %d slot %d: warm %v, cold %v", tick, i, wp[i], cp[i])
			}
		}
		for i := range w.Delegations {
			wd, cd := w.Delegations[i], c.Delegations[i]
			if wd.Name != cd.Name || wd.Weight.Cmp(cd.Weight) != 0 {
				t.Fatalf("tick %d delegation %d: warm %v, cold %v", tick, i, wd, cd)
			}
		}
		wantC := tick == 5 || tick < 4
		if jailTestHas(wp, jailTestPillarC) != wantC {
			t.Fatalf("tick %d includes pillar-c: %v, want %v", tick, !wantC, wantC)
		}
	}
}

// With every pillar jailed no kept pillar can produce, so every tick
// from 2 on is a fallback election over the full delegation list.
func TestElection_AllJailedFallsBackToFullList(t *testing.T) {
	useJailTestConsensus(t)
	ch := newJailTestChain(1, "pillar-a", "pillar-b", "pillar-c")
	em, consensusDB := newJailTestElection(ch)

	for tick := uint64(2); tick <= 7; tick++ {
		got := jailTestProducers(t, jailTestElect(t, em, tick))

		proofBlock, err := getMomentumBeforeTime(ch.live, em.genProofTime(tick))
		common.FailIfErr(t, err)
		detailed, err := ch.GetMomentumStore(proofBlock.Identifier()).ComputePillarDelegations()
		common.FailIfErr(t, err)
		hashH := types.HashHeight{Hash: proofBlock.Hash, Height: proofBlock.Height}
		offset := tick - em.ToTick(*proofBlock.Timestamp)
		want, err := em.algo.SelectProducers(newFallbackAlgorithmContext(types.ToPillarDelegation(detailed), &hashH, offset))
		common.FailIfErr(t, err)
		for i := range want {
			if got[i] != want[i].Producing {
				t.Fatalf("tick %d slot %d: got %v, want %v", tick, i, got[i], want[i].Producing)
			}
		}
		stored, err := consensusDB.GetFallbackElectionResult(proofBlock.Hash, tick)
		common.FailIfErr(t, err)
		if stored == nil {
			t.Fatalf("tick %d stored no fallback result", tick)
		}
	}
}

func TestElection_EmptyTickTwoBackWithoutJailUsesCache(t *testing.T) {
	useJailTestConsensus(t)
	ch := newJailTestChain(0)
	h3 := ch.momentum(3)

	em, consensusDB := newJailTestElection(ch)
	jailTestElect(t, em, 5)
	stored, err := consensusDB.GetElectionResultByHash(h3.Hash)
	common.FailIfErr(t, err)
	if stored == nil {
		t.Fatal("a tick with an empty tick-2 and no jail wrote no result under h3")
	}

	em, consensusDB = newJailTestElection(ch)
	addrs := make([]types.Address, 5)
	for i := range addrs {
		addrs[i] = jailTestPillars[0].addr
	}
	delegations := make([]*types.PillarDelegation, 0, len(jailTestPillars))
	for _, p := range jailTestPillars {
		delegations = append(delegations, &types.PillarDelegation{Name: p.name, Producing: p.addr, Weight: big.NewInt(p.weight)})
	}
	sort.Slice(delegations, func(i, j int) bool { return delegations[i].Weight.Cmp(delegations[j].Weight) > 0 })
	common.FailIfErr(t, consensusDB.StoreElectionResultByHash(h3.Hash, storage.GenElectionData(addrs, delegations)))
	got := jailTestProducers(t, jailTestElect(t, em, 5))
	if len(got) != 5 {
		t.Fatalf("got %d producers, want 5", len(got))
	}
	for i, a := range got {
		if a != jailTestPillars[0].addr {
			t.Fatalf("slot %d is %v, want pillar-a", i, a)
		}
	}
}
