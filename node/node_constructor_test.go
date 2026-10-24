package node

import (
	"path/filepath"
	"testing"

	"github.com/prometheus/tsdb/fileutil"
)

// When NewNode fails partway through construction (after openDataDir has
// acquired the data-dir lock), the cleanup-on-error path must release the
// lock so the caller can retry or the directory can be reused.
//
// A nonexistent genesis file causes makeGenesisConfig to fail after
// openDataDir and startWallet have already run, exercising the cleanup
// path. The key assertion: the data-dir lock must be released after the
// failed NewNode returns.
func TestNewNodeErrorReleasesDataDirLock(t *testing.T) {
	dir := t.TempDir()

	cfg := &Config{
		DataPath:    dir,
		GenesisFile: filepath.Join(dir, "nonexistent-genesis.json"),
	}

	_, err := NewNode(cfg)
	if err == nil {
		t.Fatal("expected NewNode to fail with a nonexistent genesis file")
	}

	// The data-dir lock must have been released by cleanupOnError.
	release, _, err := fileutil.Flock(filepath.Join(dir, ".lock"))
	if err != nil {
		t.Fatalf("data-dir lock still held after failed NewNode: %v", err)
	}
	release.Release()
}

// When NewNode fails because the data-dir lock is already held by another
// instance, it must return an error (not panic or exit). The lock remains
// held by the first instance; NewNode must not have released it.
func TestNewNodeFailsWhenDataDirLocked(t *testing.T) {
	dir := t.TempDir()

	// Acquire the lock as if another instance holds it.
	release, _, err := fileutil.Flock(filepath.Join(dir, ".lock"))
	if err != nil {
		t.Fatalf("pre-acquire lock: %v", err)
	}
	defer release.Release()

	cfg := &Config{
		DataPath:    dir,
		GenesisFile: filepath.Join(dir, "nonexistent-genesis.json"),
	}

	_, err = NewNode(cfg)
	if err == nil {
		t.Fatal("expected NewNode to fail when data dir is already locked")
	}
}
