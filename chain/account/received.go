package account

import (
	"github.com/syndtr/goleveldb/leveldb"

	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/types"
)

func receivedBlockKey(hash types.Hash) []byte {
	return common.JoinBytes(receivedBlockPrefix, hash.Bytes())
}

func (as *accountStore) MarkAsReceived(hash types.Hash) error {
	return as.DB.Put(receivedBlockKey(hash), common.Uint64ToBytes(Received))
}
func (as *accountStore) IsReceived(hash types.Hash) bool {
	_, err := as.DB.Get(receivedBlockKey(hash))
	if err == leveldb.ErrNotFound {
		return false
	}
	common.DealWithErr(err)
	return true
}
