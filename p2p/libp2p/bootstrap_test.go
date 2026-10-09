package libp2p

import (
	"testing"
)

func TestParseBootstrapPeer(t *testing.T) {
	// Use a well-known peer ID format for testing
	maddr := "/ip4/172.30.0.10/tcp/35995/p2p/16Uiu2HAmRe2RazMPxYEV1A8Vs7LbVfNVMqBdiw7zgSkBLB3E2vQv"

	info, err := ParseBootstrapPeer(maddr)
	if err != nil {
		t.Fatal(err)
	}

	if info.ID.String() != "16Uiu2HAmRe2RazMPxYEV1A8Vs7LbVfNVMqBdiw7zgSkBLB3E2vQv" {
		t.Fatalf("unexpected peer ID: %s", info.ID)
	}
	if len(info.Addrs) != 1 {
		t.Fatalf("expected 1 address, got %d", len(info.Addrs))
	}
}

func TestParseBootstrapPeer_Invalid(t *testing.T) {
	tests := []struct {
		name  string
		maddr string
	}{
		{"empty", ""},
		{"no p2p", "/ip4/172.30.0.10/tcp/35995"},
		{"garbage", "not-a-multiaddr"},
		{"enode", "enode://abc@1.2.3.4:35995"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseBootstrapPeer(tt.maddr)
			if err == nil {
				t.Fatal("expected error for invalid multiaddr")
			}
		})
	}
}

func TestParseBootstrapPeers(t *testing.T) {
	maddrs := []string{
		"/ip4/172.30.0.10/tcp/35995/p2p/16Uiu2HAmRe2RazMPxYEV1A8Vs7LbVfNVMqBdiw7zgSkBLB3E2vQv",
		"/ip4/172.30.0.11/tcp/35995/p2p/16Uiu2HAmQroSuHiJbVYRSvdU3HCTGuSsy1a3s4Jzx6LMaTVRiyyW",
	}

	peers, err := ParseBootstrapPeers(maddrs)
	if err != nil {
		t.Fatal(err)
	}
	if len(peers) != 2 {
		t.Fatalf("expected 2 peers, got %d", len(peers))
	}
	if peers[0].ID.String() == peers[1].ID.String() {
		t.Fatal("peer IDs should be different")
	}
}

func TestParseBootstrapPeers_Empty(t *testing.T) {
	peers, err := ParseBootstrapPeers([]string{})
	if err != nil {
		t.Fatal(err)
	}
	if len(peers) != 0 {
		t.Fatalf("expected 0 peers, got %d", len(peers))
	}
}

func TestParseBootstrapPeers_SkipsEmpty(t *testing.T) {
	maddrs := []string{
		"",
		"/ip4/172.30.0.10/tcp/35995/p2p/16Uiu2HAmRe2RazMPxYEV1A8Vs7LbVfNVMqBdiw7zgSkBLB3E2vQv",
		"",
	}

	peers, err := ParseBootstrapPeers(maddrs)
	if err != nil {
		t.Fatal(err)
	}
	if len(peers) != 1 {
		t.Fatalf("expected 1 peer, got %d", len(peers))
	}
}

func TestParseBootstrapPeers_Invalid(t *testing.T) {
	maddrs := []string{
		"/ip4/172.30.0.10/tcp/35995/p2p/16Uiu2HAmRe2RazMPxYEV1A8Vs7LbVfNVMqBdiw7zgSkBLB3E2vQv",
		"garbage",
	}

	_, err := ParseBootstrapPeers(maddrs)
	if err == nil {
		t.Fatal("expected error for invalid multiaddr in list")
	}
}

// ParseBootstrapPeers tolerates accidental legacy enode:// entries (an
// operator pasting their old Seeders into the new BootstrapPeers field
// shouldn't take their node offline). The entry is skipped, a warning
// is logged, and the remaining valid entries are returned.
func TestParseBootstrapPeers_SkipsLegacyEnode(t *testing.T) {
	maddrs := []string{
		"enode://f0e3e1f507c7cc5a2d2cf0eb173c204c820786c7cecbc0118d1cf76e5eaba90348ce57e5c9dee35c27a40f5a44dafc61b8996f00bb33647c66f66c997ce6592b@206.188.197.194:35995",
		"/ip4/172.30.0.10/tcp/35995/p2p/16Uiu2HAmRe2RazMPxYEV1A8Vs7LbVfNVMqBdiw7zgSkBLB3E2vQv",
	}
	peers, err := ParseBootstrapPeers(maddrs)
	if err != nil {
		t.Fatalf("enode:// entry should be skipped, not error: %v", err)
	}
	if len(peers) != 1 {
		t.Fatalf("expected 1 peer (legacy entry skipped), got %d", len(peers))
	}
	if peers[0].ID.String() != "16Uiu2HAmRe2RazMPxYEV1A8Vs7LbVfNVMqBdiw7zgSkBLB3E2vQv" {
		t.Fatalf("unexpected surviving peer: %s", peers[0].ID)
	}
}

// All-enode input is fully skipped — caller gets an empty slice with no
// error. The dial loop is responsible for handling "no bootstrap peers".
func TestParseBootstrapPeers_AllLegacyEnode(t *testing.T) {
	maddrs := []string{
		"enode://abc@1.2.3.4:35995",
		"enode://def@5.6.7.8:35995",
	}
	peers, err := ParseBootstrapPeers(maddrs)
	if err != nil {
		t.Fatalf("all-enode input should be skipped silently, got: %v", err)
	}
	if len(peers) != 0 {
		t.Fatalf("expected 0 peers, got %d", len(peers))
	}
}

// Whitespace around an otherwise-valid multiaddr should be trimmed.
func TestParseBootstrapPeers_TrimsWhitespace(t *testing.T) {
	maddrs := []string{
		"   /ip4/172.30.0.10/tcp/35995/p2p/16Uiu2HAmRe2RazMPxYEV1A8Vs7LbVfNVMqBdiw7zgSkBLB3E2vQv\n",
	}
	peers, err := ParseBootstrapPeers(maddrs)
	if err != nil {
		t.Fatal(err)
	}
	if len(peers) != 1 {
		t.Fatalf("expected 1 peer, got %d", len(peers))
	}
}

func TestParseBootstrapPeer_IPv6(t *testing.T) {
	maddr := "/ip6/::1/tcp/35995/p2p/16Uiu2HAmRe2RazMPxYEV1A8Vs7LbVfNVMqBdiw7zgSkBLB3E2vQv"

	info, err := ParseBootstrapPeer(maddr)
	if err != nil {
		t.Fatal(err)
	}
	if len(info.Addrs) != 1 {
		t.Fatalf("expected 1 address, got %d", len(info.Addrs))
	}
}
