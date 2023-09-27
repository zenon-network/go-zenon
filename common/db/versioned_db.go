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

type rollbackCache struct {
	frontier types.HashHeight
	raw      db
}

type ldbManager struct {
	location string
	l1Cache  *lru.Cache
	l2Cache  *lru.Cache
	ldb      *leveldb.DB
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
