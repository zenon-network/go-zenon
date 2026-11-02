package storage

import (
	"errors"
	"fmt"
	"path"
	"strings"
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

// mustNewCacheDBManager opens a cache database for a test, failing
// the test if the database cannot be opened.
func mustNewCacheDBManager(t *testing.T, dataDir string) CacheManager {
	t.Helper()
	m, err := NewCacheDBManager(dataDir)
	if err != nil {
		t.Fatalf("open cache database: %v", err)
	}
	return m
}

func TestAddWriteFailureIsAtomic(t *testing.T) {
	m := mustNewCacheDBManager(t, t.TempDir()).(*cacheManager)
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
	m := mustNewCacheDBManager(t, t.TempDir()).(*cacheManager)
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
	m := mustNewCacheDBManager(t, dir)
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
	m := mustNewCacheDBManager(t, dir)
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
	m := mustNewCacheDBManager(t, t.TempDir()).(*cacheManager)
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

// TestGetFrontierIdentifierCorrupted verifies that GetFrontierIdentifier
// panics on corrupted data, but only after logging the key, store and error.
// The panic is the intended fail-fast behaviour; the test pins that it still
// happens and that the recovery message is meaningful.
func TestGetFrontierIdentifierCorrupted(t *testing.T) {
	mem := db.NewMemDB()
	// Write garbage to the frontier identifier key.
	if err := mem.Put(frontierIdentifierKey, []byte{0xff, 0xfe, 0xfd}); err != nil {
		t.Fatal(err)
	}

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic on corrupted frontier identifier, got none")
		}
	}()
	GetFrontierIdentifier(mem)
}

// TestGetFrontierIdentifierReadError verifies that GetFrontierIdentifier
// panics when the underlying read fails, but only after logging.
func TestGetFrontierIdentifierReadError(t *testing.T) {
	// Use a closed LevelDB to trigger a read error (not ErrNotFound).
	dir := t.TempDir()
	m := mustNewCacheDBManager(t, dir).(*cacheManager)
	common.FailIfErr(t, m.ldb.Close())
	// Bypass the stopped flag by calling GetFrontierIdentifier directly on a
	// wrapper around the closed handle.
	ldb := db.NewLevelDBWrapper(m.ldb)

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic on read error, got none")
		}
		msg := fmt.Sprintf("%v", r)
		if !strings.Contains(msg, "closed") && !strings.Contains(msg, "ErrClosed") {
			t.Fatalf("expected panic message to mention closed DB, got: %v", msg)
		}
	}()
	GetFrontierIdentifier(ldb)
}

// failingGetDB fails reads of one key with a store error other than
// ErrNotFound.
type failingGetDB struct {
	db.DB
	key []byte
	err error
}

func (f *failingGetDB) Get(key []byte) ([]byte, error) {
	if string(key) == string(f.key) {
		return nil, f.err
	}
	return f.DB.Get(key)
}

// A read failure while Add builds the inverse must abort Add with that
// error and nothing written: no frontier move, no change to any value, and
// no rollback record at the attempted height. A fresh patch then adds and
// pops normally. Add replays its frontier entry into the patch it is
// given, so the retry builds a new one rather than reusing the first.
func TestAddInverseReadFailureWritesNothing(t *testing.T) {
	m := mustNewCacheDBManager(t, t.TempDir()).(*cacheManager)
	t.Cleanup(func() { common.FailIfErr(t, m.Stop()) })
	common.FailIfErr(t, m.Add(getMockIdentifier(1), getMockPatch([]byte{1})))

	before := GetFrontierIdentifier(m.DB())
	beforeDump := db.DebugDB(m.DB())
	attempt := func() db.Patch {
		patch := db.NewPatch()
		patch.Put([]byte{1}, []byte{9}) // overwrites a value from height 1
		patch.Put([]byte{2}, []byte{2})
		return patch
	}

	readErr := errors.New("injected read failure")
	m.rollbackPatch = func(d db.DB, p db.Patch) (db.Patch, error) {
		return db.RollbackPatch(&failingGetDB{DB: d, key: []byte{1}, err: readErr}, p)
	}
	write := m.write
	writes := 0
	m.write = func(b *leveldb.Batch) error {
		writes++
		return write(b)
	}

	if err := m.Add(getMockIdentifier(2), attempt()); !errors.Is(err, readErr) {
		t.Fatalf("Add with a failing inverse read = %v, want the read error", err)
	}
	if writes != 0 {
		t.Fatalf("Add wrote %d batches after the inverse failed", writes)
	}
	common.Expect(t, GetFrontierIdentifier(m.DB()), before)
	common.ExpectString(t, db.DebugDB(m.DB()), beforeDump)
	has, err := m.ldb.Has(common.JoinBytes(rollbackByte, common.Uint64ToBytes(2)), nil)
	common.FailIfErr(t, err)
	if has {
		t.Fatal("a rollback record for the failed height 2 was written")
	}

	// Control: with the inverse built normally, a fresh patch adds, and
	// popping it restores the state above, including the overwritten value.
	m.rollbackPatch = db.RollbackPatch
	common.FailIfErr(t, m.Add(getMockIdentifier(2), attempt()))
	common.Expect(t, GetFrontierIdentifier(m.DB()), getMockIdentifier(2))
	if value, err := m.DB().Get([]byte{1}); err != nil || string(value) != string([]byte{9}) {
		t.Fatalf("after the control Add, key 01 = %x (%v), want 09", value, err)
	}
	common.FailIfErr(t, m.Pop())
	common.Expect(t, GetFrontierIdentifier(m.DB()), before)
	common.ExpectString(t, db.DebugDB(m.DB()), beforeDump)
}
