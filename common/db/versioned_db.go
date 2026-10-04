package db

import (
	"runtime"
	"sort"
	"sync"

	lru "github.com/hashicorp/golang-lru"
	"github.com/pkg/errors"
	"github.com/syndtr/goleveldb/leveldb"
	"github.com/syndtr/goleveldb/leveldb/opt"

	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/types"
)

const (
	l1CacheSize                  = 400
	l2CacheSize                  = 100
	maximumCacheHeightDifference = 360
)

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

type Manager interface {
	Frontier() DB
	Get(types.HashHeight) DB
	GetPatch(identifier types.HashHeight) Patch

	Add(Transaction) error
	Pop() error

	// Rebase moves the stable floor of the manager to a new stable DB.
	// Versions at or below the new stable height are discarded; versions
	// above it are preserved.  This lets a caller advance the stable
	// reference without re-creating the manager and re-applying patches.
	// It is a no-op for managers that are their own stable store.
	//
	// Rebase returns an error if the new stable DB is not on the same
	// chain as the manager's current state (i.e. its frontier hash does
	// not match the manager's version at that height), or if any pending
	// version cannot be rebuilt on the new stable.
	Rebase(newStableDB DB) error

	Stop() error
	Location() string
}

type memdbManager struct {
	stableDB           DB
	stableIdentifier   types.HashHeight
	frontierIdentifier types.HashHeight
	previous           map[types.HashHeight]types.HashHeight
	versions           map[types.HashHeight]DB
	patches            map[types.HashHeight]Patch

	changes sync.Mutex
}

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
func (m *memdbManager) Pop() error {
	m.changes.Lock()
	defer m.changes.Unlock()
	if m.stableIdentifier == m.frontierIdentifier {
		return errors.Errorf("can't rollback stable db")
	}

	previous, ok := m.previous[m.frontierIdentifier]
	if !ok {
		return errors.Errorf("can't find previous for %v", m.frontierIdentifier)
	}

	// Remove intermediate batched commits that share the popped head's DB.
	// Without this cleanup the intermediate entries survive Pop, leaving
	// stale references to the orphaned DB object.
	poppedDB := m.versions[m.frontierIdentifier]
	for id, verDB := range m.versions {
		if id != m.frontierIdentifier && verDB == poppedDB {
			delete(m.versions, id)
			delete(m.patches, id)
		}
	}

	delete(m.previous, m.frontierIdentifier)
	delete(m.versions, m.frontierIdentifier)
	delete(m.patches, m.frontierIdentifier)
	m.frontierIdentifier = previous
	return nil
}

// Rebase moves the stable floor of the manager to a new stable DB.
// Versions at or below the new stable height are discarded; versions
// above it are preserved but their overlay chains are rebuilt directly
// on top of the new stable DB.
//
// Without the rebuild, each pending version's mergedDb would retain a
// reference to the deleted committed overlay as its read-through base,
// so the overlay chain would grow by one level per committed block and
// never shrink, causing unbounded memory growth across momentums.
func (m *memdbManager) Rebase(newStableDB DB) error {
	m.changes.Lock()
	defer m.changes.Unlock()
	newStableIdentifier := GetFrontierIdentifier(newStableDB)
	newStableHeight := newStableIdentifier.Height

	// Validate that the new stable DB is on the same chain: if the manager
	// already has any version at the new stable height, its hash must match.
	// A forked stable at the same height with a different hash would
	// silently re-parent pending heads onto a different chain.
	for id := range m.versions {
		if id.Height == newStableHeight && id != newStableIdentifier {
			return errors.Errorf(
				"rebase: new stable frontier %v conflicts with existing version %v at same height",
				newStableIdentifier, id)
		}
	}

	// Advance the frontier identifier if the current frontier is at or
	// below the new stable height.  Without this, a manager whose frontier
	// was discarded by the rebase would return nil from Frontier().
	if m.frontierIdentifier.Height <= newStableHeight {
		m.frontierIdentifier = newStableIdentifier
	}

	// Capture pending versions before mutating maps.  A pending version is
	// any version above the new stable height.  Head commits carry a
	// m.previous link to the preceding head (or the old stable); intermediate
	// batched commits share the same DB object as their batch's head and
	// have no m.previous entry.
	type pendingVersion struct {
		id      types.HashHeight
		origDB  DB
		patch   Patch
		prev    types.HashHeight
		hasPrev bool
	}
	var pending []pendingVersion
	for id, verDB := range m.versions {
		if id.Height > newStableHeight {
			prev, hasPrev := m.previous[id]
			pending = append(pending, pendingVersion{
				id:      id,
				origDB:  verDB,
				patch:   m.patches[id],
				prev:    prev,
				hasPrev: hasPrev,
			})
		}
	}
	sort.Slice(pending, func(i, j int) bool { return pending[i].id.Height < pending[j].id.Height })

	// Discard committed versions; they are now served from the stable store.
	for id := range m.versions {
		if id.Height <= newStableHeight && id != newStableIdentifier {
			delete(m.versions, id)
			delete(m.previous, id)
			delete(m.patches, id)
		}
	}

	// Rebuild each pending version's overlay chain on top of the new stable
	// DB.  Head commits are rebuilt by snapshotting from their (already
	// rebuilt) previous version and replaying their stored patch.  Versions
	// that shared the same original DB (batched commits) continue to share
	// the same rebuilt DB, preserving the invariant established by Add.
	rebuilt := make(map[types.HashHeight]DB, len(pending))
	rebuiltByOrig := make(map[DB]DB, len(pending))
	var deferred []pendingVersion

	for _, p := range pending {
		if !p.hasPrev {
			// Intermediate batched commit; resolved after its head.
			deferred = append(deferred, p)
			continue
		}
		var base DB
		if p.prev.Height <= newStableHeight {
			base = newStableDB
			// The head is being re-parented onto the new stable; record
			// the link so a later Pop() does not land on a deleted id.
			m.previous[p.id] = newStableIdentifier
		} else {
			base = rebuilt[p.prev]
			if base == nil {
				return errors.Errorf(
					"rebase: pending version %v previous %v not rebuilt",
					p.id, p.prev)
			}
		}
		newDB := base.Snapshot()
		if p.patch != nil {
			common.DealWithErr(newDB.Apply(p.patch))
		}
		rebuilt[p.id] = newDB
		rebuiltByOrig[p.origDB] = newDB
		m.versions[p.id] = newDB
	}

	for _, p := range deferred {
		if newDB, ok := rebuiltByOrig[p.origDB]; ok {
			m.versions[p.id] = newDB
		} else {
			// The batch head that shared this DB was popped before Rebase;
			// the intermediate is orphaned.  Remove it so no stale reference
			// to the old DB survives.
			delete(m.versions, p.id)
			delete(m.patches, p.id)
		}
	}

	// A fresh manager has no previous or patch for its stable; delete any
	// stale entries so GetPatch(stable) returns nil as it does on dev.
	delete(m.previous, newStableIdentifier)
	delete(m.patches, newStableIdentifier)

	m.stableDB = newStableDB
	m.stableIdentifier = newStableIdentifier
	m.versions[newStableIdentifier] = newStableDB
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

type rollbackCache struct {
	frontier types.HashHeight
	raw      db
}

type ldbManager struct {
	location string
	l1Cache  *lru.Cache
	l2Cache  *lru.Cache
	ldb      *leveldb.DB
	write    func(*leveldb.Batch) error
	changes  sync.Mutex
	stopped  bool
}

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
		write: func(batch *leveldb.Batch) error {
			return ldb.Write(batch, nil)
		},
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
	return enableDelete(u, false)
}
func (m *ldbManager) GetPatch(identifier types.HashHeight) Patch {
	m.changes.Lock()
	defer m.changes.Unlock()
	if m.stopped {
		return nil
	}
	return m.getPatch(identifier)
}
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
	batch := new(leveldb.Batch)
	batch.Put(common.JoinBytes(patchByte, common.Uint64ToBytes(identifier.Height)), patch.Dump())
	batch.Put(common.JoinBytes(rollbackByte, common.Uint64ToBytes(identifier.Height)), rollbackPatch.Dump())
	if err := AppendPatchToLevelDBBatch(batch, frontierByte, patch, false); err != nil {
		return err
	}

	m.changes.Lock()
	defer m.changes.Unlock()
	if m.stopped {
		return errors.Errorf("can't add transaction to stopped db")
	}

	// Compare against the real disk frontier; `db` is a historical view at
	// `previous`, so its own frontier identifier always equals `previous`.
	frontierIdentifier := GetFrontierIdentifier(NewLevelDBWrapper(m.ldb).Subset(frontierByte))
	if previous != frontierIdentifier {
		return errors.Errorf("can't insert identifier %v. previous %v doesn't match with current frontier %v", identifier, previous, frontierIdentifier)
	}
	return m.write(batch)
}
func (m *ldbManager) Pop() error {
	m.changes.Lock()
	defer m.changes.Unlock()
	if m.stopped {
		return errors.Errorf("can't pop stopped db")
	}

	frontierIdentifier := GetFrontierIdentifier(NewLevelDBWrapper(m.ldb).Subset(frontierByte))
	rollbackData, err := m.ldb.Get(common.JoinBytes(rollbackByte, common.Uint64ToBytes(frontierIdentifier.Height)), nil)
	if err != nil {
		return err
	}
	rollbackPatch, err := NewPatchFromDump(rollbackData)
	if err != nil {
		return err
	}

	batch := new(leveldb.Batch)
	if err := AppendPatchToLevelDBBatch(batch, frontierByte, rollbackPatch, false); err != nil {
		return err
	}
	batch.Delete(common.JoinBytes(patchByte, common.Uint64ToBytes(frontierIdentifier.Height)))
	batch.Delete(common.JoinBytes(rollbackByte, common.Uint64ToBytes(frontierIdentifier.Height)))
	if err := m.write(batch); err != nil {
		return err
	}
	// Each cached overlay records the frontier it was built against and Get
	// replays only the rollbacks above that height. The frontier now sits
	// below every cached one and the next Add reuses the popped height, so
	// an overlay kept here would skip the rollbacks of whatever is added
	// there and show that branch's writes at its identifier.
	m.l1Cache.Purge()
	m.l2Cache.Purge()
	return nil
}
func (m *ldbManager) Rebase(_ DB) error {
	// ldbManager IS the stable store; there is nothing to rebase.
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
