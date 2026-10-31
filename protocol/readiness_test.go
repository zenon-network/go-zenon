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
	useReadinessClock(t)
	pm, _ := newReadinessManager(t, 2, 10)
	addPeer(t, pm, 1, 10)
	if pm.ReadyToProduce() {
		t.Fatal("ready with fewer peers than the minimum")
	}
	if pm.SyncInfo().State != NotEnoughPeers {
		t.Fatalf("SyncInfo state = %v, want NotEnoughPeers", pm.SyncInfo().State)
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

// A running download session blocks production however long the window has
// been exceeded.
func TestReadyToProduce_RunningSessionBlocks(t *testing.T) {
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
	if pm.ReadyToProduce() {
		t.Fatal("ready while a download session is running")
	}
	// Reaching the advertised height does not end the session; the
	// frontier may still move, so the caught-up fast path waits too.
	bridge.height = 12
	if pm.ReadyToProduce() {
		t.Fatal("ready while a session runs although the frontier matches the target")
	}
	bridge.height = 10
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
