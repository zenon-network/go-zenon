package downloader

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common/types"
)

func newIdleDownloader() *Downloader {
	return New(
		func(types.Hash) bool { return false },
		func(types.Hash) *nom.DetailedMomentum { return nil },
		func() *nom.Momentum { return &nom.Momentum{} },
		func([]*nom.DetailedMomentum) (int, error) { return 0, nil },
		func(string) {},
		nil,
	)
}

// A hash delivery that arrives after the hash fetcher has returned blocks
// on the one-slot channel and selects only on the session's cancel
// channel. Ending the session must release it, because the peer's
// protocol handler cannot return while it waits, and the transport now
// joins that handler on peer removal and shutdown.
func TestEndSessionReleasesLateDelivery(t *testing.T) {
	d := newIdleDownloader()
	atomic.StoreInt32(&d.synchronising, 1)
	defer atomic.StoreInt32(&d.synchronising, 0)
	d.cancelLock.Lock()
	d.cancelCh = make(chan struct{})
	d.cancelLock.Unlock()

	d.hashCh <- hashPack{"p", nil} // fills the slot; the fetcher is gone
	errc := make(chan error, 1)
	go func() { errc <- d.DeliverHashes("p", nil, nil) }()
	select {
	case err := <-errc:
		t.Fatalf("delivery returned before the session ended: %v", err)
	case <-time.After(100 * time.Millisecond):
	}

	d.endSession()
	select {
	case err := <-errc:
		if err != errNoSyncActive {
			t.Fatalf("released delivery returned %v, want errNoSyncActive", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("late delivery still blocked after the session ended")
	}
}

// A delivery can pass the synchronising check before the session has
// installed its cancel channel. What it captures then is nil, which a
// select never fires on, so a full slot must not hold it (and the peer's handler,
// and the shutdown that joins it) forever. The queue lock holds
// synchronise between publishing the flag and installing the channel.
func TestDeliveryBeforeFirstSessionIsReleased(t *testing.T) {
	for _, kind := range []string{"hashes", "blocks"} {
		t.Run(kind, func(t *testing.T) {
			d := newIdleDownloader()
			d.queue.lock.Lock()
			syncErr := make(chan error, 1)
			go func() { syncErr <- d.synchronise("nobody", types.Hash{}, 1) }()
			deadline := time.Now().Add(5 * time.Second)
			for atomic.LoadInt32(&d.synchronising) == 0 {
				if time.Now().After(deadline) {
					d.queue.lock.Unlock()
					t.Fatal("synchronise never published its flag")
				}
				time.Sleep(time.Millisecond)
			}
			// The slot is full and the session has no channel yet.
			delivered := make(chan error, 1)
			if kind == "hashes" {
				d.hashCh <- hashPack{"a", nil}
				go func() { delivered <- d.DeliverHashes("b", nil, nil) }()
			} else {
				d.blockCh <- blockPack{"a", nil}
				go func() { delivered <- d.DeliverBlocks("b", nil, nil) }()
			}
			// The delivery must complete while the session is still held
			// before installing its channel: there is nothing to wait on.
			unlocked := false
			unlock := func() {
				if !unlocked {
					unlocked = true
					d.queue.lock.Unlock()
				}
			}
			defer unlock()
			select {
			case err := <-delivered:
				if err != errNoSyncActive {
					t.Fatalf("early delivery returned %v, want errNoSyncActive", err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("delivery that raced the first session's setup is blocked")
			}
			unlock()
			if err := <-syncErr; err != errUnknownPeer {
				t.Fatalf("synchronise returned %v, want errUnknownPeer", err)
			}
		})
	}
}

// synchronise must end the session on every return path, not only on the
// error paths that cancel. With no peer registered it returns right after
// creating the session's cancel channel, which must then be closed.
func TestSynchroniseEndsSessionOnReturn(t *testing.T) {
	d := newIdleDownloader()
	if err := d.synchronise("nobody", types.Hash{}, 1); err != errUnknownPeer {
		t.Fatalf("synchronise returned %v, want errUnknownPeer", err)
	}
	d.cancelLock.RLock()
	cancel := d.cancelCh
	d.cancelLock.RUnlock()
	select {
	case <-cancel:
	default:
		t.Fatal("session cancel channel left open after Synchronise returned")
	}
}

// A delivery whose session never takes the batch (it has moved past that
// stage, or stalled) must end when its peer does: the transport joins the
// peer's handler on exit, and the handler is inside this call.
func TestDeliveryAbortsWhenPeerCloses(t *testing.T) {
	for _, kind := range []string{"hashes", "blocks"} {
		t.Run(kind, func(t *testing.T) {
			d := newIdleDownloader()
			atomic.StoreInt32(&d.synchronising, 1)
			defer atomic.StoreInt32(&d.synchronising, 0)
			d.cancelLock.Lock()
			d.cancelCh = make(chan struct{}) // a live session that is not reading
			d.cancelLock.Unlock()
			abort := make(chan struct{})
			delivered := make(chan error, 1)
			if kind == "hashes" {
				d.hashCh <- hashPack{"a", nil}
				go func() { delivered <- d.DeliverHashes("b", nil, abort) }()
			} else {
				d.blockCh <- blockPack{"a", nil}
				go func() { delivered <- d.DeliverBlocks("b", nil, abort) }()
			}
			select {
			case err := <-delivered:
				t.Fatalf("delivery returned before the peer closed: %v", err)
			case <-time.After(100 * time.Millisecond):
			}
			close(abort)
			select {
			case err := <-delivered:
				if err != errPeerClosed {
					t.Fatalf("delivery returned %v after the peer closed, want errPeerClosed", err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("delivery still blocked after its peer closed")
			}
		})
	}
}
