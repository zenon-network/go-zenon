package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/tyler-smith/go-bip39"

	"github.com/zenon-network/go-zenon/wallet"
)

// testKeyStore builds the devnet keystore from the fixed dev mnemonic so
// the test does not depend on external files.
func testKeyStore(t *testing.T) *wallet.KeyStore {
	t.Helper()
	entropy, err := bip39.EntropyFromMnemonic(devMnemonic)
	if err != nil {
		t.Fatal(err)
	}
	ks, err := keystoreFromEntropy(entropy)
	if err != nil {
		t.Fatal(err)
	}
	return ks
}

// TestWriteKeystoreRoundtrip calls writeKeystore directly, loads its
// output through the wallet manager, unlocks it and checks the identity.
// It covers both the current output (Path set to "" by Encrypt, then
// overwritten by the caller) and the legacy container path
// (/root/.znn/wallet/<address>) that existing files carry, since
// existing files are kept unless regenerated.
func TestWriteKeystoreRoundtrip(t *testing.T) {
	ks := testKeyStore(t)
	wantAddr := ks.BaseAddress

	cases := []struct {
		name string
		path string // serialized Path to write into the JSON
	}{
		{"current-empty-path", ""},
		{"legacy-container-path", "/root/.znn/wallet/" + wantAddr.String()},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			dst := filepath.Join(dir, wantAddr.String())

			if err := writeKeystore(ks, wantAddr, dst); err != nil {
				t.Fatalf("writeKeystore: %v", err)
			}

			// Overwrite the serialized Path to simulate the legacy shape.
			if tc.path != "" {
				data, err := os.ReadFile(dst)
				if err != nil {
					t.Fatal(err)
				}
				var raw map[string]json.RawMessage
				if err := json.Unmarshal(data, &raw); err != nil {
					t.Fatal(err)
				}
				pathJSON, _ := json.Marshal(tc.path)
				raw["Path"] = pathJSON
				data, err = json.MarshalIndent(raw, "", "    ")
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(dst, data, 0o600); err != nil {
					t.Fatal(err)
				}
			}

			// Load through the wallet manager and unlock.
			m := wallet.New(&wallet.Config{WalletDir: dir})
			if err := m.Start(); err != nil {
				t.Fatal(err)
			}

			got, err := m.GetKeyFileAndDecrypt(dst, devPassword)
			if err != nil {
				t.Fatalf("GetKeyFileAndDecrypt: %v", err)
			}
			if got.BaseAddress != wantAddr {
				t.Fatalf("BaseAddress = %s, want %s", got.BaseAddress, wantAddr)
			}
		})
	}
}
