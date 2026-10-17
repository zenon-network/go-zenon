// Package vm_context defines the execution contexts the vm package
// runs against. An AccountVmContext combines the account store the
// block builds on, a read-only momentum store fixed at the block's
// acknowledged momentum and a consensus pillar reader, and adds
// balance arithmetic, a save/reset/done snapshot window for embedded
// execution and spork queries. A MomentumVMContext is simply the
// momentum store the momentum's account-block patches are folded
// into. Both come in genesis flavors backed by empty in-memory
// stores.
package vm_context

import (
	"math/big"

	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/chain/store"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/consensus/api"
)

// AccountVmContext is the environment an account block executes in.
// It is the writable store.Account at the block's predecessor — all
// balance, storage and plasma writes land there and are read back as
// the block's Changes patch — plus a read-only momentum store fixed
// at the block's MomentumAcknowledged (MomentumStore), the chain
// cache store at the same momentum (CacheStore, which answers the
// spork and plasma-variable queries) and the consensus
// api.PillarReader there, which embedded contracts use for epoch
// statistics and pillar weights. Not every context carries all of
// them: the supervisor builds user-block contexts with a nil momentum
// store and pillar reader, so the momentum- and pillar-reading helpers
// may not be called on those, and rpc/api's GetFrontierContext passes
// a momentum store but a nil pillar reader, so RPC code may read
// momentum state but not pillar statistics through it.
//
// GetFrontierMomentum returns the frontier of the context's momentum
// store — the acknowledged momentum, not the chain's live frontier —
// and GetGenesisMomentum the genesis momentum.
//
// Save opens a snapshot window: subsequent writes go to a
// copy-on-write snapshot of the account store. Done folds the
// snapshot's changes back into the saved store; Reset discards them
// instead. The vm wraps each embedded-method execution in such a
// window so a failing method leaves no trace.
//
// AddBalance and SubBalance adjust the account's balance for one
// token standard; SubBalance panics if the balance would go negative
// (callers check funds first).
//
// The Is*SporkEnforced methods report whether the respective spork is
// active at the context's momentum; the embedded package uses them to
// select which contract-method set is live.
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
	IsDynamicPlasmaSporkEnforced() bool
	IsPtlcSporkEnforced() bool
}
