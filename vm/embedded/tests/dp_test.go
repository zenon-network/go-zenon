package tests

import (
	"testing"

	g "github.com/zenon-network/go-zenon/chain/genesis/mock"
	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/rpc/api"
	"github.com/zenon-network/go-zenon/rpc/api/embedded"
	"github.com/zenon-network/go-zenon/vm/embedded/definition"
	"github.com/zenon-network/go-zenon/zenon/mock"
)

func activateDynamicPlasma(z mock.MockZenon) {
	z.InsertSendBlock(&nom.AccountBlock{
		Address:   g.Spork.Address,
		ToAddress: types.SporkContract,
		Data: definition.ABISpork.PackMethodPanic(definition.SporkCreateMethodName,
			"spork-dynamic-plasma",              // name
			"activate spork for dynamic plasma", // description
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

	definition.DefaultMaxBasePlasmaInMomentum = 42000
	definition.DefaultFusedPlasmaTarget = 10500

	activateDynamicPlasma(z)

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

// - test plasma.GetRequiredPoWForAccountBlock rpc with increased work price
func TestDynamicPlasma_rpc(t *testing.T) {
	z := mock.NewMockZenon(t)
	plasmaApi := embedded.NewPlasmaApi(z)
	defer z.StopPanic()

	definition.DefaultMaxBasePlasmaInMomentum = 42000
	definition.DefaultPowPlasmaTarget = 10500

	activateDynamicPlasma(z)

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

func TestDynamicPlasma_SetPlasmaVariables(t *testing.T) {
	z := mock.NewMockZenon(t)
	plasmaApi := embedded.NewPlasmaApi(z)
	defer z.StopPanic()

	types.GovernanceAddress = g.User1.Address

	activateDynamicPlasma(z)

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
