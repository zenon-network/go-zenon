package tests

import (
	"math/big"
	"testing"

	cache "github.com/zenon-network/go-zenon/chain/cache/storage"
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

// TestCache_RollbackAfterPrune exercises the prune-then-restore path: fused-amount
// cache entries get pruned once the beneficiary's own acknowledged height advances
// past them, and rolling the cache back to a point before that pruning must
// restore the pruned entries.
func TestCache_RollbackAfterPrune(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()

	constants.FuseExpiration = 5

	// g.User1 fuses QSR to g.User6 twice, producing two distinct fused-amount
	// cache entries for the beneficiary.
	fuse1 := z.InsertSendBlock(&nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     types.PlasmaContract,
		Data:          definition.ABIPlasma.PackMethodPanic(definition.FuseMethodName, g.User6.Address),
		TokenStandard: types.QsrTokenStandard,
		Amount:        big.NewInt(10 * g.Zexp),
	}, nil, mock.SkipVmChanges)
	z.InsertNewMomentum() // confirm the first fuse send
	z.InsertNewMomentum() // confirm the auto-generated contract-receive

	fuse1Identifier := z.Chain().GetFrontierCacheStore().Identifier()
	amountAfterFuse1, err := z.Chain().GetCacheStore(fuse1Identifier).GetStakeBeneficialAmount(g.User6.Address)
	common.FailIfErr(t, err)
	common.ExpectAmount(t, amountAfterFuse1, big.NewInt(10*g.Zexp))

	z.InsertSendBlock(&nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     types.PlasmaContract,
		Data:          definition.ABIPlasma.PackMethodPanic(definition.FuseMethodName, g.User6.Address),
		TokenStandard: types.QsrTokenStandard,
		Amount:        big.NewInt(10 * g.Zexp),
	}, nil, mock.SkipVmChanges)
	z.InsertNewMomentum() // confirm the second fuse send
	z.InsertNewMomentum() // confirm the auto-generated contract-receive

	fuse2Identifier := z.Chain().GetFrontierCacheStore().Identifier()
	amountAfterFuse2, err := z.Chain().GetCacheStore(fuse2Identifier).GetStakeBeneficialAmount(g.User6.Address)
	common.FailIfErr(t, err)
	common.ExpectAmount(t, amountAfterFuse2, big.NewInt(20*g.Zexp))

	// let the first fusion expire
	z.InsertMomentumsTo(fuse1Identifier.Height + constants.FuseExpiration + 1)

	// cancel the first fuse only, leaving the second fusion (and its plasma)
	// with g.User6, and producing a third fused-amount cache entry for it.
	z.InsertSendBlock(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.PlasmaContract,
		Data:      definition.ABIPlasma.PackMethodPanic(definition.CancelFuseMethodName, fuse1.Hash),
	}, nil, mock.SkipVmChanges)
	z.InsertNewMomentum() // confirm the cancel send
	z.InsertNewMomentum() // confirm the auto-generated contract-receive (cancel applied)

	cancelIdentifier := z.Chain().GetFrontierCacheStore().Identifier()
	amountAfterCancel, err := z.Chain().GetCacheStore(cancelIdentifier).GetStakeBeneficialAmount(g.User6.Address)
	common.FailIfErr(t, err)
	common.ExpectAmount(t, amountAfterCancel, big.NewInt(10*g.Zexp))

	// g.User6 sends an unrelated block, paid for with the plasma from the
	// remaining fusion. This advances g.User6's own acknowledged height past
	// the fuse/cancel entries and drives pruneAccountCache to delete the two
	// now-expired entries (fuse1, fuse2), keeping only the last one (cancel).
	z.InsertSendBlock(&nom.AccountBlock{
		Address:       g.User6.Address,
		ToAddress:     g.User2.Address,
		TokenStandard: types.ZeroTokenStandard,
		Amount:        common.Big0,
	}, nil, mock.SkipVmChanges)
	z.InsertNewMomentum() // prunes the fuse1 and fuse2 entries

	pruneIdentifier := z.Chain().GetFrontierCacheStore().Identifier()
	common.ExpectTrue(t, pruneIdentifier.Height-cancelIdentifier.Height < uint64(cache.GetRollbackCacheSize()))

	// Roll the cache back to before the pruning momentum.
	insert := z.Chain().AcquireInsert("")
	err = z.Chain().RollbackCacheTo(insert, cancelIdentifier)
	insert.Unlock()
	common.FailIfErr(t, err)

	frontier := z.Chain().GetFrontierCacheStore().Identifier()
	common.Expect(t, frontier.Height, cancelIdentifier.Height)

	// The two pruned entries must be restored.
	restoredFuse1, err := z.Chain().GetCacheStore(fuse1Identifier).GetStakeBeneficialAmount(g.User6.Address)
	common.FailIfErr(t, err)
	common.ExpectAmount(t, restoredFuse1, amountAfterFuse1)

	restoredFuse2, err := z.Chain().GetCacheStore(fuse2Identifier).GetStakeBeneficialAmount(g.User6.Address)
	common.FailIfErr(t, err)
	common.ExpectAmount(t, restoredFuse2, amountAfterFuse2)

	restoredCancel, err := z.Chain().GetCacheStore(cancelIdentifier).GetStakeBeneficialAmount(g.User6.Address)
	common.FailIfErr(t, err)
	common.ExpectAmount(t, restoredCancel, amountAfterCancel)

	// Re-apply forward: catch the cache back up to the chain frontier and
	// confirm the end state is identical to what it was before the rollback.
	common.FailIfErr(t, z.Chain().Init())

	frontier = z.Chain().GetFrontierCacheStore().Identifier()
	common.Expect(t, frontier.Height, pruneIdentifier.Height)

	finalCancelAmount, err := z.Chain().GetCacheStore(cancelIdentifier).GetStakeBeneficialAmount(g.User6.Address)
	common.FailIfErr(t, err)
	common.ExpectAmount(t, finalCancelAmount, amountAfterCancel)
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
