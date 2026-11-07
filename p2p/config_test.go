package p2p

import (
	"net"
	"strconv"
	"testing"

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

// nonGlobal lists the address blocks that the IANA special-purpose
// registries mark as not globally reachable, or that are otherwise
// never a valid public endpoint. Blocks that mix global and non-global
// assignments (192.0.0.0/24, 2001::/23) are excluded whole, which is
// conservative for a seeder list. IPv6 outside 2000::/3 is rejected
// separately, which covers ::/128, ::1, ::ffff:0:0/96, 64:ff9b::/96,
// 64:ff9b:1::/48, 100::/64, 5f00::/16, fc00::/7 and fe80::/10.
var nonGlobal = func() []*net.IPNet {
	var out []*net.IPNet
	for _, cidr := range []string{
		"0.0.0.0/8",          // "this" network
		"10.0.0.0/8",         // private use
		"100.64.0.0/10",      // shared address space
		"127.0.0.0/8",        // loopback
		"169.254.0.0/16",     // link local
		"172.16.0.0/12",      // private use
		"192.0.0.0/24",       // IETF protocol assignments
		"192.0.2.0/24",       // TEST-NET-1
		"192.88.99.0/24",     // deprecated 6to4 relay anycast
		"192.168.0.0/16",     // private use
		"198.18.0.0/15",      // benchmarking
		"198.51.100.0/24",    // TEST-NET-2
		"203.0.113.0/24",     // TEST-NET-3
		"224.0.0.0/4",        // multicast
		"240.0.0.0/4",        // reserved
		"255.255.255.255/32", // limited broadcast
		"2001::/23",          // IETF protocol assignments
		"2001:db8::/32",      // documentation
		"2002::/16",          // 6to4
		"3fff::/20",          // documentation
	} {
		_, n, err := net.ParseCIDR(cidr)
		if err != nil {
			panic(err)
		}
		out = append(out, n)
	}
	return out
}()

// globalUnicast6 is the IPv6 global unicast allocation.
var _, globalUnicast6, _ = net.ParseCIDR("2000::/3")

// isPublicUnicast reports whether ip is a globally reachable unicast
// address, which a mainnet seeder must have.
func isPublicUnicast(ip net.IP) bool {
	if ip == nil || !ip.IsGlobalUnicast() {
		return false
	}
	if ip.To4() == nil && !globalUnicast6.Contains(ip) {
		return false
	}
	for _, n := range nonGlobal {
		if n.Contains(ip) {
			return false
		}
	}
	return true
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
		{"192.0.0.8", false},   // IETF protocol assignments
		{"240.0.0.1", false},   // reserved
		{"192.88.99.1", false}, // 6to4 relay anycast
		{"::ffff:10.0.0.1", false},
		{"64:ff9b::1", false},   // NAT64
		{"64:ff9b:1::1", false}, // local-use NAT64
		{"100::1", false},       // discard-only
		{"5f00::1", false},      // SRv6 SIDs
		{"2001::1", false},      // Teredo
		{"2002::1", false},      // 6to4
		{"3fff::1", false},      // documentation
		{"2606:4700::1", true},
	} {
		if got := isPublicUnicast(net.ParseIP(tc.ip)); got != tc.public {
			t.Errorf("isPublicUnicast(%s) = %v, want %v", tc.ip, got, tc.public)
		}
	}
}
