// Package api exposes the public JSON-RPC handlers of a Zenon node: the
// ledger namespace (account blocks, momentums, account info), the stats
// namespace (node/network status), and shared wire types. Handlers are
// registered by the node's RPC server; each exported method on an API
// struct is served as <namespace>.<lowerCamelMethodName>.
package api

import (
	"time"

	"github.com/inconshreveable/log15"
	"github.com/pkg/errors"

	"github.com/zenon-network/go-zenon/chain"
	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/vm"
	"github.com/zenon-network/go-zenon/zenon"
)

// NewLedgerApi returns a LedgerApi bound to the given node's chain,
// consensus and broadcaster. It is called by the RPC server when the
// "ledger" namespace is enabled; it is not itself an RPC method.
func NewLedgerApi(z zenon.Zenon) *LedgerApi {
	api := &LedgerApi{
		z:     z,
		chain: z.Chain(),
		log:   common.RPCLogger.New("module", "ledger_api"),
	}

	return api
}

// LedgerApi implements the "ledger" JSON-RPC namespace, which reads the
// dual ledger (per-account chains of account blocks and the momentum
// chain that confirms them) and accepts signed account blocks for
// insertion. Every exported method is served as
// ledger.<lowerCamelMethodName>.
//
// All read methods operate on the frontier (most recent) state of the
// chain at the time of the call.
type LedgerApi struct {
	z     zenon.Zenon
	chain chain.Chain
	log   log15.Logger
}

// Limits specific to GetUnreceivedBlocksByAddress: at most
// unreceivedQuerySize (500) pending hashes are scanned per call, browsable
// as pages 0..9 of at most 50 entries each.
const (
	unreceivedMaxPageIndex = 10
	unreceivedMaxPageSize  = 50
	unreceivedQuerySize    = unreceivedMaxPageIndex * unreceivedMaxPageSize
)

// String returns the constant identifier "LedgerApi", which exists as a
// log/name tag for the API object. Because the reflection-based RPC
// server registers every exported method with a suitable callback
// signature, it is nevertheless exposed over RPC as ledger.string.
func (l LedgerApi) String() string {
	return "LedgerApi"
}

// PublishRawTransaction submits a fully signed account block to the
// node. The block is applied through the VM supervisor against the
// frontier state and, on success, handed to the broadcaster for network
// propagation; nil is returned in that case.
//
// It returns ErrParamIsNull for a nil block, an error if the block's
// ChainIdentifier is non-zero and differs from the node's network id
// (zero is accepted as "unset"), an error if the block's TokenStandard
// is non-zero and unknown to the chain, and any validation error raised
// while applying the block.
//
// JSON-RPC: ledger.publishRawTransaction
func (l *LedgerApi) PublishRawTransaction(block *AccountBlock) error {
	defer common.RecoverStack()
	if block == nil {
		return ErrParamIsNull
	}

	if block.ChainIdentifier != 0 && block.ChainIdentifier != l.chain.ChainIdentifier() {
		return errors.Errorf("the block has a different network Id (%d) from the node (%d)", block.ChainIdentifier, l.chain.ChainIdentifier())
	}

	lb, err := block.ToLedgerBlock()
	if err != nil {
		return err
	}
	if err := checkTokenIdValid(l.chain, &lb.TokenStandard); err != nil {
		return err
	}
	m, err := l.chain.GetFrontierMomentumStore().GetFrontierMomentum()
	if m == nil {
		return errors.New("failed to get latest momentum")
	}

	supervisor := vm.NewSupervisor(l.z.Chain(), l.z.Consensus())
	transaction, err := supervisor.ApplyBlock(lb)

	if err != nil {
		return err
	}

	l.z.Broadcaster().CreateAccountBlock(transaction)
	return nil
}

// GetUnconfirmedBlocksByAddress returns account blocks of the given
// address that the node has accepted but that are not yet committed in a
// momentum.
//
// pageIndex is 0-based; pageSize must be at most RpcMaxPageSize (1024)
// or ErrPageSizeParamTooBig is returned. The page window is clamped to
// the available blocks, so a page past the end yields an empty List. In
// the result, Count is the total number of unconfirmed blocks for the
// address (not the page length) and More is always false.
//
// JSON-RPC: ledger.getUnconfirmedBlocksByAddress
func (l *LedgerApi) GetUnconfirmedBlocksByAddress(address types.Address, pageIndex, pageSize uint32) (*AccountBlockList, error) {
	if pageSize > RpcMaxPageSize {
		return nil, ErrPageSizeParamTooBig
	}

	unreceived := l.chain.GetUncommittedAccountBlocksByAddress(address)
	start, end := GetRange(pageIndex, pageSize, uint32(len(unreceived)))
	a, err := ledgerAccountBlocksToRpc(l.chain, unreceived[start:end])

	if err != nil {
		return nil, err
	}

	return &AccountBlockList{
		List:  a,
		Count: len(unreceived),
		More:  false,
	}, nil
}

// GetFrontierAccountBlock returns the most recent committed block of
// the given address's account chain, i.e. the block with the highest
// account-chain height. It returns (nil, nil) if the account chain has
// no blocks.
//
// JSON-RPC: ledger.getFrontierAccountBlock
func (l *LedgerApi) GetFrontierAccountBlock(address types.Address) (*AccountBlock, error) {
	accountStore := l.chain.GetFrontierAccountStore(address)
	block, err := accountStore.Frontier()
	if err != nil {
		return nil, err
	}
	if block == nil {
		return nil, nil
	}
	return ledgerAccountBlockToRpc(l.chain, block)
}

// GetAccountBlockByHash returns the committed account block with the
// given hash, enriched with token, confirmation and paired-block
// details. It returns (nil, nil) if no block with that hash exists.
//
// JSON-RPC: ledger.getAccountBlockByHash
func (l *LedgerApi) GetAccountBlockByHash(blockHash types.Hash) (*AccountBlock, error) {
	momentumStore := l.chain.GetFrontierMomentumStore()
	block, err := momentumStore.GetAccountBlockByHash(blockHash)
	if err != nil {
		l.log.Error("GetAccountBlockByHash failed", "reason", err, "method-called", "momentumStore.GetAccountBlockByHash")
		return nil, err
	}
	if block == nil {
		return nil, nil
	}

	return ledgerAccountBlockToRpc(l.chain, block)
}

// GetAccountBlocksByHeight returns up to count committed blocks of the
// given address's account chain, starting at the given account-chain
// height and walking towards the frontier in ascending height order.
// Heights past the frontier are skipped, so List may hold fewer than
// count entries.
//
// height must be at least 1 (the first block of every account chain)
// or ErrHeightParamIsZero is returned; count is a maximum and must not
// exceed RpcMaxCountSize (1024) or ErrCountParamTooBig is returned. In
// the result, Count is the total number of blocks in the account chain
// (the frontier height), not the page length; an account with no blocks
// yields an empty List with Count 0.
//
// JSON-RPC: ledger.getAccountBlocksByHeight
func (l *LedgerApi) GetAccountBlocksByHeight(address types.Address, height, count uint64) (*AccountBlockList, error) {
	if height == 0 {
		return nil, ErrHeightParamIsZero
	}
	if count > RpcMaxCountSize {
		return nil, ErrCountParamTooBig
	}

	accountStore := l.chain.GetFrontierAccountStore(address)
	frontier, err := accountStore.Frontier()
	if err != nil {
		l.log.Error("GetAccountBlocksByHeight failed", "reason", err, "method-called", "accountStore.Frontier")
		return nil, err
	}
	if frontier == nil {
		return &AccountBlockList{
			List:  make([]*AccountBlock, 0),
			Count: 0,
		}, nil
	}

	accountBlocks, err := accountStore.MoreByHeight(height, count)
	if err != nil {
		l.log.Error("GetAccountBlocksByHeight failed", "reason", err, "method-called", "GetAccountBlocksByHeight")
		return nil, err
	}

	list, err := ledgerAccountBlocksToRpc(l.chain, accountBlocks)
	if err != nil {
		l.log.Error("GetAccountBlocksByHeight failed", "reason", err, "method-called", "ledgerAccountBlocksToRpc")
		return nil, err
	}

	return &AccountBlockList{
		List:  list,
		Count: int(frontier.Height),
	}, nil
}

// GetAccountBlocksByPage returns one page of the given address's
// account chain, paginating backwards from the frontier: pageIndex 0
// holds the pageSize most recent blocks, pageIndex 1 the pageSize
// before those, and so on. Within a page, blocks are ordered by
// descending height (newest first).
//
// pageIndex is 0-based; pageSize must be at most RpcMaxPageSize (1024)
// or ErrPageSizeParamTooBig is returned. Pages that extend past the
// start of the chain are truncated, and a page entirely before block 1
// yields an empty List. In the result, Count is the total number of
// blocks in the account chain and More reports whether older blocks
// exist beyond the returned page.
//
// JSON-RPC: ledger.getAccountBlocksByPage
func (l *LedgerApi) GetAccountBlocksByPage(address types.Address, pageIndex, pageSize uint32) (*AccountBlockList, error) {
	if pageSize > RpcMaxPageSize {
		return nil, ErrPageSizeParamTooBig
	}

	accountStore := l.chain.GetFrontierAccountStore(address)
	frontier, err := accountStore.Frontier()
	if err != nil {
		l.log.Error("GetAccountBlocksByHeight failed", "reason", err, "method-called", "accountStore.Frontier")
		return nil, err
	}
	if frontier == nil {
		return &AccountBlockList{
			List:  make([]*AccountBlock, 0),
			Count: 0,
		}, nil
	}

	startHeight := int64(frontier.Height) - int64(pageIndex+1)*int64(pageSize) + 1
	count := int64(pageSize)
	tooMuch := 1 - startHeight
	if tooMuch > 0 {
		startHeight = 1
		count -= tooMuch
	}
	if count < 1 {
		return &AccountBlockList{
			Count: int(frontier.Height),
			More:  false,
			List:  []*AccountBlock{},
		}, nil
	}

	ans, err := l.GetAccountBlocksByHeight(address, uint64(startHeight), uint64(count))
	if err != nil {
		return nil, err
	}

	for i, j := 0, len(ans.List)-1; i < j; i, j = i+1, j-1 {
		ans.List[i], ans.List[j] = ans.List[j], ans.List[i]
	}

	// Set More to true if there are more pages available (startHeight > 1 means we haven't reached the first block)
	ans.More = startHeight > 1

	return ans, nil
}

// GetAccountInfoByAddress returns a summary of the given account: its
// address, the height of its account chain (0 if the account has no
// blocks) and a map from token standard to balance. Balances are raw
// base units (serialized as JSON strings); tokens for which no token
// info exists on chain are omitted from the map.
//
// JSON-RPC: ledger.getAccountInfoByAddress
func (l *LedgerApi) GetAccountInfoByAddress(address types.Address) (*AccountInfo, error) {
	l.log.Info("GetAccountInfoByAddress")

	momentumStore := l.chain.GetFrontierMomentumStore()
	accountStore := l.chain.GetFrontierAccountStore(address)
	frontierAccountBlock, err := accountStore.Frontier()
	if err != nil {
		l.log.Error("GetFrontierAccountBlock failed, error is "+err.Error(), "method", "GetAccountInfoByAddress")
		return nil, err
	}

	totalNum := uint64(0)
	if frontierAccountBlock != nil {
		totalNum = frontierAccountBlock.Height
	}

	balanceMap, err := accountStore.GetBalanceMap()
	if err != nil {
		l.log.Error("GetAccountBalance failed, error is "+err.Error(), "method", "GetAccountInfoByAddress")
		return nil, err
	}

	balanceInfoMap := make(map[types.ZenonTokenStandard]*BalanceInfo)

	for zts, balance := range balanceMap {
		tokenInfo, _ := momentumStore.GetTokenInfoByTs(zts)
		if tokenInfo == nil {
			continue
		}

		balanceInfoMap[zts] = &BalanceInfo{
			TokenInfo: LedgerTokenInfoToRpc(tokenInfo),
			Balance:   balance,
		}
	}

	return &AccountInfo{
		Address:        address,
		AccountHeight:  totalNum,
		BalanceInfoMap: balanceInfoMap,
	}, nil
}

// GetUnreceivedBlocksByAddress returns committed send blocks addressed
// to the given account for which the account has not yet generated a
// receive block.
//
// This method uses stricter limits than the other paginated calls:
// pageSize must be at most 50 (ErrPageSizeParamTooBig) and pageIndex,
// 0-based, must be less than 10 (ErrPageIndexParamTooBig). Each call
// scans at most 500 pending hashes; in the result, Count is the number
// of unreceived blocks found within that window and More is true when
// the window was full, meaning additional unreceived blocks may exist
// beyond it.
//
// JSON-RPC: ledger.getUnreceivedBlocksByAddress
func (l *LedgerApi) GetUnreceivedBlocksByAddress(address types.Address, pageIndex, pageSize uint32) (*AccountBlockList, error) {
	l.log.Info("GetUnreceivedBlocksByAddress", "address", address, "page", pageIndex, "size", pageSize)
	if pageSize > unreceivedMaxPageSize {
		return nil, ErrPageSizeParamTooBig
	}
	if pageIndex >= unreceivedMaxPageIndex {
		return nil, ErrPageIndexParamTooBig
	}

	accountStore := l.chain.GetFrontierAccountStore(address)
	hashList, err := l.chain.GetFrontierMomentumStore().GetAccountMailbox(address).GetUnreceivedAccountBlockHashes(unreceivedQuerySize)
	if err != nil {
		return nil, err
	}

	ledgerFrontier := l.chain.GetFrontierMomentumStore()
	blockList := make([]*nom.AccountBlock, 0, len(hashList))
	for _, hash := range hashList {
		if accountStore.IsReceived(hash) {
			continue
		}
		block, err := ledgerFrontier.GetAccountBlockByHash(hash)

		if err != nil {
			return nil, err
		}
		blockList = append(blockList, block)
	}

	// Check if there are 100% more blocks that could've been returned
	isMore := false
	if len(hashList) == unreceivedQuerySize {
		isMore = true
	}

	start, end := GetRange(pageIndex, pageSize, uint32(len(blockList)))
	a, err := ledgerAccountBlocksToRpc(l.chain, blockList[start:end])

	if err != nil {
		return nil, err
	}

	return &AccountBlockList{
		List:  a,
		Count: len(blockList),
		More:  isMore,
	}, nil
}

// GetFrontierMomentum returns the momentum at the head of the momentum
// chain, i.e. the most recently inserted momentum.
//
// JSON-RPC: ledger.getFrontierMomentum
func (l *LedgerApi) GetFrontierMomentum() (*Momentum, error) {
	momentum, err := l.chain.GetFrontierMomentumStore().GetFrontierMomentum()
	if err != nil {
		return nil, err
	}
	return ledgerMomentumToRpc(momentum)
}

// GetMomentumBeforeTime returns the most recent momentum whose
// timestamp is strictly before the given Unix timestamp (seconds). It
// returns (nil, nil) when no such momentum exists, i.e. when the
// genesis momentum's timestamp is at or after the given time.
//
// JSON-RPC: ledger.getMomentumBeforeTime
func (l *LedgerApi) GetMomentumBeforeTime(timestamp int64) (*Momentum, error) {
	currentTime := time.Unix(timestamp, 0)
	momentum, err := l.chain.GetFrontierMomentumStore().GetMomentumBeforeTime(&currentTime)
	if err != nil || momentum == nil {
		return nil, err
	}

	return ledgerMomentumToRpc(momentum)
}

// GetMomentumByHash returns the momentum with the given hash. It
// returns (nil, nil) if no momentum with that hash exists.
//
// JSON-RPC: ledger.getMomentumByHash
func (l *LedgerApi) GetMomentumByHash(hash types.Hash) (*Momentum, error) {
	block, err := l.chain.GetFrontierMomentumStore().GetMomentumByHash(hash)
	if err != nil {
		l.log.Error("GetMomentumByHash failed, error is "+err.Error(), "method", "GetMomentumByHash")
		return nil, err
	}
	return ledgerMomentumToRpc(block)
}

// GetMomentumsByHeight returns up to count momentums starting at the
// given momentum-chain height, in ascending height order. Heights past
// the frontier are skipped, so List may hold fewer than count entries.
//
// height must be at least 1 (the genesis momentum) or
// ErrHeightParamIsZero is returned; count is a maximum and must not
// exceed RpcMaxCountSize (1024) or ErrCountParamTooBig is returned. In
// the result, Count is the current frontier momentum height, not the
// page length.
//
// JSON-RPC: ledger.getMomentumsByHeight
func (l *LedgerApi) GetMomentumsByHeight(height, count uint64) (*MomentumList, error) {
	if height == 0 {
		return nil, ErrHeightParamIsZero
	}
	if count > RpcMaxCountSize {
		return nil, ErrCountParamTooBig
	}

	momentumStore := l.chain.GetFrontierMomentumStore()
	frontier, err := momentumStore.GetFrontierMomentum()
	if err != nil {
		l.log.Error("GetMomentumsByHeight failed", "reason", err, "method-called", "momentumStore.GetFrontierMomentum")
		return nil, err
	}

	momentums, err := momentumStore.GetMomentumsByHeight(height, true, count)
	if err != nil {
		l.log.Error("GetMomentumsByHeight failed", "reason", err, "method-called", "momentumStore.GetMomentumsByHeight")
		return nil, err
	}

	list, err := ledgerMomentumsToRpc(momentums)
	if err != nil {
		l.log.Error("GetMomentumsByHeight failed", "reason", err, "method-called", "ledgerMomentumsToRpc")
		return nil, err
	}

	return &MomentumList{
		List:  list,
		Count: int(frontier.Height),
	}, nil
}

// GetMomentumsByPage returns one page of the momentum chain,
// paginating backwards from the frontier: pageIndex 0 holds the
// pageSize most recent momentums, pageIndex 1 the pageSize before
// those, and so on. Within a page, momentums are ordered by descending
// height (newest first).
//
// pageIndex is 0-based; pageSize must be at most RpcMaxPageSize (1024)
// or ErrPageSizeParamTooBig is returned. Pages that extend past the
// genesis momentum are truncated, and a page entirely before height 1
// yields an empty List. In the result, Count is the current frontier
// momentum height.
//
// JSON-RPC: ledger.getMomentumsByPage
func (l *LedgerApi) GetMomentumsByPage(pageIndex, pageSize uint32) (*MomentumList, error) {
	if pageSize > RpcMaxPageSize {
		return nil, ErrPageSizeParamTooBig
	}

	momentumStore := l.chain.GetFrontierMomentumStore()
	frontier, err := momentumStore.GetFrontierMomentum()
	if err != nil {
		l.log.Error("GetMomentumsByPage failed", "reason", err, "method-called", "momentumStore.GetFrontierMomentum")
		return nil, err
	}

	startHeight := int64(frontier.Height) - int64(pageIndex+1)*int64(pageSize) + 1
	count := int64(pageSize)
	tooMuch := 1 - startHeight
	if tooMuch > 0 {
		startHeight = 1
		count -= tooMuch
	}
	if count < 1 {
		return &MomentumList{
			Count: int(frontier.Height),
			List:  []*Momentum{},
		}, nil
	}

	ans, err := l.GetMomentumsByHeight(uint64(startHeight), uint64(count))
	if err != nil {
		return nil, err
	}

	for i, j := 0, len(ans.List)-1; i < j; i, j = i+1, j-1 {
		ans.List[i], ans.List[j] = ans.List[j], ans.List[i]
	}
	return ans, nil
}

// GetDetailedMomentumsByHeight behaves like GetMomentumsByHeight (same
// parameters, limits, ascending order and Count semantics) but pairs
// each momentum with the full account blocks it confirms instead of
// only their references.
//
// JSON-RPC: ledger.getDetailedMomentumsByHeight
func (l *LedgerApi) GetDetailedMomentumsByHeight(height, count uint64) (*DetailedMomentumList, error) {
	l.log.Info("GetDetailedMomentumsByHeight", "height", height, "count", count)
	if count > RpcMaxCountSize {
		return nil, ErrCountParamTooBig
	}

	ans, err := l.GetMomentumsByHeight(height, count)
	if err != nil {
		return nil, err
	}
	return momentumListToDetailedList(l.chain, ans)
}
