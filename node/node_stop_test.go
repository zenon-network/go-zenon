package node

import (
	"errors"
	"testing"

	"github.com/zenon-network/go-zenon/p2p"
	"github.com/zenon-network/go-zenon/p2p/discover"
	rpc "github.com/zenon-network/go-zenon/rpc/server"
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

// When walletManager and zenon are nil, stopWallet and stopZenon both
// return ErrNodeStopped. Stop must still run stopRPC and closeDataDir,
// and must return a joined error rather than aborting early.
func TestStopRunsAllStepsDespiteErrors(t *testing.T) {
	n := &Node{
		server: &stubServer{},
		stop:   make(chan struct{}),
		http:   newHTTPServer(rpc.DefaultHTTPTimeouts),
		ws:     newHTTPServer(rpc.DefaultHTTPTimeouts),
		// walletManager and z are nil → stopWallet/stopZenon return ErrNodeStopped.
	}

	err := n.Stop()
	if err == nil {
		t.Fatal("expected a non-nil error from Stop when subsystems fail")
	}
	if !errors.Is(err, ErrNodeStopped) {
		t.Fatalf("expected joined error to contain ErrNodeStopped, got %v", err)
	}

	// The stop channel must be closed exactly once.
	select {
	case <-n.stop:
	default:
		t.Fatal("stop channel was not closed")
	}

	// A second call must return nil (idempotent guard).
	if err := n.Stop(); err != nil {
		t.Fatalf("second Stop returned %v, want nil", err)
	}
}
