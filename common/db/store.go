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
func GetEntryByHash(db DB, hash types.Hash) ([]byte, error) {
	heightData, err := db.Get(getHeightByHashKey(hash))
	if err != nil {
		return nil, err
	}
	height := common.BytesToUint64(heightData)
	return GetEntryByHeight(db, height)
}
func GetEntryByHeight(db DB, height uint64) ([]byte, error) {
	return db.Get(getEntryByHeightKey(height))
}
