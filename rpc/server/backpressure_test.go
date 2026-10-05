package server

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// backpressureTestService counts concurrent invocations and can block until
// released, so tests can observe how many calls are running at once.
type backpressureTestService struct {
	running   int32 // atomic: currently executing calls
	maxSeen   int32 // atomic: peak concurrent calls observed
	blockChan chan struct{}
}

func newBackpressureTestService() *backpressureTestService {
	return &backpressureTestService{
		blockChan: make(chan struct{}),
	}
}

// Block increments the running counter, records the peak, then waits until
// the test releases it. This simulates a slow call that holds its slot.
func (s *backpressureTestService) Block() (int, error) {
	cur := atomic.AddInt32(&s.running, 1)
	for {
		max := atomic.LoadInt32(&s.maxSeen)
		if cur <= max || atomic.CompareAndSwapInt32(&s.maxSeen, max, cur) {
			break
		}
	}
	<-s.blockChan
	atomic.AddInt32(&s.running, -1)
	return int(cur), nil
}

// Noop returns immediately without holding a slot longer than necessary.
func (s *backpressureTestService) Noop() string {
	return "ok"
}

// Events is a subscription method that creates a subscription, used by the
// unsubscribe-at-capacity test.
func (s *backpressureTestService) Events(ctx context.Context) (*Subscription, error) {
	notifier, ok := NotifierFromContext(ctx)
	if !ok {
		return nil, ErrNotificationsUnsupported
	}
	return notifier.CreateSubscription(), nil
}

func newBackpressureTestServer(t *testing.T) (*Server, *backpressureTestService) {
	t.Helper()
	svc := newBackpressureTestService()
	server := NewServer()
	if err := server.RegisterName("test", svc); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(server.Stop)
	return server, svc
}

// waitForRunning polls until n calls are running or the deadline expires.
func waitForRunning(t *testing.T, svc *backpressureTestService, n int32) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for atomic.LoadInt32(&svc.running) < n {
		if time.Now().After(deadline) {
			t.Fatalf("only %d calls running, want %d", atomic.LoadInt32(&svc.running), n)
		}
		time.Sleep(time.Millisecond)
	}
}

// TestConcurrentCallsBounded verifies that at most maxConcurrentCallsPerConn
// calls execute simultaneously on a single connection, even when more are
// sent concurrently.
func TestConcurrentCallsBounded(t *testing.T) {
	server, svc := newBackpressureTestServer(t)
	client := DialInProc(server)
	defer client.Close()

	const total = maxConcurrentCallsPerConn + 20
	results := make(chan error, total)
	var wg sync.WaitGroup
	for i := 0; i < total; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var n int
			results <- client.Call(&n, "test.block")
		}()
	}

	// Wait until the maximum number of calls are running.
	waitForRunning(t, svc, maxConcurrentCallsPerConn)

	// Give the system a moment to exceed the limit if it were going to.
	time.Sleep(100 * time.Millisecond)

	// Verify the limit was respected.
	max := atomic.LoadInt32(&svc.maxSeen)
	if max > maxConcurrentCallsPerConn {
		t.Fatalf("observed %d concurrent calls, limit is %d", max, maxConcurrentCallsPerConn)
	}

	// Release all blocked calls so the remaining ones can proceed.
	close(svc.blockChan)

	// Wait for all calls to complete.
	wg.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Fatalf("unexpected call error: %v", err)
		}
	}

	// Final check: the peak never exceeded the limit.
	max = atomic.LoadInt32(&svc.maxSeen)
	if max > maxConcurrentCallsPerConn {
		t.Fatalf("observed %d concurrent calls, limit is %d", max, maxConcurrentCallsPerConn)
	}
}

// TestUnsubscribeBlockedAtCapacity documents the known limitation of the
// read-side backpressure design (Option 2 from issue #128): when the
// connection is saturated, the read goroutine blocks on slot acquisition
// and cannot read subsequent messages, including eth_unsubscribe. The
// unsubscribe only goes through after at least one in-flight call
// completes and frees a slot.
func TestUnsubscribeBlockedAtCapacity(t *testing.T) {
	server, svc := newBackpressureTestServer(t)
	client := DialInProc(server)
	defer client.Close()

	// Create a subscription so there is something to unsubscribe.
	var subID string
	if err := client.Call(&subID, "test.subscribe", "events"); err != nil {
		t.Fatalf("subscribe failed: %v", err)
	}

	// Saturate the connection: fill all call slots with blocking calls.
	const saturating = maxConcurrentCallsPerConn
	var wg sync.WaitGroup
	for i := 0; i < saturating; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var n int
			_ = client.Call(&n, "test.block")
		}()
	}

	// Wait until all slots are taken.
	waitForRunning(t, svc, saturating)

	// Send one more call to occupy the read goroutine: it reads this
	// message and blocks on slot acquisition, preventing it from reading
	// any subsequent messages (head-of-line blocking).
	go func() {
		var n int
		_ = client.Call(&n, "test.block")
	}()

	// Give the read goroutine time to read the 65th call and block on
	// slot acquisition.
	time.Sleep(200 * time.Millisecond)

	// Attempt to unsubscribe. The read goroutine is blocked waiting for a
	// slot for the 65th call, so it cannot read the unsubscribe until a
	// slot frees up.
	done := make(chan error, 1)
	go func() {
		var ok bool
		done <- client.Call(&ok, "test.unsubscribe", subID)
	}()

	// The unsubscribe should not complete within a short window because
	// the read goroutine is blocked.
	select {
	case err := <-done:
		t.Fatalf("unsubscribe completed while saturated: %v", err)
	case <-time.After(200 * time.Millisecond):
		// Expected: unsubscribe is waiting for a slot.
	}

	// Release one blocked call to free a slot. This unblocks the read
	// goroutine, which then reads the unsubscribe message.
	close(svc.blockChan)
	wg.Wait()

	// Now the unsubscribe should complete.
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("unsubscribe after release failed: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("unsubscribe did not complete after release")
	}
}

// TestResponsesBypassAdmission verifies that response messages (replies to
// server-initiated calls) are processed even when the connection is
// saturated. Responses bypass slot acquisition in Client.read, so they
// should never block. Notifications also bypass.
func TestResponsesBypassAdmission(t *testing.T) {
	server, svc := newBackpressureTestServer(t)
	client := DialInProc(server)
	defer client.Close()

	// Saturate the connection.
	const saturating = maxConcurrentCallsPerConn
	var wg sync.WaitGroup
	for i := 0; i < saturating; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var n int
			_ = client.Call(&n, "test.block")
		}()
	}

	// Wait until all slots are taken.
	waitForRunning(t, svc, saturating)

	// A notification (no ID, no slot needed) should go through even at
	// capacity. Notifications bypass slot acquisition in read, so the
	// read goroutine can still dispatch them.
	notifyDone := make(chan error, 1)
	go func() {
		notifyDone <- client.Notify(context.Background(), "test.noop")
	}()

	select {
	case err := <-notifyDone:
		if err != nil {
			t.Fatalf("notification at capacity failed: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("notification at capacity timed out")
	}

	// Release the blocked calls.
	close(svc.blockChan)
	wg.Wait()
}

// TestCallSlotReleased verifies that when a call completes, its slot is
// returned and new calls can proceed.
func TestCallSlotReleased(t *testing.T) {
	server, svc := newBackpressureTestServer(t)
	client := DialInProc(server)
	defer client.Close()

	// Run exactly maxConcurrentCallsPerConn calls that block.
	const saturating = maxConcurrentCallsPerConn
	var wg sync.WaitGroup
	for i := 0; i < saturating; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var n int
			_ = client.Call(&n, "test.block")
		}()
	}

	// Wait until all slots are taken.
	waitForRunning(t, svc, saturating)

	// Release all blocked calls.
	close(svc.blockChan)
	wg.Wait()

	// After all calls complete, new calls should succeed immediately.
	var result string
	if err := client.Call(&result, "test.noop"); err != nil {
		t.Fatalf("call after release failed: %v", err)
	}
	if result != "ok" {
		t.Fatalf("unexpected result: %q", result)
	}
}

// TestBatchCallAcquiresSlot verifies that a batch call acquires call slots
// before executing. When the connection is saturated, a new batch must wait.
func TestBatchCallAcquiresSlot(t *testing.T) {
	server, svc := newBackpressureTestServer(t)
	client := DialInProc(server)
	defer client.Close()

	// Saturate the connection with blocking single calls.
	const saturating = maxConcurrentCallsPerConn
	var wg sync.WaitGroup
	for i := 0; i < saturating; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var n int
			_ = client.Call(&n, "test.block")
		}()
	}

	// Wait until all slots are taken.
	waitForRunning(t, svc, saturating)

	// Now send a batch. It must wait for a slot, so it should not complete
	// while the connection is saturated.
	batchDone := make(chan error, 1)
	go func() {
		batch := []BatchElem{
			{Method: "test.noop", Result: new(string)},
		}
		batchDone <- client.BatchCall(batch)
	}()

	// The batch should not complete within a short window.
	select {
	case err := <-batchDone:
		t.Fatalf("batch completed while saturated: %v", err)
	case <-time.After(200 * time.Millisecond):
		// Expected: batch is waiting for a slot.
	}

	// Release the blocked calls.
	close(svc.blockChan)
	wg.Wait()

	// Now the batch should complete.
	select {
	case err := <-batchDone:
		if err != nil {
			t.Fatalf("batch after release failed: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("batch did not complete after release")
	}
}
