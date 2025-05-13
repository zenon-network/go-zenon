package dp

import (
	"testing"

	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common"
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
