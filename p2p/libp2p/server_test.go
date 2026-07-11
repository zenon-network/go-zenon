package libp2p

import (
	"bytes"
	"crypto/ecdsa"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	ethcrypto "github.com/ethereum/go-ethereum/crypto"
	"github.com/inconshreveable/log15"
	"github.com/libp2p/go-libp2p/core/peer"
	ma "github.com/multiformats/go-multiaddr"

	"github.com/zenon-network/go-zenon/common"
)

// syncBuffer is a concurrency-safe bytes.Buffer for capturing log
// output written by server goroutines.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// captureP2PLogs redirects common.P2PLogger into a buffer for the
// duration of the test.
func captureP2PLogs(t *testing.T) *syncBuffer {
	t.Helper()
	buf := &syncBuffer{}
	common.P2PLogger.SetHandler(log15.StreamHandler(buf, log15.LogfmtFormat()))
	t.Cleanup(func() { common.P2PLogger.SetHandler(log15.DiscardHandler()) })
	return buf
}

// newTestServer constructs a libp2p Server configured for in-process
// tests: random TCP port, optional peer database, no DHT
// (Discovery=false avoids the DHT bootstrap goroutine which would
// otherwise need network access).
func newTestServer(key *ecdsa.PrivateKey, peerstoreDir string, t *testing.T) *Server {
	t.Helper()
	return &Server{
		PrivateKey:        key,
		Name:              "libp2p-test",
		MaxPeers:          16,
		MinConnectedPeers: 0,
		MaxPendingPeers:   0,
		Discovery:         false,
		NoDial:            true,
		ListenAddr:        "127.0.0.1:0",
		PeerstoreDir:      peerstoreDir,
	}
}

func mustGenKey(t *testing.T) *ecdsa.PrivateKey {
	t.Helper()
	k, err := ethcrypto.GenerateKey()
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	return k
}

// freePortTCP reserves an ephemeral TCP port and returns it. The
// listener is closed before returning, so another process could in
// principle grab the port before the test rebinds it; acceptable for
// tests.
func freePortTCP(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("freePortTCP: %v", err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	_ = l.Close()
	return port
}

// TestStartFailureAfterHostReleasesResources injects a failure at the
// last constructed-but-not-yet-running point of Start() — context,
// datastore, host, and DHT all live — and verifies nothing leaks: the
// TCP listener is released, the peerstore datastore lock is released,
// the server context is cancelled, and the same Server can Start()
// again once the failure cause is gone. This is the scenario the
// switcher's swap-retry path depends on.
func TestStartFailureAfterHostReleasesResources(t *testing.T) {
	port := freePortTCP(t)
	psDir := filepath.Join(t.TempDir(), "ps")

	injected := errors.New("injected start failure")
	srv := &Server{
		PrivateKey:    mustGenKey(t),
		Name:          "cleanup-test",
		MaxPeers:      16,
		Discovery:     true, // ensure the DHT is built and torn down too
		NoDial:        true,
		ListenAddr:    fmt.Sprintf("127.0.0.1:%d", port),
		PeerstoreDir:  psDir,
		testStartHook: func() error { return injected },
	}

	err := srv.Start()
	if err == nil {
		srv.Stop()
		t.Fatal("Start succeeded; want injected failure")
	}
	if !errors.Is(err, injected) {
		t.Fatalf("Start error = %v, want wrapped injected failure", err)
	}

	select {
	case <-srv.ctx.Done():
	default:
		t.Error("server context not cancelled after failed Start")
	}

	if srv.peerdb.Load() != nil {
		t.Error("peer database not closed and detached after failed Start")
	}

	// The listener must be released — bind the same port directly.
	l, lerr := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if lerr != nil {
		t.Fatalf("port %d still bound after failed Start: %v", port, lerr)
	}
	_ = l.Close()

	// The same Server is usable again: this re-binds the same port and
	// re-opens the same peerstore directory, which also proves the
	// LevelDB lock was released.
	srv.testStartHook = nil
	if err := srv.Start(); err != nil {
		t.Fatalf("second Start after failed first: %v", err)
	}
	srv.Stop()
}

// TestStartFailureBeforeHostCancelsContext forces the earliest resource
// failure — the datastore open, by pointing PeerstoreDir below a
// regular file — and verifies the context (the only resource acquired
// by then) is cancelled, and that the server recovers with a valid
// configuration.
func TestStartFailureBeforeHostCancelsContext(t *testing.T) {
	tmp := t.TempDir()
	blocker := filepath.Join(tmp, "not-a-dir")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	srv := newTestServer(mustGenKey(t), filepath.Join(blocker, "ps"), t)
	if err := srv.Start(); err == nil {
		srv.Stop()
		t.Fatal("Start succeeded with peerstore dir under a regular file; want error")
	}

	select {
	case <-srv.ctx.Done():
	default:
		t.Error("server context not cancelled after failed Start")
	}

	srv.PeerstoreDir = filepath.Join(tmp, "ps")
	if err := srv.Start(); err != nil {
		t.Fatalf("second Start with valid dir: %v", err)
	}
	srv.Stop()
}

// TestStartIsolationGuardrail verifies the Crit warning fires exactly
// when a dialing node has neither bootstrap entries nor remembered
// peers — and stays quiet once the peer database has a candidate.
func TestStartIsolationGuardrail(t *testing.T) {
	const isolationMsg = "node is isolated until inbound peers arrive"

	// Case 1: no bootstrap peers, fresh peer database → guardrail fires.
	buf := captureP2PLogs(t)
	srv := &Server{
		PrivateKey:   mustGenKey(t),
		Name:         "guardrail-test",
		MaxPeers:     8,
		ListenAddr:   "127.0.0.1:0",
		PeerstoreDir: filepath.Join(t.TempDir(), "ps"),
	}
	if err := srv.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	srv.Stop()
	if !strings.Contains(buf.String(), isolationMsg) {
		t.Fatal("isolation guardrail did not fire for empty bootstrap + empty peer DB")
	}

	// Case 2: the peer database has a candidate → warm bootstrap dials
	// it (the dial itself may fail; irrelevant) and the guardrail stays
	// quiet.
	dir2 := filepath.Join(t.TempDir(), "ps2")
	pdb, err := openPeerDB(dir2)
	if err != nil {
		t.Fatal(err)
	}
	addr := mustAddr(t, fmt.Sprintf("/ip4/127.0.0.1/tcp/%d", freePortTCP(t)))
	pdb.recordConnected(testPeerID(t), []ma.Multiaddr{addr})
	if err := pdb.Close(); err != nil {
		t.Fatal(err)
	}

	buf2 := captureP2PLogs(t)
	srv2 := &Server{
		PrivateKey:   mustGenKey(t),
		Name:         "guardrail-test-2",
		MaxPeers:     8,
		ListenAddr:   "127.0.0.1:0",
		PeerstoreDir: dir2,
	}
	if err := srv2.Start(); err != nil {
		t.Fatalf("Start 2: %v", err)
	}
	srv2.Stop()
	if strings.Contains(buf2.String(), isolationMsg) {
		t.Fatal("guardrail fired despite warm-bootstrap candidates")
	}
}

// waitForPeerCount polls srv.PeerCount() until it reaches want or the
// deadline elapses.
func waitForPeerCount(t *testing.T, srv *Server, want int, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if srv.PeerCount() >= want {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("PeerCount = %d after %s, want >= %d", srv.PeerCount(), timeout, want)
}

// TestDHTDiscoveryConnectsIndirectPeer is the regression test the July
// 2026 review round asked for: a node bootstrapped to only one peer must
// still reach MinConnectedPeers by discovering a second, indirect peer
// through the DHT rather than staying bootstrap-only.
//
// Topology: A and B bootstrap to each other directly, so each ends up
// with the other in its own DHT routing table. C is bootstrapped only to
// A and requires two peers, so it can only reach MinConnectedPeers by
// learning about B from A's routing table via dhtDiscoveryLoop's active
// RefreshRoutingTable walk and dialing it.
func TestDHTDiscoveryConnectsIndirectPeer(t *testing.T) {
	keyA, keyB, keyC := mustGenKey(t), mustGenKey(t), mustGenKey(t)
	portA, portB, portC := freePortTCP(t), freePortTCP(t), freePortTCP(t)

	addrInfo := func(key *ecdsa.PrivateKey, port int) peer.AddrInfo {
		pid, err := PeerIDFromECDSA(key)
		if err != nil {
			t.Fatalf("derive peer ID: %v", err)
		}
		return peer.AddrInfo{
			ID:    pid,
			Addrs: []ma.Multiaddr{mustAddr(t, fmt.Sprintf("/ip4/127.0.0.1/tcp/%d", port))},
		}
	}
	infoA := addrInfo(keyA, portA)
	infoB := addrInfo(keyB, portB)

	newDiscoveryServer := func(key *ecdsa.PrivateKey, port, minConnected int, bootstrap []peer.AddrInfo) *Server {
		return &Server{
			PrivateKey:        key,
			Name:              "discovery-test",
			MaxPeers:          8,
			MinConnectedPeers: minConnected,
			Discovery:         true,
			NoDial:            false,
			ListenAddr:        fmt.Sprintf("127.0.0.1:%d", port),
			BootstrapPeers:    bootstrap,
		}
	}

	srvA := newDiscoveryServer(keyA, portA, 1, []peer.AddrInfo{infoB})
	if err := srvA.Start(); err != nil {
		t.Fatalf("start A: %v", err)
	}
	defer srvA.Stop()

	srvB := newDiscoveryServer(keyB, portB, 1, []peer.AddrInfo{infoA})
	if err := srvB.Start(); err != nil {
		t.Fatalf("start B: %v", err)
	}
	defer srvB.Stop()

	// B's bootstrap dial to A (already listening by the time B starts)
	// establishes the connection; A picks it up as an inbound stream.
	// Wait for both sides to register it before bringing C up.
	waitForPeerCount(t, srvA, 1, 10*time.Second)
	waitForPeerCount(t, srvB, 1, 10*time.Second)

	srvC := newDiscoveryServer(keyC, portC, 2, []peer.AddrInfo{infoA})
	if err := srvC.Start(); err != nil {
		t.Fatalf("start C: %v", err)
	}
	defer srvC.Stop()

	// C only knows A directly. Reaching MinConnectedPeers=2 requires
	// dhtDiscoveryLoop to learn about B from A's routing table and dial
	// it — bounded well past the 15s discovery tick to absorb DHT
	// convergence and CI scheduling variance.
	waitForPeerCount(t, srvC, 2, 90*time.Second)

	pidB, err := PeerIDFromECDSA(keyB)
	if err != nil {
		t.Fatalf("derive B peer ID: %v", err)
	}
	found := false
	for _, p := range srvC.Peers() {
		if lp, ok := p.(*Peer); ok && lp.RemotePeerID() == pidB.String() {
			found = true
		}
	}
	if !found {
		t.Fatalf("C connected to %d peers but not to B (%s) specifically", srvC.PeerCount(), pidB)
	}
}

// TestStopWaitsForInFlightDHTRefreshBeforeClosingDHT pins the lifecycle
// contract Stop() relies on: it must fully drain any in-flight
// triggerDHTRefresh goroutine before closing the DHT, not merely before
// Stop() itself returns. testRefreshHook blocks the refresh goroutine
// mid-flight so the ordering is deterministic instead of racing the
// real 15s discovery ticker; under -race this would previously have
// flagged RefreshRoutingTable() racing dht.Close() if Stop() closed the
// DHT first.
func TestStopWaitsForInFlightDHTRefreshBeforeClosingDHT(t *testing.T) {
	srv := &Server{
		PrivateKey: mustGenKey(t),
		Name:       "refresh-stop-test",
		MaxPeers:   8,
		Discovery:  true,
		NoDial:     true,
		ListenAddr: "127.0.0.1:0",
	}

	inRefresh := make(chan struct{})
	release := make(chan struct{})
	srv.testRefreshHook = func() {
		close(inRefresh)
		<-release
	}

	if err := srv.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	srv.triggerDHTRefresh()

	select {
	case <-inRefresh:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for refresh goroutine to start")
	}

	stopped := make(chan struct{})
	go func() {
		srv.Stop()
		close(stopped)
	}()

	// Give Stop() time to reach discoveryWG.Wait() before releasing the
	// refresh goroutine, so the assertion below is meaningful rather
	// than a race between this goroutine and Stop()'s own scheduling.
	time.Sleep(100 * time.Millisecond)

	select {
	case <-stopped:
		t.Fatal("Stop() returned while the refresh goroutine was still blocked in-flight")
	default:
	}

	close(release)

	select {
	case <-stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("Stop() did not return after the refresh goroutine was released")
	}
}

// TestStopRejectsInFlightDialAdoption pins the lifecycle contract that
// dialPeer must not adopt a peer (loopWG.Add(1) + peerMap insertion)
// once Stop() has started tearing the server down, even if the dial's
// handshake completed before Stop() was called. testDialAdoptHook holds
// the dialing goroutine in-flight, right after the handshake and before
// adoption, until after Stop() has already returned — the exact window
// in which an unguarded dialPeer would call loopWG.Add(1) after
// loopWG.Wait() had already returned zero, the WaitGroup-reuse panic
// condition. Stop() itself must not block on this goroutine: it hasn't
// been adopted (no loopWG entry) yet, so Stop() only needs to make sure
// it's rejected once it does try to adopt.
func TestStopRejectsInFlightDialAdoption(t *testing.T) {
	keyA := mustGenKey(t)
	keyB := mustGenKey(t)
	portA := freePortTCP(t)

	srvA := newTestServer(keyA, "", t)
	srvA.ListenAddr = fmt.Sprintf("127.0.0.1:%d", portA)
	if err := srvA.Start(); err != nil {
		t.Fatalf("start A: %v", err)
	}
	defer srvA.Stop()

	pidA, err := PeerIDFromECDSA(keyA)
	if err != nil {
		t.Fatalf("derive A peer ID: %v", err)
	}
	infoA := peer.AddrInfo{
		ID:    pidA,
		Addrs: []ma.Multiaddr{mustAddr(t, fmt.Sprintf("/ip4/127.0.0.1/tcp/%d", portA))},
	}

	srvB := newTestServer(keyB, "", t)
	srvB.ListenAddr = "127.0.0.1:0"

	inAdopt := make(chan struct{})
	release := make(chan struct{})
	srvB.testDialAdoptHook = func() {
		close(inAdopt)
		<-release
	}

	if err := srvB.Start(); err != nil {
		t.Fatalf("start B: %v", err)
	}

	dialErrCh := make(chan error, 1)
	go func() {
		dialErrCh <- srvB.dialPeer(infoA)
	}()

	select {
	case <-inAdopt:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for dial goroutine to reach the adoption hook")
	}

	// Stop() must return promptly even though the dial goroutine is
	// still blocked in the hook: it was never adopted, so it holds no
	// loopWG entry for Stop() to wait on.
	stopped := make(chan struct{})
	go func() {
		srvB.Stop()
		close(stopped)
	}()

	select {
	case <-stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("Stop() did not return while waiting on an unadopted dial goroutine")
	}

	// Only now let the dial goroutine reach the adoption section, after
	// Stop()'s loopWG.Wait() has already returned zero.
	close(release)

	select {
	case err := <-dialErrCh:
		if !errors.Is(err, errServerStopped) {
			t.Fatalf("dialPeer error = %v, want errServerStopped", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for dialPeer to return after Stop()")
	}

	if n := srvB.PeerCount(); n != 0 {
		t.Fatalf("PeerCount = %d after Stop() rejected the in-flight adoption, want 0", n)
	}
}
