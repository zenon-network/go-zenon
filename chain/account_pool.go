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
	"github.com/zenon-network/go-zenon/dp"
)

var (
	ErrFailedToAddAccountBlockTransaction = errors.Errorf("failed to insert account-block-transaction")
	ErrPlasmaRatioIsWorse                 = errors.Errorf("plasma ratio is smaller for current block")
	ErrHashTieBreak                       = errors.Errorf("hash tie-break is worse for current block")
	ErrBlockHeightNotFound                = errors.Errorf("block height does not exist in account manager")

	// MaxAccountBlocksInMomentum takes into account batched account-blocks
	// Not used after Dynamic Plasma spork
	MaxAccountBlocksInMomentum = 100

	// MaxUncommittedBlocksPerAccount limits the length of the uncommitted
	// state a user account chain can have to limit node resource consumption.
	MaxUncommittedBlocksPerAccount uint64 = 500
)

type Stable interface {
	GetStableAccountDB(address types.Address) db.DB
	GetFrontierMomentumStore() store.Momentum
}

type accountManager struct {
	db     db.Manager
	blocks map[uint64]*nom.AccountBlock
}

func (am *accountManager) Add(transaction *nom.AccountBlockTransaction) error {
	if err := am.db.Add(transaction); err != nil {
		return err
	}

	block := transaction.Block.Copy()
	am.blocks[block.Height] = block
	for _, d := range block.DescendantBlocks {
		am.blocks[d.Height] = d
	}
	return nil
}

func (am *accountManager) Pop() error {
	frontier := db.GetFrontierIdentifier(am.db.Frontier())
	if err := am.db.Pop(); err != nil {
		return err
	}
	newFrontier := db.GetFrontierIdentifier(am.db.Frontier())
	for height := newFrontier.Height + 1; height <= frontier.Height; height += 1 {
		delete(am.blocks, height)
	}
	return nil
}

func (am *accountManager) BlockByHeight(height uint64) (*nom.AccountBlock, error) {
	block, ok := am.blocks[height]
	if !ok {
		return nil, ErrBlockHeightNotFound
	}
	return block.Copy(), nil
}

type accountPool struct {
	log      log15.Logger
	stable   Stable
	managers map[types.Address]*accountManager
	changes  sync.Mutex

	// plasma is the dynamic-plasma pricing context derived from the
	// committed frontier momentum, or nil while the dynamic-plasma spork
	// is inactive or the momentum store cannot answer. It only changes
	// when the frontier momentum does, so it is refreshed once per
	// committed (or rolled-back) momentum rather than recomputed on every
	// contested insert.
	//
	// plasmaMu guards the field alone: it is never held across a store
	// read or across ap.changes, so it is a leaf lock. Every production
	// writer (momentum insert/rollback) and reader (higherPriority) runs
	// under chain.insert already, so plasmaMu is what keeps that invariant
	// enforced rather than assumed — including for direct unit-test use of
	// the pool, and for -race.
	plasma   dp.DynamicPlasma
	plasmaMu sync.Mutex
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
		return fmt.Errorf(`%w reason:%v; frontier-identifier:%v; identifier:%v`, ErrFailedToAddAccountBlockTransaction, "previous mismatch", frontierIdentifier, identifier)
	}

	return nil
}

// higherPricedBlock reports whether a should replace b under dynamic-plasma
// pricing. An exact price tie is resolved by smallest hash so that nodes
// seeing the two blocks in different orders converge on the same one, the
// rule documented on the AccountPool interface.
func higherPricedBlock(plasma dp.DynamicPlasma, a, b *nom.AccountBlock) error {
	err := plasma.HigherPrice(a, b)
	if err == dp.ErrBlockPriceSame {
		if bytes.Compare(a.Hash.Bytes()[:], b.Hash.Bytes()[:]) > -1 {
			return ErrHashTieBreak
		}
		return nil
	}
	return err
}

// higherPriority reports whether a should replace b as the pool's block
// at their shared height. Once dynamic plasma is active, momentum
// content selection ranks blocks by the same price rule,
// dp.DynamicPlasma.HigherPrice (see pillar/content_selector.go), so
// replacement must use it too — otherwise the pool can accept a
// replacement that content selection would then rank below the block it
// just evicted. An exact price tie is resolved here by smallest hash, the
// fork-resolution rule documented on the AccountPool interface
// (chain/interface.go), deliberately independent of the content
// selector's larger-hash tie-break: the two answer different questions —
// which block survives a fork vs. which block is emitted first. The
// comparator uses the pricing context cached from the committed frontier
// momentum; while that context is empty (dynamic plasma inactive, or the
// momentum store could not answer — genesis construction, for one) momentum
// content selection still uses the legacy TotalPlasma/BasePlasma ratio, so
// that remains the fallback.
func (ap *accountPool) higherPriority(a, b *nom.AccountBlock) error {
	ap.plasmaMu.Lock()
	plasma := ap.plasma
	ap.plasmaMu.Unlock()
	if plasma != nil {
		return higherPricedBlock(plasma, a, b)
	}

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
		// check uncommitted plasma amount. Only applies to fast-forward
		// inserts: a rollback/replacement doesn't lengthen the pending
		// chain, and an already-inserted duplicate is idempotent —
		// neither should be rejected just because the account is at cap.
		if !forceAdd && !types.IsEmbeddedAddress(address) {
			if err := ap.checkUncommittedBlocksCount(address); err != nil {
				return err
			}
		}
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
	if err := ap.higherPriority(block, trueBlock); !forceAdd && err != nil {
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

func (ap *accountPool) getStableAccountStore(address types.Address) store.Account {
	return account.NewAccountStore(address, db.NewMemDBManager(ap.stable.GetStableAccountDB(address)).Frontier())
}
func (ap *accountPool) getFrontierAccountStore(address types.Address) store.Account {
	return account.NewAccountStore(address, ap.getAccountManager(address).db.Frontier())
}

// refreshDynamicPlasma recomputes the pricing context from the committed
// frontier momentum. Called on every momentum insert and rollback, and
// once at chain start-up.
func (ap *accountPool) refreshDynamicPlasma() {
	plasma := ap.computeDynamicPlasma()
	ap.plasmaMu.Lock()
	ap.plasma = plasma
	ap.plasmaMu.Unlock()
}

// computeDynamicPlasma returns the pricing context for the committed
// frontier momentum, or nil when dynamic plasma is inactive or the
// momentum store cannot answer. A store that cannot answer is logged at
// Warn: it means pool replacement falls back to the legacy plasma-ratio
// comparator while momentum content selection keeps ranking by price, so
// the pool can keep a block content selection would rank below the one it
// evicted.
func (ap *accountPool) computeDynamicPlasma() dp.DynamicPlasma {
	store := ap.stable.GetFrontierMomentumStore()
	if store == nil {
		return nil
	}
	active, err := store.IsSporkActive(types.DynamicPlasmaSpork)
	if err != nil {
		ap.log.Warn("using legacy plasma-ratio comparator", "reason", err)
		return nil
	}
	if !active {
		return nil
	}
	previous, err := store.GetFrontierMomentum()
	if err != nil {
		ap.log.Warn("using legacy plasma-ratio comparator", "reason", err)
		return nil
	}
	config, err := store.GetPlasmaVariables()
	if err != nil {
		ap.log.Warn("using legacy plasma-ratio comparator", "reason", err)
		return nil
	}
	return dp.NewDynamicPlasma(previous, config)
}

func (ap *accountPool) InsertMomentum(detailed *nom.DetailedMomentum) {
	ap.refreshDynamicPlasma()

	ap.changes.Lock()
	defer ap.changes.Unlock()

	if err := ap.rebuild(detailed); err != nil {
		common.ChainLogger.Error("failed to handle InsertMomentum in AccountPool", "reason", err)
	}
}
func (ap *accountPool) DeleteMomentum(*nom.DetailedMomentum) {
	ap.refreshDynamicPlasma()

	ap.changes.Lock()
	defer ap.changes.Unlock()

	ap.managers = make(map[types.Address]*accountManager)
}
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

func (ap *accountPool) GetNewMomentumContent() []*nom.AccountBlock {
	return ap.filterBlocksToCommit(ap.GetAllUncommittedAccountBlocks())
}
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
func (ap *accountPool) getUncommittedAccountBlocksByAddress(address types.Address) []*nom.AccountBlock {
	blocks := make([]*nom.AccountBlock, 0)

	stable := ap.getStableAccountStore(address)
	frontier := ap.getFrontierAccountStore(address)
	manager := ap.getAccountManager(address)
	for i := stable.Identifier().Height + 1; i <= frontier.Identifier().Height; i += 1 {
		block, err := manager.BlockByHeight(i)
		common.DealWithErr(err)
		blocks = append(blocks, block)
	}

	return blocks
}

func (ap *accountPool) checkUncommittedBlocksCount(address types.Address) error {
	frontier, err := ap.getFrontierAccountStore(address).Frontier()
	if err != nil {
		ap.log.Info("failed to get frontier block", "reason", err)
		return fmt.Errorf(`%w reason:%v; address:%v`, ErrFailedToAddAccountBlockTransaction, err, address)
	}
	stableFrontier, err := ap.getStableAccountStore(address).Frontier()
	if err != nil {
		ap.log.Info("failed to get stable frontier block", "reason", err)
		return fmt.Errorf(`%w reason:%v; address:%v`, ErrFailedToAddAccountBlockTransaction, err, address)
	}
	if frontier == nil {
		return nil
	}
	// A never-committed account has no stable frontier yet; treat it as height 0
	// so its uncommitted blocks are still counted against the limit.
	var stableHeight uint64
	if stableFrontier != nil {
		stableHeight = stableFrontier.Height
	}
	uncommittedBlockCount := frontier.Height - stableHeight
	if uncommittedBlockCount+1 > MaxUncommittedBlocksPerAccount {
		ap.log.Info("max uncommitted blocks per account reached")
		return fmt.Errorf(`%w reason: max uncommitted blocks per account reached; address:%v`,
			ErrFailedToAddAccountBlockTransaction, address)
	}
	return nil
}

func newAccountPool(stable Stable) *accountPool {
	return &accountPool{
		log:      common.ChainLogger.New("module", "account-pool"),
		stable:   stable,
		managers: make(map[types.Address]*accountManager),
	}
}
func NewAccountPool(stable Stable) AccountPool {
	return newAccountPool(stable)
}
