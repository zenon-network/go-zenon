package fetcher

import (
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common/types"
)

// stopHarness drives a started Fetcher through the public API with gated
// callbacks, so a test can hold an import at a chosen point and watch
// what Stop does meanwhile.
type stopHarness struct {
	f             *Fetcher
	parent, child *nom.DetailedMomentum
	inVerify      chan struct{} // closed once verifyBlock has been entered
	releaseVerify chan struct{} // verifyBlock returns once this is closed
	inGetBlock    chan struct{} // closed once getBlock has been entered for the child's parent
	releaseGet    chan struct{}
	inHeight      chan struct{} // closed once chainHeight has been entered
	releaseHeight chan struct{}
	inserted      int32
	parentKnown   bool
	gateHeight    bool
}

func newStopHarness(parentKnown, gateHeight bool) *stopHarness {
	parent := newTestMomentum(1, types.Hash{})
	child := newTestMomentum(2, parent.Hash)
	h := &stopHarness{
		parent:        &nom.DetailedMomentum{Momentum: parent},
		child:         &nom.DetailedMomentum{Momentum: child},
		inVerify:      make(chan struct{}),
		releaseVerify: make(chan struct{}),
		inGetBlock:    make(chan struct{}),
		releaseGet:    make(chan struct{}),
		inHeight:      make(chan struct{}),
		releaseHeight: make(chan struct{}),
		parentKnown:   parentKnown,
		gateHeight:    gateHeight,
	}
	var getOnce, verifyOnce, heightOnce int32
	h.f = New(
		func(hash types.Hash) *nom.DetailedMomentum {
			if hash == parent.Hash {
				if atomic.CompareAndSwapInt32(&getOnce, 0, 1) {
					close(h.inGetBlock)
				}
				<-h.releaseGet
				if h.parentKnown {
					return h.parent
				}
			}
			return nil
		},
		func(detailed *nom.DetailedMomentum) error {
			if atomic.CompareAndSwapInt32(&verifyOnce, 0, 1) {
				close(h.inVerify)
			}
			<-h.releaseVerify
			return nil
		},
		func(*nom.DetailedMomentum, bool) {},
		func() uint64 {
			if h.gateHeight {
				if atomic.CompareAndSwapInt32(&heightOnce, 0, 1) {
					close(h.inHeight)
				}
				<-h.releaseHeight
			}
			return 1
		},
		func(momentums []*nom.DetailedMomentum) (int, error) {
			atomic.StoreInt32(&h.inserted, 1)
			return len(momentums), nil
		},
		func(string) {},
	)
	return h
}

// stopAsync calls Stop on another goroutine and returns a channel closed
// when it has returned.
func stopAsync(f *Fetcher) <-chan struct{} {
	stopped := make(chan struct{})
	go func() {
		f.Stop()
		close(stopped)
	}()
	return stopped
}

func mustNotReturn(t *testing.T, stopped <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-stopped:
		t.Fatalf("Stop returned while %s", what)
	case <-time.After(150 * time.Millisecond):
	}
}

func mustReturn(t *testing.T, stopped <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-stopped:
	case <-time.After(3 * time.Second):
		t.Fatalf("Stop did not return after %s", what)
	}
}

// Stop must not return while an import is still being verified: the import
// goes on to insertChain, which must not run after the fetcher's owner has
// moved on to tearing the chain down.
func TestStop_WaitsForAnImportStillInVerification(t *testing.T) {
	h := newStopHarness(true, false)
	close(h.releaseGet)
	h.f.Start()
	if err := h.f.Enqueue("peer", h.child); err != nil {
		t.Fatal(err)
	}
	<-h.inVerify

	stopped := stopAsync(h.f)
	mustNotReturn(t, stopped, "the import was still in verification")

	close(h.releaseVerify)
	mustReturn(t, stopped, "the verification was released")
	if atomic.LoadInt32(&h.inserted) != 1 {
		t.Fatal("the released import did not reach insertChain before Stop returned")
	}
}

// An import that aborts after Stop (here: parent unknown) must still be
// able to finish: its completion notice has no receiver once the loop has
// exited, so it must not block on it, and Stop must wait for it.
func TestStop_ReleasesAnImportThatAbortsAfterStop(t *testing.T) {
	before := runtime.NumGoroutine()
	h := newStopHarness(false, false)
	h.f.Start()
	if err := h.f.Enqueue("peer", h.child); err != nil {
		t.Fatal(err)
	}
	<-h.inGetBlock

	stopped := stopAsync(h.f)
	mustNotReturn(t, stopped, "the import was still looking up its parent")

	close(h.releaseGet)
	mustReturn(t, stopped, "the parent lookup was released")

	time.Sleep(50 * time.Millisecond)
	if leaked := runtime.NumGoroutine() - before; leaked > 2 {
		t.Fatalf("%d goroutines still alive after Stop", leaked)
	}
}

// Stop must join the event loop itself, not only the imports: a loop that
// is mid-iteration can still spawn an import after Stop would otherwise
// have returned.
func TestStop_JoinsTheEventLoop(t *testing.T) {
	h := newStopHarness(true, true)
	h.f.Start()
	<-h.inHeight

	stopped := stopAsync(h.f)
	mustNotReturn(t, stopped, "the loop was still inside an iteration")

	close(h.releaseHeight)
	mustReturn(t, stopped, "the loop iteration was released")
}

func TestStop_IdleFetcherStopsPromptly(t *testing.T) {
	h := newStopHarness(true, false)
	h.f.Start()
	mustReturn(t, stopAsync(h.f), "an idle start")
}
