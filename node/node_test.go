package node

import (
	"encoding/json"
	"testing"

	"github.com/zenon-network/go-zenon/p2p/libp2p"
)

func TestConfigIgnoresLegacySeedersKey(t *testing.T) {
	raw := `{"Net":{"Seeders":["enode://abc@1.2.3.4:35995"],"MaxPeers":7,"ListenPort":12345}}`
	c := DefaultNodeConfig
	if err := json.Unmarshal([]byte(raw), &c); err != nil {
		t.Fatalf("unmarshal config with Seeders: %v", err)
	}
	if c.Net.MaxPeers != 7 || c.Net.ListenPort != 12345 {
		t.Fatalf("known keys not applied: MaxPeers=%d ListenPort=%d", c.Net.MaxPeers, c.Net.ListenPort)
	}
}

func TestNewNodeUsesLibp2pServer(t *testing.T) {
	c := DefaultNodeConfig
	c.DataPath = t.TempDir()
	c.WalletPath = t.TempDir()
	c.GenesisFile = ""
	c.Net.BootstrapPeers = []string{"enode://abc@1.2.3.4:35995"}

	n, err := NewNode(&c)
	if err != nil {
		t.Fatalf("NewNode: %v", err)
	}
	defer n.dataDirLock.Release()

	srv, ok := n.server.(*libp2p.Server)
	if !ok {
		t.Fatalf("node.server is %T, want *libp2p.Server", n.server)
	}
	if !srv.Discovery {
		t.Fatal("Discovery = false, want true")
	}
	if len(srv.BootstrapPeers) != 0 {
		t.Fatalf("BootstrapPeers = %d entries, want enode entry skipped", len(srv.BootstrapPeers))
	}
}
