// Package genesis materializes the initial ledger state of a Zenon
// network from a GenesisConfig. The config arrives either as a JSON
// file (ReadGenesisConfigFromFile, the node's --genesis option) or as
// the alphanet config embedded in the binary as a JSON string literal
// (MakeEmbeddedGenesisConfig); the embedded alphanet uses
// ChainIdentifier 1.
//
// NewGenesis deterministically fabricates the whole genesis state:
// one height-1 account block of nom.BlockTypeGenesisReceive per
// seeded address — the pillar, token, plasma, swap and (optionally)
// spork embedded contracts receive their initial contract storage
// from the corresponding config sections, every other address listed
// in GenesisBlocks receives only balances — and a height-1 momentum
// whose Content confirms all of them. The genesis momentum is built
// by the momentum VM like any other momentum but is neither signed
// nor verified; its producer fields stay empty.
//
// The resulting store.Genesis is what chain.Init checks the database
// against: an empty database receives the genesis transaction, an
// existing one must hold a momentum at height 1 with the same hash.
// Since the momentum hash commits to the ChangesHash of the full
// genesis state, any difference in the seeded state yields an
// incompatible database. The spork address is not part of that state:
// chain.Init copies it from the config into types.SporkAddress after
// the hash check, so it can differ between two nodes on the same
// chain.
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

// NewGenesis builds the full genesis state from config: it fabricates
// the genesis account blocks in an in-memory account pool and packs
// them into the unsigned height-1 genesis momentum. The construction
// is deterministic, so the returned store.Genesis can be compared
// against an existing database by momentum hash (see
// CheckGenesisCheckSum and chain.Init). The config is assumed valid;
// use CheckGenesis to validate untrusted input first.
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
