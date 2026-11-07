package db

import (
	"bytes"
	"testing"

	"github.com/zenon-network/go-zenon/common"
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
