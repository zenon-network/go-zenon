package pillar

import (
	"github.com/zenon-network/go-zenon/chain"
	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/consensus"
	"github.com/zenon-network/go-zenon/dp"
)

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

	// The acknowledgement recency rule at this parent, read once for the
	// whole selection; nil while DynamicPlasmaSpork is not enforced,
	// which leaves the pre-dynamic-plasma selection below unchanged.
	acknowledgement, err := chain.NewAcknowledgementRequirement(store, w.chain.GetCacheStore)
	if err != nil {
		return nil, nil, err
	}

	var (
		m      *nom.Momentum
		blocks []*nom.AccountBlock
	)

	if isDynamicPlasmaActive {
		config, err := store.GetPlasmaVariables()
		if err != nil {
			return nil, nil, err
		}
		plasma := dp.NewDynamicPlasma(previousMomentum, config)
		blocks = NewMomentumContentSelectorWithAcknowledgement(plasma, acknowledgement).Content(w.chain.GetAllUncommittedAccountBlocks())
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
		// Blocks that cannot be confirmed are removed, with everything above
		// them in their account, before the block-count cap and byte budget
		// are applied, so they cannot take the room of eligible blocks.
		// Without an enforced requirement this is GetNewMomentumContent.
		candidates := acknowledgement.Filter(w.chain.GetAllUncommittedAccountBlocks(), reportStaleAcknowledgement)
		blocks = w.chain.SelectNewMomentumContent(candidates)
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
