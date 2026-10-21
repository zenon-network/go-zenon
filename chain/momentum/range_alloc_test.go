package momentum

import (
	"runtime"
	"testing"
	"time"

	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/chain/store"
	"github.com/zenon-network/go-zenon/common/db"
)

type fixedGenesis struct {
	store.Genesis
	momentum *nom.Momentum
}

func (g *fixedGenesis) GetGenesisMomentum() *nom.Momentum { return g.momentum }

// timedChain stores count momentums ten seconds apart, starting at
// genesisTime, and returns the store. Heights listed in omit are left
// out of the store (a hole), though the chain is built as if they were
// there.
func timedChain(t *testing.T, count uint64, genesisTime time.Time, omit ...uint64) *momentumStore {
	t.Helper()
	memDB := db.NewMemDB()
	var genesis *nom.Momentum
	var prev *nom.Momentum
	omitted := make(map[uint64]bool, len(omit))
	for _, h := range omit {
		omitted[h] = true
	}
	for height := uint64(1); height <= count; height++ {
		m := &nom.Momentum{Version: 1, ChainIdentifier: 100, Height: height, TimestampUnix: uint64(genesisTime.Add(time.Duration(height-1) * 10 * time.Second).Unix())}
		if prev != nil {
			m.PreviousHash = prev.Hash
		}
		m.Hash = m.ComputeHash()
		m.EnsureCache()
		data, err := m.Serialize()
		if err != nil {
			t.Fatal(err)
		}
		if !omitted[height] {
			if err := db.SetFrontier(memDB, m.Identifier(), data); err != nil {
				t.Fatal(err)
			}
		}
		if height == 1 {
			genesis = m
		}
		prev = m
	}
	return &momentumStore{Genesis: &fixedGenesis{momentum: genesis}, DB: memDB}
}

// allocatedBy measures process-wide allocation, so the tests that use it
// must stay serial (no t.Parallel in this package).
func allocatedBy(f func()) uint64 {
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	f()
	runtime.ReadMemStats(&after)
	return after.TotalAlloc - before.TotalAlloc
}

// The search visits a logarithmic number of momentums; its working memory
// must not grow linearly with the height span (logarithmic growth is what
// the visited entries cost). On dev the map was created with the whole
// span as its capacity hint, which for a mainnet-sized span is hundreds
// of megabytes per RPC call.
func TestBinarySearchBeforeTime_AllocationDoesNotGrowWithTheSpan(t *testing.T) {
	const count = 200_000
	genesisTime := time.Unix(1_700_000_000, 0)
	ms := timedChain(t, count, genesisTime)
	start, err := ms.GetMomentumByHeight(1)
	if err != nil {
		t.Fatal(err)
	}
	end, err := ms.GetMomentumByHeight(count)
	if err != nil {
		t.Fatal(err)
	}
	// Look for the momentum before height 777's timestamp + 1 s: expect 777.
	target := genesisTime.Add(776*10*time.Second + time.Second)

	var got *nom.Momentum
	allocated := allocatedBy(func() {
		got, err = ms.binarySearchBeforeTime(start, end, target.UnixNano())
	})
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.Height != 777 {
		t.Fatalf("got height %v, want 777", got)
	}
	if allocated > 1<<20 {
		t.Fatalf("the search over a span of %d heights allocated %d bytes; it visits about %d momentums", count, allocated, 18)
	}
}

// The public lookup brackets the target by treating seconds as heights.
// A timestamp that is further from the frontier than the chain's height in
// seconds, and further from genesis than that too, is bracketed as
// [genesis, frontier]: the search then runs over the whole chain, so this
// is the span a caller can make as wide as the chain itself.
func TestGetMomentumBeforeTime_WideBracketDoesNotAllocateTheChain(t *testing.T) {
	const count = 200_000
	genesisTime := time.Unix(1_700_000_000, 0)
	ms := timedChain(t, count, genesisTime)
	// Height 30001 sits at genesis + 300,000 s; one second later still
	// selects it, and 300,000 s is more than count seconds from genesis
	// and more than count seconds before the frontier.
	target := genesisTime.Add(300_000*time.Second + time.Second)

	var got *nom.Momentum
	var err error
	allocated := allocatedBy(func() {
		got, err = ms.GetMomentumBeforeTime(&target)
	})
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.Height != 30001 {
		t.Fatalf("got %v, want height 30001", got)
	}
	if allocated > 1<<20 {
		t.Fatalf("a wide-bracket lookup on a %d-momentum chain allocated %d bytes", count, allocated)
	}
}

// Boundary timestamps and bracket shapes, and a hole in the chain.
func TestGetMomentumBeforeTime_Boundaries(t *testing.T) {
	const count = 50
	genesisTime := time.Unix(1_700_000_000, 0)
	ms := timedChain(t, count, genesisTime)
	at := func(height uint64, delta time.Duration) *time.Time {
		ts := genesisTime.Add(time.Duration(height-1)*10*time.Second + delta)
		return &ts
	}
	cases := []struct {
		name string
		ts   *time.Time
		want uint64 // 0 = nil result
	}{
		{"exactly genesis", at(1, 0), 0},
		{"one second after genesis", at(1, time.Second), 1},
		{"exactly an interior momentum", at(20, 0), 19},
		{"one second after an interior momentum", at(20, time.Second), 20},
		{"one second before an interior momentum", at(20, -time.Second), 19},
		{"exactly the frontier", at(count, 0), count - 1},
		{"after the frontier", at(count, time.Second), count},
		{"adjacent bracket", at(2, time.Second), 2},
	}
	for _, c := range cases {
		got, err := ms.GetMomentumBeforeTime(c.ts)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		switch {
		case c.want == 0 && got != nil:
			t.Fatalf("%s: got height %d, want nil", c.name, got.Height)
		case c.want != 0 && (got == nil || got.Height != c.want):
			t.Fatalf("%s: got %v, want height %d", c.name, got, c.want)
		}
	}

	// A hole below the frontier surfaces as an error, not a wrong answer.
	holed := timedChain(t, count, genesisTime, 25)
	frontier, err := holed.GetMomentumByHeight(count)
	if err != nil {
		t.Fatal(err)
	}
	first, err := holed.GetMomentumByHeight(1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := holed.binarySearchBeforeTime(first, frontier, at(25, time.Second).UnixNano()); err == nil {
		t.Fatal("a missing momentum inside the bracket was not reported")
	}
}
