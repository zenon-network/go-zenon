package node

import (
	"os"
	"path/filepath"
	"testing"
)

// TestMakeGenesisConfig_InvalidFile_ReturnsError verifies that a
// nonexistent genesis file produces an error from makeGenesisConfig
// rather than exiting the process. Before the fix, this path called
// os.Exit(1), which would kill the test runner.
func TestMakeGenesisConfig_InvalidFile_ReturnsError(t *testing.T) {
	cfg := &Config{
		GenesisFile: filepath.Join(t.TempDir(), "nonexistent-genesis.json"),
	}

	_, err := cfg.makeGenesisConfig()
	if err == nil {
		t.Fatal("expected an error for a nonexistent genesis file, got nil")
	}
}

// TestMakeGenesisConfig_EmptyPath_UsesEmbedded verifies that an empty
// GenesisFile path falls through to the embedded genesis, which is
// available in the test binary. This exercises the happy path of the
// refactored function: no os.Exit on success either.
func TestMakeGenesisConfig_EmptyPath_UsesEmbedded(t *testing.T) {
	cfg := &Config{
		GenesisFile: "",
	}

	genesisConfig, err := cfg.makeGenesisConfig()
	if err != nil {
		t.Fatalf("expected embedded genesis to load, got error: %v", err)
	}
	if genesisConfig == nil {
		t.Fatal("expected non-nil genesis config")
	}
}

// TestMakeGenesisConfig_ValidFile_ReturnsGenesis verifies that a valid
// genesis file loads without error. We reuse the embedded genesis by
// writing it out — but since we can't easily extract it, we just
// verify the error path for a malformed file.
func TestMakeGenesisConfig_MalformedFile_ReturnsError(t *testing.T) {
	dir := t.TempDir()
	genesisPath := filepath.Join(dir, "genesis.json")

	if err := os.WriteFile(genesisPath, []byte(`{"invalid": true}`), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg := &Config{
		GenesisFile: genesisPath,
	}

	_, err := cfg.makeGenesisConfig()
	if err == nil {
		t.Fatal("expected an error for a malformed genesis file, got nil")
	}
}
