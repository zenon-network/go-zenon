// Package wallet manages hierarchical-deterministic key stores for
// Network of Momentum accounts and the encrypted files that persist
// them on disk.
//
// A KeyStore holds a BIP-39 mnemonic, its entropy, and the derived
// seed. Account key pairs are derived from that seed along the BIP-44
// path m/44'/73404'/index' (coin type 73404) using SLIP-0010 ed25519
// derivation, which is hardened-only. Each KeyPair signs with ed25519
// and its address is types.PubKeyToAddress of the public key.
//
// On disk a key store is serialized as a KeyFile: the BIP-39 entropy
// is encrypted with AES-256-GCM under a key stretched from the user
// password by Argon2id, with a random per-file salt and GCM nonce. The
// Manager owns the wallet directory, lists and reads KeyFiles, and
// tracks which stores are currently unlocked (decrypted) in memory.
package wallet

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ed25519"
	"crypto/rand"
	"io"

	"github.com/pkg/errors"
)

const (
	gcmAdditionData = "zenon"
)

func aesGCMEncrypt(key, inText []byte) (outText, nonce []byte, err error) {
	aesBlock, err := aes.NewCipher(key)
	if err != nil {
		return nil, nil, err
	}
	stream, err := cipher.NewGCM(aesBlock)
	if err != nil {
		return nil, nil, err
	}

	nonce = GetEntropyCSPRNG(12)

	outText = stream.Seal(nil, nonce, inText, []byte(gcmAdditionData))
	return outText, nonce, err
}
func aesGCMDecrypt(key, cipherText, nonce []byte) ([]byte, error) {
	aesBlock, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	stream, err := cipher.NewGCM(aesBlock)
	if err != nil {
		return nil, err
	}

	outText, err := stream.Open(nil, nonce, cipherText, []byte(gcmAdditionData))
	if err != nil {
		return nil, err
	}

	return outText, err
}

// GetEntropyCSPRNG returns n bytes read from the operating system's
// cryptographically secure random source (crypto/rand). It is used to
// generate salts, GCM nonces, and proof-of-work search seeds. It panics
// if the random source cannot be read, since proceeding with weak
// randomness would be unsafe.
func GetEntropyCSPRNG(n int) []byte {
	mainBuff := make([]byte, n)
	_, err := io.ReadFull(rand.Reader, mainBuff)
	if err != nil {
		panic("reading from crypto/rand failed: " + err.Error())
	}
	return mainBuff
}

// VerifySignature reports whether sig is a valid ed25519 signature of
// message under pubkey. It returns an error if pubkey is not of the
// ed25519 public-key size, and otherwise returns the verification
// result with a nil error.
func VerifySignature(pubkey ed25519.PublicKey, message, sig []byte) (bool, error) {
	if len(pubkey) != ed25519.PublicKeySize {
		return false, errors.Errorf("ed25519: bad public key length; length=%v", len(pubkey))
	}
	return ed25519.Verify(pubkey, message, sig), nil
}
