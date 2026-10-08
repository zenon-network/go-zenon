package chain

import (
	"errors"
	"testing"

	"github.com/zenon-network/go-zenon/common/db"
	"github.com/zenon-network/go-zenon/common/types"
)

// stopRecordingManager is a db.Manager that records whether Stop was
// called and returns a configured error from it.
type stopRecordingManager struct {
	stopErr   error
	stopCalls int
}

func (m *stopRecordingManager) Frontier() db.DB                    { return nil }
func (m *stopRecordingManager) Get(types.HashHeight) db.DB         { return nil }
func (m *stopRecordingManager) GetPatch(types.HashHeight) db.Patch { return nil }
func (m *stopRecordingManager) Add(db.Transaction) error           { return nil }
func (m *stopRecordingManager) Pop() error                         { return nil }
func (m *stopRecordingManager) Location() string                   { return "stop-recording" }
func (m *stopRecordingManager) Stop() error {
	m.stopCalls++
	return m.stopErr
}

// stopRecordingCacheManager is a storage.CacheManager with the same
// recording Stop.
type stopRecordingCacheManager struct {
	stopErr   error
	stopCalls int
}

func (m *stopRecordingCacheManager) DB() db.DB                            { return nil }
func (m *stopRecordingCacheManager) Add(types.HashHeight, db.Patch) error { return nil }
func (m *stopRecordingCacheManager) Pop() error                           { return nil }
func (m *stopRecordingCacheManager) Stop() error {
	m.stopCalls++
	return m.stopErr
}

// TestChainStopStopsEveryManagerAfterCacheError pins that chain.Stop keeps
// stopping the chain manager when the cache manager's Stop reports an
// error, and that the error is still returned. Both managers mark
// themselves stopped whatever Close returns, so a Stop that bails out on
// the first error would leave the nom LevelDB handle open until exit.
func TestChainStopStopsEveryManagerAfterCacheError(t *testing.T) {
	cacheErr := errors.New("cache close failed")
	cacheManager := &stopRecordingCacheManager{stopErr: cacheErr}
	chainManager := &stopRecordingManager{}
	c := NewChain(chainManager, cacheManager, nil)

	err := c.Stop()
	if !errors.Is(err, cacheErr) {
		t.Fatalf("expected Stop to report the cache error, got %v", err)
	}
	if chainManager.stopCalls != 1 {
		t.Fatalf("expected the chain manager to be stopped once, got %d calls", chainManager.stopCalls)
	}
}

// TestChainStopReportsEveryManagerError pins that when both managers fail
// the caller sees both errors, not only the first.
func TestChainStopReportsEveryManagerError(t *testing.T) {
	cacheErr := errors.New("cache close failed")
	chainErr := errors.New("chain close failed")
	c := NewChain(&stopRecordingManager{stopErr: chainErr}, &stopRecordingCacheManager{stopErr: cacheErr}, nil)

	err := c.Stop()
	if !errors.Is(err, cacheErr) || !errors.Is(err, chainErr) {
		t.Fatalf("expected Stop to report both manager errors, got %v", err)
	}
}
