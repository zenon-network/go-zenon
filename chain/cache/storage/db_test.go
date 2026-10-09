package storage

import (
	"errors"
	"fmt"
	"path"
	"testing"

	"github.com/syndtr/goleveldb/leveldb"

	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/db"
	"github.com/zenon-network/go-zenon/common/types"
)

func getMockPatch(value []byte) db.Patch {
	patch := db.NewPatch()
	patch.Put(value, value)
	return patch
}

func getMockIdentifier(height uint64) types.HashHeight {
	return types.HashHeight{
		Hash:   types.NewHash([]byte(fmt.Sprint(height))),
		Height: height,
	}
}

func TestAddWriteFailureIsAtomic(t *testing.T) {
	m := NewCacheDBManager(t.TempDir()).(*cacheManager)
	t.Cleanup(func() { common.FailIfErr(t, m.Stop()) })

	common.FailIfErr(t, m.Add(getMockIdentifier(1), getMockPatch([]byte{1})))
	beforeIdentifier := GetFrontierIdentifier(m.DB())
	beforeDump := db.DebugDB(m.DB())
	writeErr := errors.New("write failed")
	m.write = func(*leveldb.Batch) error {
		return writeErr
	}

	if err := m.Add(getMockIdentifier(2), getMockPatch([]byte{2})); !errors.Is(err, writeErr) {
		t.Fatalf("expected write error, got %v", err)
	}
	common.Expect(t, GetFrontierIdentifier(m.DB()), beforeIdentifier)
	common.ExpectString(t, db.DebugDB(m.DB()), beforeDump)

	has, err := m.ldb.Has(common.JoinBytes(rollbackByte, common.Uint64ToBytes(2)), nil)
	common.FailIfErr(t, err)
	common.ExpectTrue(t, !has)
}

func TestPopWriteFailureIsAtomic(t *testing.T) {
	m := NewCacheDBManager(t.TempDir()).(*cacheManager)
	t.Cleanup(func() { common.FailIfErr(t, m.Stop()) })

	common.FailIfErr(t, m.Add(getMockIdentifier(1), getMockPatch([]byte{1})))
	common.FailIfErr(t, m.Add(getMockIdentifier(2), getMockPatch([]byte{2})))
	beforeIdentifier := GetFrontierIdentifier(m.DB())
	beforeDump := db.DebugDB(m.DB())
	writeErr := errors.New("write failed")
	m.write = func(*leveldb.Batch) error {
		return writeErr
	}

	if err := m.Pop(); !errors.Is(err, writeErr) {
		t.Fatalf("expected write error, got %v", err)
	}
	common.Expect(t, GetFrontierIdentifier(m.DB()), beforeIdentifier)
	common.ExpectString(t, db.DebugDB(m.DB()), beforeDump)

	has, err := m.ldb.Has(common.JoinBytes(rollbackByte, common.Uint64ToBytes(2)), nil)
	common.FailIfErr(t, err)
	common.ExpectTrue(t, has)
}

func TestPop(t *testing.T) {
	dir := t.TempDir()
	m := NewCacheDBManager(dir)
	t.Cleanup(func() { common.FailIfErr(t, m.Stop()) })

	for i := 1; i <= 10; i++ {
		m.Add(getMockIdentifier(uint64(i)), getMockPatch([]byte{byte(i)}))
	}

	common.ExpectString(t, db.DebugDB(m.DB()), `
00 - 0a220a20dd121e36961a04627eacff629765dd3528471ed745c1e32222db4a8a5f3421c4100a
01 - 01
02 - 02
03 - 03
04 - 04
05 - 05
06 - 06
07 - 07
08 - 08
09 - 09
0a - 0a
`)

	for i := 0; i < 5; i++ {
		m.Pop()
	}

	frontierIdentifier := GetFrontierIdentifier(m.DB())
	expectedIdentifier := getMockIdentifier(5)
	common.Expect(t, frontierIdentifier, expectedIdentifier)
	common.ExpectString(t, db.DebugDB(m.DB()), `
00 - 0a220a2086bc56fc56af4c3cde021282f6b727ee9f90dd636e0b0c712a85d416c75e652d1005
01 - 01
02 - 02
03 - 03
04 - 04
05 - 05
`)
}

// TestCacheDBManagerStopIsIdempotent pins the same Stop contract the
// common/db managers have: the first Stop releases the LevelDB handle (the
// directory can be reopened) and a second Stop is a no-op that returns nil,
// so callers such as chain.Stop and test cleanups may call it unconditionally.
func TestCacheDBManagerStopIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	m := NewCacheDBManager(dir)
	// Registered before any assertion; a repeated Stop is the contract
	// under test, so the explicit calls below stay.
	t.Cleanup(func() { common.FailIfErr(t, m.Stop()) })
	common.FailIfErr(t, m.Add(getMockIdentifier(1), getMockPatch([]byte{1})))
	common.FailIfErr(t, m.Stop())
	// The raw handle has no nil-on-repeat contract, so its single Close
	// belongs to the cleanup registered as soon as the open succeeds.
	ldb, err := leveldb.OpenFile(path.Join(dir, "cache"), nil)
	common.FailIfErr(t, err)
	t.Cleanup(func() { common.FailIfErr(t, ldb.Close()) })
	common.FailIfErr(t, m.Stop())
	if m.DB() != nil {
		t.Fatalf("DB must be nil after Stop")
	}
}

// TestCacheDBManagerStopAfterFailedClose pins that Stop marks the manager
// stopped even when closing the handle fails, since goleveldb treats the
// handle as closed from the first Close call regardless of its result.
func TestCacheDBManagerStopAfterFailedClose(t *testing.T) {
	m := NewCacheDBManager(t.TempDir()).(*cacheManager)
	t.Cleanup(func() { common.FailIfErr(t, m.Stop()) })
	// The handle is closed underneath the manager, so the failure the first
	// Stop sees is goleveldb's ErrClosed; an I/O failure of a first Close
	// is not what this fixture produces.
	common.FailIfErr(t, m.ldb.Close())
	if err := m.Stop(); err != leveldb.ErrClosed {
		t.Fatalf("expected Stop to report leveldb.ErrClosed, got %v", err)
	}
	common.FailIfErr(t, m.Stop())
	if m.DB() != nil {
		t.Fatalf("DB must be nil after a Stop that reported an error")
	}
}
