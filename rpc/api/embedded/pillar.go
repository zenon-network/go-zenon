package embedded

import (
	"encoding/json"
	"math/big"
	"sort"

	"github.com/inconshreveable/log15"

	"github.com/zenon-network/go-zenon/chain"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/rpc/api"
	"github.com/zenon-network/go-zenon/vm/constants"
	"github.com/zenon-network/go-zenon/vm/embedded/definition"
	"github.com/zenon-network/go-zenon/vm/embedded/implementation"
	"github.com/zenon-network/go-zenon/zenon"
)

// PillarApi implements the "embedded.pillar" JSON-RPC namespace, which
// reads the state of the pillar embedded contract (registrations,
// delegations, deposits, rewards) as of the frontier momentum. Voting
// weights and momentum-production statistics come from a consensus
// cache that is refreshed asynchronously at most every five minutes, so
// they may lag contract state slightly. Every exported method is served
// as embedded.pillar.<lowerCamelMethodName>.
type PillarApi struct {
	log            log15.Logger
	chain          chain.Chain
	consensusCache ConsensusCache
}

// NewPillarApi returns a PillarApi bound to the given node's chain and
// consensus. With testing true the consensus cache is recomputed
// synchronously on every call instead of being refreshed in the
// background. It is called by the RPC server when the "embedded"
// namespace is enabled; it is not itself an RPC method.
func NewPillarApi(z zenon.Zenon, testing bool) *PillarApi {
	return &PillarApi{
		log:            common.RPCLogger.New("module", "embedded_pillar_api"),
		chain:          z.Chain(),
		consensusCache: NewConsensusCache(z, testing),
	}
}

// Status codes reported in GetDelegatedPillarResponse.NodeStatus:
// PillarActive when the delegated pillar exists in contract state with
// RevokeTime 0, PillarInActive when it has been revoked or its
// registration no longer exists.
var (
	PillarActive   uint8 = 1
	PillarInActive uint8 = 2
)

// === Shared RPCs ===

// GetDepositedQsr returns the QSR address has deposited in the pillar
// contract toward a future pillar registration, read from contract
// state at the frontier momentum. The amount is a base-10 string in
// smallest units; an address with no deposit yields "0".
//
// JSON-RPC: embedded.pillar.getDepositedQsr
func (a *PillarApi) GetDepositedQsr(address types.Address) (string, error) {
	depositedQsr, err := getDepositedQsr(a.chain, types.PillarContract, address)
	return depositedQsr.String(), err
}

// GetUncollectedReward returns the ZNN and QSR pillar rewards credited
// to address but not yet collected, read from contract state at the
// frontier momentum. An address with nothing to collect yields a
// deposit with both amounts 0, not an error.
//
// JSON-RPC: embedded.pillar.getUncollectedReward
func (a *PillarApi) GetUncollectedReward(address types.Address) (*definition.RewardDeposit, error) {
	return getUncollectedReward(a.chain, types.PillarContract, address)
}

// GetFrontierRewardByPage pages over the per-epoch pillar reward
// history of address, newest epoch first; epochs without a recorded
// reward yield zero-amount entries. A pageSize above 1024 is rejected
// with api.ErrPageSizeParamTooBig.
//
// JSON-RPC: embedded.pillar.getFrontierRewardByPage
func (a *PillarApi) GetFrontierRewardByPage(address types.Address, pageIndex, pageSize uint32) (*RewardHistoryList, error) {
	if pageSize > api.RpcMaxPageSize {
		return nil, api.ErrPageSizeParamTooBig
	}
	return getFrontierRewardByPage(a.chain, types.PillarContract, address, pageIndex, pageSize)
}

// GetQsrRegistrationCost returns the QSR deposit currently required to
// register a new pillar. It is computed on the fly, not read from a
// stored value: the base amount (150,000 QSR) plus the per-pillar
// increase (10,000 QSR) for each active non-legacy pillar registered at
// the frontier momentum. The amount is a base-10 string in smallest
// units.
//
// JSON-RPC: embedded.pillar.getQsrRegistrationCost
func (a *PillarApi) GetQsrRegistrationCost() (string, error) {
	_, context, err := api.GetFrontierContext(a.chain, types.PillarContract)
	if err != nil {
		return "", err
	}

	currentQsrCost, err := implementation.GetQsrCostForNextPillar(context)
	if err != nil {
		return "", err
	}

	return currentQsrCost.String(), nil
}

// PillarInfo describes one registered pillar as reported by GetAll and
// its derived methods. Name, the three addresses, the reward-sharing
// percentages and RevokeTime (unix seconds of revocation, 0 while not
// revoked) come from contract state at the frontier momentum. Type is 1
// for legacy pillars and 2 for regular ones.
//
// CanBeRevoked and RevokeCooldown are computed from the registration
// time and the frontier momentum timestamp: a pillar's lifetime cycles
// through an 83-day locked window followed by a 7-day revocable window.
// CanBeRevoked reports whether the pillar is currently in the revocable
// window; RevokeCooldown is the number of seconds until that state
// flips (until revocation opens while locked, until it closes while
// revocable).
//
// Rank, Weight and CurrentStats come from the consensus cache: Rank is
// the 0-based position after sorting by descending weight, Weight is
// the pillar's voting weight in producer elections, and CurrentStats
// counts momentums in the current epoch. While the cache is cold they
// remain at their zero placeholders.
type PillarInfo struct {
	Name string `json:"name"`
	Rank int    `json:"rank"`
	Type uint8  `json:"type"`

	StakeAddress          types.Address `json:"ownerAddress"`
	BlockProducingAddress types.Address `json:"producerAddress"`
	RewardWithdrawAddress types.Address `json:"withdrawAddress"`

	CanBeRevoked   bool  `json:"isRevocable"`
	RevokeCooldown int64 `json:"revokeCooldown"`
	RevokeTime     int64 `json:"revokeTimestamp"`

	GiveMomentumRewardPercentage uint8 `json:"giveMomentumRewardPercentage"`
	GiveDelegateRewardPercentage uint8 `json:"giveDelegateRewardPercentage"`

	CurrentStats *PillarStats `json:"currentStats"`
	Weight       *big.Int     `json:"weight"`
}

// PillarInfoMarshal is the JSON wire form of PillarInfo, with Weight
// rendered as a base-10 string. It exists so the custom
// MarshalJSON/UnmarshalJSON of PillarInfo can round-trip the weight
// without precision loss.
type PillarInfoMarshal struct {
	Name string `json:"name"`
	Rank int    `json:"rank"`
	Type uint8  `json:"type"`

	StakeAddress          types.Address `json:"ownerAddress"`
	BlockProducingAddress types.Address `json:"producerAddress"`
	RewardWithdrawAddress types.Address `json:"withdrawAddress"`

	CanBeRevoked   bool  `json:"isRevocable"`
	RevokeCooldown int64 `json:"revokeCooldown"`
	RevokeTime     int64 `json:"revokeTimestamp"`

	GiveMomentumRewardPercentage uint8 `json:"giveMomentumRewardPercentage"`
	GiveDelegateRewardPercentage uint8 `json:"giveDelegateRewardPercentage"`

	CurrentStats *PillarStats `json:"currentStats"`
	Weight       string       `json:"weight"`
}

// ToPillarInfoMarshal converts the pillar info to its JSON wire
// representation, rendering Weight as a base-10 string.
func (p *PillarInfo) ToPillarInfoMarshal() *PillarInfoMarshal {
	aux := &PillarInfoMarshal{
		Name:                         p.Name,
		Rank:                         p.Rank,
		Type:                         p.Type,
		StakeAddress:                 p.StakeAddress,
		BlockProducingAddress:        p.BlockProducingAddress,
		RewardWithdrawAddress:        p.RewardWithdrawAddress,
		CanBeRevoked:                 p.CanBeRevoked,
		RevokeCooldown:               p.RevokeCooldown,
		RevokeTime:                   p.RevokeTime,
		GiveMomentumRewardPercentage: p.GiveMomentumRewardPercentage,
		GiveDelegateRewardPercentage: p.GiveDelegateRewardPercentage,
		CurrentStats:                 p.CurrentStats,
		Weight:                       p.Weight.String(),
	}

	return aux
}

// MarshalJSON encodes the pillar info via its PillarInfoMarshal wire
// form, so Weight appears as a base-10 JSON string.
func (p *PillarInfo) MarshalJSON() ([]byte, error) {
	return json.Marshal(p.ToPillarInfoMarshal())
}

// UnmarshalJSON decodes the PillarInfoMarshal wire form produced by
// MarshalJSON. A weight string that is not a valid base-10 integer
// decodes to 0 rather than producing an error.
func (p *PillarInfo) UnmarshalJSON(data []byte) error {
	aux := new(PillarInfoMarshal)
	if err := json.Unmarshal(data, aux); err != nil {
		return err
	}
	p.Name = aux.Name
	p.Rank = aux.Rank
	p.Type = aux.Type
	p.StakeAddress = aux.StakeAddress
	p.BlockProducingAddress = aux.BlockProducingAddress
	p.RewardWithdrawAddress = aux.RewardWithdrawAddress
	p.CanBeRevoked = aux.CanBeRevoked
	p.RevokeCooldown = aux.RevokeCooldown
	p.RevokeTime = aux.RevokeTime
	p.GiveMomentumRewardPercentage = aux.GiveMomentumRewardPercentage
	p.GiveDelegateRewardPercentage = aux.GiveDelegateRewardPercentage
	p.CurrentStats = aux.CurrentStats
	p.Weight = common.StringToBigInt(aux.Weight)
	return nil
}

// PillarInfoList is one page of pillars as returned by GetAll. Count is
// the total number of active pillars, not the number of entries in
// List.
type PillarInfoList struct {
	Count uint32        `json:"count"`
	List  []*PillarInfo `json:"list"`
}

// PillarStats counts the momentums a pillar has produced in the current
// epoch against the number it was expected to produce, as reported by
// the consensus cache.
type PillarStats struct {
	ProducedMomentums uint64 `json:"producedMomentums"`
	ExpectedMomentums uint64 `json:"expectedMomentums"`
}

// PillarInfoByWeight implements sort.Interface, ordering pillars by
// descending weight with ties broken by ascending name. GetAll uses it
// to assign ranks.
type PillarInfoByWeight []*PillarInfo

func (a PillarInfoByWeight) Len() int      { return len(a) }
func (a PillarInfoByWeight) Swap(i, j int) { a[i], a[j] = a[j], a[i] }
func (a PillarInfoByWeight) Less(i, j int) bool {
	r := a[j].Weight.Cmp(a[i].Weight)
	if r == 0 {
		return a[i].Name < a[j].Name
	} else {
		return r < 0
	}
}

// GetAll returns one page of the active (non-revoked) pillars, of both
// legacy and regular type, read from contract state at the frontier
// momentum and enriched with weights and momentum statistics from the
// consensus cache. The full list is sorted by descending weight (ties
// by ascending name) and ranked before the page window is cut, so Rank
// is global, and Count is the total number of active pillars. A
// pageSize above 1024 is rejected with api.ErrPageSizeParamTooBig.
//
// JSON-RPC: embedded.pillar.getAll
func (a *PillarApi) GetAll(pageIndex, pageSize uint32) (*PillarInfoList, error) {
	if pageSize > api.RpcMaxPageSize {
		return nil, api.ErrPageSizeParamTooBig
	}

	m, context, err := api.GetFrontierContext(a.chain, types.PillarContract)
	if err != nil {
		return nil, err
	}

	// pillars
	candidateList, err := definition.GetPillarsList(context.Storage(), true, definition.AnyPillarType)
	if err != nil {
		return nil, err
	}

	targetList := make([]*PillarInfo, len(candidateList))

	for index, pillar := range candidateList {
		// canBeRevoked
		canBeRevoked, revokeCooldown := implementation.PillarGetRevokeStatus(pillar, m)

		targetList[index] = &PillarInfo{
			Name:                         pillar.Name,
			Type:                         pillar.PillarType,
			StakeAddress:                 pillar.StakeAddress,
			BlockProducingAddress:        pillar.BlockProducingAddress,
			RewardWithdrawAddress:        pillar.RewardWithdrawAddress,
			RevokeTime:                   pillar.RevokeTime,
			GiveMomentumRewardPercentage: pillar.GiveBlockRewardPercentage,
			GiveDelegateRewardPercentage: pillar.GiveDelegateRewardPercentage,
			CanBeRevoked:                 canBeRevoked,
			RevokeCooldown:               revokeCooldown,
			CurrentStats: &PillarStats{
				ProducedMomentums: 0,
				ExpectedMomentums: 0,
			},
			Weight: common.Big0,
		}
	}

	// feed information from rpc consensus cache
	weights, stats := a.consensusCache.Get()
	if weights != nil {
		for _, pillar := range targetList {
			weight, ok := weights[pillar.Name]
			if !ok {
				pillar.Weight = big.NewInt(0)
			} else {
				pillar.Weight = (&big.Int{}).Set(weight)
			}
		}
	}

	if stats != nil {
		for _, pillar := range targetList {
			pillarStat, ok := stats.Pillars[pillar.Name]
			if ok {
				pillar.CurrentStats.ProducedMomentums = pillarStat.BlockNum
				pillar.CurrentStats.ExpectedMomentums = pillarStat.ExceptedBlockNum
			}
		}
	}

	sort.Sort(PillarInfoByWeight(targetList))
	for i := range targetList {
		targetList[i].Rank = i
	}

	start, end := api.GetRange(pageIndex, pageSize, uint32(len(targetList)))

	return &PillarInfoList{
		Count: uint32(len(targetList)),
		List:  targetList[start:end],
	}, nil
}

// GetByOwner returns the active pillars whose owner (stake) address is
// stakeAddress, with the same enrichment and ordering as GetAll. It
// scans only the first GetAll page of maximum size, so at most the
// 1024 highest-weighted pillars are considered. An empty list is
// returned when none match.
//
// JSON-RPC: embedded.pillar.getByOwner
func (a *PillarApi) GetByOwner(stakeAddress types.Address) ([]*PillarInfo, error) {
	list, err := a.GetAll(0, api.RpcMaxPageSize)
	if err != nil {
		return nil, err
	}
	targetList := make([]*PillarInfo, 0)
	for _, pillar := range list.List {
		if pillar.StakeAddress == stakeAddress {
			targetList = append(targetList, pillar)
		}
	}

	return targetList, nil
}

// GetByName returns the active pillar registered under the given name,
// with the same enrichment as GetAll, or nil without an error when no
// such pillar exists. Like GetByOwner it considers at most the 1024
// highest-weighted pillars.
//
// JSON-RPC: embedded.pillar.getByName
func (a *PillarApi) GetByName(name string) (*PillarInfo, error) {
	list, err := a.GetAll(0, api.RpcMaxPageSize)
	if err != nil {
		return nil, err
	}
	for _, pillar := range list.List {
		if pillar.Name == name {
			return pillar, nil
		}
	}

	return nil, nil
}

// CheckNameAvailability reports whether name can still be used to
// register a pillar. Unlike GetAll, the check runs against every
// pillar record in contract state, including revoked ones, so a revoked
// pillar's name stays unavailable.
//
// JSON-RPC: embedded.pillar.checkNameAvailability
func (a *PillarApi) CheckNameAvailability(name string) (bool, error) {
	_, context, err := api.GetFrontierContext(a.chain, types.PillarContract)
	if err != nil {
		return false, err
	}

	// pillars
	pillars, err := definition.GetPillarsList(context.Storage(), false, definition.AnyPillarType)
	if err != nil {
		return false, err
	}

	for _, pillar := range pillars {
		if pillar.Name == name {
			return false, nil
		}
	}
	return true, nil
}

// GetDelegatedPillarResponse describes an address's current delegation:
// the delegated pillar's name, its status (PillarActive while the
// pillar's contract record has RevokeTime 0, PillarInActive when the
// pillar has been revoked or no longer exists), and Balance, the
// delegating address's own ZNN balance at the frontier momentum in
// smallest units, which is what the delegation weighs (JSON field
// "weight").
type GetDelegatedPillarResponse struct {
	Name       string   `json:"name"`
	NodeStatus uint8    `json:"status"`
	Balance    *big.Int `json:"weight"`
}

// GetDelegatedPillarResponseMarshal is the JSON wire form of
// GetDelegatedPillarResponse, with Balance rendered as a base-10
// string.
type GetDelegatedPillarResponseMarshal struct {
	Name       string `json:"name"`
	NodeStatus uint8  `json:"status"`
	Balance    string `json:"weight"`
}

// ToGetDelegatedPillarResponse converts the response to its JSON wire
// representation, rendering Balance as a base-10 string.
func (g *GetDelegatedPillarResponse) ToGetDelegatedPillarResponse() *GetDelegatedPillarResponseMarshal {
	aux := &GetDelegatedPillarResponseMarshal{
		Name:       g.Name,
		NodeStatus: g.NodeStatus,
		Balance:    g.Balance.String(),
	}
	return aux
}

// MarshalJSON encodes the response via its
// GetDelegatedPillarResponseMarshal wire form, so Balance appears as a
// base-10 JSON string.
func (g *GetDelegatedPillarResponse) MarshalJSON() ([]byte, error) {
	return json.Marshal(g.ToGetDelegatedPillarResponse())
}

// UnmarshalJSON decodes the GetDelegatedPillarResponseMarshal wire form
// produced by MarshalJSON. A balance string that is not a valid base-10
// integer decodes to 0 rather than producing an error.
func (g *GetDelegatedPillarResponse) UnmarshalJSON(data []byte) error {
	aux := new(GetDelegatedPillarResponseMarshal)
	if err := json.Unmarshal(data, aux); err != nil {
		return err
	}
	g.Name = aux.Name
	g.NodeStatus = aux.NodeStatus
	g.Balance = common.StringToBigInt(aux.Balance)
	return nil
}

// GetDelegatedPillar returns the delegation of addr read from contract
// state at the frontier momentum, or nil without an error when addr
// does not delegate. The status is PillarActive only when the delegated
// pillar still exists with RevokeTime 0; a revoked or missing pillar
// record yields PillarInActive while the delegation entry itself
// remains. Balance is computed on the fly from addr's ZNN balance, not
// from contract state.
//
// JSON-RPC: embedded.pillar.getDelegatedPillar
func (a *PillarApi) GetDelegatedPillar(addr types.Address) (*GetDelegatedPillarResponse, error) {
	_, context, err := api.GetFrontierContext(a.chain, types.PillarContract)
	if err != nil {
		return nil, err
	}
	delegationInfo, err := definition.GetDelegationInfo(context.Storage(), addr)
	if err == constants.ErrDataNonExistent {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if delegationInfo != nil {
		balance, err := a.chain.GetFrontierMomentumStore().GetAccountStore(addr).GetBalance(types.ZnnTokenStandard)
		if err != nil {
			return nil, err
		}
		status := PillarInActive
		if pillar, err := definition.GetPillarInfo(context.Storage(), delegationInfo.Name); err == constants.ErrDataNonExistent {
		} else if err == nil {
			if pillar.RevokeTime == 0 {
				status = PillarActive
			}
		} else {
			return nil, err
		}

		return &GetDelegatedPillarResponse{
			Name:       delegationInfo.Name,
			NodeStatus: status,
			Balance:    balance}, nil

	}
	return nil, nil
}

// PillarEpochHistoryList is one page of per-epoch pillar history. Count
// depends on the producing method: GetPillarEpochHistory sets it to the
// total number of processed epochs (last processed epoch + 1), while
// GetPillarsHistoryByEpoch sets it to the number of pillar entries
// recorded for the requested epoch.
type PillarEpochHistoryList struct {
	Count int64                            `json:"count"`
	List  []*definition.PillarEpochHistory `json:"list"`
}

// GetPillarEpochHistory pages over the recorded epoch history of the
// named pillar (reward percentages, produced and expected momentums,
// weight per epoch), newest epoch first: page 0 starts at the last
// epoch the contract has processed and each entry steps one epoch back,
// stopping at epoch 0. Epochs with no record for the name yield
// synthesized entries with zero percentages, counts and weight, so the
// method does not distinguish a not-yet-registered pillar from an idle
// one. A pageSize above 1024 is rejected with
// api.ErrPageSizeParamTooBig.
//
// JSON-RPC: embedded.pillar.getPillarEpochHistory
func (a *PillarApi) GetPillarEpochHistory(pillarName string, pageIndex, pageSize uint32) (*PillarEpochHistoryList, error) {
	if pageSize > api.RpcMaxPageSize {
		return nil, api.ErrPageSizeParamTooBig
	}

	_, context, err := api.GetFrontierContext(a.chain, types.PillarContract)
	if err != nil {
		return nil, err
	}

	// get latest epoch
	lastEpoch, err := definition.GetLastEpochUpdate(context.Storage())
	if err != nil {
		return nil, err
	}

	epoch := lastEpoch.LastEpoch - int64(pageIndex*pageSize)

	result := &PillarEpochHistoryList{
		Count: lastEpoch.LastEpoch + 1,
		List:  make([]*definition.PillarEpochHistory, 0, pageSize),
	}
	for i := 0; i < int(pageSize); i += 1 {
		if epoch < 0 {
			break
		}
		if pillars, err := definition.GetPillarEpochHistoryList(context.Storage(), uint64(epoch)); err == nil {
			found := false
			for _, pillar := range pillars {
				if pillar.Name == pillarName {
					result.List = append(result.List, pillar)
					found = true
					break
				}
			}
			if !found {
				result.List = append(result.List, &definition.PillarEpochHistory{
					Name:                         pillarName,
					Epoch:                        uint64(epoch),
					GiveDelegateRewardPercentage: 0,
					GiveBlockRewardPercentage:    0,
					ProducedBlockNum:             0,
					ExpectedBlockNum:             0,
					Weight:                       common.Big0,
				})
			}
		} else {
			return nil, err
		}
		epoch -= 1
	}

	return result, err
}

// GetPillarsHistoryByEpoch returns one page of the history entries
// recorded for all pillars in the given epoch, in storage (key) order.
// Count is the total number of entries for that epoch; an epoch without
// records yields an empty list. A pageSize above 1024 is rejected with
// api.ErrPageSizeParamTooBig.
//
// JSON-RPC: embedded.pillar.getPillarsHistoryByEpoch
func (a *PillarApi) GetPillarsHistoryByEpoch(epoch uint64, pageIndex, pageSize uint32) (*PillarEpochHistoryList, error) {
	if pageSize > api.RpcMaxPageSize {
		return nil, api.ErrPageSizeParamTooBig
	}

	_, context, err := api.GetFrontierContext(a.chain, types.PillarContract)
	if err != nil {
		return nil, err
	}

	pillars, err := definition.GetPillarEpochHistoryList(context.Storage(), epoch)
	if err != nil {
		return nil, err
	}

	start, end := api.GetRange(pageIndex, pageSize, uint32(len(pillars)))

	return &PillarEpochHistoryList{
		Count: int64(len(pillars)),
		List:  pillars[start:end],
	}, nil
}
