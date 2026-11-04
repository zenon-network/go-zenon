package downloader

import (
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common/types"
)

// livenessHeights lets the mock block fetcher answer with the height a hash
// was advertised at, so the queue files the block where the hash chain says.
var (
	livenessMu      sync.Mutex
	livenessHeights = map[types.Hash]uint64{}
)

// livenessReply is a hash reply for heights from..from+count-1 in wire
// order, newest first.
func livenessReply(prefix byte, from, count uint64) []types.Hash {
	hashes := make([]types.Hash, 0, count)
	for h := from + count; h > from; h-- {
		hash := testHash(prefix, h-1)
		livenessMu.Lock()
		livenessHeights[hash] = h - 1
		livenessMu.Unlock()
		hashes = append(hashes, hash)
	}
	return hashes
}

// livenessRun is a downloader with one registered peer whose hash fetcher
// answers the i-th request with replies[i] (nil = empty reply, missing = no
// answer) and whose block fetcher serves every requested hash at its
// advertised height. Imported heights are recorded. The session is ended
// when the test finishes so a failed assertion leaves no fetcher behind.
type livenessRun struct {
	d        *Downloader
	p        *peer
	requests int32 // hash requests made
	mu       sync.Mutex
	froms    []uint64 // heights the hash requests started from
	imported []uint64
	endOnce  sync.Once
}

func newLivenessRun(t *testing.T, name string, replies [][]types.Hash) *livenessRun {
	t.Helper()
	r := &livenessRun{}
	r.d = New(
		func(types.Hash) bool { return false },
		func(types.Hash) *nom.DetailedMomentum { return nil },
		func() *nom.Momentum { return &nom.Momentum{Height: 0} },
		func(blocks []*nom.DetailedMomentum) (int, error) {
			r.mu.Lock()
			for _, b := range blocks {
				r.imported = append(r.imported, b.Momentum.Height)
			}
			r.mu.Unlock()
			return 0, nil
		},
		func(string) {},
		nil,
	)
	r.d.cancelCh = make(chan struct{})
	atomic.StoreInt32(&r.d.synchronising, 1)
	t.Cleanup(r.end)

	r.p = newPeer(name, eth61, types.Hash{}, nil, nil, nil)
	r.p.getAbsHashes = func(from uint64, count int) error {
		i := int(atomic.AddInt32(&r.requests, 1)) - 1
		r.mu.Lock()
		r.froms = append(r.froms, from)
		r.mu.Unlock()
		if i >= len(replies) {
			return nil
		}
		reply := replies[i]
		go func() {
			select {
			case r.d.hashCh <- hashPack{name, reply}:
			case <-r.d.cancelCh:
			}
		}()
		return nil
	}
	r.p.getBlocks = func(hashes []types.Hash) error {
		blocks := make([]*nom.DetailedMomentum, 0, len(hashes))
		for _, h := range hashes {
			livenessMu.Lock()
			height := livenessHeights[h]
			livenessMu.Unlock()
			blocks = append(blocks, &nom.DetailedMomentum{Momentum: &nom.Momentum{Hash: h, Height: height}})
		}
		go func() {
			select {
			case r.d.blockCh <- blockPack{name, blocks}:
			case <-r.d.cancelCh:
			}
		}()
		return nil
	}
	if err := r.d.peers.Register(r.p); err != nil {
		t.Fatalf("register peer: %v", err)
	}
	return r
}

func (r *livenessRun) end() {
	r.endOnce.Do(r.d.endSession)
}

// startHashFetchToFullQueue runs fetchHashes with the continuation slot
// already holding an unread "more coming" flag, as it does when the block
// fetcher lags one read, and waits until the fetcher has queued the reply
// that fills the queue. Past that point its only remaining step is the stop
// flag; a fetcher that drops it returns at once, so the fetcher must still
// be running a second later.
func (r *livenessRun) startHashFetchToFullQueue(t *testing.T) <-chan error {
	t.Helper()
	r.d.processCh <- true
	errc := make(chan error, 1)
	go func() { errc <- r.d.fetchHashes(r.p, 0, 1) }()
	waitFor(t, 10*time.Second, "the queue to fill", func() bool { return r.d.queue.Pending() >= maxQueuedHashes })
	select {
	case err := <-errc:
		t.Fatalf("fetchHashes returned %v before anyone read its stop flag", err)
	case <-time.After(time.Second):
	}
	return errc
}

func waitFor(t *testing.T, timeout time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func expectReturn(t *testing.T, what string, errc <-chan error, want error, timeout time.Duration) {
	t.Helper()
	select {
	case err := <-errc:
		if err != want {
			t.Fatalf("%s returned %v, want %v", what, err, want)
		}
	case <-time.After(timeout):
		t.Fatalf("%s did not return", what)
	}
}

// The hash fetcher stops when one reply fills the queue. The block fetcher
// learns that only from the stop flag, so the flag must reach it even when
// the one-slot channel still holds an earlier "more coming" value the block
// fetcher has not read yet, and no further hashes may be requested.
func TestHashFetchQueueFullFlagReachesBlockFetcher(t *testing.T) {
	orig := maxQueuedHashes
	maxQueuedHashes = 300
	defer func() { maxQueuedHashes = orig }()

	r := newLivenessRun(t, "queue-full", [][]types.Hash{livenessReply('a', 1, 400)})
	hashErr := r.startHashFetchToFullQueue(t)

	var sawStop bool
	deadline := time.After(5 * time.Second)
	for !sawStop {
		select {
		case cont := <-r.d.processCh:
			sawStop = !cont
		case <-deadline:
			t.Fatal("queue-full stop flag never reached the block fetcher")
		}
	}
	expectReturn(t, "fetchHashes", hashErr, nil, 5*time.Second)
	if n := atomic.LoadInt32(&r.requests); n != 1 {
		t.Fatalf("%d hash requests, want 1: the fetcher kept going after the queue filled", n)
	}
}

// End to end: with the queue filled before the block fetcher starts, the
// block fetcher must still drain the queue and return once the hash fetcher
// has stopped, instead of ticking forever while the peer stays connected.
func TestBlockFetchCompletesAfterQueueFull(t *testing.T) {
	orig := maxQueuedHashes
	maxQueuedHashes = 300
	defer func() { maxQueuedHashes = orig }()

	r := newLivenessRun(t, "complete", [][]types.Hash{livenessReply('c', 1, 400)})
	hashErr := r.startHashFetchToFullQueue(t)

	blockErr := make(chan error, 1)
	go func() { blockErr <- r.d.fetchBlocks(1) }()
	expectReturn(t, "fetchBlocks", blockErr, nil, 15*time.Second)
	expectReturn(t, "fetchHashes", hashErr, nil, 5*time.Second)
	if n := atomic.LoadInt32(&r.requests); n != 1 {
		t.Fatalf("%d hash requests, want 1", n)
	}
	r.requireImported(t, 1, 400)
}

// Cancelling the session while the stop flag waits for its slot releases
// the hash fetcher.
func TestHashFetchStopFlagReleasedByCancel(t *testing.T) {
	orig := maxQueuedHashes
	maxQueuedHashes = 300
	defer func() { maxQueuedHashes = orig }()

	r := newLivenessRun(t, "cancelled", [][]types.Hash{livenessReply('x', 1, 400)})
	hashErr := r.startHashFetchToFullQueue(t)
	r.end()
	expectReturn(t, "fetchHashes", hashErr, nil, 5*time.Second)
}

// Control: the empty-reply end of a hash fetch keeps terminating the block
// fetcher as before.
func TestBlockFetchCompletesAfterEmptyReply(t *testing.T) {
	r := newLivenessRun(t, "empty-reply", [][]types.Hash{livenessReply('e', 1, 300), nil})
	hashErr := make(chan error, 1)
	blockErr := make(chan error, 1)
	go func() { hashErr <- r.d.fetchHashes(r.p, 0, 1) }()
	go func() { blockErr <- r.d.fetchBlocks(1) }()
	expectReturn(t, "fetchHashes", hashErr, nil, 15*time.Second)
	expectReturn(t, "fetchBlocks", blockErr, nil, 15*time.Second)
	r.requireImported(t, 1, 300)
}

// A reply longer than the window we asked for keeps the requested window,
// which is the tail of a newest-first reply, and drops the rest: the next
// request continues right after the kept window, the peer is not dropped (a
// serving node's frontier can grow between the two reads it answers from),
// and the chain imports contiguously across the continuation.
func TestHashFetchTruncatesOversizedReply(t *testing.T) {
	window := uint64(MaxHashFetch)
	r := newLivenessRun(t, "oversized", [][]types.Hash{
		livenessReply('f', 1, window+1),   // heights 1..513, one past the window
		livenessReply('f', window+1, 101), // heights 513..613: the continuation
		nil,
	})
	hashErr := make(chan error, 1)
	blockErr := make(chan error, 1)
	go func() { hashErr <- r.d.fetchHashes(r.p, 0, 1) }()
	go func() { blockErr <- r.d.fetchBlocks(1) }()
	expectReturn(t, "fetchHashes", hashErr, nil, 15*time.Second)
	expectReturn(t, "fetchBlocks", blockErr, nil, 15*time.Second)

	r.mu.Lock()
	froms := append([]uint64(nil), r.froms...)
	r.mu.Unlock()
	if len(froms) != 3 || froms[0] != 1 || froms[1] != window+1 || froms[2] != window+102 {
		t.Fatalf("hash requests from %v, want [1 %d %d]", froms, window+1, window+102)
	}
	r.requireImported(t, 1, window+101)
}

// requireImported checks that exactly the heights lo..hi were imported, each
// once, in a contiguous ascending sequence. The importer runs on its own
// goroutine after the fetchers have returned, so the count is awaited.
func (r *livenessRun) requireImported(t *testing.T, lo, hi uint64) {
	t.Helper()
	snapshot := func() []uint64 {
		r.mu.Lock()
		defer r.mu.Unlock()
		return append([]uint64(nil), r.imported...)
	}
	waitFor(t, 5*time.Second, "the importer to finish", func() bool { return uint64(len(snapshot())) >= hi-lo+1 })
	got := snapshot()
	if !sort.SliceIsSorted(got, func(i, j int) bool { return got[i] < got[j] }) {
		t.Fatalf("imported out of order: %v", got)
	}
	if uint64(len(got)) != hi-lo+1 {
		t.Fatalf("imported %d heights, want %d (%d..%d)", len(got), hi-lo+1, lo, hi)
	}
	for i, h := range got {
		if h != lo+uint64(i) {
			t.Fatalf("imported height %d at position %d, want %d", h, i, lo+uint64(i))
		}
	}
}

// A continuation flag left in the channel by an earlier run must not be
// carried into the next one: a stale "more coming" could arm the loss of
// the next stop flag, a stale stop flag would end the next block fetch
// early. The run here fails at the ancestor step (the peer answers with an
// empty head set), after the point where the session resets its state.
func TestSynchroniseClearsStaleContinuationFlag(t *testing.T) {
	for _, stale := range []bool{true, false} {
		r := newLivenessRun(t, "stale-flag", [][]types.Hash{nil})
		atomic.StoreInt32(&r.d.synchronising, 0)
		r.d.cancelCh = nil
		r.d.processCh <- stale

		if err := r.d.synchronise(r.p.id, types.Hash{1}, 1); err != errEmptyHashSet {
			t.Fatalf("synchronise returned %v, want errEmptyHashSet", err)
		}
		select {
		case cont := <-r.d.processCh:
			t.Fatalf("stale continuation flag %v survived into the new run", cont)
		default:
		}
	}
}
