package chain

import (
	"bytes"
	"fmt"
	"sync"

	"github.com/inconshreveable/log15"
	"github.com/pkg/errors"

	"github.com/zenon-network/go-zenon/chain/account"
	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/chain/store"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/db"
	"github.com/zenon-network/go-zenon/common/types"
)

var (
	// ErrFailedToAddAccountBlockTransaction wraps every structural
	// insertion failure of the account pool (older than the stable
	// state, missing or mismatched previous block, rollback failure);
	// the wrapped message carries the specific reason.
	ErrFailedToAddAccountBlockTransaction = errors.Errorf("failed to insert account-block-transaction")
	// ErrPlasmaRatioIsWorse rejects a forking account block whose
	// TotalPlasma/BasePlasma ratio is lower than the unconfirmed
	// block it would replace.
	ErrPlasmaRatioIsWorse = errors.Errorf("plasma ratio is smaller for current block")
	// ErrHashTieBreak rejects a forking account block that ties on
	// plasma ratio but does not have the strictly smaller hash.
	ErrHashTieBreak = errors.Errorf("hash tie-break is worse for current block")
	// ErrBlockHeightNotFound reports a request for a height that has
	// no unconfirmed block cached in an account manager — at or below
	// the stable height, or above the pool frontier.
	ErrBlockHeightNotFound = errors.Errorf("block height does not exist in account manager")

	// MaxAccountBlocksInMomentum caps how many account blocks
	// GetNewMomentumContent proposes for one momentum. The limit
	// takes into account batched account-blocks: a batch of
	// contract-sends is never split, so the proposal stops before a
	// batch that would exceed the cap.
	MaxAccountBlocksInMomentum = 100
)

// Stable supplies the momentum-confirmed database of an account
// chain — the base under the account pool's in-memory managers. The
// momentum pool implements it with the frontier momentum store's
// per-address subset; chain/genesis uses an empty mock while
// generating the genesis state.
type Stable interface {
	GetStableAccountDB(address types.Address) db.DB
}

// accountManager is the account pool's per-address unit: an
// in-memory db.Manager rooted at the stable (momentum-confirmed)
// account state, whose versions are the unconfirmed account blocks,
// plus a height-indexed cache of those blocks so reads avoid
// deserializing from the db.
type accountManager struct {
	db     db.Manager
	blocks map[uint64]*nom.AccountBlock
}

// Add appends an unconfirmed account block to the manager's db and
// caches the block — and its descendant blocks — by height.
func (am *accountManager) Add(transaction *nom.AccountBlockTransaction) error {
	if err := am.db.Add(transaction); err != nil {
		return err
	}
	am.blocks[transaction.Block.Height] = transaction.Block
	for _, d := range transaction.Block.DescendantBlocks {
		am.blocks[d.Height] = d
	}
	return nil
}

// Pop rolls back the manager's frontier block and evicts it from the
// height cache.
func (am *accountManager) Pop() error {
	frontier := db.GetFrontierIdentifier(am.db.Frontier()).Height
	if err := am.db.Pop(); err != nil {
		return err
	}
	delete(am.blocks, frontier)
	return nil
}

// BlockByHeight returns the cached unconfirmed block at the given
// height, or ErrBlockHeightNotFound if the manager holds none.
func (am *accountManager) BlockByHeight(height uint64) (*nom.AccountBlock, error) {
	block, ok := am.blocks[height]
	if !ok {
		return nil, ErrBlockHeightNotFound
	}
	return block, nil
}

// accountPool implements AccountPool with one lazily-created
// accountManager per address, rooted at the stable
// (momentum-confirmed) account state; the managers hold the
// unconfirmed account blocks. It also implements
// MomentumEventListener: chain.Init registers it so each committed
// momentum rebuilds the managers over the new stable state and each
// rollback discards them.
type accountPool struct {
	log      log15.Logger
	stable   Stable
	managers map[types.Address]*accountManager
	changes  sync.Mutex
}

func (ap *accountPool) getAccountManager(address types.Address) *accountManager {
	manager := ap.managers[address]
	if manager == nil {
		manager = &accountManager{
			db:     db.NewMemDBManager(ap.stable.GetStableAccountDB(address)),
			blocks: make(map[uint64]*nom.AccountBlock),
		}
		ap.managers[address] = manager
	}
	return manager
}

// canRollback checks that a forking block could structurally replace
// the unconfirmed tip: it must be above the stable
// (momentum-confirmed) height and its previous block must exist on
// the current frontier with a matching identifier.
func (ap *accountPool) canRollback(block *nom.AccountBlock) error {
	log := ap.log.New("header", block.Header())
	address := block.Address
	identifier := block.Identifier()
	previous := block.Previous()

	stable := ap.getStableAccountStore(address)
	stableIdentifier := stable.Identifier()

	// can't insert at all since it's too old
	if stableIdentifier.Height >= identifier.Height {
		log.Info("failed to insert account-block-transaction", "reason", "older than stable identifier", "stable-identifier", stableIdentifier)
		return fmt.Errorf(`%w reason:%v; stable-identifier:%v; identifier:%v`, ErrFailedToAddAccountBlockTransaction, "older than stable identifier", stableIdentifier, identifier)
	}

	frontier := ap.getFrontierAccountStore(address)
	frontierIdentifier := frontier.Identifier()

	// previous doesn't match
	truePrevious, err := frontier.ByHeight(identifier.Height - 1)
	if err != nil {
		log.Info("failed to insert account-block-transaction", "reason", err, "frontier-identifier", frontierIdentifier)
		return fmt.Errorf(`%w reason:%v; frontier-identifier:%v; identifier:%v`, ErrFailedToAddAccountBlockTransaction, err, frontierIdentifier, identifier)
	}
	if truePrevious == nil {
		log.Info("failed to insert account-block-transaction", "reason", "no previous", "frontier-identifier", frontierIdentifier)
		return fmt.Errorf(`%w reason:%v; frontier-identifier:%v; identifier:%v`, ErrFailedToAddAccountBlockTransaction, "missing previous", frontierIdentifier, identifier)
	}
	if truePrevious.Identifier() != previous {
		log.Info("failed to insert account-block-transaction", "reason", "previous mismatch", "frontier-identifier", frontierIdentifier)
		return fmt.Errorf(`%w reason:%v; frontier-identifier:%v; identifier:%v`, ErrFailedToAddAccountBlockTransaction, "missing previous", frontierIdentifier, identifier)
	}

	return nil
}

// higherPriority decides forks deterministically: nil if a beats b by
// a strictly higher TotalPlasma/BasePlasma ratio (compared via cross
// multiplication), or by a strictly smaller hash on equal ratios.
func higherPriority(a, b *nom.AccountBlock) error {
	if a.TotalPlasma*b.BasePlasma < b.TotalPlasma*a.BasePlasma {
		return ErrPlasmaRatioIsWorse
	} else if a.TotalPlasma*b.BasePlasma == b.TotalPlasma*a.BasePlasma && bytes.Compare(a.Hash.Bytes()[:], b.Hash.Bytes()[:]) > -1 {
		return ErrHashTieBreak
	}

	return nil
}

func (ap *accountPool) AddAccountBlockTransaction(insertLocker sync.Locker, transaction *nom.AccountBlockTransaction) error {
	if insertLocker == nil {
		return errors.Errorf("insertLocker can't be nil")
	}
	ap.changes.Lock()
	defer ap.changes.Unlock()
	return ap.addAccountBlockTransaction(transaction, false)
}
func (ap *accountPool) ForceAddAccountBlockTransaction(insertLocker sync.Locker, transaction *nom.AccountBlockTransaction) error {
	if insertLocker == nil {
		return errors.Errorf("insertLocker can't be nil")
	}
	ap.changes.Lock()
	defer ap.changes.Unlock()
	return ap.addAccountBlockTransaction(transaction, true)
}

// addAccountBlockTransaction inserts one block: fast-forward when its
// previous is the frontier, no-op when already present, otherwise a
// fork — allowed only above the stable height, with a valid previous
// and (unless forceAdd) a higherPriority win — that pops the
// conflicting unconfirmed blocks before inserting.
func (ap *accountPool) addAccountBlockTransaction(transaction *nom.AccountBlockTransaction, forceAdd bool) error {
	block := transaction.Block
	address := block.Address
	identifier := block.Identifier()
	previous := block.Previous()

	log := ap.log.New("header", block.Header())

	frontier := ap.getFrontierAccountStore(address)
	frontierIdentifier := frontier.Identifier()

	// fast-forward insert on top of chain
	if previous == frontierIdentifier {
		log.Info("fast-forward inserting account-block")
		return ap.getAccountManager(address).Add(transaction)
	}

	// already inserted
	trueBlock, err := frontier.ByHeight(identifier.Height)
	if err != nil {
		log.Info("failed to insert account-block-transaction", "reason", err, "frontier-identifier", frontierIdentifier)
		return fmt.Errorf(`%w reason:%v; frontier-identifier:%v; identifier:%v`, ErrFailedToAddAccountBlockTransaction, err, frontierIdentifier, identifier)
	}
	if trueBlock != nil && trueBlock.Identifier() == identifier {
		log.Info("account-block is already inserted")
		return nil
	}

	if err := ap.canRollback(block); err != nil {
		return err
	}
	if err := higherPriority(block, trueBlock); !forceAdd && err != nil {
		log.Info("failed to insert account-block-transaction", "reason", err, "frontier-identifier", frontierIdentifier)
		return err
	}

	// rollback blocks and insert this one
	manager := ap.getAccountManager(address)
	for {
		currentIdentifier := db.GetFrontierIdentifier(manager.db.Frontier())
		if currentIdentifier == previous {
			break
		}
		log.Info("rolling back account-block-transaction", "current-identifier", currentIdentifier)
		err = manager.Pop()
		if err != nil {
			log.Info("failed to insert account-block-transaction. can't pop manager", "reason", err, "frontier-identifier", currentIdentifier)
			return fmt.Errorf(`%w can't pop manager; reason:%v; frontier-identifier:%v; identifier:%v`, ErrFailedToAddAccountBlockTransaction, err, currentIdentifier, identifier)
		}
	}

	log.Info("inserting account-block after rollback")
	return ap.getAccountManager(address).Add(transaction)
}

func (ap *accountPool) GetPatch(address types.Address, identifier types.HashHeight) db.Patch {
	ap.changes.Lock()
	defer ap.changes.Unlock()

	return ap.getAccountManager(address).db.GetPatch(identifier)
}
func (ap *accountPool) GetAccountStore(address types.Address, identifier types.HashHeight) store.Account {
	ap.changes.Lock()
	defer ap.changes.Unlock()

	stable := ap.getStableAccountStore(address)
	stableIdentifier := stable.Identifier()
	if stableIdentifier == identifier {
		return stable
	} else if stableIdentifier.Height > identifier.Height {
		ap.log.Info("unable to get account store", "address", address, "stable-identifier", stableIdentifier, "reason", "older than most stable account")
		return nil
	}

	manager := ap.getAccountManager(address)
	accountDb := manager.db.Get(identifier)
	if accountDb == nil {
		frontier := db.GetFrontierIdentifier(manager.db.Frontier())
		ap.log.Info("unable to get account store", "address", address, "frontier-identifier", frontier, "reason", "missing-db")
		return nil
	}
	return account.NewAccountStore(address, accountDb)
}
func (ap *accountPool) GetFrontierAccountStore(address types.Address) store.Account {
	ap.changes.Lock()
	defer ap.changes.Unlock()

	return ap.getFrontierAccountStore(address)
}

// getStableAccountStore returns the momentum-confirmed account state,
// without any pooled blocks.
func (ap *accountPool) getStableAccountStore(address types.Address) store.Account {
	return account.NewAccountStore(address, db.NewMemDBManager(ap.stable.GetStableAccountDB(address)).Frontier())
}
func (ap *accountPool) getFrontierAccountStore(address types.Address) store.Account {
	return account.NewAccountStore(address, ap.getAccountManager(address).db.Frontier())
}

// InsertMomentum implements MomentumEventListener: after a momentum
// is committed the stable account state has advanced, so the pool
// rebuilds its in-memory managers on top of it.
func (ap *accountPool) InsertMomentum(detailed *nom.DetailedMomentum) {
	ap.changes.Lock()
	defer ap.changes.Unlock()

	if err := ap.rebuild(detailed); err != nil {
		common.ChainLogger.Error("failed to handle InsertMomentum in AccountPool", "reason", err)
	}
}

// DeleteMomentum implements MomentumEventListener: a momentum
// rollback invalidates everything layered on the old stable state, so
// all managers — and with them all unconfirmed account blocks — are
// dropped.
func (ap *accountPool) DeleteMomentum(*nom.DetailedMomentum) {
	ap.changes.Lock()
	defer ap.changes.Unlock()

	ap.managers = make(map[types.Address]*accountManager)
}

// rebuild re-bases every per-address manager after a momentum commit:
// it collects the blocks still above the new stable height, discards
// the old manager and re-applies those blocks (with their original
// patches) on a fresh manager rooted at the advanced stable state.
// Blocks the momentum confirmed are below the new stable height and
// thus drop out of the pool.
func (ap *accountPool) rebuild(detailed *nom.DetailedMomentum) error {
	addresses := make([]types.Address, 0, len(ap.managers))
	for address := range ap.managers {
		addresses = append(addresses, address)
	}

	ap.log.Debug("started rebuilding account-pool", "momentum-identifier", detailed.Momentum.Identifier())
	for _, address := range addresses {
		log := ap.log.New("address", address)
		log.Debug("start rebuilding")

		uncommitted := make([]*nom.AccountBlock, 0)
		oldManager := ap.managers[address]

		stable := account.NewAccountStore(address, ap.stable.GetStableAccountDB(address))
		uncommittedStore := account.NewAccountStore(address, oldManager.db.Frontier())
		for i := stable.Identifier().Height + 1; i <= uncommittedStore.Identifier().Height; i += 1 {
			block, err := oldManager.BlockByHeight(i)
			common.DealWithErr(err)
			uncommitted = append(uncommitted, block)
		}

		delete(ap.managers, address)

		if len(uncommitted) == 0 {
			log.Debug("no uncommitted changes")
			continue
		}

		log.Debug("staring applying blocks", "num-uncommitted", len(uncommitted))
		manager := &accountManager{
			db:     db.NewMemDBManager(ap.stable.GetStableAccountDB(address)),
			blocks: make(map[uint64]*nom.AccountBlock),
		}
		for _, block := range uncommitted {
			patch := oldManager.db.GetPatch(block.Identifier())
			err := manager.Add(&nom.AccountBlockTransaction{
				Block:   block,
				Changes: patch,
			})
			if err != nil {
				return errors.Errorf("account pool rebuild error. Unable to re-apply block %v. Reason %v", block.Header(), err)
			}
		}
		ap.managers[address] = manager

		log.Debug("successfully rebuild", "num-uncommitted", len(uncommitted))
	}

	ap.log.Debug("finished rebuilding account-pool")
	return nil
}

// GetNewMomentumContent returns the uncommitted account blocks the
// pillar should confirm in its next momentum, capped at
// MaxAccountBlocksInMomentum without splitting contract-send batches.
func (ap *accountPool) GetNewMomentumContent() []*nom.AccountBlock {
	return ap.filterBlocksToCommit(ap.GetAllUncommittedAccountBlocks())
}

// filterBlocksToCommit enforces the momentum content cap, treating a
// run of contract-send blocks plus the block that ends the run as one
// atomic batch: it stops before the batch that would push the total
// past MaxAccountBlocksInMomentum.
func (ap *accountPool) filterBlocksToCommit(blocks []*nom.AccountBlock) []*nom.AccountBlock {
	toCommit := make([]*nom.AccountBlock, 0, len(blocks))
	batch := make([]*nom.AccountBlock, 0, MaxAccountBlocksInMomentum)
	for index := range blocks {
		batch = append(batch, blocks[index])
		if blocks[index].BlockType != nom.BlockTypeContractSend {
			if len(toCommit)+len(batch) > MaxAccountBlocksInMomentum {
				break
			}
			toCommit = append(toCommit, batch...)
			batch = batch[:0]
		}
	}
	return toCommit
}

func (ap *accountPool) GetAllUncommittedAccountBlocks() []*nom.AccountBlock {
	ap.changes.Lock()
	defer ap.changes.Unlock()

	blocks := make([]*nom.AccountBlock, 0)
	for address := range ap.managers {
		blocks = append(blocks, ap.getUncommittedAccountBlocksByAddress(address)...)
	}

	return blocks
}
func (ap *accountPool) GetUncommittedAccountBlocksByAddress(address types.Address) []*nom.AccountBlock {
	ap.changes.Lock()
	defer ap.changes.Unlock()

	return ap.getUncommittedAccountBlocksByAddress(address)
}

// getUncommittedAccountBlocksByAddress returns the address's pooled
// blocks in ascending height order — everything between the stable
// (momentum-confirmed) height and the pool frontier.
func (ap *accountPool) getUncommittedAccountBlocksByAddress(address types.Address) []*nom.AccountBlock {
	blocks := make([]*nom.AccountBlock, 0)

	stable := ap.getStableAccountStore(address)
	frontier := ap.getFrontierAccountStore(address)
	for i := stable.Identifier().Height + 1; i <= frontier.Identifier().Height; i += 1 {
		block, err := ap.getAccountManager(address).BlockByHeight(i)
		common.DealWithErr(err)
		blocks = append(blocks, block)
	}

	return blocks
}

func newAccountPool(stable Stable) *accountPool {
	return &accountPool{
		log:      common.ChainLogger.New("module", "account-pool"),
		stable:   stable,
		managers: make(map[types.Address]*accountManager),
	}
}

// NewAccountPool returns a standalone AccountPool over the given
// stable state. The chain builds its own pool internally; this
// constructor exists for chain/genesis, which uses a pool with an
// empty stable base to assemble the genesis account blocks.
func NewAccountPool(stable Stable) AccountPool {
	return newAccountPool(stable)
}
