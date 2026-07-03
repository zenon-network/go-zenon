package embedded

import (
	"encoding/json"
	"math/big"
	"sort"

	"github.com/inconshreveable/log15"
	"github.com/pkg/errors"

	"github.com/zenon-network/go-zenon/chain"
	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/consensus"
	"github.com/zenon-network/go-zenon/rpc/api"
	"github.com/zenon-network/go-zenon/vm"
	"github.com/zenon-network/go-zenon/vm/embedded/definition"
	"github.com/zenon-network/go-zenon/zenon"
)

// PlasmaApi implements the "embedded.plasma" JSON-RPC namespace, which
// reads plasma state derived from fused QSR: per-address plasma totals,
// the fusion entries stored in the plasma embedded contract, and the
// proof-of-work difficulty needed when fused plasma does not cover a
// prospective account block. All reads are against the frontier
// momentum. Every exported method is served as
// embedded.plasma.<lowerCamelMethodName>.
type PlasmaApi struct {
	chain chain.Chain
	z     zenon.Zenon
	cs    consensus.Consensus
	log   log15.Logger
}

// NewPlasmaApi returns a PlasmaApi bound to the given node's chain and
// consensus. It is called by the RPC server when the "embedded"
// namespace is enabled; it is not itself an RPC method.
func NewPlasmaApi(z zenon.Zenon) *PlasmaApi {
	return &PlasmaApi{
		chain: z.Chain(),
		z:     z,
		cs:    z.Consensus(),
		log:   common.RPCLogger.New("module", "embedded_plasma_api"),
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
// by MarshalJSON. Only QsrAmount and Count round-trip reliably: the
// entry slice is sized from the receiver's previous length instead of
// the decoded one, so decoding a list with more entries than the
// receiver already holds panics with an index out of range.
func (r *FusionEntryList) UnmarshalJSON(data []byte) error {
	aux := new(FusionEntryListMarshal)
	if err := json.Unmarshal(data, aux); err != nil {
		return err
	}
	r.QsrAmount = common.StringToBigInt(aux.QsrAmount)
	r.Count = aux.Count
	r.Fusions = make([]*FusionEntry, len(r.Fusions))
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

func (a SortFusionEntryByHeight) Len() int      { return len(a) }
func (a SortFusionEntryByHeight) Swap(i, j int) { a[i], a[j] = a[j], a[i] }
func (a SortFusionEntryByHeight) Less(i, j int) bool {
	if a[i].ExpirationHeight == a[j].ExpirationHeight {
		return a[i].Beneficiary.String() < a[j].Beneficiary.String()
	}
	return a[i].ExpirationHeight < a[j].ExpirationHeight
}

// Get returns the plasma of address at the frontier momentum: the
// total QSR fused with it as beneficiary, the plasma that amount
// converts to, and the plasma currently available once the plasma held
// by the address's unconfirmed account blocks is subtracted.
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

	available, err := vm.AvailablePlasma(context.MomentumStore(), context)
	if err != nil {
		return nil, err
	}

	return &PlasmaInfo{
		CurrentPlasma: available,
		MaxPlasma:     vm.FussedAmountToPlasma(amount),
		QsrAmount:     amount,
	}, nil
}

// GetEntriesByAddress returns one page of the fusion entries owned by
// address (the address that sent the fuse transactions, not the
// beneficiary), read from plasma contract state at the frontier
// momentum and sorted by ascending expiration height with ties broken
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
// the plasma currently available to the address from fused QSR,
// the minimum plasma the described block requires, and the
// proof-of-work difficulty that would cover the shortfall
// (0 when the available plasma already suffices).
type GetRequiredResult struct {
	AvailablePlasma    uint64 `json:"availablePlasma"`
	BasePlasma         uint64 `json:"basePlasma"`
	RequiredDifficulty uint64 `json:"requiredDifficulty"`
}

// GetRequiredPoWForAccountBlock computes, against the frontier
// momentum, how much proof of work the described account block would
// need on top of the address's available fused plasma. The base plasma
// is the block's minimum requirement (a flat base for receive blocks; a
// base plus a per-byte data charge for sends to user addresses; the
// called method's fixed cost for sends to embedded contracts). When
// available plasma covers it the required difficulty is 0; otherwise
// the difficulty is proportional to the missing plasma (1500 per unit),
// and a shortfall beyond what proof of work may cover for one block is
// rejected with an error. A user-send block without a destination
// address is rejected.
//
// JSON-RPC: embedded.plasma.getRequiredPoWForAccountBlock
func (a *PlasmaApi) GetRequiredPoWForAccountBlock(param GetRequiredParam) (*GetRequiredResult, error) {
	_, context, err := api.GetFrontierContext(a.chain, param.SelfAddr)
	frontierMomentum, err := context.GetFrontierMomentum()
	if err != nil {
		return nil, err
	}

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

	availablePlasma, err := vm.AvailablePlasma(context.MomentumStore(), context)
	if err != nil {
		return nil, err
	}

	basePlasma, err := vm.GetBasePlasmaForAccountBlock(context, block)
	if err != nil {
		return nil, err
	}

	if availablePlasma > basePlasma {
		return &GetRequiredResult{
			AvailablePlasma:    availablePlasma,
			BasePlasma:         basePlasma,
			RequiredDifficulty: 0,
		}, nil
	} else {
		difficulty, err := vm.GetDifficultyForPlasma(basePlasma - availablePlasma)
		if err != nil {
			return nil, err
		}
		return &GetRequiredResult{
			AvailablePlasma:    availablePlasma,
			BasePlasma:         basePlasma,
			RequiredDifficulty: difficulty,
		}, nil
	}
}
