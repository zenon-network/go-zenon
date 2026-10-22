package chain

import (
	"sync"
	"testing"

	"github.com/pkg/errors"
	"github.com/zenon-network/go-zenon/chain/cache/storage"
	"github.com/zenon-network/go-zenon/common/db"
	"github.com/zenon-network/go-zenon/common/types"
)

// stoppedManager implements storage.CacheManager with DB() returning nil,
// matching the post-Stop state of the real cacheManager.
type stoppedManager struct{}

func (m *stoppedManager) DB() db.DB                            { return nil }
func (m *stoppedManager) Add(types.HashHeight, db.Patch) error { return storage.ErrCacheStopped }
func (m *stoppedManager) Pop() error                           { return storage.ErrCacheStopped }
func (m *stoppedManager) Stop() error                          { return nil }

func TestRollbackCacheTo_StoppedManager(t *testing.T) {
	c := NewChainCache(&stoppedManager{})
	var locker sync.Mutex
	err := c.RollbackCacheTo(&locker, types.ZeroHashHeight)
	if !errors.Is(err, storage.ErrCacheStopped) {
		t.Fatalf("expected ErrCacheStopped, got: %v", err)
	}
}

func TestGetCacheStore_StoppedManager(t *testing.T) {
	c := NewChainCache(&stoppedManager{})
	store := c.GetCacheStore(types.ZeroHashHeight)
	if store != nil {
		t.Fatal("expected nil store from stopped manager")
	}
}

func TestGetFrontierCacheStore_StoppedManager(t *testing.T) {
	c := NewChainCache(&stoppedManager{})
	store := c.GetFrontierCacheStore()
	if store != nil {
		t.Fatal("expected nil store from stopped manager")
	}
}

// stopsAfter wraps a real manager and makes DB() return nil after limit
// calls. It models the acquisition results a caller can observe around a
// Stop; the underlying database stays open, so this is not a concurrency or
// closed-handle test.
type stopsAfter struct {
	inner storage.CacheManager
	calls int
	limit int
}

func (m *stopsAfter) DB() db.DB {
	m.calls++
	if m.calls > m.limit {
		return nil
	}
	return m.inner.DB()
}
func (m *stopsAfter) Add(id types.HashHeight, p db.Patch) error { return m.inner.Add(id, p) }
func (m *stopsAfter) Pop() error                                { return m.inner.Pop() }
func (m *stopsAfter) Stop() error                               { return m.inner.Stop() }

// One acquisition succeeds, the next returns nil: the store handed out must
// still be backed by the handle that was acquired.
func TestGetCacheStore_SingleAcquisition(t *testing.T) {
	inner := storage.NewCacheDBManager(t.TempDir())
	defer inner.Stop()
	c := NewChainCache(&stopsAfter{inner: inner, limit: 1})
	s := c.GetCacheStore(types.ZeroHashHeight)
	if s == nil {
		t.Fatal("expected a store: the single DB acquisition succeeded")
	}
	if _, err := s.GetStakeBeneficialAmount(types.Address{}); err != nil {
		t.Fatalf("read through the acquired handle: %v", err)
	}
}

// Frontier at height 2, target 0. The pre-loop frontier read uses two DB()
// calls; the loop's first read sees a stopped manager and must return the
// sentinel instead of dereferencing a nil store.
func TestRollbackCacheTo_StopInsideLoop(t *testing.T) {
	inner := storage.NewCacheDBManager(t.TempDir())
	defer inner.Stop()
	for h := uint64(1); h <= 2; h++ {
		if err := inner.Add(types.HashHeight{Height: h, Hash: types.Hash{byte(h)}}, db.NewPatch()); err != nil {
			t.Fatal(err)
		}
	}
	c := NewChainCache(&stopsAfter{inner: inner, limit: 2})
	var l sync.Mutex
	err := c.RollbackCacheTo(&l, types.ZeroHashHeight)
	if !errors.Is(err, storage.ErrCacheStopped) {
		t.Fatalf("want ErrCacheStopped, got %v", err)
	}
}
