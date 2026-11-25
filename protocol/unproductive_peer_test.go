package protocol

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/p2p"
	"github.com/zenon-network/go-zenon/p2p/discover"
	"github.com/zenon-network/go-zenon/protocol/downloader"
)

// unknownHash is a hash no test chain holds.
var unknownHash = types.Hash{0xde, 0xad}

// sessionPeer registers p with the downloader. Every hash request is
// passed to onRequest, when set, and then answered the way a peer that
// knows nothing beyond the local chain answers: the ancestor lookup with a
// hash the node does not hold, so the common ancestor is genesis, and the
// hash fetch that follows with no hashes. The session then ends without an
// error and without fetching a block, and the peer is not dropped. Block
// requests, which another peer's session may send it, are answered with
// no blocks.
func sessionPeer(t *testing.T, pm *ProtocolManager, p *peer, onRequest func(from uint64, count int)) {
	t.Helper()
	answer := func(from uint64, count int) error {
		if onRequest != nil {
			onRequest(from, count)
		}
		var hashes []types.Hash
		if from == 0 || count == 1 {
			hashes = []types.Hash{unknownHash}
		}
		_ = pm.downloader.DeliverHashes(p.id, hashes, nil)
		return nil
	}
	err := pm.downloader.RegisterPeer(p.id, int(ProtocolVersions[0]), p.Head(),
		func(types.Hash) error { return nil },
		answer,
		func([]types.Hash) error {
			go func() { _ = pm.downloader.DeliverBlocks(p.id, nil, nil) }()
			return nil
		})
	if err != nil {
		t.Fatal(err)
	}
}

// sessionStart reports whether a hash request opens a session: the
// ancestor lookup's first request covers a full window from genesis on a
// chain shorter than the window.
func sessionStart(from uint64, count int) bool {
	return from == 0 && count == downloader.MaxHashFetch
}

// A peer advertising a height far above the network, whose session ends
// without delivering anything, must not keep every download session: once
// its session has imported nothing, the syncer picks another peer, also
// when the first peer stays connected and keeps announcing itself.
//
// The test runs on the real clock and stops the manager before it returns,
// which joins the goroutines the syncer started sessions on.
func TestSyncerPassesOverUnproductiveBestPeer(t *testing.T) {
	pm, _ := newReadinessManager(t, 1, 10)
	liar := addPeer(t, pm, 1, 1_000_000)
	honest := addPeer(t, pm, 2, 12)

	var liarSessions atomic.Int32
	sessionPeer(t, pm, liar, func(from uint64, count int) {
		if sessionStart(from, count) {
			liarSessions.Add(1)
		}
	})
	honestAsked := make(chan struct{})
	var once sync.Once
	sessionPeer(t, pm, honest, func(uint64, int) {
		once.Do(func() { close(honestAsked) })
	})

	pm.Start()
	defer pm.Stop()
	// The liar stays connected and keeps announcing itself, as a peer
	// reconnecting does; each announcement asks the syncer for a session.
	deadline := time.After(5 * time.Second)
	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case pm.newPeerCh <- liar:
		default:
		}
		select {
		case <-honestAsked:
			if liarSessions.Load() == 0 {
				t.Fatal("the honest peer was asked before the best peer had a session")
			}
			return
		case <-deadline:
			t.Fatalf("the syncer never asked the honest peer; the best peer had %d sessions that imported nothing", liarSessions.Load())
		case <-tick.C:
		}
	}
}

// gossipBridge is a bridge whose frontier is parent. inserted is closed
// on the first import.
type gossipBridge struct {
	stubChainBridge
	parent   *nom.Momentum
	inserted chan struct{}
	once     sync.Once
}

func (b *gossipBridge) CurrentBlock() *nom.Momentum { return b.parent }
func (b *gossipBridge) GetBlock(hash types.Hash) *nom.DetailedMomentum {
	if hash == b.parent.Hash {
		return &nom.DetailedMomentum{Momentum: b.parent}
	}
	return nil
}
func (b *gossipBridge) InsertChain([]*nom.DetailedMomentum) (int, error) {
	b.once.Do(func() { close(b.inserted) })
	return 0, nil
}

// A gossiped momentum on top of the frontier is imported while a peer
// advertises a height far above the node's, which keeps the sync state at
// Syncing. Otherwise a peer that claims a height it never delivers would
// cut the node off from the momentums the network gossips.
func TestGossipImportedWhileBestPeerIsAhead(t *testing.T) {
	useReadinessClock(t)
	parent := &nom.Momentum{Height: 10, TimestampUnix: 1_700_000_000}
	parent.Hash = parent.ComputeHash()
	block := &nom.Momentum{Height: 11, PreviousHash: parent.Hash, TimestampUnix: parent.TimestampUnix + 10}
	block.Hash = block.ComputeHash()

	bridge := &gossipBridge{parent: parent, inserted: make(chan struct{})}
	pm := NewProtocolManager(1, 1, bridge)
	t.Cleanup(pm.Stop)
	addPeer(t, pm, 1, 1_000_000)

	app, net := p2p.MsgPipe()
	t.Cleanup(func() { _ = app.Close() })
	var nodeID discover.NodeID
	nodeID[0] = 2
	gossiper := newPeer(1, 1, &readinessPeer{id: nodeID}, net)
	gossiper.SetTd(10)
	if err := pm.peers.Register(gossiper); err != nil {
		t.Fatal(err)
	}
	if state := pm.SyncInfo().State; state != Syncing {
		t.Fatalf("SyncInfo state = %v, want Syncing", state)
	}

	pm.fetcher.Start()
	t.Cleanup(pm.fetcher.Stop)
	sent := make(chan error, 1)
	go func() { sent <- p2p.Send(app, NewBlockMsg, &nom.DetailedMomentum{Momentum: block}) }()
	if err := pm.handleMsg(gossiper); err != nil {
		t.Fatal(err)
	}
	if err := <-sent; err != nil {
		t.Fatal(err)
	}
	select {
	case <-bridge.inserted:
	case <-time.After(5 * time.Second):
		t.Fatal("the gossiped momentum was not imported while a peer advertised a higher height")
	}
	// SyncInfo is telemetry and keeps reporting the advertised target.
	if state := pm.SyncInfo().State; state != Syncing {
		t.Fatalf("SyncInfo state = %v, want Syncing", state)
	}
	// A child of the local head is the fetcher's to import: the handler
	// asks for no session.
	select {
	case <-pm.syncReqCh:
		t.Fatal("a child of the local head asked for a session")
	default:
	}
}

// A session that ends without an import while the node stays below the
// height the peer advertised passes the peer over: it gets no session for
// unproductivePeerBackoff, however the session is asked for, and then
// becomes eligible again.
func TestUnproductiveSessionBacksPeerOff(t *testing.T) {
	clock := useReadinessClock(t)
	pm, _ := newReadinessManager(t, 1, 10)
	liar := addPeer(t, pm, 1, 1_000_000)
	honest := addPeer(t, pm, 2, 12)
	var asked atomic.Int32
	sessionPeer(t, pm, liar, func(from uint64, count int) {
		if sessionStart(from, count) {
			asked.Add(1)
		}
	})

	if got := pm.syncPeer(); got != liar {
		t.Fatalf("sync peer before any session = %v, want the highest peer", got)
	}
	pm.synchronise(liar)
	if asked.Load() != 1 {
		t.Fatalf("sessions = %d, want 1", asked.Load())
	}
	if !pm.unproductive.active(liar.id) {
		t.Fatal("a session that imported nothing did not pass the peer over")
	}
	if pm.peers.Peer(liar.id) == nil {
		t.Fatal("passing a peer over must not disconnect it")
	}
	if got := pm.syncPeer(); got != honest {
		t.Fatalf("sync peer after the unproductive session = %v, want the next highest peer", got)
	}
	// Asked for directly, as gossip asks, the peer still gets no session.
	pm.synchronise(liar)
	if asked.Load() != 1 {
		t.Fatalf("a passed-over peer got another session (%d sessions)", asked.Load())
	}

	clock.t = clock.t.Add(unproductivePeerBackoff)
	if pm.unproductive.active(liar.id) {
		t.Fatal("the backoff did not run out")
	}
	// Until a session imports, the peer that has waited longest goes
	// first: the honest peer has had no session, the liar has had one.
	if got := pm.syncPeer(); got != honest {
		t.Fatalf("sync peer after the backoff = %v, want the peer that has waited longest", got)
	}
}

// With every peer passed over there is no sync peer, so the backoff holds
// against a node whose only peers keep failing.
func TestSyncPeerNilWhenAllPassedOver(t *testing.T) {
	useReadinessClock(t)
	pm, _ := newReadinessManager(t, 1, 10)
	p := addPeer(t, pm, 1, 1_000_000)
	pm.unproductive.add(p.id)
	if got := pm.syncPeer(); got != nil {
		t.Fatalf("sync peer = %v, want none", got)
	}
}

// A session that imported a batch is productive, whatever height the node
// ends at.
func TestProductiveSessionKeepsPeer(t *testing.T) {
	useReadinessClock(t)
	pm, bridge := newReadinessManager(t, 1, 10)
	p := addPeer(t, pm, 1, 1_000_000)
	bridge.moved = true
	var once sync.Once
	sessionPeer(t, pm, p, func(uint64, int) {
		once.Do(func() {
			if _, err := pm.insertFromDownload(nil); err != nil {
				t.Error(err)
			}
		})
	})
	pm.synchronise(p)
	if pm.unproductive.active(p.id) {
		t.Fatal("a session that imported a batch passed its peer over")
	}
}

// A session after which the node stands at the advertised height is not
// held against the peer even though it imported nothing itself: a gossiped
// momentum can reach the chain first while a session fetches it.
func TestSessionOvertakenByGossipKeepsPeer(t *testing.T) {
	useReadinessClock(t)
	pm, bridge := newReadinessManager(t, 1, 10)
	p := addPeer(t, pm, 1, 15)
	sessionPeer(t, pm, p, func(uint64, int) { bridge.height = 15 })
	pm.synchronise(p)
	if pm.unproductive.active(p.id) {
		t.Fatal("a session overtaken by another import path passed its peer over")
	}
	if pm.preferWaiting.Load() {
		t.Fatal("a session overtaken by another import path changed peer selection")
	}
}

// A relay that passed on the next momentum before importing it can serve
// nothing above the node yet; an empty session against it is not judged.
func TestSessionOneAheadNotJudged(t *testing.T) {
	useReadinessClock(t)
	pm, _ := newReadinessManager(t, 1, 10)
	p := addPeer(t, pm, 1, 11)
	sessionPeer(t, pm, p, nil)
	pm.synchronise(p)
	if pm.unproductive.active(p.id) || pm.preferWaiting.Load() {
		t.Fatal("an empty session against a peer one momentum ahead was held against it")
	}
}

// When the downloader removes another peer during a session, that peer
// was at fault: the session's peer is not passed over, but peers are
// chosen by how long they have waited until a session imports, so the
// session's peer cannot keep every session with a helper identity.
func TestSessionWithOtherPeerAtFaultKeepsOrigin(t *testing.T) {
	useReadinessClock(t)
	pm, _ := newReadinessManager(t, 1, 10)
	origin := addPeer(t, pm, 1, 1_000_000)
	supplier := addPeer(t, pm, 2, 1_000_000)
	var once sync.Once
	sessionPeer(t, pm, origin, func(uint64, int) {
		once.Do(func() { pm.dropFromDownload(supplier.id) })
	})
	pm.synchronise(origin)
	if pm.unproductive.active(origin.id) {
		t.Fatal("the session's peer was passed over for another peer's fault")
	}
	if !pm.preferWaiting.Load() {
		t.Fatal("a session that imported nothing left selection by advertised height")
	}
	if pm.peers.Peer(supplier.id) != nil {
		t.Fatal("the peer at fault was not removed")
	}
}

// A sync that did not start because another session was running asked
// nothing of the peer and is not held against it.
func TestBusyDownloaderKeepsPeer(t *testing.T) {
	useReadinessClock(t)
	pm, _ := newReadinessManager(t, 1, 10)
	running := addPeer(t, pm, 1, 12)
	waiting := addPeer(t, pm, 2, 1_000_000)
	entered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	sessionPeer(t, pm, running, func(uint64, int) {
		once.Do(func() {
			close(entered)
			<-release
		})
	})
	sessionPeer(t, pm, waiting, nil)

	// The running session is started on the downloader directly: only
	// the sync worker calls synchronise, one session at a time.
	done := make(chan struct{})
	go func() {
		pm.downloader.Synchronise(running.id, running.Head(), running.Td())
		close(done)
	}()
	var releaseOnce sync.Once
	t.Cleanup(func() {
		releaseOnce.Do(func() { close(release) })
		<-done
	})
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("session did not start")
	}
	pm.synchronise(waiting)
	if pm.unproductive.active(waiting.id) {
		t.Fatal("a sync refused as busy passed the peer over")
	}
	releaseOnce.Do(func() { close(release) })
	<-done
}

// growingBridge is a stub bridge whose frontier height moves with every
// download batch that reaches above it.
type growingBridge struct {
	stubChainBridge
	height atomic.Uint64
}

func (b *growingBridge) CurrentBlock() *nom.Momentum { return &nom.Momentum{Height: b.height.Load()} }
func (b *growingBridge) InsertChainMoved(momentums []*nom.DetailedMomentum) (int, bool, error) {
	last := momentums[len(momentums)-1].Momentum.Height
	for {
		h := b.height.Load()
		if last <= h {
			return 0, false, nil
		}
		if b.height.CompareAndSwap(h, last) {
			return 0, true, nil
		}
	}
}

// servingPeer registers p with the downloader as a peer holding chain,
// momentums 1 to len(chain): the ancestor lookup settles on genesis, and
// the hashes and momentums above it are served as asked.
func servingPeer(t *testing.T, pm *ProtocolManager, p *peer, chain []*nom.DetailedMomentum) {
	t.Helper()
	byHash := make(map[types.Hash]*nom.DetailedMomentum)
	for _, d := range chain {
		byHash[d.Momentum.Hash] = d
	}
	err := pm.downloader.RegisterPeer(p.id, int(ProtocolVersions[0]), p.Head(),
		func(types.Hash) error { return nil },
		func(from uint64, count int) error {
			var reply []types.Hash
			switch {
			case from == 0 || count == 1:
				reply = []types.Hash{unknownHash}
			case from >= 1 && from <= uint64(len(chain)):
				for _, d := range chain[from-1:] {
					reply = append(reply, d.Momentum.Hash)
				}
			}
			go func() { _ = pm.downloader.DeliverHashes(p.id, reply, nil) }()
			return nil
		},
		func(requested []types.Hash) error {
			var blocks []*nom.DetailedMomentum
			for _, h := range requested {
				if d := byHash[h]; d != nil {
					blocks = append(blocks, d)
				}
			}
			go func() { _ = pm.downloader.DeliverBlocks(p.id, blocks, nil) }()
			return nil
		})
	if err != nil {
		t.Fatal(err)
	}
}

// testChain builds momentums 1 to n, each on the one before.
func testChain(n uint64) []*nom.DetailedMomentum {
	var chain []*nom.DetailedMomentum
	previous := types.Hash{}
	for height := uint64(1); height <= n; height++ {
		m := &nom.Momentum{Height: height, PreviousHash: previous, TimestampUnix: 1_700_000_000 + height}
		m.Hash = m.ComputeHash()
		previous = m.Hash
		chain = append(chain, &nom.DetailedMomentum{Momentum: m})
	}
	return chain
}

// Identity churn: every few moments a new connection claims a height far
// above the network's and its session delivers nothing. Each new identity
// is eligible, so passing over the ones that failed is not enough; after a
// session that imported nothing, the turn goes to the peer ahead of the
// node that has waited longest, which serves what the node is missing.
func TestSyncerRecoversGapUnderIdentityChurn(t *testing.T) {
	bridge := &growingBridge{}
	bridge.height.Store(10)
	pm := NewProtocolManager(1, 1, bridge)
	t.Cleanup(pm.Stop)
	honest := addPeer(t, pm, 1, 12)
	servingPeer(t, pm, honest, testChain(12))

	pm.Start()
	defer pm.Stop()
	deadline := time.After(5 * time.Second)
	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()
	// Two identities a round, one announced: an identity that has had no
	// session yet is always connected, as with an attacker that keeps one
	// in reserve.
	for id := byte(2); ; id += 2 {
		if id < 2 {
			t.Fatal("ran out of identities")
		}
		liar := addPeer(t, pm, id, 1_000_000)
		sessionPeer(t, pm, liar, nil)
		sessionPeer(t, pm, addPeer(t, pm, id+1, 1_000_000), nil)
		select {
		case pm.newPeerCh <- liar:
		default:
		}
		if bridge.height.Load() >= 12 {
			return
		}
		select {
		case <-deadline:
			t.Fatalf("the node stayed at height %d; %d identities claimed a higher height", bridge.height.Load(), id)
		case <-tick.C:
		}
	}
}

// drainBridge holds the first download import until released and reports
// that it did not move the chain: the session served momentums the node
// already had.
type drainBridge struct {
	readinessBridge
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (b *drainBridge) InsertChainMoved([]*nom.DetailedMomentum) (int, bool, error) {
	b.once.Do(func() { close(b.entered) })
	<-b.release
	return 0, false, nil
}

// A session's verdict waits for the blocks it fetched: while the import
// worker holds them the session is not judged, and once they have been
// imported without moving the chain it is unproductive.
func TestJudgementWaitsForPendingImports(t *testing.T) {
	useReadinessClock(t)
	bridge := &drainBridge{readinessBridge: readinessBridge{height: 10}, entered: make(chan struct{}), release: make(chan struct{})}
	pm := NewProtocolManager(1, 1, bridge)
	t.Cleanup(pm.Stop)
	p := addPeer(t, pm, 1, 1_000_000)

	// The session fetches momentums 1 to 3 after the ancestor lookup
	// settles on genesis.
	servingPeer(t, pm, p, testChain(3))

	done := make(chan struct{})
	go func() {
		pm.synchronise(p)
		close(done)
	}()
	var releaseOnce sync.Once
	t.Cleanup(func() {
		releaseOnce.Do(func() { close(bridge.release) })
		<-done
	})
	select {
	case <-bridge.entered:
	case <-time.After(10 * time.Second):
		t.Fatal("the session did not reach the import")
	}
	// While the worker holds the session's blocks the session is not
	// judged.
	time.Sleep(100 * time.Millisecond)
	select {
	case <-done:
		t.Fatal("the session returned while the import worker held its blocks")
	default:
	}
	if pm.unproductive.active(p.id) {
		t.Fatal("the session was judged while the import worker held its blocks")
	}
	releaseOnce.Do(func() { close(bridge.release) })
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("the session did not finish after the import")
	}
	if !pm.unproductive.active(p.id) {
		t.Fatal("a session whose import moved nothing did not pass the peer over")
	}
}

// Backoffs that run out before an honest peer's turn must not starve it:
// after a session that imported nothing, the turn goes to the peer that
// has waited longest, so peers that were served go behind it whether or
// not they are eligible again.
func TestWaitingPeerServedBeforeReturningPeers(t *testing.T) {
	clock := useReadinessClock(t)
	pm, _ := newReadinessManager(t, 1, 10)
	// Three connections claiming a high height were there first.
	for id := byte(1); id <= 3; id++ {
		sessionPeer(t, pm, addPeer(t, pm, id, 1_000_000), nil)
	}
	honest := addPeer(t, pm, 4, 12)
	var honestSessions atomic.Int32
	sessionPeer(t, pm, honest, func(from uint64, count int) {
		if sessionStart(from, count) {
			honestSessions.Add(1)
		}
	})
	for i := 0; i < 5 && honestSessions.Load() == 0; i++ {
		pm.synchronise(pm.syncPeer())
		// Every backoff runs out before the next selection.
		clock.t = clock.t.Add(unproductivePeerBackoff)
	}
	if honestSessions.Load() == 0 {
		t.Fatal("the honest peer got no session while peers connected before it were served again")
	}
}

// A gossiped momentum beyond the next height shows a gap: the peer becomes
// a sync target and the syncer is asked for a session, which it runs on a
// goroutine Stop joins. The handler starts none itself.
func TestGapGossipRequestsSync(t *testing.T) {
	useReadinessClock(t)
	parent := &nom.Momentum{Height: 10, TimestampUnix: 1_700_000_000}
	parent.Hash = parent.ComputeHash()
	bridge := &gossipBridge{parent: parent, inserted: make(chan struct{})}
	pm := NewProtocolManager(1, 1, bridge)
	t.Cleanup(pm.Stop)

	app, net := p2p.MsgPipe()
	t.Cleanup(func() { _ = app.Close() })
	var nodeID discover.NodeID
	nodeID[0] = 1
	gossiper := newPeer(1, 1, &readinessPeer{id: nodeID}, net)
	gossiper.SetTd(10)
	if err := pm.peers.Register(gossiper); err != nil {
		t.Fatal(err)
	}
	var asked atomic.Int32
	sessionPeer(t, pm, gossiper, func(uint64, int) { asked.Add(1) })
	// The node counts as in sync until the momentum arrives, so the
	// handler hands it to the fetcher as well.
	pm.fetcher.Start()
	t.Cleanup(pm.fetcher.Stop)

	block := &nom.Momentum{Height: 13, PreviousHash: types.Hash{1}, TimestampUnix: parent.TimestampUnix + 30}
	block.Hash = block.ComputeHash()
	sent := make(chan error, 1)
	go func() { sent <- p2p.Send(app, NewBlockMsg, &nom.DetailedMomentum{Momentum: block}) }()
	if err := pm.handleMsg(gossiper); err != nil {
		t.Fatal(err)
	}
	if err := <-sent; err != nil {
		t.Fatal(err)
	}
	if gossiper.Td() != 13 {
		t.Fatalf("gossiper Td = %d, want 13", gossiper.Td())
	}
	select {
	case <-pm.syncReqCh:
	default:
		t.Fatal("a gap did not ask the syncer for a session")
	}
	time.Sleep(50 * time.Millisecond)
	if asked.Load() != 0 {
		t.Fatal("the handler started a session itself")
	}
}

// The peer minimum holds for every session, also one a gossiped momentum
// asks for.
func TestSynchroniseRespectsMinPeers(t *testing.T) {
	useReadinessClock(t)
	pm, _ := newReadinessManager(t, 2, 10)
	p := addPeer(t, pm, 1, 1_000_000)
	var asked atomic.Int32
	sessionPeer(t, pm, p, func(uint64, int) { asked.Add(1) })
	pm.synchronise(p)
	if asked.Load() != 0 {
		t.Fatal("a session started with fewer peers than the minimum")
	}
}

// A momentum at the next height on another parent is a branch the fetcher
// cannot import: the peer becomes a sync target and the sync worker is
// asked for a session.
func TestForkAtNextHeightRequestsSync(t *testing.T) {
	useReadinessClock(t)
	parent := &nom.Momentum{Height: 10, TimestampUnix: 1_700_000_000}
	parent.Hash = parent.ComputeHash()
	bridge := &gossipBridge{parent: parent, inserted: make(chan struct{})}
	pm := NewProtocolManager(1, 1, bridge)
	t.Cleanup(pm.Stop)
	pm.fetcher.Start()
	t.Cleanup(pm.fetcher.Stop)

	app, net := p2p.MsgPipe()
	t.Cleanup(func() { _ = app.Close() })
	var nodeID discover.NodeID
	nodeID[0] = 1
	relay := newPeer(1, 1, &readinessPeer{id: nodeID}, net)
	relay.SetTd(10)
	if err := pm.peers.Register(relay); err != nil {
		t.Fatal(err)
	}
	block := &nom.Momentum{Height: 11, PreviousHash: types.Hash{7}, TimestampUnix: parent.TimestampUnix + 10}
	block.Hash = block.ComputeHash()
	sent := make(chan error, 1)
	go func() { sent <- p2p.Send(app, NewBlockMsg, &nom.DetailedMomentum{Momentum: block}) }()
	if err := pm.handleMsg(relay); err != nil {
		t.Fatal(err)
	}
	if err := <-sent; err != nil {
		t.Fatal(err)
	}
	if relay.Td() != 11 {
		t.Fatalf("relay Td = %d, want 11", relay.Td())
	}
	select {
	case <-pm.syncReqCh:
	default:
		t.Fatal("a branch at the next height did not ask for a session")
	}
}

// Who a session that imported nothing is held against: its peer is
// passed over when its own hash list ran out or the downloader removed it,
// and not when the session ended without blaming it, such as a stall on
// blocks requested from other peers. Peers are chosen by how long they
// have waited after any of them.
func TestUnproductiveSessionBlame(t *testing.T) {
	stall := errors.New("block download stalled")
	cases := []struct {
		name          string
		ended         error
		dropOrigin    bool
		dropOther     bool
		wantPassedOff bool
	}{
		{"hash list ran out", nil, false, false, true},
		{"origin removed", stall, true, false, true},
		{"ended without blame", stall, false, false, false},
		{"other peer removed", stall, false, true, false},
		{"origin and other removed", stall, true, true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			useReadinessClock(t)
			pm, _ := newReadinessManager(t, 1, 10)
			origin := addPeer(t, pm, 1, 1_000_000)
			other := addPeer(t, pm, 2, 12)
			pm.session.begin(origin.id)
			if tc.dropOrigin {
				pm.dropFromDownload(origin.id)
			}
			if tc.dropOther {
				pm.dropFromDownload(other.id)
			}
			pm.judgeSession(origin.id, 1_000_000, pm.progress.importCount(), tc.ended)
			pm.session.end()
			if got := pm.unproductive.active(origin.id); got != tc.wantPassedOff {
				t.Fatalf("passed over = %v, want %v", got, tc.wantPassedOff)
			}
			if !pm.preferWaiting.Load() {
				t.Fatal("an unproductive session left selection by advertised height")
			}
		})
	}
}

// Among peers advertising the same height the one that has waited longest
// for a session is chosen, so a peer serving a branch one momentum above
// the node gets its turn alongside one that cannot.
func TestEqualHeightTieGoesToWaitingPeer(t *testing.T) {
	useReadinessClock(t)
	pm, _ := newReadinessManager(t, 1, 10)
	first := addPeer(t, pm, 1, 11)
	second := addPeer(t, pm, 2, 11)
	if got := pm.syncPeer(); got != first {
		t.Fatalf("sync peer = %v, want the peer registered first", got)
	}
	pm.peers.markTurn(first)
	if got := pm.syncPeer(); got != second {
		t.Fatalf("sync peer after the first was served = %v, want the other", got)
	}
}

// countingBridge counts imports of momentums whose parent is the frontier.
type countingBridge struct {
	gossipBridge
	imports atomic.Int32
}

func (b *countingBridge) InsertChain([]*nom.DetailedMomentum) (int, error) {
	b.imports.Add(1)
	return 0, nil
}

// While the node is not in sync, a peer gets at most one child of the
// local head to the fetcher while the head stays where it is; another peer
// still gets its own through.
func TestChildAdmittedOncePerPeerWhileSyncing(t *testing.T) {
	useReadinessClock(t)
	parent := &nom.Momentum{Height: 10, TimestampUnix: 1_700_000_000}
	parent.Hash = parent.ComputeHash()
	bridge := &countingBridge{gossipBridge: gossipBridge{parent: parent}}
	pm := NewProtocolManager(1, 1, bridge)
	t.Cleanup(pm.Stop)
	addPeer(t, pm, 9, 1_000_000)
	pm.fetcher.Start()
	t.Cleanup(pm.fetcher.Stop)

	gossip := func(from *peer, app *p2p.MsgPipeRW, stamp uint64) {
		t.Helper()
		block := &nom.Momentum{Height: 11, PreviousHash: parent.Hash, TimestampUnix: stamp}
		block.Hash = block.ComputeHash()
		sent := make(chan error, 1)
		go func() { sent <- p2p.Send(app, NewBlockMsg, &nom.DetailedMomentum{Momentum: block}) }()
		if err := pm.handleMsg(from); err != nil {
			t.Fatal(err)
		}
		if err := <-sent; err != nil {
			t.Fatal(err)
		}
	}
	connect := func(id byte) (*peer, *p2p.MsgPipeRW) {
		app, net := p2p.MsgPipe()
		t.Cleanup(func() { _ = app.Close() })
		var nodeID discover.NodeID
		nodeID[0] = id
		p := newPeer(1, 1, &readinessPeer{id: nodeID}, net)
		p.SetTd(10)
		if err := pm.peers.Register(p); err != nil {
			t.Fatal(err)
		}
		return p, app
	}
	waitImports := func(want int32) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for bridge.imports.Load() < want && time.Now().Before(deadline) {
			time.Sleep(5 * time.Millisecond)
		}
		time.Sleep(100 * time.Millisecond)
		if got := bridge.imports.Load(); got != want {
			t.Fatalf("imports = %d, want %d", got, want)
		}
	}

	a, appA := connect(1)
	gossip(a, appA, 1_700_000_010)
	waitImports(1)
	gossip(a, appA, 1_700_000_011)
	waitImports(1)
	b, appB := connect(2)
	gossip(b, appB, 1_700_000_012)
	waitImports(2)
}

// failingBridge holds every download import until released, then fails it
// with a verdict the downloader holds against nobody.
type failingBridge struct {
	readinessBridge
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (b *failingBridge) InsertChainMoved([]*nom.DetailedMomentum) (int, bool, error) {
	b.once.Do(func() { close(b.entered) })
	<-b.release
	return 0, false, fmt.Errorf("refused: %w", downloader.ErrBatchInconclusive)
}

// A peer that served its whole hash list and every block asked of it is
// not passed over when the import then fails for a reason the downloader
// holds against nobody, such as a batch that no longer fits a chain
// another path moved; peers are still chosen by how long they have waited.
func TestImportFailureAfterCompleteSessionKeepsOrigin(t *testing.T) {
	useReadinessClock(t)
	bridge := &failingBridge{readinessBridge: readinessBridge{height: 10}, entered: make(chan struct{}), release: make(chan struct{})}
	pm := NewProtocolManager(1, 1, bridge)
	t.Cleanup(pm.Stop)
	p := addPeer(t, pm, 1, 1_000_000)
	servingPeer(t, pm, p, testChain(3))

	done := make(chan struct{})
	go func() {
		pm.synchronise(p)
		close(done)
	}()
	var releaseOnce sync.Once
	t.Cleanup(func() {
		releaseOnce.Do(func() { close(bridge.release) })
		<-done
	})
	// Retrieval completes while the import is held, so the failure comes
	// after a session that ended without an error.
	select {
	case <-bridge.entered:
	case <-time.After(10 * time.Second):
		t.Fatal("the session did not reach the import")
	}
	deadline := time.Now().Add(10 * time.Second)
	for pm.downloader.Synchronising() {
		if time.Now().After(deadline) {
			t.Fatal("retrieval did not complete")
		}
		time.Sleep(5 * time.Millisecond)
	}
	releaseOnce.Do(func() { close(bridge.release) })
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("the session was not judged after the import")
	}
	if pm.unproductive.active(p.id) {
		t.Fatal("the peer was passed over for an import failure held against nobody")
	}
	if !pm.preferWaiting.Load() {
		t.Fatal("a session that imported nothing left selection by advertised height")
	}
	if pm.peers.Peer(p.id) == nil {
		t.Fatal("the peer was removed")
	}
}

// The limit on children while the node is not in sync is per node and per
// parent, not tied to advertised heights: a relay whose advertised height
// is already above the child's gets its first child through, and the same
// node reconnecting does not get a second one of the same parent.
func TestChildAdmissionByNodeAndParent(t *testing.T) {
	useReadinessClock(t)
	parent := &nom.Momentum{Height: 10, TimestampUnix: 1_700_000_000}
	parent.Hash = parent.ComputeHash()
	bridge := &countingBridge{gossipBridge: gossipBridge{parent: parent}}
	pm := NewProtocolManager(1, 1, bridge)
	t.Cleanup(pm.Stop)
	addPeer(t, pm, 9, 1_000_000)
	pm.fetcher.Start()
	t.Cleanup(pm.fetcher.Stop)

	connect := func(id byte, td uint64) (*peer, *p2p.MsgPipeRW) {
		app, net := p2p.MsgPipe()
		t.Cleanup(func() { _ = app.Close() })
		var nodeID discover.NodeID
		nodeID[0] = id
		p := newPeer(1, 1, &readinessPeer{id: nodeID}, net)
		p.SetTd(td)
		if err := pm.peers.Register(p); err != nil {
			t.Fatal(err)
		}
		return p, app
	}
	gossip := func(from *peer, app *p2p.MsgPipeRW, stamp uint64) {
		t.Helper()
		block := &nom.Momentum{Height: 11, PreviousHash: parent.Hash, TimestampUnix: stamp}
		block.Hash = block.ComputeHash()
		sent := make(chan error, 1)
		go func() { sent <- p2p.Send(app, NewBlockMsg, &nom.DetailedMomentum{Momentum: block}) }()
		if err := pm.handleMsg(from); err != nil {
			t.Fatal(err)
		}
		if err := <-sent; err != nil {
			t.Fatal(err)
		}
	}
	waitImports := func(want int32) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for bridge.imports.Load() < want && time.Now().Before(deadline) {
			time.Sleep(5 * time.Millisecond)
		}
		time.Sleep(100 * time.Millisecond)
		if got := bridge.imports.Load(); got != want {
			t.Fatalf("imports = %d, want %d", got, want)
		}
	}

	// Advertised 12 at the handshake: above the child's height.
	relay, app := connect(1, 12)
	gossip(relay, app, 1_700_000_010)
	waitImports(1)

	// The same node on a new connection.
	if err := pm.peers.Unregister(relay.id); err != nil {
		t.Fatal(err)
	}
	again, app2 := connect(1, 10)
	gossip(again, app2, 1_700_000_011)
	waitImports(1)
}

// A momentum showing a gap asks for a session also when its sender had
// advertised that much already.
func TestGapFromPeerAlreadyAheadRequestsSync(t *testing.T) {
	useReadinessClock(t)
	parent := &nom.Momentum{Height: 10, TimestampUnix: 1_700_000_000}
	parent.Hash = parent.ComputeHash()
	pm := NewProtocolManager(1, 1, &gossipBridge{parent: parent, inserted: make(chan struct{})})
	t.Cleanup(pm.Stop)
	pm.fetcher.Start()
	t.Cleanup(pm.fetcher.Stop)

	app, net := p2p.MsgPipe()
	t.Cleanup(func() { _ = app.Close() })
	var nodeID discover.NodeID
	nodeID[0] = 1
	relay := newPeer(1, 1, &readinessPeer{id: nodeID}, net)
	relay.SetTd(15)
	if err := pm.peers.Register(relay); err != nil {
		t.Fatal(err)
	}
	block := &nom.Momentum{Height: 13, PreviousHash: types.Hash{1}, TimestampUnix: parent.TimestampUnix + 30}
	block.Hash = block.ComputeHash()
	sent := make(chan error, 1)
	go func() { sent <- p2p.Send(app, NewBlockMsg, &nom.DetailedMomentum{Momentum: block}) }()
	if err := pm.handleMsg(relay); err != nil {
		t.Fatal(err)
	}
	if err := <-sent; err != nil {
		t.Fatal(err)
	}
	if relay.Td() != 15 {
		t.Fatalf("relay Td = %d, want 15 kept", relay.Td())
	}
	select {
	case <-pm.syncReqCh:
	default:
		t.Fatal("a gap from a peer already advertising more did not ask for a session")
	}
}

// A turn is stamped on the connection that was served only: a later
// connection of the same node keeps its place.
func TestMarkTurnStampsServedConnectionOnly(t *testing.T) {
	ps := newPeerSet()
	served := newStubPeer(1)
	if err := ps.Register(served); err != nil {
		t.Fatal(err)
	}
	if err := ps.Unregister(served.id); err != nil {
		t.Fatal(err)
	}
	again := newStubPeer(1)
	if err := ps.Register(again); err != nil {
		t.Fatal(err)
	}
	other := newStubPeer(2)
	if err := ps.Register(other); err != nil {
		t.Fatal(err)
	}
	ps.markTurn(served)
	if got := ps.leastRecentlyServedWhere(nil); got != again {
		t.Fatalf("least recently served = %v, want the node's new connection", got)
	}
}

// The child-admission table is reset only by a head above the last one
// seen, declines a child of a stale head, and stops admitting at its cap
// without evicting anyone.
func TestChildAdmissionStaleHeadAndCap(t *testing.T) {
	var c childAdmission
	h10, h11 := types.Hash{10}, types.Hash{11}
	if !c.admit("a", 11, h11) {
		t.Fatal("first child of the head declined")
	}
	if c.admit("b", 10, h10) {
		t.Fatal("a child of a stale head admitted")
	}
	if c.admit("a", 11, h11) {
		t.Fatal("a stale reading reset the table: the node got a second child")
	}
	for i := 0; i < childAdmissionCap; i++ {
		c.admit(fmt.Sprintf("n%d", i), 11, h11)
	}
	if c.admit("late", 11, h11) {
		t.Fatal("admitted past the cap")
	}
	if c.admit("a", 11, h11) {
		t.Fatal("a node already admitted got in again at the cap")
	}
	if !c.admit("late", 12, types.Hash{12}) {
		t.Fatal("the first child of a new head was declined")
	}
}
