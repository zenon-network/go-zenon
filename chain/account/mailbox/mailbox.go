package mailbox

import (
	"fmt"

	"github.com/syndtr/goleveldb/leveldb"

	"github.com/zenon-network/go-zenon/chain/store"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/db"
	"github.com/zenon-network/go-zenon/common/types"
)

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

type mailbox struct {
	address types.Address
	db.DB
}

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

func (m *mailbox) GetBlockWhichReceives(fromHash types.Hash) *types.AccountHeader {
	return parseAccountHeader(m.DB.Get(getBlockWhichReceivesKey(fromHash)))
}
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

func (m *mailbox) SequencerSize() uint64 {
	data, err := m.DB.Get(sequencerNumInsertedKey)
	if err == leveldb.ErrNotFound {
		return 0
	}
	return common.BytesToUint64(data)
}
func (m *mailbox) SequencerPushBack(header types.AccountHeader) {
	total := m.SequencerSize() + 1
	common.DealWithErr(m.DB.Put(sequencerNumInsertedKey, common.Uint64ToBytes(total)))
	data, err := header.Serialize()
	common.DealWithErr(err)
	common.DealWithErr(m.DB.Put(getSequencerHeaderByHeightKey(total), data))
}
func (m *mailbox) SequencerByHeight(height uint64) *types.AccountHeader {
	return parseAccountHeader(m.DB.Get(getSequencerHeaderByHeightKey(height)))
}
