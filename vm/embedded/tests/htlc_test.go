package tests

import (
	"encoding/hex"
	"math/big"
	"testing"

	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/rpc/api/embedded"
	"github.com/zenon-network/go-zenon/vm/constants"
	"github.com/zenon-network/go-zenon/vm/embedded/definition"

	g "github.com/zenon-network/go-zenon/chain/genesis/mock"
	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/crypto"
	"github.com/zenon-network/go-zenon/zenon/mock"
)

const (
	genesisTimestamp = 1000000000
)

var (
	// iykyk
	preimageZ, _ = hex.DecodeString("b7845adcd41eec4e4fa1cc75a868014811b575942c6e4a72551bc01f63705634")
	preimageQ, _ = hex.DecodeString("d70b59367334f9c6d4771059093ec11cb505d7b2b0e233cc8bde00fe7aec3cee")

	preimageZlong = []byte("a718ee3fe739bd6435f0bd7bb4ee90e1deff2343372d92a04592c26a39b570a8")
)

func activateHtlc(z mock.MockZenon) {
	sporkAPI := embedded.NewSporkApi(z)
	z.InsertSendBlock(&nom.AccountBlock{
		Address:   g.Spork.Address,
		ToAddress: types.SporkContract,
		Data: definition.ABISpork.PackMethodPanic(definition.SporkCreateMethodName,
			"spork-htlc",              // name
			"activate spork for htlc", // description
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
	types.HtlcSpork.SporkId = id
	types.ImplementedSporksMap[id] = true
	z.InsertMomentumsTo(20)
}

func TestHtlc_zero(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).Equals(t, `
t=2001-09-09T01:46:50+0000 lvl=dbug msg=created module=embedded contract=spork spork="&{Id:664147f0c0a127bb4388bf8ff9a2ce777c9cc5ce9f04f9a6d418a32ef3f481c9 Name:spork-htlc Description:activate spork for htlc Activated:false EnforcementHeight:0}"
t=2001-09-09T01:47:00+0000 lvl=dbug msg=activated module=embedded contract=spork spork="&{Id:664147f0c0a127bb4388bf8ff9a2ce777c9cc5ce9f04f9a6d418a32ef3f481c9 Name:spork-htlc Description:activate spork for htlc Activated:true EnforcementHeight:9}"
t=2001-09-09T01:49:50+0000 lvl=dbug msg="invalid create - cannot create zero amount" module=embedded contract=htlc address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz
`)
	activateHtlc(z)

	lock := crypto.HashSHA256(preimageZ)

	// user 1 creates an htlc for user 2
	z.InsertSendBlock(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.HtlcContract,
		Data: definition.ABIHtlc.PackMethodPanic(definition.CreateHtlcMethodName,
			g.User2.Address,                // hashlocked
			int64(genesisTimestamp+300),    // expiration time
			uint8(definition.HashTypeSHA3), // hash type
			uint8(32),                      // max preimage size
			lock,                           // hashlock
		),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(0),
	}, constants.ErrInvalidTokenOrAmount, mock.NoVmChanges)
	z.InsertNewMomentum()
	z.InsertNewMomentum()

	z.ExpectBalance(g.User1.Address, types.ZnnTokenStandard, 12000*g.Zexp)
	z.ExpectBalance(g.User1.Address, types.QsrTokenStandard, 120000*g.Zexp)

	z.ExpectBalance(g.User2.Address, types.ZnnTokenStandard, 8000*g.Zexp)
	z.ExpectBalance(g.User2.Address, types.QsrTokenStandard, 80000*g.Zexp)

	z.ExpectBalance(types.HtlcContract, types.ZnnTokenStandard, 0*g.Zexp)
	z.ExpectBalance(types.HtlcContract, types.QsrTokenStandard, 0*g.Zexp)
}

func TestHtlc_unlock(t *testing.T) {
	z := mock.NewMockZenon(t)
	htlcApi := embedded.NewHtlcApi(z)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).Equals(t, `
t=2001-09-09T01:46:50+0000 lvl=dbug msg=created module=embedded contract=spork spork="&{Id:664147f0c0a127bb4388bf8ff9a2ce777c9cc5ce9f04f9a6d418a32ef3f481c9 Name:spork-htlc Description:activate spork for htlc Activated:false EnforcementHeight:0}"
t=2001-09-09T01:47:00+0000 lvl=dbug msg=activated module=embedded contract=spork spork="&{Id:664147f0c0a127bb4388bf8ff9a2ce777c9cc5ce9f04f9a6d418a32ef3f481c9 Name:spork-htlc Description:activate spork for htlc Activated:true EnforcementHeight:9}"
t=2001-09-09T01:50:00+0000 lvl=dbug msg=created module=embedded contract=htlc htlcInfo="Id:7efdcca315f86cdb04e84113bfc5f003fa49c4b3f9b287cd3b4a08d8ccdf6ffc TimeLocked:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz HashLocked:z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx TokenStandard:zts1znnxxxxxxxxxxxxx9z4ulx Amount:1000000000 ExpirationTime:1000000300 HashType:0 KeyMaxSize:32 HashLock:Fd4QDoNykDbHp30bYIghQmK4OOcnATsGilLcpt7kV8s="
t=2001-09-09T01:50:20+0000 lvl=dbug msg="invalid reclaim - entry not expired" module=embedded contract=htlc id=7efdcca315f86cdb04e84113bfc5f003fa49c4b3f9b287cd3b4a08d8ccdf6ffc address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz time=1000000220 expiration-time=1000000300
t=2001-09-09T01:50:30+0000 lvl=dbug msg="invalid unlock - wrong preimage" module=embedded contract=htlc id=7efdcca315f86cdb04e84113bfc5f003fa49c4b3f9b287cd3b4a08d8ccdf6ffc address=z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx preimage=d70b59367334f9c6d4771059093ec11cb505d7b2b0e233cc8bde00fe7aec3cee
t=2001-09-09T01:50:40+0000 lvl=dbug msg=unlocked module=embedded contract=htlc htlcInfo="Id:7efdcca315f86cdb04e84113bfc5f003fa49c4b3f9b287cd3b4a08d8ccdf6ffc TimeLocked:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz HashLocked:z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx TokenStandard:zts1znnxxxxxxxxxxxxx9z4ulx Amount:1000000000 ExpirationTime:1000000300 HashType:0 KeyMaxSize:32 HashLock:Fd4QDoNykDbHp30bYIghQmK4OOcnATsGilLcpt7kV8s=" preimage=b7845adcd41eec4e4fa1cc75a868014811b575942c6e4a72551bc01f63705634
`)
	activateHtlc(z)

	preimage := preimageZ
	lock := crypto.Hash(preimage)

	// user 1 creates an htlc for user 2
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.HtlcContract,
		Data: definition.ABIHtlc.PackMethodPanic(definition.CreateHtlcMethodName,
			g.User2.Address,                // hashlocked
			int64(genesisTimestamp+300),    // expiration time
			uint8(definition.HashTypeSHA3), // hash type
			uint8(32),                      // max preimage size
			lock,                           // hashlock
		),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(10 * g.Zexp),
	}).Error(t, nil)
	z.InsertNewMomentum()
	z.InsertNewMomentum()

	z.ExpectBalance(g.User1.Address, types.ZnnTokenStandard, 11990*g.Zexp)
	z.ExpectBalance(g.User1.Address, types.QsrTokenStandard, 120000*g.Zexp)

	z.ExpectBalance(g.User2.Address, types.ZnnTokenStandard, 8000*g.Zexp)
	z.ExpectBalance(g.User2.Address, types.QsrTokenStandard, 80000*g.Zexp)

	z.ExpectBalance(types.HtlcContract, types.ZnnTokenStandard, 10*g.Zexp)
	z.ExpectBalance(types.HtlcContract, types.QsrTokenStandard, 0*g.Zexp)

	htlcId := types.HexToHashPanic("7efdcca315f86cdb04e84113bfc5f003fa49c4b3f9b287cd3b4a08d8ccdf6ffc")

	common.Json(htlcApi.GetById(htlcId)).Equals(t, `
{
	"id": "7efdcca315f86cdb04e84113bfc5f003fa49c4b3f9b287cd3b4a08d8ccdf6ffc",
	"timeLocked": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
	"hashLocked": "z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx",
	"tokenStandard": "zts1znnxxxxxxxxxxxxx9z4ulx",
	"amount": "1000000000",
	"expirationTime": 1000000300,
	"hashType": 0,
	"keyMaxSize": 32,
	"hashLock": "Fd4QDoNykDbHp30bYIghQmK4OOcnATsGilLcpt7kV8s="
}
`)

	// user 1 tries to reclaim unexpired
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.HtlcContract,
		Data: definition.ABIHtlc.PackMethodPanic(definition.ReclaimHtlcMethodName,
			htlcId, // entry id
		),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(0 * g.Zexp),
	}).Error(t, constants.ReclaimNotDue)
	z.InsertNewMomentum()

	// user 2 tries to unlock with wrong preimage
	wrong_preimage := preimageQ
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User2.Address,
		ToAddress: types.HtlcContract,
		Data: definition.ABIHtlc.PackMethodPanic(definition.UnlockHtlcMethodName,
			htlcId,         // entry id
			wrong_preimage, // preimage
		),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(0 * g.Zexp),
	}).Error(t, constants.ErrInvalidPreimage)
	z.InsertNewMomentum()

	// user2 unlocks with correct preimage
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User2.Address,
		ToAddress: types.HtlcContract,
		Data: definition.ABIHtlc.PackMethodPanic(definition.UnlockHtlcMethodName,
			htlcId,   // entry id
			preimage, // preimage
		),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(0 * g.Zexp),
	}).Error(t, nil)
	z.InsertNewMomentum()
	z.InsertNewMomentum()

	autoreceive(t, z, g.User2.Address)
	z.InsertNewMomentum()
	z.InsertNewMomentum()

	z.ExpectBalance(g.User1.Address, types.ZnnTokenStandard, 11990*g.Zexp)
	z.ExpectBalance(g.User1.Address, types.QsrTokenStandard, 120000*g.Zexp)

	z.ExpectBalance(g.User2.Address, types.ZnnTokenStandard, 8010*g.Zexp)
	z.ExpectBalance(g.User2.Address, types.QsrTokenStandard, 80000*g.Zexp)

	z.ExpectBalance(types.HtlcContract, types.ZnnTokenStandard, 0*g.Zexp)
	z.ExpectBalance(types.HtlcContract, types.QsrTokenStandard, 0*g.Zexp)

}

func TestHtlc_reclaim(t *testing.T) {
	z := mock.NewMockZenon(t)
	htlcApi := embedded.NewHtlcApi(z)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).Equals(t, `
t=2001-09-09T01:46:50+0000 lvl=dbug msg=created module=embedded contract=spork spork="&{Id:664147f0c0a127bb4388bf8ff9a2ce777c9cc5ce9f04f9a6d418a32ef3f481c9 Name:spork-htlc Description:activate spork for htlc Activated:false EnforcementHeight:0}"
t=2001-09-09T01:47:00+0000 lvl=dbug msg=activated module=embedded contract=spork spork="&{Id:664147f0c0a127bb4388bf8ff9a2ce777c9cc5ce9f04f9a6d418a32ef3f481c9 Name:spork-htlc Description:activate spork for htlc Activated:true EnforcementHeight:9}"
t=2001-09-09T01:50:00+0000 lvl=dbug msg=created module=embedded contract=htlc htlcInfo="Id:c1b78112f757c24d290374c7992c8c90c980237e95d04ab010531c55ca6496c9 TimeLocked:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz HashLocked:z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx TokenStandard:zts1qsrxxxxxxxxxxxxxmrhjll Amount:1000000000 ExpirationTime:1000000300 HashType:0 KeyMaxSize:32 HashLock:Fd4QDoNykDbHp30bYIghQmK4OOcnATsGilLcpt7kV8s="
t=2001-09-09T01:53:20+0000 lvl=dbug msg="invalid unlock - entry is expired" module=embedded contract=htlc id=c1b78112f757c24d290374c7992c8c90c980237e95d04ab010531c55ca6496c9 address=z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx time=1000000400 expiration-time=1000000300
t=2001-09-09T01:53:30+0000 lvl=dbug msg=reclaimed module=embedded contract=htlc htlcInfo="Id:c1b78112f757c24d290374c7992c8c90c980237e95d04ab010531c55ca6496c9 TimeLocked:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz HashLocked:z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx TokenStandard:zts1qsrxxxxxxxxxxxxxmrhjll Amount:1000000000 ExpirationTime:1000000300 HashType:0 KeyMaxSize:32 HashLock:Fd4QDoNykDbHp30bYIghQmK4OOcnATsGilLcpt7kV8s="
`)
	activateHtlc(z)

	preimage := preimageZ
	lock := crypto.Hash(preimage)

	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.HtlcContract,
		Data: definition.ABIHtlc.PackMethodPanic(definition.CreateHtlcMethodName,
			g.User2.Address,                // hashlocked
			int64(genesisTimestamp+300),    // expiration time
			uint8(definition.HashTypeSHA3), // hash type
			uint8(32),                      // max preimage size
			lock,                           // hashlock
		),
		TokenStandard: types.QsrTokenStandard,
		Amount:        big.NewInt(10 * g.Zexp),
	}).Error(t, nil)
	z.InsertNewMomentum()
	z.InsertMomentumsTo(40)

	z.ExpectBalance(g.User1.Address, types.ZnnTokenStandard, 12000*g.Zexp)
	z.ExpectBalance(g.User1.Address, types.QsrTokenStandard, 119990*g.Zexp)

	z.ExpectBalance(g.User2.Address, types.ZnnTokenStandard, 8000*g.Zexp)
	z.ExpectBalance(g.User2.Address, types.QsrTokenStandard, 80000*g.Zexp)

	z.ExpectBalance(types.HtlcContract, types.ZnnTokenStandard, 0*g.Zexp)
	z.ExpectBalance(types.HtlcContract, types.QsrTokenStandard, 10*g.Zexp)

	htlcId := types.HexToHashPanic("c1b78112f757c24d290374c7992c8c90c980237e95d04ab010531c55ca6496c9")

	common.Json(htlcApi.GetById(htlcId)).Equals(t, `
{
	"id": "c1b78112f757c24d290374c7992c8c90c980237e95d04ab010531c55ca6496c9",
	"timeLocked": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
	"hashLocked": "z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx",
	"tokenStandard": "zts1qsrxxxxxxxxxxxxxmrhjll",
	"amount": "1000000000",
	"expirationTime": 1000000300,
	"hashType": 0,
	"keyMaxSize": 32,
	"hashLock": "Fd4QDoNykDbHp30bYIghQmK4OOcnATsGilLcpt7kV8s="
}
`)

	// user2 tries to unlock expired with correct preimage
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User2.Address,
		ToAddress: types.HtlcContract,
		Data: definition.ABIHtlc.PackMethodPanic(definition.UnlockHtlcMethodName,
			htlcId,   // entry id
			preimage, // preimage
		),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(0 * g.Zexp),
	}).Error(t, constants.ErrExpired)
	z.InsertNewMomentum()

	// user 1 reclaims expired
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.HtlcContract,
		Data: definition.ABIHtlc.PackMethodPanic(definition.ReclaimHtlcMethodName,
			htlcId, // entry id
		),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(0 * g.Zexp),
	}).Error(t, nil)
	z.InsertNewMomentum()
	z.InsertNewMomentum()

	autoreceive(t, z, g.User1.Address)
	z.InsertNewMomentum()
	z.InsertNewMomentum()

	z.ExpectBalance(g.User1.Address, types.ZnnTokenStandard, 12000*g.Zexp)
	z.ExpectBalance(g.User1.Address, types.QsrTokenStandard, 120000*g.Zexp)

	z.ExpectBalance(g.User2.Address, types.ZnnTokenStandard, 8000*g.Zexp)
	z.ExpectBalance(g.User2.Address, types.QsrTokenStandard, 80000*g.Zexp)

	z.ExpectBalance(types.HtlcContract, types.ZnnTokenStandard, 0*g.Zexp)
	z.ExpectBalance(types.HtlcContract, types.QsrTokenStandard, 0*g.Zexp)

}

func TestHtlc_create_expiration_time(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).Equals(t, `
t=2001-09-09T01:46:50+0000 lvl=dbug msg=created module=embedded contract=spork spork="&{Id:664147f0c0a127bb4388bf8ff9a2ce777c9cc5ce9f04f9a6d418a32ef3f481c9 Name:spork-htlc Description:activate spork for htlc Activated:false EnforcementHeight:0}"
t=2001-09-09T01:47:00+0000 lvl=dbug msg=activated module=embedded contract=spork spork="&{Id:664147f0c0a127bb4388bf8ff9a2ce777c9cc5ce9f04f9a6d418a32ef3f481c9 Name:spork-htlc Description:activate spork for htlc Activated:true EnforcementHeight:9}"
t=2001-09-09T01:51:40+0000 lvl=dbug msg="invalid create - cannot create already expired" module=embedded contract=htlc address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz time=1000000300 expiration-time=1000000300
`)
	activateHtlc(z)

	preimage := preimageZ
	lock := crypto.Hash(preimage)

	z.InsertMomentumsTo(30)
	// Sun Sep 09 2001 01:51:40 GMT+0000
	// check the time in the logs

	// user 1 creates an htlc for user 2
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.HtlcContract,
		Data: definition.ABIHtlc.PackMethodPanic(definition.CreateHtlcMethodName,
			g.User2.Address,                // hashlocked
			int64(genesisTimestamp+300),    // expiration time
			uint8(definition.HashTypeSHA3), // hash type
			uint8(32),                      // max preimage size
			lock,                           // hashlock
		),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(10 * g.Zexp),
	}).Error(t, constants.ErrInvalidExpirationTime)
	z.InsertNewMomentum()
	z.InsertNewMomentum()
}

func TestHtlc_unlock_expiration_time(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).Equals(t, `
t=2001-09-09T01:46:50+0000 lvl=dbug msg=created module=embedded contract=spork spork="&{Id:664147f0c0a127bb4388bf8ff9a2ce777c9cc5ce9f04f9a6d418a32ef3f481c9 Name:spork-htlc Description:activate spork for htlc Activated:false EnforcementHeight:0}"
t=2001-09-09T01:47:00+0000 lvl=dbug msg=activated module=embedded contract=spork spork="&{Id:664147f0c0a127bb4388bf8ff9a2ce777c9cc5ce9f04f9a6d418a32ef3f481c9 Name:spork-htlc Description:activate spork for htlc Activated:true EnforcementHeight:9}"
t=2001-09-09T01:50:00+0000 lvl=dbug msg=created module=embedded contract=htlc htlcInfo="Id:7efdcca315f86cdb04e84113bfc5f003fa49c4b3f9b287cd3b4a08d8ccdf6ffc TimeLocked:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz HashLocked:z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx TokenStandard:zts1znnxxxxxxxxxxxxx9z4ulx Amount:1000000000 ExpirationTime:1000000300 HashType:0 KeyMaxSize:32 HashLock:Fd4QDoNykDbHp30bYIghQmK4OOcnATsGilLcpt7kV8s="
t=2001-09-09T01:51:40+0000 lvl=dbug msg="invalid unlock - entry is expired" module=embedded contract=htlc id=7efdcca315f86cdb04e84113bfc5f003fa49c4b3f9b287cd3b4a08d8ccdf6ffc address=z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx time=1000000300 expiration-time=1000000300
`)
	activateHtlc(z)

	preimage := preimageZ
	lock := crypto.Hash(preimage)

	// user 1 creates an htlc for user 2
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.HtlcContract,
		Data: definition.ABIHtlc.PackMethodPanic(definition.CreateHtlcMethodName,
			g.User2.Address,                // hashlocked
			int64(genesisTimestamp+300),    // expiration time
			uint8(definition.HashTypeSHA3), // hash type
			uint8(32),                      // max preimage size
			lock,                           // hashlock
		),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(10 * g.Zexp),
	}).Error(t, nil)
	z.InsertNewMomentum()
	z.InsertNewMomentum()

	htlcId := types.HexToHashPanic("7efdcca315f86cdb04e84113bfc5f003fa49c4b3f9b287cd3b4a08d8ccdf6ffc")

	z.InsertMomentumsTo(30)
	// Sun Sep 09 2001 01:51:40 GMT+0000
	// check the time in the logs

	// user2 tries to unlock expired with correct preimage
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User2.Address,
		ToAddress: types.HtlcContract,
		Data: definition.ABIHtlc.PackMethodPanic(definition.UnlockHtlcMethodName,
			htlcId,   // entry id
			preimage, // preimage
		),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(0 * g.Zexp),
	}).Error(t, constants.ErrExpired)
	z.InsertNewMomentum()
	z.InsertNewMomentum()

}

func TestHtlc_reclaim_expiration_time(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).Equals(t, `
t=2001-09-09T01:46:50+0000 lvl=dbug msg=created module=embedded contract=spork spork="&{Id:664147f0c0a127bb4388bf8ff9a2ce777c9cc5ce9f04f9a6d418a32ef3f481c9 Name:spork-htlc Description:activate spork for htlc Activated:false EnforcementHeight:0}"
t=2001-09-09T01:47:00+0000 lvl=dbug msg=activated module=embedded contract=spork spork="&{Id:664147f0c0a127bb4388bf8ff9a2ce777c9cc5ce9f04f9a6d418a32ef3f481c9 Name:spork-htlc Description:activate spork for htlc Activated:true EnforcementHeight:9}"
t=2001-09-09T01:50:00+0000 lvl=dbug msg=created module=embedded contract=htlc htlcInfo="Id:7efdcca315f86cdb04e84113bfc5f003fa49c4b3f9b287cd3b4a08d8ccdf6ffc TimeLocked:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz HashLocked:z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx TokenStandard:zts1znnxxxxxxxxxxxxx9z4ulx Amount:1000000000 ExpirationTime:1000000300 HashType:0 KeyMaxSize:32 HashLock:Fd4QDoNykDbHp30bYIghQmK4OOcnATsGilLcpt7kV8s="
t=2001-09-09T01:51:40+0000 lvl=dbug msg=reclaimed module=embedded contract=htlc htlcInfo="Id:7efdcca315f86cdb04e84113bfc5f003fa49c4b3f9b287cd3b4a08d8ccdf6ffc TimeLocked:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz HashLocked:z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx TokenStandard:zts1znnxxxxxxxxxxxxx9z4ulx Amount:1000000000 ExpirationTime:1000000300 HashType:0 KeyMaxSize:32 HashLock:Fd4QDoNykDbHp30bYIghQmK4OOcnATsGilLcpt7kV8s="
`)
	activateHtlc(z)

	preimage := preimageZ
	lock := crypto.Hash(preimage)

	// user 1 creates an htlc for user 2
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.HtlcContract,
		Data: definition.ABIHtlc.PackMethodPanic(definition.CreateHtlcMethodName,
			g.User2.Address,                // hashlocked
			int64(genesisTimestamp+300),    // expiration time
			uint8(definition.HashTypeSHA3), // hash type
			uint8(32),                      // max preimage size
			lock,                           // hashlock
		),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(10 * g.Zexp),
	}).Error(t, nil)
	z.InsertNewMomentum()
	z.InsertNewMomentum()

	htlcId := types.HexToHashPanic("7efdcca315f86cdb04e84113bfc5f003fa49c4b3f9b287cd3b4a08d8ccdf6ffc")

	z.InsertMomentumsTo(30)
	// Sun Sep 09 2001 01:51:40 GMT+0000
	// check the time in the logs

	// user 1 reclaims expired
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.HtlcContract,
		Data: definition.ABIHtlc.PackMethodPanic(definition.ReclaimHtlcMethodName,
			htlcId, // entry id
		),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(0 * g.Zexp),
	}).Error(t, nil)
	z.InsertNewMomentum()
	z.InsertNewMomentum()
}

func TestHtlc_proxy_unlock_default(t *testing.T) {
	z := mock.NewMockZenon(t)
	htlcApi := embedded.NewHtlcApi(z)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).Equals(t, `
t=2001-09-09T01:46:50+0000 lvl=dbug msg=created module=embedded contract=spork spork="&{Id:664147f0c0a127bb4388bf8ff9a2ce777c9cc5ce9f04f9a6d418a32ef3f481c9 Name:spork-htlc Description:activate spork for htlc Activated:false EnforcementHeight:0}"
t=2001-09-09T01:47:00+0000 lvl=dbug msg=activated module=embedded contract=spork spork="&{Id:664147f0c0a127bb4388bf8ff9a2ce777c9cc5ce9f04f9a6d418a32ef3f481c9 Name:spork-htlc Description:activate spork for htlc Activated:true EnforcementHeight:9}"
t=2001-09-09T01:50:00+0000 lvl=dbug msg=created module=embedded contract=htlc htlcInfo="Id:7efdcca315f86cdb04e84113bfc5f003fa49c4b3f9b287cd3b4a08d8ccdf6ffc TimeLocked:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz HashLocked:z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx TokenStandard:zts1znnxxxxxxxxxxxxx9z4ulx Amount:1000000000 ExpirationTime:1000000300 HashType:0 KeyMaxSize:32 HashLock:Fd4QDoNykDbHp30bYIghQmK4OOcnATsGilLcpt7kV8s="
t=2001-09-09T01:50:20+0000 lvl=dbug msg="invalid unlock - wrong preimage" module=embedded contract=htlc id=7efdcca315f86cdb04e84113bfc5f003fa49c4b3f9b287cd3b4a08d8ccdf6ffc address=z1qrs2lpccnsneglhnnfwvlsj0qncnxjnwlfmjac preimage=d70b59367334f9c6d4771059093ec11cb505d7b2b0e233cc8bde00fe7aec3cee
t=2001-09-09T01:50:40+0000 lvl=dbug msg=unlocked module=embedded contract=htlc htlcInfo="Id:7efdcca315f86cdb04e84113bfc5f003fa49c4b3f9b287cd3b4a08d8ccdf6ffc TimeLocked:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz HashLocked:z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx TokenStandard:zts1znnxxxxxxxxxxxxx9z4ulx Amount:1000000000 ExpirationTime:1000000300 HashType:0 KeyMaxSize:32 HashLock:Fd4QDoNykDbHp30bYIghQmK4OOcnATsGilLcpt7kV8s=" preimage=b7845adcd41eec4e4fa1cc75a868014811b575942c6e4a72551bc01f63705634
`)
	activateHtlc(z)

	preimage := preimageZ
	lock := crypto.Hash(preimage)

	// Check the default proxy unlock status
	common.Json(htlcApi.GetProxyUnlockStatus(g.User2.Address)).Equals(t, `true`)

	// user 1 creates an htlc for user 2
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.HtlcContract,
		Data: definition.ABIHtlc.PackMethodPanic(definition.CreateHtlcMethodName,
			g.User2.Address,                // hashlocked
			int64(genesisTimestamp+300),    // expiration time
			uint8(definition.HashTypeSHA3), // hash type
			uint8(32),                      // max preimage size
			lock,                           // hashlock
		),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(10 * g.Zexp),
	}).Error(t, nil)
	z.InsertNewMomentum()
	z.InsertNewMomentum()

	htlcId := types.HexToHashPanic("7efdcca315f86cdb04e84113bfc5f003fa49c4b3f9b287cd3b4a08d8ccdf6ffc")

	// user 3 tries to unlock with wrong preimage
	wrong_preimage := preimageQ
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User3.Address,
		ToAddress: types.HtlcContract,
		Data: definition.ABIHtlc.PackMethodPanic(definition.UnlockHtlcMethodName,
			htlcId,         // entry id
			wrong_preimage, // preimage
		),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(0 * g.Zexp),
	}).Error(t, constants.ErrInvalidPreimage)
	z.InsertNewMomentum()
	z.InsertNewMomentum()

	// user 3 unlocks with correct preimage
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User3.Address,
		ToAddress: types.HtlcContract,
		Data: definition.ABIHtlc.PackMethodPanic(definition.UnlockHtlcMethodName,
			htlcId,   // entry id
			preimage, // preimage
		),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(0 * g.Zexp),
	}).Error(t, nil)
	z.InsertNewMomentum()
	z.InsertNewMomentum()

	common.Json(htlcApi.GetById(htlcId)).Error(t, constants.ErrDataNonExistent)

	autoreceive(t, z, g.User2.Address)
	z.InsertNewMomentum()
	z.InsertNewMomentum()

	z.ExpectBalance(g.User1.Address, types.ZnnTokenStandard, (12000-10)*g.Zexp)
	z.ExpectBalance(g.User1.Address, types.QsrTokenStandard, 120000*g.Zexp)

	z.ExpectBalance(g.User2.Address, types.ZnnTokenStandard, (8000+10)*g.Zexp)
	z.ExpectBalance(g.User2.Address, types.QsrTokenStandard, 80000*g.Zexp)

	z.ExpectBalance(g.User3.Address, types.ZnnTokenStandard, 1000*g.Zexp)
	z.ExpectBalance(g.User3.Address, types.QsrTokenStandard, 10000*g.Zexp)

	z.ExpectBalance(types.HtlcContract, types.ZnnTokenStandard, 0*g.Zexp)
	z.ExpectBalance(types.HtlcContract, types.QsrTokenStandard, 0*g.Zexp)
}

func TestHtlc_deny_proxy_unlock(t *testing.T) {
	z := mock.NewMockZenon(t)
	htlcApi := embedded.NewHtlcApi(z)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).Equals(t, `
t=2001-09-09T01:46:50+0000 lvl=dbug msg=created module=embedded contract=spork spork="&{Id:664147f0c0a127bb4388bf8ff9a2ce777c9cc5ce9f04f9a6d418a32ef3f481c9 Name:spork-htlc Description:activate spork for htlc Activated:false EnforcementHeight:0}"
t=2001-09-09T01:47:00+0000 lvl=dbug msg=activated module=embedded contract=spork spork="&{Id:664147f0c0a127bb4388bf8ff9a2ce777c9cc5ce9f04f9a6d418a32ef3f481c9 Name:spork-htlc Description:activate spork for htlc Activated:true EnforcementHeight:9}"
t=2001-09-09T01:50:00+0000 lvl=dbug msg=created module=embedded contract=htlc htlcInfo="Id:7efdcca315f86cdb04e84113bfc5f003fa49c4b3f9b287cd3b4a08d8ccdf6ffc TimeLocked:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz HashLocked:z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx TokenStandard:zts1znnxxxxxxxxxxxxx9z4ulx Amount:1000000000 ExpirationTime:1000000300 HashType:0 KeyMaxSize:32 HashLock:Fd4QDoNykDbHp30bYIghQmK4OOcnATsGilLcpt7kV8s="
t=2001-09-09T01:50:20+0000 lvl=dbug msg="deny proxy unlock" module=embedded contract=htlc address=z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx
t=2001-09-09T01:50:30+0000 lvl=dbug msg="invalid unlock - permission denied" module=embedded contract=htlc id=7efdcca315f86cdb04e84113bfc5f003fa49c4b3f9b287cd3b4a08d8ccdf6ffc address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz
t=2001-09-09T01:50:40+0000 lvl=dbug msg="invalid unlock - permission denied" module=embedded contract=htlc id=7efdcca315f86cdb04e84113bfc5f003fa49c4b3f9b287cd3b4a08d8ccdf6ffc address=z1qrs2lpccnsneglhnnfwvlsj0qncnxjnwlfmjac
t=2001-09-09T01:50:50+0000 lvl=dbug msg=unlocked module=embedded contract=htlc htlcInfo="Id:7efdcca315f86cdb04e84113bfc5f003fa49c4b3f9b287cd3b4a08d8ccdf6ffc TimeLocked:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz HashLocked:z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx TokenStandard:zts1znnxxxxxxxxxxxxx9z4ulx Amount:1000000000 ExpirationTime:1000000300 HashType:0 KeyMaxSize:32 HashLock:Fd4QDoNykDbHp30bYIghQmK4OOcnATsGilLcpt7kV8s=" preimage=b7845adcd41eec4e4fa1cc75a868014811b575942c6e4a72551bc01f63705634
`)
	activateHtlc(z)

	preimage := preimageZ
	lock := crypto.Hash(preimage)

	// user 1 creates an htlc for user 2
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.HtlcContract,
		Data: definition.ABIHtlc.PackMethodPanic(definition.CreateHtlcMethodName,
			g.User2.Address,                // hashlocked
			int64(genesisTimestamp+300),    // expiration time
			uint8(definition.HashTypeSHA3), // hash type
			uint8(32),                      // max preimage size
			lock,                           // hashlock
		),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(10 * g.Zexp),
	}).Error(t, nil)
	z.InsertNewMomentum()
	z.InsertNewMomentum()

	htlcId := types.HexToHashPanic("7efdcca315f86cdb04e84113bfc5f003fa49c4b3f9b287cd3b4a08d8ccdf6ffc")

	// user 2 disables proxy unlock
	defer z.CallContract(&nom.AccountBlock{
		Address:       g.User2.Address,
		ToAddress:     types.HtlcContract,
		Data:          definition.ABIHtlc.PackMethodPanic(definition.DenyHtlcProxyUnlockMethodName),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(0 * g.Zexp),
	}).Error(t, nil)
	z.InsertNewMomentum()

	common.Json(htlcApi.GetProxyUnlockStatus(g.User2.Address)).Equals(t, `false`)

	// user 1 tries to unlock with correct preimage
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.HtlcContract,
		Data: definition.ABIHtlc.PackMethodPanic(definition.UnlockHtlcMethodName,
			htlcId,   // entry id
			preimage, // preimage
		),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(0 * g.Zexp),
	}).Error(t, constants.ErrPermissionDenied)
	z.InsertNewMomentum()

	// user 3 tries to unlock with correct preimage
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User3.Address,
		ToAddress: types.HtlcContract,
		Data: definition.ABIHtlc.PackMethodPanic(definition.UnlockHtlcMethodName,
			htlcId,   // entry id
			preimage, // preimage
		),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(0 * g.Zexp),
	}).Error(t, constants.ErrPermissionDenied)
	z.InsertNewMomentum()

	// user 2 unlocks with correct preimage
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User2.Address,
		ToAddress: types.HtlcContract,
		Data: definition.ABIHtlc.PackMethodPanic(definition.UnlockHtlcMethodName,
			htlcId,   // entry id
			preimage, // preimage
		),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(0 * g.Zexp),
	}).Error(t, nil)
	z.InsertNewMomentum()
	z.InsertNewMomentum()

	common.Json(htlcApi.GetById(htlcId)).Error(t, constants.ErrDataNonExistent)

	autoreceive(t, z, g.User2.Address)
	z.InsertNewMomentum()
	z.InsertNewMomentum()

	z.ExpectBalance(g.User1.Address, types.ZnnTokenStandard, (12000-10)*g.Zexp)
	z.ExpectBalance(g.User1.Address, types.QsrTokenStandard, 120000*g.Zexp)

	z.ExpectBalance(g.User2.Address, types.ZnnTokenStandard, (8000+10)*g.Zexp)
	z.ExpectBalance(g.User2.Address, types.QsrTokenStandard, 80000*g.Zexp)

	z.ExpectBalance(types.HtlcContract, types.ZnnTokenStandard, 0*g.Zexp)
	z.ExpectBalance(types.HtlcContract, types.QsrTokenStandard, 0*g.Zexp)
}

func TestHtlc_allow_proxy_unlock(t *testing.T) {
	z := mock.NewMockZenon(t)
	htlcApi := embedded.NewHtlcApi(z)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).Equals(t, `
t=2001-09-09T01:46:50+0000 lvl=dbug msg=created module=embedded contract=spork spork="&{Id:664147f0c0a127bb4388bf8ff9a2ce777c9cc5ce9f04f9a6d418a32ef3f481c9 Name:spork-htlc Description:activate spork for htlc Activated:false EnforcementHeight:0}"
t=2001-09-09T01:47:00+0000 lvl=dbug msg=activated module=embedded contract=spork spork="&{Id:664147f0c0a127bb4388bf8ff9a2ce777c9cc5ce9f04f9a6d418a32ef3f481c9 Name:spork-htlc Description:activate spork for htlc Activated:true EnforcementHeight:9}"
t=2001-09-09T01:50:00+0000 lvl=dbug msg=created module=embedded contract=htlc htlcInfo="Id:7efdcca315f86cdb04e84113bfc5f003fa49c4b3f9b287cd3b4a08d8ccdf6ffc TimeLocked:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz HashLocked:z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx TokenStandard:zts1znnxxxxxxxxxxxxx9z4ulx Amount:1000000000 ExpirationTime:1000000300 HashType:0 KeyMaxSize:32 HashLock:Fd4QDoNykDbHp30bYIghQmK4OOcnATsGilLcpt7kV8s="
t=2001-09-09T01:50:20+0000 lvl=dbug msg=created module=embedded contract=htlc htlcInfo="Id:ffb6f2e5328e535a8e9a3e85682ab107dd60ec4e3180cf48c03c0a3fbef9b635 TimeLocked:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz HashLocked:z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx TokenStandard:zts1qsrxxxxxxxxxxxxxmrhjll Amount:1000000000 ExpirationTime:1000000300 HashType:0 KeyMaxSize:32 HashLock:MbH5meCadfnDy+VGghfqoWtLjsq+87jsgPg92BtG//0="
t=2001-09-09T01:50:40+0000 lvl=dbug msg="deny proxy unlock" module=embedded contract=htlc address=z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx
t=2001-09-09T01:50:50+0000 lvl=dbug msg="allow proxy unlock" module=embedded contract=htlc address=z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx
t=2001-09-09T01:51:00+0000 lvl=dbug msg="invalid unlock - wrong preimage" module=embedded contract=htlc id=7efdcca315f86cdb04e84113bfc5f003fa49c4b3f9b287cd3b4a08d8ccdf6ffc address=z1qrs2lpccnsneglhnnfwvlsj0qncnxjnwlfmjac preimage=d70b59367334f9c6d4771059093ec11cb505d7b2b0e233cc8bde00fe7aec3cee
t=2001-09-09T01:51:10+0000 lvl=dbug msg=unlocked module=embedded contract=htlc htlcInfo="Id:7efdcca315f86cdb04e84113bfc5f003fa49c4b3f9b287cd3b4a08d8ccdf6ffc TimeLocked:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz HashLocked:z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx TokenStandard:zts1znnxxxxxxxxxxxxx9z4ulx Amount:1000000000 ExpirationTime:1000000300 HashType:0 KeyMaxSize:32 HashLock:Fd4QDoNykDbHp30bYIghQmK4OOcnATsGilLcpt7kV8s=" preimage=b7845adcd41eec4e4fa1cc75a868014811b575942c6e4a72551bc01f63705634
t=2001-09-09T01:51:20+0000 lvl=dbug msg="invalid unlock - wrong preimage" module=embedded contract=htlc id=ffb6f2e5328e535a8e9a3e85682ab107dd60ec4e3180cf48c03c0a3fbef9b635 address=z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx preimage=b7845adcd41eec4e4fa1cc75a868014811b575942c6e4a72551bc01f63705634
t=2001-09-09T01:51:30+0000 lvl=dbug msg=unlocked module=embedded contract=htlc htlcInfo="Id:ffb6f2e5328e535a8e9a3e85682ab107dd60ec4e3180cf48c03c0a3fbef9b635 TimeLocked:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz HashLocked:z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx TokenStandard:zts1qsrxxxxxxxxxxxxxmrhjll Amount:1000000000 ExpirationTime:1000000300 HashType:0 KeyMaxSize:32 HashLock:MbH5meCadfnDy+VGghfqoWtLjsq+87jsgPg92BtG//0=" preimage=d70b59367334f9c6d4771059093ec11cb505d7b2b0e233cc8bde00fe7aec3cee
`)
	activateHtlc(z)

	preimage := preimageZ
	lock := crypto.Hash(preimage)
	lockQ := crypto.Hash(preimageQ)

	// user 1 creates an htlc for user 2
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.HtlcContract,
		Data: definition.ABIHtlc.PackMethodPanic(definition.CreateHtlcMethodName,
			g.User2.Address,                // hashlocked
			int64(genesisTimestamp+300),    // expiration time
			uint8(definition.HashTypeSHA3), // hash type
			uint8(32),                      // max preimage size
			lock,                           // hashlock
		),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(10 * g.Zexp),
	}).Error(t, nil)
	z.InsertNewMomentum()
	z.InsertNewMomentum()

	htlcId := types.HexToHashPanic("7efdcca315f86cdb04e84113bfc5f003fa49c4b3f9b287cd3b4a08d8ccdf6ffc")

	// user 1 creates another htlc for user 2
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.HtlcContract,
		Data: definition.ABIHtlc.PackMethodPanic(definition.CreateHtlcMethodName,
			g.User2.Address,                // hashlocked
			int64(genesisTimestamp+300),    // expiration time
			uint8(definition.HashTypeSHA3), // hash type
			uint8(32),                      // max preimage size
			lockQ,                          // hashlock
		),
		TokenStandard: types.QsrTokenStandard,
		Amount:        big.NewInt(10 * g.Zexp),
	}).Error(t, nil)
	z.InsertNewMomentum()
	z.InsertNewMomentum()

	htlcId2 := types.HexToHashPanic("ffb6f2e5328e535a8e9a3e85682ab107dd60ec4e3180cf48c03c0a3fbef9b635")

	// user 2 disables proxy unlock
	defer z.CallContract(&nom.AccountBlock{
		Address:       g.User2.Address,
		ToAddress:     types.HtlcContract,
		Data:          definition.ABIHtlc.PackMethodPanic(definition.DenyHtlcProxyUnlockMethodName),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(0 * g.Zexp),
	}).Error(t, nil)
	z.InsertNewMomentum()

	common.Json(htlcApi.GetProxyUnlockStatus(g.User2.Address)).Equals(t, `false`)

	// user 2 allows proxy unlock
	defer z.CallContract(&nom.AccountBlock{
		Address:       g.User2.Address,
		ToAddress:     types.HtlcContract,
		Data:          definition.ABIHtlc.PackMethodPanic(definition.AllowHtlcProxyUnlockMethodName),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(0 * g.Zexp),
	}).Error(t, nil)
	z.InsertNewMomentum()

	common.Json(htlcApi.GetProxyUnlockStatus(g.User2.Address)).Equals(t, `true`)

	// user 3 tries to unlock the first htlc with wrong preimage
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User3.Address,
		ToAddress: types.HtlcContract,
		Data: definition.ABIHtlc.PackMethodPanic(definition.UnlockHtlcMethodName,
			htlcId,    // entry id
			preimageQ, // preimage
		),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(0 * g.Zexp),
	}).Error(t, constants.ErrInvalidPreimage)
	z.InsertNewMomentum()

	// user 3 unlocks with the first htlc with the correct preimage
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User3.Address,
		ToAddress: types.HtlcContract,
		Data: definition.ABIHtlc.PackMethodPanic(definition.UnlockHtlcMethodName,
			htlcId,   // entry id
			preimage, // preimage
		),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(0 * g.Zexp),
	}).Error(t, nil)
	z.InsertNewMomentum()

	// user 2 tries to unlock the second htlc with wrong preimage
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User2.Address,
		ToAddress: types.HtlcContract,
		Data: definition.ABIHtlc.PackMethodPanic(definition.UnlockHtlcMethodName,
			htlcId2,  // entry id
			preimage, // preimage
		),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(0 * g.Zexp),
	}).Error(t, constants.ErrInvalidPreimage)
	z.InsertNewMomentum()

	// user 2 unlocks with the second htlc with the correct preimage
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User2.Address,
		ToAddress: types.HtlcContract,
		Data: definition.ABIHtlc.PackMethodPanic(definition.UnlockHtlcMethodName,
			htlcId2,   // entry id
			preimageQ, // preimage
		),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(0 * g.Zexp),
	}).Error(t, nil)
	z.InsertNewMomentum()
	z.InsertNewMomentum()

	common.Json(htlcApi.GetById(htlcId)).Error(t, constants.ErrDataNonExistent)

	autoreceive(t, z, g.User2.Address)
	z.InsertNewMomentum()
	z.InsertNewMomentum()

	z.ExpectBalance(g.User1.Address, types.ZnnTokenStandard, (12000-10)*g.Zexp)
	z.ExpectBalance(g.User1.Address, types.QsrTokenStandard, (120000-10)*g.Zexp)

	z.ExpectBalance(g.User2.Address, types.ZnnTokenStandard, (8000+10)*g.Zexp)
	z.ExpectBalance(g.User2.Address, types.QsrTokenStandard, (80000+10)*g.Zexp)

	z.ExpectBalance(types.HtlcContract, types.ZnnTokenStandard, 0*g.Zexp)
	z.ExpectBalance(types.HtlcContract, types.QsrTokenStandard, 0*g.Zexp)
}

func TestHtlc_reclaim_access(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).Equals(t, `
t=2001-09-09T01:46:50+0000 lvl=dbug msg=created module=embedded contract=spork spork="&{Id:664147f0c0a127bb4388bf8ff9a2ce777c9cc5ce9f04f9a6d418a32ef3f481c9 Name:spork-htlc Description:activate spork for htlc Activated:false EnforcementHeight:0}"
t=2001-09-09T01:47:00+0000 lvl=dbug msg=activated module=embedded contract=spork spork="&{Id:664147f0c0a127bb4388bf8ff9a2ce777c9cc5ce9f04f9a6d418a32ef3f481c9 Name:spork-htlc Description:activate spork for htlc Activated:true EnforcementHeight:9}"
t=2001-09-09T01:50:00+0000 lvl=dbug msg=created module=embedded contract=htlc htlcInfo="Id:c1b78112f757c24d290374c7992c8c90c980237e95d04ab010531c55ca6496c9 TimeLocked:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz HashLocked:z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx TokenStandard:zts1qsrxxxxxxxxxxxxxmrhjll Amount:1000000000 ExpirationTime:1000000300 HashType:0 KeyMaxSize:32 HashLock:Fd4QDoNykDbHp30bYIghQmK4OOcnATsGilLcpt7kV8s="
t=2001-09-09T01:50:10+0000 lvl=dbug msg="invalid reclaim - permission denied" module=embedded contract=htlc id=c1b78112f757c24d290374c7992c8c90c980237e95d04ab010531c55ca6496c9 address=z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx
t=2001-09-09T01:50:20+0000 lvl=dbug msg="invalid reclaim - permission denied" module=embedded contract=htlc id=c1b78112f757c24d290374c7992c8c90c980237e95d04ab010531c55ca6496c9 address=z1qrs2lpccnsneglhnnfwvlsj0qncnxjnwlfmjac
t=2001-09-09T01:53:20+0000 lvl=dbug msg="invalid reclaim - permission denied" module=embedded contract=htlc id=c1b78112f757c24d290374c7992c8c90c980237e95d04ab010531c55ca6496c9 address=z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx
t=2001-09-09T01:53:30+0000 lvl=dbug msg="invalid reclaim - permission denied" module=embedded contract=htlc id=c1b78112f757c24d290374c7992c8c90c980237e95d04ab010531c55ca6496c9 address=z1qrs2lpccnsneglhnnfwvlsj0qncnxjnwlfmjac
t=2001-09-09T01:53:40+0000 lvl=dbug msg=reclaimed module=embedded contract=htlc htlcInfo="Id:c1b78112f757c24d290374c7992c8c90c980237e95d04ab010531c55ca6496c9 TimeLocked:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz HashLocked:z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx TokenStandard:zts1qsrxxxxxxxxxxxxxmrhjll Amount:1000000000 ExpirationTime:1000000300 HashType:0 KeyMaxSize:32 HashLock:Fd4QDoNykDbHp30bYIghQmK4OOcnATsGilLcpt7kV8s="
`)
	activateHtlc(z)

	preimage := preimageZ
	lock := crypto.Hash(preimage)

	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.HtlcContract,
		Data: definition.ABIHtlc.PackMethodPanic(definition.CreateHtlcMethodName,
			g.User2.Address,                // hashlocked
			int64(genesisTimestamp+300),    // expiration time
			uint8(definition.HashTypeSHA3), // hash type
			uint8(32),                      // max preimage size
			lock,                           // hashlock
		),
		TokenStandard: types.QsrTokenStandard,
		Amount:        big.NewInt(10 * g.Zexp),
	}).Error(t, nil)
	z.InsertNewMomentum()

	htlcId := types.HexToHashPanic("c1b78112f757c24d290374c7992c8c90c980237e95d04ab010531c55ca6496c9")

	// user 2 tries to reclaim unexpired
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User2.Address,
		ToAddress: types.HtlcContract,
		Data: definition.ABIHtlc.PackMethodPanic(definition.ReclaimHtlcMethodName,
			htlcId, // entry id
		),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(0 * g.Zexp),
	}).Error(t, constants.ErrPermissionDenied)
	z.InsertNewMomentum()

	// user 3 tries to reclaim unexpired
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User3.Address,
		ToAddress: types.HtlcContract,
		Data: definition.ABIHtlc.PackMethodPanic(definition.ReclaimHtlcMethodName,
			htlcId, // entry id
		),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(0 * g.Zexp),
	}).Error(t, constants.ErrPermissionDenied)
	z.InsertNewMomentum()

	// expire the entry
	z.InsertMomentumsTo(40)

	// user 2 tries to reclaim expired
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User2.Address,
		ToAddress: types.HtlcContract,
		Data: definition.ABIHtlc.PackMethodPanic(definition.ReclaimHtlcMethodName,
			htlcId, // entry id
		),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(0 * g.Zexp),
	}).Error(t, constants.ErrPermissionDenied)
	z.InsertNewMomentum()

	// user 3 tries to reclaim expired
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User3.Address,
		ToAddress: types.HtlcContract,
		Data: definition.ABIHtlc.PackMethodPanic(definition.ReclaimHtlcMethodName,
			htlcId, // entry id
		),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(0 * g.Zexp),
	}).Error(t, constants.ErrPermissionDenied)
	z.InsertNewMomentum()

	// user 1 reclaims expired
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.HtlcContract,
		Data: definition.ABIHtlc.PackMethodPanic(definition.ReclaimHtlcMethodName,
			htlcId, // entry id
		),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(0 * g.Zexp),
	}).Error(t, nil)
	z.InsertNewMomentum()
	z.InsertNewMomentum()

	autoreceive(t, z, g.User1.Address)
	z.InsertNewMomentum()
	z.InsertNewMomentum()

	z.ExpectBalance(g.User1.Address, types.ZnnTokenStandard, 12000*g.Zexp)
	z.ExpectBalance(g.User1.Address, types.QsrTokenStandard, 120000*g.Zexp)

	z.ExpectBalance(g.User2.Address, types.ZnnTokenStandard, 8000*g.Zexp)
	z.ExpectBalance(g.User2.Address, types.QsrTokenStandard, 80000*g.Zexp)

	z.ExpectBalance(types.HtlcContract, types.ZnnTokenStandard, 0*g.Zexp)
	z.ExpectBalance(types.HtlcContract, types.QsrTokenStandard, 0*g.Zexp)
}

func TestHtlc_nonexistent(t *testing.T) {
	z := mock.NewMockZenon(t)
	htlcApi := embedded.NewHtlcApi(z)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).Equals(t, `
t=2001-09-09T01:46:50+0000 lvl=dbug msg=created module=embedded contract=spork spork="&{Id:664147f0c0a127bb4388bf8ff9a2ce777c9cc5ce9f04f9a6d418a32ef3f481c9 Name:spork-htlc Description:activate spork for htlc Activated:false EnforcementHeight:0}"
t=2001-09-09T01:47:00+0000 lvl=dbug msg=activated module=embedded contract=spork spork="&{Id:664147f0c0a127bb4388bf8ff9a2ce777c9cc5ce9f04f9a6d418a32ef3f481c9 Name:spork-htlc Description:activate spork for htlc Activated:true EnforcementHeight:9}"
t=2001-09-09T01:50:00+0000 lvl=dbug msg="invalid unlock - entry does not exist" module=embedded contract=htlc id=7efdcca315f86cdb04e84113bfc5f003fa49c4b3f9b287cd3b4a08d8ccdf6ffc address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz
t=2001-09-09T01:50:10+0000 lvl=dbug msg="invalid reclaim - entry does not exist" module=embedded contract=htlc id=7efdcca315f86cdb04e84113bfc5f003fa49c4b3f9b287cd3b4a08d8ccdf6ffc address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz
`)
	activateHtlc(z)

	preimage := preimageZ
	nonexistentId := types.HexToHashPanic("7efdcca315f86cdb04e84113bfc5f003fa49c4b3f9b287cd3b4a08d8ccdf6ffc")

	// get htlcinfo rpc nonexistent
	common.Json(htlcApi.GetById(nonexistentId)).Error(t, constants.ErrDataNonExistent)

	// unlock nonexistent
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.HtlcContract,
		Data: definition.ABIHtlc.PackMethodPanic(definition.UnlockHtlcMethodName,
			nonexistentId, // entry id
			preimage,      // preimage
		),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(0 * g.Zexp),
	}).Error(t, constants.ErrDataNonExistent)
	z.InsertNewMomentum()

	// reclaim nonexistent
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.HtlcContract,
		Data: definition.ABIHtlc.PackMethodPanic(definition.ReclaimHtlcMethodName,
			nonexistentId, // entry id
		),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(0 * g.Zexp),
	}).Error(t, constants.ErrDataNonExistent)
	z.InsertNewMomentum()
}

func TestHtlc_nonexistent_after_unlock(t *testing.T) {
	z := mock.NewMockZenon(t)
	htlcApi := embedded.NewHtlcApi(z)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).Equals(t, `
t=2001-09-09T01:46:50+0000 lvl=dbug msg=created module=embedded contract=spork spork="&{Id:664147f0c0a127bb4388bf8ff9a2ce777c9cc5ce9f04f9a6d418a32ef3f481c9 Name:spork-htlc Description:activate spork for htlc Activated:false EnforcementHeight:0}"
t=2001-09-09T01:47:00+0000 lvl=dbug msg=activated module=embedded contract=spork spork="&{Id:664147f0c0a127bb4388bf8ff9a2ce777c9cc5ce9f04f9a6d418a32ef3f481c9 Name:spork-htlc Description:activate spork for htlc Activated:true EnforcementHeight:9}"
t=2001-09-09T01:50:00+0000 lvl=dbug msg=created module=embedded contract=htlc htlcInfo="Id:7efdcca315f86cdb04e84113bfc5f003fa49c4b3f9b287cd3b4a08d8ccdf6ffc TimeLocked:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz HashLocked:z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx TokenStandard:zts1znnxxxxxxxxxxxxx9z4ulx Amount:1000000000 ExpirationTime:1000000300 HashType:0 KeyMaxSize:32 HashLock:Fd4QDoNykDbHp30bYIghQmK4OOcnATsGilLcpt7kV8s="
t=2001-09-09T01:50:20+0000 lvl=dbug msg=unlocked module=embedded contract=htlc htlcInfo="Id:7efdcca315f86cdb04e84113bfc5f003fa49c4b3f9b287cd3b4a08d8ccdf6ffc TimeLocked:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz HashLocked:z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx TokenStandard:zts1znnxxxxxxxxxxxxx9z4ulx Amount:1000000000 ExpirationTime:1000000300 HashType:0 KeyMaxSize:32 HashLock:Fd4QDoNykDbHp30bYIghQmK4OOcnATsGilLcpt7kV8s=" preimage=b7845adcd41eec4e4fa1cc75a868014811b575942c6e4a72551bc01f63705634
t=2001-09-09T01:50:40+0000 lvl=dbug msg="invalid unlock - entry does not exist" module=embedded contract=htlc id=7efdcca315f86cdb04e84113bfc5f003fa49c4b3f9b287cd3b4a08d8ccdf6ffc address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz
t=2001-09-09T01:50:50+0000 lvl=dbug msg="invalid reclaim - entry does not exist" module=embedded contract=htlc id=7efdcca315f86cdb04e84113bfc5f003fa49c4b3f9b287cd3b4a08d8ccdf6ffc address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz
`)
	activateHtlc(z)

	preimage := preimageZ
	lock := crypto.Hash(preimage)

	// user 1 creates an htlc for user 2
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.HtlcContract,
		Data: definition.ABIHtlc.PackMethodPanic(definition.CreateHtlcMethodName,
			g.User2.Address,                // hashlocked
			int64(genesisTimestamp+300),    // expiration time
			uint8(definition.HashTypeSHA3), // hash type
			uint8(32),                      // max preimage size
			lock,                           // hashlock
		),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(10 * g.Zexp),
	}).Error(t, nil)
	z.InsertNewMomentum()
	z.InsertNewMomentum()

	htlcId := types.HexToHashPanic("7efdcca315f86cdb04e84113bfc5f003fa49c4b3f9b287cd3b4a08d8ccdf6ffc")

	// user2 unlocks with correct preimage
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User2.Address,
		ToAddress: types.HtlcContract,
		Data: definition.ABIHtlc.PackMethodPanic(definition.UnlockHtlcMethodName,
			htlcId,   // entry id
			preimage, // preimage
		),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(0 * g.Zexp),
	}).Error(t, nil)
	z.InsertNewMomentum()
	z.InsertNewMomentum()

	// get htlcinfo rpc nonexistent
	common.Json(htlcApi.GetById(htlcId)).Error(t, constants.ErrDataNonExistent)

	// unlock nonexistent
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.HtlcContract,
		Data: definition.ABIHtlc.PackMethodPanic(definition.UnlockHtlcMethodName,
			htlcId,   // entry id
			preimage, // preimage
		),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(0 * g.Zexp),
	}).Error(t, constants.ErrDataNonExistent)
	z.InsertNewMomentum()

	// reclaim nonexistent
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.HtlcContract,
		Data: definition.ABIHtlc.PackMethodPanic(definition.ReclaimHtlcMethodName,
			htlcId, // entry id
		),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(0 * g.Zexp),
	}).Error(t, constants.ErrDataNonExistent)
	z.InsertNewMomentum()
}

func TestHtlc_nonexistent_after_reclaim(t *testing.T) {
	z := mock.NewMockZenon(t)
	htlcApi := embedded.NewHtlcApi(z)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).Equals(t, `
t=2001-09-09T01:46:50+0000 lvl=dbug msg=created module=embedded contract=spork spork="&{Id:664147f0c0a127bb4388bf8ff9a2ce777c9cc5ce9f04f9a6d418a32ef3f481c9 Name:spork-htlc Description:activate spork for htlc Activated:false EnforcementHeight:0}"
t=2001-09-09T01:47:00+0000 lvl=dbug msg=activated module=embedded contract=spork spork="&{Id:664147f0c0a127bb4388bf8ff9a2ce777c9cc5ce9f04f9a6d418a32ef3f481c9 Name:spork-htlc Description:activate spork for htlc Activated:true EnforcementHeight:9}"
t=2001-09-09T01:50:00+0000 lvl=dbug msg=created module=embedded contract=htlc htlcInfo="Id:7efdcca315f86cdb04e84113bfc5f003fa49c4b3f9b287cd3b4a08d8ccdf6ffc TimeLocked:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz HashLocked:z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx TokenStandard:zts1znnxxxxxxxxxxxxx9z4ulx Amount:1000000000 ExpirationTime:1000000300 HashType:0 KeyMaxSize:32 HashLock:Fd4QDoNykDbHp30bYIghQmK4OOcnATsGilLcpt7kV8s="
t=2001-09-09T01:53:20+0000 lvl=dbug msg=reclaimed module=embedded contract=htlc htlcInfo="Id:7efdcca315f86cdb04e84113bfc5f003fa49c4b3f9b287cd3b4a08d8ccdf6ffc TimeLocked:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz HashLocked:z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx TokenStandard:zts1znnxxxxxxxxxxxxx9z4ulx Amount:1000000000 ExpirationTime:1000000300 HashType:0 KeyMaxSize:32 HashLock:Fd4QDoNykDbHp30bYIghQmK4OOcnATsGilLcpt7kV8s="
t=2001-09-09T01:53:40+0000 lvl=dbug msg="invalid unlock - entry does not exist" module=embedded contract=htlc id=7efdcca315f86cdb04e84113bfc5f003fa49c4b3f9b287cd3b4a08d8ccdf6ffc address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz
t=2001-09-09T01:53:50+0000 lvl=dbug msg="invalid reclaim - entry does not exist" module=embedded contract=htlc id=7efdcca315f86cdb04e84113bfc5f003fa49c4b3f9b287cd3b4a08d8ccdf6ffc address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz
`)
	activateHtlc(z)

	preimage := preimageZ
	lock := crypto.Hash(preimage)

	// user 1 creates an htlc for user 2
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.HtlcContract,
		Data: definition.ABIHtlc.PackMethodPanic(definition.CreateHtlcMethodName,
			g.User2.Address,                // hashlocked
			int64(genesisTimestamp+300),    // expiration time
			uint8(definition.HashTypeSHA3), // hash type
			uint8(32),                      // max preimage size
			lock,                           // hashlock
		),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(10 * g.Zexp),
	}).Error(t, nil)
	z.InsertNewMomentum()
	z.InsertMomentumsTo(40)

	htlcId := types.HexToHashPanic("7efdcca315f86cdb04e84113bfc5f003fa49c4b3f9b287cd3b4a08d8ccdf6ffc")

	// user1 reclaims expired
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.HtlcContract,
		Data: definition.ABIHtlc.PackMethodPanic(definition.ReclaimHtlcMethodName,
			htlcId, // entry id
		),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(0 * g.Zexp),
	}).Error(t, nil)
	z.InsertNewMomentum()
	z.InsertNewMomentum()

	// get htlcinfo rpc nonexistent
	common.Json(htlcApi.GetById(htlcId)).Error(t, constants.ErrDataNonExistent)

	// unlock nonexistent
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.HtlcContract,
		Data: definition.ABIHtlc.PackMethodPanic(definition.UnlockHtlcMethodName,
			htlcId,   // entry id
			preimage, // preimage
		),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(0 * g.Zexp),
	}).Error(t, constants.ErrDataNonExistent)
	z.InsertNewMomentum()

	// reclaim nonexistent
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.HtlcContract,
		Data: definition.ABIHtlc.PackMethodPanic(definition.ReclaimHtlcMethodName,
			htlcId, // entry id
		),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(0 * g.Zexp),
	}).Error(t, constants.ErrDataNonExistent)
	z.InsertNewMomentum()

}

func TestHtlc_create_expired(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).Equals(t, `
t=2001-09-09T01:46:50+0000 lvl=dbug msg=created module=embedded contract=spork spork="&{Id:664147f0c0a127bb4388bf8ff9a2ce777c9cc5ce9f04f9a6d418a32ef3f481c9 Name:spork-htlc Description:activate spork for htlc Activated:false EnforcementHeight:0}"
t=2001-09-09T01:47:00+0000 lvl=dbug msg=activated module=embedded contract=spork spork="&{Id:664147f0c0a127bb4388bf8ff9a2ce777c9cc5ce9f04f9a6d418a32ef3f481c9 Name:spork-htlc Description:activate spork for htlc Activated:true EnforcementHeight:9}"
t=2001-09-09T01:50:00+0000 lvl=dbug msg="invalid create - cannot create already expired" module=embedded contract=htlc address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz time=1000000200 expiration-time=999999700
`)
	activateHtlc(z)

	preimage := preimageZ
	lock := crypto.Hash(preimage)

	// user tries to create expired htlc
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.HtlcContract,
		Data: definition.ABIHtlc.PackMethodPanic(definition.CreateHtlcMethodName,
			g.User2.Address,                // hashlocked
			int64(genesisTimestamp-300),    // expiration time
			uint8(definition.HashTypeSHA3), // hash type
			uint8(32),                      // max preimage size
			lock,                           // hashlock
		),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(10 * g.Zexp),
	}).Error(t, constants.ErrInvalidExpirationTime)
	z.InsertNewMomentum()
	z.InsertNewMomentum()

	autoreceive(t, z, g.User1.Address)
	z.InsertNewMomentum()
	z.InsertNewMomentum()

	z.ExpectBalance(g.User1.Address, types.ZnnTokenStandard, 12000*g.Zexp)
	z.ExpectBalance(g.User1.Address, types.QsrTokenStandard, 120000*g.Zexp)

	z.ExpectBalance(g.User2.Address, types.ZnnTokenStandard, 8000*g.Zexp)
	z.ExpectBalance(g.User2.Address, types.QsrTokenStandard, 80000*g.Zexp)

	z.ExpectBalance(types.HtlcContract, types.ZnnTokenStandard, 0*g.Zexp)
	z.ExpectBalance(types.HtlcContract, types.QsrTokenStandard, 0*g.Zexp)
}

// test unlock htlc with ErrInvalidPreimage
func TestHtlc_unlock_long_preimage(t *testing.T) {
	z := mock.NewMockZenon(t)
	htlcApi := embedded.NewHtlcApi(z)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).Equals(t, `
t=2001-09-09T01:46:50+0000 lvl=dbug msg=created module=embedded contract=spork spork="&{Id:664147f0c0a127bb4388bf8ff9a2ce777c9cc5ce9f04f9a6d418a32ef3f481c9 Name:spork-htlc Description:activate spork for htlc Activated:false EnforcementHeight:0}"
t=2001-09-09T01:47:00+0000 lvl=dbug msg=activated module=embedded contract=spork spork="&{Id:664147f0c0a127bb4388bf8ff9a2ce777c9cc5ce9f04f9a6d418a32ef3f481c9 Name:spork-htlc Description:activate spork for htlc Activated:true EnforcementHeight:9}"
t=2001-09-09T01:50:00+0000 lvl=dbug msg=created module=embedded contract=htlc htlcInfo="Id:e243752d51ece92295429843f06e21bc12b18129d6c79498552fe513b51f488f TimeLocked:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz HashLocked:z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx TokenStandard:zts1znnxxxxxxxxxxxxx9z4ulx Amount:1000000000 ExpirationTime:1000000300 HashType:0 KeyMaxSize:32 HashLock:CBRBxGZSmTPMKTf5M+Ik70GyXYeCQuiRPiTLWB7h8yU="
t=2001-09-09T01:50:20+0000 lvl=dbug msg="invalid unlock - preimage size greater than entry KeyMaxSize" module=embedded contract=htlc id=e243752d51ece92295429843f06e21bc12b18129d6c79498552fe513b51f488f address=z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx preimage-size=64 max-size=32
`)
	activateHtlc(z)

	// ideally for this test we would have a known hash collision with 2 preimages of different sizes
	// this test relies on knowledge that if the preimage produces the right hash, that it won't skip the size check
	preimage := preimageZlong
	lock := crypto.Hash(preimage)

	// user1 creates htlc for user 2
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.HtlcContract,
		Data: definition.ABIHtlc.PackMethodPanic(definition.CreateHtlcMethodName,
			g.User2.Address,                // hashlocked
			int64(genesisTimestamp+300),    // expiration time
			uint8(definition.HashTypeSHA3), // hash type
			uint8(32),                      // max preimage size
			lock,                           // hashlock
		),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(10 * g.Zexp),
	}).Error(t, nil)
	z.InsertNewMomentum()
	z.InsertNewMomentum()

	htlcId := types.HexToHashPanic("e243752d51ece92295429843f06e21bc12b18129d6c79498552fe513b51f488f")

	common.Json(htlcApi.GetById(htlcId)).Equals(t, `
{
	"id": "e243752d51ece92295429843f06e21bc12b18129d6c79498552fe513b51f488f",
	"timeLocked": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
	"hashLocked": "z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx",
	"tokenStandard": "zts1znnxxxxxxxxxxxxx9z4ulx",
	"amount": "1000000000",
	"expirationTime": 1000000300,
	"hashType": 0,
	"keyMaxSize": 32,
	"hashLock": "CBRBxGZSmTPMKTf5M+Ik70GyXYeCQuiRPiTLWB7h8yU="
}
`)

	// user2 tries to unlock with oversized preimage
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User2.Address,
		ToAddress: types.HtlcContract,
		Data: definition.ABIHtlc.PackMethodPanic(definition.UnlockHtlcMethodName,
			htlcId,   // entry id
			preimage, // preimage
		),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(0 * g.Zexp),
	}).Error(t, constants.ErrInvalidPreimage)
	z.InsertNewMomentum()
	z.InsertNewMomentum()

	z.ExpectBalance(g.User1.Address, types.ZnnTokenStandard, (12000-10)*g.Zexp)
	z.ExpectBalance(g.User1.Address, types.QsrTokenStandard, 120000*g.Zexp)

	z.ExpectBalance(g.User2.Address, types.ZnnTokenStandard, 8000*g.Zexp)
	z.ExpectBalance(g.User2.Address, types.QsrTokenStandard, 80000*g.Zexp)

	z.ExpectBalance(types.HtlcContract, types.ZnnTokenStandard, 10*g.Zexp)
	z.ExpectBalance(types.HtlcContract, types.QsrTokenStandard, 0*g.Zexp)

}

// SHA256 testing
// blackbox testing principles would dictate that we run the same set of tests
// as above with each different hash type
// But until we have parameterized tests, will add streamlined higher value tests
// happy path and making sure sha3 preimage can't unlock sha256 hashlock
// and vice versa

func TestHtlc_unlockSHA256(t *testing.T) {
	z := mock.NewMockZenon(t)
	htlcApi := embedded.NewHtlcApi(z)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).Equals(t, `
t=2001-09-09T01:46:50+0000 lvl=dbug msg=created module=embedded contract=spork spork="&{Id:664147f0c0a127bb4388bf8ff9a2ce777c9cc5ce9f04f9a6d418a32ef3f481c9 Name:spork-htlc Description:activate spork for htlc Activated:false EnforcementHeight:0}"
t=2001-09-09T01:47:00+0000 lvl=dbug msg=activated module=embedded contract=spork spork="&{Id:664147f0c0a127bb4388bf8ff9a2ce777c9cc5ce9f04f9a6d418a32ef3f481c9 Name:spork-htlc Description:activate spork for htlc Activated:true EnforcementHeight:9}"
t=2001-09-09T01:50:00+0000 lvl=dbug msg=created module=embedded contract=htlc htlcInfo="Id:6cd142141eb8485376a04194000764a08f42cf2b9531945b5dbfed1259969e82 TimeLocked:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz HashLocked:z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx TokenStandard:zts1znnxxxxxxxxxxxxx9z4ulx Amount:1000000000 ExpirationTime:1000000300 HashType:1 KeyMaxSize:32 HashLock:zYaMcaHJ1yxUmbaLsG7tN0J34zNthDoPEZFEYcNd0DY="
t=2001-09-09T01:50:20+0000 lvl=dbug msg="invalid unlock - wrong preimage" module=embedded contract=htlc id=6cd142141eb8485376a04194000764a08f42cf2b9531945b5dbfed1259969e82 address=z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx preimage=d70b59367334f9c6d4771059093ec11cb505d7b2b0e233cc8bde00fe7aec3cee
t=2001-09-09T01:50:30+0000 lvl=dbug msg=unlocked module=embedded contract=htlc htlcInfo="Id:6cd142141eb8485376a04194000764a08f42cf2b9531945b5dbfed1259969e82 TimeLocked:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz HashLocked:z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx TokenStandard:zts1znnxxxxxxxxxxxxx9z4ulx Amount:1000000000 ExpirationTime:1000000300 HashType:1 KeyMaxSize:32 HashLock:zYaMcaHJ1yxUmbaLsG7tN0J34zNthDoPEZFEYcNd0DY=" preimage=b7845adcd41eec4e4fa1cc75a868014811b575942c6e4a72551bc01f63705634
`)
	activateHtlc(z)

	preimage := preimageZ
	lock := crypto.HashSHA256(preimage)

	// user 1 creates an htlc for user 2 using sha256 locktype
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.HtlcContract,
		Data: definition.ABIHtlc.PackMethodPanic(definition.CreateHtlcMethodName,
			g.User2.Address,                  // hashlocked
			int64(genesisTimestamp+300),      // expiration time
			uint8(definition.HashTypeSHA256), // hash type
			uint8(32),                        // max preimage size
			lock,                             // hashlock
		),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(10 * g.Zexp),
	}).Error(t, nil)
	z.InsertNewMomentum()
	z.InsertNewMomentum()

	z.ExpectBalance(g.User1.Address, types.ZnnTokenStandard, 11990*g.Zexp)
	z.ExpectBalance(g.User1.Address, types.QsrTokenStandard, 120000*g.Zexp)

	z.ExpectBalance(g.User2.Address, types.ZnnTokenStandard, 8000*g.Zexp)
	z.ExpectBalance(g.User2.Address, types.QsrTokenStandard, 80000*g.Zexp)

	z.ExpectBalance(types.HtlcContract, types.ZnnTokenStandard, 10*g.Zexp)
	z.ExpectBalance(types.HtlcContract, types.QsrTokenStandard, 0*g.Zexp)

	htlcId := types.HexToHashPanic("6cd142141eb8485376a04194000764a08f42cf2b9531945b5dbfed1259969e82")

	common.Json(htlcApi.GetById(htlcId)).Equals(t, `
{
	"id": "6cd142141eb8485376a04194000764a08f42cf2b9531945b5dbfed1259969e82",
	"timeLocked": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
	"hashLocked": "z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx",
	"tokenStandard": "zts1znnxxxxxxxxxxxxx9z4ulx",
	"amount": "1000000000",
	"expirationTime": 1000000300,
	"hashType": 1,
	"keyMaxSize": 32,
	"hashLock": "zYaMcaHJ1yxUmbaLsG7tN0J34zNthDoPEZFEYcNd0DY="
}
`)

	// user 2 tries to unlock with wrong preimage
	wrong_preimage := preimageQ
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User2.Address,
		ToAddress: types.HtlcContract,
		Data: definition.ABIHtlc.PackMethodPanic(definition.UnlockHtlcMethodName,
			htlcId,         // entry id
			wrong_preimage, // preimage
		),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(0 * g.Zexp),
	}).Error(t, constants.ErrInvalidPreimage)
	z.InsertNewMomentum()

	// user2 unlocks with correct preimage
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User2.Address,
		ToAddress: types.HtlcContract,
		Data: definition.ABIHtlc.PackMethodPanic(definition.UnlockHtlcMethodName,
			htlcId,   // entry id
			preimage, // preimage
		),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(0 * g.Zexp),
	}).Error(t, nil)
	z.InsertNewMomentum()
	z.InsertNewMomentum()

	autoreceive(t, z, g.User2.Address)
	z.InsertNewMomentum()
	z.InsertNewMomentum()

	z.ExpectBalance(g.User1.Address, types.ZnnTokenStandard, (12000-10)*g.Zexp)
	z.ExpectBalance(g.User1.Address, types.QsrTokenStandard, 120000*g.Zexp)

	z.ExpectBalance(g.User2.Address, types.ZnnTokenStandard, (8000+10)*g.Zexp)
	z.ExpectBalance(g.User2.Address, types.QsrTokenStandard, 80000*g.Zexp)

	z.ExpectBalance(types.HtlcContract, types.ZnnTokenStandard, 0*g.Zexp)
	z.ExpectBalance(types.HtlcContract, types.QsrTokenStandard, 0*g.Zexp)

}

func TestHtlc_hashType(t *testing.T) {
	z := mock.NewMockZenon(t)
	htlcApi := embedded.NewHtlcApi(z)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).Equals(t, `
t=2001-09-09T01:46:50+0000 lvl=dbug msg=created module=embedded contract=spork spork="&{Id:664147f0c0a127bb4388bf8ff9a2ce777c9cc5ce9f04f9a6d418a32ef3f481c9 Name:spork-htlc Description:activate spork for htlc Activated:false EnforcementHeight:0}"
t=2001-09-09T01:47:00+0000 lvl=dbug msg=activated module=embedded contract=spork spork="&{Id:664147f0c0a127bb4388bf8ff9a2ce777c9cc5ce9f04f9a6d418a32ef3f481c9 Name:spork-htlc Description:activate spork for htlc Activated:true EnforcementHeight:9}"
t=2001-09-09T01:50:00+0000 lvl=dbug msg=created module=embedded contract=htlc htlcInfo="Id:147b9ca1b4c6b0c205b6c5bfe0f501ec6cd9f2acfeaaeb51751e960a6d9543cf TimeLocked:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz HashLocked:z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx TokenStandard:zts1znnxxxxxxxxxxxxx9z4ulx Amount:1000000000 ExpirationTime:1000000300 HashType:0 KeyMaxSize:32 HashLock:zYaMcaHJ1yxUmbaLsG7tN0J34zNthDoPEZFEYcNd0DY="
t=2001-09-09T01:50:20+0000 lvl=dbug msg=created module=embedded contract=htlc htlcInfo="Id:3b126fa7fc90cbd55e311fdfe9b2a9943a0ced7ba41a6388913a539cb719821e TimeLocked:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz HashLocked:z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx TokenStandard:zts1znnxxxxxxxxxxxxx9z4ulx Amount:1000000000 ExpirationTime:1000000300 HashType:1 KeyMaxSize:32 HashLock:Fd4QDoNykDbHp30bYIghQmK4OOcnATsGilLcpt7kV8s="
t=2001-09-09T01:50:40+0000 lvl=dbug msg="invalid unlock - wrong preimage" module=embedded contract=htlc id=147b9ca1b4c6b0c205b6c5bfe0f501ec6cd9f2acfeaaeb51751e960a6d9543cf address=z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx preimage=b7845adcd41eec4e4fa1cc75a868014811b575942c6e4a72551bc01f63705634
t=2001-09-09T01:50:50+0000 lvl=dbug msg="invalid unlock - wrong preimage" module=embedded contract=htlc id=3b126fa7fc90cbd55e311fdfe9b2a9943a0ced7ba41a6388913a539cb719821e address=z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx preimage=b7845adcd41eec4e4fa1cc75a868014811b575942c6e4a72551bc01f63705634
`)
	activateHtlc(z)

	preimage := preimageZ
	sha3lock := crypto.Hash(preimage)
	sha256lock := crypto.HashSHA256(preimage)

	// user 1 creates an htlc for user 2 using sha3 locktype with sha256 hashlock
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.HtlcContract,
		Data: definition.ABIHtlc.PackMethodPanic(definition.CreateHtlcMethodName,
			g.User2.Address,                // hashlocked
			int64(genesisTimestamp+300),    // expiration time
			uint8(definition.HashTypeSHA3), // hash type
			uint8(32),                      // max preimage size
			sha256lock,                     // hashlock
		),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(10 * g.Zexp),
	}).Error(t, nil)
	z.InsertNewMomentum()
	z.InsertNewMomentum()

	htlcId1 := types.HexToHashPanic("147b9ca1b4c6b0c205b6c5bfe0f501ec6cd9f2acfeaaeb51751e960a6d9543cf")
	common.Json(htlcApi.GetById(htlcId1)).Equals(t, `
{
	"id": "147b9ca1b4c6b0c205b6c5bfe0f501ec6cd9f2acfeaaeb51751e960a6d9543cf",
	"timeLocked": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
	"hashLocked": "z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx",
	"tokenStandard": "zts1znnxxxxxxxxxxxxx9z4ulx",
	"amount": "1000000000",
	"expirationTime": 1000000300,
	"hashType": 0,
	"keyMaxSize": 32,
	"hashLock": "zYaMcaHJ1yxUmbaLsG7tN0J34zNthDoPEZFEYcNd0DY="
}
`)

	// user 1 creates an htlc for user 2 using sha256
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.HtlcContract,
		Data: definition.ABIHtlc.PackMethodPanic(definition.CreateHtlcMethodName,
			g.User2.Address,                  // hashlocked
			int64(genesisTimestamp+300),      // expiration time
			uint8(definition.HashTypeSHA256), // hash type
			uint8(32),                        // max preimage size
			sha3lock,                         // hashlock
		),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(10 * g.Zexp),
	}).Error(t, nil)
	z.InsertNewMomentum()
	z.InsertNewMomentum()

	htlcId2 := types.HexToHashPanic("3b126fa7fc90cbd55e311fdfe9b2a9943a0ced7ba41a6388913a539cb719821e")
	common.Json(htlcApi.GetById(htlcId2)).Equals(t, `
{
	"id": "3b126fa7fc90cbd55e311fdfe9b2a9943a0ced7ba41a6388913a539cb719821e",
	"timeLocked": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
	"hashLocked": "z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx",
	"tokenStandard": "zts1znnxxxxxxxxxxxxx9z4ulx",
	"amount": "1000000000",
	"expirationTime": 1000000300,
	"hashType": 1,
	"keyMaxSize": 32,
	"hashLock": "Fd4QDoNykDbHp30bYIghQmK4OOcnATsGilLcpt7kV8s="
}
`)

	// user 2 cannot unlock sha3 lock
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User2.Address,
		ToAddress: types.HtlcContract,
		Data: definition.ABIHtlc.PackMethodPanic(definition.UnlockHtlcMethodName,
			htlcId1,  // entry id
			preimage, // preimage
		),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(0 * g.Zexp),
	}).Error(t, constants.ErrInvalidPreimage)
	z.InsertNewMomentum()

	// user 2 cannot unlock sha256 lock
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User2.Address,
		ToAddress: types.HtlcContract,
		Data: definition.ABIHtlc.PackMethodPanic(definition.UnlockHtlcMethodName,
			htlcId2,  // entry id
			preimage, // preimage
		),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(0 * g.Zexp),
	}).Error(t, constants.ErrInvalidPreimage)
	z.InsertNewMomentum()

	z.ExpectBalance(g.User1.Address, types.ZnnTokenStandard, (12000-20)*g.Zexp)
	z.ExpectBalance(g.User1.Address, types.QsrTokenStandard, 120000*g.Zexp)

	z.ExpectBalance(g.User2.Address, types.ZnnTokenStandard, 8000*g.Zexp)
	z.ExpectBalance(g.User2.Address, types.QsrTokenStandard, 80000*g.Zexp)

	z.ExpectBalance(types.HtlcContract, types.ZnnTokenStandard, 20*g.Zexp)
	z.ExpectBalance(types.HtlcContract, types.QsrTokenStandard, 0*g.Zexp)

}
