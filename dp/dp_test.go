package dp

import (
	"testing"

	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/vm/constants"
	"github.com/zenon-network/go-zenon/vm/embedded/definition"
)

func TestNextFusionPrice(t *testing.T) {
	config := &definition.PlasmaVariables{
		MaxBasePlasmaInMomentum: 4200000,
		FusedPlasmaTarget:       1050000,
		MaxPriceChangePercent:   10,
		PriceChangeDenominator:  20,
	}
	dp := &dynamicPlasma{fusionPrice: 1000, config: config}

	common.Json(dp.NextFusionPrice(21000*0), nil).Equals(t, "1000")
	common.Json(dp.NextFusionPrice(21000*50), nil).Equals(t, "1000")
	common.Json(dp.NextFusionPrice(21000*100), nil).Equals(t, "1050")
	common.Json(dp.NextFusionPrice(21000*150), nil).Equals(t, "1100")
	common.Json(dp.NextFusionPrice(21000*200), nil).Equals(t, "1100")

	dp = &dynamicPlasma{fusionPrice: 2000, config: config}
	common.Json(dp.NextFusionPrice(21000*0), nil).Equals(t, "1900")
	common.Json(dp.NextFusionPrice(21000*50), nil).Equals(t, "2000")
	common.Json(dp.NextFusionPrice(21000*100), nil).Equals(t, "2100")
	common.Json(dp.NextFusionPrice(21000*150), nil).Equals(t, "2200")
	common.Json(dp.NextFusionPrice(21000*200), nil).Equals(t, "2200")
}

func TestNextWorkPrice(t *testing.T) {
	config := &definition.PlasmaVariables{
		MaxBasePlasmaInMomentum: 4200000,
		PowPlasmaTarget:         1050000,
		MaxPriceChangePercent:   10,
		PriceChangeDenominator:  20,
	}
	dp := &dynamicPlasma{workPrice: 1000, config: config}

	common.Json(dp.NextWorkPrice(21000*0), nil).Equals(t, "1000")
	common.Json(dp.NextWorkPrice(21000*50), nil).Equals(t, "1000")
	common.Json(dp.NextWorkPrice(21000*100), nil).Equals(t, "1050")
	common.Json(dp.NextWorkPrice(21000*150), nil).Equals(t, "1100")
	common.Json(dp.NextWorkPrice(21000*200), nil).Equals(t, "1100")

	dp = &dynamicPlasma{workPrice: 2000, config: config}
	common.Json(dp.NextWorkPrice(21000*0), nil).Equals(t, "1900")
	common.Json(dp.NextWorkPrice(21000*50), nil).Equals(t, "2000")
	common.Json(dp.NextWorkPrice(21000*100), nil).Equals(t, "2100")
	common.Json(dp.NextWorkPrice(21000*150), nil).Equals(t, "2200")
	common.Json(dp.NextWorkPrice(21000*200), nil).Equals(t, "2200")
}

// A sustained-high currentPrice must not collapse the clamp arithmetic: the
// max-change bound is computed entirely in big.Int, so it stays accurate even
// when currentPrice*percent would overflow a raw uint64 multiplication.
func TestNextResourcePrice_NoOverflowAtHighCurrentPrice(t *testing.T) {
	config := &definition.PlasmaVariables{
		FusedPlasmaTarget:      1000,
		MaxPriceChangePercent:  100,
		PriceChangeDenominator: 1,
	}
	currentPrice := uint64(1) << 62
	dp := &dynamicPlasma{fusionPrice: currentPrice, config: config}

	// usedPlasma is double the target, so the raw change equals currentPrice
	// and the max-change bound (currentPrice * 200 / 100) would overflow a
	// uint64 multiplication before reaching the final clamp.
	got := dp.NextFusionPrice(2000)
	common.ExpectUint64(t, got, currentPrice*2)
}

// The absolute MinResourcePrice floor must be applied only after the
// percentage clamps, so a downward step is always bounded by
// MaxPriceChangePercent instead of collapsing straight to the floor.
func TestNextResourcePrice_PercentClampAppliesBeforeAbsoluteFloor(t *testing.T) {
	config := &definition.PlasmaVariables{
		FusedPlasmaTarget:      1000,
		MaxPriceChangePercent:  10,
		PriceChangeDenominator: 1,
	}
	currentPrice := uint64(1000000)
	dp := &dynamicPlasma{fusionPrice: currentPrice, config: config}

	// A small PriceChangeDenominator relative to the price move drives the
	// raw change below MinResourcePrice, so a floor-first implementation
	// would return MinResourcePrice (a ~99.9% drop) instead of the
	// percentage-bounded minimum.
	got := dp.NextFusionPrice(0)
	common.ExpectUint64(t, got, currentPrice*90/100)
}

func TestHigherBlockPrice(t *testing.T) {
	dp := &dynamicPlasma{fusionPrice: 1241, workPrice: 1052}
	a := &nom.AccountBlock{
		FusedPlasma: (21001 * dp.fusionPrice / PriceScaleFactor),
		Difficulty:  0,
		BasePlasma:  21000,
	}
	difficulty, _ := GetDifficultyForPlasma((21000 * dp.workPrice / PriceScaleFactor))
	b := &nom.AccountBlock{
		FusedPlasma: 0,
		Difficulty:  difficulty,
		BasePlasma:  21000,
	}
	common.ExpectError(t, dp.HigherPrice(a, b), nil)
}

func TestSameBlockPrice(t *testing.T) {
	dp := &dynamicPlasma{fusionPrice: 1156, workPrice: 1999}
	a := &nom.AccountBlock{
		FusedPlasma: (21000 * dp.fusionPrice / PriceScaleFactor),
		Difficulty:  0,
		BasePlasma:  21000,
	}
	difficulty, _ := GetDifficultyForPlasma((42000 * dp.workPrice / PriceScaleFactor))
	b := &nom.AccountBlock{
		FusedPlasma: 0,
		Difficulty:  difficulty,
		BasePlasma:  42000,
	}
	common.ExpectError(t, dp.HigherPrice(a, b), ErrBlockPriceSame)
}

func TestWorseBlockPrice(t *testing.T) {
	dp := &dynamicPlasma{fusionPrice: 1200, workPrice: 1200}
	a := &nom.AccountBlock{
		FusedPlasma: 21000,
		Difficulty:  0,
		BasePlasma:  21000,
	}
	difficulty, _ := GetDifficultyForPlasma(42001)
	b := &nom.AccountBlock{
		FusedPlasma: 0,
		Difficulty:  difficulty,
		BasePlasma:  42000,
	}
	common.ExpectError(t, dp.HigherPrice(a, b), ErrBlockPriceWorse)
}

func TestValidPrice(t *testing.T) {
	dp := &dynamicPlasma{fusionPrice: 1000, workPrice: 1000}

	atMinimum := &nom.AccountBlock{
		FusedPlasma: constants.AccountBlockBasePlasma * dp.fusionPrice / PriceScaleFactor,
		Difficulty:  0,
		BasePlasma:  constants.AccountBlockBasePlasma,
	}
	common.ExpectTrue(t, dp.ValidPrice(atMinimum))

	belowMinimum := &nom.AccountBlock{
		FusedPlasma: atMinimum.FusedPlasma - 1,
		Difficulty:  0,
		BasePlasma:  constants.AccountBlockBasePlasma,
	}
	common.ExpectTrue(t, !dp.ValidPrice(belowMinimum))
}

// fusionPrice is chosen so the naive uint64 computation
// AccountBlockBasePlasma * fusionPrice / PriceScaleFactor overflows
// (21000 * 2^61 exceeds MaxUint64), which is exactly why the floor is
// computed in big.Int: the exact floor at this fusionPrice is 21,000 PoW
// plasma, and the comparison holds that floor precisely rather than
// clamping it down to a saturated (and here, unreachable) value.
func TestValidPrice_ExtremeFusionPriceHoldsTheFloor(t *testing.T) {
	const extremeFusionPrice = uint64(1) << 61
	dp := &dynamicPlasma{fusionPrice: extremeFusionPrice, workPrice: 1000}

	freeBlock := &nom.AccountBlock{
		FusedPlasma: 0,
		Difficulty:  0,
		BasePlasma:  constants.AccountBlockBasePlasma,
	}
	common.ExpectTrue(t, !dp.ValidPrice(freeBlock))

	belowFloor, _ := GetDifficultyForPlasma(10000)
	belowFloorBlock := &nom.AccountBlock{
		FusedPlasma: 0,
		Difficulty:  belowFloor,
		BasePlasma:  constants.AccountBlockBasePlasma,
	}
	common.ExpectTrue(t, !dp.ValidPrice(belowFloorBlock))

	atFloor, _ := GetDifficultyForPlasma(21000)
	atFloorBlock := &nom.AccountBlock{
		FusedPlasma: 0,
		Difficulty:  atFloor,
		BasePlasma:  constants.AccountBlockBasePlasma,
	}
	common.ExpectTrue(t, dp.ValidPrice(atFloorBlock))
}

func TestComputeBasePlasma_WeightsByOppositeResourcePrice(t *testing.T) {
	dp := &dynamicPlasma{fusionPrice: 2000, workPrice: 1000}
	difficulty, _ := GetDifficultyForPlasma(10000)
	block := &nom.AccountBlock{
		FusedPlasma: 10000,
		Difficulty:  difficulty,
		BasePlasma:  930,
	}

	result := dp.ComputeBasePlasma(block)
	common.ExpectUint64(t, result.Fusion, 310)
	common.ExpectUint64(t, result.Pow, 620)
}

func TestTotalBasePlasma(t *testing.T) {
	dp := &dynamicPlasma{fusionPrice: 1000, workPrice: 1000}
	a := &nom.AccountBlock{
		FusedPlasma: 42000,
		Difficulty:  0,
		BasePlasma:  21000,
	}
	difficulty, _ := GetDifficultyForPlasma(21000)
	b := &nom.AccountBlock{
		FusedPlasma: 21000,
		Difficulty:  difficulty,
		BasePlasma:  42000,
	}
	result := dp.ComputeTotalBasePlasma([]*nom.AccountBlock{a, b})
	common.ExpectUint64(t, result.Fusion, 42000)
	common.ExpectUint64(t, result.Pow, 21000)
	common.ExpectUint64(t, result.Total(), a.BasePlasma+b.BasePlasma)

	dp = &dynamicPlasma{fusionPrice: 1200, workPrice: 1200}
	a = &nom.AccountBlock{
		FusedPlasma: 30000,
		Difficulty:  0,
		BasePlasma:  21000,
	}
	difficulty, _ = GetDifficultyForPlasma(50000)
	b = &nom.AccountBlock{
		FusedPlasma: 1000,
		Difficulty:  difficulty,
		BasePlasma:  42000,
	}
	result = dp.ComputeTotalBasePlasma([]*nom.AccountBlock{a, b})
	common.ExpectUint64(t, result.Fusion, 21823)
	common.ExpectUint64(t, result.Pow, 41177)
	common.ExpectUint64(t, result.Total(), a.BasePlasma+b.BasePlasma)
}
