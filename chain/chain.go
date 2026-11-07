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
// which the account pool drops the pending blocks the rollback
// invalidated and keeps the rest.
package chain

import (
	stderrors "errors"
	"fmt"
	"strings"
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

// ErrSporkAddressMismatch is returned by Init when the genesis config
// names a different spork address than the one this database was
// first started with. The genesis momentum hash does not commit to the
// spork address, so without this check a node could reopen its
// database under a new spork authority and only split from the
// network at the first spork transaction.
var ErrSporkAddressMismatch = errors.New("the genesis spork address differs from the one this database was initialized with")

// sporkAddressMetadataKey is the db.MetadataStore key under which the
// chain database records the spork address it was first started with.
var sporkAddressMetadataKey = []byte("genesis-spork-address")

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

// NewChain assembles the chain module on top of chainManager — the
// momentum-chain db.Manager, persistent in production — and
// cacheManager, which backs the chain cache, wiring the account pool
// to draw its stable account state from the momentum pool's frontier.
// Call Init then Start before use.
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
	if err := c.checkSporkAddressBinding(); err != nil {
		return err
	}
	types.SporkAddress = c.genesis.GetSporkAddress()
	c.Register(c.accountPool)
	// Prime the pool's pricing context from the committed frontier so the
	// first contested insert after start-up is priced the same way as one
	// arriving after the next momentum, rather than falling back to the
	// legacy comparator until then.
	c.accountPool.refreshDynamicPlasma()

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

		var sb strings.Builder
		fmt.Fprintf(&sb, "can't start node: unimplemented spork(s) active at height %d hash %s:\n", frontier.Height, frontier.Hash)
		for _, spork := range unimplemented {
			fmt.Fprintf(&sb, "  Spork name `%v` id:`%v`\n", spork.Name, spork.Id)
		}
		fmt.Fprintf(&sb, "Please upgrade your znnd binary")
		return fmt.Errorf("%s", sb.String())
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

	// Every manager is stopped whatever the earlier ones report: a manager
	// marks itself stopped even when its Close fails, so bailing out on the
	// first error would leave the remaining handles open until exit. The
	// caller sees every error that occurred.
	return stderrors.Join(c.cacheManager.Stop(), c.chainManager.Stop())
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

// checkSporkAddressBinding binds the chain database to the genesis
// spork address. The first start of a database (and the first start of
// an existing database on a binary that has this check) records the
// configured address; every later start must configure the same one or
// fails with ErrSporkAddressMismatch. The record is node-local metadata
// outside the versioned state, so it changes no hash and is not shared
// with peers: it keeps one node from silently switching spork
// authority under an existing database, but two nodes started from
// genesis files that differ only in SporkAddress still accept the same
// chain until the first spork transaction. Managers that do not
// implement db.MetadataStore are not bound; the persistent LevelDB
// manager the node runs on does implement it.
func (c *chain) checkSporkAddressBinding() error {
	metadata, ok := c.chainManager.(db.MetadataStore)
	if !ok {
		return nil
	}
	configured := c.genesis.GetSporkAddress()
	if configured == nil {
		return errors.Errorf("the genesis config has no spork address")
	}

	stored, err := metadata.GetMetadata(sporkAddressMetadataKey)
	if err != nil {
		return err
	}
	if stored == nil {
		c.log.Info("binding chain database to genesis spork address", "spork-address", configured)
		return metadata.PutMetadata(sporkAddressMetadataKey, configured.Bytes())
	}

	bound, err := types.BytesToAddress(stored)
	if err != nil {
		return errors.Wrapf(err, "corrupt spork-address record in chain database")
	}
	if bound != *configured {
		c.log.Crit("genesis spork address does not match the chain database",
			"configured", configured, "database", bound)
		return errors.Wrapf(ErrSporkAddressMismatch,
			"database was initialized with %v but the genesis config has %v. "+
				"Restore SporkAddress %v in the genesis config, or remove the database manually to start a new chain",
			bound, configured, bound)
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
