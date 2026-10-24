package libp2p

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
	ma "github.com/multiformats/go-multiaddr"
)

// The record cap is enforced on the write path as well as by the hourly
// pass, and over-cap eviction takes newcomers first (newest first, whatever
// subnet they advertise) and only then established records, from the
// largest subnet group (#77). These tests drive recordConnected and expire
// with an injected clock and synthetic peer IDs (the database keys on the
// raw ID bytes, so any distinct string is a distinct peer), and count
// records by iterating the store.

func syntheticPeer(group, n int) peer.ID {
	return peer.ID(fmt.Sprintf("peer-%03d-%06d", group, n))
}

// subnetAddr returns an address in the /16 10.group.0.0, host n.
func subnetAddr(t *testing.T, group, n int) []ma.Multiaddr {
	t.Helper()
	return []ma.Multiaddr{mustAddr(t, fmt.Sprintf("/ip4/10.%d.%d.%d/tcp/35995", group, (n>>8)&0xff, n&0xff))}
}

// wideAddr returns an address whose /16 is distinct for every n.
func wideAddr(t *testing.T, n int) []ma.Multiaddr {
	t.Helper()
	return []ma.Multiaddr{mustAddr(t, fmt.Sprintf("/ip4/%d.%d.0.1/tcp/35995", 11+(n>>8)&0xff, n&0xff))}
}

func openTestPeerDB(t *testing.T) (*peerDB, *time.Time) {
	t.Helper()
	d, err := openPeerDB(filepath.Join(t.TempDir(), "peerdb"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	clock := time.Unix(1_700_000_000, 0)
	d.now = func() time.Time { return clock }
	return d, &clock
}

func (d *peerDB) countRecords() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	iter := d.db.NewIterator(nil, nil)
	defer iter.Release()
	n := 0
	for iter.Next() {
		n++
	}
	return n
}

func (d *peerDB) has(id peer.ID) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.get(id) != nil
}

func (d *peerDB) record(id peer.ID) *peerDBRecord {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.get(id)
}

// countPresent returns how many of ids are still stored.
func (d *peerDB) countPresent(ids []peer.ID) int {
	n := 0
	for _, id := range ids {
		if d.has(id) {
			n++
		}
	}
	return n
}

// establish advances the clock past the tenure window so every record
// stored so far counts as established.
func establish(clock *time.Time) { *clock = clock.Add(peerDBTenure + time.Hour) }

// TestPeerDBWritePathTriggersExpireAtTheCap: without the hourly ticker,
// the database holds at most 2*cap-1 records between calls. The pass runs
// on the write path at the cap-th new record since the last pass, refreshes
// of existing records do not count, and every pass resets the counter.
func TestPeerDBWritePathTriggersExpireAtTheCap(t *testing.T) {
	d, _ := openTestPeerDB(t)

	insert := func(from, to int) {
		for i := from; i < to; i++ {
			d.recordConnected(syntheticPeer(i%64, i), subnetAddr(t, i%64, i))
		}
	}
	insert(0, peerDBMaxRecords) // the cap-th new record runs a pass: nothing over cap yet
	if got := d.countRecords(); got != peerDBMaxRecords {
		t.Fatalf("after %d inserts: %d records, want %d", peerDBMaxRecords, got, peerDBMaxRecords)
	}
	insert(peerDBMaxRecords, 2*peerDBMaxRecords-1)
	if got := d.countRecords(); got != 2*peerDBMaxRecords-1 {
		t.Fatalf("one short of the trigger: %d records, want %d (no pass yet)", got, 2*peerDBMaxRecords-1)
	}
	for round := 0; round < 2; round++ { // refreshes do not count
		for i := 0; i < peerDBMaxRecords; i++ {
			d.recordConnected(syntheticPeer(i%64, i), subnetAddr(t, i%64, i))
		}
	}
	if got := d.countRecords(); got != 2*peerDBMaxRecords-1 {
		t.Fatalf("after %d refreshes: %d records, want %d (refreshes must not trigger a pass)", 2*peerDBMaxRecords, got, 2*peerDBMaxRecords-1)
	}
	insert(2*peerDBMaxRecords-1, 2*peerDBMaxRecords) // the trigger
	if got := d.countRecords(); got != peerDBMaxRecords {
		t.Fatalf("at the trigger: %d records, want the cap %d", got, peerDBMaxRecords)
	}
	insert(2*peerDBMaxRecords, 3*peerDBMaxRecords-1) // the counter reset
	if got := d.countRecords(); got != 2*peerDBMaxRecords-1 {
		t.Fatalf("after the reset, one short of the next trigger: %d records, want %d", got, 2*peerDBMaxRecords-1)
	}
	d.expire() // the ticker pass also resets the counter
	if got := d.countRecords(); got != peerDBMaxRecords {
		t.Fatalf("after the ticker pass: %d records, want %d", got, peerDBMaxRecords)
	}
	insert(3*peerDBMaxRecords-1, 4*peerDBMaxRecords-2)
	if got := d.countRecords(); got != 2*peerDBMaxRecords-1 {
		t.Fatalf("after the ticker reset, one short of the trigger: %d records, want %d", got, 2*peerDBMaxRecords-1)
	}
}

// TestPeerDBFirstSeenStampedOnceAndKeptAcrossReopen: a new record gets the
// stamp; refreshes move lastSeen only; a legacy record stays unstamped
// through a refresh; the stamp survives close and reopen.
func TestPeerDBFirstSeenStampedOnceAndKeptAcrossReopen(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "peerdb")
	d, err := openPeerDB(dir)
	if err != nil {
		t.Fatal(err)
	}
	// Anchored to the real clock: reopening runs an expiry pass with
	// time.Now, which must not age these records out.
	clock := time.Now().Truncate(time.Second).Add(-72 * time.Hour)
	d.now = func() time.Time { return clock }

	id := syntheticPeer(1, 1)
	d.recordConnected(id, subnetAddr(t, 1, 1))
	first := clock.Unix()
	legacy := syntheticPeer(1, 2)
	raw, _ := json.Marshal(&peerDBRecord{Version: peerDBVersion, Addrs: dedupAddrStrings(subnetAddr(t, 1, 2)), LastSeen: first})
	if err := d.db.Put([]byte(legacy), raw, nil); err != nil {
		t.Fatal(err)
	}

	clock = clock.Add(48 * time.Hour)
	d.recordConnected(id, subnetAddr(t, 1, 1))
	d.recordConnected(legacy, subnetAddr(t, 1, 2))
	if rec := d.record(id); rec == nil || rec.FirstSeen != first || rec.LastSeen != clock.Unix() {
		t.Fatalf("stamped record after refresh: %+v, want firstSeen %d and lastSeen %d", rec, first, clock.Unix())
	}
	if rec := d.record(legacy); rec == nil || rec.FirstSeen != 0 || rec.LastSeen != clock.Unix() {
		t.Fatalf("legacy record after refresh: %+v, want firstSeen 0 and lastSeen %d", rec, clock.Unix())
	}
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}

	d2, err := openPeerDB(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d2.Close() }()
	if rec := d2.record(id); rec == nil || rec.FirstSeen != first {
		t.Fatalf("stamp lost across reopen: %+v", rec)
	}
	if rec := d2.record(legacy); rec == nil || rec.FirstSeen != 0 {
		t.Fatalf("legacy record changed across reopen: %+v", rec)
	}
}

// TestPeerDBSameSourceBurstEvictsItself: a burst of fresh identities from
// one /16, large enough to push the database over the cap, evicts its own
// newest records and no established peer anywhere, however stale that
// peer's lastSeen looks.
func TestPeerDBSameSourceBurstEvictsItself(t *testing.T) {
	d, clock := openTestPeerDB(t)

	const tenured = 50
	var established []peer.ID
	for i := 0; i < tenured; i++ {
		id := syntheticPeer(100+i, 0) // 50 distinct /16s
		d.recordConnected(id, subnetAddr(t, 100+i, 0))
		established = append(established, id)
	}
	// Long-connected peers: lastSeen stamped only at adoption, 20 days ago.
	*clock = clock.Add(20 * 24 * time.Hour)

	var burst []peer.ID
	for i := 0; i < peerDBMaxRecords; i++ {
		*clock = clock.Add(time.Second) // distinct first-seen per record
		id := syntheticPeer(7, i)
		d.recordConnected(id, subnetAddr(t, 7, i))
		burst = append(burst, id)
	}
	d.expire()

	if got := d.countRecords(); got != peerDBMaxRecords {
		t.Fatalf("%d records after expire, want the cap %d", got, peerDBMaxRecords)
	}
	if d.countPresent(established) != tenured {
		t.Fatal("an established peer was evicted by a burst from another /16")
	}
	for i, id := range burst {
		if want := i < peerDBMaxRecords-tenured; d.has(id) != want {
			t.Fatalf("burst record %d present=%v, want %v (the newest %d must go)", i, !want, want, tenured)
		}
	}
}

// TestPeerDBSpoofedSubnetBurstEvictsItself: a burst that advertises a
// distinct /16 per identity, against a database whose established peers
// are concentrated in one dense /16, still evicts only itself. Group size
// never decides between a newcomer and an established peer.
func TestPeerDBSpoofedSubnetBurstEvictsItself(t *testing.T) {
	d, clock := openTestPeerDB(t)

	var dense, spread []peer.ID
	for i := 0; i < 1500; i++ { // one dense honest /16
		id := syntheticPeer(1, i)
		d.recordConnected(id, subnetAddr(t, 1, i))
		dense = append(dense, id)
	}
	for i := 0; i < 500; i++ { // and 500 peers in 500 other /16s
		id := syntheticPeer(2, i)
		d.recordConnected(id, wideAddr(t, i))
		spread = append(spread, id)
	}
	establish(clock)
	for _, id := range dense[:100] { // some established peers reconnect recently
		d.recordConnected(id, subnetAddr(t, 1, 0))
	}

	var burst []peer.ID
	for i := 0; i < 100; i++ { // the burst: one identity per spoofed /16
		*clock = clock.Add(time.Second)
		id := syntheticPeer(9, i)
		d.recordConnected(id, []ma.Multiaddr{mustAddr(t, fmt.Sprintf("/ip4/203.%d.0.1/tcp/35995", i))})
		burst = append(burst, id)
	}
	d.expire()

	if got := d.countRecords(); got != peerDBMaxRecords {
		t.Fatalf("%d records after expire, want %d", got, peerDBMaxRecords)
	}
	if d.countPresent(dense) != 1500 || d.countPresent(spread) != 500 {
		t.Fatalf("established peers evicted: dense %d/1500, spread %d/500 remain", d.countPresent(dense), d.countPresent(spread))
	}
	if d.countPresent(burst) != 0 {
		t.Fatalf("%d burst records survived; all 100 must go before any established peer", d.countPresent(burst))
	}
}

// TestPeerDBNewcomersGoNewestFirstAcrossGroups: among newcomers the group
// does not matter; the newest first-seen go first, interleaved across
// groups, and the established records are untouched.
func TestPeerDBNewcomersGoNewestFirstAcrossGroups(t *testing.T) {
	d, clock := openTestPeerDB(t)

	var established []peer.ID
	for group := 1; group <= 3; group++ {
		for i := 0; i < 10; i++ {
			id := syntheticPeer(group, 100000+i)
			d.recordConnected(id, subnetAddr(t, group, 1000+i))
			established = append(established, id)
		}
	}
	establish(clock)

	// Newcomers: A 1000, B 800, C 400, interleaved one second apart, so the
	// global first-seen order mixes the groups.
	sizes := map[int]int{1: 1000, 2: 800, 3: 400}
	var order []peer.ID // global first-seen order
	for i := 0; i < 1000; i++ {
		for group := 1; group <= 3; group++ {
			if i < sizes[group] {
				*clock = clock.Add(time.Second)
				id := syntheticPeer(group, i)
				d.recordConnected(id, subnetAddr(t, group, i))
				order = append(order, id)
			}
		}
	}
	excess := 30 + len(order) - peerDBMaxRecords // 230
	d.expire()
	if got := d.countRecords(); got != peerDBMaxRecords {
		t.Fatalf("%d records after expire, want %d", got, peerDBMaxRecords)
	}
	if d.countPresent(established) != 30 {
		t.Fatal("an established peer was evicted while newcomers remained")
	}
	for i, id := range order {
		if want := i < len(order)-excess; d.has(id) != want {
			t.Fatalf("newcomer %d (of %d) present=%v, want %v: the newest %d go, whatever their group", i, len(order), !want, want, excess)
		}
	}
}

// TestPeerDBEstablishedEvictedFromLargestGroupFirst: with no newcomers, the
// largest groups lose their oldest-lastSeen records first and equalise;
// smaller groups are untouched.
func TestPeerDBEstablishedEvictedFromLargestGroupFirst(t *testing.T) {
	d, clock := openTestPeerDB(t)

	sizes := map[int]int{1: 1010, 2: 810, 3: 400, 4: 10}
	members := map[int][]peer.ID{} // insertion order = lastSeen order (oldest first)
	for group := 1; group <= 4; group++ {
		for i := 0; i < sizes[group]; i++ {
			*clock = clock.Add(time.Second)
			id := syntheticPeer(group, i)
			d.recordConnected(id, subnetAddr(t, group, i))
			members[group] = append(members[group], id)
		}
	}
	establish(clock)
	d.expire() // 2230 -> 2000
	if got := d.countRecords(); got != peerDBMaxRecords {
		t.Fatalf("%d records after expire, want %d", got, peerDBMaxRecords)
	}
	if d.countPresent(members[3]) != 400 || d.countPresent(members[4]) != 10 {
		t.Fatalf("smaller groups touched: C %d/400, D %d/10", d.countPresent(members[3]), d.countPresent(members[4]))
	}
	a, b := d.countPresent(members[1]), d.countPresent(members[2])
	if a+b != 1820-230 {
		t.Fatalf("A and B kept %d+%d, want %d together", a, b, 1820-230)
	}
	if diff := a - b; diff < -1 || diff > 1 {
		t.Fatalf("A kept %d and B %d: the largest group evicts first, so they must equalise", a, b)
	}
	for group, ids := range members { // within a group the oldest lastSeen went
		kept := d.countPresent(ids)
		for i, id := range ids {
			if want := i >= len(ids)-kept; d.has(id) != want {
				t.Fatalf("group %d record %d present=%v, want %v (oldest lastSeen first)", group, i, !want, want)
			}
		}
	}
}

// TestPeerDBAddresslessRecordsAreEvictedLikeAnyOther: records with no
// stored address form their own group (the empty subnet key) and are
// subject to the cap and the group rule like every other group.
func TestPeerDBAddresslessRecordsAreEvictedLikeAnyOther(t *testing.T) {
	// Addressless only: the cap still holds.
	d, clock := openTestPeerDB(t)
	for i := 0; i < peerDBMaxRecords+10; i++ {
		*clock = clock.Add(time.Second)
		d.recordConnected(syntheticPeer(0, i), nil)
	}
	establish(clock)
	d.expire()
	if got := d.countRecords(); got != peerDBMaxRecords {
		t.Fatalf("addressless-only database: %d records after expire, want %d", got, peerDBMaxRecords)
	}
	for i := 0; i < peerDBMaxRecords+10; i++ { // oldest lastSeen first within the group
		if want := i >= 10; d.has(syntheticPeer(0, i)) != want {
			t.Fatalf("addressless record %d present=%v, want %v", i, !want, want)
		}
	}

	// Mixed: the addressless group is the largest and loses the excess.
	d2, clock2 := openTestPeerDB(t)
	var addressed, addressless []peer.ID
	for i := 0; i < 600; i++ {
		id := syntheticPeer(1, i)
		d2.recordConnected(id, subnetAddr(t, 1, i))
		addressed = append(addressed, id)
	}
	for i := 0; i < 1500; i++ {
		id := syntheticPeer(0, i)
		d2.recordConnected(id, nil)
		addressless = append(addressless, id)
	}
	establish(clock2)
	d2.expire() // 2100 -> 2000
	if d2.countPresent(addressed) != 600 || d2.countPresent(addressless) != 1400 {
		t.Fatalf("mixed: addressed %d/600, addressless %d/1500 remain; want 600 and 1400", d2.countPresent(addressed), d2.countPresent(addressless))
	}

	// Mixed the other way: the addressed group is the largest and loses.
	d3, clock3 := openTestPeerDB(t)
	addressed, addressless = nil, nil
	for i := 0; i < 1500; i++ {
		id := syntheticPeer(1, i)
		d3.recordConnected(id, subnetAddr(t, 1, i))
		addressed = append(addressed, id)
	}
	for i := 0; i < 600; i++ {
		id := syntheticPeer(0, i)
		d3.recordConnected(id, nil)
		addressless = append(addressless, id)
	}
	establish(clock3)
	d3.expire()
	if d3.countPresent(addressed) != 1400 || d3.countPresent(addressless) != 600 {
		t.Fatalf("mixed: addressed %d/1500, addressless %d/600 remain; want 1400 and 600", d3.countPresent(addressed), d3.countPresent(addressless))
	}
}

// TestPeerDBEqualSizedGroupsTieBreak: with exactly one record over the cap
// and two equal-sized established groups (plus a third, smaller group that
// is never the victim), the group whose next victim has the older lastSeen
// loses it; on an exact lastSeen tie the lower key goes.
func TestPeerDBEqualSizedGroupsTieBreak(t *testing.T) {
	half := peerDBMaxRecords / 2
	build := func(t *testing.T, group2OlderBy time.Duration) *peerDB {
		d, clock := openTestPeerDB(t)
		d.recordConnected(syntheticPeer(2, 0), subnetAddr(t, 2, 0))
		*clock = clock.Add(group2OlderBy)
		d.recordConnected(syntheticPeer(1, 0), subnetAddr(t, 1, 0))
		*clock = clock.Add(time.Minute)
		for i := 1; i < half; i++ {
			d.recordConnected(syntheticPeer(1, i), subnetAddr(t, 1, i))
			d.recordConnected(syntheticPeer(2, i), subnetAddr(t, 2, i))
		}
		d.recordConnected(syntheticPeer(3, 0), subnetAddr(t, 3, 0)) // 2*half + 1 = one over the cap
		establish(clock)
		d.expire()
		if got := d.countRecords(); got != peerDBMaxRecords {
			t.Fatalf("%d records, want %d", got, peerDBMaxRecords)
		}
		if !d.has(syntheticPeer(3, 0)) {
			t.Fatal("the smaller group must never be the victim")
		}
		return d
	}

	// Group 2's oldest record is a minute older: group 2 loses it.
	d := build(t, time.Minute)
	if d.has(syntheticPeer(2, 0)) || !d.has(syntheticPeer(1, 0)) {
		t.Fatalf("equal-sized groups: the older next victim must go (group 2's); group2-0 present=%v, group1-0 present=%v", d.has(syntheticPeer(2, 0)), d.has(syntheticPeer(1, 0)))
	}

	// Exact lastSeen tie between the two oldest records: the lower key
	// (group 1's) goes.
	d2 := build(t, 0)
	if d2.has(syntheticPeer(1, 0)) || !d2.has(syntheticPeer(2, 0)) {
		t.Fatalf("exact tie: the lower key must go (group 1's); group1-0 present=%v, group2-0 present=%v", d2.has(syntheticPeer(1, 0)), d2.has(syntheticPeer(2, 0)))
	}
}

// TestPeerDBWithinGroupExactTieLowestKeyGoes: inside one group, records
// with identical lastSeen are evicted lowest key first.
func TestPeerDBWithinGroupExactTieLowestKeyGoes(t *testing.T) {
	d, clock := openTestPeerDB(t)
	for i := 0; i < peerDBMaxRecords+3; i++ {
		d.recordConnected(syntheticPeer(1, i), subnetAddr(t, 1, i)) // same instant
	}
	establish(clock)
	d.expire()
	for i := 0; i < peerDBMaxRecords+3; i++ {
		if want := i >= 3; d.has(syntheticPeer(1, i)) != want {
			t.Fatalf("record %d present=%v, want %v (lowest keys go first on an exact tie)", i, !want, want)
		}
	}
}

// TestPeerDBExactTiesAreDeterministic: two databases holding identical
// records with identical timestamps evict identical sets.
func TestPeerDBExactTiesAreDeterministic(t *testing.T) {
	build := func() *peerDB {
		d, clock := openTestPeerDB(t)
		for i := 0; i < peerDBMaxRecords+25; i++ {
			d.recordConnected(syntheticPeer(i%5, i), subnetAddr(t, i%5, i)) // same instant for all
		}
		establish(clock)
		d.expire()
		return d
	}
	a, b := build(), build()
	if a.countRecords() != peerDBMaxRecords || b.countRecords() != peerDBMaxRecords {
		t.Fatalf("counts %d and %d, want %d", a.countRecords(), b.countRecords(), peerDBMaxRecords)
	}
	for i := 0; i < peerDBMaxRecords+25; i++ {
		id := syntheticPeer(i%5, i)
		if a.has(id) != b.has(id) {
			t.Fatalf("record %d survives in one database and not the other: eviction is not deterministic on exact ties", i)
		}
	}
}

// TestPeerDBLegacyRecordsAreEstablished: a record without a first-seen
// stamp predates the stamp, so it is established: stamped newcomers go
// before it, and among established records, legacy included, the largest
// group loses its oldest lastSeen first (a single legacy group therefore
// keeps the previous oldest-lastSeen order).
func TestPeerDBLegacyRecordsAreEstablished(t *testing.T) {
	d, clock := openTestPeerDB(t)
	putLegacy := func(id peer.ID, group, n int, lastSeen int64) {
		raw, err := json.Marshal(&peerDBRecord{Version: peerDBVersion, Addrs: dedupAddrStrings(subnetAddr(t, group, n)), LastSeen: lastSeen})
		if err != nil {
			t.Fatal(err)
		}
		if err := d.db.Put([]byte(id), raw, nil); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 1500; i++ { // legacy, lastSeen increasing, all "recent"
		putLegacy(syntheticPeer(5, i), 5, i, clock.Unix()+int64(i))
	}
	*clock = clock.Add(time.Hour)
	for i := 0; i < 600; i++ { // stamped newcomers in the same group, later
		*clock = clock.Add(time.Second)
		d.recordConnected(syntheticPeer(5, 100000+i), subnetAddr(t, 5, 2000+i))
	}
	d.expire() // 2100 -> 2000: the 100 newest newcomers go, no legacy record
	if got := d.countRecords(); got != peerDBMaxRecords {
		t.Fatalf("%d records, want %d", got, peerDBMaxRecords)
	}
	for i := 0; i < 1500; i++ {
		if !d.has(syntheticPeer(5, i)) {
			t.Fatalf("legacy record %d evicted before a stamped newcomer", i)
		}
	}
	for i := 0; i < 600; i++ {
		if want := i < 500; d.has(syntheticPeer(5, 100000+i)) != want {
			t.Fatalf("newcomer %d present=%v, want %v", i, !want, want)
		}
	}

	// Legacy records are established, so once the newcomers are gone the
	// group rule applies to them: the largest legacy group loses its
	// oldest, not the globally oldest lastSeen. Group A: 1500 legacy
	// records seen recently; group B: 550 legacy records seen long ago;
	// 10 stamped newcomers; 60 over the cap.
	d3, clock3 := openTestPeerDB(t)
	putLegacy3 := func(id peer.ID, group, n int, lastSeen int64) {
		raw, _ := json.Marshal(&peerDBRecord{Version: peerDBVersion, Addrs: dedupAddrStrings(subnetAddr(t, group, n)), LastSeen: lastSeen})
		if err := d3.db.Put([]byte(id), raw, nil); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 550; i++ {
		putLegacy3(syntheticPeer(2, i), 2, i, clock3.Unix()+int64(i)) // old
	}
	for i := 0; i < 1500; i++ {
		putLegacy3(syntheticPeer(1, i), 1, i, clock3.Unix()+100000+int64(i)) // recent
	}
	*clock3 = clock3.Add(200000 * time.Second)
	var newcomers3 []peer.ID
	for i := 0; i < 10; i++ {
		*clock3 = clock3.Add(time.Second)
		id := syntheticPeer(3, i)
		d3.recordConnected(id, subnetAddr(t, 3, i))
		newcomers3 = append(newcomers3, id)
	}
	d3.expire() // 2060 -> 2000: 10 newcomers, then 50 from group A (the largest)
	if d3.countPresent(newcomers3) != 0 {
		t.Fatal("newcomers must go before any legacy record")
	}
	groupA := make([]peer.ID, 0, 1500)
	for i := 0; i < 1500; i++ {
		groupA = append(groupA, syntheticPeer(1, i))
	}
	groupB := make([]peer.ID, 0, 550)
	for i := 0; i < 550; i++ {
		groupB = append(groupB, syntheticPeer(2, i))
	}
	if d3.countPresent(groupA) != 1450 || d3.countPresent(groupB) != 550 {
		t.Fatalf("legacy groups after expire: A %d/1500, B %d/550; want 1450 and 550 (largest group first, not globally oldest lastSeen)", d3.countPresent(groupA), d3.countPresent(groupB))
	}

	// Legacy only, over the cap: the oldest-lastSeen go, as before.
	d2, clock2 := openTestPeerDB(t)
	for i := 0; i < peerDBMaxRecords+10; i++ {
		raw, _ := json.Marshal(&peerDBRecord{Version: peerDBVersion, Addrs: dedupAddrStrings(subnetAddr(t, 6, i)), LastSeen: clock2.Unix() + int64(i)})
		if err := d2.db.Put([]byte(syntheticPeer(6, i)), raw, nil); err != nil {
			t.Fatal(err)
		}
	}
	d2.expire()
	for i := 0; i < peerDBMaxRecords+10; i++ {
		if want := i >= 10; d2.has(syntheticPeer(6, i)) != want {
			t.Fatalf("legacy-only: record %d present=%v, want %v (oldest lastSeen first)", i, !want, want)
		}
	}
}

// TestPeerDBNewcomerTieBreaks: among newcomers the newest first-seen goes
// first even if an older newcomer was refreshed more recently (firstSeen
// never moves); with the same first-seen, the one seen least recently goes
// first; with both stamps equal, the lower key.
func TestPeerDBNewcomerTieBreaks(t *testing.T) {
	// Later arrival versus refreshed older newcomer: one over the cap.
	d, clock := openTestPeerDB(t)
	for i := 0; i < peerDBMaxRecords-1; i++ {
		d.recordConnected(syntheticPeer(1, i), subnetAddr(t, 1, i))
	}
	establish(clock)
	older := syntheticPeer(5, 1)
	d.recordConnected(older, subnetAddr(t, 5, 0))
	*clock = clock.Add(time.Minute)
	later := syntheticPeer(5, 2)
	d.recordConnected(later, subnetAddr(t, 5, 0))
	*clock = clock.Add(time.Minute)
	d.recordConnected(older, subnetAddr(t, 5, 0)) // refreshed: newest lastSeen, still the older arrival
	d.expire()                                    // 2001 -> 2000
	if d.has(later) || !d.has(older) {
		t.Fatalf("the later arrival must go, not the refreshed older newcomer: later=%v older=%v", d.has(later), d.has(older))
	}

	// Same first-seen: oldest lastSeen first, then the lower key. Two over.
	d2, clock2 := openTestPeerDB(t)
	for i := 0; i < peerDBMaxRecords-1; i++ {
		d2.recordConnected(syntheticPeer(1, i), subnetAddr(t, 1, i))
	}
	establish(clock2)
	x, y, z := syntheticPeer(5, 1), syntheticPeer(5, 2), syntheticPeer(5, 3)
	for _, id := range []peer.ID{x, y, z} {
		d2.recordConnected(id, subnetAddr(t, 5, 0)) // same first-seen
	}
	*clock2 = clock2.Add(time.Minute)
	d2.recordConnected(x, subnetAddr(t, 5, 0)) // x: newest lastSeen; y and z tie on both stamps
	d2.expire()                                // 2002 -> 2000
	if !d2.has(x) || d2.has(y) || d2.has(z) {
		t.Fatalf("same first-seen: oldest lastSeen first, then lower key; x=%v y=%v z=%v", d2.has(x), d2.has(y), d2.has(z))
	}
}

// TestPeerDBTenureBoundary: a record is a newcomer while it is younger than
// peerDBTenure and established from exactly peerDBTenure on. The database
// holds the probe plus a cap of established legacy records in another
// group, one over the cap: a newcomer probe goes first; an established
// probe stays and the larger legacy group loses one instead.
func TestPeerDBTenureBoundary(t *testing.T) {
	for _, tc := range []struct {
		name        string
		age         time.Duration
		established bool
	}{
		{"one second short", peerDBTenure - time.Second, false},
		{"exactly the tenure", peerDBTenure, true},
		{"one second past", peerDBTenure + time.Second, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, clock := openTestPeerDB(t)
			probe := syntheticPeer(5, 0)
			d.recordConnected(probe, subnetAddr(t, 5, 0))
			*clock = clock.Add(tc.age)
			for i := 0; i < peerDBMaxRecords; i++ {
				raw, _ := json.Marshal(&peerDBRecord{Version: peerDBVersion, Addrs: dedupAddrStrings(subnetAddr(t, 1, i)), LastSeen: clock.Unix()})
				if err := d.db.Put([]byte(syntheticPeer(1, i)), raw, nil); err != nil {
					t.Fatal(err)
				}
			}
			d.expire()
			if d.has(probe) != tc.established {
				t.Fatalf("age %v: probe present=%v, want established=%v", tc.age, d.has(probe), tc.established)
			}
		})
	}
}
