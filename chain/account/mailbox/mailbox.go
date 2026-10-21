// Package mailbox implements store.AccountMailbox: the per-address
// record of incoming send blocks, stored under the momentum ledger
// (not the account chain) and updated only when a momentum confirms
// blocks. It tracks which send blocks are still unreceived, which
// receive block consumed each send, and — for embedded contracts —
// the FIFO queue of send-block headers that must be received in
// order.
package mailbox

import (
	"github.com/pkg/errors"
	"github.com/syndtr/goleveldb/leveldb"

	"github.com/zenon-network/go-zenon/chain/store"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/db"
	"github.com/zenon-network/go-zenon/common/types"
)

func parseAccountHeader(data []byte, err error) (*types.AccountHeader, error) {
	if err == leveldb.ErrNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if header, err := types.DeserializeAccountHeader(data); err != nil {
		return nil, errors.Errorf("failed to deserialize account-header; reason: %v", err.Error())
	} else {
		return header, nil
	}
}

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

func (m *mailbox) MarkAsUnreceived(hash types.Hash) error {
	err := m.DB.Put(getUnreceivedBlockKey(hash), common.Uint64ToBytes(1))
	if err != nil {
		return err
	}
	return m.DB.Put(getPendingBlockKey(hash), common.Uint64ToBytes(1))
}
func (m *mailbox) MarkAsReceived(hash types.Hash) error {
	return m.DB.Delete(getPendingBlockKey(hash))
}
func (m *mailbox) MarkBlockThatReceives(hash types.Hash, receiveHeader types.AccountHeader) error {
	data, err := receiveHeader.Serialize()
	common.DealWithErr(err)
	return m.DB.Put(getBlockWhichReceivesKey(hash), data)
}

func (m *mailbox) GetBlockWhichReceives(fromHash types.Hash) (*types.AccountHeader, error) {
	return parseAccountHeader(m.DB.Get(getBlockWhichReceivesKey(fromHash)))
}
func (m *mailbox) GetUnreceivedAccountBlockHashes(atMost uint64) ([]types.Hash, error) {
	list := make([]types.Hash, 0)
	if atMost == 0 {
		return list, nil
	}
	iterator := m.DB.NewIterator(getPendingBlocksIterator())
	defer iterator.Release()

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

func (m *mailbox) SequencerSize() (uint64, error) {
	data, err := m.DB.Get(sequencerNumInsertedKey)
	if err == leveldb.ErrNotFound {
		return 0, nil
	}
	if err != nil {
		return 0, errors.Errorf("failed to read sequencer size for mailbox %s: %v", m.address, err)
	}
	if len(data) < 8 {
		return 0, errors.Errorf("sequencer size for mailbox %s is too short: got %d bytes, want 8", m.address, len(data))
	}
	return common.BytesToUint64(data), nil
}
func (m *mailbox) SequencerPushBack(header types.AccountHeader) {
	total, err := m.SequencerSize()
	if err != nil {
		common.DealWithErr(err)
	}
	total++
	common.DealWithErr(m.DB.Put(sequencerNumInsertedKey, common.Uint64ToBytes(total)))
	data, err := header.Serialize()
	common.DealWithErr(err)
	common.DealWithErr(m.DB.Put(getSequencerHeaderByHeightKey(total), data))
}
func (m *mailbox) SequencerByHeight(height uint64) (*types.AccountHeader, error) {
	return parseAccountHeader(m.DB.Get(getSequencerHeaderByHeightKey(height)))
}
