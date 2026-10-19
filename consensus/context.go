package consensus

import (
	"time"

	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/vm/constants"
)

// Context bundles what the election needs to know about timing: the
// consensus parameters (constants.Consensus), the genesis timestamp,
// and the election-tick Ticker anchored at genesis whose interval is
// BlockTime * NodeCount seconds — one slot per node per tick.
type Context struct {
	common.Ticker
	constants.Consensus
	GenesisTime time.Time
}

// NewConsensusContext builds the election Context for a chain whose
// genesis momentum has the given timestamp, using the global
// constants.ConsensusConfig (on Alphanet: 10-second momentums and 30
// slots, so a 300-second election tick).
func NewConsensusContext(genesisTime time.Time) *Context {
	config := constants.ConsensusConfig
	context := &Context{
		Consensus:   *config,
		GenesisTime: genesisTime,
	}

	context.Ticker = common.NewTicker(genesisTime, time.Second*time.Duration(uint64(config.BlockTime)*uint64(config.NodeCount)))
	return context
}
