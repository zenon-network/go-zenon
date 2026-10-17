package tests

import (
	"testing"

	g "github.com/zenon-network/go-zenon/chain/genesis/mock"
	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/rpc/api/embedded"
	"github.com/zenon-network/go-zenon/vm/constants"
	"github.com/zenon-network/go-zenon/vm/embedded/definition"
	"github.com/zenon-network/go-zenon/zenon/mock"
)

// TestDynamicPlasma_SetPlasmaVariables_CeilingUpperBound pins the largest
// MaxBasePlasmaInMomentum the setter accepts to 100,000 base blocks. A
// momentum is capped at the protocol message size (10 MiB), which holds a
// few tens of thousands of blocks at most, so a ceiling above this can
// never be used and only makes every ceiling-derived bound (the contract
// block cap, the verifier's content-count check) meaningless.
func TestDynamicPlasma_SetPlasmaVariables_CeilingUpperBound(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()
	saveGovernanceAddress(t)

	types.GovernanceAddress = g.User1.Address

	activateDynamicPlasma(t, z)

	setCeiling := func(maxBasePlasma uint64) *nom.AccountBlock {
		return &nom.AccountBlock{
			Address:       g.User1.Address,
			ToAddress:     types.PlasmaContract,
			TokenStandard: types.ZeroTokenStandard,
			Amount:        common.Big0,
			Data: definition.ABIPlasma.PackMethodPanic(definition.SetVariablesMethodName,
				maxBasePlasma, uint64(1), uint64(1), uint8(10), uint8(20)),
		}
	}

	limit := uint64(100000 * constants.AccountBlockBasePlasma)
	common.Expect(t, definition.MaxBasePlasmaInMomentumUpperLimit, limit)

	// The limit itself is accepted, executed and persisted.
	defer z.CallContract(setCeiling(limit)).Error(t, nil)
	insertMomentums(z, 2)
	plasmaApi := embedded.NewPlasmaApi(z)
	common.Json(plasmaApi.GetVariables()).Equals(t, `
{
	"MaxBasePlasmaInMomentum": 2100000000,
	"FusedPlasmaTarget": 1,
	"PowPlasmaTarget": 1,
	"MaxPriceChangePercent": 10,
	"PriceChangeDenominator": 20
}`)

	// One above it is rejected at send validation and leaves the stored
	// configuration untouched.
	z.InsertSendBlock(setCeiling(limit+1), constants.ErrForbiddenParam, mock.SkipVmChanges)
	insertMomentums(z, 2)
	common.Json(plasmaApi.GetVariables()).Equals(t, `
{
	"MaxBasePlasmaInMomentum": 2100000000,
	"FusedPlasmaTarget": 1,
	"PowPlasmaTarget": 1,
	"MaxPriceChangePercent": 10,
	"PriceChangeDenominator": 20
}`)
}
