// Package store declares the read/write interfaces of the two NoM
// ledgers — Account for a single account chain and Momentum for the
// momentum chain — together with the per-address AccountMailbox and
// the read-only Genesis configuration view. The chain package and its
// consumers (verifier, vm, consensus, rpc) program against these
// interfaces; the implementations live in chain/account,
// chain/account/mailbox, chain/momentum and chain/genesis.
package store

import (
	"math/big"

	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common/db"
	"github.com/zenon-network/go-zenon/common/types"
)

// Account is a view over one account chain at a specific version. The
// implementation (chain/account) wraps a db.DB that may be the
// momentum-confirmed ("stable") state, the account pool's frontier
// including unconfirmed blocks, or any version in between.
//
// Identifier returns the hash-height of the frontier account block
// (zero if the chain is empty) and Frontier the block itself.
// ByHash, ByHeight and MoreByHeight return nil blocks with a nil
// error when an entry does not exist.
//
// Storage returns the embedded-contract key-value storage: a subset
// of the account database wrapped with db.DisableNotFound, so missing
// keys read as empty values instead of leveldb.ErrNotFound — the
// behaviour embedded-contract code expects.
//
// GetChainPlasma returns the cumulative FusedPlasma consumed by the
// blocks of this account chain and AddChainPlasma increases it; the
// difference between the stable and the frontier counter is the
// plasma used by unconfirmed blocks (see vm.AvailablePlasma).
//
// MarkAsReceived records that a send block (identified by its hash)
// has been consumed by a receive block of this account; IsReceived is
// the verifier's double-receive check.
//
// SequencerFront returns the next send-block header an embedded
// contract has to receive, pairing the mailbox's FIFO queue with the
// consumed-count cursor kept in the account store, or nil when the
// queue is drained; it panics if the mailbox belongs to a different
// address. SequencerPopFront advances the cursor.
//
// Apply, Changes and Snapshot are inherited from the underlying
// db.DB: Snapshot returns a copy-on-write view of the store and
// Changes exports the writes accumulated by this instance as a
// db.Patch (memory-backed instances only).
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

// AccountMailbox tracks the send blocks addressed to an account. It
// is stored under the momentum ledger (implemented by
// chain/account/mailbox) and written only while a momentum confirms
// blocks, in Momentum.AddAccountBlockTransaction.
//
// MarkAsUnreceived records an incoming send-block hash both in a
// permanent index and in the pending set; MarkAsReceived removes the
// hash from the pending set only. GetUnreceivedAccountBlockHashes
// returns up to atMost pending hashes in ascending byte order — the
// ledger RPC scans at most 500 per call. MarkBlockThatReceives maps a
// send-block hash to the header of the receive block that consumed it
// and GetBlockWhichReceives reads the mapping back (nil if none).
//
// The Sequencer methods maintain the FIFO queue of send-block headers
// addressed to an embedded contract: SequencerPushBack appends a
// header (heights are 1-based), SequencerSize returns the number of
// headers ever pushed and SequencerByHeight reads one. The consumed
// cursor lives in the account store (see Account.SequencerFront).
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
