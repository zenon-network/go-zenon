package cache

import (
	"errors"
	"testing"

	"github.com/zenon-network/go-zenon/common/db"
	"github.com/zenon-network/go-zenon/common/types"
)

// errSentinel is the error returned by the mock iterator to simulate a seek failure.
var errSentinel = errors.New("sentinel: iterator seek error")

// mockIterator implements db.StorageIterator. lastResult controls what Last()
// returns; when it returns false, Error() yields errSentinel so callers can
// verify that the error is propagated instead of being swallowed.
type mockIterator struct {
	lastResult bool
	err        error
}

func (m *mockIterator) Next() bool    { return false }
func (m *mockIterator) Prev() bool    { return false }
func (m *mockIterator) Last() bool    { return m.lastResult }
func (m *mockIterator) Key() []byte   { return nil }
func (m *mockIterator) Value() []byte { return nil }
func (m *mockIterator) Error() error  { return m.err }
func (m *mockIterator) Release()      {}

// mockDB implements db.DB, returning a mockIterator from NewIterator.
type mockDB struct {
	db.DB // embed nil interface; only NewIterator is called
	iter  *mockIterator
}

func (m *mockDB) NewIterator(prefix []byte) db.StorageIterator {
	return m.iter
}

func newMockCacheStore(iter *mockIterator) *cacheStore {
	return &cacheStore{
		identifier: types.ZeroHashHeight,
		changes:    db.NewMemDB(),
		db:         &mockDB{iter: iter},
	}
}

func TestFindValue_LastError(t *testing.T) {
	cs := newMockCacheStore(&mockIterator{
		lastResult: false,
		err:        errSentinel,
	})

	_, err := cs.findValue([]byte{0x03})
	if !errors.Is(err, errSentinel) {
		t.Fatalf("expected sentinel error, got: %v", err)
	}
}

func TestFindValue_LastEmpty(t *testing.T) {
	cs := newMockCacheStore(&mockIterator{
		lastResult: false,
		err:        nil,
	})

	val, err := cs.findValue([]byte{0x03})
	if err != nil {
		t.Fatalf("expected nil error, got: %v", err)
	}
	if len(val) != 0 {
		t.Fatalf("expected empty slice, got: %x", val)
	}
}
