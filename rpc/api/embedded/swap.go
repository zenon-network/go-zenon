package embedded

import (
	"encoding/hex"
	"math/big"

	"github.com/inconshreveable/log15"

	"github.com/zenon-network/go-zenon/chain"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/consensus"
	"github.com/zenon-network/go-zenon/rpc/api"
	"github.com/zenon-network/go-zenon/vm/constants"
	"github.com/zenon-network/go-zenon/vm/embedded/definition"
	"github.com/zenon-network/go-zenon/vm/embedded/implementation"
	"github.com/zenon-network/go-zenon/zenon"
)

type SwapApi struct {
	chain     chain.Chain
	consensus consensus.Consensus
	log       log15.Logger
}

func NewSwapApi(z zenon.Zenon) *SwapApi {
	return &SwapApi{
		chain:     z.Chain(),
		consensus: z.Consensus(),
		log:       common.RPCLogger.New("module", "rpc_api/embedded_swap_api"),
	}
}

type SwapAssetEntry struct {
	KeyIdHash string   `json:"keyIdHash"`
	Znn       *big.Int `json:"znn"`
	Qsr       *big.Int `json:"qsr"`
}
type SwapAssetEntrySimple struct {
	Znn *big.Int `json:"znn"`
	Qsr *big.Int `json:"qsr"`
}
type SwapLegacyPillarEntry struct {
	KeyIdHash  string `json:"keyIdHash"`
	NumPillars int    `json:"numPillars"`
}

// === Swap Assets ===

func (p *SwapApi) GetAssetsByKeyIdHash(keyIdHas types.Hash) (*SwapAssetEntry, error) {
	m, context, err := api.GetFrontierContext(p.chain, types.SwapContract)
	if err != nil {
		return nil, err
	}

	entry, err := definition.GetSwapAssetsByKeyIdHash(context.Storage(), keyIdHas)
	if err == constants.ErrDataNonExistent {
		return &SwapAssetEntry{
			KeyIdHash: keyIdHas.String(),
			Znn:       common.Big0,
			Qsr:       common.Big0,
		}, nil
	}
	if err != nil {
		return nil, err
	}

	currentM, err := context.GetFrontierMomentum()
	common.DealWithErr(err)
	currentEpoch := int(p.consensus.FixedPillarReader(m.Identifier()).EpochTicker().ToTick(*currentM.Timestamp))
	implementation.ApplyDecay(entry, currentEpoch)
	return &SwapAssetEntry{
		KeyIdHash: keyIdHas.String(),
		Znn:       entry.Znn,
		Qsr:       entry.Qsr,
	}, nil
}
func (p *SwapApi) GetAssets() (map[types.Hash]*SwapAssetEntrySimple, error) {
	m, context, err := api.GetFrontierContext(p.chain, types.SwapContract)
	if err != nil {
		return nil, err
	}

	listRaw, err := definition.GetSwapAssets(context.Storage())
	if err != nil {
		return nil, err
	}

	result := make(map[types.Hash]*SwapAssetEntrySimple, len(listRaw))
	currentM, err := context.GetFrontierMomentum()
	common.DealWithErr(err)
	currentEpoch := int(p.consensus.FixedPillarReader(m.Identifier()).EpochTicker().ToTick(*currentM.Timestamp))
	for _, entry := range listRaw {
		implementation.ApplyDecay(entry, currentEpoch)
		result[entry.KeyIdHash] = &SwapAssetEntrySimple{
			Znn: entry.Znn,
			Qsr: entry.Qsr,
		}
	}

	return result, nil
}

// === Swap Legacy Pillars ===

func (p *SwapApi) GetLegacyPillars() ([]*SwapLegacyPillarEntry, error) {
	_, context, err := api.GetFrontierContext(p.chain, types.PillarContract)
	if err != nil {
		return nil, err
	}
	entries, err := definition.GetLegacyPillarList(context.Storage())
	if err != nil {
		return nil, err
	}

	result := make([]*SwapLegacyPillarEntry, len(entries))

	for itr, entry := range entries {
		result[itr] = &SwapLegacyPillarEntry{
			NumPillars: int(entry.PillarCount),
			KeyIdHash:  hex.EncodeToString(entry.KeyIdHash[:]),
		}
	}
	return result, nil
}
