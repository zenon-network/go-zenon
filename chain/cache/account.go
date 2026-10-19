// Package cache implements store.Cache: the derived account state the
// node needs at every momentum but does not want to recompute from the
// ledger — each beneficiary's fused QSR amount, each address's chain
// plasma and the activation state of sporks. Values are stored under
// keys suffixed with the momentum height they became valid at, so a
// cache store fixed at an identifier answers reads as of that height
// by taking the newest entry at or below it. Superseded entries of an
// address are pruned when that address next sends a block, down to the
// height its block acknowledges, so an address that never sends keeps
// its history. The backing database is
// the storage.CacheManager, which keeps the last rollback patches so the
// cache follows momentum rollbacks.
package cache

import (
	"math/big"

	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/types"
)

var (
	fusedAmountKeyPrefix = []byte{0}
	chainPlasmaKeyPrefix = []byte{1}
)

func getFusedAmountKeyPrefix(address []byte) []byte {
	return common.JoinBytes(accountCacheKeyPrefix, fusedAmountKeyPrefix, address)
}

func getChainPlasmaKeyPrefix(address []byte) []byte {
	return common.JoinBytes(accountCacheKeyPrefix, chainPlasmaKeyPrefix, address)
}

func (cs *cacheStore) GetStakeBeneficialAmount(address types.Address) (*big.Int, error) {
	value, err := cs.findValue(getFusedAmountKeyPrefix(address.Bytes()))
	if err != nil {
		return nil, err
	}
	return big.NewInt(0).SetBytes(value), nil
}

func (cs *cacheStore) GetChainPlasma(address types.Address) (*big.Int, error) {
	value, err := cs.findValue(getChainPlasmaKeyPrefix(address.Bytes()))
	if err != nil {
		return nil, err
	}
	return big.NewInt(0).SetBytes(value), nil
}

// pruneAccountCache deletes cache entries that are older than the given
// blocks' own acknowledged height, keyed under the block's own address.
//
// Fused-amount entries are keyed by beneficiary, not by the sender that fused
// the plasma, so they are only pruned once the beneficiary itself sends a
// block (its own acknowledged height advancing past the entries). Pruning
// them earlier, based on any bound tied to the frontier, is unsound: there is
// no maximum age for MomentumAcknowledged, so a pure beneficiary that never
// sends may later submit its first block acknowledging an arbitrarily old
// momentum, at which point an already-pruned entry would under-credit its
// plasma. The accepted tradeoff is that an address which only ever receives
// fused plasma retains one entry per fuse/cancel until it sends.
func (cs *cacheStore) pruneAccountCache(blocks []*nom.AccountBlock) error {
	for _, block := range blocks {
		all := append([]*nom.AccountBlock{block}, block.DescendantBlocks...)
		for _, b := range all {
			prefix := getFusedAmountKeyPrefix(b.Address.Bytes())
			fusedPlasmaKeys, err := cs.findExpiredKeys(prefix, b.MomentumAcknowledged.Height)
			if err != nil {
				return err
			}

			prefix = getChainPlasmaKeyPrefix(b.Address.Bytes())
			chainPlasmaKeys, err := cs.findExpiredKeys(prefix, b.MomentumAcknowledged.Height)
			if err != nil {
				return err
			}

			for _, key := range append(fusedPlasmaKeys, chainPlasmaKeys...) {
				cs.changes.Delete(key)
			}
		}
	}
	return nil
}
