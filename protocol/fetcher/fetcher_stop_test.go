package fetcher

import (
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
	inInsert      chan struct{} // closed once insertChain has been entered
	releaseInsert chan struct{}
	inBroadcast   chan struct{} // closed once broadcastBlock has been entered
	releaseBcast  chan struct{}
	inserted      int32
	broadcast     int32
	parentKnown   bool
	gateHeight    bool
}

// release opens every gate so a failed test cannot leave the fetcher's
// goroutines parked.
func (h *stopHarness) release() {
	for _, c := range []chan struct{}{h.releaseVerify, h.releaseGet, h.releaseHeight, h.releaseInsert, h.releaseBcast} {
		select {
		case <-c:
		default:
			close(c)
		}
	}
}

// entered waits for a callback gate to be reached, with a bound.
func entered(t *testing.T, gate <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-gate:
	case <-time.After(3 * time.Second):
		t.Fatalf("%s was not reached", what)
	}
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
		inInsert:      make(chan struct{}),
		releaseInsert: make(chan struct{}),
		inBroadcast:   make(chan struct{}),
		releaseBcast:  make(chan struct{}),
		parentKnown:   parentKnown,
		gateHeight:    gateHeight,
	}
	var getOnce, verifyOnce, heightOnce, insertOnce, bcastOnce int32
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
		func(*nom.DetailedMomentum, bool) {
			if atomic.CompareAndSwapInt32(&bcastOnce, 0, 1) {
				close(h.inBroadcast)
			}
			<-h.releaseBcast
			atomic.StoreInt32(&h.broadcast, 1)
		},
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
			if atomic.CompareAndSwapInt32(&insertOnce, 0, 1) {
				close(h.inInsert)
			}
			<-h.releaseInsert
			atomic.StoreInt32(&h.inserted, 1)
			return len(momentums), nil
		},
		func(string) {},
		nil,
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
	t.Cleanup(h.release)
	close(h.releaseGet)
	close(h.releaseInsert)
	close(h.releaseBcast)
	h.f.Start()
	if err := h.f.Enqueue("peer", h.child); err != nil {
		t.Fatal(err)
	}
	entered(t, h.inVerify, "verifyBlock")

	stopped := stopAsync(h.f)
	mustNotReturn(t, stopped, "the import was still in verification")

	close(h.releaseVerify)
	mustReturn(t, stopped, "the verification was released")
	if atomic.LoadInt32(&h.inserted) != 1 {
		t.Fatal("the released import did not reach insertChain before Stop returned")
	}
}

// The same with the import held inside insertChain, the call the owner
// most needs to see finished before it tears the chain down.
func TestStop_WaitsForAnImportStillInInsertChain(t *testing.T) {
	h := newStopHarness(true, false)
	t.Cleanup(h.release)
	close(h.releaseGet)
	close(h.releaseVerify)
	close(h.releaseBcast)
	h.f.Start()
	if err := h.f.Enqueue("peer", h.child); err != nil {
		t.Fatal(err)
	}
	entered(t, h.inInsert, "insertChain")

	stopped := stopAsync(h.f)
	mustNotReturn(t, stopped, "the import was still in insertChain")

	close(h.releaseInsert)
	mustReturn(t, stopped, "the insert was released")
	if atomic.LoadInt32(&h.inserted) != 1 {
		t.Fatal("insertChain did not complete before Stop returned")
	}
}

// The broadcast that follows a successful import is part of the import's
// lifetime too.
func TestStop_WaitsForTheBroadcastOfAnImportedBlock(t *testing.T) {
	h := newStopHarness(true, false)
	t.Cleanup(h.release)
	close(h.releaseGet)
	close(h.releaseVerify)
	close(h.releaseInsert)
	h.f.Start()
	if err := h.f.Enqueue("peer", h.child); err != nil {
		t.Fatal(err)
	}
	entered(t, h.inBroadcast, "broadcastBlock")

	stopped := stopAsync(h.f)
	mustNotReturn(t, stopped, "the imported block was still being broadcast")

	close(h.releaseBcast)
	mustReturn(t, stopped, "the broadcast was released")
	if atomic.LoadInt32(&h.broadcast) != 1 {
		t.Fatal("the broadcast did not complete before Stop returned")
	}
}

// An import that aborts after Stop (here: parent unknown) must still be
// able to finish: its completion notice has no receiver once the loop has
// exited, so it must not block on it, and Stop must wait for it.
func TestStop_ReleasesAnImportThatAbortsAfterStop(t *testing.T) {
	h := newStopHarness(false, false)
	t.Cleanup(h.release)
	h.f.Start()
	if err := h.f.Enqueue("peer", h.child); err != nil {
		t.Fatal(err)
	}
	entered(t, h.inGetBlock, "getBlock")

	stopped := stopAsync(h.f)
	mustNotReturn(t, stopped, "the import was still looking up its parent")

	// Once released the import aborts (parent unknown) and must still be
	// able to finish although the loop is gone: Stop returning is the
	// evidence, since it joins the import.
	close(h.releaseGet)
	mustReturn(t, stopped, "the parent lookup was released")

	// The public entry points refuse work after Stop.
	if err := h.f.Enqueue("peer", h.child); err != errTerminated {
		t.Fatalf("Enqueue after Stop: %v, want errTerminated", err)
	}
	if err := h.f.Notify("peer", h.child.Momentum.Hash, time.Now(), nil); err != errTerminated {
		t.Fatalf("Notify after Stop: %v, want errTerminated", err)
	}
	if got := h.f.Filter("peer", []*nom.DetailedMomentum{h.child}); got != nil {
		t.Fatalf("Filter after Stop returned %v, want nil", got)
	}
}

// Stop must join the event loop itself, not only the imports: a loop that
// is mid-iteration can still spawn an import after Stop would otherwise
// have returned.
func TestStop_JoinsTheEventLoop(t *testing.T) {
	h := newStopHarness(true, true)
	t.Cleanup(h.release)
	h.f.Start()
	entered(t, h.inHeight, "chainHeight")

	stopped := stopAsync(h.f)
	mustNotReturn(t, stopped, "the loop was still inside an iteration")

	close(h.releaseHeight)
	mustReturn(t, stopped, "the loop iteration was released")
}

func TestStop_IdleFetcherStopsPromptly(t *testing.T) {
	h := newStopHarness(true, false)
	t.Cleanup(h.release)
	h.f.Start()
	mustReturn(t, stopAsync(h.f), "an idle start")
}
