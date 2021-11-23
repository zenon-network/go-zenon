package consensus

import (
	"time"

	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/vm/constants"
)

type Context struct {
	common.Ticker
	constants.Consensus
	GenesisTime time.Time
}

func NewConsensusContext(genesisTime time.Time) *Context {
	config := constants.ConsensusConfig
	context := &Context{
		Consensus:   *config,
		GenesisTime: genesisTime,
	}

	context.Ticker = common.NewTicker(genesisTime, time.Second*time.Duration(uint64(config.BlockTime)*uint64(config.NodeCount)))
	return context
}
