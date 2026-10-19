package pillar

import (
	"math"
	"testing"

	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/dp"
	"github.com/zenon-network/go-zenon/vm/constants"
	"github.com/zenon-network/go-zenon/vm/embedded/definition"
)

func TestUpdateFusedPlasma(t *testing.T) {
	config := &definition.PlasmaVariables{MaxBasePlasmaInMomentum: definition.DefaultMaxBasePlasmaInMomentum}
	for _, workPrice := range []uint64{1000, 2500} {
		for _, fusionPrice := range []uint64{1000, 1001, 1100, 3750, 4120, 379069} {
			plasma := dp.NewDynamicPlasma(&nom.Momentum{
				NextFusionPrice: fusionPrice,
				NextWorkPrice:   workPrice,
				Version:         nom.DynamicPlasmaMomentumVersion,
			}, config)
			for _, basePlasma := range []uint64{constants.EmbeddedSimplePlasma, constants.EmbeddedWResponse, constants.EmbeddedWDoubleResponse} {
				withHeadroom, err := updateFusedPlasma(basePlasma, fusionPrice, math.MaxUint64)
				common.FailIfErr(t, err)
				common.ExpectUint64(t, withHeadroom%updateFusionPriceHeadroom, 0)
				floor := withHeadroom / updateFusionPriceHeadroom
				common.ExpectTrue(t, plasma.ValidPrice(&nom.AccountBlock{
					BlockType:   nom.BlockTypeUserSend,
					BasePlasma:  basePlasma,
					FusedPlasma: floor,
				}))
				if fusionPrice == dp.MinResourcePrice {
					common.ExpectUint64(t, floor, basePlasma)
				}

				fused, err := updateFusedPlasma(basePlasma, fusionPrice, withHeadroom-1)
				common.FailIfErr(t, err)
				common.ExpectUint64(t, fused, floor)

				fused, err = updateFusedPlasma(basePlasma, fusionPrice, floor)
				common.FailIfErr(t, err)
				common.ExpectUint64(t, fused, floor)

				_, err = updateFusedPlasma(basePlasma, fusionPrice, floor-1)
				common.ExpectError(t, err, constants.ErrNotEnoughPlasma)
			}
		}
	}
}
