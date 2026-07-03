package embedded

import (
	"encoding/json"
	"github.com/inconshreveable/log15"
	"github.com/zenon-network/go-zenon/chain"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/rpc/api"
	"github.com/zenon-network/go-zenon/vm/embedded/definition"
	"github.com/zenon-network/go-zenon/zenon"
	"math/big"
	"sort"
)

// LiquidityApi implements the "embedded.liquidity" JSON-RPC namespace,
// which reads the state of the liquidity embedded contract as of the
// frontier momentum. The contract rewards staking of
// administrator-whitelisted liquidity ZTS tokens: each token tuple
// assigns a token its share of the epoch ZNN and QSR rewards and a
// minimum stake amount, and stake entries lock tokens until an
// expiration time. Sensitive administrator methods are protected by
// guardian-backed time challenges. Every exported method is served as
// embedded.liquidity.<lowerCamelMethodName>.
type LiquidityApi struct {
	chain chain.Chain
	log   log15.Logger
}

// NewLiquidityApi returns a LiquidityApi bound to the given node's
// chain. It is called by the RPC server when the "embedded" namespace
// is enabled; it is not itself an RPC method.
func NewLiquidityApi(z zenon.Zenon) *LiquidityApi {
	return &LiquidityApi{
		chain: z.Chain(),
		log:   common.RPCLogger.New("module", "embedded_liquidity_api"),
	}
}

// GetLiquidityInfo returns the contract's global state: the
// administrator address, whether the contract is halted, the additional
// ZNN and QSR amounts (smallest units) distributed each epoch from the
// contract's own balance on top of the protocol liquidity rewards, and
// the whitelisted token tuples with their ZNN and QSR reward
// percentages and minimum stake amounts. Before the state is first
// written it reads as defaults: the initial administrator hard-coded in
// vm/constants, not halted, zero extra rewards and no token tuples.
//
// JSON-RPC: embedded.liquidity.getLiquidityInfo
func (a *LiquidityApi) GetLiquidityInfo() (*definition.LiquidityInfo, error) {
	_, context, err := api.GetFrontierContext(a.chain, types.LiquidityContract)
	if err != nil {
		return nil, err
	}

	liquidityInfo, err := definition.GetLiquidityInfo(context.Storage())
	if err != nil {
		return nil, err
	}

	return liquidityInfo, nil
}

// GetSecurityInfo returns the time-challenge security configuration of
// the liquidity contract: the guardian addresses that can vote in a new
// administrator during an emergency, their current votes, and the two
// challenge delays in momentums (AdministratorDelay for administrator
// and guardian changes, SoftDelay for the other protected methods).
// Before security is initialized it reads as the minimum delays from
// vm/constants with no guardians.
//
// JSON-RPC: embedded.liquidity.getSecurityInfo
func (a *LiquidityApi) GetSecurityInfo() (*definition.SecurityInfoVariable, error) {
	_, context, err := api.GetFrontierContext(a.chain, types.LiquidityContract)
	if err != nil {
		return nil, err
	}

	security, err := definition.GetSecurityInfoVariable(context.Storage())
	if err != nil {
		return nil, err
	}

	return security, nil
}

// LiquidityStakeList is one page of an address's active liquidity stake
// entries as reported by GetLiquidityStakeEntriesByAddress. Count,
// TotalAmount and TotalWeightedAmount cover all active entries of the
// address, not just the page; amounts are in smallest units of each
// entry's staked ZTS token.
type LiquidityStakeList struct {
	TotalAmount         *big.Int                          `json:"totalAmount"`
	TotalWeightedAmount *big.Int                          `json:"totalWeightedAmount"`
	Count               int                               `json:"count"`
	Entries             []*definition.LiquidityStakeEntry `json:"list"`
}

// LiquidityStakeListMarshal is the JSON wire form of LiquidityStakeList,
// with the total amounts rendered as base-10 strings. It exists so the
// custom MarshalJSON/UnmarshalJSON of LiquidityStakeList can round-trip
// amounts without precision loss.
type LiquidityStakeListMarshal struct {
	TotalAmount         string                            `json:"totalAmount"`
	TotalWeightedAmount string                            `json:"totalWeightedAmount"`
	Count               int                               `json:"count"`
	Entries             []*definition.LiquidityStakeEntry `json:"list"`
}

// ToLiquidityStakeListMarshal converts the list to its JSON wire
// representation, rendering the total amounts as base-10 strings.
func (stake *LiquidityStakeList) ToLiquidityStakeListMarshal() *LiquidityStakeListMarshal {
	aux := &LiquidityStakeListMarshal{
		TotalAmount:         stake.TotalAmount.String(),
		TotalWeightedAmount: stake.TotalWeightedAmount.String(),
		Count:               stake.Count,
	}
	aux.Entries = make([]*definition.LiquidityStakeEntry, len(stake.Entries))
	for idx, entry := range stake.Entries {
		aux.Entries[idx] = entry
	}
	return aux
}

// MarshalJSON encodes the list via its LiquidityStakeListMarshal wire
// form, so the total amounts appear as base-10 JSON strings.
func (stake *LiquidityStakeList) MarshalJSON() ([]byte, error) {
	return json.Marshal(stake.ToLiquidityStakeListMarshal())
}

// UnmarshalJSON decodes the LiquidityStakeListMarshal wire form
// produced by MarshalJSON. Amount strings that are not valid base-10
// integers decode to 0 rather than producing an error.
func (stake *LiquidityStakeList) UnmarshalJSON(data []byte) error {
	aux := new(LiquidityStakeListMarshal)
	if err := json.Unmarshal(data, aux); err != nil {
		return err
	}
	stake.TotalAmount = common.StringToBigInt(aux.TotalAmount)
	stake.TotalWeightedAmount = common.StringToBigInt(aux.TotalWeightedAmount)
	stake.Count = aux.Count
	stake.Entries = make([]*definition.LiquidityStakeEntry, len(aux.Entries))
	for idx, entry := range aux.Entries {
		stake.Entries[idx] = entry
	}
	return nil
}

// GetLiquidityStakeEntriesByAddress pages over the active (not yet
// revoked) liquidity stake entries of address, sorted by ascending
// expiration time (soonest-expiring first) with ties broken by
// ascending entry id. Each entry carries the staked amount and its
// weighted amount in smallest units of the staked ZTS token, unix
// second timestamps, and the entry id used to cancel it. The list
// totals sum every active entry of the address, not just the returned
// page. A pageSize above 1024 is rejected with
// api.ErrPageSizeParamTooBig.
//
// JSON-RPC: embedded.liquidity.getLiquidityStakeEntriesByAddress
func (a *LiquidityApi) GetLiquidityStakeEntriesByAddress(address types.Address, pageIndex, pageSize uint32) (*LiquidityStakeList, error) {
	if pageSize > api.RpcMaxPageSize {
		return nil, api.ErrPageSizeParamTooBig
	}

	_, context, err := api.GetFrontierContext(a.chain, types.LiquidityContract)
	if err != nil {
		return nil, err
	}
	list, total, totalWeighted, err := definition.GetLiquidityStakeListByAddress(context.Storage(), address)
	if err != nil {
		return nil, err
	}

	sort.Sort(definition.LiquidityStakeByExpirationTime(list))

	listLen := len(list)
	start, end := api.GetRange(pageIndex, pageSize, uint32(listLen))

	return &LiquidityStakeList{
		TotalAmount:         total,
		TotalWeightedAmount: totalWeighted,
		Count:               listLen,
		Entries:             list[start:end],
	}, nil
}

// GetUncollectedReward returns the liquidity rewards credited to
// address but not yet collected, read from contract state at the
// frontier momentum. Liquidity rewards can include both ZNN and QSR; an
// address with nothing to collect yields a deposit with both amounts 0,
// not an error.
//
// JSON-RPC: embedded.liquidity.getUncollectedReward
func (a *LiquidityApi) GetUncollectedReward(address types.Address) (*definition.RewardDeposit, error) {
	return getUncollectedReward(a.chain, types.LiquidityContract, address)
}

// GetFrontierRewardByPage pages over the per-epoch liquidity reward
// history of address, newest epoch first; epochs without a recorded
// reward yield zero-amount entries. A pageSize above 1024 is rejected
// with api.ErrPageSizeParamTooBig.
//
// JSON-RPC: embedded.liquidity.getFrontierRewardByPage
func (a *LiquidityApi) GetFrontierRewardByPage(address types.Address, pageIndex, pageSize uint32) (*RewardHistoryList, error) {
	if pageSize > api.RpcMaxPageSize {
		return nil, api.ErrPageSizeParamTooBig
	}
	return getFrontierRewardByPage(a.chain, types.LiquidityContract, address, pageIndex, pageSize)
}

// GetTimeChallengesInfo returns the recorded time challenges for the
// liquidity contract's protected administrator methods:
// NominateGuardians, SetTokenTuple, ChangeAdministrator and
// SetAdditionalReward. Methods that have never been challenged are
// omitted from the list.
//
// JSON-RPC: embedded.liquidity.getTimeChallengesInfo
func (a *LiquidityApi) GetTimeChallengesInfo() (*TimeChallengesList, error) {
	_, context, err := api.GetFrontierContext(a.chain, types.LiquidityContract)
	if err != nil {
		return nil, err
	}

	ans := make([]*definition.TimeChallengeInfo, 0)
	methods := []string{"NominateGuardians", "SetTokenTuple", "ChangeAdministrator", "SetAdditionalReward"}

	for _, m := range methods {
		timeC, err := definition.GetTimeChallengeInfoVariable(context.Storage(), m)
		if err != nil {
			return nil, err
		}
		if timeC != nil {
			ans = append(ans, timeC)
		}
	}

	return &TimeChallengesList{
		Count: len(ans),
		List:  ans,
	}, nil
}
