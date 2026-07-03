package genesis

import (
	"time"

	"github.com/zenon-network/go-zenon/chain"
	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/vm"
)

// newGenesisMomentum packs the fabricated genesis account blocks
// into the height-1 genesis momentum: its timestamp comes from
// GenesisTimestampSec, its Data from ExtraData and its Content lists
// every block in the pool. The momentum VM merges the blocks'
// patches into the momentum-wide db.Patch, but unlike regular
// production the result is neither signed nor verified — the
// producer fields stay empty and the hash alone identifies the
// network's genesis.
func newGenesisMomentum(genesisConfig *GenesisConfig, pool chain.AccountPool) *nom.MomentumTransaction {
	timestamp := time.Unix(genesisConfig.GenesisTimestampSec, 0)
	blocks := pool.GetAllUncommittedAccountBlocks()

	supervisor := vm.NewSupervisor(nil, nil)
	// the genesis momentum does not go through the verifier
	m := &nom.Momentum{
		Version:         1,
		ChainIdentifier: genesisConfig.ChainIdentifier,
		Height:          1, // height
		TimestampUnix:   uint64(timestamp.Unix()),
		Data:            []byte(genesisConfig.ExtraData),
		Content:         nom.NewMomentumContent(blocks),
	}
	m.EnsureCache()
	transaction, err := supervisor.GenerateGenesisMomentum(m, pool)
	common.DealWithErr(err)

	return transaction
}
