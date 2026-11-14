package chain

import (
	"bytes"
	"errors"
	"fmt"
	"sort"

	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/chain/store"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/vm/embedded/definition"
)

var (
	// ErrStaleMomentumAcknowledged rejects a user account block whose
	// acknowledged momentum does not yet enforce every spork enforced at
	// the parent of the momentum that would confirm it. See
	// AcknowledgementRequirement.
	ErrStaleMomentumAcknowledged = errors.New("account-block momentum-acknowledged predates a spork enforced at the parent momentum")
	// ErrMomentumAcknowledgedNotCanonical rejects a user account block
	// whose acknowledged momentum is not the canonical momentum at its
	// height at or below the parent momentum.
	ErrMomentumAcknowledgedNotCanonical = errors.New("account-block momentum-acknowledged is not canonical at the parent momentum")
)

// IsAcknowledgementRejection reports whether err, returned by
// AcknowledgementRequirement.Check, means the block fails the rule, as
// opposed to the rule being unreadable. Callers treat both as "the block
// may not be confirmed now"; only a rejection is grounds for evicting it.
func IsAcknowledgementRejection(err error) bool {
	return errors.Is(err, ErrStaleMomentumAcknowledged) || errors.Is(err, ErrMomentumAcknowledgedNotCanonical)
}

// CacheStoreAt returns the chain cache view at a momentum, or nil if the
// cache cannot answer for it. ChainCache.GetCacheStore has this shape.
type CacheStoreAt func(types.HashHeight) store.Cache

// AcknowledgementRequirement is the acknowledgement recency rule as it
// applies to the momentum built on one parent momentum P. The rule is in
// force from the momentum whose parent enforces DynamicPlasmaSpork (issue
// #227): the spork-gated rules a block runs under are read at its own
// acknowledged momentum, so without this rule a block acknowledging a
// momentum from before an activation could keep the rules from before it.
//
// Let E(P) be the ids of the sporks defined at P that are activated with
// EnforcementHeight <= P.Height. A user account block confirmed by the
// momentum after P passes when every id in E(P) is also enforced at the
// block's MomentumAcknowledged M, as the chain cache at M answers it.
// Enforced sets only grow along a canonical branch, so this says M's
// enforced set equals P's. The rule compares sets, not heights, because
// an activation record reaches state only when the momentum containing
// the spork contract's receive is applied, which can be after the
// EnforcementHeight it records; a height comparison would let an
// acknowledgement in that window through.
//
// Embedded account blocks are exempt: a contract receive's
// acknowledgement is pinned to its send's confirmation height and its
// descendants share it.
//
// E(P) is read once, when the requirement is built; Check costs one
// canonical-hash lookup plus |E(P)| point reads of the cache per distinct
// acknowledged momentum, and remembers the answer. A requirement is
// bound to one parent and is not safe for concurrent use.
type AcknowledgementRequirement struct {
	parent           types.HashHeight
	enforced         []types.Hash
	momentumByHeight func(uint64) (*nom.Momentum, error)
	cacheAt          CacheStoreAt
	results          map[types.HashHeight]error
}

// NewAcknowledgementRequirement builds the requirement for the momentum
// that extends parent. It returns nil, and no error, when the rule does
// not apply there: DynamicPlasmaSpork is not enforced at parent.
// A nil requirement passes every block. A spork state that cannot be read
// is an error, never an empty set.
func NewAcknowledgementRequirement(parent store.Momentum, cacheAt CacheStoreAt) (*AcknowledgementRequirement, error) {
	if parent == nil {
		return nil, errors.New("acknowledgement requirement: no parent momentum store")
	}
	return newAcknowledgementRequirement(
		parent.Identifier(),
		func() ([]*definition.Spork, error) {
			account := parent.GetAccountStore(types.SporkContract)
			if account == nil {
				return nil, errors.New("no spork contract store")
			}
			return definition.ReadAllSporks(account.Storage())
		},
		parent.GetMomentumByHeight,
		cacheAt,
	)
}

func newAcknowledgementRequirement(parent types.HashHeight, sporks func() ([]*definition.Spork, error), momentumByHeight func(uint64) (*nom.Momentum, error), cacheAt CacheStoreAt) (*AcknowledgementRequirement, error) {
	// No spork is enforced at genesis (see IsSporkActive).
	if parent.Height <= 1 {
		return nil, nil
	}
	defined, err := sporks()
	if err != nil {
		return nil, fmt.Errorf("acknowledgement requirement: reading sporks at parent %v: %w", parent, err)
	}

	gated := false
	enforced := make([]types.Hash, 0, len(defined))
	for _, spork := range defined {
		if !spork.Activated || spork.EnforcementHeight > parent.Height {
			continue
		}
		enforced = append(enforced, spork.Id)
		if spork.Id == types.DynamicPlasmaSpork.SporkId {
			gated = true
		}
	}
	if !gated {
		return nil, nil
	}
	// The answer does not depend on the order; sorting makes the first
	// missing spork named in an error the same on every node.
	sort.Slice(enforced, func(i, j int) bool {
		return bytes.Compare(enforced[i].Bytes(), enforced[j].Bytes()) < 0
	})
	return &AcknowledgementRequirement{
		parent:           parent,
		enforced:         enforced,
		momentumByHeight: momentumByHeight,
		cacheAt:          cacheAt,
		results:          make(map[types.HashHeight]error),
	}, nil
}

// Check returns nil when block may be confirmed by the momentum after the
// requirement's parent as far as this rule is concerned. A rule failure
// wraps ErrStaleMomentumAcknowledged or ErrMomentumAcknowledgedNotCanonical
// (see IsAcknowledgementRejection); any other error means the state needed
// to decide could not be read, and the block must not pass either.
func (r *AcknowledgementRequirement) Check(block *nom.AccountBlock) error {
	if r == nil || types.IsEmbeddedAddress(block.Address) {
		return nil
	}
	acknowledged := block.MomentumAcknowledged
	if result, ok := r.results[acknowledged]; ok {
		return result
	}
	result := r.evaluate(acknowledged)
	r.results[acknowledged] = result
	return result
}

func (r *AcknowledgementRequirement) evaluate(acknowledged types.HashHeight) error {
	if acknowledged == r.parent {
		return nil
	}
	if acknowledged.Height > r.parent.Height {
		return fmt.Errorf("%w: acknowledged %v is above parent %v", ErrMomentumAcknowledgedNotCanonical, acknowledged, r.parent)
	}
	momentum, err := r.momentumByHeight(acknowledged.Height)
	if err != nil {
		return fmt.Errorf("acknowledgement requirement: reading momentum at height %d: %w", acknowledged.Height, err)
	}
	if momentum == nil || momentum.Hash != acknowledged.Hash {
		return fmt.Errorf("%w: acknowledged %v, parent %v", ErrMomentumAcknowledgedNotCanonical, acknowledged, r.parent)
	}
	var cache store.Cache
	if r.cacheAt != nil {
		cache = r.cacheAt(acknowledged)
	}
	if cache == nil {
		return fmt.Errorf("acknowledgement requirement: no cache store at %v", acknowledged)
	}
	for _, id := range r.enforced {
		active, err := cache.IsSporkActive(&types.ImplementedSpork{SporkId: id})
		if err != nil {
			return fmt.Errorf("acknowledgement requirement: reading spork %v at %v: %w", id, acknowledged, err)
		}
		if !active {
			return fmt.Errorf("%w: spork %v is enforced at parent height %d but not at acknowledged height %d", ErrStaleMomentumAcknowledged, id, r.parent.Height, acknowledged.Height)
		}
	}
	return nil
}

// Filter returns blocks without every user account block that does not
// pass Check and without every block of the same account above it, since
// those chain onto a block that cannot be confirmed. The relative order of
// the kept blocks is unchanged. onSkip, if not nil, is called once for each
// account whose chain is cut, with the lowest failing block and its error.
// A nil requirement returns blocks itself.
func (r *AcknowledgementRequirement) Filter(blocks []*nom.AccountBlock, onSkip func(*nom.AccountBlock, error)) []*nom.AccountBlock {
	if r == nil {
		return blocks
	}
	type cut struct {
		block *nom.AccountBlock
		err   error
	}
	cuts := make(map[types.Address]cut)
	for _, block := range blocks {
		if err := r.Check(block); err != nil {
			if c, ok := cuts[block.Address]; !ok || block.Height < c.block.Height {
				cuts[block.Address] = cut{block: block, err: err}
			}
		}
	}
	if len(cuts) == 0 {
		return blocks
	}
	if onSkip != nil {
		for _, block := range blocks {
			if c, ok := cuts[block.Address]; ok && c.block == block {
				onSkip(c.block, c.err)
			}
		}
	}
	kept := make([]*nom.AccountBlock, 0, len(blocks))
	for _, block := range blocks {
		if c, ok := cuts[block.Address]; ok && block.Height >= c.block.Height {
			continue
		}
		kept = append(kept, block)
	}
	return kept
}
