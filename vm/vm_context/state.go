package vm_context

import (
	"github.com/zenon-network/go-zenon/chain/nom"
)

// GetFrontierMomentum returns the frontier of the context's momentum
// store — for supervisor-built contexts, the block's acknowledged
// momentum rather than the chain's live frontier.
func (ctx *accountVmContext) GetFrontierMomentum() (*nom.Momentum, error) {
	return ctx.momentumStore.GetFrontierMomentum()
}

// GetGenesisMomentum returns the genesis momentum.
func (ctx *accountVmContext) GetGenesisMomentum() *nom.Momentum {
	return ctx.momentumStore.GetGenesisMomentum()
}
