package wallet

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/tyler-smith/go-bip39"

	"github.com/zenon-network/go-zenon/common"
)

// malformedNonceLengths are the shapes a key file must be refused for:
// absent, short by one, long by one, and far too long.
var malformedNonceLengths = []int{0, 11, 13, 64}

func TestAESGCMDecrypt_MalformedNonceIsAnError(t *testing.T) {
	key := bytes.Repeat([]byte{0x11}, 32)
	cipherText, nonce, err := aesGCMEncrypt(key, []byte("plain"))
	common.FailIfErr(t, err)

	out, err := aesGCMDecrypt(key, cipherText, nonce)
	common.FailIfErr(t, err)
	common.Expect(t, string(out), "plain")

	for _, n := range malformedNonceLengths {
		_, err := aesGCMDecrypt(key, cipherText, make([]byte, n))
		if err != ErrKeyFileInvalidNonce {
			t.Fatalf("aesGCMDecrypt with a %d-byte nonce: got %v, want ErrKeyFileInvalidNonce", n, err)
		}
	}
}

func TestKeyFileDecrypt_MalformedNonceIsAnError(t *testing.T) {
	ks := testKeyStore(t)
	kf, err := ks.Encrypt(testPassword)
	common.FailIfErr(t, err)

	for _, n := range malformedNonceLengths {
		bad := *kf
		bad.Crypto.AesNonce = make([]byte, n)
		// The shape check runs before the Argon2 derivation, which
		// allocates 64 MiB; a refused file must not pay for it.
		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)
		_, err := bad.Decrypt(testPassword)
		runtime.ReadMemStats(&after)
		common.ExpectError(t, err, ErrKeyFileInvalidNonce)
		if allocated := after.TotalAlloc - before.TotalAlloc; allocated > 1<<20 {
			t.Fatalf("Decrypt with a %d-byte nonce allocated %d bytes; the key derivation ran before the nonce was checked", n, allocated)
		}
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

	for _, n := range malformedNonceLengths {
		bad := *kf
		bad.Crypto.AesNonce = make([]byte, n)
		data, err := json.Marshal(bad)
		common.FailIfErr(t, err)
		badPath := filepath.Join(dir, "bad")
		common.FailIfErr(t, os.WriteFile(badPath, data, 0o600))
		_, err = ReadKeyFile(badPath)
		common.ExpectError(t, err, ErrKeyFileInvalidNonce)
	}

	// A file with no nonce field at all decodes to an empty nonce.
	var raw map[string]json.RawMessage
	data, err := json.Marshal(kf)
	common.FailIfErr(t, err)
	common.FailIfErr(t, json.Unmarshal(data, &raw))
	var crypto map[string]json.RawMessage
	common.FailIfErr(t, json.Unmarshal(raw["crypto"], &crypto))
	delete(crypto, "nonce")
	raw["crypto"], err = json.Marshal(crypto)
	common.FailIfErr(t, err)
	data, err = json.Marshal(raw)
	common.FailIfErr(t, err)
	noNonce := filepath.Join(dir, "no-nonce")
	common.FailIfErr(t, os.WriteFile(noNonce, data, 0o600))
	_, err = ReadKeyFile(noNonce)
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

	for _, entropy := range [][]byte{nil, {1, 2, 3}, make([]byte, 15), make([]byte, 33)} {
		_, err := (&KeyStore{Entropy: entropy}).Encrypt(testPassword)
		common.ExpectError(t, err, ErrKeyStoreNoEntropy)
	}

	// The smallest and largest valid BIP-39 entropy sizes still encrypt and
	// decrypt (one KDF round each way).
	for _, size := range []int{16, 32} {
		entropy := bytes.Repeat([]byte{0x5a}, size)
		valid, err := keyStoreFromEntropy(entropy)
		common.FailIfErr(t, err)
		kf, err := valid.Encrypt(testPassword)
		common.FailIfErr(t, err)
		back, err := kf.Decrypt(testPassword)
		common.FailIfErr(t, err)
		if !bytes.Equal(back.Entropy, entropy) {
			t.Fatalf("%d-byte entropy did not round-trip", size)
		}
		if _, err := bip39.NewMnemonic(back.Entropy); err != nil {
			t.Fatal(err)
		}
	}
}
