package app

import (
	"flag"
	"testing"

	"github.com/urfave/cli/v2"

	"github.com/zenon-network/go-zenon/node"
)

// applyFlags parses args against the given flags and applies them to a
// default configuration, the way MakeConfig does after reading the file.
func applyFlags(t *testing.T, flags []cli.Flag, args ...string) node.Config {
	t.Helper()
	set := flag.NewFlagSet("znnd", flag.ContinueOnError)
	for _, f := range flags {
		if err := f.Apply(set); err != nil {
			t.Fatal(err)
		}
	}
	if err := set.Parse(args); err != nil {
		t.Fatal(err)
	}
	cfg := node.DefaultNodeConfig
	applyFlagsToConfig(cli.NewContext(cli.NewApp(), set, nil), &cfg)
	return cfg
}

// Each listen flag sets the field its name and usage describe and nothing
// else: the network flags the P2P listener, the RPC flags the RPC hosts.
func TestListenFlagsMapToTheirFields(t *testing.T) {
	flags := []cli.Flag{ListenHostFlag, ListenPortFlag, RPCListenAddrFlag, RPCPortFlag, WSListenAddrFlag, WSPortFlag}
	def := node.DefaultNodeConfig

	cfg := applyFlags(t, flags, "--"+ListenHostFlag.Name, "10.0.0.5")
	if cfg.Net.ListenHost != "10.0.0.5" {
		t.Errorf("--%s left Net.ListenHost at %q", ListenHostFlag.Name, cfg.Net.ListenHost)
	}
	if cfg.RPC.HTTPHost != def.RPC.HTTPHost || cfg.RPC.WSHost != def.RPC.WSHost {
		t.Errorf("--%s changed the RPC hosts to %q / %q", ListenHostFlag.Name, cfg.RPC.HTTPHost, cfg.RPC.WSHost)
	}

	cfg = applyFlags(t, flags, "--"+ListenPortFlag.Name, "40000")
	if cfg.Net.ListenPort != 40000 || cfg.RPC.HTTPPort != def.RPC.HTTPPort || cfg.RPC.WSPort != def.RPC.WSPort {
		t.Errorf("--%s: Net.ListenPort %d, HTTPPort %d, WSPort %d", ListenPortFlag.Name, cfg.Net.ListenPort, cfg.RPC.HTTPPort, cfg.RPC.WSPort)
	}

	cfg = applyFlags(t, flags, "--"+RPCListenAddrFlag.Name, "10.0.0.6")
	if cfg.RPC.HTTPHost != "10.0.0.6" || cfg.Net.ListenHost != def.Net.ListenHost || cfg.RPC.WSHost != def.RPC.WSHost {
		t.Errorf("--%s: HTTPHost %q, Net.ListenHost %q, WSHost %q", RPCListenAddrFlag.Name, cfg.RPC.HTTPHost, cfg.Net.ListenHost, cfg.RPC.WSHost)
	}

	cfg = applyFlags(t, flags, "--"+WSListenAddrFlag.Name, "10.0.0.7")
	if cfg.RPC.WSHost != "10.0.0.7" || cfg.Net.ListenHost != def.Net.ListenHost || cfg.RPC.HTTPHost != def.RPC.HTTPHost {
		t.Errorf("--%s: WSHost %q, Net.ListenHost %q, HTTPHost %q", WSListenAddrFlag.Name, cfg.RPC.WSHost, cfg.Net.ListenHost, cfg.RPC.HTTPHost)
	}

	// Flags that are not passed leave everything at the file/default values,
	// including the flags that carry a default of their own.
	cfg = applyFlags(t, flags)
	if cfg.Net.ListenHost != def.Net.ListenHost || cfg.RPC.HTTPHost != def.RPC.HTTPHost || cfg.RPC.WSHost != def.RPC.WSHost {
		t.Errorf("no flags: Net.ListenHost %q, HTTPHost %q, WSHost %q", cfg.Net.ListenHost, cfg.RPC.HTTPHost, cfg.RPC.WSHost)
	}
}
