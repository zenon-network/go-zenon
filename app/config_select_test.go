package app

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/inconshreveable/log15"
	"github.com/urfave/cli/v2"

	"github.com/zenon-network/go-zenon/node"
)

// configContext parses args against the config and data flags, the way
// znnd's command line reaches MakeConfig. MakeConfig installs a global log
// handler writing under the data path, so the previous handler is put back
// when the test ends.
func configContext(t *testing.T, args ...string) *cli.Context {
	t.Helper()
	previous := log15.Root().GetHandler()
	t.Cleanup(func() { log15.Root().SetHandler(previous) })
	set := flag.NewFlagSet("znnd", flag.ContinueOnError)
	for _, f := range []cli.Flag{ConfigFileFlag, DataPathFlag} {
		if err := f.Apply(set); err != nil {
			t.Fatal(err)
		}
	}
	if err := set.Parse(args); err != nil {
		t.Fatal(err)
	}
	return cli.NewContext(cli.NewApp(), set, nil)
}

// chdir moves the process into dir for the test and back afterwards. Tests
// that use it must not run in parallel.
func chdir(t *testing.T, dir string) {
	t.Helper()
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(old) })
}

func writeConfig(t *testing.T, path, name string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"Name":"`+name+`"}`), 0o600); err != nil {
		t.Fatal(err)
	}
}

// Without --config, the config file is <data>/config.json. The flag's
// default "DataPath/config.json" is a placeholder for the help text, not a
// path to open relative to the working directory.
func TestConfig_WithoutConfigFlagIgnoresTheWorkingDirectory(t *testing.T) {
	cwd := t.TempDir()
	writeConfig(t, filepath.Join(cwd, "DataPath", "config.json"), "planted")
	chdir(t, cwd)

	dataPath := t.TempDir()
	writeConfig(t, filepath.Join(dataPath, "config.json"), "operator")

	stdout := captureStdout(t)
	cfg, err := MakeConfig(configContext(t, "--"+DataPathFlag.Name, dataPath))
	stdout()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Name != "operator" {
		t.Fatalf("config came from ./DataPath/config.json in the working directory (Name=%q), not from %s", cfg.Name, dataPath)
	}
}

// An explicit --config selects exactly that file, over a config in the data
// path.
func TestConfig_ExplicitConfigFlagWins(t *testing.T) {
	dataPath := t.TempDir()
	writeConfig(t, filepath.Join(dataPath, "config.json"), "operator")
	named := filepath.Join(t.TempDir(), "named.json")
	writeConfig(t, named, "named")

	stdout := captureStdout(t)
	cfg, err := MakeConfig(configContext(t, "--"+ConfigFileFlag.Name, named, "--"+DataPathFlag.Name, dataPath))
	stdout()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Name != "named" {
		t.Fatalf("--config %s was not used (Name=%q)", named, cfg.Name)
	}
}

// An explicit --config that cannot be read is an error, not a silent fall
// back to the data path: the operator asked for that file.
func TestConfig_ExplicitMissingConfigIsAnError(t *testing.T) {
	dataPath := t.TempDir()
	writeConfig(t, filepath.Join(dataPath, "config.json"), "operator")
	missing := filepath.Join(t.TempDir(), "missing.json")

	stdout := captureStdout(t)
	cfg, err := MakeConfig(configContext(t, "--"+ConfigFileFlag.Name, missing, "--"+DataPathFlag.Name, dataPath))
	stdout()
	if err == nil {
		t.Fatalf("--config %s does not exist, but MakeConfig returned a config (Name=%q) instead of an error", missing, cfg.Name)
	}

	// A read error other than absence, deterministically: a directory.
	dir := t.TempDir()
	stdout = captureStdout(t)
	cfg, err = MakeConfig(configContext(t, "--"+ConfigFileFlag.Name, dir, "--"+DataPathFlag.Name, dataPath))
	stdout()
	if err == nil {
		t.Fatalf("--config %s is a directory, but MakeConfig returned a config (Name=%q) instead of an error", dir, cfg.Name)
	}
}

// An empty explicit --config= is treated like no flag: the data-path file
// is used. This is the one explicit form that still falls back.
func TestConfig_EmptyExplicitConfigFallsBackToTheDataPath(t *testing.T) {
	dataPath := t.TempDir()
	writeConfig(t, filepath.Join(dataPath, "config.json"), "operator")

	stdout := captureStdout(t)
	cfg, err := MakeConfig(configContext(t, "--"+ConfigFileFlag.Name+"=", "--"+DataPathFlag.Name, dataPath))
	stdout()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Name != "operator" {
		t.Fatalf("--config= did not fall back to %s (Name=%q)", dataPath, cfg.Name)
	}
}

// Without --config, a missing <data>/config.json is only a warning and the
// defaults apply.
func TestConfig_MissingImplicitConfigIsOnlyAWarning(t *testing.T) {
	dataPath := t.TempDir()

	// The warning is logged before MakeConfig installs file logging, so
	// capture it on the root handler that is in place at that point.
	ctx := configContext(t, "--"+DataPathFlag.Name, dataPath)
	var logged bytes.Buffer
	log15.Root().SetHandler(log15.StreamHandler(&logged, log15.LogfmtFormat()))

	stdout := captureStdout(t)
	cfg, err := MakeConfig(ctx)
	stdout()
	if err != nil {
		t.Fatalf("missing %s/config.json must not be an error: %v", dataPath, err)
	}
	if cfg.Name != node.DefaultNodeConfig.Name {
		t.Fatalf("Name=%q, want the default %q", cfg.Name, node.DefaultNodeConfig.Name)
	}
	if !strings.Contains(logged.String(), "Config file missing") {
		t.Fatalf("the missing-config warning was not logged:\n%s", logged.String())
	}
}

// A malformed explicit --config is an error (as it already was).
func TestConfig_MalformedExplicitConfigIsAnError(t *testing.T) {
	named := filepath.Join(t.TempDir(), "named.json")
	if err := os.WriteFile(named, []byte(`{"Name":`), 0o600); err != nil {
		t.Fatal(err)
	}

	stdout := captureStdout(t)
	_, err := MakeConfig(configContext(t, "--"+ConfigFileFlag.Name, named))
	stdout()
	if err == nil {
		t.Fatal("malformed --config file was accepted")
	}
}

// The pprof server starts on the flag's value, not its presence.
func TestPprof_FlagValueDecides(t *testing.T) {
	cases := []struct {
		args []string
		want bool
	}{
		{nil, false},
		{[]string{"--pprof=false"}, false},
		{[]string{"--pprof"}, true},
		{[]string{"--pprof=true"}, true},
	}
	for _, c := range cases {
		set := flag.NewFlagSet("znnd", flag.ContinueOnError)
		if err := PprofFlag.Apply(set); err != nil {
			t.Fatal(err)
		}
		if err := set.Parse(c.args); err != nil {
			t.Fatal(err)
		}
		if got := pprofRequested(cli.NewContext(cli.NewApp(), set, nil)); got != c.want {
			t.Fatalf("pprofRequested(%v) = %v, want %v", c.args, got, c.want)
		}
	}
}
