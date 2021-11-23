package account

import (
	"math/big"

	"github.com/syndtr/goleveldb/leveldb"

	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/types"
)

func getBalanceKey(zts types.ZenonTokenStandard) []byte {
	return common.JoinBytes(balanceKeyPrefix, zts.Bytes())
}
func getBalancePrefix() []byte {
	return common.JoinBytes(balanceKeyPrefix)
}

func (as *accountStore) GetBalance(zts types.ZenonTokenStandard) (*big.Int, error) {
	data, err := as.DB.Get(getBalanceKey(zts))
	if err == leveldb.ErrNotFound {
		return big.NewInt(0), nil
	}
	if err != nil {
		return nil, err
	}

	return big.NewInt(0).SetBytes(data), nil
}
func (as *accountStore) SetBalance(zts types.ZenonTokenStandard, balance *big.Int) error {
	if err := as.DB.Put(getBalanceKey(zts), common.BigIntToBytes(balance)); err != nil {
		return err
	}
	return nil
}
func (as *accountStore) GetBalanceMap() (map[types.ZenonTokenStandard]*big.Int, error) {
	iterator := as.DB.NewIterator(getBalancePrefix())
	defer iterator.Release()
	result := make(map[types.ZenonTokenStandard]*big.Int, 0)

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

		zts, err := types.BytesToZTS(iterator.Key()[1:])

		if err != nil {
			return nil, err
		}
		result[zts] = common.BytesToBigInt(iterator.Value())
	}
	return result, nil
}
