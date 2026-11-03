package wallet

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

const testPassword = "secret"

func testManagerWithStores(t *testing.T, names ...string) *Manager {
	t.Helper()
	m := New(&Config{WalletDir: t.TempDir()})
	if err := m.Start(); err != nil {
		t.Fatal(err)
	}
	ks := testKeyStore(t)
	for _, name := range names {
		kf, err := ks.Encrypt(testPassword)
		if err != nil {
			t.Fatal(err)
		}
		kf.Path = filepath.Join(m.config.WalletDir, name)
		if err := kf.Write(); err != nil {
			t.Fatal(err)
		}
	}
	if err := m.Start(); err != nil {
		t.Fatal(err)
	}
	return m
}

// Lock left a nil entry in the decrypted map, so IsUnlocked stayed true,
// GetKeyStore returned (nil, nil), and a double Lock or a later Stop
// panicked on the nil KeyStore.
func TestLockActuallyLocks(t *testing.T) {
	m := testManagerWithStores(t, "store1")

	if err := m.Unlock("store1", testPassword); err != nil {
		t.Fatal(err)
	}
	if unlocked, err := m.IsUnlocked("store1"); err != nil || !unlocked {
		t.Fatalf("after Unlock: IsUnlocked = %v, %v", unlocked, err)
	}

	m.Lock("store1")
	if unlocked, err := m.IsUnlocked("store1"); err != nil || unlocked {
		t.Fatalf("after Lock: IsUnlocked = %v, %v; want false, nil", unlocked, err)
	}
	if ks, err := m.GetKeyStore("store1"); err != ErrKeyStoreLocked || ks != nil {
		t.Fatalf("after Lock: GetKeyStore = %v, %v; want nil, ErrKeyStoreLocked", ks, err)
	}
	// double Lock must not panic
	m.Lock("store1")
}

func TestStopSurvivesLockedStores(t *testing.T) {
	m := testManagerWithStores(t, "store1", "store2")

	if err := m.Unlock("store1", testPassword); err != nil {
		t.Fatal(err)
	}
	if err := m.Unlock("store2", testPassword); err != nil {
		t.Fatal(err)
	}
	still, err := m.GetKeyStore("store2")
	if err != nil {
		t.Fatal(err)
	}

	m.Lock("store1")
	m.Stop() // panicked on the nil entry Lock left behind

	if still.Seed != nil || still.Entropy != nil {
		t.Fatal("Stop did not zero the still-unlocked store")
	}
}

// Start rescans the wallet directory. A store unlocked from a file that is
// gone, or that now holds another seed, must not stay unlocked: its
// secrets are zeroed and dropped. A store whose file is unchanged stays
// unlocked.
func TestStartRescanDropsUnlockedStoresOfChangedFiles(t *testing.T) {
	m := testManagerWithStores(t, "removed", "replaced", "kept")
	stores := map[string]*KeyStore{}
	for _, name := range []string{"removed", "replaced", "kept"} {
		if err := m.Unlock(name, testPassword); err != nil {
			t.Fatal(err)
		}
		ks, err := m.GetKeyStore(name)
		if err != nil {
			t.Fatal(err)
		}
		stores[name] = ks
	}

	if err := os.Remove(m.MakePathAbsolut("removed")); err != nil {
		t.Fatal(err)
	}
	if err := encryptedKeyFile(t, otherKeyStore(t), m.MakePathAbsolut("replaced")).Write(); err != nil {
		t.Fatal(err)
	}
	if err := m.Start(); err != nil {
		t.Fatal(err)
	}

	if unlocked, err := m.IsUnlocked("removed"); err != ErrKeyStoreNotFound {
		t.Fatalf("removed file: IsUnlocked = %v, %v; want ErrKeyStoreNotFound", unlocked, err)
	}
	if unlocked, err := m.IsUnlocked("replaced"); err != nil || unlocked {
		t.Fatalf("replaced file: IsUnlocked = %v, %v; want false, nil", unlocked, err)
	}
	if unlocked, err := m.IsUnlocked("kept"); err != nil || !unlocked {
		t.Fatalf("unchanged file: IsUnlocked = %v, %v; want true, nil", unlocked, err)
	}
	for _, name := range []string{"removed", "replaced"} {
		if _, ok := m.decrypted[m.MakePathAbsolut(name)]; ok {
			t.Fatalf("%s: the old store is still held after the rescan", name)
		}
		if ks := stores[name]; ks.Seed != nil || ks.Entropy != nil {
			t.Fatalf("%s: the old store was dropped without being zeroed", name)
		}
	}
	if ks := stores["kept"]; ks.Seed == nil {
		t.Fatal("kept: the store of an unchanged file was zeroed")
	}

	// The replaced file unlocks again, now as the other wallet.
	if err := m.Unlock("replaced", testPassword); err != nil {
		t.Fatal(err)
	}
	ks, err := m.GetKeyStore("replaced")
	if err != nil {
		t.Fatal(err)
	}
	if want := otherKeyStore(t).BaseAddress; ks.BaseAddress != want {
		t.Fatalf("replaced file unlocked as %v, want %v", ks.BaseAddress, want)
	}
}

// GetKeyFile handed out the manager's own record. Changing it and writing
// it back changed both sides of the rescan comparison, so the store
// unlocked from the old contents stayed unlocked.
func TestStartRescanDropsStoresOfFilesChangedThroughGetKeyFile(t *testing.T) {
	// Changing bytes in place catches a copy that still shares a slice.
	changes := map[string]func(kf *KeyFile){
		"replaced": func(kf *KeyFile) { *kf = *encryptedKeyFile(t, otherKeyStore(t), kf.Path) },
		"cipher":   func(kf *KeyFile) { kf.Crypto.CipherData[0] ^= 0xff },
		"nonce":    func(kf *KeyFile) { kf.Crypto.AesNonce[0] ^= 0xff },
		"salt":     func(kf *KeyFile) { kf.Crypto.Argon2Params.Salt[0] ^= 0xff },
	}
	names := []string{"replaced", "cipher", "nonce", "salt"}
	m := testManagerWithStores(t, names...)
	stores := map[string]*KeyStore{}
	for _, name := range names {
		if err := m.Unlock(name, testPassword); err != nil {
			t.Fatal(err)
		}
		ks, err := m.GetKeyStore(name)
		if err != nil {
			t.Fatal(err)
		}
		stores[name] = ks
	}

	for _, name := range names {
		kf, err := m.GetKeyFile(name)
		if err != nil {
			t.Fatal(err)
		}
		changes[name](kf)
		if err := kf.Write(); err != nil {
			t.Fatal(err)
		}
	}
	if err := m.Start(); err != nil {
		t.Fatal(err)
	}

	for _, name := range names {
		if unlocked, err := m.IsUnlocked(name); err != nil || unlocked {
			t.Fatalf("%s: IsUnlocked = %v, %v; want false, nil", name, unlocked, err)
		}
		if ks := stores[name]; ks.Seed != nil || ks.Entropy != nil {
			t.Fatalf("%s: the old store was dropped without being zeroed", name)
		}
	}
}

// A key file that cannot be read during a rescan is treated like one that
// is gone: its store is zeroed and dropped, and the rescan still completes,
// so it cannot keep the stores of removed files unlocked. Once the file can
// be read again it is known again, locked.
func TestStartRescanDropsTheStoreOfAnUnreadableFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix permission bits")
	}
	names := []string{"unreadable", "removed", "kept"}
	m := testManagerWithStores(t, names...)
	stores := map[string]*KeyStore{}
	for _, name := range names {
		if err := m.Unlock(name, testPassword); err != nil {
			t.Fatal(err)
		}
		ks, err := m.GetKeyStore(name)
		if err != nil {
			t.Fatal(err)
		}
		stores[name] = ks
	}

	unreadable := m.MakePathAbsolut("unreadable")
	if err := os.Chmod(unreadable, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(unreadable, 0o600) })
	if _, err := os.ReadFile(unreadable); err == nil {
		t.Skip("mode 0 does not stop this user reading the file (root)")
	}
	if err := os.Remove(m.MakePathAbsolut("removed")); err != nil {
		t.Fatal(err)
	}
	if err := m.Start(); err != nil {
		t.Fatalf("Start with an unreadable key file = %v, want nil", err)
	}

	for _, name := range []string{"unreadable", "removed"} {
		if unlocked, err := m.IsUnlocked(name); err != ErrKeyStoreNotFound {
			t.Fatalf("%s: IsUnlocked = %v, %v; want ErrKeyStoreNotFound", name, unlocked, err)
		}
		if ks := stores[name]; ks.Seed != nil || ks.Entropy != nil {
			t.Fatalf("%s: the old store was not zeroed", name)
		}
	}
	if unlocked, err := m.IsUnlocked("kept"); err != nil || !unlocked {
		t.Fatalf("kept: IsUnlocked = %v, %v; want true, nil", unlocked, err)
	}

	if err := os.Chmod(unreadable, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := m.Start(); err != nil {
		t.Fatal(err)
	}
	if unlocked, err := m.IsUnlocked("unreadable"); err != nil || unlocked {
		t.Fatalf("readable again: IsUnlocked = %v, %v; want false, nil", unlocked, err)
	}
	if err := m.Unlock("unreadable", testPassword); err != nil {
		t.Fatalf("readable again: Unlock = %v", err)
	}
}

// Stop drops both maps; a later Start must leave the manager usable.
func TestStartAfterStopCanUnlock(t *testing.T) {
	m := testManagerWithStores(t, "store1")
	m.Stop()
	if err := m.Start(); err != nil {
		t.Fatal(err)
	}
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("Unlock after Stop and Start panicked: %v", r)
			}
		}()
		if err := m.Unlock("store1", testPassword); err != nil {
			t.Fatal(err)
		}
	}()
	if unlocked, err := m.IsUnlocked("store1"); err != nil || !unlocked {
		t.Fatalf("IsUnlocked = %v, %v; want true, nil", unlocked, err)
	}
}
