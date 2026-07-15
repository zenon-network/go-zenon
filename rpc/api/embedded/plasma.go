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
	"github.com/zenon-network/go-zenon/dp"
	"github.com/zenon-network/go-zenon/rpc/api"
	"github.com/zenon-network/go-zenon/vm"
	"github.com/zenon-network/go-zenon/vm/constants"
	"github.com/zenon-network/go-zenon/vm/embedded/definition"
	"github.com/zenon-network/go-zenon/zenon"
)

type PlasmaApi struct {
	chain chain.Chain
	z     zenon.Zenon
	cs    consensus.Consensus
	log   log15.Logger
}

func NewPlasmaApi(z zenon.Zenon) *PlasmaApi {
	return &PlasmaApi{
		chain: z.Chain(),
		z:     z,
		cs:    z.Consensus(),
		log:   common.RPCLogger.New("module", "embedded_plasma_api"),
	}
}

type PlasmaInfo struct {
	CurrentPlasma uint64   `json:"currentPlasma"`
	MaxPlasma     uint64   `json:"maxPlasma"`
	QsrAmount     *big.Int `json:"qsrAmount"`
}
type PlasmaInfoMarshal struct {
	CurrentPlasma uint64 `json:"currentPlasma"`
	MaxPlasma     uint64 `json:"maxPlasma"`
	QsrAmount     string `json:"qsrAmount"`
}

func (r *PlasmaInfo) ToPlasmaInfoMarshal() *PlasmaInfoMarshal {
	aux := &PlasmaInfoMarshal{
		CurrentPlasma: r.CurrentPlasma,
		MaxPlasma:     r.MaxPlasma,
		QsrAmount:     r.QsrAmount.String(),
	}

	return aux
}

func (r *PlasmaInfo) MarshalJSON() ([]byte, error) {
	return json.Marshal(r.ToPlasmaInfoMarshal())
}

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

type FusionEntry struct {
	QsrAmount        *big.Int      `json:"qsrAmount"`
	Beneficiary      types.Address `json:"beneficiary"`
	ExpirationHeight uint64        `json:"expirationHeight"`
	Id               types.Hash    `json:"id"`
	IsRevocable      bool          `json:"isRevocable"`
}
type FusionEntryMarshal struct {
	QsrAmount        string        `json:"qsrAmount"`
	Beneficiary      types.Address `json:"beneficiary"`
	ExpirationHeight uint64        `json:"expirationHeight"`
	Id               types.Hash    `json:"id"`
	IsRevocable      bool          `json:"isRevocable"`
}

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

func (r *FusionEntry) MarshalJSON() ([]byte, error) {
	return json.Marshal(r.ToFusionEntryMarshal())
}

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

type FusionEntryList struct {
	QsrAmount *big.Int       `json:"qsrAmount"`
	Count     int            `json:"count"`
	Fusions   []*FusionEntry `json:"list"`
}
type FusionEntryListMarshal struct {
	QsrAmount string         `json:"qsrAmount"`
	Count     int            `json:"count"`
	Fusions   []*FusionEntry `json:"list"`
}

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

func (r *FusionEntryList) MarshalJSON() ([]byte, error) {
	return json.Marshal(r.ToFusionEntryListMarshal())
}

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

type SortFusionEntryByHeight []*definition.FusionInfo

func (a SortFusionEntryByHeight) Len() int      { return len(a) }
func (a SortFusionEntryByHeight) Swap(i, j int) { a[i], a[j] = a[j], a[i] }
func (a SortFusionEntryByHeight) Less(i, j int) bool {
	if a[i].ExpirationHeight == a[j].ExpirationHeight {
		return a[i].Beneficiary.String() < a[j].Beneficiary.String()
	}
	return a[i].ExpirationHeight < a[j].ExpirationHeight
}

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

type GetRequiredParam struct {
	SelfAddr  types.Address  `json:"address"`
	BlockType uint64         `json:"blockType"`
	ToAddr    *types.Address `json:"toAddress"`
	Data      []byte         `json:"data"`
}
type GetRequiredResult struct {
	AvailablePlasma    uint64 `json:"availablePlasma"`
	BasePlasma         uint64 `json:"basePlasma"`
	RequiredDifficulty uint64 `json:"requiredDifficulty"`
}

func (a *PlasmaApi) GetRequiredPoWForAccountBlock(param GetRequiredParam) (*GetRequiredResult, error) {
	_, context, err := api.GetFrontierContext(a.chain, param.SelfAddr)
	if err != nil {
		return nil, err
	}
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

	basePlasma, err := vm.GetBasePlasmaForAccountBlock(context, block)
	if err != nil {
		return nil, err
	}

	var availablePlasma uint64
	var requiredFusedPlasma uint64

	// NextFusionPrice/NextWorkPrice are only meaningful once the frontier
	// momentum itself was produced under dynamic plasma; the spork can be
	// enforced for one momentum before the first v2 momentum lands.
	dynamicPlasmaActive := context.IsDynamicPlasmaSporkEnforced() && frontierMomentum.Version >= nom.DynamicPlasmaMomentumVersion

	if dynamicPlasmaActive {
		availablePlasma, err = vm.AvailablePlasmaV2(context.CacheStore(), context)
		if err != nil {
			return nil, err
		}
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
		availablePlasma, err = vm.AvailablePlasma(context.CacheStore(), context)
		if err != nil {
			return nil, err
		}
		requiredFusedPlasma = basePlasma
	}

	if availablePlasma >= requiredFusedPlasma {
		return &GetRequiredResult{
			AvailablePlasma:    availablePlasma,
			BasePlasma:         basePlasma,
			RequiredDifficulty: 0,
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
		} else {
			requiredDifficulty, err = vm.GetDifficultyForPlasma(basePlasma - availablePlasma)
			if err != nil {
				return nil, err
			}
		}
		return &GetRequiredResult{
			AvailablePlasma:    availablePlasma,
			BasePlasma:         basePlasma,
			RequiredDifficulty: requiredDifficulty,
		}, nil
	}
}

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
