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
	"github.com/zenon-network/go-zenon/wallet"
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

		// Everything that can be checked without chain state is checked
		// before any canonical momentum is removed, so a candidate that is
		// not even internally consistent never triggers a rollback.
		for index, detailed := range momentums {
			if err := verifyMomentumStatically(detailed.Momentum); err != nil {
				log.Info("side-chain momentum failed static verification", "reason", err, "momentum-identifier", detailed.Momentum.Identifier())
				return index + start, err
			}
		}
		if c.verifier != nil {
			if err := c.verifier.Momentum(momentums[0]); err != nil {
				log.Info("side-chain head failed verification", "reason", err, "momentum-identifier", head.Identifier())
				return start, err
			}
		}

		// The state-dependent checks can only run after the rollback, so keep
		// the branch being removed and put it back if the replacement fails.
		removed, err := c.chain.CaptureBranchAbove(insert, target.Identifier())
		if err != nil {
			return 0, errors.Errorf("unable to capture branch above %v for rollback. Reason:%v", target.Identifier(), err)
		}

		if err := c.rollbackSideChain(insert, target.Identifier()); err != nil {
			var uncertain *chain.ErrCanonicalStateUncertain
			if errors.As(err, &uncertain) {
				log.Crit("chain state uncertain after failed side-chain rollback, can't continue", "reason", err, "target", target.Identifier())
				os.Exit(2)
			}
			return 0, errors.Errorf("unable to rollback to %v. Reason:%v", target.Identifier(), err)
		}

		index, err := c.insertMomentums(insert, momentums)
		if err != nil {
			// index is also how many candidates were committed. Once that
			// prefix is longer than the branch it replaced, the node sits on a
			// valid chain longer than the one it had, so the prefix stays.
			// A restore only happens while the prefix is at most as long as
			// the removed branch, which the depth check above bounds to 30;
			// rolling that far back always fits in the cache's rollback
			// window, whereas the committed prefix has no such bound.
			if index > len(removed) {
				log.Info("side-chain replacement failed after exceeding the removed branch, keeping the applied prefix", "reason", err, "target", target.Identifier(), "num-applied", index, "num-removed", len(removed))
				return index + start, err
			}
			log.Info("side-chain replacement failed, restoring original branch", "reason", err, "target", target.Identifier(), "num-momentums", len(removed))
			if restoreErr := c.restoreBranch(insert, target.Identifier(), removed); restoreErr != nil {
				log.Crit("chain state uncertain after failed side-chain restore, can't continue", "reason", restoreErr, "cause", err, "target", target.Identifier())
				os.Exit(2)
			}
			return index + start, err
		}
		return 0, nil
	}

	index, err := c.insertMomentums(insert, momentums)
	if err != nil {
		return index + start, err
	}
	return 0, nil
}

// verifyMomentumStatically runs the checks that need no chain state: the
// advertised hash matches the content, and the signature is valid for the
// public key carried by the momentum. Whether that key is the elected
// producer is a state-dependent check that runs later.
func verifyMomentumStatically(momentum *nom.Momentum) error {
	if momentum == nil {
		return errors.Errorf("missing momentum")
	}
	if momentum.ComputeHash() != momentum.Hash {
		return verifier.ErrMHashInvalid
	}
	if len(momentum.Signature) == 0 {
		return verifier.ErrMSignatureMissing
	}
	if len(momentum.PublicKey) == 0 {
		return verifier.ErrMPublicKeyMissing
	}
	isVerified, err := wallet.VerifySignature(momentum.PublicKey, momentum.Hash.Bytes(), momentum.Signature)
	if err != nil || !isVerified {
		return verifier.ErrMSignatureInvalid
	}
	return nil
}

// restoreBranch drops whatever replaced the branch above target and puts the
// captured momentums back, in order, with their cache state.
//
// What it restores is the canonical chain and the cache. It does not undo the
// delete events listeners already saw (they see insert events for the same
// momentums instead), and the account pool stays as the rollback left it. The
// captured branch lives only in memory: if the process dies part-way through,
// the node comes back at the fork point plus whatever was committed, which is
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

// insertMomentums applies and commits the momentums one by one. The returned
// index identifies the momentum that failed.
func (c chainBridge) insertMomentums(insert sync.Locker, momentums []*nom.DetailedMomentum) (int, error) {
	for index, detailed := range momentums {
		for _, block := range detailed.AccountBlocks {
			if block.BlockType == nom.BlockTypeContractSend {
				continue
			}
			if patch := c.chain.GetPatch(block.Address, block.Identifier()); patch != nil {
				// already applied
				continue
			}
			transaction, err := c.supervisor.ApplyBlock(block)
			if err != nil {
				log.Error("error while applying account-block", "reason", err, "account-block-header", block.Header())
				return index, err
			}
			if err := c.chain.ForceAddAccountBlockTransaction(insert, transaction); err != nil {
				log.Error("error while inserting account-block in pool", "reason", err, "account-block-header", block.Header())
				return index, err
			}
		}

		transaction, err := c.supervisor.ApplyMomentum(detailed)
		if err != nil {
			return index, err
		}
		if err := c.chain.UpdateCache(insert, detailed, transaction.Changes); err != nil {
			log.Error("error while inserting cache", "reason", err, "momentum-identifier", detailed.Momentum.Identifier())
			return index, err
		}
		if err := c.chain.AddMomentumTransaction(insert, transaction); err != nil {
			log.Error("error while inserting momentum", "reason", err, "momentum-identifier", detailed.Momentum.Identifier())
			var uncertain *chain.ErrCanonicalStateUncertain
			if errors.As(err, &uncertain) {
				log.Crit("canonical chain state uncertain after failed momentum insertion, can't continue", "reason", err, "momentum-identifier", detailed.Momentum.Identifier())
				os.Exit(2)
			}
			if rollbackErr := c.chain.RollbackCacheTo(insert, detailed.Momentum.Previous()); rollbackErr != nil {
				log.Crit("cache rollback failed after failed momentum insertion, can't continue", "reason", rollbackErr, "cause", err, "momentum-identifier", detailed.Momentum.Identifier())
				os.Exit(2)
			}
			return index, err
		}
	}

	return 0, nil
}
