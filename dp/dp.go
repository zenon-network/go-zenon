package dp

import (
	"math/big"

	"github.com/pkg/errors"
	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/vm/constants"
	"github.com/zenon-network/go-zenon/vm/embedded/definition"
)

var (
	ErrBlockPriceWorse = errors.Errorf("block price is smaller for current block")
	ErrBlockPriceSame  = errors.Errorf("block price is the same for current block")

	MaxFusedAmountForAccountBig = big.NewInt(MaxFusedAmountForAccount)
)

const (
	// The max amount of fusion and PoW plasma is unlimited in a practical sense.
	// A theoretical maximum for plasma is used to maintain consistency.
	MaxFusionUnitsPerAccount  = 100000000
	MaxFusionPlasmaForAccount = MaxFusionUnitsPerAccount * constants.CostPerFusionUnit
	MaxFusedAmountForAccount  = constants.CostPerFusionUnit * MaxFusionUnitsPerAccount

	MaxPoWPlasmaForAccountBlock  = MaxFusionPlasmaForAccount
	MaxDifficultyForAccountBlock = MaxPoWPlasmaForAccountBlock * constants.PoWDifficultyPerPlasma

	MinResourcePrice uint64 = 1000
	PriceScaleFactor uint64 = 1000
)

type DynamicPlasma interface {
	NextFusionPrice(fusedBasePlasma uint64) uint64
	NextWorkPrice(powBasePlasma uint64) uint64
	ComputeBasePlasma(block *nom.AccountBlock) types.BasePlasma
	ComputeTotalBasePlasma(blocks []*nom.AccountBlock) types.BasePlasma
	ValidPrice(block *nom.AccountBlock) bool
	HigherPrice(a, b *nom.AccountBlock) error
	Config() *definition.PlasmaVariables
	MaxContractBlocksInMomentum() uint64
}

type dynamicPlasma struct {
	fusionPrice uint64
	workPrice   uint64
	config      *definition.PlasmaVariables
}

func (dp *dynamicPlasma) NextFusionPrice(fusedBasePlasma uint64) uint64 {
	return dp.nextResourcePrice(dp.fusionPrice, fusedBasePlasma, dp.config.FusedPlasmaTarget)
}
func (dp *dynamicPlasma) NextWorkPrice(powBasePlasma uint64) uint64 {
	return dp.nextResourcePrice(dp.workPrice, powBasePlasma, dp.config.PowPlasmaTarget)
}
func (dp *dynamicPlasma) nextResourcePrice(currentPrice uint64, usedPlasma uint64, targetPlasma uint64) uint64 {
	if usedPlasma == targetPlasma {
		return currentPrice
	}

	currentPriceBig := new(big.Int).SetUint64(currentPrice)
	usedPlasmaBig := new(big.Int).SetUint64(usedPlasma)
	targetPlasmaBig := new(big.Int).SetUint64(targetPlasma)

	var (
		num    = new(big.Int)
		denom  = new(big.Int)
		change = new(big.Int)
	)

	// change = currentPrice * usedPlasmaDelta / targetPlasma / priceChangeDenominator
	num.Sub(usedPlasmaBig, targetPlasmaBig)
	num.Mul(num, currentPriceBig)
	num.Div(num, denom.SetUint64(targetPlasma))
	change.Div(num, denom.SetUint64(uint64(dp.config.PriceChangeDenominator)))

	nextPriceBig := new(big.Int).Add(currentPriceBig, change)
	if nextPriceBig.Cmp(new(big.Int).SetUint64(MinResourcePrice)) == -1 {
		return MinResourcePrice
	}

	nextPrice := nextPriceBig.Uint64()
	maxNextPrice := currentPrice * (uint64(dp.config.MaxPriceChangePercent) + 100) / 100
	if nextPrice > maxNextPrice {
		return maxNextPrice
	}

	minNextPrice := currentPrice * (100 - uint64(dp.config.MaxPriceChangePercent)) / 100
	if nextPrice < minNextPrice {
		return minNextPrice
	}

	return nextPrice
}

// Calculates nominal base plasma values for fused plasma and PoW plasma. These nominal base plasma
// values are used to account for and control the amount of network bandwidth paid for by the two plasma resources.
func (dp *dynamicPlasma) ComputeBasePlasma(block *nom.AccountBlock) types.BasePlasma {
	if types.IsEmbeddedAddress(block.Address) {
		return types.NewBasePlasma(0, 0)
	}

	if block.Difficulty == 0 {
		return types.NewBasePlasma(block.BasePlasma, 0)
	} else if block.FusedPlasma == 0 {
		return types.NewBasePlasma(0, block.BasePlasma)
	}

	basePlasmaBig := new(big.Int).SetUint64(block.BasePlasma)
	fusionPriceBig := new(big.Int).SetUint64(dp.fusionPrice)
	workPriceBig := new(big.Int).SetUint64(dp.workPrice)

	// f = block.FusedPlasma * dp.fusionPrice * dp.workPrice
	f := new(big.Int).SetUint64(block.FusedPlasma)
	f.Mul(f, fusionPriceBig)
	f.Mul(f, workPriceBig)

	// p = DifficultyToPlasma(block.Difficulty) * dp.fusionPrice * dp.workPrice
	p := new(big.Int).SetUint64(DifficultyToPlasma(block.Difficulty))
	p.Mul(p, fusionPriceBig)
	p.Mul(p, workPriceBig)

	// fusedBasePlasma = f * block.BasePlasma / (f + p)
	fusedBasePlasma := new(big.Int).Mul(f, basePlasmaBig)
	fusedBasePlasma.Div(fusedBasePlasma, f.Add(f, p))

	powBasePlasma := basePlasmaBig.Sub(basePlasmaBig, fusedBasePlasma)

	return types.NewBasePlasma(fusedBasePlasma.Uint64(), powBasePlasma.Uint64())
}

func (dp *dynamicPlasma) ComputeTotalBasePlasma(blocks []*nom.AccountBlock) types.BasePlasma {
	total := types.NewBasePlasma(0, 0)
	for _, block := range blocks {
		total.Add(dp.ComputeBasePlasma(block))
	}
	return total
}

func (dp *dynamicPlasma) ValidPrice(block *nom.AccountBlock) bool {
	if types.IsEmbeddedAddress(block.Address) {
		return true
	}
	minimumPricedBlock := &nom.AccountBlock{
		FusedPlasma: constants.AccountBlockBasePlasma * dp.fusionPrice / PriceScaleFactor,
		Difficulty:  0,
		BasePlasma:  constants.AccountBlockBasePlasma,
	}
	err := dp.HigherPrice(block, minimumPricedBlock)
	return err == nil || err == ErrBlockPriceSame
}

func (dp *dynamicPlasma) HigherPrice(a, b *nom.AccountBlock) error {
	aRatio := (a.FusedPlasma*dp.workPrice + DifficultyToPlasma(a.Difficulty)*dp.fusionPrice) * b.BasePlasma
	bRatio := (b.FusedPlasma*dp.workPrice + DifficultyToPlasma(b.Difficulty)*dp.fusionPrice) * a.BasePlasma

	if aRatio < bRatio {
		return ErrBlockPriceWorse
	} else if aRatio == bRatio {
		return ErrBlockPriceSame
	}

	return nil
}

func (dp *dynamicPlasma) Config() *definition.PlasmaVariables {
	return dp.config
}

func (dp *dynamicPlasma) MaxContractBlocksInMomentum() uint64 {
	return dp.config.MaxBasePlasmaInMomentum / constants.EmbeddedSimplePlasma
}

func NewDynamicPlasma(context *nom.Momentum, config *definition.PlasmaVariables) DynamicPlasma {
	fusionPrice := MinResourcePrice
	workPrice := MinResourcePrice

	// Check if context momentum has dynamic plasma activated,
	// otherwise use MinResourcePrice
	if context.Version >= nom.DynamicPlasmaMomentumVersion {
		fusionPrice = context.NextFusionPrice
		workPrice = context.NextWorkPrice
	}

	return &dynamicPlasma{
		fusionPrice: fusionPrice,
		workPrice:   workPrice,
		config:      config,
	}
}

func GetDifficultyForPlasma(requiredPlasma uint64) (uint64, error) {
	if requiredPlasma > MaxPoWPlasmaForAccountBlock {
		return 0, constants.ErrForbiddenParam
	} else if requiredPlasma == 0 {
		return 0, nil
	}
	return requiredPlasma * constants.PoWDifficultyPerPlasma, nil
}

func DifficultyToPlasma(difficulty uint64) uint64 {
	// Check for 0
	if difficulty == 0 {
		return 0
	}

	// Check for more than max plasma allowed
	if difficulty > MaxDifficultyForAccountBlock {
		return MaxPoWPlasmaForAccountBlock
	}

	return difficulty / constants.PoWDifficultyPerPlasma
}

func FusedAmountToPlasma(amount *big.Int) uint64 {
	// Check for 0
	if amount == nil || amount.Sign() <= 0 {
		return 0
	}
	// Check for more than max plasma allowed
	if amount.Cmp(MaxFusedAmountForAccountBig) >= 0 {
		return MaxFusionPlasmaForAccount
	}

	numUnits := amount.Uint64() / constants.CostPerFusionUnit
	return numUnits * constants.PlasmaPerFusionUnit
}
