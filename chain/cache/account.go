package cache

import (
	"math/big"

	"github.com/syndtr/goleveldb/leveldb"
	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/types"
)

var (
	fusedAmountKeyPrefix = []byte{0}
	chainPlasmaKeyPrefix = []byte{1}
)

func getFusedAmountKeyPrefix(address []byte) []byte {
	return common.JoinBytes(accountCacheKeyPrefix, fusedAmountKeyPrefix, address)
}

func getChainPlasmaKeyPrefix(address []byte) []byte {
	return common.JoinBytes(accountCacheKeyPrefix, chainPlasmaKeyPrefix, address)
}

func (cs *cacheStore) GetStakeBeneficialAmount(address types.Address) (*big.Int, error) {
	value, err := cs.findValue(getFusedAmountKeyPrefix(address.Bytes()))
	if err == leveldb.ErrNotFound {
		return big.NewInt(0), nil
	}
	if err != nil {
		return nil, err
	}
	return big.NewInt(0).SetBytes(value), nil
}

func (cs *cacheStore) GetChainPlasma(address types.Address) (*big.Int, error) {
	value, err := cs.findValue(getChainPlasmaKeyPrefix(address.Bytes()))
	if err == leveldb.ErrNotFound {
		return big.NewInt(0), nil
	}
	if err != nil {
		return nil, err
	}
	return big.NewInt(0).SetBytes(value), nil
}

func (cs *cacheStore) pruneAccountCache(blocks []*nom.AccountBlock) error {
	for _, block := range blocks {
		all := append([]*nom.AccountBlock{block}, block.DescendantBlocks...)
		for _, b := range all {
			prefix := getFusedAmountKeyPrefix(b.Address.Bytes())
			fusedPlasmaKeys, err := cs.findExpiredKeys(prefix, b.MomentumAcknowledged.Height)
			if err != nil {
				return err
			}

			prefix = getChainPlasmaKeyPrefix(b.Address.Bytes())
			chainPlasmaKeys, err := cs.findExpiredKeys(prefix, b.MomentumAcknowledged.Height)
			if err != nil {
				return err
			}

			for _, key := range append(fusedPlasmaKeys, chainPlasmaKeys...) {
				cs.changes.Delete(key)
			}
		}
	}
	return nil
}
