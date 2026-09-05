package app

import (
	"encoding/json"
	"flag"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/urfave/cli/v2"
)

const sentinel = "sentinel-producer-password-7f3a"

// MakeConfig prints the effective configuration to stdout and logs it; a
// configured producer password must reach neither sink, while the rest of
// the configuration still does.
func TestMakeConfigDoesNotRevealProducerPassword(t *testing.T) {
	dataPath := t.TempDir()
	cfgPath := filepath.Join(dataPath, "config.json")
	file := map[string]interface{}{
		"DataPath": dataPath,
		"Producer": map[string]interface{}{
			"Address":     "z1qqjnwjjpnue8xmmpanz6csze6tcmtzzdtfsww7",
			"Index":       0,
			"KeyFilePath": "producer",
			"Password":    sentinel,
		},
	}
	raw, err := json.Marshal(file)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfgPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}

	set := flag.NewFlagSet("znnd", flag.ContinueOnError)
	ConfigFileFlag.Apply(set)
	if err := set.Parse([]string{"--" + ConfigFileFlag.Name, cfgPath}); err != nil {
		t.Fatal(err)
	}
	ctx := cli.NewContext(cli.NewApp(), set, nil)

	stdout := captureStdout(t)
	cfg, err := MakeConfig(ctx)
	out := stdout()
	if err != nil {
		t.Fatalf("MakeConfig: %v", err)
	}

	if !strings.Contains(out, "Using the following znnd config") {
		t.Fatalf("configuration was not printed: %q", out)
	}
	if strings.Contains(out, sentinel) {
		t.Fatalf("stdout reveals the producer password:\n%s", out)
	}
	if !strings.Contains(out, `"KeyFilePath"`) {
		t.Fatalf("stdout lost the producer section:\n%s", out)
	}

	logs := readLogs(t, dataPath)
	if !strings.Contains(logs, "using znnd config") {
		t.Fatalf("configuration was not logged:\n%s", logs)
	}
	if strings.Contains(logs, sentinel) {
		t.Fatalf("log file reveals the producer password:\n%s", logs)
	}

	// The value is still available for the unlock.
	if cfg.Producer == nil || string(cfg.Producer.Password) != sentinel {
		t.Fatalf("configured password was lost: %+v", cfg.Producer)
	}
}

func captureStdout(t *testing.T) func() string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdout
	os.Stdout = w
	done := make(chan string)
	go func() {
		b, _ := io.ReadAll(r)
		done <- string(b)
	}()
	return func() string {
		os.Stdout = old
		w.Close()
		return <-done
	}
}

func readLogs(t *testing.T, dataPath string) string {
	t.Helper()
	var all strings.Builder
	err := filepath.Walk(filepath.Join(dataPath, "log"), func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		all.Write(b)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return all.String()
}
