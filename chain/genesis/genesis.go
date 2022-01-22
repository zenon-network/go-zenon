package genesis

import (
	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/chain/store"
	"github.com/zenon-network/go-zenon/common/types"
)

type genesis struct {
	config              *GenesisConfig
	momentumTransaction *nom.MomentumTransaction
}

func NewGenesis(config *GenesisConfig) store.Genesis {
	accountPool := newGenesisAccountBlocks(config)
	momentumTransaction := newGenesisMomentum(config, accountPool)

	return &genesis{
		config:              config,
		momentumTransaction: momentumTransaction,
	}
}

func (g *genesis) ChainIdentifier() uint64 {
	return g.config.ChainIdentifier
}
func (g *genesis) IsGenesisMomentum(hash types.Hash) bool {
	return hash == g.momentumTransaction.Momentum.Hash
}
func (g *genesis) GetGenesisMomentum() *nom.Momentum {
	return g.momentumTransaction.Momentum
}
func (g *genesis) GetGenesisTransaction() *nom.MomentumTransaction {
	return g.momentumTransaction
}
func (g *genesis) GetSporkAddress() *types.Address {
	return g.config.SporkAddress
}
