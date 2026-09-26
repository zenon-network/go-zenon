package app

import (
	"flag"
	"fmt"
	"reflect"
	"strconv"
	"testing"

	"github.com/urfave/cli/v2"

	"github.com/zenon-network/go-zenon/node"
)

// listenFlags are the six flags that choose where the node listens.
var listenFlags = []cli.Flag{ListenHostFlag, ListenPortFlag, RPCListenAddrFlag, RPCPortFlag, WSListenAddrFlag, WSPortFlag}

// applyFlags parses args against the listen flags and applies them to
// initial, the way MakeConfig applies the command line to the
// configuration it has just read from the file.
func applyFlags(t *testing.T, initial node.Config, args ...string) node.Config {
	t.Helper()
	set := flag.NewFlagSet("znnd", flag.ContinueOnError)
	for _, f := range listenFlags {
		if err := f.Apply(set); err != nil {
			t.Fatal(err)
		}
	}
	if err := set.Parse(args); err != nil {
		t.Fatal(err)
	}
	cfg := initial
	applyFlagsToConfig(cli.NewContext(cli.NewApp(), set, nil), &cfg)
	return cfg
}

// fileConfig is a configuration as a file might set it: every listen host
// and port differs from the defaults, from the flags' own default values
// and from each other, so a value that leaks from one to another is seen.
func fileConfig() node.Config {
	cfg := node.DefaultNodeConfig
	cfg.Net.ListenHost = "10.1.1.1"
	cfg.Net.ListenPort = 41000
	cfg.RPC.HTTPHost = "10.2.2.2"
	cfg.RPC.HTTPPort = 42000
	cfg.RPC.WSHost = "10.3.3.3"
	cfg.RPC.WSPort = 43000
	return cfg
}

func listenFields(c node.Config) string {
	return fmt.Sprintf("P2P %s:%d HTTP %s:%d WS %s:%d",
		c.Net.ListenHost, c.Net.ListenPort, c.RPC.HTTPHost, c.RPC.HTTPPort, c.RPC.WSHost, c.RPC.WSPort)
}

// requireConfig compares the whole configuration, so a change to any other
// scalar field fails too (the copies share their slices' backing arrays, so
// an in-place change to a slice element would not be seen), and reports the
// listen fields, which are the ones these tests move.
func requireConfig(t *testing.T, what string, got, want node.Config) {
	t.Helper()
	if reflect.DeepEqual(got, want) {
		return
	}
	if g, w := listenFields(got), listenFields(want); g != w {
		t.Errorf("%s: got %s, want %s", what, g, w)
		return
	}
	t.Errorf("%s: a field outside the listen fields changed", what)
}

// Each listen flag sets the field its name and usage describe and nothing
// else: the network flags the P2P listener, the RPC flags the RPC hosts.
// Values already in the configuration survive every flag they are not
// named by.
func TestListenFlagsMapToTheirFields(t *testing.T) {
	cases := []struct {
		flag cli.Flag
		arg  string
		want func(*node.Config)
	}{
		{ListenHostFlag, "10.0.0.5", func(c *node.Config) { c.Net.ListenHost = "10.0.0.5" }},
		{ListenPortFlag, "40000", func(c *node.Config) { c.Net.ListenPort = 40000 }},
		{RPCListenAddrFlag, "10.0.0.6", func(c *node.Config) { c.RPC.HTTPHost = "10.0.0.6" }},
		{RPCPortFlag, "40001", func(c *node.Config) { c.RPC.HTTPPort = 40001 }},
		{WSListenAddrFlag, "10.0.0.7", func(c *node.Config) { c.RPC.WSHost = "10.0.0.7" }},
		{WSPortFlag, "40002", func(c *node.Config) { c.RPC.WSPort = 40002 }},
	}
	for _, c := range cases {
		name := c.flag.Names()[0]
		for _, initial := range []node.Config{node.DefaultNodeConfig, fileConfig()} {
			want := initial
			c.want(&want)
			got := applyFlags(t, initial, "--"+name, c.arg)
			requireConfig(t, "--"+name+" "+c.arg, got, want)
		}
	}
}

// Flags that are not passed change nothing, including the flags that
// carry a default value of their own: a host or port read from the file
// is not overwritten by a flag default.
func TestOmittedListenFlagsPreserveTheConfiguration(t *testing.T) {
	for _, initial := range []node.Config{node.DefaultNodeConfig, fileConfig()} {
		requireConfig(t, "no flags", applyFlags(t, initial), initial)
	}
}

// A flag passed with its own default value still applies: the operator
// asked for that value explicitly, whatever the file says.
func TestListenFlagsWithDefaultValuesStillApply(t *testing.T) {
	want := fileConfig()
	want.Net.ListenHost = node.DefaultNodeConfig.Net.ListenHost
	want.Net.ListenPort = node.DefaultNodeConfig.Net.ListenPort
	got := applyFlags(t, fileConfig(),
		"--"+ListenHostFlag.Name, node.DefaultNodeConfig.Net.ListenHost,
		"--"+ListenPortFlag.Name, strconv.Itoa(node.DefaultNodeConfig.Net.ListenPort))
	requireConfig(t, "flags at their default values", got, want)
}

// All six flags at once each reach their own field.
func TestListenFlagsCombined(t *testing.T) {
	want := fileConfig()
	want.Net.ListenHost = "10.0.0.5"
	want.Net.ListenPort = 40000
	want.RPC.HTTPHost = "10.0.0.6"
	want.RPC.HTTPPort = 40001
	want.RPC.WSHost = "10.0.0.7"
	want.RPC.WSPort = 40002
	got := applyFlags(t, fileConfig(),
		"--"+ListenHostFlag.Name, "10.0.0.5",
		"--"+ListenPortFlag.Name, "40000",
		"--"+RPCListenAddrFlag.Name, "10.0.0.6",
		"--"+RPCPortFlag.Name, "40001",
		"--"+WSListenAddrFlag.Name, "10.0.0.7",
		"--"+WSPortFlag.Name, "40002")
	requireConfig(t, "all listen flags", got, want)
}

// An empty host is ignored and the configured value stays, as for every
// string flag applyFlagsToConfig handles.
func TestEmptyHostFlagsAreIgnored(t *testing.T) {
	initial := fileConfig()
	got := applyFlags(t, initial,
		"--"+ListenHostFlag.Name, "",
		"--"+RPCListenAddrFlag.Name, "",
		"--"+WSListenAddrFlag.Name, "")
	requireConfig(t, "empty host flags", got, initial)
}
