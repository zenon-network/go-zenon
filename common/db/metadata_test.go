package db

import (
	"bytes"
	"testing"

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
