package pillar

import (
	"math"
	"testing"

	"github.com/zenon-network/go-zenon/chain"
	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/chain/store"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/dp"
	"github.com/zenon-network/go-zenon/vm/constants"
	"github.com/zenon-network/go-zenon/vm/embedded/definition"
	"github.com/zenon-network/go-zenon/wallet"
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

type poolWithoutPredecessor struct {
	chain.Chain
	asked []types.HashHeight
}

func (p *poolWithoutPredecessor) GetAccountStore(_ types.Address, identifier types.HashHeight) store.Account {
	p.asked = append(p.asked, identifier)
	return nil
}

func TestPriceUpdate_ReplacedPredecessorMissing(t *testing.T) {
	producer := types.ParseAddressPanic("z1qz8v73ea2vy2rrlq7skssngu8cm8mknjjkr2ju")
	pool := &poolWithoutPredecessor{}
	w := &worker{
		log:      common.PillarLogger.New("submodule", "worker"),
		coinbase: &wallet.KeyPair{Address: producer},
		chain:    pool,
	}
	replaced := newUpdateTemplate(producer, types.PillarContract)
	replaced.Height = 3
	replaced.PreviousHash = types.HexToHashPanic("06aac631b44daf47f68810a416bea4ed3695e89c0ab9668a0ab8acb834dfb953")

	template := newUpdateTemplate(producer, types.PillarContract)
	err := w.priceUpdate(nil, dp.MinResourcePrice, template, replaced)
	common.ExpectError(t, err, ErrUpdatePredecessorMissing)
	common.Expect(t, len(pool.asked), 1)
	common.Expect(t, pool.asked[0], replaced.Previous())
	common.ExpectUint64(t, template.Height, 0)
}
