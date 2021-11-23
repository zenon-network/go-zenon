package api

import (
	"math/big"

	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/types"
)

type EpochPillarStats struct {
	Epoch            uint64   `json:"epoch"`
	BlockNum         uint64   `json:"blockNum"`
	ExceptedBlockNum uint64   `json:"exceptedBlockNum"`
	Weight           *big.Int `json:"weight"`
	Name             string   `json:"name"`
}

type EpochStats struct {
	Epoch       uint64                       `json:"epoch"`
	Pillars     map[string]*EpochPillarStats `json:"pillars"`
	TotalWeight *big.Int                     `json:"totalWeight"`
	// Total number of blocks generated in an epoch
	TotalBlocks uint64 `json:"totalBlocks"`
}

type PillarReader interface {
	GetPillarWeights() (map[string]*big.Int, error)
	EpochTicker() common.Ticker
	EpochStats(epoch uint64) (*EpochStats, error)
	GetPillarDelegationsByEpoch(epoch uint64) (map[string]*types.PillarDelegationDetail, error)
}
