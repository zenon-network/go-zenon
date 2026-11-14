package protocol

import (
	"errors"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/p2p"
	"github.com/zenon-network/go-zenon/p2p/discover"
)

// readinessClock replaces common.Clock for a test and is advanced by hand.
type readinessClock struct{ t time.Time }

func (c *readinessClock) Now() time.Time { return c.t }

func useReadinessClock(t *testing.T) *readinessClock {
	t.Helper()
	saved := common.Clock
	clock := &readinessClock{t: time.Unix(1_700_000_000, 0)}
	common.Clock = clock
	t.Cleanup(func() { common.Clock = saved })
	return clock
}

// readinessBridge is a stub bridge with a settable frontier height and a
// settable insert outcome.
type readinessBridge struct {
	stubChainBridge
	height uint64
	// moved is what InsertChainMoved reports; false models a batch the
	// node already had.
	moved bool
}

func (b *readinessBridge) CurrentBlock() *nom.Momentum { return &nom.Momentum{Height: b.height} }
func (b *readinessBridge) InsertChainMoved([]*nom.DetailedMomentum) (int, bool, error) {
	return 0, b.moved, nil
}

type readinessPeer struct{ id discover.NodeID }

func (s *readinessPeer) ID() discover.NodeID       { return s.id }
func (s *readinessPeer) Name() string              { return "stub" }
func (s *readinessPeer) Caps() []p2p.Cap           { return nil }
func (s *readinessPeer) RemoteAddr() net.Addr      { return &net.TCPAddr{} }
func (s *readinessPeer) Disconnect(p2p.DiscReason) {}

type readinessRW struct{}

func (s *readinessRW) ReadMsg() (p2p.Msg, error) { return p2p.Msg{}, errors.New("no messages") }
func (s *readinessRW) WriteMsg(p2p.Msg) error    { return nil }

// addPeer registers a peer advertising td and returns it.
func addPeer(t *testing.T, pm *ProtocolManager, id byte, td uint64) *peer {
	t.Helper()
	var nodeID discover.NodeID
	nodeID[0] = id
	p := newPeer(1, 1, &readinessPeer{id: nodeID}, &readinessRW{})
	p.SetTd(td)
	if err := pm.peers.Register(p); err != nil {
		t.Fatal(err)
	}
	return p
}

func newReadinessManager(t *testing.T, minPeers int, height uint64) (*ProtocolManager, *readinessBridge) {
	t.Helper()
	bridge := &readinessBridge{height: height}
	pm := NewProtocolManager(minPeers, 1, bridge)
	t.Cleanup(pm.Stop)
	return pm, bridge
}

func TestReadyToProduce_NotEnoughPeersBlocks(t *testing.T) {
	clock := useReadinessClock(t)
	pm, _ := newReadinessManager(t, 2, 10)
	addPeer(t, pm, 1, 10)
	if pm.ReadyToProduce() {
		t.Fatal("ready with fewer peers than the minimum")
	}
	if pm.SyncInfo().State != NotEnoughPeers {
		t.Fatalf("SyncInfo state = %v, want NotEnoughPeers", pm.SyncInfo().State)
	}
	// The peer minimum is not a claim and does not lapse with the window.
	clock.t = clock.t.Add(2 * producerStallWindow)
	if pm.ReadyToProduce() {
		t.Fatal("ready with fewer peers than the minimum once the window elapsed")
	}
	if pm.readyToProduce(&handoffDownloader{session: true}) {
		t.Fatal("ready with fewer peers than the minimum while a session runs")
	}
}

func TestReadyToProduce_CaughtUpIsReady(t *testing.T) {
	useReadinessClock(t)
	pm, _ := newReadinessManager(t, 1, 10)
	addPeer(t, pm, 1, 10)
	if !pm.ReadyToProduce() {
		t.Fatal("not ready although the frontier matches the best advertised height")
	}
}

// An advertised height above the frontier holds production only for the
// stall window when no download session imports anything; SyncInfo keeps
// reporting Syncing throughout.
func TestReadyToProduce_UnverifiedAdvertisementHoldsOnlyForTheWindow(t *testing.T) {
	clock := useReadinessClock(t)
	pm, _ := newReadinessManager(t, 1, 10)
	addPeer(t, pm, 1, 12)

	if pm.ReadyToProduce() {
		t.Fatal("ready right after a higher advertisement, before the window elapsed")
	}
	clock.t = clock.t.Add(producerStallWindow - time.Second)
	if pm.ReadyToProduce() {
		t.Fatal("ready before the window elapsed")
	}
	clock.t = clock.t.Add(time.Second)
	if !pm.ReadyToProduce() {
		t.Fatal("still blocked after the window elapsed with no import")
	}
	if info := pm.SyncInfo(); info.State != Syncing || info.TargetHeight != 12 {
		t.Fatalf("SyncInfo changed meaning: %+v", info)
	}
}

// A peer reconnecting, or another peer advertising the same unverified
// height, does not restart the window.
func TestReadyToProduce_ReconnectDoesNotRenewTheHold(t *testing.T) {
	clock := useReadinessClock(t)
	pm, _ := newReadinessManager(t, 1, 10)
	p := addPeer(t, pm, 1, 12)
	clock.t = clock.t.Add(producerStallWindow)
	if !pm.ReadyToProduce() {
		t.Fatal("not ready after the window")
	}
	if err := pm.peers.Unregister(p.id); err != nil {
		t.Fatal(err)
	}
	addPeer(t, pm, 1, 12)
	addPeer(t, pm, 2, 15)
	if !pm.ReadyToProduce() {
		t.Fatal("a reconnect or a new advertisement renewed the hold")
	}
}

// Removing the only taller peer makes the node caught up at once.
func TestReadyToProduce_RemovingTheTallerPeerReleases(t *testing.T) {
	useReadinessClock(t)
	pm, _ := newReadinessManager(t, 1, 10)
	addPeer(t, pm, 1, 10)
	taller := addPeer(t, pm, 2, 12)
	if pm.ReadyToProduce() {
		t.Fatal("ready while a taller peer is connected inside the window")
	}
	if err := pm.peers.Unregister(taller.id); err != nil {
		t.Fatal(err)
	}
	if !pm.ReadyToProduce() {
		t.Fatal("not ready after the taller peer left")
	}
}

// A batch imported by a download session that left new momentums on the
// chain is the one thing that restarts the window; an import through the
// fetcher's path does not, and neither does a download batch the node
// already had.
func TestReadyToProduce_DownloadImportRestartsTheWindow(t *testing.T) {
	clock := useReadinessClock(t)
	pm, bridge := newReadinessManager(t, 1, 10)
	addPeer(t, pm, 1, 12)
	clock.t = clock.t.Add(producerStallWindow)
	if !pm.ReadyToProduce() {
		t.Fatal("not ready after the window")
	}

	if _, err := bridge.InsertChain(nil); err != nil {
		t.Fatal(err)
	}
	if !pm.ReadyToProduce() {
		t.Fatal("a fetcher-path import restarted the window")
	}

	// A batch the node already had: the frontier did not move.
	if _, err := pm.insertFromDownload(nil); err != nil {
		t.Fatal(err)
	}
	if !pm.ReadyToProduce() {
		t.Fatal("a download batch that moved nothing restarted the window")
	}

	bridge.moved = true
	if _, err := pm.insertFromDownload(nil); err != nil {
		t.Fatal(err)
	}
	if pm.ReadyToProduce() {
		t.Fatal("ready right after a download session imported a batch")
	}
	clock.t = clock.t.Add(producerStallWindow)
	if !pm.ReadyToProduce() {
		t.Fatal("still blocked a full window after the last import")
	}

	// A failed import that moved nothing is not progress; one that left a
	// valid prefix on the chain before failing is.
	clock.t = clock.t.Add(time.Second)
	bridge.height = 10
	failing := &failingInsertBridge{readinessBridge: bridge}
	pm.chainman = failing
	bridge.moved = false
	if _, err := pm.insertFromDownload(nil); err == nil {
		t.Fatal("expected the injected failure")
	}
	if !pm.ReadyToProduce() {
		t.Fatal("a failed import that moved nothing restarted the window")
	}
	bridge.moved = true
	if _, err := pm.insertFromDownload(nil); err == nil {
		t.Fatal("expected the injected failure")
	}
	if pm.ReadyToProduce() {
		t.Fatal("a kept prefix before a failure is progress and must restart the window")
	}
}

type failingInsertBridge struct{ *readinessBridge }

func (b *failingInsertBridge) InsertChain([]*nom.DetailedMomentum) (int, error) {
	return 0, errors.New("injected import failure")
}
func (b *failingInsertBridge) InsertChainMoved([]*nom.DetailedMomentum) (int, bool, error) {
	return 0, b.moved, errors.New("injected import failure")
}

// A running download session holds production while the node has imported
// a download batch within the stall window, also when the frontier matches
// the target. A session that has gone the window without an import holds
// nothing: it is the claim that started it, still unverified.
func TestReadyToProduce_RunningSessionHoldsOnlyWhileImporting(t *testing.T) {
	clock := useReadinessClock(t)
	pm, bridge := newReadinessManager(t, 1, 10)
	p := addPeer(t, pm, 1, 12)
	clock.t = clock.t.Add(2 * producerStallWindow)
	if !pm.ReadyToProduce() {
		t.Fatal("not ready before the session")
	}

	entered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	// The session's first request is a hash fetch; whichever form the
	// downloader picks, block in it so the session is observably running.
	hashFetch := func() error {
		once.Do(func() { close(entered) })
		<-release
		return nil
	}
	// The downloader only runs eth/61 sessions; the peer's own version
	// field is the manager's concern.
	err := pm.downloader.RegisterPeer(p.id, int(ProtocolVersions[0]), p.Head(),
		func(types.Hash) error { return hashFetch() },
		func(uint64, int) error { return hashFetch() },
		func([]types.Hash) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		pm.downloader.Synchronise(p.id, p.Head(), p.Td())
		close(done)
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("session did not start")
	}
	if !pm.downloader.Synchronising() {
		t.Fatal("the session is not running")
	}
	// The window lapsed before the session started and the session has
	// imported nothing, so starting it does not close the gate again.
	if !pm.ReadyToProduce() {
		t.Fatal("blocked by a session that has imported nothing since the window elapsed")
	}

	// The session imports a batch: from here it holds production.
	bridge.moved = true
	if _, err := pm.insertFromDownload(nil); err != nil {
		t.Fatal(err)
	}
	if pm.ReadyToProduce() {
		t.Fatal("ready while a download session that is importing runs")
	}
	// Reaching the advertised height does not end the session; the
	// frontier may still move, so the caught-up fast path waits too.
	bridge.height = 12
	if pm.ReadyToProduce() {
		t.Fatal("ready while an importing session runs although the frontier matches the target")
	}
	clock.t = clock.t.Add(producerStallWindow - time.Second)
	if pm.ReadyToProduce() {
		t.Fatal("ready before the window since the session's last import elapsed")
	}
	bridge.height = 10
	if pm.ReadyToProduce() {
		t.Fatal("ready before the window since the session's last import elapsed")
	}
	// The session is still running but has imported nothing for a full
	// window.
	clock.t = clock.t.Add(time.Second)
	if !pm.downloader.Synchronising() {
		t.Fatal("the session ended early")
	}
	if !pm.ReadyToProduce() {
		t.Fatal("blocked by a session that has imported nothing for the window")
	}

	close(release)
	// An empty hash set ends the session quickly.
	_ = pm.downloader.DeliverHashes(p.id, nil, nil)
	select {
	case <-done:
	case <-time.After(15 * time.Second):
		t.Fatal("session did not end")
	}
	// The failed session dropped the peer, which leaves the node below
	// minPeers; the peer coming back is the reconnect case, which does not
	// renew the hold.
	if pm.peers.Len() == 0 {
		addPeer(t, pm, 1, 12)
	}
	if !pm.ReadyToProduce() {
		t.Fatal("still blocked after the session ended without importing")
	}
}

// Sessions that never import do not hold production past the stall window,
// however they are arranged: back to back, each shorter than the window,
// with the gap the syncer leaves before it starts the next, with or
// without blocks parked in the queue behind a head that never arrives.
// This is what one peer claiming the best height can produce at will.
// Nothing in it is an import, so nothing in it restarts the window; an
// import does.
func TestReadyToProduce_SessionsThatImportNothingDoNotHoldPastTheWindow(t *testing.T) {
	clock := useReadinessClock(t)
	pm, bridge := newReadinessManager(t, 1, 10)
	addPeer(t, pm, 1, 1<<24)
	start := clock.t

	// Each session ends inside what would be its own first window, and the
	// next starts on the following forced sync.
	const sessionLength = producerStallWindow - 5*time.Second
	const cycle = sessionLength + forceSyncCycle
	for elapsed := time.Duration(0); elapsed < 10*producerStallWindow; elapsed += time.Second {
		clock.t = start.Add(elapsed)
		phase := elapsed % cycle
		work := &handoffDownloader{session: phase < sessionLength}
		if work.session && phase >= sessionLength/2 {
			// Blocks delivered for every position but the head: the queue
			// counts them and the worker can take none.
			work.stage = stageDelivered
		}
		ready := pm.readyToProduce(work)
		if elapsed < producerStallWindow && ready {
			t.Fatalf("ready %v after start-up, behind the advertised height and inside the window", elapsed)
		}
		if elapsed >= producerStallWindow && !ready {
			t.Fatalf("blocked %v after the last import by download activity that imported nothing (session %v, stage %d)", elapsed, work.session, work.stage)
		}
	}

	// A download import is progress: sessions hold again, for one window
	// from it.
	bridge.moved = true
	if _, err := pm.insertFromDownload(nil); err != nil {
		t.Fatal(err)
	}
	if pm.readyToProduce(&handoffDownloader{session: true}) {
		t.Fatal("ready while a session runs right after a download import")
	}
	if pm.readyToProduce(&handoffDownloader{session: true, stage: stageDelivered}) {
		t.Fatal("ready with blocks in the queue right after a download import")
	}
	clock.t = clock.t.Add(producerStallWindow - time.Second)
	if pm.readyToProduce(&handoffDownloader{session: true}) {
		t.Fatal("ready while a session runs inside the window since the last import")
	}
	clock.t = clock.t.Add(time.Second)
	if !pm.readyToProduce(&handoffDownloader{session: true}) {
		t.Fatal("blocked by a session a full window after the last import")
	}
}

// The same through a real downloader session against a scripted origin:
// every hash request is answered with a hash the node does not have, which
// walks the ancestor search over the whole local chain, and the forward
// request with an empty reply, which ends the session as completed. The
// gate is read while each request is outstanding, so the session is
// running at every read. Inside the window production is held at every
// read; once the window has passed without an import, sessions run back to
// back and hold nothing.
func TestReadyToProduce_AncestorSearchSessionsDoNotHoldPastTheWindow(t *testing.T) {
	clock := useReadinessClock(t)
	pm, _ := newReadinessManager(t, 1, 1<<20)
	p := addPeer(t, pm, 1, 1<<24)

	var (
		mu         sync.Mutex
		rounds     int
		notRunning int
		ready      int
		unknown    uint64
	)
	// Every request, whatever it asks for, is observed and then answered.
	answer := func(from uint64, count int) error {
		running, gate := pm.downloader.Synchronising(), pm.ReadyToProduce()
		mu.Lock()
		rounds++
		if !running {
			notRunning++
		}
		if gate {
			ready++
		}
		unknown++
		var hash types.Hash
		hash[0] = 0xee
		for i := 0; i < 8; i++ {
			hash[types.HashSize-1-i] = byte(unknown >> (8 * i))
		}
		mu.Unlock()
		// The forward fetch starts right above the ancestor the search
		// settled on, which is the bottom of the chain.
		if from == 1 && count > 1 {
			return pm.downloader.DeliverHashes(p.id, nil, nil)
		}
		return pm.downloader.DeliverHashes(p.id, []types.Hash{hash}, nil)
	}
	err := pm.downloader.RegisterPeer(p.id, int(ProtocolVersions[0]), p.Head(),
		func(types.Hash) error { return nil },
		answer,
		func([]types.Hash) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	session := func() (int, int) {
		t.Helper()
		mu.Lock()
		rounds, notRunning, ready = 0, 0, 0
		mu.Unlock()
		done := make(chan struct{})
		go func() {
			pm.downloader.Synchronise(p.id, p.Head(), p.Td())
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(30 * time.Second):
			t.Fatal("session did not end")
		}
		mu.Lock()
		defer mu.Unlock()
		if notRunning != 0 {
			t.Fatalf("%d of %d requests were observed outside a running session", notRunning, rounds)
		}
		return rounds, ready
	}

	// Inside the window since start-up: held at every round.
	rounds, readyRounds := session()
	if rounds < 3 {
		t.Fatalf("the session made %d requests, want the head request, an ancestor search and the forward request", rounds)
	}
	if readyRounds != 0 {
		t.Fatalf("ready in %d of %d rounds of a session inside the window", readyRounds, rounds)
	}
	if pm.peers.Peer(p.id) == nil {
		t.Fatal("the scripted origin was dropped; the back-to-back case needs a session ending that keeps its origin")
	}

	// Past the window: the same sessions, one after another, hold nothing.
	clock.t = clock.t.Add(producerStallWindow)
	for i := 0; i < 3; i++ {
		rounds, readyRounds := session()
		if rounds < 3 {
			t.Fatalf("session %d made %d requests", i, rounds)
		}
		if readyRounds != rounds {
			t.Fatalf("session %d: blocked in %d of %d rounds although nothing was imported for the window", i, rounds-readyRounds, rounds)
		}
		t.Logf("session %d: %d request rounds, ready in all of them", i, rounds)
	}
}
