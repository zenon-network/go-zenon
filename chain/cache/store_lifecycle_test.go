package cache

import (
	"errors"
	"testing"

	"github.com/zenon-network/go-zenon/chain/cache/storage"
	"github.com/zenon-network/go-zenon/common/db"
	"github.com/zenon-network/go-zenon/common/types"
)

// stopsAfterAcquire hands out a live handle and then stops the manager before
// the caller can use it: the acquisition succeeds, every read through the
// handle fails with a closed-database error.
type stopsAfterAcquire struct {
	inner storage.CacheManager
}

func (m *stopsAfterAcquire) DB() db.DB {
	handle := m.inner.DB()
	_ = m.inner.Stop()
	return handle
}
func (m *stopsAfterAcquire) Add(id types.HashHeight, p db.Patch) error { return m.inner.Add(id, p) }
func (m *stopsAfterAcquire) Pop() error                                { return m.inner.Pop() }
func (m *stopsAfterAcquire) Stop() error                               { return m.inner.Stop() }

// The constructor's frontier read runs on a handle that was closed after it
// was acquired. It must report the stopped manager as a nil store, the same
// answer a pre-acquisition Stop gives, instead of panicking.
func TestNewCacheStore_StoppedAfterAcquisition(t *testing.T) {
	inner, err := storage.NewCacheDBManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer inner.Stop()
	s := NewCacheStore(types.ZeroHashHeight, &stopsAfterAcquire{inner: inner})
	if s != nil {
		t.Fatalf("want nil store from a manager stopped after acquisition, got %v", s)
	}
}

// failingReadManager hands out a handle whose reads fail with an error that
// is not a closed handle.
type failingReadManager struct {
	storage.CacheManager
	err error
}

type errDB struct {
	db.DB
	err error
}

func (e *errDB) Get([]byte) ([]byte, error) { return nil, e.err }

func (m *failingReadManager) DB() db.DB { return &errDB{err: m.err} }

// Only the stopped manager is downgraded to a nil store; any other frontier
// read error keeps the fail-fast panic the constructor had before.
func TestNewCacheStore_OtherReadErrorStillPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("want a panic on a non-closed frontier read error, got none")
		}
	}()
	NewCacheStore(types.ZeroHashHeight, &failingReadManager{err: errors.New("disk read failed")})
	t.Fatal("unreachable: NewCacheStore returned")
}
