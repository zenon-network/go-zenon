package genesis

import (
	"os"
	"path"
	"testing"

	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/types"
)

var (
	emptyGenesisJsonStr = []byte(`
{
	"ChainIdentifier": 100,
	"ExtraData": "",
	"GenesisTimestampSec": 1000000000,
	"SporkAddress": "z1qqv2fnc3avjg39dcste4c5lag7l42xyykjf49w",
	"PillarConfig": {
		"Pillars": [],
		"Delegations": [],
		"LegacyEntries": []
	},
	"TokenConfig": {
		"Tokens": []
	},
	"PlasmaConfig": {
		"Fusions": []
	},
	"SwapConfig": {
		"Entries": []
	},
	"SporkConfig": {
		"Sporks": []
	},
	"GenesisBlocks": {
		"Blocks": []
	}
}`)
	emptyHash = types.HexToHashPanic("30a9c36aa27d0b441eff8328a277e417ae0f1661f298f6376858f6819492811a")
)

func TestGenesisToFromJson(t *testing.T) {
	genesisFile := path.Join(t.TempDir(), "genesis.json")
	err := os.WriteFile(genesisFile, emptyGenesisJsonStr, 777)
	common.FailIfErr(t, err)

	config, err := ReadGenesisConfigFromFile(genesisFile)
	common.FailIfErr(t, err)
	common.ExpectString(t, config.GetGenesisMomentum().Hash.String(), emptyHash.String())
}

func TestPartiallyWrittenFile(t *testing.T) {
	for i := 0; i < len(emptyGenesisJsonStr)-5; i += 1 {
		genesisFile := path.Join(t.TempDir(), "genesis.json")
		if err := os.WriteFile(genesisFile, emptyGenesisJsonStr[0:i], 777); err != nil {
			t.Fatal(err)
		}
		if _, err := ReadGenesisConfigFromFile(genesisFile); err != ErrIncompleteGenesisJson {
			t.Fatalf("Unexpected error %v `%v`", err, emptyGenesisJsonStr[0:i])
		}
	}
}

func TestMalformedGenesis(t *testing.T) {
	genesisFile := path.Join(t.TempDir(), "genesis.json")
	if err := os.WriteFile(genesisFile, []byte(`{"a":"aaaa"]}`), 777); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadGenesisConfigFromFile(genesisFile); err != ErrInvalidGenesisJson {
		t.Fatalf("Unexpected error %v", err)
	}
}

func TestEmptyGenesis(t *testing.T) {
	genesisFile := path.Join(t.TempDir(), "genesis.json")
	if err := os.WriteFile(genesisFile, []byte(`{}`), 777); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadGenesisConfigFromFile(genesisFile); err != ErrInvalidGenesisConfig {
		t.Fatalf("Unexpected error %v", err)
	}
}
