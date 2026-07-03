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

// The search must honor the given bound instead of the old package constant.
func TestFindAddressHonorsMaxIndex(t *testing.T) {
	ks := testKeyStore(t)

	_, kp, err := ks.DeriveForIndexPath(2)
	if err != nil {
		t.Fatal(err)
	}

	if _, _, err := ks.FindAddress(kp.Address, 1); err != ErrAddressNotFound {
		t.Fatalf("maxIndex=1: expected ErrAddressNotFound, got %v", err)
	}
	key, index, err := ks.FindAddress(kp.Address, DefaultMaxIndex)
	if err != nil {
		t.Fatal(err)
	}
	if index != 2 || key.Address != kp.Address {
		t.Fatalf("expected to find address at index 2, got index %d", index)
	}
}

// Zero must overwrite the secret bytes, not merely drop the references.
func TestZeroWipesSecretBytes(t *testing.T) {
	ks := testKeyStore(t)
	entropy := ks.Entropy
	seed := ks.Seed

	ks.Zero()

	for i, b := range entropy {
		if b != 0 {
			t.Fatalf("entropy byte %d not wiped", i)
		}
	}
	for i, b := range seed {
		if b != 0 {
			t.Fatalf("seed byte %d not wiped", i)
		}
	}
	if ks.Entropy != nil || ks.Seed != nil || ks.Mnemonic != "" {
		t.Fatal("references not cleared")
	}
}
