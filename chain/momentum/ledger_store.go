// Package momentum implements store.Momentum: the state of the
// momentum ledger backed by one version of the momentum-chain
// database. The momentum entries themselves are stored through the
// common/db version helpers (SetFrontier, GetEntryByHeight,
// GetEntryByHash), while everything derived from confirming account
// blocks — per-address account state, account mailboxes, the
// header-by-hash index, block confirmation heights and a cached ZNN
// balance per address — lives in prefixed subsets of the same
// database, so a momentum version pins the confirmed state of every
// account chain.
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

// momentumStore implements store.Momentum by combining the genesis
// configuration view with one version of the momentum-chain database.
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

// Snapshot returns a copy-on-write view of the store; writes to the
// snapshot never reach the receiver.
func (ms *momentumStore) Snapshot() store.Momentum {
	return NewStore(ms.Genesis, ms.DB.Snapshot())
}

// GetAccountDB returns a copy-on-write snapshot of the given
// account's momentum-confirmed database subset; the account pool
// builds its in-memory managers on top of it.
func (ms *momentumStore) GetAccountDB(address types.Address) db.DB {
	return ms.DB.Subset(getAccountStorePrefix(address)).Snapshot()
}

// GetAccountStore returns the momentum-confirmed store.Account of the
// given address, backed by a copy-on-write snapshot.
func (ms *momentumStore) GetAccountStore(address types.Address) store.Account {
	return account.NewAccountStore(address, ms.GetAccountDB(address))
}

// getAccountMailbox returns the writable mailbox subset used while
// confirming blocks; exported readers go through GetAccountMailbox.
func (ms *momentumStore) getAccountMailbox(address types.Address) store.AccountMailbox {
	return mailbox.NewAccountMailbox(address, ms.DB.Subset(getAccountMailboxPrefix(address)))
}

// GetAccountMailbox returns a copy-on-write snapshot of the given
// account's mailbox.
func (ms *momentumStore) GetAccountMailbox(address types.Address) store.AccountMailbox {
	return ms.getAccountMailbox(address).Snapshot()
}

// AddAccountBlockTransaction confirms one account block in this
// momentum version: it applies the block's state patch onto the
// per-address subset, refreshes the cached ZNN balance, and for the
// block plus all its descendant blocks records the header-by-hash
// index and the confirmation height (frontier height + 1). Send
// blocks are marked unreceived in the destination's mailbox (and
// pushed onto its sequencer queue when the destination is an embedded
// contract); receive blocks mark the consumed send in both mailboxes
// involved. Batched descendant blocks arriving with an empty patch
// are skipped — their changes are part of the main block's patch.
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

// Identifier returns the hash-height of the frontier momentum, or the
// zero hash-height if the store is empty.
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

// NewStore returns a store.Momentum that reads and writes through db,
// which must be one version of the momentum-chain database (a
// db.Manager frontier or historical snapshot); it panics if db is
// nil. The genesis view supplies the chain identifier and the genesis
// momentum.
func NewStore(genesis store.Genesis, db db.DB) store.Momentum {
	if db == nil {
		panic("momentum store can't operate with nil db")
	}
	return &momentumStore{
		Genesis: genesis,
		DB:      db,
	}
}

// NewGenesisStore returns an empty in-memory store.Momentum with no
// genesis configuration; it backs the momentum VM context used to
// generate the genesis momentum itself.
func NewGenesisStore() store.Momentum {
	return &momentumStore{
		Genesis: nil,
		DB:      db.NewMemDB(),
	}
}
