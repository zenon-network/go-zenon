package pillar

import (
	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/consensus"
)

func (w *worker) generateMomentum(e consensus.ProducerEvent) (*nom.MomentumTransaction, *nom.DetailedMomentum, error) {
	insert := w.chain.AcquireInsert("momentum-generator")
	defer insert.Unlock()

	store := w.chain.GetFrontierMomentumStore()
	blocks := w.chain.GetNewMomentumContent()

	previousMomentum, err := store.GetFrontierMomentum()
	if err != nil {
		return nil, nil, err
	}

	m := &nom.Momentum{
		ChainIdentifier: w.chain.ChainIdentifier(),
		PreviousHash:    previousMomentum.Hash,
		Height:          previousMomentum.Height + 1,
		TimestampUnix:   uint64(e.StartTime.Unix()),
		Content:         nom.NewMomentumContent(blocks),
		Version:         uint64(1),
	}
	m.EnsureCache()
	detailed := &nom.DetailedMomentum{
		Momentum:      m,
		AccountBlocks: blocks,
	}
	transaction, err := w.supervisor.GenerateMomentum(detailed, w.coinbase.Signer)
	return transaction, detailed, err
}
