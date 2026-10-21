package wallet

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// writeKeyFileWithSerializedPath creates a key file on disk at realPath
// but whose serialized JSON "Path" field says serializedPath. It returns
// the real path.
func writeKeyFileWithSerializedPath(t *testing.T, ks *KeyStore, password, realPath, serializedPath string) string {
	t.Helper()
	kf, err := ks.Encrypt(password)
	if err != nil {
		t.Fatal(err)
	}
	kf.Path = serializedPath
	data, err := json.MarshalIndent(kf, "", "    ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(realPath, data, 0600); err != nil {
		t.Fatal(err)
	}
	return realPath
}

// TestSerializedPathIgnored verifies that a key file whose JSON names
// another path is indexed, looked up, unlocked, locked and written back
// under its real scanned path. A sentinel at the serialized path must be
// untouched.
func TestSerializedPathIgnored(t *testing.T) {
	dir := t.TempDir()
	ks := testKeyStore(t)

	realPath := filepath.Join(dir, "real")
	sentinelPath := filepath.Join(dir, "sentinel")
	writeKeyFileWithSerializedPath(t, ks, testPassword, realPath, sentinelPath)

	// Create a sentinel at the serialized path; it must not be touched.
	sentinelContent := []byte("sentinel")
	if err := os.WriteFile(sentinelPath, sentinelContent, 0600); err != nil {
		t.Fatal(err)
	}

	m := New(&Config{WalletDir: dir})
	if err := m.Start(); err != nil {
		t.Fatal(err)
	}

	// Lookup by the real path (absolute = scanned path).
	kf, err := m.GetKeyFile(realPath)
	if err != nil {
		t.Fatalf("GetKeyFile(%q): %v", realPath, err)
	}
	if kf.Path != realPath {
		t.Fatalf("KeyFile.Path = %q, want scanned path %q", kf.Path, realPath)
	}

	// Unlock by the real path.
	if err := m.Unlock(realPath, testPassword); err != nil {
		t.Fatalf("Unlock(%q): %v", realPath, err)
	}
	if unlocked, err := m.IsUnlocked(realPath); err != nil || !unlocked {
		t.Fatalf("IsUnlocked(%q) = %v, %v; want true, nil", realPath, unlocked, err)
	}

	// Lock by the real path.
	m.Lock(realPath)
	if unlocked, err := m.IsUnlocked(realPath); err != nil || unlocked {
		t.Fatalf("after Lock: IsUnlocked(%q) = %v, %v; want false, nil", realPath, unlocked, err)
	}

	// Write back: the destination must be the scanned path.
	kf2, err := m.GetKeyFile(realPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := kf2.Write(); err != nil {
		t.Fatalf("Write: %v", err)
	}

	// Sentinel must be untouched.
	got, err := os.ReadFile(sentinelPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(sentinelContent) {
		t.Fatal("sentinel at serialized path was modified")
	}
}

// TestSerializedPathVariants verifies that missing, relative, stale
// absolute and colliding serialized paths all resolve to the scanned
// location.
func TestSerializedPathVariants(t *testing.T) {
	dir := t.TempDir()
	ks := testKeyStore(t)

	variants := []struct {
		name           string
		serializedPath string
	}{
		{"missing", filepath.Join(dir, "nonexistent")},
		{"relative", "some/relative/path"},
		{"stale-absolute", "/tmp/stale/absolute/path"},
		{"colliding", filepath.Join(dir, "other")},
	}

	for _, v := range variants {
		t.Run(v.name, func(t *testing.T) {
			realPath := filepath.Join(dir, "real_"+v.name)
			writeKeyFileWithSerializedPath(t, ks, testPassword, realPath, v.serializedPath)

			m := New(&Config{WalletDir: dir})
			if err := m.Start(); err != nil {
				t.Fatal(err)
			}

			// The file is indexed under its scanned path.
			kf, err := m.GetKeyFile(realPath)
			if err != nil {
				t.Fatalf("GetKeyFile(%q): %v", realPath, err)
			}
			if kf.Path != realPath {
				t.Fatalf("KeyFile.Path = %q, want %q", kf.Path, realPath)
			}

			// Unlock works by the scanned path.
			if err := m.Unlock(realPath, testPassword); err != nil {
				t.Fatalf("Unlock(%q): %v", realPath, err)
			}
			m.Lock(realPath)
		})
	}
}

// TestRelativeWalletDirConsistency verifies that absolute and relative
// wallet directories behave the same for lookup and unlock.
func TestRelativeWalletDirConsistency(t *testing.T) {
	base := t.TempDir()
	ks := testKeyStore(t)

	// Create a wallet dir with one key file, using an absolute path.
	absDir := filepath.Join(base, "wallet")
	if err := os.MkdirAll(absDir, 0700); err != nil {
		t.Fatal(err)
	}
	kf, err := ks.Encrypt(testPassword)
	if err != nil {
		t.Fatal(err)
	}
	kf.Path = filepath.Join(absDir, "store1")
	if err := kf.Write(); err != nil {
		t.Fatal(err)
	}

	// Absolute path manager.
	mAbs := New(&Config{WalletDir: absDir})
	if err := mAbs.Start(); err != nil {
		t.Fatal(err)
	}

	// Relative path manager: chdir into base so "wallet" resolves.
	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(base); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(oldWd)

	mRel := New(&Config{WalletDir: "wallet"})
	if err := mRel.Start(); err != nil {
		t.Fatal(err)
	}

	// Both managers should find the key file by the same relative name.
	absKf, err := mAbs.GetKeyFile("store1")
	if err != nil {
		t.Fatalf("absolute GetKeyFile: %v", err)
	}
	relKf, err := mRel.GetKeyFile("store1")
	if err != nil {
		t.Fatalf("relative GetKeyFile: %v", err)
	}
	// The scanned paths differ (absolute vs relative WalletDir), but each
	// KeyFile.Path matches what its manager scanned.
	if absKf.Path != filepath.Join(absDir, "store1") {
		t.Fatalf("absolute KeyFile.Path = %q, want %q", absKf.Path, filepath.Join(absDir, "store1"))
	}
	if relKf.Path != filepath.Join("wallet", "store1") {
		t.Fatalf("relative KeyFile.Path = %q, want %q", relKf.Path, filepath.Join("wallet", "store1"))
	}

	// Both should unlock by the same relative name.
	if err := mAbs.Unlock("store1", testPassword); err != nil {
		t.Fatalf("absolute Unlock: %v", err)
	}
	if err := mRel.Unlock("store1", testPassword); err != nil {
		t.Fatalf("relative Unlock: %v", err)
	}
	mAbs.Lock("store1")
	mRel.Lock("store1")
}

// TestRenamedKeyFileLoads verifies that a normally generated key file
// that was renamed (so its serialized Path no longer matches its
// location) still loads and unlocks under its scanned path.
func TestRenamedKeyFileLoads(t *testing.T) {
	dir := t.TempDir()
	ks := testKeyStore(t)

	// Write a key file with a matching serialized path.
	originalPath := filepath.Join(dir, "original")
	kf, err := ks.Encrypt(testPassword)
	if err != nil {
		t.Fatal(err)
	}
	kf.Path = originalPath
	if err := kf.Write(); err != nil {
		t.Fatal(err)
	}

	// Rename it: the serialized Path now points to the old name.
	renamedPath := filepath.Join(dir, "renamed")
	if err := os.Rename(originalPath, renamedPath); err != nil {
		t.Fatal(err)
	}

	m := New(&Config{WalletDir: dir})
	if err := m.Start(); err != nil {
		t.Fatal(err)
	}

	// Lookup by the renamed path works.
	if _, err := m.GetKeyFile(renamedPath); err != nil {
		t.Fatalf("GetKeyFile(%q): %v", renamedPath, err)
	}

	// Unlock by the renamed path works.
	if err := m.Unlock(renamedPath, testPassword); err != nil {
		t.Fatalf("Unlock(%q): %v", renamedPath, err)
	}
	m.Lock(renamedPath)
}
