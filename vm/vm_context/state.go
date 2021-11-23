package vm_context

import (
	"github.com/zenon-network/go-zenon/chain/nom"
)

func (ctx *accountVmContext) GetFrontierMomentum() (*nom.Momentum, error) {
	return ctx.momentumStore.GetFrontierMomentum()
}
func (ctx *accountVmContext) GetGenesisMomentum() *nom.Momentum {
	return ctx.momentumStore.GetGenesisMomentum()
}
