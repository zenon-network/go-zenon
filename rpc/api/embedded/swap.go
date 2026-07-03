package embedded

import (
	"encoding/hex"
	"encoding/json"
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

// SwapApi implements the "embedded.swap" JSON-RPC namespace, which
// reads the legacy-network migration state seeded at Alphanet genesis:
// the ZNN and QSR balances still claimable through the swap embedded
// contract's RetrieveAssets method, keyed by the hash of the owning
// legacy key, and the legacy pillar registrations redeemable through
// the pillar contract. Unclaimed balances decay over time: they stay
// whole for the first 90 epochs after genesis, then lose 10% of the
// original amount per further 30 epochs until nothing is left. Every
// exported method is served as embedded.swap.<lowerCamelMethodName>.
type SwapApi struct {
	chain     chain.Chain
	consensus consensus.Consensus
	log       log15.Logger
}

// NewSwapApi returns a SwapApi bound to the given node's chain and
// consensus. It is called by the RPC server when the "embedded"
// namespace is enabled; it is not itself an RPC method.
func NewSwapApi(z zenon.Zenon) *SwapApi {
	return &SwapApi{
		chain:     z.Chain(),
		consensus: z.Consensus(),
		log:       common.RPCLogger.New("module", "rpc_api/embedded_swap_api"),
	}
}

// SwapAssetEntry is the claimable swap balance of one legacy key: the
// ZNN and QSR amounts in smallest units, after decay, and KeyIdHash,
// the hex form of the SHA-256 hash of the legacy key id (itself the
// RIPEMD-160 of the SHA-256 of the compressed secp256k1 public key).
type SwapAssetEntry struct {
	KeyIdHash string   `json:"keyIdHash"`
	Znn       *big.Int `json:"znn"`
	Qsr       *big.Int `json:"qsr"`
}

// SwapAssetEntryMarshal is the JSON wire form of SwapAssetEntry, with
// the ZNN and QSR amounts rendered as base-10 strings. It exists so the
// custom MarshalJSON/UnmarshalJSON of SwapAssetEntry can round-trip
// amounts without precision loss.
type SwapAssetEntryMarshal struct {
	KeyIdHash string `json:"keyIdHash"`
	Znn       string `json:"znn"`
	Qsr       string `json:"qsr"`
}

// ToSwapAssetEntryMarshal converts the entry to its JSON wire
// representation, rendering the ZNN and QSR amounts as base-10 strings.
func (s *SwapAssetEntry) ToSwapAssetEntryMarshal() *SwapAssetEntryMarshal {
	aux := &SwapAssetEntryMarshal{
		KeyIdHash: s.KeyIdHash,
		Znn:       s.Znn.String(),
		Qsr:       s.Qsr.String(),
	}

	return aux
}

// MarshalJSON encodes the entry via its SwapAssetEntryMarshal wire
// form, so the amounts appear as base-10 JSON strings.
func (s *SwapAssetEntry) MarshalJSON() ([]byte, error) {
	return json.Marshal(s.ToSwapAssetEntryMarshal())
}

// UnmarshalJSON decodes the SwapAssetEntryMarshal wire form produced by
// MarshalJSON. Amount strings that are not valid base-10 integers
// decode to 0 rather than producing an error.
func (s *SwapAssetEntry) UnmarshalJSON(data []byte) error {
	aux := new(SwapAssetEntryMarshal)
	if err := json.Unmarshal(data, aux); err != nil {
		return err
	}
	s.KeyIdHash = aux.KeyIdHash
	s.Znn = common.StringToBigInt(aux.Znn)
	s.Qsr = common.StringToBigInt(aux.Qsr)
	return nil
}

// SwapAssetEntrySimple is a SwapAssetEntry without the key id hash,
// used by GetAssets where the hash is already the map key: the
// claimable ZNN and QSR amounts in smallest units, after decay.
type SwapAssetEntrySimple struct {
	Znn *big.Int `json:"znn"`
	Qsr *big.Int `json:"qsr"`
}

// SwapAssetEntrySimpleMarshal is the JSON wire form of
// SwapAssetEntrySimple, with the ZNN and QSR amounts rendered as
// base-10 strings. It exists so the custom MarshalJSON/UnmarshalJSON of
// SwapAssetEntrySimple can round-trip amounts without precision loss.
type SwapAssetEntrySimpleMarshal struct {
	Znn string `json:"znn"`
	Qsr string `json:"qsr"`
}

// ToSwapAssetEntrySimpleMarshal converts the entry to its JSON wire
// representation, rendering the ZNN and QSR amounts as base-10 strings.
func (s *SwapAssetEntrySimple) ToSwapAssetEntrySimpleMarshal() *SwapAssetEntrySimpleMarshal {
	aux := &SwapAssetEntrySimpleMarshal{
		Znn: s.Znn.String(),
		Qsr: s.Qsr.String(),
	}

	return aux
}

// MarshalJSON encodes the entry via its SwapAssetEntrySimpleMarshal
// wire form, so the amounts appear as base-10 JSON strings.
func (s *SwapAssetEntrySimple) MarshalJSON() ([]byte, error) {
	return json.Marshal(s.ToSwapAssetEntrySimpleMarshal())
}

// UnmarshalJSON decodes the SwapAssetEntrySimpleMarshal wire form
// produced by MarshalJSON. Amount strings that are not valid base-10
// integers decode to 0 rather than producing an error.
func (s *SwapAssetEntrySimple) UnmarshalJSON(data []byte) error {
	aux := new(SwapAssetEntrySimpleMarshal)
	if err := json.Unmarshal(data, aux); err != nil {
		return err
	}
	s.Znn = common.StringToBigInt(aux.Znn)
	s.Qsr = common.StringToBigInt(aux.Qsr)
	return nil
}

// SwapLegacyPillarEntry counts the legacy pillar slots still
// registered under one legacy key: KeyIdHash is the hex form of the
// SHA-256 hash of the legacy key id and NumPillars is how many pillars
// that key may still redeem.
type SwapLegacyPillarEntry struct {
	KeyIdHash  string `json:"keyIdHash"`
	NumPillars int    `json:"numPillars"`
}

// === Swap Assets ===

// GetAssetsByKeyIdHash returns the swap balance still claimable by the
// legacy key with the given key id hash, read from contract state at
// the frontier momentum with decay applied for the current epoch. A
// hash with no swap entry yields zero amounts, not an error.
//
// JSON-RPC: embedded.swap.getAssetsByKeyIdHash
func (p *SwapApi) GetAssetsByKeyIdHash(keyIdHash types.Hash) (*SwapAssetEntry, error) {
	m, context, err := api.GetFrontierContext(p.chain, types.SwapContract)
	if err != nil {
		return nil, err
	}

	entry, err := definition.GetSwapAssetsByKeyIdHash(context.Storage(), keyIdHash)
	if err == constants.ErrDataNonExistent {
		return &SwapAssetEntry{
			KeyIdHash: keyIdHash.String(),
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
		KeyIdHash: keyIdHash.String(),
		Znn:       entry.Znn,
		Qsr:       entry.Qsr,
	}, nil
}

// GetAssets returns every remaining swap balance, keyed by key id hash,
// read from contract state at the frontier momentum with decay applied
// for the current epoch. The result is a JSON object, not a paged list.
//
// JSON-RPC: embedded.swap.getAssets
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

// GetLegacyPillars returns the legacy pillar slots not yet redeemed,
// one entry per legacy key, read from the pillar contract's state (not
// the swap contract's) at the frontier momentum, in storage (key id
// hash) order.
//
// JSON-RPC: embedded.swap.getLegacyPillars
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
