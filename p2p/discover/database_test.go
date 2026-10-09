package discover

import (
	"errors"
	"net"
	"runtime/debug"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/syndtr/goleveldb/leveldb"
)

func newTestNodeID(t *testing.T) NodeID {
	t.Helper()
	priv, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	return PubkeyID(&priv.PublicKey)
}

// newTestNodeDB opens an in-memory database on which a bond has already
// succeeded, so that node records are subject to expiration.
func newTestNodeDB(t *testing.T) *nodeDB {
	t.Helper()
	db, err := newNodeDB("", Version, newTestNodeID(t))
	if err != nil {
		t.Fatal(err)
	}
	db.markBonded()
	return db
}

// hasEntries reports whether any key is stored for id.
func (db *nodeDB) hasEntries(id NodeID) bool {
	it := db.lvl.NewIterator(nil, nil)
	defer it.Release()
	for it.Next() {
		if kid, _ := splitKey(it.Key()); kid == id {
			return true
		}
	}
	return false
}

// aliveIterators reports how many iterators the database still holds open.
func (db *nodeDB) aliveIterators(t *testing.T) int32 {
	t.Helper()
	var stats leveldb.DBStats
	if err := db.lvl.Stats(&stats); err != nil {
		t.Fatal(err)
	}
	return stats.AliveIterators
}

// Retention is decided by the last pong alone. Timing metadata or a failure
// count without a node record is reclaimed whether the ping is old or recent,
// a node record whose pong is old is reclaimed as before, and a recent pong
// keeps everything stored for the identity, with or without a node record.
func TestExpireNodesReclaimsEntriesWithoutNodeRecord(t *testing.T) {
	db := newTestNodeDB(t)
	defer db.close()

	stale := time.Now().Add(-2 * nodeDBNodeExpiration)
	fresh := time.Now()

	unansweredStale := newTestNodeID(t)
	if err := db.updateLastPing(unansweredStale, stale); err != nil {
		t.Fatal(err)
	}
	unansweredFresh := newTestNodeID(t)
	if err := db.updateLastPing(unansweredFresh, fresh); err != nil {
		t.Fatal(err)
	}
	answered := newTestNodeID(t)
	if err := db.updateLastPong(answered, fresh); err != nil {
		t.Fatal(err)
	}
	bonded := newTestNodeID(t)
	bondedNode := newNode(bonded, net.IPv4(10, 0, 0, 2), 30303, 30303)
	if err := db.updateNode(bondedNode); err != nil {
		t.Fatal(err)
	}
	if err := db.updateLastPong(bonded, fresh); err != nil {
		t.Fatal(err)
	}
	gone := newTestNodeID(t)
	if err := db.updateNode(newNode(gone, net.IPv4(10, 0, 0, 3), 30303, 30303)); err != nil {
		t.Fatal(err)
	}
	if err := db.updateLastPong(gone, stale); err != nil {
		t.Fatal(err)
	}
	failed := newTestNodeID(t)
	if err := db.updateFindFails(failed, 1); err != nil {
		t.Fatal(err)
	}

	if err := db.expireNodes(); err != nil {
		t.Fatal(err)
	}
	if db.hasEntries(failed) {
		t.Error("entries for the identity with only a findnode failure count were not reclaimed")
	}
	if db.hasEntries(unansweredStale) {
		t.Error("entries for the identity with an old unanswered ping were not reclaimed")
	}
	if db.hasEntries(unansweredFresh) {
		t.Error("entries for the identity with a recent unanswered ping were not reclaimed")
	}
	if !db.hasEntries(answered) {
		t.Error("entries for the recently answered identity without a node record were removed")
	}
	if db.hasEntries(gone) {
		t.Error("entries for the stale node were not reclaimed")
	}
	node := db.node(bonded)
	if node == nil {
		t.Fatal("node record for the recently seen node was removed")
	}
	if !node.IP.Equal(bondedNode.IP) || node.UDP != bondedNode.UDP || node.TCP != bondedNode.TCP {
		t.Errorf("node record for the recently seen node changed: got %v, want %v", node, bondedNode)
	}
	if got := db.lastPong(bonded); got.Unix() != fresh.Unix() {
		t.Errorf("last pong of the recently seen node changed: got %v, want %v", got.Unix(), fresh.Unix())
	}
}

// Every iterator opened while deleting must be released before the sweep
// returns; a released iterator lets leveldb drop the snapshot it was reading.
// Garbage collection is switched off so that finalizers cannot hide an
// unreleased iterator.
func TestExpireNodesReleasesIterators(t *testing.T) {
	defer debug.SetGCPercent(debug.SetGCPercent(-1))

	db := newTestNodeDB(t)
	defer db.close()

	stale := time.Now().Add(-2 * nodeDBNodeExpiration)
	for i := 0; i < 3; i++ {
		if err := db.updateLastPing(newTestNodeID(t), stale); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.deleteNode(newTestNodeID(t)); err != nil {
		t.Fatal(err)
	}
	if got := db.aliveIterators(t); got != 0 {
		t.Fatalf("iterators left open after deleteNode: %d", got)
	}
	if err := db.expireNodes(); err != nil {
		t.Fatal(err)
	}
	if got := db.aliveIterators(t); got != 0 {
		t.Fatalf("iterators left open after expireNodes: %d", got)
	}
}

// An iterator that stops because of an error must surface that error rather
// than let the sweep report success.
func TestExpireNodesReportsIteratorErrors(t *testing.T) {
	db := newTestNodeDB(t)
	db.close()

	if err := db.deleteNode(newTestNodeID(t)); !errors.Is(err, leveldb.ErrClosed) {
		t.Errorf("deleteNode on a closed database: got %v, want %v", err, leveldb.ErrClosed)
	}
	if err := db.expireNodes(); !errors.Is(err, leveldb.ErrClosed) {
		t.Errorf("expireNodes on a closed database: got %v, want %v", err, leveldb.ErrClosed)
	}
}

// A deletion that fails after the sweep has selected an identity must be
// reported with that identity. The store is closed from the seam, so the
// selection has already happened when the deletion's iterator fails.
func TestExpireNodesReportsDeletionErrors(t *testing.T) {
	db := newTestNodeDB(t)
	defer db.close()

	target := newTestNodeID(t)
	if err := db.updateLastPing(target, time.Now().Add(-2*nodeDBNodeExpiration)); err != nil {
		t.Fatal(err)
	}
	db.beforeDelete = func(id NodeID) {
		if id == target {
			if err := db.lvl.Close(); err != nil {
				t.Fatal(err)
			}
		}
	}

	err := db.expireNodes()
	if !errors.Is(err, leveldb.ErrClosed) {
		t.Fatalf("expireNodes with a failing deletion: got %v, want %v", err, leveldb.ErrClosed)
	}
	if !strings.Contains(err.Error(), target.String()) {
		t.Errorf("deletion error does not name the identity: %v", err)
	}
}

// Until a bond has succeeded, a node record is kept whatever the age of its
// pong, so that stored nodes remain available as seeds, while identities
// without a record are reclaimed. The first successful bond lifts that
// restriction.
func TestExpireNodesKeepsNodeRecordsUntilFirstBond(t *testing.T) {
	db, err := newNodeDB("", Version, newTestNodeID(t))
	if err != nil {
		t.Fatal(err)
	}
	defer db.close()

	stale := time.Now().Add(-2 * nodeDBNodeExpiration)

	pinged := newTestNodeID(t)
	if err := db.updateLastPing(pinged, stale); err != nil {
		t.Fatal(err)
	}
	seed := newTestNodeID(t)
	if err := db.updateNode(newNode(seed, net.IPv4(10, 0, 0, 6), 30303, 30303)); err != nil {
		t.Fatal(err)
	}
	if err := db.updateLastPong(seed, stale); err != nil {
		t.Fatal(err)
	}

	if err := db.expireNodes(); err != nil {
		t.Fatal(err)
	}
	if db.hasEntries(pinged) {
		t.Error("before the first bond: entries for the identity with an unanswered ping were not reclaimed")
	}
	if seeds := db.querySeeds(10); len(seeds) != 1 || seeds[0].ID != seed {
		t.Errorf("before the first bond: stored node is not offered as a seed, got %v", seeds)
	}

	db.markBonded()
	if err := db.expireNodes(); err != nil {
		t.Fatal(err)
	}
	if db.hasEntries(seed) {
		t.Error("after the first bond: entries for the node with an old pong were not reclaimed")
	}
}

// The sweep runs from the moment the database is opened, without waiting for
// a bond to succeed. The cleanup cycle is shortened for the test.
func TestExpirerRunsBeforeFirstBond(t *testing.T) {
	defer func(cycle time.Duration) { nodeDBCleanupCycle = cycle }(nodeDBCleanupCycle)
	nodeDBCleanupCycle = 10 * time.Millisecond

	db, err := newNodeDB("", Version, newTestNodeID(t))
	if err != nil {
		t.Fatal(err)
	}
	defer db.close()

	pinged := newTestNodeID(t)
	if err := db.updateLastPing(pinged, time.Now().Add(-2*nodeDBNodeExpiration)); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for db.hasEntries(pinged) {
		if time.Now().After(deadline) {
			t.Fatal("entries for the identity with an unanswered ping were not reclaimed by the running sweep")
		}
		time.Sleep(nodeDBCleanupCycle)
	}
}

// Discovery fields stored under the nil id sit in the global key namespace,
// next to the version marker, and belong to no identity. A sweep must skip
// them, and deleting the nil id must not touch other keys.
func TestExpireNodesLeavesGlobalKeysAlone(t *testing.T) {
	db, err := newNodeDB(t.TempDir(), Version, newTestNodeID(t))
	if err != nil {
		t.Fatal(err)
	}
	defer db.close()

	stale := time.Now().Add(-2 * nodeDBNodeExpiration)
	fresh := time.Now()

	if err := db.updateLastPing(NodeID{}, stale); err != nil {
		t.Fatal(err)
	}
	if err := db.updateNode(newNode(NodeID{}, net.IPv4(10, 0, 0, 1), 30303, 30303)); err != nil {
		t.Fatal(err)
	}
	bonded := newTestNodeID(t)
	if err := db.updateNode(newNode(bonded, net.IPv4(10, 0, 0, 2), 30303, 30303)); err != nil {
		t.Fatal(err)
	}
	if err := db.updateLastPong(bonded, fresh); err != nil {
		t.Fatal(err)
	}

	check := func(step string) {
		t.Helper()
		if _, err := db.lvl.Get(nodeDBVersionKey, nil); err != nil {
			t.Errorf("%s: version key: %v", step, err)
		}
		if got := db.lastPing(NodeID{}); got.Unix() != stale.Unix() {
			t.Errorf("%s: ping time under the nil id changed: got %v, want %v", step, got.Unix(), stale.Unix())
		}
		if db.node(NodeID{}) == nil {
			t.Errorf("%s: node record under the nil id was removed", step)
		}
		if db.node(bonded) == nil {
			t.Errorf("%s: node record for the recently seen node was removed", step)
		}
		if got := db.lastPong(bonded); got.Unix() != fresh.Unix() {
			t.Errorf("%s: last pong of the recently seen node changed: got %v, want %v", step, got.Unix(), fresh.Unix())
		}
	}
	if err := db.deleteNode(NodeID{}); err != nil {
		t.Fatal(err)
	}
	check("after deleteNode of the nil id")
	if err := db.expireNodes(); err != nil {
		t.Fatal(err)
	}
	check("after expireNodes")
}

// A bond that completes while the sweep is deciding about its identity must
// keep its pong and node record, whether the identity had only an unanswered
// ping or a node record with an old pong. The seam runs after the sweep has
// read the last pong and before it deletes. The bond's writes are started
// there, and the seam waits until the writer has reached its first write and
// then until the writes have either landed, which the sweep's lock must not
// allow, or been held for a second.
func TestExpireNodesKeepsBondCompletedDuringSweep(t *testing.T) {
	db := newTestNodeDB(t)
	defer db.close()

	stale := time.Now().Add(-2 * nodeDBNodeExpiration)
	fresh := time.Now()

	pinged := newTestNodeID(t)
	if err := db.updateLastPing(pinged, stale); err != nil {
		t.Fatal(err)
	}
	known := newTestNodeID(t)
	if err := db.updateNode(newNode(known, net.IPv4(10, 0, 0, 5), 30303, 30303)); err != nil {
		t.Fatal(err)
	}
	if err := db.updateLastPong(known, stale); err != nil {
		t.Fatal(err)
	}
	type bond struct {
		name       string
		attempting chan struct{} // closed when the writer is about to write
		written    chan error    // receives the result once both writes are done
	}
	targets := map[NodeID]*bond{
		pinged: {"identity with an unanswered ping", make(chan struct{}), make(chan error, 1)},
		known:  {"node record with an old pong", make(chan struct{}), make(chan error, 1)},
	}

	db.beforeDelete = func(id NodeID) {
		target, ok := targets[id]
		if !ok {
			return
		}
		go func() {
			close(target.attempting)
			if err := db.updateLastPong(id, fresh); err != nil {
				target.written <- err
				return
			}
			target.written <- db.updateNode(newNode(id, net.IPv4(10, 0, 0, 4), 30303, 30303))
		}()
		<-target.attempting
		select {
		case err := <-target.written:
			target.written <- err
			t.Errorf("%s: the bond's writes landed while the sweep was deciding about it", target.name)
		case <-time.After(time.Second):
		}
	}

	if err := db.expireNodes(); err != nil {
		t.Fatal(err)
	}
	for id, target := range targets {
		select {
		case <-target.attempting:
		default:
			t.Errorf("%s: the sweep never reached the seam, so the bond was not started", target.name)
			continue
		}
		if err := <-target.written; err != nil {
			t.Fatal(err)
		}
		if got := db.lastPong(id); got.Unix() != fresh.Unix() {
			t.Errorf("%s: last pong of the bond completed during the sweep: got %v, want %v", target.name, got.Unix(), fresh.Unix())
		}
		if db.node(id) == nil {
			t.Errorf("%s: node record of the bond completed during the sweep was removed", target.name)
		}
	}
}
