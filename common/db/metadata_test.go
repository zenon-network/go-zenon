package db

import (
	"bytes"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/syndtr/goleveldb/leveldb"
	"github.com/syndtr/goleveldb/leveldb/storage"

	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/types"
)

func TestLevelDBManagerMetadata(t *testing.T) {
	dir := t.TempDir()
	manager, err := NewLevelDBManager(dir)
	common.FailIfErr(t, err)
	store, ok := manager.(MetadataStore)
	if !ok {
		t.Fatal("the LevelDB manager must implement MetadataStore")
	}

	value, err := store.GetMetadata([]byte("key"))
	common.FailIfErr(t, err)
	if value != nil {
		t.Fatalf("expected no value for an unwritten key, got %x", value)
	}

	common.FailIfErr(t, store.PutMetadata([]byte("key"), []byte("value")))
	value, err = store.GetMetadata([]byte("key"))
	common.FailIfErr(t, err)
	if !bytes.Equal(value, []byte("value")) {
		t.Fatalf("expected %q, got %q", "value", value)
	}

	// Metadata stays outside the versioned state.
	if dump := DumpDB(manager.Frontier()); len(dump.Dump()) != len(NewPatch().Dump()) {
		t.Fatalf("metadata leaked into the frontier state: %x", dump.Dump())
	}

	// It persists across a reopen.
	common.FailIfErr(t, manager.Stop())
	if _, err := store.GetMetadata([]byte("key")); err == nil {
		t.Fatal("expected an error reading metadata from a stopped manager")
	}
	if err := store.PutMetadata([]byte("key"), []byte("value")); err == nil {
		t.Fatal("expected an error writing metadata to a stopped manager")
	}
	manager, err = NewLevelDBManager(dir)
	common.FailIfErr(t, err)
	defer manager.Stop()
	value, err = manager.(MetadataStore).GetMetadata([]byte("key"))
	common.FailIfErr(t, err)
	if !bytes.Equal(value, []byte("value")) {
		t.Fatalf("expected %q after reopen, got %q", "value", value)
	}
}

// Metadata never enters a version or a patch and survives Add and Pop:
// a manager with metadata produces exactly the same versions and
// patches as one without.
func TestLevelDBManagerMetadataOutsideVersions(t *testing.T) {
	withMeta := mustNewLevelDBManager(t, t.TempDir())
	t.Cleanup(func() { common.FailIfErr(t, withMeta.Stop()) })
	without := mustNewLevelDBManager(t, t.TempDir())
	t.Cleanup(func() { common.FailIfErr(t, without.Stop()) })
	store := withMeta.(MetadataStore)
	common.FailIfErr(t, store.PutMetadata([]byte("key"), []byte("value")))

	var identifiers []types.HashHeight
	for seed := int64(1); seed <= 3; seed++ {
		a := newMockTransaction(seed, withMeta.Frontier())
		b := newMockTransaction(seed, without.Frontier())
		identifier := a.commit.Identifier()
		common.Expect(t, b.commit.Identifier(), identifier)
		common.FailIfErr(t, withMeta.Add(a))
		common.FailIfErr(t, without.Add(b))
		identifiers = append(identifiers, identifier)
	}
	for _, identifier := range identifiers {
		if !bytes.Equal(DumpDB(withMeta.Get(identifier)).Dump(), DumpDB(without.Get(identifier)).Dump()) {
			t.Fatalf("version %v differs with metadata present", identifier)
		}
		if !bytes.Equal(withMeta.GetPatch(identifier).Dump(), without.GetPatch(identifier).Dump()) {
			t.Fatalf("patch %v differs with metadata present", identifier)
		}
	}

	for range identifiers {
		common.FailIfErr(t, withMeta.Pop())
	}
	value, err := store.GetMetadata([]byte("key"))
	common.FailIfErr(t, err)
	if !bytes.Equal(value, []byte("value")) {
		t.Fatalf("expected metadata to survive Pop, got %q", value)
	}
}

// syncCountingStorage counts Sync calls on the LevelDB journal, which is
// how a synced write reaches stable storage.
type syncCountingStorage struct {
	storage.Storage
	journalSyncs *int64
}

func (s syncCountingStorage) Create(fd storage.FileDesc) (storage.Writer, error) {
	w, err := s.Storage.Create(fd)
	if err != nil || fd.Type != storage.TypeJournal {
		return w, err
	}
	return syncCountingWriter{Writer: w, syncs: s.journalSyncs}, nil
}

type syncCountingWriter struct {
	storage.Writer
	syncs *int64
}

func (w syncCountingWriter) Sync() error {
	atomic.AddInt64(w.syncs, 1)
	return w.Writer.Sync()
}

// PutMetadata fsyncs the journal before it returns, so a crash after a
// successful call cannot lose the record. An ordinary versioned Add is
// the control: it does not sync, which shows the counter tells the two
// apart.
func TestLevelDBManagerMetadataWriteIsSynced(t *testing.T) {
	var syncs int64
	ldb, err := leveldb.Open(syncCountingStorage{Storage: storage.NewMemStorage(), journalSyncs: &syncs}, nil)
	common.FailIfErr(t, err)
	m, err := newLDBManager("mem", ldb)
	common.FailIfErr(t, err)
	t.Cleanup(func() { common.FailIfErr(t, m.Stop()) })

	before := atomic.LoadInt64(&syncs)
	common.FailIfErr(t, m.Add(newMockTransaction(1, m.Frontier())))
	if got := atomic.LoadInt64(&syncs); got != before {
		t.Fatalf("control: an unsynced Add synced the journal %d times", got-before)
	}

	common.FailIfErr(t, m.PutMetadata([]byte("key"), []byte("value")))
	if got := atomic.LoadInt64(&syncs); got <= before {
		t.Fatal("PutMetadata returned without syncing the journal")
	}
}

// A failed synced write is returned to the caller and leaves no record.
func TestLevelDBManagerMetadataWriteFailure(t *testing.T) {
	manager := mustNewLevelDBManager(t, t.TempDir())
	t.Cleanup(func() { common.FailIfErr(t, manager.Stop()) })
	m := manager.(*ldbManager)
	writeErr := errors.New("injected sync failure")
	m.writeSync = func(*leveldb.Batch) error { return writeErr }

	if err := m.PutMetadata([]byte("key"), []byte("value")); !errors.Is(err, writeErr) {
		t.Fatalf("PutMetadata with a failing synced write = %v, want the write error", err)
	}
	value, err := m.GetMetadata([]byte("key"))
	common.FailIfErr(t, err)
	if value != nil {
		t.Fatalf("a failed write left a record: %q", value)
	}
}
