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

// TestBlockFetchStallTimeout verifies that fetchBlocks terminates with
// errStallNoProgress when a peer withholds the head block of the download
// window: the cache fills behind the hole, TakeBlocks hands over nothing,
// and without a session-level deadline the loop would spin forever
// (issue #247).
func TestBlockFetchStallTimeout(t *testing.T) {
	origTimeout := stallTimeout
	stallTimeout = 500 * time.Millisecond
	defer func() { stallTimeout = origTimeout }()
	// Shrink the block cache so a small number of withheld blocks fills
	// half of it and disables the peers-unavailable guard, matching the
	// production stall scenario from issue #247.
	origLimit := blockCacheLimit
	blockCacheLimit = 16
	defer func() { blockCacheLimit = origLimit }()

	d := newIdleDownloader()
	d.cancelCh = make(chan struct{})
	atomic.StoreInt32(&d.synchronising, 1)
	defer atomic.StoreInt32(&d.synchronising, 0)
	defer d.endSession()

	// Register a peer that delivers all blocks except height 1.
	p := newPeer("staller", eth61, types.Hash{}, nil, nil, nil)
	p.getBlocks = func(hashes []types.Hash) error {
		blocks := make([]*nom.DetailedMomentum, 0, len(hashes))
		for _, h := range hashes {
			height := stallHeight(h)
			if height > 1 {
				blocks = append(blocks, &nom.DetailedMomentum{
					Momentum: &nom.Momentum{Hash: h, Height: height},
				})
			}
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

	// Pre-populate the queue with hashes for heights 1..10 and signal
	// that the hash fetcher is done.
	d.queue.Prepare(1)
	d.queue.Insert([]types.Hash{
		stallHash('s', 1), stallHash('s', 2), stallHash('s', 3),
		stallHash('s', 4), stallHash('s', 5), stallHash('s', 6),
		stallHash('s', 7), stallHash('s', 8), stallHash('s', 9),
		stallHash('s', 10),
	}, false)
	d.processCh <- false // hash fetcher finished

	blockErr := make(chan error, 1)
	go func() { blockErr <- d.fetchBlocks(1) }()
	expectReturn(t, "fetchBlocks", blockErr, errStallNoProgress, 15*time.Second)
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
	d.processCh <- false // hash fetcher done, no hashes to fetch

	blockErr := make(chan error, 1)
	go func() { blockErr <- d.fetchBlocks(1) }()
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
			return 0, nil // no handover: BlockOffset does not advance
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
