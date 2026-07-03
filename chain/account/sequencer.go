package account

import (
	"github.com/syndtr/goleveldb/leveldb"

	"github.com/zenon-network/go-zenon/chain/store"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/types"
)

// sequencerFrontIndex returns how many queued send-block headers this
// embedded contract has already received (0 if never written).
func (as *accountStore) sequencerFrontIndex() uint64 {
	data, err := as.DB.Get(sequencerLastReceivedKey)
	if err == leveldb.ErrNotFound {
		return 0
	}
	return common.BytesToUint64(data)
}

// SequencerFront returns the next send-block header this embedded
// contract must receive, combining the mailbox's FIFO queue (which
// belongs to the momentum ledger) with the consumed-count cursor kept
// on this account chain, or nil when the queue is drained. It panics
// if the mailbox belongs to a different address. The verifier uses it
// to enforce in-order contract receives.
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

// SequencerPopFront advances the consumed-count cursor by one; the VM
// calls it when generating an embedded contract's receive block.
func (as *accountStore) SequencerPopFront() {
	last := as.sequencerFrontIndex()
	common.DealWithErr(as.DB.Put(sequencerLastReceivedKey, common.Uint64ToBytes(last+1)))
}
