package node

import (
	"path/filepath"
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
