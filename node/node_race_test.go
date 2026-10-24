package node

import (
	"errors"
	"path/filepath"
	"sync"
	"testing"

	"github.com/prometheus/tsdb/fileutil"
	"github.com/zenon-network/go-zenon/chain"
	"github.com/zenon-network/go-zenon/consensus"
	"github.com/zenon-network/go-zenon/pillar"
	"github.com/zenon-network/go-zenon/protocol"
	rpc "github.com/zenon-network/go-zenon/rpc/server"
	"github.com/zenon-network/go-zenon/verifier"
	"github.com/zenon-network/go-zenon/zenon"
)

// errStartFailed is returned by raceStubZenon.Start to make Node.Start
// return early (after startZenon), avoiding the RPC API setup that
// requires a real subscribe server. The lock is still exercised.
var errStartFailed = errors.New("start failed")

// raceStubZenon is a stub whose Start returns an error so Node.Start
// unwinds before reaching the RPC API setup (which needs a real
// subscribe server). Init still returns nil so startZenon proceeds to
// the Start call.
type raceStubZenon struct{}

func (z *raceStubZenon) Init() error  { return nil }
func (z *raceStubZenon) Start() error { return errStartFailed }
func (z *raceStubZenon) Stop() error  { return nil }
func (z *raceStubZenon) Chain() chain.Chain {
	return nil
}
func (z *raceStubZenon) Consensus() consensus.Consensus { return nil }
func (z *raceStubZenon) Verifier() verifier.Verifier    { return nil }
func (z *raceStubZenon) Protocol() *protocol.ProtocolManager {
	return nil
}
func (z *raceStubZenon) Producer() pillar.Manager          { return nil }
func (z *raceStubZenon) Config() *zenon.Config             { return nil }
func (z *raceStubZenon) Broadcaster() protocol.Broadcaster { return nil }

// newRaceTestNode returns a Node with just enough state for Start/Stop to
// exercise their lock paths. The zenon core is a stub whose Start returns
// an error so Start unwinds after startZenon, before the RPC API setup
// that would require a real subscribe server. The server is a stub so no
// real p2p backend is needed.
func newRaceTestNode(t *testing.T) *Node {
	t.Helper()
	dir := t.TempDir()
	return &Node{
		config: &Config{DataPath: dir},
		server: &stubServer{},
		stop:   make(chan struct{}),
		http:   newHTTPServer(rpc.DefaultHTTPTimeouts),
		ws:     newHTTPServer(rpc.DefaultHTTPTimeouts),
		z:      &raceStubZenon{},
	}
}

// Two goroutines call Start() concurrently. Node.Start acquires
// node.lock.Lock(), so one call must finish before the other begins.
// Under -race any unsynchronised access to shared state is reported.
// The stub's Start error makes Start unwind after startZenon; the second
// caller then sees node.stopped == true (set by the first caller's
// unwind) and returns immediately.
func TestStartStartRace(t *testing.T) {
	n := newRaceTestNode(t)

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_ = n.Start()
	}()
	go func() {
		defer wg.Done()
		_ = n.Start()
	}()
	wg.Wait()
}

// One goroutine calls Start(), another calls Stop() concurrently.
// Both acquire node.lock, so they are fully serialised. Under -race
// any unsynchronised access to shared state is reported.
func TestStartStopRace(t *testing.T) {
	n := newRaceTestNode(t)

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_ = n.Start()
	}()
	go func() {
		defer wg.Done()
		_ = n.Stop()
	}()
	wg.Wait()
}

// Two goroutines call Stop() concurrently. The first sets node.stopped
// under the lock; the second sees it and returns nil immediately.
// Under -race any unsynchronised access to shared state is reported.
func TestStopStopRace(t *testing.T) {
	n := newRaceTestNode(t)

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_ = n.Stop()
	}()
	go func() {
		defer wg.Done()
		_ = n.Stop()
	}()
	wg.Wait()

	// The data-dir lock must have been released exactly once and be
	// re-acquirable.
	if n.config.DataPath != "" {
		release, _, err := fileutil.Flock(filepath.Join(n.config.DataPath, ".lock"))
		if err != nil {
			t.Fatalf("data-dir lock still held after concurrent Stop: %v", err)
		}
		release.Release()
	}
}
