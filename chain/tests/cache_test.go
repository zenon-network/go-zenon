package tests

import (
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

func activateSpork(z mock.MockZenon) {
	sporkAPI := embedded.NewSporkApi(z)
	z.InsertSendBlock(&nom.AccountBlock{
		Address:   g.Spork.Address,
		ToAddress: types.SporkContract,
		Data: definition.ABISpork.PackMethodPanic(definition.SporkCreateMethodName,
			"spork-accelerator",              // name
			"activate spork for accelerator", // description
		),
	}, nil, mock.SkipVmChanges)
	z.InsertNewMomentum()

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
	types.AcceleratorSpork.SporkId = id
	types.ImplementedSporksMap[id] = true
	z.InsertMomentumsTo(20)
}

func TestCache_ChainPlasma(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()
	ledgerApi := api.NewLedgerApi(z)

	z.InsertSendBlock(&nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     g.User6.Address,
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(10 * g.Zexp),
	}, nil, mock.SkipVmChanges)
	z.InsertNewMomentum()

	momentums, err := ledgerApi.GetMomentumsByHeight(1, 2)
	common.FailIfErr(t, err)

	z.InsertSendBlock(&nom.AccountBlock{
		Address:              g.User1.Address,
		ToAddress:            g.User6.Address,
		TokenStandard:        types.ZnnTokenStandard,
		Amount:               big.NewInt(10 * g.Zexp),
		MomentumAcknowledged: momentums.List[0].Identifier(),
	}, nil, mock.SkipVmChanges)
	z.InsertNewMomentum()

	store := z.Chain().GetFrontierCacheStore()
	current, err := store.GetChainPlasma(g.User1.Address)
	common.FailIfErr(t, err)
	common.ExpectAmount(t, current, big.NewInt(42000))

	store = z.Chain().GetCacheStore(momentums.List[1].Identifier())
	current, err = store.GetChainPlasma(g.User1.Address)
	common.FailIfErr(t, err)
	common.ExpectAmount(t, current, big.NewInt(21000))

	store = z.Chain().GetCacheStore(momentums.List[0].Identifier())
	current, err = store.GetChainPlasma(g.User1.Address)
	common.FailIfErr(t, err)
	common.ExpectAmount(t, current, common.Big0)
}

func TestCache_Spork(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()
	ledgerApi := api.NewLedgerApi(z)

	activateSpork(z)

	momentums, err := ledgerApi.GetMomentumsByHeight(1, 20)
	common.FailIfErr(t, err)

	store := z.Chain().GetCacheStore(momentums.List[0].Identifier())
	isActive, err := store.IsSporkActive(types.AcceleratorSpork)
	common.FailIfErr(t, err)
	common.Expect(t, isActive, false)

	store = z.Chain().GetCacheStore(momentums.List[7].Identifier())
	isActive, err = store.IsSporkActive(types.AcceleratorSpork)
	common.FailIfErr(t, err)
	common.Expect(t, isActive, false)

	store = z.Chain().GetCacheStore(momentums.List[8].Identifier())
	isActive, err = store.IsSporkActive(types.AcceleratorSpork)
	common.FailIfErr(t, err)
	common.Expect(t, isActive, true)

	store = z.Chain().GetCacheStore(momentums.List[19].Identifier())
	isActive, err = store.IsSporkActive(types.AcceleratorSpork)
	common.FailIfErr(t, err)
	common.Expect(t, isActive, true)
}

func TestCache_FusedPlasma(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()
	ledgerApi := api.NewLedgerApi(z)

	constants.FuseExpiration = 100

	defer z.CallContract(&nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     types.PlasmaContract,
		Data:          definition.ABIPlasma.PackMethodPanic(definition.FuseMethodName, g.User6.Address),
		TokenStandard: types.QsrTokenStandard,
		Amount:        big.NewInt(10 * g.Zexp),
	}).Error(t, nil)

	z.InsertMomentumsTo(101)

	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.PlasmaContract,
		Data:      definition.ABIPlasma.PackMethodPanic(definition.CancelFuseMethodName, types.HexToHashPanic("6fce867a507bf026e4299761b6dd7fa51d288fed75716adcbd71bd6d241fc7ee")),
	}).Error(t, nil)

	z.InsertNewMomentum()
	z.InsertNewMomentum()

	momentum, err := ledgerApi.GetMomentumsByHeight(1, 1)
	common.FailIfErr(t, err)

	store := z.Chain().GetCacheStore(momentum.List[0].Identifier())
	amount, err := store.GetStakeBeneficialAmount(g.User6.Address)
	common.FailIfErr(t, err)
	common.ExpectAmount(t, amount, common.Big0)

	momentum, err = ledgerApi.GetMomentumsByHeight(3, 1)
	common.FailIfErr(t, err)

	store = z.Chain().GetCacheStore(momentum.List[0].Identifier())
	amount, err = store.GetStakeBeneficialAmount(g.User6.Address)
	common.FailIfErr(t, err)
	common.ExpectAmount(t, amount, big.NewInt(10*g.Zexp))

	momentum, err = ledgerApi.GetMomentumsByHeight(102, 1)
	common.FailIfErr(t, err)

	store = z.Chain().GetCacheStore(momentum.List[0].Identifier())
	amount, err = store.GetStakeBeneficialAmount(g.User6.Address)
	common.FailIfErr(t, err)
	common.ExpectAmount(t, amount, big.NewInt(10*g.Zexp))

	momentum, err = ledgerApi.GetMomentumsByHeight(103, 1)
	common.FailIfErr(t, err)

	store = z.Chain().GetCacheStore(momentum.List[0].Identifier())
	amount, err = store.GetStakeBeneficialAmount(g.User6.Address)
	common.FailIfErr(t, err)
	common.ExpectAmount(t, amount, common.Big0)
}

func TestCache_Rollback(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()

	z.InsertMomentumsTo(200)

	frontier := z.Chain().GetFrontierCacheStore().Identifier()
	common.Expect(t, frontier.Height, 200)

	momentum, err := z.Chain().GetFrontierMomentumStore().GetMomentumByHeight(99)
	common.FailIfErr(t, err)

	insert := z.Chain().AcquireInsert("")
	err = z.Chain().RollbackCacheTo(insert, momentum.Identifier())
	insert.Unlock()

	// Expect rollback to fail when trying to rollback more than rollbackCacheSize
	common.ExpectTrue(t, err != nil)
	frontier = z.Chain().GetFrontierCacheStore().Identifier()
	common.Expect(t, frontier.Height, 200)

	momentum, err = z.Chain().GetFrontierMomentumStore().GetMomentumByHeight(100)
	common.FailIfErr(t, err)

	insert = z.Chain().AcquireInsert("")
	err = z.Chain().RollbackCacheTo(insert, momentum.Identifier())
	insert.Unlock()

	common.ExpectTrue(t, err == nil)
	frontier = z.Chain().GetFrontierCacheStore().Identifier()
	common.Expect(t, frontier.Height, 100)
}
