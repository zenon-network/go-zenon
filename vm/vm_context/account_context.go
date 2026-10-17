package vm_context

import (
	"github.com/zenon-network/go-zenon/chain/account"
	"github.com/zenon-network/go-zenon/chain/store"
	"github.com/zenon-network/go-zenon/common/db"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/consensus/api"
)

type accountVmContext struct {
	accountStoreSnapshot store.Account
	api.PillarReader
	store.Account
	momentumStore store.Momentum
	cacheStore    store.Cache
}

func (ctx *accountVmContext) MomentumStore() store.Momentum {
	return ctx.momentumStore
}

func (ctx *accountVmContext) CacheStore() store.Cache {
	return ctx.cacheStore
}

// NewAccountContext assembles an AccountVmContext from its stores.
// For an embedded contract's block the supervisor passes the momentum
// store at the block's MomentumAcknowledged, the account store at the
// block's predecessor, the chain cache store at the same momentum and
// the consensus pillar reader fixed there. For an ordinary user block
// it passes a nil momentum store and a nil pillar reader, since user
// blocks never execute contract code; rpc/api's GetFrontierContext
// passes frontier stores and a nil pillar reader.
func NewAccountContext(momentumStore store.Momentum, accountBlock store.Account, cacheStore store.Cache, pillarReader api.PillarReader) AccountVmContext {
	return &accountVmContext{
		momentumStore: momentumStore,
		Account:       accountBlock,
		cacheStore:    cacheStore,
		PillarReader:  pillarReader,
	}
}

// NewGenesisAccountContext returns a context over an empty in-memory
// account store for address, with no momentum store and no pillar
// reader. chain/genesis uses it to write the embedded contracts'
// genesis storage entries.
func NewGenesisAccountContext(address types.Address) AccountVmContext {
	return NewAccountContext(nil, account.NewAccountStore(address, db.NewMemDB()), nil, nil)
}
