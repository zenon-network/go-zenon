package store

import (
	"math/big"
	"time"

	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common/db"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/vm/embedded/definition"
)

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
