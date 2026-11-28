package tests

import (
	"math/big"
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

// activatePillarJailSpork creates and activates a spork, points
// PillarJailSpork at it, registers its id as implemented and advances
// to its enforcement height. The spork id and the implemented-spork
// map are restored when the test ends.
func activatePillarJailSpork(t *testing.T, z mock.MockZenon) {
	t.Helper()
	saveSporkState(t)
	savedId := types.PillarJailSpork.SporkId
	t.Cleanup(func() { types.PillarJailSpork.SporkId = savedId })

	spork := createAndActivateSpork(t, z, "pillar-jail")
	types.PillarJailSpork.SporkId = spork.Id
	types.ImplementedSporksMap[spork.Id] = true
	z.InsertMomentumsTo(spork.EnforcementHeight + 1)

	active, err := z.Chain().GetFrontierMomentumStore().IsSporkActive(types.PillarJailSpork)
	common.FailIfErr(t, err)
	if !active {
		t.Fatal("pillar jail spork is not active after its enforcement height")
	}
}

func unjailSend(address types.Address, name string, amount *big.Int) *nom.AccountBlock {
	return &nom.AccountBlock{
		Address:       address,
		ToAddress:     types.PillarContract,
		Data:          definition.ABIPillars.PackMethodPanic(definition.UnjailMethodName, name),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        amount,
	}
}

func znnTotalSupply(t *testing.T, z mock.MockZenon) *big.Int {
	t.Helper()
	token, err := embedded.NewTokenApi(z).GetByZts(types.ZnnTokenStandard)
	common.FailIfErr(t, err)
	return new(big.Int).Set(token.TotalSupply)
}

// Before the spork the method does not exist: the send is rejected and
// no balance moves.
func TestPillarUnjail_SendRejectedBeforeSpork(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()

	z.InsertSendBlock(unjailSend(g.Pillar1.Address, g.Pillar1Name, constants.PillarUnjailAmount), constants.ErrContractMethodNotFound, mock.NoVmChanges)
	insertMomentums(z, 3)
	z.ExpectBalance(g.Pillar1.Address, types.ZnnTokenStandard, 1000*g.Zexp)
}

// An amount other than exactly 100 ZNN fails ValidateSendBlock at send
// time, so nothing reaches the chain.
func TestPillarUnjail_WrongAmountRejectedAtSend(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()
	activatePillarJailSpork(t, z)

	z.InsertSendBlock(unjailSend(g.Pillar1.Address, g.Pillar1Name, big.NewInt(99*g.Zexp)), constants.ErrInvalidTokenOrAmount, mock.NoVmChanges)
	insertMomentums(z, 3)
	z.ExpectBalance(g.Pillar1.Address, types.ZnnTokenStandard, 1000*g.Zexp)
}

// A pillar without a jail record cannot be unjailed: the 100 ZNN come
// back and nothing is burned.
func TestPillarUnjail_NotJailedRefunded(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()
	activatePillarJailSpork(t, z)
	supply := znnTotalSupply(t, z)

	defer z.CallContract(unjailSend(g.Pillar1.Address, g.Pillar1Name, constants.PillarUnjailAmount)).Error(t, constants.ErrPillarNotJailed)
	z.InsertNewMomentum()
	z.ExpectBalance(g.Pillar1.Address, types.ZnnTokenStandard, 900*g.Zexp)
	insertMomentums(z, 2)
	autoreceive(t, z, g.Pillar1.Address)
	z.InsertNewMomentum()
	z.ExpectBalance(g.Pillar1.Address, types.ZnnTokenStandard, 1000*g.Zexp)

	if got := znnTotalSupply(t, z); got.Cmp(supply) != 0 {
		t.Fatalf("ZNN total supply = %v, want %v", got, supply)
	}
	pillar, err := embedded.NewPillarApi(z, true).GetByName(g.Pillar1Name)
	common.FailIfErr(t, err)
	if pillar.IsJailed {
		t.Fatal("pillar reports isJailed true without a jail record")
	}
}

// Only the pillar's owner can unjail it.
func TestPillarUnjail_NonOwnerRefunded(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()
	activatePillarJailSpork(t, z)
	supply := znnTotalSupply(t, z)

	z.ExpectBalance(g.User1.Address, types.ZnnTokenStandard, 12000*g.Zexp)
	defer z.CallContract(unjailSend(g.User1.Address, g.Pillar1Name, constants.PillarUnjailAmount)).Error(t, constants.ErrPermissionDenied)
	z.InsertNewMomentum()
	z.ExpectBalance(g.User1.Address, types.ZnnTokenStandard, 11900*g.Zexp)
	insertMomentums(z, 2)
	autoreceive(t, z, g.User1.Address)
	z.InsertNewMomentum()
	z.ExpectBalance(g.User1.Address, types.ZnnTokenStandard, 12000*g.Zexp)

	if got := znnTotalSupply(t, z); got.Cmp(supply) != 0 {
		t.Fatalf("ZNN total supply = %v, want %v", got, supply)
	}
}
