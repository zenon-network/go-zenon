package constants

import (
	"github.com/zenon-network/go-zenon/common/types"
)

type Consensus struct {
	BlockTime   int64                    // Interval in seconds between 2 momentums
	NodeCount   uint8                    // NodeCount in an election tick
	RandCount   uint8                    // RandCount of pillars which are chosen in an election tick
	CountingZTS types.ZenonTokenStandard // CountingZTS used to compute pillar weights
}

var (
	ConsensusConfig = &Consensus{
		BlockTime:   10,
		NodeCount:   30,
		RandCount:   15,
		CountingZTS: types.ZnnTokenStandard,
	}
)
