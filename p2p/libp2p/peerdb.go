package libp2p

import (
	"encoding/json"
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
}

// peerDBRecord is the stored value, JSON-encoded. Addresses are kept
// as multiaddr strings; entries that fail to re-parse on read are
// skipped individually.
type peerDBRecord struct {
	Version   int      `json:"v"`
	Addrs     []string `json:"addrs"`
	LastSeen  int64    `json:"lastSeen"` // unix seconds
	FailCount int      `json:"failCount"`
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
// hook fires, and the disconnect-time refresh fills them in later.
func (d *peerDB) recordConnected(id peer.ID, addrs []ma.Multiaddr) {
	d.mu.Lock()
	defer d.mu.Unlock()

	rec := d.get(id)
	if rec == nil {
		rec = &peerDBRecord{Version: peerDBVersion}
	}
	if dialable := dedupAddrStrings(addrs); len(dialable) > 0 {
		rec.Addrs = dialable
	}
	rec.LastSeen = d.now().Unix()
	rec.FailCount = 0
	d.put(id, rec)
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

// seeds returns up to n warm-bootstrap candidates, most recently seen
// first, skipping records that are expired, failed-out, or have no
// usable addresses. Records that don't parse as ours (e.g. leftovers
// from the pstoreds schema this database replaced) are deleted on
// sight, so a pre-existing directory self-cleans.
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
	if n > 0 && len(cands) > n {
		cands = cands[:n]
	}
	out := make([]peer.AddrInfo, len(cands))
	for i, c := range cands {
		out[i] = c.info
	}
	return out
}

// expire deletes records that are older than peerDBMaxAge, failed out,
// or unparseable. Called once at open and then on the server's
// peerDBCleanupCycle ticker.
func (d *peerDB) expire() {
	d.mu.Lock()
	defer d.mu.Unlock()

	cutoff := d.now().Add(-peerDBMaxAge).Unix()
	iter := d.db.NewIterator(nil, nil)
	defer iter.Release()
	for iter.Next() {
		var rec peerDBRecord
		if err := json.Unmarshal(iter.Value(), &rec); err == nil &&
			rec.Version == peerDBVersion &&
			rec.LastSeen >= cutoff &&
			rec.FailCount <= peerDBMaxFailCount {
			continue
		}
		key := append([]byte(nil), iter.Key()...)
		_ = d.db.Delete(key, nil)
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
