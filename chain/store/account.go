package store

import (
	"math/big"

	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common/db"
	"github.com/zenon-network/go-zenon/common/types"
)

type Account interface {
	Identifier() types.HashHeight
	Address() *types.Address

	Storage() db.DB

	Frontier() (*nom.AccountBlock, error)
	ByHash(hash types.Hash) (*nom.AccountBlock, error)
	ByHeight(height uint64) (*nom.AccountBlock, error)
	MoreByHeight(height, count uint64) ([]*nom.AccountBlock, error)

	GetBalance(zts types.ZenonTokenStandard) (*big.Int, error)
	SetBalance(zts types.ZenonTokenStandard, balance *big.Int) error
	GetBalanceMap() (map[types.ZenonTokenStandard]*big.Int, error)

	GetChainPlasma() (*big.Int, error)
	AddChainPlasma(uint64) error

	MarkAsReceived(hash types.Hash) error
	IsReceived(hash types.Hash) bool

	SequencerFront(mailbox AccountMailbox) *types.AccountHeader
	SequencerPopFront()

	Apply(patch db.Patch) error
	Snapshot() Account
	Changes() (db.Patch, error)
}

type AccountMailbox interface {
	Address() types.Address
	Snapshot() AccountMailbox

	MarkAsUnreceived(hash types.Hash) error
	MarkAsReceived(hash types.Hash) error
	MarkBlockThatReceives(hash types.Hash, receiveHeader types.AccountHeader) error

	GetBlockWhichReceives(fromHash types.Hash) *types.AccountHeader
	GetUnreceivedAccountBlockHashes(atMost uint64) ([]types.Hash, error)

	SequencerPushBack(types.AccountHeader)
	SequencerSize() uint64
	SequencerByHeight(uint64) *types.AccountHeader
}
