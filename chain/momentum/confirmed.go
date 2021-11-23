package momentum

import (
	"github.com/syndtr/goleveldb/leveldb"

	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/types"
)

func getBlockConfirmationHeightKey(hash types.Hash) []byte {
	return common.JoinBytes(blockConfirmationHeightPrefix, hash.Bytes())
}

func (ms *momentumStore) GetBlockConfirmationHeight(hash types.Hash) (uint64, error) {
	data, err := ms.DB.Get(getBlockConfirmationHeightKey(hash))
	if err == leveldb.ErrNotFound {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return common.BytesToUint64(data), nil
}
func (ms *momentumStore) setBlockConfirmationHeight(hash types.Hash, height uint64) error {
	return ms.DB.Put(getBlockConfirmationHeightKey(hash), common.Uint64ToBytes(height))
}
