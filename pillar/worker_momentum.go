package pillar

import (
	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/consensus"
)

func (w *worker) generateMomentum(e consensus.ProducerEvent) (*nom.MomentumTransaction, error) {
	insert := w.chain.AcquireInsert("momentum-generator")
	defer insert.Unlock()

	store := w.chain.GetFrontierMomentumStore()
	blocks := w.chain.GetNewMomentumContent()

	previousMomentum, err := store.GetFrontierMomentum()
	if err != nil {
		return nil, err
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
	return w.supervisor.GenerateMomentum(&nom.DetailedMomentum{
		Momentum:      m,
		AccountBlocks: blocks,
	}, w.coinbase.Signer)
}
