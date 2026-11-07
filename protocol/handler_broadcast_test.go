package protocol

import (
	"bytes"
	"errors"
	"io"
	"net"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/rlp"

	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/p2p"
	"github.com/zenon-network/go-zenon/p2p/discover"
)

// Tests for issue #262: broadcasts are queued per peer and written by the
// peer's own writer, so a peer whose writes block cannot hold up the
// broadcaster (the pillar worker inside its working lock).

const broadcastTestWait = 5 * time.Second

// broadcastNetPeer is the transport side of a test peer. Disconnect
// records the reason and runs onDisc, which a test uses to end writes
// the way the transport does when it closes the stream.
type broadcastNetPeer struct {
	id     discover.NodeID
	discs  chan p2p.DiscReason
	once   sync.Once
	onDisc func()
}

func newBroadcastNetPeer(n byte, onDisc func()) *broadcastNetPeer {
	return &broadcastNetPeer{id: discover.NodeID{n}, discs: make(chan p2p.DiscReason, 16), onDisc: onDisc}
}

func (f *broadcastNetPeer) ID() discover.NodeID  { return f.id }
func (f *broadcastNetPeer) Name() string         { return "broadcast-test" }
func (f *broadcastNetPeer) Caps() []p2p.Cap      { return nil }
func (f *broadcastNetPeer) RemoteAddr() net.Addr { return &net.TCPAddr{} }
func (f *broadcastNetPeer) Disconnect(r p2p.DiscReason) {
	select {
	case f.discs <- r:
	default:
	}
	if f.onDisc != nil {
		f.once.Do(f.onDisc)
	}
}

type sentMsg struct {
	code    uint64
	payload []byte
}

// recordingRW accepts every write at once and records it.
type recordingRW struct {
	plainRW
	got chan sentMsg
}

func newRecordingRW() *recordingRW { return &recordingRW{got: make(chan sentMsg, 4096)} }

func (rw *recordingRW) WriteMsg(m p2p.Msg) error {
	b, err := io.ReadAll(m.Payload)
	if err != nil {
		return err
	}
	rw.got <- sentMsg{m.Code, b}
	return nil
}

var errStalledClosed = errors.New("stalled stream closed")

// stallingRW models a peer that stops reading: every write blocks until
// the test releases the stream or the transport closes it.
type stallingRW struct {
	plainRW
	entered chan struct{}
	release chan struct{}
	closed  chan struct{}
	relOnce sync.Once
	clsOnce sync.Once
	got     chan sentMsg
}

func newStallingRW() *stallingRW {
	return &stallingRW{
		entered: make(chan struct{}, 4096),
		release: make(chan struct{}),
		closed:  make(chan struct{}),
		got:     make(chan sentMsg, 4096),
	}
}

func (rw *stallingRW) WriteMsg(m p2p.Msg) error {
	rw.entered <- struct{}{}
	select {
	case <-rw.release:
		b, err := io.ReadAll(m.Payload)
		if err != nil {
			return err
		}
		rw.got <- sentMsg{m.Code, b}
		return nil
	case <-rw.closed:
		return errStalledClosed
	}
}

func (rw *stallingRW) unblock() { rw.relOnce.Do(func() { close(rw.release) }) }
func (rw *stallingRW) close()   { rw.clsOnce.Do(func() { close(rw.closed) }) }

// hasBlockBridge reports every block as present, so BroadcastMomentum
// announces.
type hasBlockBridge struct{ stubChainBridge }

func (*hasBlockBridge) HasBlock(types.Hash) bool { return true }

func newBroadcastTestManager() *ProtocolManager {
	return &ProtocolManager{peers: newPeerSet(), chainman: &hasBlockBridge{}}
}

// addBroadcastPeer registers a peer on rw with a queue of queueSize (the
// default when zero) and starts its writer. Cleanup disconnects the peer
// (closing rw) and joins the writer.
func addBroadcastPeer(t *testing.T, pm *ProtocolManager, n byte, rw p2p.MsgReadWriter, closeRW func(), queueSize int) (*peer, *broadcastNetPeer) {
	t.Helper()
	np := newBroadcastNetPeer(n, closeRW)
	p := newPeer(1, 1, np, rw)
	if queueSize > 0 {
		p.broadcasts = newBroadcastQueue(queueSize)
	}
	if err := pm.peers.Register(p); err != nil {
		t.Fatal(err)
	}
	p.startBroadcast()
	t.Cleanup(func() {
		np.Disconnect(p2p.DiscQuitting)
		p.stopBroadcast()
	})
	return p, np
}

func testMomentum(n byte) *nom.DetailedMomentum {
	return &nom.DetailedMomentum{Momentum: &nom.Momentum{Height: uint64(n) + 2, Hash: types.Hash{0xaa, n}}}
}

func testAccountBlock(n byte) *nom.AccountBlock {
	return &nom.AccountBlock{Hash: types.Hash{0xbb, n}}
}

func encodeTest(t *testing.T, v interface{}) []byte {
	t.Helper()
	b, err := rlp.EncodeToBytes(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// returnsPromptly runs f and fails if it has not returned within
// broadcastTestWait.
func returnsPromptly(t *testing.T, what string, f func()) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		f()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(broadcastTestWait):
		t.Fatalf("%s blocked on a stalled peer", what)
	}
}

func expectMsgs(t *testing.T, who string, got <-chan sentMsg, want []sentMsg) {
	t.Helper()
	for i, w := range want {
		select {
		case g := <-got:
			if g.code != w.code || !bytes.Equal(g.payload, w.payload) {
				t.Fatalf("%s: message %d is code %d (%x), want code %d (%x)", who, i, g.code, g.payload, w.code, w.payload)
			}
		case <-time.After(broadcastTestWait):
			t.Fatalf("%s: message %d (code %d) not received", who, i, w.code)
		}
	}
}

func expectNoMsg(t *testing.T, who string, got <-chan sentMsg) {
	t.Helper()
	select {
	case g := <-got:
		t.Fatalf("%s: unexpected message code %d", who, g.code)
	case <-time.After(50 * time.Millisecond):
	}
}

func waitEntered(t *testing.T, rw *stallingRW) {
	t.Helper()
	select {
	case <-rw.entered:
	case <-time.After(broadcastTestWait):
		t.Fatal("writer did not start writing to the stalled peer")
	}
}

// A peer whose writes block does not hold up BroadcastMomentum or
// BroadcastAccountBlock, and the other peers still receive every
// broadcast, in the order it was made.
func TestBroadcastDoesNotWaitOnStalledPeer(t *testing.T) {
	pm := newBroadcastTestManager()
	stalled := newStallingRW()
	sp, _ := addBroadcastPeer(t, pm, 1, stalled, stalled.close, 0)
	healthy := []*recordingRW{newRecordingRW(), newRecordingRW()}
	for i, rw := range healthy {
		addBroadcastPeer(t, pm, byte(10+i), rw, nil, 0)
	}

	m1, m2 := testMomentum(1), testMomentum(2)
	txs := []*nom.AccountBlock{testAccountBlock(1), testAccountBlock(2), testAccountBlock(3)}

	// Three peers: below the propagation cut-off, so every peer gets
	// the full momentum, then (m2) the hash announcement only.
	returnsPromptly(t, "BroadcastMomentum", func() { pm.BroadcastMomentum(m1, true) })
	waitEntered(t, stalled) // the stalled peer's writer is now stuck
	for _, tx := range txs {
		tx := tx
		returnsPromptly(t, "BroadcastAccountBlock", func() { pm.BroadcastAccountBlock(tx) })
	}
	returnsPromptly(t, "BroadcastMomentum", func() { pm.BroadcastMomentum(m2, false) })

	m1.Momentum.EnsureCache()
	want := []sentMsg{{NewBlockMsg, encodeTest(t, m1)}}
	for _, tx := range txs {
		want = append(want, sentMsg{TxMsg, encodeTest(t, []*nom.AccountBlock{tx})})
	}
	want = append(want, sentMsg{NewBlockHashesMsg, encodeTest(t, []types.Hash{m2.Momentum.Hash})})
	for i, rw := range healthy {
		expectMsgs(t, "healthy peer "+string(rune('A'+i)), rw.got, want)
	}

	// What was queued for the stalled peer counts as known to it, and
	// reaches it in the same order once it reads again.
	if !sp.knownBlocks.Contains(m1.Momentum.Hash) || !sp.knownBlocks.Contains(m2.Momentum.Hash) {
		t.Fatal("queued momentum broadcasts are not marked known to the stalled peer")
	}
	for _, tx := range txs {
		if !sp.knownTxs.Contains(tx.Hash) {
			t.Fatal("queued account blocks are not marked known to the stalled peer")
		}
	}
	stalled.unblock()
	expectMsgs(t, "stalled peer", stalled.got, want)
}

// A full-momentum propagation or announcement that finds the queue full
// is dropped: the peer stays connected and the momentum is not marked
// known to it, so a later announcement still reaches it.
func TestBroadcastMomentumQueueFullDrops(t *testing.T) {
	pm := newBroadcastTestManager()
	stalled := newStallingRW()
	const size = 2
	sp, np := addBroadcastPeer(t, pm, 1, stalled, stalled.close, size)
	healthy := newRecordingRW()
	addBroadcastPeer(t, pm, 2, healthy, nil, 0)

	// One broadcast in the stalled write, size more in the queue.
	fill := make([]*nom.DetailedMomentum, size+1)
	for i := range fill {
		fill[i] = testMomentum(byte(i + 1))
		pm.BroadcastMomentum(fill[i], false)
		if i == 0 {
			waitEntered(t, stalled)
		}
	}
	dropped := testMomentum(100)
	returnsPromptly(t, "BroadcastMomentum", func() { pm.BroadcastMomentum(dropped, true) })

	if sp.knownBlocks.Contains(dropped.Momentum.Hash) {
		t.Fatal("a dropped propagation was marked known to the peer")
	}
	select {
	case r := <-np.discs:
		t.Fatalf("peer disconnected (%v) for a dropped momentum broadcast", r)
	default:
	}
	// The healthy peer is unaffected.
	want := make([]sentMsg, 0, len(fill)+1)
	for _, m := range fill {
		want = append(want, sentMsg{NewBlockHashesMsg, encodeTest(t, []types.Hash{m.Momentum.Hash})})
	}
	dropped.Momentum.EnsureCache()
	want = append(want, sentMsg{NewBlockMsg, encodeTest(t, dropped)})
	expectMsgs(t, "healthy peer", healthy.got, want)

	// Once the peer drains its queue, it gets what was queued, without
	// the dropped propagation, and the later announcement of the same
	// momentum reaches it.
	stalled.unblock()
	expectMsgs(t, "stalled peer", stalled.got, want[:len(fill)])
	expectNoMsg(t, "stalled peer", stalled.got)
	pm.BroadcastMomentum(dropped, false)
	expectMsgs(t, "stalled peer", stalled.got, []sentMsg{{NewBlockHashesMsg, encodeTest(t, []types.Hash{dropped.Momentum.Hash})}})
	expectNoMsg(t, "healthy peer", healthy.got) // already known there
}

// An account block that finds the queue full disconnects the peer, since
// nothing offers it the block again while it stays connected. The
// disconnect does not wait for the stalled write, and the broadcast does
// not wait for the disconnect.
func TestBroadcastAccountBlockQueueFullDisconnects(t *testing.T) {
	pm := newBroadcastTestManager()
	stalled := newStallingRW()
	const size = 2
	sp, np := addBroadcastPeer(t, pm, 1, stalled, stalled.close, size)
	healthy := newRecordingRW()
	addBroadcastPeer(t, pm, 2, healthy, nil, 0)

	for i := 0; i < size+1; i++ {
		pm.BroadcastAccountBlock(testAccountBlock(byte(i)))
		if i == 0 {
			waitEntered(t, stalled)
		}
	}
	select {
	case r := <-np.discs:
		t.Fatalf("peer disconnected (%v) before its queue overflowed", r)
	default:
	}
	overflow := testAccountBlock(100)
	returnsPromptly(t, "BroadcastAccountBlock", func() { pm.BroadcastAccountBlock(overflow) })
	select {
	case r := <-np.discs:
		if r != p2p.DiscUselessPeer {
			t.Fatalf("disconnect reason %v, want %v", r, p2p.DiscUselessPeer)
		}
	case <-time.After(broadcastTestWait):
		t.Fatal("peer whose queue overflowed on an account block was not disconnected")
	}
	if sp.knownTxs.Contains(overflow.Hash) {
		t.Fatal("an account block that was not queued was marked known to the peer")
	}
	// A later broadcast does not disconnect the peer again.
	returnsPromptly(t, "BroadcastAccountBlock", func() { pm.BroadcastAccountBlock(testAccountBlock(101)) })
	select {
	case r := <-np.discs:
		t.Fatalf("peer disconnected a second time (%v)", r)
	case <-time.After(50 * time.Millisecond):
	}
	// The healthy peer got every account block, in order.
	want := make([]sentMsg, 0, size+3)
	for i := 0; i < size+1; i++ {
		want = append(want, sentMsg{TxMsg, encodeTest(t, []*nom.AccountBlock{testAccountBlock(byte(i))})})
	}
	want = append(want, sentMsg{TxMsg, encodeTest(t, []*nom.AccountBlock{overflow})})
	want = append(want, sentMsg{TxMsg, encodeTest(t, []*nom.AccountBlock{testAccountBlock(101)})})
	expectMsgs(t, "healthy peer", healthy.got, want)
	// The disconnect ended the stalled write; nothing else reaches the
	// peer, as the writer discards its queue.
	expectNoMsg(t, "stalled peer", stalled.got)
	sp.stopBroadcast()
}

// stopBroadcast returns, joining the writer and an overflow disconnect,
// once the transport ends the stalled write; broadcasts after it are
// refused without blocking and without marking anything known.
func TestStopBroadcastJoinsWriter(t *testing.T) {
	pm := newBroadcastTestManager()
	stalled := newStallingRW()
	sp, np := addBroadcastPeer(t, pm, 1, stalled, stalled.close, 1)
	pm.BroadcastAccountBlock(testAccountBlock(1))
	waitEntered(t, stalled)
	pm.BroadcastAccountBlock(testAccountBlock(2))

	np.Disconnect(p2p.DiscQuitting) // the transport closes the stream
	returnsPromptly(t, "stopBroadcast", sp.stopBroadcast)
	select {
	case <-sp.broadcasts.done:
	default:
		t.Fatal("writer still running after stopBroadcast")
	}
	tx := testAccountBlock(3)
	returnsPromptly(t, "BroadcastAccountBlock", func() { pm.BroadcastAccountBlock(tx) })
	if sp.knownTxs.Contains(tx.Hash) {
		t.Fatal("a broadcast refused after stop was marked known")
	}
	returnsPromptly(t, "stopBroadcast", sp.stopBroadcast) // idempotent
}

// End to end through handle: a connected peer that stops reading does not
// hold up broadcasts, and removing it ends handle and releases the
// writer goroutine.
func TestHandleReleasesBroadcastWriterOnRemoval(t *testing.T) {
	pm := NewProtocolManager(1, 1, &stubChainBridge{})
	defer pm.Stop()
	baseline := runtime.NumGoroutine()

	local, remote := p2p.MsgPipe()
	np := newBroadcastNetPeer(7, func() { _ = local.Close() })
	p := pm.newPeer(int(ProtocolVersions[0]), 1, np, local)

	handled := make(chan error, 1)
	go func() { handled <- pm.handle(p) }()

	// Remote side of the handshake.
	msg, err := remote.ReadMsg()
	if err != nil || msg.Code != StatusMsg {
		t.Fatalf("handshake: %v, code %d", err, msg.Code)
	}
	_ = msg.Discard()
	_, _, genesis := pm.chainman.Status()
	if err := p2p.Send(remote, StatusMsg, &statusData{
		ProtocolVersion: uint32(ProtocolVersions[0]),
		NetworkId:       1,
		GenesisBlock:    genesis,
	}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(broadcastTestWait)
	for pm.peers.Peer(p.id) == nil {
		if time.Now().After(deadline) {
			t.Fatal("peer was not admitted")
		}
		time.Sleep(time.Millisecond)
	}

	// The remote never reads again: a MsgPipe write blocks until it does.
	for i := 0; i < 4; i++ {
		tx := testAccountBlock(byte(i))
		returnsPromptly(t, "BroadcastAccountBlock", func() { pm.BroadcastAccountBlock(tx) })
	}
	m := testMomentum(1)
	returnsPromptly(t, "BroadcastMomentum", func() { pm.BroadcastMomentum(m, true) })

	pm.removePeer(p.id)
	select {
	case <-handled:
	case <-time.After(broadcastTestWait):
		t.Fatal("handle did not return after the peer was removed")
	}
	select {
	case <-p.broadcasts.done:
	default:
		t.Fatal("broadcast writer still running after handle returned")
	}
	deadline = time.Now().Add(broadcastTestWait)
	for runtime.NumGoroutine() > baseline {
		if time.Now().After(deadline) {
			buf := make([]byte, 1<<20)
			t.Fatalf("goroutines grew from %d to %d after removal:\n%s", baseline, runtime.NumGoroutine(), buf[:runtime.Stack(buf, true)])
		}
		time.Sleep(5 * time.Millisecond)
	}
}
