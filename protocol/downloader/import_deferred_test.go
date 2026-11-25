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
// peer, and causes process() to wait until RetryAfter and retry the batch.
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
	retryAt := time.Now().Add(30 * time.Millisecond)

	d := New(
		func(types.Hash) bool { return false },
		func(types.Hash) *nom.DetailedMomentum { return nil },
		func() *nom.Momentum { return &nom.Momentum{Height: 1} },
		func(batch []*nom.DetailedMomentum) (int, error) {
			n := atomic.AddInt32(&attempts, 1)
			if n == 1 {
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

	// Queue one block and run process(): it must defer, wait until RetryAfter,
	// retry, succeed, and keep the peer connected.
	s := &session{peer: peerID}
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

	d.process()

	if got := atomic.LoadInt32(&attempts); got != 2 {
		t.Fatalf("expected 2 insertChain attempts (defer + retry), got %d", got)
	}
	if atomic.LoadInt32(&dropped) != 0 || atomic.LoadInt32(&rejected) != 0 {
		t.Fatalf("honest peer was penalized on deferred import (dropped=%d, rejected=%d)", dropped, rejected)
	}
}
