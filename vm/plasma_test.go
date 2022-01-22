package vm

import (
	"testing"

	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/vm/constants"
)

func TestGetDifficultyForPlasma(t *testing.T) {
	common.Json(GetDifficultyForPlasma(21000*4.5)).Equals(t, "141750000")
	common.Json(GetDifficultyForPlasma(constants.AlphanetPlasmaTable.EmbeddedSimple)).Equals(t, "78750000")
	common.Json(GetDifficultyForPlasma(constants.AlphanetPlasmaTable.EmbeddedWWithdraw)).Equals(t, "110250000")
	common.Json(GetDifficultyForPlasma(constants.AlphanetPlasmaTable.EmbeddedWDoubleWithdraw)).Equals(t, "141750000")
}
