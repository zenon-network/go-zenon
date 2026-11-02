package momentum

import (
	"github.com/pkg/errors"
	"github.com/syndtr/goleveldb/leveldb"

	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/types"
)

func getAccountHeaderByHashKey(hash types.Hash) []byte {
	return common.JoinBytes(accountHeaderByHashPrefix, hash.Bytes())
}

func (ms *momentumStore) GetFrontierAccountBlock(address types.Address) (*nom.AccountBlock, error) {
	return ms.GetAccountStore(address).Frontier()
}

// GetAccountBlock returns the block the header names, or nil if the account
// chain has no block at that height. A block at that height with another
// hash is an inconsistent store, not the block asked for, and is an error.
func (ms *momentumStore) GetAccountBlock(header types.AccountHeader) (*nom.AccountBlock, error) {
	block, err := ms.GetAccountStore(header.Address).ByHeight(header.Height)
	if err != nil || block == nil {
		return nil, err
	}
	if block.Hash != header.Hash {
		return nil, errors.Errorf("account-block at height %v of %v is %v, not %v", header.Height, header.Address, block.Hash, header.Hash)
	}
	return block, nil
}
func (ms *momentumStore) GetAccountBlockByHeight(address types.Address, height uint64) (*nom.AccountBlock, error) {
	return ms.GetAccountStore(address).ByHeight(height)
}
func (ms *momentumStore) GetAccountBlocksByHeight(address types.Address, height, count uint64) ([]*nom.AccountBlock, error) {
	return ms.GetAccountStore(address).MoreByHeight(height, count)
}

func (ms *momentumStore) addAccountBlockHeader(header types.AccountHeader) error {
	data, err := header.Serialize()
	if err != nil {
		return err
	}
	return ms.DB.Put(getAccountHeaderByHashKey(header.Hash), data)
}
func (ms *momentumStore) GetAccountBlockByHash(hash types.Hash) (*nom.AccountBlock, error) {
	data, err := ms.DB.Get(getAccountHeaderByHashKey(hash))

	if err == leveldb.ErrNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	if header, err := types.DeserializeAccountHeader(data); err != nil {
		return nil, err
	} else {
		return ms.GetAccountStore(header.Address).ByHeight(header.Height)
	}
}
