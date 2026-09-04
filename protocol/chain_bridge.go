package protocol

import (
	"fmt"
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

func (c chainBridge) InsertChain(momentums []*nom.DetailedMomentum) (int, error) {
	a := momentums[0]
	b := momentums[len(momentums)-1]
	log.Info("start inserting chain", "num-momentums", len(momentums), "start-identifier", a.Momentum.Identifier(), "end-identifier", b.Momentum.Identifier())
	insert := c.chain.AcquireInsert(fmt.Sprintf("Insert momentums in chain-bridge. Start-identifier:%v End-identifier:%v", a.Momentum.Identifier(), b.Momentum.Identifier()))
	defer insert.Unlock()

	store := c.chain.GetFrontierMomentumStore()

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

	// Cheap structural check on peer-supplied data before anything is
	// sized from it, before any rollback and before any pool state is
	// touched. The verifier repeats the semantic checks later; this only
	// bounds the input and puts the blocks into content order. The caller
	// keeps using its own objects concurrently (the fetcher broadcasts the
	// same DetailedMomentum while importing it), and the VM writes computed
	// plasma fields onto whatever block it applies, so the loop below works
	// on private copies of the blocks rather than on the caller's.
	candidates := make([]*nom.DetailedMomentum, len(momentums))
	for index, detailed := range momentums {
		ordered, err := validatePrefetchedBlocks(detailed)
		if err != nil {
			log.Error("malformed prefetched account-blocks", "reason", err, "momentum-identifier", detailed.Momentum.Identifier())
			return index + start, err
		}
		candidates[index] = &nom.DetailedMomentum{Momentum: detailed.Momentum, AccountBlocks: copyAccountBlocks(ordered)}
	}
	momentums = candidates

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

		err = c.chain.RollbackTo(insert, target.Identifier())
		if err != nil {
			return 0, errors.Errorf("unable to rollback to %v. Reason:%v", target.Identifier(), err)
		}
	}

	// Insert momentum now
	for index, detailed := range momentums {
		// Blocks are force-inserted into the pool before the momentum itself
		// is validated, so keep a snapshot of every account the momentum
		// touches and put it back if anything fails.
		snapshot := c.chain.SnapshotUncommitted(insert, momentumAddresses(detailed))
		frontierBefore := c.chain.GetFrontierMomentumStore().Identifier()
		if err := c.insertMomentum(insert, detailed); err != nil {
			if c.chain.GetFrontierMomentumStore().Identifier() != frontierBefore {
				// The momentum was committed before the error surfaced. The
				// snapshot predates that commit, so putting it back would
				// resurrect pool state that no longer links to the chain.
				log.Error("momentum committed despite insert error; not restoring account-pool", "reason", err, "momentum-identifier", detailed.Momentum.Identifier())
			} else if restoreErr := c.chain.RestoreUncommitted(insert, snapshot); restoreErr != nil {
				log.Error("error while restoring account-pool after failed momentum", "reason", restoreErr, "momentum-identifier", detailed.Momentum.Identifier())
			}
			return index + start, err
		}
	}

	return 0, nil
}

// validatePrefetchedBlocks rejects a detailed momentum whose block list is not
// exactly the set of blocks named by the momentum's content: no nil entries,
// no duplicates, no more entries than a momentum may hold, and one block per
// content header. Peers are not required to send the blocks in content order,
// so on success it returns the blocks in content order, which is also the
// order they must be applied in. The supplied momentum is not modified.
func validatePrefetchedBlocks(detailed *nom.DetailedMomentum) ([]*nom.AccountBlock, error) {
	if detailed == nil || detailed.Momentum == nil {
		return nil, errors.Errorf("missing momentum")
	}
	blocks := detailed.AccountBlocks
	content := detailed.Momentum.Content
	if len(blocks) > chain.MaxAccountBlocksInMomentum {
		return nil, errors.Errorf("too many prefetched account-blocks: %v > %v", len(blocks), chain.MaxAccountBlocksInMomentum)
	}
	if len(blocks) != len(content) {
		return nil, errors.Errorf("prefetched account-blocks (%v) do not match momentum content (%v)", len(blocks), len(content))
	}

	byIdentifier := make(map[types.HashHeight]*nom.AccountBlock, len(blocks))
	for index, block := range blocks {
		if block == nil {
			return nil, errors.Errorf("prefetched account-block at index %v is nil", index)
		}
		identifier := block.Identifier()
		if _, seen := byIdentifier[identifier]; seen {
			return nil, errors.Errorf("duplicate prefetched account-block %v", identifier)
		}
		byIdentifier[identifier] = block
	}

	ordered := make([]*nom.AccountBlock, len(content))
	for index, header := range content {
		if header == nil {
			return nil, errors.Errorf("momentum content header at index %v is nil", index)
		}
		block, ok := byIdentifier[header.Identifier()]
		if !ok || block.Address != header.Address {
			return nil, errors.Errorf("momentum content header %v has no matching prefetched account-block", header)
		}
		ordered[index] = block
	}
	return ordered, nil
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
// commits the momentum. Under the caller's insert lock.
func (c chainBridge) insertMomentum(insert sync.Locker, detailed *nom.DetailedMomentum) error {
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
	if err := c.chain.AddMomentumTransaction(insert, transaction); err != nil {
		log.Error("error while inserting momentum", "reason", err, "momentum-identifier", detailed.Momentum.Identifier())
		return err
	}
	return nil
}
