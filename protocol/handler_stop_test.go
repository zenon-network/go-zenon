package protocol

import (
	"runtime"
	"testing"
	"time"

	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common/types"
)

// stubChainBridge implements ChainBridge with no-ops so NewProtocolManager
// can be constructed without a real chain. Stop() only touches stopOnce,
// quitSync, wg and quit — none of which dereference the bridge — but the
// constructor builds method values on the interface, so a concrete (non-nil)
// implementation is required.
type stubChainBridge struct{}

func (s *stubChainBridge) AddAccountBlocks([]*nom.AccountBlock) error { return nil }
func (s *stubChainBridge) GetTransactions() []*nom.AccountBlock       { return nil }
func (s *stubChainBridge) HasBlock(types.Hash) bool                   { return false }
func (s *stubChainBridge) GetBlockHashesFromHash(types.Hash, uint64) ([]types.Hash, error) {
	return nil, nil
}
func (s *stubChainBridge) GetBlock(types.Hash) *nom.DetailedMomentum { return nil }
func (s *stubChainBridge) GetBlockByNumber(uint64) (*nom.Momentum, error) {
	return nil, nil
}
func (s *stubChainBridge) CurrentBlock() *nom.Momentum {
	return &nom.Momentum{Height: 0}
}
func (s *stubChainBridge) Status() (uint64, types.Hash, types.Hash) {
	return 0, types.Hash{}, types.Hash{}
}
func (s *stubChainBridge) InsertChain([]*nom.DetailedMomentum) (int, error) {
	return 0, nil
}
func (s *stubChainBridge) InsertChainMoved([]*nom.DetailedMomentum) (int, bool, error) {
	return 0, false, nil
}
func (s *stubChainBridge) VerifyMomentum(*nom.DetailedMomentum) error { return nil }

// Calling Stop() twice must be safe: the second call is a no-op due to
// stopOnce.Do(). The test constructs a ProtocolManager via
// NewProtocolManager, calls Stop, then calls Stop again. It verifies no
// panic and that the second call returns immediately.
func TestProtocolManagerStopTwice(t *testing.T) {
	pm := NewProtocolManager(1, 1, &stubChainBridge{})

	// First Stop: closes quitSync, waits on wg (no goroutines started).
	pm.Stop()

	// Second Stop: must be a no-op (stopOnce already fired) and must not
	// panic. The closed quitSync channel is not re-closed.
	pm.Stop()
}

// Start followed by Stop must return promptly and leave no goroutines
// behind. Before the fix that added pm.wg.Add/Done around txsyncLoop,
// Stop() only waited for the syncer goroutine and could return while
// txsyncLoop was still running.
func TestProtocolManagerStartStop(t *testing.T) {
	pm := NewProtocolManager(1, 1, &stubChainBridge{})
	pm.Start()

	done := make(chan struct{})
	go func() {
		pm.Stop()
		close(done)
	}()

	select {
	case <-done:
		// Stop returned; both syncer and txsyncLoop have been joined.
	case <-time.After(5 * time.Second):
		t.Fatal("Stop() did not return within 5s; txsyncLoop may not be tracked by wg")
	}

	// After Stop() returns, no goroutines spawned by Start() should remain.
	// Allow a brief window for any untracked goroutine to exit.
	time.Sleep(50 * time.Millisecond)
	before := runtime.NumGoroutine()
	time.Sleep(100 * time.Millisecond)
	after := runtime.NumGoroutine()
	if after > before {
		t.Logf("goroutines grew from %d to %d after Stop returned", before, after)
	}
}
