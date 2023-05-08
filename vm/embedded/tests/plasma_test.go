package tests

import (
	"math/big"
	"testing"
	"time"

	g "github.com/zenon-network/go-zenon/chain/genesis/mock"
	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/pow"
	"github.com/zenon-network/go-zenon/rpc/api"
	"github.com/zenon-network/go-zenon/rpc/api/embedded"
	"github.com/zenon-network/go-zenon/vm/constants"
	"github.com/zenon-network/go-zenon/vm/embedded/definition"
	"github.com/zenon-network/go-zenon/zenon/mock"
)

func parseNonce(a string) nom.Nonce {
	nonce := nom.Nonce{}
	common.DealWithErr(nonce.UnmarshalText([]byte(a)))
	return nonce
}

// - test that sender can fuse plasma for another account
// - test that sender consumes plasma by sending blocks
// - test that sender regains full plasma after block is confirmed
// - test when receiver receives the plasma
// - test that receiver does not have plasma if it acknowledges an earlier momentum
func TestPlasma_simple(t *testing.T) {
	z := mock.NewMockZenon(t)
	plasmaApi := embedded.NewPlasmaApi(z)
	ledgerApi := api.NewLedgerApi(z)

	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).HideHashes().Equals(t, `
t=2001-09-09T01:47:00+0000 lvl=dbug msg="fused new entry" module=embedded contract=plasma fusionInfo="{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz Id:XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX Amount:+1000000000 ExpirationHeight:103 Beneficiary:z1qqdt06lnwz57x38rwlyutcx5wgrtl0ynkfe3kv}" beneficiary="&{Beneficiary:z1qqdt06lnwz57x38rwlyutcx5wgrtl0ynkfe3kv Amount:+1000000000}"
`)

	z.InsertSendBlock(&nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     g.User6.Address,
		TokenStandard: types.QsrTokenStandard,
		Amount:        big.NewInt(10 * g.Zexp),
	}, nil, mock.SkipVmChanges)
	z.InsertNewMomentum() // include User1 -> User6 send in momentum 2

	defer z.CallContract(&nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     types.PlasmaContract,
		Data:          definition.ABIPlasma.PackMethodPanic(definition.FuseMethodName, g.User6.Address),
		TokenStandard: types.QsrTokenStandard,
		Amount:        big.NewInt(10 * g.Zexp),
	}).Error(t, nil)
	common.Json(plasmaApi.Get(g.User1.Address)).Equals(t, `
{
	"currentPlasma": 10447500,
	"maxPlasma": 10500000,
	"qsrAmount": "1000000000000"
}`) // User1 consumed plasma by sending blocks

	z.InsertNewMomentum() // include send block
	common.Json(plasmaApi.Get(g.User1.Address)).Equals(t, `{
	"currentPlasma": 10500000,
	"maxPlasma": 10500000,
	"qsrAmount": "1000000000000"
}`) // User 1 refreshed to full plasma
	common.Json(plasmaApi.Get(g.User6.Address)).Equals(t, `{
	"currentPlasma": 0,
	"maxPlasma": 0,
	"qsrAmount": "0"
}`) // User 6 didn't gain plasma (yet)

	z.InsertNewMomentum() // include contract receive block
	common.Json(plasmaApi.Get(g.User6.Address)).Equals(t, `{
	"currentPlasma": 21000,
	"maxPlasma": 21000,
	"qsrAmount": "1000000000"
}`) // User 6 just gained plasma

	momentums, err := ledgerApi.GetMomentumsByHeight(3, 2)
	common.FailIfErr(t, err)

	z.InsertReceiveBlock(types.AccountHeader{
		Address: g.User1.Address,
		HashHeight: types.HashHeight{
			Hash:   types.HexToHashPanic("1e4859a604f37c7bf1d3a9da668cf09eaab20bfe1460f8e2444ccc7058d11d19"),
			Height: 2,
		},
	}, &nom.AccountBlock{
		MomentumAcknowledged: momentums.List[0].Identifier(),
	}, constants.ErrNotEnoughPlasma, mock.SkipVmChanges) // when acknowledging momentum 3, User 6 has 0 plasma
	z.InsertReceiveBlock(types.AccountHeader{
		Address: g.User1.Address,
		HashHeight: types.HashHeight{
			Hash:   types.HexToHashPanic("1e4859a604f37c7bf1d3a9da668cf09eaab20bfe1460f8e2444ccc7058d11d19"),
			Height: 2,
		},
	}, &nom.AccountBlock{
		MomentumAcknowledged: momentums.List[1].Identifier(),
	}, nil, mock.SkipVmChanges) // when acknowledging momentum 4, User 6 has 21K plasma
}

// - test that it's possible to create blocks with both plasma & PoW
// - test that User loses only the minimum amount of plasma (if too much PoW is provided, doesn't consume all plasma)
// - test that User refreshes to full plasma (not more than max plasma) after block is confirmed
func TestPlasma_combined(t *testing.T) {
	z := mock.NewMockZenon(t)
	plasmaApi := embedded.NewPlasmaApi(z)
	ledgerApi := api.NewLedgerApi(z)

	defer z.StopPanic()
	defer z.CallContract(&nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     types.PlasmaContract,
		Data:          definition.ABIPlasma.PackMethodPanic(definition.FuseMethodName, g.User6.Address),
		TokenStandard: types.QsrTokenStandard,
		Amount:        big.NewInt(10 * g.Zexp),
	}).Error(t, nil)
	z.InsertSendBlock(&nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     g.User6.Address,
		Amount:        big.NewInt(10 * g.Zexp),
		TokenStandard: types.QsrTokenStandard,
	}, nil, mock.SkipVmChanges)

	z.InsertNewMomentum() // include send block
	z.InsertNewMomentum() // include contract receive block
	autoreceive(t, z, g.User6.Address)
	z.ExpectBalance(g.User6.Address, types.QsrTokenStandard, 1000000000)
	z.InsertNewMomentum() // include User6 ReceiveBlock

	z.InsertSendBlock(&nom.AccountBlock{
		Address:       g.User6.Address,
		ToAddress:     types.PlasmaContract,
		Data:          definition.ABIPlasma.PackMethodPanic(definition.FuseMethodName, g.User6.Address),
		TokenStandard: types.QsrTokenStandard,
		Amount:        big.NewInt(10 * g.Zexp),
	}, constants.ErrNotEnoughPlasma, mock.NoVmChanges)

	// get pow-hash to generate nonce from it
	last, err := ledgerApi.GetFrontierAccountBlock(g.User6.Address)
	common.FailIfErr(t, err)
	common.Expect(t, pow.GetAccountBlockHash(&nom.AccountBlock{
		Address:      g.User6.Address,
		PreviousHash: last.Hash,
	}), "5b52014cd1d1fad1e7528f1450847f56341728bc59316d35ac6582904d659ff6")

	z.InsertSendBlock(&nom.AccountBlock{
		Address:       g.User6.Address,
		ToAddress:     types.PlasmaContract,
		Data:          definition.ABIPlasma.PackMethodPanic(definition.FuseMethodName, g.User6.Address),
		TokenStandard: types.QsrTokenStandard,
		Amount:        big.NewInt(10 * g.Zexp),
		FusedPlasma:   11000,
		Difficulty:    41500 * constants.PoWDifficultyPerPlasma,
		Nonce:         parseNonce("135759ef94039b2e"),
	}, nil, mock.SkipVmChanges)
	common.Json(plasmaApi.Get(g.User6.Address)).Equals(t, `
{
	"currentPlasma": 10000,
	"maxPlasma": 21000,
	"qsrAmount": "1000000000"
}`) // User 6 used all plasma
	z.InsertNewMomentum() // include send block
	common.Json(plasmaApi.Get(g.User6.Address)).Equals(t, `
{
	"currentPlasma": 21000,
	"maxPlasma": 21000,
	"qsrAmount": "1000000000"
}`) // User 6 refreshed to full 21K plasma
	z.InsertNewMomentum() // include contract receive block
	common.Json(plasmaApi.Get(g.User6.Address)).Equals(t, `
{
	"currentPlasma": 42000,
	"maxPlasma": 42000,
	"qsrAmount": "2000000000"
}`) // User 6 refreshed to full 42K plasma (new plasma kicked in)
}

// - test plasma.GetRequiredPoWForAccountBlock rpc with 0 plasma
// - test plasma.GetRequiredPoWForAccountBlock rpc with some plasma
// - test plasma.GetRequiredPoWForAccountBlock rpc with a lot of plasma
// - test plasma.GetEntriesByAddress rpc with some responses
// - test plasma.GetEntriesByAddress rpc with 0 responses
func TestPlasma_rpc(t *testing.T) {
	z := mock.NewMockZenon(t)
	plasmaApi := embedded.NewPlasmaApi(z)
	defer z.StopPanic()

	common.Json(plasmaApi.GetRequiredPoWForAccountBlock(embedded.GetRequiredParam{
		BlockType: nom.BlockTypeUserSend,
		SelfAddr:  g.User1.Address,
		ToAddr:    &types.PlasmaContract,
		Data:      definition.ABIPlasma.PackMethodPanic(definition.FuseMethodName, g.User6.Address),
	})).Equals(t, `
{
	"availablePlasma": 10500000,
	"basePlasma": 52500,
	"requiredDifficulty": 0
}`)
	common.Json(plasmaApi.GetRequiredPoWForAccountBlock(embedded.GetRequiredParam{
		BlockType: nom.BlockTypeUserSend,
		SelfAddr:  g.User6.Address,
		ToAddr:    &types.PlasmaContract,
		Data:      definition.ABIPlasma.PackMethodPanic(definition.FuseMethodName, g.User6.Address),
	})).Equals(t, `
{
	"availablePlasma": 0,
	"basePlasma": 52500,
	"requiredDifficulty": 78750000
}`)

	defer z.CallContract(&nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     types.PlasmaContract,
		Data:          definition.ABIPlasma.PackMethodPanic(definition.FuseMethodName, g.User6.Address),
		TokenStandard: types.QsrTokenStandard,
		Amount:        big.NewInt(10 * g.Zexp),
	}).Error(t, nil)
	z.InsertNewMomentum()
	z.InsertNewMomentum()

	common.Json(plasmaApi.GetRequiredPoWForAccountBlock(embedded.GetRequiredParam{
		BlockType: nom.BlockTypeUserSend,
		SelfAddr:  g.User6.Address,
		ToAddr:    &types.PlasmaContract,
		Data:      definition.ABIPlasma.PackMethodPanic(definition.FuseMethodName, g.User6.Address),
	})).Equals(t, `
{
	"availablePlasma": 21000,
	"basePlasma": 52500,
	"requiredDifficulty": 47250000
}`)
	common.Json(plasmaApi.GetEntriesByAddress(g.User1.Address, 0, 10)).HideHashes().Equals(t, `
{
	"qsrAmount": "2001000000000",
	"count": 3,
	"list": [
		{
			"qsrAmount": "1000000000000",
			"beneficiary": "z1qqfmjdays57w488sta69ykc2ey7r6d0q9wdvtj",
			"expirationHeight": 0,
			"id": "XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX"
		},
		{
			"qsrAmount": "1000000000000",
			"beneficiary": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
			"expirationHeight": 0,
			"id": "XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX"
		},
		{
			"qsrAmount": "1000000000",
			"beneficiary": "z1qqdt06lnwz57x38rwlyutcx5wgrtl0ynkfe3kv",
			"expirationHeight": 102,
			"id": "XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX"
		}
	]
}`)
	common.Json(plasmaApi.GetEntriesByAddress(g.User6.Address, 0, 10)).Equals(t, `
{
	"qsrAmount": "0",
	"count": 0,
	"list": []
}`)
}

// - test revoke plasma entry which was in genesis (expiration height = 0)
// - test that you have an unreceived block
// - test that the plasma.Get RPC returns 0 now
func TestPlasma_RevokeGenesisPlasma(t *testing.T) {
	z := mock.NewMockZenon(t)
	plasmaApi := embedded.NewPlasmaApi(z)
	ledgerApi := api.NewLedgerApi(z)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).Equals(t, `
t=2001-09-09T01:46:50+0000 lvl=dbug msg="canceled fusion entry" module=embedded contract=plasma fusionInfo="&{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz Id:117613e734b6cb0fd7b7583f5b0e863a3f0c856cd32fa36f1b60b464d068c5a6 Amount:+1000000000000 ExpirationHeight:0 Beneficiary:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz}" beneficiary-remaining="&{Beneficiary:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz Amount:+0}"
`)

	common.Json(plasmaApi.GetEntriesByAddress(g.User1.Address, 0, 10)).Equals(t, `
{
	"qsrAmount": "2000000000000",
	"count": 2,
	"list": [
		{
			"qsrAmount": "1000000000000",
			"beneficiary": "z1qqfmjdays57w488sta69ykc2ey7r6d0q9wdvtj",
			"expirationHeight": 0,
			"id": "0000000000000000000000000000000000000000000000000000000000000000"
		},
		{
			"qsrAmount": "1000000000000",
			"beneficiary": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
			"expirationHeight": 0,
			"id": "117613e734b6cb0fd7b7583f5b0e863a3f0c856cd32fa36f1b60b464d068c5a6"
		}
	]
}`)
	common.Json(plasmaApi.Get(g.User1.Address)).Equals(t, `
{
	"currentPlasma": 10500000,
	"maxPlasma": 10500000,
	"qsrAmount": "1000000000000"
}`)
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.PlasmaContract,
		Data:      definition.ABIPlasma.PackMethodPanic(definition.CancelFuseMethodName, types.HexToHashPanic("117613e734b6cb0fd7b7583f5b0e863a3f0c856cd32fa36f1b60b464d068c5a6")),
	}).Error(t, nil)
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
			"height": 2,
			"momentumAcknowledged": {
				"hash": "XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX",
				"height": 2
			},
			"address": "z1qxemdeddedxplasmaxxxxxxxxxxxxxxxxsctrp",
			"toAddress": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
			"amount": "1000000000000",
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
				"totalSupply": "180550000000000",
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
				"momentumHeight": 3,
				"momentumHash": "XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX",
				"momentumTimestamp": 1000000020
			},
			"pairedAccountBlock": null
		}
	],
	"count": 1,
	"more": false
}`)
	common.Json(plasmaApi.GetEntriesByAddress(g.User1.Address, 0, 10)).Equals(t, `
{
	"qsrAmount": "1000000000000",
	"count": 1,
	"list": [
		{
			"qsrAmount": "1000000000000",
			"beneficiary": "z1qqfmjdays57w488sta69ykc2ey7r6d0q9wdvtj",
			"expirationHeight": 0,
			"id": "0000000000000000000000000000000000000000000000000000000000000000"
		}
	]
}`)
	common.Json(plasmaApi.Get(g.User1.Address)).Equals(t, `
{
	"currentPlasma": 0,
	"maxPlasma": 0,
	"qsrAmount": "0"
}`)
}

// - fuse plasma entry
// - revoke newly-fused plasma entry
// - auto-receive QSR to initial funds
func TestPlasma_RevokeFusedPlasma(t *testing.T) {
	z := mock.NewMockZenonWithCustomEpochDuration(t, time.Hour)
	plasmaApi := embedded.NewPlasmaApi(z)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).HideHashes().Equals(t, `
t=2001-09-09T01:46:50+0000 lvl=dbug msg="fused new entry" module=embedded contract=plasma fusionInfo="{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz Id:XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX Amount:+1000000000 ExpirationHeight:32 Beneficiary:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz}" beneficiary="&{Beneficiary:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz Amount:+1001000000000}"
t=2001-09-09T01:52:10+0000 lvl=dbug msg="canceled fusion entry" module=embedded contract=plasma fusionInfo="&{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz Id:XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX Amount:+1000000000 ExpirationHeight:32 Beneficiary:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz}" beneficiary-remaining="&{Beneficiary:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz Amount:+1000000000000}"
`)
	constants.FuseExpiration = 30

	common.Json(plasmaApi.GetEntriesByAddress(g.User1.Address, 0, 10)).Equals(t, `
{
	"qsrAmount": "2000000000000",
	"count": 2,
	"list": [
		{
			"qsrAmount": "1000000000000",
			"beneficiary": "z1qqfmjdays57w488sta69ykc2ey7r6d0q9wdvtj",
			"expirationHeight": 0,
			"id": "0000000000000000000000000000000000000000000000000000000000000000"
		},
		{
			"qsrAmount": "1000000000000",
			"beneficiary": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
			"expirationHeight": 0,
			"id": "117613e734b6cb0fd7b7583f5b0e863a3f0c856cd32fa36f1b60b464d068c5a6"
		}
	]
}`)
	common.Json(plasmaApi.Get(g.User1.Address)).Equals(t, `
{
	"currentPlasma": 10500000,
	"maxPlasma": 10500000,
	"qsrAmount": "1000000000000"
}`)
	defer z.CallContract(&nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     types.PlasmaContract,
		Data:          definition.ABIPlasma.PackMethodPanic(definition.FuseMethodName, g.User1.Address),
		TokenStandard: types.QsrTokenStandard,
		Amount:        big.NewInt(10 * g.Zexp),
	}).Error(t, nil)
	z.InsertMomentumsTo(33)
	common.Json(plasmaApi.GetEntriesByAddress(g.User1.Address, 0, 10)).HideHashes().Equals(t, `
{
	"qsrAmount": "2001000000000",
	"count": 3,
	"list": [
		{
			"qsrAmount": "1000000000000",
			"beneficiary": "z1qqfmjdays57w488sta69ykc2ey7r6d0q9wdvtj",
			"expirationHeight": 0,
			"id": "XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX"
		},
		{
			"qsrAmount": "1000000000000",
			"beneficiary": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
			"expirationHeight": 0,
			"id": "XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX"
		},
		{
			"qsrAmount": "1000000000",
			"beneficiary": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
			"expirationHeight": 32,
			"id": "XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX"
		}
	]
}`)
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.PlasmaContract,
		Data:      definition.ABIPlasma.PackMethodPanic(definition.CancelFuseMethodName, types.HexToHashPanic("1336bcb3978306f39cb441165aa37efd0f2edfccc9cb94ff47e08c2a7777fbd6")),
	}).Error(t, nil)
	z.InsertNewMomentum()
	z.InsertNewMomentum()
	autoreceive(t, z, g.User1.Address)
	z.ExpectBalance(g.User1.Address, types.QsrTokenStandard, 120000*g.Zexp)
}

// - user 1 & user 2 both fuse to user 6 too much
// - user6 gains max plasma
// - future fuse entries do not increase
// - canceling a fuse works as expected
func TestPlasma_FuseMaxPlasma(t *testing.T) {
	z := mock.NewMockZenonWithCustomEpochDuration(t, time.Hour)
	plasmaApi := embedded.NewPlasmaApi(z)
	constants.FuseExpiration = 10
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).HideHashes().Equals(t, `
t=2001-09-09T01:46:50+0000 lvl=dbug msg="fused new entry" module=embedded contract=plasma fusionInfo="{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz Id:XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX Amount:+350000000000 ExpirationHeight:12 Beneficiary:z1qqdt06lnwz57x38rwlyutcx5wgrtl0ynkfe3kv}" beneficiary="&{Beneficiary:z1qqdt06lnwz57x38rwlyutcx5wgrtl0ynkfe3kv Amount:+350000000000}"
t=2001-09-09T01:47:10+0000 lvl=dbug msg="fused new entry" module=embedded contract=plasma fusionInfo="{Owner:z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx Id:XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX Amount:+350000000000 ExpirationHeight:14 Beneficiary:z1qqdt06lnwz57x38rwlyutcx5wgrtl0ynkfe3kv}" beneficiary="&{Beneficiary:z1qqdt06lnwz57x38rwlyutcx5wgrtl0ynkfe3kv Amount:+700000000000}"
t=2001-09-09T01:47:30+0000 lvl=dbug msg="fused new entry" module=embedded contract=plasma fusionInfo="{Owner:z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx Id:XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX Amount:+100000000000 ExpirationHeight:16 Beneficiary:z1qqdt06lnwz57x38rwlyutcx5wgrtl0ynkfe3kv}" beneficiary="&{Beneficiary:z1qqdt06lnwz57x38rwlyutcx5wgrtl0ynkfe3kv Amount:+800000000000}"
t=2001-09-09T01:51:40+0000 lvl=dbug msg="canceled fusion entry" module=embedded contract=plasma fusionInfo="&{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz Id:XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX Amount:+350000000000 ExpirationHeight:12 Beneficiary:z1qqdt06lnwz57x38rwlyutcx5wgrtl0ynkfe3kv}" beneficiary-remaining="&{Beneficiary:z1qqdt06lnwz57x38rwlyutcx5wgrtl0ynkfe3kv Amount:+450000000000}"
`)

	common.Json(plasmaApi.Get(g.User6.Address)).Equals(t, `
{
	"currentPlasma": 0,
	"maxPlasma": 0,
	"qsrAmount": "0"
}`)
	defer z.CallContract(&nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     types.PlasmaContract,
		Data:          definition.ABIPlasma.PackMethodPanic(definition.FuseMethodName, g.User6.Address),
		TokenStandard: types.QsrTokenStandard,
		Amount:        big.NewInt(3500 * g.Zexp),
	}).Error(t, nil)
	z.InsertNewMomentum()
	z.InsertNewMomentum()

	common.Json(plasmaApi.Get(g.User6.Address)).Equals(t, `
{
	"currentPlasma": 7350000,
	"maxPlasma": 7350000,
	"qsrAmount": "350000000000"
}`)

	defer z.CallContract(&nom.AccountBlock{
		Address:       g.User2.Address,
		ToAddress:     types.PlasmaContract,
		Data:          definition.ABIPlasma.PackMethodPanic(definition.FuseMethodName, g.User6.Address),
		TokenStandard: types.QsrTokenStandard,
		Amount:        big.NewInt(3500 * g.Zexp),
	}).Error(t, nil)
	z.InsertNewMomentum()
	z.InsertNewMomentum()

	common.Json(plasmaApi.Get(g.User6.Address)).Equals(t, `
{
	"currentPlasma": 10500000,
	"maxPlasma": 10500000,
	"qsrAmount": "700000000000"
}`)

	defer z.CallContract(&nom.AccountBlock{
		Address:       g.User2.Address,
		ToAddress:     types.PlasmaContract,
		Data:          definition.ABIPlasma.PackMethodPanic(definition.FuseMethodName, g.User6.Address),
		TokenStandard: types.QsrTokenStandard,
		Amount:        big.NewInt(1000 * g.Zexp),
	}).Error(t, nil)
	z.InsertNewMomentum()
	z.InsertNewMomentum()

	common.Json(plasmaApi.Get(g.User6.Address)).Equals(t, `
{
	"currentPlasma": 10500000,
	"maxPlasma": 10500000,
	"qsrAmount": "800000000000"
}`)

	z.InsertMomentumsTo(30)
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.PlasmaContract,
		Data:      definition.ABIPlasma.PackMethodPanic(definition.CancelFuseMethodName, types.HexToHashPanic("706d52954a8182e686858459c6e111c958fc7a2a596d23b255285bf0b9121351")),
	}).Error(t, nil)

	z.InsertNewMomentum()
	z.InsertNewMomentum()
	z.InsertNewMomentum()

	common.Json(plasmaApi.Get(g.User6.Address)).Equals(t, `
{
	"currentPlasma": 9450000,
	"maxPlasma": 9450000,
	"qsrAmount": "450000000000"
}`)
}

// - limit plasma.expiration to 100
// - try to cancel entry after 50 momentums => "staking period still active"
func TestPlasma_TooEarlyRevoke(t *testing.T) {
	z := mock.NewMockZenonWithCustomEpochDuration(t, time.Hour)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).HideHashes().Equals(t, `
t=2001-09-09T01:46:50+0000 lvl=dbug msg="fused new entry" module=embedded contract=plasma fusionInfo="{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz Id:XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX Amount:+10000000000000 ExpirationHeight:102 Beneficiary:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz}" beneficiary="&{Beneficiary:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz Amount:+11000000000000}"
`)
	constants.FuseExpiration = 100

	defer z.CallContract(&nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     types.PlasmaContract,
		Data:          definition.ABIPlasma.PackMethodPanic(definition.FuseMethodName, g.User1.Address),
		TokenStandard: types.QsrTokenStandard,
		Amount:        big.NewInt(100000 * g.Zexp),
	}).Error(t, nil)
	z.InsertMomentumsTo(50)

	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.PlasmaContract,
		Data:      definition.ABIPlasma.PackMethodPanic(definition.CancelFuseMethodName, types.HexToHashPanic("92857f9876a9a6fb24108157122502589bcb6f894039b41bd5f50f7e1caf9b01")),
	}).Error(t, constants.RevokeNotDue)
	z.InsertNewMomentum()
}

// - User1 stakes plasma entry
// - User2 tries to cancel it => "data non existent"
// - User1 tries to cancel plasma entry with invalid id => "data non existent"
func TestPlasma_InvalidRevoke(t *testing.T) {
	z := mock.NewMockZenonWithCustomEpochDuration(t, time.Hour)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).HideHashes().Equals(t, `
t=2001-09-09T01:46:50+0000 lvl=dbug msg="fused new entry" module=embedded contract=plasma fusionInfo="{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz Id:XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX Amount:+10000000000000 ExpirationHeight:102 Beneficiary:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz}" beneficiary="&{Beneficiary:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz Amount:+11000000000000}"
`)
	defer z.CallContract(&nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     types.PlasmaContract,
		Data:          definition.ABIPlasma.PackMethodPanic(definition.FuseMethodName, g.User1.Address),
		TokenStandard: types.QsrTokenStandard,
		Amount:        big.NewInt(100000 * g.Zexp),
	}).Error(t, nil)
	z.InsertNewMomentum()

	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User2.Address,
		ToAddress: types.PlasmaContract,
		Data:      definition.ABIPlasma.PackMethodPanic(definition.CancelFuseMethodName, types.HexToHashPanic("b3f9b2a54d84db80a0f371766c82fbdc6544fc2ce585fee7d3ef75862c77bb42")),
	}).Error(t, constants.ErrDataNonExistent)
	z.InsertNewMomentum()

	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.PlasmaContract,
		Data:      definition.ABIPlasma.PackMethodPanic(definition.CancelFuseMethodName, types.HexToHashPanic("07bdc7be6262051c1e332eca2b7cf685992175957ea7886f8b5fa2ec5674e466")),
	}).Error(t, constants.ErrDataNonExistent)
	z.InsertNewMomentum()
}
