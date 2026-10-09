package store

import (
	"math/big"

	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common/db"
	"github.com/zenon-network/go-zenon/common/types"
)

type Cache interface {
	Identifier() types.HashHeight
	GetStakeBeneficialAmount(types.Address) (*big.Int, error)
	GetChainPlasma(types.Address) (*big.Int, error)
	IsSporkActive(*types.ImplementedSpork) (bool, error)

	ApplyMomentum(*nom.DetailedMomentum, db.Patch) error
	Changes() (db.Patch, error)
}
