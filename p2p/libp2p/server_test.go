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

	ethcrypto "github.com/ethereum/go-ethereum/crypto"
	"github.com/inconshreveable/log15"
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
