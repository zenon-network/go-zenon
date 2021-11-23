package wallet

import (
	"fmt"
	"time"

	"github.com/tyler-smith/go-bip39"

	"github.com/zenon-network/go-zenon/common/types"
)

const (
	maxSearchIndex = 128
)

type KeyStore struct {
	Entropy  []byte
	Seed     []byte
	Mnemonic string

	BaseAddress types.Address
}

func keyStoreFromEntropy(entropy []byte) (*KeyStore, error) {
	mnemonic, err := bip39.NewMnemonic(entropy)
	if err != nil {
		return nil, err
	}

	ks := &KeyStore{
		Entropy:  entropy,
		Seed:     bip39.NewSeed(mnemonic, ""),
		Mnemonic: mnemonic,
	}

	// setup base address
	if _, kp, err := ks.DeriveForIndexPath(0); err == nil {
		ks.BaseAddress = kp.Address
	} else {
		return nil, err
	}

	return ks, nil
}

func (ks *KeyStore) Zero() {
	ks.Entropy = nil
	ks.Seed = nil
	ks.Mnemonic = ""
	ks.BaseAddress = types.ZeroAddress
}

func (ks *KeyStore) DeriveForFullPath(ipath string) (path string, key *KeyPair, err error) {
	key, err = DeriveForPath(ipath, ks.Seed)
	if err != nil {
		return "", nil, err
	}
	return path, key, nil
}
func (ks *KeyStore) DeriveForIndexPath(index uint32) (path string, key *KeyPair, err error) {
	return ks.DeriveForFullPath(fmt.Sprintf(ZenonAccountPathFormat, index))
}

func (ks *KeyStore) FindAddress(address types.Address) (key *KeyPair, index uint32, err error) {
	for index = uint32(0); index < maxSearchIndex; index++ {
		_, key, err = ks.DeriveForIndexPath(index)
		if err != nil {
			return nil, 0, err
		}
		if address == key.Address {
			return
		}
	}
	return nil, 0, ErrAddressNotFound
}

func (ks *KeyStore) Encrypt(password string) (*KeyFile, error) {
	derivedKey := new(passwordHash)
	err := derivedKey.Set(password)
	if err != nil {
		return nil, err
	}

	cipherData, nonce, err := aesGCMEncrypt(derivedKey.password[:], ks.Entropy)
	if err != nil {
		return nil, err
	}

	return &KeyFile{
		BaseAddress: ks.BaseAddress,
		Crypto: cryptoParams{
			CipherName: aesMode,
			KDF:        argonName,
			CipherData: cipherData,
			AesNonce:   nonce,
			Argon2Params: argon2Params{
				Salt: derivedKey.salt,
			},
		},
		Version:   cryptoStoreVersion,
		Timestamp: time.Now().UTC().Unix(),
	}, nil
}
