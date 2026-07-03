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

// KeyStore is the decrypted, in-memory form of a wallet: the BIP-39
// Entropy, its Mnemonic, the derived Seed, and the BaseAddress (the
// account at index 0). All accounts are derived deterministically from
// Seed, so the struct holds the full secret of the wallet and should be
// zeroed (see Zero) once no longer needed.
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

// Zero drops the key store's references to its secret material — the
// entropy and seed slices are set to nil, the mnemonic to "" and the
// base address reset — and is called when a wallet is locked or the
// manager stops. Note this only makes the secrets eligible for garbage
// collection; it does not overwrite the underlying entropy or seed
// bytes, and the discarded string cannot be scrubbed, so the material
// may persist in memory until it is collected.
func (ks *KeyStore) Zero() {
	ks.Entropy = nil
	ks.Seed = nil
	ks.Mnemonic = ""
	ks.BaseAddress = types.ZeroAddress
}

// DeriveForFullPath derives the key pair for the explicit derivation
// path ipath from the store's seed. The returned path string is
// currently always empty; callers needing the path should use the value
// they passed in.
func (ks *KeyStore) DeriveForFullPath(ipath string) (path string, key *KeyPair, err error) {
	key, err = DeriveForPath(ipath, ks.Seed)
	if err != nil {
		return "", nil, err
	}
	return path, key, nil
}

// DeriveForIndexPath derives the key pair for the account at the given
// index, using the standard path m/44'/73404'/index'. The returned path
// string carries the same caveat as DeriveForFullPath.
func (ks *KeyStore) DeriveForIndexPath(index uint32) (path string, key *KeyPair, err error) {
	return ks.DeriveForFullPath(fmt.Sprintf(ZenonAccountPathFormat, index))
}

// FindAddress searches the first maxSearchIndex account indices for the
// one that derives to address, returning its key pair and index. It
// returns ErrAddressNotFound when no index in range matches.
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

// Encrypt produces a password-protected KeyFile for the store. It
// stretches password with Argon2id over a fresh random salt, seals the
// BIP-39 entropy with AES-256-GCM under that key, and records the
// cipher, KDF, salt, nonce, base address, and a UTC creation timestamp.
// The returned KeyFile has no Path set; the caller assigns one before
// writing it.
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
