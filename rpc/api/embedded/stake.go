package embedded

import (
	"encoding/json"
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

// StakeApi implements the "embedded.stake" JSON-RPC namespace, which
// reads the state of the stake embedded contract (active stake entries
// and the QSR rewards they earn) as of the frontier momentum. A stake
// locks at least 1 ZNN for a duration that is a multiple of 30 days,
// between 30 and 360 days; it can be cancelled only after expiration,
// which returns the ZNN and marks the entry revoked. Every exported
// method is served as embedded.stake.<lowerCamelMethodName>.
type StakeApi struct {
	chain chain.Chain
	z     zenon.Zenon
	cs    consensus.Consensus
	log   log15.Logger
}

// NewStakeApi returns a StakeApi bound to the given node's chain. It is
// called by the RPC server when the "embedded" namespace is enabled; it
// is not itself an RPC method.
func NewStakeApi(z zenon.Zenon) *StakeApi {
	return &StakeApi{
		chain: z.Chain(),
		z:     z,
		cs:    z.Consensus(),
		log:   common.RPCLogger.New("module", "embedded_stake_api"),
	}
}

// === Shared RPCs ===

// GetUncollectedReward returns the staking rewards credited to address
// but not yet collected, read from contract state at the frontier
// momentum. Staking rewards are paid in QSR, so the ZNN amount of the
// deposit stays 0; an address with nothing to collect yields a deposit
// with both amounts 0, not an error.
//
// JSON-RPC: embedded.stake.getUncollectedReward
func (a *StakeApi) GetUncollectedReward(address types.Address) (*definition.RewardDeposit, error) {
	return getUncollectedReward(a.chain, types.StakeContract, address)
}

// GetFrontierRewardByPage pages over the per-epoch staking reward
// history of address, newest epoch first; epochs without a recorded
// reward yield zero-amount entries. A pageSize above 1024 is rejected
// with api.ErrPageSizeParamTooBig.
//
// JSON-RPC: embedded.stake.getFrontierRewardByPage
func (a *StakeApi) GetFrontierRewardByPage(address types.Address, pageIndex, pageSize uint32) (*RewardHistoryList, error) {
	if pageSize > api.RpcMaxPageSize {
		return nil, api.ErrPageSizeParamTooBig
	}
	return getFrontierRewardByPage(a.chain, types.StakeContract, address, pageIndex, pageSize)
}

// StakeEntry describes one active stake as reported by
// GetEntriesByAddress. Amount is the locked ZNN in smallest units; Id
// is the hash of the send block that created the stake and is the
// handle passed to the contract's Cancel method. WeightedAmount is the
// amount scaled by the duration multiplier used when splitting epoch
// rewards: amount times (9 + duration in 30-day units) / 10, so a
// 30-day stake weighs 1x and each additional 30-day unit adds 0.1x, up
// to 2.1x at 360 days. The timestamps are unix seconds; the stake can
// be cancelled once ExpirationTimestamp has passed.
type StakeEntry struct {
	Amount              *big.Int      `json:"amount"`
	WeightedAmount      *big.Int      `json:"weightedAmount"`
	StartTimestamp      int64         `json:"startTimestamp"`
	ExpirationTimestamp int64         `json:"expirationTimestamp"`
	Address             types.Address `json:"address"`
	Id                  types.Hash    `json:"id"`
}

// StakeEntryMarshal is the JSON wire form of StakeEntry, with the
// amount and weighted amount rendered as base-10 strings. It exists so
// the custom MarshalJSON/UnmarshalJSON of StakeEntry can round-trip
// amounts without precision loss.
type StakeEntryMarshal struct {
	Amount              string        `json:"amount"`
	WeightedAmount      string        `json:"weightedAmount"`
	StartTimestamp      int64         `json:"startTimestamp"`
	ExpirationTimestamp int64         `json:"expirationTimestamp"`
	Address             types.Address `json:"address"`
	Id                  types.Hash    `json:"id"`
}

// ToStakeEntryMarshal converts the entry to its JSON wire
// representation, rendering the amount and weighted amount as base-10
// strings.
func (s *StakeEntry) ToStakeEntryMarshal() *StakeEntryMarshal {
	aux := &StakeEntryMarshal{
		Amount:              s.Amount.String(),
		WeightedAmount:      s.WeightedAmount.String(),
		StartTimestamp:      s.StartTimestamp,
		ExpirationTimestamp: s.ExpirationTimestamp,
		Address:             s.Address,
		Id:                  s.Id,
	}
	return aux
}

// MarshalJSON encodes the entry via its StakeEntryMarshal wire form, so
// the amounts appear as base-10 JSON strings.
func (s *StakeEntry) MarshalJSON() ([]byte, error) {
	return json.Marshal(s.ToStakeEntryMarshal())
}

// UnmarshalJSON decodes the StakeEntryMarshal wire form produced by
// MarshalJSON. Amount strings that are not valid base-10 integers
// decode to 0 rather than producing an error.
func (s *StakeEntry) UnmarshalJSON(data []byte) error {
	aux := new(StakeEntryMarshal)
	if err := json.Unmarshal(data, aux); err != nil {
		return err
	}
	s.Amount = common.StringToBigInt(aux.Amount)
	s.WeightedAmount = common.StringToBigInt(aux.WeightedAmount)
	s.StartTimestamp = aux.StartTimestamp
	s.ExpirationTimestamp = aux.ExpirationTimestamp
	s.Address = aux.Address
	s.Id = aux.Id
	return nil
}

// StakeList is one page of an address's active stake entries. Count is
// the total number of active entries, not the number of entries in the
// page, and TotalAmount and TotalWeightedAmount sum over all active
// entries (in smallest units of ZNN), not just the page.
type StakeList struct {
	TotalAmount         *big.Int      `json:"totalAmount"`
	TotalWeightedAmount *big.Int      `json:"totalWeightedAmount"`
	Count               int           `json:"count"`
	Entries             []*StakeEntry `json:"list"`
}

// StakeListMarshal is the JSON wire form of StakeList, with the two
// totals rendered as base-10 strings. It exists so the custom
// MarshalJSON/UnmarshalJSON of StakeList can round-trip the totals
// without precision loss.
type StakeListMarshal struct {
	TotalAmount         string        `json:"totalAmount"`
	TotalWeightedAmount string        `json:"totalWeightedAmount"`
	Count               int           `json:"count"`
	Entries             []*StakeEntry `json:"list"`
}

// ToStakeEntryMarshal converts the list to its JSON wire
// representation, rendering the two totals as base-10 strings.
func (s *StakeList) ToStakeEntryMarshal() *StakeListMarshal {
	aux := &StakeListMarshal{
		TotalAmount:         s.TotalAmount.String(),
		TotalWeightedAmount: s.TotalWeightedAmount.String(),
		Count:               s.Count,
	}
	aux.Entries = make([]*StakeEntry, len(s.Entries))
	for idx, entry := range s.Entries {
		aux.Entries[idx] = entry
	}
	return aux
}

// MarshalJSON encodes the list via its StakeListMarshal wire form, so
// the totals appear as base-10 JSON strings.
func (s *StakeList) MarshalJSON() ([]byte, error) {
	return json.Marshal(s.ToStakeEntryMarshal())
}

// UnmarshalJSON decodes the StakeListMarshal wire form produced by
// MarshalJSON. Total strings that are not valid base-10 integers decode
// to 0 rather than producing an error.
func (s *StakeList) UnmarshalJSON(data []byte) error {
	aux := new(StakeListMarshal)
	if err := json.Unmarshal(data, aux); err != nil {
		return err
	}
	s.TotalAmount = common.StringToBigInt(aux.TotalAmount)
	s.TotalWeightedAmount = common.StringToBigInt(aux.TotalWeightedAmount)
	s.Count = aux.Count
	s.Entries = make([]*StakeEntry, len(aux.Entries))
	for idx, entry := range aux.Entries {
		s.Entries[idx] = entry
	}
	return nil
}

// GetEntriesByAddress returns one page of the active (not yet
// cancelled) stake entries of address, read from contract state at the
// frontier momentum and sorted by ascending expiration time, soonest
// expiring first, with ties broken by ascending entry id. Count and the
// two totals cover all of the address's active entries, not just the
// page. A pageSize above 1024 is rejected with
// api.ErrPageSizeParamTooBig.
//
// JSON-RPC: embedded.stake.getEntriesByAddress
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
