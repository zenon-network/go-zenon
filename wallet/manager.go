package wallet

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/zenon-network/go-zenon/common"
)

const (
	DefaultMaxIndex = 128
)

type Config struct {
	WalletDir      string
	MaxSearchIndex uint32
}

type Manager struct {
	config *Config
	log    common.Logger

	encrypted map[string]*KeyFile  // map from path to
	decrypted map[string]*KeyStore // map from path to
}

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
func (m *Manager) Stop() {
	for _, ks := range m.decrypted {
		ks.Zero()
	}
	m.decrypted = nil
	m.encrypted = nil
}

func (m *Manager) MakePathAbsolut(path string) string {
	if filepath.IsAbs(path) {
		return path
	} else {
		return filepath.Join(m.config.WalletDir, path)
	}
}

func (m *Manager) GetKeyFile(path string) (*KeyFile, error) {
	path = m.MakePathAbsolut(path)
	if kf, ok := m.encrypted[path]; ok == false {
		return nil, ErrKeyStoreNotFound
	} else {
		return kf, nil
	}
}
func (m *Manager) GetKeyStore(path string) (*KeyStore, error) {
	path = m.MakePathAbsolut(path)
	if _, ok := m.encrypted[path]; ok == false {
		return nil, ErrKeyStoreNotFound
	} else if ks, ok := m.decrypted[path]; ok == false {
		return nil, ErrKeyStoreLocked
	} else {
		return ks, nil
	}
}
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
	path = m.MakePathAbsolut(path)
	kf, err := m.GetKeyFile(path)
	if err != nil {
		return err
	}
	ks, err := kf.Decrypt(password)
	if err != nil {
		return err
	}

	m.encrypted[path] = kf
	m.decrypted[path] = ks
	return nil
}
func (m *Manager) Lock(path string) {
	path = m.MakePathAbsolut(path)
	if ks, ok := m.decrypted[path]; ok == true {
		ks.Zero()
		m.decrypted[path] = nil
	}
}
func (m *Manager) IsUnlocked(path string) (bool, error) {
	path = m.MakePathAbsolut(path)
	if _, ok := m.encrypted[path]; ok == false {
		return false, ErrKeyStoreNotFound
	}
	_, ok := m.decrypted[path]
	return ok, nil
}
