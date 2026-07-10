package chain

import (
	"fmt"
	"os"
	"sync"

	"github.com/pkg/errors"

	"github.com/zenon-network/go-zenon/chain/cache/storage"
	"github.com/zenon-network/go-zenon/chain/store"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/db"
	"github.com/zenon-network/go-zenon/common/types"
)

var (
	inserterLog = common.ChainLogger.New("submodule", "chain-insert-mutex")
)

type chain struct {
	log common.Logger

	store.Genesis
	*accountPool
	*momentumPool
	*momentumEventManager
	*chainCache

	chainManager db.Manager
	cacheManager storage.CacheManager
	insert       sync.Mutex
}

func NewChain(chainManager db.Manager, cacheManager storage.CacheManager, genesis store.Genesis) *chain {
	momentumPool := NewMomentumPool(chainManager, genesis)
	cache := NewChainCache(cacheManager)
	return &chain{
		log:                  common.ChainLogger,
		Genesis:              genesis,
		accountPool:          newAccountPool(momentumPool),
		momentumPool:         momentumPool,
		momentumEventManager: momentumPool.momentumEventManager,
		chainCache:           cache,
		chainManager:         chainManager,
		cacheManager:         cacheManager,
	}
}

func (c *chain) Init() error {
	c.log.Info("initializing ...")
	defer c.log.Info("initialized")

	c.log.Info("starting chain module with db", "location", c.chainManager.Location(), "frontier-identifier", c.GetFrontierMomentumStore().Identifier())

	// check if the configured genesis matches the existent chain
	if err := c.checkGenesisCompatibility(); err != nil {
		return err
	}
	types.SporkAddress = c.genesis.GetSporkAddress()
	c.Register(c.accountPool)

	frontierStore := c.GetFrontierMomentumStore()
	frontier, err := frontierStore.GetFrontierMomentum()
	if err != nil {
		return err
	}

	if err := c.chainCache.Init(c.chainManager, frontierStore); err != nil {
		return err
	}

	fmt.Printf("Initialized NoM. Height: %v, Hash: %v\n", frontier.Height, frontier.Hash)
	c.log.Info("initialized nom", "identifier", frontier.Identifier())

	if _, unimplemented, err := GotAllActiveSporksImplemented(frontierStore); err != nil {
		return err
	} else if unimplemented != nil {
		c.log.Crit("can't start node because don't have all sporks implemented",
			"hash", frontier.Hash, "height", frontier.Height, "unimplemented", unimplemented)

		fmt.Printf("===== Error =====\n")
		fmt.Printf("Can't start node. %v height %v\n", frontier.Hash, frontier.Height)
		fmt.Printf("Detected an unimplemented spork.\n")
		for _, spork := range unimplemented {
			fmt.Printf("  Spork name `%v` id:`%v`\n", spork.Name, spork.Id)
		}
		fmt.Printf("\n")
		fmt.Printf("Please upgrade your znnd binary\n")
		fmt.Printf("znnd is terminating\n")
		os.Exit(2)
	}

	return nil
}
func (c *chain) Start() error {
	c.log.Info("starting ...")
	defer c.log.Info("started")

	return nil
}
func (c *chain) Stop() error {
	c.log.Info("stopping ...")
	defer c.log.Info("stopped")

	c.UnRegister(c.accountPool)

	if err := c.cacheManager.Stop(); err != nil {
		return err
	}

	return c.chainManager.Stop()
}

func (c *chain) checkGenesisCompatibility() error {
	frontierStore := c.GetFrontierMomentumStore()
	if frontierStore.Identifier().IsZero() {
		insert := c.AcquireInsert("add genesis momentum")
		defer insert.Unlock()
		c.log.Info("did not find any blocks. Inserting genesis block")
		// chain is empty, apply genesis
		if err := c.momentumPool.AddMomentumTransaction(insert, c.GetGenesisTransaction()); err != nil {
			return err
		}
	} else {
		genesisMomentum, err := frontierStore.GetMomentumByHeight(1)
		if err != nil {
			return err
		}
		if genesisMomentum.Hash != c.GetGenesisMomentum().Hash {
			return errors.Errorf("The genesis state is incorrect. " +
				"You can fix the problem by removing the database manually.")
		}
		c.log.Info("found momentums in DB. genesis-hash matches")
	}
	return nil
}

func (c *chain) AcquireInsert(reason string) sync.Locker {
	inserterLog.Debug("waiting", "reason", reason)
	c.insert.Lock()

	inserterLog.Debug("acquired", "reason", reason)
	return &inserter{
		reason: reason,
		mutex:  &c.insert,
	}
}

type inserter struct {
	reason string
	mutex  *sync.Mutex
}

func (i *inserter) Lock() {
	panic("can't lock an insert - already locked")
}
func (i *inserter) Unlock() {
	inserterLog.Debug("released", "reason", i.reason)
	i.mutex.Unlock()
	i.mutex = nil
}
