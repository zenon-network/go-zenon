package protocol

import (
	"io"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/rlp"

	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/p2p"
	"github.com/zenon-network/go-zenon/protocol/downloader"
	"github.com/zenon-network/go-zenon/protocol/fetcher"
)

// The three request codes (GetBlocksMsg, GetBlockHashesMsg and
// GetBlockHashesFromNumberMsg) are bounded per message; the per-peer request
// budget bounds the work one peer can have served over time by pacing it,
// and refuses only a request that could never be covered. These tests drive
// the real handleMsg entry point over a message pipe with an injected clock
// and a recording wait, so admission order, charging, refill, cross-code
// sharing, pacing and the refusal are asserted on the receiver's actual
// behaviour, including what it writes.

// fakeClock is a manually advanced time source for the budget's buckets.
type fakeClock struct{ t time.Time }

func newFakeClock() *fakeClock               { return &fakeClock{t: time.Unix(1_700_000_000, 0)} }
func (c *fakeClock) now() time.Time          { return c.t }
func (c *fakeClock) advance(d time.Duration) { c.t = c.t.Add(d) }

// budgetTestChain serves blocks by hash and hashes by walk, counting the
// store work each handler causes and logging every store event in order,
// so a test can check what the store saw at the moment of each wait.
type budgetTestChain struct {
	lookupCountingChain
	hashWalks int // hashes returned by GetBlockHashesFromHash, summed
	head      *nom.Momentum
	events    []string // "lookup", "height", "frontier", "walk"
}

func (c *budgetTestChain) GetBlock(hash types.Hash) *nom.DetailedMomentum {
	c.events = append(c.events, "lookup")
	return c.lookupCountingChain.GetBlock(hash)
}

func (c *budgetTestChain) GetBlockHashesFromHash(hash types.Hash, amount uint64) ([]types.Hash, error) {
	c.events = append(c.events, "walk")
	hashes := make([]types.Hash, 0, amount)
	for i := uint64(0); i < amount; i++ {
		hashes = append(hashes, types.NewHash([]byte{'w', 'a', 'l', 'k', byte(i >> 8), byte(i)}))
	}
	c.hashWalks += len(hashes)
	return hashes, nil
}

// GetBlockByNumber returns the head for any height up to the head's, and
// nil for a height beyond it, as the chain does.
func (c *budgetTestChain) GetBlockByNumber(n uint64) (*nom.Momentum, error) {
	c.events = append(c.events, "height")
	c.lookups++
	if c.head == nil || n > c.head.Height {
		return nil, nil
	}
	return c.head, nil
}

func (c *budgetTestChain) CurrentBlock() *nom.Momentum {
	c.events = append(c.events, "frontier")
	return c.head
}

// requestReply is what the requesting side saw for one request.
type requestReply struct {
	code    uint64
	payload []byte
}

// budgetHarness wires a ProtocolManager, a peer with an explicit budget on a
// fake clock and a recording wait, and the app side of the pipe. One reader
// goroutine, started here and ended when the pipe closes, pumps every reply
// into replies, so what the handler wrote is observed independently of what
// it returned.
type budgetHarness struct {
	pm      *ProtocolManager
	peer    *peer
	app     p2p.MsgReadWriter
	chain   *budgetTestChain
	clock   *fakeClock
	waits   []time.Duration // every wait the budget asked for, in order
	waitErr error           // returned by the wait when set
	// failWaitAt, when >= 0, makes only that wait (by index) return
	// waitErr; the others elapse normally.
	failWaitAt int
	// lookupsAtWait and eventsAtWait record chain.lookups and the length
	// of the chain's event log at the moment of each wait, so a test can
	// check that admission happened before the store was touched.
	lookupsAtWait []int
	eventsAtWait  []int
	replies       chan requestReply
	close         func()
}

func newBudgetHarness(t *testing.T, chain *budgetTestChain, lookupRate, lookupBurst, byteRate, byteBurst float64) *budgetHarness {
	t.Helper()
	app, net := p2p.MsgPipe()
	clock := newFakeClock()
	h := &budgetHarness{app: app, chain: chain, clock: clock, failWaitAt: -1, replies: make(chan requestReply, 64), close: func() { _ = app.Close() }}
	wait := func(d time.Duration) error {
		index := len(h.waits)
		h.waits = append(h.waits, d)
		h.lookupsAtWait = append(h.lookupsAtWait, chain.lookups)
		h.eventsAtWait = append(h.eventsAtWait, len(chain.events))
		if h.waitErr != nil && (h.failWaitAt < 0 || h.failWaitAt == index) {
			return h.waitErr
		}
		clock.advance(d) // the wait really elapsed
		return nil
	}
	h.pm = &ProtocolManager{chainman: chain}
	h.peer = &peer{rw: net, id: "budget-peer", budget: newRequestBudget("budget-peer", lookupRate, lookupBurst, byteRate, byteBurst, clock.now, wait)}
	go func() {
		for {
			msg, err := app.ReadMsg()
			if err != nil {
				close(h.replies)
				return
			}
			buf, err := io.ReadAll(msg.Payload)
			if err != nil {
				t.Errorf("read reply: %v", err)
			}
			h.replies <- requestReply{code: msg.Code, payload: buf}
		}
	}()
	return h
}

// serveRequest sends one request of the given code, runs handleMsg once on
// the harness peer and returns the reply. A non-nil error means the handler
// asked for the peer to be disconnected; the caller then checks noReply.
func (h *budgetHarness) serveRequest(t *testing.T, code uint64, payload interface{}) (requestReply, error) {
	t.Helper()
	sendErr := make(chan error, 1)
	go func() { sendErr <- p2p.Send(h.app, code, payload) }()
	err := h.pm.handleMsg(h.peer)
	if serr := <-sendErr; serr != nil {
		t.Fatalf("send request: %v", serr)
	}
	if err != nil {
		return requestReply{}, err
	}
	select {
	case r := <-h.replies:
		return r, nil
	case <-time.After(10 * time.Second):
		t.Fatalf("no reply to message code %d within 10 s", code)
		return requestReply{}, nil
	}
}

// noReply fails the test if the handler wrote anything.
func (h *budgetHarness) noReply(t *testing.T) {
	t.Helper()
	select {
	case r := <-h.replies:
		t.Fatalf("expected no reply, got message code %d (%d bytes)", r.code, len(r.payload))
	case <-time.After(100 * time.Millisecond):
	}
}

func (h *budgetHarness) totalWait() time.Duration {
	var total time.Duration
	for _, d := range h.waits {
		total += d
	}
	return total
}

func decodeBlocksReply(t *testing.T, r requestReply) []*nom.DetailedMomentum {
	t.Helper()
	if r.code != BlocksMsg {
		t.Fatalf("expected a BlocksMsg reply, got code %d", r.code)
	}
	var blocks []*nom.DetailedMomentum
	if err := rlp.DecodeBytes(r.payload, &blocks); err != nil {
		t.Fatalf("decode blocks reply: %v", err)
	}
	return blocks
}

func decodeHashesReply(t *testing.T, r requestReply) []types.Hash {
	t.Helper()
	if r.code != BlockHashesMsg {
		t.Fatalf("expected a BlockHashesMsg reply, got code %d", r.code)
	}
	var hashes []types.Hash
	if err := rlp.DecodeBytes(r.payload, &hashes); err != nil {
		t.Fatalf("decode hashes reply: %v", err)
	}
	return hashes
}

func isBudgetError(err error) bool {
	return err != nil && strings.Contains(err.Error(), errorToString[ErrRequestBudgetExceeded])
}

// waitFor is the pacing a deficit of units costs at rate.
func waitFor(units, rate float64) time.Duration {
	return time.Duration(units / rate * float64(time.Second))
}

func expectWaits(t *testing.T, got []time.Duration, want ...time.Duration) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("expected %d waits %v, got %d: %v", len(want), want, len(got), got)
	}
	for i := range want {
		if d := got[i] - want[i]; d < -time.Microsecond || d > time.Microsecond {
			t.Fatalf("wait %d: got %v, want %v", i, got[i], want[i])
		}
	}
}

// --- token bucket -----------------------------------------------------------

func TestTokenBucket_ReserveRefillAndCap(t *testing.T) {
	clock := newFakeClock()
	b := newTokenBucket(10, 100, clock.now) // 10 units/s, burst 100

	if d, ok := b.reserve(100); !ok || d != 0 {
		t.Fatalf("a fresh bucket must cover its full burst at once, got %v %v", d, ok)
	}
	if d, ok := b.reserve(5); !ok || d != 500*time.Millisecond {
		t.Fatalf("5 units short at 10/s must wait 500ms, got %v %v", d, ok)
	}
	clock.advance(500 * time.Millisecond) // the deficit is repaid
	if d, ok := b.reserve(1); !ok || d != 100*time.Millisecond {
		t.Fatalf("the bucket must be exactly empty after the repaid deficit, got %v %v", d, ok)
	}
	clock.advance(time.Hour)
	if d, ok := b.reserve(100); !ok || d != 0 {
		t.Fatalf("refill must cap at the burst and cover a full burst, got %v %v", d, ok)
	}
}

func TestTokenBucket_RequestAboveBurstIsRefusedWithoutCharge(t *testing.T) {
	clock := newFakeClock()
	b := newTokenBucket(1, 10, clock.now)
	if _, ok := b.reserve(11); ok {
		t.Fatal("a reserve above the burst must be refused")
	}
	if d, ok := b.reserve(10); !ok || d != 0 {
		t.Fatalf("a refused reserve must not charge anything, got %v %v", d, ok)
	}
}

func TestTokenBucket_DeficitWithoutRateIsRefused(t *testing.T) {
	clock := newFakeClock()
	b := newTokenBucket(0, 10, clock.now)
	if d, ok := b.reserve(10); !ok || d != 0 {
		t.Fatalf("the burst itself must be covered, got %v %v", d, ok)
	}
	if _, ok := b.reserve(1); ok {
		t.Fatal("a deficit that no refill will ever repay must be refused")
	}
}

func TestTokenBucket_BackwardClockDoesNotRefill(t *testing.T) {
	clock := newFakeClock()
	b := newTokenBucket(10, 100, clock.now)
	if d, _ := b.reserve(100); d != 0 {
		t.Fatalf("fresh bucket: %v", d)
	}
	clock.advance(-time.Hour) // clock jumps back
	if d, ok := b.reserve(1); !ok || d != 100*time.Millisecond {
		t.Fatalf("a backward clock must credit nothing, got %v %v", d, ok)
	}
	clock.advance(time.Hour) // back to where it was: still no elapsed time
	if d, ok := b.reserve(1); !ok || d != 200*time.Millisecond {
		t.Fatalf("returning to the last recorded time must credit nothing either, got %v %v", d, ok)
	}
}

// --- handler: GetBlocksMsg --------------------------------------------------

// A peer that streams requests for hashes the receiver does not have is
// paced once its burst is spent: the requests within the burst are served
// without a wait, the next one waits for its deficit before its first hash
// is looked up, and is then served. Nothing is refused and nothing reaches
// the store before the wait.
func TestHandleRequests_UnknownHashStreamIsPacedOnceBudgetRunsDry(t *testing.T) {
	chain := &budgetTestChain{}
	// 1 unit per message + RequestMissCost per unknown hash; 10 hashes per
	// request. The burst allows exactly three such requests plus one unit:
	// the fourth pays its message unit and is one unit short at its first
	// hash, which must be admitted (and waited for) before it is looked up.
	perRequest := 1 + 10*RequestMissCost
	const rate = 100.0
	h := newBudgetHarness(t, chain, rate, 3*perRequest+1, 1<<30, 1<<30)
	defer h.close()

	for i := 0; i < 3; i++ {
		r, err := h.serveRequest(t, GetBlocksMsg, unknownHashes(10))
		if err != nil {
			t.Fatalf("request %d within the burst: %v", i, err)
		}
		if blocks := decodeBlocksReply(t, r); len(blocks) != 0 {
			t.Fatalf("request %d: expected an empty reply, got %d blocks", i, len(blocks))
		}
	}
	expectWaits(t, h.waits)
	lookupsBefore := chain.lookups

	r, err := h.serveRequest(t, GetBlocksMsg, unknownHashes(10))
	if err != nil {
		t.Fatalf("the paced request must still be served, got: %v", err)
	}
	if blocks := decodeBlocksReply(t, r); len(blocks) != 0 {
		t.Fatalf("expected an empty reply, got %d blocks", len(blocks))
	}
	// First wait: one unit short at the first hash. Later waits: each miss
	// surcharge and each further hash is paid as it comes.
	if len(h.waits) == 0 || h.waits[0] != waitFor(1, rate) {
		t.Fatalf("expected the first wait to be one unit at the rate (%v), got %v", waitFor(1, rate), h.waits)
	}
	if h.lookupsAtWait[0] != lookupsBefore {
		t.Fatalf("the first wait must happen before the store is touched: %d lookups before, %d at the wait", lookupsBefore, h.lookupsAtWait[0])
	}
	if chain.lookups != lookupsBefore+10 {
		t.Fatalf("the paced request must then be served in full: %d lookups, want %d", chain.lookups-lookupsBefore, 10)
	}
	if total, want := h.totalWait(), waitFor(perRequest-1, rate); math.Abs(float64(total-want)) > float64(time.Microsecond) {
		t.Fatalf("the fourth request must wait for exactly its deficit: %v, want %v", total, want)
	}
}

// A hit costs one unit, a miss costs RequestMissCost: a request that fits
// exactly is served without a wait, one unit more waits for exactly one
// unit. This pins the miss weighting.
func TestHandleRequests_MissCostsMoreThanHit(t *testing.T) {
	known, knownHashes := knownBlocks(4)
	request := append(append([]types.Hash{}, knownHashes...), unknownHashes(2)...)
	// 1 (message) + 4 hits + 2 misses*RequestMissCost fits exactly.
	exact := 1 + 4 + 2*RequestMissCost
	const rate = 50.0

	h := newBudgetHarness(t, &budgetTestChain{lookupCountingChain: lookupCountingChain{known: known}}, rate, exact, 1<<30, 1<<30)
	defer h.close()
	r, err := h.serveRequest(t, GetBlocksMsg, request)
	if err != nil {
		t.Fatalf("a request that fits exactly: %v", err)
	}
	if blocks := decodeBlocksReply(t, r); len(blocks) != 4 {
		t.Fatalf("expected the 4 known blocks, got %d", len(blocks))
	}
	expectWaits(t, h.waits)

	h2 := newBudgetHarness(t, &budgetTestChain{lookupCountingChain: lookupCountingChain{known: known}}, rate, exact-1, 1<<30, 1<<30)
	defer h2.close()
	if _, err := h2.serveRequest(t, GetBlocksMsg, request); err != nil {
		t.Fatalf("one unit short must pace, not refuse: %v", err)
	}
	expectWaits(t, h2.waits, waitFor(1, rate))
}

// An honest downloader asks for up to MaxBlockFetch known blocks per request,
// one in flight at a time. At the default budget, a peer doing that ten
// times a second indefinitely is never paced.
func TestHandleRequests_HonestSyncPaceIsNeverPaced(t *testing.T) {
	known, knownHashes := knownBlocks(downloader.MaxBlockFetch)
	chain := &budgetTestChain{lookupCountingChain: lookupCountingChain{known: known}}
	h := newBudgetHarness(t, chain, RequestLookupRate, RequestLookupBurst, RequestReplyByteRate, RequestReplyByteBurst)
	defer h.close()

	const requests = 2000 // 200 s of sync at 10 requests/s
	for i := 0; i < requests; i++ {
		r, err := h.serveRequest(t, GetBlocksMsg, knownHashes)
		if err != nil {
			t.Fatalf("honest request %d: %v", i, err)
		}
		if blocks := decodeBlocksReply(t, r); len(blocks) != downloader.MaxBlockFetch {
			t.Fatalf("request %d: expected %d blocks, got %d", i, downloader.MaxBlockFetch, len(blocks))
		}
		h.clock.advance(100 * time.Millisecond)
	}
	expectWaits(t, h.waits)
}

// On a fast link the downloader fetches hashes flat out: one full request
// after another with no pause, up to its queue limit. At the default budget
// such a peer is paced after its burst but never refused, every request is
// answered in full, and no single wait exceeds one request's cost at the
// rate, which is far inside the downloader's hash timeout.
func TestHandleRequests_FlatOutHashSyncIsPacedNotRefused(t *testing.T) {
	known, knownHashes := knownBlocks(1)
	chain := &budgetTestChain{lookupCountingChain: lookupCountingChain{known: known}}
	h := newBudgetHarness(t, chain, RequestLookupRate, RequestLookupBurst, RequestReplyByteRate, RequestReplyByteBurst)
	defer h.close()

	const requests = 512 // maxQueuedHashes / MaxHashFetch: a whole queue fill back to back
	perRequest := 1 + float64(downloader.MaxHashFetch)
	for i := 0; i < requests; i++ {
		r, err := h.serveRequest(t, GetBlockHashesMsg, getBlockHashesData{knownHashes[0], uint64(downloader.MaxHashFetch)})
		if err != nil {
			t.Fatalf("flat-out hash request %d: %v", i, err)
		}
		if hashes := decodeHashesReply(t, r); len(hashes) != downloader.MaxHashFetch {
			t.Fatalf("request %d: expected %d hashes, got %d", i, downloader.MaxHashFetch, len(hashes))
		}
	}
	maxWait := waitFor(perRequest, RequestLookupRate)
	for i, d := range h.waits {
		if d > maxWait+time.Microsecond {
			t.Fatalf("wait %d of %v exceeds one request's cost at the rate (%v)", i, d, maxWait)
		}
	}
	if maxWait > time.Second {
		t.Fatalf("one request's pacing (%v) must stay well inside the downloader's 5 s hash timeout", maxWait)
	}
	spent := requests * perRequest
	if total, want := h.totalWait(), waitFor(spent-RequestLookupBurst, RequestLookupRate); math.Abs(float64(total-want)) > float64(time.Millisecond) {
		t.Fatalf("total pacing %v, want the deficit beyond the burst at the rate (%v)", total, want)
	}
}

// Large replies flat out: SoftResponseLimit-sized replies one after another
// are paced by the byte bucket after its burst, never refused, and each
// wait stays inside one reply's cost at the byte rate.
func TestHandleRequests_FlatOutLargeRepliesArePacedNotRefused(t *testing.T) {
	big := makeLargeMomentum(types.NewHash([]byte("big")), SoftResponseLimit)
	chain := &budgetTestChain{lookupCountingChain: lookupCountingChain{known: map[types.Hash]*nom.DetailedMomentum{big.Momentum.Hash: big}}}
	h := newBudgetHarness(t, chain, RequestLookupRate, RequestLookupBurst, RequestReplyByteRate, RequestReplyByteBurst)
	defer h.close()

	const requests = 48 // ~96 MiB of replies against a 64 MiB burst
	var replyBytes int
	for i := 0; i < requests; i++ {
		r, err := h.serveRequest(t, GetBlocksMsg, []types.Hash{big.Momentum.Hash})
		if err != nil {
			t.Fatalf("large reply %d: %v", i, err)
		}
		if blocks := decodeBlocksReply(t, r); len(blocks) != 1 {
			t.Fatalf("request %d: expected the block, got %d", i, len(blocks))
		}
		replyBytes += len(r.payload)
	}
	if len(h.waits) == 0 {
		t.Fatal("expected the byte bucket to pace once its burst was spent")
	}
	maxWait := waitFor(float64(replyBytes)/requests, RequestReplyByteRate)
	for i, d := range h.waits {
		if d > maxWait+time.Millisecond {
			t.Fatalf("wait %d of %v exceeds one reply's cost at the byte rate (%v)", i, d, maxWait)
		}
	}
}

// A paced budget refills: once the deficit is repaid by the clock, the next
// request is served without a wait.
func TestHandleRequests_BudgetRefillsOverTime(t *testing.T) {
	known, knownHashes := knownBlocks(2)
	chain := &budgetTestChain{lookupCountingChain: lookupCountingChain{known: known}}
	// 3 units per request (1 message + 2 hits); burst 3, rate 3/s.
	h := newBudgetHarness(t, chain, 3, 3, 1<<30, 1<<30)
	defer h.close()

	if _, err := h.serveRequest(t, GetBlocksMsg, knownHashes); err != nil {
		t.Fatalf("first request: %v", err)
	}
	expectWaits(t, h.waits)
	h.clock.advance(500 * time.Millisecond) // 1.5 units back
	if _, err := h.serveRequest(t, GetBlocksMsg, knownHashes); err != nil {
		t.Fatalf("second request: %v", err)
	}
	// Message unit covered (0.5 left), first hit waits for 0.5 unit, the
	// second hit for a full unit.
	expectWaits(t, h.waits, waitFor(0.5, 3), waitFor(1, 3))
	h.clock.advance(time.Second) // a full burst again
	if _, err := h.serveRequest(t, GetBlocksMsg, knownHashes); err != nil {
		t.Fatalf("third request: %v", err)
	}
	expectWaits(t, h.waits, waitFor(0.5, 3), waitFor(1, 3))
}

// A reply larger than the byte bucket can ever hold is refused: the handler
// returns the budget error, which disconnects the peer, and writes nothing.
func TestHandleRequests_ReplyAboveByteBurstIsRefusedAndNotSent(t *testing.T) {
	big := makeLargeMomentum(types.NewHash([]byte("big")), 64*1024)
	chain := &budgetTestChain{lookupCountingChain: lookupCountingChain{known: map[types.Hash]*nom.DetailedMomentum{big.Momentum.Hash: big}}}
	h := newBudgetHarness(t, chain, 1<<30, 1<<30, 1<<20, 32*1024)
	defer h.close()

	_, err := h.serveRequest(t, GetBlocksMsg, []types.Hash{big.Momentum.Hash})
	if !isBudgetError(err) {
		t.Fatalf("expected the budget error for a reply above the byte burst, got: %v", err)
	}
	h.noReply(t)
	expectWaits(t, h.waits)
}

// A reply within the byte burst but beyond the current balance is paced by
// the byte bucket and then sent.
func TestHandleRequests_ReplyBytesArePaced(t *testing.T) {
	big := makeLargeMomentum(types.NewHash([]byte("big")), 16*1024)
	chain := &budgetTestChain{lookupCountingChain: lookupCountingChain{known: map[types.Hash]*nom.DetailedMomentum{big.Momentum.Hash: big}}}
	const byteRate = 8 * 1024.0
	h := newBudgetHarness(t, chain, 1<<30, 1<<30, byteRate, 20*1024)
	defer h.close()

	r1, err := h.serveRequest(t, GetBlocksMsg, []types.Hash{big.Momentum.Hash})
	if err != nil {
		t.Fatalf("first reply: %v", err)
	}
	expectWaits(t, h.waits)
	r2, err := h.serveRequest(t, GetBlocksMsg, []types.Hash{big.Momentum.Hash})
	if err != nil {
		t.Fatalf("second reply must be paced, not refused: %v", err)
	}
	if len(r1.payload) != len(r2.payload) || len(decodeBlocksReply(t, r2)) != 1 {
		t.Fatal("the paced reply must be sent in full")
	}
	if len(h.waits) != 1 || h.waits[0] <= 0 {
		t.Fatalf("expected exactly one byte wait, got %v", h.waits)
	}
}

// The wait ends with the peer: when the connection goes away while a request
// is paced, the handler returns that error and sends nothing.
func TestHandleRequests_PacedWaitAbandonedWhenPeerCloses(t *testing.T) {
	known, knownHashes := knownBlocks(2)
	chain := &budgetTestChain{lookupCountingChain: lookupCountingChain{known: known}}
	h := newBudgetHarness(t, chain, 1, 2, 1<<30, 1<<30) // 3 units needed, 2 held
	defer h.close()
	h.waitErr = io.ErrClosedPipe

	_, err := h.serveRequest(t, GetBlocksMsg, knownHashes)
	if err != io.ErrClosedPipe {
		t.Fatalf("expected the wait's error to end the request, got: %v", err)
	}
	h.noReply(t)
}

// --- handler: hash requests share the same budget --------------------------

// GetBlockHashesMsg is charged the clamped amount it will walk, from the
// same bucket as GetBlocksMsg, before the chain is touched.
func TestHandleRequests_HashRequestsDrawFromTheSharedBudget(t *testing.T) {
	known, knownHashes := knownBlocks(1)
	chain := &budgetTestChain{lookupCountingChain: lookupCountingChain{known: known}}
	// One full hash request (1 + MaxHashFetch) plus one two-unit block
	// request fit; a second hash request is one full request short.
	hashCost := 1 + float64(downloader.MaxHashFetch)
	const rate = 1000.0
	h := newBudgetHarness(t, chain, rate, hashCost+2, 1<<30, 1<<30)
	defer h.close()

	r, err := h.serveRequest(t, GetBlockHashesMsg, getBlockHashesData{knownHashes[0], uint64(downloader.MaxHashFetch)})
	if err != nil {
		t.Fatalf("first hash request: %v", err)
	}
	if hashes := decodeHashesReply(t, r); len(hashes) != downloader.MaxHashFetch {
		t.Fatalf("expected %d hashes, got %d", downloader.MaxHashFetch, len(hashes))
	}
	if _, err := h.serveRequest(t, GetBlocksMsg, knownHashes); err != nil {
		t.Fatalf("block request after the hash request: %v", err)
	}
	expectWaits(t, h.waits)

	walksBefore := chain.hashWalks
	if _, err := h.serveRequest(t, GetBlockHashesMsg, getBlockHashesData{knownHashes[0], uint64(downloader.MaxHashFetch)}); err != nil {
		t.Fatalf("second hash request must be paced, not refused: %v", err)
	}
	expectWaits(t, h.waits, waitFor(hashCost, rate))
	if h.eventsAtWait[0] != len(chain.events)-1 || chain.events[len(chain.events)-1] != "walk" {
		t.Fatalf("the wait must precede the walk: events at wait %d, events now %v", h.eventsAtWait[0], chain.events)
	}
	if chain.hashWalks != walksBefore+downloader.MaxHashFetch {
		t.Fatalf("the paced request must then walk in full: %d", chain.hashWalks-walksBefore)
	}
}

// The amount is clamped to MaxHashFetch before it is charged, so an
// oversized request costs the clamped amount, not the requested one.
func TestHandleRequests_HashRequestIsChargedTheClampedAmount(t *testing.T) {
	known, knownHashes := knownBlocks(1)
	chain := &budgetTestChain{lookupCountingChain: lookupCountingChain{known: known}}
	h := newBudgetHarness(t, chain, 1, 1+float64(downloader.MaxHashFetch), 1<<30, 1<<30)
	defer h.close()

	r, err := h.serveRequest(t, GetBlockHashesMsg, getBlockHashesData{knownHashes[0], 1 << 40})
	if err != nil {
		t.Fatalf("clamped request must fit a budget of its clamped cost: %v", err)
	}
	if hashes := decodeHashesReply(t, r); len(hashes) != downloader.MaxHashFetch {
		t.Fatalf("expected %d hashes, got %d", downloader.MaxHashFetch, len(hashes))
	}
	expectWaits(t, h.waits)
}

// GetBlockHashesFromNumberMsg costs the message, the height lookup and one
// unit per hash the walk visits, charged before the walk from the shared
// bucket.
func TestHandleRequests_HashesFromNumberDrawFromTheSharedBudget(t *testing.T) {
	head := &nom.Momentum{Height: 1000}
	head.Hash = head.ComputeHash()
	chain := &budgetTestChain{head: head}
	// 1 message + 1 height lookup + 100 walked fits exactly.
	const rate = 100.0
	h := newBudgetHarness(t, chain, rate, 102, 1<<30, 1<<30)
	defer h.close()

	r, err := h.serveRequest(t, GetBlockHashesFromNumberMsg, getBlockHashesFromNumberData{1, 100})
	if err != nil {
		t.Fatalf("first from-number request: %v", err)
	}
	if hashes := decodeHashesReply(t, r); len(hashes) != 100 {
		t.Fatalf("expected 100 hashes, got %d", len(hashes))
	}
	expectWaits(t, h.waits)

	walksBefore := chain.hashWalks
	if _, err := h.serveRequest(t, GetBlockHashesFromNumberMsg, getBlockHashesFromNumberData{1, 10}); err != nil {
		t.Fatalf("second from-number request must be paced, not refused: %v", err)
	}
	// Two units for message + height lookup, then ten for the walk, each
	// paced from an empty bucket.
	expectWaits(t, h.waits, waitFor(2, rate), waitFor(10, rate))
	events := chain.events[h.eventsAtWait[0]:]
	if len(events) != 2 || events[0] != "height" || events[1] != "walk" {
		t.Fatalf("expected the first wait before the height lookup and the second between it and the walk, store saw %v", events)
	}
	if h.eventsAtWait[1] != h.eventsAtWait[0]+1 {
		t.Fatalf("the walk's wait must come after the height lookup and before the walk: waits at %v", h.eventsAtWait)
	}
	if chain.hashWalks != walksBefore+10 {
		t.Fatalf("the paced walk must then run in full: %d", chain.hashWalks-walksBefore)
	}
}

// When the requested range runs past the head, the handler reads the
// frontier to shorten it: that read is a lookup and is charged before it
// happens, with the walk charged after it, for what it will visit.
func TestHandleRequests_MissingHeightFallbackIsCharged(t *testing.T) {
	head := &nom.Momentum{Height: 1000}
	head.Hash = head.ComputeHash()
	chain := &budgetTestChain{head: head}
	const rate = 100.0
	// The burst covers one in-range request of 11 (2 + 11 = 13) exactly, so
	// the fallback request that follows is charged from an empty bucket and
	// every one of its charges shows up as a wait.
	h := newBudgetHarness(t, chain, rate, 13, 1<<30, 1<<30)
	defer h.close()
	if _, err := h.serveRequest(t, GetBlockHashesFromNumberMsg, getBlockHashesFromNumberData{1, 11}); err != nil {
		t.Fatalf("in-range request: %v", err)
	}
	expectWaits(t, h.waits)
	chain.events = nil

	// 990 + 20 - 1 is past the head: the fallback shortens the walk to 11.
	r, err := h.serveRequest(t, GetBlockHashesFromNumberMsg, getBlockHashesFromNumberData{990, 20})
	if err != nil {
		t.Fatalf("fallback request: %v", err)
	}
	if hashes := decodeHashesReply(t, r); len(hashes) != 11 {
		t.Fatalf("expected 11 hashes up to the head, got %d", len(hashes))
	}
	// Message + height lookup, the frontier read, then the shortened walk.
	expectWaits(t, h.waits, waitFor(2, rate), waitFor(1, rate), waitFor(11, rate))
	if got := chain.events; len(got) != 3 || got[0] != "height" || got[1] != "frontier" || got[2] != "walk" {
		t.Fatalf("store events %v, want height, frontier, walk", got)
	}
	if h.eventsAtWait[0] != 0 || h.eventsAtWait[1] != 1 || h.eventsAtWait[2] != 2 {
		t.Fatalf("each charge must precede the store event it pays for: waits at events %v", h.eventsAtWait)
	}

	// Entirely past the head: height lookup and frontier read, empty reply,
	// no walk.
	chain.events = nil
	r, err = h.serveRequest(t, GetBlockHashesFromNumberMsg, getBlockHashesFromNumberData{1001, 1})
	if err != nil {
		t.Fatalf("past-the-head request: %v", err)
	}
	if hashes := decodeHashesReply(t, r); len(hashes) != 0 {
		t.Fatalf("expected an empty reply past the head, got %d hashes", len(hashes))
	}
	if got := chain.events; len(got) != 2 || got[0] != "height" || got[1] != "frontier" {
		t.Fatalf("store events %v, want height, frontier and no walk", got)
	}
}

// The downloader's actual sync path is GetBlockHashesFromNumberMsg: a whole
// queue fill of full requests back to back is paced, never refused, every
// wait within one request's cost.
func TestHandleRequests_FlatOutFromNumberSyncIsPacedNotRefused(t *testing.T) {
	head := &nom.Momentum{Height: 1 << 40}
	head.Hash = head.ComputeHash()
	chain := &budgetTestChain{head: head}
	h := newBudgetHarness(t, chain, RequestLookupRate, RequestLookupBurst, RequestReplyByteRate, RequestReplyByteBurst)
	defer h.close()

	const requests = 512
	perRequest := 2 + float64(downloader.MaxHashFetch)
	from := uint64(1)
	for i := 0; i < requests; i++ {
		r, err := h.serveRequest(t, GetBlockHashesFromNumberMsg, getBlockHashesFromNumberData{from, uint64(downloader.MaxHashFetch)})
		if err != nil {
			t.Fatalf("flat-out from-number request %d: %v", i, err)
		}
		if hashes := decodeHashesReply(t, r); len(hashes) != downloader.MaxHashFetch {
			t.Fatalf("request %d: expected %d hashes, got %d", i, downloader.MaxHashFetch, len(hashes))
		}
		from += uint64(downloader.MaxHashFetch)
	}
	maxWait := waitFor(perRequest, RequestLookupRate)
	for i, d := range h.waits {
		if d > maxWait+time.Microsecond {
			t.Fatalf("wait %d of %v exceeds one request's cost at the rate (%v)", i, d, maxWait)
		}
	}
	if total, want := h.totalWait(), waitFor(requests*perRequest-RequestLookupBurst, RequestLookupRate); math.Abs(float64(total-want)) > float64(time.Millisecond) {
		t.Fatalf("total pacing %v, want the deficit beyond the burst at the rate (%v)", total, want)
	}
}

// Honest traffic is concurrent as well as flat out: the downloader keeps a
// hash request and a block request in flight, and the fetcher sends a batch
// per timer tick without waiting for the last; production RequestBlocks
// splits a fetcher batch of HashLimit hashes into two messages of
// MaxBlockFetch. A request then waits behind the replies the same peer has
// queued ahead of it on the connection. mixedFlatOutTraffic sends that
// outstanding set over and over with no pause, checks every reply in full,
// and returns the total wait of each set, which is the most any one request
// of the set can be delayed by the others.
func mixedFlatOutTraffic(t *testing.T, withLargeReply bool) (h *budgetHarness, perSet []time.Duration) {
	t.Helper()
	known, knownHashes := knownBlocks(fetcher.HashLimit)
	big := makeLargeMomentum(types.NewHash([]byte("big")), SoftResponseLimit)
	known[big.Momentum.Hash] = big
	head := &nom.Momentum{Height: 1 << 40}
	head.Hash = head.ComputeHash()
	chain := &budgetTestChain{lookupCountingChain: lookupCountingChain{known: known}, head: head}
	h = newBudgetHarness(t, chain, RequestLookupRate, RequestLookupBurst, RequestReplyByteRate, RequestReplyByteBurst)

	type request struct {
		code    uint64
		payload interface{}
		check   func(requestReply)
	}
	// wantBlocks checks the reply holds exactly the requested blocks, in
	// order, each identical to its fixture once encoded.
	wantBlocks := func(hashes []types.Hash) func(requestReply) {
		return func(r requestReply) {
			blocks := decodeBlocksReply(t, r)
			if len(blocks) != len(hashes) {
				t.Fatalf("expected %d blocks in the reply, got %d", len(hashes), len(blocks))
			}
			for i, block := range blocks {
				got, err := rlp.EncodeToBytes(block)
				if err != nil {
					t.Fatal(err)
				}
				want, err := rlp.EncodeToBytes(known[hashes[i]])
				if err != nil {
					t.Fatal(err)
				}
				if string(got) != string(want) {
					t.Fatalf("block %d of the reply differs from the fixture for %x", i, hashes[i][:4])
				}
			}
		}
	}
	// The from-number handler walks back from the last block and reverses,
	// so the reply is the fake walk's hashes in reverse order.
	wantHashes := func(amount int) func(requestReply) {
		return func(r requestReply) {
			hashes := decodeHashesReply(t, r)
			if len(hashes) != amount {
				t.Fatalf("expected %d hashes, got %d", amount, len(hashes))
			}
			for i, h := range hashes {
				j := uint64(amount - 1 - i)
				if want := types.NewHash([]byte{'w', 'a', 'l', 'k', byte(j >> 8), byte(j)}); h != want {
					t.Fatalf("hash %d of the reply is %x, want walk hash %d", i, h[:4], j)
				}
			}
		}
	}
	set := []request{
		{GetBlockHashesFromNumberMsg, getBlockHashesFromNumberData{1, uint64(downloader.MaxHashFetch)}, wantHashes(downloader.MaxHashFetch)},         // downloader hashes
		{GetBlocksMsg, knownHashes[:downloader.MaxBlockFetch], wantBlocks(knownHashes[:downloader.MaxBlockFetch])},                                   // downloader blocks
		{GetBlocksMsg, knownHashes[:downloader.MaxBlockFetch], wantBlocks(knownHashes[:downloader.MaxBlockFetch])},                                   // fetcher batch, first half
		{GetBlocksMsg, knownHashes[downloader.MaxBlockFetch:fetcher.HashLimit], wantBlocks(knownHashes[downloader.MaxBlockFetch:fetcher.HashLimit])}, // fetcher batch, second half
	}
	if withLargeReply {
		set = append(set, request{GetBlocksMsg, []types.Hash{big.Momentum.Hash}, wantBlocks([]types.Hash{big.Momentum.Hash})}) // a full-size reply
	}
	const sets = 64 // long past the bursts either way
	for c := 0; c < sets; c++ {
		before := len(h.waits)
		for _, req := range set {
			r, err := h.serveRequest(t, req.code, req.payload)
			if err != nil {
				t.Fatalf("set %d code %d: %v", c, req.code, err)
			}
			req.check(r)
		}
		var d time.Duration
		for _, w := range h.waits[before:] {
			d += w
		}
		perSet = append(perSet, d)
	}
	return h, perSet
}

// Lookup-heavy mix (small replies): the lookup bucket paces after its
// burst, and no request waits longer than a second behind the rest of the
// outstanding set, far inside hashTTL (5 s) and blockHardTTL (9 s).
func TestHandleRequests_MixedFlatOutLookupTrafficStaysInsideTimeouts(t *testing.T) {
	h, perSet := mixedFlatOutTraffic(t, false)
	defer h.close()
	if h.totalWait() == 0 {
		t.Fatal("expected the lookup bucket to pace once its burst was spent")
	}
	// Every wait here is a lookup wait: the replies are far below the byte
	// burst in total (64 sets of ~3 small replies).
	for i, d := range perSet {
		if d >= time.Second {
			t.Fatalf("set %d waited %v in total; must stay under 1 s", i, d)
		}
	}
	// Steady state: once the burst is gone, a set costs its lookup units at
	// the rate.
	perSetUnits := (2 + float64(downloader.MaxHashFetch)) + 3*(1+float64(downloader.MaxBlockFetch))
	if last, want := perSet[len(perSet)-1], waitFor(perSetUnits, RequestLookupRate); math.Abs(float64(last-want)) > float64(time.Millisecond) {
		t.Fatalf("steady-state set wait %v, want its lookup cost at the rate (%v)", last, want)
	}
}

// Byte-heavy mix (a SoftResponseLimit reply in every set): the byte bucket
// paces too, and the bound still holds.
func TestHandleRequests_MixedFlatOutByteTrafficStaysInsideTimeouts(t *testing.T) {
	h, perSet := mixedFlatOutTraffic(t, true)
	defer h.close()
	// Hash request, three 128-block requests, and the large request (one
	// message unit, one hit): 903 lookup units per set.
	perSetUnits := (2 + float64(downloader.MaxHashFetch)) + 3*(1+float64(downloader.MaxBlockFetch)) + 2
	lookupOnly := waitFor(perSetUnits, RequestLookupRate)
	if last := perSet[len(perSet)-1]; last <= lookupOnly {
		t.Fatalf("steady-state set wait %v does not exceed the lookup cost alone (%v): the byte bucket did not pace", last, lookupOnly)
	}
	for i, d := range perSet {
		if d >= time.Second {
			t.Fatalf("set %d waited %v in total; must stay under 1 s", i, d)
		}
	}
}

// A wait abandoned by the peer leaving stops the handler wherever it was:
// before a reply is written, before a frontier read, before a walk.
func TestHandleRequests_AbandonedWaitStopsBeforeTheNextStep(t *testing.T) {
	head := &nom.Momentum{Height: 1000}
	head.Hash = head.ComputeHash()
	big := makeLargeMomentum(types.NewHash([]byte("big")), 16*1024)

	// Reply-byte wait fails: nothing is written.
	chain := &budgetTestChain{lookupCountingChain: lookupCountingChain{known: map[types.Hash]*nom.DetailedMomentum{big.Momentum.Hash: big}}, head: head}
	// Burst 20 KiB covers one 16 KiB reply; the second is paced.
	h := newBudgetHarness(t, chain, 1<<30, 1<<30, 1024, 20*1024)
	defer h.close()
	if _, err := h.serveRequest(t, GetBlocksMsg, []types.Hash{big.Momentum.Hash}); err != nil {
		t.Fatalf("first reply: %v", err)
	}
	h.waitErr = io.ErrClosedPipe
	if _, err := h.serveRequest(t, GetBlocksMsg, []types.Hash{big.Momentum.Hash}); err != io.ErrClosedPipe {
		t.Fatalf("expected the byte wait's error, got: %v", err)
	}
	h.noReply(t)

	// From-number: the frontier read's wait fails: no frontier read, no walk.
	chain2 := &budgetTestChain{head: head}
	h2 := newBudgetHarness(t, chain2, 1, 2, 1<<30, 1<<30) // message + height covered; frontier waits
	defer h2.close()
	h2.waitErr = io.ErrClosedPipe
	if _, err := h2.serveRequest(t, GetBlockHashesFromNumberMsg, getBlockHashesFromNumberData{990, 20}); err != io.ErrClosedPipe {
		t.Fatalf("expected the frontier wait's error, got: %v", err)
	}
	if got := chain2.events; len(got) != 1 || got[0] != "height" {
		t.Fatalf("store events %v, want the height lookup only", got)
	}
	h2.noReply(t)

	// From-number: the walk's wait fails: no walk. The burst (14) covers an
	// in-range request of 11 (13 units) and leaves one unit, so the fallback
	// request waits for its message and height lookup, then for the frontier
	// read, then for the walk; only the third wait fails.
	chain3 := &budgetTestChain{head: head}
	h3 := newBudgetHarness(t, chain3, 1, 14, 1<<30, 1<<30)
	defer h3.close()
	if _, err := h3.serveRequest(t, GetBlockHashesFromNumberMsg, getBlockHashesFromNumberData{1, 11}); err != nil {
		t.Fatalf("in-range request: %v", err)
	}
	chain3.events = nil
	h3.waitErr, h3.failWaitAt = io.ErrClosedPipe, 2
	if _, err := h3.serveRequest(t, GetBlockHashesFromNumberMsg, getBlockHashesFromNumberData{990, 20}); err != io.ErrClosedPipe {
		t.Fatalf("expected the walk wait's error, got: %v", err)
	}
	if got := chain3.events; len(got) != 2 || got[0] != "height" || got[1] != "frontier" {
		t.Fatalf("store events %v, want height and frontier, no walk", got)
	}
	if len(h3.waits) != 3 {
		t.Fatalf("expected three waits (message+height, frontier, walk), got %v", h3.waits)
	}
	h3.noReply(t)
}

// Both hash handlers charge 33 bytes per hash (the RLP string header and
// the hash) against the byte bucket.
func TestHandleRequests_HashReplyBytesAreCharged(t *testing.T) {
	head := &nom.Momentum{Height: 1000}
	head.Hash = head.ComputeHash()
	known, knownHashes := knownBlocks(1)
	const hashes = 10
	const cost = hashes * (types.HashSize + 1)
	const byteRate = 33.0

	chain := &budgetTestChain{lookupCountingChain: lookupCountingChain{known: known}, head: head}
	h := newBudgetHarness(t, chain, 1<<30, 1<<30, byteRate, cost)
	defer h.close()
	if _, err := h.serveRequest(t, GetBlockHashesMsg, getBlockHashesData{knownHashes[0], hashes}); err != nil {
		t.Fatalf("by-hash reply that fits exactly: %v", err)
	}
	expectWaits(t, h.waits)
	if _, err := h.serveRequest(t, GetBlockHashesFromNumberMsg, getBlockHashesFromNumberData{1, hashes}); err != nil {
		t.Fatalf("from-number reply from an empty byte bucket: %v", err)
	}
	expectWaits(t, h.waits, waitFor(cost, byteRate))

	// One byte short: a burst of 2*cost-1 covers the first reply and leaves
	// the second exactly one byte short, which pins the per-hash size.
	h2 := newBudgetHarness(t, &budgetTestChain{lookupCountingChain: lookupCountingChain{known: known}, head: head}, 1<<30, 1<<30, byteRate, 2*cost-1)
	defer h2.close()
	for i := 0; i < 2; i++ {
		if _, err := h2.serveRequest(t, GetBlockHashesMsg, getBlockHashesData{knownHashes[0], hashes}); err != nil {
			t.Fatalf("by-hash reply %d: %v", i, err)
		}
	}
	expectWaits(t, h2.waits, waitFor(1, byteRate))
}

// Every request costs at least one unit, so a stream of empty requests is
// paced like any other.
func TestHandleRequests_EmptyRequestsAreNotFree(t *testing.T) {
	chain := &budgetTestChain{}
	const rate = 4.0
	h := newBudgetHarness(t, chain, rate, 2, 1<<30, 1<<30)
	defer h.close()

	for i := 0; i < 3; i++ {
		if _, err := h.serveRequest(t, GetBlocksMsg, []types.Hash{}); err != nil {
			t.Fatalf("empty request %d: %v", i, err)
		}
	}
	expectWaits(t, h.waits, waitFor(1, rate))
}

// A peer built without an explicit budget (as the older handler tests do)
// gets the default one on first use, once, with the default rates.
func TestPeer_RequestBudgetDefaultsWhenUnset(t *testing.T) {
	p := &peer{id: "lazy"}
	b := p.requestBudget()
	if b == nil {
		t.Fatal("expected a default budget")
	}
	if b != p.requestBudget() {
		t.Fatal("the default budget must be created once and kept")
	}
	if b.lookups.rate != RequestLookupRate || b.lookups.burst != RequestLookupBurst {
		t.Fatalf("lookup bucket %v/%v, want the defaults", b.lookups.rate, b.lookups.burst)
	}
	if b.replyBytes.rate != RequestReplyByteRate || b.replyBytes.burst != RequestReplyByteBurst {
		t.Fatalf("byte bucket %v/%v, want the defaults", b.replyBytes.rate, b.replyBytes.burst)
	}
	if b.wait == nil {
		t.Fatal("the default budget must be able to wait")
	}
}

// The production wait sleeps for the duration unless the peer's close signal
// fires first, in which case it returns an error at once.
func TestSleepUnlessClosed(t *testing.T) {
	closed := make(chan struct{})
	wait := sleepUnlessClosed(closed)
	if err := wait(time.Millisecond); err != nil {
		t.Fatalf("a short wait on an open peer: %v", err)
	}
	// Close during an active wait: it ends with an error well before the
	// duration.
	start := time.Now()
	done := make(chan error, 1)
	go func() { done <- wait(10 * time.Second) }()
	time.Sleep(50 * time.Millisecond)
	close(closed)
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("a wait interrupted by the close signal must return an error")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the wait did not end when the peer closed")
	}
	if time.Since(start) > time.Second {
		t.Fatal("the wait must end at the close, not after the duration")
	}
	// Already closed: returns at once.
	if err := wait(2 * time.Second); err == nil {
		t.Fatal("a wait on a closed peer must return an error")
	}
}

// The defaults keep the honest requesters' pacing inside the downloader's
// timeouts with room: one request of any kind paces for a small fraction
// of hashTTL (5 s) and blockHardTTL (9 s), the lookup burst covers the
// honest outstanding set (one hash request, one block request, a fetcher
// batch) several times over so pacing only starts under sustained load,
// and a miss costs more than a hit. TestHandleRequests_MixedFlatOutTraffic
// covers the combined case.
func TestRequestBudgetDefaults_CoverHonestRequesters(t *testing.T) {
	blockRequest := 1 + float64(downloader.MaxBlockFetch)
	hashRequest := 2 + float64(downloader.MaxHashFetch)
	fetcherBatch := 1 + float64(fetcher.HashLimit)
	costliestBlockRequest := 1 + float64(MaxBlocksRequest)*RequestMissCost
	if w := waitFor(hashRequest, RequestLookupRate); w > 500*time.Millisecond {
		t.Fatalf("one full hash request paces for %v, must stay well inside the 5 s hash timeout", w)
	}
	if w := waitFor(costliestBlockRequest, RequestLookupRate); w > 500*time.Millisecond {
		t.Fatalf("the costliest block request paces for %v, must stay well inside the 9 s block timeout", w)
	}
	if w := waitFor(float64(SoftResponseLimit), RequestReplyByteRate); w > 500*time.Millisecond {
		t.Fatalf("a full reply paces for %v, must stay well inside the 9 s block timeout", w)
	}
	if outstanding := blockRequest + hashRequest + fetcherBatch; RequestLookupBurst < 8*outstanding {
		t.Fatalf("lookup burst %v must cover the honest outstanding set (%v units) several times over", RequestLookupBurst, outstanding)
	}
	if RequestLookupBurst < costliestBlockRequest {
		t.Fatalf("lookup burst %v must cover the costliest single request (%v)", RequestLookupBurst, costliestBlockRequest)
	}
	if RequestReplyByteBurst < 8*float64(SoftResponseLimit) {
		t.Fatalf("reply byte burst %v must cover several full replies", RequestReplyByteBurst)
	}
	if RequestMissCost < 2 {
		t.Fatalf("a miss must cost more than a hit, got %v", RequestMissCost)
	}
}

// Each connection has its own budget, and the default budget's wait is wired
// to the connection's close signal: exhausting one peer leaves another
// untouched, and closing the transport ends a wait in progress.
func TestPeer_DefaultBudgetsAreIndependentAndEndWithTheConnection(t *testing.T) {
	closedA := make(chan struct{})
	a := &peer{id: "a", budget: newDefaultRequestBudget("a", closedA)}
	b := &peer{id: "b", budget: newDefaultRequestBudget("b", make(chan struct{}))}
	if a.requestBudget() == b.requestBudget() {
		t.Fatal("two peers must not share a budget")
	}
	if d, ok := a.requestBudget().lookups.reserve(RequestLookupBurst); !ok || d != 0 {
		t.Fatalf("peer a's full burst: %v %v", d, ok)
	}
	if d, ok := b.requestBudget().lookups.reserve(RequestLookupBurst); !ok || d != 0 {
		t.Fatalf("exhausting peer a must leave peer b's burst intact: %v %v", d, ok)
	}

	// Peer a is now in deficit; its next charge waits, and the wait ends
	// when the connection closes.
	done := make(chan error, 1)
	go func() { done <- a.requestBudget().admitLookups(RequestLookupBurst) }()
	time.Sleep(50 * time.Millisecond)
	close(closedA)
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("a wait interrupted by the connection closing must return an error")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the default budget's wait did not end when the connection closed")
	}
}

func (c *budgetTestChain) InsertChainMoved([]*nom.DetailedMomentum) (int, bool, error) {
	return 0, false, nil
}
