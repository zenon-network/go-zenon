package account

import (
	"github.com/pkg/errors"
	"github.com/syndtr/goleveldb/leveldb"

	"github.com/zenon-network/go-zenon/chain/store"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/types"
)

func (as *accountStore) sequencerFrontIndex() (uint64, error) {
	data, err := as.DB.Get(sequencerLastReceivedKey)
	if err == leveldb.ErrNotFound {
		return 0, nil
	}
	if err != nil {
		return 0, errors.Errorf("failed to read sequencer front index for account %s: %v", as.address, err)
	}
	if len(data) < 8 {
		return 0, errors.Errorf("sequencer front index for account %s is too short: got %d bytes, want 8", as.address, len(data))
	}
	return common.BytesToUint64(data), nil
}

func (as *accountStore) SequencerFront(mailbox store.AccountMailbox) (*types.AccountHeader, error) {
	if mailbox.Address() != as.address {
		panic("not my mailbox")
	}
	last, err := as.sequencerFrontIndex()
	if err != nil {
		return nil, err
	}
	total, err := mailbox.SequencerSize()
	if err != nil {
		return nil, err
	}
	if last == total {
		return nil, nil
	}
	return mailbox.SequencerByHeight(last + 1)
}
func (as *accountStore) SequencerPopFront() {
	last, err := as.sequencerFrontIndex()
	if err != nil {
		common.DealWithErr(err)
	}
	common.DealWithErr(as.DB.Put(sequencerLastReceivedKey, common.Uint64ToBytes(last+1)))
}
