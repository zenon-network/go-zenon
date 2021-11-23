package storage

import (
	"encoding/binary"

	lru "github.com/hashicorp/golang-lru"
	"github.com/pkg/errors"
	"github.com/syndtr/goleveldb/leveldb"

	"github.com/zenon-network/go-zenon/common/db"
	"github.com/zenon-network/go-zenon/common/types"
)

const (
	PrefixPeriodPoint = byte(0)
	PrefixEpochPoint  = byte(1)
	// Total number of possible points
	NumPointTypes        = 2
	PrefixElectionResult = byte(10)
)

type DB struct {
	db            db.DB
	electionCache *lru.Cache
	pointCache    []*lru.Cache
}

func NewConsensusDB(db db.DB, electionCacheSize int, pointCacheSize int) *DB {
	electionCache, err := lru.New(electionCacheSize)
	if err != nil {
		panic(err)
	}

	pointCache := make([]*lru.Cache, NumPointTypes)
	for i := 0; i < NumPointTypes; i += 1 {
		pointCache[i], err = lru.New(pointCacheSize)
		if err != nil {
			panic(err)
		}
	}

	return &DB{
		db:            db,
		electionCache: electionCache,
		pointCache:    pointCache,
	}
}

// Point
func (db *DB) GetPointByHeight(prefix byte, height uint64) (*Point, error) {
	// Get from cache
	cacheValue, ok := db.pointCache[prefix].Get(height)
	if ok {
		return cacheValue.(*Point), nil
	}

	// Get from DB
	key := CreatePointKey(prefix, height)
	value, err := db.db.Get(key)
	if err != nil {
		if err == leveldb.ErrNotFound {
			return nil, nil
		}
		return nil, err
	}

	// Create object from bytes
	data := &Point{}
	err = data.Unmarshal(value)
	if err != nil {
		return nil, errors.Errorf("error Unmarshal Point prefix %v height %v reason %e", prefix, height, err)
	}

	// Update cache
	db.pointCache[prefix].Add(height, data)
	return data, nil
}
func (db *DB) DeletePointByHeight(prefix byte, height uint64) error {
	db.pointCache[prefix].Remove(height)
	return db.db.Delete(CreatePointKey(prefix, height))
}
func (db *DB) StorePointByHeight(prefix byte, height uint64, p *Point) error {
	bytes, err := p.Marshal()
	if err != nil {
		return err
	}
	err = db.db.Put(CreatePointKey(prefix, height), bytes)
	if err != nil {
		return err
	}

	// Saved to DB ok - Update cache
	db.pointCache[prefix].Add(height, p)
	return nil
}

// ElectionData
func (db *DB) GetElectionResultByHash(hash types.Hash) (*ElectionData, error) {
	// Get from cache
	cacheValue, ok := db.electionCache.Get(hash)
	if ok {
		return cacheValue.(*ElectionData), nil
	}

	// Get from DB
	key := CreateElectionResultKey(hash)
	value, err := db.db.Get(key)
	if err != nil {
		if err == leveldb.ErrNotFound {
			return nil, nil
		}
		return nil, err
	}

	// Create object from bytes
	data := &ElectionData{}
	err = data.Unmarshal(value)
	if err != nil {
		return nil, errors.Errorf("error Unmarshal ElectionData hash %v reason %e", hash, err)
	}

	// Update cache
	db.electionCache.Add(hash, data)
	return data, nil
}
func (db *DB) StoreElectionResultByHash(hash types.Hash, data *ElectionData) error {
	bytes, err := data.Marshal()
	if err != nil {
		return err
	}
	err = db.db.Put(CreateElectionResultKey(hash), bytes)
	if err != nil {
		return err
	}

	// Saved to DB ok - Update cache
	db.electionCache.Add(hash, data)
	return nil
}

func CreateElectionResultKey(hash types.Hash) []byte {
	key := make([]byte, 1+types.HashSize)
	key[0] = PrefixElectionResult
	copy(key[1:types.HashSize+1], hash.Bytes())
	return key
}
func CreatePointKey(prefix byte, height uint64) []byte {
	key := make([]byte, 1+8)
	key[0] = prefix
	binary.BigEndian.PutUint64(key[1:9], height)
	return key
}
