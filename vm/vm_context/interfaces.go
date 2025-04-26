package vm_context

import (
	"math/big"

	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/chain/store"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/consensus/api"
)

type AccountVmContext interface {
	api.PillarReader
	store.Account
	CacheStore() store.Cache
	MomentumStore() store.Momentum

	// ====== State ======

	GetFrontierMomentum() (*nom.Momentum, error)
	GetGenesisMomentum() *nom.Momentum

	// ====== Lifecycle ======

	Save()
	Reset()
	Done()

	// ====== Balance ======

	AddBalance(ts *types.ZenonTokenStandard, amount *big.Int)
	SubBalance(ts *types.ZenonTokenStandard, amount *big.Int)

	// ====== Spork ======

	IsAcceleratorSporkEnforced() bool
	IsHtlcSporkEnforced() bool
	IsBridgeAndLiquiditySporkEnforced() bool
}
