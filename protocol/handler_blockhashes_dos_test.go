package protocol

import (
	"testing"

	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/p2p"
	"github.com/zenon-network/go-zenon/protocol/downloader"
)

// GetBlockHashesFromNumberMsg caps the requested count once, but a request whose
// range runs past the frontier recomputes the count from the frontier height.
// Without a second bound that recomputed count is the whole chain, so a tiny
// request makes the receiver load every momentum into memory. The tests below
// drive the real handleMsg entry point over a message pipe against a chain that
// records the count reaching GetBlockHashesFromHash, so the per-request bound is
// asserted on the receiver's actual behaviour rather than on a helper.

// boundRecordingChain is the minimal chainManager the GetBlockHashes* paths need.
// It models a chain of `height` momentums (heights 1..height, genesis at 1) and
// records every GetBlockHashesFromHash call and the largest count it was asked
// for. It deliberately caps the slice it returns so that running these tests
// against the unpatched handler fails on the recorded count instead of trying to
// allocate the whole chain inside the test process.
type boundRecordingChain struct {
	height          uint64
	hashFetchCalls  int
	maxFetchAmount  uint64
	lastFetchAmount uint64
}

func momentumAt(height uint64) *nom.Momentum {
	m := &nom.Momentum{Height: height}
	m.Hash = m.ComputeHash()
	return m
}

func (c *boundRecordingChain) GetBlockByNumber(num uint64) (*nom.Momentum, error) {
	// Genesis is height 1; height 0 and anything past the frontier is a miss,
	// reported as (nil, nil) exactly like the real store.
	if num == 0 || num > c.height {
		return nil, nil
	}
	return momentumAt(num), nil
}

func (c *boundRecordingChain) CurrentBlock() *nom.Momentum { return momentumAt(c.height) }

func (c *boundRecordingChain) GetBlockHashesFromHash(_ types.Hash, amount uint64) ([]types.Hash, error) {
	c.hashFetchCalls++
	c.lastFetchAmount = amount
	if amount > c.maxFetchAmount {
		c.maxFetchAmount = amount
	}
	n := amount
	if n > 2*uint64(downloader.MaxHashFetch) {
		n = 2 * uint64(downloader.MaxHashFetch) // never allocate the whole chain, even unpatched
	}
	return make([]types.Hash, n), nil
}

func (c *boundRecordingChain) HasBlock(types.Hash) bool                  { panic("not used") }
func (c *boundRecordingChain) GetBlock(types.Hash) *nom.DetailedMomentum { panic("not used") }
func (c *boundRecordingChain) Status() (uint64, types.Hash, types.Hash)  { panic("not used") }
func (c *boundRecordingChain) InsertChain([]*nom.DetailedMomentum) (int, error) {
	panic("not used")
}
func (c *boundRecordingChain) VerifyMomentum(*nom.DetailedMomentum) error { panic("not used") }

type blockHashesResult struct {
	hashes   []types.Hash
	answered bool
}

// serveBlockHashesFromNumber sends one GetBlockHashesFromNumberMsg{number,amount}
// to a handler backed by chain and returns the BlockHashesMsg reply.
func serveBlockHashesFromNumber(t *testing.T, chain *boundRecordingChain, number, amount uint64) (blockHashesResult, error) {
	t.Helper()
	return serveBlockHashQuery(t, chain, GetBlockHashesFromNumberMsg, getBlockHashesFromNumberData{number, amount})
}

func serveBlockHashQuery(t *testing.T, chain *boundRecordingChain, code uint64, payload interface{}) (blockHashesResult, error) {
	t.Helper()

	app, net := p2p.MsgPipe()
	defer func() { _ = app.Close() }()

	pm := &ProtocolManager{chainman: chain}
	p := &peer{rw: net, id: "test-peer"}

	sendErr := make(chan error, 1)
	go func() { sendErr <- p2p.Send(app, code, payload) }()

	done := make(chan blockHashesResult, 1)
	go func() {
		msg, err := app.ReadMsg()
		if err != nil {
			done <- blockHashesResult{}
			return
		}
		result := blockHashesResult{answered: true}
		if msg.Code != BlockHashesMsg {
			t.Errorf("reply code %d, want BlockHashesMsg (%d)", msg.Code, BlockHashesMsg)
		}
		if err := msg.Decode(&result.hashes); err != nil {
			t.Errorf("decode reply: %v", err)
		}
		done <- result
	}()

	err := pm.handleMsg(p)
	if err != nil {
		_ = app.Close()
	}
	if serr := <-sendErr; serr != nil {
		t.Fatalf("send request: %v", serr)
	}
	return <-done, err
}

// A degenerate range that misses the frontier must never ask the store for more
// than MaxHashFetch hashes, however large the chain is.
func TestGetBlockHashesFromNumber_BoundsRecomputedAmount(t *testing.T) {
	const frontier = 5_000_000 // far above MaxHashFetch

	cases := []struct {
		name           string
		number, amount uint64
		wantFetchCalls int
		wantMaxAmount  uint64 // 0 means "sink not called"
		wantEmptyReply bool
	}{
		// {0,0} and {1,0}: the zero guard answers empty before any lookup.
		{"zero-zero", 0, 0, 0, 0, true},
		{"one-zero", 1, 0, 0, 0, true},
		// {0,1}: endpoint height 0 misses, the recompute is re-capped to MaxHashFetch.
		{"zero-one", 0, 1, 1, uint64(downloader.MaxHashFetch), false},
		// Number past the frontier: answered empty without reaching the sink.
		{"number-past-frontier", frontier + 5, 10, 0, 0, true},
		// Number near MaxUint64: the Number+Amount-1 addition wraps, but the
		// result cannot bypass the bound or reach the sink with a huge amount.
		{"addition-wrap", ^uint64(0) - 2, 5, 0, 0, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			chain := &boundRecordingChain{height: frontier}
			res, err := serveBlockHashesFromNumber(t, chain, tc.number, tc.amount)
			if err != nil {
				t.Fatalf("handleMsg: %v", err)
			}
			if !res.answered {
				t.Fatal("no reply written")
			}
			if tc.wantEmptyReply && len(res.hashes) != 0 {
				t.Fatalf("want empty reply, got %d hashes", len(res.hashes))
			}
			if chain.hashFetchCalls != tc.wantFetchCalls {
				t.Fatalf("GetBlockHashesFromHash called %d times, want %d", chain.hashFetchCalls, tc.wantFetchCalls)
			}
			if chain.maxFetchAmount != tc.wantMaxAmount {
				t.Fatalf("max amount reaching the sink %d, want %d", chain.maxFetchAmount, tc.wantMaxAmount)
			}
			if chain.maxFetchAmount > uint64(downloader.MaxHashFetch) {
				t.Fatalf("sink asked for %d hashes, exceeds MaxHashFetch %d", chain.maxFetchAmount, downloader.MaxHashFetch)
			}
		})
	}
}

// A legitimate range that crosses the frontier returns exactly the hashes the
// chain holds from the start height to the frontier, still within the cap.
func TestGetBlockHashesFromNumber_ClampsToFrontier(t *testing.T) {
	const frontier = 10
	chain := &boundRecordingChain{height: frontier}

	// Honest downloader request: 512 from height 0 against a 10-high chain.
	res, err := serveBlockHashesFromNumber(t, chain, 0, uint64(downloader.MaxHashFetch))
	if err != nil {
		t.Fatalf("handleMsg: %v", err)
	}
	// Heights 1..10 available from Number 0 → 11 (including the genesis-0 slot
	// the frontier recompute counts), well under the cap.
	const wantAvailable = frontier - 0 + 1
	if chain.lastFetchAmount != wantAvailable {
		t.Fatalf("sink asked for %d hashes, want the %d available", chain.lastFetchAmount, wantAvailable)
	}
	if len(res.hashes) != wantAvailable {
		t.Fatalf("reply carried %d hashes, want %d", len(res.hashes), wantAvailable)
	}
}

// A range fully inside the chain serves exactly the requested count.
func TestGetBlockHashesFromNumber_InRangeServesRequested(t *testing.T) {
	chain := &boundRecordingChain{height: 100}
	res, err := serveBlockHashesFromNumber(t, chain, 5, 3)
	if err != nil {
		t.Fatalf("handleMsg: %v", err)
	}
	if chain.lastFetchAmount != 3 {
		t.Fatalf("sink asked for %d hashes, want the 3 requested", chain.lastFetchAmount)
	}
	if len(res.hashes) != 3 {
		t.Fatalf("reply carried %d hashes, want 3", len(res.hashes))
	}
}
