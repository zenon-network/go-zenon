package store

import (
	"math/big"
	"time"

	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common/db"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/vm/embedded/definition"
)

// Momentum is a view over the momentum ledger at a specific version —
// one momentum-chain database version, implemented by chain/momentum.
// Besides the momentum chain itself it exposes the momentum-confirmed
// state of every account chain (GetAccountStore, GetAccountDB and
// GetAccountMailbox return copy-on-write snapshots of per-address
// subsets) and read helpers over embedded-contract state.
//
// Identifier returns the frontier momentum's hash-height (zero for an
// empty store). Getters return nil entries with a nil error when the
// requested entry does not exist; GetAccountBlockByHash resolves
// through a hash-to-header index that contains only
// momentum-confirmed blocks. GetMomentumBeforeTime returns the most
// recent momentum whose timestamp is strictly before the given time,
// or nil if there is none. PrefetchMomentum expands a momentum's
// Content headers into the full account blocks of a
// nom.DetailedMomentum, the form broadcast to momentum event
// listeners.
//
// GetBlockWhichReceives returns the confirmed receive block that
// consumed the given send-block hash, and GetBlockConfirmationHeight
// the height of the momentum that confirmed a block (0 if
// unconfirmed).
//
// AddAccountBlockTransaction applies a confirmed account block's
// state patch onto the per-address subset and updates the derived
// indexes (header-by-hash, confirmation heights, mailboxes, embedded
// sequencer, cached ZNN balances). The momentum VM calls it for every
// header in a momentum's content; the resulting writes become the
// Changes patch of the nom.MomentumTransaction.
type Momentum interface {
	Genesis

	Identifier() types.HashHeight
	GetFrontierMomentum() (*nom.Momentum, error)
	GetMomentumByHash(types.Hash) (*nom.Momentum, error)
	GetMomentumByHeight(uint64) (*nom.Momentum, error)

	GetAccountBlock(types.AccountHeader) (*nom.AccountBlock, error)
	GetFrontierAccountBlock(types.Address) (*nom.AccountBlock, error)
	GetAccountBlockByHash(types.Hash) (*nom.AccountBlock, error)
	GetAccountBlockByHeight(types.Address, uint64) (*nom.AccountBlock, error)

	// Range queries

	GetAccountBlocksByHeight(address types.Address, height, count uint64) ([]*nom.AccountBlock, error)
	GetMomentumsByHash(blockHash types.Hash, higher bool, count uint64) ([]*nom.Momentum, error)
	GetMomentumsByHeight(height uint64, higher bool, count uint64) ([]*nom.Momentum, error)
	GetMomentumBeforeTime(timestamp *time.Time) (*nom.Momentum, error)
	PrefetchMomentum(momentum *nom.Momentum) (*nom.DetailedMomentum, error)

	// Unreceived

	GetBlockWhichReceives(hash types.Hash) (*nom.AccountBlock, error)

	// Confirmed

	GetBlockConfirmationHeight(hash types.Hash) (uint64, error)

	// Embedded

	GetAllDefinedSporks() ([]*definition.Spork, error)
	GetActivePillars() ([]*definition.PillarInfo, error)
	IsSporkActive(*types.ImplementedSpork) (bool, error)
	GetStakeBeneficialAmount(addr types.Address) (*big.Int, error)
	GetTokenInfoByTs(ts types.ZenonTokenStandard) (*definition.TokenInfo, error)
	ComputePillarDelegations() ([]*types.PillarDelegationDetail, error)

	GetAccountStore(address types.Address) Account
	GetAccountDB(address types.Address) db.DB
	GetAccountMailbox(address types.Address) AccountMailbox

	Snapshot() Momentum
	Changes() (db.Patch, error)

	AddAccountBlockTransaction(header types.AccountHeader, patch db.Patch) error
}
