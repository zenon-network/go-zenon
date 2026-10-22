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

// writeKeyFileWithSerializedPathRaw creates a key file on disk at realPath
// but whose serialized JSON "Path" field says serializedPath. Unlike
// writeKeyFileWithSerializedPath it does not use the helper above; the
// caller provides the KeyStore so distinct passwords (and therefore
// distinct identities) can be used per file.
func writeKeyFileWithSerializedPathRaw(t *testing.T, ks *KeyStore, password, realPath, serializedPath string) string {
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

// writeKeyFileOmittingPath creates a key file on disk at realPath whose
// JSON has no "Path" key at all (not even an empty string), the shape
// produced by generators that predate the field.
func writeKeyFileOmittingPath(t *testing.T, ks *KeyStore, password, realPath string) string {
	t.Helper()
	kf, err := ks.Encrypt(password)
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.MarshalIndent(kf, "", "    ")
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	delete(raw, "Path")
	data, err = json.MarshalIndent(raw, "", "    ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(realPath, data, 0600); err != nil {
		t.Fatal(err)
	}
	return realPath
}

// TestSerializedPathVariants verifies that missing, relative and stale
// absolute serialized paths all resolve to the scanned location. The
// colliding case (two files sharing one serialized Path) has its own
// test below; the omitted-Path case (no Path key at all) is a separate
// sub-test here.
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

	t.Run("omitted", func(t *testing.T) {
		realPath := filepath.Join(dir, "real_omitted")
		writeKeyFileOmittingPath(t, ks, testPassword, realPath)

		m := New(&Config{WalletDir: dir})
		if err := m.Start(); err != nil {
			t.Fatal(err)
		}

		kf, err := m.GetKeyFile(realPath)
		if err != nil {
			t.Fatalf("GetKeyFile(%q): %v", realPath, err)
		}
		if kf.Path != realPath {
			t.Fatalf("KeyFile.Path = %q, want %q", kf.Path, realPath)
		}

		if err := m.Unlock(realPath, testPassword); err != nil {
			t.Fatalf("Unlock(%q): %v", realPath, err)
		}
		m.Lock(realPath)
	})
}

// TestSerializedPathColliding verifies that two valid key files whose
// JSON carries the same serialized Path are each found and unlocked
// under their own scanned path. Distinct passwords and distinct
// identities ensure an accidental substitution of one file for the
// other would fail.
func TestSerializedPathColliding(t *testing.T) {
	dir := t.TempDir()

	// Two genuinely different keystores (different entropy, different
	// base addresses) whose key files carry the same serialized Path.
	ks1 := testKeyStore(t)
	entropy2 := make([]byte, 32)
	for i := range entropy2 {
		entropy2[i] = byte(i + 1)
	}
	ks2, err := keyStoreFromEntropy(entropy2)
	if err != nil {
		t.Fatal(err)
	}
	if ks1.BaseAddress == ks2.BaseAddress {
		t.Fatal("test setup: keystores must have distinct base addresses")
	}

	collidingPath := filepath.Join(dir, "colliding")

	path1 := filepath.Join(dir, "file1")
	path2 := filepath.Join(dir, "file2")
	writeKeyFileWithSerializedPathRaw(t, ks1, "password-one", path1, collidingPath)
	writeKeyFileWithSerializedPathRaw(t, ks2, "password-two", path2, collidingPath)

	m := New(&Config{WalletDir: dir})
	if err := m.Start(); err != nil {
		t.Fatal(err)
	}

	// Both files are indexed under their scanned paths.
	kf1, err := m.GetKeyFile(path1)
	if err != nil {
		t.Fatalf("GetKeyFile(%q): %v", path1, err)
	}
	if kf1.Path != path1 {
		t.Fatalf("KeyFile.Path = %q, want scanned path %q", kf1.Path, path1)
	}

	kf2, err := m.GetKeyFile(path2)
	if err != nil {
		t.Fatalf("GetKeyFile(%q): %v", path2, err)
	}
	if kf2.Path != path2 {
		t.Fatalf("KeyFile.Path = %q, want scanned path %q", kf2.Path, path2)
	}

	// Each file unlocks with its own password; a substitution would fail.
	if err := m.Unlock(path1, "password-one"); err != nil {
		t.Fatalf("Unlock(%q, password-one): %v", path1, err)
	}
	m.Lock(path1)

	if err := m.Unlock(path2, "password-two"); err != nil {
		t.Fatalf("Unlock(%q, password-two): %v", path2, err)
	}
	m.Lock(path2)

	// Cross-unlock must fail: path1 was encrypted with password-one.
	if err := m.Unlock(path1, "password-two"); err == nil {
		t.Fatal("Unlock(path1, password-two) succeeded; files were substituted")
	}
	m.Lock(path1)
}

// TestRelativeWalletDirConsistency verifies that absolute and relative
// wallet directories behave the same for lookup and unlock.
func TestRelativeWalletDirConsistency(t *testing.T) {
	// Resolve symlinks so that os.Getwd (which resolves symlinks) and
	// filepath.Abs (which does not) agree on the spelling of base. On
	// macOS t.TempDir returns /var/folders/..., a symlink to
	// /private/var/folders/...; the same happens on Linux with a
	// symlinked $TMPDIR.
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
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
	// Start resolves a relative WalletDir to absolute, so both managers
	// index the key file under its absolute scanned path.
	wantPath := filepath.Join(absDir, "store1")
	if absKf.Path != wantPath {
		t.Fatalf("absolute KeyFile.Path = %q, want %q", absKf.Path, wantPath)
	}
	if relKf.Path != wantPath {
		t.Fatalf("relative KeyFile.Path = %q, want %q", relKf.Path, wantPath)
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

	// Absolute-path lookup and unlock must also work on the relative-dir
	// manager: Start resolved the directory, so the index keys match.
	absFilePath := filepath.Join(absDir, "store1")
	if _, err := mRel.GetKeyFile(absFilePath); err != nil {
		t.Fatalf("relative-dir GetKeyFile by absolute path: %v", err)
	}
	if err := mRel.Unlock(absFilePath, testPassword); err != nil {
		t.Fatalf("relative-dir Unlock by absolute path: %v", err)
	}
	if unlocked, err := mRel.IsUnlocked(absFilePath); err != nil || !unlocked {
		t.Fatalf("relative-dir IsUnlocked by absolute path = %v, %v; want true, nil", unlocked, err)
	}
	mRel.Lock(absFilePath)
	if unlocked, err := mRel.IsUnlocked(absFilePath); err != nil || unlocked {
		t.Fatalf("after Lock: IsUnlocked(%q) = %v, %v; want false, nil", absFilePath, unlocked, err)
	}
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
