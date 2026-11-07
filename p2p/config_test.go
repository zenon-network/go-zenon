package p2p

import (
	"net"
	"strconv"
	"testing"

	"github.com/ethereum/go-ethereum/p2p/netutil"

	"github.com/zenon-network/go-zenon/p2p/discover"
)

// TestDefaultSeedersAreValidAndUnique checks that every compiled-in
// legacy seeder is a complete, usable enode URL and that no seeder,
// node identity or endpoint appears twice. A malformed entry would make
// Net.Nodes fail and stop the node from starting with the default
// configuration; an unusable or duplicate one would silently shrink the
// discovery bootstrap set.
func TestDefaultSeedersAreValidAndUnique(t *testing.T) {
	if len(DefaultSeeders) == 0 {
		t.Fatal("DefaultSeeders is empty")
	}

	urls := make(map[string]int, len(DefaultSeeders))
	ids := make(map[discover.NodeID]int, len(DefaultSeeders))
	udpEndpoints := make(map[string]int, len(DefaultSeeders))
	tcpEndpoints := make(map[string]int, len(DefaultSeeders))
	for i, url := range DefaultSeeders {
		if j, ok := urls[url]; ok {
			t.Errorf("seeder %d duplicates seeder %d: %s", i, j, url)
			continue
		}
		urls[url] = i

		node, err := discover.ParseNode(url)
		if err != nil {
			t.Errorf("seeder %d is not a valid enode URL: %v (%s)", i, err, url)
			continue
		}
		// ParseNode checks only the ID's length; the discovery handshake
		// needs it to be a point on the curve.
		if _, err := node.ID.Pubkey(); err != nil {
			t.Errorf("seeder %d has an invalid node ID: %v (%s)", i, err, url)
		}
		if !isPublicUnicast(node.IP) {
			t.Errorf("seeder %d has a non-public IP address %v: %s", i, node.IP, url)
		}
		if node.TCP == 0 || node.UDP == 0 {
			t.Errorf("seeder %d has a zero port (tcp=%d udp=%d): %s", i, node.TCP, node.UDP, url)
		}
		if j, ok := ids[node.ID]; ok {
			t.Errorf("seeder %d reuses the node ID of seeder %d: %s", i, j, url)
		}
		ids[node.ID] = i

		// Bootstrap bonding goes to the UDP endpoint (which a discport
		// query can set apart from the TCP port); later dials use TCP.
		udp := net.JoinHostPort(node.IP.String(), strconv.Itoa(int(node.UDP)))
		if j, ok := udpEndpoints[udp]; ok {
			t.Errorf("seeder %d reuses the UDP endpoint %s of seeder %d", i, udp, j)
		}
		udpEndpoints[udp] = i
		tcp := net.JoinHostPort(node.IP.String(), strconv.Itoa(int(node.TCP)))
		if j, ok := tcpEndpoints[tcp]; ok {
			t.Errorf("seeder %d reuses the TCP endpoint %s of seeder %d", i, tcp, j)
		}
		tcpEndpoints[tcp] = i
	}

	nodes, err := (&Net{Seeders: DefaultSeeders}).Nodes()
	if err != nil {
		t.Fatalf("Net.Nodes rejects DefaultSeeders: %v", err)
	}
	if len(nodes) != len(DefaultSeeders) {
		t.Fatalf("Net.Nodes returned %d nodes for %d seeders", len(nodes), len(DefaultSeeders))
	}
}

// sharedAddressSpace is RFC 6598 carrier-grade NAT space, which
// netutil's special-use lists do not cover.
var sharedAddressSpace = &net.IPNet{IP: net.IPv4(100, 64, 0, 0), Mask: net.CIDRMask(10, 32)}

// isPublicUnicast reports whether ip is a globally routable unicast
// address, which a mainnet seeder must have: not private, loopback,
// link-local, carrier-grade NAT, or an IANA special-use range such as
// documentation or benchmarking space.
func isPublicUnicast(ip net.IP) bool {
	return ip != nil &&
		ip.IsGlobalUnicast() &&
		!ip.IsPrivate() &&
		!ip.IsLoopback() &&
		!ip.IsLinkLocalUnicast() &&
		!netutil.IsLAN(ip) &&
		!netutil.IsSpecialNetwork(ip) &&
		!sharedAddressSpace.Contains(ip)
}

func TestIsPublicUnicast(t *testing.T) {
	for _, tc := range []struct {
		ip     string
		public bool
	}{
		{"178.18.250.224", true},
		{"5.9.153.75", true},
		{"2a01:4f8::1", true},
		{"0.0.0.0", false},
		{"127.0.0.1", false},
		{"10.0.0.1", false},
		{"172.16.0.1", false},
		{"192.168.1.1", false},
		{"169.254.0.1", false},
		{"100.64.0.1", false},      // shared address space
		{"192.0.2.1", false},       // TEST-NET-1
		{"198.51.100.1", false},    // TEST-NET-2
		{"203.0.113.1", false},     // TEST-NET-3
		{"198.18.0.1", false},      // benchmarking
		{"224.0.0.1", false},       // multicast
		{"255.255.255.255", false}, // broadcast
		{"::1", false},
		{"fe80::1", false},
		{"fc00::1", false},
		{"2001:db8::1", false}, // documentation
	} {
		if got := isPublicUnicast(net.ParseIP(tc.ip)); got != tc.public {
			t.Errorf("isPublicUnicast(%s) = %v, want %v", tc.ip, got, tc.public)
		}
	}
}
