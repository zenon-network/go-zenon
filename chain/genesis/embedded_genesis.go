package genesis

import (
	"encoding/json"

	"github.com/zenon-network/go-zenon/common"
)

func init() {
	if embeddedGenesis == nil && len(embeddedGenesisStr) != 0 {
		embeddedGenesis = new(GenesisConfig)
		common.DealWithErr(json.Unmarshal([]byte(embeddedGenesisStr), embeddedGenesis))
	}
}

var (
	embeddedGenesis *GenesisConfig
)
