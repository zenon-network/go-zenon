package fetcher

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common/types"
)

func newTestMomentum(height uint64, prevHash types.Hash) *nom.Momentum {
	m := &nom.Momentum{
		Version:         1,
		ChainIdentifier: 69,
		PreviousHash:    prevHash,
		Height:          height,
		TimestampUnix:   uint64(time.Now().Unix()),
	}
	m.Hash = m.ComputeHash()
	return m
}

type testHarness struct {
	f            *Fetcher
	droppedPeers chan string
	broadcasts   chan *nom.DetailedMomentum
	quit         chan struct{}
	stopOnce     sync.Once
}

func newTestHarness(getBlock blockRetrievalFn, verifyBlock blockVerifierFn, insertChain chainInsertFn) *testHarness {
	return newHookedTestHarness(getBlock, verifyBlock, insertChain, nil, nil)
}

// newHookedTestHarness is newTestHarness with the fetcher's fetchingHook and
// expiredHook set before the loop starts, so a test can observe when hashes
// begin fetching and when timed-out fetches are swept.
func newHookedTestHarness(getBlock blockRetrievalFn, verifyBlock blockVerifierFn, insertChain chainInsertFn, fetchingHook, expiredHook func([]types.Hash)) *testHarness {
	h := &testHarness{
		droppedPeers: make(chan string, 10),
		broadcasts:   make(chan *nom.DetailedMomentum, 10),
		quit:         make(chan struct{}),
	}

	h.f = New(
		getBlock,
		verifyBlock,
		func(block *nom.DetailedMomentum, propagate bool) {
			if propagate {
				h.broadcasts <- block
			}
		},
		func() uint64 { return 1 },
		insertChain,
		func(id string) { h.droppedPeers <- id },
	)
	h.f.fetchingHook = fetchingHook
	h.f.expiredHook = expiredHook

	go func() {
		h.f.loop()
		close(h.quit)
	}()

	return h
}

func (h *testHarness) stop() {
	h.stopOnce.Do(h.f.Stop)
	<-h.quit
}

func TestInsert_VerifyBlockCalled(t *testing.T) {
	parent := newTestMomentum(1, types.Hash{})
	block := newTestMomentum(2, parent.Hash)

	var verifyCalled, insertCalled bool
	var mu sync.Mutex

	h := newTestHarness(
		func(hash types.Hash) *nom.DetailedMomentum {
			if hash == parent.Hash {
				return &nom.DetailedMomentum{Momentum: parent}
			}
			return nil
		},
		func(detailed *nom.DetailedMomentum) error {
			mu.Lock()
			verifyCalled = true
			mu.Unlock()
			return nil
		},
		func(momentums []*nom.DetailedMomentum) (int, error) {
			mu.Lock()
			insertCalled = true
			mu.Unlock()
			return 1, nil
		},
	)
	defer h.stop()

	h.f.insert("test-peer", &nom.DetailedMomentum{Momentum: block})

	// Wait for broadcast (indicates successful full flow)
	select {
	case <-h.broadcasts:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for broadcast")
	}

	mu.Lock()
	defer mu.Unlock()
	if !verifyCalled {
		t.Error("verifyBlock was not called")
	}
	if !insertCalled {
		t.Error("insertChain was not called")
	}
}

func TestInsert_VerifyBlockFails_DropsPeer(t *testing.T) {
	parent := newTestMomentum(1, types.Hash{})
	block := newTestMomentum(2, parent.Hash)

	verifyErr := errors.New("invalid momentum: bad chain identifier")
	var insertCalled bool
	var mu sync.Mutex

	h := newTestHarness(
		func(hash types.Hash) *nom.DetailedMomentum {
			if hash == parent.Hash {
				return &nom.DetailedMomentum{Momentum: parent}
			}
			return nil
		},
		func(detailed *nom.DetailedMomentum) error {
			return verifyErr
		},
		func(momentums []*nom.DetailedMomentum) (int, error) {
			mu.Lock()
			insertCalled = true
			mu.Unlock()
			return 0, nil
		},
	)
	defer h.stop()

	h.f.insert("bad-peer", &nom.DetailedMomentum{Momentum: block})

	// Should drop the peer
	select {
	case peer := <-h.droppedPeers:
		if peer != "bad-peer" {
			t.Errorf("expected peer 'bad-peer' to be dropped, got %q", peer)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for peer drop")
	}

	// Should NOT broadcast
	select {
	case <-h.broadcasts:
		t.Error("broadcastBlock should not be called when verification fails")
	case <-time.After(500 * time.Millisecond):
		// expected
	}

	mu.Lock()
	defer mu.Unlock()
	if insertCalled {
		t.Error("insertChain should not be called when verification fails")
	}
}

func TestInsert_InsertChainFails_NoBroadcast(t *testing.T) {
	parent := newTestMomentum(1, types.Hash{})
	block := newTestMomentum(2, parent.Hash)

	insertErr := errors.New("VM execution failed")
	var verifyCalled bool
	var mu sync.Mutex

	h := newTestHarness(
		func(hash types.Hash) *nom.DetailedMomentum {
			if hash == parent.Hash {
				return &nom.DetailedMomentum{Momentum: parent}
			}
			return nil
		},
		func(detailed *nom.DetailedMomentum) error {
			mu.Lock()
			verifyCalled = true
			mu.Unlock()
			return nil
		},
		func(momentums []*nom.DetailedMomentum) (int, error) {
			return 0, insertErr
		},
	)
	defer h.stop()

	h.f.insert("test-peer", &nom.DetailedMomentum{Momentum: block})

	// Wait for the done signal (insert completes even on failure)
	select {
	case <-h.droppedPeers:
		t.Error("peer should not be dropped on insertion failure")
	case <-time.After(1 * time.Second):
		// expected — insert() sends to done channel, not droppedPeers
	}

	// Should NOT broadcast
	select {
	case <-h.broadcasts:
		t.Error("broadcastBlock should not be called when insertion fails")
	case <-time.After(500 * time.Millisecond):
		// expected
	}

	mu.Lock()
	defer mu.Unlock()
	if !verifyCalled {
		t.Error("verifyBlock should be called even when insertion fails")
	}
}

func TestInsert_Success_BroadcastsAfterInsertion(t *testing.T) {
	parent := newTestMomentum(1, types.Hash{})
	block := newTestMomentum(2, parent.Hash)

	var verifyTime, insertTime time.Time
	var mu sync.Mutex

	h := newTestHarness(
		func(hash types.Hash) *nom.DetailedMomentum {
			if hash == parent.Hash {
				return &nom.DetailedMomentum{Momentum: parent}
			}
			return nil
		},
		func(detailed *nom.DetailedMomentum) error {
			mu.Lock()
			verifyTime = time.Now()
			mu.Unlock()
			return nil
		},
		func(momentums []*nom.DetailedMomentum) (int, error) {
			mu.Lock()
			insertTime = time.Now()
			mu.Unlock()
			return 1, nil
		},
	)
	defer h.stop()

	h.f.insert("test-peer", &nom.DetailedMomentum{Momentum: block})

	select {
	case received := <-h.broadcasts:
		if received.Momentum.Hash != block.Hash {
			t.Errorf("broadcast received wrong block: got %v, want %v", received.Momentum.Hash, block.Hash)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for broadcast")
	}

	mu.Lock()
	defer mu.Unlock()
	if verifyTime.IsZero() {
		t.Error("verifyBlock was not called")
	}
	if insertTime.IsZero() {
		t.Error("insertChain was not called")
	}
	// Verify must happen before insert
	if !verifyTime.Before(insertTime) && !verifyTime.Equal(insertTime) {
		t.Error("verifyBlock should be called before insertChain")
	}
}

func TestFilter_ExplicitFetch_AttributesToActualSender(t *testing.T) {
	parent := newTestMomentum(1, types.Hash{})
	block := newTestMomentum(2, parent.Hash)

	h := newTestHarness(
		func(hash types.Hash) *nom.DetailedMomentum {
			if hash == parent.Hash {
				return &nom.DetailedMomentum{Momentum: parent}
			}
			return nil
		},
		func(detailed *nom.DetailedMomentum) error {
			return errors.New("invalid momentum: bad signature")
		},
		func(momentums []*nom.DetailedMomentum) (int, error) {
			return 0, nil
		},
	)
	defer h.stop()

	// "announcing-peer" announces the hash; it never actually delivers it.
	if err := h.f.Notify("announcing-peer", block.Hash, time.Now(), func(hashes []types.Hash) error {
		return nil
	}); err != nil {
		t.Fatalf("Notify failed: %v", err)
	}

	// Wait for the fetcher to move the hash into the "fetching" state, i.e.
	// past arriveTimeout, so the subsequent delivery is treated as an
	// explicit fetch response rather than an unsolicited block.
	time.Sleep(arriveTimeout + 250*time.Millisecond)

	// A different peer ("sending-peer") delivers the (malformed) momentum in
	// response - this must be attributed to sending-peer, not announcing-peer.
	remaining := h.f.Filter("sending-peer", []*nom.DetailedMomentum{{Momentum: block}})
	if len(remaining) != 0 {
		t.Fatalf("expected explicit fetch to be consumed, got %d remaining", len(remaining))
	}

	// Only the peer that actually delivered the malformed momentum should be dropped.
	select {
	case peer := <-h.droppedPeers:
		if peer != "sending-peer" {
			t.Errorf("expected 'sending-peer' to be dropped, got %q", peer)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for peer drop")
	}

	select {
	case peer := <-h.droppedPeers:
		t.Errorf("announcing peer must not be penalized, but dropped: %q", peer)
	case <-time.After(500 * time.Millisecond):
		// expected
	}
}

func TestInsert_ParentUnknown_Aborts(t *testing.T) {
	block := newTestMomentum(2, types.Hash{0xff})

	var verifyCalled, insertCalled bool
	var mu sync.Mutex

	h := newTestHarness(
		func(hash types.Hash) *nom.DetailedMomentum {
			return nil // parent not found
		},
		func(detailed *nom.DetailedMomentum) error {
			mu.Lock()
			verifyCalled = true
			mu.Unlock()
			return nil
		},
		func(momentums []*nom.DetailedMomentum) (int, error) {
			mu.Lock()
			insertCalled = true
			mu.Unlock()
			return 1, nil
		},
	)
	defer h.stop()

	h.f.insert("test-peer", &nom.DetailedMomentum{Momentum: block})

	// Should NOT broadcast
	select {
	case <-h.broadcasts:
		t.Error("broadcastBlock should not be called when parent is unknown")
	case <-time.After(500 * time.Millisecond):
		// expected
	}

	// Should NOT drop peer (parent unknown is not a misbehavior)
	select {
	case <-h.droppedPeers:
		t.Error("peer should not be dropped when parent is unknown")
	case <-time.After(500 * time.Millisecond):
		// expected
	}

	mu.Lock()
	defer mu.Unlock()
	if verifyCalled {
		t.Error("verifyBlock should not be called when parent is unknown")
	}
	if insertCalled {
		t.Error("insertChain should not be called when parent is unknown")
	}
}

// The per-peer announce counter is the fetcher's only guard against a peer
// filling its announced and fetching tables. The tests below drive whole
// announce lifecycles through the real loop and then, with the loop stopped,
// compare the counter with the entries it is supposed to count.

// importedSet lets a test flip getBlock from "unknown" to "known" for chosen
// hashes while the loop is running.
type importedSet struct {
	mu     sync.Mutex
	blocks map[types.Hash]*nom.DetailedMomentum
}

func (s *importedSet) get(hash types.Hash) *nom.DetailedMomentum {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.blocks[hash]
}

func (s *importedSet) add(blocks []*nom.DetailedMomentum) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, b := range blocks {
		s.blocks[b.Momentum.Hash] = b
	}
}

// accountingEvents carries the fetcher's test hooks: each batch of hashes as
// it begins fetching, and each batch swept after timing out.
type accountingEvents struct {
	fetching chan []types.Hash
	expired  chan []types.Hash
}

// newAccountingHarness returns a harness whose chain knows only what the test
// marks imported, the hook events, and n distinct blocks to announce.
func newAccountingHarness(n int) (*testHarness, *importedSet, *accountingEvents, []*nom.DetailedMomentum) {
	imported := &importedSet{blocks: make(map[types.Hash]*nom.DetailedMomentum)}
	// In these tests each hash begins fetching at most once and expires at
	// most once, and every hook call carries at least one hash, so n slots
	// keep the hooks from ever blocking the loop however the events are
	// batched. A test that re-announced an expired hash would need more.
	events := &accountingEvents{
		fetching: make(chan []types.Hash, n),
		expired:  make(chan []types.Hash, n),
	}
	h := newHookedTestHarness(
		imported.get,
		func(*nom.DetailedMomentum) error { return nil },
		func([]*nom.DetailedMomentum) (int, error) { return 0, nil },
		func(hashes []types.Hash) { events.fetching <- hashes },
		func(hashes []types.Hash) { events.expired <- hashes },
	)
	return h, imported, events, testBlocks(n)
}

// testBlocks returns n distinct chained blocks starting at height 2.
func testBlocks(n int) []*nom.DetailedMomentum {
	blocks := make([]*nom.DetailedMomentum, n)
	prev := types.Hash{}
	for i := range blocks {
		m := newTestMomentum(uint64(i+2), prev)
		blocks[i] = &nom.DetailedMomentum{Momentum: m}
		prev = m.Hash
	}
	return blocks
}

// announceAll sends every block's hash to the fetcher on behalf of peer with a
// request function that never delivers. Whether each was accepted is read
// from the fetcher's tables once the loop is stopped.
func announceAll(t *testing.T, h *testHarness, peer string, blocks []*nom.DetailedMomentum) {
	t.Helper()
	for _, b := range blocks {
		if err := h.f.Notify(peer, b.Momentum.Hash, time.Now(), func([]types.Hash) error { return nil }); err != nil {
			t.Fatalf("Notify: %v", err)
		}
	}
}

// waitForHashes blocks until n hashes have been reported on events, or fails
// the test once the deadline passes.
func waitForHashes(t *testing.T, events <-chan []types.Hash, n int, deadline time.Duration, what string) {
	t.Helper()
	timeout := time.After(deadline)
	for got := 0; got < n; {
		select {
		case hashes := <-events:
			got += len(hashes)
		case <-timeout:
			t.Fatalf("only %d of %d hashes %s before the deadline", got, n, what)
		}
	}
}

// waitForFetching blocks until n hashes have been handed to a fetch request,
// which happens after the loop has moved them into the fetching table.
func waitForFetching(t *testing.T, events *accountingEvents, n int) {
	t.Helper()
	waitForHashes(t, events.fetching, n, 10*arriveTimeout, "began fetching")
}

// waitForExpiry blocks until n fetches have timed out and been swept, which
// happens after the loop has removed them and released their allowance.
func waitForExpiry(t *testing.T, events *accountingEvents, n int) {
	t.Helper()
	waitForHashes(t, events.expired, n, fetchTimeout+10*arriveTimeout, "expired")
}

// checkAccounting compares, on a stopped fetcher, each peer's counter with the
// announced and fetching entries attributed to it, and fails on any negative
// or stale counter.
func checkAccounting(t *testing.T, f *Fetcher) {
	t.Helper()
	want := make(map[string]int)
	for _, announces := range f.announced {
		for _, a := range announces {
			want[a.origin]++
		}
	}
	for _, a := range f.fetching {
		want[a.origin]++
	}
	for peer, count := range f.announces {
		if count <= 0 {
			t.Errorf("peer %q: counter %d is not positive", peer, count)
		}
		if count != want[peer] {
			t.Errorf("peer %q: counter %d, but %d announced or fetching entries", peer, count, want[peer])
		}
	}
	for peer, count := range want {
		if _, ok := f.announces[peer]; !ok {
			t.Errorf("peer %q: no counter, but %d announced or fetching entries", peer, count)
		}
	}
}

// A hash that is announced, fetched and then found imported at delivery must
// leave the announcing peer's counter exactly where it started.
func TestAnnounces_ExactThroughFetchAndCompletion(t *testing.T) {
	h, imported, events, blocks := newAccountingHarness(8)
	defer h.stop()

	announceAll(t, h, "announcer", blocks)
	waitForFetching(t, events, len(blocks))

	// Delivery finds every block already imported, which is the completion
	// path that does not go through the import queue.
	imported.add(blocks)
	if rest := h.f.Filter("deliverer", blocks); len(rest) != 0 {
		t.Fatalf("%d explicitly fetched blocks were not consumed", len(rest))
	}

	h.stop()
	if len(h.f.announced) != 0 || len(h.f.fetching) != 0 {
		t.Fatalf("%d announced and %d fetching entries remain", len(h.f.announced), len(h.f.fetching))
	}
	checkAccounting(t, h.f)
	if count, ok := h.f.announces["announcer"]; ok {
		t.Fatalf("announcer's counter is %d after all its hashes completed, want no entry", count)
	}
}

// A hash whose fetch times out must be forgotten with the same effect on the
// counter as a completed one.
func TestAnnounces_ExactThroughFetchTimeout(t *testing.T) {
	if testing.Short() {
		t.Skip("waits out fetchTimeout")
	}
	h, _, events, blocks := newAccountingHarness(8)
	defer h.stop()

	announceAll(t, h, "announcer", blocks)
	waitForFetching(t, events, len(blocks))
	// Nothing else wakes the loop from here on, so the sweep that reports
	// these hashes ran off the expiry timer.
	waitForExpiry(t, events, len(blocks))

	h.stop()
	if len(h.f.fetching) != 0 {
		t.Fatalf("%d fetching entries survived fetchTimeout", len(h.f.fetching))
	}
	checkAccounting(t, h.f)
	if count, ok := h.f.announces["announcer"]; ok {
		t.Fatalf("announcer's counter is %d after all its fetches timed out, want no entry", count)
	}
}

// Hashes in the fetching table are still outstanding work for the peer that
// announced them, so they count toward HashLimit.
func TestAnnounces_FetchingCountsTowardHashLimit(t *testing.T) {
	h, _, events, blocks := newAccountingHarness(HashLimit + 8)
	defer h.stop()

	announceAll(t, h, "announcer", blocks[:HashLimit])
	waitForFetching(t, events, HashLimit)
	announceAll(t, h, "announcer", blocks[HashLimit:])

	h.stop()
	if len(h.f.fetching) != HashLimit {
		t.Fatalf("%d fetching entries, want %d", len(h.f.fetching), HashLimit)
	}
	if len(h.f.announced) != 0 {
		t.Fatalf("%d announces accepted past HashLimit while %d hashes were fetching", len(h.f.announced), HashLimit)
	}
	checkAccounting(t, h.f)
}

// Repeated lifecycles must not widen the set of announces a peer may hold:
// after a full batch completes, the next batch is still bounded by HashLimit.
func TestAnnounces_LimitHoldsAcrossLifecycles(t *testing.T) {
	h, imported, events, blocks := newAccountingHarness(2*HashLimit + 8)
	defer h.stop()

	for round := 0; round < 2; round++ {
		batch := blocks[round*HashLimit : (round+1)*HashLimit]
		announceAll(t, h, "announcer", batch)
		waitForFetching(t, events, HashLimit)
		imported.add(batch)
		h.f.Filter("deliverer", batch)
	}
	announceAll(t, h, "announcer", blocks[2*HashLimit:])
	announceAll(t, h, "announcer", blocks[:HashLimit]) // already imported hashes are still announces

	h.stop()
	// Some of the final announces may already have moved to fetching, or
	// been dropped as known, if a timer fired meanwhile; the bound is on
	// what the peer holds in total, not on the announced table alone.
	if got := len(h.f.announced) + len(h.f.fetching); got > HashLimit {
		t.Fatalf("%d announced or fetching entries held after two completed lifecycles, want at most %d", got, HashLimit)
	}
	checkAccounting(t, h.f)
}

// Fetches that time out while the loop is idle must free the peer's
// allowance before its next announce is judged, not after.
func TestAnnounces_ExpiredFetchesFreeAllowanceBeforeNextAnnounce(t *testing.T) {
	if testing.Short() {
		t.Skip("waits out fetchTimeout")
	}
	h, _, events, blocks := newAccountingHarness(HashLimit + 8)
	defer h.stop()

	announceAll(t, h, "announcer", blocks[:HashLimit])
	waitForFetching(t, events, HashLimit)
	// Let every fetch expire with the loop otherwise idle: nothing else
	// wakes it between here and the next announce.
	waitForExpiry(t, events, HashLimit)
	announceAll(t, h, "announcer", blocks[HashLimit:])

	h.stop()
	// The new announces may already have moved to fetching if a timer fired
	// meanwhile, so check the exact set the peer holds across both tables.
	expectHeld(t, h.f, blocks[HashLimit:], blocks[:HashLimit])
	checkAccounting(t, h.f)
}

// Re-announcing a hash that is already fetching is dropped without charging
// anyone: not the peer whose fetch it is, and not another peer.
func TestAnnounces_ReannounceOfFetchingHashIsNotCharged(t *testing.T) {
	h, _, events, blocks := newAccountingHarness(8)
	defer h.stop()

	announceAll(t, h, "announcer", blocks)
	waitForFetching(t, events, len(blocks))
	announceAll(t, h, "announcer", blocks)
	announceAll(t, h, "other", blocks)

	h.stop()
	expectHeld(t, h.f, blocks, nil)
	if len(h.f.announced) != 0 {
		t.Fatalf("%d re-announces of fetching hashes were kept", len(h.f.announced))
	}
	checkAccounting(t, h.f)
	if count := h.f.announces["announcer"]; count != len(blocks) {
		t.Fatalf("announcer charged %d for %d fetches after re-announcing them", count, len(blocks))
	}
	if count, ok := h.f.announces["other"]; ok {
		t.Fatalf("other charged %d for re-announcing hashes it never fetches", count)
	}
}

// When several peers announce one hash, the fetch is charged to the peer it
// was requested from and the other announcers are released.
func TestAnnounces_OnlySelectedAnnouncerIsChargedForFetch(t *testing.T) {
	h, _, events, blocks := newAccountingHarness(8)
	defer h.stop()

	// Announce each hash from every peer before moving to the next, so the
	// three announces of one hash are microseconds apart and resident
	// together long before the announce timer can fire.
	peers := []string{"first", "second", "third"}
	for _, b := range blocks {
		for _, peer := range peers {
			announceAll(t, h, peer, []*nom.DetailedMomentum{b})
		}
	}
	waitForFetching(t, events, len(blocks))

	h.stop()
	expectHeld(t, h.f, blocks, nil)
	if len(h.f.announced) != 0 {
		t.Fatalf("%d announces survived the transition to fetching", len(h.f.announced))
	}
	checkAccounting(t, h.f)
	total := 0
	for _, count := range h.f.announces {
		total += count
	}
	if total != len(blocks) {
		t.Fatalf("%d charged across %d announcers for %d fetches, want one charge per fetch", total, len(peers), len(blocks))
	}
}

// A hash that becomes known before its announce timer fires is released at
// the transition and never charged as a fetch.
func TestAnnounces_KnownAtTransitionIsNotCharged(t *testing.T) {
	h, imported, events, blocks := newAccountingHarness(9)
	defer h.stop()

	known, probe := blocks[:8], blocks[8:]
	// Known before they are announced: admission does not consult the
	// chain, so the announces are accepted and the transition finds them.
	imported.add(known)
	announceAll(t, h, "announcer", known)
	// The probe is announced after the known hashes, so by the time it
	// begins fetching the known hashes have been through the transition.
	announceAll(t, h, "probe", probe)
	waitForFetching(t, events, len(probe))

	h.stop()
	expectHeld(t, h.f, probe, known)
	checkAccounting(t, h.f)
	if count, ok := h.f.announces["announcer"]; ok {
		t.Fatalf("announcer charged %d for hashes that were known at the transition", count)
	}
}

// farBlock returns a block too far above the chain head for enqueue to accept.
func farBlock() []*nom.DetailedMomentum {
	return []*nom.DetailedMomentum{{Momentum: newTestMomentum(uint64(maxQueueDist)+10, types.Hash{})}}
}

// deliverRefused announces far on behalf of announcer, waits for it to begin
// fetching, and delivers it, which the import queue refuses for distance.
func deliverRefused(t *testing.T, h *testHarness, events *accountingEvents, far []*nom.DetailedMomentum) {
	t.Helper()
	announceAll(t, h, "announcer", far)
	waitForFetching(t, events, 1)
	if rest := h.f.Filter("deliverer", far); len(rest) != 0 {
		t.Fatalf("the explicitly fetched block was not consumed")
	}
}

// A delivered block that the import queue refuses leaves its fetching entry
// in place and still charged to the announcer.
func TestAnnounces_RejectedDeliveryStaysCharged(t *testing.T) {
	h, _, events, _ := newAccountingHarness(1)
	defer h.stop()

	far := farBlock()
	deliverRefused(t, h, events, far)

	h.stop()
	expectHeld(t, h.f, far, nil)
	if len(h.f.fetching) != 1 {
		t.Fatalf("%d fetching entries after a refused delivery, want the refused fetch", len(h.f.fetching))
	}
	checkAccounting(t, h.f)
	if count := h.f.announces["announcer"]; count != 1 {
		t.Fatalf("announcer charged %d while its refused fetch is still pending, want 1", count)
	}
}

// The refused fetch is released by the sweep when it times out.
func TestAnnounces_RejectedDeliveryReleasedAtTimeout(t *testing.T) {
	if testing.Short() {
		t.Skip("waits out fetchTimeout")
	}
	h, _, events, _ := newAccountingHarness(1)
	defer h.stop()

	far := farBlock()
	deliverRefused(t, h, events, far)
	// The sweep reports only entries it removed, so this event shows the
	// refused delivery left the fetch in place until it timed out.
	waitForExpiry(t, events, 1)

	h.stop()
	expectHeld(t, h.f, nil, far)
	checkAccounting(t, h.f)
	if count, ok := h.f.announces["announcer"]; ok {
		t.Fatalf("announcer still charged %d after its refused fetch timed out", count)
	}
}

// expectHeld fails unless the fetcher holds exactly the want blocks, each in
// announced or fetching, and none of the gone blocks.
func expectHeld(t *testing.T, f *Fetcher, want, gone []*nom.DetailedMomentum) {
	t.Helper()
	held := func(hash types.Hash) bool {
		_, announced := f.announced[hash]
		_, fetching := f.fetching[hash]
		return announced || fetching
	}
	for i, b := range want {
		if !held(b.Momentum.Hash) {
			t.Errorf("wanted block %d is neither announced nor fetching", i)
		}
	}
	for i, b := range gone {
		if held(b.Momentum.Hash) {
			t.Errorf("block %d should be gone but is still announced or fetching", i)
		}
	}
	if total := len(f.announced) + len(f.fetching); total != len(want) {
		t.Errorf("%d announced or fetching entries, want %d", total, len(want))
	}
}

// newIdleFetcher returns a fetcher whose loop is not running, for driving
// the admission path directly with hand-built state.
func newIdleFetcher() *Fetcher {
	return New(
		func(types.Hash) *nom.DetailedMomentum { return nil },
		func(*nom.DetailedMomentum) error { return nil },
		func(*nom.DetailedMomentum, bool) {},
		func() uint64 { return 1 },
		func([]*nom.DetailedMomentum) (int, error) { return 0, nil },
		func(string) {},
	)
}

// fillFetching puts HashLimit fetches from peer into the fetcher, all with
// the given announce time, and charges the peer for them as the loop would.
func fillFetching(f *Fetcher, peer string, blocks []*nom.DetailedMomentum, at time.Time) {
	for _, b := range blocks {
		f.fetching[b.Momentum.Hash] = &announce{hash: b.Momentum.Hash, time: at, origin: peer}
	}
	f.announces[peer] = len(blocks)
	f.oldestFetch = at
}

// A peer at its limit whose fetches have timed out since the last sweep has
// them released before its next announce is judged. This is the state
// between the loop's sweep and its select when the timeout passes and a
// notification wins the select ahead of the expiry timer.
func TestReleaseExpired_FreesAllowanceOfTimedOutFetches(t *testing.T) {
	blocks := testBlocks(HashLimit)
	f := newIdleFetcher()
	stale := time.Now().Add(-fetchTimeout - time.Second)
	fillFetching(f, "announcer", blocks, stale)

	f.releaseExpired("announcer")

	expectHeld(t, f, nil, blocks)
	checkAccounting(t, f)
	if count := f.announces["announcer"]; count+1 > HashLimit {
		t.Fatalf("announcer still charged %d after every fetch timed out, next announce would be refused", count)
	}
}

// Live fetches are left alone: a peer at its limit stays refused, and a
// peer under its limit never triggers a sweep.
func TestReleaseExpired_LeavesLiveFetchesAlone(t *testing.T) {
	blocks := testBlocks(HashLimit)
	f := newIdleFetcher()
	fillFetching(f, "announcer", blocks, time.Now())

	f.releaseExpired("announcer")

	expectHeld(t, f, blocks, nil)
	checkAccounting(t, f)
	if count := f.announces["announcer"]; count+1 <= HashLimit {
		t.Fatalf("announcer charged %d with %d live fetches, next announce would be accepted", count, HashLimit)
	}

	// Under the limit with stale fetches: the sweep is not this peer's
	// problem, and nothing is released on its behalf.
	other := testBlocks(HashLimit / 2)
	g := newIdleFetcher()
	fillFetching(g, "quiet", other, time.Now().Add(-fetchTimeout-time.Second))
	g.releaseExpired("quiet")
	expectHeld(t, g, other, nil)
}
