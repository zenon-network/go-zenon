// Package dp implements Dynamic Plasma: the per-momentum pricing of the
// two resources an account block can pay with, fused plasma and proof
// of work. Each momentum produced after the Dynamic Plasma spork carries
// a fusion price and a work price (nom.Momentum.NextFusionPrice and
// NextWorkPrice, in units of PriceScaleFactor, never below
// MinResourcePrice). A DynamicPlasma built from that momentum prices the
// blocks the next momentum confirms: ValidPrice checks that a block pays
// at least the floor a minimum-priced block would, HigherPrice ranks two
// blocks competing for the same account-chain slot, and ComputeBasePlasma
// attributes a block's base plasma to the two resources so NextFusionPrice
// and NextWorkPrice can move each price toward its target share of the
// momentum (the targets and the step limits are the on-chain
// definition.PlasmaVariables). The conversion helpers mirror the
// pre-spork ones in package vm with the much larger per-account bounds
// that apply once plasma is priced.
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
	// ErrBlockPriceWorse is returned by HigherPrice when the first block
	// pays a lower price per unit of base plasma than the second.
	ErrBlockPriceWorse = errors.Errorf("block price is smaller for current block")
	// ErrBlockPriceSame is returned by HigherPrice when both blocks pay the
	// same price; callers break the tie by hash.
	ErrBlockPriceSame = errors.Errorf("block price is the same for current block")

	// MaxFusedAmountForAccountBig is MaxFusedAmountForAccount as a big.Int,
	// for balance comparisons.
	MaxFusedAmountForAccountBig = big.NewInt(MaxFusedAmountForAccount)
	// MaxFusionPlasmaForAccountBig is MaxFusionPlasmaForAccount as a big.Int.
	MaxFusionPlasmaForAccountBig = big.NewInt(MaxFusionPlasmaForAccount)
)

const (
	// The max amount of fusion and PoW plasma is unlimited in a practical sense.
	// A theoretical maximum for plasma is used to maintain consistency.
	// MaxFusionUnitsPerAccount bounds the fusion units a single account can
	// hold under Dynamic Plasma. It is deliberately far above any practical
	// amount; it exists so conversions stay bounded.
	MaxFusionUnitsPerAccount = 100000000
	// MaxFusionPlasmaForAccount is the plasma MaxFusionUnitsPerAccount grants.
	MaxFusionPlasmaForAccount = MaxFusionUnitsPerAccount * constants.PlasmaPerFusionUnit
	// MaxFusedAmountForAccount is the QSR, in smallest units, that fuses
	// MaxFusionUnitsPerAccount.
	MaxFusedAmountForAccount = constants.CostPerFusionUnit * MaxFusionUnitsPerAccount

	// MaxPoWPlasmaForAccountBlock bounds the proof-of-work plasma a single
	// block may claim.
	MaxPoWPlasmaForAccountBlock = MaxFusionPlasmaForAccount
	// MaxDifficultyForAccountBlock is the difficulty that yields
	// MaxPoWPlasmaForAccountBlock.
	MaxDifficultyForAccountBlock = MaxPoWPlasmaForAccountBlock * constants.PoWDifficultyPerPlasma

	// MinResourcePrice is the floor of both resource prices and the price
	// every chain starts at; with PriceScaleFactor it represents 1.0, so a
	// block at the floor pays exactly its base plasma.
	MinResourcePrice uint64 = 1000
	// PriceScaleFactor is the fixed-point scale of the resource prices: a
	// price of PriceScaleFactor means one unit of plasma per unit of base
	// plasma.
	PriceScaleFactor uint64 = 1000
)

// DynamicPlasma prices account blocks against one momentum's fusion and
// work prices and the on-chain plasma variables. NextFusionPrice and
// NextWorkPrice return the price the next momentum should carry given
// how much base plasma this momentum's blocks paid with each resource;
// ComputeBasePlasma and ComputeTotalBasePlasma make that attribution;
// ValidPrice reports whether a block pays at least the current floor;
// HigherPrice compares two blocks' price per unit of base plasma
// (ErrBlockPriceWorse, ErrBlockPriceSame or nil when a pays more than
// b); Config returns the variables in force and
// MaxContractBlocksInMomentum the contract-block cap they imply.
// Blocks from embedded addresses are exempt: zero base plasma, always a
// valid price.
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

	hundred := big.NewInt(100)
	// maxNextPrice = currentPrice * (percent + 100) / 100
	maxNextPrice := new(big.Int).Mul(currentPriceBig, big.NewInt(int64(dp.config.MaxPriceChangePercent)+100))
	maxNextPrice.Div(maxNextPrice, hundred)
	if nextPriceBig.Cmp(maxNextPrice) == 1 {
		nextPriceBig.Set(maxNextPrice)
	}

	// minNextPrice = currentPrice * (100 - percent) / 100
	minNextPrice := new(big.Int).Mul(currentPriceBig, big.NewInt(100-int64(dp.config.MaxPriceChangePercent)))
	minNextPrice.Div(minNextPrice, hundred)
	if nextPriceBig.Cmp(minNextPrice) == -1 {
		nextPriceBig.Set(minNextPrice)
	}

	// absolute floor applied last
	minResourcePriceBig := new(big.Int).SetUint64(MinResourcePrice)
	if nextPriceBig.Cmp(minResourcePriceBig) == -1 {
		nextPriceBig.Set(minResourcePriceBig)
	}

	return nextPriceBig.Uint64()
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

	// f = block.FusedPlasma * dp.workPrice
	f := new(big.Int).SetUint64(block.FusedPlasma)
	f.Mul(f, workPriceBig)

	// p = DifficultyToPlasma(block.Difficulty) * dp.fusionPrice
	p := new(big.Int).SetUint64(DifficultyToPlasma(block.Difficulty))
	p.Mul(p, fusionPriceBig)

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

	// The block's price is compared against the floor set by a
	// minimum-priced block. The minimum fused plasma is itself an integer
	// quantity, AccountBlockBasePlasma * fusionPrice / PriceScaleFactor, so
	// the PriceScaleFactor division is applied before the multiplications --
	// that integer is the floor consensus is defined against. The
	// comparison is done in big.Int because AccountBlockBasePlasma *
	// fusionPrice can exceed MaxUint64 at large fusionPrice.
	floor := new(big.Int).Mul(
		new(big.Int).SetUint64(constants.AccountBlockBasePlasma),
		new(big.Int).SetUint64(dp.fusionPrice),
	)
	floor.Div(floor, new(big.Int).SetUint64(PriceScaleFactor))
	floor.Mul(floor, new(big.Int).SetUint64(dp.workPrice))
	floor.Mul(floor, new(big.Int).SetUint64(block.BasePlasma))

	value := dp.priceValue(block)
	value.Mul(value, new(big.Int).SetUint64(constants.AccountBlockBasePlasma))

	return value.Cmp(floor) >= 0
}

// priceValue is a block's plasma priced against the current resource
// prices: fused plasma valued at the work price, PoW plasma at the fusion
// price. Computed in big.Int because either product can exceed MaxUint64
// at large prices. Returns a fresh value the caller may mutate.
func (dp *dynamicPlasma) priceValue(block *nom.AccountBlock) *big.Int {
	return new(big.Int).Add(
		new(big.Int).Mul(new(big.Int).SetUint64(block.FusedPlasma), new(big.Int).SetUint64(dp.workPrice)),
		new(big.Int).Mul(new(big.Int).SetUint64(DifficultyToPlasma(block.Difficulty)), new(big.Int).SetUint64(dp.fusionPrice)),
	)
}

func (dp *dynamicPlasma) HigherPrice(a, b *nom.AccountBlock) error {
	aValue := dp.priceValue(a)
	aRatio := aValue.Mul(aValue, new(big.Int).SetUint64(b.BasePlasma))

	bValue := dp.priceValue(b)
	bRatio := bValue.Mul(bValue, new(big.Int).SetUint64(a.BasePlasma))

	switch aRatio.Cmp(bRatio) {
	case -1:
		return ErrBlockPriceWorse
	case 0:
		return ErrBlockPriceSame
	default:
		return nil
	}
}

func (dp *dynamicPlasma) Config() *definition.PlasmaVariables {
	return dp.config
}

// MaxContractBlocks is the per-momentum contract-block cap implied by a
// plasma configuration. Package-level because the momentum verifier needs
// this bound to sanity-check content size before it has a DynamicPlasma
// instance to ask.
func MaxContractBlocks(config *definition.PlasmaVariables) uint64 {
	return config.MaxBasePlasmaInMomentum / constants.EmbeddedSimplePlasma
}

func (dp *dynamicPlasma) MaxContractBlocksInMomentum() uint64 {
	return MaxContractBlocks(dp.config)
}

// NewDynamicPlasma returns the pricing for the blocks confirmed after
// context: its NextFusionPrice and NextWorkPrice when context is a
// Dynamic Plasma momentum (Version at least
// nom.DynamicPlasmaMomentumVersion), MinResourcePrice for both
// otherwise, together with the plasma variables config.
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

// GetDifficultyForPlasma returns the proof-of-work difficulty that
// yields requiredPlasma (constants.PoWDifficultyPerPlasma per unit), or
// constants.ErrForbiddenParam above MaxPoWPlasmaForAccountBlock.
func GetDifficultyForPlasma(requiredPlasma uint64) (uint64, error) {
	if requiredPlasma > MaxPoWPlasmaForAccountBlock {
		return 0, constants.ErrForbiddenParam
	} else if requiredPlasma == 0 {
		return 0, nil
	}
	return requiredPlasma * constants.PoWDifficultyPerPlasma, nil
}

// DifficultyToPlasma returns the plasma a proof of work of the given
// difficulty is worth, capped at MaxPoWPlasmaForAccountBlock.
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

// FusedAmountToPlasma converts fused QSR (smallest units) into plasma:
// constants.PlasmaPerFusionUnit per whole constants.CostPerFusionUnit,
// capped at MaxFusionPlasmaForAccount; nil or non-positive amounts give
// 0.
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
