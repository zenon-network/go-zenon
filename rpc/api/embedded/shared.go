package embedded

import (
	"encoding/json"
	"github.com/zenon-network/go-zenon/common"
	"math/big"

	"github.com/zenon-network/go-zenon/chain"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/rpc/api"
	"github.com/zenon-network/go-zenon/vm/embedded/definition"
)

func getDepositedQsr(chain chain.Chain, contract types.Address, address types.Address) (*big.Int, error) {
	_, context, err := api.GetFrontierContext(chain, contract)
	if err != nil {
		return nil, err
	}
	qsrDeposit, err := definition.GetQsrDeposit(context.Storage(), &address)
	if err != nil {
		return nil, err
	} else {
		return qsrDeposit.Qsr, nil
	}
}
func getUncollectedReward(chain chain.Chain, contract types.Address, address types.Address) (*definition.RewardDeposit, error) {
	_, context, err := api.GetFrontierContext(chain, contract)
	if err != nil {
		return nil, err
	}
	return definition.GetRewardDeposit(context.Storage(), &address)
}

type RewardHistoryEntry struct {
	Epoch int64    `json:"epoch"`
	Znn   *big.Int `json:"znnAmount"`
	Qsr   *big.Int `json:"qsrAmount"`
}

type RewardHistoryEntryMarshal struct {
	Epoch int64  `json:"epoch"`
	Znn   string `json:"znnAmount"`
	Qsr   string `json:"qsrAmount"`
}

func (r *RewardHistoryEntry) ToRewardDepositMarshal() *RewardHistoryEntryMarshal {
	aux := &RewardHistoryEntryMarshal{
		Epoch: r.Epoch,
		Znn:   r.Znn.String(),
		Qsr:   r.Qsr.String(),
	}

	return aux
}

func (r *RewardHistoryEntry) MarshalJSON() ([]byte, error) {
	return json.Marshal(r.ToRewardDepositMarshal())
}

func (r *RewardHistoryEntry) UnmarshalJSON(data []byte) error {
	aux := new(RewardHistoryEntryMarshal)
	if err := json.Unmarshal(data, aux); err != nil {
		return err
	}
	r.Epoch = aux.Epoch
	r.Znn = common.StringToBigInt(aux.Znn)
	r.Qsr = common.StringToBigInt(aux.Qsr)
	return nil
}

type RewardHistoryList struct {
	Count int64                 `json:"count"`
	List  []*RewardHistoryEntry `json:"list"`
}

func getFrontierRewardByPage(chain chain.Chain, contract types.Address, address types.Address, pageIndex, pageSize uint32) (*RewardHistoryList, error) {
	if pageSize > api.RpcMaxPageSize {
		return nil, api.ErrPageSizeParamTooBig
	}

	_, context, err := api.GetFrontierContext(chain, contract)
	if err != nil {
		return nil, err
	}

	// get latest epoch
	lastEpoch, err := definition.GetLastEpochUpdate(context.Storage())
	if err != nil {
		return nil, err
	}

	epoch := lastEpoch.LastEpoch - int64(pageIndex*pageSize)

	result := &RewardHistoryList{
		Count: lastEpoch.LastEpoch + 1,
		List:  make([]*RewardHistoryEntry, 0, pageSize),
	}
	for i := 0; i < int(pageSize); i += 1 {
		if epoch < 0 {
			break
		}
		if d, err := definition.GetRewardDepositHistory(context.Storage(), uint64(epoch), &address); err == nil {
			result.List = append(result.List, &RewardHistoryEntry{
				Epoch: epoch,
				Znn:   (new(big.Int)).Set(d.Znn),
				Qsr:   (new(big.Int)).Set(d.Qsr),
			})
		} else {
			return nil, err
		}
		epoch -= 1
	}

	return result, err
}
