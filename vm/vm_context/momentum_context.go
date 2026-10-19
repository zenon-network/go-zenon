package vm_context

import (
	"github.com/zenon-network/go-zenon/chain/momentum"
	"github.com/zenon-network/go-zenon/chain/store"
)

// MomentumVMContext is the environment a momentum executes in: just
// the writable momentum store the account-block patches of the
// momentum's content are folded into. Its accumulated Changes patch
// is hashed into the momentum's ChangesHash.
type MomentumVMContext interface {
	store.Momentum
}

type momentumVMContext struct {
	store.Momentum
}

// NewMomentumVMContext wraps a momentum store — the supervisor passes
// the snapshot at the momentum's predecessor — as a MomentumVMContext.
func NewMomentumVMContext(store store.Momentum) MomentumVMContext {
	return &momentumVMContext{
		Momentum: store,
	}
}

// NewGenesisMomentumVMContext returns a context over an empty
// in-memory momentum store, used to build the genesis momentum, which
// has no predecessor to snapshot.
func NewGenesisMomentumVMContext() MomentumVMContext {
	return &momentumVMContext{
		Momentum: momentum.NewGenesisStore(),
	}
}
