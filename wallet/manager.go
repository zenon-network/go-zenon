package wallet

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/zenon-network/go-zenon/common"
)

const (
	// DefaultMaxIndex fills Config.MaxSearchIndex when it is unset; the
	// field is not consulted by the address search (see Config).
	DefaultMaxIndex = 128
)

// Config configures a wallet Manager: WalletDir is the directory key
// files live in. MaxSearchIndex is retained for configuration
// compatibility but is not consulted; address searches use a fixed
// internal bound.
type Config struct {
	WalletDir      string
	MaxSearchIndex uint32
}

// Manager owns the wallet directory and the set of key stores known to
// the node. It tracks every key file found on disk (encrypted) and the
// subset currently unlocked in memory (decrypted). The maps are keyed
// by the scanned path (the path passed to ReadKeyFile during Start),
// which is the path lookups resolve to through MakePathAbsolut. With an
// absolute WalletDir, a relative stored Path in the JSON is ignored.
// Manager is not safe for concurrent use.
type Manager struct {
	config *Config
	log    common.Logger

	encrypted map[string]*KeyFile  // map from path to
	decrypted map[string]*KeyStore // map from path to
}

// New returns a Manager for the given configuration, defaulting
// MaxSearchIndex to DefaultMaxIndex when unset. It returns nil if config
// is nil. Start must be called before the manager is used.
func New(config *Config) *Manager {
	if config == nil {
		return nil
	}

	if config.MaxSearchIndex == 0 {
		config.MaxSearchIndex = DefaultMaxIndex
	}

	return &Manager{
		config:    config,
		encrypted: make(map[string]*KeyFile),
		decrypted: make(map[string]*KeyStore),
		log:       common.WalletLogger,
	}
}

// Start ensures the wallet directory exists and loads every key file in
// it into the encrypted set. It does not decrypt any store. It returns
// an error if the directory cannot be created or listed.
func (m *Manager) Start() error {
	// ensure WalletDir exists
	if err := os.MkdirAll(m.config.WalletDir, 0700); err != nil {
		return err
	}
	m.log.Info("successfully ensured WalletDir exists", "wallet-dir-path", m.config.WalletDir)

	m.encrypted = make(map[string]*KeyFile)
	keyFiles, err := m.ListEntropyFilesInStandardDir()
	if err != nil {
		m.log.Error("wallet start err", "err", err)
		return err
	}
	for _, keyFile := range keyFiles {
		m.encrypted[keyFile.Path] = keyFile
	}
	return nil
}

// Stop zeroes every unlocked key store (see KeyStore.Zero) and drops
// the manager's references to all known key files. Nil entries are
// skipped, so it is safe whatever sequence of Unlock and Lock calls
// preceded it.
func (m *Manager) Stop() {
	for _, ks := range m.decrypted {
		if ks != nil {
			ks.Zero()
		}
	}
	m.decrypted = nil
	m.encrypted = nil
}

// MakePathAbsolut resolves path against the wallet directory: it
// returns path unchanged if already absolute, otherwise joins it under
// WalletDir. Lookups use the result as the map key (see Manager).
func (m *Manager) MakePathAbsolut(path string) string {
	if filepath.IsAbs(path) {
		return path
	} else {
		return filepath.Join(m.config.WalletDir, path)
	}
}

// GetKeyFile returns the known key file at path, resolving path against
// the wallet directory first. It returns ErrKeyStoreNotFound when no
// such file is known to the manager.
func (m *Manager) GetKeyFile(path string) (*KeyFile, error) {
	path = m.MakePathAbsolut(path)
	if kf, ok := m.encrypted[path]; !ok {
		return nil, ErrKeyStoreNotFound
	} else {
		return kf, nil
	}
}

// GetKeyStore returns the unlocked key store at path. It returns
// ErrKeyStoreNotFound when no key file is known for the path and
// ErrKeyStoreLocked when the file exists but has not been unlocked.
func (m *Manager) GetKeyStore(path string) (*KeyStore, error) {
	path = m.MakePathAbsolut(path)
	if _, ok := m.encrypted[path]; !ok {
		return nil, ErrKeyStoreNotFound
	} else if ks, ok := m.decrypted[path]; !ok {
		return nil, ErrKeyStoreLocked
	} else {
		return ks, nil
	}
}

// GetKeyFileAndDecrypt looks up the key file at path and decrypts it
// with password, returning the resulting key store. Unlike Unlock it
// does not retain the decrypted store in the manager. It returns
// ErrKeyStoreNotFound for an unknown path or ErrWrongPassword for a bad
// password.
func (m *Manager) GetKeyFileAndDecrypt(path, password string) (*KeyStore, error) {
	if kf, err := m.GetKeyFile(path); err != nil {
		return nil, err
	} else {
		return kf.Decrypt(password)
	}
}

// ListEntropyFilesInStandardDir reads them from the disk
func (m *Manager) ListEntropyFilesInStandardDir() ([]*KeyFile, error) {
	filePaths, err := os.ReadDir(m.config.WalletDir)
	if err != nil {
		return nil, err
	}

	files := make([]*KeyFile, 0)
	for _, file := range filePaths {
		if file.IsDir() || file.Type() != 0 {
			continue
		}
		fn := file.Name()
		if strings.HasPrefix(fn, ".") || strings.HasSuffix(fn, "~") {
			continue
		}
		absFilePath := filepath.Join(m.config.WalletDir, file.Name())
		keyFile, _ := ReadKeyFile(absFilePath)
		if keyFile != nil {
			files = append(files, keyFile)
		}
	}

	return files, nil
}

// Unlock also adds keyFile to encrypted if not present
func (m *Manager) Unlock(path, password string) error {
	absPath := m.MakePathAbsolut(path)
	kf, err := m.GetKeyFile(path)
	if err != nil {
		return err
	}
	ks, err := kf.Decrypt(password)
	if err != nil {
		return err
	}

	m.encrypted[absPath] = kf
	m.decrypted[absPath] = ks
	return nil
}

// Lock locks the key store at path: it zeroes the decrypted secrets and
// removes the store from the manager's unlocked set, so IsUnlocked
// reports false afterwards. It is a no-op when the store is not
// currently unlocked.
func (m *Manager) Lock(path string) {
	path = m.MakePathAbsolut(path)
	if ks, ok := m.decrypted[path]; ok {
		ks.Zero()
		delete(m.decrypted, path)
	}
}

// IsUnlocked reports whether the key store at path has an entry in the
// decrypted map, i.e. it has been unlocked and not locked since. It
// returns ErrKeyStoreNotFound when no key file is known for the path.
func (m *Manager) IsUnlocked(path string) (bool, error) {
	path = m.MakePathAbsolut(path)
	if _, ok := m.encrypted[path]; !ok {
		return false, ErrKeyStoreNotFound
	}
	_, ok := m.decrypted[path]
	return ok, nil
}
