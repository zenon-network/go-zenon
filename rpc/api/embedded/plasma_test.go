package embedded

import (
	"encoding/json"
	"math/big"
	"testing"

	"github.com/zenon-network/go-zenon/chain"
	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/chain/store"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/dp"
	"github.com/zenon-network/go-zenon/vm/constants"
)

// Synthetic stores call the real RPC handler without networking, global spork
// mutation or block admission. Unexpected store calls fail through the embedded
// nil interface instead of silently inventing state.
type requiredPoWQuoteChain struct {
	chain.Chain
	momentum store.Momentum
	account  store.Account
	cache    store.Cache
	// cacheAtFrontier is what the cache frontier shows; the quote fixtures
	// make it disagree with cache so a handler reading the wrong one is
	// caught (an insert in flight commits the cache before the momentum).
	cacheAtFrontier store.Cache
}

func (c *requiredPoWQuoteChain) GetFrontierMomentumStore() store.Momentum { return c.momentum }
func (c *requiredPoWQuoteChain) GetFrontierAccountStore(types.Address) store.Account {
	return c.account
}

func (c *requiredPoWQuoteChain) GetFrontierCacheStore() store.Cache { return c.cacheAtFrontier }

// GetCacheStore serves the cache only for the captured frontier momentum:
// the handler must pin its price to the momentum it quotes against, not to
// whatever the cache frontier is when it looks (an insert in flight commits
// the cache before the momentum). Any other identifier yields nil, which the
// handler reports as an error.
func (c *requiredPoWQuoteChain) GetCacheStore(identifier types.HashHeight) store.Cache {
	if identifier != c.momentum.(*requiredPoWQuoteMomentum).frontier.Identifier() {
		return nil
	}
	return c.cache
}

type requiredPoWQuoteMomentum struct {
	store.Momentum
	frontier *nom.Momentum
}

func (s *requiredPoWQuoteMomentum) GetFrontierMomentum() (*nom.Momentum, error) {
	copy := *s.frontier
	return &copy, nil
}

type requiredPoWQuoteAccount struct {
	store.Account
	address types.Address
}

func (s *requiredPoWQuoteAccount) Address() *types.Address { return &s.address }
func (s *requiredPoWQuoteAccount) GetChainPlasma() (*big.Int, error) {
	return new(big.Int), nil
}

type requiredPoWQuoteCache struct {
	store.Cache
	active bool
	fused  *big.Int
}

func (s *requiredPoWQuoteCache) IsSporkActive(spork *types.ImplementedSpork) (bool, error) {
	return s.active && spork == types.DynamicPlasmaSpork, nil
}
func (s *requiredPoWQuoteCache) GetChainPlasma(types.Address) (*big.Int, error) {
	return new(big.Int), nil
}
func (s *requiredPoWQuoteCache) GetStakeBeneficialAmount(types.Address) (*big.Int, error) {
	return new(big.Int).Set(s.fused), nil
}

func requiredPoWQuote(t *testing.T, active bool, version, fusionPrice, workPrice, units uint64, data []byte) (*GetRequiredResult, *nom.Momentum) {
	t.Helper()
	self, recipient := types.Address{0, 0x71}, types.Address{0, 0x72}
	frontier := &nom.Momentum{
		Version: version, Height: 100,
		NextFusionPrice: fusionPrice, NextWorkPrice: workPrice,
	}
	fused := new(big.Int).Mul(new(big.Int).SetUint64(units), new(big.Int).SetUint64(constants.CostPerFusionUnit))
	a := &PlasmaApi{chain: &requiredPoWQuoteChain{
		momentum: &requiredPoWQuoteMomentum{frontier: frontier},
		account:  &requiredPoWQuoteAccount{address: self},
		cache:    &requiredPoWQuoteCache{active: active, fused: fused},
		// the cache frontier disagrees on both spork state and fusion
		cacheAtFrontier: &requiredPoWQuoteCache{active: !active, fused: new(big.Int)},
	}}
	result, err := a.GetRequiredPoWForAccountBlock(GetRequiredParam{
		SelfAddr: self, ToAddr: &recipient, BlockType: nom.BlockTypeUserSend, Data: data,
	})
	if err != nil || result == nil {
		t.Fatalf("RPC quote = %v, error %v", result, err)
	}
	return result, frontier
}

func requiredFusedPlasmaFromJSON(t *testing.T, result *GetRequiredResult) uint64 {
	t.Helper()
	raw, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	value, ok := fields["requiredFusedPlasma"]
	if !ok {
		t.Fatalf("requiredFusedPlasma is absent from RPC JSON: %s", raw)
	}
	if len(fields) != 4 || fields["availablePlasma"] == nil || fields["basePlasma"] == nil || fields["requiredDifficulty"] == nil {
		t.Fatalf("unexpected RPC result fields: %s", raw)
	}
	var fused uint64
	if err := json.Unmarshal(value, &fused); err != nil {
		t.Fatalf("requiredFusedPlasma = %s: %v", value, err)
	}
	var decoded GetRequiredResult
	if err := json.Unmarshal(raw, &decoded); err != nil || decoded != *result {
		t.Fatalf("RPC JSON round trip = %+v, error %v; want %+v", decoded, err, *result)
	}
	return fused
}

func TestGetRequiredPoWFusedPlasmaTarget(t *testing.T) {
	for _, tc := range []struct {
		name                                               string
		active                                             bool
		version, fusion, work, units                       uint64
		data                                               []byte
		wantAvailable, wantBase, wantDifficulty, wantFused uint64
	}{
		{"dynamic-floor", true, 2, 1000, 1000, 12, nil, 25200, 21000, 0, 21000},
		{"dynamic-fused-only", true, 2, 1050, 1100, 12, nil, 25200, 21000, 0, 22050},
		{"dynamic-one-byte", true, 2, 1050, 1100, 12, []byte{1}, 25200, 21068, 0, 22122},
		{"dynamic-partial-fusion", true, 2, 1050, 1100, 9, nil, 18900, 21000, 4950000, 22050},
		{"dynamic-no-fusion", true, 2, 1050, 1100, 0, nil, 0, 21000, 34650000, 22050},
		{"before-first-v2", true, 1, 0, 0, 12, nil, 25200, 21000, 0, 21000},
		{"before-first-v2-partial", true, 1, 0, 0, 9, nil, 18900, 21000, 3150000, 21000},
		// Enforced but before the first v2 momentum: the proof-of-work cap is
		// already the Dynamic Plasma one, so a shortfall above the legacy cap
		// (94,500) is quoted rather than rejected.
		{"before-first-v2-above-legacy-pow-cap", true, 1, 0, 0, 0, make([]byte, 1100), 0, 95800, 143700000, 95800},
		// Enforced but before the first v2 momentum: available plasma follows
		// the Dynamic Plasma fusion cap (legacy would cap 6000 units at 5000).
		{"before-first-v2-above-legacy-fusion-cap", true, 1, 0, 0, 6000, nil, 12600000, 21000, 0, 21000},
		{"before-enforcement", false, 1, 0, 0, 12, nil, 25200, 21000, 0, 21000},
		{"before-enforcement-partial", false, 1, 0, 0, 9, nil, 18900, 21000, 3150000, 21000},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result, _ := requiredPoWQuote(t, tc.active, tc.version, tc.fusion, tc.work, tc.units, tc.data)
			if result.AvailablePlasma != tc.wantAvailable || result.BasePlasma != tc.wantBase || result.RequiredDifficulty != tc.wantDifficulty {
				t.Fatalf("quote %+v, want available=%d base=%d difficulty=%d", result, tc.wantAvailable, tc.wantBase, tc.wantDifficulty)
			}
			if got := requiredFusedPlasmaFromJSON(t, result); got != tc.wantFused {
				t.Fatalf("requiredFusedPlasma = %d, want total fused-only target %d", got, tc.wantFused)
			}
		})
	}
}

func TestGetRequiredPoWFusedOnlyTargetMeetsPrice(t *testing.T) {
	for _, tc := range []struct {
		name string
		data []byte
		want uint64
	}{
		{"plain-send", nil, 22050},
		{"ceil-not-floor", []byte{1}, 22122},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result, momentum := requiredPoWQuote(t, true, 2, 1050, 1100, 12, tc.data)
			fused := requiredFusedPlasmaFromJSON(t, result)
			if result.RequiredDifficulty != 0 || fused != tc.want {
				t.Fatalf("quote %+v, fused %d; want difficulty 0 and fused %d", result, fused, tc.want)
			}
			checker := dp.NewDynamicPlasma(momentum, nil)
			block := &nom.AccountBlock{Address: types.Address{0, 0x71}, BasePlasma: result.BasePlasma, FusedPlasma: fused}
			if !checker.ValidPrice(block) {
				t.Fatalf("RPC fused-only target %d does not meet the price", fused)
			}
			block.FusedPlasma--
			if checker.ValidPrice(block) {
				t.Fatal("one below the RPC fused-only target meets the price")
			}
		})
	}
}

func TestGetRequiredResultJSONIncludesZeroFusedTarget(t *testing.T) {
	if got := requiredFusedPlasmaFromJSON(t, new(GetRequiredResult)); got != 0 {
		t.Fatalf("zero result fused target = %d", got)
	}
}

// The slice was sized from the receiver's OLD length but indexed over the
// decoded aux entries - decoding a non-empty list into a fresh receiver
// panicked with index out of range.
func TestFusionEntryListUnmarshalFreshReceiver(t *testing.T) {
	original := &FusionEntryList{
		QsrAmount: big.NewInt(5000),
		Count:     1,
		Fusions: []*FusionEntry{{
			QsrAmount:        big.NewInt(5000),
			Beneficiary:      types.PlasmaContract,
			ExpirationHeight: 42,
			Id:               types.NewHash([]byte{1}),
			IsRevocable:      true,
		}},
	}
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}

	decoded := new(FusionEntryList)
	if err := json.Unmarshal(data, decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded.Fusions) != 1 {
		t.Fatalf("decoded %d fusions, want 1", len(decoded.Fusions))
	}
	reencoded, err := json.Marshal(decoded)
	if err != nil {
		t.Fatal(err)
	}
	if string(reencoded) != string(data) {
		t.Fatalf("round-trip mismatch:\n got  %s\n want %s", reencoded, data)
	}
}

// The quote reads the cache at the frontier momentum it captured; when the
// chain has no cache for that identifier the quote fails instead of pricing
// against some other state.
func TestGetRequiredPoWMissingCapturedFrontierCache(t *testing.T) {
	self, recipient := types.Address{0, 0x71}, types.Address{0, 0x72}
	a := &PlasmaApi{chain: &requiredPoWQuoteChain{
		momentum:        &requiredPoWQuoteMomentum{frontier: &nom.Momentum{Version: 2, Height: 100}},
		account:         &requiredPoWQuoteAccount{address: self},
		cache:           nil, // GetCacheStore returns nil for the captured identifier
		cacheAtFrontier: &requiredPoWQuoteCache{active: true, fused: new(big.Int)},
	}}
	result, err := a.GetRequiredPoWForAccountBlock(GetRequiredParam{SelfAddr: self, ToAddr: &recipient, BlockType: nom.BlockTypeUserSend})
	if err == nil || result != nil {
		t.Fatalf("quote without a cache for the captured frontier = %+v, %v; want an error", result, err)
	}
}
