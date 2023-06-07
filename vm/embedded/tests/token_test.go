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
	"github.com/zenon-network/go-zenon/verifier"
	"github.com/zenon-network/go-zenon/vm/constants"
	"github.com/zenon-network/go-zenon/vm/embedded/definition"
	"github.com/zenon-network/go-zenon/zenon/mock"
)

var (
	customZts types.ZenonTokenStandard
)

func issueTokenSetup(t *testing.T, z mock.MockZenon) {
	tokenAPI := embedded.NewTokenApi(z)
	defer z.CallContract(&nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     types.TokenContract,
		TokenStandard: types.ZnnTokenStandard,
		Amount:        constants.TokenIssueAmount,
		Data: definition.ABIToken.PackMethodPanic(definition.IssueMethodName,
			"test.tok3n_na-m3", //param.TokenName
			"TEST",             //param.TokenSymbol
			"",                 //param.TokenDomain
			big.NewInt(100),    //param.TotalSupply
			big.NewInt(1000),   //param.MaxSupply
			uint8(1),           //param.Decimals
			true,               //param.IsMintable
			true,               //param.IsBurnable
			false,              //param.IsUtility
		),
	}).Error(t, nil)
	z.InsertNewMomentum() // cemented send block
	z.InsertNewMomentum() // cemented token-receive-block

	tokenList, err := tokenAPI.GetByOwner(g.User1.Address, 0, 10)
	common.FailIfErr(t, err)

	common.Json(tokenList, err).Equals(t, `
{
	"count": 1,
	"list": [
		{
			"name": "test.tok3n_na-m3",
			"symbol": "TEST",
			"domain": "",
			"totalSupply": "100",
			"decimals": 1,
			"owner": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
			"tokenStandard": "zts103tsa5yqngu9cfpj2m0z9u",
			"maxSupply": "1000",
			"isBurnable": true,
			"isMintable": true,
			"isUtility": false
		}
	]
}`)
	customZts = tokenList.List[0].ZenonTokenStandard
}

func mint(user types.Address, zts types.ZenonTokenStandard, amount *big.Int, beneficiary types.Address) *nom.AccountBlock {
	return &nom.AccountBlock{
		Address:   user,
		ToAddress: types.TokenContract,
		Data: definition.ABIToken.PackMethodPanic(definition.MintMethodName,
			zts, amount, beneficiary),
	}
}

func issue(user types.Address, name, symbol, domain string, totalSupply, maxSupply *big.Int, decimals uint8, mintable, burnable, utility bool) *nom.AccountBlock {
	return &nom.AccountBlock{
		Address:       user,
		ToAddress:     types.TokenContract,
		TokenStandard: types.ZnnTokenStandard,
		Amount:        constants.TokenIssueAmount,
		Data: definition.ABIToken.PackMethodPanic(definition.IssueMethodName,
			name,
			symbol,
			domain,
			totalSupply,
			maxSupply,
			decimals,
			mintable,
			burnable,
			utility,
		),
	}
}

// Test token amounts
// - abi packaging works as expected
// - send & receive of huge values works ok
// - send > 2^255 fails automatically when verifying the block's amounts
func TestToken_TryToOverflow(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).Equals(t, `
t=2001-09-09T01:46:50+0000 lvl=dbug msg="issued ZTS" module=embedded contract=token token="{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenName:test.tok3n_na-m3 TokenSymbol:TEST TokenDomain: TotalSupply:+57896044618658097711785492504343953926634992332820282019728792003956564819967 MaxSupply:+57896044618658097711785492504343953926634992332820282019728792003956564819967 Decimals:1 IsMintable:true IsBurnable:true IsUtility:false TokenStandard:zts1h5xauyvh7vxgnhj09dfclk}"
`)
	ledgerApi := api.NewLedgerApi(z)
	zts := types.ParseZTSPanic("zts1h5xauyvh7vxgnhj09dfclk")

	// Too much token ~ all 1nes
	z.InsertSendBlock(&nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     types.TokenContract,
		TokenStandard: types.ZnnTokenStandard,
		Amount:        constants.TokenIssueAmount,
		Data: definition.ABIToken.PackMethodPanic(definition.IssueMethodName,
			"test.tok3n_na-m3", //param.TokenName
			"TEST",             //param.TokenSymbol
			"",                 //param.TokenDomain
			big.NewInt(100),    //param.TotalSupply
			common.BigP256m1,   //param.MaxSupply
			uint8(1),           //param.Decimals
			true,               //param.IsMintable
			true,               //param.IsBurnable
			false,              //param.IsUtility
		),
	}, constants.ErrTokenInvalidAmount, mock.NoVmChanges)
	// Given how ABI works, 2^256 is treated as 0
	z.InsertSendBlock(&nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     types.TokenContract,
		TokenStandard: types.ZnnTokenStandard,
		Amount:        constants.TokenIssueAmount,
		Data: definition.ABIToken.PackMethodPanic(definition.IssueMethodName,
			"test.tok3n_na-m3", //param.TokenName
			"TEST",             //param.TokenSymbol
			"",                 //param.TokenDomain
			big.NewInt(100),    //param.TotalSupply
			common.BigP256,     //param.MaxSupply
			uint8(1),           //param.Decimals
			true,               //param.IsMintable
			true,               //param.IsBurnable
			false,              //param.IsUtility
		),
	}, constants.ErrTokenInvalidAmount, mock.NoVmChanges)

	defer z.CallContract(&nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     types.TokenContract,
		TokenStandard: types.ZnnTokenStandard,
		Amount:        constants.TokenIssueAmount,
		Data: definition.ABIToken.PackMethodPanic(definition.IssueMethodName,
			"test.tok3n_na-m3", //param.TokenName
			"TEST",             //param.TokenSymbol
			"",                 //param.TokenDomain
			common.BigP255m1,   //param.TotalSupply
			common.BigP255m1,   //param.MaxSupply
			uint8(1),           //param.Decimals
			true,               //param.IsMintable
			true,               //param.IsBurnable
			false,              //param.IsUtility
		),
	}).Error(t, nil)
	z.InsertNewMomentum()
	z.InsertNewMomentum()
	autoreceive(t, z, g.User1.Address)
	z.InsertNewMomentum()
	common.Json(ledgerApi.GetAccountInfoByAddress(g.User1.Address)).Equals(t, `
{
	"address": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
	"accountHeight": 3,
	"balanceInfoMap": {
		"zts1h5xauyvh7vxgnhj09dfclk": {
			"token": {
				"name": "test.tok3n_na-m3",
				"symbol": "TEST",
				"domain": "",
				"totalSupply": "57896044618658097711785492504343953926634992332820282019728792003956564819967",
				"decimals": 1,
				"owner": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
				"tokenStandard": "zts1h5xauyvh7vxgnhj09dfclk",
				"maxSupply": "57896044618658097711785492504343953926634992332820282019728792003956564819967",
				"isBurnable": true,
				"isMintable": true,
				"isUtility": false
			},
			"balance": "57896044618658097711785492504343953926634992332820282019728792003956564819967"
		},
		"zts1qsrxxxxxxxxxxxxxmrhjll": {
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
			"balance": "12000000000000"
		},
		"zts1znnxxxxxxxxxxxxx9z4ulx": {
			"token": {
				"name": "Zenon Coin",
				"symbol": "ZNN",
				"domain": "zenon.network",
				"totalSupply": "19500000000000",
				"decimals": 8,
				"owner": "z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg",
				"tokenStandard": "zts1znnxxxxxxxxxxxxx9z4ulx",
				"maxSupply": "4611686018427387903",
				"isBurnable": true,
				"isMintable": true,
				"isUtility": true
			},
			"balance": "1199900000000"
		}
	}
}`)

	// Try to send too much token - fails in verifier
	z.InsertSendBlock(&nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     g.User2.Address,
		TokenStandard: zts,
		Amount:        common.BigP255,
	}, verifier.ErrABAmountTooBig, mock.NoVmChanges)
	common.Json(ledgerApi.GetAccountInfoByAddress(g.User1.Address)).Equals(t, `
{
	"address": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
	"accountHeight": 3,
	"balanceInfoMap": {
		"zts1h5xauyvh7vxgnhj09dfclk": {
			"token": {
				"name": "test.tok3n_na-m3",
				"symbol": "TEST",
				"domain": "",
				"totalSupply": "57896044618658097711785492504343953926634992332820282019728792003956564819967",
				"decimals": 1,
				"owner": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
				"tokenStandard": "zts1h5xauyvh7vxgnhj09dfclk",
				"maxSupply": "57896044618658097711785492504343953926634992332820282019728792003956564819967",
				"isBurnable": true,
				"isMintable": true,
				"isUtility": false
			},
			"balance": "57896044618658097711785492504343953926634992332820282019728792003956564819967"
		},
		"zts1qsrxxxxxxxxxxxxxmrhjll": {
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
			"balance": "12000000000000"
		},
		"zts1znnxxxxxxxxxxxxx9z4ulx": {
			"token": {
				"name": "Zenon Coin",
				"symbol": "ZNN",
				"domain": "zenon.network",
				"totalSupply": "19500000000000",
				"decimals": 8,
				"owner": "z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg",
				"tokenStandard": "zts1znnxxxxxxxxxxxxx9z4ulx",
				"maxSupply": "4611686018427387903",
				"isBurnable": true,
				"isMintable": true,
				"isUtility": true
			},
			"balance": "1199900000000"
		}
	}
}`)
	// Send all the token to check that nothing bad happens
	z.InsertSendBlock(&nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     g.User2.Address,
		TokenStandard: zts,
		Amount:        common.BigP255m1,
	}, nil, mock.SkipVmChanges)
	z.InsertNewMomentum()
	autoreceive(t, z, g.User2.Address)
	common.Json(ledgerApi.GetAccountInfoByAddress(g.User2.Address)).Equals(t, `
{
	"address": "z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx",
	"accountHeight": 2,
	"balanceInfoMap": {
		"zts1h5xauyvh7vxgnhj09dfclk": {
			"token": {
				"name": "test.tok3n_na-m3",
				"symbol": "TEST",
				"domain": "",
				"totalSupply": "57896044618658097711785492504343953926634992332820282019728792003956564819967",
				"decimals": 1,
				"owner": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
				"tokenStandard": "zts1h5xauyvh7vxgnhj09dfclk",
				"maxSupply": "57896044618658097711785492504343953926634992332820282019728792003956564819967",
				"isBurnable": true,
				"isMintable": true,
				"isUtility": false
			},
			"balance": "57896044618658097711785492504343953926634992332820282019728792003956564819967"
		},
		"zts1qsrxxxxxxxxxxxxxmrhjll": {
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
			"balance": "8000000000000"
		},
		"zts1znnxxxxxxxxxxxxx9z4ulx": {
			"token": {
				"name": "Zenon Coin",
				"symbol": "ZNN",
				"domain": "zenon.network",
				"totalSupply": "19500000000000",
				"decimals": 8,
				"owner": "z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg",
				"tokenStandard": "zts1znnxxxxxxxxxxxxx9z4ulx",
				"maxSupply": "4611686018427387903",
				"isBurnable": true,
				"isMintable": true,
				"isUtility": true
			},
			"balance": "800000000000"
		}
	}
}`)
}

// Test Issue token
// - owner receives totalSupply of tokens
// - owner misses 100 ZNN
// - token embedded has a balance of 100 ZNN
func TestToken_IssueOk(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).Equals(t, `
t=2001-09-09T01:46:50+0000 lvl=dbug msg="issued ZTS" module=embedded contract=token token="{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenName:test.tok3n_na-m3 TokenSymbol:TEST TokenDomain: TotalSupply:+100 MaxSupply:+1000 Decimals:1 IsMintable:true IsBurnable:true IsUtility:false TokenStandard:zts103tsa5yqngu9cfpj2m0z9u}"
`)

	z.ExpectBalance(g.User1.Address, types.ZnnTokenStandard, 12000*g.Zexp)
	issueTokenSetup(t, z)
	autoreceive(t, z, g.User1.Address)
	z.ExpectBalance(g.User1.Address, customZts, 100)
	// check znn balances
	z.ExpectBalance(g.User1.Address, types.ZnnTokenStandard, 11999*g.Zexp)
	z.ExpectBalance(types.TokenContract, types.ZnnTokenStandard, 1*g.Zexp)
}

// Test Mint to owner & non-owner
// - mint to owner & non-owner
func TestToken_MintOk(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).Equals(t, `
t=2001-09-09T01:46:50+0000 lvl=dbug msg="issued ZTS" module=embedded contract=token token="{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenName:test.tok3n_na-m3 TokenSymbol:TEST TokenDomain: TotalSupply:+100 MaxSupply:+1000 Decimals:1 IsMintable:true IsBurnable:true IsUtility:false TokenStandard:zts103tsa5yqngu9cfpj2m0z9u}"
t=2001-09-09T01:47:10+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenName:test.tok3n_na-m3 TokenSymbol:TEST TokenDomain: TotalSupply:+110 MaxSupply:+1000 Decimals:1 IsMintable:true IsBurnable:true IsUtility:false TokenStandard:zts103tsa5yqngu9cfpj2m0z9u}" minted-amount=10 to-address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz
t=2001-09-09T01:47:10+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenName:test.tok3n_na-m3 TokenSymbol:TEST TokenDomain: TotalSupply:+130 MaxSupply:+1000 Decimals:1 IsMintable:true IsBurnable:true IsUtility:false TokenStandard:zts103tsa5yqngu9cfpj2m0z9u}" minted-amount=20 to-address=z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx
t=2001-09-09T01:47:10+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenName:test.tok3n_na-m3 TokenSymbol:TEST TokenDomain: TotalSupply:+180 MaxSupply:+1000 Decimals:1 IsMintable:true IsBurnable:true IsUtility:false TokenStandard:zts103tsa5yqngu9cfpj2m0z9u}" minted-amount=50 to-address=z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx
`)
	issueTokenSetup(t, z)

	// Mint token to owner
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.TokenContract,
		Data:      definition.ABIToken.PackMethodPanic(definition.MintMethodName, customZts, big.NewInt(10), g.User1.Address),
	}).Error(t, nil)
	// Mint token to non-owner
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.TokenContract,
		Data:      definition.ABIToken.PackMethodPanic(definition.MintMethodName, customZts, big.NewInt(20), g.User2.Address),
	}).Error(t, nil)
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.TokenContract,
		Data:      definition.ABIToken.PackMethodPanic(definition.MintMethodName, customZts, big.NewInt(50), g.User2.Address),
	}).Error(t, nil)

	z.InsertNewMomentum() // cemented sent-blocks
	z.InsertNewMomentum() // cemented token receive-blocks
	autoreceive(t, z, g.User1.Address)
	autoreceive(t, z, g.User2.Address)

	z.ExpectBalance(g.User1.Address, customZts, 110)
	z.ExpectBalance(g.User2.Address, customZts, 70)
}

// Test Mint to embedded
// - can't mint to pillar embedded since it doesn't have the method
// - can mint to liquidity embedded since it doesn't have the method
func TestToken_MintToEmbedded(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).Equals(t, `
t=2001-09-09T01:46:50+0000 lvl=dbug msg="issued ZTS" module=embedded contract=token token="{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenName:test.tok3n_na-m3 TokenSymbol:TEST TokenDomain: TotalSupply:+100 MaxSupply:+1000 Decimals:1 IsMintable:true IsBurnable:true IsUtility:false TokenStandard:zts103tsa5yqngu9cfpj2m0z9u}"
t=2001-09-09T01:47:10+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenName:test.tok3n_na-m3 TokenSymbol:TEST TokenDomain: TotalSupply:+110 MaxSupply:+1000 Decimals:1 IsMintable:true IsBurnable:true IsUtility:false TokenStandard:zts103tsa5yqngu9cfpj2m0z9u}" minted-amount=10 to-address=z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg
t=2001-09-09T01:47:10+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenName:test.tok3n_na-m3 TokenSymbol:TEST TokenDomain: TotalSupply:+110 MaxSupply:+1000 Decimals:1 IsMintable:true IsBurnable:true IsUtility:false TokenStandard:zts103tsa5yqngu9cfpj2m0z9u}" minted-amount=10 to-address=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae
t=2001-09-09T01:47:20+0000 lvl=info msg="received donation" module=embedded contract=common embedded=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae from-address=z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0 zts=zts103tsa5yqngu9cfpj2m0z9u amount=10
`)
	issueTokenSetup(t, z)

	// Mint token to embedded pillar which has no donate method
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.TokenContract,
		Data:      definition.ABIToken.PackMethodPanic(definition.MintMethodName, customZts, big.NewInt(10), types.PillarContract),
	}).Error(t, constants.ErrContractMethodNotFound)

	// Mint token to embedded liquidity which has a donate method
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.TokenContract,
		Data:      definition.ABIToken.PackMethodPanic(definition.MintMethodName, customZts, big.NewInt(10), types.LiquidityContract),
	}).Error(t, nil)

	z.InsertMomentumsTo(10)
}

// Test mint too much
// - issue token for User1
// - try to mint maxSupply
func TestToken_MintTooMuch(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).Equals(t, `
t=2001-09-09T01:46:50+0000 lvl=dbug msg="issued ZTS" module=embedded contract=token token="{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenName:test.tok3n_na-m3 TokenSymbol:TEST TokenDomain: TotalSupply:+100 MaxSupply:+1000 Decimals:1 IsMintable:true IsBurnable:true IsUtility:false TokenStandard:zts103tsa5yqngu9cfpj2m0z9u}"
`)
	issueTokenSetup(t, z)

	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.TokenContract,
		Data:      definition.ABIToken.PackMethodPanic(definition.MintMethodName, customZts, big.NewInt(1000), g.User2.Address),
	}).Error(t, constants.ErrTokenInvalidAmount)
	z.InsertNewMomentum()
}

// Test owner update from non-owner
// - issue token
// - try to update from non-owner
func TestToken_UpdateFromNonOwner(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()
	tokenAPI := embedded.NewTokenApi(z)
	defer z.SaveLogs(common.EmbeddedLogger).Equals(t, `
t=2001-09-09T01:46:50+0000 lvl=dbug msg="issued ZTS" module=embedded contract=token token="{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenName:test.tok3n_na-m3 TokenSymbol:TEST TokenDomain: TotalSupply:+100 MaxSupply:+1000 Decimals:1 IsMintable:true IsBurnable:true IsUtility:false TokenStandard:zts103tsa5yqngu9cfpj2m0z9u}"
`)
	issueTokenSetup(t, z)
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User2.Address,
		ToAddress: types.TokenContract,
		Data: definition.ABIToken.PackMethodPanic(definition.UpdateTokenMethodName,
			customZts,       //param.TokenStandard
			g.User1.Address, //param.Owner
			true,            //param.IsMintable
			true,            //param.IsBurnable
		),
	}).Error(t, constants.ErrPermissionDenied)
	z.InsertNewMomentum() // cemented update block
	z.InsertNewMomentum() // cemented token receive-blocks
	// Check that token is still the same
	common.Json(tokenAPI.GetByZts(customZts)).Equals(t, `
{
	"name": "test.tok3n_na-m3",
	"symbol": "TEST",
	"domain": "",
	"totalSupply": "100",
	"decimals": 1,
	"owner": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
	"tokenStandard": "zts103tsa5yqngu9cfpj2m0z9u",
	"maxSupply": "1000",
	"isBurnable": true,
	"isMintable": true,
	"isUtility": false
}`)
}

// Test update from owner
// - issue token for User1
// - update owner for token (the new owner is User2)
// - call RPC & check results
func TestToken_UpdateOwner(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()
	tokenAPI := embedded.NewTokenApi(z)
	defer z.SaveLogs(common.EmbeddedLogger).Equals(t, `
t=2001-09-09T01:46:50+0000 lvl=dbug msg="issued ZTS" module=embedded contract=token token="{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenName:test.tok3n_na-m3 TokenSymbol:TEST TokenDomain: TotalSupply:+100 MaxSupply:+1000 Decimals:1 IsMintable:true IsBurnable:true IsUtility:false TokenStandard:zts103tsa5yqngu9cfpj2m0z9u}"
t=2001-09-09T01:47:10+0000 lvl=dbug msg="updating token owner" module=embedded contract=token old=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz new=z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx
t=2001-09-09T01:47:10+0000 lvl=dbug msg="updated ZTS" module=embedded contract=token token="&{Owner:z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx TokenName:test.tok3n_na-m3 TokenSymbol:TEST TokenDomain: TotalSupply:+100 MaxSupply:+1000 Decimals:1 IsMintable:true IsBurnable:true IsUtility:false TokenStandard:zts103tsa5yqngu9cfpj2m0z9u}"
`)
	issueTokenSetup(t, z)
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.TokenContract,
		Data: definition.ABIToken.PackMethodPanic(definition.UpdateTokenMethodName,
			customZts,       //param.TokenStandard
			g.User2.Address, //param.Owner
			true,            //param.IsMintable
			true,            //param.IsBurnable
		),
	}).Error(t, nil)
	z.InsertNewMomentum() // cemented update block
	z.InsertNewMomentum() // cemented token receive-blocks
	common.Json(tokenAPI.GetByOwner(g.User2.Address, 0, 5)).HideHashes().Equals(t, `
{
	"count": 1,
	"list": [
		{
			"name": "test.tok3n_na-m3",
			"symbol": "TEST",
			"domain": "",
			"totalSupply": "100",
			"decimals": 1,
			"owner": "z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx",
			"tokenStandard": "zts103tsa5yqngu9cfpj2m0z9u",
			"maxSupply": "1000",
			"isBurnable": true,
			"isMintable": true,
			"isUtility": false
		}
	]
}`)
	common.Json(tokenAPI.GetByOwner(g.User1.Address, 0, 5)).HideHashes().Equals(t, `
{
	"count": 0,
	"list": []
}`)
}

// Test the possibility to burn token after update
// - issue token for User1
// - update owner & isBurnable for token (the new owner is User2 & isBurnable becomes false)
// - try to burn token from User1
func TestToken_UpdateIsBurnable(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()
	tokenAPI := embedded.NewTokenApi(z)
	defer z.SaveLogs(common.EmbeddedLogger).Equals(t, `
t=2001-09-09T01:46:50+0000 lvl=dbug msg="issued ZTS" module=embedded contract=token token="{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenName:test.tok3n_na-m3 TokenSymbol:TEST TokenDomain: TotalSupply:+100 MaxSupply:+1000 Decimals:1 IsMintable:true IsBurnable:true IsUtility:false TokenStandard:zts103tsa5yqngu9cfpj2m0z9u}"
t=2001-09-09T01:47:10+0000 lvl=dbug msg="updating token owner" module=embedded contract=token old=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz new=z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx
t=2001-09-09T01:47:10+0000 lvl=dbug msg="updating token IsBurnable" module=embedded contract=token old=true new=false
t=2001-09-09T01:47:10+0000 lvl=dbug msg="updated ZTS" module=embedded contract=token token="&{Owner:z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx TokenName:test.tok3n_na-m3 TokenSymbol:TEST TokenDomain: TotalSupply:+100 MaxSupply:+1000 Decimals:1 IsMintable:true IsBurnable:false IsUtility:false TokenStandard:zts103tsa5yqngu9cfpj2m0z9u}"
`)
	issueTokenSetup(t, z)
	autoreceive(t, z, g.User1.Address)
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.TokenContract,
		Data: definition.ABIToken.PackMethodPanic(definition.UpdateTokenMethodName,
			customZts,       //param.TokenStandard
			g.User2.Address, //param.Owner
			true,            //param.IsMintable
			false,           //param.IsBurnable
		),
	}).Error(t, nil)
	z.InsertNewMomentum() // cemented update block
	z.InsertNewMomentum() // cemented token receive-blocks
	common.Json(tokenAPI.GetByOwner(g.User2.Address, 0, 5)).HideHashes().Equals(t, `
{
	"count": 1,
	"list": [
		{
			"name": "test.tok3n_na-m3",
			"symbol": "TEST",
			"domain": "",
			"totalSupply": "100",
			"decimals": 1,
			"owner": "z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx",
			"tokenStandard": "zts103tsa5yqngu9cfpj2m0z9u",
			"maxSupply": "1000",
			"isBurnable": false,
			"isMintable": true,
			"isUtility": false
		}
	]
}`)
	z.ExpectBalance(g.User1.Address, customZts, 100)
	defer z.CallContract(&nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     types.TokenContract,
		Data:          definition.ABIToken.PackMethodPanic(definition.BurnMethodName),
		TokenStandard: customZts,
		Amount:        big.NewInt(1),
	}).Error(t, constants.ErrPermissionDenied)
	z.InsertNewMomentum() // cemented send block
	z.InsertNewMomentum() // cemented token receive-blocks
}

// Test the results provided by the token RPCs
// - call RPCs & check the results
// - issue token for User1
// - call the RPCs again & check that the results are up to date
func TestToken_CheckRpc(t *testing.T) {
	z := mock.NewMockZenon(t)
	tokenAPI := embedded.NewTokenApi(z)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).Equals(t, `
t=2001-09-09T01:46:50+0000 lvl=dbug msg="issued ZTS" module=embedded contract=token token="{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenName:test.tok3n_na-m3 TokenSymbol:TEST TokenDomain: TotalSupply:+100 MaxSupply:+1000 Decimals:1 IsMintable:true IsBurnable:true IsUtility:false TokenStandard:zts103tsa5yqngu9cfpj2m0z9u}"
`)

	common.Json(tokenAPI.GetByOwner(g.User1.Address, 0, 10)).Equals(t, `
{
	"count": 0,
	"list": []
}`)
	common.Json(tokenAPI.GetAll(0, 10)).Equals(t, `
{
	"count": 2,
	"list": [
		{
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
		{
			"name": "Zenon Coin",
			"symbol": "ZNN",
			"domain": "zenon.network",
			"totalSupply": "19500000000000",
			"decimals": 8,
			"owner": "z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg",
			"tokenStandard": "zts1znnxxxxxxxxxxxxx9z4ulx",
			"maxSupply": "4611686018427387903",
			"isBurnable": true,
			"isMintable": true,
			"isUtility": true
		}
	]
}`)
	common.Json(tokenAPI.GetByZts(customZts)).Equals(t, "null")

	issueTokenSetup(t, z)

	common.Json(tokenAPI.GetByOwner(g.User1.Address, 0, 10)).Equals(t, `
{
	"count": 1,
	"list": [
		{
			"name": "test.tok3n_na-m3",
			"symbol": "TEST",
			"domain": "",
			"totalSupply": "100",
			"decimals": 1,
			"owner": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
			"tokenStandard": "zts103tsa5yqngu9cfpj2m0z9u",
			"maxSupply": "1000",
			"isBurnable": true,
			"isMintable": true,
			"isUtility": false
		}
	]
}`)
	common.Json(tokenAPI.GetAll(0, 10)).Equals(t, `
{
	"count": 3,
	"list": [
		{
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
		{
			"name": "Zenon Coin",
			"symbol": "ZNN",
			"domain": "zenon.network",
			"totalSupply": "19500000000000",
			"decimals": 8,
			"owner": "z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg",
			"tokenStandard": "zts1znnxxxxxxxxxxxxx9z4ulx",
			"maxSupply": "4611686018427387903",
			"isBurnable": true,
			"isMintable": true,
			"isUtility": true
		},
		{
			"name": "test.tok3n_na-m3",
			"symbol": "TEST",
			"domain": "",
			"totalSupply": "100",
			"decimals": 1,
			"owner": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
			"tokenStandard": "zts103tsa5yqngu9cfpj2m0z9u",
			"maxSupply": "1000",
			"isBurnable": true,
			"isMintable": true,
			"isUtility": false
		}
	]
}`)
	common.Json(tokenAPI.GetByZts(customZts)).Equals(t, `
{
	"name": "test.tok3n_na-m3",
	"symbol": "TEST",
	"domain": "",
	"totalSupply": "100",
	"decimals": 1,
	"owner": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
	"tokenStandard": "zts103tsa5yqngu9cfpj2m0z9u",
	"maxSupply": "1000",
	"isBurnable": true,
	"isMintable": true,
	"isUtility": false
}`)
}

// - issue non-burnable token for User1
// - try to burn non-burnable token from owner address
// - mint non-burnable token from owner address
// - try to burn non-burnable token from non-owner address
func TestToken_BurnTokens(t *testing.T) {
	z := mock.NewMockZenon(t)
	tokenAPI := embedded.NewTokenApi(z)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).Equals(t, `
t=2001-09-09T01:46:50+0000 lvl=dbug msg="issued ZTS" module=embedded contract=token token="{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenName:test.tok3n_na-m3 TokenSymbol:TEST TokenDomain: TotalSupply:+100 MaxSupply:+1000 Decimals:1 IsMintable:true IsBurnable:false IsUtility:false TokenStandard:zts17dc044zt9a2d80ajc2j5my}"
t=2001-09-09T01:47:10+0000 lvl=dbug msg="burned ZTS" module=embedded contract=token token="&{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenName:test.tok3n_na-m3 TokenSymbol:TEST TokenDomain: TotalSupply:+90 MaxSupply:+1000 Decimals:1 IsMintable:true IsBurnable:false IsUtility:false TokenStandard:zts17dc044zt9a2d80ajc2j5my}" burned-amount=10
t=2001-09-09T01:47:30+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenName:test.tok3n_na-m3 TokenSymbol:TEST TokenDomain: TotalSupply:+110 MaxSupply:+1000 Decimals:1 IsMintable:true IsBurnable:false IsUtility:false TokenStandard:zts17dc044zt9a2d80ajc2j5my}" minted-amount=20 to-address=z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx
`)

	// Issue Non-Burnable Token
	defer z.CallContract(&nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     types.TokenContract,
		TokenStandard: types.ZnnTokenStandard,
		Amount:        constants.TokenIssueAmount,
		Data: definition.ABIToken.PackMethodPanic(definition.IssueMethodName,
			"test.tok3n_na-m3", //param.TokenName
			"TEST",             //param.TokenSymbol
			"",                 //param.TokenDomain
			big.NewInt(100),    //param.TotalSupply
			big.NewInt(1000),   //param.MaxSupply
			uint8(1),           //param.Decimals
			true,               //param.IsMintable
			false,              //param.IsBurnable
			false,              //param.IsUtility
		),
	}).Error(t, nil)
	z.InsertNewMomentum() // cemented send block
	z.InsertNewMomentum() // cemented token receive-blocks
	common.Json(tokenAPI.GetByOwner(g.User1.Address, 0, 10)).Equals(t, `
{
	"count": 1,
	"list": [
		{
			"name": "test.tok3n_na-m3",
			"symbol": "TEST",
			"domain": "",
			"totalSupply": "100",
			"decimals": 1,
			"owner": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
			"tokenStandard": "zts17dc044zt9a2d80ajc2j5my",
			"maxSupply": "1000",
			"isBurnable": false,
			"isMintable": true,
			"isUtility": false
		}
	]
}`)
	autoreceive(t, z, g.User1.Address)

	// get customZts of the new token
	tokens, err := tokenAPI.GetByOwner(g.User1.Address, 0, 10)
	common.FailIfErr(t, err)
	customZts := tokens.List[0].ZenonTokenStandard
	z.ExpectBalance(g.User1.Address, customZts, 100)

	// Try to burn non-burnable token from owner address
	defer z.CallContract(&nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     types.TokenContract,
		Data:          definition.ABIToken.PackMethodPanic(definition.BurnMethodName),
		TokenStandard: customZts,
		Amount:        big.NewInt(10),
	}).Error(t, nil)
	z.InsertNewMomentum()
	z.InsertNewMomentum()
	autoreceive(t, z, g.User1.Address)
	z.ExpectBalance(g.User1.Address, customZts, 90)

	// Mint non-burnable token from address 1
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.TokenContract,
		Data:      definition.ABIToken.PackMethodPanic(definition.MintMethodName, customZts, big.NewInt(20), g.User2.Address),
	}).Error(t, nil)
	z.InsertNewMomentum()
	z.InsertNewMomentum()
	autoreceive(t, z, g.User2.Address)
	z.ExpectBalance(g.User2.Address, customZts, 20)

	// Try to burn non-burnable token from non-owner address
	defer z.CallContract(&nom.AccountBlock{
		Address:       g.User2.Address,
		ToAddress:     types.TokenContract,
		Data:          definition.ABIToken.PackMethodPanic(definition.BurnMethodName),
		TokenStandard: customZts,
		Amount:        big.NewInt(10),
	}).Error(t, constants.ErrPermissionDenied)
	z.InsertNewMomentum()
	z.InsertNewMomentum()
	autoreceive(t, z, g.User2.Address)
	z.ExpectBalance(g.User2.Address, customZts, 20)
}

// Test update token - all options
// Try to update from non-owner
// Try to re-unable mint
// Try to burn from non-owner
// Burn from owner
func TestToken_UpdateTokens(t *testing.T) {
	z := mock.NewMockZenon(t)
	tokenAPI := embedded.NewTokenApi(z)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).HideHashes().Equals(t, `
t=2001-09-09T01:46:50+0000 lvl=dbug msg="issued ZTS" module=embedded contract=token token="{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenName:test.tok3n_na-m3 TokenSymbol:TEST TokenDomain: TotalSupply:+100 MaxSupply:+1000 Decimals:1 IsMintable:true IsBurnable:true IsUtility:false TokenStandard:zts103tsa5yqngu9cfpj2m0z9u}"
t=2001-09-09T01:47:30+0000 lvl=dbug msg="updated ZTS" module=embedded contract=token token="&{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenName:test.tok3n_na-m3 TokenSymbol:TEST TokenDomain: TotalSupply:+100 MaxSupply:+1000 Decimals:1 IsMintable:true IsBurnable:true IsUtility:false TokenStandard:zts103tsa5yqngu9cfpj2m0z9u}"
t=2001-09-09T01:47:50+0000 lvl=dbug msg="updating token IsMintable" module=embedded contract=token old=true new=false
t=2001-09-09T01:47:50+0000 lvl=dbug msg="updated ZTS" module=embedded contract=token token="&{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenName:test.tok3n_na-m3 TokenSymbol:TEST TokenDomain: TotalSupply:+100 MaxSupply:+100 Decimals:1 IsMintable:false IsBurnable:true IsUtility:false TokenStandard:zts103tsa5yqngu9cfpj2m0z9u}"
t=2001-09-09T01:49:10+0000 lvl=dbug msg="burned ZTS" module=embedded contract=token token="&{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenName:test.tok3n_na-m3 TokenSymbol:TEST TokenDomain: TotalSupply:+99 MaxSupply:+99 Decimals:1 IsMintable:false IsBurnable:true IsUtility:false TokenStandard:zts103tsa5yqngu9cfpj2m0z9u}" burned-amount=1
t=2001-09-09T01:49:30+0000 lvl=dbug msg="updating token owner" module=embedded contract=token old=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz new=z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx
t=2001-09-09T01:49:30+0000 lvl=dbug msg="updating token IsBurnable" module=embedded contract=token old=true new=false
t=2001-09-09T01:49:30+0000 lvl=dbug msg="updated ZTS" module=embedded contract=token token="&{Owner:z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx TokenName:test.tok3n_na-m3 TokenSymbol:TEST TokenDomain: TotalSupply:+99 MaxSupply:+99 Decimals:1 IsMintable:false IsBurnable:false IsUtility:false TokenStandard:zts103tsa5yqngu9cfpj2m0z9u}"
t=2001-09-09T01:50:30+0000 lvl=dbug msg="burned ZTS" module=embedded contract=token token="&{Owner:z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx TokenName:test.tok3n_na-m3 TokenSymbol:TEST TokenDomain: TotalSupply:+50 MaxSupply:+50 Decimals:1 IsMintable:false IsBurnable:false IsUtility:false TokenStandard:zts103tsa5yqngu9cfpj2m0z9u}" burned-amount=49
`)

	// Issue Token
	issueTokenSetup(t, z)
	common.Json(tokenAPI.GetByOwner(g.User1.Address, 0, 10)).Equals(t, `
{
	"count": 1,
	"list": [
		{
			"name": "test.tok3n_na-m3",
			"symbol": "TEST",
			"domain": "",
			"totalSupply": "100",
			"decimals": 1,
			"owner": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
			"tokenStandard": "zts103tsa5yqngu9cfpj2m0z9u",
			"maxSupply": "1000",
			"isBurnable": true,
			"isMintable": true,
			"isUtility": false
		}
	]
}`)
	autoreceive(t, z, g.User1.Address)
	tokens, err := tokenAPI.GetByOwner(g.User1.Address, 0, 10)
	common.FailIfErr(t, err)
	customZts := tokens.List[0].ZenonTokenStandard
	z.ExpectBalance(g.User1.Address, customZts, 100)

	// No update + Disable mint
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User2.Address,
		ToAddress: types.TokenContract,
		Data: definition.ABIToken.PackMethodPanic(definition.UpdateTokenMethodName,
			customZts,       //param.TokenStandard
			g.User1.Address, //param.Owner
			true,            //param.IsMintable
			true,            //param.IsBurnable
		),
	}).Error(t, constants.ErrPermissionDenied)
	z.InsertNewMomentum() // cemented update block
	z.InsertNewMomentum() // cemented token receive-blocks
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.TokenContract,
		Data: definition.ABIToken.PackMethodPanic(definition.UpdateTokenMethodName,
			customZts,       //param.TokenStandard
			g.User1.Address, //param.Owner
			true,            //param.IsMintable
			true,            //param.IsBurnable
		),
	}).Error(t, nil)
	z.InsertNewMomentum() // cemented update block
	z.InsertNewMomentum() // cemented token receive-blocks
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.TokenContract,
		Data: definition.ABIToken.PackMethodPanic(definition.UpdateTokenMethodName,
			customZts,       //param.TokenStandard
			g.User1.Address, //param.Owner
			false,           //param.IsMintable
			true,            //param.IsBurnable
		),
	}).Error(t, nil)
	z.InsertNewMomentum() // cemented update block
	z.InsertNewMomentum() // cemented token receive-blocks

	// Try to enable mint + Try to mint token to addr1
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.TokenContract,
		Data: definition.ABIToken.PackMethodPanic(definition.UpdateTokenMethodName,
			customZts,       //param.TokenStandard
			g.User1.Address, //param.Owner
			true,            //param.IsMintable
			true,            //param.IsBurnable
		),
	}).Error(t, constants.ErrForbiddenParam)
	z.InsertNewMomentum() // cemented update block
	z.InsertNewMomentum() // cemented token receive-blocks
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.TokenContract,
		Data:      definition.ABIToken.PackMethodPanic(definition.MintMethodName, customZts, big.NewInt(10), g.User1.Address),
	}).Error(t, constants.ErrPermissionDenied)
	z.InsertNewMomentum() // cemented mint block
	z.InsertNewMomentum() // cemented token receive-blocks

	// Transfer to user 3 + Burn
	z.InsertSendBlock(&nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     g.User3.Address,
		TokenStandard: customZts,
		Amount:        big.NewInt(2),
	}, nil, mock.SkipVmChanges)
	z.InsertNewMomentum() // cemented send block
	z.InsertNewMomentum() // cemented token receive-blocks
	autoreceive(t, z, g.User3.Address)
	z.ExpectBalance(g.User3.Address, customZts, 2)

	tokens, err = tokenAPI.GetByOwner(g.User1.Address, 0, 10)
	common.FailIfErr(t, err)
	customZts = tokens.List[0].ZenonTokenStandard
	common.ExpectAmount(t, tokens.List[0].TotalSupply, big.NewInt(100))
	z.ExpectBalance(types.TokenContract, customZts, 0)

	defer z.CallContract(&nom.AccountBlock{
		Address:       g.User3.Address,
		ToAddress:     types.TokenContract,
		Data:          definition.ABIToken.PackMethodPanic(definition.BurnMethodName),
		TokenStandard: customZts,
		Amount:        big.NewInt(1),
	}).Error(t, nil)
	z.InsertNewMomentum() // cemented send block
	z.InsertNewMomentum() // cemented token receive-blocks
	tokens, err = tokenAPI.GetByOwner(g.User1.Address, 0, 10)
	customZts = tokens.List[0].ZenonTokenStandard
	common.FailIfErr(t, err)
	common.ExpectAmount(t, tokens.List[0].TotalSupply, big.NewInt(99))
	z.ExpectBalance(types.TokenContract, customZts, 0)

	// Transfer ownership + disable burn
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.TokenContract,
		Data: definition.ABIToken.PackMethodPanic(definition.UpdateTokenMethodName,
			customZts,       //param.TokenStandard
			g.User2.Address, //param.Owner
			false,           //param.IsMintable
			false,           //param.IsBurnable
		),
	}).Error(t, nil)
	z.InsertNewMomentum() // cemented send block
	z.InsertNewMomentum() // cemented token receive-blocks
	autoreceive(t, z, g.User2.Address)

	// Try to burn from non-owner - balances should not change
	z.ExpectBalance(g.User3.Address, customZts, 1)
	defer z.CallContract(&nom.AccountBlock{
		Address:       g.User3.Address,
		ToAddress:     types.TokenContract,
		Data:          definition.ABIToken.PackMethodPanic(definition.BurnMethodName),
		TokenStandard: customZts,
		Amount:        big.NewInt(1),
	}).Error(t, constants.ErrPermissionDenied)
	z.InsertNewMomentum() // cemented send block
	z.InsertNewMomentum() // cemented token receive-blocks
	autoreceive(t, z, g.User3.Address)
	z.ExpectBalance(g.User1.Address, customZts, 98)
	z.ExpectBalance(g.User3.Address, customZts, 1)

	tokens, err = tokenAPI.GetByOwner(g.User2.Address, 0, 10)
	common.FailIfErr(t, err)
	common.ExpectAmount(t, tokens.List[0].TotalSupply, big.NewInt(99))
	z.ExpectBalance(types.TokenContract, customZts, 0)

	// Transfer to new owner + burn
	z.InsertSendBlock(&nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     g.User2.Address,
		TokenStandard: customZts,
		Amount:        big.NewInt(98),
	}, nil, mock.SkipVmChanges)
	z.InsertNewMomentum() // cemented send block
	z.InsertNewMomentum() // cemented token receive-blocks
	autoreceive(t, z, g.User2.Address)
	z.ExpectBalance(g.User1.Address, customZts, 0)
	z.ExpectBalance(g.User2.Address, customZts, 98)

	defer z.CallContract(&nom.AccountBlock{
		Address:       g.User2.Address,
		ToAddress:     types.TokenContract,
		Data:          definition.ABIToken.PackMethodPanic(definition.BurnMethodName),
		TokenStandard: customZts,
		Amount:        big.NewInt(49),
	}).Error(t, nil)
	z.InsertNewMomentum() // cemented send block
	z.InsertNewMomentum() // cemented token receive-blocks
	z.ExpectBalance(g.User2.Address, customZts, 49)
}

// Test issue & mint & burn all
// - issue token for User1
// - try to mint token from User2
// - mint token from User1 to User2
// - burn all token from addr 1 & 2
func TestToken_IssueMintBurnAll(t *testing.T) {
	z := mock.NewMockZenon(t)
	tokenAPI := embedded.NewTokenApi(z)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).Equals(t, `
t=2001-09-09T01:46:50+0000 lvl=dbug msg="issued ZTS" module=embedded contract=token token="{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenName:test.tok3n_na-m3 TokenSymbol:TEST TokenDomain: TotalSupply:+150 MaxSupply:+1000 Decimals:1 IsMintable:true IsBurnable:true IsUtility:false TokenStandard:zts1k7w8wl0wqjaxhjt6zqa3dd}"
t=2001-09-09T01:47:30+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenName:test.tok3n_na-m3 TokenSymbol:TEST TokenDomain: TotalSupply:+170 MaxSupply:+1000 Decimals:1 IsMintable:true IsBurnable:true IsUtility:false TokenStandard:zts1k7w8wl0wqjaxhjt6zqa3dd}" minted-amount=20 to-address=z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx
t=2001-09-09T01:47:50+0000 lvl=dbug msg="burned ZTS" module=embedded contract=token token="&{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenName:test.tok3n_na-m3 TokenSymbol:TEST TokenDomain: TotalSupply:+20 MaxSupply:+1000 Decimals:1 IsMintable:true IsBurnable:true IsUtility:false TokenStandard:zts1k7w8wl0wqjaxhjt6zqa3dd}" burned-amount=150
t=2001-09-09T01:48:10+0000 lvl=dbug msg="burned ZTS" module=embedded contract=token token="&{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenName:test.tok3n_na-m3 TokenSymbol:TEST TokenDomain: TotalSupply:+0 MaxSupply:+1000 Decimals:1 IsMintable:true IsBurnable:true IsUtility:false TokenStandard:zts1k7w8wl0wqjaxhjt6zqa3dd}" burned-amount=20
`)

	// issue token for User1
	defer z.CallContract(&nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     types.TokenContract,
		TokenStandard: types.ZnnTokenStandard,
		Amount:        constants.TokenIssueAmount,
		Data: definition.ABIToken.PackMethodPanic(definition.IssueMethodName,
			"test.tok3n_na-m3", //param.TokenName
			"TEST",             //param.TokenSymbol
			"",                 //param.TokenDomain
			big.NewInt(150),    //param.TotalSupply
			big.NewInt(1000),   //param.MaxSupply
			uint8(1),           //param.Decimals
			true,               //param.IsMintable
			true,               //param.IsBurnable
			false,              //param.IsUtility
		),
	}).Error(t, nil)
	z.InsertNewMomentum() // cemented send block
	z.InsertNewMomentum() // cemented token-receive-block
	autoreceive(t, z, g.User1.Address)
	// get customZts of the new token
	tokens, err := tokenAPI.GetByOwner(g.User1.Address, 0, 10)
	common.FailIfErr(t, err)
	customZts := tokens.List[0].ZenonTokenStandard
	z.ExpectBalance(g.User1.Address, customZts, 150)

	// try to mint token from User2
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User2.Address,
		ToAddress: types.TokenContract,
		Data:      definition.ABIToken.PackMethodPanic(definition.MintMethodName, customZts, big.NewInt(20), g.User2.Address),
	}).Error(t, constants.ErrPermissionDenied)
	z.InsertNewMomentum() // cemented send block
	z.InsertNewMomentum() // cemented token-receive-block

	// mint token from User1
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.TokenContract,
		Data:      definition.ABIToken.PackMethodPanic(definition.MintMethodName, customZts, big.NewInt(20), g.User2.Address),
	}).Error(t, nil)
	z.InsertNewMomentum() // cemented send block
	z.InsertNewMomentum() // cemented token-receive-block
	autoreceive(t, z, g.User2.Address)
	z.ExpectBalance(g.User2.Address, customZts, 20)

	// Burn all token from addr 1 & 2
	defer z.CallContract(&nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     types.TokenContract,
		Data:          definition.ABIToken.PackMethodPanic(definition.BurnMethodName),
		TokenStandard: customZts,
		Amount:        big.NewInt(150),
	}).Error(t, nil)
	z.InsertNewMomentum() // cemented send bloeck
	z.InsertNewMomentum() // cemented token-receive-block
	z.ExpectBalance(g.User1.Address, customZts, 0)
	defer z.CallContract(&nom.AccountBlock{
		Address:       g.User2.Address,
		ToAddress:     types.TokenContract,
		Data:          definition.ABIToken.PackMethodPanic(definition.BurnMethodName),
		TokenStandard: customZts,
		Amount:        big.NewInt(20),
	}).Error(t, nil)
	z.InsertNewMomentum() // cemented send block
	z.InsertNewMomentum() // cemented token-receive-block
	z.ExpectBalance(g.User2.Address, customZts, 0)

	common.Json(tokenAPI.GetByZts(customZts)).Equals(t, `
{
	"name": "test.tok3n_na-m3",
	"symbol": "TEST",
	"domain": "",
	"totalSupply": "0",
	"decimals": 1,
	"owner": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
	"tokenStandard": "zts1k7w8wl0wqjaxhjt6zqa3dd",
	"maxSupply": "1000",
	"isBurnable": true,
	"isMintable": true,
	"isUtility": false
}`)
}
