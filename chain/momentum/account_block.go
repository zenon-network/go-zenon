package momentum

import (
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
func (ms *momentumStore) GetAccountBlock(header types.AccountHeader) (*nom.AccountBlock, error) {
	return ms.GetAccountStore(header.Address).ByHeight(header.Height)
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
