package store

import (
	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common/types"
)

type Genesis interface {
	ChainIdentifier() uint64
	IsGenesisMomentum(hash types.Hash) bool
	GetGenesisMomentum() *nom.Momentum
	GetGenesisTransaction() *nom.MomentumTransaction
	GetSporkAddress() *types.Address
}
