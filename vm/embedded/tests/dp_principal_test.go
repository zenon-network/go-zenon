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
	"github.com/zenon-network/go-zenon/vm/embedded/implementation"
	"github.com/zenon-network/go-zenon/zenon/mock"
)

func setVariablesFrom(from types.Address, maxBasePlasma uint64) *nom.AccountBlock {
	return &nom.AccountBlock{
		Address:       from,
		ToAddress:     types.PlasmaContract,
		TokenStandard: types.ZeroTokenStandard,
		Amount:        common.Big0,
		Data: definition.ABIPlasma.PackMethodPanic(definition.SetVariablesMethodName,
			maxBasePlasma, maxBasePlasma/2, maxBasePlasma/2, uint8(10), uint8(20)),
	}
}

func expectMaxBasePlasma(t *testing.T, plasmaApi *embedded.PlasmaApi, want uint64) {
	t.Helper()
	variables, err := plasmaApi.GetVariables()
	common.FailIfErr(t, err)
	if variables.MaxBasePlasmaInMomentum != want {
		t.Fatalf("MaxBasePlasmaInMomentum = %d, want %d", variables.MaxBasePlasmaInMomentum, want)
	}
}

// TestDynamicPlasma_SetPlasmaVariables_Principal pins who may change the
// plasma variables: the genesis spork address at any height, the community
// spork address only inside the same windows that let it manage sporks, and
// nobody else. Pillar1 stands in for the community spork address with the
// original window [30, 35) and the renewal window [40, 45): activating the
// spork in the mock leaves the chain near height 20, so the windows sit
// above it and the test starts with both closed.
func TestDynamicPlasma_SetPlasmaVariables_Principal(t *testing.T) {
	z := mock.NewMockZenon(t)
	plasmaApi := embedded.NewPlasmaApi(z)
	defer z.StopPanic()
	pinCommunitySporkWindow(t, g.Pillar1.Address, 30, 35, 40, 45)
	activateDynamicPlasma(t, z)

	// An ordinary account is refused at the send.
	z.InsertSendBlock(setVariablesFrom(g.User1.Address, 1050000), constants.ErrPermissionDenied, mock.SkipVmChanges)

	// The genesis spork address is accepted.
	z.InsertSendBlock(setVariablesFrom(g.Spork.Address, 1050000), nil, mock.SkipVmChanges)
	insertMomentums(z, 2)
	expectMaxBasePlasma(t, plasmaApi, 1050000)

	// The community spork address passes the send but is refused at the
	// receive while no window is open.
	defer z.CallContract(setVariablesFrom(g.Pillar1.Address, 1100000)).Error(t, constants.ErrPermissionDenied)
	insertMomentums(z, 2)
	expectMaxBasePlasma(t, plasmaApi, 1050000)

	// Confirmed at 30: the original start height is inclusive.
	z.InsertMomentumsTo(29)
	z.InsertSendBlock(setVariablesFrom(g.Pillar1.Address, 1100000), nil, mock.SkipVmChanges)
	insertMomentums(z, 2)
	expectMaxBasePlasma(t, plasmaApi, 1100000)

	// Confirmed at 35: the original end height is exclusive.
	z.InsertMomentumsTo(34)
	defer z.CallContract(setVariablesFrom(g.Pillar1.Address, 1150000)).Error(t, constants.ErrPermissionDenied)
	insertMomentums(z, 2)
	expectMaxBasePlasma(t, plasmaApi, 1100000)

	// Confirmed at 40: the renewal start height is inclusive.
	z.InsertMomentumsTo(39)
	z.InsertSendBlock(setVariablesFrom(g.Pillar1.Address, 1150000), nil, mock.SkipVmChanges)
	insertMomentums(z, 2)
	expectMaxBasePlasma(t, plasmaApi, 1150000)

	// Confirmed at 45: the renewal end height is exclusive.
	z.InsertMomentumsTo(44)
	defer z.CallContract(setVariablesFrom(g.Pillar1.Address, 1200000)).Error(t, constants.ErrPermissionDenied)
	insertMomentums(z, 2)
	expectMaxBasePlasma(t, plasmaApi, 1150000)

	// The genesis spork address does not depend on the windows.
	z.InsertSendBlock(setVariablesFrom(g.Spork.Address, 1200000), nil, mock.SkipVmChanges)
	insertMomentums(z, 2)
	expectMaxBasePlasma(t, plasmaApi, 1200000)
}

// TestDynamicPlasma_SetPlasmaVariables_NoCompiledPrincipal checks that the
// address the setter used to be compiled with carries no authority on a chain
// whose genesis names another spork address, which is every chain except the
// docker devnet that this key was taken from.
func TestDynamicPlasma_SetPlasmaVariables_NoCompiledPrincipal(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()
	activateDynamicPlasma(t, z)

	method := &implementation.SetVariablesMethod{MethodName: definition.SetVariablesMethodName}
	formerPrincipal := types.ParseAddressPanic("z1qq9n7fpaqd8lpcljandzmx4xtku9w4ftwyg0mq")
	if formerPrincipal == *types.SporkAddress || formerPrincipal == types.CommunitySporkAddress {
		t.Fatal("the test genesis must not name the former compiled principal")
	}
	block := setVariablesFrom(formerPrincipal, 1050000)
	block.BlockType = nom.BlockTypeUserSend
	if err := method.ValidateSendBlock(block); err != constants.ErrPermissionDenied {
		t.Fatalf("ValidateSendBlock from the former compiled principal = %v, want %v", err, constants.ErrPermissionDenied)
	}
	block = setVariablesFrom(g.Spork.Address, 1050000)
	block.BlockType = nom.BlockTypeUserSend
	if err := method.ValidateSendBlock(block); err != nil {
		t.Fatalf("ValidateSendBlock from the genesis spork address = %v, want nil", err)
	}
}

// TestDynamicPlasma_SetPlasmaVariables_SporkAddressIsCommunityAddress pins the
// corner the rule shares with the Spork contract: a genesis spork address that
// is also the community spork address is bounded by the community windows,
// because the receive-time check keys on the address alone.
func TestDynamicPlasma_SetPlasmaVariables_SporkAddressIsCommunityAddress(t *testing.T) {
	z := mock.NewMockZenon(t)
	plasmaApi := embedded.NewPlasmaApi(z)
	defer z.StopPanic()
	// Activate first: once the spork address is also the community address,
	// the Spork contract applies the same windows to it.
	activateDynamicPlasma(t, z)
	pinCommunitySporkWindow(t, g.Spork.Address, 30, 35, 40, 45)

	// Refused at the receive while no window is open, although the sender
	// is the genesis spork address.
	defer z.CallContract(setVariablesFrom(g.Spork.Address, 1050000)).Error(t, constants.ErrPermissionDenied)
	insertMomentums(z, 2)
	expectMaxBasePlasma(t, plasmaApi, definition.DefaultMaxBasePlasmaInMomentum)

	// Confirmed at 30: accepted inside the window.
	z.InsertMomentumsTo(29)
	z.InsertSendBlock(setVariablesFrom(g.Spork.Address, 1050000), nil, mock.SkipVmChanges)
	insertMomentums(z, 2)
	expectMaxBasePlasma(t, plasmaApi, 1050000)
}
