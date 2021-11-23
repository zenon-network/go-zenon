package momentum

import (
	"math/big"

	"github.com/syndtr/goleveldb/leveldb"

	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/types"
)

func getAccountZNNBalance(address types.Address) []byte {
	return common.JoinBytes(accountZNNBalancePrefix, address.Bytes())
}

func (ms *momentumStore) getZnnBalance(address types.Address) (*big.Int, error) {
	data, err := ms.DB.Get(getAccountZNNBalance(address))
	if err == leveldb.ErrNotFound {
		return big.NewInt(0), nil
	}
	if err != nil {
		return nil, err
	}
	return big.NewInt(0).SetBytes(data), nil
}
func (ms *momentumStore) setZnnBalance(address types.Address, balance *big.Int) error {
	return ms.DB.Put(getAccountZNNBalance(address), common.BigIntToBytes(balance))
}
