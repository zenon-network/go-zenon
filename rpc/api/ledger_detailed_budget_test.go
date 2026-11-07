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

// counts records the work done building detailed momentums: account
// blocks read from the store, momentums prefetched whole (which the
// budgeted path must never do), and account blocks converted, each of
// which looks up its paired block exactly once.
type counts struct {
	read       int
	prefetched int
	paired     int
}

type countingChain struct {
	chain.Chain
	n *counts
}

func (c countingChain) GetFrontierMomentumStore() store.Momentum {
	return countingStore{Momentum: c.Chain.GetFrontierMomentumStore(), n: c.n}
}

type countingStore struct {
	store.Momentum
	n *counts
}

func (s countingStore) PrefetchMomentum(m *nom.Momentum) (*nom.DetailedMomentum, error) {
	s.n.prefetched++
	return s.Momentum.PrefetchMomentum(m)
}

func (s countingStore) GetAccountBlock(header types.AccountHeader) (*nom.AccountBlock, error) {
	s.n.read++
	return s.Momentum.GetAccountBlock(header)
}

func (s countingStore) GetBlockWhichReceives(hash types.Hash) (*nom.AccountBlock, error) {
	s.n.paired++
	return s.Momentum.GetBlockWhichReceives(hash)
}

func (s countingStore) GetAccountBlockByHash(hash types.Hash) (*nom.AccountBlock, error) {
	s.n.paired++
	return s.Momentum.GetAccountBlockByHash(hash)
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

// receiveMomentumHeight is the fixture momentum that confirms only
// receive blocks.
const receiveMomentumHeight = budgetFixtureMomentums + 2

// newBudgetFixture builds a chain whose momentums 2 through
// budgetFixtureMomentums+1 each confirm budgetFixtureBlocksPerMom send
// blocks carrying budgetFixtureBlockDataBytes of data, so every detailed
// momentum in that range encodes to several KB. Momentum
// receiveMomentumHeight then confirms the receive blocks of the first
// budgetFixtureBlocksPerMom sends: the receives carry no data, but each
// is answered with its paired send, data included.
func newBudgetFixture(t *testing.T) (mock.MockZenon, *LedgerApi, *counts) {
	z := mock.NewMockZenon(t)
	var sends []*nom.AccountBlock
	for i := 0; i < budgetFixtureMomentums; i++ {
		for j := 0; j < budgetFixtureBlocksPerMom; j++ {
			data := bytes.Repeat([]byte{byte(i*budgetFixtureBlocksPerMom + j + 1)}, budgetFixtureBlockDataBytes)
			send := z.InsertSendBlock(&nom.AccountBlock{
				Address:       g.User1.Address,
				ToAddress:     g.User2.Address,
				TokenStandard: types.ZnnTokenStandard,
				Amount:        big.NewInt(1),
				Data:          data,
			}, nil, mock.SkipVmChanges)
			sends = append(sends, send)
		}
		z.InsertNewMomentum()
	}
	for _, send := range sends[:budgetFixtureBlocksPerMom] {
		z.InsertReceiveBlock(send.Header(), nil, nil, mock.SkipVmChanges)
	}
	z.InsertNewMomentum()
	n := new(counts)
	l := &LedgerApi{
		z:     z,
		chain: countingChain{Chain: z.Chain(), n: n},
		log:   common.RPCLogger.New("module", "ledger_api"),
	}
	return z, l, n
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
// them, abandoning it at its first block; a budget one byte smaller
// drops the k-th.
func TestMomentumListToDetailedList_StopsAtByteBudget(t *testing.T) {
	z, l, n := newBudgetFixture(t)
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
		cases := []struct {
			budget int
			want   int
		}{{encodedListSize(sizes, k), k}}
		if k > 1 {
			cases = append(cases, struct {
				budget int
				want   int
			}{encodedListSize(sizes, k) - 1, k - 1})
		}
		for _, tc := range cases {
			*n = counts{}
			got, err := momentumListToDetailedList(l.chain, headers, tc.budget)
			if err != nil {
				t.Fatal(err)
			}
			if len(got.List) != tc.want || !got.More {
				t.Fatalf("budget %d: got %d momentums more=%v, want %d more=true", tc.budget, len(got.List), got.More, tc.want)
			}
			// at the exact budget nothing is left for the next momentum,
			// which is dropped before any of its blocks is built; one
			// byte short, it is dropped at its last block
			wantPaired := tc.want * budgetFixtureBlocksPerMom
			if tc.budget == encodedListSize(sizes, tc.want+1)-1 {
				wantPaired += budgetFixtureBlocksPerMom
			}
			if n.read != wantPaired || n.paired != wantPaired || n.prefetched != 0 {
				t.Fatalf("budget %d: read %d and built %d blocks (prefetched %d momentums) for a %d-momentum answer, want %d",
					tc.budget, n.read, n.paired, n.prefetched, tc.want, wantPaired)
			}
			encoded := mustMarshal(t, got.List)
			if len(encoded) > tc.budget {
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
	*n = counts{}
	got, err := momentumListToDetailedList(l.chain, headers, encodedListSize(sizes, count))
	if err != nil {
		t.Fatal(err)
	}
	if len(got.List) != count || got.More || n.read != count*budgetFixtureBlocksPerMom || n.prefetched != 0 {
		t.Fatalf("exact budget: got %d momentums more=%v read=%d prefetched=%d", len(got.List), got.More, n.read, n.prefetched)
	}
}

// A first momentum that alone exceeds the budget is an error, not an
// unbounded answer, and building it stops at the block that crosses the
// budget. The receive-only momentum is the case the verifier's content
// limit does not bound: its blocks carry no data, yet each is answered
// with its paired send block and that block's data.
func TestMomentumListToDetailedList_OversizedFirstMomentumIsAnError(t *testing.T) {
	z, l, n := newBudgetFixture(t)
	defer z.StopPanic()

	ref, sizes := reference(t, l, receiveMomentumHeight, 1)
	if len(ref.List) != 1 || ref.List[0].Momentum.Height != receiveMomentumHeight {
		t.Fatalf("fixture lacks the receive momentum: %d momentums", len(ref.List))
	}
	receives := ref.List[0].AccountBlocks
	if len(receives) != budgetFixtureBlocksPerMom {
		t.Fatalf("receive momentum has %d blocks, want %d", len(receives), budgetFixtureBlocksPerMom)
	}
	for _, b := range receives {
		if b.BlockType != nom.BlockTypeUserReceive || len(b.Data) != 0 {
			t.Fatalf("block type %d with %d data bytes, want a data-less user receive", b.BlockType, len(b.Data))
		}
		if b.PairedAccountBlock == nil || len(b.PairedAccountBlock.Data) != budgetFixtureBlockDataBytes {
			t.Fatal("receive is not answered with its data-carrying paired send")
		}
	}
	if sizes[0] < budgetFixtureBlocksPerMom*budgetFixtureBlockDataBytes {
		t.Fatalf("receive momentum encodes to only %d bytes", sizes[0])
	}

	for _, height := range []uint64{2, receiveMomentumHeight} {
		headers, err := l.GetMomentumsByHeight(height, 2)
		if err != nil {
			t.Fatal(err)
		}
		*n = counts{}
		got, err := momentumListToDetailedList(l.chain, headers, 1)
		if err != ErrDetailedMomentumTooLarge || got != nil {
			t.Fatalf("height %d: got %v, %v; want ErrDetailedMomentumTooLarge", height, got, err)
		}
		if n.read != 0 || n.paired != 0 || n.prefetched != 0 {
			t.Fatalf("height %d: read %d and built %d blocks (prefetched %d) under a 1-byte budget", height, n.read, n.paired, n.prefetched)
		}
	}

	// A budget that holds the momentum and its first block only: the
	// second block is built, crosses, and the rest are never built.
	headers, err := l.GetMomentumsByHeight(receiveMomentumHeight, 1)
	if err != nil {
		t.Fatal(err)
	}
	shell := len(mustMarshal(t, &DetailedMomentum{Momentum: ref.List[0].Momentum, AccountBlocks: []*AccountBlock{}}))
	budget := len("[]") + shell + len(mustMarshal(t, receives[0]))
	*n = counts{}
	if _, err := momentumListToDetailedList(l.chain, headers, budget); err != ErrDetailedMomentumTooLarge {
		t.Fatalf("got %v, want ErrDetailedMomentumTooLarge", err)
	}
	if n.read != 2 || n.paired != 2 || n.prefetched != 0 {
		t.Fatalf("read %d and built %d of %d blocks (prefetched %d); want 2", n.read, n.paired, len(receives), n.prefetched)
	}

	// Behind a momentum that fits, the oversized one ends the list with
	// more instead.
	headers, err = l.GetMomentumsByHeight(receiveMomentumHeight-1, 2)
	if err != nil {
		t.Fatal(err)
	}
	prev, prevSizes := reference(t, l, receiveMomentumHeight-1, 1)
	got, err := momentumListToDetailedList(l.chain, headers, encodedListSize(prevSizes, 1)+sizes[0])
	if err != nil {
		t.Fatal(err)
	}
	if len(got.List) != 1 || !got.More || !bytes.Equal(mustMarshal(t, prev.List), mustMarshal(t, got.List)) {
		t.Fatalf("got %d momentums more=%v", len(got.List), got.More)
	}

	// Through the RPC method, the error reaches the caller.
	saved := detailedMomentumsByteBudget
	defer func() { detailedMomentumsByteBudget = saved }()
	detailedMomentumsByteBudget = sizes[0]
	if _, err := l.GetDetailedMomentumsByHeight(receiveMomentumHeight, 1); err != ErrDetailedMomentumTooLarge {
		t.Fatalf("got %v, want ErrDetailedMomentumTooLarge", err)
	}
	detailedMomentumsByteBudget = encodedListSize(sizes, 1)
	if got, err := l.GetDetailedMomentumsByHeight(receiveMomentumHeight, 1); err != nil || len(got.List) != 1 || got.More {
		t.Fatalf("exact budget: %v, %v", got, err)
	}
}

// Through the RPC method, a range larger than the budget comes back
// short with "more":true, and a client that continues from the height
// after the last returned momentum reassembles the full range exactly.
func TestGetDetailedMomentumsByHeight_ContinuesAcrossBudget(t *testing.T) {
	z, l, n := newBudgetFixture(t)
	defer z.StopPanic()

	const height, count = 2, budgetFixtureMomentums
	ref, sizes := reference(t, l, height, count)

	saved := detailedMomentumsByteBudget
	defer func() { detailedMomentumsByteBudget = saved }()
	detailedMomentumsByteBudget = encodedListSize(sizes, 2)

	var all []*DetailedMomentum
	next, left, calls := uint64(height), uint64(count), 0
	for left > 0 {
		*n = counts{}
		got, err := l.GetDetailedMomentumsByHeight(next, left)
		if err != nil {
			t.Fatal(err)
		}
		calls++
		if len(got.List) == 0 {
			t.Fatalf("call %d returned no momentums", calls)
		}
		if n.prefetched != 0 || n.read > (len(got.List)+1)*budgetFixtureBlocksPerMom {
			t.Fatalf("call %d read %d blocks (prefetched %d) for %d momentums returned", calls, n.read, n.prefetched, len(got.List))
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
