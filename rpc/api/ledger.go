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

func NewLedgerApi(z zenon.Zenon) *LedgerApi {
	api := &LedgerApi{
		z:     z,
		chain: z.Chain(),
		log:   common.RPCLogger.New("module", "ledger_api"),
	}

	return api
}

type LedgerApi struct {
	z     zenon.Zenon
	chain chain.Chain
	log   log15.Logger
}

const (
	unreceivedMaxPageIndex = 10
	unreceivedMaxPageSize  = 50
	unreceivedQuerySize    = unreceivedMaxPageIndex * unreceivedMaxPageSize
)

func (l LedgerApi) String() string {
	return "LedgerApi"
}

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

// Unconfirmed AccountBlocks
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

// AccountBlocks
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
	return ans, nil
}
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

// Momentum
func (l *LedgerApi) GetFrontierMomentum() (*Momentum, error) {
	momentum, err := l.chain.GetFrontierMomentumStore().GetFrontierMomentum()
	if err != nil {
		return nil, err
	}
	return ledgerMomentumToRpc(momentum)
}
func (l *LedgerApi) GetMomentumBeforeTime(timestamp int64) (*Momentum, error) {
	currentTime := time.Unix(timestamp, 0)
	momentum, err := l.chain.GetFrontierMomentumStore().GetMomentumBeforeTime(&currentTime)
	if err != nil || momentum == nil {
		return nil, err
	}

	return ledgerMomentumToRpc(momentum)
}
func (l *LedgerApi) GetMomentumByHash(hash types.Hash) (*Momentum, error) {
	block, err := l.chain.GetFrontierMomentumStore().GetMomentumByHash(hash)
	if err != nil {
		l.log.Error("GetMomentumByHash failed, error is "+err.Error(), "method", "GetMomentumByHash")
		return nil, err
	}
	return ledgerMomentumToRpc(block)
}
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
