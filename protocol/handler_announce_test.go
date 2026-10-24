package protocol

import (
	"testing"
	"time"
)

// Once the manager has stopped, nothing drains newPeerCh. A protocol
// goroutine announcing a late peer must return instead of blocking, or
// the peer-level join in the transport waits on it forever.
func TestAnnouncePeerReturnsAfterStop(t *testing.T) {
	pm := &ProtocolManager{newPeerCh: make(chan *peer, 1), quitSync: make(chan struct{})}
	pm.newPeerCh <- &peer{} // fills the buffer; the syncer is gone
	close(pm.quitSync)
	errc := make(chan error, 1)
	go func() { errc <- pm.announcePeer(&peer{}) }()
	select {
	case err := <-errc:
		if err == nil {
			t.Fatal("announcePeer returned nil after the manager stopped")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("announcePeer blocked after the manager stopped")
	}
}

func TestAnnouncePeerQueuesWhileRunning(t *testing.T) {
	pm := &ProtocolManager{newPeerCh: make(chan *peer, 1), quitSync: make(chan struct{})}
	p := &peer{}
	if err := pm.announcePeer(p); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-pm.newPeerCh:
		if got != p {
			t.Fatal("a different peer was queued")
		}
	default:
		t.Fatal("peer was not queued")
	}
}
