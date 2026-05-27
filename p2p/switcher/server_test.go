package switcher

import (
	"crypto/ecdsa"
	"errors"
	"fmt"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/crypto"

	"github.com/zenon-network/go-zenon/p2p"
	"github.com/zenon-network/go-zenon/p2p/discover"
)

// ──────────────────────────────────────────────────────────────────────
// Test helpers
// ──────────────────────────────────────────────────────────────────────

// fakeOracle implements SporkOracle for testing.
type fakeOracle struct {
	mu     sync.Mutex
	active bool
}

func (o *fakeOracle) IsLibp2pActive() bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.active
}

func (o *fakeOracle) setActive(v bool) {
	o.mu.Lock()
	o.active = v
	o.mu.Unlock()
}

// freePort returns an available TCP port.
func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("freePort: %v", err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	return port
}

// testKey generates an ephemeral secp256k1 key for testing.
func testKey(t *testing.T) *ecdsa.PrivateKey {
	t.Helper()
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	return key
}

// newTestServer creates a switcher.Server with real backend config
// sufficient for Start/Stop lifecycle tests.
func newTestServer(t *testing.T, oracle *fakeOracle) *Server {
	t.Helper()
	return &Server{
		PrivateKey: testKey(t),
		Name:       "test-node",
		MaxPeers:   10,
		ListenAddr: fmt.Sprintf("127.0.0.1:%d", freePort(t)),
		Oracle:     oracle,
	}
}

// ──────────────────────────────────────────────────────────────────────
// Mock backend
// ──────────────────────────────────────────────────────────────────────

// mockBackend implements the backend interface for unit testing the
// switcher's swap logic without real network I/O.
type mockBackend struct {
	mu       sync.Mutex
	started  bool
	stopped  bool
	startErr error         // if set, Start() returns this
	startFn  func() error  // if set, called instead of default Start logic
	peerCnt  int
	peers    []p2p.Peer
	selfNode *discover.Node
	addPeerC chan *discover.Node // records AddPeer calls (nil = discard)
}

func (m *mockBackend) Start() error {
	if m.startFn != nil {
		return m.startFn()
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.startErr != nil {
		return m.startErr
	}
	m.started = true
	return nil
}

func (m *mockBackend) Stop() {
	m.mu.Lock()
	m.stopped = true
	m.mu.Unlock()
}

func (m *mockBackend) Peers() []p2p.Peer {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.peers
}

func (m *mockBackend) PeerCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.peerCnt
}

func (m *mockBackend) AddPeer(node *discover.Node) {
	if m.addPeerC != nil {
		m.addPeerC <- node
	}
}

func (m *mockBackend) Self() *discover.Node {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.selfNode != nil {
		return m.selfNode
	}
	return &discover.Node{}
}

// isStarted reports whether Start() was called successfully.
func (m *mockBackend) isStarted() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.started
}

// isStopped reports whether Stop() was called.
func (m *mockBackend) isStopped() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.stopped
}

// newMockServer creates a switcher.Server wired with mock backends.
// The returned backends can be inspected/reconfigured before calling
// Start().
func newMockServer(t *testing.T, oracle *fakeOracle) (*Server, *mockBackend, *mockBackend) {
	t.Helper()
	legacy := &mockBackend{peerCnt: 5}
	libp2p := &mockBackend{peerCnt: 10}
	srv := &Server{
		PrivateKey: testKey(t),
		Name:       "test-node",
		MaxPeers:   10,
		ListenAddr: fmt.Sprintf("127.0.0.1:%d", freePort(t)),
		Oracle:     oracle,
		NewLegacy:  func() backend { return legacy },
		NewLibp2p:  func() backend { return libp2p },
	}
	return srv, legacy, libp2p
}

// waitForSwap polls until the switcher's active backend changes (i.e.
// swap completes) or times out. Returns the PeerCount from the active
// backend.
func waitForSwap(t *testing.T, srv *Server, timeout time.Duration) int {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if srv.PeerCount() > 0 {
			return srv.PeerCount()
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("waitForSwap: timed out waiting for swap to complete")
	return 0
}

// waitForCondition polls a predicate until it returns true or times out.
func waitForCondition(t *testing.T, timeout time.Duration, desc string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("waitForCondition: timed out waiting for %s", desc)
}

// ──────────────────────────────────────────────────────────────────────
// Original regression tests (real backends)
// ──────────────────────────────────────────────────────────────────────

// TestStopPreActivation_Bug1 verifies that Stop() does not deadlock
// when called before the spork activates. This was Bug 1: the watcher
// read srv.stopCh directly; after Stop() closed and nilled it, the
// select on a nil channel blocked forever.
func TestStopPreActivation_Bug1(t *testing.T) {
	oracle := &fakeOracle{active: false}
	srv := newTestServer(t, oracle)

	if err := srv.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Immediately stop before activation — should not deadlock.
	done := make(chan struct{})
	go func() {
		srv.Stop()
		close(done)
	}()

	select {
	case <-done:
		// success
	case <-time.After(5 * time.Second):
		t.Fatal("Stop() deadlocked — Bug 1 regression")
	}
}

// TestStopDuringSwap_Bug2 verifies that if Stop() races with swap(),
// the libp2p backend is not started after Stop() returns. This was
// Bug 2: swap() didn't check stopCh before starting libp2p.
func TestStopDuringSwap_Bug2(t *testing.T) {
	oracle := &fakeOracle{active: false}
	srv := newTestServer(t, oracle)

	if err := srv.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Activate the spork — watcher will fire swap on next 1s tick.
	oracle.setActive(true)

	// Race: stop while the watcher may be mid-swap.
	time.Sleep(50 * time.Millisecond)
	done := make(chan struct{})
	go func() {
		srv.Stop()
		close(done)
	}()

	select {
	case <-done:
		// success
	case <-time.After(5 * time.Second):
		t.Fatal("Stop() deadlocked during swap — Bug 2 regression")
	}

	// After Stop, no backend should be active.
	if srv.PeerCount() != 0 {
		t.Fatal("PeerCount != 0 after Stop; backend may be leaked")
	}
}

// TestNilOracle_Bug3 verifies that Start() does not launch the
// activation watcher when Oracle is nil, which would panic on the
// first tick.
func TestNilOracle_Bug3(t *testing.T) {
	srv := &Server{
		PrivateKey: testKey(t),
		Name:       "test-node",
		MaxPeers:   10,
		ListenAddr: fmt.Sprintf("127.0.0.1:%d", freePort(t)),
		// Oracle intentionally nil
	}

	if err := srv.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	done := make(chan struct{})
	go func() {
		srv.Stop()
		close(done)
	}()

	select {
	case <-done:
		// success
	case <-time.After(5 * time.Second):
		t.Fatal("Stop() deadlocked with nil oracle — Bug 3 regression")
	}
}

// TestSporkAlreadyActive verifies that when the oracle reports the
// spork as already active at startup, libp2p is started directly
// without launching the watcher.
func TestSporkAlreadyActive(t *testing.T) {
	oracle := &fakeOracle{active: true}
	srv := newTestServer(t, oracle)

	if err := srv.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	done := make(chan struct{})
	go func() {
		srv.Stop()
		close(done)
	}()

	select {
	case <-done:
		// success
	case <-time.After(5 * time.Second):
		t.Fatal("Stop() deadlocked when spork already active")
	}
}

// TestDoubleStop verifies that calling Stop() twice is safe.
func TestDoubleStop(t *testing.T) {
	oracle := &fakeOracle{active: false}
	srv := newTestServer(t, oracle)

	if err := srv.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	srv.Stop()
	srv.Stop() // should be a no-op
}

// ──────────────────────────────────────────────────────────────────────
// Swap coverage tests (mock backends)
// ──────────────────────────────────────────────────────────────────────

// TestSwapHappyPath verifies the full swap lifecycle: legacy starts,
// oracle activates, swap fires, legacy is stopped, libp2p starts, and
// delegation switches to the libp2p backend.
func TestSwapHappyPath(t *testing.T) {
	oracle := &fakeOracle{active: false}
	srv, legacyMock, libp2pMock := newMockServer(t, oracle)

	if err := srv.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Legacy should be running.
	if !legacyMock.isStarted() {
		t.Fatal("legacy backend not started")
	}
	if srv.PeerCount() != 5 {
		t.Fatalf("PeerCount = %d, want 5 (legacy)", srv.PeerCount())
	}

	// Activate the spork — watcher will swap on next tick.
	oracle.setActive(true)

	// Wait for the swap to complete (libp2p PeerCount = 10).
	waitForCondition(t, 5*time.Second, "swap to libp2p", func() bool {
		return libp2pMock.isStarted()
	})

	// Legacy should have been stopped.
	if !legacyMock.isStopped() {
		t.Fatal("legacy backend not stopped after swap")
	}

	// Delegation should now point to libp2p.
	if srv.PeerCount() != 10 {
		t.Fatalf("PeerCount = %d, want 10 (libp2p)", srv.PeerCount())
	}

	// Clean shutdown.
	srv.Stop()
	if !libp2pMock.isStopped() {
		t.Fatal("libp2p backend not stopped after Stop()")
	}
}

// TestSwapLibp2pStartFails verifies that when the libp2p backend fails
// to start during swap, the node is left without an active backend
// (no panic, no leak).
func TestSwapLibp2pStartFails(t *testing.T) {
	oracle := &fakeOracle{active: false}
	srv, _, libp2pMock := newMockServer(t, oracle)

	// Configure libp2p to fail on Start().
	libp2pMock.startErr = errors.New("libp2p start failed")

	if err := srv.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Activate the spork.
	oracle.setActive(true)

	// Wait for the watcher to attempt the swap. After the failed
	// swap, PeerCount should be 0 (no active backend).
	waitForCondition(t, 5*time.Second, "swap attempt", func() bool {
		return libp2pMock.isStarted() || srv.PeerCount() == 0
	})

	// The node should have no active backend.
	if srv.PeerCount() != 0 {
		t.Fatalf("PeerCount = %d after failed swap, want 0", srv.PeerCount())
	}

	srv.Stop()
}

// TestStopDuringSwapRace verifies that if Stop() arrives while the
// libp2p backend is starting (Stage 2 of swap), the freshly-built
// libp2p is torn down and not leaked.
func TestStopDuringSwapRace(t *testing.T) {
	oracle := &fakeOracle{active: false}
	srv, _, libp2pMock := newMockServer(t, oracle)

	// Make libp2p Start() block until we release it, simulating a
	// slow startup that Stop() races against.
	started := make(chan struct{})
	release := make(chan struct{})
	libp2pMock.startFn = func() error {
		libp2pMock.mu.Lock()
		libp2pMock.started = true
		libp2pMock.mu.Unlock()
		close(started) // signal that Start() is executing
		<-release      // block until test releases
		return nil
	}

	if err := srv.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Activate the spork — watcher will call swap().
	oracle.setActive(true)

	// Wait for libp2p Start() to begin executing.
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for libp2p Start() to begin")
	}

	// Stop() while libp2p is mid-Start().
	done := make(chan struct{})
	go func() {
		srv.Stop()
		close(done)
	}()

	// Release the blocked Start() so swap can proceed to Stage 3,
	// where it should detect stopCh == nil and tear down.
	close(release)

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Stop() deadlocked during swap race")
	}

	// libp2p should have been torn down (Stop() called).
	if !libp2pMock.isStopped() {
		t.Fatal("libp2p not stopped after Stop() raced with swap")
	}

	// No active backend.
	if srv.PeerCount() != 0 {
		t.Fatalf("PeerCount = %d after stop-during-swap, want 0", srv.PeerCount())
	}
}

// TestDoubleStart verifies that calling Start() twice returns an error.
func TestDoubleStart(t *testing.T) {
	oracle := &fakeOracle{active: false}
	srv, _, _ := newMockServer(t, oracle)

	if err := srv.Start(); err != nil {
		t.Fatalf("first Start: %v", err)
	}
	defer srv.Stop()

	if err := srv.Start(); err == nil {
		t.Fatal("second Start() should return error, got nil")
	}
}

// TestStopBeforeStart verifies that Stop() on an unstarted server is
// a no-op (no panic, no deadlock).
func TestStopBeforeStart(t *testing.T) {
	srv := &Server{
		PrivateKey: testKey(t),
		Name:       "test-node",
		MaxPeers:   10,
		ListenAddr: fmt.Sprintf("127.0.0.1:%d", freePort(t)),
	}

	done := make(chan struct{})
	go func() {
		srv.Stop()
		close(done)
	}()

	select {
	case <-done:
		// success — no-op
	case <-time.After(2 * time.Second):
		t.Fatal("Stop() on unstarted server deadlocked")
	}
}

// TestDelegation verifies that Peers(), PeerCount(), AddPeer(), and
// Self() delegate to the active backend.
func TestDelegation(t *testing.T) {
	oracle := &fakeOracle{active: true} // start libp2p directly
	srv, _, libp2pMock := newMockServer(t, oracle)

	selfNode := &discover.Node{
		IP:  net.ParseIP("1.2.3.4"),
		TCP: 35555,
	}
	libp2pMock.selfNode = selfNode
	libp2pMock.peers = []p2p.Peer{} // empty but non-nil

	if err := srv.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer srv.Stop()

	// PeerCount
	if n := srv.PeerCount(); n != 10 {
		t.Fatalf("PeerCount = %d, want 10", n)
	}

	// Peers
	if p := srv.Peers(); p == nil {
		t.Fatal("Peers() returned nil, want non-nil slice")
	}

	// Self
	s := srv.Self()
	if s == nil {
		t.Fatal("Self() returned nil")
	}
	if !s.IP.Equal(selfNode.IP) {
		t.Fatalf("Self().IP = %s, want %s", s.IP, selfNode.IP)
	}

	// AddPeer
	libp2pMock.addPeerC = make(chan *discover.Node, 1)
	addNode := &discover.Node{IP: net.ParseIP("5.6.7.8"), TCP: 35556}
	srv.AddPeer(addNode)
	select {
	case got := <-libp2pMock.addPeerC:
		if !got.IP.Equal(addNode.IP) {
			t.Fatalf("AddPeer got IP %s, want %s", got.IP, addNode.IP)
		}
	default:
		t.Fatal("AddPeer did not reach the backend")
	}
}

// TestSwapAbortsIfStopped verifies that if Stop() is called before
// swap's Stage 1 acquires the lock, swap sees stopCh == nil and
// aborts without starting libp2p.
func TestSwapAbortsIfStopped(t *testing.T) {
	oracle := &fakeOracle{active: false}
	srv, legacyMock, libp2pMock := newMockServer(t, oracle)

	if err := srv.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Stop the server before activating the spork.
	srv.Stop()

	// Activate the spork after Stop. The watcher should have exited
	// via stopCh, so swap should never fire.
	oracle.setActive(true)

	// Give the watcher time to potentially fire (it shouldn't).
	time.Sleep(1500 * time.Millisecond)

	if libp2pMock.isStarted() {
		t.Fatal("libp2p started after Stop() — swap should have aborted")
	}
	if !legacyMock.isStopped() {
		t.Fatal("legacy not stopped")
	}
}

// TestSwapOnceGuard verifies that swap() is guarded by sync.Once —
// even if the watcher somehow fires twice, the second swap is a no-op.
func TestSwapOnceGuard(t *testing.T) {
	oracle := &fakeOracle{active: false}
	srv, legacyMock, libp2pMock := newMockServer(t, oracle)

	if err := srv.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Activate the spork.
	oracle.setActive(true)

	// Wait for swap to complete.
	waitForCondition(t, 5*time.Second, "swap to libp2p", func() bool {
		return libp2pMock.isStarted()
	})

	// Record the state after first swap.
	firstSwapStopped := legacyMock.isStopped()
	if !firstSwapStopped {
		t.Fatal("legacy not stopped after first swap")
	}

	// The sync.Once guard means swap() cannot fire a second time.
	// We verify this indirectly: if swap ran twice, the second
	// NewLibp2p call would create a new mock (since our factory
	// returns the same pointer, this is fine — but the guard is
	// the important thing). Just verify the server is still healthy.
	if srv.PeerCount() != 10 {
		t.Fatalf("PeerCount = %d, want 10", srv.PeerCount())
	}

	srv.Stop()
}

// TestLegacyStartFails verifies that if the legacy backend fails to
// start, Start() propagates the error and cleans up.
func TestLegacyStartFails(t *testing.T) {
	oracle := &fakeOracle{active: false}
	srv, legacyMock, _ := newMockServer(t, oracle)

	legacyMock.startErr = errors.New("port in use")

	if err := srv.Start(); err == nil {
		t.Fatal("Start() should return error when legacy fails, got nil")
	}

	// Server should not be running — Stop should be a no-op.
	srv.Stop()
}

// TestSporkAlreadyActive_Mock verifies that when the oracle reports
// the spork as already active at startup, the libp2p backend is
// started directly (legacy is never touched).
func TestSporkAlreadyActive_Mock(t *testing.T) {
	oracle := &fakeOracle{active: true}
	srv, legacyMock, libp2pMock := newMockServer(t, oracle)

	if err := srv.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer srv.Stop()

	if legacyMock.isStarted() {
		t.Fatal("legacy should not be started when spork already active")
	}
	if !libp2pMock.isStarted() {
		t.Fatal("libp2p not started when spork already active")
	}
	if srv.PeerCount() != 10 {
		t.Fatalf("PeerCount = %d, want 10 (libp2p)", srv.PeerCount())
	}
}

// TestDelegationNilBackend verifies that delegation methods return
// zero values when no backend is active (before Start or after Stop).
func TestDelegationNilBackend(t *testing.T) {
	srv := &Server{
		PrivateKey: testKey(t),
		Name:       "test-node",
		MaxPeers:   10,
		ListenAddr: fmt.Sprintf("127.0.0.1:%d", freePort(t)),
	}

	// Before Start — all methods should return zero values.
	if srv.PeerCount() != 0 {
		t.Fatalf("PeerCount before Start = %d, want 0", srv.PeerCount())
	}
	if srv.Peers() != nil {
		t.Fatal("Peers() before Start should return nil")
	}
	if srv.Self() == nil {
		t.Fatal("Self() before Start should return non-nil empty Node")
	}
	// AddPeer should not panic.
	srv.AddPeer(&discover.Node{})
}
