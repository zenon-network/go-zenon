package api

import (
	"bytes"
	"encoding/json"
	"math/big"
	"testing"

	"github.com/zenon-network/go-zenon/chain"
	g "github.com/zenon-network/go-zenon/chain/genesis/mock"
	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/chain/store"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/zenon/mock"
)

// countingChain counts the momentums expanded through PrefetchMomentum,
// the first step of building one detailed momentum.
type countingChain struct {
	chain.Chain
	prefetched *int
}

func (c countingChain) GetFrontierMomentumStore() store.Momentum {
	return countingStore{Momentum: c.Chain.GetFrontierMomentumStore(), prefetched: c.prefetched}
}

type countingStore struct {
	store.Momentum
	prefetched *int
}

func (s countingStore) PrefetchMomentum(m *nom.Momentum) (*nom.DetailedMomentum, error) {
	*s.prefetched++
	return s.Momentum.PrefetchMomentum(m)
}

// unbudgetedDetailedList is momentumListToDetailedList as it was before
// the byte budget, kept as the reference for byte-identical responses.
func unbudgetedDetailedList(chain chain.Chain, list *MomentumList) (*DetailedMomentumList, error) {
	ans := &DetailedMomentumList{
		Count: list.Count,
		List:  make([]*DetailedMomentum, len(list.List)),
	}
	for index, momentum := range list.List {
		store := chain.GetFrontierMomentumStore()
		m, err := store.PrefetchMomentum(momentum.Momentum)
		if err != nil {
			return nil, err
		}
		accountBlocks, err := ledgerAccountBlocksToRpc(chain, m.AccountBlocks)
		if err != nil {
			return nil, err
		}
		ans.List[index] = &DetailedMomentum{
			Momentum:      momentum,
			AccountBlocks: accountBlocks,
		}
	}
	return ans, nil
}

const (
	budgetFixtureMomentums      = 6
	budgetFixtureBlocksPerMom   = 3
	budgetFixtureBlockDataBytes = 1500
)

// newBudgetFixture builds a chain whose momentums 2 through
// budgetFixtureMomentums+1 each confirm budgetFixtureBlocksPerMom send
// blocks carrying budgetFixtureBlockDataBytes of data, so every detailed
// momentum in that range encodes to several KB.
func newBudgetFixture(t *testing.T) (mock.MockZenon, *LedgerApi, *int) {
	z := mock.NewMockZenon(t)
	for i := 0; i < budgetFixtureMomentums; i++ {
		for j := 0; j < budgetFixtureBlocksPerMom; j++ {
			data := bytes.Repeat([]byte{byte(i*budgetFixtureBlocksPerMom + j + 1)}, budgetFixtureBlockDataBytes)
			z.InsertSendBlock(&nom.AccountBlock{
				Address:       g.User1.Address,
				ToAddress:     g.User2.Address,
				TokenStandard: types.ZnnTokenStandard,
				Amount:        big.NewInt(1),
				Data:          data,
			}, nil, mock.SkipVmChanges)
		}
		z.InsertNewMomentum()
	}
	prefetched := new(int)
	l := &LedgerApi{
		z:     z,
		chain: countingChain{Chain: z.Chain(), prefetched: prefetched},
		log:   common.RPCLogger.New("module", "ledger_api"),
	}
	return z, l, prefetched
}

func mustMarshal(t *testing.T, v interface{}) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// reference returns the pre-budget response for [height, height+count)
// and the encoded size of each of its elements.
func reference(t *testing.T, l *LedgerApi, height, count uint64) (*DetailedMomentumList, []int) {
	t.Helper()
	headers, err := l.GetMomentumsByHeight(height, count)
	if err != nil {
		t.Fatal(err)
	}
	ref, err := unbudgetedDetailedList(l.chain, headers)
	if err != nil {
		t.Fatal(err)
	}
	sizes := make([]int, len(ref.List))
	for i, d := range ref.List {
		sizes[i] = len(mustMarshal(t, d))
	}
	return ref, sizes
}

// encodedListSize is the size of "[e0,e1,...,ek-1]" for the first k
// elements.
func encodedListSize(sizes []int, k int) int {
	size := 2
	for i := 0; i < k; i++ {
		size += sizes[i]
		if i > 0 {
			size++
		}
	}
	return size
}

// A range that fits the budget is answered exactly as before the budget
// existed: same bytes, no "more" key. The fixture's whole chain is a few
// tens of KB, far below the production budget.
func TestGetDetailedMomentumsByHeight_SmallRangeIsByteIdentical(t *testing.T) {
	z, l, _ := newBudgetFixture(t)
	defer z.StopPanic()

	for _, r := range []struct{ height, count uint64 }{{1, 1}, {1, 3}, {2, 4}, {1, RpcMaxCountSize}} {
		ref, _ := reference(t, l, r.height, r.count)
		got, err := l.GetDetailedMomentumsByHeight(r.height, r.count)
		if err != nil {
			t.Fatal(err)
		}
		want, have := mustMarshal(t, ref), mustMarshal(t, got)
		if !bytes.Equal(want, have) {
			t.Fatalf("height %d count %d: response changed\nwant %s\nhave %s", r.height, r.count, want, have)
		}
		if bytes.Contains(have, []byte(`"more"`)) {
			t.Fatalf("height %d count %d: complete response carries a more key", r.height, r.count)
		}
	}
}

// With a budget that fits exactly the first k momentums of the range,
// the list holds those k (byte-identical to the reference prefix), sets
// More, stays within the budget, and expands only one momentum past
// them; a budget one byte smaller drops the k-th.
func TestMomentumListToDetailedList_StopsAtByteBudget(t *testing.T) {
	z, l, prefetched := newBudgetFixture(t)
	defer z.StopPanic()

	const height, count = 2, budgetFixtureMomentums
	ref, sizes := reference(t, l, height, count)
	if len(ref.List) != count {
		t.Fatalf("fixture has %d momentums in range, want %d", len(ref.List), count)
	}
	for i, s := range sizes {
		if s < budgetFixtureBlocksPerMom*budgetFixtureBlockDataBytes {
			t.Fatalf("momentum %d encodes to only %d bytes; fixture blocks are missing", i, s)
		}
	}
	headers, err := l.GetMomentumsByHeight(height, count)
	if err != nil {
		t.Fatal(err)
	}

	for k := 1; k < count; k++ {
		for _, tc := range []struct {
			budget int
			want   int
		}{
			{encodedListSize(sizes, k), k},
			{encodedListSize(sizes, k) - 1, max(k-1, 1)},
		} {
			*prefetched = 0
			got, err := momentumListToDetailedList(l.chain, headers, tc.budget)
			if err != nil {
				t.Fatal(err)
			}
			if len(got.List) != tc.want || !got.More {
				t.Fatalf("budget %d: got %d momentums more=%v, want %d more=true", tc.budget, len(got.List), got.More, tc.want)
			}
			if *prefetched != tc.want+1 {
				t.Fatalf("budget %d: expanded %d momentums for a %d-momentum answer; the rest of the range must not be built", tc.budget, *prefetched, tc.want)
			}
			encoded := mustMarshal(t, got.List)
			if tc.want > 1 && len(encoded) > tc.budget {
				t.Fatalf("budget %d: list encodes to %d bytes", tc.budget, len(encoded))
			}
			if want := mustMarshal(t, ref.List[:tc.want]); !bytes.Equal(want, encoded) {
				t.Fatalf("budget %d: truncated list differs from the reference prefix", tc.budget)
			}
			if got.Count != ref.Count {
				t.Fatalf("budget %d: count %d, want %d", tc.budget, got.Count, ref.Count)
			}
		}
	}

	// The whole range at exactly its size is complete.
	*prefetched = 0
	got, err := momentumListToDetailedList(l.chain, headers, encodedListSize(sizes, count))
	if err != nil {
		t.Fatal(err)
	}
	if len(got.List) != count || got.More || *prefetched != count {
		t.Fatalf("exact budget: got %d momentums more=%v expanded=%d", len(got.List), got.More, *prefetched)
	}
}

// The first momentum is returned even when it alone exceeds the budget,
// so a caller always advances.
func TestMomentumListToDetailedList_FirstMomentumAlwaysReturned(t *testing.T) {
	z, l, prefetched := newBudgetFixture(t)
	defer z.StopPanic()

	headers, err := l.GetMomentumsByHeight(2, budgetFixtureMomentums)
	if err != nil {
		t.Fatal(err)
	}
	*prefetched = 0
	got, err := momentumListToDetailedList(l.chain, headers, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.List) != 1 || !got.More || got.List[0].Momentum.Height != 2 || *prefetched != 2 {
		t.Fatalf("got %d momentums more=%v expanded=%d", len(got.List), got.More, *prefetched)
	}
}

// Through the RPC method, a range larger than the budget comes back
// short with "more":true, and a client that continues from the height
// after the last returned momentum reassembles the full range exactly.
func TestGetDetailedMomentumsByHeight_ContinuesAcrossBudget(t *testing.T) {
	z, l, prefetched := newBudgetFixture(t)
	defer z.StopPanic()

	const height, count = 2, budgetFixtureMomentums
	ref, sizes := reference(t, l, height, count)

	saved := detailedMomentumsByteBudget
	defer func() { detailedMomentumsByteBudget = saved }()
	detailedMomentumsByteBudget = encodedListSize(sizes, 2)

	var all []*DetailedMomentum
	next, left, calls := uint64(height), uint64(count), 0
	for left > 0 {
		*prefetched = 0
		got, err := l.GetDetailedMomentumsByHeight(next, left)
		if err != nil {
			t.Fatal(err)
		}
		calls++
		if len(got.List) == 0 {
			t.Fatalf("call %d returned no momentums", calls)
		}
		if *prefetched > len(got.List)+1 {
			t.Fatalf("call %d expanded %d momentums for %d returned", calls, *prefetched, len(got.List))
		}
		raw := mustMarshal(t, got)
		if got.More != bytes.Contains(raw, []byte(`"more":true`)) {
			t.Fatalf("call %d: more=%v but encoded as %s", calls, got.More, raw)
		}
		if uint64(len(got.List)) < left && !got.More {
			t.Fatalf("call %d: short list without more", calls)
		}
		all = append(all, got.List...)
		last := got.List[len(got.List)-1].Momentum.Height
		left -= last + 1 - next
		next = last + 1
	}
	if calls < 2 {
		t.Fatalf("budget did not split the range (%d call)", calls)
	}
	if !bytes.Equal(mustMarshal(t, ref.List), mustMarshal(t, all)) {
		t.Fatal("continued responses do not reassemble the reference range")
	}
}
