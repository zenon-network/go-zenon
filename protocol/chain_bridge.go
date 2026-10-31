package protocol

import (
	"fmt"
	"os"
	"sync"

	"github.com/pkg/errors"

	"github.com/zenon-network/go-zenon/chain"
	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/consensus"
	"github.com/zenon-network/go-zenon/verifier"
	"github.com/zenon-network/go-zenon/vm"
)

type chainBridge struct {
	chain      chain.Chain
	consensus  consensus.Consensus
	verifier   verifier.Verifier
	supervisor *vm.Supervisor
}

func (c chainBridge) rollbackSideChain(insert sync.Locker, identifier types.HashHeight) error {
	if err := c.chain.RollbackTo(insert, identifier); err != nil {
		return err
	}
	if err := c.chain.RollbackCacheTo(insert, identifier); err != nil {
		return &chain.ErrCanonicalStateUncertain{
			Cause:       errors.Errorf("canonical chain rolled back to %v but cache rollback failed", identifier),
			RollbackErr: err,
		}
	}
	return nil
}

// NewChainBridge returns the ChainBridge used by the protocol layer.
// Reads are answered from the frontier momentum store; insertions
// (AddAccountBlocks, InsertChain) apply blocks through the supervisor
// and commit them while holding the chain insert lock. InsertChain
// also rolls the chain back by up to 30 momentums when a longer
// side-chain arrives.
func NewChainBridge(chain chain.Chain, consensus consensus.Consensus, verifier verifier.Verifier, supervisor *vm.Supervisor) ChainBridge {
	return chainBridge{
		chain:      chain,
		consensus:  consensus,
		verifier:   verifier,
		supervisor: supervisor,
	}
}

func (c chainBridge) AddAccountBlocks(blocks []*nom.AccountBlock) error {
	insert := c.chain.AcquireInsert(fmt.Sprintf("Insert blocks in chain-bridge. Len:%v", len(blocks)))
	defer insert.Unlock()
	for _, block := range blocks {
		if patch := c.chain.GetPatch(block.Address, block.Identifier()); patch != nil {
			continue
		}
		if block.BlockType == nom.BlockTypeContractSend {
			continue
		}
		transaction, err := c.supervisor.ApplyBlock(block)
		if err != nil {
			log.Error("error while applying account-block", "reason", err, "account-block-header", block.Header())
			return err
		}

		if err := c.chain.AddAccountBlockTransaction(insert, transaction); err != nil {
			log.Error("error while inserting account-block in pool", "reason", err, "account-block-header", block.Header())
			return err
		}
	}
	return nil
}

// poolHoldsSameBytes reports whether the account pool's frontier chain already
// contains this exact block. A pool copy that matches by identifier but not by
// bytes must not be reused, because the stored bytes feed into the momentum
// changes-hash and the momentum was built from the copy passed in here.
func (c chainBridge) poolHoldsSameBytes(block *nom.AccountBlock) bool {
	stored, err := c.chain.GetFrontierAccountStore(block.Address).ByHeight(block.Height)
	if err != nil || stored == nil {
		return false
	}
	return stored.EqualBytes(block)
}

func (c chainBridge) GetTransactions() []*nom.AccountBlock {
	blocks := c.chain.GetAllUncommittedAccountBlocks()
	return blocks
}

func (c chainBridge) HasBlock(hash types.Hash) bool {
	m, _ := c.chain.GetFrontierMomentumStore().GetMomentumByHash(hash)
	return m != nil
}
func (c chainBridge) GetBlockHashesFromHash(hash types.Hash, amount uint64) ([]types.Hash, error) {
	momentums, err := c.chain.GetFrontierMomentumStore().GetMomentumsByHash(hash, false, amount)
	if err != nil {
		return nil, err
	}
	hashes := make([]types.Hash, len(momentums))
	for i := range momentums {
		hashes[i] = momentums[i].Hash
	}
	return hashes, nil
}
func (c chainBridge) GetBlock(hash types.Hash) *nom.DetailedMomentum {
	store := c.chain.GetFrontierMomentumStore()
	momentum, _ := store.GetMomentumByHash(hash)
	if momentum == nil {
		return nil
	}
	prefetched := make([]*nom.AccountBlock, len(momentum.Content))

	for i := range prefetched {
		block, _ := store.GetAccountBlock(*momentum.Content[i])
		prefetched[i] = block
	}

	return &nom.DetailedMomentum{
		Momentum:      momentum,
		AccountBlocks: prefetched,
	}
}
func (c chainBridge) CurrentBlock() *nom.Momentum {
	store := c.chain.GetFrontierMomentumStore()
	momentum, err := store.GetFrontierMomentum()
	common.DealWithErr(err)

	return momentum
}
func (c chainBridge) GetBlockByNumber(num uint64) (*nom.Momentum, error) {
	store := c.chain.GetFrontierMomentumStore()
	return store.GetMomentumByHeight(num)
}
func (c chainBridge) Status() (td uint64, currentBlock types.Hash, genesisBlock types.Hash) {
	store := c.chain.GetFrontierMomentumStore()
	frontier, err := store.GetFrontierMomentum()
	common.DealWithErr(err)

	return frontier.Height, frontier.Hash, c.chain.GetGenesisMomentum().Hash
}

func (c chainBridge) VerifyMomentum(detailed *nom.DetailedMomentum) error {
	return c.verifier.Momentum(detailed)
}

func (c chainBridge) InsertChain(momentums []*nom.DetailedMomentum) (int, error) {
	return c.insertChain(momentums, nil)
}

// InsertChainMoved inserts like InsertChain and also reports whether the
// canonical frontier changed during the call. The comparison is made under
// the insert lock the call holds, so it is attributable to this call alone:
// a batch the node already had, or a replacement that failed and was
// restored, reports false; new momentums that stayed on the chain report
// true, also when a later candidate in the batch failed.
func (c chainBridge) InsertChainMoved(momentums []*nom.DetailedMomentum) (index int, moved bool, err error) {
	index, err = c.insertChain(momentums, &moved)
	return index, moved, err
}

func (c chainBridge) insertChain(momentums []*nom.DetailedMomentum, moved *bool) (int, error) {
	// Cheap structural check on peer-supplied data before anything below
	// indexes or dereferences it, before anything is sized from it, before
	// any rollback and before any pool state is touched. The verifier
	// repeats the semantic checks later; this only bounds the input and
	// checks that the blocks are the momentum's content, in content order.
	// The wire decoder never yields an empty batch or a nil entry; those
	// cases guard callers.
	if len(momentums) == 0 {
		return 0, errors.Errorf("no momentums to insert")
	}
	for index, detailed := range momentums {
		if err := validatePrefetchedBlocks(detailed); err != nil {
			log.Error("malformed prefetched account-blocks", "reason", err, "index", index)
			return index, err
		}
	}
	a := momentums[0]
	b := momentums[len(momentums)-1]
	log.Info("start inserting chain", "num-momentums", len(momentums), "start-identifier", a.Momentum.Identifier(), "end-identifier", b.Momentum.Identifier())
	insert := c.chain.AcquireInsert(fmt.Sprintf("Insert momentums in chain-bridge. Start-identifier:%v End-identifier:%v", a.Momentum.Identifier(), b.Momentum.Identifier()))
	defer insert.Unlock()

	store := c.chain.GetFrontierMomentumStore()
	if moved != nil {
		before := store.Identifier()
		// Runs before the unlock above, so the frontier read is this
		// call's outcome and not a later insert's.
		defer func() { *moved = c.chain.GetFrontierMomentumStore().Identifier() != before }()
	}

	// remove momentums which we already have
	start := 0
	for ; start < len(momentums); start += 1 {
		our, err := store.GetMomentumByHeight(momentums[start].Momentum.Height)
		if err != nil {
			log.Info("failed to get momentum by height", "reason", err)
			return start, err
		}
		if our == nil {
			break
		}

		if our.Hash != momentums[start].Momentum.Hash {
			break
		}
	}

	// nothing to add, all momentums are already inserted
	if start == len(momentums) {
		log.Info("nothing to insert. All momentums already inserted")
		return 0, nil
	}
	momentums = momentums[start:]

	head := momentums[0].Momentum
	tail := momentums[len(momentums)-1].Momentum
	ourFrontier, err := store.GetFrontierMomentum()
	if err != nil {
		return 0, err
	}

	// if we are dealing with a side-chain, check if it should replace our chain and rollback for insertion
	if head.Previous() != ourFrontier.Identifier() {
		// check if we can roll back for insertion
		target, err := store.GetMomentumByHeight(head.Height - 1)
		if err != nil {
			return 0, err
		}
		// GetMomentumByHeight returns (nil, nil) when that height is not in the
		// store (e.g. a side-chain whose head is more than one above our frontier).
		// Guard the nil before dereferencing it, matching the check in the loop
		// above; the downloader turns this error into a peer drop + retry.
		if target == nil {
			return 0, errors.Errorf("can't link momentums to insert. No momentum at height %d to link head %v (frontier %v)", head.Height-1, head.Identifier(), ourFrontier.Identifier())
		}
		if target.Identifier() != head.Previous() {
			log.Error("can't link momentums to insert", "first")
			return 0, errors.Errorf("can't link momentums to insert. First momentum Prev is %v but he have %v", head.Previous(), target.Identifier())
		}

		// check that the distance allows rollback
		if ourFrontier.Height-target.Height > 30 {
			return 0, errors.Errorf("can't rollback to %v. Too far. Frontier is %v. Wanted to be able to insert %v", target.Identifier(), ourFrontier.Identifier(), head.Identifier())
		}

		// check that current tail is longer than frontier
		if tail.Height <= ourFrontier.Height {
			return 0, errors.Errorf("won't insert side-chain which is not longer")
		}

		// Everything that can be checked before any canonical momentum is
		// removed is checked here, so that a rollback costs the sender at
		// least a momentum the elected producer signed.
		//
		// Every candidate has to be internally consistent.
		for index, detailed := range momentums {
			if err := verifier.MomentumStatic(detailed.Momentum); err != nil {
				log.Info("side-chain momentum failed static verification", "reason", err, "momentum-identifier", detailed.Momentum.Identifier())
				return index + start, err
			}
		}
		// The head extends target, which the node has, so its state-dependent
		// checks run against the store at target. Later candidates extend
		// momentums the node does not have yet.
		if err := c.verifier.Momentum(momentums[0]); err != nil {
			log.Info("side-chain head failed verification", "reason", err, "momentum-identifier", head.Identifier())
			return start, err
		}
		// The head's producer is judged by the election of the chain the head
		// belongs to, which agrees with ours up to target. Our own frontier
		// may imply a different election for the same slot when our branch
		// continued past the proof time the head's tick uses, so it is not
		// the reference here.
		isProducer, err := c.consensus.VerifyMomentumProducerAt(target.Identifier(), head)
		if err != nil {
			log.Info("side-chain head producer check failed", "reason", err, "momentum-identifier", head.Identifier())
			return start, verifier.InternalError(err)
		}
		if !isProducer {
			log.Info("side-chain head not signed by the elected producer", "momentum-identifier", head.Identifier(), "producer", head.Producer())
			return start, verifier.ErrMProducerInvalid
		}

		// The remaining checks need the replacement state, so keep the branch
		// being removed and put it back if the replacement fails.
		removed, err := c.chain.CaptureBranchAbove(insert, target.Identifier())
		if err != nil {
			return 0, errors.Errorf("unable to capture branch above %v for rollback. Reason:%v", target.Identifier(), err)
		}
		// Rollback can discard pending state even for accounts outside the
		// replacement's content (for example, a recent acknowledged momentum).
		// Keep every pending account, not only the side-chain's addresses.
		pending := c.chain.GetAllUncommittedAccountBlocks()
		pendingAddresses := make([]types.Address, 0, len(pending))
		for _, block := range pending {
			pendingAddresses = append(pendingAddresses, block.Address)
		}
		pendingSnapshot := c.chain.SnapshotUncommitted(insert, pendingAddresses)

		if err := c.rollbackSideChain(insert, target.Identifier()); err != nil {
			var uncertain *chain.ErrCanonicalStateUncertain
			if errors.As(err, &uncertain) {
				log.Crit("chain state uncertain after failed side-chain rollback, can't continue", "reason", err, "target", target.Identifier())
				os.Exit(2)
			}
			return 0, errors.Errorf("unable to rollback to %v. Reason:%v", target.Identifier(), err)
		}

		failed, committed, err := c.insertMomentums(insert, momentums)
		if err != nil {
			// committed is how many candidates are on the chain. Once that
			// prefix is longer than the branch it replaced, the node sits on a
			// valid chain longer than the one it had, so the prefix stays.
			// A restore only happens while the prefix is at most as long as
			// the removed branch, which the depth check above bounds to 30;
			// rolling that far back always fits in the cache's rollback
			// window, whereas the committed prefix has no such bound.
			if committed > len(removed) {
				log.Info("side-chain replacement failed after exceeding the removed branch, keeping the applied prefix", "reason", err, "target", target.Identifier(), "num-applied", committed, "num-removed", len(removed))
				return failed + start, err
			}
			log.Info("side-chain replacement failed, restoring original branch", "reason", err, "target", target.Identifier(), "num-momentums", len(removed))
			if restoreErr := c.restoreBranch(insert, target.Identifier(), removed); restoreErr != nil {
				log.Crit("chain state uncertain after failed side-chain restore, can't continue", "reason", restoreErr, "cause", err, "target", target.Identifier())
				os.Exit(2)
			}
			// The snapshot is valid only on the original canonical/cache branch.
			// Never reinstate it when a longer replacement prefix is retained.
			if restoreErr := c.chain.RestoreUncommitted(insert, pendingSnapshot); restoreErr != nil {
				log.Error("error while restoring account-pool after rejected side-chain", "reason", restoreErr, "cause", err, "target", target.Identifier())
			}
			return failed + start, err
		}
		return 0, nil
	}

	failed, _, err := c.insertMomentums(insert, momentums)
	if err != nil {
		return failed + start, err
	}
	return 0, nil
}

// restoreBranch drops whatever replaced the branch above target and puts the
// captured momentums back, in order, with their cache state.
//
// What it restores is the canonical chain and the cache. It does not undo the
// delete events listeners already saw (they see insert events for the same
// momentums instead). The caller restores the pre-rollback account pool only
// after this succeeds. The captured branch lives only in memory: if the process
// dies part-way through, the node comes back at the fork point plus whatever was committed, which is
// a valid prefix it resynchronises from, not the restored branch.
func (c chainBridge) restoreBranch(insert sync.Locker, target types.HashHeight, removed []*chain.RemovedMomentum) error {
	if err := c.rollbackSideChain(insert, target); err != nil {
		return err
	}
	for _, momentum := range removed {
		if err := c.chain.UpdateCache(insert, momentum.Detailed, momentum.Changes); err != nil {
			return errors.Errorf("unable to restore cache for %v. Reason:%v", momentum.Detailed.Momentum.Identifier(), err)
		}
		transaction := &nom.MomentumTransaction{Momentum: momentum.Detailed.Momentum, Changes: momentum.Changes}
		if err := c.chain.AddMomentumTransaction(insert, transaction); err != nil {
			return errors.Errorf("unable to restore momentum %v. Reason:%v", momentum.Detailed.Momentum.Identifier(), err)
		}
	}
	return nil
}

// insertMomentums applies and commits the momentums one by one. On error,
// failed is the index of the momentum that failed and committed is how many
// of the momentums are on the canonical chain. The two differ by one when
// the failing momentum was committed before its error surfaced, so callers
// that reason about the committed prefix must use committed, not failed.
func (c chainBridge) insertMomentums(insert sync.Locker, momentums []*nom.DetailedMomentum) (failed, committed int, err error) {
	for index, supplied := range momentums {
		// Reject an oversized momentum before the copy and snapshot
		// below. The verifier applies the same rule inside insertMomentum;
		// checking here first keeps the momentum's blocks out of the pool
		// and the VM, and avoids the copy and snapshot for a momentum that
		// would be rejected anyway.
		maxContent, err := verifier.MaxMomentumContent(c.chain.GetFrontierMomentumStore())
		if err != nil {
			return index, index, err
		}
		if uint64(len(supplied.AccountBlocks)) > maxContent {
			log.Info("refusing momentum with too many account-blocks", "num-blocks", len(supplied.AccountBlocks), "max", maxContent, "momentum-identifier", supplied.Momentum.Identifier())
			return index, index, verifier.ErrMContentTooBig
		}

		// The caller keeps using its own objects concurrently (the fetcher
		// broadcasts the same DetailedMomentum while importing it), and the
		// VM writes computed plasma fields onto whatever block it applies,
		// so insert works on private copies of the blocks. Copies are made
		// one momentum at a time so a batch that fails early does not pay
		// for the momentums it never reaches.
		detailed := &nom.DetailedMomentum{Momentum: supplied.Momentum, AccountBlocks: copyAccountBlocks(supplied.AccountBlocks)}

		// Blocks are force-inserted into the pool before the momentum itself
		// is validated, so keep a snapshot of every account the momentum
		// touches and put it back if anything fails.
		snapshot := c.chain.SnapshotUncommitted(insert, momentumAddresses(detailed))
		frontierBefore := c.chain.GetFrontierMomentumStore().Identifier()
		if err := c.insertMomentum(insert, detailed, frontierBefore); err != nil {
			if c.chain.GetFrontierMomentumStore().Identifier() != frontierBefore {
				// The momentum was committed before the error surfaced. The
				// snapshot predates that commit, so putting it back would
				// resurrect pool state that no longer links to the chain.
				log.Error("momentum committed despite insert error; not restoring account-pool", "reason", err, "momentum-identifier", detailed.Momentum.Identifier())
				return index, index + 1, err
			}
			if restoreErr := c.chain.RestoreUncommitted(insert, snapshot); restoreErr != nil {
				log.Error("error while restoring account-pool after failed momentum", "reason", restoreErr, "momentum-identifier", detailed.Momentum.Identifier())
			}
			return index, index, err
		}
	}

	return 0, len(momentums), nil
}

// validatePrefetchedBlocks rejects a detailed momentum whose block list is not
// exactly the momentum's content, in content order: no nil entries and the
// block at each index matching the content header at the same index by
// address, hash and height. Every producer of a DetailedMomentum emits the
// blocks in content order, which is also the order they must be applied in,
// so a mismatch is malformed input rather than a legitimate alternative
// layout. A content header listed twice is rejected here as well, so that no
// duplicate reaches the pool before the verifier sees it.
//
// How many blocks a momentum may carry is not checked here: before dynamic
// plasma it is a fixed count, after it a plasma budget, and which rule
// applies is known only against the chain the momentum extends. That check
// (verifier.MaxMomentumContent) runs in insertMomentums, when the frontier
// is the momentum's previous, before the copy and snapshot and before any
// block reaches the pool, and again in the momentum verifier.
// The work here is linear in the list, which the protocol message size
// bounds. The supplied momentum is not modified.
func validatePrefetchedBlocks(detailed *nom.DetailedMomentum) error {
	if detailed == nil || detailed.Momentum == nil {
		return errors.Errorf("missing momentum")
	}
	blocks := detailed.AccountBlocks
	content := detailed.Momentum.Content
	if len(blocks) != len(content) {
		return errors.Errorf("prefetched account-blocks (%v) do not match momentum content (%v)", len(blocks), len(content))
	}

	seen := make(map[types.HashHeight]struct{}, len(content))
	for index, header := range content {
		if header == nil {
			return errors.Errorf("momentum content header at index %v is nil", index)
		}
		block := blocks[index]
		if block == nil {
			return errors.Errorf("prefetched account-block at index %v is nil", index)
		}
		if block.Address != header.Address || block.Identifier() != header.Identifier() {
			return errors.Errorf("prefetched account-block at index %v (%v) does not match momentum content header %v", index, block.Header(), header)
		}
		if _, duplicate := seen[header.Identifier()]; duplicate {
			return errors.Errorf("duplicate momentum content header %v", header)
		}
		seen[header.Identifier()] = struct{}{}
	}
	return nil
}

// copyAccountBlocks returns deep copies of the blocks, descendants included,
// so nothing downstream writes to objects the caller still shares.
func copyAccountBlocks(blocks []*nom.AccountBlock) []*nom.AccountBlock {
	copied := make([]*nom.AccountBlock, len(blocks))
	for index, block := range blocks {
		copied[index] = block.Copy()
	}
	return copied
}

// momentumAddresses lists each account whose pool state the momentum's blocks
// can modify.
func momentumAddresses(detailed *nom.DetailedMomentum) []types.Address {
	addresses := make([]types.Address, 0, len(detailed.AccountBlocks))
	for _, block := range detailed.AccountBlocks {
		addresses = append(addresses, block.Address)
	}
	return addresses
}

// insertMomentum applies the momentum's blocks to the pool, then validates and
// commits the momentum with its cache state. Under the caller's insert lock.
// frontierBefore is the canonical frontier the caller observed before the
// call; it tells a commit failure apart from a failure after the commit.
func (c chainBridge) insertMomentum(insert sync.Locker, detailed *nom.DetailedMomentum, frontierBefore types.HashHeight) error {
	// The content limit was already checked by insertMomentums before
	// the copy and snapshot; the verifier applies the same rule to the
	// momentum later.
	for _, block := range detailed.AccountBlocks {
		if block.BlockType == nom.BlockTypeContractSend {
			continue
		}
		if c.poolHoldsSameBytes(block) {
			// already applied
			continue
		}
		transaction, err := c.supervisor.ApplyBlock(block)
		if err != nil {
			log.Error("error while applying account-block", "reason", err, "account-block-header", block.Header())
			return err
		}
		if err := c.chain.ForceAddAccountBlockTransaction(insert, transaction); err != nil {
			log.Error("error while inserting account-block in pool", "reason", err, "account-block-header", block.Header())
			return err
		}
	}

	transaction, err := c.supervisor.ApplyMomentum(detailed)
	if err != nil {
		return err
	}
	if err := c.chain.UpdateCache(insert, detailed, transaction.Changes); err != nil {
		log.Error("error while inserting cache", "reason", err, "momentum-identifier", detailed.Momentum.Identifier())
		return err
	}
	if err := c.chain.AddMomentumTransaction(insert, transaction); err != nil {
		log.Error("error while inserting momentum", "reason", err, "momentum-identifier", detailed.Momentum.Identifier())
		var uncertain *chain.ErrCanonicalStateUncertain
		if errors.As(err, &uncertain) {
			log.Crit("canonical chain state uncertain after failed momentum insertion, can't continue", "reason", err, "momentum-identifier", detailed.Momentum.Identifier())
			os.Exit(2)
		}
		if c.chain.GetFrontierMomentumStore().Identifier() != frontierBefore {
			// The momentum is on the canonical chain although the call
			// reported an error, so the cache state applied above belongs
			// to the chain as it now is and stays; the caller skips the
			// pool restore for the same reason.
			log.Error("momentum committed despite insert error; keeping its cache state", "reason", err, "momentum-identifier", detailed.Momentum.Identifier())
			return err
		}
		if rollbackErr := c.chain.RollbackCacheTo(insert, detailed.Momentum.Previous()); rollbackErr != nil {
			log.Crit("cache rollback failed after failed momentum insertion, can't continue", "reason", rollbackErr, "cause", err, "momentum-identifier", detailed.Momentum.Identifier())
			os.Exit(2)
		}
		return err
	}
	return nil
}
