package store

import (
	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common/types"
)

// Genesis is the read-only view of the genesis configuration that the
// momentum store carries along: the network's ChainIdentifier, the
// genesis momentum (height 1) with the db.Patch that creates the
// initial state (GetGenesisTransaction), and the address allowed to
// activate sporks. It is implemented by chain/genesis and embedded in
// the Momentum interface.
type Genesis interface {
	ChainIdentifier() uint64
	IsGenesisMomentum(hash types.Hash) bool
	GetGenesisMomentum() *nom.Momentum
	GetGenesisTransaction() *nom.MomentumTransaction
	GetSporkAddress() *types.Address
}
