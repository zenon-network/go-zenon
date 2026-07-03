// Package chain orchestrates the node's dual ledger. The momentum
// pool owns the persistent momentum chain — a db.Manager whose
// versions are momentums and whose state embeds the
// momentum-confirmed ("stable") state of every account chain — while
// the account pool layers unconfirmed account blocks on top, in one
// in-memory db.Manager per address. The store interfaces both pools
// expose are defined in chain/store.
//
// All ledger writes are serialized through a single chain-wide mutex:
// writers (the pillar's momentum and contract-receive generators, the
// protocol broadcaster and chain bridge) hold the locker returned by
// AcquireInsert across an insertion. Account blocks first enter the
// account pool; when a pillar produces a momentum confirming them,
// the momentum VM copies their patches into the momentum store and
// AddMomentumTransaction commits the result, broadcasting an
// InsertMomentum event to the registered MomentumEventListeners — the
// account pool itself (which rebuilds its managers over the new
// stable state), the consensus points and election caches, the event
// printer and the RPC subscription server. RollbackTo pops momentums
// one by one, emitting a DeleteMomentum event per momentum, upon
// which the account pool discards all unconfirmed state.
package chain

import (
	"fmt"
	"os"
	"sync"

	"github.com/pkg/errors"

	"github.com/zenon-network/go-zenon/chain/store"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/db"
	"github.com/zenon-network/go-zenon/common/types"
)

var (
	inserterLog = common.ChainLogger.New("submodule", "chain-insert-mutex")
)

// chain implements Chain by composing the account pool, the momentum
// pool and the momentum event manager over a shared db.Manager; the
// insert mutex serializes all ledger writers (see AcquireInsert).
type chain struct {
	log common.Logger

	store.Genesis
	*accountPool
	*momentumPool
	*momentumEventManager

	chainManager db.Manager
	insert       sync.Mutex
}

// NewChain assembles the chain module on top of chainManager — the
// momentum-chain db.Manager, persistent in production — wiring the
// account pool to draw its stable account state from the momentum
// pool's frontier. Call Init then Start before use.
func NewChain(chainManager db.Manager, genesis store.Genesis) *chain {
	momentumPool := NewMomentumPool(chainManager, genesis)
	return &chain{
		log:                  common.ChainLogger,
		Genesis:              genesis,
		accountPool:          newAccountPool(momentumPool),
		momentumPool:         momentumPool,
		momentumEventManager: momentumPool.momentumEventManager,
		chainManager:         chainManager,
	}
}

// Init prepares the ledger for use: it verifies (or, on an empty
// database, inserts) the genesis momentum, publishes the genesis
// spork address into types.SporkAddress, registers the account pool
// as a momentum event listener and verifies that every activated
// spork is implemented by this build — if one is not, the node prints
// an upgrade notice and terminates with exit status 2 (see
// GotAllActiveSporksImplemented).
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

// Start is part of the module lifecycle; the chain has no background
// work, so it only logs and returns nil.
func (c *chain) Start() error {
	c.log.Info("starting ...")
	defer c.log.Info("started")

	return nil
}

// Stop unregisters the account pool from momentum events and shuts
// down the underlying database manager.
func (c *chain) Stop() error {
	c.log.Info("stopping ...")
	defer c.log.Info("stopped")

	c.UnRegister(c.accountPool)

	return c.chainManager.Stop()
}

// checkGenesisCompatibility ensures the database matches the
// configured genesis: an empty database receives the genesis momentum
// transaction (under the insert lock), otherwise the momentum at
// height 1 must hash to the configured genesis momentum — if not, the
// node refuses to start and the database must be removed manually.
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

// AcquireInsert blocks until the chain-wide insert mutex is acquired
// and returns a single-use locker that is already held: Unlock
// releases the mutex (once only) and Lock panics. The lock is not
// reentrant — a holder calling AcquireInsert again deadlocks. The
// reason string is only used for logging.
func (c *chain) AcquireInsert(reason string) sync.Locker {
	inserterLog.Debug("waiting", "reason", reason)
	c.insert.Lock()

	inserterLog.Debug("acquired", "reason", reason)
	return &inserter{
		reason: reason,
		mutex:  &c.insert,
	}
}

// inserter is the held-lock token returned by AcquireInsert; methods
// that take an insertLocker check only that it is non-nil — the token
// exists to make lock ownership explicit and traceable in logs.
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
