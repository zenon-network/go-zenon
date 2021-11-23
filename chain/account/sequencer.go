package account

import (
	"github.com/syndtr/goleveldb/leveldb"

	"github.com/zenon-network/go-zenon/chain/store"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/types"
)

func (as *accountStore) sequencerFrontIndex() uint64 {
	data, err := as.DB.Get(sequencerLastReceivedKey)
	if err == leveldb.ErrNotFound {
		return 0
	}
	return common.BytesToUint64(data)
}

func (as *accountStore) SequencerFront(mailbox store.AccountMailbox) *types.AccountHeader {
	if mailbox.Address() != as.address {
		panic("not my mailbox")
	}
	last := as.sequencerFrontIndex()
	total := mailbox.SequencerSize()
	if last == total {
		return nil
	}
	return mailbox.SequencerByHeight(last + 1)
}
func (as *accountStore) SequencerPopFront() {
	last := as.sequencerFrontIndex()
	common.DealWithErr(as.DB.Put(sequencerLastReceivedKey, common.Uint64ToBytes(last+1)))
}
