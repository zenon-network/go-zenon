package chain

import (
	"bytes"
	"fmt"
	"sync"

	"github.com/ethereum/go-ethereum/rlp"
	lru "github.com/hashicorp/golang-lru"
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
	// price — equal TotalPlasma/BasePlasma ratio before Dynamic Plasma,
	// equal priced value under it — but does not have the strictly
	// smaller hash.
	ErrHashTieBreak = errors.Errorf("hash tie-break is worse for current block")
	// ErrBlockHeightNotFound reports a request for a height that has
	// no unconfirmed block cached in an account manager — at or below
	// the stable height, or above the pool frontier.
	ErrBlockHeightNotFound = errors.Errorf("block height does not exist in account manager")

	// MaxAccountBlocksInMomentum takes into account batched account-blocks
	// Not used after Dynamic Plasma spork
	MaxAccountBlocksInMomentum = 100

	// MaxUncommittedBlocksPerAccount limits the length of the uncommitted
	// state a user account chain can have to limit node resource consumption.
	MaxUncommittedBlocksPerAccount uint64 = 500
)

// maxDetailedMomentumWireSize is the largest RLP-encoded DetailedMomentum
// that fits in a protocol message.  It mirrors protocol.MaxDetailedMomentumWireSize
// but is defined locally because chain cannot import protocol (import cycle).
const maxDetailedMomentumWireSize = 10*1024*1024 - 2*1024*1024 - 4096 // 8,384,512

// oversizedReportMemory is how many distinct over-budget pending blocks
// the pool remembers having warned about, so that a block that stays
// pending is reported once rather than at every momentum.
const oversizedReportMemory = 1024

// Stable supplies the momentum-confirmed database of an account
// chain — the base under the account pool's in-memory managers. The
// momentum pool implements it with the frontier momentum store's
// per-address subset; chain/genesis uses an empty mock while
// generating the genesis state.
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

// clone returns an independent manager holding the same uncommitted state:
// the versioned DB is cloned and the block index is copied. The indexed
// blocks themselves are shared between the two managers, which is safe
// because Add stores its own copy and BlockByHeight hands out copies, so no
// caller ever writes to an indexed block.
func (am *accountManager) clone() *accountManager {
	cloneable, ok := am.db.(db.Cloneable)
	if !ok {
		return nil
	}
	blocks := make(map[uint64]*nom.AccountBlock, len(am.blocks))
	for height, block := range am.blocks {
		blocks[height] = block
	}
	return &accountManager{
		db:     cloneable.Clone(),
		blocks: blocks,
	}
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

	// oversizedReported remembers the hashes of pending blocks already
	// reported as over the momentum wire budget (see reportOversizedBatch),
	// created on first use. It bounds the warnings, not the work: with
	// more than oversizedReportMemory distinct such blocks pending, the
	// oldest entries are evicted and their blocks are reported again.
	oversizedReported     *lru.Cache
	oversizedReportedOnce sync.Once
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

	// A batch may start below the block's own height, so its previous is
	// checked against the stable frontier as well: a rollback can only
	// reach the stable identifier, never below it, and it must be refused
	// here, before the rollback pops the account's uncommitted blocks.
	if previous.Height < stableIdentifier.Height {
		log.Info("failed to insert account-block-transaction", "reason", "previous below stable identifier", "stable-identifier", stableIdentifier)
		return fmt.Errorf(`%w reason:%v; stable-identifier:%v; identifier:%v`, ErrFailedToAddAccountBlockTransaction, "previous below stable identifier", stableIdentifier, identifier)
	}

	frontier := ap.getFrontierAccountStore(address)
	frontierIdentifier := frontier.Identifier()

	// previous doesn't match. A block that carries descendants reports the
	// batch's previous, which sits below the first descendant rather than one
	// height under the block itself, so the lookup goes by the previous' own
	// height. The zero identifier opens an account and has no block to match;
	// the guard above has already required the stable frontier to be empty.
	if previous.Height == 0 {
		if previous != types.ZeroHashHeight {
			log.Info("failed to insert account-block-transaction", "reason", "previous mismatch", "frontier-identifier", frontierIdentifier)
			return fmt.Errorf(`%w reason:%v; frontier-identifier:%v; identifier:%v`, ErrFailedToAddAccountBlockTransaction, "missing previous", frontierIdentifier, identifier)
		}
		return nil
	}
	truePrevious, err := frontier.ByHeight(previous.Height)
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
		// A block's identifier covers only its hashed fields. Fields that are
		// stored but not hashed can still differ between two copies, and the
		// stored bytes feed into the momentum changes-hash. A forced insert
		// carries the copy the momentum was built from, so it must replace a
		// byte-different copy instead of being treated as a duplicate.
		if !forceAdd || trueBlock.EqualBytes(block) {
			log.Info("account-block is already inserted")
			return nil
		}
		log.Info("replacing account-block with different bytes but same identifier")
	}

	if err := ap.canRollback(block); err != nil {
		return err
	}
	// The competitor is the pool block at the first height the incoming
	// block occupies, the one just above its previous: for a plain block
	// that is its own height, for a batch it is the first descendant's,
	// so the comparison does not depend on how long the competing chain
	// is. The fast-forward path above has already taken every case in
	// which that height is empty.
	competitor, err := frontier.ByHeight(previous.Height + 1)
	if err != nil {
		log.Info("failed to insert account-block-transaction", "reason", err, "frontier-identifier", frontierIdentifier)
		return fmt.Errorf(`%w reason:%v; frontier-identifier:%v; identifier:%v`, ErrFailedToAddAccountBlockTransaction, err, frontierIdentifier, identifier)
	}
	if competitor != nil {
		if err := ap.higherPriority(block, competitor); !forceAdd && err != nil {
			log.Info("failed to insert account-block-transaction", "reason", err, "frontier-identifier", frontierIdentifier)
			return err
		}
	}

	manager := ap.getAccountManager(address)

	// A same-identifier replacement leaves the hash the later uncommitted
	// blocks chain onto unchanged, so they stay valid: keep them with their
	// patches and put them back after the replacement. A different block at
	// this height invalidates them and they are dropped as before.
	var keep []*nom.AccountBlockTransaction
	if trueBlock != nil && trueBlock.Identifier() == identifier {
		// Walk down from the frontier by previous links. A batch is stored
		// as its descendants at their own heights plus the head carrying
		// them in its record, and the head's Previous() points below its
		// descendants, so the walk visits only the heads; the head alone,
		// with its patch, puts the whole batch back. The walk yields the
		// blocks top down and they are re-added bottom up.
		for height := frontierIdentifier.Height; height > identifier.Height; {
			later, err := frontier.ByHeight(height)
			if err != nil || later == nil {
				log.Info("failed to insert account-block-transaction", "reason", "can't read later pending block", "height", height, "err", err)
				return fmt.Errorf(`%w reason:%v; frontier-identifier:%v; identifier:%v`, ErrFailedToAddAccountBlockTransaction, "can't read later pending block", frontierIdentifier, identifier)
			}
			// The walk terminates only when each block's previous sits
			// strictly below the block itself. The VM guarantees this for
			// well-formed batches, but the loop itself must not rely on
			// that invariant: a block whose previous is at or above its
			// own height would spin here forever while holding ap.changes.
			// Check before any allocation so a violated invariant fails
			// fast, matching the neighbouring error shape.
			if later.Previous().Height >= later.Height {
				log.Info("failed to insert account-block-transaction", "reason", "non-descending previous height", "height", later.Previous().Height, "block-height", later.Height)
				return fmt.Errorf(`%w reason:%v; frontier-identifier:%v; identifier:%v`, ErrFailedToAddAccountBlockTransaction, "non-descending previous height", frontierIdentifier, identifier)
			}
			// A block without its patch cannot go back with its state
			// changes, so the replacement fails here, before the rollback.
			stored := manager.db.GetPatch(later.Identifier())
			if stored == nil {
				log.Info("failed to insert account-block-transaction", "reason", "missing later pending patch", "height", later.Height)
				return fmt.Errorf(`%w reason:%v; frontier-identifier:%v; identifier:%v`, ErrFailedToAddAccountBlockTransaction, "missing later pending patch", frontierIdentifier, identifier)
			}
			// Add appends to the patch it is given and stores it, and a
			// snapshot clone shares the stored patch objects, so the
			// re-add works on a copy. Dump aliases the patch buffer and
			// Load aliases its input, hence the explicit byte copy.
			patch, err := db.NewPatchFromDump(append([]byte(nil), stored.Dump()...))
			if err != nil {
				log.Info("failed to insert account-block-transaction", "reason", "can't copy later pending patch", "height", later.Height, "err", err)
				return fmt.Errorf(`%w reason:%v; frontier-identifier:%v; identifier:%v`, ErrFailedToAddAccountBlockTransaction, "can't copy later pending patch", frontierIdentifier, identifier)
			}
			keep = append(keep, &nom.AccountBlockTransaction{Block: later, Changes: patch})
			height = later.Previous().Height
		}
		for i, j := 0, len(keep)-1; i < j; i, j = i+1, j-1 {
			keep[i], keep[j] = keep[j], keep[i]
		}
	}

	// rollback blocks and insert this one
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
	if err := manager.Add(transaction); err != nil {
		return err
	}
	for _, later := range keep {
		if err := manager.Add(later); err != nil {
			// The replacement itself is in place, which is what the caller
			// asked for, so this returns nil: the blocks above it are lost
			// the way they were before they were kept at all, and only the
			// log says so.
			log.Warn("dropping later pending account-blocks after replacement", "reason", err, "header", later.Block.Header())
			break
		}
	}
	return nil
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
//
// Staleness bound: the momentum pool moves its frontier and then notifies
// listeners with its lock released, and this listener reads the frontier
// at that moment, so when the store answers the cache is exact at every
// event boundary, including each pop of a multi-momentum rollback. It can
// lag the frontier only between a frontier change and the listener call
// that follows it. No production reader observes that window because
// higherPriority runs under the chain insert lock, and so does every
// momentum insert and rollback; that lock is a caller contract (see
// chain.AcquireInsert), not something this code can check. A failed
// momentum insert that is compensated by popping the momentum again emits
// no event and leaves the frontier where it was, so the unchanged cache is
// still exact. The bound is accepted rather than closed with a per-read
// store lookup, which is what this cache replaced.
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
func (ap *accountPool) DeleteMomentum(detailed *nom.DetailedMomentum) {
	ap.refreshDynamicPlasma()

	ap.changes.Lock()
	defer ap.changes.Unlock()

	// Only remove managers for addresses whose account blocks were included
	// in the deleted momentum.  Wiping every manager discards pending blocks
	// that are unrelated to the rollback and leaves the subsequent rebuild
	// with nothing to iterate over.
	if detailed == nil {
		return
	}
	touched := make(map[types.Address]struct{})
	rolledBackSends := make(map[types.Hash]struct{})
	for _, block := range detailed.AccountBlocks {
		touched[block.Address] = struct{}{}
		if block.IsSendBlock() {
			rolledBackSends[block.Hash] = struct{}{}
		}
	}
	for address := range touched {
		delete(ap.managers, address)
	}

	// Evict managers holding pending receives whose from-block (send) was
	// rolled back.  Such receives are orphaned: their from-block no longer
	// exists on the committed chain, so including them in the next momentum
	// would fail with "Can't find from-block in store" and stall block
	// production.
	if len(rolledBackSends) > 0 {
		for address, manager := range ap.managers {
			for _, block := range manager.blocks {
				if block.IsReceiveBlock() && block.BlockType != nom.BlockTypeGenesisReceive {
					if _, ok := rolledBackSends[block.FromBlockHash]; ok {
						delete(ap.managers, address)
						break
					}
				}
			}
		}
	}

	// Evict blocks whose MomentumAcknowledged is no longer canonical after
	// the rollback.  Rollback pops from the top, so at DeleteMomentum(popped)
	// every pending block with MA.Height >= popped.Height acknowledges a
	// momentum that is no longer canonical.  After the last pop every retained
	// block acknowledges a height at or below the target, where the chain is
	// byte-identical by construction (RollbackTo verifies the target hash).
	// A height-only suffix truncation closes the window without a store
	// lookup.  This runs during DeleteMomentum (not just during rebuild) so
	// that stale-acknowledged blocks are removed BEFORE momentum selection
	// can pick them up.
	poppedHeight := detailed.Momentum.Height
	for address, manager := range ap.managers {
		// Find the lowest height whose block acknowledges a stale momentum.
		// Blocks form a chain and MA is non-decreasing along it, so stale
		// blocks are a contiguous suffix: truncate from the cutoff, keep
		// everything below it.
		cutoff := uint64(0)
		for height, block := range manager.blocks {
			if block.MomentumAcknowledged.Height > 0 && block.MomentumAcknowledged.Height >= poppedHeight {
				if cutoff == 0 || height < cutoff {
					cutoff = height
				}
			}
		}
		if cutoff == 0 {
			continue
		}
		ap.log.Info("truncating stale momentum-acknowledged suffix",
			"address", address,
			"cutoff-height", cutoff,
			"popped-height", poppedHeight)
		// Pop versions until the frontier is below the cutoff.  Each Pop
		// removes one block (and its descendants) from the manager.
		for {
			frontier := db.GetFrontierIdentifier(manager.db.Frontier())
			if frontier.Height < cutoff {
				break
			}
			if err := manager.Pop(); err != nil {
				ap.log.Error("failed to pop stale block during truncation",
					"address", address,
					"frontier", frontier,
					"reason", err)
				delete(ap.managers, address)
				break
			}
		}
		// If every block was stale the manager is now empty; drop it.
		if len(manager.blocks) == 0 {
			delete(ap.managers, address)
		}
	}
}

func (ap *accountPool) rebuild(detailed *nom.DetailedMomentum) error {
	addresses := make([]types.Address, 0, len(ap.managers))
	for address := range ap.managers {
		addresses = append(addresses, address)
	}

	ap.log.Debug("started rebuilding account-pool", "momentum-identifier", detailed.Momentum.Identifier())
	// Rebuild must not abandon the remaining addresses when one of them fails.
	// Returning mid-loop leaves the addresses already processed bound to the
	// new stable DB and the rest still bound to the previous one, with no
	// indication which is which.  Fail each address independently instead:
	// the address whose rebuild failed keeps no manager, and every other
	// address is rebuilt consistently.
	var firstErr error
	for _, address := range addresses {
		log := ap.log.New("address", address)
		log.Debug("start rebuilding")

		oldManager := ap.managers[address]
		newStable := ap.stable.GetStableAccountDB(address)

		// When the stable store has caught up to (or passed) the manager's
		// frontier there is nothing to rebase: every pending version is already
		// committed.  Drop the manager so the next access creates a fresh one
		// on the new stable, matching dev behaviour.
		if db.GetFrontierIdentifier(newStable).Height >= db.GetFrontierIdentifier(oldManager.db.Frontier()).Height {
			log.Debug("no uncommitted changes", "address", address)
			delete(ap.managers, address)
			continue
		}

		// Rebase moves this manager's versions onto the new stable DB and
		// re-applies every pending patch, so uncommitted state survives the
		// rollback without being replayed block by block here. On error no
		// mutation has occurred: drop this address's manager and carry on with
		// the others, so one failing address cannot abandon the remaining ones.
		if err := oldManager.db.Rebase(newStable); err != nil {
			log.Error("rebuild failed, dropping pending blocks for address",
				"address", address,
				"reason", err)
			if firstErr == nil {
				firstErr = errors.Errorf("account pool rebuild error. Unable to rebase address %v. Reason %v", address, err)
			}
			delete(ap.managers, address)
			continue
		}

		// The blocks map is kept beside the DB and has to be pruned to match
		// it. Blocks at or below the new stable height are committed now. A
		// block whose MomentumAcknowledged is above the frontier is invalid
		// against the new chain state, and so is every block built on it.
		newStableHeight := db.GetFrontierIdentifier(newStable).Height
		invalidFrom := uint64(0)
		for height, block := range oldManager.blocks {
			if height <= newStableHeight {
				delete(oldManager.blocks, height)
				continue
			}
			if block.MomentumAcknowledged.Height > detailed.Momentum.Height {
				log.Info("dropping block with momentum-acknowledged above frontier",
					"block", block.Header(),
					"ma-height", block.MomentumAcknowledged.Height,
					"frontier-height", detailed.Momentum.Height)
				if invalidFrom == 0 || height < invalidFrom {
					invalidFrom = height
				}
			}
		}
		for height := range oldManager.blocks {
			if invalidFrom != 0 && height >= invalidFrom {
				delete(oldManager.blocks, height)
			}
		}

		// Pop the DB manager so its frontier matches the pruned blocks map.
		// Without this the map can end at height 1 while the DB frontier
		// remains at height 3, leaving orphaned versions whose application
		// writes are still visible through Frontier().
		if invalidFrom != 0 {
			for {
				frontier := db.GetFrontierIdentifier(oldManager.db.Frontier())
				if frontier.Height < invalidFrom {
					break
				}
				if err := oldManager.db.Pop(); err != nil {
					log.Error("failed to pop invalid-MA suffix from DB manager",
						"address", address,
						"frontier", frontier,
						"invalid-from", invalidFrom,
						"reason", err)
					if firstErr == nil {
						firstErr = errors.Errorf("account pool rebuild error. Unable to pop invalid-MA suffix for address %v. Reason %v", address, err)
					}
					delete(ap.managers, address)
					break
				}
			}
			if _, ok := ap.managers[address]; !ok {
				continue // Pop failed; manager was dropped above
			}
		}

		// Publish a new wrapper over the rebased DB and pruned blocks map.
		// The new wrapper holds the same db and blocks map as the old one,
		// but the replacement instance is what lets the failure-isolation
		// test tell rebuilt addresses from untouched ones.
		// An empty manager is a map entry with nothing in it until the next
		// rebuild, so only re-register the address when blocks survived.
		if len(oldManager.blocks) == 0 {
			log.Debug("rebuild produced no blocks, dropping manager")
			delete(ap.managers, address)
			continue
		}
		ap.managers[address] = &accountManager{
			db:     oldManager.db,
			blocks: oldManager.blocks,
		}
		log.Debug("successfully rebuild", "num-uncommitted", len(oldManager.blocks))
	}

	ap.log.Debug("finished rebuilding account-pool")
	return firstErr
}

// UncommittedSnapshot captures the pool managers of a set of addresses so they
// can be put back after a failed insertion. Each entry is an independent clone
// of the manager as it was, or nil when the address had no manager. A snapshot
// can be restored once; restoring installs the clones themselves.
type UncommittedSnapshot struct {
	managers map[types.Address]*accountManager
}

// SnapshotUncommitted clones the current pool manager of each given address.
// Restoring the snapshot returns those addresses to exactly this state, byte
// for byte and patch for patch, as long as their stable state has not advanced.
func (ap *accountPool) SnapshotUncommitted(insertLocker sync.Locker, addresses []types.Address) *UncommittedSnapshot {
	if insertLocker == nil {
		panic("insertLocker can't be nil")
	}
	ap.changes.Lock()
	defer ap.changes.Unlock()

	snapshot := &UncommittedSnapshot{managers: make(map[types.Address]*accountManager, len(addresses))}
	for _, address := range addresses {
		if _, seen := snapshot.managers[address]; seen {
			continue
		}
		manager := ap.managers[address]
		if manager == nil {
			snapshot.managers[address] = nil
			continue
		}
		cloned := manager.clone()
		if cloned == nil {
			// The pool only ever creates in-memory managers, which clone.
			panic(fmt.Sprintf("account pool manager for %v can't be cloned", address))
		}
		snapshot.managers[address] = cloned
	}
	return snapshot
}

// RestoreUncommitted puts the snapshotted managers back, discarding whatever
// the pool holds for those addresses now. The snapshot is consumed.
func (ap *accountPool) RestoreUncommitted(insertLocker sync.Locker, snapshot *UncommittedSnapshot) error {
	if insertLocker == nil {
		return errors.Errorf("insertLocker can't be nil")
	}
	if snapshot == nil {
		return nil
	}
	ap.changes.Lock()
	defer ap.changes.Unlock()

	for address, manager := range snapshot.managers {
		if manager == nil {
			delete(ap.managers, address)
		} else {
			ap.managers[address] = manager
		}
	}
	snapshot.managers = nil
	return nil
}

func (ap *accountPool) GetNewMomentumContent() []*nom.AccountBlock {
	return ap.filterBlocksToCommit(ap.GetAllUncommittedAccountBlocks())
}

// filterBlocksToCommit is the pre-spork content selection: it walks the
// offered blocks in order (each account's chain contiguous and by
// height, contract sends grouped with the receive that produced them)
// and keeps whole batches until the block-count cap or the wire-size
// budget of the detailed momentum is reached.
//
// A batch that does not fit the remaining budget is skipped, together
// with every later block of the same accounts, since momentum content
// may not leave a gap in an account chain; the walk then continues with
// the other accounts. Up to the first batch that does not fit, the
// selection is what the walk produced before this rule: the same blocks
// in the same order. After it, smaller batches of later accounts may
// still be packed into a momentum that is nearly full, as the post-spork
// selector does. The rule exists because a single pending block can be
// larger than the whole budget (before the dynamic-plasma spork a
// receive block's data is not bounded), and such a block stays pending
// until it is replaced, rolled back or confirmed by another producer.
// Ending the walk there, as this used to, let one such block keep every
// account the walk had not reached yet out of the momentum, at a point
// that may change from one momentum to the next since the walk order is
// the pool's map order. The block-count cap keeps its original
// behaviour and ends the walk.
func (ap *accountPool) filterBlocksToCommit(blocks []*nom.AccountBlock) []*nom.AccountBlock {
	toCommit := make([]*nom.AccountBlock, 0, len(blocks))
	batch := make([]*nom.AccountBlock, 0, MaxAccountBlocksInMomentum)
	headerSize := worstCaseMomentumHeaderSize()
	sizeEstimate := headerSize
	var skippedAddresses map[types.Address]bool
	skipBatch := func() {
		if skippedAddresses == nil {
			skippedAddresses = make(map[types.Address]bool)
		}
		for _, skipped := range batch {
			skippedAddresses[skipped.Address] = true
		}
		batch = batch[:0]
	}
	for _, block := range blocks {
		if skippedAddresses[block.Address] {
			continue
		}
		batch = append(batch, block)
		if block.BlockType == nom.BlockTypeContractSend {
			continue
		}
		if len(toCommit)+len(batch) > MaxAccountBlocksInMomentum {
			break
		}
		// Byte-budget check: the batch must fit the remaining room of
		// the RLP-encoded DetailedMomentum, each block counted with the
		// content entry (AccountHeader) it adds to the momentum. The raw
		// field lengths are a lower bound on the encoded size, so a batch
		// that could not fit even an empty momentum by that measure alone
		// is skipped without encoding it; a pending set full of oversized
		// blocks then costs a length sum, not an encoding of each, at
		// every momentum. Anything smaller is measured exactly, so that a
		// batch is only reported when it can never fit by that measure.
		if rawSize := batchRawSize(batch); headerSize+rawSize > maxDetailedMomentumWireSize {
			ap.reportOversizedBatch(batch, rawSize, headerSize, false, false)
			skipBatch()
			continue
		}
		batchSize := 0
		unencodable := false
		for _, b := range batch {
			sz := blockWireSize(b)
			if sz < 0 {
				unencodable = true
				break
			}
			batchSize += sz + contentEntrySize(b)
		}
		if unencodable || sizeEstimate+batchSize > maxDetailedMomentumWireSize {
			ap.reportOversizedBatch(batch, batchSize, headerSize, true, unencodable)
			skipBatch()
			continue
		}
		sizeEstimate += batchSize
		toCommit = append(toCommit, batch...)
		batch = batch[:0]
	}
	return toCommit
}

// reportOversizedBatch warns about a batch that could never fit a
// momentum on its own, once per block: a batch that merely did not fit
// the room left in this momentum is normal and not reported. "Fit" is
// by the selector's estimate (worst-case header plus the encoded blocks
// and their content entries), which is conservative: a batch a few bytes
// over it may still encode under the wire limit in a real momentum, and
// is skipped and reported all the same. Selection runs at every
// momentum and the block stays pending, so the warning is keyed on the
// hash of the block that closes the batch and remembered in a bounded
// cache. size is the batch's encoded size (content entries included)
// when exact is set, otherwise the raw-length lower bound the batch was
// rejected on.
func (ap *accountPool) reportOversizedBatch(batch []*nom.AccountBlock, size, headerSize int, exact, unencodable bool) {
	room := maxDetailedMomentumWireSize - headerSize
	if !unencodable && size <= room {
		return
	}
	ap.oversizedReportedOnce.Do(func() {
		cache, err := lru.New(oversizedReportMemory)
		common.DealWithErr(err)
		ap.oversizedReported = cache
	})
	block := batch[len(batch)-1]
	if seen, _ := ap.oversizedReported.ContainsOrAdd(block.Hash, struct{}{}); seen {
		return
	}
	log := ap.log.New("header", block.Header())
	switch {
	case unencodable:
		log.Warn("skipping pending account-block that can't be encoded")
	case exact:
		log.Warn("skipping pending account-block over the momentum wire budget", "batch-size", size, "budget", room)
	default:
		log.Warn("skipping pending account-block over the momentum wire budget", "batch-size-at-least", size, "budget", room)
	}
}

// batchRawSize is a cheap lower bound on the RLP-encoded size of the
// batch: the lengths of every block's variable-length fields, descendant
// blocks included, before any encoding overhead.
func batchRawSize(batch []*nom.AccountBlock) int {
	total := 0
	for _, block := range batch {
		total += blockRawSize(block)
	}
	return total
}

func blockRawSize(block *nom.AccountBlock) int {
	if block == nil {
		return 0
	}
	size := len(block.Data) + len(block.PublicKey) + len(block.Signature)
	for _, d := range block.DescendantBlocks {
		size += blockRawSize(d)
	}
	return size
}

// worstCaseMomentumHeaderSize returns the RLP-encoded size of a fully
// populated momentum header with every field at its maximum width.
func worstCaseMomentumHeaderSize() int {
	m := &nom.Momentum{
		Version:         2,
		ChainIdentifier: ^uint64(0),
		Hash:            types.Hash{0xff},
		PreviousHash:    types.Hash{0xff},
		Height:          ^uint64(0),
		TimestampUnix:   ^uint64(0),
		Data:            []byte{},
		Content:         nom.MomentumContent{},
		ChangesHash:     types.Hash{0xff},
		PublicKey:       make([]byte, 32),
		Signature:       make([]byte, 64),
		NextFusionPrice: ^uint64(0),
		NextWorkPrice:   ^uint64(0),
	}
	encoded, err := rlp.EncodeToBytes(m)
	if err != nil {
		return -1
	}
	return len(encoded)
}

// blockWireSize returns the exact RLP-encoded size of block, including its
// descendant blocks.  Returns -1 on encoding failure.
func blockWireSize(block *nom.AccountBlock) int {
	encoded, err := rlp.EncodeToBytes(block)
	if err != nil {
		return -1
	}
	return len(encoded)
}

// contentEntrySize returns the RLP-encoded size of the AccountHeader entry
// that a block contributes to the Momentum.Content list.
func contentEntrySize(block *nom.AccountBlock) int {
	h := block.Header()
	encoded, err := rlp.EncodeToBytes(&h)
	if err != nil {
		return -1
	}
	return len(encoded)
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

// NewAccountPool returns a standalone AccountPool over the given
// stable state. The chain builds its own pool internally; this
// constructor exists for chain/genesis, which uses a pool with an
// empty stable base to assemble the genesis account blocks.
func NewAccountPool(stable Stable) AccountPool {
	return newAccountPool(stable)
}
