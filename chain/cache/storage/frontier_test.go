package storage

import (
	"errors"
	"strings"
	"testing"

	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/db"
	"github.com/zenon-network/go-zenon/common/types"
)

// A handle acquired before Stop keeps pointing at the closed LevelDB. Reading
// the frontier through it must report the stopped manager as an error, not
// panic, so a caller that lost the race with Stop can return the sentinel.
func TestReadFrontierIdentifierClosedHandle(t *testing.T) {
	m := NewCacheDBManager(t.TempDir()).(*cacheManager)
	handle := db.NewLevelDBWrapper(m.ldb).Subset(storageByte)
	common.FailIfErr(t, m.Stop())

	hh, err := ReadFrontierIdentifier(handle)
	if !errors.Is(err, ErrCacheStopped) {
		t.Fatalf("want ErrCacheStopped, got %v", err)
	}
	if !strings.Contains(err.Error(), "closed") {
		t.Fatalf("want the closed-handle cause in the message, got %q", err.Error())
	}
	if hh != types.ZeroHashHeight {
		t.Fatalf("want zero identifier with the error, got %v", hh)
	}
}

func TestReadFrontierIdentifierEmpty(t *testing.T) {
	hh, err := ReadFrontierIdentifier(db.NewMemDB())
	common.FailIfErr(t, err)
	if hh != types.ZeroHashHeight {
		t.Fatalf("want zero identifier for an empty cache, got %v", hh)
	}
}

func TestReadFrontierIdentifierRoundTrip(t *testing.T) {
	mem := db.NewMemDB()
	want := getMockIdentifier(7)
	common.FailIfErr(t, mem.Put(frontierIdentifierKey, want.Serialize()))
	hh, err := ReadFrontierIdentifier(mem)
	common.FailIfErr(t, err)
	if hh != want {
		t.Fatalf("want %v, got %v", want, hh)
	}
}

// Corrupted data is still an error, returned rather than panicked, and it is
// not the stopped sentinel.
func TestReadFrontierIdentifierCorrupted(t *testing.T) {
	mem := db.NewMemDB()
	common.FailIfErr(t, mem.Put(frontierIdentifierKey, []byte{0xff, 0xfe, 0xfd}))
	_, err := ReadFrontierIdentifier(mem)
	if err == nil {
		t.Fatal("want an error for a corrupted frontier identifier")
	}
	if errors.Is(err, ErrCacheStopped) {
		t.Fatalf("corruption must not be reported as a stopped manager: %v", err)
	}
}

// errDB fails every Get with a fixed error; nothing else is called.
type errDB struct {
	db.DB
	err error
}

func (e *errDB) Get([]byte) ([]byte, error) { return nil, e.err }

// A storage error other than a closed handle is returned as itself, so a
// caller cannot mistake a failing read for a stopped manager.
func TestReadFrontierIdentifierOtherReadError(t *testing.T) {
	want := errors.New("disk read failed")
	_, err := ReadFrontierIdentifier(&errDB{err: want})
	if !errors.Is(err, want) {
		t.Fatalf("want the read error itself, got %v", err)
	}
	if errors.Is(err, ErrCacheStopped) {
		t.Fatalf("a read error must not be reported as a stopped manager: %v", err)
	}
}
