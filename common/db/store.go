package db

import (
	"github.com/syndtr/goleveldb/leveldb"

	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/types"
)

func getFrontierIdentifierKey() []byte {
	return frontierIdentifierKey
}
func getHeightByHashKey(hash types.Hash) []byte {
	return common.JoinBytes(heightByHashPrefix, hash.Bytes())
}
func getEntryByHeightKey(height uint64) []byte {
	return common.JoinBytes(entryByHeightPrefix, common.Uint64ToBytes(height))
}

// SetFrontier records version bookkeeping in db: it sets version as
// the frontier identifier, indexes its height by its hash and stores
// data (the serialized commit — an account block or momentum) under
// its height. Manager implementations call it once per Commit, so the
// version's own entries are part of the version's state and roll back
// with it.
func SetFrontier(db DB, version types.HashHeight, data []byte) error {
	if err := db.Put(getFrontierIdentifierKey(), version.Serialize()); err != nil {
		return err
	}
	if err := db.Put(getHeightByHashKey(version.Hash), common.Uint64ToBytes(version.Height)); err != nil {
		return err
	}
	if err := db.Put(getEntryByHeightKey(version.Height), data); err != nil {
		return err
	}
	return nil
}

// GetFrontierIdentifier returns the identifier of the latest version
// recorded in db, or types.ZeroHashHeight for a database that has no
// frontier yet. It panics on any storage error other than not-found.
func GetFrontierIdentifier(db DB) types.HashHeight {
	data, err := db.Get(getFrontierIdentifierKey())
	if err == leveldb.ErrNotFound {
		return types.ZeroHashHeight
	}
	common.DealWithErr(err)
	hh, err := types.DeserializeHashHeight(data)
	common.DealWithErr(err)
	return *hh
}

// GetIdentifierByHash resolves hash to a full hash-height identifier
// using the index written by SetFrontier. It returns
// leveldb.ErrNotFound if the hash is not part of this version's
// chain.
func GetIdentifierByHash(db DB, hash types.Hash) (*types.HashHeight, error) {
	heightData, err := db.Get(getHeightByHashKey(hash))
	if err != nil {
		return nil, err
	}
	height := common.BytesToUint64(heightData)
	return &types.HashHeight{
		Height: height,
		Hash:   hash,
	}, nil
}

// GetEntryByHash returns the serialized commit entry whose hash is
// hash, by first resolving the hash to its height. It returns
// leveldb.ErrNotFound if the hash is unknown.
func GetEntryByHash(db DB, hash types.Hash) ([]byte, error) {
	heightData, err := db.Get(getHeightByHashKey(hash))
	if err != nil {
		return nil, err
	}
	height := common.BytesToUint64(heightData)
	return GetEntryByHeight(db, height)
}

// GetEntryByHeight returns the serialized commit entry stored at
// height by SetFrontier, or leveldb.ErrNotFound if the version
// contains no entry at that height. The ledger stores deserialize the
// result into account blocks or momentums.
func GetEntryByHeight(db DB, height uint64) ([]byte, error) {
	return db.Get(getEntryByHeightKey(height))
}
