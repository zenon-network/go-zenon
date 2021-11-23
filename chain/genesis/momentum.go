package genesis

import (
	"time"

	"github.com/zenon-network/go-zenon/chain"
	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/vm"
)

func newGenesisMomentum(genesisConfig *GenesisConfig, pool chain.AccountPool) *nom.MomentumTransaction {
	timestamp := time.Unix(genesisConfig.GenesisTimestampSec, 0)
	blocks := pool.GetAllUncommittedAccountBlocks()

	supervisor := vm.NewSupervisor(nil, nil)
	// genesis momentum does not go throw verifier
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
