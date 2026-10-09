package tests

import (
	"bytes"
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

func activatePtlc(t *testing.T, z mock.MockZenon) {
	t.Helper()

	oldSporkId := types.PtlcSpork.SporkId
	oldImplemented := types.ImplementedSporksMap[oldSporkId]
	var activatedSporkId types.Hash
	t.Cleanup(func() {
		if oldImplemented {
			types.ImplementedSporksMap[oldSporkId] = true
		} else {
			delete(types.ImplementedSporksMap, oldSporkId)
		}
		if activatedSporkId != oldSporkId {
			delete(types.ImplementedSporksMap, activatedSporkId)
		}
		types.PtlcSpork.SporkId = oldSporkId
	})

	sporkAPI := embedded.NewSporkApi(z)
	z.InsertSendBlock(&nom.AccountBlock{
		Address:   g.Spork.Address,
		ToAddress: types.SporkContract,
		Data: definition.ABISpork.PackMethodPanic(definition.SporkCreateMethodName,
			"spork-ptlc",
			"activate spork for ptlc",
		),
	}, nil, mock.SkipVmChanges)
	z.InsertNewMomentum()

	sporkList, _ := sporkAPI.GetAll(0, 10)
	id := sporkList.List[0].Id

	z.InsertSendBlock(&nom.AccountBlock{
		Address:   g.Spork.Address,
		ToAddress: types.SporkContract,
		Data: definition.ABISpork.PackMethodPanic(definition.SporkActivateMethodName,
			id,
		),
	}, nil, mock.SkipVmChanges)
	z.InsertNewMomentum()
	types.PtlcSpork.SporkId = id
	types.ImplementedSporksMap[id] = true
	activatedSporkId = id
	z.InsertMomentumsTo(20)
}

func ptlcUnlockMessage(z mock.MockZenon, pointType uint8, id types.Hash, destination types.Address) []byte {
	return definition.GetPtlcUnlockMessage(z.Chain().ChainIdentifier(), pointType, id, destination)
}

func ptlcUnlockMessageForContract(z mock.MockZenon, contract types.Address, pointType uint8, id types.Hash, destination types.Address) []byte {
	return crypto.Hash(common.JoinBytes(
		[]byte(definition.PtlcUnlockMessageDomain),
		common.Uint64ToBytes(z.Chain().ChainIdentifier()),
		contract.Bytes(),
		[]byte{pointType},
		id.Bytes(),
		destination.Bytes(),
	))
}

func createBIP340Ptlc(t *testing.T, z mock.MockZenon, pointLock []byte, expirationTime int64) types.Hash {
	t.Helper()

	createBlock := z.InsertSendBlock(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.PtlcContract,
		Data: definition.ABIPtlc.PackMethodPanic(definition.CreatePtlcMethodName,
			expirationTime,
			definition.PointTypeBIP340,
			pointLock,
			types.ZeroAddress,
		),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(10 * g.Zexp),
	}, nil, mock.SkipVmChanges)
	z.InsertNewMomentum()

	return createBlock.Hash
}

func createED25519Ptlc(t *testing.T, z mock.MockZenon, pointLock []byte, expirationTime int64, token types.ZenonTokenStandard, amount *big.Int) types.Hash {
	t.Helper()

	createBlock := z.InsertSendBlock(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.PtlcContract,
		Data: definition.ABIPtlc.PackMethodPanic(definition.CreatePtlcMethodName,
			expirationTime,
			definition.PointTypeED25519,
			pointLock,
			types.ZeroAddress,
		),
		TokenStandard: token,
		Amount:        amount,
	}, nil, mock.SkipVmChanges)
	z.InsertNewMomentum()

	return createBlock.Hash
}

func signBIP340Unlock(t *testing.T, z mock.MockZenon, privateKey *btcec.PrivateKey, id types.Hash, destination types.Address) []byte {
	t.Helper()

	signature, err := schnorr.Sign(privateKey, ptlcUnlockMessage(z, definition.PointTypeBIP340, id, destination))
	common.FailIfErr(t, err)
	return signature.Serialize()
}

func TestPtlc_spork_gating(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()

	z.InsertSendBlock(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.PtlcContract,
		Data: definition.ABIPtlc.PackMethodPanic(definition.CreatePtlcMethodName,
			int64(genesisTimestamp+300),
			definition.PointTypeED25519,
			g.User2.Public,
			types.ZeroAddress,
		),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(10 * g.Zexp),
	}, constants.ErrContractDoesntExist, mock.NoVmChanges)

	activatePtlc(t, z)

	z.InsertSendBlock(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.PtlcContract,
		Data: definition.ABIPtlc.PackMethodPanic(definition.CreatePtlcMethodName,
			int64(genesisTimestamp+300),
			definition.PointTypeED25519,
			g.User2.Public,
			types.ZeroAddress,
		),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(10 * g.Zexp),
	}, nil, mock.SkipVmChanges)
	z.InsertNewMomentum()

	lock := crypto.Hash(preimageZ)
	z.InsertSendBlock(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.HtlcContract,
		Data: definition.ABIHtlc.PackMethodPanic(definition.CreateHtlcMethodName,
			g.User2.Address,
			int64(genesisTimestamp+300),
			uint8(definition.HashTypeSHA3),
			uint8(32),
			lock,
		),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(10 * g.Zexp),
	}, constants.ErrContractDoesntExist, mock.NoVmChanges)
	z.InsertNewMomentum()
}

func TestPtlc_zero(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).Equals(t, `
t=2001-09-09T01:46:50+0000 lvl=dbug msg=created module=embedded contract=spork spork="&{Id:d82f15026ad67abbc99786a9ed5b667ac578a78fb80df4ea573c22e727fd736a Name:spork-ptlc Description:activate spork for ptlc Activated:false EnforcementHeight:0}"
t=2001-09-09T01:47:00+0000 lvl=dbug msg=activated module=embedded contract=spork spork="&{Id:d82f15026ad67abbc99786a9ed5b667ac578a78fb80df4ea573c22e727fd736a Name:spork-ptlc Description:activate spork for ptlc Activated:true EnforcementHeight:9}"
t=2001-09-09T01:49:50+0000 lvl=dbug msg="invalid create - amount must be positive" module=embedded contract=ptlc address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz
`)
	activatePtlc(t, z)

	z.InsertSendBlock(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.PtlcContract,
		Data: definition.ABIPtlc.PackMethodPanic(definition.CreatePtlcMethodName,
			int64(genesisTimestamp+300),
			definition.PointTypeED25519,
			g.User2.Public,
			types.ZeroAddress,
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
	activatePtlc(t, z)

	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.PtlcContract,
		Data: definition.ABIPtlc.PackMethodPanic(definition.CreatePtlcMethodName,
			int64(genesisTimestamp+300),
			definition.PointTypeED25519,
			g.User2.Public,
			types.ZeroAddress,
		),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(10 * g.Zexp),
	}).Error(t, nil)
	z.InsertNewMomentum()
	z.InsertNewMomentum()

	ptlcId := types.HexToHashPanic("b228343cbc272f38186bb6bf8fcbb49843194d0ef48df2d652561b3fe9bb62da")
	common.Json(ptlcApi.GetById(ptlcId)).Equals(t, `
{
	"id": "b228343cbc272f38186bb6bf8fcbb49843194d0ef48df2d652561b3fe9bb62da",
	"timeLocked": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
	"tokenStandard": "zts1znnxxxxxxxxxxxxx9z4ulx",
	"amount": "1000000000",
	"expirationTime": 1000000300,
	"pointType": 0,
	"pointLock": "tUJu3P7Drp25XP662lIjyFlFpvj8bWUpyC+0y5YTzXM=",
	"destination": "z1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqsggv2f"
}
`)

	z.ExpectBalance(g.User1.Address, types.ZnnTokenStandard, 11990*g.Zexp)
	z.ExpectBalance(g.User1.Address, types.QsrTokenStandard, 120000*g.Zexp)

	z.ExpectBalance(g.User2.Address, types.ZnnTokenStandard, 8000*g.Zexp)
	z.ExpectBalance(g.User2.Address, types.QsrTokenStandard, 80000*g.Zexp)

	z.ExpectBalance(types.PtlcContract, types.ZnnTokenStandard, 10*g.Zexp)
	z.ExpectBalance(types.PtlcContract, types.QsrTokenStandard, 0*g.Zexp)

	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.PtlcContract,
		Data: definition.ABIPtlc.PackMethodPanic(definition.ReclaimPtlcMethodName,
			ptlcId,
		),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(0 * g.Zexp),
	}).Error(t, constants.ReclaimNotDue)
	z.InsertNewMomentum()

	wrong_message := ptlcUnlockMessage(z, definition.PointTypeED25519, ptlcId, g.User2.Address)
	wrong_message[0] ^= 1
	wrong_signature := g.User2.Sign(wrong_message)
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User2.Address,
		ToAddress: types.PtlcContract,
		Data: definition.ABIPtlc.PackMethodPanic(definition.UnlockPtlcMethodName,
			ptlcId,
			wrong_signature,
		),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(0 * g.Zexp),
	}).Error(t, constants.ErrInvalidPointSignature)
	z.InsertNewMomentum()
	z.InsertNewMomentum()

	unlockPtlcRefusedAtSend(t, z, g.User2, ptlcId, wrong_signature[1:])

	old_message := crypto.Hash(common.JoinBytes(ptlcId.Bytes(), g.User2.Address.Bytes()))
	old_signature := g.User2.Sign(old_message)
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User2.Address,
		ToAddress: types.PtlcContract,
		Data: definition.ABIPtlc.PackMethodPanic(definition.UnlockPtlcMethodName,
			ptlcId,
			old_signature,
		),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(0 * g.Zexp),
	}).Error(t, constants.ErrInvalidPointSignature)
	z.InsertNewMomentum()
	z.InsertNewMomentum()

	mh := ptlcUnlockMessage(z, definition.PointTypeED25519, ptlcId, g.User2.Address)
	signature := g.User2.Sign(mh)
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User2.Address,
		ToAddress: types.PtlcContract,
		Data: definition.ABIPtlc.PackMethodPanic(definition.UnlockPtlcMethodName,
			ptlcId,
			signature,
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
	activatePtlc(t, z)

	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.PtlcContract,
		Data: definition.ABIPtlc.PackMethodPanic(definition.CreatePtlcMethodName,
			int64(genesisTimestamp+300),
			definition.PointTypeED25519,
			g.User2.Public,
			types.ZeroAddress,
		),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(10 * g.Zexp),
	}).Error(t, nil)
	z.InsertNewMomentum()
	z.InsertNewMomentum()

	ptlcId := types.HexToHashPanic("b228343cbc272f38186bb6bf8fcbb49843194d0ef48df2d652561b3fe9bb62da")
	common.Json(ptlcApi.GetById(ptlcId)).Equals(t, `
{
	"id": "b228343cbc272f38186bb6bf8fcbb49843194d0ef48df2d652561b3fe9bb62da",
	"timeLocked": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
	"tokenStandard": "zts1znnxxxxxxxxxxxxx9z4ulx",
	"amount": "1000000000",
	"expirationTime": 1000000300,
	"pointType": 0,
	"pointLock": "tUJu3P7Drp25XP662lIjyFlFpvj8bWUpyC+0y5YTzXM=",
	"destination": "z1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqsggv2f"
}
`)

	z.ExpectBalance(g.User1.Address, types.ZnnTokenStandard, 11990*g.Zexp)
	z.ExpectBalance(g.User1.Address, types.QsrTokenStandard, 120000*g.Zexp)

	z.ExpectBalance(g.User2.Address, types.ZnnTokenStandard, 8000*g.Zexp)
	z.ExpectBalance(g.User2.Address, types.QsrTokenStandard, 80000*g.Zexp)

	z.ExpectBalance(types.PtlcContract, types.ZnnTokenStandard, 10*g.Zexp)
	z.ExpectBalance(types.PtlcContract, types.QsrTokenStandard, 0*g.Zexp)

	unlock_message := ptlcUnlockMessage(z, definition.PointTypeED25519, ptlcId, g.User2.Address)

	wrong_signature := g.User3.Sign(unlock_message)
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User3.Address,
		ToAddress: types.PtlcContract,
		Data: definition.ABIPtlc.PackMethodPanic(definition.ProxyUnlockPtlcMethodName,
			ptlcId,
			g.User2.Address,
			wrong_signature,
		),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(0 * g.Zexp),
	}).Error(t, constants.ErrInvalidPointSignature)
	z.InsertNewMomentum()
	z.InsertNewMomentum()

	right_signature := g.User2.Sign(unlock_message)

	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User3.Address,
		ToAddress: types.PtlcContract,
		Data: definition.ABIPtlc.PackMethodPanic(definition.ProxyUnlockPtlcMethodName,
			ptlcId,
			g.User3.Address,
			right_signature,
		),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(0 * g.Zexp),
	}).Error(t, constants.ErrInvalidPointSignature)
	z.InsertNewMomentum()
	z.InsertNewMomentum()

	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User3.Address,
		ToAddress: types.PtlcContract,
		Data: definition.ABIPtlc.PackMethodPanic(definition.ProxyUnlockPtlcMethodName,
			ptlcId,
			g.User2.Address,
			right_signature,
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

func TestPtlc_wrongChainSignature(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()
	activatePtlc(t, z)

	createBlock := z.InsertSendBlock(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.PtlcContract,
		Data: definition.ABIPtlc.PackMethodPanic(definition.CreatePtlcMethodName,
			int64(genesisTimestamp+300),
			definition.PointTypeED25519,
			g.User2.Public,
			types.ZeroAddress,
		),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(10 * g.Zexp),
	}, nil, mock.SkipVmChanges)
	z.InsertNewMomentum()

	ptlcId := createBlock.Hash
	wrongChainMessage := definition.GetPtlcUnlockMessage(z.Chain().ChainIdentifier()+1, definition.PointTypeED25519, ptlcId, g.User2.Address)
	wrongChainSignature := g.User2.Sign(wrongChainMessage)
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User2.Address,
		ToAddress: types.PtlcContract,
		Data: definition.ABIPtlc.PackMethodPanic(definition.UnlockPtlcMethodName,
			ptlcId,
			wrongChainSignature,
		),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(0),
	}).Error(t, constants.ErrInvalidPointSignature)
	z.InsertNewMomentum()

	z.ExpectBalance(types.PtlcContract, types.ZnnTokenStandard, 10*g.Zexp)
}

func TestPtlc_wrongContractSignature(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()
	activatePtlc(t, z)

	createBlock := z.InsertSendBlock(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.PtlcContract,
		Data: definition.ABIPtlc.PackMethodPanic(definition.CreatePtlcMethodName,
			int64(genesisTimestamp+300),
			definition.PointTypeED25519,
			g.User2.Public,
			types.ZeroAddress,
		),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(10 * g.Zexp),
	}, nil, mock.SkipVmChanges)
	z.InsertNewMomentum()

	ptlcId := createBlock.Hash
	wrongContractMessage := ptlcUnlockMessageForContract(z, types.HtlcContract, definition.PointTypeED25519, ptlcId, g.User2.Address)
	wrongContractSignature := g.User2.Sign(wrongContractMessage)
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User2.Address,
		ToAddress: types.PtlcContract,
		Data: definition.ABIPtlc.PackMethodPanic(definition.UnlockPtlcMethodName,
			ptlcId,
			wrongContractSignature,
		),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(0),
	}).Error(t, constants.ErrInvalidPointSignature)
	z.InsertNewMomentum()

	z.ExpectBalance(types.PtlcContract, types.ZnnTokenStandard, 10*g.Zexp)
}

func TestPtlc_reclaim(t *testing.T) {
	z := mock.NewMockZenon(t)
	ptlcApi := embedded.NewPtlcApi(z)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).Equals(t, `
t=2001-09-09T01:46:50+0000 lvl=dbug msg=created module=embedded contract=spork spork="&{Id:d82f15026ad67abbc99786a9ed5b667ac578a78fb80df4ea573c22e727fd736a Name:spork-ptlc Description:activate spork for ptlc Activated:false EnforcementHeight:0}"
t=2001-09-09T01:47:00+0000 lvl=dbug msg=activated module=embedded contract=spork spork="&{Id:d82f15026ad67abbc99786a9ed5b667ac578a78fb80df4ea573c22e727fd736a Name:spork-ptlc Description:activate spork for ptlc Activated:true EnforcementHeight:9}"
t=2001-09-09T01:50:00+0000 lvl=dbug msg=created module=embedded contract=ptlc ptlcInfo="Id:a699ce2b0f7438c97df714b663cc66cf38fa59ec16cc6e8113c18104b93095d7 TimeLocked:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenStandard:zts1qsrxxxxxxxxxxxxxmrhjll Amount:1000000000 ExpirationTime:1000000300 PointType:0 PointLock:tUJu3P7Drp25XP662lIjyFlFpvj8bWUpyC+0y5YTzXM= Destination:z1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqsggv2f "
t=2001-09-09T01:53:20+0000 lvl=dbug msg="invalid unlock - entry is expired" module=embedded contract=ptlc id=a699ce2b0f7438c97df714b663cc66cf38fa59ec16cc6e8113c18104b93095d7 address=z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx time=1000000400 expiration-time=1000000300
t=2001-09-09T01:53:40+0000 lvl=dbug msg=reclaimed module=embedded contract=ptlc ptlcInfo="Id:a699ce2b0f7438c97df714b663cc66cf38fa59ec16cc6e8113c18104b93095d7 TimeLocked:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenStandard:zts1qsrxxxxxxxxxxxxxmrhjll Amount:1000000000 ExpirationTime:1000000300 PointType:0 PointLock:tUJu3P7Drp25XP662lIjyFlFpvj8bWUpyC+0y5YTzXM= Destination:z1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqsggv2f "
`)
	activatePtlc(t, z)

	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.PtlcContract,
		Data: definition.ABIPtlc.PackMethodPanic(definition.CreatePtlcMethodName,
			int64(genesisTimestamp+300),
			definition.PointTypeED25519,
			g.User2.Public,
			types.ZeroAddress,
		),
		TokenStandard: types.QsrTokenStandard,
		Amount:        big.NewInt(10 * g.Zexp),
	}).Error(t, nil)
	z.InsertNewMomentum()
	z.InsertMomentumsTo(40)

	ptlcId := types.HexToHashPanic("a699ce2b0f7438c97df714b663cc66cf38fa59ec16cc6e8113c18104b93095d7")
	common.Json(ptlcApi.GetById(ptlcId)).Equals(t, `
{
	"id": "a699ce2b0f7438c97df714b663cc66cf38fa59ec16cc6e8113c18104b93095d7",
	"timeLocked": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
	"tokenStandard": "zts1qsrxxxxxxxxxxxxxmrhjll",
	"amount": "1000000000",
	"expirationTime": 1000000300,
	"pointType": 0,
	"pointLock": "tUJu3P7Drp25XP662lIjyFlFpvj8bWUpyC+0y5YTzXM=",
	"destination": "z1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqsggv2f"
}
`)

	z.ExpectBalance(g.User1.Address, types.ZnnTokenStandard, 12000*g.Zexp)
	z.ExpectBalance(g.User1.Address, types.QsrTokenStandard, 119990*g.Zexp)

	z.ExpectBalance(g.User2.Address, types.ZnnTokenStandard, 8000*g.Zexp)
	z.ExpectBalance(g.User2.Address, types.QsrTokenStandard, 80000*g.Zexp)

	z.ExpectBalance(types.PtlcContract, types.ZnnTokenStandard, 0*g.Zexp)
	z.ExpectBalance(types.PtlcContract, types.QsrTokenStandard, 10*g.Zexp)

	mh := ptlcUnlockMessage(z, definition.PointTypeED25519, ptlcId, g.User2.Address)
	signature := g.User2.Sign(mh)
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User2.Address,
		ToAddress: types.PtlcContract,
		Data: definition.ABIPtlc.PackMethodPanic(definition.UnlockPtlcMethodName,
			ptlcId,
			signature,
		),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(0 * g.Zexp),
	}).Error(t, constants.ErrExpired)
	z.InsertNewMomentum()
	z.InsertNewMomentum()

	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.PtlcContract,
		Data: definition.ABIPtlc.PackMethodPanic(definition.ReclaimPtlcMethodName,
			ptlcId,
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
	activatePtlc(t, z)

	z.InsertMomentumsTo(30)

	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.PtlcContract,
		Data: definition.ABIPtlc.PackMethodPanic(definition.CreatePtlcMethodName,
			int64(genesisTimestamp+300),
			definition.PointTypeED25519,
			g.User2.Public,
			types.ZeroAddress,
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
t=2001-09-09T01:50:00+0000 lvl=dbug msg=created module=embedded contract=ptlc ptlcInfo="Id:b228343cbc272f38186bb6bf8fcbb49843194d0ef48df2d652561b3fe9bb62da TimeLocked:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenStandard:zts1znnxxxxxxxxxxxxx9z4ulx Amount:1000000000 ExpirationTime:1000000300 PointType:0 PointLock:tUJu3P7Drp25XP662lIjyFlFpvj8bWUpyC+0y5YTzXM= Destination:z1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqsggv2f "
t=2001-09-09T01:51:40+0000 lvl=dbug msg="invalid unlock - entry is expired" module=embedded contract=ptlc id=b228343cbc272f38186bb6bf8fcbb49843194d0ef48df2d652561b3fe9bb62da address=z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx time=1000000300 expiration-time=1000000300
`)
	activatePtlc(t, z)

	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.PtlcContract,
		Data: definition.ABIPtlc.PackMethodPanic(definition.CreatePtlcMethodName,
			int64(genesisTimestamp+300),
			definition.PointTypeED25519,
			g.User2.Public,
			types.ZeroAddress,
		),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(10 * g.Zexp),
	}).Error(t, nil)
	z.InsertNewMomentum()
	z.InsertNewMomentum()

	ptlcId := types.HexToHashPanic("b228343cbc272f38186bb6bf8fcbb49843194d0ef48df2d652561b3fe9bb62da")

	z.InsertMomentumsTo(30)

	mh := ptlcUnlockMessage(z, definition.PointTypeED25519, ptlcId, g.User2.Address)
	signature := g.User2.Sign(mh)
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User2.Address,
		ToAddress: types.PtlcContract,
		Data: definition.ABIPtlc.PackMethodPanic(definition.UnlockPtlcMethodName,
			ptlcId,
			signature,
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
t=2001-09-09T01:50:00+0000 lvl=dbug msg=created module=embedded contract=ptlc ptlcInfo="Id:b228343cbc272f38186bb6bf8fcbb49843194d0ef48df2d652561b3fe9bb62da TimeLocked:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenStandard:zts1znnxxxxxxxxxxxxx9z4ulx Amount:1000000000 ExpirationTime:1000000300 PointType:0 PointLock:tUJu3P7Drp25XP662lIjyFlFpvj8bWUpyC+0y5YTzXM= Destination:z1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqsggv2f "
t=2001-09-09T01:51:40+0000 lvl=dbug msg=reclaimed module=embedded contract=ptlc ptlcInfo="Id:b228343cbc272f38186bb6bf8fcbb49843194d0ef48df2d652561b3fe9bb62da TimeLocked:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenStandard:zts1znnxxxxxxxxxxxxx9z4ulx Amount:1000000000 ExpirationTime:1000000300 PointType:0 PointLock:tUJu3P7Drp25XP662lIjyFlFpvj8bWUpyC+0y5YTzXM= Destination:z1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqsggv2f "
`)
	activatePtlc(t, z)

	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.PtlcContract,
		Data: definition.ABIPtlc.PackMethodPanic(definition.CreatePtlcMethodName,
			int64(genesisTimestamp+300),
			definition.PointTypeED25519,
			g.User2.Public,
			types.ZeroAddress,
		),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(10 * g.Zexp),
	}).Error(t, nil)
	z.InsertNewMomentum()
	z.InsertNewMomentum()

	ptlcId := types.HexToHashPanic("b228343cbc272f38186bb6bf8fcbb49843194d0ef48df2d652561b3fe9bb62da")

	z.InsertMomentumsTo(30)

	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.PtlcContract,
		Data: definition.ABIPtlc.PackMethodPanic(definition.ReclaimPtlcMethodName,
			ptlcId,
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
t=2001-09-09T01:50:00+0000 lvl=dbug msg=created module=embedded contract=ptlc ptlcInfo="Id:b228343cbc272f38186bb6bf8fcbb49843194d0ef48df2d652561b3fe9bb62da TimeLocked:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenStandard:zts1znnxxxxxxxxxxxxx9z4ulx Amount:1000000000 ExpirationTime:1000000300 PointType:0 PointLock:tUJu3P7Drp25XP662lIjyFlFpvj8bWUpyC+0y5YTzXM= Destination:z1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqsggv2f "
t=2001-09-09T01:50:20+0000 lvl=dbug msg="invalid reclaim - permission denied" module=embedded contract=ptlc id=b228343cbc272f38186bb6bf8fcbb49843194d0ef48df2d652561b3fe9bb62da address=z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx
t=2001-09-09T01:50:30+0000 lvl=dbug msg="invalid reclaim - permission denied" module=embedded contract=ptlc id=b228343cbc272f38186bb6bf8fcbb49843194d0ef48df2d652561b3fe9bb62da address=z1qrs2lpccnsneglhnnfwvlsj0qncnxjnwlfmjac
t=2001-09-09T01:53:20+0000 lvl=dbug msg="invalid reclaim - permission denied" module=embedded contract=ptlc id=b228343cbc272f38186bb6bf8fcbb49843194d0ef48df2d652561b3fe9bb62da address=z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx
t=2001-09-09T01:53:30+0000 lvl=dbug msg="invalid reclaim - permission denied" module=embedded contract=ptlc id=b228343cbc272f38186bb6bf8fcbb49843194d0ef48df2d652561b3fe9bb62da address=z1qrs2lpccnsneglhnnfwvlsj0qncnxjnwlfmjac
t=2001-09-09T01:53:40+0000 lvl=dbug msg=reclaimed module=embedded contract=ptlc ptlcInfo="Id:b228343cbc272f38186bb6bf8fcbb49843194d0ef48df2d652561b3fe9bb62da TimeLocked:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenStandard:zts1znnxxxxxxxxxxxxx9z4ulx Amount:1000000000 ExpirationTime:1000000300 PointType:0 PointLock:tUJu3P7Drp25XP662lIjyFlFpvj8bWUpyC+0y5YTzXM= Destination:z1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqsggv2f "
`)
	activatePtlc(t, z)

	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.PtlcContract,
		Data: definition.ABIPtlc.PackMethodPanic(definition.CreatePtlcMethodName,
			int64(genesisTimestamp+300),
			definition.PointTypeED25519,
			g.User2.Public,
			types.ZeroAddress,
		),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(10 * g.Zexp),
	}).Error(t, nil)
	z.InsertNewMomentum()
	z.InsertNewMomentum()

	ptlcId := types.HexToHashPanic("b228343cbc272f38186bb6bf8fcbb49843194d0ef48df2d652561b3fe9bb62da")

	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User2.Address,
		ToAddress: types.PtlcContract,
		Data: definition.ABIPtlc.PackMethodPanic(definition.ReclaimPtlcMethodName,
			ptlcId,
		),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(0 * g.Zexp),
	}).Error(t, constants.ErrPermissionDenied)
	z.InsertNewMomentum()

	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User3.Address,
		ToAddress: types.PtlcContract,
		Data: definition.ABIPtlc.PackMethodPanic(definition.ReclaimPtlcMethodName,
			ptlcId,
		),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(0 * g.Zexp),
	}).Error(t, constants.ErrPermissionDenied)
	z.InsertNewMomentum()

	z.InsertMomentumsTo(40)

	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User2.Address,
		ToAddress: types.PtlcContract,
		Data: definition.ABIPtlc.PackMethodPanic(definition.ReclaimPtlcMethodName,
			ptlcId,
		),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(0 * g.Zexp),
	}).Error(t, constants.ErrPermissionDenied)
	z.InsertNewMomentum()

	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User3.Address,
		ToAddress: types.PtlcContract,
		Data: definition.ABIPtlc.PackMethodPanic(definition.ReclaimPtlcMethodName,
			ptlcId,
		),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(0 * g.Zexp),
	}).Error(t, constants.ErrPermissionDenied)
	z.InsertNewMomentum()

	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.PtlcContract,
		Data: definition.ABIPtlc.PackMethodPanic(definition.ReclaimPtlcMethodName,
			ptlcId,
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
	activatePtlc(t, z)

	nonexistentId := types.HexToHashPanic("7efdcca315f86cdb04e84113bfc5f003fa49c4b3f9b287cd3b4a08d8ccdf6ffc")

	common.Json(ptlcApi.GetById(nonexistentId)).Error(t, constants.ErrDataNonExistent)

	mh := ptlcUnlockMessage(z, definition.PointTypeED25519, nonexistentId, g.User2.Address)
	signature := g.User2.Sign(mh)
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User2.Address,
		ToAddress: types.PtlcContract,
		Data: definition.ABIPtlc.PackMethodPanic(definition.UnlockPtlcMethodName,
			nonexistentId,
			signature,
		),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(0 * g.Zexp),
	}).Error(t, constants.ErrDataNonExistent)
	z.InsertNewMomentum()

	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.PtlcContract,
		Data: definition.ABIPtlc.PackMethodPanic(definition.ReclaimPtlcMethodName,
			nonexistentId,
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
	activatePtlc(t, z)

	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.PtlcContract,
		Data: definition.ABIPtlc.PackMethodPanic(definition.CreatePtlcMethodName,
			int64(genesisTimestamp+300),
			definition.PointTypeED25519,
			g.User2.Public,
			types.ZeroAddress,
		),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(10 * g.Zexp),
	}).Error(t, nil)
	z.InsertNewMomentum()
	z.InsertNewMomentum()

	ptlcId := types.HexToHashPanic("b228343cbc272f38186bb6bf8fcbb49843194d0ef48df2d652561b3fe9bb62da")

	mh := ptlcUnlockMessage(z, definition.PointTypeED25519, ptlcId, g.User2.Address)
	signature := g.User2.Sign(mh)
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User2.Address,
		ToAddress: types.PtlcContract,
		Data: definition.ABIPtlc.PackMethodPanic(definition.UnlockPtlcMethodName,
			ptlcId,
			signature,
		),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(0 * g.Zexp),
	}).Error(t, nil)
	z.InsertNewMomentum()
	z.InsertNewMomentum()

	common.Json(ptlcApi.GetById(ptlcId)).Error(t, constants.ErrDataNonExistent)

	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User2.Address,
		ToAddress: types.PtlcContract,
		Data: definition.ABIPtlc.PackMethodPanic(definition.UnlockPtlcMethodName,
			ptlcId,
			signature,
		),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(0 * g.Zexp),
	}).Error(t, constants.ErrDataNonExistent)
	z.InsertNewMomentum()

	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.PtlcContract,
		Data: definition.ABIPtlc.PackMethodPanic(definition.ReclaimPtlcMethodName,
			ptlcId,
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
t=2001-09-09T01:50:00+0000 lvl=dbug msg=created module=embedded contract=ptlc ptlcInfo="Id:b228343cbc272f38186bb6bf8fcbb49843194d0ef48df2d652561b3fe9bb62da TimeLocked:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenStandard:zts1znnxxxxxxxxxxxxx9z4ulx Amount:1000000000 ExpirationTime:1000000300 PointType:0 PointLock:tUJu3P7Drp25XP662lIjyFlFpvj8bWUpyC+0y5YTzXM= Destination:z1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqsggv2f "
t=2001-09-09T01:53:20+0000 lvl=dbug msg=reclaimed module=embedded contract=ptlc ptlcInfo="Id:b228343cbc272f38186bb6bf8fcbb49843194d0ef48df2d652561b3fe9bb62da TimeLocked:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenStandard:zts1znnxxxxxxxxxxxxx9z4ulx Amount:1000000000 ExpirationTime:1000000300 PointType:0 PointLock:tUJu3P7Drp25XP662lIjyFlFpvj8bWUpyC+0y5YTzXM= Destination:z1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqsggv2f "
t=2001-09-09T01:53:30+0000 lvl=dbug msg="invalid unlock - entry does not exist" module=embedded contract=ptlc id=b228343cbc272f38186bb6bf8fcbb49843194d0ef48df2d652561b3fe9bb62da address=z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx
t=2001-09-09T01:53:40+0000 lvl=dbug msg="invalid reclaim - entry does not exist" module=embedded contract=ptlc id=b228343cbc272f38186bb6bf8fcbb49843194d0ef48df2d652561b3fe9bb62da address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz
`)
	activatePtlc(t, z)

	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.PtlcContract,
		Data: definition.ABIPtlc.PackMethodPanic(definition.CreatePtlcMethodName,
			int64(genesisTimestamp+300),
			definition.PointTypeED25519,
			g.User2.Public,
			types.ZeroAddress,
		),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(10 * g.Zexp),
	}).Error(t, nil)
	z.InsertNewMomentum()
	z.InsertMomentumsTo(40)

	ptlcId := types.HexToHashPanic("b228343cbc272f38186bb6bf8fcbb49843194d0ef48df2d652561b3fe9bb62da")

	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.PtlcContract,
		Data: definition.ABIPtlc.PackMethodPanic(definition.ReclaimPtlcMethodName,
			ptlcId,
		),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(0 * g.Zexp),
	}).Error(t, nil)
	z.InsertNewMomentum()

	common.Json(ptlcApi.GetById(ptlcId)).Error(t, constants.ErrDataNonExistent)

	mh := ptlcUnlockMessage(z, definition.PointTypeED25519, ptlcId, g.User2.Address)
	signature := g.User2.Sign(mh)
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User2.Address,
		ToAddress: types.PtlcContract,
		Data: definition.ABIPtlc.PackMethodPanic(definition.UnlockPtlcMethodName,
			ptlcId,
			signature,
		),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(0 * g.Zexp),
	}).Error(t, constants.ErrDataNonExistent)
	z.InsertNewMomentum()

	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.PtlcContract,
		Data: definition.ABIPtlc.PackMethodPanic(definition.ReclaimPtlcMethodName,
			ptlcId,
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
	activatePtlc(t, z)

	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.PtlcContract,
		Data: definition.ABIPtlc.PackMethodPanic(definition.CreatePtlcMethodName,
			int64(genesisTimestamp-300),
			definition.PointTypeED25519,
			g.User2.Public,
			types.ZeroAddress,
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

func TestPtlc_createRejectsInvalidBIP340Point(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()
	activatePtlc(t, z)

	z.InsertSendBlock(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.PtlcContract,
		Data: definition.ABIPtlc.PackMethodPanic(definition.CreatePtlcMethodName,
			int64(genesisTimestamp+300),
			definition.PointTypeBIP340,
			bytes.Repeat([]byte{0xff}, 32),
			types.ZeroAddress,
		),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(10 * g.Zexp),
	}, constants.ErrInvalidPointLock, mock.NoVmChanges)
	z.InsertNewMomentum()

	z.ExpectBalance(g.User1.Address, types.ZnnTokenStandard, 12000*g.Zexp)
	z.ExpectBalance(types.PtlcContract, types.ZnnTokenStandard, 0)
}

func TestPtlc_unlockBIP340(t *testing.T) {
	z := mock.NewMockZenon(t)
	ptlcApi := embedded.NewPtlcApi(z)
	defer z.StopPanic()
	activatePtlc(t, z)

	prv1, _ := btcec.PrivKeyFromBytes(g.Secp1PrvKey)

	prv2, pub2 := btcec.PrivKeyFromBytes(g.Secp2PrvKey)
	pub2bip340 := schnorr.SerializePubKey(pub2)

	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.PtlcContract,
		Data: definition.ABIPtlc.PackMethodPanic(definition.CreatePtlcMethodName,
			int64(genesisTimestamp+300),
			definition.PointTypeBIP340,
			pub2bip340,
			types.ZeroAddress,
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

	ptlcId := types.HexToHashPanic("47f57134b5d8308335b5c146e586cfdcb62dc476c0279dd6c04963ecca586a0e")

	common.Json(ptlcApi.GetById(ptlcId)).Equals(t, `
{
	"id": "47f57134b5d8308335b5c146e586cfdcb62dc476c0279dd6c04963ecca586a0e",
	"timeLocked": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
	"tokenStandard": "zts1znnxxxxxxxxxxxxx9z4ulx",
	"amount": "1000000000",
	"expirationTime": 1000000300,
	"pointType": 1,
	"pointLock": "fiG8+7m7odpA43OSLYoUwZ0RvTtY6wwnAPKWVeOJ5ww=",
	"destination": "z1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqsggv2f"
}
`)
	mh := ptlcUnlockMessage(z, definition.PointTypeBIP340, ptlcId, g.User2.Address)

	wrong_signature := g.User2.Sign(mh)
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User2.Address,
		ToAddress: types.PtlcContract,
		Data: definition.ABIPtlc.PackMethodPanic(definition.UnlockPtlcMethodName,
			ptlcId,
			wrong_signature,
		),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(0 * g.Zexp),
	}).Error(t, constants.ErrInvalidPointSignature)
	z.InsertNewMomentum()

	wrong_signature2, _ := schnorr.Sign(prv1, mh)
	ws2 := wrong_signature2.Serialize()
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User2.Address,
		ToAddress: types.PtlcContract,
		Data: definition.ABIPtlc.PackMethodPanic(definition.UnlockPtlcMethodName,
			ptlcId,
			ws2,
		),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(0 * g.Zexp),
	}).Error(t, constants.ErrInvalidPointSignature)
	z.InsertNewMomentum()

	oldMessage := crypto.Hash(common.JoinBytes(ptlcId.Bytes(), g.User2.Address.Bytes()))
	oldSignature, _ := schnorr.Sign(prv2, oldMessage)
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User2.Address,
		ToAddress: types.PtlcContract,
		Data: definition.ABIPtlc.PackMethodPanic(definition.UnlockPtlcMethodName,
			ptlcId,
			oldSignature.Serialize(),
		),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(0 * g.Zexp),
	}).Error(t, constants.ErrInvalidPointSignature)
	z.InsertNewMomentum()

	signature, _ := schnorr.Sign(prv2, mh)
	sig := signature.Serialize()
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User2.Address,
		ToAddress: types.PtlcContract,
		Data: definition.ABIPtlc.PackMethodPanic(definition.UnlockPtlcMethodName,
			ptlcId,
			sig,
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

func TestPtlc_proxyUnlockBIP340(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()

	activatePtlc(t, z)

	prv1, _ := btcec.PrivKeyFromBytes(g.Secp1PrvKey)
	prv2, pub2 := btcec.PrivKeyFromBytes(g.Secp2PrvKey)
	pub2bip340 := schnorr.SerializePubKey(pub2)

	createBlock := z.InsertSendBlock(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.PtlcContract,
		Data: definition.ABIPtlc.PackMethodPanic(definition.CreatePtlcMethodName,
			int64(genesisTimestamp+300),
			definition.PointTypeBIP340,
			pub2bip340,
			types.ZeroAddress,
		),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(10 * g.Zexp),
	}, nil, mock.SkipVmChanges)
	z.InsertNewMomentum()
	z.InsertNewMomentum()

	ptlcId := createBlock.Hash
	unlockMessage := ptlcUnlockMessage(z, definition.PointTypeBIP340, ptlcId, g.User2.Address)

	wrongSignerSignature, _ := schnorr.Sign(prv1, unlockMessage)
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User3.Address,
		ToAddress: types.PtlcContract,
		Data: definition.ABIPtlc.PackMethodPanic(definition.ProxyUnlockPtlcMethodName,
			ptlcId,
			g.User2.Address,
			wrongSignerSignature.Serialize(),
		),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(0),
	}).Error(t, constants.ErrInvalidPointSignature)
	z.InsertNewMomentum()

	rightSignature, _ := schnorr.Sign(prv2, unlockMessage)
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User3.Address,
		ToAddress: types.PtlcContract,
		Data: definition.ABIPtlc.PackMethodPanic(definition.ProxyUnlockPtlcMethodName,
			ptlcId,
			g.User3.Address,
			rightSignature.Serialize(),
		),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(0),
	}).Error(t, constants.ErrInvalidPointSignature)
	z.InsertNewMomentum()

	nonexistentId := types.HexToHashPanic("7efdcca315f86cdb04e84113bfc5f003fa49c4b3f9b287cd3b4a08d8ccdf6ffc")
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User3.Address,
		ToAddress: types.PtlcContract,
		Data: definition.ABIPtlc.PackMethodPanic(definition.ProxyUnlockPtlcMethodName,
			nonexistentId,
			g.User2.Address,
			rightSignature.Serialize(),
		),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(0),
	}).Error(t, constants.ErrDataNonExistent)
	z.InsertNewMomentum()

	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User3.Address,
		ToAddress: types.PtlcContract,
		Data: definition.ABIPtlc.PackMethodPanic(definition.ProxyUnlockPtlcMethodName,
			ptlcId,
			g.User2.Address,
			rightSignature.Serialize(),
		),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(0),
	}).Error(t, nil)
	z.InsertNewMomentum()
	z.InsertNewMomentum()

	autoreceive(t, z, g.User2.Address)
	z.InsertNewMomentum()
	z.InsertNewMomentum()

	z.ExpectBalance(g.User2.Address, types.ZnnTokenStandard, (8000+10)*g.Zexp)
	z.ExpectBalance(types.PtlcContract, types.ZnnTokenStandard, 0)
}

func TestPtlc_proxyUnlockBIP340_lifecycle(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()
	activatePtlc(t, z)

	prv2, pub2 := btcec.PrivKeyFromBytes(g.Secp2PrvKey)
	pub2bip340 := schnorr.SerializePubKey(pub2)

	expiredId := createBIP340Ptlc(t, z, pub2bip340, int64(genesisTimestamp+300))
	z.InsertMomentumsTo(30)
	expiredSignature := signBIP340Unlock(t, z, prv2, expiredId, g.User2.Address)
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User3.Address,
		ToAddress: types.PtlcContract,
		Data: definition.ABIPtlc.PackMethodPanic(definition.ProxyUnlockPtlcMethodName,
			expiredId,
			g.User2.Address,
			expiredSignature,
		),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(0),
	}).Error(t, constants.ErrExpired)
	z.InsertNewMomentum()

	reclaimedId := createBIP340Ptlc(t, z, pub2bip340, int64(genesisTimestamp+330))
	reclaimedSignature := signBIP340Unlock(t, z, prv2, reclaimedId, g.User2.Address)
	z.InsertMomentumsTo(33)
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.PtlcContract,
		Data: definition.ABIPtlc.PackMethodPanic(definition.ReclaimPtlcMethodName,
			reclaimedId,
		),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(0),
	}).Error(t, nil)
	z.InsertNewMomentum()
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User3.Address,
		ToAddress: types.PtlcContract,
		Data: definition.ABIPtlc.PackMethodPanic(definition.ProxyUnlockPtlcMethodName,
			reclaimedId,
			g.User2.Address,
			reclaimedSignature,
		),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(0),
	}).Error(t, constants.ErrDataNonExistent)
	z.InsertNewMomentum()

	proxyFirstId := createBIP340Ptlc(t, z, pub2bip340, int64(genesisTimestamp+1000))
	proxyFirstSignature := signBIP340Unlock(t, z, prv2, proxyFirstId, g.User2.Address)
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User3.Address,
		ToAddress: types.PtlcContract,
		Data: definition.ABIPtlc.PackMethodPanic(definition.ProxyUnlockPtlcMethodName,
			proxyFirstId,
			g.User2.Address,
			proxyFirstSignature,
		),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(0),
	}).Error(t, nil)
	z.InsertNewMomentum()
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User2.Address,
		ToAddress: types.PtlcContract,
		Data: definition.ABIPtlc.PackMethodPanic(definition.UnlockPtlcMethodName,
			proxyFirstId,
			proxyFirstSignature,
		),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(0),
	}).Error(t, constants.ErrDataNonExistent)
	z.InsertNewMomentum()

	directFirstId := createBIP340Ptlc(t, z, pub2bip340, int64(genesisTimestamp+1000))
	directFirstSignature := signBIP340Unlock(t, z, prv2, directFirstId, g.User2.Address)
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User2.Address,
		ToAddress: types.PtlcContract,
		Data: definition.ABIPtlc.PackMethodPanic(definition.UnlockPtlcMethodName,
			directFirstId,
			directFirstSignature,
		),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(0),
	}).Error(t, nil)
	z.InsertNewMomentum()
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User3.Address,
		ToAddress: types.PtlcContract,
		Data: definition.ABIPtlc.PackMethodPanic(definition.ProxyUnlockPtlcMethodName,
			directFirstId,
			g.User2.Address,
			directFirstSignature,
		),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(0),
	}).Error(t, constants.ErrDataNonExistent)
	z.InsertNewMomentum()
}

func TestPtlc_unlockQsrAccounting(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()
	activatePtlc(t, z)

	ptlcId := createED25519Ptlc(t, z, g.User2.Public, int64(genesisTimestamp+300), types.QsrTokenStandard, big.NewInt(10*g.Zexp))

	z.ExpectBalance(g.User1.Address, types.ZnnTokenStandard, 12000*g.Zexp)
	z.ExpectBalance(g.User1.Address, types.QsrTokenStandard, 119990*g.Zexp)

	z.ExpectBalance(g.User2.Address, types.ZnnTokenStandard, 8000*g.Zexp)
	z.ExpectBalance(g.User2.Address, types.QsrTokenStandard, 80000*g.Zexp)

	z.ExpectBalance(types.PtlcContract, types.ZnnTokenStandard, 0)
	z.ExpectBalance(types.PtlcContract, types.QsrTokenStandard, 10*g.Zexp)

	signature := g.User2.Sign(ptlcUnlockMessage(z, definition.PointTypeED25519, ptlcId, g.User2.Address))
	unlock := z.CallContract(&nom.AccountBlock{
		Address:   g.User2.Address,
		ToAddress: types.PtlcContract,
		Data: definition.ABIPtlc.PackMethodPanic(definition.UnlockPtlcMethodName,
			ptlcId,
			signature,
		),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(0),
	})
	z.InsertNewMomentum()
	unlock.Error(t, nil)
	z.InsertNewMomentum()

	autoreceive(t, z, g.User2.Address)
	z.InsertNewMomentum()

	z.ExpectBalance(g.User1.Address, types.ZnnTokenStandard, 12000*g.Zexp)
	z.ExpectBalance(g.User1.Address, types.QsrTokenStandard, 119990*g.Zexp)

	z.ExpectBalance(g.User2.Address, types.ZnnTokenStandard, 8000*g.Zexp)
	z.ExpectBalance(g.User2.Address, types.QsrTokenStandard, 80010*g.Zexp)

	z.ExpectBalance(types.PtlcContract, types.ZnnTokenStandard, 0)
	z.ExpectBalance(types.PtlcContract, types.QsrTokenStandard, 0)
}

func TestPtlc_ED25519UnlockReplayCompetition(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()
	activatePtlc(t, z)

	proxyFirstId := createED25519Ptlc(t, z, g.User2.Public, int64(genesisTimestamp+1000), types.ZnnTokenStandard, big.NewInt(10*g.Zexp))
	proxyFirstSignature := g.User2.Sign(ptlcUnlockMessage(z, definition.PointTypeED25519, proxyFirstId, g.User2.Address))
	proxyUnlock := z.CallContract(&nom.AccountBlock{
		Address:   g.User3.Address,
		ToAddress: types.PtlcContract,
		Data: definition.ABIPtlc.PackMethodPanic(definition.ProxyUnlockPtlcMethodName,
			proxyFirstId,
			g.User2.Address,
			proxyFirstSignature,
		),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(0),
	})
	z.InsertNewMomentum()
	proxyUnlock.Error(t, nil)

	directReplay := z.CallContract(&nom.AccountBlock{
		Address:   g.User2.Address,
		ToAddress: types.PtlcContract,
		Data: definition.ABIPtlc.PackMethodPanic(definition.UnlockPtlcMethodName,
			proxyFirstId,
			proxyFirstSignature,
		),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(0),
	})
	z.InsertNewMomentum()
	directReplay.Error(t, constants.ErrDataNonExistent)

	directFirstId := createED25519Ptlc(t, z, g.User2.Public, int64(genesisTimestamp+1000), types.ZnnTokenStandard, big.NewInt(10*g.Zexp))
	directFirstSignature := g.User2.Sign(ptlcUnlockMessage(z, definition.PointTypeED25519, directFirstId, g.User2.Address))
	directUnlock := z.CallContract(&nom.AccountBlock{
		Address:   g.User2.Address,
		ToAddress: types.PtlcContract,
		Data: definition.ABIPtlc.PackMethodPanic(definition.UnlockPtlcMethodName,
			directFirstId,
			directFirstSignature,
		),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(0),
	})
	z.InsertNewMomentum()
	directUnlock.Error(t, nil)

	proxyReplay := z.CallContract(&nom.AccountBlock{
		Address:   g.User3.Address,
		ToAddress: types.PtlcContract,
		Data: definition.ABIPtlc.PackMethodPanic(definition.ProxyUnlockPtlcMethodName,
			directFirstId,
			g.User2.Address,
			directFirstSignature,
		),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(0),
	})
	z.InsertNewMomentum()
	proxyReplay.Error(t, constants.ErrDataNonExistent)

	autoreceive(t, z, g.User2.Address)
	z.InsertNewMomentum()

	z.ExpectBalance(g.User1.Address, types.ZnnTokenStandard, 11980*g.Zexp)
	z.ExpectBalance(g.User2.Address, types.ZnnTokenStandard, 8020*g.Zexp)
	z.ExpectBalance(types.PtlcContract, types.ZnnTokenStandard, 0)
}
