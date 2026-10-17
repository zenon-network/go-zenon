package store

import (
	"math/big"

	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common/db"
	"github.com/zenon-network/go-zenon/common/types"
)

// Cache is the read view of the chain cache (chain/cache) at one
// momentum: the fused QSR backing an address's plasma, an address's
// cumulative chain plasma and whether a spork is active, all as of
// Identifier. ApplyMomentum folds a momentum's state patch into the
// cache's change set and Changes exports that set.
type Cache interface {
	Identifier() types.HashHeight
	GetStakeBeneficialAmount(types.Address) (*big.Int, error)
	GetChainPlasma(types.Address) (*big.Int, error)
	IsSporkActive(*types.ImplementedSpork) (bool, error)

	ApplyMomentum(*nom.DetailedMomentum, db.Patch) error
	Changes() (db.Patch, error)
}
