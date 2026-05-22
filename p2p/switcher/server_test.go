package switcher

import (
	"crypto/ecdsa"
	"fmt"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/crypto"
)

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
