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

// SentinelApi implements the "embedded.sentinel" JSON-RPC namespace,
// which reads the state of the sentinel embedded contract
// (registrations, deposits, rewards) as of the frontier momentum.
// Every exported method is served as
// embedded.sentinel.<lowerCamelMethodName>.
type SentinelApi struct {
	chain chain.Chain
	log   log15.Logger
}

// SentinelInfo describes one registered sentinel. Owner,
// RegistrationTimestamp (unix seconds) and the revocation state come
// from contract state at the frontier momentum; Active reports whether
// the sentinel's RevokeTimestamp is still 0.
//
// CanBeRevoked and RevokeCooldown are computed from the registration
// timestamp and the frontier momentum timestamp: a sentinel's lifetime
// cycles through a 27-day locked window followed by a 3-day revocable
// window. CanBeRevoked reports whether the sentinel is currently in the
// revocable window; RevokeCooldown is the number of seconds until that
// state flips (until revocation opens while locked, until it closes
// while revocable).
type SentinelInfo struct {
	Owner                 types.Address `json:"owner"`
	RegistrationTimestamp int64         `json:"registrationTimestamp"`
	CanBeRevoked          bool          `json:"isRevocable"`
	RevokeCooldown        int64         `json:"revokeCooldown"`
	Active                bool          `json:"active"`
}

// SentinelInfoList is one page of sentinels as returned by
// GetAllActive. Count is the total number of active sentinels, not the
// number of entries in List.
type SentinelInfoList struct {
	Count int             `json:"count"`
	List  []*SentinelInfo `json:"list"`
}

// NewSentinelApi returns a SentinelApi bound to the given node's chain.
// It is called by the RPC server when the "embedded" namespace is
// enabled; it is not itself an RPC method.
func NewSentinelApi(z zenon.Zenon) *SentinelApi {
	return &SentinelApi{
		chain: z.Chain(),
		log:   common.RPCLogger.New("module", "embedded_sentinel_api"),
	}
}

// toSentinelInfo converts a contract-state sentinel record into the
// RPC response shape, computing the revocation window against the
// frontier momentum timestamp. It returns nil when the frontier
// context cannot be obtained; the error is swallowed.
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

// GetByOwner returns the sentinel registered by owner, revoked or not,
// read from contract state at the frontier momentum, or nil without an
// error when owner has none.
//
// JSON-RPC: embedded.sentinel.getByOwner
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

// GetAllActive returns one page of the active (non-revoked) sentinels,
// read from contract state at the frontier momentum; revoked sentinels
// are filtered out before pagination, so Count is the total number of
// active ones. A pageSize above 1024 is rejected with
// rpcapi.ErrPageSizeParamTooBig.
//
// JSON-RPC: embedded.sentinel.getAllActive
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

// GetDepositedQsr returns the QSR address has deposited in the
// sentinel contract toward a future sentinel registration, read from
// contract state at the frontier momentum. The amount is a base-10
// string in smallest units; an address with no deposit yields "0".
//
// JSON-RPC: embedded.sentinel.getDepositedQsr
func (api *SentinelApi) GetDepositedQsr(address types.Address) (string, error) {
	depositedQsr, err := getDepositedQsr(api.chain, types.SentinelContract, address)
	return depositedQsr.String(), err
}

// GetUncollectedReward returns the ZNN and QSR sentinel rewards
// credited to address but not yet collected, read from contract state
// at the frontier momentum. An address with nothing to collect yields a
// deposit with both amounts 0, not an error.
//
// JSON-RPC: embedded.sentinel.getUncollectedReward
func (api *SentinelApi) GetUncollectedReward(address types.Address) (*definition.RewardDeposit, error) {
	return getUncollectedReward(api.chain, types.SentinelContract, address)
}

// GetFrontierRewardByPage pages over the per-epoch sentinel reward
// history of address, newest epoch first; epochs without a recorded
// reward yield zero-amount entries. A pageSize above 1024 is rejected
// with rpcapi.ErrPageSizeParamTooBig.
//
// JSON-RPC: embedded.sentinel.getFrontierRewardByPage
func (api *SentinelApi) GetFrontierRewardByPage(address types.Address, pageIndex, pageSize uint32) (*RewardHistoryList, error) {
	if pageSize > rpcapi.RpcMaxPageSize {
		return nil, rpcapi.ErrPageSizeParamTooBig
	}
	return getFrontierRewardByPage(api.chain, types.SentinelContract, address, pageIndex, pageSize)
}
