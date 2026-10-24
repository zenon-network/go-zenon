package node

import (
	"errors"
	"fmt"
	"net"
	"path/filepath"
	"testing"
	"time"

	"github.com/prometheus/tsdb/fileutil"
	"github.com/zenon-network/go-zenon/chain"
	"github.com/zenon-network/go-zenon/consensus"
	"github.com/zenon-network/go-zenon/p2p"
	"github.com/zenon-network/go-zenon/p2p/discover"
	"github.com/zenon-network/go-zenon/pillar"
	"github.com/zenon-network/go-zenon/protocol"
	rpc "github.com/zenon-network/go-zenon/rpc/server"
	"github.com/zenon-network/go-zenon/verifier"
	"github.com/zenon-network/go-zenon/zenon"
)

// stubServer implements p2p.Server with no-ops so Node.Stop can run
// without a real p2p backend.
type stubServer struct{}

func (s *stubServer) Start() error           { return nil }
func (s *stubServer) Stop()                  {}
func (s *stubServer) Peers() []p2p.Peer      { return nil }
func (s *stubServer) PeerCount() int         { return 0 }
func (s *stubServer) AddPeer(*discover.Node) {}
func (s *stubServer) Self() *discover.Node   { return nil }

// stubZenon implements zenon.Zenon with a configurable Stop error so
// Node.Stop's error-joining can be observed without a real core.
type stubZenon struct {
	stopErr error
}

func (z *stubZenon) Init() error  { return nil }
func (z *stubZenon) Start() error { return nil }
func (z *stubZenon) Stop() error  { return z.stopErr }
func (z *stubZenon) Chain() chain.Chain {
	return nil
}
func (z *stubZenon) Consensus() consensus.Consensus { return nil }
func (z *stubZenon) Verifier() verifier.Verifier    { return nil }
func (z *stubZenon) Protocol() *protocol.ProtocolManager {
	return nil
}
func (z *stubZenon) Producer() pillar.Manager          { return nil }
func (z *stubZenon) Config() *zenon.Config             { return nil }
func (z *stubZenon) Broadcaster() protocol.Broadcaster { return nil }

// When walletManager and zenon are nil, stopWallet and stopZenon both
// return ErrNodeStopped. Stop must still run stopRPC and closeDataDir,
// and must return a joined error rather than aborting early.
//
// This test uses a real data-dir lock and a real HTTP listener so that
// every cleanup step has observable side effects. It fails under two
// mutations: (1) re-inserting the early return on the first error, which
// leaves the listener accepting and the lock held; (2) joining errors
// but skipping stopRPC/closeDataDir, which has the same visible effect.
func TestStopRunsAllStepsDespiteErrors(t *testing.T) {
	dir := t.TempDir()

	n := &Node{
		config: &Config{DataPath: dir},
		server: &stubServer{},
		stop:   make(chan struct{}),
		http:   newHTTPServer(rpc.DefaultHTTPTimeouts),
		ws:     newHTTPServer(rpc.DefaultHTTPTimeouts),
		// walletManager and z are nil → stopWallet/stopZenon return ErrNodeStopped.
	}

	// Acquire the data-dir lock and start a real HTTP listener.
	if err := n.openDataDir(); err != nil {
		t.Fatalf("openDataDir: %v", err)
	}
	if err := n.http.setListenAddr("127.0.0.1", 0); err != nil {
		t.Fatalf("setListenAddr: %v", err)
	}
	if err := n.http.start(); err != nil {
		t.Fatalf("http.start: %v", err)
	}
	addr := n.http.listener.Addr().String()

	// Confirm the listener is accepting before Stop.
	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		t.Fatalf("pre-stop dial: %v", err)
	}
	conn.Close()

	// Confirm the lock is held.
	if _, _, err := fileutil.Flock(filepath.Join(dir, ".lock")); err == nil {
		t.Fatal("expected data-dir lock to be held before Stop")
	}

	err = n.Stop()
	if err == nil {
		t.Fatal("expected a non-nil error from Stop when subsystems fail")
	}
	if !errors.Is(err, ErrNodeStopped) {
		t.Fatalf("expected joined error to contain ErrNodeStopped, got %v", err)
	}

	// The HTTP listener must be closed: a dial to the old address must fail.
	conn, dialErr := net.DialTimeout("tcp", addr, 2*time.Second)
	if dialErr == nil {
		conn.Close()
		t.Fatal("HTTP listener still accepting after Stop")
	}

	// The data-dir lock must be released: re-acquiring must succeed.
	release, _, err := fileutil.Flock(filepath.Join(dir, ".lock"))
	if err != nil {
		t.Fatalf("data-dir lock still held after Stop: %v", err)
	}
	release.Release()

	// The stop channel must be closed (Wait must return).
	select {
	case <-n.stop:
	case <-time.After(2 * time.Second):
		t.Fatal("stop channel was not closed")
	}

	// A second call must return nil (idempotent guard).
	if err := n.Stop(); err != nil {
		t.Fatalf("second Stop returned %v, want nil", err)
	}
}

// When the zenon core's Stop returns a distinct sentinel error and the
// wallet manager is nil, the joined error from Node.Stop must contain
// both the sentinel and ErrNodeStopped. This pins the error-joining
// behaviour: a mutation that returns only the first error would drop
// the sentinel.
func TestStopJoinsErrorsFromMultipleSubsystems(t *testing.T) {
	dir := t.TempDir()
	sentinel := errors.New("zenon core stop failed")

	n := &Node{
		config: &Config{DataPath: dir},
		server: &stubServer{},
		stop:   make(chan struct{}),
		http:   newHTTPServer(rpc.DefaultHTTPTimeouts),
		ws:     newHTTPServer(rpc.DefaultHTTPTimeouts),
		z:      &stubZenon{stopErr: sentinel},
		// walletManager is nil → stopWallet returns ErrNodeStopped.
	}

	if err := n.openDataDir(); err != nil {
		t.Fatalf("openDataDir: %v", err)
	}

	err := n.Stop()
	if err == nil {
		t.Fatal("expected a non-nil error from Stop")
	}
	if !errors.Is(err, sentinel) {
		t.Errorf("joined error missing zenon sentinel: %v", err)
	}
	if !errors.Is(err, ErrNodeStopped) {
		t.Errorf("joined error missing ErrNodeStopped: %v", err)
	}

	// The data-dir lock must be released even though both subsystems failed.
	release, _, err := fileutil.Flock(filepath.Join(dir, ".lock"))
	if err != nil {
		t.Fatalf("data-dir lock still held after Stop: %v", err)
	}
	release.Release()
}

// The pre-dca90df Stop returned on the first error, abandoning
// stopRPC and closeDataDir. This test verifies that a failing core
// stop with RPC enabled still closes the listener and releases the
// lock — the exact scenario 0x3639 described.
func TestStopFailingCoreStillCleansUp(t *testing.T) {
	dir := t.TempDir()
	sentinel := fmt.Errorf("database close error")

	n := &Node{
		config: &Config{DataPath: dir},
		server: &stubServer{},
		stop:   make(chan struct{}),
		http:   newHTTPServer(rpc.DefaultHTTPTimeouts),
		ws:     newHTTPServer(rpc.DefaultHTTPTimeouts),
		z:      &stubZenon{stopErr: sentinel},
	}

	if err := n.openDataDir(); err != nil {
		t.Fatalf("openDataDir: %v", err)
	}
	if err := n.http.setListenAddr("127.0.0.1", 0); err != nil {
		t.Fatalf("setListenAddr: %v", err)
	}
	if err := n.http.start(); err != nil {
		t.Fatalf("http.start: %v", err)
	}
	addr := n.http.listener.Addr().String()

	err := n.Stop()
	if err == nil {
		t.Fatal("expected non-nil error from Stop")
	}
	if !errors.Is(err, sentinel) {
		t.Errorf("expected sentinel in joined error, got: %v", err)
	}

	// Listener must be closed.
	conn, dialErr := net.DialTimeout("tcp", addr, 2*time.Second)
	if dialErr == nil {
		conn.Close()
		t.Fatal("HTTP listener still accepting after Stop with failing core")
	}

	// Lock must be released.
	release, _, err := fileutil.Flock(filepath.Join(dir, ".lock"))
	if err != nil {
		t.Fatalf("data-dir lock still held after Stop with failing core: %v", err)
	}
	release.Release()

	// Wait must return.
	select {
	case <-n.stop:
	case <-time.After(2 * time.Second):
		t.Fatal("stop channel was not closed")
	}

	// Second Stop must be nil.
	if err := n.Stop(); err != nil {
		t.Fatalf("second Stop returned %v, want nil", err)
	}
}
