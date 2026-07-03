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

// momentumPool implements MomentumPool over the momentum-chain
// db.Manager and embeds the momentum event manager that fans out
// Insert/DeleteMomentum events. It also implements Stable for the
// account pool by exposing the frontier's per-address account
// databases. The changes mutex guards the manager against concurrent
// readers; writers are additionally serialized by the chain insert
// lock.
type momentumPool struct {
	*momentumEventManager
	chainManager db.Manager
	genesis      store.Genesis
	log          log15.Logger
	changes      sync.Mutex
}

// AddMomentumTransaction commits a verified momentum on top of the
// frontier and broadcasts the InsertMomentum event (with the changes
// mutex temporarily released, since listeners read back from the
// pool). It then re-checks spork coverage: if an activated spork past
// its enforcement height is not implemented by this build, the node
// prints an upgrade notice and terminates with exit status 2. The
// caller must hold the chain insert lock; only non-nilness of the
// locker is checked.
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

// RollbackTo pops momentums from the frontier down to identifier,
// which must match an existing momentum, broadcasting a
// DeleteMomentum event per popped momentum (most recent first). The
// chain bridge uses it to switch to a heavier fork during sync. The
// caller must hold the chain insert lock; only non-nilness of the
// locker is checked.
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

// GotAllActiveSporksImplemented checks every spork defined on-chain
// against types.ImplementedSporksMap: unimplemented collects the
// activated sporks at or past their enforcement height that this
// build does not know how to enforce, and justNow is the spork whose
// enforcement height equals the frontier height, if any. It is called
// at chain Init and after every momentum insertion; a non-nil
// unimplemented list makes the node terminate with exit status 2.
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

// getFrontierStore wraps the manager's current frontier snapshot in a
// momentum store; callers must hold the changes mutex.
func (c *momentumPool) getFrontierStore() store.Momentum {
	if momentumDB := c.chainManager.Frontier(); momentumDB == nil {
		return nil
	} else {
		return momentum.NewStore(c.genesis, momentumDB)
	}
}

// GetFrontierMomentumStore returns a store over the latest committed
// momentum version; the snapshot is copy-on-write, so it stays
// consistent while the chain advances.
func (c *momentumPool) GetFrontierMomentumStore() store.Momentum {
	c.changes.Lock()
	defer c.changes.Unlock()
	return c.getFrontierStore()
}

// GetMomentumStore returns a store over an arbitrary committed
// momentum version, or nil if the identifier is unknown.
func (c *momentumPool) GetMomentumStore(identifier types.HashHeight) store.Momentum {
	c.changes.Lock()
	defer c.changes.Unlock()
	momentumDB := c.chainManager.Get(identifier)
	if momentumDB == nil {
		return nil
	}

	return momentum.NewStore(c.genesis, momentumDB)
}

// GetStableAccountDB implements Stable: it returns the
// momentum-confirmed database of the given account chain, taken from
// the current frontier; the account pool roots its in-memory managers
// at it.
func (c *momentumPool) GetStableAccountDB(address types.Address) db.DB {
	c.changes.Lock()
	defer c.changes.Unlock()
	return c.getFrontierStore().GetAccountDB(address)
}

// NewMomentumPool returns a momentum pool over chainManager with a
// fresh event manager and no listeners; NewChain is its only caller.
func NewMomentumPool(chainManager db.Manager, genesis store.Genesis) *momentumPool {
	return &momentumPool{
		momentumEventManager: newMomentumEventManager(),
		chainManager:         chainManager,
		genesis:              genesis,
		log:                  common.ChainLogger.New("submodule", "momentum-pool"),
	}
}
