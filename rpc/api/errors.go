package api

import (
	"github.com/zenon-network/go-zenon/common"
)

// Parameter-validation errors shared by the JSON-RPC handlers in this
// package and in rpc/api/embedded. All of them carry JSON-RPC error
// code -32000.
var (
	// ErrPageSizeParamTooBig is returned by paginated methods when the
	// pageSize parameter exceeds the method's limit: RpcMaxPageSize
	// (1024) for most methods in this package and in rpc/api/embedded,
	// or 50 for LedgerApi.GetUnreceivedBlocksByAddress.
	ErrPageSizeParamTooBig = common.NewErrorWCode(-32000, "page-size parameter is too big")
	// ErrPageIndexParamTooBig is returned by
	// LedgerApi.GetUnreceivedBlocksByAddress when pageIndex is 10 or
	// greater. No other handler currently returns it.
	ErrPageIndexParamTooBig = common.NewErrorWCode(-32000, "page-index parameter is too big")
	// ErrCountParamTooBig is returned by the height-ranged ledger reads
	// (GetAccountBlocksByHeight, GetMomentumsByHeight and
	// GetDetailedMomentumsByHeight) when the count parameter exceeds
	// RpcMaxCountSize (1024).
	ErrCountParamTooBig = common.NewErrorWCode(-32000, "count parameter is too big")
	// ErrHeightParamIsZero is returned by the height-ranged ledger
	// reads (GetAccountBlocksByHeight and GetMomentumsByHeight, and
	// through the latter GetDetailedMomentumsByHeight) when the height
	// parameter is 0; account-chain and momentum heights are 1-based.
	ErrHeightParamIsZero = common.NewErrorWCode(-32000, "height parameter must be strictly greater than zero")
	// ErrParamIsNull is returned by LedgerApi.PublishRawTransaction
	// when the block parameter is null. No other handler currently
	// returns it.
	ErrParamIsNull = common.NewErrorWCode(-32000, "parameter must not be null")
)
