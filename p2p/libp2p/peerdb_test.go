package libp2p

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
	ma "github.com/multiformats/go-multiaddr"
	"github.com/syndtr/goleveldb/leveldb"
)

func mustAddr(t *testing.T, s string) ma.Multiaddr {
	t.Helper()
	a, err := ma.NewMultiaddr(s)
	if err != nil {
		t.Fatalf("parse multiaddr %q: %v", s, err)
	}
	return a
}

func testPeerID(t *testing.T) peer.ID {
	t.Helper()
	pid, err := PeerIDFromECDSA(mustGenKey(t))
	if err != nil {
		t.Fatalf("derive peer ID: %v", err)
	}
	return pid
}

// TestPeerDBRoundTripAndPersistence is the bedrock invariant: a peer
// recorded in run N is a seed candidate in run N+1, with the lifetime
// under our control rather than libp2p's identify TTLs.
func TestPeerDBRoundTripAndPersistence(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "peerdb")
	pid := testPeerID(t)
	addr := mustAddr(t, "/ip4/10.1.2.3/tcp/35995")

	d, err := openPeerDB(dir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	d.recordConnected(pid, []ma.Multiaddr{addr})

	seeds := d.seeds(0)
	if len(seeds) != 1 || seeds[0].ID != pid {
		t.Fatalf("seeds = %v, want single record for %s", seeds, pid)
	}
	if len(seeds[0].Addrs) != 1 || !seeds[0].Addrs[0].Equal(addr) {
		t.Fatalf("addrs = %v, want [%v]", seeds[0].Addrs, addr)
	}
	if err := d.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	d2, err := openPeerDB(dir)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer d2.Close()
	seeds = d2.seeds(0)
	if len(seeds) != 1 || seeds[0].ID != pid {
		t.Fatal("record did not survive reopen")
	}
}

// TestPeerDBOrderingAndLimit verifies seeds() returns most-recently-seen
// first and respects the limit.
func TestPeerDBOrderingAndLimit(t *testing.T) {
	d, err := openPeerDB(filepath.Join(t.TempDir(), "peerdb"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	clock := time.Now()
	d.now = func() time.Time { return clock }

	var pids []peer.ID
	for i := 0; i < 3; i++ {
		pid := testPeerID(t)
		pids = append(pids, pid)
		addr := mustAddr(t, fmt.Sprintf("/ip4/10.0.0.%d/tcp/35995", i+1))
		d.recordConnected(pid, []ma.Multiaddr{addr})
		clock = clock.Add(time.Minute)
	}

	seeds := d.seeds(0)
	if len(seeds) != 3 {
		t.Fatalf("len(seeds) = %d, want 3", len(seeds))
	}
	// Most recently seen first: pids[2], pids[1], pids[0].
	for i, want := range []peer.ID{pids[2], pids[1], pids[0]} {
		if seeds[i].ID != want {
			t.Fatalf("seeds[%d].ID = %s, want %s", i, seeds[i].ID, want)
		}
	}

	if limited := d.seeds(2); len(limited) != 2 || limited[0].ID != pids[2] {
		t.Fatalf("seeds(2) = %v, want newest two", limited)
	}
}

// TestPeerDBExpiry verifies that records past peerDBMaxAge are filtered
// from seeds() and physically deleted by expire().
func TestPeerDBExpiry(t *testing.T) {
	d, err := openPeerDB(filepath.Join(t.TempDir(), "peerdb"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	base := time.Now()
	clock := base
	d.now = func() time.Time { return clock }

	pid := testPeerID(t)
	d.recordConnected(pid, []ma.Multiaddr{mustAddr(t, "/ip4/10.0.0.1/tcp/35995")})

	clock = base.Add(peerDBMaxAge + time.Hour)
	if seeds := d.seeds(0); len(seeds) != 0 {
		t.Fatalf("expired record still in seeds: %v", seeds)
	}
	d.expire()

	// Winding the clock back would resurrect a merely-filtered record;
	// after expire() it must be gone for good.
	clock = base
	if seeds := d.seeds(0); len(seeds) != 0 {
		t.Fatal("expire() did not delete the record")
	}
}

// TestPeerDBFailCountPrune verifies failed-out peers stop being seed
// candidates and that a successful connect resets the counter.
func TestPeerDBFailCountPrune(t *testing.T) {
	d, err := openPeerDB(filepath.Join(t.TempDir(), "peerdb"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	pid := testPeerID(t)
	addrs := []ma.Multiaddr{mustAddr(t, "/ip4/10.0.0.1/tcp/35995")}
	d.recordConnected(pid, addrs)

	for i := 0; i <= peerDBMaxFailCount; i++ {
		d.recordDialFail(pid)
	}
	if seeds := d.seeds(0); len(seeds) != 0 {
		t.Fatalf("failed-out peer still in seeds: %v", seeds)
	}

	// A successful connect resets the counter.
	d.recordConnected(pid, addrs)
	if seeds := d.seeds(0); len(seeds) != 1 {
		t.Fatal("reconnect did not reset fail count")
	}

	// Unknown peers are not tracked.
	stranger := testPeerID(t)
	d.recordDialFail(stranger)
	if seeds := d.seeds(0); len(seeds) != 1 {
		t.Fatal("recordDialFail created a record for an unknown peer")
	}
}

// TestPeerDBSkipsForeignRecords verifies that records not matching our
// schema — e.g. leftovers from the pstoreds-formatted database this
// replaced — are skipped and deleted, so a pre-existing directory
// self-cleans.
func TestPeerDBSkipsForeignRecords(t *testing.T) {
	d, err := openPeerDB(filepath.Join(t.TempDir(), "peerdb"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	foreignKey := []byte("/peers/addrs/legacy-pstoreds-entry")
	if err := d.db.Put(foreignKey, []byte("not-our-json"), nil); err != nil {
		t.Fatal(err)
	}
	pid := testPeerID(t)
	d.recordConnected(pid, []ma.Multiaddr{mustAddr(t, "/ip4/10.0.0.1/tcp/35995")})

	seeds := d.seeds(0)
	if len(seeds) != 1 || seeds[0].ID != pid {
		t.Fatalf("seeds = %v, want only our record", seeds)
	}
	if _, err := d.db.Get(foreignKey, nil); err != leveldb.ErrNotFound {
		t.Fatalf("foreign record not deleted on sight (err=%v)", err)
	}
}

// TestPeerDBEmptyAddrsKeepExisting verifies that a record refresh
// without addresses (identify not yet completed) keeps the previously
// stored ones.
func TestPeerDBEmptyAddrsKeepExisting(t *testing.T) {
	d, err := openPeerDB(filepath.Join(t.TempDir(), "peerdb"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	pid := testPeerID(t)
	addr := mustAddr(t, "/ip4/10.0.0.1/tcp/35995")
	d.recordConnected(pid, []ma.Multiaddr{addr})
	d.recordConnected(pid, nil)

	seeds := d.seeds(0)
	if len(seeds) != 1 || len(seeds[0].Addrs) != 1 || !seeds[0].Addrs[0].Equal(addr) {
		t.Fatalf("addresses lost on empty refresh: %v", seeds)
	}
}

// TestPeerDBExpireCapsRecordCount verifies expire() evicts the
// oldest-lastSeen records once the database holds more than
// peerDBMaxRecords, rather than letting it grow without bound.
func TestPeerDBExpireCapsRecordCount(t *testing.T) {
	d, err := openPeerDB(filepath.Join(t.TempDir(), "peerdb"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	clock := time.Now()
	d.now = func() time.Time { return clock }

	const extra = 10
	var newest []peer.ID
	for i := 0; i < peerDBMaxRecords+extra; i++ {
		pid := testPeerID(t)
		d.recordConnected(pid, []ma.Multiaddr{mustAddr(t, fmt.Sprintf("/ip4/10.%d.%d.%d/tcp/35995", (i>>16)&0xff, (i>>8)&0xff, i&0xff))})
		if i >= extra {
			newest = append(newest, pid)
		}
		clock = clock.Add(time.Second)
	}

	d.expire()

	seeds := d.seeds(0)
	if len(seeds) != peerDBMaxRecords {
		t.Fatalf("len(seeds) after expire = %d, want %d", len(seeds), peerDBMaxRecords)
	}
	present := make(map[peer.ID]bool, len(seeds))
	for _, s := range seeds {
		present[s.ID] = true
	}
	for _, pid := range newest {
		if !present[pid] {
			t.Fatalf("expire() evicted a newer record (%s) while over cap", pid)
		}
	}
}

// TestPeerDBSeedsDiversifyAcrossSubnets verifies seeds() doesn't let a
// single subnet dominate a limited result: many recently-seen peers
// from one /16 must not crowd out an older peer from a different
// subnet entirely.
func TestPeerDBSeedsDiversifyAcrossSubnets(t *testing.T) {
	d, err := openPeerDB(filepath.Join(t.TempDir(), "peerdb"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	clock := time.Now()
	d.now = func() time.Time { return clock }

	// One peer from a distinct subnet, seen first (oldest).
	outsider := testPeerID(t)
	d.recordConnected(outsider, []ma.Multiaddr{mustAddr(t, "/ip4/203.0.113.1/tcp/35995")})
	clock = clock.Add(time.Minute)

	// Many more-recently-seen peers all from the same /24, as an
	// attacker cycling identities from one source would produce.
	for i := 0; i < 20; i++ {
		pid := testPeerID(t)
		d.recordConnected(pid, []ma.Multiaddr{mustAddr(t, fmt.Sprintf("/ip4/10.0.0.%d/tcp/35995", i+1))})
		clock = clock.Add(time.Minute)
	}

	limited := d.seeds(5)
	if len(limited) != 5 {
		t.Fatalf("len(seeds(5)) = %d, want 5", len(limited))
	}
	found := false
	for _, s := range limited {
		if s.ID == outsider {
			found = true
		}
	}
	if !found {
		t.Fatalf("seeds(5) excluded the only peer from a different subnet: %v", limited)
	}
}

// TestSubnetKeyGroups verifies the granularity subnetKey buckets
// addresses at: IPv4 by /16, and DNS-only peers sharing one group
// regardless of the hostname they advertise.
func TestSubnetKeyGroups(t *testing.T) {
	a := subnetKey([]ma.Multiaddr{mustAddr(t, "/ip4/10.0.0.1/tcp/35995")})
	b := subnetKey([]ma.Multiaddr{mustAddr(t, "/ip4/10.0.5.9/tcp/35995")})
	if a != b {
		t.Fatalf("expected same /16 to share a key: %q != %q", a, b)
	}

	c := subnetKey([]ma.Multiaddr{mustAddr(t, "/ip4/203.0.113.1/tcp/35995")})
	if a == c {
		t.Fatalf("expected different /16s to have different keys, both %q", a)
	}

	dnsA := subnetKey([]ma.Multiaddr{mustAddr(t, "/dns4/seed.example.com/tcp/35995")})
	dnsB := subnetKey([]ma.Multiaddr{mustAddr(t, "/dns4/other.example.com/tcp/35995")})
	if dnsA != dnsB {
		t.Fatalf("expected DNS-only peers to share a key regardless of hostname: %q != %q", dnsA, dnsB)
	}

	if dnsA == subnetKey(nil) {
		t.Fatalf("expected the DNS key to differ from the no-address key")
	}
}

// waitForPeers polls until srv has at least n connected peers.
func waitForPeers(t *testing.T, srv *Server, n int, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if srv.PeerCount() >= n {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d peers (have %d)", n, srv.PeerCount())
}

// TestWarmBootstrapAcrossRestart is the regression guard for the whole
// peer-database design: node A connects to node B via an explicit
// bootstrap entry, restarts with an EMPTY bootstrap list, and must
// rejoin B purely from its peer database. This scenario was impossible
// with the previous pstoreds-backed peerstore for any restart after
// >30min of downtime (identify downgrades address TTLs on disconnect),
// which is exactly why the database is Zenon-owned.
func TestWarmBootstrapAcrossRestart(t *testing.T) {
	portA, portB := freePortTCP(t), freePortTCP(t)
	keyA, keyB := mustGenKey(t), mustGenKey(t)
	dirA := filepath.Join(t.TempDir(), "peerdb-a")

	// B: plain listener, no persistence, accepts inbound only.
	srvB := &Server{
		PrivateKey:        keyB,
		Name:              "node-b",
		MaxPeers:          8,
		MinConnectedPeers: 1,
		NoDial:            true,
		ListenAddr:        fmt.Sprintf("127.0.0.1:%d", portB),
	}
	if err := srvB.Start(); err != nil {
		t.Fatalf("start B: %v", err)
	}
	defer srvB.Stop()

	pidB, err := PeerIDFromECDSA(keyB)
	if err != nil {
		t.Fatal(err)
	}
	bootstrapB, err := ParseBootstrapPeer(fmt.Sprintf("/ip4/127.0.0.1/tcp/%d/p2p/%s", portB, pidB))
	if err != nil {
		t.Fatal(err)
	}

	// First run of A: learns B via the bootstrap list.
	srvA := &Server{
		PrivateKey:        keyA,
		Name:              "node-a",
		MaxPeers:          8,
		MinConnectedPeers: 1,
		ListenAddr:        fmt.Sprintf("127.0.0.1:%d", portA),
		PeerstoreDir:      dirA,
		BootstrapPeers:    []peer.AddrInfo{bootstrapB},
	}
	if err := srvA.Start(); err != nil {
		t.Fatalf("start A: %v", err)
	}
	waitForPeers(t, srvA, 1, 10*time.Second)
	srvA.Stop()

	// Second run of A: no bootstrap entries at all. Only the peer
	// database knows B.
	srvA2 := &Server{
		PrivateKey:        keyA,
		Name:              "node-a",
		MaxPeers:          8,
		MinConnectedPeers: 1,
		ListenAddr:        fmt.Sprintf("127.0.0.1:%d", portA),
		PeerstoreDir:      dirA,
	}
	if err := srvA2.Start(); err != nil {
		t.Fatalf("restart A: %v", err)
	}
	defer srvA2.Stop()

	waitForPeers(t, srvA2, 1, 10*time.Second)
	waitForPeers(t, srvB, 1, 10*time.Second)
}

// TestNoPeerDBWhenDirUnset ensures an empty PeerstoreDir disables
// persistence without breaking startup.
func TestNoPeerDBWhenDirUnset(t *testing.T) {
	srv := newTestServer(mustGenKey(t), "", t)
	if err := srv.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer srv.Stop()
	if srv.peerdb.Load() != nil {
		t.Error("expected nil peerdb with empty PeerstoreDir")
	}
}
