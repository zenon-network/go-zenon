package downloader

import (
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

// TestBlockFetchStallTimeout verifies that fetchBlocks terminates when a
// peer withholds the head block of the download window: the cache fills
// behind the hole, TakeBlocks hands over nothing, and the extended
// peers-unavailable guard ends the session on the spot (issue #247).
//
// The fixture mirrors the production scheduling order: hash replies are
// newest first (fetchHashes inserts them as they arrive with fifo=true),
// so the head of the window ends up at the BOTTOM of the priority queue
// and reservations drain the tail first. The peer below honestly answers
// every request with the non-empty subset it holds — nothing else exists
// on the wire, where an empty BlocksMsg reply is dropped by the handler
// and never reaches the queue. No ignored hashes and no injected empty
// packs are involved: the session reaches zero reserve space, nothing in
// flight and a missing head block purely through the delivery path.
func TestBlockFetchStallTimeout(t *testing.T) {
	origTimeout := stallTimeout
	stallTimeout = 500 * time.Millisecond
	defer func() { stallTimeout = origTimeout }()
	// Shrink the block cache so a small number of withheld blocks fills
	// half of it and disables the free-space arm of the peers-unavailable
	// guard, matching the production stall scenario from issue #247.
	origLimit := blockCacheLimit
	blockCacheLimit = 16
	defer func() { blockCacheLimit = origLimit }()

	d := newIdleDownloader()
	d.cancelCh = make(chan struct{})
	atomic.StoreInt32(&d.synchronising, 1)
	defer atomic.StoreInt32(&d.synchronising, 0)
	defer d.endSession()

	// Register a peer that has every block except the head of the window
	// (height 1). It replies with the subset of each request it can serve;
	// a request it cannot serve at all gets no reply, exactly like the wire,
	// where an empty reply would be dropped and the reservation would stay
	// outstanding until expiry.
	p := newPeer("staller", eth61, types.Hash{}, nil, nil, nil)
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
			case d.blockCh <- blockPack{"staller", blocks}:
			case <-d.cancelCh:
			}
		}()
		return nil
	}
	if err := d.peers.Register(p); err != nil {
		t.Fatalf("register peer: %v", err)
	}

	// Pre-populate the queue with the window newest first, like a
	// production hash reply. Reservations drain heights 10 downwards, the
	// peer fills the cache to exactly half behind the missing head, and
	// the session is left with space == 0, nothing in flight, no cached
	// head and hashes still pending — the issue #247 dead state.
	hashes := make([]types.Hash, 10)
	for i := range hashes {
		hashes[i] = stallHash('s', uint64(len(hashes)-i))
	}
	d.queue.Prepare(1)
	d.queue.Insert(hashes, true)
	d.processCh <- false // hash fetcher finished

	start := time.Now()
	blockErr := make(chan error, 1)
	go func() { blockErr <- d.fetchBlocks(1) }()
	expectReturn(t, "fetchBlocks", blockErr, errPeersUnavailable, 15*time.Second)
	// The guard must end the dead state on the spot. If the session only
	// ends after stallTimeout, the stall deadline covered for the guard
	// and the test no longer discriminates it (removing the guard arm
	// would still pass).
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
// reset when TakeBlocks makes progress: a normal download must not trigger
// the deadline even when stallTimeout is very short.
func TestBlockFetchProgressResetsStallTimer(t *testing.T) {
	orig := stallTimeout
	stallTimeout = 500 * time.Millisecond
	defer func() { stallTimeout = orig }()

	r := newLivenessRun(t, "progress", [][]types.Hash{livenessReply('p', 1, 50), nil})
	hashErr := make(chan error, 1)
	blockErr := make(chan error, 1)
	go func() { hashErr <- r.d.fetchHashes(r.p, 0, 1) }()
	go func() { blockErr <- r.d.fetchBlocks(1) }()

	expectReturn(t, "fetchBlocks", blockErr, nil, 15*time.Second)
	expectReturn(t, "fetchHashes", hashErr, nil, 5*time.Second)
	r.requireImported(t, 1, 50)
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
			return 0, nil // block 1 was handed over; nothing further is takable (2 is withheld in flight)
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

// TestSynchroniseStallDropGatedOnInFlight verifies the drop policy for
// errStallNoProgress: the peer is dropped only when nothing is in flight
// anymore (a sustained withholder); while a request is still in flight the
// session is cancelled without a drop, so an honest slow peer set cannot
// get the best-TD origin removed.
func TestSynchroniseStallDropGatedOnInFlight(t *testing.T) {
	for _, tc := range []struct {
		name     string
		inFlight bool
		wantDrop bool
	}{
		{"drops the withholder when nothing is in flight", false, true},
		{"keeps the origin while a request is in flight", true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var dropped []string
			d := newIdleDownloader()
			d.dropPeer = func(id string) { dropped = append(dropped, id) }
			d.synchroniseMock = func(string, types.Hash) error { return errStallNoProgress }

			if tc.inFlight {
				d.queue.Prepare(1)
				d.queue.Insert([]types.Hash{stallHash('g', 1)}, true)
				p := newPeer("holder", eth61, types.Hash{}, nil, nil, nil)
				if err := d.peers.Register(p); err != nil {
					t.Fatalf("register peer: %v", err)
				}
				if req := d.queue.Reserve(p, 1); req == nil {
					t.Fatal("expected a reserved request")
				}
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

// TestBlockFetchDeadStateGuard verifies the extended peers-unavailable
// guard: when the head block of the window was never delivered, the cache
// is exactly half full and nothing is in flight, no reservation can ever
// succeed and no import can ever run, so the session can never make
// progress again — the guard ends it on the spot instead of waiting for
// the stall deadline (issue #247).
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
	expectReturn(t, "fetchBlocks", blockErr, errPeersUnavailable, 15*time.Second)
	// The guard must fire on the spot, not via the stall deadline backstop.
	if elapsed := time.Since(start); elapsed > stallTimeout {
		t.Fatalf("peers-unavailable guard did not fire on the spot; the %.0fs stall deadline ended the session instead (took %v)", stallTimeout.Seconds(), elapsed)
	}
}
