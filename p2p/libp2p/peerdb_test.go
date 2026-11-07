package libp2p

import (
	"fmt"
	"net"
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
	d.recordDirect(pid, []ma.Multiaddr{addr})

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
		d.recordDirect(pid, []ma.Multiaddr{addr})
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
	d.recordDirect(pid, []ma.Multiaddr{mustAddr(t, "/ip4/10.0.0.1/tcp/35995")})

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
	d.recordDirect(pid, addrs)

	for i := 0; i <= peerDBMaxFailCount; i++ {
		d.recordDialFail(pid)
	}
	if seeds := d.seeds(0); len(seeds) != 0 {
		t.Fatalf("failed-out peer still in seeds: %v", seeds)
	}

	// A successful connect resets the counter.
	d.recordDirect(pid, addrs)
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
	d.recordDirect(pid, []ma.Multiaddr{mustAddr(t, "/ip4/10.0.0.1/tcp/35995")})

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
	d.recordDirect(pid, []ma.Multiaddr{addr})
	d.recordDirect(pid, nil)

	seeds := d.seeds(0)
	if len(seeds) != 1 || len(seeds[0].Addrs) != 1 || !seeds[0].Addrs[0].Equal(addr) {
		t.Fatalf("addresses lost on empty refresh: %v", seeds)
	}
}

// TestPeerDBExpireCapsRecordCount verifies the cap holds after an
// expire() pass and that established records are evicted group-wise:
// eight /16 groups of 256 records (the last of 218), all established,
// over the cap by ten. The ten evictions come from the full groups, one
// or two each, never from the smaller group, and within a group the
// oldest lastSeen goes first.
func TestPeerDBExpireCapsRecordCount(t *testing.T) {
	d, err := openPeerDB(filepath.Join(t.TempDir(), "peerdb"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	clock := time.Now()
	d.now = func() time.Time { return clock }

	const extra = 10
	groups := make(map[int][]peer.ID)
	for i := 0; i < peerDBMaxRecords+extra; i++ {
		pid := testPeerID(t)
		group := (i >> 8) & 0xff // 0..7
		d.recordDirect(pid, []ma.Multiaddr{mustAddr(t, fmt.Sprintf("/ip4/10.%d.0.%d/tcp/35995", group, i&0xff))})
		groups[group] = append(groups[group], pid)
		clock = clock.Add(time.Second)
	}
	clock = clock.Add(peerDBTenure + time.Hour) // everyone is established

	d.expire()

	seeds := d.seeds(0)
	if len(seeds) != peerDBMaxRecords {
		t.Fatalf("len(seeds) after expire = %d, want %d", len(seeds), peerDBMaxRecords)
	}
	present := make(map[peer.ID]bool, len(seeds))
	for _, s := range seeds {
		present[s.ID] = true
	}
	for group, ids := range groups {
		lost := 0
		for _, pid := range ids {
			if !present[pid] {
				lost++
			}
		}
		if len(ids) < 256 && lost != 0 {
			t.Fatalf("the smaller group %d (%d records) lost %d; only the largest groups may lose records", group, len(ids), lost)
		}
		if lost > 2 {
			t.Fatalf("group %d lost %d records; the loss must spread across the largest groups", group, lost)
		}
		for i, pid := range ids { // the oldest-lastSeen records of a group go first
			if want := i >= lost; present[pid] != want {
				t.Fatalf("group %d record %d present=%v, want %v", group, i, present[pid], want)
			}
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
	d.recordDirect(outsider, []ma.Multiaddr{mustAddr(t, "/ip4/203.0.113.1/tcp/35995")})
	clock = clock.Add(time.Minute)

	// Many more-recently-seen peers all from the same /24, as an
	// attacker cycling identities from one source would produce.
	for i := 0; i < 20; i++ {
		pid := testPeerID(t)
		d.recordDirect(pid, []ma.Multiaddr{mustAddr(t, fmt.Sprintf("/ip4/10.0.0.%d/tcp/35995", i+1))})
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

// TestSubnetKeyGroups verifies the granularity subnetKey buckets origin
// IPs at (IPv4 by /16, IPv6 by /48, no IP as the unobserved group) and
// what originIP takes from a connection's remote multiaddr.
func TestSubnetKeyGroups(t *testing.T) {
	a := subnetKey(net.ParseIP("10.0.0.1"))
	if b := subnetKey(net.ParseIP("10.0.5.9")); a != b {
		t.Fatalf("expected same /16 to share a key: %q != %q", a, b)
	}
	if c := subnetKey(net.ParseIP("203.0.113.1")); a == c {
		t.Fatalf("expected different /16s to have different keys, both %q", a)
	}
	if m := subnetKey(net.ParseIP("::ffff:10.0.9.9")); m != a {
		t.Fatalf("expected an IPv4-mapped IPv6 origin to key as IPv4: %q != %q", m, a)
	}
	v6a := subnetKey(net.ParseIP("2001:db8:1::1"))
	if v6b := subnetKey(net.ParseIP("2001:db8:1:ffff::2")); v6a != v6b {
		t.Fatalf("expected same /48 to share a key: %q != %q", v6a, v6b)
	}
	if v6c := subnetKey(net.ParseIP("2001:db8:2::1")); v6a == v6c {
		t.Fatalf("expected different /48s to have different keys, both %q", v6a)
	}
	if k := subnetKey(nil); k != unobservedGroup {
		t.Fatalf("subnetKey(nil) = %q, want %q", k, unobservedGroup)
	}
	if k := recordGroup(&peerDBRecord{Addrs: []string{"/ip4/203.0.113.1/tcp/35995"}}); k != unobservedGroup {
		t.Fatalf("a record without an origin keyed %q from its advertised addresses, want %q", k, unobservedGroup)
	}

	for _, tc := range []struct {
		addr string
		want string
	}{
		{"/ip4/198.51.100.7/tcp/51234", "198.51.100.7"},
		{"/ip6/2001:db8::7/udp/4001/quic-v1", "2001:db8::7"},
		{"/ip4/192.0.2.1/tcp/4001/p2p/QmcgpsyWgH8Y8ajJz1Cu72KnS5uo2Aa2LpzU7kinSupNKC/p2p-circuit", "192.0.2.1"},
		{"/dns4/seed.example.com/tcp/35995", ""},
	} {
		got := originIP(mustAddr(t, tc.addr))
		if (got == nil && tc.want != "") || (got != nil && got.String() != tc.want) {
			t.Fatalf("originIP(%s) = %v, want %q", tc.addr, got, tc.want)
		}
	}
	if originIP(nil) != nil {
		t.Fatal("originIP(nil) != nil")
	}
}

// TestPeerDBDiversityKeyedOnObservedOrigin is #263: the diversity group is
// the observed connection origin, not the self-advertised addresses. One
// source cycling identities that each advertise a different /16 occupies
// one group, in seeds and in over-cap eviction; two peers advertising the
// same /16 from different origins occupy two.
func TestPeerDBDiversityKeyedOnObservedOrigin(t *testing.T) {
	attacker := mustAddr(t, "/ip4/198.51.100.7/tcp/51234")

	t.Run("many advertised subnets, one origin", func(t *testing.T) {
		d, clock := openTestPeerDB(t)
		honest := syntheticPeer(9, 0)
		d.recordDirect(honest, []ma.Multiaddr{mustAddr(t, "/ip4/203.0.113.1/tcp/35995")})
		var sybils []peer.ID
		for i := 0; i < 20; i++ {
			*clock = clock.Add(time.Minute)
			id := syntheticPeer(1, i)
			d.recordConnected(id, wideAddr(t, i), attacker)
			sybils = append(sybils, id)
		}
		groups := make(map[string]bool)
		for _, id := range sybils {
			groups[recordGroup(d.record(id))] = true
		}
		if len(groups) != 1 {
			t.Fatalf("20 identities from one origin span %d groups, want 1", len(groups))
		}
		seeds := d.seeds(2)
		if len(seeds) != 2 || (seeds[0].ID != honest && seeds[1].ID != honest) {
			t.Fatalf("seeds(2) = %v; the older honest peer must get the second slot", seeds)
		}
		// The stored addresses are still the advertised, dialable ones.
		if got := d.record(sybils[3]).Addrs; len(got) != 1 || got[0] != wideAddr(t, 3)[0].String() {
			t.Fatalf("stored addrs = %v, want the advertised address", got)
		}
	})

	t.Run("one advertised subnet, two origins", func(t *testing.T) {
		d, clock := openTestPeerDB(t)
		far := syntheticPeer(2, 0)
		d.recordConnected(far, []ma.Multiaddr{mustAddr(t, "/ip4/10.0.0.1/tcp/35995")}, mustAddr(t, "/ip4/203.0.113.9/tcp/40000"))
		for i := 0; i < 5; i++ {
			*clock = clock.Add(time.Minute)
			d.recordConnected(syntheticPeer(1, i), []ma.Multiaddr{mustAddr(t, fmt.Sprintf("/ip4/10.0.0.%d/tcp/35995", i+2))}, attacker)
		}
		if a, b := recordGroup(d.record(far)), recordGroup(d.record(syntheticPeer(1, 0))); a == b {
			t.Fatalf("same advertised /16 from different origins share group %q", a)
		}
		seeds := d.seeds(2)
		if len(seeds) != 2 || (seeds[0].ID != far && seeds[1].ID != far) {
			t.Fatalf("seeds(2) = %v; the peer from the other origin must get a slot", seeds)
		}
	})

	t.Run("eviction takes from the shared origin", func(t *testing.T) {
		// 600 honest peers, each from its own origin and seen longest ago,
		// and 1500 identities from one origin advertising distinct /16s:
		// 100 over the cap, all taken from the one-origin group.
		d, clock := openTestPeerDB(t)
		var honest, sybils []peer.ID
		for i := 0; i < 600; i++ {
			*clock = clock.Add(time.Second)
			id := syntheticPeer(9, i)
			d.recordDirect(id, wideAddr(t, i))
			honest = append(honest, id)
		}
		for i := 0; i < 1500; i++ {
			*clock = clock.Add(time.Second)
			id := syntheticPeer(1, i)
			d.recordConnected(id, wideAddr(t, 1000+i), attacker)
			sybils = append(sybils, id)
		}
		establish(clock)
		d.expire()
		if d.countPresent(honest) != 600 || d.countPresent(sybils) != 1400 {
			t.Fatalf("honest %d/600, one-origin %d/1500 remain; want 600 and 1400", d.countPresent(honest), d.countPresent(sybils))
		}
	})
}

// TestPeerDBOriginRefreshAndLegacyRecords: records in the format written
// before the origin field existed still load and seed, share the
// unobserved group whatever they advertise, and gain an origin on the
// peer's next connection; a later record call without an origin keeps
// the stored one.
func TestPeerDBOriginRefreshAndLegacyRecords(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "peerdb")
	d, err := openPeerDB(dir)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().Unix()
	legacyA, legacyB := syntheticPeer(1, 0), syntheticPeer(2, 0)
	// Byte-for-byte the shape the previous code wrote, with and without
	// the firstSeen stamp.
	for id, raw := range map[peer.ID]string{
		legacyA: fmt.Sprintf(`{"v":1,"addrs":["/ip4/10.1.0.1/tcp/35995"],"lastSeen":%d,"failCount":0,"firstSeen":%d}`, now, now-3600),
		legacyB: fmt.Sprintf(`{"v":1,"addrs":["/ip4/10.2.0.1/tcp/35995"],"lastSeen":%d,"failCount":0}`, now-60),
	} {
		if err := d.db.Put([]byte(id), []byte(raw), nil); err != nil {
			t.Fatal(err)
		}
	}
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}

	d, err = openPeerDB(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()
	if seeds := d.seeds(0); len(seeds) != 2 {
		t.Fatalf("legacy records: seeds = %v, want both", seeds)
	}
	for _, id := range []peer.ID{legacyA, legacyB} {
		rec := d.record(id)
		if rec == nil || rec.Origin != "" || recordGroup(rec) != unobservedGroup {
			t.Fatalf("legacy record %s = %+v, want no origin and the unobserved group", id, rec)
		}
	}

	origin := mustAddr(t, "/ip4/198.51.100.7/tcp/51234")
	d.recordConnected(legacyA, nil, origin)
	rec := d.record(legacyA)
	if rec == nil || rec.Origin != "198.51.100.7" || len(rec.Addrs) != 1 {
		t.Fatalf("after reconnect: %+v, want origin 198.51.100.7 and the stored address kept", rec)
	}
	d.recordConnected(legacyA, nil, nil)
	if rec := d.record(legacyA); rec.Origin != "198.51.100.7" {
		t.Fatalf("a record call without an origin replaced it: %+v", rec)
	}
	d.recordConnected(legacyA, nil, mustAddr(t, "/ip6/2001:db8::7/tcp/51234"))
	if rec := d.record(legacyA); rec.Origin != "2001:db8::7" {
		t.Fatalf("origin not refreshed from the latest connection: %+v", rec)
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
	dirB := filepath.Join(t.TempDir(), "peerdb-b")

	// B: plain listener, accepts inbound only; its peer database records
	// A's inbound connection.
	srvB := &Server{
		PrivateKey:        keyB,
		Name:              "node-b",
		MaxPeers:          8,
		MinConnectedPeers: 1,
		NoDial:            true,
		ListenAddr:        fmt.Sprintf("127.0.0.1:%d", portB),
		PeerstoreDir:      dirB,
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
	waitForPeers(t, srvB, 1, 10*time.Second)

	// Both ends record the observed connection origin (#263): A the
	// address it dialled, B the inbound source address.
	pidA, err := PeerIDFromECDSA(keyA)
	if err != nil {
		t.Fatal(err)
	}
	if rec := srvA.peerdb.Load().record(pidB); rec == nil || rec.Origin != "127.0.0.1" {
		t.Fatalf("A's record of B (outbound) = %+v, want origin 127.0.0.1", rec)
	}
	if rec := srvB.peerdb.Load().record(pidA); rec == nil || rec.Origin != "127.0.0.1" {
		t.Fatalf("B's record of A (inbound) = %+v, want origin 127.0.0.1", rec)
	}
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
