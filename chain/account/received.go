package account

import (
	"github.com/syndtr/goleveldb/leveldb"

	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/types"
)

func receivedBlockKey(hash types.Hash) []byte {
	return common.JoinBytes(receivedBlockPrefix, hash.Bytes())
}

// MarkAsReceived records that the send block with the given hash has
// been consumed by a receive block of this account.
func (as *accountStore) MarkAsReceived(hash types.Hash) error {
	return as.DB.Put(receivedBlockKey(hash), common.Uint64ToBytes(Received))
}

// IsReceived reports whether the send block with the given hash has
// already been received on this account chain; the verifier uses it
// to reject double receives.
func (as *accountStore) IsReceived(hash types.Hash) bool {
	_, err := as.DB.Get(receivedBlockKey(hash))
	if err == leveldb.ErrNotFound {
		return false
	}
	common.DealWithErr(err)
	return true
}
