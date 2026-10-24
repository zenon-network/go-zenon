package node

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/zenon-network/go-zenon/p2p"
)

func TestMakeNetConfig_PeerstoreDirResolution(t *testing.T) {
	custom := "/custom/peerstore/path"
	empty := ""

	cases := []struct {
		name          string
		peerstoreDir  *string
		wantPersisted string // want value of PeerstoreDir; computed default when empty
	}{
		{
			name:         "omitted falls back to default path",
			peerstoreDir: nil,
		},
		{
			name:          "explicit empty string disables persistence",
			peerstoreDir:  &empty,
			wantPersisted: "",
		},
		{
			name:          "explicit custom path is used verbatim",
			peerstoreDir:  &custom,
			wantPersisted: custom,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := &Config{
				DataPath: "/data",
				Net:      NetConfig{PeerstoreDir: tc.peerstoreDir},
			}
			netCfg := c.makeNetConfig()

			want := tc.wantPersisted
			if tc.peerstoreDir == nil {
				want = filepath.Join("/data", p2p.DefaultNetDirName, p2p.DefaultPeerstoreDirName)
			}
			if netCfg.PeerstoreDir != want {
				t.Errorf("PeerstoreDir = %q, want %q", netCfg.PeerstoreDir, want)
			}
		})
	}
}

// TestConfigClone verifies that Clone returns a deep copy sharing no
// mutable slice backing arrays or pointers with the original.
func TestConfigClone(t *testing.T) {
	original := &Config{
		DataPath: "/data",
		Name:     "test",
		Producer: &ProducerConfig{
			Address:     "z1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqq",
			Index:       0,
			KeyFilePath: "/key",
			Password:    "pw",
		},
		RPC: RPCConfig{
			Endpoints:        []string{"ledger", "stats"},
			HTTPVirtualHosts: []string{"localhost"},
			HTTPCors:         []string{"*"},
			WSOrigins:        []string{"http://localhost"},
		},
		Net: NetConfig{
			Seeders:        []string{"enode://a", "enode://b"},
			BootstrapPeers: []string{"/ip4/1.2.3.4/tcp/4001"},
			PeerstoreDir:   strPtr("/peerstore"),
		},
	}

	clone := original.Clone()

	// Mutate every slice and pointer in the clone.
	clone.RPC.Endpoints[0] = "MUTATED"
	clone.RPC.HTTPVirtualHosts[0] = "MUTATED"
	clone.RPC.HTTPCors[0] = "MUTATED"
	clone.RPC.WSOrigins[0] = "MUTATED"
	clone.Net.Seeders[0] = "MUTATED"
	clone.Net.BootstrapPeers[0] = "MUTATED"
	*clone.Net.PeerstoreDir = "MUTATED"
	clone.Producer.Address = "MUTATED"

	// The original must be unchanged.
	if original.RPC.Endpoints[0] == "MUTATED" {
		t.Error("Clone: mutating clone.RPC.Endpoints affected original")
	}
	if original.RPC.HTTPVirtualHosts[0] == "MUTATED" {
		t.Error("Clone: mutating clone.RPC.HTTPVirtualHosts affected original")
	}
	if original.RPC.HTTPCors[0] == "MUTATED" {
		t.Error("Clone: mutating clone.RPC.HTTPCors affected original")
	}
	if original.RPC.WSOrigins[0] == "MUTATED" {
		t.Error("Clone: mutating clone.RPC.WSOrigins affected original")
	}
	if original.Net.Seeders[0] == "MUTATED" {
		t.Error("Clone: mutating clone.Net.Seeders affected original")
	}
	if original.Net.BootstrapPeers[0] == "MUTATED" {
		t.Error("Clone: mutating clone.Net.BootstrapPeers affected original")
	}
	if *original.Net.PeerstoreDir == "MUTATED" {
		t.Error("Clone: mutating clone.Net.PeerstoreDir affected original")
	}
	if original.Producer.Address == "MUTATED" {
		t.Error("Clone: mutating clone.Producer.Address affected original")
	}
}

// TestConfigClone_NilSlices verifies that Clone handles nil slices
// without panicking.
func TestConfigClone_NilSlices(t *testing.T) {
	c := &Config{}
	clone := c.Clone()
	if clone.RPC.Endpoints != nil {
		t.Error("Clone: nil Endpoints became non-nil")
	}
	if clone.Net.Seeders != nil {
		t.Error("Clone: nil Seeders became non-nil")
	}
	if clone.Net.PeerstoreDir != nil {
		t.Error("Clone: nil PeerstoreDir became non-nil")
	}
	if clone.Producer != nil {
		t.Error("Clone: nil Producer became non-nil")
	}
}

// TestJSONOverlayDoesNotMutateDefaults verifies that applying a JSON
// overlay to a clone of DefaultNodeConfig leaves the package-level
// defaults untouched. This is the regression test for the shared-slice
// bug where json.Unmarshal wrote into DefaultNodeConfig's backing
// arrays.
func TestJSONOverlayDoesNotMutateDefaults(t *testing.T) {
	// Snapshot the defaults before the overlay. Copy independently —
	// using Clone for the snapshot would alias the defaults under a
	// shallow Clone and mask the mutation being tested.
	beforeSeeders := append([]string(nil), DefaultNodeConfig.Net.Seeders...)
	beforeHosts := append([]string(nil), DefaultNodeConfig.RPC.HTTPVirtualHosts...)
	beforeName := DefaultNodeConfig.Name

	// Apply an overlay the way MakeConfig and RunNodeWithConfig do.
	overlay := `{
		"Name": "overlay-node",
		"RPC": {"HTTPVirtualHosts": ["example.com"]},
		"Net": {"Seeders": ["enode://1.2.3.4:35995"]}
	}`
	cfg := DefaultNodeConfig.Clone()
	if err := json.Unmarshal([]byte(overlay), cfg); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	// The overlay must have been applied to the clone.
	if cfg.Name != "overlay-node" {
		t.Errorf("Name = %q, want %q", cfg.Name, "overlay-node")
	}
	if cfg.RPC.HTTPVirtualHosts[0] != "example.com" {
		t.Errorf("HTTPVirtualHosts[0] = %q, want %q", cfg.RPC.HTTPVirtualHosts[0], "example.com")
	}
	if cfg.Net.Seeders[0] != "enode://1.2.3.4:35995" {
		t.Errorf("Seeders[0] = %q, want %q", cfg.Net.Seeders[0], "enode://1.2.3.4:35995")
	}

	// The package-level defaults must be unchanged.
	if !reflect.DeepEqual(DefaultNodeConfig.RPC.HTTPVirtualHosts, beforeHosts) {
		t.Errorf("DefaultNodeConfig.RPC.HTTPVirtualHosts mutated: %v, want %v",
			DefaultNodeConfig.RPC.HTTPVirtualHosts, beforeHosts)
	}
	if !reflect.DeepEqual(DefaultNodeConfig.Net.Seeders, beforeSeeders) {
		t.Errorf("DefaultNodeConfig.Net.Seeders mutated: %v, want %v",
			DefaultNodeConfig.Net.Seeders, beforeSeeders)
	}
	if DefaultNodeConfig.Name != beforeName {
		t.Errorf("DefaultNodeConfig.Name mutated: %q, want %q", DefaultNodeConfig.Name, beforeName)
	}
}

// TestRejectedJSONOverlayLeavesDefaultsUnchanged verifies that a
// malformed JSON overlay returns an error and does not modify the
// package-level defaults.
func TestRejectedJSONOverlayLeavesDefaultsUnchanged(t *testing.T) {
	beforeSeeders := append([]string(nil), DefaultNodeConfig.Net.Seeders...)
	beforeHosts := append([]string(nil), DefaultNodeConfig.RPC.HTTPVirtualHosts...)
	beforeName := DefaultNodeConfig.Name

	cfg := DefaultNodeConfig.Clone()
	err := json.Unmarshal([]byte(`{"Name":`), cfg)
	if err == nil {
		t.Fatal("expected an error for malformed JSON, got nil")
	}

	// Even the clone may be partially modified by a rejected Unmarshal,
	// but the package-level defaults must be untouched.
	if !reflect.DeepEqual(DefaultNodeConfig.RPC.HTTPVirtualHosts, beforeHosts) {
		t.Errorf("DefaultNodeConfig.RPC.HTTPVirtualHosts mutated by rejected overlay: %v, want %v",
			DefaultNodeConfig.RPC.HTTPVirtualHosts, beforeHosts)
	}
	if !reflect.DeepEqual(DefaultNodeConfig.Net.Seeders, beforeSeeders) {
		t.Errorf("DefaultNodeConfig.Net.Seeders mutated by rejected overlay: %v, want %v",
			DefaultNodeConfig.Net.Seeders, beforeSeeders)
	}
	if DefaultNodeConfig.Name != beforeName {
		t.Errorf("DefaultNodeConfig.Name mutated by rejected overlay: %q, want %q",
			DefaultNodeConfig.Name, beforeName)
	}
}

// TestConfigClone_RejectedOverlayLeavesDefaultsUnchanged verifies that
// when a syntactically valid JSON overlay with a type error in a later
// field is rejected by encoding/json, the package-level defaults are not
// mutated. The overlay writes into a mutable field (Seeders) before the
// type error is hit, so without Clone the defaults' backing array would
// be modified (edgepillar's review comment).
func TestConfigClone_RejectedOverlayLeavesDefaultsUnchanged(t *testing.T) {
	beforeSeeders := append([]string(nil), DefaultNodeConfig.Net.Seeders...)
	beforeHTTPPort := DefaultNodeConfig.RPC.HTTPPort

	cfg := DefaultNodeConfig.Clone()
	// Valid JSON syntax; Seeders is a valid mutable-field overlay, but
	// HTTPPort has a type error (string instead of int). encoding/json
	// processes fields in order, so Seeders is decoded before the error.
	overlay := `{"Net": {"Seeders": ["valid-seeder"]}, "RPC": {"HTTPPort": "not-a-number"}}`
	err := json.Unmarshal([]byte(overlay), cfg)
	if err == nil {
		t.Fatal("expected a type error for HTTPPort, got nil")
	}
	var typeErr *json.UnmarshalTypeError
	if !errors.As(err, &typeErr) {
		t.Errorf("expected *json.UnmarshalTypeError, got %T: %v", err, err)
	}
	// Seeders was decoded before the type error, so the clone's slice
	// must have the overlay value — proving the overlay was applied to
	// the clone, not rejected outright.
	if len(cfg.Net.Seeders) == 0 || cfg.Net.Seeders[0] != "valid-seeder" {
		t.Errorf("cfg.Net.Seeders = %v, want [valid-seeder] (overlay applied before type error)", cfg.Net.Seeders)
	}

	// The package-level defaults must be untouched even though the overlay
	// was syntactically valid and wrote into a mutable field before failing.
	if !reflect.DeepEqual(DefaultNodeConfig.Net.Seeders, beforeSeeders) {
		t.Errorf("DefaultNodeConfig.Net.Seeders mutated by rejected overlay: %v, want %v",
			DefaultNodeConfig.Net.Seeders, beforeSeeders)
	}
	if DefaultNodeConfig.RPC.HTTPPort != beforeHTTPPort {
		t.Errorf("DefaultNodeConfig.RPC.HTTPPort mutated by rejected overlay: %v, want %v",
			DefaultNodeConfig.RPC.HTTPPort, beforeHTTPPort)
	}
}

func strPtr(s string) *string { return &s }
