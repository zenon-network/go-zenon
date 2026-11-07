package p2p

import (
	"net"
	"strconv"
	"testing"

	"github.com/zenon-network/go-zenon/p2p/discover"
)

// TestDefaultSeedersAreValidAndUnique checks that every compiled-in
// legacy seeder is a complete enode URL and that no seeder, node
// identity or endpoint appears twice. A malformed entry would make
// Net.Nodes fail and stop the node from starting with the default
// configuration; a duplicate would silently shrink the bootstrap set.
func TestDefaultSeedersAreValidAndUnique(t *testing.T) {
	if len(DefaultSeeders) == 0 {
		t.Fatal("DefaultSeeders is empty")
	}

	urls := make(map[string]int, len(DefaultSeeders))
	ids := make(map[discover.NodeID]int, len(DefaultSeeders))
	endpoints := make(map[string]int, len(DefaultSeeders))
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
		if node.IP == nil || node.IP.IsUnspecified() {
			t.Errorf("seeder %d has no IP address: %s", i, url)
		}
		if node.TCP == 0 || node.UDP == 0 {
			t.Errorf("seeder %d has a zero port (tcp=%d udp=%d): %s", i, node.TCP, node.UDP, url)
		}
		if j, ok := ids[node.ID]; ok {
			t.Errorf("seeder %d reuses the node ID of seeder %d: %s", i, j, url)
		}
		ids[node.ID] = i

		endpoint := net.JoinHostPort(node.IP.String(), strconv.Itoa(int(node.TCP)))
		if j, ok := endpoints[endpoint]; ok {
			t.Errorf("seeder %d reuses the endpoint %s of seeder %d", i, endpoint, j)
		}
		endpoints[endpoint] = i
	}

	nodes, err := (&Net{Seeders: DefaultSeeders}).Nodes()
	if err != nil {
		t.Fatalf("Net.Nodes rejects DefaultSeeders: %v", err)
	}
	if len(nodes) != len(DefaultSeeders) {
		t.Fatalf("Net.Nodes returned %d nodes for %d seeders", len(nodes), len(DefaultSeeders))
	}
}
