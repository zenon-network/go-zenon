package downloader

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common/types"
)

// stallHeights maps test hashes to their advertised heights, like
// livenessHeights does for the hash-fetch-liveness tests.
var (
	stallMu      sync.Mutex
	stallHeights = map[types.Hash]uint64{}
)

func stallHash(prefix byte, height uint64) types.Hash {
	h := testHash(prefix, height)
	stallMu.Lock()
	stallHeights[h] = height
	stallMu.Unlock()
	return h
}

func stallHeight(h types.Hash) uint64 {
	stallMu.Lock()
	defer stallMu.Unlock()
	return stallHeights[h]
}

// TestBlockFetchWithheldHeadEndsSession verifies that fetchBlocks
// terminates when no peer can serve the head block of the download window:
// the cache fills behind the hole, TakeBlocks hands over nothing and the
// peers-unavailable guard (reserve space left, nothing reservable) ends the
// session. It sits in the issue #247 family of dead states.
//
// This is a fixture test, not a regression test for the fix: run against
// the pre-fix downloader (merge base 0e80915, with only a test-only
// stallTimeout supplied for compilation) it meets the same assertions,
// because the state it reaches still has one reserve slot free — the
// in-flight head reservation caps the tail fill at 7 of the 8 slots
// (space == 1), which is exactly the condition the pre-fix guard already
// fired on. Verified by instrumenting queue.NoProgress in this fixture:
// spaceAtLoop=1, space=1, the space > 0 arm. The tests that discriminate
// the fix from dev are TestBlockFetchDeadStateGuard and
// TestBlockFetchOmittedHeadCacheFull (the zero-space arm, unreachable for
// the pre-fix guard), TestBlockFetchStallCapturesInFlight (the deadline)
// and the drop-gate tests.
//
// Wire order, for the record: the handler serves
// GetBlockHashesFromNumberMsg newest first (handler.go reverses the
// ascending hash walk), and fetchHashes inserts arrivals with fifo=true,
// so in production the first hash of a batch is reserved first and the
// window head is reserved last. This fixture inserts ascending on purpose —
// the reverse of the wire order — so the window head (height 1) is reserved
// first here. The production shape of a missing head is a hash no peer
// actually has (the origin advertised something it cannot serve): whichever
// peer reserves it holds the request until blockHardTTL. Expire re-pushes
// the hash but does not SetIdle the peer, so each expiry sidelines that
// peer for the rest of the session. With two peers and a 16-slot cache, the
// tail fills 7 of the 8 reserve slots behind the never-delivered head; once
// both peers have held and expired on the head, no idle peer remains to
// re-reserve it, reserve space is still 1 and the guard ends the session
// with errPeersUnavailable.
func TestBlockFetchWithheldHeadEndsSession(t *testing.T) {
	origTimeout := stallTimeout
	stallTimeout = 5 * time.Second
	defer func() { stallTimeout = origTimeout }()
	// The withheld head request must expire well inside the stall window so
	// the guard, not the deadline, ends the session.
	origHard := blockHardTTL
	blockHardTTL = 200 * time.Millisecond
	defer func() { blockHardTTL = origHard }()
	// Shrink the block cache so a small number of served blocks fills
	// most of the reserve budget: the in-flight head reservation caps the
	// tail fill at 7 of the 8 slots, so the space > 0 arm of the guard
	// (space available, nothing reservable) is the arm that fires.
	origLimit := blockCacheLimit
	blockCacheLimit = 16
	defer func() { blockCacheLimit = origLimit }()

	d := newIdleDownloader()
	d.cancelCh = make(chan struct{})
	atomic.StoreInt32(&d.synchronising, 1)
	defer atomic.StoreInt32(&d.synchronising, 0)
	defer d.endSession()

	// Both peers serve every block except the head of the window (height
	// 1); whichever of them reserves the head holds the request until
	// expiry, like a peer that does not have the block at all. A request
	// that contains only the head gets no reply, exactly like the wire,
	// where the reservation would stay outstanding until expiry.
	newStaller := func(id string) {
		p := newPeer(id, eth61, types.Hash{}, nil, nil, nil)
		p.getBlocks = func(hashes []types.Hash) error {
			blocks := make([]*nom.DetailedMomentum, 0, len(hashes))
			for _, h := range hashes {
				if height := stallHeight(h); height > 1 {
					blocks = append(blocks, &nom.DetailedMomentum{
						Momentum: &nom.Momentum{Hash: h, Height: height},
					})
				}
			}
			if len(blocks) == 0 {
				return nil
			}
			go func() {
				select {
				case d.blockCh <- blockPack{id, blocks}:
				case <-d.cancelCh:
				}
			}()
			return nil
		}
		if err := d.peers.Register(p); err != nil {
			t.Fatalf("register peer: %v", err)
		}
	}
	newStaller("staller-a")
	newStaller("staller-b")

	// Pre-populate the queue in ascending order — the reverse of the wire
	// order (see the doc comment), chosen so the head is reserved first.
	// The tail fills 7 of the 8 reserve slots behind the withheld head, and
	// the session is left with space == 1, nothing in flight, no cached head
	// and hashes still pending.
	hashes := make([]types.Hash, 10)
	for i := range hashes {
		hashes[i] = stallHash('s', uint64(i+1))
	}
	d.queue.Prepare(1)
	d.queue.Insert(hashes, true)
	d.processCh <- false // hash fetcher finished

	start := time.Now()
	blockErr := make(chan error, 1)
	go func() { blockErr <- d.fetchBlocks(1) }()
	expectReturn(t, "fetchBlocks", blockErr, errPeersUnavailable, 15*time.Second)
	// The error assertion already separates a guard end from a deadline
	// end (the deadline returns errStallNoProgress, which would fail the
	// assertion above). The elapsed check additionally pins that the guard
	// ended the session on the spot rather than the deadline covering for
	// it.
	if elapsed := time.Since(start); elapsed > stallTimeout {
		t.Fatalf("peers-unavailable guard did not fire on the spot; the %.0fs stall deadline ended the session instead (took %v)", stallTimeout.Seconds(), elapsed)
	}
}

// TestBlockFetchNoStallWhileImportBusy verifies the false-positive guard: a
// slow import must not trip the stall deadline. The peer hands over heights
// 1..8, then goes silent on the withheld tail, keeping its request in
// flight so the extra hashes inserted while the import worker is blocked
// stay pending (Reserve only drains the queue for idle peers). Pending > 0,
// no handover and Processing() == true is the attack signature except the
// import is alive, so the deadline must not fire. Once the import finishes,
// the withheld tail must trip it.
func TestBlockFetchNoStallWhileImportBusy(t *testing.T) {
	origTimeout := stallTimeout
	stallTimeout = 300 * time.Millisecond
	defer func() { stallTimeout = origTimeout }()
	origLimit := blockCacheLimit
	blockCacheLimit = 16
	defer func() { blockCacheLimit = origLimit }()

	importEntered := make(chan struct{})
	releaseImport := make(chan struct{})
	var entered sync.Once
	d := New(
		func(types.Hash) bool { return false },
		func(types.Hash) *nom.DetailedMomentum { return nil },
		func() *nom.Momentum { return &nom.Momentum{} },
		func([]*nom.DetailedMomentum) (int, error) {
			entered.Do(func() { close(importEntered) })
			<-releaseImport
			return 0, nil
		},
		func(string) {},
		nil,
	)
	d.cancelCh = make(chan struct{})
	atomic.StoreInt32(&d.synchronising, 1)
	defer atomic.StoreInt32(&d.synchronising, 0)
	defer d.endSession()

	// The peer delivers the subset of each request up to height 8. If a
	// request contains only withheld hashes it goes unanswered, so the
	// request stays in flight and the peer stays busy (a response with an
	// empty pack would mark the hashes ignored and return them to the
	// queue, idling the peer).
	p := newPeer("slowimport", eth61, types.Hash{}, nil, nil, nil)
	p.getBlocks = func(hashes []types.Hash) error {
		blocks := make([]*nom.DetailedMomentum, 0, len(hashes))
		for _, h := range hashes {
			if height := stallHeight(h); height <= 8 {
				blocks = append(blocks, &nom.DetailedMomentum{
					Momentum: &nom.Momentum{Hash: h, Height: height},
				})
			}
		}
		if len(blocks) == 0 {
			return nil
		}
		go func() {
			select {
			case d.blockCh <- blockPack{"slowimport", blocks}:
			case <-d.cancelCh:
			}
		}()
		return nil
	}
	if err := d.peers.Register(p); err != nil {
		t.Fatalf("register peer: %v", err)
	}

	d.queue.Prepare(1)
	d.queue.Insert([]types.Hash{
		stallHash('i', 1), stallHash('i', 2), stallHash('i', 3),
		stallHash('i', 4), stallHash('i', 5), stallHash('i', 6),
		stallHash('i', 7), stallHash('i', 8), stallHash('i', 9),
		stallHash('i', 10), stallHash('i', 11), stallHash('i', 12),
		stallHash('i', 13), stallHash('i', 14), stallHash('i', 15),
	}, true) // ascending: requests run from the window head downwards
	d.processCh <- false // hash fetcher finished

	blockErr := make(chan error, 1)
	go func() { blockErr <- d.fetchBlocks(1) }()

	// Wait until the import worker owns its batch.
	select {
	case <-importEntered:
	case <-time.After(10 * time.Second):
		select {
		case err := <-blockErr:
			t.Fatalf("import worker never took a batch; fetchBlocks returned %v", err)
		default:
			t.Fatal("import worker never took a batch; fetchBlocks still running")
		}
	}

	// With the peer busy holding the withheld tail, these hashes cannot be
	// reserved: they stay pending while the import worker is blocked.
	d.queue.Insert([]types.Hash{
		stallHash('j', 16), stallHash('j', 17), stallHash('j', 18),
		stallHash('j', 19), stallHash('j', 20), stallHash('j', 21),
		stallHash('j', 22), stallHash('j', 23), stallHash('j', 24),
	}, true)

	// The attack signature — pending hashes, no handover for longer than
	// stallTimeout — holds now, except Processing() is true.
	time.Sleep(3 * stallTimeout)
	select {
	case err := <-blockErr:
		t.Fatalf("stall deadline fired while the import worker was busy: %v", err)
	default:
	}

	// Release the import: heights 2..8 import, then the withheld tail must
	// trip the deadline.
	close(releaseImport)
	expectReturn(t, "fetchBlocks", blockErr, errStallNoProgress, 15*time.Second)
}

// TestBlockFetchProgressResetsStallTimer verifies that the stall timer is
// reset whenever TakeBlocks hands a contiguous run to the import worker: a
// paced download whose total runtime exceeds stallTimeout must still
// complete, because every handover gap stays below it. The download is
// paced so it cannot pass by finishing inside the deadline window: 24
// hashes are queued up front and the peer answers each request after a
// fixed delay, so handovers are spaced by the pace (~200 ms apart, six
// batches, ~1.2 s total against a 500 ms deadline), hashes remain pending
// throughout and the importer is idle between batches. The offset reset is
// the mechanism that keeps the clock fresh here: with it removed, the
// deadline fires mid-download and this test returns errStallNoProgress
// instead of nil (verified by mutation; note a busy import also refreshes
// the clock, so the reset is not the only refresh path — with the paced
// importer idle between batches the deadline still fires).
func TestBlockFetchProgressResetsStallTimer(t *testing.T) {
	origTimeout := stallTimeout
	stallTimeout = 500 * time.Millisecond
	defer func() { stallTimeout = origTimeout }()
	origLimit := blockCacheLimit
	blockCacheLimit = 16
	defer func() { blockCacheLimit = origLimit }()

	d := newIdleDownloader()
	d.cancelCh = make(chan struct{})
	atomic.StoreInt32(&d.synchronising, 1)
	defer atomic.StoreInt32(&d.synchronising, 0)
	defer d.endSession()

	const pace = 200 * time.Millisecond
	p := newPeer("paced", eth61, types.Hash{}, nil, nil, nil)
	p.getBlocks = func(hashes []types.Hash) error {
		blocks := make([]*nom.DetailedMomentum, 0, len(hashes))
		for _, h := range hashes {
			blocks = append(blocks, &nom.DetailedMomentum{
				Momentum: &nom.Momentum{Hash: h, Height: stallHeight(h)},
			})
		}
		go func() {
			timer := time.NewTimer(pace)
			defer timer.Stop()
			select {
			case <-timer.C:
				select {
				case d.blockCh <- blockPack{"paced", blocks}:
				case <-d.cancelCh:
				}
			case <-d.cancelCh:
			}
		}()
		return nil
	}
	if err := d.peers.Register(p); err != nil {
		t.Fatalf("register peer: %v", err)
	}

	// Ascending fixture order (the reverse of the wire order, chosen so
	// the head is reserved first): imports run from the front and the cache
	// never fills. Reservation sizes follow the capacity ramp 1, 2, 4, 8, 8:
	// five paced batches of 23 blocks plus one for the last.
	hashes := make([]types.Hash, 24)
	for i := range hashes {
		hashes[i] = stallHash('p', uint64(i+1))
	}
	d.queue.Prepare(1)
	d.queue.Insert(hashes, true)
	d.processCh <- false // hash fetcher finished

	start := time.Now()
	blockErr := make(chan error, 1)
	go func() { blockErr <- d.fetchBlocks(1) }()
	expectReturn(t, "fetchBlocks", blockErr, nil, 15*time.Second)
	if elapsed := time.Since(start); elapsed < 6*pace {
		t.Fatalf("download finished in %v, want at least %v: the batches were not paced, so the test cannot prove the timer reset", elapsed, 6*pace)
	}
}

// TestBlockFetchNoStallWhenIdle verifies that the stall deadline does not
// fire when there is nothing left to download (Pending == 0).
func TestBlockFetchNoStallWhenIdle(t *testing.T) {
	orig := stallTimeout
	stallTimeout = 200 * time.Millisecond
	defer func() { stallTimeout = orig }()

	d := newIdleDownloader()
	d.cancelCh = make(chan struct{})
	atomic.StoreInt32(&d.synchronising, 1)
	defer atomic.StoreInt32(&d.synchronising, 0)
	defer d.endSession()

	// Register an idle peer so the no-peers check passes.
	p := newPeer("idle", eth61, types.Hash{}, nil, nil, nil)
	if err := d.peers.Register(p); err != nil {
		t.Fatalf("register peer: %v", err)
	}

	d.queue.Prepare(1)

	blockErr := make(chan error, 1)
	go func() { blockErr <- d.fetchBlocks(1) }()
	// Let the stall deadline window elapse while there is nothing pending:
	// the deadline must not fire on an idle queue.
	time.Sleep(3 * stallTimeout)
	d.processCh <- false // hash fetcher done, no hashes to fetch
	expectReturn(t, "fetchBlocks", blockErr, nil, 5*time.Second)
}

// TestBlockFetchLongImportResetsStallWindow verifies that the stall clock is
// refreshed while the import worker is busy: an import that runs longer than
// stallTimeout and yields no handover (the insert returns 0) must leave the
// peer a full deadline window after the import finishes, not fire
// immediately because the pre-import handover has aged out.
func TestBlockFetchLongImportResetsStallWindow(t *testing.T) {
	origTimeout := stallTimeout
	stallTimeout = 500 * time.Millisecond
	defer func() { stallTimeout = origTimeout }()
	origLimit := blockCacheLimit
	blockCacheLimit = 16
	defer func() { blockCacheLimit = origLimit }()

	importEntered := make(chan struct{})
	releaseImport := make(chan struct{})
	var entered sync.Once
	d := New(
		func(types.Hash) bool { return false },
		func(types.Hash) *nom.DetailedMomentum { return nil },
		func() *nom.Momentum { return &nom.Momentum{} },
		func([]*nom.DetailedMomentum) (int, error) {
			entered.Do(func() { close(importEntered) })
			<-releaseImport
			// Block 1 was handed over before this ran; returning 0 means no
			// further TakeBlocks finds a contiguous run (2 is withheld in
			// flight).
			return 0, nil
		},
		func(string) {},
		nil,
	)
	d.cancelCh = make(chan struct{})
	atomic.StoreInt32(&d.synchronising, 1)
	defer atomic.StoreInt32(&d.synchronising, 0)
	defer d.endSession()

	// Deliver only height 1, then hold the withheld tail in flight so the
	// extra hashes stay pending while the import worker is blocked. Nothing
	// remains cached, so no TakeBlocks after the release can advance the
	// offset and mask the clock-refresh under test.
	p := newPeer("longimport", eth61, types.Hash{}, nil, nil, nil)
	p.getBlocks = func(hashes []types.Hash) error {
		blocks := make([]*nom.DetailedMomentum, 0, len(hashes))
		for _, h := range hashes {
			if height := stallHeight(h); height <= 1 {
				blocks = append(blocks, &nom.DetailedMomentum{
					Momentum: &nom.Momentum{Hash: h, Height: height},
				})
			}
		}
		if len(blocks) == 0 {
			return nil
		}
		go func() {
			select {
			case d.blockCh <- blockPack{"longimport", blocks}:
			case <-d.cancelCh:
			}
		}()
		return nil
	}
	if err := d.peers.Register(p); err != nil {
		t.Fatalf("register peer: %v", err)
	}

	d.queue.Prepare(1)
	d.queue.Insert([]types.Hash{
		stallHash('l', 1), stallHash('l', 2), stallHash('l', 3),
		stallHash('l', 4), stallHash('l', 5), stallHash('l', 6),
		stallHash('l', 7), stallHash('l', 8), stallHash('l', 9),
		stallHash('l', 10), stallHash('l', 11), stallHash('l', 12),
	}, true)
	d.processCh <- false // hash fetcher finished

	blockErr := make(chan error, 1)
	go func() { blockErr <- d.fetchBlocks(1) }()

	select {
	case <-importEntered:
	case <-time.After(10 * time.Second):
		t.Fatal("import worker never took a batch")
	}

	// Keep the import blocked for more than stallTimeout with hashes pending.
	d.queue.Insert([]types.Hash{
		stallHash('m', 16), stallHash('m', 17), stallHash('m', 18),
		stallHash('m', 19), stallHash('m', 20), stallHash('m', 21),
	}, true)
	time.Sleep(3 * stallTimeout)
	select {
	case err := <-blockErr:
		t.Fatalf("stall deadline fired while the import worker was busy: %v", err)
	default:
	}

	// The import ends with no handover and an aged-out pre-import handover.
	// With the clock refresh it must still get a full fresh window.
	close(releaseImport)
	time.Sleep(stallTimeout / 2) // < stallTimeout after the release
	select {
	case err := <-blockErr:
		t.Fatalf("stall deadline fired without a fresh window after the import: %v", err)
	default:
	}
	expectReturn(t, "fetchBlocks", blockErr, errStallNoProgress, 15*time.Second)
}

// TestSynchroniseStallDropGatedOnInFlight verifies the drop policy for the
// stall deadline: the peer is dropped only when nothing was in flight when
// the deadline fired (a sustained withholder); while a request was still in
// flight the session is cancelled without a drop, so an honest slow peer
// set cannot get the best-TD origin removed. The in-flight count travels
// inside the error because cancellation resets the queue before Synchronise
// runs: reading the queue there would always see zero.
func TestSynchroniseStallDropGatedOnInFlight(t *testing.T) {
	for _, tc := range []struct {
		name     string
		inFlight int
		wantDrop bool
	}{
		{"drops the withholder when nothing is in flight", 0, true},
		{"keeps the origin while a request is in flight", 1, false},
		{"keeps the origin while several requests are in flight", 3, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var dropped []string
			d := newIdleDownloader()
			d.dropPeer = func(id string) { dropped = append(dropped, id) }
			d.synchroniseMock = func(string, types.Hash) error {
				return &stallNoProgressError{inFlight: tc.inFlight}
			}

			d.Synchronise("origin", types.Hash{}, 100)
			if tc.wantDrop {
				if len(dropped) != 1 || dropped[0] != "origin" {
					t.Fatalf("expected origin dropped once, got %v", dropped)
				}
			} else if len(dropped) != 0 {
				t.Fatalf("origin must not be dropped while a request is in flight, got %v", dropped)
			}
		})
	}
}

// TestSynchroniseCacheFullStallNoDrop verifies that the zero-space stall
// error aborts the session without dropping the origin: a cache full
// behind a missing head does not identify the origin as a withholder the
// way errPeersUnavailable does.
func TestSynchroniseCacheFullStallNoDrop(t *testing.T) {
	var dropped []string
	d := newIdleDownloader()
	d.dropPeer = func(id string) { dropped = append(dropped, id) }
	d.synchroniseMock = func(string, types.Hash) error { return errStallCacheFull }

	d.Synchronise("origin", types.Hash{}, 100)
	if len(dropped) != 0 {
		t.Fatalf("origin must not be dropped on a cache-full stall, got %v", dropped)
	}
}

// TestBlockFetchStallCapturesInFlight verifies that the stall deadline
// captures the number of requests in flight at the moment it fires, and
// that a request in flight counts as outstanding work even after the
// pending queue is drained: with a single hash and a silent peer, Pending()
// is 0 from the first reservation on, and the deadline must still fire.
// The captured value is what Synchronise uses for the drop decision, and it
// must be captured here — cancellation resets the queue before Synchronise
// sees the error.
func TestBlockFetchStallCapturesInFlight(t *testing.T) {
	origTimeout := stallTimeout
	stallTimeout = 300 * time.Millisecond
	defer func() { stallTimeout = origTimeout }()
	// Keep the request alive past the deadline so exactly one is in flight
	// when it fires.
	origHard := blockHardTTL
	blockHardTTL = 10 * time.Second
	defer func() { blockHardTTL = origHard }()

	d := newIdleDownloader()
	d.cancelCh = make(chan struct{})
	atomic.StoreInt32(&d.synchronising, 1)
	defer atomic.StoreInt32(&d.synchronising, 0)
	defer d.endSession()

	// The peer never answers: its first request stays in flight and the
	// pending queue drains to zero.
	p := newPeer("silent", eth61, types.Hash{}, nil, nil, nil)
	p.getBlocks = func(hashes []types.Hash) error { return nil }
	if err := d.peers.Register(p); err != nil {
		t.Fatalf("register peer: %v", err)
	}

	d.queue.Prepare(1)
	d.queue.Insert([]types.Hash{stallHash('f', 1)}, true)
	d.processCh <- false // hash fetcher finished

	blockErr := make(chan error, 1)
	go func() { blockErr <- d.fetchBlocks(1) }()
	select {
	case err := <-blockErr:
		var stalled *stallNoProgressError
		if !errors.As(err, &stalled) {
			t.Fatalf("fetchBlocks returned %v, want a stallNoProgressError", err)
		}
		if stalled.inFlight != 1 {
			t.Fatalf("deadline captured inFlight == %d, want 1", stalled.inFlight)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("fetchBlocks did not return")
	}
}

// TestBlockFetchDeadStateGuard verifies the zero-space arm of the
// no-progress guard: when the head block of the window was never delivered
// and the cache is exactly half full, no reservation can ever succeed and
// no import can ever run, so the session can never make progress again —
// the guard ends it on the spot with errStallCacheFull instead of waiting
// for the stall deadline (issue #247). The error is distinct from
// errPeersUnavailable because a full cache behind a missing head does not
// identify the origin as the withholder.
func TestBlockFetchDeadStateGuard(t *testing.T) {
	origTimeout := stallTimeout
	stallTimeout = 500 * time.Millisecond
	defer func() { stallTimeout = origTimeout }()
	origLimit := blockCacheLimit
	blockCacheLimit = 16
	defer func() { blockCacheLimit = origLimit }()

	d := newIdleDownloader()
	d.cancelCh = make(chan struct{})
	atomic.StoreInt32(&d.synchronising, 1)
	defer atomic.StoreInt32(&d.synchronising, 0)
	defer d.endSession()

	// Register a peer so the no-peers check passes; it is never used
	// because no reservation can succeed in the seeded state.
	p := newPeer("staller", eth61, types.Hash{}, nil, nil, nil)
	if err := d.peers.Register(p); err != nil {
		t.Fatalf("register peer: %v", err)
	}

	hashes := make([]types.Hash, 10)
	for i := range hashes {
		hashes[i] = stallHash('d', uint64(i+1))
	}
	d.queue.Prepare(1)
	d.queue.Insert(hashes, true)

	// Seed the dead state: heights 2..9 sit in the cache (exactly half of
	// it, so Reserve has no space), the slot for the head block stays
	// empty and height 1 stays pending. No request is in flight and the
	// import worker owns nothing, so no event can ever change this state.
	d.queue.lock.Lock()
	for i, h := range hashes[1:9] {
		d.queue.blockCache[i+1] = &Block{
			RawBlock:   &nom.DetailedMomentum{Momentum: &nom.Momentum{Hash: h, Height: uint64(i + 2)}},
			OriginPeer: "staller",
		}
		d.queue.blockPool[h] = uint64(i + 2)
	}
	d.queue.lock.Unlock()

	d.processCh <- false // hash fetcher finished
	start := time.Now()
	blockErr := make(chan error, 1)
	go func() { blockErr <- d.fetchBlocks(1) }()
	expectReturn(t, "fetchBlocks", blockErr, errStallCacheFull, 15*time.Second)
	// The guard must fire on the spot, not via the stall deadline backstop.
	if elapsed := time.Since(start); elapsed > stallTimeout {
		t.Fatalf("peers-unavailable guard did not fire on the spot; the %.0fs stall deadline ended the session instead (took %v)", stallTimeout.Seconds(), elapsed)
	}
}

// TestBlockFetchOmittedHeadCacheFull verifies that the zero-space arm of the
// no-progress guard is reachable through an ordinary delivery path, with no
// seeded state: the origin serves a hash list that skips the window head
// (fetchHashes never checks that the list covers the requested height, and
// Deliver places each block by its own height), an honest peer delivers
// everything it reserved, and the cache fills to exactly half behind the
// never-queued head. No reservation can ever succeed and no import can ever
// run, so the guard ends the session on the spot with errStallCacheFull
// (issue #247). On the pre-fix downloader this fixture never returns: the
// old guard requires free reserve space, no deadline exists, and the session
// is the #247 dead state — which is what makes this a regression test,
// unlike TestBlockFetchWithheldHeadEndsSession. The elapsed check pins the
// guard, not the stall deadline, as the terminator.
func TestBlockFetchOmittedHeadCacheFull(t *testing.T) {
	origTimeout := stallTimeout
	stallTimeout = 5 * time.Second
	defer func() { stallTimeout = origTimeout }()
	origLimit := blockCacheLimit
	blockCacheLimit = 16
	defer func() { blockCacheLimit = origLimit }()

	d := newIdleDownloader()
	d.cancelCh = make(chan struct{})
	atomic.StoreInt32(&d.synchronising, 1)
	defer atomic.StoreInt32(&d.synchronising, 0)
	defer d.endSession()

	// An honest peer serves every hash it is asked for; height 1 is never
	// queued, so it is never requested.
	p := newPeer("honest", eth61, types.Hash{}, nil, nil, nil)
	p.getBlocks = func(hashes []types.Hash) error {
		blocks := make([]*nom.DetailedMomentum, 0, len(hashes))
		for _, h := range hashes {
			blocks = append(blocks, &nom.DetailedMomentum{
				Momentum: &nom.Momentum{Hash: h, Height: stallHeight(h)},
			})
		}
		go func() {
			select {
			case d.blockCh <- blockPack{"honest", blocks}:
			case <-d.cancelCh:
			}
		}()
		return nil
	}
	if err := d.peers.Register(p); err != nil {
		t.Fatalf("register peer: %v", err)
	}

	// The origin's list skips the window head: heights 2..10, ascending.
	hashes := make([]types.Hash, 9)
	for i := range hashes {
		hashes[i] = stallHash('o', uint64(i+2))
	}
	d.queue.Prepare(1)
	d.queue.Insert(hashes, true)
	d.processCh <- false // hash fetcher finished

	start := time.Now()
	blockErr := make(chan error, 1)
	go func() { blockErr <- d.fetchBlocks(1) }()
	expectReturn(t, "fetchBlocks", blockErr, errStallCacheFull, 15*time.Second)
	if elapsed := time.Since(start); elapsed > stallTimeout {
		t.Fatalf("cache-full guard did not fire on the spot; the %.0fs stall deadline ended the session instead (took %v)", stallTimeout.Seconds(), elapsed)
	}
}

// the raced shape end to end — an import draining the cache while the
// scheduling pass runs — and verifies the session neither trips the
// peers-unavailable guard nor ends early: the pre-import zero reserve
// space must not be combined with the post-import missing head block. The
// fetcher must instead schedule the pending tail against the freed space on
// the next tick and complete the session. The sleep followed by an explicit
// d.process() does not guarantee the import lands inside a scheduling pass,
// so the interleaving itself is pinned deterministically by
// TestQueueNoProgress's freed case, which checks the predicate directly.
func TestBlockFetchImportFreesCapacityDuringPass(t *testing.T) {
	origTimeout := stallTimeout
	stallTimeout = 30 * time.Second // the deadline must not end this session
	defer func() { stallTimeout = origTimeout }()
	origHard := blockHardTTL
	blockHardTTL = 10 * time.Second
	defer func() { blockHardTTL = origHard }()
	origLimit := blockCacheLimit
	blockCacheLimit = 16
	defer func() { blockCacheLimit = origLimit }()

	d := newIdleDownloader()
	d.cancelCh = make(chan struct{})
	atomic.StoreInt32(&d.synchronising, 1)
	defer atomic.StoreInt32(&d.synchronising, 0)
	defer d.endSession()

	// An honest peer serves the pending tail as soon as it is reserved.
	p := newPeer("server", eth61, types.Hash{}, nil, nil, nil)
	p.getBlocks = func(hashes []types.Hash) error {
		blocks := make([]*nom.DetailedMomentum, 0, len(hashes))
		for _, h := range hashes {
			blocks = append(blocks, &nom.DetailedMomentum{
				Momentum: &nom.Momentum{Hash: h, Height: stallHeight(h)},
			})
		}
		go func() {
			select {
			case d.blockCh <- blockPack{"server", blocks}:
			case <-d.cancelCh:
			}
		}()
		return nil
	}
	if err := d.peers.Register(p); err != nil {
		t.Fatalf("register peer: %v", err)
	}

	hashes := make([]types.Hash, 10)
	for i := range hashes {
		hashes[i] = stallHash('r', uint64(i+1))
	}
	d.queue.Prepare(1)
	// Only the tail beyond the seeded cache stays pending, like a real
	// delivery path: served hashes leave the pending queue.
	d.queue.Insert(hashes[8:], true)

	// Seed the edge of the raced state: the head block is present (a
	// successful import is possible), the cache is exactly half full, so
	// the scheduling pass reserves nothing, and nothing is in flight.
	d.queue.lock.Lock()
	for i, h := range hashes[:8] {
		d.queue.blockCache[i] = &Block{
			RawBlock:   &nom.DetailedMomentum{Momentum: &nom.Momentum{Hash: h, Height: uint64(i + 1)}},
			OriginPeer: "server",
		}
		d.queue.blockPool[h] = uint64(i + 1)
	}
	d.queue.lock.Unlock()

	d.processCh <- false // hash fetcher finished
	blockErr := make(chan error, 1)
	go func() { blockErr <- d.fetchBlocks(1) }()

	// Let several scheduling passes run against the full cache: the guard
	// must stay silent because the head is present and nothing expired.
	time.Sleep(500 * time.Millisecond)
	select {
	case err := <-blockErr:
		t.Fatalf("fetchBlocks returned %v before the import freed capacity", err)
	default:
	}

	// The import drains the cache mid-session. A guard that combined the
	// pre-import zero space with the now-missing head would end the
	// session here with errPeersUnavailable.
	d.process()

	// The fetcher must retry scheduling against the freed space and
	// complete the session normally.
	expectReturn(t, "fetchBlocks", blockErr, nil, 15*time.Second)
}

// TestQueueNoProgress pins the guard predicate queue.NoProgress: the
// genuinely dead state fires, the state left by an import that freed space
// during the pass defers (spaceAtLoop == 0, space > 0, head taken), a busy
// import suppresses the dead-state arm, and room before and after a failed
// pass fires the no-peer-can-serve arm.
func TestQueueNoProgress(t *testing.T) {
	origLimit := blockCacheLimit
	blockCacheLimit = 16
	defer func() { blockCacheLimit = origLimit }()

	// Dead state (issue #247): half cache filled behind the missing head,
	// hashes pending, nothing in flight, no import running.
	dead := newQueue()
	dead.Prepare(1)
	h := make([]types.Hash, 10)
	for i := range h {
		h[i] = testHash('q', uint64(i+1))
	}
	dead.Insert(h, true)
	dead.lock.Lock()
	for i := 1; i <= 8; i++ {
		dead.blockCache[i] = &Block{RawBlock: &nom.DetailedMomentum{Momentum: &nom.Momentum{Hash: h[i], Height: uint64(i + 1)}}}
		dead.blockPool[h[i]] = uint64(i + 1)
	}
	dead.lock.Unlock()
	if err := dead.NoProgress(0, false); err != errStallCacheFull {
		t.Fatalf("dead state: NoProgress = %v, want errStallCacheFull", err)
	}
	if err := dead.NoProgress(0, true); err != nil {
		t.Fatal("dead-state arm fired while an import is running")
	}
	if err := dead.NoProgress(0, false); err != errStallCacheFull {
		t.Fatal("dead state not stable across ticks")
	}

	// Import freed room during the pass: the head was taken and space is
	// available again, but the pass ran against a full cache. Must defer
	// to the next tick, not fire.
	freed := newQueue()
	freed.Prepare(1)
	freed.Insert(h[8:], true) // heights 9 and 10 pending
	freed.lock.Lock()
	freed.blockOffset = 9
	freed.lock.Unlock()
	if err := freed.NoProgress(0, false); err != nil {
		t.Fatal("guard fired although an import freed space during the pass")
	}

	// Room before and after a failed pass: no idle peer could serve.
	room := newQueue()
	room.Prepare(1)
	room.Insert(h, true)
	if err := room.NoProgress(8, false); err != errPeersUnavailable {
		t.Fatalf("guard did not fire with room before and after the pass: %v", err)
	}
	if err := room.NoProgress(0, false); err != nil {
		t.Fatal("guard fired although the pass had no room to reserve anything")
	}
}
