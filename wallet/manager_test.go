package wallet

import (
	"path/filepath"
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
