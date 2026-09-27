// Copyright 2015 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

// Contains the node database, storing previously seen nodes and any collected
// metadata about them for QoS purposes.

package discover

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/syndtr/goleveldb/leveldb"
	"github.com/syndtr/goleveldb/leveldb/errors"
	"github.com/syndtr/goleveldb/leveldb/iterator"
	"github.com/syndtr/goleveldb/leveldb/opt"
	"github.com/syndtr/goleveldb/leveldb/storage"
	"github.com/syndtr/goleveldb/leveldb/util"

	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/types"
)

var (
	nodeDBNilNodeID      = NodeID{}       // Special node ID to use as a nil element.
	nodeDBNodeExpiration = 24 * time.Hour // Time after which an unseen node should be dropped.
	nodeDBCleanupCycle   = time.Hour      // Time period for running the expiration task.
)

// nodeDB stores all nodes we know about.
type nodeDB struct {
	lvl    *leveldb.DB       // Interface to the database itself
	seeder iterator.Iterator // Iterator for fetching possible seed nodes

	self NodeID // Own node id to prevent adding it into the database

	// mu orders the expiration sweep against writers: a writer holds it
	// shared for the duration of one put, the sweep holds it exclusively
	// from reading an identity's last pong to deleting its keys. A bond
	// that completes concurrently is therefore either seen by the sweep or
	// written after the sweep has finished with that identity.
	mu sync.RWMutex

	bonded atomic.Bool   // A bond has succeeded, so node records may be expired
	quit   chan struct{} // Channel to signal the expiring thread to stop

	beforeDelete func(id NodeID) // Test seam, called before an identity is deleted by a sweep
}

// Schema layout for the node database
var (
	nodeDBVersionKey = []byte("version") // Version of the database to flush if changes
	nodeDBItemPrefix = []byte("n:")      // Header to prefix node entries with

	nodeDBDiscoverRoot      = ":discover"
	nodeDBDiscoverPing      = nodeDBDiscoverRoot + ":lastping"
	nodeDBDiscoverPong      = nodeDBDiscoverRoot + ":lastpong"
	nodeDBDiscoverFindFails = nodeDBDiscoverRoot + ":findfail"
)

// newNodeDB creates a new node database for storing and retrieving infos about
// known peers in the network. If no path is given, an in-memory, temporary
// database is constructed.
func newNodeDB(path string, version int, self NodeID) (*nodeDB, error) {
	if path == "" {
		return newMemoryNodeDB(self)
	}
	return newPersistentNodeDB(path, version, self)
}

// newMemoryNodeDB creates a new in-memory node database without a persistent
// backend.
func newMemoryNodeDB(self NodeID) (*nodeDB, error) {
	db, err := leveldb.Open(storage.NewMemStorage(), nil)
	if err != nil {
		return nil, err
	}
	return openNodeDB(db, self), nil
}

// newPersistentNodeDB creates/opens a leveldb backed persistent node database,
// also flushing its contents in case of a version mismatch.
func newPersistentNodeDB(path string, version int, self NodeID) (*nodeDB, error) {
	opts := &opt.Options{OpenFilesCacheCapacity: 5}
	db, err := leveldb.OpenFile(path, opts)
	if _, iscorrupted := err.(*errors.ErrCorrupted); iscorrupted {
		db, err = leveldb.RecoverFile(path, nil)
	}
	if err != nil {
		return nil, err
	}
	// The nodes contained in the cache correspond to a certain protocol version.
	// Flush all nodes if the version doesn't match.
	currentVer := make([]byte, binary.MaxVarintLen64)
	currentVer = currentVer[:binary.PutVarint(currentVer, int64(version))]

	blob, err := db.Get(nodeDBVersionKey, nil)
	switch err {
	case leveldb.ErrNotFound:
		// Version not found (i.e. empty cache), insert it
		if err := db.Put(nodeDBVersionKey, currentVer, nil); err != nil {
			db.Close()
			return nil, err
		}

	case nil:
		// Version present, flush if different
		if !bytes.Equal(blob, currentVer) {
			db.Close()
			if err = os.RemoveAll(path); err != nil {
				return nil, err
			}
			return newPersistentNodeDB(path, version, self)
		}
	}
	return openNodeDB(db, self), nil
}

// openNodeDB wraps an opened store and starts its expiration sweep.
func openNodeDB(lvl *leveldb.DB, self NodeID) *nodeDB {
	db := &nodeDB{
		lvl:  lvl,
		self: self,
		quit: make(chan struct{}),
	}
	go db.expirer(nodeDBCleanupCycle)
	return db
}

// makeKey generates the leveldb key-blob from a node id and its particular
// field of interest.
func makeKey(id NodeID, field string) []byte {
	if bytes.Equal(id[:], nodeDBNilNodeID[:]) {
		return []byte(field)
	}
	return append(nodeDBItemPrefix, append(id[:], field...)...)
}

// splitKey tries to split a database key into a node id and a field part.
func splitKey(key []byte) (id NodeID, field string) {
	// If the key is not of a node, return it plainly
	if !bytes.HasPrefix(key, nodeDBItemPrefix) {
		return NodeID{}, string(key)
	}
	// Otherwise split the id and field
	item := key[len(nodeDBItemPrefix):]
	copy(id[:], item[:len(id)])
	field = string(item[len(id):])

	return id, field
}

// fetchInt64 retrieves an integer instance associated with a particular
// database key.
func (db *nodeDB) fetchInt64(key []byte) int64 {
	blob, err := db.lvl.Get(key, nil)
	if err != nil {
		return 0
	}
	val, read := binary.Varint(blob)
	if read <= 0 {
		return 0
	}
	return val
}

// storeInt64 update a specific database entry to the current time instance as a
// unix timestamp.
func (db *nodeDB) storeInt64(key []byte, n int64) error {
	blob := make([]byte, binary.MaxVarintLen64)
	blob = blob[:binary.PutVarint(blob, n)]

	db.mu.RLock()
	defer db.mu.RUnlock()
	return db.lvl.Put(key, blob, nil)
}

// node retrieves a node with a given id from the database.
func (db *nodeDB) node(id NodeID) *Node {
	blob, err := db.lvl.Get(makeKey(id, nodeDBDiscoverRoot), nil)
	if err != nil {
		common.P2PLogger.Debug(fmt.Sprintf("failed to retrieve node %v; reason %v", id, err))
		return nil
	}
	node := new(Node)
	if err := rlp.DecodeBytes(blob, node); err != nil {
		common.P2PLogger.Warn(fmt.Sprintf("failed to decode node RLP: %v", err))
		return nil
	}
	node.sha = types.BytesToHashPanic(crypto.Keccak256(node.ID[:]))
	return node
}

// updateNode inserts - potentially overwriting - a node into the peer database.
func (db *nodeDB) updateNode(node *Node) error {
	blob, err := rlp.EncodeToBytes(node)
	if err != nil {
		return err
	}
	db.mu.RLock()
	defer db.mu.RUnlock()
	return db.lvl.Put(makeKey(node.ID, nodeDBDiscoverRoot), blob, nil)
}

// deleteNode deletes all information/keys associated with a node.
func (db *nodeDB) deleteNode(id NodeID) error {
	// The nil id names the global key namespace, not an identity, and has
	// no keys of its own to delete.
	if id == nodeDBNilNodeID {
		return nil
	}
	deleter := db.lvl.NewIterator(util.BytesPrefix(makeKey(id, "")), nil)
	defer deleter.Release()

	for deleter.Next() {
		if err := db.lvl.Delete(deleter.Key(), nil); err != nil {
			return err
		}
	}
	return deleter.Error()
}

// markBonded records that a bond has succeeded, which lifts the protection
// of node records from expiration.
//
// The sweep itself runs from the moment the database is opened, but node
// records must not be dropped before the network has been bootstrapped, as
// they are the seeds that bootstrapping draws on. Since it would require
// significant overhead to exactly trace the first successful convergence,
// the first successful bonding is taken as that point.
func (db *nodeDB) markBonded() {
	db.bonded.Store(true)
}

// expirer should be started in a go routine, and is responsible for looping ad
// infinitum and dropping stale data from the database.
func (db *nodeDB) expirer(cycle time.Duration) {
	tick := time.NewTicker(cycle)
	defer tick.Stop()
	for {
		select {
		case <-tick.C:
			if err := db.expireNodes(); err != nil {
				common.P2PLogger.Error("Failed to expire nodedb items", "reason", err)
			}

		case <-db.quit:
			return
		}
	}
}

// expireNodes iterates over the database and deletes all nodes that have not
// been seen (i.e. received a pong from) for some alloted time.
//
// An identity is considered whenever any discovery field is stored for it,
// not only when it has a node record: a ping that was never answered leaves
// timing metadata alone, and those entries expire on the same terms as a
// node record whose last pong is old. Until a bond has succeeded, node
// records are kept regardless of age (see markBonded).
func (db *nodeDB) expireNodes() error {
	threshold := time.Now().Add(-nodeDBNodeExpiration)

	// Find discovered nodes that are older than the allowance
	it := db.lvl.NewIterator(nil, nil)
	defer it.Release()

	var (
		lastID  NodeID
		checked bool
	)
	for it.Next() {
		// Skip the item if not a discovery field
		id, field := splitKey(it.Key())
		if !strings.HasPrefix(field, nodeDBDiscoverRoot) {
			continue
		}
		// Skip the global key namespace, which belongs to no identity
		if id == nodeDBNilNodeID {
			continue
		}
		// The fields of one identity are adjacent; decide it once
		if checked && id == lastID {
			continue
		}
		lastID, checked = id, true
		if err := db.expireNode(id, threshold); err != nil {
			return fmt.Errorf("expire node %v: %w", id, err)
		}
	}
	return it.Error()
}

// expireNode deletes all information about an identity unless a pong from it
// is more recent than threshold. Reading the pong and deleting happen under
// the exclusive lock so that no write from a concurrent bond can fall between
// the two.
func (db *nodeDB) expireNode(id NodeID, threshold time.Time) error {
	db.mu.Lock()
	defer db.mu.Unlock()

	// Skip the node if not expired yet (and not self)
	if bytes.Compare(id[:], db.self[:]) != 0 {
		if seen := db.lastPong(id); seen.After(threshold) {
			return nil
		}
		// Until a bond has succeeded, keep node records as seeds
		if !db.bonded.Load() {
			stored, err := db.lvl.Has(makeKey(id, nodeDBDiscoverRoot), nil)
			if err != nil {
				return err
			}
			if stored {
				return nil
			}
		}
	}
	// Otherwise delete all associated information
	if db.beforeDelete != nil {
		db.beforeDelete(id)
	}
	return db.deleteNode(id)
}

// lastPing retrieves the time of the last ping packet send to a remote node,
// requesting binding.
func (db *nodeDB) lastPing(id NodeID) time.Time {
	return time.Unix(db.fetchInt64(makeKey(id, nodeDBDiscoverPing)), 0)
}

// updateLastPing updates the last time we tried contacting a remote node.
func (db *nodeDB) updateLastPing(id NodeID, instance time.Time) error {
	return db.storeInt64(makeKey(id, nodeDBDiscoverPing), instance.Unix())
}

// lastPong retrieves the time of the last successful contact from remote node.
func (db *nodeDB) lastPong(id NodeID) time.Time {
	return time.Unix(db.fetchInt64(makeKey(id, nodeDBDiscoverPong)), 0)
}

// updateLastPong updates the last time a remote node successfully contacted.
func (db *nodeDB) updateLastPong(id NodeID, instance time.Time) error {
	return db.storeInt64(makeKey(id, nodeDBDiscoverPong), instance.Unix())
}

// findFails retrieves the number of findnode failures since bonding.
func (db *nodeDB) findFails(id NodeID) int {
	return int(db.fetchInt64(makeKey(id, nodeDBDiscoverFindFails)))
}

// updateFindFails updates the number of findnode failures since bonding.
func (db *nodeDB) updateFindFails(id NodeID, fails int) error {
	return db.storeInt64(makeKey(id, nodeDBDiscoverFindFails), int64(fails))
}

// querySeeds retrieves a batch of nodes to be used as potential seed servers
// during bootstrapping the node into the network.
//
// Ideal seeds are the most recently seen nodes (highest probability to be still
// alive), but yet untried. However, since leveldb only supports dumb iteration
// we will instead start pulling in potential seeds that haven't been yet pinged
// since the start of the boot procedure.
//
// If the database runs out of potential seeds, we restart the startup counter
// and start iterating over the peers again.
func (db *nodeDB) querySeeds(n int) []*Node {
	// Create a new seed iterator if none exists
	if db.seeder == nil {
		db.seeder = db.lvl.NewIterator(nil, nil)
	}
	// Iterate over the nodes and find suitable seeds
	nodes := make([]*Node, 0, n)
	for len(nodes) < n && db.seeder.Next() {
		// Iterate until a discovery node is found
		id, field := splitKey(db.seeder.Key())
		if field != nodeDBDiscoverRoot {
			continue
		}
		// Dump it if its a self reference
		if bytes.Compare(id[:], db.self[:]) == 0 {
			db.deleteNode(id)
			continue
		}
		// Load it as a potential seed
		if node := db.node(id); node != nil {
			nodes = append(nodes, node)
		}
	}
	// Release the iterator if we reached the end
	if len(nodes) == 0 {
		db.seeder.Release()
		db.seeder = nil
	}
	return nodes
}

// close flushes and closes the database files.
func (db *nodeDB) close() {
	if db.seeder != nil {
		db.seeder.Release()
	}
	close(db.quit)
	db.lvl.Close()
}
