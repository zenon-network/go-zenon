package tests

import (
	"math"
	"math/big"
	"testing"

	g "github.com/zenon-network/go-zenon/chain/genesis/mock"
	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/dp"
	"github.com/zenon-network/go-zenon/rpc/api"
	"github.com/zenon-network/go-zenon/rpc/api/embedded"
	"github.com/zenon-network/go-zenon/vm/constants"
	"github.com/zenon-network/go-zenon/vm/embedded/definition"
	"github.com/zenon-network/go-zenon/zenon/mock"
)

func activateDynamicPlasma(t *testing.T, z mock.MockZenon) {
	saveSporkState(t)
	z.InsertSendBlock(&nom.AccountBlock{
		Address:   g.Spork.Address,
		ToAddress: types.SporkContract,
		Data: definition.ABISpork.PackMethodPanic(definition.SporkCreateMethodName,
			"dynamic-plasma",           // name
			"Activates Dynamic Plasma", // description
		),
	}, nil, mock.SkipVmChanges)
	z.InsertNewMomentum()

	sporkAPI := embedded.NewSporkApi(z)
	sporkList, _ := sporkAPI.GetAll(0, 10)
	id := sporkList.List[0].Id

	z.InsertSendBlock(&nom.AccountBlock{
		Address:   g.Spork.Address,
		ToAddress: types.SporkContract,
		Data: definition.ABISpork.PackMethodPanic(definition.SporkActivateMethodName,
			id, // id
		),
	}, nil, mock.SkipVmChanges)
	z.InsertNewMomentum()
	types.DynamicPlasmaSpork.SporkId = id
	types.ImplementedSporksMap[id] = true
	z.InsertMomentumsTo(20)
}

func TestDynamicPlasma(t *testing.T) {
	z := mock.NewMockZenon(t)
	ledgerApi := api.NewLedgerApi(z)
	defer z.StopPanic()
	savePlasmaDefaults(t)

	definition.DefaultMaxBasePlasmaInMomentum = 42000
	definition.DefaultFusedPlasmaTarget = 10500

	activateDynamicPlasma(t, z)

	z.InsertSendBlock(&nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     g.User2.Address,
		TokenStandard: types.ZeroTokenStandard,
		Amount:        common.Big0,
		FusedPlasma:   21000,
	}, nil, mock.SkipVmChanges)
	z.InsertNewMomentum()

	frontier, _ := ledgerApi.GetFrontierMomentum()
	common.ExpectUint64(t, uint64(len(frontier.Content)), 1)

	z.InsertSendBlock(&nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     g.User2.Address,
		TokenStandard: types.ZeroTokenStandard,
		Amount:        common.Big0,
		FusedPlasma:   21000,
	}, nil, mock.SkipVmChanges)
	z.InsertNewMomentum()

	frontier, _ = ledgerApi.GetFrontierMomentum()
	common.ExpectUint64(t, uint64(len(frontier.Content)), 0)

	z.InsertNewMomentum()

	frontier, _ = ledgerApi.GetFrontierMomentum()
	common.ExpectUint64(t, uint64(len(frontier.Content)), 1)
}

// A single block's FusedPlasma payment can legitimately exceed the legacy
// per-block cap (10.5M) while remaining well within the dynamic plasma cap,
// so the block must still be accepted once dynamic plasma is enforced.
func TestDynamicPlasma_HighFusedPlasmaUsesDynamicPlasmaCap(t *testing.T) {
	z := mock.NewMockZenon(t)
	ledgerApi := api.NewLedgerApi(z)
	defer z.StopPanic()
	savePlasmaDefaults(t)

	definition.DefaultMaxBasePlasmaInMomentum = 42000
	definition.DefaultFusedPlasmaTarget = 10500

	activateDynamicPlasma(t, z)

	z.InsertSendBlock(&nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     g.User2.Address,
		TokenStandard: types.ZeroTokenStandard,
		Amount:        common.Big0,
		FusedPlasma:   20000000,
	}, nil, mock.SkipVmChanges)
	z.InsertNewMomentum()

	frontier, _ := ledgerApi.GetFrontierMomentum()
	common.ExpectUint64(t, uint64(len(frontier.Content)), 1)
}

// - test plasma.GetRequiredPoWForAccountBlock rpc with increased work price
func TestDynamicPlasma_rpc(t *testing.T) {
	z := mock.NewMockZenon(t)
	plasmaApi := embedded.NewPlasmaApi(z)
	defer z.StopPanic()
	savePlasmaDefaults(t)

	definition.DefaultMaxBasePlasmaInMomentum = 42000
	definition.DefaultPowPlasmaTarget = 10500

	activateDynamicPlasma(t, z)

	ab := &nom.AccountBlock{
		Address:       g.User6.Address,
		ToAddress:     g.User1.Address,
		TokenStandard: types.ZeroTokenStandard,
		Amount:        common.Big0,
		Difficulty:    31500000,
	}
	ab.Nonce.UnmarshalText([]byte("6fa3063406240b1c"))

	z.InsertSendBlock(ab, nil, mock.SkipVmChanges)
	z.InsertNewMomentum()

	common.Json(plasmaApi.GetRequiredPoWForAccountBlock(embedded.GetRequiredParam{
		BlockType: nom.BlockTypeUserSend,
		SelfAddr:  g.User6.Address,
		ToAddr:    &g.User1.Address,
	})).Equals(t, `
{
	"availablePlasma": 0,
	"basePlasma": 21000,
	"requiredFusedPlasma": 21000,
	"requiredDifficulty": 33075000
}`)
}

// GetRequiredPoWForAccountBlock's fusion branch computes requiredFusedPlasma
// with a big.Int multiply/round-up guard; when an account's fused plasma
// covers the block's required plasma, no PoW should be requested.
func TestDynamicPlasma_rpc_FusionCoversRequiredPlasma(t *testing.T) {
	z := mock.NewMockZenon(t)
	plasmaApi := embedded.NewPlasmaApi(z)
	defer z.StopPanic()
	saveGovernanceAddress(t)

	types.GovernanceAddress = g.User1.Address

	activateDynamicPlasma(t, z)

	defer z.CallContract(&nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     types.PlasmaContract,
		Data:          definition.ABIPlasma.PackMethodPanic(definition.FuseMethodName, g.User6.Address),
		TokenStandard: types.QsrTokenStandard,
		Amount:        big.NewInt(10 * g.Zexp),
	}).Error(t, nil)
	insertMomentums(z, 2)

	common.Json(plasmaApi.GetRequiredPoWForAccountBlock(embedded.GetRequiredParam{
		BlockType: nom.BlockTypeUserSend,
		SelfAddr:  g.User6.Address,
		ToAddr:    &g.User1.Address,
	})).Equals(t, `
{
	"availablePlasma": 21000,
	"basePlasma": 21000,
	"requiredFusedPlasma": 21000,
	"requiredDifficulty": 0
}`)
}

func TestDynamicPlasma_SetPlasmaVariables(t *testing.T) {
	z := mock.NewMockZenon(t)
	plasmaApi := embedded.NewPlasmaApi(z)
	defer z.StopPanic()
	saveGovernanceAddress(t)

	types.GovernanceAddress = g.User1.Address

	activateDynamicPlasma(t, z)

	ab := &nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     types.PlasmaContract,
		TokenStandard: types.ZeroTokenStandard,
		Amount:        common.Big0,
		Data: definition.ABIPlasma.PackMethodPanic(definition.SetVariablesMethodName,
			uint64(1050000), uint64(525000), uint64(525000), uint8(10), uint8(20)),
	}
	defer z.CallContract(ab).Error(t, nil)

	insertMomentums(z, 2)
	common.Json(plasmaApi.GetVariables()).Equals(t, `
{
	"MaxBasePlasmaInMomentum": 1050000,
	"FusedPlasmaTarget": 525000,
	"PowPlasmaTarget": 525000,
	"MaxPriceChangePercent": 10,
	"PriceChangeDenominator": 20
}`)
}

func TestDynamicPlasma_SetPlasmaVariables_TargetSumBounds(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()
	saveGovernanceAddress(t)

	types.GovernanceAddress = g.User1.Address

	activateDynamicPlasma(t, z)

	setVariables := func(maxBasePlasma, fusedTarget, powTarget uint64) *nom.AccountBlock {
		return &nom.AccountBlock{
			Address:       g.User1.Address,
			ToAddress:     types.PlasmaContract,
			TokenStandard: types.ZeroTokenStandard,
			Amount:        common.Big0,
			Data: definition.ABIPlasma.PackMethodPanic(definition.SetVariablesMethodName,
				maxBasePlasma, fusedTarget, powTarget, uint8(10), uint8(20)),
		}
	}

	// fusedTarget + powTarget == maxBasePlasmaInMomentum is the accepted boundary
	z.InsertSendBlock(setVariables(210000, 209999, 1), nil, mock.SkipVmChanges)

	// fusedTarget + powTarget == maxBasePlasmaInMomentum + 1 is rejected
	z.InsertSendBlock(setVariables(210000, 210000, 1), constants.ErrForbiddenParam, mock.SkipVmChanges)

	// fusedTarget + powTarget must be checked without a uint64 sum that wraps around
	z.InsertSendBlock(setVariables(210000, math.MaxUint64, 1), constants.ErrForbiddenParam, mock.SkipVmChanges)
}

// TestDynamicPlasma_rpc_RequiredFusedPlasmaAboveFloor pins the field #147
// asked for: once the fusion price has moved above the floor, a fused-only
// client that pays basePlasma underpays, and the required-PoW result must
// say how much fused plasma the block needs at the current price. One fused
// send against a lowered fused target moves the price from 1000 to 1050, so
// a 21,000 base block needs 22,050 fused plasma.
func TestDynamicPlasma_rpc_RequiredFusedPlasmaAboveFloor(t *testing.T) {
	z := mock.NewMockZenon(t)
	plasmaApi := embedded.NewPlasmaApi(z)
	defer z.StopPanic()
	savePlasmaDefaults(t)

	definition.DefaultMaxBasePlasmaInMomentum = 42000
	definition.DefaultFusedPlasmaTarget = 10500

	activateDynamicPlasma(t, z)

	z.InsertSendBlock(&nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     g.User2.Address,
		TokenStandard: types.ZnnTokenStandard,
		Amount:        common.Big1,
	}, nil, mock.SkipVmChanges)
	// The momentum that carries the send sets the next fusion price from its
	// own usage; an empty momentum after it would bring the price back down.
	z.InsertNewMomentum()

	frontier, err := z.Chain().GetFrontierMomentumStore().GetFrontierMomentum()
	common.FailIfErr(t, err)
	common.ExpectUint64(t, frontier.NextFusionPrice, 1050)

	// An account with plenty of fused plasma: no PoW needed, but the fused
	// requirement is price-scaled above basePlasma.
	common.Json(plasmaApi.GetRequiredPoWForAccountBlock(embedded.GetRequiredParam{
		BlockType: nom.BlockTypeUserSend,
		SelfAddr:  g.User1.Address,
		ToAddr:    &g.User2.Address,
	})).Equals(t, `
{
	"availablePlasma": 21000000,
	"basePlasma": 21000,
	"requiredFusedPlasma": 22050,
	"requiredDifficulty": 0
}`)

	// An account with no fused plasma: the same requirement, and PoW for all
	// of it at the work price.
	common.Json(plasmaApi.GetRequiredPoWForAccountBlock(embedded.GetRequiredParam{
		BlockType: nom.BlockTypeUserSend,
		SelfAddr:  g.User6.Address,
		ToAddr:    &g.User1.Address,
	})).Equals(t, `
{
	"availablePlasma": 0,
	"basePlasma": 21000,
	"requiredFusedPlasma": 22050,
	"requiredDifficulty": 31500000
}`)

	// Fractional rounding: one data byte makes the base 21,068, and
	// 21,068 x 1050 / 1000 = 22,121.4 rounds up to 22,122. That is exactly
	// the floor the consensus rule enforces: 22,122 passes ValidPrice and
	// 22,121 does not.
	common.Json(plasmaApi.GetRequiredPoWForAccountBlock(embedded.GetRequiredParam{
		BlockType: nom.BlockTypeUserSend,
		SelfAddr:  g.User6.Address,
		ToAddr:    &g.User1.Address,
		Data:      []byte{1},
	})).Equals(t, `
{
	"availablePlasma": 0,
	"basePlasma": 21068,
	"requiredFusedPlasma": 22122,
	"requiredDifficulty": 31602000
}`)
	variables, err := plasmaApi.GetVariables()
	common.FailIfErr(t, err)
	pricing := dp.NewDynamicPlasma(frontier, variables)
	atRequirement := &nom.AccountBlock{Address: g.User6.Address, BasePlasma: 21068, FusedPlasma: 22122}
	oneBelow := &nom.AccountBlock{Address: g.User6.Address, BasePlasma: 21068, FusedPlasma: 22121}
	common.Expect(t, pricing.ValidPrice(atRequirement), true)
	common.Expect(t, pricing.ValidPrice(oneBelow), false)
}
