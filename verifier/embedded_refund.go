package verifier

import (
	"github.com/pkg/errors"

	"github.com/zenon-network/go-zenon/chain"
	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/chain/store"
	"github.com/zenon-network/go-zenon/common/types"
)

// CacheStoreAt returns the cache store at the canonical momentum of a
// height, the store a contract receive acknowledging that momentum reads
// spork state from.
type CacheStoreAt func(height uint64) (store.Cache, error)

// CanonicalCacheStores resolves heights against frontier's momentum chain
// and opens the cache store at each one through c. A cache store is a
// height-filtered view over the cache database, so opening one does not
// rebuild historical state.
func CanonicalCacheStores(c chain.Chain, frontier store.Momentum) CacheStoreAt {
	return func(height uint64) (store.Cache, error) {
		momentum, err := frontier.GetMomentumByHeight(height)
		if err != nil {
			return nil, err
		}
		if momentum == nil {
			return nil, errors.Errorf("no momentum at height %d", height)
		}
		cacheStore := c.GetCacheStore(momentum.Identifier())
		if cacheStore == nil {
			return nil, errors.Errorf("no cache store for momentum %v", momentum.Identifier())
		}
		return cacheStore, nil
	}
}

// EmbeddedRefundRecoveryHeight reports whether the contract receive of send,
// confirmed at confirmationHeight, acknowledges a later momentum than the
// one that confirmed it, and which one. frontierHeight is the height of the
// momentum frontier the caller works against.
//
// A contract receive normally acknowledges the momentum that confirmed its
// send, and the VM reads spork state from that momentum's cache store. A
// send confirmed before EmbeddedRefundSpork was in force would never see
// the spork, so a queue that stalled on a failed contract-to-contract
// refund (issue #271) before then would stay stalled. Once the spork is in
// force, such a send is received at the first momentum whose cache store
// has it in force. That is normally its EnforcementHeight, later only if
// the activation receive itself was confirmed after that height.
//
// Only a positive-amount send from an embedded contract qualifies, since
// only its failure needs the refund that stalls. Those are the Bridge,
// Pillar and Liquidity burns and the Liquidity donations; their Token
// Burn and Accelerator Donate receives read no momentum, time or spork
// state, so moving them to a later momentum cannot change a successful
// outcome. Every other receive, including a user's HTLC or PTLC unlock,
// keeps its confirmation momentum.
//
// The answer depends only on chain history. Spork state is monotonic (a
// spork cannot be deactivated), so the recovery height is fixed once the
// spork is in force at the frontier. The producer passes its frontier; the
// verifier passes the frontier it checks the block against, which is at or
// above the block's acknowledged momentum. Before the spork is in force it
// reports false and receives are unchanged.
//
// Cost: an eligible receive whose send was confirmed once the spork was in
// force costs two cache-store lookups (frontier and confirmation height).
// Only a send confirmed before then adds a binary search over the heights
// in between.
func EmbeddedRefundRecoveryHeight(frontierHeight uint64, cacheAt CacheStoreAt, send *nom.AccountBlock, confirmationHeight uint64) (uint64, bool, error) {
	if !types.IsEmbeddedAddress(send.Address) || send.Amount == nil || send.Amount.Sign() <= 0 {
		return 0, false, nil
	}
	if confirmationHeight == 0 || confirmationHeight >= frontierHeight {
		return 0, false, nil
	}
	inForce := func(height uint64) (bool, error) {
		cacheStore, err := cacheAt(height)
		if err != nil {
			return false, err
		}
		return cacheStore.IsSporkActive(types.EmbeddedRefundSpork)
	}
	if active, err := inForce(frontierHeight); err != nil || !active {
		return 0, false, err
	}
	if active, err := inForce(confirmationHeight); err != nil || active {
		return 0, false, err
	}
	// The spork is not in force at the confirmation height and is at the
	// frontier: find the first height in between where it is.
	low, high := confirmationHeight+1, frontierHeight
	for low < high {
		middle := low + (high-low)/2
		active, err := inForce(middle)
		if err != nil {
			return 0, false, err
		}
		if active {
			high = middle
		} else {
			low = middle + 1
		}
	}
	return low, true, nil
}
