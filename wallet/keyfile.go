package wallet

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"syscall"

	"github.com/ethereum/go-ethereum/common/hexutil"

	"github.com/zenon-network/go-zenon/common/types"
)

const (
	cryptoStoreVersion = 1
	aesMode            = "aes-256-gcm"
	argonName          = "argon2.IDKey"
)

// KeyFile is the on-disk, password-protected form of a KeyStore. It
// records the base address in the clear for identification and stores
// the BIP-39 entropy encrypted under Crypto. Path is the file path it
// was read from or will be written to, as given (not made absolute).
// It is serialized under the JSON key "Path" for informational
// purposes only; ReadKeyFile always sets it to the scanned path.
type KeyFile struct {
	Path string

	BaseAddress types.Address `json:"baseAddress"`
	Crypto      cryptoParams  `json:"crypto"`
	Version     int           `json:"version"`
	Timestamp   int64         `json:"timestamp"`
}

type cryptoParams struct {
	// Constants
	CipherName string `json:"cipherName"`
	KDF        string `json:"kdf"`
	// Data
	CipherData   hexutil.Bytes `json:"cipherData"`
	AesNonce     hexutil.Bytes `json:"nonce"`
	Argon2Params argon2Params  `json:"argon2Params"`
}

type argon2Params struct {
	Salt hexutil.Bytes `json:"salt"`
}

// ReadKeyFile reads and JSON-decodes the key file at path, recording
// path on the returned KeyFile. A "Path" value inside the file is
// ignored: the caller-selected path is authoritative for lookup, unlock,
// lock and write-back. It rejects files whose version, cipher, KDF, or
// nonce length do not match what this build supports, returning
// ErrKeyFileInvalidVersion, ErrKeyFileInvalidCipher, ErrKeyFileInvalidKDF,
// or ErrKeyFileInvalidNonce respectively. It does not decrypt the
// contents.
func ReadKeyFile(path string) (*KeyFile, error) {
	keyFileJson, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	k := &KeyFile{}
	// parse and check entropyJSON params
	if err := json.Unmarshal(keyFileJson, k); err != nil {
		return nil, err
	}
	// The scanned path always wins over any serialized value.
	k.Path = path
	if k.Version != cryptoStoreVersion {
		return nil, ErrKeyFileInvalidVersion
	}

	// parse and check  cryptoParams params
	if k.Crypto.CipherName != aesMode {
		return nil, ErrKeyFileInvalidCipher
	}
	if k.Crypto.KDF != argonName {
		return nil, ErrKeyFileInvalidKDF
	}
	if len(k.Crypto.AesNonce) != gcmNonceSize {
		return nil, ErrKeyFileInvalidNonce
	}
	return k, nil
}

// Write serializes the key file as indented JSON to its Path. It writes a
// new file with mode 0600 in the same directory, syncs it and renames it
// over Path. The key file therefore ends up private whatever the mode of a
// file it replaces, a symlink at Path is replaced rather than followed, and
// a reader sees either the old file or the new one, never a partial write.
// The temporary name is short and starts with a dot, so it fits wherever
// Path's own name does and a leftover from a crash is skipped by the
// manager's scan. The directory is taken from Path without cleaning it,
// so the temporary file and Path resolve through the same directory even
// when Path runs through a symlink and "..".
//
// The new file is created fresh, so it carries the permissions a new file
// in that directory gets: mode 0600 plus whatever ACL the directory passes
// on to new files. A directory whose inherited ACL grants others access
// exposes every key file created in it, with or without this change.
//
// After the rename Write syncs the directory, so that the new name survives
// a crash as well as the contents. If that fails, the key file is already
// in place at Path but may be lost or rolled back by a crash, and Write
// returns an error wrapping ErrKeyFileNotDurable; writing again is safe,
// though it fails the same way until the cause is fixed. How durable the
// rename is depends on the filesystem: directory syncing is skipped on
// Windows, and a filesystem that reports it cannot sync a directory (such
// as Linux 9p mounts using 9p2000 or 9p2000.u, or some FUSE filesystems) is
// not treated as a failure.
func (kf *KeyFile) Write() error {
	keyFileJson, err := json.MarshalIndent(kf, "", "    ")
	if err != nil {
		return err
	}
	dir := keyFileDir(kf.Path)
	tmp, err := os.CreateTemp(dir, ".keyfile-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer func() {
		if tmpPath != "" {
			_ = os.Remove(tmpPath)
		}
	}()
	if _, err := tmp.Write(keyFileJson); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, kf.Path); err != nil {
		return err
	}
	tmpPath = ""
	if err := syncDir(dir); err != nil {
		return fmt.Errorf("%w: %s: %w", ErrKeyFileNotDurable, kf.Path, err)
	}
	return nil
}

// keyFileDir returns the directory path lives in, as path spells it: it is
// not cleaned, so "A/link/../wallet" gives "A/link/../", which the kernel
// resolves exactly as it resolves path. A path with no directory part
// gives ".", and on Windows a drive-relative path ("C:wallet") gives "C:.",
// the current directory on that drive, not the drive's root.
func keyFileDir(path string) string {
	dir, _ := filepath.Split(path)
	if dir == "" || dir == filepath.VolumeName(path) {
		dir += "."
	}
	return dir
}

// syncDir makes a rename in dir durable. It skips Windows, where a
// directory cannot be opened for syncing this way, and returns nil when the
// filesystem reports that it does not support syncing a directory. A
// failure to open the directory or any other sync error is returned.
func syncDir(dir string) error {
	if runtime.GOOS == "windows" {
		return nil
	}
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	err = d.Sync()
	_ = d.Close()
	if dirSyncUnsupported(err) {
		return nil
	}
	return err
}

// dirSyncUnsupported reports whether err from syncing a directory means
// the filesystem cannot sync directories at all, rather than that a sync
// failed: EINVAL (for example Linux 9p mounts using 9p2000 or 9p2000.u,
// whose directories have no fsync) and ENOTSUP, EOPNOTSUPP or ENOSYS (for
// example FUSE filesystems without a directory-sync handler).
func dirSyncUnsupported(err error) bool {
	return errors.Is(err, syscall.EINVAL) || errors.Is(err, errors.ErrUnsupported)
}

// Decrypt recovers the KeyStore from the key file using password. It
// re-derives the Argon2id key from password and the stored salt, opens
// the AES-256-GCM ciphertext to recover the BIP-39 entropy, and rebuilds
// the key store (mnemonic, seed, and base address) from it. It returns
// ErrWrongPassword when the password does not authenticate the
// ciphertext, and ErrKeyFileInvalidNonce, before any key derivation,
// when the stored nonce is not the length the cipher expects. A file that
// advertises a base address other than the one its seed derives returns
// ErrKeyFileAddressMismatch; one that advertises none (the zero address)
// is accepted.
func (kf *KeyFile) Decrypt(password string) (*KeyStore, error) {
	if len(kf.Crypto.AesNonce) != gcmNonceSize {
		return nil, ErrKeyFileInvalidNonce
	}
	derivedKey := new(passwordHash)
	err := derivedKey.SetFromJSON(password, kf.Crypto.Argon2Params)
	if err != nil {
		return nil, err
	}

	entropy, err := aesGCMDecrypt(derivedKey.password[:32], kf.Crypto.CipherData, kf.Crypto.AesNonce)
	if err != nil {
		return nil, ErrWrongPassword
	}

	ks, err := keyStoreFromEntropy(entropy)
	if err != nil {
		return nil, err
	}
	if kf.BaseAddress != (types.Address{}) && kf.BaseAddress != ks.BaseAddress {
		ks.Zero()
		return nil, ErrKeyFileAddressMismatch
	}
	return ks, nil
}
