package downloader

import (
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common/types"
)

// TestImportDeferred_PreservesPeerAndRetriesAfterExpiry verifies that a local
// ImportDeferredError from insertChain neither drops nor rejects the delivering
// peer, survives normal endSession() while waiting, and causes process() to
// wait until RetryAfter and retry the batch.
func TestImportDeferred_PreservesPeerAndRetriesAfterExpiry(t *testing.T) {
	const peerID = "honest-peer"
	m := &nom.Momentum{
		Version:         1,
		ChainIdentifier: 1,
		Height:          2,
		Hash:            types.NewHash([]byte("deferred-momentum")),
	}
	detailed := &nom.DetailedMomentum{Momentum: m}

	var attempts int32
	var dropped int32
	var rejected int32
	retryAt := time.Now().Add(40 * time.Millisecond)
	enteredWait := make(chan struct{})

	var d *Downloader
	d = New(
		func(types.Hash) bool { return false },
		func(types.Hash) *nom.DetailedMomentum { return nil },
		func() *nom.Momentum { return &nom.Momentum{Height: 1} },
		func(batch []*nom.DetailedMomentum) (int, error) {
			n := atomic.AddInt32(&attempts, 1)
			if n == 1 {
				close(enteredWait)
				return 0, &ImportDeferredError{
					RetryAfter: retryAt,
					Cause:      errors.New("cooldown active"),
				}
			}
			if time.Now().Before(retryAt) {
				t.Errorf("retried before RetryAfter elapsed")
			}
			return 0, nil
		},
		func(string) { atomic.AddInt32(&dropped, 1) },
		func(string, error) { atomic.AddInt32(&rejected, 1) },
	)
	d.cancelCh = make(chan struct{})

	// Directly verify dropForImportFailure does not penalize the peer.
	d.dropForImportFailure(&Block{RawBlock: detailed, OriginPeer: peerID}, &ImportDeferredError{RetryAfter: retryAt})
	if atomic.LoadInt32(&dropped) != 0 || atomic.LoadInt32(&rejected) != 0 {
		t.Fatalf("dropForImportFailure penalized peer on ImportDeferredError (dropped=%d, rejected=%d)", dropped, rejected)
	}

	// Queue one block and end the session normally while process() is waiting
	// on RetryAfter: endSession() must NOT drop the deferred batch.
	s := newSession(peerID, types.NewHash([]byte("head-1")))
	d.queue.Prepare(2)
	d.queue.SetSession(s)
	d.queue.Insert([]types.Hash{m.Hash}, true)
	req := d.queue.Reserve(newPeer(peerID, 1, types.ZeroHash, nil, nil, nil), 1)
	if req == nil {
		t.Fatal("failed to reserve scheduled momentum")
	}
	if err := d.queue.Deliver(peerID, []*nom.DetailedMomentum{detailed}); err != nil {
		t.Fatalf("queue.Deliver failed: %v", err)
	}

	done := make(chan struct{})
	go func() {
		d.process()
		close(done)
	}()

	<-enteredWait
	d.endSession()
	<-done

	if got := atomic.LoadInt32(&attempts); got != 2 {
		t.Fatalf("expected 2 insertChain attempts after normal endSession, got %d", got)
	}
	if atomic.LoadInt32(&dropped) != 0 || atomic.LoadInt32(&rejected) != 0 {
		t.Fatalf("honest peer was penalized on deferred import (dropped=%d, rejected=%d)", dropped, rejected)
	}
}

// TestImportDeferred_AbortsOnCancelAndTerminate verifies that an explicit
// cancel() or Terminate() wakes the RetryAfter wait immediately.
func TestImportDeferred_AbortsOnCancelAndTerminate(t *testing.T) {
	for _, tc := range []struct {
		name  string
		abort func(*Downloader)
	}{
		{"cancel", func(d *Downloader) { d.cancel() }},
		{"terminate", func(d *Downloader) { d.Terminate() }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			const peerID = "honest-peer"
			m := &nom.Momentum{
				Version:         1,
				ChainIdentifier: 1,
				Height:          2,
				Hash:            types.NewHash([]byte("deferred-" + tc.name)),
			}
			detailed := &nom.DetailedMomentum{Momentum: m}

			enteredWait := make(chan struct{})
			var attempts int32
			d := New(
				func(types.Hash) bool { return false },
				func(types.Hash) *nom.DetailedMomentum { return nil },
				func() *nom.Momentum { return &nom.Momentum{Height: 1} },
				func(batch []*nom.DetailedMomentum) (int, error) {
					if atomic.AddInt32(&attempts, 1) == 1 {
						close(enteredWait)
						return 0, &ImportDeferredError{
							RetryAfter: time.Now().Add(10 * time.Second),
							Cause:      errors.New("long cooldown"),
						}
					}
					return 0, nil
				},
				func(string) {},
				func(string, error) {},
			)
			d.cancelCh = make(chan struct{})

			s := newSession(peerID, types.NewHash([]byte("head-1")))
			d.queue.Prepare(2)
			d.queue.SetSession(s)
			d.queue.Insert([]types.Hash{m.Hash}, true)
			if req := d.queue.Reserve(newPeer(peerID, 1, types.ZeroHash, nil, nil, nil), 1); req == nil {
				t.Fatal("failed to reserve scheduled momentum")
			}
			if err := d.queue.Deliver(peerID, []*nom.DetailedMomentum{detailed}); err != nil {
				t.Fatalf("queue.Deliver failed: %v", err)
			}

			done := make(chan struct{})
			go func() {
				d.process()
				close(done)
			}()

			<-enteredWait
			start := time.Now()
			tc.abort(d)
			select {
			case <-done:
			case <-time.After(2 * time.Second):
				t.Fatalf("%s did not wake deferred RetryAfter wait", tc.name)
			}
			if elapsed := time.Since(start); elapsed > time.Second {
				t.Fatalf("%s took %v to wake wait", tc.name, elapsed)
			}
			if got := atomic.LoadInt32(&attempts); got != 1 {
				t.Fatalf("expected aborted wait not to retry, got %d attempts", got)
			}
		})
	}
}

// TestImportDeferred_RetriesUnslicedBatchOnNonZeroIndex verifies that when
// insertChain defers at index > 0 (for example after restoring a side-chain
// prefix), process() retries the full unsliced batch without advancing the
// session's imported head prematurely.
func TestImportDeferred_RetriesUnslicedBatchOnNonZeroIndex(t *testing.T) {
	const peerID = "honest-peer"
	m1 := &nom.Momentum{Version: 1, ChainIdentifier: 1, Height: 2, Hash: types.NewHash([]byte("side-1"))}
	m2 := &nom.Momentum{Version: 1, ChainIdentifier: 1, Height: 3, Hash: types.NewHash([]byte("side-2"))}
	initialHead := types.NewHash([]byte("head-1"))

	var attempts int32
	var secondBatchLen int32
	var s *session

	d := New(
		func(types.Hash) bool { return false },
		func(types.Hash) *nom.DetailedMomentum { return nil },
		func() *nom.Momentum { return &nom.Momentum{Height: 1, Hash: initialHead} },
		func(batch []*nom.DetailedMomentum) (int, error) {
			n := atomic.AddInt32(&attempts, 1)
			if n == 1 {
				return 1, &ImportDeferredError{
					RetryAfter: time.Now().Add(20 * time.Millisecond),
					Cause:      errors.New("side-chain deferred at index 1"),
				}
			}
			atomic.StoreInt32(&secondBatchLen, int32(len(batch)))
			if !s.knowsHead(initialHead) {
				t.Errorf("session head moved prematurely before retry succeeded")
			}
			return 0, nil
		},
		func(string) { t.Errorf("unexpected dropPeer") },
		func(string, error) { t.Errorf("unexpected rejectPeer") },
	)
	d.cancelCh = make(chan struct{})

	s = newSession(peerID, initialHead)
	d.queue.Prepare(2)
	d.queue.SetSession(s)
	d.queue.Insert([]types.Hash{m1.Hash, m2.Hash}, true)
	if req := d.queue.Reserve(newPeer(peerID, 1, types.ZeroHash, nil, nil, nil), 2); req == nil {
		t.Fatal("failed to reserve scheduled momentums")
	}
	if err := d.queue.Deliver(peerID, []*nom.DetailedMomentum{{Momentum: m1}, {Momentum: m2}}); err != nil {
		t.Fatalf("queue.Deliver failed: %v", err)
	}

	d.process()

	if got := atomic.LoadInt32(&attempts); got != 2 {
		t.Fatalf("expected 2 insertChain attempts, got %d", got)
	}
	if got := atomic.LoadInt32(&secondBatchLen); got != 2 {
		t.Fatalf("expected retry to pass unsliced 2-block batch, got %d", got)
	}
	if !s.knowsHead(m2.Hash) {
		t.Fatal("expected session head to advance to m2 after retry succeeded")
	}
}
