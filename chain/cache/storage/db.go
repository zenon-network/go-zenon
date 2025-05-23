package storage

import (
	"path"
	"runtime"
	"sync"

	"github.com/syndtr/goleveldb/leveldb"
	"github.com/syndtr/goleveldb/leveldb/opt"
	"github.com/syndtr/goleveldb/leveldb/util"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/db"
	"github.com/zenon-network/go-zenon/common/types"
)

const (
	rollbackCacheSize = 100
)

var (
	storageByte  = []byte{85}
	rollbackByte = []byte{119}
)

func getConsensusOpenFilesCacheCapacity() int {
	switch runtime.GOOS {
	case "darwin":
		return 20
	case "windows":
		return 200
	default:
		return 200
	}
}

type CacheManager interface {
	DB() db.DB

	Add(types.HashHeight, db.Patch) error
	Pop() error

	Stop() error
}

type cacheManager struct {
	ldb     *leveldb.DB
	changes sync.Mutex
	stopped bool
}

func NewCacheDBManager(dataDir string) CacheManager {
	opts := &opt.Options{OpenFilesCacheCapacity: getConsensusOpenFilesCacheCapacity()}
	db, err := leveldb.OpenFile(path.Join(dataDir, "cache"), opts)
	common.DealWithErr(err)
	return &cacheManager{
		ldb: db,
	}
}

func GetRollbackCacheSize() int {
	return rollbackCacheSize
}

func GetFrontierIdentifier(db db.DB) types.HashHeight {
	data, err := db.Get(frontierIdentifierKey)
	if err == leveldb.ErrNotFound {
		return types.ZeroHashHeight
	}
	common.DealWithErr(err)
	hh, err := types.DeserializeHashHeight(data)
	common.DealWithErr(err)
	return *hh
}

func (m *cacheManager) DB() db.DB {
	m.changes.Lock()
	defer m.changes.Unlock()
	if m.stopped {
		return nil
	}
	return db.NewLevelDBWrapper(m.ldb).Subset(storageByte)
}

func (m *cacheManager) Add(identifier types.HashHeight, patch db.Patch) error {
	temp := db.NewMemDB()
	if err := temp.Put(frontierIdentifierKey, identifier.Serialize()); err != nil {
		return err
	}
	frontierPatch, err := temp.Changes()
	if err != nil {
		return err
	}
	if err := frontierPatch.Replay(patch); err != nil {
		return err
	}
	rollbackPatch := db.RollbackPatch(m.DB(), patch)

	m.changes.Lock()
	defer m.changes.Unlock()

	if err := m.ldb.Put(common.JoinBytes(rollbackByte, common.Uint64ToBytes(identifier.Height)), rollbackPatch.Dump(), nil); err != nil {
		return err
	}
	if identifier.Height > rollbackCacheSize {
		if err := m.ldb.Delete(common.JoinBytes(rollbackByte, common.Uint64ToBytes(identifier.Height-rollbackCacheSize)), nil); err != nil {
			return err
		}
	}
	if err := db.ApplyPatch(db.NewLevelDBWrapperWithFullDelete(m.ldb).Subset(storageByte), patch); err != nil {
		return err
	}
	// Compact the db manually since the automatic compaction mechanism causes performance issues when throughput increases.
	if identifier.Height%100 == 0 {
		m.ldb.CompactRange(*util.BytesPrefix([]byte{}))
	}
	return nil
}

func (m *cacheManager) Pop() error {
	frontierIdentifier := GetFrontierIdentifier(m.DB())
	rollbackPatch, err := m.getRollback(frontierIdentifier.Height)
	if err != nil {
		return err
	}

	m.changes.Lock()
	defer m.changes.Unlock()

	if err := db.ApplyPatch(db.NewLevelDBWrapperWithFullDelete(m.ldb).Subset(storageByte), rollbackPatch); err != nil {
		return err
	}
	if err := m.ldb.Delete(common.JoinBytes(rollbackByte, common.Uint64ToBytes(frontierIdentifier.Height)), nil); err != nil {
		return err
	}
	return nil
}

func (m *cacheManager) Stop() error {
	m.changes.Lock()
	defer m.changes.Unlock()
	if err := m.ldb.Close(); err != nil {
		return err
	}
	m.stopped = true
	m.ldb = nil
	return nil
}

func (m *cacheManager) getRollback(height uint64) (db.Patch, error) {
	snapshot, _ := m.ldb.GetSnapshot()
	value, err := snapshot.Get(common.JoinBytes(rollbackByte, common.Uint64ToBytes(height)), nil)
	if err != nil {
		return nil, err
	}

	patch, err := db.NewPatchFromDump(value)
	if err != nil {
		return nil, err
	}
	return patch, nil
}
