package account

import (
	"math/big"

	"github.com/syndtr/goleveldb/leveldb"

	"github.com/zenon-network/go-zenon/common"
)

func getChainPlasmaKey() []byte {
	return chainPlasmaKey
}

func (as *accountStore) GetChainPlasma() (*big.Int, error) {
	data, err := as.DB.Get(getChainPlasmaKey())
	if err == leveldb.ErrNotFound {
		return big.NewInt(0), nil
	}
	if err != nil {
		return nil, err
	}

	return big.NewInt(0).SetBytes(data), nil
}
func (as *accountStore) AddChainPlasma(add uint64) error {
	plasma, err := as.GetChainPlasma()
	if err != nil {
		return err
	}
	plasma.Add(plasma, big.NewInt(int64(add)))
	if err := as.DB.Put(getChainPlasmaKey(), common.BigIntToBytes(plasma)); err != nil {
		return err
	}
	return nil
}
