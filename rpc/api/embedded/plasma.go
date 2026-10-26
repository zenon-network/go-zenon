package embedded

import (
	"encoding/json"
	"math/big"
	"sort"

	"github.com/pkg/errors"

	"github.com/zenon-network/go-zenon/chain"
	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/dp"
	"github.com/zenon-network/go-zenon/rpc/api"
	"github.com/zenon-network/go-zenon/vm"
	"github.com/zenon-network/go-zenon/vm/constants"
	"github.com/zenon-network/go-zenon/vm/embedded/definition"
	"github.com/zenon-network/go-zenon/vm/vm_context"
	"github.com/zenon-network/go-zenon/zenon"
)

// PlasmaApi implements the "embedded.plasma" JSON-RPC namespace, which
// reads plasma state derived from fused QSR: per-address plasma totals,
// the fusion entries stored in the plasma embedded contract, and the
// proof-of-work difficulty needed when fused plasma does not cover a
// prospective account block. Contract storage is read at the
// account-pool frontier; prices and heights come from the frontier
// momentum. Every exported method is served as
// embedded.plasma.<lowerCamelMethodName>.
type PlasmaApi struct {
	chain chain.Chain
}

// NewPlasmaApi returns a PlasmaApi bound to the given node's chain. It
// is called by the RPC server when the "embedded"
// namespace is enabled; it is not itself an RPC method.
func NewPlasmaApi(z zenon.Zenon) *PlasmaApi {
	return &PlasmaApi{
		chain: z.Chain(),
	}
}

// PlasmaInfo summarizes an address's plasma as reported by Get.
// QsrAmount is the total QSR fused with the address as beneficiary, in
// smallest units; MaxPlasma is the plasma that fused amount converts
// to; CurrentPlasma is the portion still available after subtracting
// plasma held by the address's unconfirmed account blocks.
type PlasmaInfo struct {
	CurrentPlasma uint64   `json:"currentPlasma"`
	MaxPlasma     uint64   `json:"maxPlasma"`
	QsrAmount     *big.Int `json:"qsrAmount"`
}

// PlasmaInfoMarshal is the JSON wire form of PlasmaInfo, with QsrAmount
// rendered as a base-10 string.
type PlasmaInfoMarshal struct {
	CurrentPlasma uint64 `json:"currentPlasma"`
	MaxPlasma     uint64 `json:"maxPlasma"`
	QsrAmount     string `json:"qsrAmount"`
}

// ToPlasmaInfoMarshal converts the plasma info to its JSON wire
// representation, rendering QsrAmount as a base-10 string.
func (r *PlasmaInfo) ToPlasmaInfoMarshal() *PlasmaInfoMarshal {
	aux := &PlasmaInfoMarshal{
		CurrentPlasma: r.CurrentPlasma,
		MaxPlasma:     r.MaxPlasma,
		QsrAmount:     r.QsrAmount.String(),
	}

	return aux
}

// MarshalJSON encodes the plasma info via its PlasmaInfoMarshal wire
// form, so QsrAmount appears as a base-10 JSON string.
func (r *PlasmaInfo) MarshalJSON() ([]byte, error) {
	return json.Marshal(r.ToPlasmaInfoMarshal())
}

// UnmarshalJSON decodes the PlasmaInfoMarshal wire form produced by
// MarshalJSON. A QSR amount string that is not a valid base-10 integer
// decodes to 0 rather than producing an error.
func (r *PlasmaInfo) UnmarshalJSON(data []byte) error {
	aux := new(PlasmaInfoMarshal)
	if err := json.Unmarshal(data, aux); err != nil {
		return err
	}
	r.CurrentPlasma = aux.CurrentPlasma
	r.MaxPlasma = aux.MaxPlasma
	r.QsrAmount = common.StringToBigInt(aux.QsrAmount)
	return nil
}

// FusionEntry is one QSR fusion recorded in the plasma contract: the
// fused QSR amount in smallest units, the beneficiary address whose
// plasma it backs, the momentum height starting at which the owner may
// cancel the fusion and reclaim the QSR, the entry's id, which is the
// hash of the send block that created it, and whether the entry is
// already revocable (the frontier momentum has reached its expiration
// height).
type FusionEntry struct {
	QsrAmount        *big.Int      `json:"qsrAmount"`
	Beneficiary      types.Address `json:"beneficiary"`
	ExpirationHeight uint64        `json:"expirationHeight"`
	Id               types.Hash    `json:"id"`
	IsRevocable      bool          `json:"isRevocable"`
}

// FusionEntryMarshal is the JSON wire form of FusionEntry, with
// QsrAmount rendered as a base-10 string.
type FusionEntryMarshal struct {
	QsrAmount        string        `json:"qsrAmount"`
	Beneficiary      types.Address `json:"beneficiary"`
	ExpirationHeight uint64        `json:"expirationHeight"`
	Id               types.Hash    `json:"id"`
	IsRevocable      bool          `json:"isRevocable"`
}

// ToFusionEntryMarshal converts the entry to its JSON wire
// representation, rendering QsrAmount as a base-10 string.
func (r *FusionEntry) ToFusionEntryMarshal() *FusionEntryMarshal {
	aux := &FusionEntryMarshal{
		QsrAmount:        r.QsrAmount.String(),
		Beneficiary:      r.Beneficiary,
		ExpirationHeight: r.ExpirationHeight,
		Id:               r.Id,
		IsRevocable:      r.IsRevocable,
	}

	return aux
}

// MarshalJSON encodes the entry via its FusionEntryMarshal wire form,
// so QsrAmount appears as a base-10 JSON string.
func (r *FusionEntry) MarshalJSON() ([]byte, error) {
	return json.Marshal(r.ToFusionEntryMarshal())
}

// UnmarshalJSON decodes the FusionEntryMarshal wire form produced by
// MarshalJSON. A QSR amount string that is not a valid base-10 integer
// decodes to 0 rather than producing an error.
func (r *FusionEntry) UnmarshalJSON(data []byte) error {
	aux := new(FusionEntryMarshal)
	if err := json.Unmarshal(data, aux); err != nil {
		return err
	}
	r.QsrAmount = common.StringToBigInt(aux.QsrAmount)
	r.Beneficiary = aux.Beneficiary
	r.ExpirationHeight = aux.ExpirationHeight
	r.Id = aux.Id
	r.IsRevocable = aux.IsRevocable
	return nil
}

// FusionEntryList is one page of an owner's fusion entries as returned
// by GetEntriesByAddress. QsrAmount and Count cover all of the owner's
// entries, not just the page: QsrAmount is the total QSR the owner has
// fused, in smallest units, and Count is the total number of entries.
type FusionEntryList struct {
	QsrAmount *big.Int       `json:"qsrAmount"`
	Count     int            `json:"count"`
	Fusions   []*FusionEntry `json:"list"`
}

// FusionEntryListMarshal is the JSON wire form of FusionEntryList, with
// the total QsrAmount rendered as a base-10 string.
type FusionEntryListMarshal struct {
	QsrAmount string         `json:"qsrAmount"`
	Count     int            `json:"count"`
	Fusions   []*FusionEntry `json:"list"`
}

// ToFusionEntryListMarshal converts the list to its JSON wire
// representation, rendering the total QsrAmount as a base-10 string.
func (r *FusionEntryList) ToFusionEntryListMarshal() *FusionEntryListMarshal {
	aux := &FusionEntryListMarshal{
		QsrAmount: r.QsrAmount.String(),
		Count:     r.Count,
	}
	aux.Fusions = make([]*FusionEntry, len(r.Fusions))
	for idx, fusion := range r.Fusions {
		aux.Fusions[idx] = fusion
	}

	return aux
}

// MarshalJSON encodes the list via its FusionEntryListMarshal wire
// form, so the total QsrAmount appears as a base-10 JSON string.
func (r *FusionEntryList) MarshalJSON() ([]byte, error) {
	return json.Marshal(r.ToFusionEntryListMarshal())
}

// UnmarshalJSON decodes the FusionEntryListMarshal wire form produced
// by MarshalJSON, sizing the entry slice from the decoded list.
func (r *FusionEntryList) UnmarshalJSON(data []byte) error {
	aux := new(FusionEntryListMarshal)
	if err := json.Unmarshal(data, aux); err != nil {
		return err
	}
	r.QsrAmount = common.StringToBigInt(aux.QsrAmount)
	r.Count = aux.Count
	r.Fusions = make([]*FusionEntry, len(aux.Fusions))
	for idx, fusion := range aux.Fusions {
		r.Fusions[idx] = fusion
	}

	return nil
}

// SortFusionEntryByHeight implements sort.Interface, ordering fusion
// entries by ascending expiration height with ties broken by ascending
// beneficiary address string. GetEntriesByAddress uses it before
// paginating.
type SortFusionEntryByHeight []*definition.FusionInfo

// Len implements sort.Interface.
func (a SortFusionEntryByHeight) Len() int { return len(a) }

// Swap implements sort.Interface.
func (a SortFusionEntryByHeight) Swap(i, j int) { a[i], a[j] = a[j], a[i] }

// Less orders by ascending expiration height, then beneficiary.
func (a SortFusionEntryByHeight) Less(i, j int) bool {
	if a[i].ExpirationHeight == a[j].ExpirationHeight {
		return a[i].Beneficiary.String() < a[j].Beneficiary.String()
	}
	return a[i].ExpirationHeight < a[j].ExpirationHeight
}

// Get returns the plasma of address: the total QSR fused with it as
// beneficiary and the plasma that amount converts to, both as confirmed
// by the frontier momentum (a fusion whose receive has executed but is
// not yet confirmed does not count), and the plasma currently
// available once the plasma held by the address's unconfirmed account
// blocks is subtracted from that total.
//
// JSON-RPC: embedded.plasma.get
func (a *PlasmaApi) Get(address types.Address) (*PlasmaInfo, error) {
	_, context, err := api.GetFrontierContext(a.chain, address)
	if err != nil {
		return nil, err
	}

	amount, err := a.chain.GetFrontierMomentumStore().GetStakeBeneficialAmount(address)
	if err != nil {
		return nil, err
	}

	var availablePlasma uint64
	var maxPlasma uint64

	if context.IsDynamicPlasmaSporkEnforced() {
		availablePlasma, err = vm.AvailablePlasmaV2(context.CacheStore(), context)
		if err != nil {
			return nil, err
		}
		maxPlasma = dp.FusedAmountToPlasma(amount)
	} else {
		availablePlasma, err = vm.AvailablePlasma(context.CacheStore(), context)
		if err != nil {
			return nil, err
		}
		maxPlasma = vm.FussedAmountToPlasma(amount)
	}

	return &PlasmaInfo{
		CurrentPlasma: availablePlasma,
		MaxPlasma:     maxPlasma,
		QsrAmount:     amount,
	}, nil
}

// GetEntriesByAddress returns one page of the fusion entries owned by
// address (the address that sent the fuse transactions, not the
// beneficiary), read from plasma contract state at the account-pool
// frontier and sorted by ascending expiration height with ties broken
// by beneficiary. Each entry's IsRevocable reports whether the frontier
// momentum height has reached the entry's expiration height. QsrAmount
// and Count in the result cover all of the owner's entries. A pageSize
// above 1024 is rejected with api.ErrPageSizeParamTooBig.
//
// JSON-RPC: embedded.plasma.getEntriesByAddress
func (a *PlasmaApi) GetEntriesByAddress(address types.Address, pageIndex, pageSize uint32) (*FusionEntryList, error) {
	if pageSize > api.RpcMaxPageSize {
		return nil, api.ErrPageSizeParamTooBig
	}

	momentum, context, err := api.GetFrontierContext(a.chain, types.PlasmaContract)
	if err != nil {
		return nil, err
	}
	list, amount, err := definition.GetFusionInfoListByOwner(context.Storage(), address)
	if err != nil {
		return nil, err
	}

	sort.Sort(SortFusionEntryByHeight(list))
	listLen := len(list)
	start, end := api.GetRange(pageIndex, pageSize, uint32(listLen))
	entryList := make([]*FusionEntry, end-start)

	for i, info := range list[start:end] {
		entryList[i] = &FusionEntry{
			QsrAmount:        info.Amount,
			Beneficiary:      info.Beneficiary,
			ExpirationHeight: info.ExpirationHeight,
			Id:               info.Id,
			IsRevocable:      momentum.Height >= info.ExpirationHeight,
		}
	}
	return &FusionEntryList{amount, listLen, entryList}, nil
}

// GetRequiredParam describes the prospective account block passed to
// GetRequiredPoWForAccountBlock: the sending address, the block type
// (nom.BlockTypeUserSend or a receive type), the destination address
// (required for user-send blocks) and the data payload, whose length
// influences the plasma requirement of send blocks.
type GetRequiredParam struct {
	SelfAddr  types.Address  `json:"address"`
	BlockType uint64         `json:"blockType"`
	ToAddr    *types.Address `json:"toAddress"`
	Data      []byte         `json:"data"`
}

// GetRequiredResult is the answer of GetRequiredPoWForAccountBlock:
// the plasma currently available to the address from fused QSR, the
// minimum plasma the described block requires, the proof-of-work
// difficulty that would cover the shortfall (0 when the available
// plasma already suffices) and the total fused plasma a fused-only
// block must carry: the block's base plasma before Dynamic Plasma, and
// the base plasma scaled by the frontier momentum's NextFusionPrice
// once Dynamic Plasma is enforced and the frontier momentum is a
// version-2 momentum.
type GetRequiredResult struct {
	AvailablePlasma    uint64 `json:"availablePlasma"`
	BasePlasma         uint64 `json:"basePlasma"`
	RequiredDifficulty uint64 `json:"requiredDifficulty"`
	// RequiredFusedPlasma is the total fused-only payment target, not an
	// additional fused amount to attach alongside the PoW recommendation.
	RequiredFusedPlasma uint64 `json:"requiredFusedPlasma"`
}

// GetRequiredPoWForAccountBlock computes, against the frontier
// momentum, how much proof of work the described account block would
// need on top of the address's available fused plasma. The base plasma
// is the block's minimum requirement (a flat base for receive blocks; a
// base plus a per-byte data charge for sends to user addresses; the
// called method's fixed cost for sends to embedded contracts, plus the
// same per-byte charge on the argument bytes once Dynamic Plasma is
// enforced, see vm.EmbeddedSendBasePlasma). When available plasma
// covers it the required difficulty is 0; otherwise the difficulty is
// proportional to the missing plasma (1500 per unit), and a shortfall
// beyond what proof of work may cover for one block is rejected with an
// error. A user-send block without a destination address is rejected.
//
// The quote prices the block against the frontier momentum and reads
// spork state from the cache at that same momentum, so the figure is
// the one the VM computes for a block acknowledging it. Once Dynamic
// Plasma is enforced the capacity rules follow the spork (available
// plasma from AvailablePlasmaV2, the proof-of-work cap from package dp),
// and once the frontier momentum is also a version-2 momentum (the first
// momentum after enforcement still carries no prices) the fused and
// work requirements are scaled by that momentum's NextFusionPrice and
// NextWorkPrice, and the result's RequiredFusedPlasma is the
// price-scaled fused-only target.
//
// JSON-RPC: embedded.plasma.getRequiredPoWForAccountBlock
func (a *PlasmaApi) GetRequiredPoWForAccountBlock(param GetRequiredParam) (*GetRequiredResult, error) {
	momentumStore := a.chain.GetFrontierMomentumStore()
	frontierMomentum, err := momentumStore.GetFrontierMomentum()
	if err != nil {
		return nil, err
	}
	// The cache is read at the captured frontier, not at the cache
	// frontier: an insert in flight commits the cache before the
	// momentum, and the price depends on the spork state of the
	// momentum the quoted block will acknowledge.
	cacheStore := a.chain.GetCacheStore(frontierMomentum.Identifier())
	if cacheStore == nil {
		return nil, errors.Errorf("can't find cache store for frontier %v", frontierMomentum.Identifier())
	}
	context := vm_context.NewAccountContext(momentumStore, a.chain.GetFrontierAccountStore(param.SelfAddr), cacheStore, nil)

	// get required plasma
	block := &nom.AccountBlock{
		BlockType:            param.BlockType,
		Address:              param.SelfAddr,
		Data:                 param.Data,
		MomentumAcknowledged: frontierMomentum.Identifier(),
	}

	if param.ToAddr != nil {
		block.ToAddress = *param.ToAddr
	} else if param.BlockType == nom.BlockTypeUserSend {
		return nil, errors.New("toAddress is nil")
	}

	basePlasma, err := vm.GetBasePlasmaForAccountBlock(context, block)
	if err != nil {
		return nil, err
	}

	var availablePlasma uint64
	var requiredFusedPlasma uint64

	// Capacity rules (available plasma, proof-of-work cap) follow the
	// spork, as the VM's enoughPlasma does. NextFusionPrice/NextWorkPrice
	// are only meaningful once the frontier momentum itself was produced
	// under dynamic plasma; the spork can be enforced for one momentum
	// before the first v2 momentum lands, so price scaling also needs
	// the momentum version.
	sporkEnforced := context.IsDynamicPlasmaSporkEnforced()
	dynamicPlasmaActive := sporkEnforced && frontierMomentum.Version >= nom.DynamicPlasmaMomentumVersion

	if sporkEnforced {
		availablePlasma, err = vm.AvailablePlasmaV2(context.CacheStore(), context)
	} else {
		availablePlasma, err = vm.AvailablePlasma(context.CacheStore(), context)
	}
	if err != nil {
		return nil, err
	}

	if dynamicPlasmaActive {
		// Round up so the recommended payment always meets the ValidPrice threshold. Uses
		// big.Int since basePlasma*NextFusionPrice can overflow uint64 at high dynamic prices.
		requiredFusedPlasmaBig := new(big.Int).Mul(new(big.Int).SetUint64(basePlasma), new(big.Int).SetUint64(frontierMomentum.NextFusionPrice))
		requiredFusedPlasmaBig.Add(requiredFusedPlasmaBig, new(big.Int).SetUint64(dp.PriceScaleFactor-1))
		requiredFusedPlasmaBig.Div(requiredFusedPlasmaBig, new(big.Int).SetUint64(dp.PriceScaleFactor))
		if !requiredFusedPlasmaBig.IsUint64() {
			return nil, constants.ErrForbiddenParam
		}
		requiredFusedPlasma = requiredFusedPlasmaBig.Uint64()
	} else {
		requiredFusedPlasma = basePlasma
	}

	if availablePlasma >= requiredFusedPlasma {
		return &GetRequiredResult{
			AvailablePlasma:     availablePlasma,
			BasePlasma:          basePlasma,
			RequiredDifficulty:  0,
			RequiredFusedPlasma: requiredFusedPlasma,
		}, nil
	} else {
		var requiredDifficulty uint64
		if dynamicPlasmaActive {
			effectivePlasma := availablePlasma * dp.PriceScaleFactor / frontierMomentum.NextFusionPrice
			requiredWorkPlasma := basePlasma - effectivePlasma

			// Round up the price-scaled work-plasma before converting to difficulty, so the
			// recommended difficulty always meets the ValidPrice threshold. Uses big.Int since
			// requiredWorkPlasma*NextWorkPrice can overflow uint64 at high dynamic prices.
			priceScaledWorkPlasma := new(big.Int).Mul(new(big.Int).SetUint64(requiredWorkPlasma), new(big.Int).SetUint64(frontierMomentum.NextWorkPrice))
			priceScaledWorkPlasma.Add(priceScaledWorkPlasma, new(big.Int).SetUint64(dp.PriceScaleFactor-1))
			priceScaledWorkPlasma.Div(priceScaledWorkPlasma, new(big.Int).SetUint64(dp.PriceScaleFactor))

			if !priceScaledWorkPlasma.IsUint64() {
				return nil, constants.ErrForbiddenParam
			}

			requiredDifficulty, err = dp.GetDifficultyForPlasma(priceScaledWorkPlasma.Uint64())
			if err != nil {
				return nil, err
			}
		} else if sporkEnforced {
			requiredDifficulty, err = dp.GetDifficultyForPlasma(basePlasma - availablePlasma)
			if err != nil {
				return nil, err
			}
		} else {
			requiredDifficulty, err = vm.GetDifficultyForPlasma(basePlasma - availablePlasma)
			if err != nil {
				return nil, err
			}
		}
		return &GetRequiredResult{
			AvailablePlasma:     availablePlasma,
			BasePlasma:          basePlasma,
			RequiredDifficulty:  requiredDifficulty,
			RequiredFusedPlasma: requiredFusedPlasma,
		}, nil
	}
}

// GetVariables returns the Dynamic Plasma variables in force at the
// account-pool frontier: the per-momentum base plasma ceiling, the fused
// and PoW targets and the price-change limits. Before SetVariables has
// ever been called it returns the compiled defaults.
//
// JSON-RPC: embedded.plasma.getVariables
func (a *PlasmaApi) GetVariables() (*definition.PlasmaVariables, error) {
	_, context, err := api.GetFrontierContext(a.chain, types.PlasmaContract)
	if err != nil {
		return nil, err
	}

	variables, err := definition.GetPlasmaVariables(context.Storage())
	if err != nil {
		return nil, err
	}

	return variables, nil
}
