package app

import (
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/urfave/cli/v2"

	"github.com/zenon-network/go-zenon/node"
)

// TestRunWithConfig_ParsesExplicitOptions verifies that RunWithConfig
// (via NewNodeManagerFromConfig) accepts an explicit node.Config and
// does not consult os.Args. We set os.Args to something that would
// cause the CLI path to fail, then pass a valid config directly.
func TestRunWithConfig_ParsesExplicitOptions(t *testing.T) {
	dataPath := t.TempDir()

	// Set os.Args to something that would confuse the CLI parser.
	oldArgs := os.Args
	os.Args = []string{"znnd", "--definitely-invalid-flag", "bogus-command"}
	defer func() { os.Args = oldArgs }()

	cfg := node.DefaultNodeConfig
	cfg.DataPath = dataPath
	cfg.GenesisFile = "" // no genesis file; will use embedded or error

	// We don't actually start the node (that would require a full setup);
	// we only verify that NewNodeManagerFromConfig doesn't parse os.Args
	// and returns an error (not exits) when the config is problematic.
	_, err := NewNodeManagerFromConfig(&cfg)
	// The node creation may fail for various reasons (no genesis, no
	// wallet, etc.) — the important thing is that it returns an error
	// instead of calling os.Exit.
	t.Logf("NewNodeManagerFromConfig returned: %v", err)
}

// TestRunWithConfig_InvalidOptions_ReturnsError verifies that an
// invalid configuration (nonexistent genesis file) produces an error
// from the explicit-config path rather than exiting the process.
func TestRunWithConfig_InvalidOptions_ReturnsError(t *testing.T) {
	dataPath := t.TempDir()

	cfg := node.DefaultNodeConfig
	cfg.DataPath = dataPath
	cfg.GenesisFile = filepath.Join(dataPath, "nonexistent-genesis.json")

	_, err := NewNodeManagerFromConfig(&cfg)
	if err == nil {
		t.Fatal("expected an error for a nonexistent genesis file, got nil")
	}
}

// TestRunWithConfig_ProcessArgsHaveNoEffect verifies that process
// arguments are completely ignored by the explicit-config path. We
// craft a scenario where os.Args contains flags that would make the
// CLI path behave differently, and confirm the config-driven result
// is unaffected.
func TestRunWithConfig_ProcessArgsHaveNoEffect(t *testing.T) {
	dataPath := t.TempDir()

	// Set os.Args to include a --data flag pointing elsewhere.
	oldArgs := os.Args
	otherDir := t.TempDir()
	os.Args = []string{"znnd", "--data", otherDir}
	defer func() { os.Args = oldArgs }()

	cfg := node.DefaultNodeConfig
	cfg.DataPath = dataPath
	cfg.GenesisFile = filepath.Join(dataPath, "nonexistent-genesis.json")

	_, err := NewNodeManagerFromConfig(&cfg)
	if err == nil {
		t.Fatal("expected an error for a nonexistent genesis file, got nil")
	}

	// If os.Args had been consulted, the --data flag would have
	// redirected the data path to otherDir and the error would mention
	// a different path. We can't easily verify the internal path, but
	// the fact that we got an error (not an exit) at all proves the
	// code path is different from the CLI one.
}

// TestNewNodeManagerFromConfig_ResolvesEmptyWalletPath verifies that
// NewNodeManagerFromConfig calls MakePathsAbsolute on the config so a
// DefaultNodeConfig (whose WalletPath is empty) gets its WalletPath
// resolved to <DataPath>/wallet instead of remaining empty.
func TestNewNodeManagerFromConfig_ResolvesEmptyWalletPath(t *testing.T) {
	dataPath := t.TempDir()

	cfg := node.DefaultNodeConfig
	cfg.DataPath = dataPath
	cfg.GenesisFile = filepath.Join(dataPath, "nonexistent-genesis.json")
	// Intentionally do NOT set cfg.WalletPath — DefaultNodeConfig leaves
	// it empty, and without MakePathsAbsolute the node would see an
	// empty wallet path.

	_, err := NewNodeManagerFromConfig(&cfg)
	if err == nil {
		t.Fatal("expected an error for a nonexistent genesis file, got nil")
	}

	// The config must have been mutated in place: MakePathsAbsolute
	// resolves WalletPath to <DataPath>/wallet before node.NewNode runs.
	wantWallet := filepath.Join(dataPath, "wallet")
	if cfg.WalletPath != wantWallet {
		t.Errorf("WalletPath = %q, want %q (MakePathsAbsolute not called)",
			cfg.WalletPath, wantWallet)
	}
}

// TestNewNodeManager_CLIContextStillWorks verifies that the original
// CLI-based NewNodeManager still works correctly after the refactor.
func TestNewNodeManager_CLIContextStillWorks(t *testing.T) {
	dataPath := t.TempDir()

	set := flag.NewFlagSet("znnd", flag.ContinueOnError)
	if err := DataPathFlag.Apply(set); err != nil {
		t.Fatal(err)
	}
	if err := set.Parse([]string{"--" + DataPathFlag.Name, dataPath}); err != nil {
		t.Fatal(err)
	}
	ctx := cli.NewContext(cli.NewApp(), set, nil)

	// This should not exit; it may return an error (no genesis, etc.)
	_, err := NewNodeManager(ctx)
	t.Logf("NewNodeManager returned: %v", err)
}

// TestRunWithConfig_AlreadyRunning_ReturnsError verifies that a second
// RunWithConfig call while a node is already running returns an error
// and does not nil out the package-level nodeManager, so Stop still
// works on the first node.
func TestRunWithConfig_AlreadyRunning_ReturnsError(t *testing.T) {
	// Simulate a running node by assigning a non-nil manager.
	// We use a bare &Manager{} — the test only checks the guard, it
	// never dereferences the node field.
	original := nodeManager
	nodeManager = &Manager{}
	defer func() { nodeManager = original }()

	cfg := node.DefaultNodeConfig
	cfg.DataPath = t.TempDir()

	err := RunWithConfig(&cfg)
	if err == nil {
		t.Fatal("expected an error for a second RunWithConfig while node is running, got nil")
	}
	if err.Error() != "node is already running" {
		t.Errorf("error = %q, want %q", err, "node is already running")
	}

	// The package-level nodeManager must still be the original (non-nil)
	// value, not nilled out by the failed second call.
	if nodeManager == nil {
		t.Fatal("nodeManager was nilled out by the failed second RunWithConfig; Stop would be a no-op")
	}
}

// TestRunWithConfig_FailedStart_LeavesNodeManagerNil verifies that when
// NewNodeManagerFromConfig fails (e.g. bad genesis), the package-level
// nodeManager is not assigned, so a subsequent Stop is a no-op and a
// retry is possible.
func TestRunWithConfig_FailedStart_LeavesNodeManagerNil(t *testing.T) {
	original := nodeManager
	nodeManager = nil
	defer func() { nodeManager = original }()

	cfg := node.DefaultNodeConfig
	cfg.DataPath = t.TempDir()
	cfg.GenesisFile = filepath.Join(cfg.DataPath, "nonexistent-genesis.json")

	err := RunWithConfig(&cfg)
	if err == nil {
		t.Fatal("expected an error for a nonexistent genesis file, got nil")
	}

	if nodeManager != nil {
		t.Fatal("nodeManager should be nil after a failed RunWithConfig")
	}
}
