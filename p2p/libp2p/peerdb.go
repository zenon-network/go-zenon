package libp2p

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
	ma "github.com/multiformats/go-multiaddr"
	"github.com/syndtr/goleveldb/leveldb"
	lerrors "github.com/syndtr/goleveldb/leveldb/errors"

	"github.com/zenon-network/go-zenon/common"
)

const (
	peerDBVersion = 1

	// peerDBMaxAge is how long a peer is considered a warm-bootstrap
	// candidate after we last completed a Zenon handshake with it.
	// This — not libp2p's 30-minute post-disconnect address TTL — is
	// what bounds how long a node can stay offline and still rejoin
	// without a current bootstrap list.
	peerDBMaxAge = 30 * 24 * time.Hour

	// peerDBMaxFailCount drops a peer from the candidate set after
	// this many consecutive dial failures without a successful
	// connect in between. Generous because dial failures can also be
	// transient local conditions (no route, FD pressure).
	peerDBMaxFailCount = 16

	// peerDBMaxAddrsPerPeer caps stored addresses per peer so a peer
	// advertising garbage via identify can't bloat its record.
	peerDBMaxAddrsPerPeer = 8

	// peerDBCleanupCycle is how often the server's cleanup loop calls
	// expire() on a running node; expire also runs once at open.
	peerDBCleanupCycle = 1 * time.Hour

	// peerDBMaxRecords bounds the total number of stored peer records,
	// enforced by expire(). Without it, an attacker cheaply cycling
	// ephemeral peer identities through the handshake could grow the
	// on-disk database without bound. The pass runs hourly, once at
	// open, and on the write path once peerDBMaxRecords new records have
	// been created since the last pass, so a normal recordConnected call
	// stays a single get+put and the database holds at most 2*cap-1
	// records between calls (the 2*cap-th insert runs the pass). Over
	// the cap, expire evicts newcomers (first seen within peerDBTenure)
	// first, newest first, whatever their subnet, so a burst of fresh
	// identities evicts itself before any established peer; established
	// records are then evicted from the largest subnet group (subnetKey)
	// first, oldest lastSeen first, which balances the advertised groups:
	// a long-lived squatter concentrated in one source loses records
	// before established peers elsewhere, and a genuinely large group
	// loses its oldest first. Subnet labels are self-advertised, so among
	// established peers this is a heuristic, not a guarantee.
	peerDBMaxRecords = 2000

	// peerDBTenure is how long a record must have existed to count as
	// established for eviction. Records first seen more recently are
	// newcomers and go first when the database is over the cap. A day is
	// well past any burst a single pass can see, and an attacker must
	// keep an identity recorded that long before it ranks with
	// established peers. Records written before the firstSeen stamp
	// existed are established: they necessarily predate it.
	peerDBTenure = 24 * time.Hour
)

// peerDB is the Zenon-owned, LevelDB-backed record of peers this node
// has successfully run the Zenon protocol with. It exists because the
// libp2p peerstore cannot serve as a restart-survival mechanism: its
// address records are managed by the identify subsystem, which
// downgrades them to a 30-minute TTL on disconnect — so a persisted
// libp2p peerstore is empty for any restart after >30min of downtime
// (or any graceful shutdown followed by a slow restart). Records here
// have no libp2p-managed lifetime; we decide when they expire.
//
// Same design lineage as the legacy stack's p2p/discover/database.go
// nodeDB (own LevelDB, last-seen stamps, fail counters, age expiry),
// reshaped for libp2p: keyed by peer.ID, storing multiaddrs.
type peerDB struct {
	db *leveldb.DB

	// mu serializes read-modify-write cycles on individual records.
	// goleveldb is internally concurrency-safe, but our updates
	// (get → mutate → put) are not atomic without it.
	mu sync.Mutex

	// now is the clock used for lastSeen stamps and expiry cutoffs;
	// injectable so tests can drive expiry without sleeping.
	now func() time.Time

	// newSinceExpire counts records created since the last expire pass;
	// recordConnected runs a pass when it reaches peerDBMaxRecords.
	// Guarded by mu.
	newSinceExpire int
}

// peerDBRecord is the stored value, JSON-encoded. Addresses are kept
// as multiaddr strings; entries that fail to re-parse on read are
// skipped individually.
type peerDBRecord struct {
	Version   int      `json:"v"`
	Addrs     []string `json:"addrs"`
	LastSeen  int64    `json:"lastSeen"` // unix seconds
	FailCount int      `json:"failCount"`
	// FirstSeen is when the record was created (unix seconds); it is
	// never refreshed, so it measures tenure (see peerDBTenure). Records
	// written before the field existed carry zero and count as
	// established: they necessarily predate it.
	FirstSeen int64 `json:"firstSeen,omitempty"`
}

// openPeerDB opens (creating if needed) the peer database at path and
// runs one expiry pass. Corrupted databases are recovered rather than
// failing startup, mirroring the legacy nodeDB's behaviour — losing
// warm-bootstrap candidates is strictly better than refusing to boot.
func openPeerDB(path string) (*peerDB, error) {
	db, err := leveldb.OpenFile(path, nil)
	if _, corrupted := err.(*lerrors.ErrCorrupted); corrupted {
		db, err = leveldb.RecoverFile(path, nil)
	}
	if err != nil {
		return nil, err
	}
	d := &peerDB{db: db, now: time.Now}
	d.expire()
	return d, nil
}

func (d *peerDB) Close() error {
	return d.db.Close()
}

// recordConnected upserts a peer we just completed a Zenon handshake
// with (or are cleanly parting from): refreshes lastSeen, resets the
// fail counter, and replaces the stored addresses when a non-empty set
// is provided. An empty addrs set keeps any previously stored
// addresses — identify may not have completed when the first adoption
// hook fires, and the disconnect-time refresh fills them in later. A
// new record is stamped firstSeen and counted; the peerDBMaxRecords-th
// new record since the last expire pass runs one inline, so the cap
// holds between the hourly passes.
func (d *peerDB) recordConnected(id peer.ID, addrs []ma.Multiaddr) {
	d.mu.Lock()
	defer d.mu.Unlock()

	now := d.now().Unix()
	rec := d.get(id)
	created := rec == nil
	if created {
		rec = &peerDBRecord{Version: peerDBVersion, FirstSeen: now}
	}
	if dialable := dedupAddrStrings(addrs); len(dialable) > 0 {
		rec.Addrs = dialable
	}
	rec.LastSeen = now
	rec.FailCount = 0
	d.put(id, rec)

	if created {
		d.newSinceExpire++
		if d.newSinceExpire >= peerDBMaxRecords {
			d.expireLocked()
		}
	}
}

// recordDialFail bumps the consecutive-failure counter for a known
// peer. Unknown peers are ignored — the database only tracks peers
// that have completed a Zenon handshake at least once.
func (d *peerDB) recordDialFail(id peer.ID) {
	d.mu.Lock()
	defer d.mu.Unlock()

	rec := d.get(id)
	if rec == nil {
		return
	}
	rec.FailCount++
	d.put(id, rec)
}

// subnetKey buckets a peer's addresses into a group so seeds() can spread
// candidates across sources rather than let one source dominate a round:
// IPv4 by /16 prefix (the granularity libp2p's own peerdiversity filter
// uses), IPv6 by /48 prefix, and every peer whose addresses carry no
// literal IP into one shared DNS group. Peers with no address information
// at all share the empty key and form a group of their own.
func subnetKey(addrs []ma.Multiaddr) string {
	for _, a := range addrs {
		ip, _, err := parseMultiaddrIPPort(a)
		if err != nil || ip == nil {
			continue
		}
		if ip4 := ip.To4(); ip4 != nil {
			return fmt.Sprintf("4:%d.%d", ip4[0], ip4[1])
		}
		if ip16 := ip.To16(); ip16 != nil {
			return fmt.Sprintf("6:%x", ip16[:6])
		}
	}
	// DNS-only peers all share one key rather than being keyed by their
	// hostname: the stored addresses are self-advertised (identify), so a
	// hostname-derived key would let one source mint a fresh group — and
	// therefore a fresh slot per round — for every identity it cycles
	// through the handshake. One shared key caps all of them at one slot
	// per round, while still keeping them out of the "no address
	// information at all" group.
	for _, a := range addrs {
		for _, proto := range []int{ma.P_DNS, ma.P_DNS4, ma.P_DNS6, ma.P_DNSADDR} {
			if _, err := a.ValueForProtocol(proto); err == nil {
				return "dns"
			}
		}
	}
	return ""
}

// seeds returns up to n warm-bootstrap candidates, skipping records
// that are expired, failed-out, or have no usable addresses. Records
// that don't parse as ours (e.g. leftovers from the pstoreds schema
// this database replaced) are deleted on sight, so a pre-existing
// directory self-cleans.
//
// Candidates are grouped by subnet (see subnetKey) and selected
// round-robin across groups, most-recently-seen group first, rather
// than taking a pure recency-ranked prefix — so a source that cycles
// many identities through the handshake can occupy at most one slot per
// round instead of owning the whole seed list.
func (d *peerDB) seeds(n int) []peer.AddrInfo {
	d.mu.Lock()
	defer d.mu.Unlock()

	type candidate struct {
		info     peer.AddrInfo
		lastSeen int64
	}
	var cands []candidate
	cutoff := d.now().Add(-peerDBMaxAge).Unix()

	iter := d.db.NewIterator(nil, nil)
	defer iter.Release()
	for iter.Next() {
		var rec peerDBRecord
		if err := json.Unmarshal(iter.Value(), &rec); err != nil || rec.Version != peerDBVersion {
			key := append([]byte(nil), iter.Key()...)
			_ = d.db.Delete(key, nil)
			continue
		}
		if rec.LastSeen < cutoff || rec.FailCount > peerDBMaxFailCount || len(rec.Addrs) == 0 {
			continue
		}
		info := peer.AddrInfo{ID: peer.ID(string(iter.Key()))}
		for _, s := range rec.Addrs {
			if a, err := ma.NewMultiaddr(s); err == nil {
				info.Addrs = append(info.Addrs, a)
			}
		}
		if len(info.Addrs) == 0 {
			continue
		}
		cands = append(cands, candidate{info: info, lastSeen: rec.LastSeen})
	}

	sort.Slice(cands, func(i, j int) bool { return cands[i].lastSeen > cands[j].lastSeen })

	// Group by subnet, preserving each group's most-recently-seen-first
	// order and the recency order of groups themselves (first sighting
	// in the already-sorted cands slice).
	groups := make(map[string][]candidate)
	var groupOrder []string
	for _, c := range cands {
		key := subnetKey(c.info.Addrs)
		if _, ok := groups[key]; !ok {
			groupOrder = append(groupOrder, key)
		}
		groups[key] = append(groups[key], c)
	}

	var out []peer.AddrInfo
	for {
		if n > 0 && len(out) >= n {
			break
		}
		progressed := false
		for _, key := range groupOrder {
			if n > 0 && len(out) >= n {
				break
			}
			remaining := groups[key]
			if len(remaining) == 0 {
				continue
			}
			out = append(out, remaining[0].info)
			groups[key] = remaining[1:]
			progressed = true
		}
		if !progressed {
			break
		}
	}
	return out
}

// expire deletes records that are older than peerDBMaxAge, failed out,
// or unparseable, then evicts survivors down to peerDBMaxRecords if the
// database is still over that cap. Newcomers (first seen within
// peerDBTenure) go first, newest first, whatever subnet they advertise:
// a burst of fresh identities, from one source or spread across spoofed
// subnets, evicts itself before any established peer. Only when no
// newcomer remains are established records evicted, from the largest
// subnet group first and, within a group, the oldest lastSeen first.
// That balances the advertised groups: a long-lived squatter
// concentrated in one source loses records before established peers
// elsewhere, and a genuinely large group loses its oldest first; the
// labels are self-advertised, so among established peers this is a
// heuristic, not a guarantee. Ties fall back to the record key, so a
// pass over the same records always deletes the same ones. Called once
// at open, on the server's peerDBCleanupCycle ticker, and by
// recordConnected once peerDBMaxRecords new records have accumulated.
func (d *peerDB) expire() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.expireLocked()
}

// expireLocked is expire with d.mu held.
func (d *peerDB) expireLocked() {
	d.newSinceExpire = 0

	type kept struct {
		key       []byte
		group     string
		firstSeen int64
		lastSeen  int64
	}
	var survivors []kept

	now := d.now()
	cutoff := now.Add(-peerDBMaxAge).Unix()
	iter := d.db.NewIterator(nil, nil)
	for iter.Next() {
		var rec peerDBRecord
		if err := json.Unmarshal(iter.Value(), &rec); err == nil &&
			rec.Version == peerDBVersion &&
			rec.LastSeen >= cutoff &&
			rec.FailCount <= peerDBMaxFailCount {
			addrs := make([]ma.Multiaddr, 0, len(rec.Addrs))
			for _, a := range rec.Addrs {
				if m, err := ma.NewMultiaddr(a); err == nil {
					addrs = append(addrs, m)
				}
			}
			survivors = append(survivors, kept{
				key:       append([]byte(nil), iter.Key()...),
				group:     subnetKey(addrs),
				firstSeen: rec.FirstSeen,
				lastSeen:  rec.LastSeen,
			})
			continue
		}
		key := append([]byte(nil), iter.Key()...)
		_ = d.db.Delete(key, nil)
	}
	iter.Release()

	excess := len(survivors) - peerDBMaxRecords
	if excess <= 0 {
		return
	}

	// Tier 1: newcomers, newest first; a zero stamp is established.
	tenureCutoff := now.Add(-peerDBTenure).Unix()
	var newcomers, established []kept
	for _, k := range survivors {
		if k.firstSeen != 0 && k.firstSeen > tenureCutoff {
			newcomers = append(newcomers, k)
		} else {
			established = append(established, k)
		}
	}
	sort.Slice(newcomers, func(i, j int) bool {
		a, b := newcomers[i], newcomers[j]
		if a.firstSeen != b.firstSeen {
			return a.firstSeen > b.firstSeen
		}
		if a.lastSeen != b.lastSeen {
			return a.lastSeen < b.lastSeen
		}
		return bytes.Compare(a.key, b.key) < 0
	})
	for _, k := range newcomers {
		if excess == 0 {
			return
		}
		_ = d.db.Delete(k.key, nil)
		excess--
	}

	// Tier 2: established records, from the largest subnet group, the
	// oldest lastSeen within it; among equal-sized groups the one whose
	// next victim is the oldest, then the lower key.
	evictsBefore := func(a, b kept) bool {
		if a.lastSeen != b.lastSeen {
			return a.lastSeen < b.lastSeen
		}
		return bytes.Compare(a.key, b.key) < 0
	}
	groups := make(map[string][]kept)
	for _, k := range established {
		groups[k.group] = append(groups[k.group], k)
	}
	keys := make([]string, 0, len(groups))
	for key, members := range groups {
		sort.Slice(members, func(i, j int) bool { return evictsBefore(members[i], members[j]) })
		groups[key] = members
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for ; excess > 0; excess-- {
		victim := -1
		for i, key := range keys {
			members := groups[key]
			if len(members) == 0 {
				continue
			}
			if victim < 0 || len(members) > len(groups[keys[victim]]) ||
				(len(members) == len(groups[keys[victim]]) && evictsBefore(members[0], groups[keys[victim]][0])) {
				victim = i
			}
		}
		if victim < 0 {
			return
		}
		key := keys[victim]
		_ = d.db.Delete(groups[key][0].key, nil)
		groups[key] = groups[key][1:]
	}
}

// get returns the stored record for id, or nil if absent/unparseable.
// Caller must hold d.mu.
func (d *peerDB) get(id peer.ID) *peerDBRecord {
	data, err := d.db.Get([]byte(id), nil)
	if err != nil {
		return nil
	}
	var rec peerDBRecord
	if json.Unmarshal(data, &rec) != nil || rec.Version != peerDBVersion {
		return nil
	}
	return &rec
}

// put writes the record for id. Write failures are logged rather than
// propagated: the peer database is an optimization, and no caller is
// in a position to do anything about a failed write. Caller must hold
// d.mu.
func (d *peerDB) put(id peer.ID, rec *peerDBRecord) {
	data, err := json.Marshal(rec)
	if err != nil {
		return
	}
	if err := d.db.Put([]byte(id), data, nil); err != nil {
		common.P2PLogger.Warn("peer database write failed", "err", err)
	}
}

// dedupAddrStrings stringifies, dedups, and caps a multiaddr set.
func dedupAddrStrings(addrs []ma.Multiaddr) []string {
	seen := make(map[string]struct{}, len(addrs))
	out := make([]string, 0, len(addrs))
	for _, a := range addrs {
		if a == nil {
			continue
		}
		s := a.String()
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
		if len(out) == peerDBMaxAddrsPerPeer {
			break
		}
	}
	return out
}
