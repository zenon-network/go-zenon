package chain

import (
	"fmt"
	"os"
	"sync"

	"github.com/inconshreveable/log15"
	"github.com/pkg/errors"

	"github.com/zenon-network/go-zenon/chain/momentum"
	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/chain/store"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/db"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/vm/embedded/definition"
)

type momentumPool struct {
	*momentumEventManager
	chainManager db.Manager
	genesis      store.Genesis
	log          log15.Logger
	changes      sync.Mutex
}

func (c *momentumPool) AddMomentumTransaction(insertLocker sync.Locker, transaction *nom.MomentumTransaction) error {
	c.log.Info("inserting new momentum", "identifier", transaction.Momentum.Identifier())
	if insertLocker == nil {
		return errors.Errorf("insertLocker can't be nil")
	}
	c.changes.Lock()
	defer c.changes.Unlock()

	momentum := transaction.Momentum

	if err := c.chainManager.Add(transaction); err != nil {
		return err
	}

	store := c.getFrontierStore()
	detailed, err := store.PrefetchMomentum(momentum)
	if err != nil {
		return err
	}

	c.changes.Unlock()
	c.broadcastInsertMomentum(detailed)
	c.changes.Lock()

	frontier := c.getFrontierStore()
	if justNow, unimplemented, err := GotAllActiveSporksImplemented(frontier); err != nil {
		return err
	} else if unimplemented != nil {
		c.log.Crit("can't insert momentum because don't have all sporks implemented",
			"hash", momentum.Hash, "height", momentum.Height, "unimplemented", unimplemented)

		fmt.Printf("===== Error =====\n")
		fmt.Printf("Can't insert momentum %v height %v\n", momentum.Hash, momentum.Height)
		fmt.Printf("Detected an unimplemented spork.\n")
		for _, spork := range unimplemented {
			fmt.Printf("  Spork name `%v`\n", spork.Name)
		}
		fmt.Printf("\n")
		fmt.Printf("Please upgrade your znnd binary\n")
		fmt.Printf("znnd is terminating\n")
		os.Exit(2)
	} else if justNow != nil {
		fmt.Printf("\n")
		fmt.Printf("===== Congratulations! =====\n")
		fmt.Printf("Just activated spork '%v'\n", justNow.Name)
		fmt.Printf("\n")
	}

	return nil
}
func (c *momentumPool) RollbackTo(insertLocker sync.Locker, identifier types.HashHeight) error {
	c.log.Info("rollbacking momentums", "to-identifier", identifier)
	if insertLocker == nil {
		return errors.Errorf("insertLocker can't be nil")
	}
	c.changes.Lock()
	defer c.changes.Unlock()
	c.log.Info("preparing to rollback momentums", "identifier", identifier)
	store := c.getFrontierStore()
	momentum, err := store.GetMomentumByHeight(identifier.Height)
	if err != nil {
		return err
	}
	if momentum.Hash != identifier.Hash {
		return errors.Errorf("can't rollback momentums. Expected %v but got %v instead", momentum.Identifier(), identifier)
	}

	for {
		store := c.getFrontierStore()
		frontier, err := store.GetFrontierMomentum()
		if err != nil {
			return err
		}

		if frontier.Height == identifier.Height {
			break
		}
		c.log.Info("rollbacking", "momentum-identifier", frontier.Identifier())
		detailed, err := store.PrefetchMomentum(frontier)
		if err != nil {
			return err
		}
		if err := c.chainManager.Pop(); err != nil {
			return err
		}

		c.changes.Unlock()
		c.broadcastDeleteMomentum(detailed)
		c.changes.Lock()
	}

	return nil
}

// Checks whatever or not all active sporks are implemented
func GotAllActiveSporksImplemented(store store.Momentum) (justNow *definition.Spork, unimplemented []*definition.Spork, err error) {
	momentum, err := store.GetFrontierMomentum()
	if err != nil {
		return nil, nil, err
	}

	// Query previous momentum for DB, since this function can be called from verifier when inserting a new momentum
	sporks, err := store.GetAllDefinedSporks()
	if err != nil {
		return nil, nil, err
	}

	for _, spork := range sporks {
		if spork.Activated && spork.EnforcementHeight <= momentum.Height {
			_, ok := types.ImplementedSporksMap[spork.Id]
			if !ok {
				unimplemented = append(unimplemented, spork)
			}
		}
		if spork.Activated && spork.EnforcementHeight == momentum.Height {
			justNow = spork
		}
	}

	if len(unimplemented) == 0 {
		return justNow, nil, nil
	}

	return justNow, unimplemented, nil
}

func (c *momentumPool) getFrontierStore() store.Momentum {
	if momentumDB := c.chainManager.Frontier(); momentumDB == nil {
		return nil
	} else {
		return momentum.NewStore(c.genesis, momentumDB)
	}
}
func (c *momentumPool) GetFrontierMomentumStore() store.Momentum {
	c.changes.Lock()
	defer c.changes.Unlock()
	return c.getFrontierStore()
}
func (c *momentumPool) GetMomentumStore(identifier types.HashHeight) store.Momentum {
	c.changes.Lock()
	defer c.changes.Unlock()
	momentumDB := c.chainManager.Get(identifier)
	if momentumDB == nil {
		return nil
	}

	return momentum.NewStore(c.genesis, momentumDB)
}
func (c *momentumPool) GetStableAccountDB(address types.Address) db.DB {
	c.changes.Lock()
	defer c.changes.Unlock()
	return c.getFrontierStore().GetAccountDB(address)
}

func NewMomentumPool(chainManager db.Manager, genesis store.Genesis) *momentumPool {
	return &momentumPool{
		momentumEventManager: newMomentumEventManager(),
		chainManager:         chainManager,
		genesis:              genesis,
		log:                  common.ChainLogger.New("submodule", "momentum-pool"),
	}
}
