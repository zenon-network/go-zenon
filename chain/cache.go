package chain

import (
	"fmt"
	"sync"

	"github.com/inconshreveable/log15"
	"github.com/pkg/errors"
	"github.com/zenon-network/go-zenon/chain/cache"
	"github.com/zenon-network/go-zenon/chain/cache/storage"
	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/chain/store"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/db"
	"github.com/zenon-network/go-zenon/common/types"
)

type chainCache struct {
	manager storage.CacheManager
	log     log15.Logger
	changes sync.Mutex
}

func (c *chainCache) getFrontierStore() store.Cache {
	if db := c.manager.DB(); db == nil {
		return nil
	} else {
		return cache.NewCacheStore(storage.GetFrontierIdentifier(db), c.manager)
	}
}

func (c *chainCache) GetFrontierCacheStore() store.Cache {
	c.changes.Lock()
	defer c.changes.Unlock()
	return c.getFrontierStore()
}

func (c *chainCache) GetCacheStore(identifier types.HashHeight) store.Cache {
	c.changes.Lock()
	defer c.changes.Unlock()
	return cache.NewCacheStore(identifier, c.manager)
}

func (c *chainCache) UpdateCache(insertLocker sync.Locker, detailed *nom.DetailedMomentum, changes db.Patch) error {
	if insertLocker == nil {
		return errors.Errorf("insertLocker can't be nil")
	}
	if changes == nil {
		return errors.Errorf("changes can't be nil")
	}
	c.changes.Lock()
	defer c.changes.Unlock()
	if err := c.update(detailed, changes); err != nil {
		return err
	}
	return nil
}

func (c *chainCache) update(detailed *nom.DetailedMomentum, changes db.Patch) error {
	momentum := detailed.Momentum
	c.log.Info("inserting new momentum to chain cache", "identifier", momentum.Identifier())
	store := c.getFrontierStore()
	store.ApplyMomentum(detailed, changes)
	patch, err := store.Changes()
	if err != nil {
		return err
	}
	if err := c.manager.Add(momentum.Identifier(), patch); err != nil {
		return err
	}
	return nil
}

func (c *chainCache) RollbackCacheTo(insertLocker sync.Locker, identifier types.HashHeight) error {
	if insertLocker == nil {
		return errors.Errorf("insertLocker can't be nil")
	}
	c.changes.Lock()
	defer c.changes.Unlock()
	if err := c.rollbackTo(identifier); err != nil {
		return err
	}
	return nil
}

func (c *chainCache) rollbackTo(identifier types.HashHeight) error {
	c.log.Info("rollbacking cache", "to-identifier", identifier)
	frontier := c.getFrontierStore().Identifier()

	if identifier.Height > frontier.Height {
		return errors.Errorf("can't rollback cache. Expected identifier height %v is greater than frontier %v", identifier, frontier)
	}

	if frontier.Height-identifier.Height > uint64(storage.GetRollbackCacheSize()) {
		return errors.Errorf("can't rollback cache. Target identifier %v is outside the rollback cache", identifier)
	}

	for {
		store := c.getFrontierStore()
		frontier := store.Identifier()
		if frontier.Height == identifier.Height {
			break
		}
		c.log.Info("rollbacking", "momentum-identifier", frontier)
		if err := c.manager.Pop(); err != nil {
			return err
		}
	}

	return nil
}

func (c *chainCache) Init(chainManager db.Manager, momentumStore store.Momentum) error {
	c.changes.Lock()
	defer c.changes.Unlock()
	chainFrontier := db.GetFrontierIdentifier(chainManager.Frontier())
	cacheFrontier := storage.GetFrontierIdentifier(c.manager.DB())

	if cacheFrontier.Height == chainFrontier.Height {
		if cacheFrontier.Hash != chainFrontier.Hash {
			return errors.Errorf("The cache's state is incorrect. " +
				"You can fix the problem by removing the cache database manually.")
		}
		return nil
	}

	if cacheFrontier.Height > chainFrontier.Height {
		if err := c.rollbackTo(chainFrontier); err != nil {
			return err
		}
		return nil
	}

	if chainFrontier.Height-cacheFrontier.Height >= 100000 {
		fmt.Println("Initializing cache: 0%")
	}

	for i := cacheFrontier.Height + 1; i <= chainFrontier.Height; i++ {
		momentum, err := momentumStore.GetMomentumByHeight(i)
		if err != nil {
			return err
		}
		changes := chainManager.GetPatch(momentum.Identifier())
		detailed, err := momentumStore.PrefetchMomentum(momentum)
		if err != nil {
			return err
		}
		if err := c.update(detailed, changes); err != nil {
			return err
		}
		if i%100000 == 0 {
			fmt.Printf("Initializing cache: %d%%\n", i*100/chainFrontier.Height)
		}
	}

	return nil
}

func NewChainCache(cacheManager storage.CacheManager) *chainCache {
	return &chainCache{
		manager: cacheManager,
		log:     common.ChainLogger.New("submodule", "chain-cache"),
	}
}
