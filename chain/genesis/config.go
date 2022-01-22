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

func MakeEmbeddedGenesisConfig() (store.Genesis, error) {
	if embeddedGenesis == nil {
		return nil, ErrNoEmbeddedGenesis
	}
	return NewGenesis(embeddedGenesis), nil
}

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

type GenesisConfig struct {
	ChainIdentifier     uint64
	ExtraData           string
	GenesisTimestampSec int64
	SporkAddress        *types.Address

	PillarConfig *PillarContractConfig
	TokenConfig  *TokenContractConfig
	PlasmaConfig *PlasmaContractConfig
	SwapConfig   *SwapContractConfig
	SporkConfig  *SporkConfig

	GenesisBlocks *GenesisBlocksConfig
}

type PillarContractConfig struct {
	Pillars       []*definition.PillarInfo
	Delegations   []*definition.DelegationInfo
	LegacyEntries []*definition.LegacyPillarEntry
}

type TokenContractConfig struct {
	Tokens []*definition.TokenInfo
}

type PlasmaContractConfig struct {
	Fusions []*definition.FusionInfo
}

type SwapContractConfig struct {
	Entries []*definition.SwapAssets
}

type GenesisBlocksConfig struct {
	Blocks []*GenesisBlockConfig
}

type GenesisBlockConfig struct {
	Address     types.Address
	BalanceList map[types.ZenonTokenStandard]*big.Int
}

type SporkConfig struct {
	Sporks []*definition.Spork
}
