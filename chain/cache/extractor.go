package cache

import (
	"bytes"

	"github.com/zenon-network/go-zenon/chain/account"
	"github.com/zenon-network/go-zenon/chain/momentum"
	"github.com/zenon-network/go-zenon/chain/store"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/db"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/vm/embedded/definition"
)

type cacheExtractor struct {
	cache  store.Cache
	height uint64
	patch  db.Patch
}

func (e *cacheExtractor) Put(key []byte, value []byte) {
	if cacheKey := e.tryToGetCacheKey(key, value); cacheKey != nil {
		e.patch.Put(cacheKey, value)
	}
}

func (e *cacheExtractor) Delete(key []byte) {
	if cacheKey := e.tryToGetCacheKey(key, nil); cacheKey != nil {
		e.patch.Put(cacheKey, []byte{})
	}
}

func (e *cacheExtractor) tryToGetCacheKey(key []byte, value []byte) []byte {
	if bytes.HasPrefix(key, momentum.AccountStorePrefix) {
		key = bytes.TrimPrefix(key, momentum.AccountStorePrefix)
		keyWithoutAddress := key[types.AddressSize:]
		address, err := types.BytesToAddress(key[:types.AddressSize])
		common.DealWithErr(err)

		// Cache fused plasma
		if address == types.PlasmaContract {
			prefix := common.JoinBytes(account.StorageKeyPrefix, definition.FusedAmountKeyPrefix)
			if bytes.HasPrefix(keyWithoutAddress, prefix) {
				beneficiary := bytes.TrimPrefix(keyWithoutAddress, prefix)
				return e.getHeightKey(getFusedAmountKeyPrefix(beneficiary))
			}
		}

		// Cache sporks
		if address == types.SporkContract {
			prefix := common.JoinBytes(account.StorageKeyPrefix, []byte{definition.SporkInfoPrefix})
			if bytes.HasPrefix(keyWithoutAddress, prefix) {
				sporkId := bytes.TrimPrefix(keyWithoutAddress, prefix)
				return e.getHeightKey(getSporkInfoKeyPrefix(sporkId))
			}
		}

		// Cache chain plasma
		if bytes.HasPrefix(keyWithoutAddress, account.ChainPlasmaKey) {
			if value == nil {
				return nil
			}
			// Verify that the state has changed
			current, err := e.cache.GetChainPlasma(address)
			common.DealWithErr(err)
			if current.Cmp(common.BytesToBigInt(value)) == 0 {
				return nil
			}
			return e.getHeightKey(getChainPlasmaKeyPrefix(address.Bytes()))
		}
	}

	return nil
}

func (e *cacheExtractor) getHeightKey(prefix []byte) []byte {
	return common.JoinBytes(prefix, common.Uint64ToBytes(e.height))
}
