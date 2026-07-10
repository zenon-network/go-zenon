package cache

import (
	"github.com/zenon-network/go-zenon/chain/cache/storage"
	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/chain/store"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/db"
	"github.com/zenon-network/go-zenon/common/types"
)

type cacheStore struct {
	identifier types.HashHeight
	changes    db.DB
	db         db.DB
}

func NewCacheStore(identifier types.HashHeight, manager storage.CacheManager) store.Cache {
	if manager == nil {
		panic("cache store can't operate with nil db manager")
	}
	frontier := storage.GetFrontierIdentifier(manager.DB())
	if identifier.Height > frontier.Height {
		panic("cache store identifier height cannot be greater than db height")
	}
	return &cacheStore{
		identifier: identifier,
		changes:    db.NewMemDB(),
		db:         manager.DB(),
	}
}

func (cs *cacheStore) Identifier() types.HashHeight {
	return cs.identifier
}

func (cs *cacheStore) Changes() (db.Patch, error) {
	return cs.changes.Changes()
}

func (cs *cacheStore) ApplyMomentum(detailed *nom.DetailedMomentum, changes db.Patch) error {
	if len(detailed.AccountBlocks) == 0 {
		return nil
	}
	extractor := &cacheExtractor{cache: cs, height: detailed.Momentum.Height, patch: db.NewPatch()}
	if err := changes.Replay(extractor); err != nil {
		return err
	}
	if err := cs.changes.Apply(extractor.patch); err != nil {
		return err
	}
	err := cs.pruneAccountCache(detailed.AccountBlocks)
	return err
}

func (cs *cacheStore) findValue(prefix []byte) ([]byte, error) {
	iterator := cs.db.NewIterator(prefix)
	defer iterator.Release()

	if !iterator.Last() {
		return []byte{}, nil
	}

	for {
		if getKeyHeight(iterator.Key()) <= cs.identifier.Height {
			return iterator.Value(), nil
		}
		if !iterator.Prev() {
			if iterator.Error() != nil {
				return nil, iterator.Error()
			}
			return []byte{}, nil
		}
	}
}

func (cs *cacheStore) findExpiredKeys(prefix []byte, validHeight uint64) ([][]byte, error) {
	iterator := cs.db.NewIterator(prefix)
	defer iterator.Release()

	keys := [][]byte{}
	for {
		if !iterator.Next() {
			if iterator.Error() != nil {
				return nil, iterator.Error()
			}
			break
		}
		if getKeyHeight(iterator.Key()) > validHeight {
			break
		}
		key := make([]byte, len(iterator.Key()))
		copy(key, iterator.Key())
		keys = append(keys, key)
	}

	// Remove key of the current state, so that it's not returned as expired
	if len(keys) > 0 {
		keys = keys[:len(keys)-1]
	}
	return keys, nil
}

func getKeyHeight(key []byte) uint64 {
	const uint64Size = 8
	return common.BytesToUint64(key[len(key)-uint64Size:])
}
