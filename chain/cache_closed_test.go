package chain

import (
	"strings"
	"sync"
	"testing"

	"github.com/pkg/errors"
	"github.com/zenon-network/go-zenon/chain/cache/storage"
	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common/db"
	"github.com/zenon-network/go-zenon/common/types"
)

// stopsOnAcquire hands out live handles and, on the n-th DB() call, stops the
// manager right after handing the handle out: the interleaving where Stop
// wins the race after DB() returned. Every read through that handle then
// fails with a closed error. n = 1 is the first acquisition of an operation.
type stopsOnAcquire struct {
	inner storage.CacheManager
	n     int
	calls int
}

func (m *stopsOnAcquire) DB() db.DB {
	handle := m.inner.DB()
	m.calls++
	if m.calls == m.n {
		_ = m.inner.Stop()
	}
	return handle
}
func (m *stopsOnAcquire) Add(id types.HashHeight, p db.Patch) error { return m.inner.Add(id, p) }
func (m *stopsOnAcquire) Pop() error                                { return m.inner.Pop() }
func (m *stopsOnAcquire) Stop() error                               { return m.inner.Stop() }

func stopsAfterFirstAcquire(inner storage.CacheManager) *stopsOnAcquire {
	return &stopsOnAcquire{inner: inner, n: 1}
}

func TestGetFrontierCacheStore_StoppedAfterAcquisition(t *testing.T) {
	inner := storage.NewCacheDBManager(t.TempDir())
	defer inner.Stop()
	c := NewChainCache(stopsAfterFirstAcquire(inner))
	if s := c.GetFrontierCacheStore(); s != nil {
		t.Fatalf("want nil store from a manager stopped after acquisition, got %v", s)
	}
}

func TestGetCacheStore_StoppedAfterAcquisition(t *testing.T) {
	inner := storage.NewCacheDBManager(t.TempDir())
	defer inner.Stop()
	c := NewChainCache(stopsAfterFirstAcquire(inner))
	if s := c.GetCacheStore(types.ZeroHashHeight); s != nil {
		t.Fatalf("want nil store from a manager stopped after acquisition, got %v", s)
	}
}

func TestRollbackCacheTo_StoppedAfterAcquisition(t *testing.T) {
	inner := storage.NewCacheDBManager(t.TempDir())
	defer inner.Stop()
	c := NewChainCache(stopsAfterFirstAcquire(inner))
	var l sync.Mutex
	err := c.RollbackCacheTo(&l, types.ZeroHashHeight)
	if !errors.Is(err, storage.ErrCacheStopped) {
		t.Fatalf("want ErrCacheStopped, got %v", err)
	}
}

// Init's frontier read is the first use of the acquired handle; a Stop that
// lands in between must surface as ErrCacheStopped, not a panic.
func TestInit_StoppedAfterAcquisition(t *testing.T) {
	inner := storage.NewCacheDBManager(t.TempDir())
	defer inner.Stop()
	c := NewChainCache(stopsAfterFirstAcquire(inner))
	err := c.Init(db.NewMemDBManager(db.NewMemDB()), nil)
	if !errors.Is(err, storage.ErrCacheStopped) {
		t.Fatalf("want ErrCacheStopped, got %v", err)
	}
}

// getFrontierStore acquires once for the frontier read and NewCacheStore
// acquires again for the store; a Stop between the two closes the second
// handle before its frontier read.
func TestGetFrontierCacheStore_StoppedOnSecondAcquisition(t *testing.T) {
	inner := storage.NewCacheDBManager(t.TempDir())
	defer inner.Stop()
	c := NewChainCache(&stopsOnAcquire{inner: inner, n: 2})
	if s := c.GetFrontierCacheStore(); s != nil {
		t.Fatalf("want nil store when the second acquisition is closed, got %v", s)
	}
}

// UpdateCache reports a stopped manager as its existing nil-frontier error
// instead of panicking in the frontier read.
func TestUpdateCache_StoppedAfterAcquisition(t *testing.T) {
	inner := storage.NewCacheDBManager(t.TempDir())
	defer inner.Stop()
	c := NewChainCache(stopsAfterFirstAcquire(inner))
	var l sync.Mutex
	err := c.UpdateCache(&l, &nom.DetailedMomentum{Momentum: &nom.Momentum{}}, db.NewPatch())
	if err == nil || !strings.Contains(err.Error(), "cache frontier store is nil") {
		t.Fatalf("want the nil-frontier error, got %v", err)
	}
}

// Frontier at height 2, target 0. Acquisitions: two before the loop, two in
// the first iteration (then Pop), and the fifth opens the second iteration;
// closing there must end the rollback with the sentinel, not a panic.
func TestRollbackCacheTo_StoppedInLaterIteration(t *testing.T) {
	inner := storage.NewCacheDBManager(t.TempDir())
	defer inner.Stop()
	for h := uint64(1); h <= 2; h++ {
		if err := inner.Add(types.HashHeight{Height: h, Hash: types.Hash{byte(h)}}, db.NewPatch()); err != nil {
			t.Fatal(err)
		}
	}
	c := NewChainCache(&stopsOnAcquire{inner: inner, n: 5})
	var l sync.Mutex
	err := c.RollbackCacheTo(&l, types.ZeroHashHeight)
	if !errors.Is(err, storage.ErrCacheStopped) {
		t.Fatalf("want ErrCacheStopped, got %v", err)
	}
}
