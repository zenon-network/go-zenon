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
