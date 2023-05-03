package embedded

import (
	"github.com/inconshreveable/log15"

	"github.com/zenon-network/go-zenon/chain"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/types"
	rpcapi "github.com/zenon-network/go-zenon/rpc/api"
	"github.com/zenon-network/go-zenon/vm/embedded/definition"
	"github.com/zenon-network/go-zenon/vm/embedded/implementation"
	"github.com/zenon-network/go-zenon/zenon"
)

type SentinelApi struct {
	chain chain.Chain
	log   log15.Logger
}

type SentinelInfo struct {
	Owner                 types.Address `json:"owner"`
	RegistrationTimestamp int64         `json:"registrationTimestamp"`
	CanBeRevoked          bool          `json:"isRevocable"`
	RevokeCooldown        int64         `json:"revokeCooldown"`
	Active                bool          `json:"active"`
}
type SentinelInfoList struct {
	Count int             `json:"count"`
	List  []*SentinelInfo `json:"list"`
}

func NewSentinelApi(z zenon.Zenon) *SentinelApi {
	return &SentinelApi{
		chain: z.Chain(),
		log:   common.RPCLogger.New("module", "embedded_sentinel_api"),
	}
}

func (api *SentinelApi) toSentinelInfo(sentinel *definition.SentinelInfo) *SentinelInfo {
	m, _, err := rpcapi.GetFrontierContext(api.chain, types.SentinelContract)
	if err != nil {
		return nil
	}

	canBeRevoked, revokeCooldown := implementation.GetSentinelRevokeStatus(sentinel.RegistrationTimestamp, m)
	return &SentinelInfo{
		Owner:                 sentinel.Owner,
		RegistrationTimestamp: sentinel.RegistrationTimestamp,
		CanBeRevoked:          canBeRevoked,
		RevokeCooldown:        revokeCooldown,
		Active:                sentinel.RevokeTimestamp == 0,
	}
}

func (api *SentinelApi) GetByOwner(owner types.Address) (*SentinelInfo, error) {
	_, context, err := rpcapi.GetFrontierContext(api.chain, types.SentinelContract)
	if err != nil {
		return nil, err
	}
	sentinel := definition.GetSentinelInfoByOwner(context.Storage(), owner)
	if sentinel != nil {
		return api.toSentinelInfo(sentinel), nil
	} else {
		return nil, nil
	}
}
func (api *SentinelApi) GetAllActive(pageIndex, pageSize uint32) (*SentinelInfoList, error) {
	if pageSize > rpcapi.RpcMaxPageSize {
		return nil, rpcapi.ErrPageSizeParamTooBig
	}
	_, context, err := rpcapi.GetFrontierContext(api.chain, types.SentinelContract)
	if err != nil {
		return nil, err
	}

	rawList := definition.GetAllSentinelInfo(context.Storage())

	list := make([]*SentinelInfo, 0, len(rawList))
	for _, raw := range rawList {
		if raw.RevokeTimestamp == 0 {
			list = append(list, api.toSentinelInfo(raw))
		}
	}
	start, end := rpcapi.GetRange(pageIndex, pageSize, uint32(len(list)))

	return &SentinelInfoList{
		Count: len(list),
		List:  list[start:end],
	}, nil
}

// === Shared RPCs ===

func (api *SentinelApi) GetDepositedQsr(address types.Address) (string, error) {
	depositedQsr, err := getDepositedQsr(api.chain, types.SentinelContract, address)
	return depositedQsr.String(), err
}
func (api *SentinelApi) GetUncollectedReward(address types.Address) (*definition.RewardDeposit, error) {
	return getUncollectedReward(api.chain, types.SentinelContract, address)
}
func (api *SentinelApi) GetFrontierRewardByPage(address types.Address, pageIndex, pageSize uint32) (*RewardHistoryList, error) {
	if pageSize > rpcapi.RpcMaxPageSize {
		return nil, rpcapi.ErrPageSizeParamTooBig
	}
	return getFrontierRewardByPage(api.chain, types.SentinelContract, address, pageIndex, pageSize)
}
