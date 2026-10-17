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

// Zero scrubs the key store's secret material: the entropy and seed
// bytes are overwritten with zeros before the slices are dropped, the
// mnemonic is set to "" and the base address reset. It is called when
// a wallet is locked or the manager stops. The mnemonic string itself
// cannot be overwritten in place, so it may persist in memory until it
// is collected.
func (ks *KeyStore) Zero() {
	// overwrite the backing arrays before dropping the references; niling
	// alone leaves the secret bytes in memory until GC
	for i := range ks.Entropy {
		ks.Entropy[i] = 0
	}
	for i := range ks.Seed {
		ks.Seed[i] = 0
	}
	ks.Entropy = nil
	ks.Seed = nil
	// Go strings are immutable; the mnemonic cannot be scrubbed in place
	ks.Mnemonic = ""
	ks.BaseAddress = types.ZeroAddress
}

// DeriveForFullPath derives the key pair for the explicit derivation
// path ipath from the store's seed and returns that path alongside the
// key pair.
func (ks *KeyStore) DeriveForFullPath(ipath string) (path string, key *KeyPair, err error) {
	key, err = DeriveForPath(ipath, ks.Seed)
	if err != nil {
		return "", nil, err
	}
	return ipath, key, nil
}

// DeriveForIndexPath derives the key pair for the account at the given
// index, using the standard path m/44'/73404'/index', and returns that
// path alongside the key pair.
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
