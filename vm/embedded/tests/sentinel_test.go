package tests

import (
	"fmt"
	"math/big"
	"testing"
	"time"

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

func depositQsr(z mock.MockZenon, t *testing.T, address types.Address, amount *big.Int) {
	sentinelApi := embedded.NewSentinelApi(z)
	initialQsrStr, err := sentinelApi.GetDepositedQsr(address)
	common.DealWithErr(err)
	initialQsr := common.StringToBigInt(initialQsrStr)
	// Deposit QSR
	z.InsertSendBlock(&nom.AccountBlock{
		Address:       address,
		ToAddress:     types.SentinelContract,
		Data:          definition.ABISentinel.PackMethodPanic(definition.DepositQsrMethodName),
		TokenStandard: types.QsrTokenStandard,
		Amount:        amount,
	}, nil, mock.SkipVmChanges)
	z.InsertNewMomentum()

	finalQsr, err := sentinelApi.GetDepositedQsr(address)
	common.ExpectString(t, fmt.Sprintf("%v", new(big.Int).Add(initialQsr, amount)), fmt.Sprintf("%v", finalQsr))
}

func withdrawQsr(z mock.MockZenon, t *testing.T, address types.Address) {
	sentinelApi := embedded.NewSentinelApi(z)
	initialQsrStr, err := sentinelApi.GetDepositedQsr(address)
	initialQsr := common.StringToBigInt(initialQsrStr)

	common.DealWithErr(err)
	if initialQsr.Cmp(big.NewInt(0)) == 0 {
		// Try to withdraw QSR
		defer z.CallContract(&nom.AccountBlock{
			Address:   address,
			ToAddress: types.SentinelContract,
			Data:      definition.ABIPillars.PackMethodPanic(definition.WithdrawQsrMethodName),
		}).Error(t, constants.ErrNothingToWithdraw)
		z.InsertNewMomentum()
	} else {
		// Withdraw QSR
		defer z.CallContract(&nom.AccountBlock{
			Address:   address,
			ToAddress: types.SentinelContract,
			Data:      definition.ABIPillars.PackMethodPanic(definition.WithdrawQsrMethodName),
		}).Error(t, nil)
		z.InsertNewMomentum()
	}
	common.Json(sentinelApi.GetDepositedQsr(address)).Equals(t, `"0"`)
}

func registerSentinel(z mock.MockZenon, t *testing.T, address types.Address) {
	sentinelApi := embedded.NewSentinelApi(z)

	depositQsr(z, t, address, constants.SentinelQsrDepositAmount)
	defer z.CallContract(&nom.AccountBlock{
		Address:       address,
		ToAddress:     types.SentinelContract,
		Data:          definition.ABISentinel.PackMethodPanic(definition.RegisterSentinelMethodName),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        constants.SentinelZnnRegisterAmount,
	}).Error(t, nil)
	z.InsertNewMomentum()

	common.Json(sentinelApi.GetDepositedQsr(address)).Equals(t, `"0"`)
	sentinel, err := sentinelApi.GetByOwner(address)
	common.DealWithErr(err)
	common.ExpectTrue(t, sentinel.Active)
	common.ExpectTrue(t, !sentinel.CanBeRevoked)
	common.ExpectString(t, fmt.Sprintf("%v", sentinel.Owner), fmt.Sprintf("%v", address))
}

// - test that users can deposit QSR
// - test that RPC method displays deposited QSR
func TestSentinel_DepositQSR(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()

	depositQsr(z, t, g.User1.Address, constants.SentinelQsrDepositAmount)
}

// - test that users can not withdraw QSR if not deposit before
// - test that users can deposit QSR
// - test that users can withdraw QSR
func TestSentinel_WithdrawQSR(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()

	withdrawQsr(z, t, g.User1.Address)
	depositQsr(z, t, g.User1.Address, constants.SentinelQsrDepositAmount)
	withdrawQsr(z, t, g.User1.Address)
}

// - test that it's not allowed to insert a block which doesn't have 5K amount
// - test that it's not allowed to insert a block which doesn't have the ZNN ZTS
func TestSentinel_RegisterWithInsufficientFunds(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()

	// Try to register with insufficient ZNN
	z.InsertSendBlock(&nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     types.SentinelContract,
		Data:          definition.ABISentinel.PackMethodPanic(definition.RegisterSentinelMethodName),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        new(big.Int).Sub(constants.SentinelQsrDepositAmount, big.NewInt(1000)),
	}, constants.ErrInvalidTokenOrAmount, mock.NoVmChanges)
	z.InsertNewMomentum()

	// Try to register with invalid ZTS
	z.InsertSendBlock(&nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     types.SentinelContract,
		Data:          definition.ABISentinel.PackMethodPanic(definition.RegisterSentinelMethodName),
		TokenStandard: types.QsrTokenStandard,
		Amount:        constants.SentinelQsrDepositAmount,
	}, constants.ErrInvalidTokenOrAmount, mock.NoVmChanges)
	z.InsertNewMomentum()
}

// - test that users can not register a sentinel without depositing QSR ahead of time
func TestSentinel_RegisterNothingDeposited(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).HideHashes().Equals(t, `
t=2001-09-09T01:46:50+0000 lvl=dbug msg="invalid register - not enough deposited qsr" module=embedded contract=sentinel address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz
`)

	defer z.CallContract(&nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     types.SentinelContract,
		Data:          definition.ABISentinel.PackMethodPanic(definition.RegisterSentinelMethodName),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        constants.SentinelZnnRegisterAmount,
	}).Error(t, constants.ErrNotEnoughDepositedQsr)
	z.InsertNewMomentum()
}

// - deposit 49'999.99999999 QSR
// - try to register, not enough QSR
func TestSentinel_RegisterWithInsufficientDeposited(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).HideHashes().Equals(t, `
t=2001-09-09T01:47:00+0000 lvl=dbug msg="invalid register - not enough deposited qsr" module=embedded contract=sentinel address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz
`)

	depositQsr(z, t, g.User1.Address, new(big.Int).Sub(constants.SentinelQsrDepositAmount, big.NewInt(1)))
	defer z.CallContract(&nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     types.SentinelContract,
		Data:          definition.ABISentinel.PackMethodPanic(definition.RegisterSentinelMethodName),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        constants.SentinelZnnRegisterAmount,
	}).Error(t, constants.ErrNotEnoughDepositedQsr)
	z.InsertNewMomentum()
}

// - deposit 50'001 QSR
// - register sentinel successfully
// - call RPC to show that there is nothing to withdraw, meaning that the deposited QSR was consumed successfully
// - call RPC to show that the sentinel was saved
func TestSentinel_RegisterSentinelSuccessfully(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).HideHashes().Equals(t, `
t=2001-09-09T01:47:00+0000 lvl=dbug msg="successfully register" module=embedded contract=sentinel sentinel="&{SentinelInfoKey:{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz} RegistrationTimestamp:1000000020 RevokeTimestamp:0 ZnnAmount:+500000000000 QsrAmount:+5000000000000}"
`)
	sentinelApi := embedded.NewSentinelApi(z)

	depositQsr(z, t, g.User1.Address, new(big.Int).Add(constants.SentinelQsrDepositAmount, big.NewInt(1)))
	defer z.CallContract(&nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     types.SentinelContract,
		Data:          definition.ABISentinel.PackMethodPanic(definition.RegisterSentinelMethodName),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        constants.SentinelZnnRegisterAmount,
	}).Error(t, nil)
	z.InsertNewMomentum()

	common.Json(sentinelApi.GetDepositedQsr(g.User1.Address)).Equals(t, `"1"`)
	common.Json(sentinelApi.GetByOwner(g.User1.Address)).Equals(t, `
{
	"owner": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
	"registrationTimestamp": 1000000020,
	"isRevocable": false,
	"revokeCooldown": 40,
	"active": true
}`)
}

// - test that you can not register another sentinel if one already exists with the same owner
func TestSentinel_DoubleRegister(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).HideHashes().Equals(t, `
t=2001-09-09T01:47:00+0000 lvl=dbug msg="successfully register" module=embedded contract=sentinel sentinel="&{SentinelInfoKey:{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz} RegistrationTimestamp:1000000020 RevokeTimestamp:0 ZnnAmount:+500000000000 QsrAmount:+5000000000000}"
t=2001-09-09T01:47:20+0000 lvl=dbug msg="invalid register - existing address" module=embedded contract=sentinel address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz
`)
	sentinelApi := embedded.NewSentinelApi(z)

	registerSentinel(z, t, g.User1.Address)
	common.Json(sentinelApi.GetByOwner(g.User1.Address)).Equals(t, `
{
	"owner": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
	"registrationTimestamp": 1000000020,
	"isRevocable": false,
	"revokeCooldown": 40,
	"active": true
}`)

	z.ExpectBalance(g.User1.Address, types.QsrTokenStandard, 70000*g.Zexp)
	z.ExpectBalance(g.User1.Address, types.ZnnTokenStandard, 7000*g.Zexp)
	depositQsr(z, t, g.User1.Address, constants.SentinelQsrDepositAmount)
	z.ExpectBalance(g.User1.Address, types.QsrTokenStandard, 20000*g.Zexp)
	defer z.CallContract(&nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     types.SentinelContract,
		Data:          definition.ABISentinel.PackMethodPanic(definition.RegisterSentinelMethodName),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        constants.SentinelZnnRegisterAmount,
	}).Error(t, constants.ErrAlreadyRegistered)
	z.InsertNewMomentum()
	z.InsertNewMomentum()
	autoreceive(t, z, g.User1.Address)
	z.ExpectBalance(g.User1.Address, types.QsrTokenStandard, 20000*g.Zexp)
	z.ExpectBalance(g.User1.Address, types.ZnnTokenStandard, 7000*g.Zexp)

	common.Json(sentinelApi.GetDepositedQsr(g.User1.Address)).Equals(t, `"5000000000000"`)
	common.Json(sentinelApi.GetByOwner(g.User1.Address)).Equals(t, `
{
	"owner": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
	"registrationTimestamp": 1000000020,
	"isRevocable": false,
	"revokeCooldown": 10,
	"active": true
}`)
}

// - test embedded.sentinel.getByOwner RPC when sentinel doesn't exists
func TestSentinel_GetByOwner(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()
	sentinelApi := embedded.NewSentinelApi(z)

	common.Json(sentinelApi.GetByOwner(g.User1.Address)).Equals(t, `null`)
}

//   - test embedded.sentinel.getAllActive RPC
//     -> return 'count' is the number of active sentinels not the number of inactive + active
//   - sentinel is missing from getAllActive after being revoked
func TestSentinel_GetAllActiveRPC(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).HideHashes().Equals(t, `
t=2001-09-09T01:47:00+0000 lvl=dbug msg="successfully register" module=embedded contract=sentinel sentinel="&{SentinelInfoKey:{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz} RegistrationTimestamp:1000000020 RevokeTimestamp:0 ZnnAmount:+500000000000 QsrAmount:+5000000000000}"
t=2001-09-09T01:47:20+0000 lvl=dbug msg="successfully register" module=embedded contract=sentinel sentinel="&{SentinelInfoKey:{Owner:z1qplpsv3wcm64js30jlumxlatgxxkqr6hgv30fg} RegistrationTimestamp:1000000040 RevokeTimestamp:0 ZnnAmount:+500000000000 QsrAmount:+5000000000000}"
t=2001-09-09T02:06:40+0000 lvl=dbug msg="successfully revoke" module=embedded contract=sentinel sentinel="&{SentinelInfoKey:{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz} RegistrationTimestamp:1000000020 RevokeTimestamp:1000001200 ZnnAmount:+0 QsrAmount:+0}"
t=2001-09-09T02:07:00+0000 lvl=dbug msg="successfully revoke" module=embedded contract=sentinel sentinel="&{SentinelInfoKey:{Owner:z1qplpsv3wcm64js30jlumxlatgxxkqr6hgv30fg} RegistrationTimestamp:1000000040 RevokeTimestamp:1000001220 ZnnAmount:+0 QsrAmount:+0}"
`)
	sentinelApi := embedded.NewSentinelApi(z)

	registerSentinel(z, t, g.User1.Address)
	common.Json(sentinelApi.GetAllActive(0, 5)).Equals(t, `
{
	"count": 1,
	"list": [
		{
			"owner": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
			"registrationTimestamp": 1000000020,
			"isRevocable": false,
			"revokeCooldown": 40,
			"active": true
		}
	]
}`)
	registerSentinel(z, t, g.Pillar4.Address)
	common.Json(sentinelApi.GetAllActive(0, 5)).Equals(t, `
{
	"count": 2,
	"list": [
		{
			"owner": "z1qplpsv3wcm64js30jlumxlatgxxkqr6hgv30fg",
			"registrationTimestamp": 1000000040,
			"isRevocable": false,
			"revokeCooldown": 40,
			"active": true
		},
		{
			"owner": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
			"registrationTimestamp": 1000000020,
			"isRevocable": false,
			"revokeCooldown": 20,
			"active": true
		}
	]
}`)
	z.InsertMomentumsTo(120)

	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.SentinelContract,
		Data:      definition.ABISentinel.PackMethodPanic(definition.RevokeSentinelMethodName),
	}).Error(t, nil)
	z.InsertNewMomentum()
	z.InsertNewMomentum()
	common.Json(sentinelApi.GetAllActive(0, 5)).Equals(t, `
{
	"count": 1,
	"list": [
		{
			"owner": "z1qplpsv3wcm64js30jlumxlatgxxkqr6hgv30fg",
			"registrationTimestamp": 1000000040,
			"isRevocable": false,
			"revokeCooldown": 10,
			"active": true
		}
	]
}`)
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.Pillar4.Address,
		ToAddress: types.SentinelContract,
		Data:      definition.ABISentinel.PackMethodPanic(definition.RevokeSentinelMethodName),
	}).Error(t, nil)
	z.InsertNewMomentum()
	z.InsertNewMomentum()
	common.Json(sentinelApi.GetAllActive(0, 5)).Equals(t, `
{
	"count": 0,
	"list": []
}`)
}

// - test you can not register after revoking sentinel
func TestSentinel_TryToRegisterAfterRevoking(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).HideHashes().Equals(t, `
t=2001-09-09T01:47:00+0000 lvl=dbug msg="successfully register" module=embedded contract=sentinel sentinel="&{SentinelInfoKey:{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz} RegistrationTimestamp:1000000020 RevokeTimestamp:0 ZnnAmount:+500000000000 QsrAmount:+5000000000000}"
t=2001-09-09T02:06:40+0000 lvl=dbug msg="successfully revoke" module=embedded contract=sentinel sentinel="&{SentinelInfoKey:{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz} RegistrationTimestamp:1000000020 RevokeTimestamp:1000001200 ZnnAmount:+0 QsrAmount:+0}"
t=2001-09-09T02:07:00+0000 lvl=dbug msg="invalid register - existing address" module=embedded contract=sentinel address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz
`)

	registerSentinel(z, t, g.User1.Address)
	z.InsertMomentumsTo(120)

	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.SentinelContract,
		Data:      definition.ABISentinel.PackMethodPanic(definition.RevokeSentinelMethodName),
	}).Error(t, nil)
	z.InsertNewMomentum()
	z.InsertNewMomentum()

	defer z.CallContract(&nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     types.SentinelContract,
		Data:          definition.ABISentinel.PackMethodPanic(definition.RegisterSentinelMethodName),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        constants.SentinelZnnRegisterAmount,
	}).Error(t, constants.ErrAlreadyRegistered)
	z.InsertNewMomentum()
	z.InsertNewMomentum()
	autoreceive(t, z, g.User1.Address)
	z.ExpectBalance(g.User1.Address, types.ZnnTokenStandard, 12000*g.Zexp)
}

// - cannot revoke sentinel which was already revoked
func TestSentinel_DoubleRevokeSentinel(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).HideHashes().Equals(t, `
t=2001-09-09T01:47:00+0000 lvl=dbug msg="successfully register" module=embedded contract=sentinel sentinel="&{SentinelInfoKey:{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz} RegistrationTimestamp:1000000020 RevokeTimestamp:0 ZnnAmount:+500000000000 QsrAmount:+5000000000000}"
t=2001-09-09T02:06:40+0000 lvl=dbug msg="successfully revoke" module=embedded contract=sentinel sentinel="&{SentinelInfoKey:{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz} RegistrationTimestamp:1000000020 RevokeTimestamp:1000001200 ZnnAmount:+0 QsrAmount:+0}"
t=2001-09-09T02:07:00+0000 lvl=dbug msg="invalid revoke - sentinel is already revoked" module=embedded contract=sentinel address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz
`)

	registerSentinel(z, t, g.User1.Address)
	z.InsertMomentumsTo(120)

	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.SentinelContract,
		Data:      definition.ABISentinel.PackMethodPanic(definition.RevokeSentinelMethodName),
	}).Error(t, nil)
	z.InsertNewMomentum()
	z.InsertNewMomentum()
	autoreceive(t, z, g.User1.Address)
	z.ExpectBalance(g.User1.Address, types.ZnnTokenStandard, 12000*g.Zexp)
	z.ExpectBalance(g.User1.Address, types.QsrTokenStandard, 120000*g.Zexp)

	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.SentinelContract,
		Data:      definition.ABISentinel.PackMethodPanic(definition.RevokeSentinelMethodName),
	}).Error(t, constants.ErrAlreadyRevoked)
	z.InsertNewMomentum()
}

// - cannot revoke sentinel when the owner never had a sentinel
func TestSentinel_TryToRevokeUnregisteredSentinel(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).HideHashes().Equals(t, `
t=2001-09-09T01:46:50+0000 lvl=dbug msg="invalid revoke - sentinel is not registered" module=embedded contract=sentinel address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz
`)

	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.SentinelContract,
		Data:      definition.ABISentinel.PackMethodPanic(definition.RevokeSentinelMethodName),
	}).Error(t, constants.ErrDataNonExistent)
	z.InsertNewMomentum()
	z.InsertNewMomentum()
	autoreceive(t, z, g.User1.Address)
	z.ExpectBalance(g.User1.Address, types.ZnnTokenStandard, 12000*g.Zexp)
	z.ExpectBalance(g.User1.Address, types.QsrTokenStandard, 120000*g.Zexp)
}

// - deposit 50'000 QSR
// - register sentinel successfully
// - call RPC to show that the sentinel was saved
// - insert momentums
// - can revoke successfully sentinel
// - receives both amounts back
func TestSentinel_RevokeSentinelSuccessfully(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).HideHashes().Equals(t, `
t=2001-09-09T01:47:00+0000 lvl=dbug msg="successfully register" module=embedded contract=sentinel sentinel="&{SentinelInfoKey:{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz} RegistrationTimestamp:1000000020 RevokeTimestamp:0 ZnnAmount:+500000000000 QsrAmount:+5000000000000}"
t=2001-09-09T02:06:40+0000 lvl=dbug msg="successfully revoke" module=embedded contract=sentinel sentinel="&{SentinelInfoKey:{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz} RegistrationTimestamp:1000000020 RevokeTimestamp:1000001200 ZnnAmount:+0 QsrAmount:+0}"
`)
	sentinelApi := embedded.NewSentinelApi(z)

	registerSentinel(z, t, g.User1.Address)

	common.Json(sentinelApi.GetDepositedQsr(g.User1.Address)).Equals(t, `"0"`)
	common.Json(sentinelApi.GetByOwner(g.User1.Address)).Equals(t, `
{
	"owner": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
	"registrationTimestamp": 1000000020,
	"isRevocable": false,
	"revokeCooldown": 40,
	"active": true
}`)
	z.InsertMomentumsTo(120)
	common.Json(sentinelApi.GetByOwner(g.User1.Address)).Equals(t, `
{
	"owner": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
	"registrationTimestamp": 1000000020,
	"isRevocable": false,
	"revokeCooldown": 10,
	"active": true
}`)
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.SentinelContract,
		Data:      definition.ABISentinel.PackMethodPanic(definition.RevokeSentinelMethodName),
	}).Error(t, nil)
	z.InsertNewMomentum()
	z.InsertNewMomentum()
	autoreceive(t, z, g.User1.Address)
	z.ExpectBalance(g.User1.Address, types.ZnnTokenStandard, 12000*g.Zexp)
	z.ExpectBalance(g.User1.Address, types.QsrTokenStandard, 120000*g.Zexp)
	common.Json(sentinelApi.GetByOwner(g.User1.Address)).Equals(t, `
{
	"owner": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
	"registrationTimestamp": 1000000020,
	"isRevocable": true,
	"revokeCooldown": 10,
	"active": false
}`)
}

// - register sentinel for User1
// - insert momentums to height 50
// - check uncollected reward (without successful update)
// - insert momentum to height 60*6 + 2
// - check uncollected reward (with one successful update)
func TestSentinel_UpdateForUniqueSentinel(t *testing.T) {
	z := mock.NewMockZenonWithCustomEpochDuration(t, time.Hour)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).HideHashes().Equals(t, `
t=2001-09-09T01:47:00+0000 lvl=dbug msg="successfully register" module=embedded contract=sentinel sentinel="&{SentinelInfoKey:{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz} RegistrationTimestamp:1000000020 RevokeTimestamp:0 ZnnAmount:+500000000000 QsrAmount:+5000000000000}"
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=0 pillar-name=TEST-pillar-1 reward="&{DelegationReward:+11538239999 BlockReward:+9916666627 TotalReward:+21454906626 ProducedBlockNum:119 ExpectedBlockNum:120 Weight:+1683333333333}" total-weight=2083333333333 self-weight=1683333333333
t=2001-09-09T02:46:40+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=0 pillar-name=TEST-pillar-cool reward="&{DelegationReward:+1382400000 BlockReward:+9999999960 TotalReward:+11382399960 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2083333333333 self-weight=200000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=0 pillar-name=TEST-pillar-znn reward="&{DelegationReward:+1382400000 BlockReward:+9999999960 TotalReward:+11382399960 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2083333333333 self-weight=200000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=pillar epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxsentynelxxxxxxxxxxxxxwy0r2r current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating sentinel reward" module=embedded contract=sentinel epoch=0 total-znn-reward=187200000000 total-qsr-reward=500000000000 cumulated-sentinel=1 start-time=1000000000 end-time=1000003600
t=2001-09-09T02:46:40+0000 lvl=dbug msg="giving rewards" module=embedded contract=sentinel address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz epoch=0 znn-amount=187200000000 qsr-amount=500000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=sentinel epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating stake reward" module=embedded contract=stake epoch=0 total-reward=1000000000000 cumulated-stake=0 start-time=1000000000 end-time=1000003600
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=stake epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating liquidity reward" module=embedded contract=liquidity epoch=0 znn-amount=187200000000 qsr-amount=500000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=liquidity epoch=1
t=2001-09-09T02:46:50+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg TokenName:Zenon Coin TokenSymbol:ZNN TokenDomain:zenon.network TotalSupply:+19687200000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1znnxxxxxxxxxxxxx9z4ulx}" minted-amount=187200000000 to-address=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae
t=2001-09-09T02:46:50+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 TokenName:QuasarCoin TokenSymbol:QSR TokenDomain:zenon.network TotalSupply:+181050000000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1qsrxxxxxxxxxxxxxmrhjll}" minted-amount=500000000000 to-address=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae
`)
	sentinelApi := embedded.NewSentinelApi(z)

	registerSentinel(z, t, g.User1.Address)
	z.InsertMomentumsTo(50)
	common.Json(sentinelApi.GetUncollectedReward(g.User1.Address)).Equals(t, `
{
	"address": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
	"znnAmount": "0",
	"qsrAmount": "0"
}`)
	z.InsertMomentumsTo(60*6 + 2)
	common.Json(sentinelApi.GetUncollectedReward(g.User1.Address)).Equals(t, `
{
	"address": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
	"znnAmount": "187200000000",
	"qsrAmount": "500000000000"
}`)
}

// - register sentinel for User1
// - insert momentums to height 60 * 5
// - check uncollected reward for User1 (without successful update)
// - insert momentums to height 60 * 6
// - register sentinel for User2
// - insert momentums to height 60 * 6 + 2
// - check uncollected reward for User1
// - check uncollected reward for User2
// - insert momentums to height 60 * 6 * 2
// - check uncollected reward for User1
// - insert momentums to height 60 * 6 * 2 + 2
// - check uncollected reward for User1 & User2
func TestSentinel_UpdateForMultipleSentinel(t *testing.T) {
	z := mock.NewMockZenonWithCustomEpochDuration(t, time.Hour)
	defer z.SaveLogs(common.EmbeddedLogger).HideHashes().Equals(t, `
t=2001-09-09T01:47:00+0000 lvl=dbug msg="successfully register" module=embedded contract=sentinel sentinel="&{SentinelInfoKey:{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz} RegistrationTimestamp:1000000020 RevokeTimestamp:0 ZnnAmount:+500000000000 QsrAmount:+5000000000000}"
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=0 pillar-name=TEST-pillar-1 reward="&{DelegationReward:+11538239999 BlockReward:+9916666627 TotalReward:+21454906626 ProducedBlockNum:119 ExpectedBlockNum:120 Weight:+1683333333333}" total-weight=2083333333333 self-weight=1683333333333
t=2001-09-09T02:46:40+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=0 pillar-name=TEST-pillar-cool reward="&{DelegationReward:+1382400000 BlockReward:+9999999960 TotalReward:+11382399960 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2083333333333 self-weight=200000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=0 pillar-name=TEST-pillar-znn reward="&{DelegationReward:+1382400000 BlockReward:+9999999960 TotalReward:+11382399960 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2083333333333 self-weight=200000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=pillar epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxsentynelxxxxxxxxxxxxxwy0r2r current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating sentinel reward" module=embedded contract=sentinel epoch=0 total-znn-reward=187200000000 total-qsr-reward=500000000000 cumulated-sentinel=1 start-time=1000000000 end-time=1000003600
t=2001-09-09T02:46:40+0000 lvl=dbug msg="giving rewards" module=embedded contract=sentinel address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz epoch=0 znn-amount=187200000000 qsr-amount=500000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=sentinel epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating stake reward" module=embedded contract=stake epoch=0 total-reward=1000000000000 cumulated-stake=0 start-time=1000000000 end-time=1000003600
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=stake epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating liquidity reward" module=embedded contract=liquidity epoch=0 znn-amount=187200000000 qsr-amount=500000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=liquidity epoch=1
t=2001-09-09T02:46:50+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg TokenName:Zenon Coin TokenSymbol:ZNN TokenDomain:zenon.network TotalSupply:+19687200000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1znnxxxxxxxxxxxxx9z4ulx}" minted-amount=187200000000 to-address=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae
t=2001-09-09T02:46:50+0000 lvl=dbug msg="successfully register" module=embedded contract=sentinel sentinel="&{SentinelInfoKey:{Owner:z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx} RegistrationTimestamp:1000003610 RevokeTimestamp:0 ZnnAmount:+500000000000 QsrAmount:+5000000000000}"
t=2001-09-09T02:46:50+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 TokenName:QuasarCoin TokenSymbol:QSR TokenDomain:zenon.network TotalSupply:+181050000000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1qsrxxxxxxxxxxxxxmrhjll}" minted-amount=500000000000 to-address=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae
t=2001-09-09T02:47:00+0000 lvl=info msg="received donation" module=embedded contract=common embedded=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae from-address=z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0 zts=zts1znnxxxxxxxxxxxxx9z4ulx amount=187200000000
t=2001-09-09T02:47:00+0000 lvl=info msg="received donation" module=embedded contract=common embedded=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae from-address=z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0 zts=zts1qsrxxxxxxxxxxxxxmrhjll amount=500000000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg current-height=722 last-update-height=361
t=2001-09-09T03:46:50+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=1 pillar-name=TEST-pillar-1 reward="&{DelegationReward:+10762105263 BlockReward:+9999999960 TotalReward:+20762105223 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+1183333333333}" total-weight=1583333333333 self-weight=1183333333333
t=2001-09-09T03:46:50+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=1 pillar-name=TEST-pillar-cool reward="&{DelegationReward:+1818947368 BlockReward:+9999999960 TotalReward:+11818947328 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=1583333333333 self-weight=200000000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=1 pillar-name=TEST-pillar-znn reward="&{DelegationReward:+1818947368 BlockReward:+9999999960 TotalReward:+11818947328 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=1583333333333 self-weight=200000000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=pillar epoch=2
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxsentynelxxxxxxxxxxxxxwy0r2r current-height=722 last-update-height=361
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating sentinel reward" module=embedded contract=sentinel epoch=1 total-znn-reward=187200000000 total-qsr-reward=500000000000 cumulated-sentinel=2 start-time=1000003600 end-time=1000007200
t=2001-09-09T03:46:50+0000 lvl=dbug msg="giving rewards" module=embedded contract=sentinel address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz epoch=1 znn-amount=93600000000 qsr-amount=250000000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="giving rewards" module=embedded contract=sentinel address=z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx epoch=1 znn-amount=93600000000 qsr-amount=250000000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=sentinel epoch=2
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 current-height=722 last-update-height=361
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating stake reward" module=embedded contract=stake epoch=1 total-reward=1000000000000 cumulated-stake=0 start-time=1000003600 end-time=1000007200
t=2001-09-09T03:46:50+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=stake epoch=2
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae current-height=722 last-update-height=361
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating liquidity reward" module=embedded contract=liquidity epoch=1 znn-amount=187200000000 qsr-amount=500000000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=liquidity epoch=2
`)
	defer z.StopPanic()
	sentinelApi := embedded.NewSentinelApi(z)

	registerSentinel(z, t, g.User1.Address)
	z.InsertMomentumsTo(60 * 5)
	common.Json(sentinelApi.GetUncollectedReward(g.User1.Address)).Equals(t, `
{
	"address": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
	"znnAmount": "0",
	"qsrAmount": "0"
}`)
	z.InsertMomentumsTo(60 * 6)
	registerSentinel(z, t, g.User2.Address)
	z.InsertMomentumsTo(60*6 + 2)
	common.Json(sentinelApi.GetUncollectedReward(g.User1.Address)).Equals(t, `
{
	"address": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
	"znnAmount": "187200000000",
	"qsrAmount": "500000000000"
}`)
	common.Json(sentinelApi.GetUncollectedReward(g.User2.Address)).Equals(t, `
{
	"address": "z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx",
	"znnAmount": "0",
	"qsrAmount": "0"
}`)
	z.InsertMomentumsTo(60 * 6 * 2)
	common.Json(sentinelApi.GetUncollectedReward(g.User1.Address)).Equals(t, `
{
	"address": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
	"znnAmount": "187200000000",
	"qsrAmount": "500000000000"
}`)
	z.InsertMomentumsTo(60*6*2 + 2)
	common.Json(sentinelApi.GetUncollectedReward(g.User1.Address)).Equals(t, `
{
	"address": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
	"znnAmount": "280800000000",
	"qsrAmount": "750000000000"
}`)
	common.Json(sentinelApi.GetUncollectedReward(g.User2.Address)).Equals(t, `
{
	"address": "z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx",
	"znnAmount": "93600000000",
	"qsrAmount": "250000000000"
}`)
}

// - register sentinel for User1
// - insert momentums to heigth 60 * 6 + 2 + SentinelLockTimeWindow
// - check uncollected reward for User1
// - revoke sentinel for User1
// - check uncollected reward for User1 (the uncollected reward must be the same)
func TestSentinel_UpdateAfterRevoke(t *testing.T) {
	z := mock.NewMockZenonWithCustomEpochDuration(t, time.Hour)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).HideHashes().Equals(t, `
t=2001-09-09T01:47:00+0000 lvl=dbug msg="successfully register" module=embedded contract=sentinel sentinel="&{SentinelInfoKey:{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz} RegistrationTimestamp:1000000020 RevokeTimestamp:0 ZnnAmount:+500000000000 QsrAmount:+5000000000000}"
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=0 pillar-name=TEST-pillar-1 reward="&{DelegationReward:+11538239999 BlockReward:+9916666627 TotalReward:+21454906626 ProducedBlockNum:119 ExpectedBlockNum:120 Weight:+1683333333333}" total-weight=2083333333333 self-weight=1683333333333
t=2001-09-09T02:46:40+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=0 pillar-name=TEST-pillar-cool reward="&{DelegationReward:+1382400000 BlockReward:+9999999960 TotalReward:+11382399960 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2083333333333 self-weight=200000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=0 pillar-name=TEST-pillar-znn reward="&{DelegationReward:+1382400000 BlockReward:+9999999960 TotalReward:+11382399960 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2083333333333 self-weight=200000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=pillar epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxsentynelxxxxxxxxxxxxxwy0r2r current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating sentinel reward" module=embedded contract=sentinel epoch=0 total-znn-reward=187200000000 total-qsr-reward=500000000000 cumulated-sentinel=1 start-time=1000000000 end-time=1000003600
t=2001-09-09T02:46:40+0000 lvl=dbug msg="giving rewards" module=embedded contract=sentinel address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz epoch=0 znn-amount=187200000000 qsr-amount=500000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=sentinel epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating stake reward" module=embedded contract=stake epoch=0 total-reward=1000000000000 cumulated-stake=0 start-time=1000000000 end-time=1000003600
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=stake epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating liquidity reward" module=embedded contract=liquidity epoch=0 znn-amount=187200000000 qsr-amount=500000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=liquidity epoch=1
t=2001-09-09T02:46:50+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg TokenName:Zenon Coin TokenSymbol:ZNN TokenDomain:zenon.network TotalSupply:+19687200000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1znnxxxxxxxxxxxxx9z4ulx}" minted-amount=187200000000 to-address=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae
t=2001-09-09T02:46:50+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 TokenName:QuasarCoin TokenSymbol:QSR TokenDomain:zenon.network TotalSupply:+181050000000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1qsrxxxxxxxxxxxxxmrhjll}" minted-amount=500000000000 to-address=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae
t=2001-09-09T02:47:00+0000 lvl=info msg="received donation" module=embedded contract=common embedded=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae from-address=z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0 zts=zts1znnxxxxxxxxxxxxx9z4ulx amount=187200000000
t=2001-09-09T02:47:00+0000 lvl=info msg="received donation" module=embedded contract=common embedded=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae from-address=z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0 zts=zts1qsrxxxxxxxxxxxxxmrhjll amount=500000000000
t=2001-09-09T02:53:40+0000 lvl=dbug msg="successfully revoke" module=embedded contract=sentinel sentinel="&{SentinelInfoKey:{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz} RegistrationTimestamp:1000000020 RevokeTimestamp:1000004020 ZnnAmount:+0 QsrAmount:+0}"
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg current-height=722 last-update-height=361
t=2001-09-09T03:46:50+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=1 pillar-name=TEST-pillar-1 reward="&{DelegationReward:+11520000000 BlockReward:+9999999960 TotalReward:+21519999960 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+1600000000000}" total-weight=2000000000000 self-weight=1600000000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=1 pillar-name=TEST-pillar-cool reward="&{DelegationReward:+1440000000 BlockReward:+9999999960 TotalReward:+11439999960 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2000000000000 self-weight=200000000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=1 pillar-name=TEST-pillar-znn reward="&{DelegationReward:+1440000000 BlockReward:+9999999960 TotalReward:+11439999960 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2000000000000 self-weight=200000000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=pillar epoch=2
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxsentynelxxxxxxxxxxxxxwy0r2r current-height=722 last-update-height=361
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating sentinel reward" module=embedded contract=sentinel epoch=1 total-znn-reward=187200000000 total-qsr-reward=500000000000 cumulated-sentinel=0 start-time=1000003600 end-time=1000007200
t=2001-09-09T03:46:50+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=sentinel epoch=2
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 current-height=722 last-update-height=361
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating stake reward" module=embedded contract=stake epoch=1 total-reward=1000000000000 cumulated-stake=0 start-time=1000003600 end-time=1000007200
t=2001-09-09T03:46:50+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=stake epoch=2
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae current-height=722 last-update-height=361
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating liquidity reward" module=embedded contract=liquidity epoch=1 znn-amount=187200000000 qsr-amount=500000000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=liquidity epoch=2
t=2001-09-09T03:47:00+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg TokenName:Zenon Coin TokenSymbol:ZNN TokenDomain:zenon.network TotalSupply:+19874400000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1znnxxxxxxxxxxxxx9z4ulx}" minted-amount=187200000000 to-address=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae
t=2001-09-09T03:47:00+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 TokenName:QuasarCoin TokenSymbol:QSR TokenDomain:zenon.network TotalSupply:+181550000000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1qsrxxxxxxxxxxxxxmrhjll}" minted-amount=500000000000 to-address=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae
t=2001-09-09T03:47:10+0000 lvl=info msg="received donation" module=embedded contract=common embedded=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae from-address=z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0 zts=zts1znnxxxxxxxxxxxxx9z4ulx amount=187200000000
t=2001-09-09T03:47:10+0000 lvl=info msg="received donation" module=embedded contract=common embedded=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae from-address=z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0 zts=zts1qsrxxxxxxxxxxxxxmrhjll amount=500000000000
`)
	sentinelApi := embedded.NewSentinelApi(z)
	registerSentinel(z, t, g.User1.Address)
	z.InsertMomentumsTo(uint64(60*6 + 2 + constants.SentinelLockTimeWindow))
	common.Json(sentinelApi.GetUncollectedReward(g.User1.Address)).Equals(t, `
{
	"address": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
	"znnAmount": "187200000000",
	"qsrAmount": "500000000000"
}`)
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.SentinelContract,
		Data:      definition.ABISentinel.PackMethodPanic(definition.RevokeSentinelMethodName),
	}).Error(t, nil)
	z.InsertNewMomentum()
	z.InsertNewMomentum()
	z.InsertMomentumsTo(60*6*3 + 2)
	common.Json(sentinelApi.GetUncollectedReward(g.User1.Address)).Equals(t, `
{
	"address": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
	"znnAmount": "187200000000",
	"qsrAmount": "500000000000"
}`)
}

// - register sentinel for User1
// - insert momentums to height 60*6 + 2
// - check uncollected reward for User1
// - collect reward for User1
// - receive the reward blocks for User1
// - check that User1 has no more rewards to collect
func TestSentinel_SimpleCollect(t *testing.T) {
	z := mock.NewMockZenonWithCustomEpochDuration(t, time.Hour)
	defer z.SaveLogs(common.EmbeddedLogger).HideHashes().Equals(t, `
t=2001-09-09T01:47:00+0000 lvl=dbug msg="successfully register" module=embedded contract=sentinel sentinel="&{SentinelInfoKey:{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz} RegistrationTimestamp:1000000020 RevokeTimestamp:0 ZnnAmount:+500000000000 QsrAmount:+5000000000000}"
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=0 pillar-name=TEST-pillar-1 reward="&{DelegationReward:+11538239999 BlockReward:+9916666627 TotalReward:+21454906626 ProducedBlockNum:119 ExpectedBlockNum:120 Weight:+1683333333333}" total-weight=2083333333333 self-weight=1683333333333
t=2001-09-09T02:46:40+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=0 pillar-name=TEST-pillar-cool reward="&{DelegationReward:+1382400000 BlockReward:+9999999960 TotalReward:+11382399960 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2083333333333 self-weight=200000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=0 pillar-name=TEST-pillar-znn reward="&{DelegationReward:+1382400000 BlockReward:+9999999960 TotalReward:+11382399960 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2083333333333 self-weight=200000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=pillar epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxsentynelxxxxxxxxxxxxxwy0r2r current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating sentinel reward" module=embedded contract=sentinel epoch=0 total-znn-reward=187200000000 total-qsr-reward=500000000000 cumulated-sentinel=1 start-time=1000000000 end-time=1000003600
t=2001-09-09T02:46:40+0000 lvl=dbug msg="giving rewards" module=embedded contract=sentinel address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz epoch=0 znn-amount=187200000000 qsr-amount=500000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=sentinel epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating stake reward" module=embedded contract=stake epoch=0 total-reward=1000000000000 cumulated-stake=0 start-time=1000000000 end-time=1000003600
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=stake epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating liquidity reward" module=embedded contract=liquidity epoch=0 znn-amount=187200000000 qsr-amount=500000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=liquidity epoch=1
t=2001-09-09T02:46:50+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg TokenName:Zenon Coin TokenSymbol:ZNN TokenDomain:zenon.network TotalSupply:+19687200000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1znnxxxxxxxxxxxxx9z4ulx}" minted-amount=187200000000 to-address=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae
t=2001-09-09T02:46:50+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 TokenName:QuasarCoin TokenSymbol:QSR TokenDomain:zenon.network TotalSupply:+181050000000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1qsrxxxxxxxxxxxxxmrhjll}" minted-amount=500000000000 to-address=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae
t=2001-09-09T02:47:00+0000 lvl=info msg="received donation" module=embedded contract=common embedded=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae from-address=z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0 zts=zts1znnxxxxxxxxxxxxx9z4ulx amount=187200000000
t=2001-09-09T02:47:00+0000 lvl=info msg="received donation" module=embedded contract=common embedded=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae from-address=z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0 zts=zts1qsrxxxxxxxxxxxxxmrhjll amount=500000000000
t=2001-09-09T02:47:10+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg TokenName:Zenon Coin TokenSymbol:ZNN TokenDomain:zenon.network TotalSupply:+19874400000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1znnxxxxxxxxxxxxx9z4ulx}" minted-amount=187200000000 to-address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz
t=2001-09-09T02:47:10+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 TokenName:QuasarCoin TokenSymbol:QSR TokenDomain:zenon.network TotalSupply:+181550000000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1qsrxxxxxxxxxxxxxmrhjll}" minted-amount=500000000000 to-address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz
`)
	defer z.StopPanic()
	sentinelApi := embedded.NewSentinelApi(z)
	ledgerApi := api.NewLedgerApi(z)
	registerSentinel(z, t, g.User1.Address)
	z.InsertMomentumsTo(60*6 + 2)
	common.Json(sentinelApi.GetUncollectedReward(g.User1.Address)).Equals(t, `
{
	"address": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
	"znnAmount": "187200000000",
	"qsrAmount": "500000000000"
}`)
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.SentinelContract,
		Data:      definition.ABIStake.PackMethodPanic(definition.CollectRewardMethodName),
	}).Error(t, nil)
	z.InsertNewMomentum()
	z.InsertNewMomentum()
	z.InsertNewMomentum()
	common.Json(ledgerApi.GetUnreceivedBlocksByAddress(g.User1.Address, 0, 10)).HideHashes().Equals(t, `
{
	"list": [
		{
			"version": 1,
			"chainIdentifier": 100,
			"blockType": 4,
			"hash": "XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX",
			"previousHash": "XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX",
			"height": 8,
			"momentumAcknowledged": {
				"hash": "XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX",
				"height": 364
			},
			"address": "z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0",
			"toAddress": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
			"amount": "500000000000",
			"tokenStandard": "zts1qsrxxxxxxxxxxxxxmrhjll",
			"fromBlockHash": "XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX",
			"descendantBlocks": [],
			"data": "",
			"fusedPlasma": 0,
			"difficulty": 0,
			"nonce": "0000000000000000",
			"basePlasma": 0,
			"usedPlasma": 0,
			"changesHash": "XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX",
			"publicKey": null,
			"signature": null,
			"token": {
				"name": "QuasarCoin",
				"symbol": "QSR",
				"domain": "zenon.network",
				"totalSupply": "181550000000000",
				"decimals": 8,
				"owner": "z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62",
				"tokenStandard": "zts1qsrxxxxxxxxxxxxxmrhjll",
				"maxSupply": "4611686018427387903",
				"isBurnable": true,
				"isMintable": true,
				"isUtility": true
			},
			"confirmationDetail": {
				"numConfirmations": 1,
				"momentumHeight": 365,
				"momentumHash": "XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX",
				"momentumTimestamp": 1000003640
			},
			"pairedAccountBlock": null
		},
		{
			"version": 1,
			"chainIdentifier": 100,
			"blockType": 4,
			"hash": "XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX",
			"previousHash": "XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX",
			"height": 6,
			"momentumAcknowledged": {
				"hash": "XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX",
				"height": 364
			},
			"address": "z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0",
			"toAddress": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
			"amount": "187200000000",
			"tokenStandard": "zts1znnxxxxxxxxxxxxx9z4ulx",
			"fromBlockHash": "XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX",
			"descendantBlocks": [],
			"data": "",
			"fusedPlasma": 0,
			"difficulty": 0,
			"nonce": "0000000000000000",
			"basePlasma": 0,
			"usedPlasma": 0,
			"changesHash": "XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX",
			"publicKey": null,
			"signature": null,
			"token": {
				"name": "Zenon Coin",
				"symbol": "ZNN",
				"domain": "zenon.network",
				"totalSupply": "19874400000000",
				"decimals": 8,
				"owner": "z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg",
				"tokenStandard": "zts1znnxxxxxxxxxxxxx9z4ulx",
				"maxSupply": "4611686018427387903",
				"isBurnable": true,
				"isMintable": true,
				"isUtility": true
			},
			"confirmationDetail": {
				"numConfirmations": 1,
				"momentumHeight": 365,
				"momentumHash": "XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX",
				"momentumTimestamp": 1000003640
			},
			"pairedAccountBlock": null
		}
	],
	"count": 2,
	"more": false
}`)
	autoreceive(t, z, g.User1.Address)
	common.Json(sentinelApi.GetUncollectedReward(g.User1.Address)).Equals(t, `
{
	"address": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
	"znnAmount": "0",
	"qsrAmount": "0"
}`)
}

// - verify that User1 cannot collect rewards if he does not yet have a sentinel registered
// - check that User1 has no blocks to receive
func TestSentinel_TryToCollectBeforeRegister(t *testing.T) {
	z := mock.NewMockZenonWithCustomEpochDuration(t, time.Hour)
	defer z.StopPanic()
	ledgerApi := api.NewLedgerApi(z)

	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.SentinelContract,
		Data:      definition.ABIStake.PackMethodPanic(definition.CollectRewardMethodName),
	}).Error(t, constants.ErrNothingToWithdraw)
	z.InsertNewMomentum()
	z.InsertNewMomentum()
	z.InsertNewMomentum()
	common.Json(ledgerApi.GetUnreceivedBlocksByAddress(g.User1.Address, 0, 10)).HideHashes().Equals(t, `
{
	"list": [],
	"count": 0,
	"more": false
}`)
}

// - register sentinel for User1
// - insert momentums to height 60 * 6 + SentinelLockTimeWindow + 2
// - check uncollected reward for User1
// - revoke sentinel for User1
// - check uncollected reward for User1
// - collect reward for User1
// - receive the reward blocks for User1
// - check balance for User1
func TestSentinel_RegisterRevokeCollect(t *testing.T) {
	z := mock.NewMockZenonWithCustomEpochDuration(t, time.Hour)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).HideHashes().Equals(t, `
t=2001-09-09T01:47:00+0000 lvl=dbug msg="successfully register" module=embedded contract=sentinel sentinel="&{SentinelInfoKey:{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz} RegistrationTimestamp:1000000020 RevokeTimestamp:0 ZnnAmount:+500000000000 QsrAmount:+5000000000000}"
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=0 pillar-name=TEST-pillar-1 reward="&{DelegationReward:+11538239999 BlockReward:+9916666627 TotalReward:+21454906626 ProducedBlockNum:119 ExpectedBlockNum:120 Weight:+1683333333333}" total-weight=2083333333333 self-weight=1683333333333
t=2001-09-09T02:46:40+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=0 pillar-name=TEST-pillar-cool reward="&{DelegationReward:+1382400000 BlockReward:+9999999960 TotalReward:+11382399960 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2083333333333 self-weight=200000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=0 pillar-name=TEST-pillar-znn reward="&{DelegationReward:+1382400000 BlockReward:+9999999960 TotalReward:+11382399960 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2083333333333 self-weight=200000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=pillar epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxsentynelxxxxxxxxxxxxxwy0r2r current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating sentinel reward" module=embedded contract=sentinel epoch=0 total-znn-reward=187200000000 total-qsr-reward=500000000000 cumulated-sentinel=1 start-time=1000000000 end-time=1000003600
t=2001-09-09T02:46:40+0000 lvl=dbug msg="giving rewards" module=embedded contract=sentinel address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz epoch=0 znn-amount=187200000000 qsr-amount=500000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=sentinel epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating stake reward" module=embedded contract=stake epoch=0 total-reward=1000000000000 cumulated-stake=0 start-time=1000000000 end-time=1000003600
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=stake epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating liquidity reward" module=embedded contract=liquidity epoch=0 znn-amount=187200000000 qsr-amount=500000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=liquidity epoch=1
t=2001-09-09T02:46:50+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg TokenName:Zenon Coin TokenSymbol:ZNN TokenDomain:zenon.network TotalSupply:+19687200000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1znnxxxxxxxxxxxxx9z4ulx}" minted-amount=187200000000 to-address=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae
t=2001-09-09T02:46:50+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 TokenName:QuasarCoin TokenSymbol:QSR TokenDomain:zenon.network TotalSupply:+181050000000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1qsrxxxxxxxxxxxxxmrhjll}" minted-amount=500000000000 to-address=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae
t=2001-09-09T02:47:00+0000 lvl=info msg="received donation" module=embedded contract=common embedded=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae from-address=z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0 zts=zts1znnxxxxxxxxxxxxx9z4ulx amount=187200000000
t=2001-09-09T02:47:00+0000 lvl=info msg="received donation" module=embedded contract=common embedded=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae from-address=z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0 zts=zts1qsrxxxxxxxxxxxxxmrhjll amount=500000000000
t=2001-09-09T02:53:40+0000 lvl=dbug msg="successfully revoke" module=embedded contract=sentinel sentinel="&{SentinelInfoKey:{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz} RegistrationTimestamp:1000000020 RevokeTimestamp:1000004020 ZnnAmount:+0 QsrAmount:+0}"
t=2001-09-09T02:54:10+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg TokenName:Zenon Coin TokenSymbol:ZNN TokenDomain:zenon.network TotalSupply:+19874400000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1znnxxxxxxxxxxxxx9z4ulx}" minted-amount=187200000000 to-address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz
t=2001-09-09T02:54:10+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 TokenName:QuasarCoin TokenSymbol:QSR TokenDomain:zenon.network TotalSupply:+181550000000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1qsrxxxxxxxxxxxxxmrhjll}" minted-amount=500000000000 to-address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz
`)
	sentinelApi := embedded.NewSentinelApi(z)

	registerSentinel(z, t, g.User1.Address)
	z.InsertMomentumsTo(uint64(60*6 + constants.SentinelLockTimeWindow + 2))
	common.Json(sentinelApi.GetUncollectedReward(g.User1.Address)).HideHashes().Equals(t, `
{
	"address": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
	"znnAmount": "187200000000",
	"qsrAmount": "500000000000"
}`)
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.SentinelContract,
		Data:      definition.ABISentinel.PackMethodPanic(definition.RevokeSentinelMethodName),
	}).Error(t, nil)
	z.InsertNewMomentum()
	z.InsertNewMomentum()
	autoreceive(t, z, g.User1.Address)
	common.Json(sentinelApi.GetUncollectedReward(g.User1.Address)).HideHashes().Equals(t, `
{
	"address": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
	"znnAmount": "187200000000",
	"qsrAmount": "500000000000"
}`)
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.SentinelContract,
		Data:      definition.ABIStake.PackMethodPanic(definition.CollectRewardMethodName),
	}).Error(t, nil)
	z.InsertNewMomentum()
	z.InsertNewMomentum()
	z.InsertNewMomentum()
	autoreceive(t, z, g.User1.Address)
	common.Json(sentinelApi.GetUncollectedReward(g.User1.Address)).HideHashes().Equals(t, `
{
	"address": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
	"znnAmount": "0",
	"qsrAmount": "0"
}`)
	z.ExpectBalance(g.User1.Address, types.ZnnTokenStandard, 187200000000+12000*g.Zexp)
	z.ExpectBalance(g.User1.Address, types.QsrTokenStandard, 500000000000+120000*g.Zexp)
}

// - register sentinel for User1
// - try to collect reward
// - check that User1 has no blocks to receive
func TestSentinel_TryToCollectImmediatelyAfterRegister(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).HideHashes().Equals(t, `
t=2001-09-09T01:47:00+0000 lvl=dbug msg="successfully register" module=embedded contract=sentinel sentinel="&{SentinelInfoKey:{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz} RegistrationTimestamp:1000000020 RevokeTimestamp:0 ZnnAmount:+500000000000 QsrAmount:+5000000000000}"
`)
	ledgerApi := api.NewLedgerApi(z)

	registerSentinel(z, t, g.User1.Address)

	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.SentinelContract,
		Data:      definition.ABIStake.PackMethodPanic(definition.CollectRewardMethodName),
	}).Error(t, constants.ErrNothingToWithdraw)
	z.InsertNewMomentum()
	z.InsertNewMomentum()
	z.InsertNewMomentum()
	common.Json(ledgerApi.GetUnreceivedBlocksByAddress(g.User1.Address, 0, 10)).HideHashes().Equals(t, `
{
	"list": [],
	"count": 0,
	"more": false
}`)
}

// - register sentinel for User1
// - try to revoke sentinel for User1
// - check that there is still a sentinel registered for User1
func TestSentinel_TryToRevokeImmediatelyAfterRegister(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).HideHashes().Equals(t, `
t=2001-09-09T01:47:00+0000 lvl=dbug msg="successfully register" module=embedded contract=sentinel sentinel="&{SentinelInfoKey:{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz} RegistrationTimestamp:1000000020 RevokeTimestamp:0 ZnnAmount:+500000000000 QsrAmount:+5000000000000}"
t=2001-09-09T01:47:10+0000 lvl=dbug msg="invalid revoke - cannot be revoked yet" module=embedded contract=sentinel address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz until-revoke=30
`)
	sentinelApi := embedded.NewSentinelApi(z)

	registerSentinel(z, t, g.User1.Address)

	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.SentinelContract,
		Data:      definition.ABISentinel.PackMethodPanic(definition.RevokeSentinelMethodName),
	}).Error(t, constants.RevokeNotDue)
	z.InsertNewMomentum()
	z.InsertNewMomentum()
	common.Json(sentinelApi.GetByOwner(g.User1.Address)).Equals(t, `
{
	"owner": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
	"registrationTimestamp": 1000000020,
	"isRevocable": false,
	"revokeCooldown": 20,
	"active": true
}`)
}

// - register sentinel for User1
// - insert momentums to height 50
// - register sentinel for User2
// - insert momentums to height 60 * 6 + 50
// - call GetFrontierRewardByPage for User1 & User2
// - insert momentums to height 60 * 6 * 2 + 50
// - call GetFrontierRewardByPage for User1 & User2
func TestSentinel_GetFrontierRewardsByPage(t *testing.T) {
	z := mock.NewMockZenonWithCustomEpochDuration(t, time.Hour)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).HideHashes().Equals(t, `
t=2001-09-09T01:47:00+0000 lvl=dbug msg="successfully register" module=embedded contract=sentinel sentinel="&{SentinelInfoKey:{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz} RegistrationTimestamp:1000000020 RevokeTimestamp:0 ZnnAmount:+500000000000 QsrAmount:+5000000000000}"
t=2001-09-09T01:55:10+0000 lvl=dbug msg="successfully register" module=embedded contract=sentinel sentinel="&{SentinelInfoKey:{Owner:z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx} RegistrationTimestamp:1000000510 RevokeTimestamp:0 ZnnAmount:+500000000000 QsrAmount:+5000000000000}"
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=0 pillar-name=TEST-pillar-1 reward="&{DelegationReward:+10936390243 BlockReward:+9916666627 TotalReward:+20853056870 ProducedBlockNum:119 ExpectedBlockNum:120 Weight:+1308333333333}" total-weight=1708333333333 self-weight=1308333333333
t=2001-09-09T02:46:40+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=0 pillar-name=TEST-pillar-cool reward="&{DelegationReward:+1685853658 BlockReward:+9999999960 TotalReward:+11685853618 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=1708333333333 self-weight=200000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=0 pillar-name=TEST-pillar-znn reward="&{DelegationReward:+1685853658 BlockReward:+9999999960 TotalReward:+11685853618 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=1708333333333 self-weight=200000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=pillar epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxsentynelxxxxxxxxxxxxxwy0r2r current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating sentinel reward" module=embedded contract=sentinel epoch=0 total-znn-reward=187200000000 total-qsr-reward=500000000000 cumulated-sentinel=1 start-time=1000000000 end-time=1000003600
t=2001-09-09T02:46:40+0000 lvl=dbug msg="giving rewards" module=embedded contract=sentinel address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz epoch=0 znn-amount=187200000000 qsr-amount=500000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=sentinel epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating stake reward" module=embedded contract=stake epoch=0 total-reward=1000000000000 cumulated-stake=0 start-time=1000000000 end-time=1000003600
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=stake epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating liquidity reward" module=embedded contract=liquidity epoch=0 znn-amount=187200000000 qsr-amount=500000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=liquidity epoch=1
t=2001-09-09T02:46:50+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg TokenName:Zenon Coin TokenSymbol:ZNN TokenDomain:zenon.network TotalSupply:+19687200000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1znnxxxxxxxxxxxxx9z4ulx}" minted-amount=187200000000 to-address=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae
t=2001-09-09T02:46:50+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 TokenName:QuasarCoin TokenSymbol:QSR TokenDomain:zenon.network TotalSupply:+181050000000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1qsrxxxxxxxxxxxxxmrhjll}" minted-amount=500000000000 to-address=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae
t=2001-09-09T02:47:00+0000 lvl=info msg="received donation" module=embedded contract=common embedded=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae from-address=z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0 zts=zts1znnxxxxxxxxxxxxx9z4ulx amount=187200000000
t=2001-09-09T02:47:00+0000 lvl=info msg="received donation" module=embedded contract=common embedded=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae from-address=z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0 zts=zts1qsrxxxxxxxxxxxxxmrhjll amount=500000000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg current-height=722 last-update-height=361
t=2001-09-09T03:46:50+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=1 pillar-name=TEST-pillar-1 reward="&{DelegationReward:+10560000000 BlockReward:+9999999960 TotalReward:+20559999960 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+1100000000000}" total-weight=1500000000000 self-weight=1100000000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=1 pillar-name=TEST-pillar-cool reward="&{DelegationReward:+1920000000 BlockReward:+9999999960 TotalReward:+11919999960 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=1500000000000 self-weight=200000000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=1 pillar-name=TEST-pillar-znn reward="&{DelegationReward:+1920000000 BlockReward:+9999999960 TotalReward:+11919999960 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=1500000000000 self-weight=200000000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=pillar epoch=2
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxsentynelxxxxxxxxxxxxxwy0r2r current-height=722 last-update-height=361
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating sentinel reward" module=embedded contract=sentinel epoch=1 total-znn-reward=187200000000 total-qsr-reward=500000000000 cumulated-sentinel=2 start-time=1000003600 end-time=1000007200
t=2001-09-09T03:46:50+0000 lvl=dbug msg="giving rewards" module=embedded contract=sentinel address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz epoch=1 znn-amount=93600000000 qsr-amount=250000000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="giving rewards" module=embedded contract=sentinel address=z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx epoch=1 znn-amount=93600000000 qsr-amount=250000000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=sentinel epoch=2
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 current-height=722 last-update-height=361
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating stake reward" module=embedded contract=stake epoch=1 total-reward=1000000000000 cumulated-stake=0 start-time=1000003600 end-time=1000007200
t=2001-09-09T03:46:50+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=stake epoch=2
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae current-height=722 last-update-height=361
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating liquidity reward" module=embedded contract=liquidity epoch=1 znn-amount=187200000000 qsr-amount=500000000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=liquidity epoch=2
t=2001-09-09T03:47:00+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg TokenName:Zenon Coin TokenSymbol:ZNN TokenDomain:zenon.network TotalSupply:+19874400000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1znnxxxxxxxxxxxxx9z4ulx}" minted-amount=187200000000 to-address=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae
t=2001-09-09T03:47:00+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 TokenName:QuasarCoin TokenSymbol:QSR TokenDomain:zenon.network TotalSupply:+181550000000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1qsrxxxxxxxxxxxxxmrhjll}" minted-amount=500000000000 to-address=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae
t=2001-09-09T03:47:10+0000 lvl=info msg="received donation" module=embedded contract=common embedded=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae from-address=z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0 zts=zts1znnxxxxxxxxxxxxx9z4ulx amount=187200000000
t=2001-09-09T03:47:10+0000 lvl=info msg="received donation" module=embedded contract=common embedded=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae from-address=z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0 zts=zts1qsrxxxxxxxxxxxxxmrhjll amount=500000000000
`)
	sentinelApi := embedded.NewSentinelApi(z)

	registerSentinel(z, t, g.User1.Address)
	z.InsertMomentumsTo(50)
	registerSentinel(z, t, g.User2.Address)
	z.InsertMomentumsTo(60*6 + 50)
	common.Json(sentinelApi.GetFrontierRewardByPage(g.User1.Address, 0, 5)).Equals(t, `
{
	"count": 1,
	"list": [
		{
			"epoch": 0,
			"znnAmount": "187200000000",
			"qsrAmount": "500000000000"
		}
	]
}`)
	common.Json(sentinelApi.GetFrontierRewardByPage(g.User2.Address, 0, 5)).Equals(t, `
{
	"count": 1,
	"list": [
		{
			"epoch": 0,
			"znnAmount": "0",
			"qsrAmount": "0"
		}
	]
}`)
	z.InsertMomentumsTo(60*6*2 + 50)
	common.Json(sentinelApi.GetFrontierRewardByPage(g.User1.Address, 0, 5)).Equals(t, `
{
	"count": 2,
	"list": [
		{
			"epoch": 1,
			"znnAmount": "93600000000",
			"qsrAmount": "250000000000"
		},
		{
			"epoch": 0,
			"znnAmount": "187200000000",
			"qsrAmount": "500000000000"
		}
	]
}`)
	common.Json(sentinelApi.GetFrontierRewardByPage(g.User2.Address, 0, 5)).Equals(t, `
{
	"count": 2,
	"list": [
		{
			"epoch": 1,
			"znnAmount": "93600000000",
			"qsrAmount": "250000000000"
		},
		{
			"epoch": 0,
			"znnAmount": "0",
			"qsrAmount": "0"
		}
	]
}`)
}
