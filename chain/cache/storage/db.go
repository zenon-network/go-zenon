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
	write   func(*leveldb.Batch) error
	changes sync.Mutex
	stopped bool
}

func NewCacheDBManager(dataDir string) CacheManager {
	opts := &opt.Options{OpenFilesCacheCapacity: getConsensusOpenFilesCacheCapacity()}
	db, err := leveldb.OpenFile(path.Join(dataDir, "cache"), opts)
	common.DealWithErr(err)
	return &cacheManager{
		ldb: db,
		write: func(batch *leveldb.Batch) error {
			return db.Write(batch, nil)
		},
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
	m.changes.Lock()
	defer m.changes.Unlock()
	if m.stopped {
		return leveldb.ErrClosed
	}

	rollbackPatch := db.RollbackPatch(db.NewLevelDBWrapperWithFullDelete(m.ldb).Subset(storageByte), patch)
	batch := new(leveldb.Batch)
	batch.Put(common.JoinBytes(rollbackByte, common.Uint64ToBytes(identifier.Height)), rollbackPatch.Dump())
	if identifier.Height > rollbackCacheSize {
		batch.Delete(common.JoinBytes(rollbackByte, common.Uint64ToBytes(identifier.Height-rollbackCacheSize)))
	}
	if err := db.AppendPatchToLevelDBBatch(batch, storageByte, patch, true); err != nil {
		return err
	}
	if err := m.write(batch); err != nil {
		return err
	}
	// Compact the db manually since the automatic compaction mechanism causes performance issues when throughput increases.
	if identifier.Height%100 == 0 {
		m.ldb.CompactRange(*util.BytesPrefix([]byte{}))
	}
	return nil
}

func (m *cacheManager) Pop() error {
	m.changes.Lock()
	defer m.changes.Unlock()
	if m.stopped {
		return leveldb.ErrClosed
	}

	frontierIdentifier := GetFrontierIdentifier(db.NewLevelDBWrapper(m.ldb).Subset(storageByte))
	rollbackPatch, err := m.getRollback(frontierIdentifier.Height)
	if err != nil {
		return err
	}

	batch := new(leveldb.Batch)
	if err := db.AppendPatchToLevelDBBatch(batch, storageByte, rollbackPatch, true); err != nil {
		return err
	}
	batch.Delete(common.JoinBytes(rollbackByte, common.Uint64ToBytes(frontierIdentifier.Height)))
	return m.write(batch)
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
	snapshot, err := m.ldb.GetSnapshot()
	if err != nil {
		return nil, err
	}
	defer snapshot.Release()

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
