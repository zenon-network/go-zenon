// Package storage implements the consensus database: a flat,
// non-versioned key-value store (in production a dedicated LevelDB
// under <DataDir>/consensus) holding the two artifacts the consensus
// module derives from the chain — election results keyed by
// proof-momentum hash and pillar statistic points keyed by tick
// height. Everything stored here is recomputable from the ledger, so
// the database is never rolled back: the points system deletes and
// regenerates points whose momentums were reorganized away, and
// election results stay valid because their proof hash pins the exact
// chain state they were derived from.
package storage

import (
	"encoding/binary"

	lru "github.com/hashicorp/golang-lru"
	"github.com/pkg/errors"
	"github.com/syndtr/goleveldb/leveldb"

	"github.com/zenon-network/go-zenon/common/db"
	"github.com/zenon-network/go-zenon/common/types"
)

// Key prefixes of the consensus database.
const (
	// PrefixPeriodPoint keys points aggregated per election tick.
	PrefixPeriodPoint = byte(0)
	// PrefixEpochPoint keys points aggregated per 24-hour epoch tick.
	PrefixEpochPoint = byte(1)
	// NumPointTypes is the number of point key prefixes. Point
	// prefixes double as indexes into the DB's point caches, so they
	// must stay smaller than this.
	NumPointTypes = 2
	// PrefixElectionResult keys election results by the hash of
	// their proof momentum.
	PrefixElectionResult = byte(10)
)

// DB is the consensus database: an underlying key-value store with
// an LRU cache in front of every key namespace — one cache per point
// prefix plus one for election results. NewConsensus sizes the
// caches to hold roughly one week of election ticks.
type DB struct {
	db            db.DB
	electionCache *lru.Cache
	pointCache    []*lru.Cache
}

// NewConsensusDB wraps db with fresh LRU caches: one of
// electionCacheSize entries for election results and one of
// pointCacheSize entries for each of the NumPointTypes point
// prefixes.
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

// GetPointByHeight returns the point stored for the given tick
// height under prefix (PrefixPeriodPoint or PrefixEpochPoint),
// reading through the prefix's cache. It returns nil, nil when no
// point is stored.
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

// DeletePointByHeight removes the point of the given tick height
// from both the cache and the database. The points system uses it to
// invalidate points whose momentums were reorganized away.
func (db *DB) DeletePointByHeight(prefix byte, height uint64) error {
	db.pointCache[prefix].Remove(height)
	return db.db.Delete(CreatePointKey(prefix, height))
}

// StorePointByHeight persists the point of the given tick height
// under prefix and, once written, caches it.
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

// GetElectionResultByHash returns the election result stored for the
// proof momentum with the given hash, reading through the election
// cache. It returns nil, nil when no result is stored.
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

// StoreElectionResultByHash persists the election result computed
// from the proof momentum with the given hash and, once written,
// caches it.
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

// CreateElectionResultKey returns the database key of the election
// result anchored at the proof momentum with the given hash:
// PrefixElectionResult (1 byte) + hash (32 bytes).
func CreateElectionResultKey(hash types.Hash) []byte {
	key := make([]byte, 1+types.HashSize)
	key[0] = PrefixElectionResult
	copy(key[1:types.HashSize+1], hash.Bytes())
	return key
}

// CreatePointKey returns the database key of the point at the given
// tick height: prefix (1 byte) + big-endian height (8 bytes).
func CreatePointKey(prefix byte, height uint64) []byte {
	key := make([]byte, 1+8)
	key[0] = prefix
	binary.BigEndian.PutUint64(key[1:9], height)
	return key
}
