package wallet

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/zenon-network/go-zenon/common"
)

func TestAESGCMDecrypt_MalformedNonceIsAnError(t *testing.T) {
	key := bytes.Repeat([]byte{0x11}, 32)
	cipherText, nonce, err := aesGCMEncrypt(key, []byte("plain"))
	common.FailIfErr(t, err)

	out, err := aesGCMDecrypt(key, cipherText, nonce)
	common.FailIfErr(t, err)
	common.Expect(t, string(out), "plain")

	for _, n := range []int{0, 11, 13} {
		bad := make([]byte, n)
		if _, err := aesGCMDecrypt(key, cipherText, bad); err == nil {
			t.Fatalf("aesGCMDecrypt accepted a %d-byte nonce", n)
		}
	}
}

func TestKeyFileDecrypt_MalformedNonceIsAnError(t *testing.T) {
	ks := testKeyStore(t)
	kf, err := ks.Encrypt(testPassword)
	common.FailIfErr(t, err)

	for _, n := range []int{0, 11, 13} {
		bad := *kf
		bad.Crypto.AesNonce = make([]byte, n)
		_, err := bad.Decrypt(testPassword)
		common.ExpectError(t, err, ErrKeyFileInvalidNonce)
	}

	// The shape check must not change the two normal outcomes.
	_, err = kf.Decrypt("not the password")
	common.ExpectError(t, err, ErrWrongPassword)
	got, err := kf.Decrypt(testPassword)
	common.FailIfErr(t, err)
	common.Expect(t, got.BaseAddress, ks.BaseAddress)
}

func TestReadKeyFile_MalformedNonceIsAnError(t *testing.T) {
	ks := testKeyStore(t)
	kf, err := ks.Encrypt(testPassword)
	common.FailIfErr(t, err)
	dir := t.TempDir()

	good := *kf
	good.Path = filepath.Join(dir, "good")
	common.FailIfErr(t, good.Write())
	if _, err := ReadKeyFile(good.Path); err != nil {
		t.Fatalf("ReadKeyFile on a well-formed file: %v", err)
	}

	bad := *kf
	bad.Crypto.AesNonce = make([]byte, 11)
	data, err := json.Marshal(bad)
	common.FailIfErr(t, err)
	badPath := filepath.Join(dir, "bad")
	common.FailIfErr(t, os.WriteFile(badPath, data, 0o600))
	_, err = ReadKeyFile(badPath)
	common.ExpectError(t, err, ErrKeyFileInvalidNonce)
}

func TestEncrypt_RefusesAKeyStoreWithoutEntropy(t *testing.T) {
	ks := testKeyStore(t)
	ks.Zero()
	if kf, err := ks.Encrypt(testPassword); err == nil {
		t.Fatalf("Encrypt produced a key file from a zeroed key store (cipherData %d bytes); it could never be decrypted into a key store", len(kf.Crypto.CipherData))
	} else {
		common.ExpectError(t, err, ErrKeyStoreNoEntropy)
	}

	corrupt := &KeyStore{Entropy: []byte{1, 2, 3}}
	_, err := corrupt.Encrypt(testPassword)
	common.ExpectError(t, err, ErrKeyStoreNoEntropy)
}
