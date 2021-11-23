package vm_context

import (
	"github.com/zenon-network/go-zenon/chain/momentum"
	"github.com/zenon-network/go-zenon/chain/store"
)

type MomentumVMContext interface {
	store.Momentum
}

type momentumVMContext struct {
	store.Momentum
}

func NewMomentumVMContext(store store.Momentum) MomentumVMContext {
	return &momentumVMContext{
		Momentum: store,
	}
}

func NewGenesisMomentumVMContext() MomentumVMContext {
	return &momentumVMContext{
		Momentum: momentum.NewGenesisStore(),
	}
}
