package tests

import (
	"encoding/base64"
	"encoding/hex"
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

func stringErrDealWith(s string, err error) string {
	common.DealWithErr(err)
	return s
}

func TestSwap_NoDecay(t *testing.T) {
	z := mock.NewMockZenon(t)
	swapRpc := embedded.NewSwapApi(z)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).Equals(t, `
t=2001-09-09T01:46:40+0000 lvl=dbug msg=swap-utils-log module=embedded contract=swap-utils-log expected-message=0x205ada59ae0f7ab9902c84adc484b92928f42dfa35a802fdb2662ad9da392146
t=2001-09-09T01:46:40+0000 lvl=dbug msg=swap-utils-log module=embedded contract=swap-utils-log expected-message=0x205ada59ae0f7ab9902c84adc484b92928f42dfa35a802fdb2662ad9da392146
t=2001-09-09T01:46:50+0000 lvl=dbug msg=swap-utils-log module=embedded contract=swap-utils-log expected-message=0x205ada59ae0f7ab9902c84adc484b92928f42dfa35a802fdb2662ad9da392146
t=2001-09-09T01:46:50+0000 lvl=dbug msg=swap-assets-log module=embedded contract=swap address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz public-key="BHMGwyX6chZyO+4GjA8roUOCF8IqBzbfQUNKwyujjAT1XI4++b5hN38ZEBbfBasPzKigubUFNxEBxGDFmu7earY=" signature="H69GQoKobYElK9HC356X9ot+t8TEoUmHcbIqUkWpk7mpDtuy8PvQtNLArKuYp+eu8giJZneBLM4h1AoxTgwl5g0="
t=2001-09-09T01:46:50+0000 lvl=dbug msg="deposit to withdraw" module=embedded contract=swap znn=1500000000000 qsr=15000000000000
t=2001-09-09T01:46:50+0000 lvl=dbug msg=swap-utils-log module=embedded contract=swap-utils-log expected-message=0x205ada59ae0f7ab9902c84adc484b92928f42dfa35a802fdb2662ad9da392146
t=2001-09-09T01:46:50+0000 lvl=dbug msg=swap-assets-log module=embedded contract=swap address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz public-key="BHMGwyX6chZyO+4GjA8roUOCF8IqBzbfQUNKwyujjAT1XI4++b5hN38ZEBbfBasPzKigubUFNxEBxGDFmu7earY=" signature="H69GQoKobYElK9HC356X9ot+t8TEoUmHcbIqUkWpk7mpDtuy8PvQtNLArKuYp+eu8giJZneBLM4h1AoxTgwl5g0="
t=2001-09-09T01:47:00+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg TokenName:Zenon Coin TokenSymbol:ZNN TokenDomain:zenon.network TotalSupply:+21000000000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1znnxxxxxxxxxxxxx9z4ulx}" minted-amount=1500000000000 to-address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz
t=2001-09-09T01:47:00+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 TokenName:QuasarCoin TokenSymbol:QSR TokenDomain:zenon.network TotalSupply:+195550000000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1qsrxxxxxxxxxxxxxmrhjll}" minted-amount=15000000000000 to-address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz
`)

	common.Json(swapRpc.GetAssetsByKeyIdHash(types.HexToHashPanic("c955c2b650452d670179068995a51132463e2d13f7519d64ff283af99dd14b43"))).
		Equals(t, `
{
	"keyIdHash": "c955c2b650452d670179068995a51132463e2d13f7519d64ff283af99dd14b43",
	"znn": "1500000000000",
	"qsr": "15000000000000"
}`)

	// RPC call with assets
	{
		list, err := swapRpc.GetAssets()
		common.FailIfErr(t, err)
		common.ExpectJson(t, list, `
{
	"c955c2b650452d670179068995a51132463e2d13f7519d64ff283af99dd14b43": {
		"znn": "1500000000000",
		"qsr": "15000000000000"
	}
}`)
	}

	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.SwapContract,
		Data: definition.ABISwap.PackMethodPanic(
			definition.RetrieveAssetsMethodName,
			g.Secp1PubKeyB64,
			stringErrDealWith(implementation.SignRetrieveAssetsMessage(g.User1.Address, g.Secp1PrvKey, g.Secp1PubKeyB64))),
	}).Error(t, nil)

	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.SwapContract,
		Data: definition.ABISwap.PackMethodPanic(
			definition.RetrieveAssetsMethodName,
			g.Secp1PubKeyB64,
			stringErrDealWith(implementation.SignRetrieveAssetsMessage(g.User1.Address, g.Secp1PrvKey, g.Secp1PubKeyB64))),
	}).Error(t, constants.ErrDataNonExistent)

	z.InsertNewMomentum()
	z.InsertNewMomentum()

	// RPC call with assets swapped
	{
		list, err := swapRpc.GetAssets()
		common.FailIfErr(t, err)
		common.ExpectJson(t, list, `
{
	"c955c2b650452d670179068995a51132463e2d13f7519d64ff283af99dd14b43": {
		"znn": "0",
		"qsr": "0"
	}
}`)
	}

	common.Json(swapRpc.GetAssetsByKeyIdHash(types.HexToHashPanic("c955c2b650452d670179068995a51132463e2d13f7519d64ff283af99dd14b43"))).
		Equals(t, `
{
	"keyIdHash": "c955c2b650452d670179068995a51132463e2d13f7519d64ff283af99dd14b43",
	"znn": "0",
	"qsr": "0"
}`)
}

func TestSwap_WithDecay(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).Equals(t, `
t=2001-09-09T02:03:10+0000 lvl=dbug msg=swap-utils-log module=embedded contract=swap-utils-log expected-message=0x205ada59ae0f7ab9902c84adc484b92928f42dfa35a802fdb2662ad9da392146
t=2001-09-09T02:03:20+0000 lvl=dbug msg=swap-utils-log module=embedded contract=swap-utils-log expected-message=0x205ada59ae0f7ab9902c84adc484b92928f42dfa35a802fdb2662ad9da392146
t=2001-09-09T02:03:20+0000 lvl=dbug msg=swap-assets-log module=embedded contract=swap address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz public-key="BHMGwyX6chZyO+4GjA8roUOCF8IqBzbfQUNKwyujjAT1XI4++b5hN38ZEBbfBasPzKigubUFNxEBxGDFmu7earY=" signature="H69GQoKobYElK9HC356X9ot+t8TEoUmHcbIqUkWpk7mpDtuy8PvQtNLArKuYp+eu8giJZneBLM4h1AoxTgwl5g0="
t=2001-09-09T02:03:20+0000 lvl=dbug msg="deposit to withdraw" module=embedded contract=swap znn=1500000000000 qsr=15000000000000
`)

	z.InsertMomentumsTo(100)

	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.SwapContract,
		Data: definition.ABISwap.PackMethodPanic(
			definition.RetrieveAssetsMethodName,
			g.Secp1PubKeyB64,
			stringErrDealWith(implementation.SignRetrieveAssetsMessage(g.User1.Address, g.Secp1PrvKey, g.Secp1PubKeyB64))),
	}).Error(t, nil)
	z.InsertNewMomentum()
}

// TestSwapVerify_RetrieveAssets tests for standard pack/unpack as a simple sanity check.
// signature-related fail-tests are handled in registerLegacyPillar test.
func TestSwapVerify_RetrieveAssets(t *testing.T) {
	methodName := definition.RetrieveAssetsMethodName
	method := &implementation.SwapRetrieveAssetsMethod{
		MethodName: methodName,
	}

	encodedMethod := "0x47f12c81000000000000000000000000000000000000000000000000000000000000004000000000000000000000000000000000000000000000000000000000000000c0000000000000000000000000000000000000000000000000000000000000005842484d477779583663685a794f2b34476a4138726f554f4346384971427a626651554e4b7779756a6a4154315849342b2b6235684e33385a45426266426173507a4b6967756255464e784542784744466d7537656172593d0000000000000000000000000000000000000000000000000000000000000000000000000000005848363947516f4b6f6259456c4b39484333353658396f742b743854456f556d4863624971556b57706b376d704474757938507651744e4c41724b7559702b65753867694a5a6e65424c4d346831416f785467776c3567303d0000000000000000"
	signatureStr, err := implementation.SignRetrieveAssetsMessage(g.User1.Address, g.Secp1PrvKey, g.Secp1PubKeyB64)
	common.FailIfErr(t, err)
	common.ExpectString(t, signatureStr, "H69GQoKobYElK9HC356X9ot+t8TEoUmHcbIqUkWpk7mpDtuy8PvQtNLArKuYp+eu8giJZneBLM4h1AoxTgwl5g0=")

	// - pack from config
	methodData, err := definition.ABISwap.PackMethod(methodName, g.Secp1PubKeyB64, signatureStr)
	common.FailIfErr(t, err)
	common.ExpectBytes(t, methodData, encodedMethod)

	// - unpack from packed data
	param := new(definition.ParamRetrieveAssets)
	err = definition.ABISwap.UnpackMethod(param, methodName, methodData)
	common.FailIfErr(t, err)

	// - pack from unpacked param
	tmpMethodData, err := definition.ABISwap.PackMethod(methodName, param.PublicKey, param.Signature)
	common.FailIfErr(t, err)
	common.ExpectBytes(t, tmpMethodData, encodedMethod)

	// test actual method StaticVerify
	block := &nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.SwapContract,
		Data:      methodData,
		Amount:    common.Big0,
	}
	err = method.ValidateSendBlock(block)
	common.FailIfErr(t, err)
	common.ExpectBytes(t, block.Data, encodedMethod)

	// test second pair of legacy keys
	common.FailIfErr(t, method.ValidateSendBlock(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.PillarContract,
		Data: definition.ABISwap.PackMethodPanic(
			methodName,
			g.Secp2PubKeyB64,
			stringErrDealWith(implementation.SignRetrieveAssetsMessage(g.User1.Address, g.Secp2PrvKey, g.Secp2PubKeyB64)),
		),
		Amount: common.Big0,
	}))
}

func TestSwap_GetKeyIdHash(t *testing.T) {
	pubKey, err := base64.StdEncoding.DecodeString(g.Secp1PubKeyB64)
	common.FailIfErr(t, err)
	keyIdHash := implementation.PubKeyToKeyIdHash(pubKey)
	keyIdHashHex := hex.EncodeToString(keyIdHash.Bytes())
	common.ExpectString(t, keyIdHashHex, g.Secp1KeyIdHex)
}
