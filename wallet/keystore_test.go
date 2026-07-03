package wallet

import (
	"testing"

	"github.com/tyler-smith/go-bip39"
)

const testMnemonic = "route become dream access impulse price inform obtain engage ski believe awful absent pig thing vibrant possible exotic flee pepper marble rural fire fancy"

func testKeyStore(t *testing.T) *KeyStore {
	t.Helper()
	entropy, err := bip39.EntropyFromMnemonic(testMnemonic)
	if err != nil {
		t.Fatal(err)
	}
	ks, err := keyStoreFromEntropy(entropy)
	if err != nil {
		t.Fatal(err)
	}
	return ks
}

// The named return `path` was never assigned, so the derivation path came
// back empty alongside a valid key.
func TestDeriveForFullPathReturnsPath(t *testing.T) {
	ks := testKeyStore(t)

	ipath := "m/44'/73404'/0'"
	path, key, err := ks.DeriveForFullPath(ipath)
	if err != nil {
		t.Fatal(err)
	}
	if key == nil {
		t.Fatal("expected a derived key")
	}
	if path != ipath {
		t.Fatalf("path = %q, want %q", path, ipath)
	}
}
