// Package mailbox implements store.AccountMailbox: the per-address
// record of incoming send blocks, stored under the momentum ledger
// (not the account chain) and updated only when a momentum confirms
// blocks. It tracks which send blocks are still unreceived, which
// receive block consumed each send, and — for embedded contracts —
// the FIFO queue of send-block headers that must be received in
// order.
package mailbox

import (
	"fmt"

	"github.com/syndtr/goleveldb/leveldb"

	"github.com/zenon-network/go-zenon/chain/store"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/db"
	"github.com/zenon-network/go-zenon/common/types"
)

// parseAccountHeader deserializes a stored account header, returning
// nil for leveldb.ErrNotFound and panicking on any other failure.
func parseAccountHeader(data []byte, err error) *types.AccountHeader {
	if err == leveldb.ErrNotFound {
		return nil
	}
	if err != nil {
		panic(err)
		return nil
	}
	if header, err := types.DeserializeAccountHeader(data); err != nil {
		panic(fmt.Sprintf("m.Deserialize failed, Error: %v", err))
	} else {
		return header
	}
}

// mailbox implements store.AccountMailbox over the address's mailbox
// subset of a momentum-chain database version.
type mailbox struct {
	address types.Address
	db.DB
}

// NewAccountMailbox returns a store.AccountMailbox for address that
// reads and writes through db, the address's mailbox subset of a
// momentum-chain database version (see chain/momentum).
func NewAccountMailbox(address types.Address, db db.DB) store.AccountMailbox {
	return &mailbox{
		address: address,
		DB:      db,
	}
}

func getUnreceivedBlockKey(hash types.Hash) []byte {
	return common.JoinBytes(unreceivedBlockPrefix, hash.Bytes())
}
func getPendingBlockKey(hash types.Hash) []byte {
	return common.JoinBytes(pendingBlockPrefix, hash.Bytes())
}
func getPendingBlocksIterator() []byte {
	return pendingBlockPrefix
}
func getBlockWhichReceivesKey(hash types.Hash) []byte {
	return common.JoinBytes(blockWhichReceives, hash.Bytes())
}

func getSequencerHeaderByHeightKey(height uint64) []byte {
	return common.JoinBytes(sequencerHeaderByHeightPrefix, common.Uint64ToBytes(height))
}

func (m *mailbox) Address() types.Address {
	return m.address
}
func (m *mailbox) Snapshot() store.AccountMailbox {
	return NewAccountMailbox(m.address, m.DB.Snapshot())
}

// MarkAsUnreceived records an incoming send-block hash in both the
// permanent unreceived index and the pending set; it runs when a
// momentum confirms the send block.
func (m *mailbox) MarkAsUnreceived(hash types.Hash) error {
	err := m.DB.Put(getUnreceivedBlockKey(hash), common.Uint64ToBytes(1))
	if err != nil {
		return err
	}
	return m.DB.Put(getPendingBlockKey(hash), common.Uint64ToBytes(1))
}

// MarkAsReceived removes the send-block hash from the pending set
// once a confirmed receive block consumes it; the permanent
// unreceived index keeps its entry.
func (m *mailbox) MarkAsReceived(hash types.Hash) error {
	return m.DB.Delete(getPendingBlockKey(hash))
}

// MarkBlockThatReceives maps the hash of a send block from this
// account to the header of the receive block that consumed it.
func (m *mailbox) MarkBlockThatReceives(hash types.Hash, receiveHeader types.AccountHeader) error {
	data, err := receiveHeader.Serialize()
	common.DealWithErr(err)
	return m.DB.Put(getBlockWhichReceivesKey(hash), data)
}

// GetBlockWhichReceives returns the header of the receive block that
// consumed the send block with hash fromHash, or nil if it has not
// been received yet.
func (m *mailbox) GetBlockWhichReceives(fromHash types.Hash) *types.AccountHeader {
	return parseAccountHeader(m.DB.Get(getBlockWhichReceivesKey(fromHash)))
}

// GetUnreceivedAccountBlockHashes returns up to atMost hashes from
// the pending set, in ascending byte order of the hashes; atMost must
// be non-zero. The ledger RPC pages unreceived blocks through a
// 500-hash window of this list.
func (m *mailbox) GetUnreceivedAccountBlockHashes(atMost uint64) ([]types.Hash, error) {
	iterator := m.DB.NewIterator(getPendingBlocksIterator())
	defer iterator.Release()
	list := make([]types.Hash, 0)

	for {
		if !iterator.Next() {
			if iterator.Error() != nil {
				return nil, iterator.Error()
			}
			break
		}
		if iterator.Value() == nil {
			continue
		}
		hash, err := types.BytesToHash(iterator.Key()[1:])

		if err != nil {
			return nil, err
		}
		list = append(list, hash)

		atMost -= 1
		if atMost == 0 {
			return list, nil
		}
	}
	return list, nil
}

// SequencerSize returns the number of send-block headers ever pushed
// onto this embedded contract's receive queue.
func (m *mailbox) SequencerSize() uint64 {
	data, err := m.DB.Get(sequencerNumInsertedKey)
	if err == leveldb.ErrNotFound {
		return 0
	}
	return common.BytesToUint64(data)
}

// SequencerPushBack appends a send-block header to the embedded
// contract's receive queue; heights are 1-based and only grow — the
// consumed cursor lives on the account chain (see
// store.Account.SequencerFront).
func (m *mailbox) SequencerPushBack(header types.AccountHeader) {
	total := m.SequencerSize() + 1
	common.DealWithErr(m.DB.Put(sequencerNumInsertedKey, common.Uint64ToBytes(total)))
	data, err := header.Serialize()
	common.DealWithErr(err)
	common.DealWithErr(m.DB.Put(getSequencerHeaderByHeightKey(total), data))
}

// SequencerByHeight returns the queued header at the given 1-based
// height, or nil if the height was never pushed.
func (m *mailbox) SequencerByHeight(height uint64) *types.AccountHeader {
	return parseAccountHeader(m.DB.Get(getSequencerHeaderByHeightKey(height)))
}
