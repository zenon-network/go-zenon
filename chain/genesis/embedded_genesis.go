package genesis

import (
	"encoding/json"

	"github.com/zenon-network/go-zenon/common"
)

// init decodes the embedded JSON payload from
// embedded_genesis_string.go into embeddedGenesis; a malformed
// payload panics at startup since the binary itself would be broken.
func init() {
	if embeddedGenesis == nil && len(embeddedGenesisStr) != 0 {
		embeddedGenesis = new(GenesisConfig)
		common.DealWithErr(json.Unmarshal([]byte(embeddedGenesisStr), embeddedGenesis))
	}
}

var (
	// embeddedGenesis is the decoded built-in genesis config (the
	// alphanet config in official builds), or nil when the build
	// carries none; served by MakeEmbeddedGenesisConfig.
	embeddedGenesis *GenesisConfig
)
