package genesis

import (
	"encoding/json"
	"math/big"
	"os"

	"github.com/zenon-network/go-zenon/chain/store"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/vm/embedded/definition"
)

var (
	log = common.ChainLogger.New("submodule", "genesis")
)

// MakeEmbeddedGenesisConfig builds the genesis from the alphanet
// config embedded in the binary (see embedded_genesis_string.go),
// or returns ErrNoEmbeddedGenesis when the codebase was built
// without one. It is the node's fallback when no genesis file is
// configured; the embedded config is trusted and not re-validated.
func MakeEmbeddedGenesisConfig() (store.Genesis, error) {
	if embeddedGenesis == nil {
		return nil, ErrNoEmbeddedGenesis
	}
	return NewGenesis(embeddedGenesis), nil
}

// ReadGenesisConfigFromFile decodes a GenesisConfig from the JSON
// file at genesisFile, validates it with CheckGenesis and builds the
// genesis state from it. An empty path returns (nil, nil) so the
// caller can fall back to the embedded genesis. Explicit open,
// decode and validation failures are logged at Crit level and return
// the corresponding Err* sentinel; a panic while building the state
// (inside NewGenesis) is recovered and logged, but the function then
// returns (nil, nil) — indistinguishable from the empty-path case.
func ReadGenesisConfigFromFile(genesisFile string) (store.Genesis, error) {
	defer func() {
		if err := recover(); err != nil {
			log.Crit("invalid genesis file", "method", "readGenesis", "genesisFile", genesisFile)
		}
	}()

	var config *GenesisConfig

	if len(genesisFile) > 0 {
		file, err := os.Open(genesisFile)
		if err != nil {
			log.Crit("invalid genesis file", "method", "readGenesis", "reason", err, "genesisFile", genesisFile)
			return nil, ErrInvalidGenesisPath
		}
		defer file.Close()

		config = new(GenesisConfig)
		if err := json.NewDecoder(file).Decode(config); err != nil {
			log.Crit("invalid genesis file", "method", "readGenesis", "reason", err, "genesisFile", genesisFile)
			if err.Error() == "unexpected EOF" || err.Error() == "EOF" {
				return nil, ErrIncompleteGenesisJson
			} else {
				return nil, ErrInvalidGenesisJson
			}
		}

		if err := CheckGenesis(config); err != nil {
			log.Crit("invalid genesis file", "method", "readGenesis", "reason", err, "genesisFile", genesisFile)
			return nil, ErrInvalidGenesisConfig
		}
		return NewGenesis(config), nil
	} else {
		return nil, nil
	}
}

// GenesisConfig is the JSON-serializable description of a network's
// initial state, consumed by NewGenesis. The per-contract sections
// seed the storage of the corresponding embedded contracts;
// GenesisBlocks assigns the initial token balances. All sections
// except SporkConfig are mandatory (see CheckFieldsExist), and the
// balances must be consistent with the contract sections (see
// CheckGenesis).
type GenesisConfig struct {
	// ChainIdentifier distinguishes networks: it is stamped into
	// every genesis block and momentum and must be matched by all
	// later blocks.
	ChainIdentifier uint64
	// ExtraData becomes the Data field of the genesis momentum.
	ExtraData string
	// GenesisTimestampSec is the Unix timestamp of the genesis
	// momentum; consensus aligns its tick schedule to it.
	GenesisTimestampSec int64
	// SporkAddress is the only address allowed to create and
	// activate sporks, the mechanism that gates protocol upgrades.
	SporkAddress *types.Address

	PillarConfig *PillarContractConfig
	TokenConfig  *TokenContractConfig
	PlasmaConfig *PlasmaContractConfig
	SwapConfig   *SwapContractConfig
	// SporkConfig optionally pre-activates sporks at genesis; when
	// nil, the spork contract starts empty.
	SporkConfig *SporkConfig

	GenesisBlocks *GenesisBlocksConfig
}

// PillarContractConfig seeds the pillar embedded contract: the
// initial pillars (each also stored as a producing-pillar entry
// keyed by its block-producing address), the initial delegations and
// the legacy-pillar entries redeemable through the swap mechanism.
// The pillar contract's ZNN balance in GenesisBlocks must equal the
// sum of the pillars' staked amounts.
type PillarContractConfig struct {
	Pillars       []*definition.PillarInfo
	Delegations   []*definition.DelegationInfo
	LegacyEntries []*definition.LegacyPillarEntry
}

// TokenContractConfig seeds the token embedded contract with the
// initial ZTS tokens (ZNN and QSR on alphanet). Each token's
// TotalSupply must equal the sum distributed via GenesisBlocks.
type TokenContractConfig struct {
	Tokens []*definition.TokenInfo
}

// PlasmaContractConfig seeds the plasma embedded contract with the
// initial QSR fusions; the per-beneficiary fused amounts are
// aggregated at genesis-block construction. The plasma contract's
// QSR balance in GenesisBlocks must equal the total fused amount.
type PlasmaContractConfig struct {
	Fusions []*definition.FusionInfo
}

// SwapContractConfig seeds the swap embedded contract with the
// legacy-network balances redeemable per key-id hash; the swapped
// funds are minted at retrieval rather than pre-funded.
type SwapContractConfig struct {
	Entries []*definition.SwapAssets
}

// GenesisBlocksConfig lists every address that receives a genesis
// account block; addresses whose contract sections already produce a
// block (the pillar, token, plasma, swap and spork contracts) have
// their balances folded into that block instead.
type GenesisBlocksConfig struct {
	Blocks []*GenesisBlockConfig
}

// GenesisBlockConfig assigns Address its initial balance per ZTS
// token, realized as a height-1 BlockTypeGenesisReceive block.
// Amounts are in the token's base units (10^8 per ZNN or QSR).
type GenesisBlockConfig struct {
	Address     types.Address
	BalanceList map[types.ZenonTokenStandard]*big.Int
}

// SporkConfig lists sporks stored in the spork contract at genesis,
// allowing a new network to start with features already activated.
type SporkConfig struct {
	Sporks []*definition.Spork
}
