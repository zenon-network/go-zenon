package vm_context

import (
	"github.com/zenon-network/go-zenon/chain/account"
	"github.com/zenon-network/go-zenon/chain/store"
	"github.com/zenon-network/go-zenon/common/db"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/consensus/api"
)

// accountVmContext implements AccountVmContext by embedding the
// account store and pillar reader directly. While a Save window is
// open, Account is the working copy-on-write snapshot and
// accountStoreSnapshot holds the original store to fall back to (see
// lifecycle.go).
type accountVmContext struct {
	accountStoreSnapshot store.Account
	api.PillarReader
	store.Account
	momentumStore store.Momentum
}

// MomentumStore returns the read-only momentum store the context was
// created with: the block's acknowledged momentum on the supervisor
// path, the frontier store for RPC contexts, or nil for genesis
// contexts (see NewAccountContext).
func (ctx *accountVmContext) MomentumStore() store.Momentum {
	return ctx.momentumStore
}

// NewAccountContext assembles an AccountVmContext from its three
// stores. The supervisor passes the momentum store at the block's
// MomentumAcknowledged, the account store at the block's predecessor
// and the consensus pillar reader fixed at the same momentum;
// rpc/api's GetFrontierContext passes frontier stores and a nil
// pillar reader.
func NewAccountContext(momentumStore store.Momentum, accountBlock store.Account, pillarReader api.PillarReader) AccountVmContext {
	return &accountVmContext{
		momentumStore: momentumStore,
		Account:       accountBlock,
		PillarReader:  pillarReader,
	}
}

// NewGenesisAccountContext returns a context over an empty in-memory
// account store for address, with no momentum store and no pillar
// reader. chain/genesis uses it to write the embedded contracts'
// genesis storage entries.
func NewGenesisAccountContext(address types.Address) AccountVmContext {
	return NewAccountContext(nil, account.NewAccountStore(address, db.NewMemDB()), nil)
}
