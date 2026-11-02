package wallet

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/zenon-network/go-zenon/common/types"
)

// otherKeyStore is a valid key store whose seed differs from testKeyStore's.
func otherKeyStore(t *testing.T) *KeyStore {
	t.Helper()
	ks, err := keyStoreFromEntropy(bytes.Repeat([]byte{7}, 32))
	if err != nil {
		t.Fatal(err)
	}
	return ks
}

func encryptedKeyFile(t *testing.T, ks *KeyStore, path string) *KeyFile {
	t.Helper()
	kf, err := ks.Encrypt(testPassword)
	if err != nil {
		t.Fatal(err)
	}
	kf.Path = path
	return kf
}

func requirePrivateKeyFile(t *testing.T, path string) {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !info.Mode().IsRegular() {
		t.Fatalf("%s is %v after Write, want a regular file", path, info.Mode().Type())
	}
	if runtime.GOOS != "windows" {
		if perm := info.Mode().Perm(); perm != 0o600 {
			t.Fatalf("%s has mode %#o after Write, want 0600", path, perm)
		}
	}
	if _, err := ReadKeyFile(path); err != nil {
		t.Fatalf("ReadKeyFile(%s) after Write: %v", path, err)
	}
}

// Write leaves nothing but the key file in the directory.
func requireOnlyEntry(t *testing.T, dir, name string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != name {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("directory holds %v after Write, want only %s", names, name)
	}
}

// A new key file holds an encrypted seed; nobody but its owner needs to
// read it, and it is not a program.
func TestKeyFileWriteCreatesAPrivateFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "wallet")
	if err := encryptedKeyFile(t, testKeyStore(t), path).Write(); err != nil {
		t.Fatal(err)
	}
	requirePrivateKeyFile(t, path)
	requireOnlyEntry(t, dir, "wallet")
}

// Writing over an existing, broadly readable key file leaves it private.
func TestKeyFileWriteTightensAnExistingFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix permission bits")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "wallet")
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o644); err != nil { // past the umask
		t.Fatal(err)
	}
	if err := encryptedKeyFile(t, testKeyStore(t), path).Write(); err != nil {
		t.Fatal(err)
	}
	requirePrivateKeyFile(t, path)
	requireOnlyEntry(t, dir, "wallet")
}

// A symlink at the key file's path is replaced by the key file; the file
// it pointed to is left alone.
func TestKeyFileWriteReplacesASymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(t.TempDir(), "elsewhere")
	if err := os.WriteFile(target, []byte("not a key file"), 0o600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "wallet")
	if err := os.Symlink(target, path); err != nil {
		t.Skipf("cannot create a symlink here: %v", err)
	}
	if err := encryptedKeyFile(t, testKeyStore(t), path).Write(); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(target); err != nil || string(got) != "not a key file" {
		t.Fatalf("the symlink's target now holds %q (err %v); Write followed the link", got, err)
	}
	requirePrivateKeyFile(t, path)
	requireOnlyEntry(t, dir, "wallet")
}

// A name as long as the filesystem allows is still writable: the
// temporary file's name does not grow with Path's.
func TestKeyFileWriteLongName(t *testing.T) {
	dir := t.TempDir()
	name := strings.Repeat("w", 250)
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("probe"), 0o600); err != nil {
		t.Skipf("this filesystem does not take a %d-byte name: %v", len(name), err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := encryptedKeyFile(t, testKeyStore(t), path).Write(); err != nil {
		t.Fatalf("Write to a %d-byte name: %v", len(name), err)
	}
	requirePrivateKeyFile(t, path)
	requireOnlyEntry(t, dir, name)
}

// Path's directory is the one the kernel resolves, not the one a lexical
// clean suggests: through a symlink, "link/.." is the link target's
// parent. The temporary file must be created there, where the rename
// lands, and not in the lexical parent, which may not even be writable.
func TestKeyFileWriteResolvesTheDirectoryLikeTheRename(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinked directories and Unix permission bits")
	}
	a, b := t.TempDir(), t.TempDir()
	if err := os.Mkdir(filepath.Join(b, "child"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(b, "child"), filepath.Join(a, "link")); err != nil {
		t.Skipf("cannot create a symlink here: %v", err)
	}
	if err := os.Chmod(a, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(a, 0o700) })
	if f, err := os.CreateTemp(a, "probe"); err == nil {
		_ = f.Close()
		_ = os.Remove(f.Name())
		t.Skip("mode 0500 does not stop this user writing the directory (root)")
	}

	path := a + string(filepath.Separator) + "link" + string(filepath.Separator) + ".." + string(filepath.Separator) + "wallet"
	if err := encryptedKeyFile(t, testKeyStore(t), path).Write(); err != nil {
		t.Fatalf("Write(%s): %v", path, err)
	}
	requirePrivateKeyFile(t, filepath.Join(b, "wallet"))
	for _, d := range []string{a, b} {
		entries, err := os.ReadDir(d)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range entries {
			if strings.HasPrefix(e.Name(), ".") {
				t.Fatalf("%s holds the temporary file %s after Write", d, e.Name())
			}
		}
	}
}

// keyFileDir keeps the directory as Path spells it and never turns a
// relative path into the drive root.
func TestKeyFileDir(t *testing.T) {
	cases := []struct{ path, want string }{
		{"wallet", "."},
		{"dir/wallet", "dir/"},
		{"/abs/dir/wallet", "/abs/dir/"},
		{"A/link/../wallet", "A/link/../"},
	}
	if runtime.GOOS == "windows" {
		cases = []struct{ path, want string }{
			{`wallet`, `.`},
			{`dir\wallet`, `dir\`},
			{`C:wallet`, `C:.`},
			{`C:\wallet`, `C:\`},
			{`C:\dir\wallet`, `C:\dir\`},
			{`\\server\share\wallet`, `\\server\share\`},
		}
	}
	for _, c := range cases {
		if got := keyFileDir(c.path); got != c.want {
			t.Errorf("keyFileDir(%q) = %q, want %q", c.path, got, c.want)
		}
	}
}

// The base address in a key file is in the clear. Decrypt derives the
// address from the decrypted seed, so a file that advertises another one
// has been edited, and must not open as if it were what it claims to be.
func TestKeyFileDecryptRejectsAnotherBaseAddress(t *testing.T) {
	ks := testKeyStore(t)
	kf := encryptedKeyFile(t, ks, "")
	kf.BaseAddress = otherKeyStore(t).BaseAddress

	if got, err := kf.Decrypt(testPassword); !errors.Is(err, ErrKeyFileAddressMismatch) || got != nil {
		t.Fatalf("Decrypt of a file advertising another address returned a store: %v, error %v; want no store and ErrKeyFileAddressMismatch", got != nil, err)
	}

	// The wrong password is still reported as such, before any address check.
	if _, err := kf.Decrypt("not the password"); !errors.Is(err, ErrWrongPassword) {
		t.Fatalf("Decrypt with the wrong password = %v, want ErrWrongPassword", err)
	}

	// A file that does not advertise an address has nothing to contradict.
	kf.BaseAddress = types.Address{}
	got, err := kf.Decrypt(testPassword)
	if err != nil {
		t.Fatalf("Decrypt of a file without a base address: %v", err)
	}
	if got.BaseAddress != ks.BaseAddress {
		t.Fatalf("Decrypt derived %v, want %v", got.BaseAddress, ks.BaseAddress)
	}
}

// The manager unlocks through Decrypt, so an edited file is refused there.
func TestUnlockRejectsAnotherBaseAddress(t *testing.T) {
	m := New(&Config{WalletDir: t.TempDir()})
	if err := m.Start(); err != nil {
		t.Fatal(err)
	}
	kf := encryptedKeyFile(t, testKeyStore(t), filepath.Join(m.config.WalletDir, "edited"))
	kf.BaseAddress = otherKeyStore(t).BaseAddress
	if err := kf.Write(); err != nil {
		t.Fatal(err)
	}
	if err := m.Start(); err != nil {
		t.Fatal(err)
	}
	if err := m.Unlock("edited", testPassword); !errors.Is(err, ErrKeyFileAddressMismatch) {
		t.Fatalf("Unlock of an edited file = %v, want ErrKeyFileAddressMismatch", err)
	}
	if unlocked, err := m.IsUnlocked("edited"); err != nil || unlocked {
		t.Fatalf("IsUnlocked after a refused Unlock = %v, %v; want false, nil", unlocked, err)
	}
}
