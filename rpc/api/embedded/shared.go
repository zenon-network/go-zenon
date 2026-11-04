// Package embedded exposes JSON-RPC handlers for the embedded contracts
// (pillar, plasma, sentinel, token, stake, spork, swap, accelerator,
// htlc, bridge, liquidity). Method results combine contract state read
// at the account-pool frontier — the latest state, including the
// contract's executed receive blocks not yet confirmed by a momentum
// (see api.GetFrontierContext) — with computed values such as uncollected
// rewards or revocation cooldowns; each API's exported methods are
// served as embedded.<contract>.<lowerCamelMethodName>.
//
// Throughout the package, epochs are consecutive 24-hour windows
// counted from the genesis timestamp, starting at epoch 0, and token
// amounts are expressed in the smallest unit (10^8 per ZNN or QSR) with
// *big.Int values rendered as base-10 JSON strings.
package embedded

import (
	"encoding/json"
	"github.com/zenon-network/go-zenon/common"
	"math/big"

	"github.com/zenon-network/go-zenon/chain"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/rpc/api"
	"github.com/zenon-network/go-zenon/vm/constants"
	"github.com/zenon-network/go-zenon/vm/embedded/definition"
)

// checkPillarName rejects a name longer than the pillar contract will
// ever register (constants.PillarNameLengthMax bytes, the same byte
// count the contract checks) with api.ErrNameParamTooLong. Handlers
// that key reads or responses by a caller-supplied name call it before
// opening a frontier context, so an oversized name costs neither a
// state read nor a response that carries it.
func checkPillarName(name string) error {
	if len(name) > constants.PillarNameLengthMax {
		return api.ErrNameParamTooLong
	}
	return nil
}

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

// RewardHistoryEntry is the reward an address earned from one embedded
// contract during one epoch: the epoch number and the ZNN and QSR
// amounts credited for it, in smallest units.
type RewardHistoryEntry struct {
	Epoch int64    `json:"epoch"`
	Znn   *big.Int `json:"znnAmount"`
	Qsr   *big.Int `json:"qsrAmount"`
}

// RewardHistoryEntryMarshal is the JSON wire form of
// RewardHistoryEntry, with the ZNN and QSR amounts rendered as base-10
// strings. It exists so the custom MarshalJSON/UnmarshalJSON of
// RewardHistoryEntry can round-trip amounts without precision loss.
type RewardHistoryEntryMarshal struct {
	Epoch int64  `json:"epoch"`
	Znn   string `json:"znnAmount"`
	Qsr   string `json:"qsrAmount"`
}

// ToRewardDepositMarshal converts the entry to its JSON wire
// representation, rendering the ZNN and QSR amounts as base-10 strings.
func (r *RewardHistoryEntry) ToRewardDepositMarshal() *RewardHistoryEntryMarshal {
	aux := &RewardHistoryEntryMarshal{
		Epoch: r.Epoch,
		Znn:   r.Znn.String(),
		Qsr:   r.Qsr.String(),
	}

	return aux
}

// MarshalJSON encodes the entry via its RewardHistoryEntryMarshal wire
// form, so the amounts appear as base-10 JSON strings.
func (r *RewardHistoryEntry) MarshalJSON() ([]byte, error) {
	return json.Marshal(r.ToRewardDepositMarshal())
}

// UnmarshalJSON decodes the RewardHistoryEntryMarshal wire form
// produced by MarshalJSON. Amount strings that are not valid base-10
// integers decode to 0 rather than producing an error.
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

// RewardHistoryList is one page of per-epoch reward history. Count is
// the total number of epochs the contract has processed so far (last
// processed epoch + 1), not the number of entries in List; List holds
// the requested page in descending epoch order.
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

	// Widen before multiplying: pageIndex*pageSize in uint32 wraps for a
	// large pageIndex and would hand back the newest epochs instead of an
	// empty page.
	epoch := lastEpoch.LastEpoch - int64(pageIndex)*int64(pageSize)

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
