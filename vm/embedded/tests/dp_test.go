package tests

import (
	"math"
	"math/big"
	"testing"

	g "github.com/zenon-network/go-zenon/chain/genesis/mock"
	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/types"
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
			"dynamic-plasma",              // name
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
