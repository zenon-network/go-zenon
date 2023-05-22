package tests

import (
	"math/big"
	"testing"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/btcec/v2/schnorr"
	g "github.com/zenon-network/go-zenon/chain/genesis/mock"
	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/crypto"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/rpc/api/embedded"
	"github.com/zenon-network/go-zenon/vm/constants"
	"github.com/zenon-network/go-zenon/vm/embedded/definition"
	"github.com/zenon-network/go-zenon/zenon/mock"
)

func activatePtlc(z mock.MockZenon) {
	sporkAPI := embedded.NewSporkApi(z)
	z.InsertSendBlock(&nom.AccountBlock{
		Address:   g.Spork.Address,
		ToAddress: types.SporkContract,
		Data: definition.ABISpork.PackMethodPanic(definition.SporkCreateMethodName,
			"spork-ptlc",              // name
			"activate spork for ptlc", // description
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
	types.PtlcSpork.SporkId = id
	types.ImplementedSporksMap[id] = true
	z.InsertMomentumsTo(20)
}

func TestPtlc_zero(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).Equals(t, `
t=2001-09-09T01:46:50+0000 lvl=dbug msg=created module=embedded contract=spork spork="&{Id:d82f15026ad67abbc99786a9ed5b667ac578a78fb80df4ea573c22e727fd736a Name:spork-ptlc Description:activate spork for ptlc Activated:false EnforcementHeight:0}"
t=2001-09-09T01:47:00+0000 lvl=dbug msg=activated module=embedded contract=spork spork="&{Id:d82f15026ad67abbc99786a9ed5b667ac578a78fb80df4ea573c22e727fd736a Name:spork-ptlc Description:activate spork for ptlc Activated:true EnforcementHeight:9}"
t=2001-09-09T01:49:50+0000 lvl=dbug msg="invalid create - cannot create zero amount" module=embedded contract=ptlc address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz
`)
	activatePtlc(z)

	// user 1 creates a ptlc for user 2
	z.InsertSendBlock(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.PtlcContract,
		Data: definition.ABIPtlc.PackMethodPanic(definition.CreatePtlcMethodName,
			int64(genesisTimestamp+300), // expiration time
			definition.PointTypeED25519, // point type
			g.User2.Public,              // point lock
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

	z.ExpectBalance(types.PtlcContract, types.ZnnTokenStandard, 0*g.Zexp)
	z.ExpectBalance(types.PtlcContract, types.QsrTokenStandard, 0*g.Zexp)
}

func TestPtlc_unlock(t *testing.T) {
	z := mock.NewMockZenon(t)
	ptlcApi := embedded.NewPtlcApi(z)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).Equals(t, `
t=2001-09-09T01:46:50+0000 lvl=dbug msg=created module=embedded contract=spork spork="&{Id:d82f15026ad67abbc99786a9ed5b667ac578a78fb80df4ea573c22e727fd736a Name:spork-ptlc Description:activate spork for ptlc Activated:false EnforcementHeight:0}"
t=2001-09-09T01:47:00+0000 lvl=dbug msg=activated module=embedded contract=spork spork="&{Id:d82f15026ad67abbc99786a9ed5b667ac578a78fb80df4ea573c22e727fd736a Name:spork-ptlc Description:activate spork for ptlc Activated:true EnforcementHeight:9}"
t=2001-09-09T01:50:00+0000 lvl=dbug msg=created module=embedded contract=ptlc ptlcInfo="Id:6809e10e211036a33d43ce4a72b71a5389ac8050df1249edefd52b632ce45b79 TimeLocked:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenStandard:zts1znnxxxxxxxxxxxxx9z4ulx Amount:1000000000 ExpirationTime:1000000300 PointType:0 PointLock:tUJu3P7Drp25XP662lIjyFlFpvj8bWUpyC+0y5YTzXM= "
t=2001-09-09T01:50:20+0000 lvl=dbug msg="invalid reclaim - entry not expired" module=embedded contract=ptlc id=6809e10e211036a33d43ce4a72b71a5389ac8050df1249edefd52b632ce45b79 address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz time=1000000220 expiration-time=1000000300
t=2001-09-09T01:50:30+0000 lvl=dbug msg="invalid unlock - invalid signature" module=embedded contract=ptlc id=6809e10e211036a33d43ce4a72b71a5389ac8050df1249edefd52b632ce45b79 address=z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx destination=z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx signature="+b3UITOi0c/ADkOdn7Gppq2e/7po8iNelu6nIWFDLbd+HbMv8ItiKj/MGcTQ7v9XMpMb/B4RBova+I9WH0CnDQ=="
t=2001-09-09T01:50:50+0000 lvl=dbug msg="invalid unlock - signature is wrong size" module=embedded contract=ptlc id=6809e10e211036a33d43ce4a72b71a5389ac8050df1249edefd52b632ce45b79 address=z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx received-size=63 expected-size=64
t=2001-09-09T01:51:10+0000 lvl=dbug msg=unlocked module=embedded contract=ptlc ptlcInfo="Id:6809e10e211036a33d43ce4a72b71a5389ac8050df1249edefd52b632ce45b79 TimeLocked:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenStandard:zts1znnxxxxxxxxxxxxx9z4ulx Amount:1000000000 ExpirationTime:1000000300 PointType:0 PointLock:tUJu3P7Drp25XP662lIjyFlFpvj8bWUpyC+0y5YTzXM= " destination=z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx signature="aFRIn613J+TaTP40Yzv9bk3eC2UPyc3PtIIp75yDnfbh+vQtm5ZOumAVNM6noBpHGjO6nFrAzHZ67Np9r8ArDA=="
`)
	activatePtlc(z)

	// user 1 creates a ptlc for user 2
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.PtlcContract,
		Data: definition.ABIPtlc.PackMethodPanic(definition.CreatePtlcMethodName,
			int64(genesisTimestamp+300), // expiration time
			definition.PointTypeED25519, // point type
			g.User2.Public,              // point lock
		),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(10 * g.Zexp),
	}).Error(t, nil)
	z.InsertNewMomentum()
	z.InsertNewMomentum()

	ptlcId := types.HexToHashPanic("6809e10e211036a33d43ce4a72b71a5389ac8050df1249edefd52b632ce45b79")
	common.Json(ptlcApi.GetById(ptlcId)).Equals(t, `
{
	"id": "6809e10e211036a33d43ce4a72b71a5389ac8050df1249edefd52b632ce45b79",
	"timeLocked": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
	"tokenStandard": "zts1znnxxxxxxxxxxxxx9z4ulx",
	"amount": 1000000000,
	"expirationTime": 1000000300,
	"pointType": 0,
	"pointLock": "tUJu3P7Drp25XP662lIjyFlFpvj8bWUpyC+0y5YTzXM="
}
`)

	z.ExpectBalance(g.User1.Address, types.ZnnTokenStandard, 11990*g.Zexp)
	z.ExpectBalance(g.User1.Address, types.QsrTokenStandard, 120000*g.Zexp)

	z.ExpectBalance(g.User2.Address, types.ZnnTokenStandard, 8000*g.Zexp)
	z.ExpectBalance(g.User2.Address, types.QsrTokenStandard, 80000*g.Zexp)

	z.ExpectBalance(types.PtlcContract, types.ZnnTokenStandard, 10*g.Zexp)
	z.ExpectBalance(types.PtlcContract, types.QsrTokenStandard, 0*g.Zexp)

	// user 1 tries to reclaim unexpired
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.PtlcContract,
		Data: definition.ABIPtlc.PackMethodPanic(definition.ReclaimPtlcMethodName,
			ptlcId, // entry id
		),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(0 * g.Zexp),
	}).Error(t, constants.ReclaimNotDue)
	z.InsertNewMomentum()

	// user 2 tries to unlock with wrong signature
	wrong_message := crypto.Hash(common.JoinBytes(ptlcId.Bytes(), g.User2.Address.Bytes(), []byte{0}))
	wrong_signature := g.User2.Sign(wrong_message)
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User2.Address,
		ToAddress: types.PtlcContract,
		Data: definition.ABIPtlc.PackMethodPanic(definition.UnlockPtlcMethodName,
			ptlcId,          // entry id
			wrong_signature, // signature
		),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(0 * g.Zexp),
	}).Error(t, constants.ErrInvalidPointSignature)
	z.InsertNewMomentum()
	z.InsertNewMomentum()

	// user 2 tries to unlock with wrong signature
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User2.Address,
		ToAddress: types.PtlcContract,
		Data: definition.ABIPtlc.PackMethodPanic(definition.UnlockPtlcMethodName,
			ptlcId,              // entry id
			wrong_signature[1:], // signature
		),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(0 * g.Zexp),
	}).Error(t, constants.ErrInvalidPointSignature)
	z.InsertNewMomentum()
	z.InsertNewMomentum()

	// user2 unlocks with correct signature
	mh := crypto.Hash(common.JoinBytes(ptlcId.Bytes(), g.User2.Address.Bytes()))
	signature := g.User2.Sign(mh)
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User2.Address,
		ToAddress: types.PtlcContract,
		Data: definition.ABIPtlc.PackMethodPanic(definition.UnlockPtlcMethodName,
			ptlcId,    // entry id
			signature, // signature
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

	z.ExpectBalance(types.PtlcContract, types.ZnnTokenStandard, 0*g.Zexp)
	z.ExpectBalance(types.PtlcContract, types.QsrTokenStandard, 0*g.Zexp)

}

func TestPtlc_proxy_unlock(t *testing.T) {
	z := mock.NewMockZenon(t)
	ptlcApi := embedded.NewPtlcApi(z)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).Equals(t, `
t=2001-09-09T01:46:50+0000 lvl=dbug msg=created module=embedded contract=spork spork="&{Id:d82f15026ad67abbc99786a9ed5b667ac578a78fb80df4ea573c22e727fd736a Name:spork-ptlc Description:activate spork for ptlc Activated:false EnforcementHeight:0}"
t=2001-09-09T01:47:00+0000 lvl=dbug msg=activated module=embedded contract=spork spork="&{Id:d82f15026ad67abbc99786a9ed5b667ac578a78fb80df4ea573c22e727fd736a Name:spork-ptlc Description:activate spork for ptlc Activated:true EnforcementHeight:9}"
t=2001-09-09T01:50:00+0000 lvl=dbug msg=created module=embedded contract=ptlc ptlcInfo="Id:6809e10e211036a33d43ce4a72b71a5389ac8050df1249edefd52b632ce45b79 TimeLocked:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenStandard:zts1znnxxxxxxxxxxxxx9z4ulx Amount:1000000000 ExpirationTime:1000000300 PointType:0 PointLock:tUJu3P7Drp25XP662lIjyFlFpvj8bWUpyC+0y5YTzXM= "
t=2001-09-09T01:50:20+0000 lvl=dbug msg="invalid unlock - invalid signature" module=embedded contract=ptlc id=6809e10e211036a33d43ce4a72b71a5389ac8050df1249edefd52b632ce45b79 address=z1qrs2lpccnsneglhnnfwvlsj0qncnxjnwlfmjac destination=z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx signature="HkdfAARMUbLniahv91th9Cc4FvnNvPdiVKHvS+arGJuU+gIcUvgbDrWEXgVxKRLgaWS0wGOwmJbTPtJXvZT+AA=="
t=2001-09-09T01:50:40+0000 lvl=dbug msg="invalid unlock - invalid signature" module=embedded contract=ptlc id=6809e10e211036a33d43ce4a72b71a5389ac8050df1249edefd52b632ce45b79 address=z1qrs2lpccnsneglhnnfwvlsj0qncnxjnwlfmjac destination=z1qrs2lpccnsneglhnnfwvlsj0qncnxjnwlfmjac signature="aFRIn613J+TaTP40Yzv9bk3eC2UPyc3PtIIp75yDnfbh+vQtm5ZOumAVNM6noBpHGjO6nFrAzHZ67Np9r8ArDA=="
t=2001-09-09T01:51:00+0000 lvl=dbug msg=unlocked module=embedded contract=ptlc ptlcInfo="Id:6809e10e211036a33d43ce4a72b71a5389ac8050df1249edefd52b632ce45b79 TimeLocked:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenStandard:zts1znnxxxxxxxxxxxxx9z4ulx Amount:1000000000 ExpirationTime:1000000300 PointType:0 PointLock:tUJu3P7Drp25XP662lIjyFlFpvj8bWUpyC+0y5YTzXM= " destination=z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx signature="aFRIn613J+TaTP40Yzv9bk3eC2UPyc3PtIIp75yDnfbh+vQtm5ZOumAVNM6noBpHGjO6nFrAzHZ67Np9r8ArDA=="
`)
	activatePtlc(z)

	// user 1 creates a ptlc for user 2
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.PtlcContract,
		Data: definition.ABIPtlc.PackMethodPanic(definition.CreatePtlcMethodName,
			int64(genesisTimestamp+300), // expiration time
			definition.PointTypeED25519, // point type
			g.User2.Public,              // point lock
		),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(10 * g.Zexp),
	}).Error(t, nil)
	z.InsertNewMomentum()
	z.InsertNewMomentum()

	ptlcId := types.HexToHashPanic("6809e10e211036a33d43ce4a72b71a5389ac8050df1249edefd52b632ce45b79")
	common.Json(ptlcApi.GetById(ptlcId)).Equals(t, `
{
	"id": "6809e10e211036a33d43ce4a72b71a5389ac8050df1249edefd52b632ce45b79",
	"timeLocked": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
	"tokenStandard": "zts1znnxxxxxxxxxxxxx9z4ulx",
	"amount": 1000000000,
	"expirationTime": 1000000300,
	"pointType": 0,
	"pointLock": "tUJu3P7Drp25XP662lIjyFlFpvj8bWUpyC+0y5YTzXM="
}
`)

	z.ExpectBalance(g.User1.Address, types.ZnnTokenStandard, 11990*g.Zexp)
	z.ExpectBalance(g.User1.Address, types.QsrTokenStandard, 120000*g.Zexp)

	z.ExpectBalance(g.User2.Address, types.ZnnTokenStandard, 8000*g.Zexp)
	z.ExpectBalance(g.User2.Address, types.QsrTokenStandard, 80000*g.Zexp)

	z.ExpectBalance(types.PtlcContract, types.ZnnTokenStandard, 10*g.Zexp)
	z.ExpectBalance(types.PtlcContract, types.QsrTokenStandard, 0*g.Zexp)

	unlock_message := crypto.Hash(common.JoinBytes(ptlcId.Bytes(), g.User2.Address.Bytes()))

	// user 3 tries to proxy unlock for user 2 with wrong signature
	wrong_signature := g.User3.Sign(unlock_message)
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User3.Address,
		ToAddress: types.PtlcContract,
		Data: definition.ABIPtlc.PackMethodPanic(definition.ProxyUnlockPtlcMethodName,
			ptlcId,          // entry id
			g.User2.Address, // destination
			wrong_signature, // signature
		),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(0 * g.Zexp),
	}).Error(t, constants.ErrInvalidPointSignature)
	z.InsertNewMomentum()
	z.InsertNewMomentum()

	right_signature := g.User2.Sign(unlock_message)

	// user 3 tries to proxy unlock for user 2 with wrong destination
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User3.Address,
		ToAddress: types.PtlcContract,
		Data: definition.ABIPtlc.PackMethodPanic(definition.ProxyUnlockPtlcMethodName,
			ptlcId,          // entry id
			g.User3.Address, // destination
			right_signature, // signature
		),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(0 * g.Zexp),
	}).Error(t, constants.ErrInvalidPointSignature)
	z.InsertNewMomentum()
	z.InsertNewMomentum()

	// user3 proxy unlocks for user 2 with correct signature
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User2.Address,
		ToAddress: types.PtlcContract,
		Data: definition.ABIPtlc.PackMethodPanic(definition.ProxyUnlockPtlcMethodName,
			ptlcId,          // entry id
			g.User2.Address, // destination
			right_signature, // signature
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

	z.ExpectBalance(types.PtlcContract, types.ZnnTokenStandard, 0*g.Zexp)
	z.ExpectBalance(types.PtlcContract, types.QsrTokenStandard, 0*g.Zexp)

}

func TestPtlc_reclaim(t *testing.T) {
	z := mock.NewMockZenon(t)
	ptlcApi := embedded.NewPtlcApi(z)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).Equals(t, `
t=2001-09-09T01:46:50+0000 lvl=dbug msg=created module=embedded contract=spork spork="&{Id:d82f15026ad67abbc99786a9ed5b667ac578a78fb80df4ea573c22e727fd736a Name:spork-ptlc Description:activate spork for ptlc Activated:false EnforcementHeight:0}"
t=2001-09-09T01:47:00+0000 lvl=dbug msg=activated module=embedded contract=spork spork="&{Id:d82f15026ad67abbc99786a9ed5b667ac578a78fb80df4ea573c22e727fd736a Name:spork-ptlc Description:activate spork for ptlc Activated:true EnforcementHeight:9}"
t=2001-09-09T01:50:00+0000 lvl=dbug msg=created module=embedded contract=ptlc ptlcInfo="Id:82eaa406d0762b558187eff923533242e0ebe801daa1aede897b6d2e3073eaad TimeLocked:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenStandard:zts1qsrxxxxxxxxxxxxxmrhjll Amount:1000000000 ExpirationTime:1000000300 PointType:0 PointLock:tUJu3P7Drp25XP662lIjyFlFpvj8bWUpyC+0y5YTzXM= "
t=2001-09-09T01:53:20+0000 lvl=dbug msg="invalid unlock - entry is expired" module=embedded contract=ptlc id=82eaa406d0762b558187eff923533242e0ebe801daa1aede897b6d2e3073eaad address=z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx time=1000000400 expiration-time=1000000300
t=2001-09-09T01:53:40+0000 lvl=dbug msg=reclaimed module=embedded contract=ptlc ptlcInfo="Id:82eaa406d0762b558187eff923533242e0ebe801daa1aede897b6d2e3073eaad TimeLocked:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenStandard:zts1qsrxxxxxxxxxxxxxmrhjll Amount:1000000000 ExpirationTime:1000000300 PointType:0 PointLock:tUJu3P7Drp25XP662lIjyFlFpvj8bWUpyC+0y5YTzXM= "
`)
	activatePtlc(z)

	// user 1 creates a ptlc for user 2
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.PtlcContract,
		Data: definition.ABIPtlc.PackMethodPanic(definition.CreatePtlcMethodName,
			int64(genesisTimestamp+300), // expiration time
			definition.PointTypeED25519, // point type
			g.User2.Public,              // point lock
		),
		TokenStandard: types.QsrTokenStandard,
		Amount:        big.NewInt(10 * g.Zexp),
	}).Error(t, nil)
	z.InsertNewMomentum()
	z.InsertMomentumsTo(40)

	ptlcId := types.HexToHashPanic("82eaa406d0762b558187eff923533242e0ebe801daa1aede897b6d2e3073eaad")
	common.Json(ptlcApi.GetById(ptlcId)).Equals(t, `
{
	"id": "82eaa406d0762b558187eff923533242e0ebe801daa1aede897b6d2e3073eaad",
	"timeLocked": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
	"tokenStandard": "zts1qsrxxxxxxxxxxxxxmrhjll",
	"amount": 1000000000,
	"expirationTime": 1000000300,
	"pointType": 0,
	"pointLock": "tUJu3P7Drp25XP662lIjyFlFpvj8bWUpyC+0y5YTzXM="
}
`)

	z.ExpectBalance(g.User1.Address, types.ZnnTokenStandard, 12000*g.Zexp)
	z.ExpectBalance(g.User1.Address, types.QsrTokenStandard, 119990*g.Zexp)

	z.ExpectBalance(g.User2.Address, types.ZnnTokenStandard, 8000*g.Zexp)
	z.ExpectBalance(g.User2.Address, types.QsrTokenStandard, 80000*g.Zexp)

	z.ExpectBalance(types.PtlcContract, types.ZnnTokenStandard, 0*g.Zexp)
	z.ExpectBalance(types.PtlcContract, types.QsrTokenStandard, 10*g.Zexp)

	// user2 tries to unlock expired with correct signature
	mh := crypto.Hash(common.JoinBytes(ptlcId.Bytes(), g.User2.Address.Bytes()))
	signature := g.User2.Sign(mh)
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User2.Address,
		ToAddress: types.PtlcContract,
		Data: definition.ABIPtlc.PackMethodPanic(definition.UnlockPtlcMethodName,
			ptlcId,    // entry id
			signature, // signature
		),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(0 * g.Zexp),
	}).Error(t, constants.ErrExpired)
	z.InsertNewMomentum()
	z.InsertNewMomentum()

	// user 1 reclaims expired
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.PtlcContract,
		Data: definition.ABIPtlc.PackMethodPanic(definition.ReclaimPtlcMethodName,
			ptlcId, // entry id
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

	z.ExpectBalance(types.PtlcContract, types.ZnnTokenStandard, 0*g.Zexp)
	z.ExpectBalance(types.PtlcContract, types.QsrTokenStandard, 0*g.Zexp)

}

func TestPtlc_create_expiration_time(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).Equals(t, `
t=2001-09-09T01:46:50+0000 lvl=dbug msg=created module=embedded contract=spork spork="&{Id:d82f15026ad67abbc99786a9ed5b667ac578a78fb80df4ea573c22e727fd736a Name:spork-ptlc Description:activate spork for ptlc Activated:false EnforcementHeight:0}"
t=2001-09-09T01:47:00+0000 lvl=dbug msg=activated module=embedded contract=spork spork="&{Id:d82f15026ad67abbc99786a9ed5b667ac578a78fb80df4ea573c22e727fd736a Name:spork-ptlc Description:activate spork for ptlc Activated:true EnforcementHeight:9}"
t=2001-09-09T01:51:40+0000 lvl=dbug msg="invalid create - cannot create already expired" module=embedded contract=ptlc address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz time=1000000300 expiration-time=1000000300
`)
	activatePtlc(z)

	z.InsertMomentumsTo(30)
	// Sun Sep 09 2001 01:51:40 GMT+0000
	// check the time in the logs

	// user 1 creates a ptlc for user 2
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.PtlcContract,
		Data: definition.ABIPtlc.PackMethodPanic(definition.CreatePtlcMethodName,
			int64(genesisTimestamp+300), // expiration time
			definition.PointTypeED25519, // point type
			g.User2.Public,              // point lock
		),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(10 * g.Zexp),
	}).Error(t, constants.ErrInvalidExpirationTime)
	z.InsertNewMomentum()
	z.InsertNewMomentum()
}

func TestPtlc_unlock_expiration_time(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).Equals(t, `
t=2001-09-09T01:46:50+0000 lvl=dbug msg=created module=embedded contract=spork spork="&{Id:d82f15026ad67abbc99786a9ed5b667ac578a78fb80df4ea573c22e727fd736a Name:spork-ptlc Description:activate spork for ptlc Activated:false EnforcementHeight:0}"
t=2001-09-09T01:47:00+0000 lvl=dbug msg=activated module=embedded contract=spork spork="&{Id:d82f15026ad67abbc99786a9ed5b667ac578a78fb80df4ea573c22e727fd736a Name:spork-ptlc Description:activate spork for ptlc Activated:true EnforcementHeight:9}"
t=2001-09-09T01:50:00+0000 lvl=dbug msg=created module=embedded contract=ptlc ptlcInfo="Id:6809e10e211036a33d43ce4a72b71a5389ac8050df1249edefd52b632ce45b79 TimeLocked:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenStandard:zts1znnxxxxxxxxxxxxx9z4ulx Amount:1000000000 ExpirationTime:1000000300 PointType:0 PointLock:tUJu3P7Drp25XP662lIjyFlFpvj8bWUpyC+0y5YTzXM= "
t=2001-09-09T01:51:40+0000 lvl=dbug msg="invalid unlock - entry is expired" module=embedded contract=ptlc id=6809e10e211036a33d43ce4a72b71a5389ac8050df1249edefd52b632ce45b79 address=z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx time=1000000300 expiration-time=1000000300
`)
	activatePtlc(z)

	// user 1 creates a ptlc for user 2
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.PtlcContract,
		Data: definition.ABIPtlc.PackMethodPanic(definition.CreatePtlcMethodName,
			int64(genesisTimestamp+300), // expiration time
			definition.PointTypeED25519, // point type
			g.User2.Public,              // point lock
		),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(10 * g.Zexp),
	}).Error(t, nil)
	z.InsertNewMomentum()
	z.InsertNewMomentum()

	ptlcId := types.HexToHashPanic("6809e10e211036a33d43ce4a72b71a5389ac8050df1249edefd52b632ce45b79")

	z.InsertMomentumsTo(30)
	// Sun Sep 09 2001 01:51:40 GMT+0000
	// check the time in the logs

	// user2 tries to unlock expired with correct preimage
	mh := crypto.Hash(common.JoinBytes(ptlcId.Bytes(), g.User2.Address.Bytes()))
	signature := g.User2.Sign(mh)
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User2.Address,
		ToAddress: types.PtlcContract,
		Data: definition.ABIPtlc.PackMethodPanic(definition.UnlockPtlcMethodName,
			ptlcId,    // entry id
			signature, // signature
		),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(0 * g.Zexp),
	}).Error(t, constants.ErrExpired)
	z.InsertNewMomentum()
	z.InsertNewMomentum()
}

func TestPtlc_reclaim_expiration_time(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).Equals(t, `
t=2001-09-09T01:46:50+0000 lvl=dbug msg=created module=embedded contract=spork spork="&{Id:d82f15026ad67abbc99786a9ed5b667ac578a78fb80df4ea573c22e727fd736a Name:spork-ptlc Description:activate spork for ptlc Activated:false EnforcementHeight:0}"
t=2001-09-09T01:47:00+0000 lvl=dbug msg=activated module=embedded contract=spork spork="&{Id:d82f15026ad67abbc99786a9ed5b667ac578a78fb80df4ea573c22e727fd736a Name:spork-ptlc Description:activate spork for ptlc Activated:true EnforcementHeight:9}"
t=2001-09-09T01:50:00+0000 lvl=dbug msg=created module=embedded contract=ptlc ptlcInfo="Id:6809e10e211036a33d43ce4a72b71a5389ac8050df1249edefd52b632ce45b79 TimeLocked:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenStandard:zts1znnxxxxxxxxxxxxx9z4ulx Amount:1000000000 ExpirationTime:1000000300 PointType:0 PointLock:tUJu3P7Drp25XP662lIjyFlFpvj8bWUpyC+0y5YTzXM= "
t=2001-09-09T01:51:40+0000 lvl=dbug msg=reclaimed module=embedded contract=ptlc ptlcInfo="Id:6809e10e211036a33d43ce4a72b71a5389ac8050df1249edefd52b632ce45b79 TimeLocked:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenStandard:zts1znnxxxxxxxxxxxxx9z4ulx Amount:1000000000 ExpirationTime:1000000300 PointType:0 PointLock:tUJu3P7Drp25XP662lIjyFlFpvj8bWUpyC+0y5YTzXM= "
`)
	activatePtlc(z)

	// user 1 creates a ptlc for user 2
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.PtlcContract,
		Data: definition.ABIPtlc.PackMethodPanic(definition.CreatePtlcMethodName,
			int64(genesisTimestamp+300), // expiration time
			definition.PointTypeED25519, // point type
			g.User2.Public,              // point lock
		),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(10 * g.Zexp),
	}).Error(t, nil)
	z.InsertNewMomentum()
	z.InsertNewMomentum()

	ptlcId := types.HexToHashPanic("6809e10e211036a33d43ce4a72b71a5389ac8050df1249edefd52b632ce45b79")

	z.InsertMomentumsTo(30)
	// Sun Sep 09 2001 01:51:40 GMT+0000
	// check the time in the logs

	// user 1 reclaims expired
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.PtlcContract,
		Data: definition.ABIPtlc.PackMethodPanic(definition.ReclaimPtlcMethodName,
			ptlcId, // entry id
		),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(0 * g.Zexp),
	}).Error(t, nil)
	z.InsertNewMomentum()
	z.InsertNewMomentum()
}

func TestPtlc_reclaim_access(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).Equals(t, `
t=2001-09-09T01:46:50+0000 lvl=dbug msg=created module=embedded contract=spork spork="&{Id:d82f15026ad67abbc99786a9ed5b667ac578a78fb80df4ea573c22e727fd736a Name:spork-ptlc Description:activate spork for ptlc Activated:false EnforcementHeight:0}"
t=2001-09-09T01:47:00+0000 lvl=dbug msg=activated module=embedded contract=spork spork="&{Id:d82f15026ad67abbc99786a9ed5b667ac578a78fb80df4ea573c22e727fd736a Name:spork-ptlc Description:activate spork for ptlc Activated:true EnforcementHeight:9}"
t=2001-09-09T01:50:00+0000 lvl=dbug msg=created module=embedded contract=ptlc ptlcInfo="Id:6809e10e211036a33d43ce4a72b71a5389ac8050df1249edefd52b632ce45b79 TimeLocked:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenStandard:zts1znnxxxxxxxxxxxxx9z4ulx Amount:1000000000 ExpirationTime:1000000300 PointType:0 PointLock:tUJu3P7Drp25XP662lIjyFlFpvj8bWUpyC+0y5YTzXM= "
t=2001-09-09T01:50:20+0000 lvl=dbug msg="invalid reclaim - permission denied" module=embedded contract=ptlc id=6809e10e211036a33d43ce4a72b71a5389ac8050df1249edefd52b632ce45b79 address=z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx
t=2001-09-09T01:50:30+0000 lvl=dbug msg="invalid reclaim - permission denied" module=embedded contract=ptlc id=6809e10e211036a33d43ce4a72b71a5389ac8050df1249edefd52b632ce45b79 address=z1qrs2lpccnsneglhnnfwvlsj0qncnxjnwlfmjac
t=2001-09-09T01:53:20+0000 lvl=dbug msg="invalid reclaim - permission denied" module=embedded contract=ptlc id=6809e10e211036a33d43ce4a72b71a5389ac8050df1249edefd52b632ce45b79 address=z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx
t=2001-09-09T01:53:30+0000 lvl=dbug msg="invalid reclaim - permission denied" module=embedded contract=ptlc id=6809e10e211036a33d43ce4a72b71a5389ac8050df1249edefd52b632ce45b79 address=z1qrs2lpccnsneglhnnfwvlsj0qncnxjnwlfmjac
t=2001-09-09T01:53:40+0000 lvl=dbug msg=reclaimed module=embedded contract=ptlc ptlcInfo="Id:6809e10e211036a33d43ce4a72b71a5389ac8050df1249edefd52b632ce45b79 TimeLocked:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenStandard:zts1znnxxxxxxxxxxxxx9z4ulx Amount:1000000000 ExpirationTime:1000000300 PointType:0 PointLock:tUJu3P7Drp25XP662lIjyFlFpvj8bWUpyC+0y5YTzXM= "
`)
	activatePtlc(z)

	// user 1 creates a ptlc for user 2
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.PtlcContract,
		Data: definition.ABIPtlc.PackMethodPanic(definition.CreatePtlcMethodName,
			int64(genesisTimestamp+300), // expiration time
			definition.PointTypeED25519, // point type
			g.User2.Public,              // point lock
		),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(10 * g.Zexp),
	}).Error(t, nil)
	z.InsertNewMomentum()
	z.InsertNewMomentum()

	ptlcId := types.HexToHashPanic("6809e10e211036a33d43ce4a72b71a5389ac8050df1249edefd52b632ce45b79")

	// user 2 tries to reclaim unexpired
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User2.Address,
		ToAddress: types.PtlcContract,
		Data: definition.ABIPtlc.PackMethodPanic(definition.ReclaimPtlcMethodName,
			ptlcId, // entry id
		),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(0 * g.Zexp),
	}).Error(t, constants.ErrPermissionDenied)
	z.InsertNewMomentum()

	// user 3 tries to reclaim unexpired
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User3.Address,
		ToAddress: types.PtlcContract,
		Data: definition.ABIPtlc.PackMethodPanic(definition.ReclaimPtlcMethodName,
			ptlcId, // entry id
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
		ToAddress: types.PtlcContract,
		Data: definition.ABIPtlc.PackMethodPanic(definition.ReclaimPtlcMethodName,
			ptlcId, // entry id
		),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(0 * g.Zexp),
	}).Error(t, constants.ErrPermissionDenied)
	z.InsertNewMomentum()

	// user 3 tries to reclaim expired
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User3.Address,
		ToAddress: types.PtlcContract,
		Data: definition.ABIPtlc.PackMethodPanic(definition.ReclaimPtlcMethodName,
			ptlcId, // entry id
		),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(0 * g.Zexp),
	}).Error(t, constants.ErrPermissionDenied)
	z.InsertNewMomentum()

	// user 1 reclaims expired
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.PtlcContract,
		Data: definition.ABIPtlc.PackMethodPanic(definition.ReclaimPtlcMethodName,
			ptlcId, // entry id
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

	z.ExpectBalance(types.PtlcContract, types.ZnnTokenStandard, 0*g.Zexp)
	z.ExpectBalance(types.PtlcContract, types.QsrTokenStandard, 0*g.Zexp)
}

func TestPtlc_nonexistent(t *testing.T) {
	z := mock.NewMockZenon(t)
	ptlcApi := embedded.NewPtlcApi(z)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).Equals(t, `
t=2001-09-09T01:46:50+0000 lvl=dbug msg=created module=embedded contract=spork spork="&{Id:d82f15026ad67abbc99786a9ed5b667ac578a78fb80df4ea573c22e727fd736a Name:spork-ptlc Description:activate spork for ptlc Activated:false EnforcementHeight:0}"
t=2001-09-09T01:47:00+0000 lvl=dbug msg=activated module=embedded contract=spork spork="&{Id:d82f15026ad67abbc99786a9ed5b667ac578a78fb80df4ea573c22e727fd736a Name:spork-ptlc Description:activate spork for ptlc Activated:true EnforcementHeight:9}"
t=2001-09-09T01:50:00+0000 lvl=dbug msg="invalid unlock - entry does not exist" module=embedded contract=ptlc id=7efdcca315f86cdb04e84113bfc5f003fa49c4b3f9b287cd3b4a08d8ccdf6ffc address=z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx
t=2001-09-09T01:50:10+0000 lvl=dbug msg="invalid reclaim - entry does not exist" module=embedded contract=ptlc id=7efdcca315f86cdb04e84113bfc5f003fa49c4b3f9b287cd3b4a08d8ccdf6ffc address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz
`)
	activatePtlc(z)

	nonexistentId := types.HexToHashPanic("7efdcca315f86cdb04e84113bfc5f003fa49c4b3f9b287cd3b4a08d8ccdf6ffc")

	// get ptlcinfo rpc nonexistent
	common.Json(ptlcApi.GetById(nonexistentId)).Error(t, constants.ErrDataNonExistent)

	// unlock nonexistent
	mh := crypto.Hash(common.JoinBytes(nonexistentId.Bytes(), g.User2.Address.Bytes()))
	signature := g.User2.Sign(mh)
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User2.Address,
		ToAddress: types.PtlcContract,
		Data: definition.ABIPtlc.PackMethodPanic(definition.UnlockPtlcMethodName,
			nonexistentId, // entry id
			signature,     // signature
		),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(0 * g.Zexp),
	}).Error(t, constants.ErrDataNonExistent)
	z.InsertNewMomentum()

	// reclaim nonexistent
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.PtlcContract,
		Data: definition.ABIPtlc.PackMethodPanic(definition.ReclaimPtlcMethodName,
			nonexistentId, // entry id
		),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(0 * g.Zexp),
	}).Error(t, constants.ErrDataNonExistent)
	z.InsertNewMomentum()
}

func TestPtlc_nonexistent_after_unlock(t *testing.T) {
	z := mock.NewMockZenon(t)
	ptlcApi := embedded.NewPtlcApi(z)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).Equals(t, `
t=2001-09-09T01:46:50+0000 lvl=dbug msg=created module=embedded contract=spork spork="&{Id:d82f15026ad67abbc99786a9ed5b667ac578a78fb80df4ea573c22e727fd736a Name:spork-ptlc Description:activate spork for ptlc Activated:false EnforcementHeight:0}"
t=2001-09-09T01:47:00+0000 lvl=dbug msg=activated module=embedded contract=spork spork="&{Id:d82f15026ad67abbc99786a9ed5b667ac578a78fb80df4ea573c22e727fd736a Name:spork-ptlc Description:activate spork for ptlc Activated:true EnforcementHeight:9}"
t=2001-09-09T01:50:00+0000 lvl=dbug msg=created module=embedded contract=ptlc ptlcInfo="Id:6809e10e211036a33d43ce4a72b71a5389ac8050df1249edefd52b632ce45b79 TimeLocked:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenStandard:zts1znnxxxxxxxxxxxxx9z4ulx Amount:1000000000 ExpirationTime:1000000300 PointType:0 PointLock:tUJu3P7Drp25XP662lIjyFlFpvj8bWUpyC+0y5YTzXM= "
t=2001-09-09T01:50:20+0000 lvl=dbug msg=unlocked module=embedded contract=ptlc ptlcInfo="Id:6809e10e211036a33d43ce4a72b71a5389ac8050df1249edefd52b632ce45b79 TimeLocked:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenStandard:zts1znnxxxxxxxxxxxxx9z4ulx Amount:1000000000 ExpirationTime:1000000300 PointType:0 PointLock:tUJu3P7Drp25XP662lIjyFlFpvj8bWUpyC+0y5YTzXM= " destination=z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx signature="aFRIn613J+TaTP40Yzv9bk3eC2UPyc3PtIIp75yDnfbh+vQtm5ZOumAVNM6noBpHGjO6nFrAzHZ67Np9r8ArDA=="
t=2001-09-09T01:50:40+0000 lvl=dbug msg="invalid unlock - entry does not exist" module=embedded contract=ptlc id=6809e10e211036a33d43ce4a72b71a5389ac8050df1249edefd52b632ce45b79 address=z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx
t=2001-09-09T01:50:50+0000 lvl=dbug msg="invalid reclaim - entry does not exist" module=embedded contract=ptlc id=6809e10e211036a33d43ce4a72b71a5389ac8050df1249edefd52b632ce45b79 address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz
`)
	activatePtlc(z)

	// user 1 creates a ptlc for user 2
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.PtlcContract,
		Data: definition.ABIPtlc.PackMethodPanic(definition.CreatePtlcMethodName,
			int64(genesisTimestamp+300), // expiration time
			definition.PointTypeED25519, // point type
			g.User2.Public,              // point lock
		),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(10 * g.Zexp),
	}).Error(t, nil)
	z.InsertNewMomentum()
	z.InsertNewMomentum()

	ptlcId := types.HexToHashPanic("6809e10e211036a33d43ce4a72b71a5389ac8050df1249edefd52b632ce45b79")

	// user2 unlocks with correct signature
	mh := crypto.Hash(common.JoinBytes(ptlcId.Bytes(), g.User2.Address.Bytes()))
	signature := g.User2.Sign(mh)
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User2.Address,
		ToAddress: types.PtlcContract,
		Data: definition.ABIPtlc.PackMethodPanic(definition.UnlockPtlcMethodName,
			ptlcId,    // entry id
			signature, // signature
		),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(0 * g.Zexp),
	}).Error(t, nil)
	z.InsertNewMomentum()
	z.InsertNewMomentum()

	// get ptlcinfo rpc nonexistent
	common.Json(ptlcApi.GetById(ptlcId)).Error(t, constants.ErrDataNonExistent)

	// unlock nonexistent
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User2.Address,
		ToAddress: types.PtlcContract,
		Data: definition.ABIPtlc.PackMethodPanic(definition.UnlockPtlcMethodName,
			ptlcId,    // entry id
			signature, // signature
		),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(0 * g.Zexp),
	}).Error(t, constants.ErrDataNonExistent)
	z.InsertNewMomentum()

	// reclaim nonexistent
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.PtlcContract,
		Data: definition.ABIPtlc.PackMethodPanic(definition.ReclaimPtlcMethodName,
			ptlcId, // entry id
		),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(0 * g.Zexp),
	}).Error(t, constants.ErrDataNonExistent)
	z.InsertNewMomentum()
}

func TestPtlc_nonexistent_after_reclaim(t *testing.T) {
	z := mock.NewMockZenon(t)
	ptlcApi := embedded.NewPtlcApi(z)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).Equals(t, `
t=2001-09-09T01:46:50+0000 lvl=dbug msg=created module=embedded contract=spork spork="&{Id:d82f15026ad67abbc99786a9ed5b667ac578a78fb80df4ea573c22e727fd736a Name:spork-ptlc Description:activate spork for ptlc Activated:false EnforcementHeight:0}"
t=2001-09-09T01:47:00+0000 lvl=dbug msg=activated module=embedded contract=spork spork="&{Id:d82f15026ad67abbc99786a9ed5b667ac578a78fb80df4ea573c22e727fd736a Name:spork-ptlc Description:activate spork for ptlc Activated:true EnforcementHeight:9}"
t=2001-09-09T01:50:00+0000 lvl=dbug msg=created module=embedded contract=ptlc ptlcInfo="Id:6809e10e211036a33d43ce4a72b71a5389ac8050df1249edefd52b632ce45b79 TimeLocked:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenStandard:zts1znnxxxxxxxxxxxxx9z4ulx Amount:1000000000 ExpirationTime:1000000300 PointType:0 PointLock:tUJu3P7Drp25XP662lIjyFlFpvj8bWUpyC+0y5YTzXM= "
t=2001-09-09T01:53:20+0000 lvl=dbug msg=reclaimed module=embedded contract=ptlc ptlcInfo="Id:6809e10e211036a33d43ce4a72b71a5389ac8050df1249edefd52b632ce45b79 TimeLocked:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenStandard:zts1znnxxxxxxxxxxxxx9z4ulx Amount:1000000000 ExpirationTime:1000000300 PointType:0 PointLock:tUJu3P7Drp25XP662lIjyFlFpvj8bWUpyC+0y5YTzXM= "
t=2001-09-09T01:53:30+0000 lvl=dbug msg="invalid unlock - entry does not exist" module=embedded contract=ptlc id=6809e10e211036a33d43ce4a72b71a5389ac8050df1249edefd52b632ce45b79 address=z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx
t=2001-09-09T01:53:40+0000 lvl=dbug msg="invalid reclaim - entry does not exist" module=embedded contract=ptlc id=6809e10e211036a33d43ce4a72b71a5389ac8050df1249edefd52b632ce45b79 address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz
`)
	activatePtlc(z)

	// user 1 creates a ptlc for user 2
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.PtlcContract,
		Data: definition.ABIPtlc.PackMethodPanic(definition.CreatePtlcMethodName,
			int64(genesisTimestamp+300), // expiration time
			definition.PointTypeED25519, // point type
			g.User2.Public,              // point lock
		),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(10 * g.Zexp),
	}).Error(t, nil)
	z.InsertNewMomentum()
	z.InsertMomentumsTo(40)

	ptlcId := types.HexToHashPanic("6809e10e211036a33d43ce4a72b71a5389ac8050df1249edefd52b632ce45b79")

	// user1 reclaims
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.PtlcContract,
		Data: definition.ABIPtlc.PackMethodPanic(definition.ReclaimPtlcMethodName,
			ptlcId, // entry id
		),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(0 * g.Zexp),
	}).Error(t, nil)
	z.InsertNewMomentum()

	// get ptlcinfo rpc nonexistent
	common.Json(ptlcApi.GetById(ptlcId)).Error(t, constants.ErrDataNonExistent)

	// unlock nonexistent
	mh := crypto.Hash(common.JoinBytes(ptlcId.Bytes(), g.User2.Address.Bytes()))
	signature := g.User2.Sign(mh)
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User2.Address,
		ToAddress: types.PtlcContract,
		Data: definition.ABIPtlc.PackMethodPanic(definition.UnlockPtlcMethodName,
			ptlcId,    // entry id
			signature, // signature
		),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(0 * g.Zexp),
	}).Error(t, constants.ErrDataNonExistent)
	z.InsertNewMomentum()

	// reclaim nonexistent
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.PtlcContract,
		Data: definition.ABIPtlc.PackMethodPanic(definition.ReclaimPtlcMethodName,
			ptlcId, // entry id
		),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(0 * g.Zexp),
	}).Error(t, constants.ErrDataNonExistent)
	z.InsertNewMomentum()
}

func TestPtlc_create_expired(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).Equals(t, `
t=2001-09-09T01:46:50+0000 lvl=dbug msg=created module=embedded contract=spork spork="&{Id:d82f15026ad67abbc99786a9ed5b667ac578a78fb80df4ea573c22e727fd736a Name:spork-ptlc Description:activate spork for ptlc Activated:false EnforcementHeight:0}"
t=2001-09-09T01:47:00+0000 lvl=dbug msg=activated module=embedded contract=spork spork="&{Id:d82f15026ad67abbc99786a9ed5b667ac578a78fb80df4ea573c22e727fd736a Name:spork-ptlc Description:activate spork for ptlc Activated:true EnforcementHeight:9}"
t=2001-09-09T01:50:00+0000 lvl=dbug msg="invalid create - cannot create already expired" module=embedded contract=ptlc address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz time=1000000200 expiration-time=999999700
`)
	activatePtlc(z)

	// user tries to create expired ptlc
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.PtlcContract,
		Data: definition.ABIPtlc.PackMethodPanic(definition.CreatePtlcMethodName,
			int64(genesisTimestamp-300), // expiration time
			definition.PointTypeED25519, // point type
			g.User2.Public,              // point lock
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

	z.ExpectBalance(types.PtlcContract, types.ZnnTokenStandard, 0*g.Zexp)
	z.ExpectBalance(types.PtlcContract, types.QsrTokenStandard, 0*g.Zexp)
}

// BIP340 Testing

func TestPtlc_unlockBIP340(t *testing.T) {
	z := mock.NewMockZenon(t)
	ptlcApi := embedded.NewPtlcApi(z)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).Equals(t, `
t=2001-09-09T01:46:50+0000 lvl=dbug msg=created module=embedded contract=spork spork="&{Id:d82f15026ad67abbc99786a9ed5b667ac578a78fb80df4ea573c22e727fd736a Name:spork-ptlc Description:activate spork for ptlc Activated:false EnforcementHeight:0}"
t=2001-09-09T01:47:00+0000 lvl=dbug msg=activated module=embedded contract=spork spork="&{Id:d82f15026ad67abbc99786a9ed5b667ac578a78fb80df4ea573c22e727fd736a Name:spork-ptlc Description:activate spork for ptlc Activated:true EnforcementHeight:9}"
t=2001-09-09T01:50:00+0000 lvl=dbug msg=created module=embedded contract=ptlc ptlcInfo="Id:80a763df5c1a41bcd03da24cad3b1b325f2fc5d125d4e3b5fa2d1b48d891bea6 TimeLocked:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenStandard:zts1znnxxxxxxxxxxxxx9z4ulx Amount:1000000000 ExpirationTime:1000000300 PointType:1 PointLock:fiG8+7m7odpA43OSLYoUwZ0RvTtY6wwnAPKWVeOJ5ww= "
t=2001-09-09T01:50:20+0000 lvl=dbug msg="invalid unlock - invalid signature" module=embedded contract=ptlc id=80a763df5c1a41bcd03da24cad3b1b325f2fc5d125d4e3b5fa2d1b48d891bea6 address=z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx destination=z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx signature="dslb7obOQZ3Yuszoa6scsyth0x8djzQL1vw+SNymX2zUrVyWY6iLhKP8nsEjHEkMBY6n/rU1eEZBrJAvgcOyDg=="
t=2001-09-09T01:50:30+0000 lvl=dbug msg="invalid unlock - invalid signature" module=embedded contract=ptlc id=80a763df5c1a41bcd03da24cad3b1b325f2fc5d125d4e3b5fa2d1b48d891bea6 address=z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx destination=z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx signature="dx3GeAjIn5x7UL7jTaXWzsYZ/LiAMGQbuKtAsPmnsx8xdkiOlMKFt/q11b8nvwq4ockTyFMhb/Bw5seERuxc+A=="
t=2001-09-09T01:50:40+0000 lvl=dbug msg=unlocked module=embedded contract=ptlc ptlcInfo="Id:80a763df5c1a41bcd03da24cad3b1b325f2fc5d125d4e3b5fa2d1b48d891bea6 TimeLocked:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenStandard:zts1znnxxxxxxxxxxxxx9z4ulx Amount:1000000000 ExpirationTime:1000000300 PointType:1 PointLock:fiG8+7m7odpA43OSLYoUwZ0RvTtY6wwnAPKWVeOJ5ww= " destination=z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx signature="VvcqV5Nm0q1HpfZU2mY0+R5IOP7mHTAp5hLWwIE4iby2IFgCdPFIsPNV2jet9Ypa3qUJbLwiyivPDx+dAvMhEw=="
`)
	activatePtlc(z)

	prv1, _ := btcec.PrivKeyFromBytes(g.Secp1PrvKey)

	prv2, pub2 := btcec.PrivKeyFromBytes(g.Secp2PrvKey)
	pub2bip340 := schnorr.SerializePubKey(pub2)

	// user 1 creates a ptlc for user 2 using BIP340 point type
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.PtlcContract,
		Data: definition.ABIPtlc.PackMethodPanic(definition.CreatePtlcMethodName,
			int64(genesisTimestamp+300), // expiration time
			definition.PointTypeBIP340,  // point type
			pub2bip340,                  // point lock
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

	z.ExpectBalance(types.PtlcContract, types.ZnnTokenStandard, 10*g.Zexp)
	z.ExpectBalance(types.PtlcContract, types.QsrTokenStandard, 0*g.Zexp)

	ptlcId := types.HexToHashPanic("80a763df5c1a41bcd03da24cad3b1b325f2fc5d125d4e3b5fa2d1b48d891bea6")

	common.Json(ptlcApi.GetById(ptlcId)).Equals(t, `
{
	"id": "80a763df5c1a41bcd03da24cad3b1b325f2fc5d125d4e3b5fa2d1b48d891bea6",
	"timeLocked": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
	"tokenStandard": "zts1znnxxxxxxxxxxxxx9z4ulx",
	"amount": 1000000000,
	"expirationTime": 1000000300,
	"pointType": 1,
	"pointLock": "fiG8+7m7odpA43OSLYoUwZ0RvTtY6wwnAPKWVeOJ5ww="
}
`)
	mh := crypto.Hash(common.JoinBytes(ptlcId.Bytes(), g.User2.Address.Bytes()))

	// user 2 tries to unlock with wrong signature type
	wrong_signature := g.User2.Sign(mh)
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User2.Address,
		ToAddress: types.PtlcContract,
		Data: definition.ABIPtlc.PackMethodPanic(definition.UnlockPtlcMethodName,
			ptlcId,          // entry id
			wrong_signature, // signature
		),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(0 * g.Zexp),
	}).Error(t, constants.ErrInvalidPointSignature)
	z.InsertNewMomentum()

	// user 2 tries to unlock with wrong signature
	wrong_signature2, _ := schnorr.Sign(prv1, mh)
	ws2 := wrong_signature2.Serialize()
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User2.Address,
		ToAddress: types.PtlcContract,
		Data: definition.ABIPtlc.PackMethodPanic(definition.UnlockPtlcMethodName,
			ptlcId, // entry id
			ws2,    // signature
		),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(0 * g.Zexp),
	}).Error(t, constants.ErrInvalidPointSignature)
	z.InsertNewMomentum()

	// user2 unlocks with correct preimage
	signature, _ := schnorr.Sign(prv2, mh)
	sig := signature.Serialize()
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User2.Address,
		ToAddress: types.PtlcContract,
		Data: definition.ABIPtlc.PackMethodPanic(definition.UnlockPtlcMethodName,
			ptlcId, // entry id
			sig,    // signature
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

	z.ExpectBalance(types.PtlcContract, types.ZnnTokenStandard, 0*g.Zexp)
	z.ExpectBalance(types.PtlcContract, types.QsrTokenStandard, 0*g.Zexp)

}
