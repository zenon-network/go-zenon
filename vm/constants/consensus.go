package constants

import (
	"github.com/zenon-network/go-zenon/common/types"
)

// Consensus holds the parameters of the pillar election: how often
// momentums are produced, how many producer slots each election tick
// assigns and how they are filled, and which token's delegations
// determine pillar weight. An election tick lasts BlockTime *
// NodeCount seconds; of its NodeCount slots, NodeCount - RandCount
// are filled from the NodeCount highest-weighted pillars and
// RandCount are drawn at random from the remaining pillars
// (including the top-weighted ones left unselected).
type Consensus struct {
	BlockTime   int64                    // Interval in seconds between 2 momentums
	NodeCount   uint8                    // Number of producer slots in an election tick
	RandCount   uint8                    // Number of slots filled by randomly chosen pillars
	CountingZTS types.ZenonTokenStandard // Token whose delegations are counted as pillar weight
}

var (
	// ConsensusConfig is the Alphanet election configuration: a
	// momentum every 10 seconds, 30 slots per 300-second tick — 15
	// from the top-weighted pillars, 15 random — with weight counted
	// in delegated ZNN.
	ConsensusConfig = &Consensus{
		BlockTime:   10,
		NodeCount:   30,
		RandCount:   15,
		CountingZTS: types.ZnnTokenStandard,
	}
)
