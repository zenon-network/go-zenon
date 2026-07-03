package db

import (
	"runtime"
	"sync"

	lru "github.com/hashicorp/golang-lru"
	"github.com/pkg/errors"
	"github.com/syndtr/goleveldb/leveldb"
	"github.com/syndtr/goleveldb/leveldb/opt"

	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/types"
)

const (
	// l1CacheSize and l2CacheSize are the entry counts of the two LRU
	// caches of reconstructed historical views kept by the
	// LevelDB-backed Manager: l1 holds versions close to the frontier
	// (the common case during syncing and RPC queries), l2 the
	// occasional deep look-back.
	l1CacheSize = 400
	l2CacheSize = 100
	// maximumCacheHeightDifference is the frontier distance (in
	// heights) below which a reconstructed version is cached in l1
	// rather than l2.
	maximumCacheHeightDifference = 360
)

// Top-level key-space prefixes inside the Manager's LevelDB: the
// current (frontier) state lives under frontierByte, while patchByte
// and rollbackByte hold, per height, the serialized patch that
// produced that version and its precomputed inverse.
var (
	frontierByte = []byte{85}
	patchByte    = []byte{102}
	rollbackByte = []byte{119}
)

func absDiff(x, y uint64) uint64 {
	if x < y {
		return y - x
	}
	return x - y
}

// getOpenFilesCacheCapacity returns the LevelDB open-files cache size
// for NewLevelDBManager, reduced on Darwin because of macOS's low
// default file-descriptor limits.
func getOpenFilesCacheCapacity() int {
	switch runtime.GOOS {
	case "darwin":
		return 100
	case "windows":
		return 200
	default:
		return 200
	}
}

// Manager is the versioned database underneath each ledger (the
// momentum chain and every account chain). Every version is
// identified by the hash-height of the Commit that produced it.
//
// Frontier returns a snapshot of the latest version and Get one of an
// arbitrary committed version (nil if the identifier is unknown);
// both are copy-on-write, so writing to them never affects the
// manager. GetPatch returns the state patch that was applied at the
// given identifier (nil if unknown). Add appends a Transaction on top
// of the current frontier and Pop rolls the frontier back one
// version.
//
// Individual methods are internally synchronized, but Add and Pop
// perform unlocked read-then-write sequences, so writers must be
// externally serialized — in practice the chain's insert lock is the
// single writer.
type Manager interface {
	Frontier() DB
	Get(types.HashHeight) DB
	GetPatch(identifier types.HashHeight) Patch

	Add(Transaction) error
	Pop() error

	Stop() error
	Location() string
}

// memdbManager is the in-memory Manager: every version is kept alive
// in the versions map as a copy-on-write overlay over its parent, so
// Get is a map lookup plus Snapshot. previous links each version to
// its parent for Pop, and patches keeps the patch applied at each
// identifier. stableDB/stableIdentifier mark the base version the
// manager started from, below which Pop refuses to go.
type memdbManager struct {
	stableDB           DB
	stableIdentifier   types.HashHeight
	frontierIdentifier types.HashHeight
	previous           map[types.HashHeight]types.HashHeight
	versions           map[types.HashHeight]DB
	patches            map[types.HashHeight]Patch

	changes sync.Mutex
}

// NewMemDBManager returns an in-memory Manager rooted at the current
// frontier of rawDB; that version becomes the stable base that cannot
// be popped. Writes are layered over rawDB in memory and are never
// persisted. The account pool uses one per account chain to track
// blocks that are not yet confirmed by a momentum.
func NewMemDBManager(rawDB DB) Manager {
	frontierIdentifier := GetFrontierIdentifier(rawDB)
	return &memdbManager{
		stableDB:           rawDB,
		stableIdentifier:   frontierIdentifier,
		frontierIdentifier: frontierIdentifier,
		previous:           map[types.HashHeight]types.HashHeight{},
		versions:           map[types.HashHeight]DB{frontierIdentifier: rawDB},
		patches:            map[types.HashHeight]Patch{},
	}
}

func (m *memdbManager) Frontier() DB {
	m.changes.Lock()
	frontierIdentifier := m.frontierIdentifier
	m.changes.Unlock()
	return m.Get(frontierIdentifier)
}
func (m *memdbManager) Get(identifier types.HashHeight) DB {
	m.changes.Lock()
	defer m.changes.Unlock()
	db, ok := m.versions[identifier]
	if ok {
		return db.Snapshot()
	}
	return nil
}
func (m *memdbManager) GetPatch(identifier types.HashHeight) Patch {
	m.changes.Lock()
	defer m.changes.Unlock()
	return m.patches[identifier]
}

// Add applies the transaction's patch — extended with the frontier
// bookkeeping of each commit (see SetFrontier) — on a snapshot of the
// previous version and registers the result as the new frontier.
// Intermediate commits map to the same DB, with an empty patch. It
// fails if the transaction's previous identifier is not the current
// frontier.
func (m *memdbManager) Add(transaction Transaction) error {
	commits := transaction.GetCommits()
	previous := commits[0].Previous()
	head := commits[len(commits)-1].Identifier()

	if previous != m.frontierIdentifier {
		return errors.Errorf("can't insert identifier %v. previous doesn't match with current frontier %v", head, m.frontierIdentifier)
	}

	// apply transaction on db
	db := m.Get(previous)
	if db == nil {
		return errors.Errorf("can't find prev")
	}

	patch := transaction.StealChanges()

	for _, commit := range commits {
		temp := NewMemDB()
		data, err := commit.Serialize()
		if err != nil {
			return err
		}
		if err := SetFrontier(temp, commit.Identifier(), data); err != nil {
			return err
		}
		frontierPatch, err := temp.Changes()
		if err != nil {
			return err
		}
		if err := frontierPatch.Replay(patch); err != nil {
			return err
		}
		if err := db.Apply(patch); err != nil {
			return err
		}
	}

	m.changes.Lock()
	defer m.changes.Unlock()

	m.frontierIdentifier = head
	m.previous[head] = previous
	m.versions[head] = db
	m.patches[head] = patch

	for _, commit := range commits[:len(commits)-1] {
		m.versions[commit.Identifier()] = db
		m.patches[commit.Identifier()] = NewPatch()
	}

	return nil
}

// Pop discards the frontier version and makes its parent the new
// frontier. It fails when the frontier is the stable base the manager
// was created from.
func (m *memdbManager) Pop() error {
	m.changes.Lock()
	defer m.changes.Unlock()
	if m.stableIdentifier == m.frontierIdentifier {
		return errors.Errorf("can't rollback stable db")
	}

	previous, ok := m.previous[m.frontierIdentifier]
	if !ok {
		return errors.Errorf("can't find previous for ")
	}

	delete(m.previous, m.frontierIdentifier)
	delete(m.versions, m.frontierIdentifier)
	delete(m.patches, m.frontierIdentifier)
	m.frontierIdentifier = previous
	return nil
}
func (m *memdbManager) Stop() error {
	m.frontierIdentifier = types.ZeroHashHeight
	m.versions = nil
	m.patches = nil
	return nil
}
func (m *memdbManager) Location() string {
	return "in-memory"
}

// rollbackCache is one cached historical reconstruction: raw holds
// the rollback overlay accumulated so far and frontier records the
// frontier it has been folded up to, so a later Get only needs to
// apply the rollback patches of newer heights.
type rollbackCache struct {
	frontier types.HashHeight
	raw      db
}

// ldbManager is the persistent Manager. The LevelDB holds only the
// frontier state plus, for every height, the patch that produced it
// and its precomputed inverse (see the key-space prefixes above);
// historical versions are reconstructed on demand in Get and memoized
// in the l1/l2 LRU caches. The changes mutex serializes individual
// methods; after Stop, the read methods return nil.
type ldbManager struct {
	location string
	l1Cache  *lru.Cache
	l2Cache  *lru.Cache
	ldb      *leveldb.DB
	changes  sync.Mutex
	stopped  bool
}

// NewLevelDBManager opens (or creates) the versioned database in dir
// and returns the persistent Manager backed by it. It panics if the
// database cannot be opened. This is the production manager for both
// the momentum ledger and the stable account ledgers.
func NewLevelDBManager(dir string) Manager {
	opts := &opt.Options{OpenFilesCacheCapacity: getOpenFilesCacheCapacity()}
	ldb, err := leveldb.OpenFile(dir, opts)
	common.DealWithErr(err)
	l1Cache, err := lru.New(l1CacheSize)
	common.DealWithErr(err)
	l2Cache, err := lru.New(l2CacheSize)
	common.DealWithErr(err)
	return &ldbManager{
		location: dir,
		l1Cache:  l1Cache,
		l2Cache:  l2Cache,
		ldb:      ldb,
	}
}

func (m *ldbManager) Frontier() DB {
	m.changes.Lock()
	defer m.changes.Unlock()
	if m.stopped {
		return nil
	}
	snapshot, _ := m.ldb.GetSnapshot()
	return NewLevelDBSnapshotWrapper(snapshot).Subset(frontierByte)
}

// Get reconstructs the state at identifier by starting from a
// LevelDB snapshot of the frontier and folding in the rollback
// patches of every height above identifier, in ascending order with
// first-write-wins semantics (ApplyWithoutOverride), which is
// equivalent to undoing them newest-first. The accumulated overlay is
// memoized per identifier — in l1Cache for versions within
// maximumCacheHeightDifference of the frontier, in l2Cache otherwise
// — so subsequent calls only fold the heights added since. It returns
// nil for unknown identifiers, an empty DB for the zero identifier,
// and the frontier view when identifier is the frontier itself.
func (m *ldbManager) Get(identifier types.HashHeight) DB {
	m.changes.Lock()
	defer m.changes.Unlock()
	if m.stopped {
		return nil
	}
	snapshot, _ := m.ldb.GetSnapshot()
	// check if has snapshot
	frontier := NewLevelDBSnapshotWrapper(snapshot).Subset(frontierByte)
	frontierIdentifier := GetFrontierIdentifier(frontier)

	if identifier.IsZero() {
		return NewMemDB()
	}
	if identifier == frontierIdentifier {
		return frontier
	}

	trueIdentifier, err := GetIdentifierByHash(frontier, identifier.Hash)
	if err == leveldb.ErrNotFound {
		return nil
	}
	common.DealWithErr(err)
	if *trueIdentifier != identifier {
		return nil
	}

	var rawChanges db
	var toIdentifier types.HashHeight

	if cache, ok := m.l1Cache.Get(identifier); ok {
		toIdentifier = cache.(*rollbackCache).frontier
		rawChanges = cache.(*rollbackCache).raw
	} else if cache, ok := m.l2Cache.Get(identifier); ok {
		toIdentifier = cache.(*rollbackCache).frontier
		rawChanges = cache.(*rollbackCache).raw
	} else {
		rawChanges = newMemDBInternal()
		toIdentifier = identifier
	}

	for i := toIdentifier.Height + 1; i <= frontierIdentifier.Height; i += 1 {
		rollback := m.getRollback(i)
		if err := ApplyWithoutOverride(rawChanges, rollback); err != nil {
			common.DealWithErr(err)
		}
	}

	if absDiff(identifier.Height, frontierIdentifier.Height) < maximumCacheHeightDifference {
		m.l1Cache.Add(identifier, &rollbackCache{
			frontier: frontierIdentifier,
			raw:      rawChanges,
		})
	} else {
		m.l2Cache.Add(identifier, &rollbackCache{
			frontier: frontierIdentifier,
			raw:      rawChanges,
		})
	}

	u := newMergedDb([]db{
		newMemDBInternal(),
		newSkipDelete(
			newMergedDb([]db{
				rawChanges,
				newSubDB(frontierByte, newLevelDBSnapshotWrapper(snapshot)),
			})),
	})
	return enableDelete(u)
}
func (m *ldbManager) GetPatch(identifier types.HashHeight) Patch {
	m.changes.Lock()
	defer m.changes.Unlock()
	if m.stopped {
		return nil
	}
	return m.getPatch(identifier)
}

// getPatch loads the persisted patch for identifier's height; note
// that the lookup is by height only, the hash is not checked.
func (m *ldbManager) getPatch(identifier types.HashHeight) Patch {
	snapshot, _ := m.ldb.GetSnapshot()
	value, err := snapshot.Get(common.JoinBytes(patchByte, common.Uint64ToBytes(identifier.Height)), nil)
	if err == leveldb.ErrNotFound {
		return nil
	}
	common.DealWithErr(err)

	patch, err := NewPatchFromDump(value)
	common.DealWithErr(err)
	return patch
}

// getRollback loads the persisted inverse patch of the given height,
// or nil if none is stored.
func (m *ldbManager) getRollback(height uint64) Patch {
	snapshot, _ := m.ldb.GetSnapshot()
	value, err := snapshot.Get(common.JoinBytes(rollbackByte, common.Uint64ToBytes(height)), nil)
	if err == leveldb.ErrNotFound {
		return nil
	}
	common.DealWithErr(err)

	patch, err := NewPatchFromDump(value)
	common.DealWithErr(err)
	return patch
}

// Add extends the transaction's patch with the frontier bookkeeping
// of each commit, computes its inverse against the previous version
// and, if the transaction builds on the current frontier, persists
// the patch and its rollback by height and applies the patch to the
// frontier state. A transaction whose previous identifier is not the
// frontier is silently ignored (nil error) — unlike the in-memory
// manager, which reports it.
func (m *ldbManager) Add(transaction Transaction) error {
	commits := transaction.GetCommits()

	previous := commits[0].Previous()
	identifier := commits[len(commits)-1].Identifier()

	// apply transaction on db
	db := m.Get(previous)
	if db == nil {
		return errors.Errorf("can't find prev")
	}

	patch := transaction.StealChanges()

	for _, commit := range commits {
		temp := NewMemDB()
		data, err := commit.Serialize()
		if err != nil {
			return err
		}
		if err := SetFrontier(temp, commit.Identifier(), data); err != nil {
			return err
		}
		frontierPatch, err := temp.Changes()
		if err != nil {
			return err
		}
		if err := frontierPatch.Replay(patch); err != nil {
			return err
		}
	}

	rollbackPatch := RollbackPatch(db, patch)

	m.changes.Lock()
	defer m.changes.Unlock()

	frontierIdentifier := GetFrontierIdentifier(db)

	if previous == frontierIdentifier {
		if err := m.ldb.Put(common.JoinBytes(patchByte, common.Uint64ToBytes(identifier.Height)), patch.Dump(), nil); err != nil {
			return err
		}
		if err := m.ldb.Put(common.JoinBytes(rollbackByte, common.Uint64ToBytes(identifier.Height)), rollbackPatch.Dump(), nil); err != nil {
			return err
		}
		if err := ApplyPatch(NewLevelDBWrapper(m.ldb).Subset(frontierByte), patch); err != nil {
			return err
		}
	}
	return nil
}

// Pop undoes the frontier version by applying its persisted rollback
// patch to the frontier state and deleting the patch/rollback records
// of that height.
func (m *ldbManager) Pop() error {
	frontierIdentifier := GetFrontierIdentifier(m.Frontier())
	rollbackPatch := m.getRollback(frontierIdentifier.Height)

	if err := ApplyPatch(NewLevelDBWrapper(m.ldb).Subset(frontierByte), rollbackPatch); err != nil {
		return err
	}
	if err := m.ldb.Delete(common.JoinBytes(patchByte, common.Uint64ToBytes(frontierIdentifier.Height)), nil); err != nil {
		return err
	}
	if err := m.ldb.Delete(common.JoinBytes(rollbackByte, common.Uint64ToBytes(frontierIdentifier.Height)), nil); err != nil {
		return err
	}

	return nil
}
func (m *ldbManager) Stop() error {
	m.changes.Lock()
	defer m.changes.Unlock()
	if err := m.ldb.Close(); err != nil {
		return err
	}
	m.stopped = true
	m.ldb = nil
	m.l1Cache = nil
	m.l2Cache = nil
	return nil
}
func (m *ldbManager) Location() string {
	return m.location
}
