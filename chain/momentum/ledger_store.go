package momentum

import (
	"github.com/pkg/errors"
	"github.com/syndtr/goleveldb/leveldb"

	"github.com/zenon-network/go-zenon/chain/account"
	"github.com/zenon-network/go-zenon/chain/account/mailbox"
	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/chain/store"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/db"
	"github.com/zenon-network/go-zenon/common/types"
)

type momentumStore struct {
	store.Genesis
	db.DB
}

func getAccountStorePrefix(address types.Address) []byte {
	return common.JoinBytes(accountStorePrefix, address.Bytes())
}
func getAccountMailboxPrefix(address types.Address) []byte {
	return common.JoinBytes(accountMailboxPrefix, address.Bytes())
}

func (ms *momentumStore) Snapshot() store.Momentum {
	return NewStore(ms.Genesis, ms.DB.Snapshot())
}

func (ms *momentumStore) GetAccountDB(address types.Address) db.DB {
	return ms.DB.Subset(getAccountStorePrefix(address)).Snapshot()
}
func (ms *momentumStore) GetAccountStore(address types.Address) store.Account {
	return account.NewAccountStore(address, ms.GetAccountDB(address))
}

func (ms *momentumStore) getAccountMailbox(address types.Address) store.AccountMailbox {
	return mailbox.NewAccountMailbox(address, ms.DB.Subset(getAccountMailboxPrefix(address)))
}
func (ms *momentumStore) GetAccountMailbox(address types.Address) store.AccountMailbox {
	return ms.getAccountMailbox(address).Snapshot()
}

func (ms *momentumStore) AddAccountBlockTransaction(header types.AccountHeader, patch db.Patch) error {
	// skip batched blocks
	if len(patch.Dump()) == 0 {
		return nil
	}
	identifier := ms.Identifier()
	if err := ms.DB.Subset(getAccountStorePrefix(header.Address)).Apply(patch); err != nil {
		return nil
	}

	// Set znn balance
	accountStore := ms.GetAccountStore(header.Address)
	znnBalance, err := accountStore.GetBalance(types.ZnnTokenStandard)
	if err != nil {
		return err
	}
	if err := ms.setZnnBalance(header.Address, znnBalance); err != nil {
		return err
	}

	block, err := ms.GetAccountBlock(header)
	if err != nil {
		return err
	}
	if block == nil {
		return errors.Errorf("can't find block for header %v", header)
	}

	blocks := []*nom.AccountBlock{block}
	blocks = append(blocks, block.DescendantBlocks...)

	for _, block := range blocks {
		if err := ms.addAccountBlockHeader(block.Header()); err != nil {
			return err
		}
		if err := ms.setBlockConfirmationHeight(block.Hash, identifier.Height+1); err != nil {
			return nil
		}

		if block.IsSendBlock() {
			othStore := ms.getAccountMailbox(block.ToAddress)
			if err := othStore.MarkAsUnreceived(block.Hash); err != nil {
				return err
			}

			if types.IsEmbeddedAddress(block.ToAddress) {
				othStore.SequencerPushBack(block.Header())
			}
		} else if block.BlockType != nom.BlockTypeGenesisReceive {
			fromBlock, err := ms.GetAccountBlockByHash(block.FromBlockHash)
			if err != nil {
				return err
			}
			if fromBlock == nil {
				return errors.Errorf("Impossible. Can't find from-block in store")
			}
			fromStore := ms.getAccountMailbox(fromBlock.Address)
			if err := fromStore.MarkBlockThatReceives(block.FromBlockHash, block.Header()); err != nil {
				return err
			}

			myStore := ms.getAccountMailbox(block.Address)
			if err := myStore.MarkAsReceived(block.FromBlockHash); err != nil {
				return err
			}
		}
	}

	return nil
}
func (ms *momentumStore) Identifier() types.HashHeight {
	frontier, err := ms.GetFrontierMomentum()
	if frontier == nil || err == leveldb.ErrNotFound {
		return types.HashHeight{
			Height: 0,
			Hash:   types.ZeroHash,
		}
	} else {
		common.DealWithErr(err)
		return frontier.Identifier()
	}
}

func NewStore(genesis store.Genesis, db db.DB) store.Momentum {
	if db == nil {
		panic("momentum store can't operate with nil db")
	}
	return &momentumStore{
		Genesis: genesis,
		DB:      db,
	}
}
func NewGenesisStore() store.Momentum {
	return &momentumStore{
		Genesis: nil,
		DB:      db.NewMemDB(),
	}
}
