package wallet

import (
	"encoding/json"
	"os"

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
// the BIP-39 entropy encrypted under Crypto. Path is the absolute file
// path it was read from or will be written to, and is not serialized.
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
// the path on the returned KeyFile. It rejects files whose version,
// cipher, or KDF do not match what this build supports, returning
// ErrKeyFileInvalidVersion, ErrKeyFileInvalidCipher, or
// ErrKeyFileInvalidKDF respectively. It does not decrypt the contents.
func ReadKeyFile(path string) (*KeyFile, error) {
	keyFileJson, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	k := &KeyFile{
		Path: path,
	}
	// parse and check entropyJSON params
	if err := json.Unmarshal(keyFileJson, k); err != nil {
		return nil, err
	}
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
	return k, nil
}
func (kf *KeyFile) Write() error {
	keyFileJson, err := json.MarshalIndent(kf, "", "    ")
	if err != nil {
		return err
	}
	return os.WriteFile(kf.Path, keyFileJson, 0700)
}

// Decrypt recovers the KeyStore from the key file using password. It
// re-derives the Argon2id key from password and the stored salt, opens
// the AES-256-GCM ciphertext to recover the BIP-39 entropy, and rebuilds
// the key store (mnemonic, seed, and base address) from it. It returns
// ErrWrongPassword when the password does not authenticate the
// ciphertext.
func (kf *KeyFile) Decrypt(password string) (*KeyStore, error) {
	derivedKey := new(passwordHash)
	err := derivedKey.SetFromJSON(password, kf.Crypto.Argon2Params)
	if err != nil {
		return nil, err
	}

	entropy, err := aesGCMDecrypt(derivedKey.password[:32], kf.Crypto.CipherData, kf.Crypto.AesNonce)
	if err != nil {
		return nil, ErrWrongPassword
	}

	return keyStoreFromEntropy(entropy)
}
