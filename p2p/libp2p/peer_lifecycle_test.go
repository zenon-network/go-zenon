package libp2p

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/zenon-network/go-zenon/p2p"
)

// An adopted peer keeps its slot until its protocol handlers have
// returned. The handler here sees the peer close and then waits on a
// gate; while it waits the peer must still count toward MaxPeers, and the
// slot is freed once the handler returns.
func TestPeerSlotHeldUntilProtocolHandlersReturn(t *testing.T) {
	srv, h, hs := newCapacityTestServer(t)
	gate := make(chan struct{})
	atGate := make(chan struct{})
	var returned int32
	srv.Protocols = []p2p.Protocol{{
		Name: "zen", Version: 1, Length: 1,
		Run: func(_ p2p.Peer, rw p2p.MsgReadWriter) error {
			_, err := rw.ReadMsg() // io.EOF once the peer closes
			close(atGate)
			<-gate
			atomic.StoreInt32(&returned, 1)
			return err
		},
	}}
	srv.delpeer = make(chan *Peer, 16)
	go srv.peerCleanupLoop() // ends with srv.ctx in the harness cleanup
	t.Cleanup(func() {
		select {
		case <-gate:
		default:
			close(gate)
		}
	})

	hs.Caps = []p2p.Cap{{Name: "zen", Version: 1}}
	s := newCapacityTestStream(t, h.net.conn, hs)
	s.readGate = make(chan struct{}) // the peer's reads wait for the reset
	srv.handleStream(s)
	if srv.PeerCount() != 1 {
		t.Fatalf("PeerCount = %d after adoption, want 1", srv.PeerCount())
	}

	s.Reset() // the read loop fails and the peer shuts down
	select {
	case <-atGate:
	case <-time.After(pongTestTimeout):
		t.Fatal("protocol handler never observed the close")
	}
	time.Sleep(200 * time.Millisecond) // time for an early release to land
	if n := srv.PeerCount(); n != 1 {
		t.Fatalf("PeerCount = %d while the protocol handler is still inside Run, want 1", n)
	}

	close(gate)
	deadline := time.Now().Add(pongTestTimeout)
	for srv.PeerCount() != 0 {
		if time.Now().After(deadline) {
			t.Fatal("peer slot not released after the protocol handler returned")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if atomic.LoadInt32(&returned) != 1 {
		t.Fatal("slot released before the protocol handler returned")
	}
}

// The disconnect frame is written under discWriteTimeout, not under
// WriteMsg's frameWriteTimeout: against a stream that accepts no writes,
// a locally requested disconnect ends the peer within about a second.
func TestDisconnectWriteHonoursDiscWriteTimeout(t *testing.T) {
	s := newHeldStream()
	p, done := runPongTestPeer(t, s)

	start := time.Now()
	go p.Disconnect(p2p.DiscQuitting)
	expectEntered(t, s.entered, discMsg)
	expectPeerReasonWithin(t, done, p2p.DiscQuitting, start, discWriteTimeout+time.Second)
}

// Against a stream that takes the write, the disconnect frame is written
// and the peer ends without waiting for the disconnect deadline.
func TestDisconnectWritesDiscFrameToHealthyStream(t *testing.T) {
	s := newHeldStream()
	p, done := runPongTestPeer(t, s)

	start := time.Now()
	go p.Disconnect(p2p.DiscQuitting)
	expectEntered(t, s.entered, discMsg)
	s.release(t)
	expectPeerReasonWithin(t, done, p2p.DiscQuitting, start, discWriteTimeout/2)
}

// A write already stalled on the stream when the peer is dropped holds
// the write lock the disconnect frame needs. The disconnect deadline cuts
// that write short too, so the stalled write and the disconnect frame
// together still end within about discWriteTimeout.
func TestDisconnectDeadlineCutsShortStalledWrite(t *testing.T) {
	s := newHeldStream()
	p, done := runPongTestPeer(t, s)

	s.deliver(t, pingMsg)
	expectEntered(t, s.entered, pongMsg) // held with frameWriteTimeout

	start := time.Now()
	go p.Disconnect(p2p.DiscQuitting)
	expectEntered(t, s.entered, discMsg)
	expectPeerReasonWithin(t, done, p2p.DiscQuitting, start, discWriteTimeout+time.Second)
}

func expectPeerReasonWithin(t *testing.T, done <-chan p2p.DiscReason, want p2p.DiscReason, start time.Time, limit time.Duration) {
	t.Helper()
	select {
	case got := <-done:
		if got != want {
			t.Fatalf("run returned %v, want %v", got, want)
		}
		if elapsed := time.Since(start); elapsed > limit {
			t.Fatalf("run returned after %v, want within %v", elapsed, limit)
		}
	case <-time.After(limit):
		t.Fatalf("run did not return within %v", limit)
	}
}
