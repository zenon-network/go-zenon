package pillar

import (
	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/db"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/consensus"
	"github.com/zenon-network/go-zenon/dp"
	"github.com/zenon-network/go-zenon/vm/embedded/definition"
)

// filterExpiredMultisigBlocks drops multisig account-blocks that would never pass
// rawMomentumVerifier.content() for the momentum being built (height = previousHeight+1): either
// their MomentumAcknowledged is too old (stale-MA backlog-hygiene bound), or their signatures no
// longer satisfy the policy active at that height (live authorisation, checked against the same
// registry snapshot content() uses). The block-selectors don't re-apply either check, so an
// honest producer must exclude such blocks itself or content() will reject the whole momentum
// every attempt, permanently stalling production on the same poison block.
//
// Dropping is contiguous per account: once one of an account's blocks is removed, every higher
// block on that account is removed too. Momentum content for an account is a contiguous height
// prefix, so keeping a descendant of a removed block would orphan it and content()'s previous
// check would reject the whole momentum — reintroducing the halt for a stale parent with a
// still-recent child. Same-address blocks arrive here in ascending height order (both selectors
// order them so), so a per-account flag preserves the prefix.
func filterExpiredMultisigBlocks(blocks []*nom.AccountBlock, previousHeight uint64, registry db.DB) []*nom.AccountBlock {
	momentumHeight := previousHeight + 1
	filtered := make([]*nom.AccountBlock, 0, len(blocks))
	dropped := make(map[types.Address]bool)
	policyCache := make(map[types.Address]*definition.MultisigPolicy)
	for _, block := range blocks {
		if types.IsMultisigAddress(block.Address) {
			var sigs [][]byte
			if block.MultisigAuth != nil {
				sigs = block.MultisigAuth.Signatures
			}
			staleMA := definition.IsMultisigMAStale(block.MomentumAcknowledged.Height, momentumHeight)
			policy, ok := policyCache[block.Address]
			if !ok {
				rec, err := definition.GetMultisigRecord(registry, block.Address)
				common.DealWithErr(err)
				policy = definition.ActivePolicyAtHeight(rec, momentumHeight)
				policyCache[block.Address] = policy
			}
			if dropped[block.Address] || staleMA || !definition.VerifyThresholdSignatures(policy, block.Hash.Bytes(), sigs) {
				dropped[block.Address] = true
				continue
			}
		}
		filtered = append(filtered, block)
	}
	return filtered
}

func (w *worker) generateMomentum(e consensus.ProducerEvent) (*nom.MomentumTransaction, *nom.DetailedMomentum, error) {
	insert := w.chain.AcquireInsert("momentum-generator")
	defer insert.Unlock()

	store := w.chain.GetFrontierMomentumStore()
	previousMomentum, err := store.GetFrontierMomentum()
	if err != nil {
		return nil, nil, err
	}

	isDynamicPlasmaActive, err := store.IsSporkActive(types.DynamicPlasmaSpork)
	if err != nil {
		return nil, nil, err
	}

	var (
		m      *nom.Momentum
		blocks []*nom.AccountBlock
	)

	registry := store.GetAccountStore(types.MultisigContract).Storage()

	if isDynamicPlasmaActive {
		config, err := store.GetPlasmaVariables()
		if err != nil {
			return nil, nil, err
		}
		plasma := dp.NewDynamicPlasma(previousMomentum, config)
		blocks = NewMomentumContentSelector(plasma).Content(w.chain.GetAllUncommittedAccountBlocks())
		blocks = filterExpiredMultisigBlocks(blocks, previousMomentum.Height, registry)
		basePlasma := plasma.ComputeTotalBasePlasma(blocks)
		m = &nom.Momentum{
			ChainIdentifier: w.chain.ChainIdentifier(),
			PreviousHash:    previousMomentum.Hash,
			Height:          previousMomentum.Height + 1,
			TimestampUnix:   uint64(e.StartTime.Unix()),
			Content:         nom.NewMomentumContent(blocks),
			Version:         nom.DynamicPlasmaMomentumVersion,
			NextFusionPrice: plasma.NextFusionPrice(basePlasma.Fusion),
			NextWorkPrice:   plasma.NextWorkPrice(basePlasma.Pow),
		}
	} else {
		blocks = w.chain.GetNewMomentumContent()
		blocks = filterExpiredMultisigBlocks(blocks, previousMomentum.Height, registry)
		m = &nom.Momentum{
			ChainIdentifier: w.chain.ChainIdentifier(),
			PreviousHash:    previousMomentum.Hash,
			Height:          previousMomentum.Height + 1,
			TimestampUnix:   uint64(e.StartTime.Unix()),
			Content:         nom.NewMomentumContent(blocks),
			Version:         uint64(1),
		}
	}
	m.EnsureCache()
	detailed := &nom.DetailedMomentum{
		Momentum:      m,
		AccountBlocks: blocks,
	}
	transaction, err := w.supervisor.GenerateMomentum(detailed, w.coinbase.Signer)
	return transaction, detailed, err
}
