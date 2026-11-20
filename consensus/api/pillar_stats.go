// Package api defines the pillar statistics surface of the consensus
// module: the per-epoch and per-election-tick production statistics
// types and the PillarReader interface through which the rest of the
// node — the VM, the RPC layer and its consensus cache — consumes
// them.
package api

import (
	"math/big"

	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/types"
)

// EpochPillarStats is one pillar's production record for one epoch.
type EpochPillarStats struct {
	Epoch            uint64   `json:"epoch"`
	BlockNum         uint64   `json:"blockNum"`
	ExpectedBlockNum uint64   `json:"exceptedBlockNum"` // tag misspelling kept for wire compatibility
	Weight           *big.Int `json:"weight"`
	Name             string   `json:"name"`
}

// EpochStats aggregates the production statistics of all pillars for
// one epoch, as served by the embedded pillar RPC API.
type EpochStats struct {
	Epoch       uint64                       `json:"epoch"`
	Pillars     map[string]*EpochPillarStats `json:"pillars"`
	TotalWeight *big.Int                     `json:"totalWeight"`
	// Total number of blocks generated in an epoch
	TotalBlocks uint64 `json:"totalBlocks"`
}

// PeriodPillarStats is one pillar's production in one election tick:
// the momentums it produced and the slots it was elected to. Eligible
// is false when the pillar was jailed at the tick's proof momentum, so
// the election excluded it unless the election fell back to every
// active pillar.
type PeriodPillarStats struct {
	BlockNum         uint64
	ExpectedBlockNum uint64
	Eligible         bool
}

// PeriodStats is the production of one election tick by pillar name.
// Tick is the election tick and StartTime its start in unix seconds. A
// momentum whose producer is not elected in the tick counts under the
// name "". Every pillar in the tick's election delegations appears,
// with ExpectedBlockNum 0 when it was not elected, for example an
// unelected or jailed pillar.
type PeriodStats struct {
	Tick      uint64
	StartTime int64
	Pillars   map[string]*PeriodPillarStats
}

// PillarReader reads pillar statistics computed by the consensus
// module at a particular chain state (see consensus.Consensus
// FrontierPillarReader and FixedPillarReader): the current delegated
// weights by pillar name, the 24-hour epoch ticker anchored at
// genesis, per-epoch production statistics, per-epoch averaged
// delegations and the production statistics of finished election
// ticks. The rpc embedded pillar API serves the weights and
// current-epoch statistics through a consensus cache refreshed at
// most every five minutes.
type PillarReader interface {
	GetPillarWeights() (map[string]*big.Int, error)
	EpochTicker() common.Ticker
	EpochStats(epoch uint64) (*EpochStats, error)
	GetPillarDelegationsByEpoch(epoch uint64) (map[string]*types.PillarDelegationDetail, error)
	// FinishedPeriodStats returns, oldest first, the statistics of the
	// last count election ticks that have finished at the reader's
	// momentum. It returns fewer entries when fewer ticks have
	// finished, and none for count 0.
	FinishedPeriodStats(count uint64) ([]*PeriodStats, error)
}
