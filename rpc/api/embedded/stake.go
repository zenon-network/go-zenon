package embedded

import (
	"math/big"
	"sort"

	"github.com/inconshreveable/log15"

	"github.com/zenon-network/go-zenon/chain"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/consensus"
	"github.com/zenon-network/go-zenon/rpc/api"
	"github.com/zenon-network/go-zenon/vm/embedded/definition"
	"github.com/zenon-network/go-zenon/zenon"
)

type StakeApi struct {
	chain chain.Chain
	z     zenon.Zenon
	cs    consensus.Consensus
	log   log15.Logger
}

func NewStakeApi(z zenon.Zenon) *StakeApi {
	return &StakeApi{
		chain: z.Chain(),
		z:     z,
		cs:    z.Consensus(),
		log:   common.RPCLogger.New("module", "embedded_stake_api"),
	}
}

// === Shared RPCs ===

func (a *StakeApi) GetUncollectedReward(address types.Address) (*definition.RewardDeposit, error) {
	return getUncollectedReward(a.chain, types.StakeContract, address)
}
func (a *StakeApi) GetFrontierRewardByPage(address types.Address, pageIndex, pageSize uint32) (*RewardHistoryList, error) {
	if pageSize > api.RpcMaxPageSize {
		return nil, api.ErrPageSizeParamTooBig
	}
	return getFrontierRewardByPage(a.chain, types.StakeContract, address, pageIndex, pageSize)
}

type StakeEntry struct {
	Amount              *big.Int      `json:"amount"`
	WeightedAmount      *big.Int      `json:"weightedAmount"`
	StartTimestamp      int64         `json:"startTimestamp"`
	ExpirationTimestamp int64         `json:"expirationTimestamp"`
	Address             types.Address `json:"address"`
	Id                  types.Hash    `json:"id"`
}
type StakeList struct {
	TotalAmount         *big.Int      `json:"totalAmount"`
	TotalWeightedAmount *big.Int      `json:"totalWeightedAmount"`
	Count               int           `json:"count"`
	Entries             []*StakeEntry `json:"list"`
}

func (a *StakeApi) GetEntriesByAddress(address types.Address, pageIndex, pageSize uint32) (*StakeList, error) {
	if pageSize > api.RpcMaxPageSize {
		return nil, api.ErrPageSizeParamTooBig
	}

	_, context, err := api.GetFrontierContext(a.chain, types.StakeContract)
	if err != nil {
		return nil, err
	}
	list, total, totalWeighted, err := definition.GetStakeListByAddress(context.Storage(), address)
	if err != nil {
		return nil, err
	}

	sort.Sort(definition.StakeByExpirationTime(list))

	listLen := len(list)
	start, end := api.GetRange(pageIndex, pageSize, uint32(listLen))
	entryList := make([]*StakeEntry, end-start)
	for index, info := range list[start:end] {
		entryList[index] = &StakeEntry{
			Amount:              info.Amount,
			WeightedAmount:      info.WeightedAmount,
			StartTimestamp:      info.StartTime,
			ExpirationTimestamp: info.ExpirationTime,
			Address:             info.StakeAddress,
			Id:                  info.Id,
		}
	}

	return &StakeList{
		TotalAmount:         total,
		TotalWeightedAmount: totalWeighted,
		Count:               listLen,
		Entries:             entryList,
	}, nil
}
