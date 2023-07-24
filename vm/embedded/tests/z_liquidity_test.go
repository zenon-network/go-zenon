package tests

import (
	"encoding/base64"
	"fmt"
	"github.com/zenon-network/go-zenon/rpc/api/embedded"
	"math/big"
	"testing"
	"time"

	g "github.com/zenon-network/go-zenon/chain/genesis/mock"
	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/vm/constants"
	"github.com/zenon-network/go-zenon/vm/embedded/definition"
	"github.com/zenon-network/go-zenon/zenon/mock"
)

var (
	tokens       []types.ZenonTokenStandard
	tokensString []string
)

func issueMultipleTokensSetup(t *testing.T, z mock.MockZenon) {
	tokenAPI := embedded.NewTokenApi(z)
	defer z.CallContract(&nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     types.TokenContract,
		TokenStandard: types.ZnnTokenStandard,
		Amount:        constants.TokenIssueAmount,
		Data: definition.ABIToken.PackMethodPanic(definition.IssueMethodName,
			"test.tok3n_liquidity-1", //param.TokenName
			"LIQ1",                   //param.TokenSymbol
			"",                       //param.TokenDomain
			big.NewInt(100*g.Zexp),   //param.TotalSupply
			big.NewInt(1000*g.Zexp),  //param.MaxSupply
			uint8(6),                 //param.Decimals
			true,                     //param.IsMintable
			true,                     //param.IsBurnable
			false,                    //param.IsUtility
		),
	}).Error(t, nil)
	z.InsertNewMomentum() // cemented send block
	z.InsertNewMomentum() // cemented token-receive-block

	defer z.CallContract(&nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     types.TokenContract,
		TokenStandard: types.ZnnTokenStandard,
		Amount:        constants.TokenIssueAmount,
		Data: definition.ABIToken.PackMethodPanic(definition.IssueMethodName,
			"test.tok3n_liquidity-2", //param.TokenName
			"LIQ2",                   //param.TokenSymbol
			"",                       //param.TokenDomain
			big.NewInt(200*g.Zexp),   //param.TotalSupply
			big.NewInt(2000*g.Zexp),  //param.MaxSupply
			uint8(6),                 //param.Decimals
			true,                     //param.IsMintable
			true,                     //param.IsBurnable
			false,                    //param.IsUtility
		),
	}).Error(t, nil)
	z.InsertNewMomentum() // cemented send block
	z.InsertNewMomentum() // cemented token-receive-block

	tokenList, err := tokenAPI.GetByOwner(g.User1.Address, 0, 10)
	common.FailIfErr(t, err)

	tokens = make([]types.ZenonTokenStandard, 0)
	tokensString = make([]string, 0)
	for _, zts := range tokenList.List {
		tokens = append(tokens, zts.ZenonTokenStandard)
		tokensString = append(tokensString, zts.ZenonTokenStandard.String())
	}

	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.TokenContract,
		Data:      definition.ABIToken.PackMethodPanic(definition.MintMethodName, tokens[0], big.NewInt(100*g.Zexp), g.User1.Address),
	}).Error(t, nil)
	z.InsertNewMomentum() // cemented send block
	z.InsertNewMomentum() // cemented token-receive-block
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.TokenContract,
		Data:      definition.ABIToken.PackMethodPanic(definition.MintMethodName, tokens[1], big.NewInt(200*g.Zexp), g.User1.Address),
	}).Error(t, nil)
	z.InsertNewMomentum() // cemented send block
	z.InsertNewMomentum() // cemented token-receive-block
	autoreceive(t, z, g.User1.Address)
}

// activate accelerator spork
// activate bridge spork
func activateLiquidityStep0(t *testing.T, z mock.MockZenon) {
	activateAccelerator(z)
	activateBridge(z)

	constants.InitialBridgeAdministrator = g.User5.Address
	constants.MinGuardians = 4
	constants.MinAdministratorDelay = 20
	constants.MinSoftDelay = 10
	constants.MomentumsPerEpoch = 10
	constants.MinUnhaltDurationInMomentums = 5

	z.InsertMomentumsTo(500)
	z.ExpectBalance(types.LiquidityContract, types.ZnnTokenStandard, 187200000000)
	z.ExpectBalance(types.LiquidityContract, types.QsrTokenStandard, 500000000000)
}

// activate accelerator spork
// activate bridge spork
// nominate guardians
func activateLiquidityStep1(t *testing.T, z mock.MockZenon) {
	activateLiquidityStep0(t, z)

	liquidityApi := embedded.NewLiquidityApi(z)
	securityInfo, err := liquidityApi.GetSecurityInfo()
	common.DealWithErr(err)
	guardians := []types.Address{g.User1.Address, g.User2.Address, g.User3.Address, g.User4.Address}
	nominateGuardiansLiq(t, z, g.User5.Address, guardians, securityInfo.AdministratorDelay)

	common.Json(liquidityApi.GetSecurityInfo()).Equals(t, `
{
	"guardians": [
		"z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx",
		"z1qraz4ermhhua89a0h0gxxan4lnzrfutgs6xxe2",
		"z1qrs2lpccnsneglhnnfwvlsj0qncnxjnwlfmjac",
		"z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz"
	],
	"guardiansVotes": [
		"z1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqsggv2f",
		"z1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqsggv2f",
		"z1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqsggv2f",
		"z1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqsggv2f"
	],
	"administratorDelay": 20,
	"softDelay": 10
}`)
}

// activate accelerator spork
// activate bridge spork
// set token tuples for 2 tokens
func activateLiquidityStep2(t *testing.T, z mock.MockZenon) {
	activateLiquidityStep1(t, z)

	issueMultipleTokensSetup(t, z)

	liquidityAPI := embedded.NewLiquidityApi(z)
	common.Json(liquidityAPI.GetTimeChallengesInfo()).Equals(t, `
{
	"count": 1,
	"list": [
		{
			"MethodName": "NominateGuardians",
			"ParamsHash": "0000000000000000000000000000000000000000000000000000000000000000",
			"ChallengeStartHeight": 501
		}
	]
}`)

	percentages := []uint32{5000, 5000}
	minAmounts := []*big.Int{big.NewInt(1000), big.NewInt(2000)}
	setTokensTuple(t, z, g.User5.Address, tokensString, percentages, percentages, minAmounts)
	common.Json(liquidityAPI.GetTimeChallengesInfo()).Equals(t, `
{
	"count": 2,
	"list": [
		{
			"MethodName": "NominateGuardians",
			"ParamsHash": "0000000000000000000000000000000000000000000000000000000000000000",
			"ChallengeStartHeight": 501
		},
		{
			"MethodName": "SetTokenTuple",
			"ParamsHash": "0000000000000000000000000000000000000000000000000000000000000000",
			"ChallengeStartHeight": 535
		}
	]
}`)
	common.Json(liquidityAPI.GetLiquidityInfo()).Equals(t, `
{
	"administrator": "z1qqaswvt0e3cc5sm7lygkyza9ra63cr8e6zre09",
	"isHalted": false,
	"znnReward": "0",
	"qsrReward": "0",
	"tokenTuples": [
		{
			"tokenStandard": "zts1992nq43xn2urz3wttklc8z",
			"znnPercentage": 5000,
			"qsrPercentage": 5000,
			"minAmount": "1000"
		},
		{
			"tokenStandard": "zts1fres5c39axw805xswvt55j",
			"znnPercentage": 5000,
			"qsrPercentage": 5000,
			"minAmount": "2000"
		}
	]
}`)
}

// activate accelerator spork
// activate bridge spork
// set token tuples for 2 tokens
// stake 1 entry with each token
func activateLiquidityStep3(t *testing.T, z mock.MockZenon) {
	activateLiquidityStep2(t, z)

	defer z.CallContract(liquidityStake(g.User1.Address, tokens[0], big.NewInt(10*g.Zexp), 1)).Error(t, nil)
	insertMomentums(z, 2)

	defer z.CallContract(liquidityStake(g.User1.Address, tokens[1], big.NewInt(30*g.Zexp), 3)).Error(t, nil)
	insertMomentums(z, 2)

	liquidityAPI := embedded.NewLiquidityApi(z)
	common.Json(liquidityAPI.GetLiquidityStakeEntriesByAddress(g.User1.Address, 0, 5)).Equals(t, `
{
	"totalAmount": "4000000000",
	"totalWeightedAmount": "10000000000",
	"count": 2,
	"list": [
		{
			"amount": "1000000000",
			"tokenStandard": "zts1992nq43xn2urz3wttklc8z",
			"weightedAmount": "1000000000",
			"startTime": 1000005480,
			"revokeTime": 0,
			"expirationTime": 1000009080,
			"stakeAddress": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
			"id": "62690006c58c67dc5b7c41e095a22e40307eba0463cee5585f84b91f6815a5b1"
		},
		{
			"amount": "3000000000",
			"tokenStandard": "zts1fres5c39axw805xswvt55j",
			"weightedAmount": "9000000000",
			"startTime": 1000005500,
			"revokeTime": 0,
			"expirationTime": 1000016300,
			"stakeAddress": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
			"id": "3de102aa795d705f1183c3422f8139983bbbcf398d3b60c848f7de27defdf4ea"
		}
	]
}`)
}

// activate accelerator spork
// activate bridge spork
// set token tuples for 2 tokens
// stake 1 entry with each token
// collect rewards
func activateLiquidityStep4(t *testing.T, z mock.MockZenon) {
	activateLiquidityStep3(t, z)

	insertMomentums(z, 60*3)

	liquidityAPI := embedded.NewLiquidityApi(z)

	z.ExpectBalance(g.User1.Address, types.ZnnTokenStandard, 11998*g.Zexp)
	z.ExpectBalance(g.User1.Address, types.QsrTokenStandard, 120000*g.Zexp)
	common.Json(liquidityAPI.GetUncollectedReward(g.User1.Address)).Equals(t, `
{
	"address": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
	"znnAmount": "187200000000",
	"qsrAmount": "500000000000"
}`)

	z.ExpectBalance(types.LiquidityContract, types.ZnnTokenStandard, 1872*g.Zexp)
	z.ExpectBalance(types.LiquidityContract, types.QsrTokenStandard, 5000*g.Zexp)
	defer z.CallContract(collectReward(g.User1.Address)).Error(t, nil)
	insertMomentums(z, 3)
	z.ExpectBalance(types.LiquidityContract, types.ZnnTokenStandard, 1872*g.Zexp)
	z.ExpectBalance(types.LiquidityContract, types.QsrTokenStandard, 5000*g.Zexp)

	autoreceive(t, z, g.User1.Address)
	insertMomentums(z, 2)

	z.ExpectBalance(g.User1.Address, types.ZnnTokenStandard, 13870*g.Zexp)
	z.ExpectBalance(g.User1.Address, types.QsrTokenStandard, 125000*g.Zexp)
}

// activate accelerator spork
// activate bridge spork
// set token tuples for 2 tokens
// stake 1 entry with each token
// collect rewards
// set additional rewards
func activateLiquidityStep5(t *testing.T, z mock.MockZenon) {
	activateLiquidityStep4(t, z)

	liquidityAPI := embedded.NewLiquidityApi(z)
	setAdditionalReward(t, z, g.User5.Address, big.NewInt(100*g.Zexp), big.NewInt(1000*g.Zexp))
	common.Json(liquidityAPI.GetLiquidityInfo()).Equals(t, `
{
	"administrator": "z1qqaswvt0e3cc5sm7lygkyza9ra63cr8e6zre09",
	"isHalted": false,
	"znnReward": "10000000000",
	"qsrReward": "100000000000",
	"tokenTuples": [
		{
			"tokenStandard": "zts1992nq43xn2urz3wttklc8z",
			"znnPercentage": 5000,
			"qsrPercentage": 5000,
			"minAmount": "1000"
		},
		{
			"tokenStandard": "zts1fres5c39axw805xswvt55j",
			"znnPercentage": 5000,
			"qsrPercentage": 5000,
			"minAmount": "2000"
		}
	]
}`)
}

// activate accelerator spork
// activate bridge spork
// set token tuples for 2 tokens
// stake 1 entry with each token
// collect rewards
// set additional rewards
// collect additional rewards
func activateLiquidityStep6(t *testing.T, z mock.MockZenon) {
	activateLiquidityStep5(t, z)

	insertMomentums(z, 500)

	liquidityAPI := embedded.NewLiquidityApi(z)

	z.ExpectBalance(g.User1.Address, types.ZnnTokenStandard, 13870*g.Zexp)
	z.ExpectBalance(g.User1.Address, types.QsrTokenStandard, 125000*g.Zexp)
	common.Json(liquidityAPI.GetUncollectedReward(g.User1.Address)).Equals(t, `
{
	"address": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
	"znnAmount": "197200000000",
	"qsrAmount": "600000000000"
}`)

	// the amount of 100 was burned
	z.ExpectBalance(types.LiquidityContract, types.ZnnTokenStandard, 1772*g.Zexp)
	// the amount of 1000 was burned
	z.ExpectBalance(types.LiquidityContract, types.QsrTokenStandard, 4000*g.Zexp)
	defer z.CallContract(collectReward(g.User1.Address)).Error(t, nil)
	insertMomentums(z, 3)
	z.ExpectBalance(types.LiquidityContract, types.ZnnTokenStandard, 1772*g.Zexp)
	z.ExpectBalance(types.LiquidityContract, types.QsrTokenStandard, 4000*g.Zexp)

	autoreceive(t, z, g.User1.Address)
	insertMomentums(z, 2)

	z.ExpectBalance(g.User1.Address, types.ZnnTokenStandard, 1584200000000)
	z.ExpectBalance(g.User1.Address, types.QsrTokenStandard, 131000*g.Zexp)
}

// activate accelerator spork
// activate bridge spork
// set token tuples for 2 tokens
// stake 1 entry with each token
// collect rewards
// set additional rewards
// collect additional rewards
// revoke stakes
func activateLiquidityStep7(t *testing.T, z mock.MockZenon) {
	activateLiquidityStep6(t, z)

	insertMomentums(z, 500)

	liquidityAPI := embedded.NewLiquidityApi(z)

	stakes, err := liquidityAPI.GetLiquidityStakeEntriesByAddress(g.User1.Address, 0, 5)
	common.FailIfErr(t, err)
	common.Json(stakes, err).Equals(t, `
{
	"totalAmount": "4000000000",
	"totalWeightedAmount": "10000000000",
	"count": 2,
	"list": [
		{
			"amount": "1000000000",
			"tokenStandard": "zts1992nq43xn2urz3wttklc8z",
			"weightedAmount": "1000000000",
			"startTime": 1000005480,
			"revokeTime": 0,
			"expirationTime": 1000009080,
			"stakeAddress": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
			"id": "62690006c58c67dc5b7c41e095a22e40307eba0463cee5585f84b91f6815a5b1"
		},
		{
			"amount": "3000000000",
			"tokenStandard": "zts1fres5c39axw805xswvt55j",
			"weightedAmount": "9000000000",
			"startTime": 1000005500,
			"revokeTime": 0,
			"expirationTime": 1000016300,
			"stakeAddress": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
			"id": "3de102aa795d705f1183c3422f8139983bbbcf398d3b60c848f7de27defdf4ea"
		}
	]
}`)

	defer z.CallContract(cancelStakeLiq(g.User1.Address, stakes.Entries[0].Id)).Error(t, nil)
	insertMomentums(z, 2)

	defer z.CallContract(cancelStakeLiq(g.User1.Address, stakes.Entries[1].Id)).Error(t, nil)
	insertMomentums(z, 2)

	z.ExpectBalance(g.User1.Address, stakes.Entries[0].TokenStandard, 190*g.Zexp)
	z.ExpectBalance(g.User1.Address, stakes.Entries[1].TokenStandard, 370*g.Zexp)
	autoreceive(t, z, g.User1.Address)
	z.ExpectBalance(g.User1.Address, stakes.Entries[0].TokenStandard, 200*g.Zexp)
	z.ExpectBalance(g.User1.Address, stakes.Entries[1].TokenStandard, 400*g.Zexp)

	stakes, err = liquidityAPI.GetLiquidityStakeEntriesByAddress(g.User1.Address, 0, 5)
	common.FailIfErr(t, err)
	common.Json(stakes, err).Equals(t, `
{
	"totalAmount": "0",
	"totalWeightedAmount": "0",
	"count": 0,
	"list": []
}`)
}

// activate accelerator spork
// activate bridge spork
// set token tuples for 2 tokens
// stake 1 entry with each token
// collect rewards
// set additional rewards
// collect additional rewards
// revoke stakes
// remove additional rewards
func activateLiquidityStep8(t *testing.T, z mock.MockZenon) {
	activateLiquidityStep7(t, z)

	liquidityAPI := embedded.NewLiquidityApi(z)
	setAdditionalReward(t, z, g.User5.Address, big.NewInt(0), big.NewInt(0))
	common.Json(liquidityAPI.GetLiquidityInfo()).Equals(t, `
{
	"administrator": "z1qqaswvt0e3cc5sm7lygkyza9ra63cr8e6zre09",
	"isHalted": false,
	"znnReward": "0",
	"qsrReward": "0",
	"tokenTuples": [
		{
			"tokenStandard": "zts1992nq43xn2urz3wttklc8z",
			"znnPercentage": 5000,
			"qsrPercentage": 5000,
			"minAmount": "1000"
		},
		{
			"tokenStandard": "zts1fres5c39axw805xswvt55j",
			"znnPercentage": 5000,
			"qsrPercentage": 5000,
			"minAmount": "2000"
		}
	]
}`)

	defer z.CallContract(liquidityStake(g.User1.Address, tokens[0], big.NewInt(100*g.Zexp), 4)).Error(t, nil)
	insertMomentums(z, 2)
	defer z.CallContract(liquidityStake(g.User1.Address, tokens[1], big.NewInt(100*g.Zexp), 4)).Error(t, nil)
	insertMomentums(z, 2)
	common.Json(liquidityAPI.GetUncollectedReward(g.User1.Address)).Equals(t, `
{
	"address": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
	"znnAmount": "197200000000",
	"qsrAmount": "600000000000"
}`)

	z.ExpectBalance(types.LiquidityContract, types.ZnnTokenStandard, 1672*g.Zexp)
	z.ExpectBalance(types.LiquidityContract, types.QsrTokenStandard, 3000*g.Zexp)

	insertMomentums(z, 500)

	common.Json(liquidityAPI.GetUncollectedReward(g.User1.Address)).Equals(t, `
{
	"address": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
	"znnAmount": "571599999998",
	"qsrAmount": "1599999999998"
}`)

	// balance should stay the same
	z.ExpectBalance(types.LiquidityContract, types.ZnnTokenStandard, 1672*g.Zexp+2)
	z.ExpectBalance(types.LiquidityContract, types.QsrTokenStandard, 3000*g.Zexp+2)
}

func TestLiquidity(t *testing.T) {
	z := mock.NewMockZenonWithCustomEpochDuration(t, time.Hour)
	defer z.StopPanic()

	activateLiquidityStep8(t, z)
}

func TestLiquidity_Burn(t *testing.T) {
	z := mock.NewMockZenonWithCustomEpochDuration(t, time.Hour)
	defer z.StopPanic()
	//defer z.SaveLogs(common.EmbeddedLogger).HideHashes().Equals(t, ``)

	activateAccelerator(z)

	// try to burn more than balance
	z.ExpectBalance(types.LiquidityContract, types.ZnnTokenStandard, 0)
	defer z.CallContract(burnLiq(g.Spork.Address, common.Big1)).Error(t, constants.ErrInvalidTokenOrAmount)
	insertMomentums(z, 2)

	z.InsertMomentumsTo(60*6 + 4)

	z.ExpectBalance(types.LiquidityContract, types.ZnnTokenStandard, 187200000000)
	z.ExpectBalance(types.LiquidityContract, types.QsrTokenStandard, 500000000000)

	defer z.CallContract(burnLiq(g.Spork.Address, common.Big1)).Error(t, nil)
	insertMomentums(z, 2)

	z.ExpectBalance(types.LiquidityContract, types.ZnnTokenStandard, 187200000000-1)
	z.ExpectBalance(types.LiquidityContract, types.QsrTokenStandard, 500000000000)

	// try to burn from non spork
	z.InsertSendBlock(burnLiq(g.User1.Address, common.Big1), constants.ErrPermissionDenied, mock.SkipVmChanges)
	insertMomentums(z, 2)

	// test with bridge activated
	activateBridge(z)

	defer z.CallContract(burnLiq(g.Spork.Address, big.NewInt(187200000000))).Error(t, constants.ErrInvalidTokenOrAmount)
	insertMomentums(z, 2)

	z.InsertSendBlock(burnLiq(g.User1.Address, common.Big1), constants.ErrPermissionDenied, mock.SkipVmChanges)
	insertMomentums(z, 2)

	defer z.CallContract(burnLiq(g.Spork.Address, common.Big1)).Error(t, nil)
	insertMomentums(z, 2)

	z.ExpectBalance(types.LiquidityContract, types.ZnnTokenStandard, 187200000000-2)
}

func TestLiquidity_Fund(t *testing.T) {
	z := mock.NewMockZenonWithCustomEpochDuration(t, time.Hour)
	defer z.StopPanic()
	activateAccelerator(z)

	// non spork with no balance
	z.InsertSendBlock(fundLiq(g.User1.Address, common.Big100, common.Big100), constants.ErrPermissionDenied, mock.SkipVmChanges)
	insertMomentums(z, 2)

	insertMomentums(z, 60*6+2)

	z.ExpectBalance(types.LiquidityContract, types.ZnnTokenStandard, 187200000000)
	z.ExpectBalance(types.LiquidityContract, types.QsrTokenStandard, 500000000000)

	// non spork with balance
	z.InsertSendBlock(fundLiq(g.User1.Address, common.Big100, common.Big100), constants.ErrPermissionDenied, mock.SkipVmChanges)
	insertMomentums(z, 2)

	defer z.CallContract(fundLiq(g.Spork.Address, big.NewInt(187200000000+1), common.Big0)).
		Error(t, constants.ErrInvalidTokenOrAmount)
	insertMomentums(z, 2)

	defer z.CallContract(fundLiq(g.Spork.Address, common.Big0, big.NewInt(500000000000+1))).
		Error(t, constants.ErrInvalidTokenOrAmount)
	insertMomentums(z, 2)

	defer z.CallContract(fundLiq(g.Spork.Address, big.NewInt(187200000000+1), big.NewInt(500000000000+1))).
		Error(t, constants.ErrInvalidTokenOrAmount)
	insertMomentums(z, 2)

	insertMomentums(z, 2)

	z.InsertSendBlock(fundLiq(g.Spork.Address, big.NewInt(100*g.Zexp), big.NewInt(1000*g.Zexp)), nil, mock.SkipVmChanges)
	insertMomentums(z, 2)

	z.ExpectBalance(types.LiquidityContract, types.ZnnTokenStandard, 1872*g.Zexp-100*g.Zexp)
	z.ExpectBalance(types.LiquidityContract, types.QsrTokenStandard, 5000*g.Zexp-1000*g.Zexp)

	// also test for bridge
	activateBridge(z)

	// non spork with balance
	z.InsertSendBlock(fundLiq(g.User1.Address, common.Big100, common.Big100), constants.ErrPermissionDenied, mock.SkipVmChanges)
	insertMomentums(z, 2)

	defer z.CallContract(fundLiq(g.Spork.Address, big.NewInt(187200000000+1), common.Big0)).
		Error(t, constants.ErrInvalidTokenOrAmount)
	insertMomentums(z, 2)

	defer z.CallContract(fundLiq(g.Spork.Address, common.Big0, big.NewInt(500000000000+1))).
		Error(t, constants.ErrInvalidTokenOrAmount)
	insertMomentums(z, 2)

	defer z.CallContract(fundLiq(g.Spork.Address, big.NewInt(187200000000+1), big.NewInt(500000000000+1))).
		Error(t, constants.ErrInvalidTokenOrAmount)
	insertMomentums(z, 2)

	insertMomentums(z, 2)

	z.InsertSendBlock(fundLiq(g.Spork.Address, big.NewInt(100*g.Zexp), big.NewInt(1000*g.Zexp)), nil, mock.SkipVmChanges)
	insertMomentums(z, 2)

	z.ExpectBalance(types.LiquidityContract, types.ZnnTokenStandard, 1872*g.Zexp-200*g.Zexp)
	z.ExpectBalance(types.LiquidityContract, types.QsrTokenStandard, 5000*g.Zexp-2000*g.Zexp)

	z.ExpectBalance(types.AcceleratorContract, types.ZnnTokenStandard, 200*g.Zexp)
	z.ExpectBalance(types.AcceleratorContract, types.QsrTokenStandard, 2000*g.Zexp)
}

func TestLiquidity_SetTokenTuples(t *testing.T) {
	z := mock.NewMockZenonWithCustomEpochDuration(t, time.Hour)
	defer z.StopPanic()
	//defer z.SaveLogs(common.EmbeddedLogger).Equals(t, ``)

	// only sporks
	activateLiquidityStep0(t, z)

	issueMultipleTokensSetup(t, z)

	// try to set tuples without guardians
	znnPercentages := []uint32{5000, 5000}
	qsrPercentages := []uint32{5000, 5000}
	minAmounts := []*big.Int{big.NewInt(1000), big.NewInt(2000)}
	defer z.CallContract(setTokensTupleStep(g.User5.Address, tokensString, znnPercentages, qsrPercentages, minAmounts)).
		Error(t, constants.ErrSecurityNotInitialized)
	insertMomentums(z, 2)

	liquidityAPI := embedded.NewLiquidityApi(z)
	securityInfo, err := liquidityAPI.GetSecurityInfo()
	common.FailIfErr(t, err)
	guardians := []types.Address{g.User1.Address, g.User2.Address, g.User3.Address, g.User4.Address}
	nominateGuardiansLiq(t, z, g.User5.Address, guardians, securityInfo.AdministratorDelay)

	// invalid percentages
	// wrong znn more
	znnPercentages[0] = 6000
	z.InsertSendBlock(setTokensTupleStep(g.User5.Address, tokensString, znnPercentages, qsrPercentages, minAmounts),
		constants.ErrInvalidPercentages, mock.SkipVmChanges)
	znnPercentages[0] = 5000

	// wrong qsr - less
	qsrPercentages[0] = 3000
	z.InsertSendBlock(setTokensTupleStep(g.User5.Address, tokensString, znnPercentages, qsrPercentages, minAmounts),
		constants.ErrInvalidPercentages, mock.SkipVmChanges)

	// both less
	znnPercentages[0] = 0
	z.InsertSendBlock(setTokensTupleStep(g.User5.Address, tokensString, znnPercentages, qsrPercentages, minAmounts),
		constants.ErrInvalidPercentages, mock.SkipVmChanges)

	// different arrays length
	tokensString = append(tokensString, "asd")
	z.InsertSendBlock(setTokensTupleStep(g.User5.Address, tokensString, znnPercentages, qsrPercentages, minAmounts),
		constants.ErrForbiddenParam, mock.SkipVmChanges)
	tokensString = tokensString[:2]

	znnPercentages = append(znnPercentages, 3)
	z.InsertSendBlock(setTokensTupleStep(g.User5.Address, tokensString, znnPercentages, qsrPercentages, minAmounts),
		constants.ErrForbiddenParam, mock.SkipVmChanges)
	znnPercentages = znnPercentages[:2]

	qsrPercentages = append(qsrPercentages, 3000)
	z.InsertSendBlock(setTokensTupleStep(g.User5.Address, tokensString, znnPercentages, qsrPercentages, minAmounts),
		constants.ErrForbiddenParam, mock.SkipVmChanges)
	qsrPercentages = qsrPercentages[:2]

	// duplicate token
	newTokensString := []string{tokensString[0], tokensString[0]}
	z.InsertSendBlock(setTokensTupleStep(g.User5.Address, newTokensString, znnPercentages, qsrPercentages, minAmounts),
		constants.ErrForbiddenParam, mock.SkipVmChanges)
	insertMomentums(z, 2)

	znnPercentages[0] = 5000
	qsrPercentages[0] = 5000

	// non admin
	defer z.CallContract(setTokensTupleStep(g.User4.Address, tokensString, znnPercentages, qsrPercentages, minAmounts)).
		Error(t, constants.ErrPermissionDenied)
	insertMomentums(z, 2)

	// should work
	defer z.CallContract(setTokensTupleStep(g.User5.Address, tokensString, znnPercentages, qsrPercentages, minAmounts)).
		Error(t, nil)
	insertMomentums(z, 2)

	// time challenge not due
	defer z.CallContract(setTokensTupleStep(g.User5.Address, tokensString, znnPercentages, qsrPercentages, minAmounts)).
		Error(t, constants.ErrTimeChallengeNotDue)
	insertMomentums(z, int(securityInfo.AdministratorDelay))

	defer z.CallContract(setTokensTupleStep(g.User5.Address, tokensString, znnPercentages, qsrPercentages, minAmounts)).
		Error(t, nil)
	insertMomentums(z, 2)

	common.Json(liquidityAPI.GetLiquidityInfo()).Equals(t, `
{
	"administrator": "z1qqaswvt0e3cc5sm7lygkyza9ra63cr8e6zre09",
	"isHalted": false,
	"znnReward": "0",
	"qsrReward": "0",
	"tokenTuples": [
		{
			"tokenStandard": "zts1xflhun39kllr8vw67y8gw9",
			"znnPercentage": 5000,
			"qsrPercentage": 5000,
			"minAmount": "1000"
		},
		{
			"tokenStandard": "zts1g9zjdnwc28hg3zx954d7hw",
			"znnPercentage": 5000,
			"qsrPercentage": 5000,
			"minAmount": "2000"
		}
	]
}`)

	znnPercentages = []uint32{0, 10000}
	qsrPercentages = []uint32{10000, 0}
	setTokensTuple(t, z, g.User5.Address, tokensString, znnPercentages, qsrPercentages, minAmounts)

	common.Json(liquidityAPI.GetLiquidityInfo()).Equals(t, `
{
	"administrator": "z1qqaswvt0e3cc5sm7lygkyza9ra63cr8e6zre09",
	"isHalted": false,
	"znnReward": "0",
	"qsrReward": "0",
	"tokenTuples": [
		{
			"tokenStandard": "zts1xflhun39kllr8vw67y8gw9",
			"znnPercentage": 0,
			"qsrPercentage": 10000,
			"minAmount": "1000"
		},
		{
			"tokenStandard": "zts1g9zjdnwc28hg3zx954d7hw",
			"znnPercentage": 10000,
			"qsrPercentage": 0,
			"minAmount": "2000"
		}
	]
}`)

	// zero token standard
	newTokensString = []string{tokensString[0], types.ZeroTokenStandard.String()}
	z.InsertSendBlock(setTokensTupleStep(g.User5.Address, newTokensString, znnPercentages, qsrPercentages, minAmounts),
		constants.ErrForbiddenParam, mock.SkipVmChanges)

	// delete all
	defer z.CallContract(setTokensTupleStep(g.User5.Address, []string{}, []uint32{}, []uint32{}, []*big.Int{})).
		Error(t, nil)
	insertMomentums(z, int(securityInfo.AdministratorDelay+2))

	defer z.CallContract(setTokensTupleStep(g.User5.Address, []string{}, []uint32{}, []uint32{}, []*big.Int{})).
		Error(t, nil)
	insertMomentums(z, 2)

	common.Json(liquidityAPI.GetLiquidityInfo()).Equals(t, `
{
	"administrator": "z1qqaswvt0e3cc5sm7lygkyza9ra63cr8e6zre09",
	"isHalted": false,
	"znnReward": "0",
	"qsrReward": "0",
	"tokenTuples": []
}`)

	// reset them
	znnPercentages = []uint32{1, 9999}
	qsrPercentages = []uint32{9999, 1}
	defer z.CallContract(setTokensTupleStep(g.User5.Address, tokensString, znnPercentages, qsrPercentages, minAmounts)).
		Error(t, nil)
	insertMomentums(z, int(2+securityInfo.AdministratorDelay))

	defer z.CallContract(setTokensTupleStep(g.User5.Address, tokensString, znnPercentages, qsrPercentages, minAmounts)).
		Error(t, nil)
	insertMomentums(z, 2)

	common.Json(liquidityAPI.GetLiquidityInfo()).Equals(t, `
{
	"administrator": "z1qqaswvt0e3cc5sm7lygkyza9ra63cr8e6zre09",
	"isHalted": false,
	"znnReward": "0",
	"qsrReward": "0",
	"tokenTuples": [
		{
			"tokenStandard": "zts1xflhun39kllr8vw67y8gw9",
			"znnPercentage": 1,
			"qsrPercentage": 9999,
			"minAmount": "1000"
		},
		{
			"tokenStandard": "zts1g9zjdnwc28hg3zx954d7hw",
			"znnPercentage": 9999,
			"qsrPercentage": 1,
			"minAmount": "2000"
		}
	]
}`)

	defer z.CallContract(liquidityStake(g.User1.Address, tokens[0], big.NewInt(100*g.Zexp), 1)).Error(t, nil)
	insertMomentums(z, 2)

	insertMomentums(z, 362)

	entries, err := liquidityAPI.GetLiquidityStakeEntriesByAddress(g.User1.Address, 0, 5)
	common.FailIfErr(t, err)
	common.Json(entries, err).Equals(t, `
{
	"totalAmount": "10000000000",
	"totalWeightedAmount": "10000000000",
	"count": 1,
	"list": [
		{
			"amount": "10000000000",
			"tokenStandard": "zts1xflhun39kllr8vw67y8gw9",
			"weightedAmount": "10000000000",
			"startTime": 1000006260,
			"revokeTime": 0,
			"expirationTime": 1000009860,
			"stakeAddress": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
			"id": "131b20547046c41ed4256290b836ae9eed649b9b4d0f4896888f790bc0eca90c"
		}
	]
}`)
	common.Json(liquidityAPI.GetUncollectedReward(g.User1.Address)).Equals(t, `
{
	"address": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
	"znnAmount": "18720000",
	"qsrAmount": "499950000000"
}`)
	// delete tokens
	setTokensTuple(t, z, g.User5.Address, []string{}, []uint32{}, []uint32{}, []*big.Int{})

	// will collect and cancel it
	defer z.CallContract(collectReward(g.User1.Address)).Error(t, nil)
	insertMomentums(z, 3)
	z.ExpectBalance(g.User1.Address, types.ZnnTokenStandard, 1199800000000)
	z.ExpectBalance(g.User1.Address, types.QsrTokenStandard, 12000000000000)
	autoreceive(t, z, g.User1.Address)

	defer z.CallContract(cancelStakeLiq(g.User1.Address, entries.Entries[0].Id)).Error(t, nil)
	insertMomentums(z, 2)

	common.Json(liquidityAPI.GetLiquidityStakeEntriesByAddress(g.User1.Address, 0, 5)).Equals(t, `
{
	"totalAmount": "0",
	"totalWeightedAmount": "0",
	"count": 0,
	"list": []
}`)
	common.Json(liquidityAPI.GetUncollectedReward(g.User1.Address)).Equals(t, `
{
	"address": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
	"znnAmount": "0",
	"qsrAmount": "0"
}`)

}

func TestLiquidity_StakeLiquidity(t *testing.T) {
	z := mock.NewMockZenonWithCustomEpochDuration(t, time.Hour)
	defer z.StopPanic()
	//defer z.SaveLogs(common.EmbeddedLogger).Equals(t, ``)

	// token tuples
	activateLiquidityStep2(t, z)

	// invalid period
	z.InsertSendBlock(liquidityStake(g.User1.Address, tokens[0], big.NewInt(10*g.Zexp), 0),
		constants.ErrInvalidStakingPeriod, mock.SkipVmChanges)

	z.InsertSendBlock(liquidityStake(g.User1.Address, tokens[0], big.NewInt(10*g.Zexp), 13),
		constants.ErrInvalidStakingPeriod, mock.SkipVmChanges)

	// less than min amount
	defer z.CallContract(liquidityStake(g.User1.Address, tokens[0], big.NewInt(0*g.Zexp), 6)).
		Error(t, constants.ErrInvalidTokenOrAmount)
	insertMomentums(z, 2)

	// not set zts
	defer z.CallContract(liquidityStake(g.User1.Address, types.ZnnTokenStandard, big.NewInt(0*g.Zexp), 6)).
		Error(t, constants.ErrInvalidToken)
	insertMomentums(z, 2)

	// delete the second zts
	percentages := []uint32{10000}
	minAmounts := []*big.Int{big.NewInt(1000)}

	setTokensTuple(t, z, g.User5.Address, tokensString[:1], percentages, percentages, minAmounts)
	liquidityAPI := embedded.NewLiquidityApi(z)
	common.Json(liquidityAPI.GetLiquidityInfo()).Equals(t, `
{
	"administrator": "z1qqaswvt0e3cc5sm7lygkyza9ra63cr8e6zre09",
	"isHalted": false,
	"znnReward": "0",
	"qsrReward": "0",
	"tokenTuples": [
		{
			"tokenStandard": "zts1992nq43xn2urz3wttklc8z",
			"znnPercentage": 10000,
			"qsrPercentage": 10000,
			"minAmount": "1000"
		}
	]
}`)

	// stake deleted zts
	defer z.CallContract(liquidityStake(g.User1.Address, tokens[1], big.NewInt(10*g.Zexp), 6)).
		Error(t, constants.ErrInvalidToken)
	insertMomentums(z, 2)

	// halt
	defer z.CallContract(setIsHalted(g.User5.Address, true)).Error(t, nil)
	insertMomentums(z, 2)

	defer z.CallContract(liquidityStake(g.User1.Address, tokens[0], big.NewInt(10*g.Zexp), 6)).
		Error(t, nil)
	insertMomentums(z, 2)
}

func TestLiquidity_SetAdditionalRewards(t *testing.T) {
	z := mock.NewMockZenonWithCustomEpochDuration(t, time.Hour)
	defer z.StopPanic()
	//defer z.SaveLogs(common.EmbeddedLogger).Equals(t, ``)

	// token tuples
	activateLiquidityStep1(t, z)

	z.ExpectBalance(types.LiquidityContract, types.ZnnTokenStandard, 1872*g.Zexp)
	z.ExpectBalance(types.LiquidityContract, types.QsrTokenStandard, 5000*g.Zexp)
	setAdditionalReward(t, z, g.User5.Address, big.NewInt(50000*g.Zexp), big.NewInt(100000*g.Zexp))

	z.InsertMomentumsTo(360*2 + 10)
	z.ExpectBalance(types.LiquidityContract, types.ZnnTokenStandard, 3744*g.Zexp)
	z.ExpectBalance(types.LiquidityContract, types.QsrTokenStandard, 10000*g.Zexp)

	setAdditionalReward(t, z, g.User5.Address, big.NewInt(500*g.Zexp), big.NewInt(1000*g.Zexp))
	z.InsertMomentumsTo(360*3 + 10)

	// we should have full balance as there are no stake entries
	z.ExpectBalance(types.LiquidityContract, types.ZnnTokenStandard, 5616*g.Zexp)
	z.ExpectBalance(types.LiquidityContract, types.QsrTokenStandard, 15000*g.Zexp)

	issueMultipleTokensSetup(t, z)
	percentages := []uint32{1000, 9000}
	minAmounts := []*big.Int{big.NewInt(1000), big.NewInt(2000)}
	setTokensTuple(t, z, g.User5.Address, tokensString, percentages, percentages, minAmounts)

	defer z.CallContract(liquidityStake(g.User1.Address, tokens[0], big.NewInt(50*g.Zexp), 12)).Error(t, nil)
	insertMomentums(z, 2)

	z.InsertMomentumsTo(360*4 + 10)

	liquidityAPI := embedded.NewLiquidityApi(z)
	// we should have 1872 / 10 + 500 / 10 znn = 237.2 and 5000 / 10 + 1000 / 10 qsr = 600 qsr
	common.Json(liquidityAPI.GetUncollectedReward(g.User1.Address)).Equals(t, `
{
	"address": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
	"znnAmount": "23720000000",
	"qsrAmount": "60000000000"
}`)
	// balance of the contract should be 1872*4 - 237.2 znn and 5000 * 4 - 600 qsr as that was my reward and the rest was minted to the contract
	z.ExpectBalance(types.LiquidityContract, types.ZnnTokenStandard, 1872*4*g.Zexp-2372*1e7)
	z.ExpectBalance(types.LiquidityContract, types.QsrTokenStandard, 5000*4*g.Zexp-600*g.Zexp)
}

func TestLiquidity_CancelLiquidityStake(t *testing.T) {
	z := mock.NewMockZenonWithCustomEpochDuration(t, time.Hour)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).HideHashes().Equals(t, `
t=2001-09-09T01:46:50+0000 lvl=dbug msg=created module=embedded contract=spork spork="&{Id:XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX Name:spork-accelerator Description:activate spork for accelerator Activated:false EnforcementHeight:0}"
t=2001-09-09T01:47:00+0000 lvl=dbug msg=activated module=embedded contract=spork spork="&{Id:XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX Name:spork-accelerator Description:activate spork for accelerator Activated:true EnforcementHeight:9}"
t=2001-09-09T01:50:00+0000 lvl=dbug msg=created module=embedded contract=spork spork="&{Id:XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX Name:spork-bridge Description:activate spork for bridge Activated:false EnforcementHeight:0}"
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=0 pillar-name=TEST-pillar-1 reward="&{DelegationReward:+10363852800000 BlockReward:+8568000000000 TotalReward:+18931852800000 ProducedBlockNum:119 ExpectedBlockNum:120 Weight:+2100000000000}" total-weight=2500000000000 self-weight=2100000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=0 pillar-name=TEST-pillar-cool reward="&{DelegationReward:+995328000000 BlockReward:+8640000000000 TotalReward:+9635328000000 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2500000000000 self-weight=200000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=0 pillar-name=TEST-pillar-znn reward="&{DelegationReward:+995328000000 BlockReward:+8640000000000 TotalReward:+9635328000000 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2500000000000 self-weight=200000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=pillar epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxsentynelxxxxxxxxxxxxxwy0r2r current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating sentinel reward" module=embedded contract=sentinel epoch=0 total-znn-reward=187200000000 total-qsr-reward=500000000000 cumulated-sentinel=0 start-time=1000000000 end-time=1000003600
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=sentinel epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating stake reward" module=embedded contract=stake epoch=0 total-reward=1000000000000 cumulated-stake=0 start-time=1000000000 end-time=1000003600
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=stake epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating liquidity stake reward" module=embedded contract=liquidity epoch=0 znn-total-amount=187200000000 qsr-total-amount=500000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating liquidity balance" module=embedded contract=liquidity epoch=0 znnReward=187200000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating liquidity balance" module=embedded contract=liquidity epoch=0 qsrReward=500000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxaccelerat0rxxxxxxxxxxp4tk22 current-height=361 last-update-height=0
t=2001-09-09T02:46:50+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg TokenName:Zenon Coin TokenSymbol:ZNN TokenDomain:zenon.network TotalSupply:+19687200000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1znnxxxxxxxxxxxxx9z4ulx}" minted-amount=187200000000 to-address=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae
t=2001-09-09T02:46:50+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 TokenName:QuasarCoin TokenSymbol:QSR TokenDomain:zenon.network TotalSupply:+181050000000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1qsrxxxxxxxxxxxxxmrhjll}" minted-amount=500000000000 to-address=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae
t=2001-09-09T02:47:00+0000 lvl=info msg="received donation" module=embedded contract=common embedded=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae from-address=z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0 zts=zts1znnxxxxxxxxxxxxx9z4ulx amount=187200000000
t=2001-09-09T02:47:00+0000 lvl=info msg="received donation" module=embedded contract=common embedded=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae from-address=z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0 zts=zts1qsrxxxxxxxxxxxxxmrhjll amount=500000000000
t=2001-09-09T03:14:20+0000 lvl=dbug msg="issued ZTS" module=embedded contract=token token="{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenName:test.tok3n_liquidity-1 TokenSymbol:LIQ1 TokenDomain: TotalSupply:+10000000000 MaxSupply:+100000000000 Decimals:6 IsMintable:true IsBurnable:true IsUtility:false TokenStandard:zts1992nq43xn2urz3wttklc8z}"
t=2001-09-09T03:14:40+0000 lvl=dbug msg="issued ZTS" module=embedded contract=token token="{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenName:test.tok3n_liquidity-2 TokenSymbol:LIQ2 TokenDomain: TotalSupply:+20000000000 MaxSupply:+200000000000 Decimals:6 IsMintable:true IsBurnable:true IsUtility:false TokenStandard:zts1fres5c39axw805xswvt55j}"
t=2001-09-09T03:15:00+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenName:test.tok3n_liquidity-1 TokenSymbol:LIQ1 TokenDomain: TotalSupply:+20000000000 MaxSupply:+100000000000 Decimals:6 IsMintable:true IsBurnable:true IsUtility:false TokenStandard:zts1992nq43xn2urz3wttklc8z}" minted-amount=10000000000 to-address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz
t=2001-09-09T03:15:20+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenName:test.tok3n_liquidity-2 TokenSymbol:LIQ2 TokenDomain: TotalSupply:+40000000000 MaxSupply:+200000000000 Decimals:6 IsMintable:true IsBurnable:true IsUtility:false TokenStandard:zts1fres5c39axw805xswvt55j}" minted-amount=20000000000 to-address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz
t=2001-09-09T03:18:00+0000 lvl=dbug msg="created liquidity stake entry" module=embedded contract=stake id=XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX owner=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz amount=1000000000 weighted-amount=1000000000 duration-in-days=0
t=2001-09-09T03:18:20+0000 lvl=dbug msg="created liquidity stake entry" module=embedded contract=stake id=XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX owner=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz amount=3000000000 weighted-amount=9000000000 duration-in-days=0
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg current-height=722 last-update-height=361
t=2001-09-09T03:46:50+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=1 pillar-name=TEST-pillar-1 reward="&{DelegationReward:+10450877642587 BlockReward:+8640000000000 TotalReward:+19090877642587 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+2099916666666}" total-weight=2499916666666 self-weight=2099916666666
t=2001-09-09T03:46:50+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=1 pillar-name=TEST-pillar-cool reward="&{DelegationReward:+995361178706 BlockReward:+8640000000000 TotalReward:+9635361178706 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2499916666666 self-weight=200000000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=1 pillar-name=TEST-pillar-znn reward="&{DelegationReward:+995361178706 BlockReward:+8640000000000 TotalReward:+9635361178706 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2499916666666 self-weight=200000000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=pillar epoch=2
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxsentynelxxxxxxxxxxxxxwy0r2r current-height=722 last-update-height=361
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating sentinel reward" module=embedded contract=sentinel epoch=1 total-znn-reward=187200000000 total-qsr-reward=500000000000 cumulated-sentinel=0 start-time=1000003600 end-time=1000007200
t=2001-09-09T03:46:50+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=sentinel epoch=2
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 current-height=722 last-update-height=361
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating stake reward" module=embedded contract=stake epoch=1 total-reward=1000000000000 cumulated-stake=0 start-time=1000003600 end-time=1000007200
t=2001-09-09T03:46:50+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=stake epoch=2
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae current-height=722 last-update-height=361
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating liquidity stake reward" module=embedded contract=liquidity epoch=1 znn-total-amount=187200000000 qsr-total-amount=500000000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="calculating percentages for each token" module=embedded contract=liquidity epoch=1 token-standard=zts1992nq43xn2urz3wttklc8z znn-percentage=5000 qsr-percentage=5000 znn-rewards=93600000000 qsr-rewards=250000000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="calculating percentages for each token" module=embedded contract=liquidity epoch=1 token-standard=zts1fres5c39axw805xswvt55j znn-percentage=5000 qsr-percentage=5000 znn-rewards=93600000000 qsr-rewards=250000000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating liquidity stake reward" module=embedded contract=liquidity id=XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX stake-address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz token-standard=zts1fres5c39axw805xswvt55j znn-amount=93600000000 qsr-amount=250000000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating liquidity stake reward" module=embedded contract=liquidity id=XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX stake-address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz token-standard=zts1992nq43xn2urz3wttklc8z znn-amount=93600000000 qsr-amount=250000000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxaccelerat0rxxxxxxxxxxp4tk22 current-height=722 last-update-height=361
t=2001-09-09T03:48:50+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg TokenName:Zenon Coin TokenSymbol:ZNN TokenDomain:zenon.network TotalSupply:+19874400000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1znnxxxxxxxxxxxxx9z4ulx}" minted-amount=187200000000 to-address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz
t=2001-09-09T03:48:50+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 TokenName:QuasarCoin TokenSymbol:QSR TokenDomain:zenon.network TotalSupply:+181550000000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1qsrxxxxxxxxxxxxxmrhjll}" minted-amount=500000000000 to-address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz
t=2001-09-09T03:50:10+0000 lvl=dbug msg="created liquidity stake entry" module=embedded contract=stake id=XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX owner=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz amount=5000000000 weighted-amount=60000000000 duration-in-days=0
t=2001-09-09T03:51:10+0000 lvl=dbug msg="revoked liquidity stake entry" module=embedded contract=stake id=XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX owner=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz start-time=1000005480 revoke-time=1000007470
t=2001-09-09T03:51:30+0000 lvl=dbug msg="revoked liquidity stake entry" module=embedded contract=stake id=XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX owner=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz start-time=1000005480 revoke-time=1000007490
`)

	// we have 2 stake entries
	activateLiquidityStep4(t, z)

	hash := types.HexToHashPanic("0003456789012345678901234567890123456789012345678901234567890123")
	defer z.CallContract(cancelStakeLiq(g.User1.Address, hash)).Error(t, constants.ErrDataNonExistent)
	insertMomentums(z, 2)

	liquidityAPI := embedded.NewLiquidityApi(z)
	entries, err := liquidityAPI.GetLiquidityStakeEntriesByAddress(g.User1.Address, 0, 5)
	common.FailIfErr(t, err)

	// the address is in the key so we don't find the entry
	defer z.CallContract(cancelStakeLiq(g.User2.Address, entries.Entries[0].Id)).
		Error(t, constants.ErrDataNonExistent)
	insertMomentums(z, 2)

	defer z.CallContract(liquidityStake(g.User1.Address, tokens[1], big.NewInt(50*g.Zexp), 12)).Error(t, nil)
	insertMomentums(z, 2)

	entries, err = liquidityAPI.GetLiquidityStakeEntriesByAddress(g.User1.Address, 0, 5)
	common.FailIfErr(t, err)

	// revoke not due
	defer z.CallContract(cancelStakeLiq(g.User1.Address, entries.Entries[2].Id)).
		Error(t, constants.RevokeNotDue)
	insertMomentums(z, 2)

	defer z.CallContract(unlockLiquidityEntries(g.User5.Address, tokens[0])).
		Error(t, nil)
	insertMomentums(z, 2)

	defer z.CallContract(cancelStakeLiq(g.User1.Address, entries.Entries[0].Id)).
		Error(t, nil)
	insertMomentums(z, 2)
	z.ExpectBalance(g.User1.Address, tokens[0], 190*g.Zexp)
	autoreceive(t, z, g.User1.Address)
	z.ExpectBalance(g.User1.Address, tokens[0], 200*g.Zexp)

	// cancel the second time
	defer z.CallContract(cancelStakeLiq(g.User1.Address, entries.Entries[0].Id)).
		Error(t, nil)
	insertMomentums(z, 2)

	autoreceive(t, z, g.User1.Address)
	z.ExpectBalance(g.User1.Address, tokens[0], 200*g.Zexp)
}

func TestLiquidity_UnlockLiquidityEntries(t *testing.T) {
	z := mock.NewMockZenonWithCustomEpochDuration(t, time.Hour)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).HideHashes().Equals(t, `
t=2001-09-09T01:46:50+0000 lvl=dbug msg=created module=embedded contract=spork spork="&{Id:XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX Name:spork-accelerator Description:activate spork for accelerator Activated:false EnforcementHeight:0}"
t=2001-09-09T01:47:00+0000 lvl=dbug msg=activated module=embedded contract=spork spork="&{Id:XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX Name:spork-accelerator Description:activate spork for accelerator Activated:true EnforcementHeight:9}"
t=2001-09-09T01:50:00+0000 lvl=dbug msg=created module=embedded contract=spork spork="&{Id:XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX Name:spork-bridge Description:activate spork for bridge Activated:false EnforcementHeight:0}"
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=0 pillar-name=TEST-pillar-1 reward="&{DelegationReward:+10363852800000 BlockReward:+8568000000000 TotalReward:+18931852800000 ProducedBlockNum:119 ExpectedBlockNum:120 Weight:+2100000000000}" total-weight=2500000000000 self-weight=2100000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=0 pillar-name=TEST-pillar-cool reward="&{DelegationReward:+995328000000 BlockReward:+8640000000000 TotalReward:+9635328000000 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2500000000000 self-weight=200000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=0 pillar-name=TEST-pillar-znn reward="&{DelegationReward:+995328000000 BlockReward:+8640000000000 TotalReward:+9635328000000 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2500000000000 self-weight=200000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=pillar epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxsentynelxxxxxxxxxxxxxwy0r2r current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating sentinel reward" module=embedded contract=sentinel epoch=0 total-znn-reward=187200000000 total-qsr-reward=500000000000 cumulated-sentinel=0 start-time=1000000000 end-time=1000003600
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=sentinel epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating stake reward" module=embedded contract=stake epoch=0 total-reward=1000000000000 cumulated-stake=0 start-time=1000000000 end-time=1000003600
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=stake epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating liquidity stake reward" module=embedded contract=liquidity epoch=0 znn-total-amount=187200000000 qsr-total-amount=500000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating liquidity balance" module=embedded contract=liquidity epoch=0 znnReward=187200000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating liquidity balance" module=embedded contract=liquidity epoch=0 qsrReward=500000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxaccelerat0rxxxxxxxxxxp4tk22 current-height=361 last-update-height=0
t=2001-09-09T02:46:50+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg TokenName:Zenon Coin TokenSymbol:ZNN TokenDomain:zenon.network TotalSupply:+19687200000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1znnxxxxxxxxxxxxx9z4ulx}" minted-amount=187200000000 to-address=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae
t=2001-09-09T02:46:50+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 TokenName:QuasarCoin TokenSymbol:QSR TokenDomain:zenon.network TotalSupply:+181050000000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1qsrxxxxxxxxxxxxxmrhjll}" minted-amount=500000000000 to-address=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae
t=2001-09-09T02:47:00+0000 lvl=info msg="received donation" module=embedded contract=common embedded=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae from-address=z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0 zts=zts1znnxxxxxxxxxxxxx9z4ulx amount=187200000000
t=2001-09-09T02:47:00+0000 lvl=info msg="received donation" module=embedded contract=common embedded=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae from-address=z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0 zts=zts1qsrxxxxxxxxxxxxxmrhjll amount=500000000000
t=2001-09-09T03:14:20+0000 lvl=dbug msg="issued ZTS" module=embedded contract=token token="{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenName:test.tok3n_liquidity-1 TokenSymbol:LIQ1 TokenDomain: TotalSupply:+10000000000 MaxSupply:+100000000000 Decimals:6 IsMintable:true IsBurnable:true IsUtility:false TokenStandard:zts1992nq43xn2urz3wttklc8z}"
t=2001-09-09T03:14:40+0000 lvl=dbug msg="issued ZTS" module=embedded contract=token token="{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenName:test.tok3n_liquidity-2 TokenSymbol:LIQ2 TokenDomain: TotalSupply:+20000000000 MaxSupply:+200000000000 Decimals:6 IsMintable:true IsBurnable:true IsUtility:false TokenStandard:zts1fres5c39axw805xswvt55j}"
t=2001-09-09T03:15:00+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenName:test.tok3n_liquidity-1 TokenSymbol:LIQ1 TokenDomain: TotalSupply:+20000000000 MaxSupply:+100000000000 Decimals:6 IsMintable:true IsBurnable:true IsUtility:false TokenStandard:zts1992nq43xn2urz3wttklc8z}" minted-amount=10000000000 to-address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz
t=2001-09-09T03:15:20+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenName:test.tok3n_liquidity-2 TokenSymbol:LIQ2 TokenDomain: TotalSupply:+40000000000 MaxSupply:+200000000000 Decimals:6 IsMintable:true IsBurnable:true IsUtility:false TokenStandard:zts1fres5c39axw805xswvt55j}" minted-amount=20000000000 to-address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz
t=2001-09-09T03:18:00+0000 lvl=dbug msg="created liquidity stake entry" module=embedded contract=stake id=XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX owner=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz amount=1000000000 weighted-amount=1000000000 duration-in-days=0
t=2001-09-09T03:18:20+0000 lvl=dbug msg="created liquidity stake entry" module=embedded contract=stake id=XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX owner=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz amount=3000000000 weighted-amount=9000000000 duration-in-days=0
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg current-height=722 last-update-height=361
t=2001-09-09T03:46:50+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=1 pillar-name=TEST-pillar-1 reward="&{DelegationReward:+10450877642587 BlockReward:+8640000000000 TotalReward:+19090877642587 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+2099916666666}" total-weight=2499916666666 self-weight=2099916666666
t=2001-09-09T03:46:50+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=1 pillar-name=TEST-pillar-cool reward="&{DelegationReward:+995361178706 BlockReward:+8640000000000 TotalReward:+9635361178706 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2499916666666 self-weight=200000000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=1 pillar-name=TEST-pillar-znn reward="&{DelegationReward:+995361178706 BlockReward:+8640000000000 TotalReward:+9635361178706 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2499916666666 self-weight=200000000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=pillar epoch=2
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxsentynelxxxxxxxxxxxxxwy0r2r current-height=722 last-update-height=361
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating sentinel reward" module=embedded contract=sentinel epoch=1 total-znn-reward=187200000000 total-qsr-reward=500000000000 cumulated-sentinel=0 start-time=1000003600 end-time=1000007200
t=2001-09-09T03:46:50+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=sentinel epoch=2
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 current-height=722 last-update-height=361
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating stake reward" module=embedded contract=stake epoch=1 total-reward=1000000000000 cumulated-stake=0 start-time=1000003600 end-time=1000007200
t=2001-09-09T03:46:50+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=stake epoch=2
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae current-height=722 last-update-height=361
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating liquidity stake reward" module=embedded contract=liquidity epoch=1 znn-total-amount=187200000000 qsr-total-amount=500000000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="calculating percentages for each token" module=embedded contract=liquidity epoch=1 token-standard=zts1992nq43xn2urz3wttklc8z znn-percentage=5000 qsr-percentage=5000 znn-rewards=93600000000 qsr-rewards=250000000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="calculating percentages for each token" module=embedded contract=liquidity epoch=1 token-standard=zts1fres5c39axw805xswvt55j znn-percentage=5000 qsr-percentage=5000 znn-rewards=93600000000 qsr-rewards=250000000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating liquidity stake reward" module=embedded contract=liquidity id=XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX stake-address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz token-standard=zts1fres5c39axw805xswvt55j znn-amount=93600000000 qsr-amount=250000000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating liquidity stake reward" module=embedded contract=liquidity id=XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX stake-address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz token-standard=zts1992nq43xn2urz3wttklc8z znn-amount=93600000000 qsr-amount=250000000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxaccelerat0rxxxxxxxxxxp4tk22 current-height=722 last-update-height=361
t=2001-09-09T03:48:50+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg TokenName:Zenon Coin TokenSymbol:ZNN TokenDomain:zenon.network TotalSupply:+19874400000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1znnxxxxxxxxxxxxx9z4ulx}" minted-amount=187200000000 to-address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz
t=2001-09-09T03:48:50+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 TokenName:QuasarCoin TokenSymbol:QSR TokenDomain:zenon.network TotalSupply:+181550000000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1qsrxxxxxxxxxxxxxmrhjll}" minted-amount=500000000000 to-address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz
t=2001-09-09T03:49:30+0000 lvl=dbug msg="created liquidity stake entry" module=embedded contract=stake id=XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX owner=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz amount=5000000000 weighted-amount=60000000000 duration-in-days=0
t=2001-09-09T03:51:50+0000 lvl=dbug msg="revoked liquidity stake entry" module=embedded contract=stake id=XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX owner=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz start-time=1000005500 revoke-time=1000007510
t=2001-09-09T03:52:10+0000 lvl=dbug msg="revoked liquidity stake entry" module=embedded contract=stake id=XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX owner=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz start-time=1000007370 revoke-time=1000007530
`)

	// we have 2 stake entries
	activateLiquidityStep4(t, z)

	defer z.CallContract(liquidityStake(g.User1.Address, tokens[1], big.NewInt(50*g.Zexp), 12)).Error(t, nil)
	insertMomentums(z, 2)

	liquidityAPI := embedded.NewLiquidityApi(z)
	entries, err := liquidityAPI.GetLiquidityStakeEntriesByAddress(g.User1.Address, 0, 5)
	common.FailIfErr(t, err)

	// revoke not due
	defer z.CallContract(cancelStakeLiq(g.User1.Address, entries.Entries[2].Id)).
		Error(t, constants.RevokeNotDue)
	insertMomentums(z, 2)

	// call as non admin
	defer z.CallContract(unlockLiquidityEntries(g.User1.Address, tokens[1])).
		Error(t, constants.ErrPermissionDenied)
	insertMomentums(z, 2)

	common.Json(entries, err).Equals(t, `
{
	"totalAmount": "9000000000",
	"totalWeightedAmount": "70000000000",
	"count": 3,
	"list": [
		{
			"amount": "1000000000",
			"tokenStandard": "zts1992nq43xn2urz3wttklc8z",
			"weightedAmount": "1000000000",
			"startTime": 1000005480,
			"revokeTime": 0,
			"expirationTime": 1000009080,
			"stakeAddress": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
			"id": "62690006c58c67dc5b7c41e095a22e40307eba0463cee5585f84b91f6815a5b1"
		},
		{
			"amount": "3000000000",
			"tokenStandard": "zts1fres5c39axw805xswvt55j",
			"weightedAmount": "9000000000",
			"startTime": 1000005500,
			"revokeTime": 0,
			"expirationTime": 1000016300,
			"stakeAddress": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
			"id": "3de102aa795d705f1183c3422f8139983bbbcf398d3b60c848f7de27defdf4ea"
		},
		{
			"amount": "5000000000",
			"tokenStandard": "zts1fres5c39axw805xswvt55j",
			"weightedAmount": "60000000000",
			"startTime": 1000007370,
			"revokeTime": 0,
			"expirationTime": 1000050570,
			"stakeAddress": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
			"id": "5fd186418784d4b963369941646bfaa63dc7b28127c71be37e64116fa1580927"
		}
	]
}`)

	// revoke not due
	defer z.CallContract(cancelStakeLiq(g.User1.Address, entries.Entries[0].Id)).
		Error(t, constants.RevokeNotDue)
	insertMomentums(z, 2)

	// revoke not due
	defer z.CallContract(cancelStakeLiq(g.User1.Address, entries.Entries[1].Id)).
		Error(t, constants.RevokeNotDue)
	insertMomentums(z, 2)

	defer z.CallContract(unlockLiquidityEntries(g.User5.Address, tokens[1])).
		Error(t, nil)
	insertMomentums(z, 2)

	common.Json(liquidityAPI.GetLiquidityStakeEntriesByAddress(g.User1.Address, 0, 5)).Equals(t, `
{
	"totalAmount": "9000000000",
	"totalWeightedAmount": "70000000000",
	"count": 3,
	"list": [
		{
			"amount": "3000000000",
			"tokenStandard": "zts1fres5c39axw805xswvt55j",
			"weightedAmount": "9000000000",
			"startTime": 1000005500,
			"revokeTime": 0,
			"expirationTime": 1000007470,
			"stakeAddress": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
			"id": "3de102aa795d705f1183c3422f8139983bbbcf398d3b60c848f7de27defdf4ea"
		},
		{
			"amount": "5000000000",
			"tokenStandard": "zts1fres5c39axw805xswvt55j",
			"weightedAmount": "60000000000",
			"startTime": 1000007370,
			"revokeTime": 0,
			"expirationTime": 1000007470,
			"stakeAddress": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
			"id": "5fd186418784d4b963369941646bfaa63dc7b28127c71be37e64116fa1580927"
		},
		{
			"amount": "1000000000",
			"tokenStandard": "zts1992nq43xn2urz3wttklc8z",
			"weightedAmount": "1000000000",
			"startTime": 1000005480,
			"revokeTime": 0,
			"expirationTime": 1000009080,
			"stakeAddress": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
			"id": "62690006c58c67dc5b7c41e095a22e40307eba0463cee5585f84b91f6815a5b1"
		}
	]
}`)

	// call for unknown zts - nothing happened
	defer z.CallContract(unlockLiquidityEntries(g.User5.Address, types.ZenonTokenStandard{})).
		Error(t, nil)
	insertMomentums(z, 2)

	entries, err = liquidityAPI.GetLiquidityStakeEntriesByAddress(g.User1.Address, 0, 5)
	common.Json(entries, err).Equals(t, `
{
	"totalAmount": "9000000000",
	"totalWeightedAmount": "70000000000",
	"count": 3,
	"list": [
		{
			"amount": "3000000000",
			"tokenStandard": "zts1fres5c39axw805xswvt55j",
			"weightedAmount": "9000000000",
			"startTime": 1000005500,
			"revokeTime": 0,
			"expirationTime": 1000007470,
			"stakeAddress": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
			"id": "3de102aa795d705f1183c3422f8139983bbbcf398d3b60c848f7de27defdf4ea"
		},
		{
			"amount": "5000000000",
			"tokenStandard": "zts1fres5c39axw805xswvt55j",
			"weightedAmount": "60000000000",
			"startTime": 1000007370,
			"revokeTime": 0,
			"expirationTime": 1000007470,
			"stakeAddress": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
			"id": "5fd186418784d4b963369941646bfaa63dc7b28127c71be37e64116fa1580927"
		},
		{
			"amount": "1000000000",
			"tokenStandard": "zts1992nq43xn2urz3wttklc8z",
			"weightedAmount": "1000000000",
			"startTime": 1000005480,
			"revokeTime": 0,
			"expirationTime": 1000009080,
			"stakeAddress": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
			"id": "62690006c58c67dc5b7c41e095a22e40307eba0463cee5585f84b91f6815a5b1"
		}
	]
}`)

	// cancel the 2 entries
	// revoke not due
	defer z.CallContract(cancelStakeLiq(g.User1.Address, entries.Entries[0].Id)).
		Error(t, nil)
	insertMomentums(z, 2)

	// revoke not due
	defer z.CallContract(cancelStakeLiq(g.User1.Address, entries.Entries[1].Id)).
		Error(t, nil)
	insertMomentums(z, 2)

	common.Json(liquidityAPI.GetLiquidityStakeEntriesByAddress(g.User1.Address, 0, 5)).Equals(t, `
{
	"totalAmount": "1000000000",
	"totalWeightedAmount": "1000000000",
	"count": 1,
	"list": [
		{
			"amount": "1000000000",
			"tokenStandard": "zts1992nq43xn2urz3wttklc8z",
			"weightedAmount": "1000000000",
			"startTime": 1000005480,
			"revokeTime": 0,
			"expirationTime": 1000009080,
			"stakeAddress": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
			"id": "62690006c58c67dc5b7c41e095a22e40307eba0463cee5585f84b91f6815a5b1"
		}
	]
}`)

}

func TestLiquidity_TestScenariosNoAdditionalRewards(t *testing.T) {
	z := mock.NewMockZenonWithCustomEpochDuration(t, time.Hour)
	defer z.StopPanic()
	//defer z.SaveLogs(common.EmbeddedLogger).HideHashes().Equals(t, ``)

	// activate sporks and set guardians
	activateLiquidityStep1(t, z)

	znnPercentages := []uint32{uint32(1000), uint32(1000), uint32(1000), uint32(1000), uint32(1000), uint32(1000), uint32(1000), uint32(1000), uint32(1000), uint32(1000)}
	qsrPercentages := []uint32{uint32(1000), uint32(1000), uint32(1000), uint32(1000), uint32(1000), uint32(1000), uint32(1000), uint32(1000), uint32(1000), uint32(1000)}
	minAmounts := []*big.Int{big.NewInt(1000), big.NewInt(2000), big.NewInt(1000), big.NewInt(2000), big.NewInt(1000), big.NewInt(2000), big.NewInt(1000), big.NewInt(2000), big.NewInt(1000), big.NewInt(2000)}
	// create 10 tokens

	bigOneBytes, _ := base64.StdEncoding.DecodeString("tWjhEdP000")
	bigOne := big.NewInt(0).SetBytes(bigOneBytes)
	bigThousand := big.NewInt(0).SetBytes(bigOne.Bytes())
	bigThousand.Mul(bigThousand, big.NewInt(1000))
	bigHT := big.NewInt(0).SetBytes(bigThousand.Bytes())
	bigHT.Mul(bigHT, big.NewInt(100))
	decimals := uint8(18)
	for i := 0; i < 10; i++ {
		name := fmt.Sprintf("testToken%d", i)
		symbol := fmt.Sprintf("TT%d", i)
		defer z.CallContract(issue(g.User1.Address, name, symbol, "", bigThousand, bigHT, decimals, true, true, true)).
			Error(t, nil)
		insertMomentums(z, 2)
	}
	tokenAPI := embedded.NewTokenApi(z)
	tokenList, err := tokenAPI.GetByOwner(g.User1.Address, 0, 10)
	common.FailIfErr(t, err)

	// mint to users
	tokensToSet := make([]types.ZenonTokenStandard, 0)
	tokensToSetString := make([]string, 0)
	for i := 0; i < 10; i++ {
		tokensToSetString = append(tokensToSetString, tokenList.List[i].ZenonTokenStandard.String())
		tokensToSet = append(tokensToSet, tokenList.List[i].ZenonTokenStandard)
		defer z.CallContract(mint(g.User1.Address, tokenList.List[i].ZenonTokenStandard, bigThousand, g.User2.Address)).
			Error(t, nil)
		insertMomentums(z, 2)
		defer z.CallContract(mint(g.User1.Address, tokenList.List[i].ZenonTokenStandard, bigThousand, g.User3.Address)).
			Error(t, nil)
		insertMomentums(z, 2)
	}
	autoreceive(t, z, g.User1.Address)
	autoreceive(t, z, g.User2.Address)
	autoreceive(t, z, g.User3.Address)

	// set them
	setTokensTuple(t, z, g.User5.Address, tokensToSetString, znnPercentages, qsrPercentages, minAmounts)
	liquidityAPI := embedded.NewLiquidityApi(z)
	common.Json(liquidityAPI.GetLiquidityInfo()).Equals(t, `
{
	"administrator": "z1qqaswvt0e3cc5sm7lygkyza9ra63cr8e6zre09",
	"isHalted": false,
	"znnReward": "0",
	"qsrReward": "0",
	"tokenTuples": [
		{
			"tokenStandard": "zts1r8z2feqa3lsz9t78rgy772",
			"znnPercentage": 1000,
			"qsrPercentage": 1000,
			"minAmount": "1000"
		},
		{
			"tokenStandard": "zts196fjtffgd8jj3c99slvnt7",
			"znnPercentage": 1000,
			"qsrPercentage": 1000,
			"minAmount": "2000"
		},
		{
			"tokenStandard": "zts1gl30jmp4qd8slt5kwcxtru",
			"znnPercentage": 1000,
			"qsrPercentage": 1000,
			"minAmount": "1000"
		},
		{
			"tokenStandard": "zts129hkhzcdys8aacwwl7r7re",
			"znnPercentage": 1000,
			"qsrPercentage": 1000,
			"minAmount": "2000"
		},
		{
			"tokenStandard": "zts13pu3yrev9rqm4qjhwpfez9",
			"znnPercentage": 1000,
			"qsrPercentage": 1000,
			"minAmount": "1000"
		},
		{
			"tokenStandard": "zts1kc4lg8jey6hfkkcycr9rpv",
			"znnPercentage": 1000,
			"qsrPercentage": 1000,
			"minAmount": "2000"
		},
		{
			"tokenStandard": "zts1c7py5fkcxgf3h7g8y8tct2",
			"znnPercentage": 1000,
			"qsrPercentage": 1000,
			"minAmount": "1000"
		},
		{
			"tokenStandard": "zts1ejte9v853kw5ydexhc68es",
			"znnPercentage": 1000,
			"qsrPercentage": 1000,
			"minAmount": "2000"
		},
		{
			"tokenStandard": "zts1emn6vr3e20tc8gjq3kh6aw",
			"znnPercentage": 1000,
			"qsrPercentage": 1000,
			"minAmount": "1000"
		},
		{
			"tokenStandard": "zts1eaas9e0f30wfcdgp433xhd",
			"znnPercentage": 1000,
			"qsrPercentage": 1000,
			"minAmount": "2000"
		}
	]
}`)

	// the first update was at height 360
	z.ExpectBalance(types.LiquidityContract, types.ZnnTokenStandard, 187200000000)
	z.ExpectBalance(types.LiquidityContract, types.QsrTokenStandard, 500000000000)

	z.InsertMomentumsTo(360*2 + 10)

	// rewards are minted to the contract as no stake entries are found
	z.ExpectBalance(types.LiquidityContract, types.ZnnTokenStandard, 187200000000*2)
	z.ExpectBalance(types.LiquidityContract, types.QsrTokenStandard, 500000000000*2)

	// stake some tokens
	for i := 0; i < 6; i++ {
		defer z.CallContract(liquidityStake(g.User1.Address, tokensToSet[i], bigOne, 1)).Error(t, nil)
		insertMomentums(z, 2)
	}
	for i := 4; i < 10; i++ {
		defer z.CallContract(liquidityStake(g.User2.Address, tokensToSet[i], bigOne, 1)).Error(t, nil)
		insertMomentums(z, 2)
	}

	z.InsertMomentumsTo(360*3 + 10)
	// both rewards added should be 187200000000 znn and 500000000000 qsr
	user1Rewards, _ := liquidityAPI.GetUncollectedReward(g.User1.Address)
	user2Rewards, _ := liquidityAPI.GetUncollectedReward(g.User2.Address)
	znnSum := big.NewInt(0)
	znnSum.Add(znnSum, user1Rewards.Znn).Add(znnSum, user2Rewards.Znn)
	qsrSum := big.NewInt(0)
	qsrSum.Add(qsrSum, user1Rewards.Qsr).Add(qsrSum, user2Rewards.Qsr)
	common.Expect(t, znnSum.Uint64(), 187199999998)
	common.Expect(t, qsrSum.Uint64(), 499999999998)

	defer z.CallContract(collectReward(g.User1.Address)).Error(t, nil)
	insertMomentums(z, 3)
	defer z.CallContract(collectReward(g.User2.Address)).Error(t, nil)
	insertMomentums(z, 3)

	// balance should stay the the same as all rewards will be given to the user
	z.ExpectBalance(types.LiquidityContract, types.ZnnTokenStandard, 187200000000*2+2)
	z.ExpectBalance(types.LiquidityContract, types.QsrTokenStandard, 500000000000*2+2)

	insertMomentums(z, 30)
	stakes1, err := liquidityAPI.GetLiquidityStakeEntriesByAddress(g.User1.Address, 0, 10)
	common.FailIfErr(t, err)
	stakes2, err := liquidityAPI.GetLiquidityStakeEntriesByAddress(g.User2.Address, 0, 10)
	common.FailIfErr(t, err)
	// cancel all stakes
	for i := 0; i < 6; i++ {
		defer z.CallContract(cancelStakeLiq(g.User1.Address, stakes1.Entries[i].Id)).Error(t, nil)
		insertMomentums(z, 2)
		defer z.CallContract(cancelStakeLiq(g.User2.Address, stakes2.Entries[i].Id)).Error(t, nil)
		insertMomentums(z, 2)
	}

	z.InsertMomentumsTo(360*4 + 10)

	common.Json(liquidityAPI.GetUncollectedReward(g.User1.Address)).Equals(t, `
{
	"address": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
	"znnAmount": "95901781201",
	"qsrAmount": "256147919875"
}`)
	common.Json(liquidityAPI.GetUncollectedReward(g.User2.Address)).Equals(t, `
{
	"address": "z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx",
	"znnAmount": "91298218797",
	"qsrAmount": "243852080123"
}`)

	// balance should stay the the same as all rewards will be given to the users
	z.ExpectBalance(types.LiquidityContract, types.ZnnTokenStandard, 187200000000*2+4)
	z.ExpectBalance(types.LiquidityContract, types.QsrTokenStandard, 500000000000*2+4)

	z.InsertMomentumsTo(360*5 + 10)

	// balance should be minted to the contract now
	z.ExpectBalance(types.LiquidityContract, types.ZnnTokenStandard, 187200000000*3+4)
	z.ExpectBalance(types.LiquidityContract, types.QsrTokenStandard, 500000000000*3+4)

	// stake each token but the first two and the last one
	for i := 2; i < 9; i++ {
		defer z.CallContract(liquidityStake(g.User1.Address, tokensToSet[i], bigOne, 1)).Error(t, nil)
		insertMomentums(z, 2)
		// some tokens have 2 stakes
		if i%2 == 0 {
			defer z.CallContract(liquidityStake(g.User2.Address, tokensToSet[i], bigOne, 1)).Error(t, nil)
			insertMomentums(z, 2)
		}
	}
	// delete the last 3 tuples
	znnPercentages = []uint32{1500, 1500, 1500, 1500, 1500, 1500, 1000}
	qsrPercentages = []uint32{1500, 1500, 1500, 1500, 1500, 1500, 1000}
	setTokensTuple(t, z, g.User5.Address, tokensToSetString[:7], znnPercentages, qsrPercentages, minAmounts[:7])

	z.InsertMomentumsTo(360*6 + 10)

	defer z.CallContract(collectReward(g.User1.Address)).Error(t, nil)
	insertMomentums(z, 3)
	defer z.CallContract(collectReward(g.User2.Address)).Error(t, nil)
	insertMomentums(z, 3)

	// as some stake don't have entries, amount is less than 1872 * 4 for znn and less than 5000 * 4 for qsr
	z.ExpectBalance(types.LiquidityContract, types.ZnnTokenStandard, 617760000007)
	z.ExpectBalance(types.LiquidityContract, types.QsrTokenStandard, 1650000000007)

	z.InsertMomentumsTo(360*6 + 180)

	stakes1, err = liquidityAPI.GetLiquidityStakeEntriesByAddress(g.User1.Address, 0, 10)
	common.Json(stakes1, err).Equals(t, `
{
	"totalAmount": "1396234400877484",
	"totalWeightedAmount": "1396234400877484",
	"count": 7,
	"list": [
		{
			"amount": "199462057268212",
			"tokenStandard": "zts1gl30jmp4qd8slt5kwcxtru",
			"weightedAmount": "199462057268212",
			"startTime": 1000018100,
			"revokeTime": 0,
			"expirationTime": 1000021700,
			"stakeAddress": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
			"id": "7fa485f45e7f0ced2a90f0b6a5b3d82b1eeaea47a6a3289629206e7ca3355643"
		},
		{
			"amount": "199462057268212",
			"tokenStandard": "zts129hkhzcdys8aacwwl7r7re",
			"weightedAmount": "199462057268212",
			"startTime": 1000018140,
			"revokeTime": 0,
			"expirationTime": 1000021740,
			"stakeAddress": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
			"id": "2c546fd7a4566a18a552ddf9c186f219d02214492d8ad0cfb8d8aed80da2f0ad"
		},
		{
			"amount": "199462057268212",
			"tokenStandard": "zts13pu3yrev9rqm4qjhwpfez9",
			"weightedAmount": "199462057268212",
			"startTime": 1000018160,
			"revokeTime": 0,
			"expirationTime": 1000021760,
			"stakeAddress": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
			"id": "2a6875bedb9e74f4f61bc2c37fa3abdf1659a8ec924dfa8c5550e9e5d09612b4"
		},
		{
			"amount": "199462057268212",
			"tokenStandard": "zts1kc4lg8jey6hfkkcycr9rpv",
			"weightedAmount": "199462057268212",
			"startTime": 1000018200,
			"revokeTime": 0,
			"expirationTime": 1000021800,
			"stakeAddress": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
			"id": "03d684838b68cd9b05277c58f8e023da4037815b7507b50e26b2e40616fc3bd5"
		},
		{
			"amount": "199462057268212",
			"tokenStandard": "zts1c7py5fkcxgf3h7g8y8tct2",
			"weightedAmount": "199462057268212",
			"startTime": 1000018220,
			"revokeTime": 0,
			"expirationTime": 1000021820,
			"stakeAddress": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
			"id": "b3f1f5b0bc0eee6b529507f2754a5fc1fa507d9c006707b00be27953f16d1493"
		},
		{
			"amount": "199462057268212",
			"tokenStandard": "zts1ejte9v853kw5ydexhc68es",
			"weightedAmount": "199462057268212",
			"startTime": 1000018260,
			"revokeTime": 0,
			"expirationTime": 1000021860,
			"stakeAddress": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
			"id": "66710f2e36a85f0c3394f09dc7c97f9ecb1cd64a7ae36e78c725cf815381ff8c"
		},
		{
			"amount": "199462057268212",
			"tokenStandard": "zts1emn6vr3e20tc8gjq3kh6aw",
			"weightedAmount": "199462057268212",
			"startTime": 1000018280,
			"revokeTime": 0,
			"expirationTime": 1000021880,
			"stakeAddress": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
			"id": "3f1e0561aadb430607b73bae1ed1937865b973af9b3dfe1d82ad2db9ad4d012d"
		}
	]
}`)
	stakes2, err = liquidityAPI.GetLiquidityStakeEntriesByAddress(g.User2.Address, 0, 10)
	common.Json(stakes2, err).Equals(t, `
{
	"totalAmount": "797848229072848",
	"totalWeightedAmount": "797848229072848",
	"count": 4,
	"list": [
		{
			"amount": "199462057268212",
			"tokenStandard": "zts1gl30jmp4qd8slt5kwcxtru",
			"weightedAmount": "199462057268212",
			"startTime": 1000018120,
			"revokeTime": 0,
			"expirationTime": 1000021720,
			"stakeAddress": "z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx",
			"id": "5b95e7bffdae5cae44a6f6774dd4adddfafd064a0357a9d5896b7b8389c8684b"
		},
		{
			"amount": "199462057268212",
			"tokenStandard": "zts13pu3yrev9rqm4qjhwpfez9",
			"weightedAmount": "199462057268212",
			"startTime": 1000018180,
			"revokeTime": 0,
			"expirationTime": 1000021780,
			"stakeAddress": "z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx",
			"id": "a4de82d0fc2b79657ae34380eac21a53e6839af25078d4733d1f27e464a9d36f"
		},
		{
			"amount": "199462057268212",
			"tokenStandard": "zts1c7py5fkcxgf3h7g8y8tct2",
			"weightedAmount": "199462057268212",
			"startTime": 1000018240,
			"revokeTime": 0,
			"expirationTime": 1000021840,
			"stakeAddress": "z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx",
			"id": "441c85b14820b8085382e28b140d58d4138eef07308be106bf2616045b3fb0a6"
		},
		{
			"amount": "199462057268212",
			"tokenStandard": "zts1emn6vr3e20tc8gjq3kh6aw",
			"weightedAmount": "199462057268212",
			"startTime": 1000018300,
			"revokeTime": 0,
			"expirationTime": 1000021900,
			"stakeAddress": "z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx",
			"id": "b1519131d644714862f6e59f51ad1b7940c0f39494121d85af8a0c4774b9c3ac"
		}
	]
}`)
	// cancel some stakes
	for i := 0; i < 3; i++ {
		defer z.CallContract(cancelStakeLiq(g.User1.Address, stakes1.Entries[i].Id)).Error(t, nil)
		insertMomentums(z, 2)
	}

	defer z.CallContract(cancelStakeLiq(g.User2.Address, stakes2.Entries[0].Id)).Error(t, nil)
	insertMomentums(z, 2)

	// we now have 7 set tuples
	// 2 tuples with no stakes (0,1)
	// one tuple with both stakes canceled (2)
	// 1 tuple with one cancelled stake (3)
	// 1 tuple with one cancelled and one active stake (4)
	// 1 tuple with one stake (5)
	// 1 tuple with 2 stakes (6)
	// 2 deleted tuples with stakes (7, 8)
	// 1 deleted tuple with no stake (9)

	// ~30% of the rewards should go to the contract because only 2 stakes had no entries
	// the entries that got canceled stakes still had them for some time and even 1 min is enought to get all the rewards
	// (673920000009-617760000007) / 187200000000 = 0.3
	z.InsertMomentumsTo(360*7 + 10)

	z.ExpectBalance(types.LiquidityContract, types.ZnnTokenStandard, 673920000009)
	z.ExpectBalance(types.LiquidityContract, types.QsrTokenStandard, 1800000000009)

	// all rewards should go to contract
	defer z.CallContract(setIsHalted(g.User5.Address, true)).Error(t, nil)
	insertMomentums(z, 2)

	z.InsertMomentumsTo(360*8 + 10)

	z.ExpectBalance(types.LiquidityContract, types.ZnnTokenStandard, 861120000009)
	z.ExpectBalance(types.LiquidityContract, types.QsrTokenStandard, 2300000000009)
}

func TestLiquidity_TestScenariosWithAdditionalRewards(t *testing.T) {
	z := mock.NewMockZenonWithCustomEpochDuration(t, time.Hour)
	defer z.StopPanic()
	//defer z.SaveLogs(common.EmbeddedLogger).HideHashes().Equals(t, ``)

	// activate sporks and set guardians
	activateLiquidityStep1(t, z)

	znnPercentages := []uint32{uint32(1000), uint32(1000), uint32(1000), uint32(1000), uint32(1000), uint32(1000), uint32(1000), uint32(1000), uint32(1000), uint32(1000)}
	qsrPercentages := []uint32{uint32(1000), uint32(1000), uint32(1000), uint32(1000), uint32(1000), uint32(1000), uint32(1000), uint32(1000), uint32(1000), uint32(1000)}
	minAmounts := []*big.Int{big.NewInt(1000), big.NewInt(2000), big.NewInt(1000), big.NewInt(2000), big.NewInt(1000), big.NewInt(2000), big.NewInt(1000), big.NewInt(2000), big.NewInt(1000), big.NewInt(2000)}
	// create 10 tokens

	bigOneBytes, _ := base64.StdEncoding.DecodeString("tWjhEdP000")
	bigOne := big.NewInt(0).SetBytes(bigOneBytes)
	bigThousand := big.NewInt(0).SetBytes(bigOne.Bytes())
	bigThousand.Mul(bigThousand, big.NewInt(1000))
	bigHT := big.NewInt(0).SetBytes(bigThousand.Bytes())
	bigHT.Mul(bigHT, big.NewInt(100))
	decimals := uint8(18)
	for i := 0; i < 10; i++ {
		name := fmt.Sprintf("testToken%d", i)
		symbol := fmt.Sprintf("TT%d", i)
		defer z.CallContract(issue(g.User1.Address, name, symbol, "", bigThousand, bigHT, decimals, true, true, true)).
			Error(t, nil)
		insertMomentums(z, 2)
	}
	tokenAPI := embedded.NewTokenApi(z)
	tokenList, err := tokenAPI.GetByOwner(g.User1.Address, 0, 10)
	common.FailIfErr(t, err)

	// mint to users
	tokensToSet := make([]types.ZenonTokenStandard, 0)
	tokensToSetString := make([]string, 0)
	for i := 0; i < 10; i++ {
		tokensToSetString = append(tokensToSetString, tokenList.List[i].ZenonTokenStandard.String())
		tokensToSet = append(tokensToSet, tokenList.List[i].ZenonTokenStandard)
		defer z.CallContract(mint(g.User1.Address, tokenList.List[i].ZenonTokenStandard, bigThousand, g.User2.Address)).
			Error(t, nil)
		insertMomentums(z, 2)
		defer z.CallContract(mint(g.User1.Address, tokenList.List[i].ZenonTokenStandard, bigThousand, g.User3.Address)).
			Error(t, nil)
		insertMomentums(z, 2)
	}
	autoreceive(t, z, g.User1.Address)
	autoreceive(t, z, g.User2.Address)
	autoreceive(t, z, g.User3.Address)

	// set them
	setTokensTuple(t, z, g.User5.Address, tokensToSetString, znnPercentages, qsrPercentages, minAmounts)
	liquidityAPI := embedded.NewLiquidityApi(z)
	common.Json(liquidityAPI.GetLiquidityInfo()).Equals(t, `
{
	"administrator": "z1qqaswvt0e3cc5sm7lygkyza9ra63cr8e6zre09",
	"isHalted": false,
	"znnReward": "0",
	"qsrReward": "0",
	"tokenTuples": [
		{
			"tokenStandard": "zts1r8z2feqa3lsz9t78rgy772",
			"znnPercentage": 1000,
			"qsrPercentage": 1000,
			"minAmount": "1000"
		},
		{
			"tokenStandard": "zts196fjtffgd8jj3c99slvnt7",
			"znnPercentage": 1000,
			"qsrPercentage": 1000,
			"minAmount": "2000"
		},
		{
			"tokenStandard": "zts1gl30jmp4qd8slt5kwcxtru",
			"znnPercentage": 1000,
			"qsrPercentage": 1000,
			"minAmount": "1000"
		},
		{
			"tokenStandard": "zts129hkhzcdys8aacwwl7r7re",
			"znnPercentage": 1000,
			"qsrPercentage": 1000,
			"minAmount": "2000"
		},
		{
			"tokenStandard": "zts13pu3yrev9rqm4qjhwpfez9",
			"znnPercentage": 1000,
			"qsrPercentage": 1000,
			"minAmount": "1000"
		},
		{
			"tokenStandard": "zts1kc4lg8jey6hfkkcycr9rpv",
			"znnPercentage": 1000,
			"qsrPercentage": 1000,
			"minAmount": "2000"
		},
		{
			"tokenStandard": "zts1c7py5fkcxgf3h7g8y8tct2",
			"znnPercentage": 1000,
			"qsrPercentage": 1000,
			"minAmount": "1000"
		},
		{
			"tokenStandard": "zts1ejte9v853kw5ydexhc68es",
			"znnPercentage": 1000,
			"qsrPercentage": 1000,
			"minAmount": "2000"
		},
		{
			"tokenStandard": "zts1emn6vr3e20tc8gjq3kh6aw",
			"znnPercentage": 1000,
			"qsrPercentage": 1000,
			"minAmount": "1000"
		},
		{
			"tokenStandard": "zts1eaas9e0f30wfcdgp433xhd",
			"znnPercentage": 1000,
			"qsrPercentage": 1000,
			"minAmount": "2000"
		}
	]
}`)

	// the first update was at height 360
	z.ExpectBalance(types.LiquidityContract, types.ZnnTokenStandard, 187200000000)
	z.ExpectBalance(types.LiquidityContract, types.QsrTokenStandard, 500000000000)

	z.InsertMomentumsTo(360*2 + 10)

	setAdditionalReward(t, z, g.User5.Address, big.NewInt(200*g.Zexp), big.NewInt(2000*g.Zexp))

	// rewards are minted to the contract as no stake entries are found
	z.ExpectBalance(types.LiquidityContract, types.ZnnTokenStandard, 187200000000*2)
	z.ExpectBalance(types.LiquidityContract, types.QsrTokenStandard, 500000000000*2)

	// stake some tokens
	for i := 0; i < 6; i++ {
		defer z.CallContract(liquidityStake(g.User1.Address, tokensToSet[i], bigOne, 1)).Error(t, nil)
		insertMomentums(z, 2)
	}
	for i := 4; i < 10; i++ {
		defer z.CallContract(liquidityStake(g.User2.Address, tokensToSet[i], bigOne, 1)).Error(t, nil)
		insertMomentums(z, 2)
	}

	z.InsertMomentumsTo(360*3 + 10)
	// both rewards added should be 187200000000 + 200 additional znn and 500000000000 + 2000 additional qsr
	user1Rewards, _ := liquidityAPI.GetUncollectedReward(g.User1.Address)
	user2Rewards, _ := liquidityAPI.GetUncollectedReward(g.User2.Address)
	znnSum := big.NewInt(0)
	znnSum.Add(znnSum, user1Rewards.Znn).Add(znnSum, user2Rewards.Znn)
	qsrSum := big.NewInt(0)
	qsrSum.Add(qsrSum, user1Rewards.Qsr).Add(qsrSum, user2Rewards.Qsr)
	common.Expect(t, znnSum.Uint64(), 207199999998)
	common.Expect(t, qsrSum.Uint64(), 699999999998)

	defer z.CallContract(collectReward(g.User1.Address)).Error(t, nil)
	insertMomentums(z, 3)
	defer z.CallContract(collectReward(g.User2.Address)).Error(t, nil)
	insertMomentums(z, 3)

	// balance should have additional rewards less
	z.ExpectBalance(types.LiquidityContract, types.ZnnTokenStandard, 187200000000*2+2-200*g.Zexp)
	z.ExpectBalance(types.LiquidityContract, types.QsrTokenStandard, 500000000000*2+2-2000*g.Zexp)

	insertMomentums(z, 30)
	stakes1, err := liquidityAPI.GetLiquidityStakeEntriesByAddress(g.User1.Address, 0, 10)
	common.FailIfErr(t, err)
	stakes2, err := liquidityAPI.GetLiquidityStakeEntriesByAddress(g.User2.Address, 0, 10)
	common.FailIfErr(t, err)
	// cancel all stakes
	for i := 0; i < 6; i++ {
		defer z.CallContract(cancelStakeLiq(g.User1.Address, stakes1.Entries[i].Id)).Error(t, nil)
		insertMomentums(z, 2)
		defer z.CallContract(cancelStakeLiq(g.User2.Address, stakes2.Entries[i].Id)).Error(t, nil)
		insertMomentums(z, 2)
	}

	z.InsertMomentumsTo(360*4 + 10)

	// rewards have been distributed + 100 znn and 1000 qsr on each
	common.Json(liquidityAPI.GetUncollectedReward(g.User1.Address)).Equals(t, `
{
	"address": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
	"znnAmount": "106147697996",
	"qsrAmount": "358607087826"
}`)
	common.Json(liquidityAPI.GetUncollectedReward(g.User2.Address)).Equals(t, `
{
	"address": "z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx",
	"znnAmount": "101052302002",
	"qsrAmount": "341392912172"
}`)

	// balance should have - 2 additional rewards
	z.ExpectBalance(types.LiquidityContract, types.ZnnTokenStandard, 187200000000*2+4-200*2*g.Zexp)
	z.ExpectBalance(types.LiquidityContract, types.QsrTokenStandard, 500000000000*2+4-2000*2*g.Zexp)

	z.InsertMomentumsTo(360*5 + 10)

	// balance should be minted to the contract now
	z.ExpectBalance(types.LiquidityContract, types.ZnnTokenStandard, 187200000000*3+4-200*2*g.Zexp)
	z.ExpectBalance(types.LiquidityContract, types.QsrTokenStandard, 500000000000*3+4-2000*2*g.Zexp)

	// stake each token but the first two and the last one
	for i := 2; i < 9; i++ {
		defer z.CallContract(liquidityStake(g.User1.Address, tokensToSet[i], bigOne, 1)).Error(t, nil)
		insertMomentums(z, 2)
		// some tokens have 2 stakes
		if i%2 == 0 {
			defer z.CallContract(liquidityStake(g.User2.Address, tokensToSet[i], bigOne, 1)).Error(t, nil)
			insertMomentums(z, 2)
		}
	}
	// delete the last 3 tuples
	znnPercentages = []uint32{1500, 1500, 1500, 1500, 1500, 1500, 1000}
	qsrPercentages = []uint32{1500, 1500, 1500, 1500, 1500, 1500, 1000}
	setTokensTuple(t, z, g.User5.Address, tokensToSetString[:7], znnPercentages, qsrPercentages, minAmounts[:7])

	z.InsertMomentumsTo(360*6 + 10)

	defer z.CallContract(collectReward(g.User1.Address)).Error(t, nil)
	insertMomentums(z, 3)
	defer z.CallContract(collectReward(g.User2.Address)).Error(t, nil)
	insertMomentums(z, 3)

	// as some stake don't have entries, amount is less than 1872 * 4 - 400 for znn and less than 5000 * 4 - 4000 for qsr
	z.ExpectBalance(types.LiquidityContract, types.ZnnTokenStandard, 563760000007)
	z.ExpectBalance(types.LiquidityContract, types.QsrTokenStandard, 1110000000007)

	z.InsertMomentumsTo(360*6 + 180)

	stakes1, err = liquidityAPI.GetLiquidityStakeEntriesByAddress(g.User1.Address, 0, 10)
	common.Json(stakes1, err).Equals(t, `
{
	"totalAmount": "1396234400877484",
	"totalWeightedAmount": "1396234400877484",
	"count": 7,
	"list": [
		{
			"amount": "199462057268212",
			"tokenStandard": "zts1gl30jmp4qd8slt5kwcxtru",
			"weightedAmount": "199462057268212",
			"startTime": 1000018100,
			"revokeTime": 0,
			"expirationTime": 1000021700,
			"stakeAddress": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
			"id": "6bf235ba6df49d27ca269cccddf852a2e71b23ae5a98d91843ea9e5ba3070b67"
		},
		{
			"amount": "199462057268212",
			"tokenStandard": "zts129hkhzcdys8aacwwl7r7re",
			"weightedAmount": "199462057268212",
			"startTime": 1000018140,
			"revokeTime": 0,
			"expirationTime": 1000021740,
			"stakeAddress": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
			"id": "468ffd339aca907988b2cba552344005ab34bfe7e345092770a8899f903f7900"
		},
		{
			"amount": "199462057268212",
			"tokenStandard": "zts13pu3yrev9rqm4qjhwpfez9",
			"weightedAmount": "199462057268212",
			"startTime": 1000018160,
			"revokeTime": 0,
			"expirationTime": 1000021760,
			"stakeAddress": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
			"id": "61b1d6e73f4bccde98742dd013f0884273fdeb6243ab8d76c117a68b45fb9b82"
		},
		{
			"amount": "199462057268212",
			"tokenStandard": "zts1kc4lg8jey6hfkkcycr9rpv",
			"weightedAmount": "199462057268212",
			"startTime": 1000018200,
			"revokeTime": 0,
			"expirationTime": 1000021800,
			"stakeAddress": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
			"id": "081158361eed80d008a5bfcb7daba3de139d84d03da9d095eb5408fd82d91ddf"
		},
		{
			"amount": "199462057268212",
			"tokenStandard": "zts1c7py5fkcxgf3h7g8y8tct2",
			"weightedAmount": "199462057268212",
			"startTime": 1000018220,
			"revokeTime": 0,
			"expirationTime": 1000021820,
			"stakeAddress": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
			"id": "ac7b1258bac27fb78126d3520429aacc79cb7dd11b1372ce29c3cf49ff33080e"
		},
		{
			"amount": "199462057268212",
			"tokenStandard": "zts1ejte9v853kw5ydexhc68es",
			"weightedAmount": "199462057268212",
			"startTime": 1000018260,
			"revokeTime": 0,
			"expirationTime": 1000021860,
			"stakeAddress": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
			"id": "4798779359065c465aed8b76a8ebd3c7cf1987ae6ac67851e80c403dbd1f3469"
		},
		{
			"amount": "199462057268212",
			"tokenStandard": "zts1emn6vr3e20tc8gjq3kh6aw",
			"weightedAmount": "199462057268212",
			"startTime": 1000018280,
			"revokeTime": 0,
			"expirationTime": 1000021880,
			"stakeAddress": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
			"id": "61155785cde5f709f3576036b872d528b470cc79a9be0b94db539d508a88aaa3"
		}
	]
}`)
	stakes2, err = liquidityAPI.GetLiquidityStakeEntriesByAddress(g.User2.Address, 0, 10)
	common.Json(stakes2, err).Equals(t, `
{
	"totalAmount": "797848229072848",
	"totalWeightedAmount": "797848229072848",
	"count": 4,
	"list": [
		{
			"amount": "199462057268212",
			"tokenStandard": "zts1gl30jmp4qd8slt5kwcxtru",
			"weightedAmount": "199462057268212",
			"startTime": 1000018120,
			"revokeTime": 0,
			"expirationTime": 1000021720,
			"stakeAddress": "z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx",
			"id": "e691977e7ff10a6fa140a0c7ff049e2bc07fcdb89fa9802da50157db29501c59"
		},
		{
			"amount": "199462057268212",
			"tokenStandard": "zts13pu3yrev9rqm4qjhwpfez9",
			"weightedAmount": "199462057268212",
			"startTime": 1000018180,
			"revokeTime": 0,
			"expirationTime": 1000021780,
			"stakeAddress": "z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx",
			"id": "3ef084e4df6034c1b41ff581fdde62984bd0770929ac7df0d192202d90160166"
		},
		{
			"amount": "199462057268212",
			"tokenStandard": "zts1c7py5fkcxgf3h7g8y8tct2",
			"weightedAmount": "199462057268212",
			"startTime": 1000018240,
			"revokeTime": 0,
			"expirationTime": 1000021840,
			"stakeAddress": "z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx",
			"id": "dca0e7a095e9b177aa14785bed435e1db173fd1dc9612c5f8969b5a7403cbb27"
		},
		{
			"amount": "199462057268212",
			"tokenStandard": "zts1emn6vr3e20tc8gjq3kh6aw",
			"weightedAmount": "199462057268212",
			"startTime": 1000018300,
			"revokeTime": 0,
			"expirationTime": 1000021900,
			"stakeAddress": "z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx",
			"id": "08a6c8cfe318234893700227d3a24d16f94e99b948e573d99b258374a82a45a8"
		}
	]
}`)
	// cancel some stakes
	for i := 0; i < 3; i++ {
		defer z.CallContract(cancelStakeLiq(g.User1.Address, stakes1.Entries[i].Id)).Error(t, nil)
		insertMomentums(z, 2)
	}

	defer z.CallContract(cancelStakeLiq(g.User2.Address, stakes2.Entries[0].Id)).Error(t, nil)
	insertMomentums(z, 2)

	// we now have 7 set tuples
	// 2 tuples with no stakes (0,1)
	// one tuple with both stakes canceled (2)
	// 1 tuple with one cancelled stake (3)
	// 1 tuple with one cancelled and one active stake (4)
	// 1 tuple with one stake (5)
	// 1 tuple with 2 stakes (6)
	// 2 deleted tuples with stakes (7, 8)
	// 1 deleted tuple with no stake (9)

	// ~30% of the rewards should go to the contract because only 2 stakes had no entries
	// the entries that got canceled stakes still had them for some time and even 1 min is enough to get all the rewards
	z.InsertMomentumsTo(360*7 + 10)

	z.ExpectBalance(types.LiquidityContract, types.ZnnTokenStandard, 605920000009)
	z.ExpectBalance(types.LiquidityContract, types.QsrTokenStandard, 1120000000009)

	// all rewards should go to contract
	defer z.CallContract(setIsHalted(g.User5.Address, true)).Error(t, nil)
	insertMomentums(z, 2)

	z.InsertMomentumsTo(360*8 + 10)

	z.ExpectBalance(types.LiquidityContract, types.ZnnTokenStandard, 793120000009)
	z.ExpectBalance(types.LiquidityContract, types.QsrTokenStandard, 1620000000009)
}

// Add LIQ1 token tuple (min: 1000, percentage: 70% znn, 30% qsr)
// Add LIQ2 token tuple (min: 2000, percentage: 30% znn, 70% qsr)
// Register an entry for User 1 (token: LIQ1, amount: 10*10^8)
// Register an entry for User 1 (token: LIQ2, amount: 10*10^8)
// Update for epoch 0 (znn-amount: 1872*10^8, qsr-amount: 5000*10^8)
// Rewards for znn: 70% * 1872*10^8 = 13104*10^7 znn, 30% * 5000*10^8 = 1500*10^8 qsr
// Rewards for qsr: 30% * 1872*10^8 = 5616*10^7 znn, 70% * 5000*10^8 = 3500*10^8 qsr
// Rewards for first entry (token: LIQ1, amount: 10*10^8) -> 100% * 13104*10^7 znn, 100% * 1500*10^8 qsr
// Rewards for second entry (token: LIQ2, amount: 10*10^8) -> 100% * 5616*10^7 znn, 100% * 3500*10^8 qsr
// Total rewards -> (13104 + 5616) * 10^7 znn, (1500 + 3500) * 10^8 qsr
func TestLiquidity_LiquidityStakeAndUpdate1(t *testing.T) {
	z := mock.NewMockZenonWithCustomEpochDuration(t, time.Hour)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).HideHashes().Equals(t, `
t=2001-09-09T01:46:50+0000 lvl=dbug msg=created module=embedded contract=spork spork="&{Id:XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX Name:spork-accelerator Description:activate spork for accelerator Activated:false EnforcementHeight:0}"
t=2001-09-09T01:47:00+0000 lvl=dbug msg=activated module=embedded contract=spork spork="&{Id:XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX Name:spork-accelerator Description:activate spork for accelerator Activated:true EnforcementHeight:9}"
t=2001-09-09T01:50:00+0000 lvl=dbug msg=created module=embedded contract=spork spork="&{Id:XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX Name:spork-bridge Description:activate spork for bridge Activated:false EnforcementHeight:0}"
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=0 pillar-name=TEST-pillar-1 reward="&{DelegationReward:+10363852800000 BlockReward:+8568000000000 TotalReward:+18931852800000 ProducedBlockNum:119 ExpectedBlockNum:120 Weight:+2100000000000}" total-weight=2500000000000 self-weight=2100000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=0 pillar-name=TEST-pillar-cool reward="&{DelegationReward:+995328000000 BlockReward:+8640000000000 TotalReward:+9635328000000 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2500000000000 self-weight=200000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=0 pillar-name=TEST-pillar-znn reward="&{DelegationReward:+995328000000 BlockReward:+8640000000000 TotalReward:+9635328000000 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2500000000000 self-weight=200000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=pillar epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxsentynelxxxxxxxxxxxxxwy0r2r current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating sentinel reward" module=embedded contract=sentinel epoch=0 total-znn-reward=187200000000 total-qsr-reward=500000000000 cumulated-sentinel=0 start-time=1000000000 end-time=1000003600
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=sentinel epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating stake reward" module=embedded contract=stake epoch=0 total-reward=1000000000000 cumulated-stake=0 start-time=1000000000 end-time=1000003600
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=stake epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating liquidity stake reward" module=embedded contract=liquidity epoch=0 znn-total-amount=187200000000 qsr-total-amount=500000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating liquidity balance" module=embedded contract=liquidity epoch=0 znnReward=187200000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating liquidity balance" module=embedded contract=liquidity epoch=0 qsrReward=500000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxaccelerat0rxxxxxxxxxxp4tk22 current-height=361 last-update-height=0
t=2001-09-09T02:46:50+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg TokenName:Zenon Coin TokenSymbol:ZNN TokenDomain:zenon.network TotalSupply:+19687200000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1znnxxxxxxxxxxxxx9z4ulx}" minted-amount=187200000000 to-address=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae
t=2001-09-09T02:46:50+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 TokenName:QuasarCoin TokenSymbol:QSR TokenDomain:zenon.network TotalSupply:+181050000000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1qsrxxxxxxxxxxxxxmrhjll}" minted-amount=500000000000 to-address=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae
t=2001-09-09T02:47:00+0000 lvl=info msg="received donation" module=embedded contract=common embedded=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae from-address=z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0 zts=zts1znnxxxxxxxxxxxxx9z4ulx amount=187200000000
t=2001-09-09T02:47:00+0000 lvl=info msg="received donation" module=embedded contract=common embedded=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae from-address=z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0 zts=zts1qsrxxxxxxxxxxxxxmrhjll amount=500000000000
t=2001-09-09T03:14:20+0000 lvl=dbug msg="issued ZTS" module=embedded contract=token token="{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenName:test.tok3n_liquidity-1 TokenSymbol:LIQ1 TokenDomain: TotalSupply:+10000000000 MaxSupply:+100000000000 Decimals:6 IsMintable:true IsBurnable:true IsUtility:false TokenStandard:zts1992nq43xn2urz3wttklc8z}"
t=2001-09-09T03:14:40+0000 lvl=dbug msg="issued ZTS" module=embedded contract=token token="{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenName:test.tok3n_liquidity-2 TokenSymbol:LIQ2 TokenDomain: TotalSupply:+20000000000 MaxSupply:+200000000000 Decimals:6 IsMintable:true IsBurnable:true IsUtility:false TokenStandard:zts1fres5c39axw805xswvt55j}"
t=2001-09-09T03:15:00+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenName:test.tok3n_liquidity-1 TokenSymbol:LIQ1 TokenDomain: TotalSupply:+20000000000 MaxSupply:+100000000000 Decimals:6 IsMintable:true IsBurnable:true IsUtility:false TokenStandard:zts1992nq43xn2urz3wttklc8z}" minted-amount=10000000000 to-address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz
t=2001-09-09T03:15:20+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenName:test.tok3n_liquidity-2 TokenSymbol:LIQ2 TokenDomain: TotalSupply:+40000000000 MaxSupply:+200000000000 Decimals:6 IsMintable:true IsBurnable:true IsUtility:false TokenStandard:zts1fres5c39axw805xswvt55j}" minted-amount=20000000000 to-address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz
t=2001-09-09T03:18:00+0000 lvl=dbug msg="created liquidity stake entry" module=embedded contract=stake id=XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX owner=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz amount=1000000000 weighted-amount=1000000000 duration-in-days=0
t=2001-09-09T03:18:20+0000 lvl=dbug msg="created liquidity stake entry" module=embedded contract=stake id=XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX owner=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz amount=1000000000 weighted-amount=1000000000 duration-in-days=0
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg current-height=722 last-update-height=361
t=2001-09-09T03:46:50+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=1 pillar-name=TEST-pillar-1 reward="&{DelegationReward:+10450877642587 BlockReward:+8640000000000 TotalReward:+19090877642587 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+2099916666666}" total-weight=2499916666666 self-weight=2099916666666
t=2001-09-09T03:46:50+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=1 pillar-name=TEST-pillar-cool reward="&{DelegationReward:+995361178706 BlockReward:+8640000000000 TotalReward:+9635361178706 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2499916666666 self-weight=200000000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=1 pillar-name=TEST-pillar-znn reward="&{DelegationReward:+995361178706 BlockReward:+8640000000000 TotalReward:+9635361178706 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2499916666666 self-weight=200000000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=pillar epoch=2
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxsentynelxxxxxxxxxxxxxwy0r2r current-height=722 last-update-height=361
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating sentinel reward" module=embedded contract=sentinel epoch=1 total-znn-reward=187200000000 total-qsr-reward=500000000000 cumulated-sentinel=0 start-time=1000003600 end-time=1000007200
t=2001-09-09T03:46:50+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=sentinel epoch=2
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 current-height=722 last-update-height=361
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating stake reward" module=embedded contract=stake epoch=1 total-reward=1000000000000 cumulated-stake=0 start-time=1000003600 end-time=1000007200
t=2001-09-09T03:46:50+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=stake epoch=2
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae current-height=722 last-update-height=361
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating liquidity stake reward" module=embedded contract=liquidity epoch=1 znn-total-amount=187200000000 qsr-total-amount=500000000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="calculating percentages for each token" module=embedded contract=liquidity epoch=1 token-standard=zts1992nq43xn2urz3wttklc8z znn-percentage=7000 qsr-percentage=3000 znn-rewards=131040000000 qsr-rewards=150000000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="calculating percentages for each token" module=embedded contract=liquidity epoch=1 token-standard=zts1fres5c39axw805xswvt55j znn-percentage=3000 qsr-percentage=7000 znn-rewards=56160000000 qsr-rewards=350000000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating liquidity stake reward" module=embedded contract=liquidity id=XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX stake-address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz token-standard=zts1fres5c39axw805xswvt55j znn-amount=56160000000 qsr-amount=350000000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating liquidity stake reward" module=embedded contract=liquidity id=XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX stake-address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz token-standard=zts1992nq43xn2urz3wttklc8z znn-amount=131040000000 qsr-amount=150000000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxaccelerat0rxxxxxxxxxxp4tk22 current-height=722 last-update-height=361
`)

	// activate sporks and set guardians
	activateLiquidityStep1(t, z)

	liquidityAPI := embedded.NewLiquidityApi(z)
	znnPercentages := []uint32{uint32(7000), uint32(3000)}
	qsrPercentages := []uint32{uint32(3000), uint32(7000)}
	minAmounts := []*big.Int{big.NewInt(1000), big.NewInt(2000)}
	issueMultipleTokensSetup(t, z)
	setTokensTuple(t, z, g.User5.Address, tokensString, znnPercentages, qsrPercentages, minAmounts)

	z.ExpectBalance(g.User1.Address, tokens[0], 200*g.Zexp)
	defer z.CallContract(liquidityStake(g.User1.Address, tokens[0], big.NewInt(10*g.Zexp), 1)).
		Error(t, nil)
	insertMomentums(z, 2)
	z.ExpectBalance(g.User1.Address, tokens[0], 190*g.Zexp)
	common.Json(liquidityAPI.GetLiquidityStakeEntriesByAddress(g.User1.Address, 0, 5)).HideHashes().Equals(t, `
{
	"totalAmount": "1000000000",
	"totalWeightedAmount": "1000000000",
	"count": 1,
	"list": [
		{
			"amount": "1000000000",
			"tokenStandard": "zts1992nq43xn2urz3wttklc8z",
			"weightedAmount": "1000000000",
			"startTime": 1000005480,
			"revokeTime": 0,
			"expirationTime": 1000009080,
			"stakeAddress": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
			"id": "XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX"
		}
	]
}`)
	defer z.CallContract(liquidityStake(g.User1.Address, tokens[1], big.NewInt(10*g.Zexp), 1)).
		Error(t, nil)
	insertMomentums(z, 2)
	common.Json(liquidityAPI.GetLiquidityStakeEntriesByAddress(g.User1.Address, 0, 5)).HideHashes().Equals(t, `
{
	"totalAmount": "2000000000",
	"totalWeightedAmount": "2000000000",
	"count": 2,
	"list": [
		{
			"amount": "1000000000",
			"tokenStandard": "zts1992nq43xn2urz3wttklc8z",
			"weightedAmount": "1000000000",
			"startTime": 1000005480,
			"revokeTime": 0,
			"expirationTime": 1000009080,
			"stakeAddress": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
			"id": "XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX"
		},
		{
			"amount": "1000000000",
			"tokenStandard": "zts1fres5c39axw805xswvt55j",
			"weightedAmount": "1000000000",
			"startTime": 1000005500,
			"revokeTime": 0,
			"expirationTime": 1000009100,
			"stakeAddress": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
			"id": "XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX"
		}
	]
}`)
	insertMomentums(z, 300)
	common.Json(liquidityAPI.GetUncollectedReward(g.User1.Address)).Equals(t, `
{
	"address": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
	"znnAmount": "187200000000",
	"qsrAmount": "500000000000"
}`)
}

// Add LIQ1 token tuple (min: 1000, percentage: 70% znn, 30% qsr)
// Add LIQ2 token tuple (min: 2000, percentage: 30% znn, 70% qsr)
// Register an entry for User 1 (token: LIQ1, amount: 10*10^8)
// Update for epoch 0 (znn-amount: 1872*10^8, qsr-amount: 5000*10^8)
// Rewards for znn: 70% * 1872*10^8 = 13104*10^7 znn, 30% * 5000*10^8 = 1500*10^8 qsr
// Rewards for qsr: 30% * 1872*10^8 = 5616*10^7 znn, 70% * 5000*10^8 = 3500*10^8 qsr
// Rewards for first entry (token: LIQ1, amount: 10*10^8) -> 100% * 13104*10^7 znn, 100% * 1500*10^8 qsr
// Minted ZTS (token: znn, amount: 5616*10^7)
// Minted ZTS (token: qsr, amount: 3500*10^8)
func TestLiquidity_LiquidityStakeAndUpdate2(t *testing.T) {
	z := mock.NewMockZenonWithCustomEpochDuration(t, time.Hour)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).HideHashes().Equals(t, `
t=2001-09-09T01:46:50+0000 lvl=dbug msg=created module=embedded contract=spork spork="&{Id:XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX Name:spork-accelerator Description:activate spork for accelerator Activated:false EnforcementHeight:0}"
t=2001-09-09T01:47:00+0000 lvl=dbug msg=activated module=embedded contract=spork spork="&{Id:XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX Name:spork-accelerator Description:activate spork for accelerator Activated:true EnforcementHeight:9}"
t=2001-09-09T01:50:00+0000 lvl=dbug msg=created module=embedded contract=spork spork="&{Id:XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX Name:spork-bridge Description:activate spork for bridge Activated:false EnforcementHeight:0}"
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=0 pillar-name=TEST-pillar-1 reward="&{DelegationReward:+10363852800000 BlockReward:+8568000000000 TotalReward:+18931852800000 ProducedBlockNum:119 ExpectedBlockNum:120 Weight:+2100000000000}" total-weight=2500000000000 self-weight=2100000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=0 pillar-name=TEST-pillar-cool reward="&{DelegationReward:+995328000000 BlockReward:+8640000000000 TotalReward:+9635328000000 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2500000000000 self-weight=200000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=0 pillar-name=TEST-pillar-znn reward="&{DelegationReward:+995328000000 BlockReward:+8640000000000 TotalReward:+9635328000000 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2500000000000 self-weight=200000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=pillar epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxsentynelxxxxxxxxxxxxxwy0r2r current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating sentinel reward" module=embedded contract=sentinel epoch=0 total-znn-reward=187200000000 total-qsr-reward=500000000000 cumulated-sentinel=0 start-time=1000000000 end-time=1000003600
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=sentinel epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating stake reward" module=embedded contract=stake epoch=0 total-reward=1000000000000 cumulated-stake=0 start-time=1000000000 end-time=1000003600
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=stake epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating liquidity stake reward" module=embedded contract=liquidity epoch=0 znn-total-amount=187200000000 qsr-total-amount=500000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating liquidity balance" module=embedded contract=liquidity epoch=0 znnReward=187200000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating liquidity balance" module=embedded contract=liquidity epoch=0 qsrReward=500000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxaccelerat0rxxxxxxxxxxp4tk22 current-height=361 last-update-height=0
t=2001-09-09T02:46:50+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg TokenName:Zenon Coin TokenSymbol:ZNN TokenDomain:zenon.network TotalSupply:+19687200000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1znnxxxxxxxxxxxxx9z4ulx}" minted-amount=187200000000 to-address=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae
t=2001-09-09T02:46:50+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 TokenName:QuasarCoin TokenSymbol:QSR TokenDomain:zenon.network TotalSupply:+181050000000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1qsrxxxxxxxxxxxxxmrhjll}" minted-amount=500000000000 to-address=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae
t=2001-09-09T02:47:00+0000 lvl=info msg="received donation" module=embedded contract=common embedded=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae from-address=z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0 zts=zts1znnxxxxxxxxxxxxx9z4ulx amount=187200000000
t=2001-09-09T02:47:00+0000 lvl=info msg="received donation" module=embedded contract=common embedded=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae from-address=z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0 zts=zts1qsrxxxxxxxxxxxxxmrhjll amount=500000000000
t=2001-09-09T03:14:20+0000 lvl=dbug msg="issued ZTS" module=embedded contract=token token="{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenName:test.tok3n_liquidity-1 TokenSymbol:LIQ1 TokenDomain: TotalSupply:+10000000000 MaxSupply:+100000000000 Decimals:6 IsMintable:true IsBurnable:true IsUtility:false TokenStandard:zts1992nq43xn2urz3wttklc8z}"
t=2001-09-09T03:14:40+0000 lvl=dbug msg="issued ZTS" module=embedded contract=token token="{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenName:test.tok3n_liquidity-2 TokenSymbol:LIQ2 TokenDomain: TotalSupply:+20000000000 MaxSupply:+200000000000 Decimals:6 IsMintable:true IsBurnable:true IsUtility:false TokenStandard:zts1fres5c39axw805xswvt55j}"
t=2001-09-09T03:15:00+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenName:test.tok3n_liquidity-1 TokenSymbol:LIQ1 TokenDomain: TotalSupply:+20000000000 MaxSupply:+100000000000 Decimals:6 IsMintable:true IsBurnable:true IsUtility:false TokenStandard:zts1992nq43xn2urz3wttklc8z}" minted-amount=10000000000 to-address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz
t=2001-09-09T03:15:20+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenName:test.tok3n_liquidity-2 TokenSymbol:LIQ2 TokenDomain: TotalSupply:+40000000000 MaxSupply:+200000000000 Decimals:6 IsMintable:true IsBurnable:true IsUtility:false TokenStandard:zts1fres5c39axw805xswvt55j}" minted-amount=20000000000 to-address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz
t=2001-09-09T03:18:00+0000 lvl=dbug msg="created liquidity stake entry" module=embedded contract=stake id=XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX owner=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz amount=1000000000 weighted-amount=1000000000 duration-in-days=0
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg current-height=722 last-update-height=361
t=2001-09-09T03:46:50+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=1 pillar-name=TEST-pillar-1 reward="&{DelegationReward:+10450877642587 BlockReward:+8640000000000 TotalReward:+19090877642587 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+2099916666666}" total-weight=2499916666666 self-weight=2099916666666
t=2001-09-09T03:46:50+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=1 pillar-name=TEST-pillar-cool reward="&{DelegationReward:+995361178706 BlockReward:+8640000000000 TotalReward:+9635361178706 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2499916666666 self-weight=200000000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=1 pillar-name=TEST-pillar-znn reward="&{DelegationReward:+995361178706 BlockReward:+8640000000000 TotalReward:+9635361178706 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2499916666666 self-weight=200000000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=pillar epoch=2
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxsentynelxxxxxxxxxxxxxwy0r2r current-height=722 last-update-height=361
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating sentinel reward" module=embedded contract=sentinel epoch=1 total-znn-reward=187200000000 total-qsr-reward=500000000000 cumulated-sentinel=0 start-time=1000003600 end-time=1000007200
t=2001-09-09T03:46:50+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=sentinel epoch=2
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 current-height=722 last-update-height=361
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating stake reward" module=embedded contract=stake epoch=1 total-reward=1000000000000 cumulated-stake=0 start-time=1000003600 end-time=1000007200
t=2001-09-09T03:46:50+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=stake epoch=2
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae current-height=722 last-update-height=361
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating liquidity stake reward" module=embedded contract=liquidity epoch=1 znn-total-amount=187200000000 qsr-total-amount=500000000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="calculating percentages for each token" module=embedded contract=liquidity epoch=1 token-standard=zts1992nq43xn2urz3wttklc8z znn-percentage=7000 qsr-percentage=3000 znn-rewards=131040000000 qsr-rewards=150000000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="calculating percentages for each token" module=embedded contract=liquidity epoch=1 token-standard=zts1fres5c39axw805xswvt55j znn-percentage=3000 qsr-percentage=7000 znn-rewards=56160000000 qsr-rewards=350000000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating liquidity stake reward" module=embedded contract=liquidity id=XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX stake-address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz token-standard=zts1992nq43xn2urz3wttklc8z znn-amount=131040000000 qsr-amount=150000000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating liquidity balance" module=embedded contract=liquidity epoch=1 znnReward=56160000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating liquidity balance" module=embedded contract=liquidity epoch=1 qsrReward=350000000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxaccelerat0rxxxxxxxxxxp4tk22 current-height=722 last-update-height=361
t=2001-09-09T03:47:00+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg TokenName:Zenon Coin TokenSymbol:ZNN TokenDomain:zenon.network TotalSupply:+19743360000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1znnxxxxxxxxxxxxx9z4ulx}" minted-amount=56160000000 to-address=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae
t=2001-09-09T03:47:00+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 TokenName:QuasarCoin TokenSymbol:QSR TokenDomain:zenon.network TotalSupply:+181400000000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1qsrxxxxxxxxxxxxxmrhjll}" minted-amount=350000000000 to-address=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae
t=2001-09-09T03:47:10+0000 lvl=info msg="received donation" module=embedded contract=common embedded=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae from-address=z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0 zts=zts1znnxxxxxxxxxxxxx9z4ulx amount=56160000000
t=2001-09-09T03:47:10+0000 lvl=info msg="received donation" module=embedded contract=common embedded=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae from-address=z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0 zts=zts1qsrxxxxxxxxxxxxxmrhjll amount=350000000000
`)
	// activate sporks and set guardians
	activateLiquidityStep1(t, z)

	liquidityAPI := embedded.NewLiquidityApi(z)
	znnPercentages := []uint32{uint32(7000), uint32(3000)}
	qsrPercentages := []uint32{uint32(3000), uint32(7000)}
	minAmounts := []*big.Int{big.NewInt(1000), big.NewInt(2000)}
	issueMultipleTokensSetup(t, z)
	setTokensTuple(t, z, g.User5.Address, tokensString, znnPercentages, qsrPercentages, minAmounts)

	z.ExpectBalance(g.User1.Address, tokens[0], 200*g.Zexp)
	defer z.CallContract(liquidityStake(g.User1.Address, tokens[0], big.NewInt(10*g.Zexp), 1)).
		Error(t, nil)
	insertMomentums(z, 2)
	z.ExpectBalance(g.User1.Address, tokens[0], 190*g.Zexp)
	common.Json(liquidityAPI.GetLiquidityStakeEntriesByAddress(g.User1.Address, 0, 5)).HideHashes().Equals(t, `
{
	"totalAmount": "1000000000",
	"totalWeightedAmount": "1000000000",
	"count": 1,
	"list": [
		{
			"amount": "1000000000",
			"tokenStandard": "zts1992nq43xn2urz3wttklc8z",
			"weightedAmount": "1000000000",
			"startTime": 1000005480,
			"revokeTime": 0,
			"expirationTime": 1000009080,
			"stakeAddress": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
			"id": "XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX"
		}
	]
}`)

	insertMomentums(z, 300)
	common.Json(liquidityAPI.GetUncollectedReward(g.User1.Address)).Equals(t, `
{
	"address": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
	"znnAmount": "131040000000",
	"qsrAmount": "150000000000"
}`)
}

// Add LIQ1 token tuple (min: 1000, percentage: 70% znn, 30% qsr)
// Add LIQ2 token tuple (min: 2000, percentage: 30% znn, 70% qsr)
// Register an entry for User 1 (token: LIQ1, amount: 10*10^8)
// Register an entry for User 2 (token: LIQ1, amount: 10*10^8)
// Register an entry for User 3 (token: LIQ1, amount: 10*10^8)
// Register an entry for User 4 (token: LIQ1, amount: 10*10^8)
// Update for epoch 0 (znn-amount: 1872*10^8, qsr-amount: 5000*10^8)
// Rewards for znn: 70% * 1872*10^8 = 13104*10^7 znn, 30% * 5000*10^8 = 1500*10^8 qsr
// Rewards for qsr: 30% * 1872*10^8 = 5616*10^7 znn, 70% * 5000*10^8 = 3500*10^8 qsr
// Rewards for first entry (token: LIQ1, amount: 10*10^8, User1) -> 25% * 13104*10^7 = 3276 * 10^7 znn, 25% * 1500*10^8 = 375 * 10^8 qsr
// Rewards for first entry (token: LIQ1, amount: 10*10^8, User2) -> 25% * 13104*10^7 = 3276 * 10^7 znn, 25% * 1500*10^8 = 375 * 10^8 qsr
// Rewards for first entry (token: LIQ1, amount: 10*10^8, User3) -> 25% * 13104*10^7 = 3276 * 10^7 znn, 25% * 1500*10^8 = 375 * 10^8 qsr
// Rewards for first entry (token: LIQ1, amount: 10*10^8, User4) -> 25% * 13104*10^7 = 3276 * 10^7 znn, 25% * 1500*10^8 = 375 * 10^8 qsr
// Minted ZTS (token: znn, amount: 5616*10^7)
// Minted ZTS (token: qsr, amount: 3500*10^8)
// Total rewards: 33512526799 + 32709831546 + 32509157733 + 32308483920 + 56160000002 = 1872 * 10^8 znn,
//
//	38361408882 + 37442572741 + 37212863705 + 36983154670 + 350000000002 = 5000 * 10^8 qsr
func TestLiquidity_LiquidityStakeAndUpdate3(t *testing.T) {
	z := mock.NewMockZenonWithCustomEpochDuration(t, time.Hour)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).Equals(t, `
t=2001-09-09T01:46:50+0000 lvl=dbug msg=created module=embedded contract=spork spork="&{Id:34d8229bd07586c243c6e74122a18d6d2002694c72964a7186111026a9cec6ab Name:spork-accelerator Description:activate spork for accelerator Activated:false EnforcementHeight:0}"
t=2001-09-09T01:47:00+0000 lvl=dbug msg=activated module=embedded contract=spork spork="&{Id:34d8229bd07586c243c6e74122a18d6d2002694c72964a7186111026a9cec6ab Name:spork-accelerator Description:activate spork for accelerator Activated:true EnforcementHeight:9}"
t=2001-09-09T01:50:00+0000 lvl=dbug msg=created module=embedded contract=spork spork="&{Id:46bc4121cd6a2dff8dde2d1b8a8d52d3e8e91888ded459fa2c36d8e829bc6251 Name:spork-bridge Description:activate spork for bridge Activated:false EnforcementHeight:0}"
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=0 pillar-name=TEST-pillar-1 reward="&{DelegationReward:+10363852800000 BlockReward:+8568000000000 TotalReward:+18931852800000 ProducedBlockNum:119 ExpectedBlockNum:120 Weight:+2100000000000}" total-weight=2500000000000 self-weight=2100000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=0 pillar-name=TEST-pillar-cool reward="&{DelegationReward:+995328000000 BlockReward:+8640000000000 TotalReward:+9635328000000 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2500000000000 self-weight=200000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=0 pillar-name=TEST-pillar-znn reward="&{DelegationReward:+995328000000 BlockReward:+8640000000000 TotalReward:+9635328000000 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2500000000000 self-weight=200000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=pillar epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxsentynelxxxxxxxxxxxxxwy0r2r current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating sentinel reward" module=embedded contract=sentinel epoch=0 total-znn-reward=187200000000 total-qsr-reward=500000000000 cumulated-sentinel=0 start-time=1000000000 end-time=1000003600
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=sentinel epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating stake reward" module=embedded contract=stake epoch=0 total-reward=1000000000000 cumulated-stake=0 start-time=1000000000 end-time=1000003600
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=stake epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating liquidity stake reward" module=embedded contract=liquidity epoch=0 znn-total-amount=187200000000 qsr-total-amount=500000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating liquidity balance" module=embedded contract=liquidity epoch=0 znnReward=187200000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating liquidity balance" module=embedded contract=liquidity epoch=0 qsrReward=500000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxaccelerat0rxxxxxxxxxxp4tk22 current-height=361 last-update-height=0
t=2001-09-09T02:46:50+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg TokenName:Zenon Coin TokenSymbol:ZNN TokenDomain:zenon.network TotalSupply:+19687200000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1znnxxxxxxxxxxxxx9z4ulx}" minted-amount=187200000000 to-address=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae
t=2001-09-09T02:46:50+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 TokenName:QuasarCoin TokenSymbol:QSR TokenDomain:zenon.network TotalSupply:+181050000000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1qsrxxxxxxxxxxxxxmrhjll}" minted-amount=500000000000 to-address=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae
t=2001-09-09T02:47:00+0000 lvl=info msg="received donation" module=embedded contract=common embedded=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae from-address=z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0 zts=zts1znnxxxxxxxxxxxxx9z4ulx amount=187200000000
t=2001-09-09T02:47:00+0000 lvl=info msg="received donation" module=embedded contract=common embedded=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae from-address=z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0 zts=zts1qsrxxxxxxxxxxxxxmrhjll amount=500000000000
t=2001-09-09T03:14:20+0000 lvl=dbug msg="issued ZTS" module=embedded contract=token token="{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenName:test.tok3n_liquidity-1 TokenSymbol:LIQ1 TokenDomain: TotalSupply:+10000000000 MaxSupply:+100000000000 Decimals:6 IsMintable:true IsBurnable:true IsUtility:false TokenStandard:zts1992nq43xn2urz3wttklc8z}"
t=2001-09-09T03:14:40+0000 lvl=dbug msg="issued ZTS" module=embedded contract=token token="{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenName:test.tok3n_liquidity-2 TokenSymbol:LIQ2 TokenDomain: TotalSupply:+20000000000 MaxSupply:+200000000000 Decimals:6 IsMintable:true IsBurnable:true IsUtility:false TokenStandard:zts1fres5c39axw805xswvt55j}"
t=2001-09-09T03:15:00+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenName:test.tok3n_liquidity-1 TokenSymbol:LIQ1 TokenDomain: TotalSupply:+20000000000 MaxSupply:+100000000000 Decimals:6 IsMintable:true IsBurnable:true IsUtility:false TokenStandard:zts1992nq43xn2urz3wttklc8z}" minted-amount=10000000000 to-address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz
t=2001-09-09T03:15:20+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenName:test.tok3n_liquidity-2 TokenSymbol:LIQ2 TokenDomain: TotalSupply:+40000000000 MaxSupply:+200000000000 Decimals:6 IsMintable:true IsBurnable:true IsUtility:false TokenStandard:zts1fres5c39axw805xswvt55j}" minted-amount=20000000000 to-address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz
t=2001-09-09T03:18:00+0000 lvl=dbug msg="created liquidity stake entry" module=embedded contract=stake id=e18402c36d70374b12ec38e1711cac8e1875d907b6f21e892aecd87deb85105e owner=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz amount=1000000000 weighted-amount=1000000000 duration-in-days=0
t=2001-09-09T03:18:20+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenName:test.tok3n_liquidity-1 TokenSymbol:LIQ1 TokenDomain: TotalSupply:+50000000000 MaxSupply:+100000000000 Decimals:6 IsMintable:true IsBurnable:true IsUtility:false TokenStandard:zts1992nq43xn2urz3wttklc8z}" minted-amount=30000000000 to-address=z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx
t=2001-09-09T03:18:40+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenName:test.tok3n_liquidity-1 TokenSymbol:LIQ1 TokenDomain: TotalSupply:+70000000000 MaxSupply:+100000000000 Decimals:6 IsMintable:true IsBurnable:true IsUtility:false TokenStandard:zts1992nq43xn2urz3wttklc8z}" minted-amount=20000000000 to-address=z1qrs2lpccnsneglhnnfwvlsj0qncnxjnwlfmjac
t=2001-09-09T03:19:00+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenName:test.tok3n_liquidity-1 TokenSymbol:LIQ1 TokenDomain: TotalSupply:+100000000000 MaxSupply:+100000000000 Decimals:6 IsMintable:true IsBurnable:true IsUtility:false TokenStandard:zts1992nq43xn2urz3wttklc8z}" minted-amount=30000000000 to-address=z1qraz4ermhhua89a0h0gxxan4lnzrfutgs6xxe2
t=2001-09-09T03:19:20+0000 lvl=dbug msg="created liquidity stake entry" module=embedded contract=stake id=29cb31d09a348152ff1f5a876d43b8359781ece9bd75eb24c3b496901e789fad owner=z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx amount=1000000000 weighted-amount=1000000000 duration-in-days=0
t=2001-09-09T03:19:40+0000 lvl=dbug msg="created liquidity stake entry" module=embedded contract=stake id=639a6e125562bc224585d3040502d93e93f3e9c0ee6ba0c10242449eb1e3918f owner=z1qrs2lpccnsneglhnnfwvlsj0qncnxjnwlfmjac amount=1000000000 weighted-amount=1000000000 duration-in-days=0
t=2001-09-09T03:20:00+0000 lvl=dbug msg="created liquidity stake entry" module=embedded contract=stake id=4a4cacd92d72262e76a0fd36710119cb1eb8d99edd1ac6b958e4d89557692e4a owner=z1qraz4ermhhua89a0h0gxxan4lnzrfutgs6xxe2 amount=1000000000 weighted-amount=1000000000 duration-in-days=0
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg current-height=722 last-update-height=361
t=2001-09-09T03:46:50+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=1 pillar-name=TEST-pillar-1 reward="&{DelegationReward:+10450877642587 BlockReward:+8640000000000 TotalReward:+19090877642587 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+2099916666666}" total-weight=2499916666666 self-weight=2099916666666
t=2001-09-09T03:46:50+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=1 pillar-name=TEST-pillar-cool reward="&{DelegationReward:+995361178706 BlockReward:+8640000000000 TotalReward:+9635361178706 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2499916666666 self-weight=200000000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=1 pillar-name=TEST-pillar-znn reward="&{DelegationReward:+995361178706 BlockReward:+8640000000000 TotalReward:+9635361178706 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2499916666666 self-weight=200000000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=pillar epoch=2
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxsentynelxxxxxxxxxxxxxwy0r2r current-height=722 last-update-height=361
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating sentinel reward" module=embedded contract=sentinel epoch=1 total-znn-reward=187200000000 total-qsr-reward=500000000000 cumulated-sentinel=0 start-time=1000003600 end-time=1000007200
t=2001-09-09T03:46:50+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=sentinel epoch=2
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 current-height=722 last-update-height=361
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating stake reward" module=embedded contract=stake epoch=1 total-reward=1000000000000 cumulated-stake=0 start-time=1000003600 end-time=1000007200
t=2001-09-09T03:46:50+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=stake epoch=2
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae current-height=722 last-update-height=361
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating liquidity stake reward" module=embedded contract=liquidity epoch=1 znn-total-amount=187200000000 qsr-total-amount=500000000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="calculating percentages for each token" module=embedded contract=liquidity epoch=1 token-standard=zts1992nq43xn2urz3wttklc8z znn-percentage=7000 qsr-percentage=3000 znn-rewards=131040000000 qsr-rewards=150000000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="calculating percentages for each token" module=embedded contract=liquidity epoch=1 token-standard=zts1fres5c39axw805xswvt55j znn-percentage=3000 qsr-percentage=7000 znn-rewards=56160000000 qsr-rewards=350000000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating liquidity stake reward" module=embedded contract=liquidity id=e18402c36d70374b12ec38e1711cac8e1875d907b6f21e892aecd87deb85105e stake-address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz token-standard=zts1992nq43xn2urz3wttklc8z znn-amount=34253617021 qsr-amount=39209726443
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating liquidity stake reward" module=embedded contract=liquidity id=639a6e125562bc224585d3040502d93e93f3e9c0ee6ba0c10242449eb1e3918f stake-address=z1qrs2lpccnsneglhnnfwvlsj0qncnxjnwlfmjac token-standard=zts1992nq43xn2urz3wttklc8z znn-amount=32262127659 qsr-amount=36930091185
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating liquidity stake reward" module=embedded contract=liquidity id=29cb31d09a348152ff1f5a876d43b8359781ece9bd75eb24c3b496901e789fad stake-address=z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx token-standard=zts1992nq43xn2urz3wttklc8z znn-amount=32660425531 qsr-amount=37386018237
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating liquidity stake reward" module=embedded contract=liquidity id=4a4cacd92d72262e76a0fd36710119cb1eb8d99edd1ac6b958e4d89557692e4a stake-address=z1qraz4ermhhua89a0h0gxxan4lnzrfutgs6xxe2 token-standard=zts1992nq43xn2urz3wttklc8z znn-amount=31863829787 qsr-amount=36474164133
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating liquidity balance" module=embedded contract=liquidity epoch=1 znnReward=56160000002
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating liquidity balance" module=embedded contract=liquidity epoch=1 qsrReward=350000000002
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxaccelerat0rxxxxxxxxxxp4tk22 current-height=722 last-update-height=361
t=2001-09-09T03:47:00+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg TokenName:Zenon Coin TokenSymbol:ZNN TokenDomain:zenon.network TotalSupply:+19743360000002 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1znnxxxxxxxxxxxxx9z4ulx}" minted-amount=56160000002 to-address=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae
t=2001-09-09T03:47:00+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 TokenName:QuasarCoin TokenSymbol:QSR TokenDomain:zenon.network TotalSupply:+181400000000002 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1qsrxxxxxxxxxxxxxmrhjll}" minted-amount=350000000002 to-address=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae
t=2001-09-09T03:47:10+0000 lvl=info msg="received donation" module=embedded contract=common embedded=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae from-address=z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0 zts=zts1znnxxxxxxxxxxxxx9z4ulx amount=56160000002
t=2001-09-09T03:47:10+0000 lvl=info msg="received donation" module=embedded contract=common embedded=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae from-address=z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0 zts=zts1qsrxxxxxxxxxxxxxmrhjll amount=350000000002
`)
	// activate sporks and set guardians
	activateLiquidityStep1(t, z)

	liquidityAPI := embedded.NewLiquidityApi(z)
	znnPercentages := []uint32{uint32(7000), uint32(3000)}
	qsrPercentages := []uint32{uint32(3000), uint32(7000)}
	minAmounts := []*big.Int{big.NewInt(1000), big.NewInt(2000)}
	issueMultipleTokensSetup(t, z)
	setTokensTuple(t, z, g.User5.Address, tokensString, znnPercentages, qsrPercentages, minAmounts)

	z.ExpectBalance(g.User1.Address, tokens[0], 200*g.Zexp)
	defer z.CallContract(liquidityStake(g.User1.Address, tokens[0], big.NewInt(10*g.Zexp), 1)).
		Error(t, nil)
	insertMomentums(z, 2)
	common.Json(liquidityAPI.GetLiquidityStakeEntriesByAddress(g.User1.Address, 0, 5)).HideHashes().Equals(t, `
{
	"totalAmount": "1000000000",
	"totalWeightedAmount": "1000000000",
	"count": 1,
	"list": [
		{
			"amount": "1000000000",
			"tokenStandard": "zts1992nq43xn2urz3wttklc8z",
			"weightedAmount": "1000000000",
			"startTime": 1000005480,
			"revokeTime": 0,
			"expirationTime": 1000009080,
			"stakeAddress": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
			"id": "XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX"
		}
	]
}`)
	z.ExpectBalance(g.User1.Address, tokens[0], 190*g.Zexp)

	mintForUsers(t, z)
	z.ExpectBalance(g.User2.Address, tokens[0], 300*g.Zexp)
	defer z.CallContract(liquidityStake(g.User2.Address, tokens[0], big.NewInt(10*g.Zexp), 1)).
		Error(t, nil)
	insertMomentums(z, 2)
	z.ExpectBalance(g.User2.Address, tokens[0], 290*g.Zexp)

	z.ExpectBalance(g.User3.Address, tokens[0], 200*g.Zexp)
	defer z.CallContract(liquidityStake(g.User3.Address, tokens[0], big.NewInt(10*g.Zexp), 1)).
		Error(t, nil)
	insertMomentums(z, 2)
	z.ExpectBalance(g.User3.Address, tokens[0], 190*g.Zexp)

	z.ExpectBalance(g.User4.Address, tokens[0], 300*g.Zexp)
	defer z.CallContract(liquidityStake(g.User4.Address, tokens[0], big.NewInt(10*g.Zexp), 1)).
		Error(t, nil)
	insertMomentums(z, 2)
	z.ExpectBalance(g.User4.Address, tokens[0], 290*g.Zexp)

	common.Json(liquidityAPI.GetLiquidityStakeEntriesByAddress(g.User1.Address, 0, 5)).HideHashes().Equals(t, `
{
	"totalAmount": "1000000000",
	"totalWeightedAmount": "1000000000",
	"count": 1,
	"list": [
		{
			"amount": "1000000000",
			"tokenStandard": "zts1992nq43xn2urz3wttklc8z",
			"weightedAmount": "1000000000",
			"startTime": 1000005480,
			"revokeTime": 0,
			"expirationTime": 1000009080,
			"stakeAddress": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
			"id": "XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX"
		}
	]
}`)
	common.Json(liquidityAPI.GetLiquidityStakeEntriesByAddress(g.User2.Address, 0, 5)).HideHashes().Equals(t, `
{
	"totalAmount": "1000000000",
	"totalWeightedAmount": "1000000000",
	"count": 1,
	"list": [
		{
			"amount": "1000000000",
			"tokenStandard": "zts1992nq43xn2urz3wttklc8z",
			"weightedAmount": "1000000000",
			"startTime": 1000005560,
			"revokeTime": 0,
			"expirationTime": 1000009160,
			"stakeAddress": "z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx",
			"id": "XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX"
		}
	]
}`)
	common.Json(liquidityAPI.GetLiquidityStakeEntriesByAddress(g.User3.Address, 0, 5)).HideHashes().Equals(t, `
{
	"totalAmount": "1000000000",
	"totalWeightedAmount": "1000000000",
	"count": 1,
	"list": [
		{
			"amount": "1000000000",
			"tokenStandard": "zts1992nq43xn2urz3wttklc8z",
			"weightedAmount": "1000000000",
			"startTime": 1000005580,
			"revokeTime": 0,
			"expirationTime": 1000009180,
			"stakeAddress": "z1qrs2lpccnsneglhnnfwvlsj0qncnxjnwlfmjac",
			"id": "XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX"
		}
	]
}`)
	common.Json(liquidityAPI.GetLiquidityStakeEntriesByAddress(g.User4.Address, 0, 5)).HideHashes().Equals(t, `
{
	"totalAmount": "1000000000",
	"totalWeightedAmount": "1000000000",
	"count": 1,
	"list": [
		{
			"amount": "1000000000",
			"tokenStandard": "zts1992nq43xn2urz3wttklc8z",
			"weightedAmount": "1000000000",
			"startTime": 1000005600,
			"revokeTime": 0,
			"expirationTime": 1000009200,
			"stakeAddress": "z1qraz4ermhhua89a0h0gxxan4lnzrfutgs6xxe2",
			"id": "XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX"
		}
	]
}`)
	insertMomentums(z, 500)

	common.Json(liquidityAPI.GetUncollectedReward(g.User1.Address)).Equals(t, `
{
	"address": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
	"znnAmount": "34253617021",
	"qsrAmount": "39209726443"
}`)
	common.Json(liquidityAPI.GetUncollectedReward(g.User2.Address)).Equals(t, `
{
	"address": "z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx",
	"znnAmount": "32660425531",
	"qsrAmount": "37386018237"
}`)
	common.Json(liquidityAPI.GetUncollectedReward(g.User3.Address)).Equals(t, `
{
	"address": "z1qrs2lpccnsneglhnnfwvlsj0qncnxjnwlfmjac",
	"znnAmount": "32262127659",
	"qsrAmount": "36930091185"
}`)
	common.Json(liquidityAPI.GetUncollectedReward(g.User4.Address)).Equals(t, `
{
	"address": "z1qraz4ermhhua89a0h0gxxan4lnzrfutgs6xxe2",
	"znnAmount": "31863829787",
	"qsrAmount": "36474164133"
}`)

	z.ExpectBalance(types.LiquidityContract, types.ZnnTokenStandard, 243360000002)
	z.ExpectBalance(types.LiquidityContract, types.QsrTokenStandard, 850000000002)
}

func TestLiquidity_Donate(t *testing.T) {
	z := mock.NewMockZenonWithCustomEpochDuration(t, time.Hour)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).HideHashes().Equals(t, `
t=2001-09-09T01:46:50+0000 lvl=dbug msg=created module=embedded contract=spork spork="&{Id:XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX Name:spork-accelerator Description:activate spork for accelerator Activated:false EnforcementHeight:0}"
t=2001-09-09T01:47:00+0000 lvl=dbug msg=activated module=embedded contract=spork spork="&{Id:XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX Name:spork-accelerator Description:activate spork for accelerator Activated:true EnforcementHeight:9}"
t=2001-09-09T01:50:00+0000 lvl=info msg="received donation" module=embedded contract=common embedded=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae from-address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz zts=zts1znnxxxxxxxxxxxxx9z4ulx amount=100
t=2001-09-09T01:50:20+0000 lvl=info msg="received donation" module=embedded contract=common embedded=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae from-address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz zts=zts1qsrxxxxxxxxxxxxxmrhjll amount=100
t=2001-09-09T01:50:40+0000 lvl=dbug msg=created module=embedded contract=spork spork="&{Id:XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX Name:spork-bridge Description:activate spork for bridge Activated:false EnforcementHeight:0}"
t=2001-09-09T01:51:00+0000 lvl=info msg="received donation" module=embedded contract=common embedded=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae from-address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz zts=zts1znnxxxxxxxxxxxxx9z4ulx amount=100
t=2001-09-09T01:51:20+0000 lvl=info msg="received donation" module=embedded contract=common embedded=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae from-address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz zts=zts1qsrxxxxxxxxxxxxxmrhjll amount=100
`)

	activateAccelerator(z)

	z.ExpectBalance(types.LiquidityContract, types.ZnnTokenStandard, 0)
	z.ExpectBalance(types.LiquidityContract, types.QsrTokenStandard, 0)

	defer z.CallContract(donateLiq(g.User1.Address, types.ZnnTokenStandard, common.Big100)).Error(t, nil)
	insertMomentums(z, 2)
	defer z.CallContract(donateLiq(g.User1.Address, types.QsrTokenStandard, common.Big100)).Error(t, nil)
	insertMomentums(z, 2)

	z.ExpectBalance(types.LiquidityContract, types.ZnnTokenStandard, 100)
	z.ExpectBalance(types.LiquidityContract, types.QsrTokenStandard, 100)

	activateBridge(z)

	defer z.CallContract(donateLiq(g.User1.Address, types.ZnnTokenStandard, common.Big100)).Error(t, nil)
	insertMomentums(z, 2)
	defer z.CallContract(donateLiq(g.User1.Address, types.QsrTokenStandard, common.Big100)).Error(t, nil)
	insertMomentums(z, 2)

	z.ExpectBalance(types.LiquidityContract, types.ZnnTokenStandard, 200)
	z.ExpectBalance(types.LiquidityContract, types.QsrTokenStandard, 200)
}

func TestLiquidity_Update(t *testing.T) {
	z := mock.NewMockZenonWithCustomEpochDuration(t, time.Hour)
	defer z.StopPanic()
	//defer z.SaveLogs(common.EmbeddedLogger).HideHashes().Equals(t, ``)

	for k, _ := range types.ImplementedSporksMap {
		types.ImplementedSporksMap[k] = false
	}
	z.ExpectBalance(types.LiquidityContract, types.ZnnTokenStandard, 0)
	z.ExpectBalance(types.LiquidityContract, types.QsrTokenStandard, 0)

	activateAccelerator(z)

	z.InsertMomentumsTo(60*6*2 + 2)
	z.ExpectBalance(types.LiquidityContract, types.ZnnTokenStandard, 1872*g.Zexp)
	z.ExpectBalance(types.LiquidityContract, types.QsrTokenStandard, 5000*g.Zexp)

	activateBridge(z)

	z.InsertMomentumsTo(60*6*3 + 2)
	z.ExpectBalance(types.LiquidityContract, types.ZnnTokenStandard, 3744*g.Zexp)
	z.ExpectBalance(types.LiquidityContract, types.QsrTokenStandard, 10000*g.Zexp)
}

// Add Znn token tuple (min: 1000, percentage: 50% znn, 50% qsr)
// Add Qsr token tuple (min: 2000, percentage: 50% znn, 50% qsr)
// Set 20*10^8 znn reward from contract
// Set 12*10^8 qsr reward from contract
// Update for epoch 0 (znn-amount: 1872*10^8, qsr-amount: 5000*10^8) -> Send to contract
// Register an entry for User 1 (token: znn, amount: 10*10^8)
// Register an entry for User 1 (token: qsr, amount: 10*10^8)
// Update for epoch 1 (znn-amount: 1872*10^8 + 20*10^8, qsr-amount: 5000*10^8 + 12*10^8)
// Rewards for znn: 50% * 1892*10^8 = 946*10^8 znn, 50% * 5012*10^8 = 2506*10^8 qsr
// Rewards for qsr: 50% * 1892*10^8 = 946*10^8 znn, 50% * 5012*10^8 = 2506*10^8 qsr
// Rewards for first entry (token: znn, amount: 10*10^8) -> 100% * 946*10^8 znn, 100% * 2506*10^8 qsr
// Rewards for second entry (token: qsr, amount: 10*10^8) -> 100% * 946*10^8 znn, 100% * 2506*10^8 qsr
// Halt and check that no additional rewards are added to the user and are minted to the contract instead
func TestLiquidity_CollectReward1(t *testing.T) {
	z := mock.NewMockZenonWithCustomEpochDuration(t, time.Hour)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).HideHashes().Equals(t, `
t=2001-09-09T01:46:50+0000 lvl=dbug msg=created module=embedded contract=spork spork="&{Id:XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX Name:spork-accelerator Description:activate spork for accelerator Activated:false EnforcementHeight:0}"
t=2001-09-09T01:47:00+0000 lvl=dbug msg=activated module=embedded contract=spork spork="&{Id:XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX Name:spork-accelerator Description:activate spork for accelerator Activated:true EnforcementHeight:9}"
t=2001-09-09T01:50:00+0000 lvl=dbug msg=created module=embedded contract=spork spork="&{Id:XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX Name:spork-bridge Description:activate spork for bridge Activated:false EnforcementHeight:0}"
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=0 pillar-name=TEST-pillar-1 reward="&{DelegationReward:+10363852800000 BlockReward:+8568000000000 TotalReward:+18931852800000 ProducedBlockNum:119 ExpectedBlockNum:120 Weight:+2100000000000}" total-weight=2500000000000 self-weight=2100000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=0 pillar-name=TEST-pillar-cool reward="&{DelegationReward:+995328000000 BlockReward:+8640000000000 TotalReward:+9635328000000 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2500000000000 self-weight=200000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=0 pillar-name=TEST-pillar-znn reward="&{DelegationReward:+995328000000 BlockReward:+8640000000000 TotalReward:+9635328000000 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2500000000000 self-weight=200000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=pillar epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxsentynelxxxxxxxxxxxxxwy0r2r current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating sentinel reward" module=embedded contract=sentinel epoch=0 total-znn-reward=187200000000 total-qsr-reward=500000000000 cumulated-sentinel=0 start-time=1000000000 end-time=1000003600
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=sentinel epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating stake reward" module=embedded contract=stake epoch=0 total-reward=1000000000000 cumulated-stake=0 start-time=1000000000 end-time=1000003600
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=stake epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating liquidity stake reward" module=embedded contract=liquidity epoch=0 znn-total-amount=187200000000 qsr-total-amount=500000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating liquidity balance" module=embedded contract=liquidity epoch=0 znnReward=187200000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating liquidity balance" module=embedded contract=liquidity epoch=0 qsrReward=500000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxaccelerat0rxxxxxxxxxxp4tk22 current-height=361 last-update-height=0
t=2001-09-09T02:46:50+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg TokenName:Zenon Coin TokenSymbol:ZNN TokenDomain:zenon.network TotalSupply:+19687200000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1znnxxxxxxxxxxxxx9z4ulx}" minted-amount=187200000000 to-address=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae
t=2001-09-09T02:46:50+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 TokenName:QuasarCoin TokenSymbol:QSR TokenDomain:zenon.network TotalSupply:+181050000000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1qsrxxxxxxxxxxxxxmrhjll}" minted-amount=500000000000 to-address=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae
t=2001-09-09T02:47:00+0000 lvl=info msg="received donation" module=embedded contract=common embedded=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae from-address=z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0 zts=zts1znnxxxxxxxxxxxxx9z4ulx amount=187200000000
t=2001-09-09T02:47:00+0000 lvl=info msg="received donation" module=embedded contract=common embedded=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae from-address=z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0 zts=zts1qsrxxxxxxxxxxxxxmrhjll amount=500000000000
t=2001-09-09T03:14:20+0000 lvl=dbug msg="issued ZTS" module=embedded contract=token token="{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenName:test.tok3n_liquidity-1 TokenSymbol:LIQ1 TokenDomain: TotalSupply:+10000000000 MaxSupply:+100000000000 Decimals:6 IsMintable:true IsBurnable:true IsUtility:false TokenStandard:zts1992nq43xn2urz3wttklc8z}"
t=2001-09-09T03:14:40+0000 lvl=dbug msg="issued ZTS" module=embedded contract=token token="{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenName:test.tok3n_liquidity-2 TokenSymbol:LIQ2 TokenDomain: TotalSupply:+20000000000 MaxSupply:+200000000000 Decimals:6 IsMintable:true IsBurnable:true IsUtility:false TokenStandard:zts1fres5c39axw805xswvt55j}"
t=2001-09-09T03:15:00+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenName:test.tok3n_liquidity-1 TokenSymbol:LIQ1 TokenDomain: TotalSupply:+20000000000 MaxSupply:+100000000000 Decimals:6 IsMintable:true IsBurnable:true IsUtility:false TokenStandard:zts1992nq43xn2urz3wttklc8z}" minted-amount=10000000000 to-address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz
t=2001-09-09T03:15:20+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenName:test.tok3n_liquidity-2 TokenSymbol:LIQ2 TokenDomain: TotalSupply:+40000000000 MaxSupply:+200000000000 Decimals:6 IsMintable:true IsBurnable:true IsUtility:false TokenStandard:zts1fres5c39axw805xswvt55j}" minted-amount=20000000000 to-address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz
t=2001-09-09T03:20:20+0000 lvl=dbug msg="created liquidity stake entry" module=embedded contract=stake id=XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX owner=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz amount=1000000000 weighted-amount=1000000000 duration-in-days=0
t=2001-09-09T03:20:40+0000 lvl=dbug msg="created liquidity stake entry" module=embedded contract=stake id=XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX owner=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz amount=1000000000 weighted-amount=1000000000 duration-in-days=0
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg current-height=722 last-update-height=361
t=2001-09-09T03:46:50+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=1 pillar-name=TEST-pillar-1 reward="&{DelegationReward:+10450877642587 BlockReward:+8640000000000 TotalReward:+19090877642587 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+2099916666666}" total-weight=2499916666666 self-weight=2099916666666
t=2001-09-09T03:46:50+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=1 pillar-name=TEST-pillar-cool reward="&{DelegationReward:+995361178706 BlockReward:+8640000000000 TotalReward:+9635361178706 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2499916666666 self-weight=200000000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=1 pillar-name=TEST-pillar-znn reward="&{DelegationReward:+995361178706 BlockReward:+8640000000000 TotalReward:+9635361178706 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2499916666666 self-weight=200000000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=pillar epoch=2
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxsentynelxxxxxxxxxxxxxwy0r2r current-height=722 last-update-height=361
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating sentinel reward" module=embedded contract=sentinel epoch=1 total-znn-reward=187200000000 total-qsr-reward=500000000000 cumulated-sentinel=0 start-time=1000003600 end-time=1000007200
t=2001-09-09T03:46:50+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=sentinel epoch=2
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 current-height=722 last-update-height=361
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating stake reward" module=embedded contract=stake epoch=1 total-reward=1000000000000 cumulated-stake=0 start-time=1000003600 end-time=1000007200
t=2001-09-09T03:46:50+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=stake epoch=2
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae current-height=722 last-update-height=361
t=2001-09-09T03:46:50+0000 lvl=dbug msg="distribute znn rewards from the liquidity contract" module=embedded contract=liquidity znn-amount=2000000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="distribute qsr rewards from the liquidity contract" module=embedded contract=liquidity qsr-amount=1200000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating liquidity stake reward" module=embedded contract=liquidity epoch=1 znn-total-amount=189200000000 qsr-total-amount=501200000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="calculating percentages for each token" module=embedded contract=liquidity epoch=1 token-standard=zts1992nq43xn2urz3wttklc8z znn-percentage=5000 qsr-percentage=5000 znn-rewards=94600000000 qsr-rewards=250600000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="calculating percentages for each token" module=embedded contract=liquidity epoch=1 token-standard=zts1fres5c39axw805xswvt55j znn-percentage=5000 qsr-percentage=5000 znn-rewards=94600000000 qsr-rewards=250600000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating liquidity stake reward" module=embedded contract=liquidity id=XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX stake-address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz token-standard=zts1fres5c39axw805xswvt55j znn-amount=94600000000 qsr-amount=250600000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating liquidity stake reward" module=embedded contract=liquidity id=XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX stake-address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz token-standard=zts1992nq43xn2urz3wttklc8z znn-amount=94600000000 qsr-amount=250600000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxaccelerat0rxxxxxxxxxxp4tk22 current-height=722 last-update-height=361
t=2001-09-09T03:47:00+0000 lvl=dbug msg="burned ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg TokenName:Zenon Coin TokenSymbol:ZNN TokenDomain:zenon.network TotalSupply:+19685200000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1znnxxxxxxxxxxxxx9z4ulx}" burned-amount=2000000000
t=2001-09-09T03:47:00+0000 lvl=dbug msg="burned ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 TokenName:QuasarCoin TokenSymbol:QSR TokenDomain:zenon.network TotalSupply:+181048800000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1qsrxxxxxxxxxxxxxmrhjll}" burned-amount=1200000000
t=2001-09-09T04:11:10+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg TokenName:Zenon Coin TokenSymbol:ZNN TokenDomain:zenon.network TotalSupply:+19874400000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1znnxxxxxxxxxxxxx9z4ulx}" minted-amount=189200000000 to-address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz
t=2001-09-09T04:11:10+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 TokenName:QuasarCoin TokenSymbol:QSR TokenDomain:zenon.network TotalSupply:+181550000000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1qsrxxxxxxxxxxxxxmrhjll}" minted-amount=501200000000 to-address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz
t=2001-09-09T04:47:00+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg current-height=1083 last-update-height=722
t=2001-09-09T04:47:00+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=2 pillar-name=TEST-pillar-1 reward="&{DelegationReward:+10523376133209 BlockReward:+8640000000000 TotalReward:+19163376133209 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+2194400000000}" total-weight=2594400000000 self-weight=2194400000000
t=2001-09-09T04:47:00+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=2 pillar-name=TEST-pillar-cool reward="&{DelegationReward:+959111933395 BlockReward:+8640000000000 TotalReward:+9599111933395 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2594400000000 self-weight=200000000000
t=2001-09-09T04:47:00+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=2 pillar-name=TEST-pillar-znn reward="&{DelegationReward:+959111933395 BlockReward:+8640000000000 TotalReward:+9599111933395 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2594400000000 self-weight=200000000000
t=2001-09-09T04:47:00+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=pillar epoch=3
t=2001-09-09T04:47:00+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxsentynelxxxxxxxxxxxxxwy0r2r current-height=1083 last-update-height=722
t=2001-09-09T04:47:00+0000 lvl=dbug msg="updating sentinel reward" module=embedded contract=sentinel epoch=2 total-znn-reward=187200000000 total-qsr-reward=500000000000 cumulated-sentinel=0 start-time=1000007200 end-time=1000010800
t=2001-09-09T04:47:00+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=sentinel epoch=3
t=2001-09-09T04:47:00+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 current-height=1083 last-update-height=722
t=2001-09-09T04:47:00+0000 lvl=dbug msg="updating stake reward" module=embedded contract=stake epoch=2 total-reward=1000000000000 cumulated-stake=0 start-time=1000007200 end-time=1000010800
t=2001-09-09T04:47:00+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=stake epoch=3
t=2001-09-09T04:47:00+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae current-height=1083 last-update-height=722
t=2001-09-09T04:47:00+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxaccelerat0rxxxxxxxxxxp4tk22 current-height=1083 last-update-height=722
t=2001-09-09T04:47:10+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg TokenName:Zenon Coin TokenSymbol:ZNN TokenDomain:zenon.network TotalSupply:+20061600000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1znnxxxxxxxxxxxxx9z4ulx}" minted-amount=187200000000 to-address=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae
t=2001-09-09T04:47:10+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 TokenName:QuasarCoin TokenSymbol:QSR TokenDomain:zenon.network TotalSupply:+182050000000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1qsrxxxxxxxxxxxxxmrhjll}" minted-amount=500000000000 to-address=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae
t=2001-09-09T04:47:20+0000 lvl=info msg="received donation" module=embedded contract=common embedded=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae from-address=z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0 zts=zts1znnxxxxxxxxxxxxx9z4ulx amount=187200000000
t=2001-09-09T04:47:20+0000 lvl=info msg="received donation" module=embedded contract=common embedded=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae from-address=z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0 zts=zts1qsrxxxxxxxxxxxxxmrhjll amount=500000000000
`)

	// we have token pairs
	activateLiquidityStep2(t, z)

	liquidityAPI := embedded.NewLiquidityApi(z)

	z.ExpectBalance(types.LiquidityContract, types.ZnnTokenStandard, 1872*g.Zexp)
	z.ExpectBalance(types.LiquidityContract, types.QsrTokenStandard, 5000*g.Zexp)

	setAdditionalReward(t, z, g.User5.Address, big.NewInt(20*g.Zexp), big.NewInt(12*g.Zexp))

	z.ExpectBalance(types.LiquidityContract, types.ZnnTokenStandard, 1872*g.Zexp)
	z.ExpectBalance(types.LiquidityContract, types.QsrTokenStandard, 5000*g.Zexp)

	common.Json(liquidityAPI.GetLiquidityInfo()).Equals(t, `
{
	"administrator": "z1qqaswvt0e3cc5sm7lygkyza9ra63cr8e6zre09",
	"isHalted": false,
	"znnReward": "2000000000",
	"qsrReward": "1200000000",
	"tokenTuples": [
		{
			"tokenStandard": "zts1992nq43xn2urz3wttklc8z",
			"znnPercentage": 5000,
			"qsrPercentage": 5000,
			"minAmount": "1000"
		},
		{
			"tokenStandard": "zts1fres5c39axw805xswvt55j",
			"znnPercentage": 5000,
			"qsrPercentage": 5000,
			"minAmount": "2000"
		}
	]
}`)

	z.ExpectBalance(g.User1.Address, tokens[0], 200*g.Zexp)
	defer z.CallContract(liquidityStake(g.User1.Address, tokens[0], big.NewInt(10*g.Zexp), 1)).
		Error(t, nil)
	insertMomentums(z, 2)
	z.ExpectBalance(g.User1.Address, tokens[0], 190*g.Zexp)

	z.ExpectBalance(g.User1.Address, tokens[1], 400*g.Zexp)
	defer z.CallContract(liquidityStake(g.User1.Address, tokens[1], big.NewInt(10*g.Zexp), 1)).
		Error(t, nil)
	insertMomentums(z, 2)
	z.ExpectBalance(g.User1.Address, tokens[1], 390*g.Zexp)

	common.Json(liquidityAPI.GetLiquidityStakeEntriesByAddress(g.User1.Address, 0, 5)).Equals(t, `
{
	"totalAmount": "2000000000",
	"totalWeightedAmount": "2000000000",
	"count": 2,
	"list": [
		{
			"amount": "1000000000",
			"tokenStandard": "zts1992nq43xn2urz3wttklc8z",
			"weightedAmount": "1000000000",
			"startTime": 1000005620,
			"revokeTime": 0,
			"expirationTime": 1000009220,
			"stakeAddress": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
			"id": "ea9fb366a9592b7a6a78d0e5881bee440d727ee04eb20fafb29628e7c02054b6"
		},
		{
			"amount": "1000000000",
			"tokenStandard": "zts1fres5c39axw805xswvt55j",
			"weightedAmount": "1000000000",
			"startTime": 1000005640,
			"revokeTime": 0,
			"expirationTime": 1000009240,
			"stakeAddress": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
			"id": "32dcda291851bcb273b9c2d77d93dfa5a907d6723bdbb07a7854fd8a6411375d"
		}
	]
}`)
	insertMomentums(z, 300)
	common.Json(liquidityAPI.GetUncollectedReward(g.User1.Address)).Equals(t, `
{
	"address": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
	"znnAmount": "189200000000",
	"qsrAmount": "501200000000"
}`)
	z.ExpectBalance(types.LiquidityContract, types.ZnnTokenStandard, 1852*g.Zexp)
	z.ExpectBalance(types.LiquidityContract, types.QsrTokenStandard, 4988*g.Zexp)

	z.ExpectBalance(g.User1.Address, types.ZnnTokenStandard, 11998*g.Zexp)
	z.ExpectBalance(g.User1.Address, types.QsrTokenStandard, 120000*g.Zexp)
	defer z.CallContract(collectReward(g.User1.Address)).Error(t, nil)
	insertMomentums(z, 3)
	z.ExpectBalance(types.LiquidityContract, types.ZnnTokenStandard, 1852*g.Zexp)
	z.ExpectBalance(types.LiquidityContract, types.QsrTokenStandard, 4988*g.Zexp)

	common.Json(liquidityAPI.GetUncollectedReward(g.User1.Address)).Equals(t, `
{
	"address": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
	"znnAmount": "0",
	"qsrAmount": "0"
}`)
	z.ExpectBalance(g.User1.Address, types.ZnnTokenStandard, 11998*g.Zexp)
	z.ExpectBalance(g.User1.Address, types.QsrTokenStandard, 120000*g.Zexp)
	autoreceive(t, z, g.User1.Address)
	z.ExpectBalance(g.User1.Address, types.ZnnTokenStandard, 13890*g.Zexp)
	z.ExpectBalance(g.User1.Address, types.QsrTokenStandard, 125012*g.Zexp)

	// halt and check that rewards remain 0 after the next update
	defer z.CallContract(setIsHalted(g.User5.Address, true)).Error(t, nil)
	insertMomentums(z, 2)

	insertMomentums(z, 362)

	// rewards still 0
	common.Json(liquidityAPI.GetUncollectedReward(g.User1.Address)).Equals(t, `
{
	"address": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
	"znnAmount": "0",
	"qsrAmount": "0"
}`)

	// balance increases
	z.ExpectBalance(types.LiquidityContract, types.ZnnTokenStandard, 3724*g.Zexp)
	z.ExpectBalance(types.LiquidityContract, types.QsrTokenStandard, 9988*g.Zexp)
}

// Add Znn token tuple (min: 1000, percentage: 50% znn, 50% qsr)
// Add Qsr token tuple (min: 2000, percentage: 50% znn, 50% qsr)
// Set 50*10^8 znn reward from contract
// Set 12*10^8 qsr reward from contract
// Update for epoch 0 (znn-amount: 1872*10^8, qsr-amount: 5000*10^8) -> Send to contract
// Register an entry for User 1 (token: znn, amount: 10*10^8)
// Register an entry for User 2 (token: qsr, amount: 10*10^8)
// Update for epoch 1 (znn-amount: 1872*10^8 + 50*10^8, qsr-amount: 5000*10^8 + 12*10^8)
// Rewards for znn: 50% * 1992*10^8 = 961*10^8 znn, 50% * 5012*10^8 = 2506*10^8 qsr
// Rewards for qsr: 50% * 1992*10^8 = 961*10^8 znn, 50% * 5012*10^8 = 2506*10^8 qsr
// Rewards for first entry (token: znn, amount: 10*10^8, User1) -> 100% * 961*10^8 znn, 100% * 2506*10^8 qsr
// Rewards for second entry (token: qsr, amount: 10*10^8, User2) -> 100% * 961*10^8 znn, 100% * 2506*10^8 qsr
func TestLiquidity_CollectReward2(t *testing.T) {
	z := mock.NewMockZenonWithCustomEpochDuration(t, time.Hour)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).HideHashes().Equals(t, `
t=2001-09-09T01:46:50+0000 lvl=dbug msg=created module=embedded contract=spork spork="&{Id:XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX Name:spork-accelerator Description:activate spork for accelerator Activated:false EnforcementHeight:0}"
t=2001-09-09T01:47:00+0000 lvl=dbug msg=activated module=embedded contract=spork spork="&{Id:XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX Name:spork-accelerator Description:activate spork for accelerator Activated:true EnforcementHeight:9}"
t=2001-09-09T01:50:00+0000 lvl=dbug msg=created module=embedded contract=spork spork="&{Id:XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX Name:spork-bridge Description:activate spork for bridge Activated:false EnforcementHeight:0}"
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=0 pillar-name=TEST-pillar-1 reward="&{DelegationReward:+10363852800000 BlockReward:+8568000000000 TotalReward:+18931852800000 ProducedBlockNum:119 ExpectedBlockNum:120 Weight:+2100000000000}" total-weight=2500000000000 self-weight=2100000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=0 pillar-name=TEST-pillar-cool reward="&{DelegationReward:+995328000000 BlockReward:+8640000000000 TotalReward:+9635328000000 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2500000000000 self-weight=200000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=0 pillar-name=TEST-pillar-znn reward="&{DelegationReward:+995328000000 BlockReward:+8640000000000 TotalReward:+9635328000000 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2500000000000 self-weight=200000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=pillar epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxsentynelxxxxxxxxxxxxxwy0r2r current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating sentinel reward" module=embedded contract=sentinel epoch=0 total-znn-reward=187200000000 total-qsr-reward=500000000000 cumulated-sentinel=0 start-time=1000000000 end-time=1000003600
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=sentinel epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating stake reward" module=embedded contract=stake epoch=0 total-reward=1000000000000 cumulated-stake=0 start-time=1000000000 end-time=1000003600
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=stake epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating liquidity stake reward" module=embedded contract=liquidity epoch=0 znn-total-amount=187200000000 qsr-total-amount=500000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating liquidity balance" module=embedded contract=liquidity epoch=0 znnReward=187200000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating liquidity balance" module=embedded contract=liquidity epoch=0 qsrReward=500000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxaccelerat0rxxxxxxxxxxp4tk22 current-height=361 last-update-height=0
t=2001-09-09T02:46:50+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg TokenName:Zenon Coin TokenSymbol:ZNN TokenDomain:zenon.network TotalSupply:+19687200000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1znnxxxxxxxxxxxxx9z4ulx}" minted-amount=187200000000 to-address=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae
t=2001-09-09T02:46:50+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 TokenName:QuasarCoin TokenSymbol:QSR TokenDomain:zenon.network TotalSupply:+181050000000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1qsrxxxxxxxxxxxxxmrhjll}" minted-amount=500000000000 to-address=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae
t=2001-09-09T02:47:00+0000 lvl=info msg="received donation" module=embedded contract=common embedded=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae from-address=z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0 zts=zts1znnxxxxxxxxxxxxx9z4ulx amount=187200000000
t=2001-09-09T02:47:00+0000 lvl=info msg="received donation" module=embedded contract=common embedded=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae from-address=z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0 zts=zts1qsrxxxxxxxxxxxxxmrhjll amount=500000000000
t=2001-09-09T03:14:20+0000 lvl=dbug msg="issued ZTS" module=embedded contract=token token="{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenName:test.tok3n_liquidity-1 TokenSymbol:LIQ1 TokenDomain: TotalSupply:+10000000000 MaxSupply:+100000000000 Decimals:6 IsMintable:true IsBurnable:true IsUtility:false TokenStandard:zts1992nq43xn2urz3wttklc8z}"
t=2001-09-09T03:14:40+0000 lvl=dbug msg="issued ZTS" module=embedded contract=token token="{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenName:test.tok3n_liquidity-2 TokenSymbol:LIQ2 TokenDomain: TotalSupply:+20000000000 MaxSupply:+200000000000 Decimals:6 IsMintable:true IsBurnable:true IsUtility:false TokenStandard:zts1fres5c39axw805xswvt55j}"
t=2001-09-09T03:15:00+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenName:test.tok3n_liquidity-1 TokenSymbol:LIQ1 TokenDomain: TotalSupply:+20000000000 MaxSupply:+100000000000 Decimals:6 IsMintable:true IsBurnable:true IsUtility:false TokenStandard:zts1992nq43xn2urz3wttklc8z}" minted-amount=10000000000 to-address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz
t=2001-09-09T03:15:20+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenName:test.tok3n_liquidity-2 TokenSymbol:LIQ2 TokenDomain: TotalSupply:+40000000000 MaxSupply:+200000000000 Decimals:6 IsMintable:true IsBurnable:true IsUtility:false TokenStandard:zts1fres5c39axw805xswvt55j}" minted-amount=20000000000 to-address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz
t=2001-09-09T03:20:40+0000 lvl=dbug msg="created liquidity stake entry" module=embedded contract=stake id=XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX owner=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz amount=1000000000 weighted-amount=1000000000 duration-in-days=0
t=2001-09-09T03:21:00+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenName:test.tok3n_liquidity-2 TokenSymbol:LIQ2 TokenDomain: TotalSupply:+60000000000 MaxSupply:+200000000000 Decimals:6 IsMintable:true IsBurnable:true IsUtility:false TokenStandard:zts1fres5c39axw805xswvt55j}" minted-amount=20000000000 to-address=z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx
t=2001-09-09T03:21:20+0000 lvl=dbug msg="created liquidity stake entry" module=embedded contract=stake id=XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX owner=z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx amount=1000000000 weighted-amount=1000000000 duration-in-days=0
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg current-height=722 last-update-height=361
t=2001-09-09T03:46:50+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=1 pillar-name=TEST-pillar-1 reward="&{DelegationReward:+10450877642587 BlockReward:+8640000000000 TotalReward:+19090877642587 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+2099916666666}" total-weight=2499916666666 self-weight=2099916666666
t=2001-09-09T03:46:50+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=1 pillar-name=TEST-pillar-cool reward="&{DelegationReward:+995361178706 BlockReward:+8640000000000 TotalReward:+9635361178706 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2499916666666 self-weight=200000000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=1 pillar-name=TEST-pillar-znn reward="&{DelegationReward:+995361178706 BlockReward:+8640000000000 TotalReward:+9635361178706 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2499916666666 self-weight=200000000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=pillar epoch=2
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxsentynelxxxxxxxxxxxxxwy0r2r current-height=722 last-update-height=361
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating sentinel reward" module=embedded contract=sentinel epoch=1 total-znn-reward=187200000000 total-qsr-reward=500000000000 cumulated-sentinel=0 start-time=1000003600 end-time=1000007200
t=2001-09-09T03:46:50+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=sentinel epoch=2
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 current-height=722 last-update-height=361
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating stake reward" module=embedded contract=stake epoch=1 total-reward=1000000000000 cumulated-stake=0 start-time=1000003600 end-time=1000007200
t=2001-09-09T03:46:50+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=stake epoch=2
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae current-height=722 last-update-height=361
t=2001-09-09T03:46:50+0000 lvl=dbug msg="distribute znn rewards from the liquidity contract" module=embedded contract=liquidity znn-amount=5000000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="distribute qsr rewards from the liquidity contract" module=embedded contract=liquidity qsr-amount=1200000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating liquidity stake reward" module=embedded contract=liquidity epoch=1 znn-total-amount=192200000000 qsr-total-amount=501200000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="calculating percentages for each token" module=embedded contract=liquidity epoch=1 token-standard=zts1992nq43xn2urz3wttklc8z znn-percentage=5000 qsr-percentage=5000 znn-rewards=96100000000 qsr-rewards=250600000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="calculating percentages for each token" module=embedded contract=liquidity epoch=1 token-standard=zts1fres5c39axw805xswvt55j znn-percentage=5000 qsr-percentage=5000 znn-rewards=96100000000 qsr-rewards=250600000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating liquidity stake reward" module=embedded contract=liquidity id=XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX stake-address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz token-standard=zts1992nq43xn2urz3wttklc8z znn-amount=96100000000 qsr-amount=250600000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating liquidity stake reward" module=embedded contract=liquidity id=XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX stake-address=z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx token-standard=zts1fres5c39axw805xswvt55j znn-amount=96100000000 qsr-amount=250600000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxaccelerat0rxxxxxxxxxxp4tk22 current-height=722 last-update-height=361
t=2001-09-09T03:47:00+0000 lvl=dbug msg="burned ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg TokenName:Zenon Coin TokenSymbol:ZNN TokenDomain:zenon.network TotalSupply:+19682200000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1znnxxxxxxxxxxxxx9z4ulx}" burned-amount=5000000000
t=2001-09-09T03:47:00+0000 lvl=dbug msg="burned ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 TokenName:QuasarCoin TokenSymbol:QSR TokenDomain:zenon.network TotalSupply:+181048800000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1qsrxxxxxxxxxxxxxmrhjll}" burned-amount=1200000000
t=2001-09-09T03:48:10+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg TokenName:Zenon Coin TokenSymbol:ZNN TokenDomain:zenon.network TotalSupply:+19778300000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1znnxxxxxxxxxxxxx9z4ulx}" minted-amount=96100000000 to-address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz
t=2001-09-09T03:48:10+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 TokenName:QuasarCoin TokenSymbol:QSR TokenDomain:zenon.network TotalSupply:+181299400000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1qsrxxxxxxxxxxxxxmrhjll}" minted-amount=250600000000 to-address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz
t=2001-09-09T03:48:40+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg TokenName:Zenon Coin TokenSymbol:ZNN TokenDomain:zenon.network TotalSupply:+19874400000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1znnxxxxxxxxxxxxx9z4ulx}" minted-amount=96100000000 to-address=z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx
t=2001-09-09T03:48:40+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 TokenName:QuasarCoin TokenSymbol:QSR TokenDomain:zenon.network TotalSupply:+181550000000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1qsrxxxxxxxxxxxxxmrhjll}" minted-amount=250600000000 to-address=z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx
`)

	// we have token pairs
	activateLiquidityStep2(t, z)

	liquidityAPI := embedded.NewLiquidityApi(z)

	setAdditionalReward(t, z, g.User5.Address, big.NewInt(50*g.Zexp), big.NewInt(12*g.Zexp))
	insertMomentums(z, 2)

	common.Json(liquidityAPI.GetTimeChallengesInfo()).Equals(t, `
{
	"count": 3,
	"list": [
		{
			"MethodName": "NominateGuardians",
			"ParamsHash": "0000000000000000000000000000000000000000000000000000000000000000",
			"ChallengeStartHeight": 501
		},
		{
			"MethodName": "SetTokenTuple",
			"ParamsHash": "0000000000000000000000000000000000000000000000000000000000000000",
			"ChallengeStartHeight": 535
		},
		{
			"MethodName": "SetAdditionalReward",
			"ParamsHash": "0000000000000000000000000000000000000000000000000000000000000000",
			"ChallengeStartHeight": 549
		}
	]
}`)

	z.ExpectBalance(types.LiquidityContract, types.ZnnTokenStandard, 1872*g.Zexp)
	z.ExpectBalance(types.LiquidityContract, types.QsrTokenStandard, 5000*g.Zexp)

	common.Json(liquidityAPI.GetLiquidityInfo()).Equals(t, `
{
	"administrator": "z1qqaswvt0e3cc5sm7lygkyza9ra63cr8e6zre09",
	"isHalted": false,
	"znnReward": "5000000000",
	"qsrReward": "1200000000",
	"tokenTuples": [
		{
			"tokenStandard": "zts1992nq43xn2urz3wttklc8z",
			"znnPercentage": 5000,
			"qsrPercentage": 5000,
			"minAmount": "1000"
		},
		{
			"tokenStandard": "zts1fres5c39axw805xswvt55j",
			"znnPercentage": 5000,
			"qsrPercentage": 5000,
			"minAmount": "2000"
		}
	]
}`)
	z.ExpectBalance(g.User1.Address, types.ZnnTokenStandard, 11998*g.Zexp)

	z.ExpectBalance(g.User1.Address, tokens[0], 200*g.Zexp)
	defer z.CallContract(liquidityStake(g.User1.Address, tokens[0], big.NewInt(10*g.Zexp), 1)).
		Error(t, nil)
	insertMomentums(z, 2)

	z.ExpectBalance(g.User1.Address, tokens[0], 190*g.Zexp)
	z.ExpectBalance(g.User1.Address, types.ZnnTokenStandard, 11998*g.Zexp)
	common.Json(liquidityAPI.GetLiquidityStakeEntriesByAddress(g.User1.Address, 0, 5)).Equals(t, `
{
	"totalAmount": "1000000000",
	"totalWeightedAmount": "1000000000",
	"count": 1,
	"list": [
		{
			"amount": "1000000000",
			"tokenStandard": "zts1992nq43xn2urz3wttklc8z",
			"weightedAmount": "1000000000",
			"startTime": 1000005640,
			"revokeTime": 0,
			"expirationTime": 1000009240,
			"stakeAddress": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
			"id": "d36d9fcef9ec7730d1e1751dca957ecaa47974e92dc2ad2e7eaac8bb3d5e508f"
		}
	]
}`)

	defer z.CallContract(mint(g.User1.Address, tokens[1], big.NewInt(200*g.Zexp), g.User2.Address)).Error(t, nil)
	insertMomentums(z, 2)

	autoreceive(t, z, g.User2.Address)
	z.ExpectBalance(g.User2.Address, tokens[1], 200*g.Zexp)

	defer z.CallContract(liquidityStake(g.User2.Address, tokens[1], big.NewInt(10*g.Zexp), 1)).
		Error(t, nil)
	insertMomentums(z, 2)

	z.ExpectBalance(g.User2.Address, tokens[1], 190*g.Zexp)
	common.Json(liquidityAPI.GetLiquidityStakeEntriesByAddress(g.User2.Address, 0, 5)).Equals(t, `
{
	"totalAmount": "1000000000",
	"totalWeightedAmount": "1000000000",
	"count": 1,
	"list": [
		{
			"amount": "1000000000",
			"tokenStandard": "zts1fres5c39axw805xswvt55j",
			"weightedAmount": "1000000000",
			"startTime": 1000005680,
			"revokeTime": 0,
			"expirationTime": 1000009280,
			"stakeAddress": "z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx",
			"id": "7d27cb9dff8de7b052861f9651f36db9e6d160f6c8f07d228f10b72dee069762"
		}
	]
}`)
	z.InsertMomentumsTo(60*6*2 + 8)
	common.Json(liquidityAPI.GetUncollectedReward(g.User1.Address)).Equals(t, `
{
	"address": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
	"znnAmount": "96100000000",
	"qsrAmount": "250600000000"
}`)
	common.Json(liquidityAPI.GetUncollectedReward(g.User2.Address)).Equals(t, `
{
	"address": "z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx",
	"znnAmount": "96100000000",
	"qsrAmount": "250600000000"
}`)
	z.ExpectBalance(types.LiquidityContract, types.ZnnTokenStandard, 1822*g.Zexp)
	z.ExpectBalance(types.LiquidityContract, types.QsrTokenStandard, 4988*g.Zexp)
	z.ExpectBalance(types.LiquidityContract, tokens[0], 10*g.Zexp)
	z.ExpectBalance(types.LiquidityContract, tokens[1], 10*g.Zexp)
	z.ExpectBalance(g.User1.Address, types.ZnnTokenStandard, 11998*g.Zexp)
	z.ExpectBalance(g.User1.Address, types.QsrTokenStandard, 120000*g.Zexp)
	z.ExpectBalance(g.User1.Address, tokens[0], 190*g.Zexp)
	z.ExpectBalance(g.User1.Address, tokens[1], 400*g.Zexp)

	defer z.CallContract(collectReward(g.User1.Address)).Error(t, nil)
	insertMomentums(z, 3)
	common.Json(liquidityAPI.GetUncollectedReward(g.User1.Address)).Equals(t, `
{
	"address": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
	"znnAmount": "0",
	"qsrAmount": "0"
}`)

	defer z.CallContract(collectReward(g.User2.Address)).Error(t, nil)
	insertMomentums(z, 3)
	common.Json(liquidityAPI.GetUncollectedReward(g.User2.Address)).Equals(t, `
{
	"address": "z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx",
	"znnAmount": "0",
	"qsrAmount": "0"
}`)
	z.ExpectBalance(types.LiquidityContract, types.ZnnTokenStandard, 1822*g.Zexp)
	z.ExpectBalance(types.LiquidityContract, types.QsrTokenStandard, 4988*g.Zexp)
	z.ExpectBalance(types.LiquidityContract, tokens[0], 10*g.Zexp)
	z.ExpectBalance(types.LiquidityContract, tokens[1], 10*g.Zexp)
	z.ExpectBalance(g.User1.Address, types.ZnnTokenStandard, 11998*g.Zexp)
	z.ExpectBalance(g.User1.Address, types.QsrTokenStandard, 120000*g.Zexp)
	z.ExpectBalance(g.User1.Address, tokens[0], 190*g.Zexp)
	z.ExpectBalance(g.User1.Address, tokens[1], 400*g.Zexp)
	z.ExpectBalance(g.User2.Address, types.ZnnTokenStandard, 8000*g.Zexp)
	z.ExpectBalance(g.User2.Address, types.QsrTokenStandard, 80000*g.Zexp)
	z.ExpectBalance(g.User2.Address, tokens[0], 0*g.Zexp)
	z.ExpectBalance(g.User2.Address, tokens[1], 190*g.Zexp)
	autoreceive(t, z, g.User1.Address)
	autoreceive(t, z, g.User2.Address)
	z.ExpectBalance(g.User1.Address, types.ZnnTokenStandard, 12959*g.Zexp)
	z.ExpectBalance(g.User1.Address, types.QsrTokenStandard, 122506*g.Zexp)
	z.ExpectBalance(g.User2.Address, types.ZnnTokenStandard, 8961*g.Zexp)
	z.ExpectBalance(g.User2.Address, types.QsrTokenStandard, 82506*g.Zexp)
	z.ExpectBalance(types.LiquidityContract, types.ZnnTokenStandard, 1822*g.Zexp)
	z.ExpectBalance(types.LiquidityContract, types.QsrTokenStandard, 4988*g.Zexp)
}

func TestLiquidity_ChangeAdministrator(t *testing.T) {
	z := mock.NewMockZenonWithCustomEpochDuration(t, time.Hour)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).HideHashes().Equals(t, `
t=2001-09-09T01:46:50+0000 lvl=dbug msg=created module=embedded contract=spork spork="&{Id:XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX Name:spork-accelerator Description:activate spork for accelerator Activated:false EnforcementHeight:0}"
t=2001-09-09T01:47:00+0000 lvl=dbug msg=activated module=embedded contract=spork spork="&{Id:XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX Name:spork-accelerator Description:activate spork for accelerator Activated:true EnforcementHeight:9}"
t=2001-09-09T01:50:00+0000 lvl=dbug msg=created module=embedded contract=spork spork="&{Id:XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX Name:spork-bridge Description:activate spork for bridge Activated:false EnforcementHeight:0}"
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=0 pillar-name=TEST-pillar-1 reward="&{DelegationReward:+10363852800000 BlockReward:+8568000000000 TotalReward:+18931852800000 ProducedBlockNum:119 ExpectedBlockNum:120 Weight:+2100000000000}" total-weight=2500000000000 self-weight=2100000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=0 pillar-name=TEST-pillar-cool reward="&{DelegationReward:+995328000000 BlockReward:+8640000000000 TotalReward:+9635328000000 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2500000000000 self-weight=200000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=0 pillar-name=TEST-pillar-znn reward="&{DelegationReward:+995328000000 BlockReward:+8640000000000 TotalReward:+9635328000000 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2500000000000 self-weight=200000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=pillar epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxsentynelxxxxxxxxxxxxxwy0r2r current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating sentinel reward" module=embedded contract=sentinel epoch=0 total-znn-reward=187200000000 total-qsr-reward=500000000000 cumulated-sentinel=0 start-time=1000000000 end-time=1000003600
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=sentinel epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating stake reward" module=embedded contract=stake epoch=0 total-reward=1000000000000 cumulated-stake=0 start-time=1000000000 end-time=1000003600
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=stake epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating liquidity stake reward" module=embedded contract=liquidity epoch=0 znn-total-amount=187200000000 qsr-total-amount=500000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating liquidity balance" module=embedded contract=liquidity epoch=0 znnReward=187200000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating liquidity balance" module=embedded contract=liquidity epoch=0 qsrReward=500000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxaccelerat0rxxxxxxxxxxp4tk22 current-height=361 last-update-height=0
t=2001-09-09T02:46:50+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg TokenName:Zenon Coin TokenSymbol:ZNN TokenDomain:zenon.network TotalSupply:+19687200000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1znnxxxxxxxxxxxxx9z4ulx}" minted-amount=187200000000 to-address=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae
t=2001-09-09T02:46:50+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 TokenName:QuasarCoin TokenSymbol:QSR TokenDomain:zenon.network TotalSupply:+181050000000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1qsrxxxxxxxxxxxxxmrhjll}" minted-amount=500000000000 to-address=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae
t=2001-09-09T02:47:00+0000 lvl=info msg="received donation" module=embedded contract=common embedded=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae from-address=z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0 zts=zts1znnxxxxxxxxxxxxx9z4ulx amount=187200000000
t=2001-09-09T02:47:00+0000 lvl=info msg="received donation" module=embedded contract=common embedded=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae from-address=z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0 zts=zts1qsrxxxxxxxxxxxxxmrhjll amount=500000000000
`)

	// activate sporks
	activateLiquidityStep0(t, z)

	// try to activate without guardians set
	defer z.CallContract(changeAdministratorLiqStep(g.User5.Address, g.User4.Address)).Error(t, constants.ErrSecurityNotInitialized)
	insertMomentums(z, 2)

	liquidityApi := embedded.NewLiquidityApi(z)
	securityInfo, err := liquidityApi.GetSecurityInfo()
	common.DealWithErr(err)
	guardians := []types.Address{g.User1.Address, g.User2.Address, g.User3.Address, g.User4.Address}
	nominateGuardiansLiq(t, z, g.User5.Address, guardians, securityInfo.AdministratorDelay)

	// call as non admin
	defer z.CallContract(changeAdministratorLiqStep(g.User4.Address, g.User4.Address)).Error(t, constants.ErrPermissionDenied)
	insertMomentums(z, 2)

	// call the first step
	defer z.CallContract(changeAdministratorLiqStep(g.User5.Address, g.User4.Address)).Error(t, nil)
	insertMomentums(z, 2)

	// should fail because of time challenge
	defer z.CallContract(changeAdministratorLiqStep(g.User5.Address, g.User4.Address)).Error(t, constants.ErrTimeChallengeNotDue)
	insertMomentums(z, int(securityInfo.AdministratorDelay))

	common.Json(liquidityApi.GetLiquidityInfo()).Equals(t, `
{
	"administrator": "z1qqaswvt0e3cc5sm7lygkyza9ra63cr8e6zre09",
	"isHalted": false,
	"znnReward": "0",
	"qsrReward": "0",
	"tokenTuples": []
}`)
	defer z.CallContract(changeAdministratorLiqStep(g.User5.Address, g.User4.Address)).Error(t, nil)
	insertMomentums(z, 2)
	common.Json(liquidityApi.GetLiquidityInfo()).Equals(t, `
{
	"administrator": "z1qraz4ermhhua89a0h0gxxan4lnzrfutgs6xxe2",
	"isHalted": false,
	"znnReward": "0",
	"qsrReward": "0",
	"tokenTuples": []
}`)
}

func TestLiquidity_NominateGuardians(t *testing.T) {
	z := mock.NewMockZenonWithCustomEpochDuration(t, time.Hour)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).Equals(t, `
t=2001-09-09T01:46:50+0000 lvl=dbug msg=created module=embedded contract=spork spork="&{Id:34d8229bd07586c243c6e74122a18d6d2002694c72964a7186111026a9cec6ab Name:spork-accelerator Description:activate spork for accelerator Activated:false EnforcementHeight:0}"
t=2001-09-09T01:47:00+0000 lvl=dbug msg=activated module=embedded contract=spork spork="&{Id:34d8229bd07586c243c6e74122a18d6d2002694c72964a7186111026a9cec6ab Name:spork-accelerator Description:activate spork for accelerator Activated:true EnforcementHeight:9}"
t=2001-09-09T01:50:00+0000 lvl=dbug msg=created module=embedded contract=spork spork="&{Id:46bc4121cd6a2dff8dde2d1b8a8d52d3e8e91888ded459fa2c36d8e829bc6251 Name:spork-bridge Description:activate spork for bridge Activated:false EnforcementHeight:0}"
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=0 pillar-name=TEST-pillar-1 reward="&{DelegationReward:+10363852800000 BlockReward:+8568000000000 TotalReward:+18931852800000 ProducedBlockNum:119 ExpectedBlockNum:120 Weight:+2100000000000}" total-weight=2500000000000 self-weight=2100000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=0 pillar-name=TEST-pillar-cool reward="&{DelegationReward:+995328000000 BlockReward:+8640000000000 TotalReward:+9635328000000 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2500000000000 self-weight=200000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=0 pillar-name=TEST-pillar-znn reward="&{DelegationReward:+995328000000 BlockReward:+8640000000000 TotalReward:+9635328000000 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2500000000000 self-weight=200000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=pillar epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxsentynelxxxxxxxxxxxxxwy0r2r current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating sentinel reward" module=embedded contract=sentinel epoch=0 total-znn-reward=187200000000 total-qsr-reward=500000000000 cumulated-sentinel=0 start-time=1000000000 end-time=1000003600
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=sentinel epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating stake reward" module=embedded contract=stake epoch=0 total-reward=1000000000000 cumulated-stake=0 start-time=1000000000 end-time=1000003600
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=stake epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating liquidity stake reward" module=embedded contract=liquidity epoch=0 znn-total-amount=187200000000 qsr-total-amount=500000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating liquidity balance" module=embedded contract=liquidity epoch=0 znnReward=187200000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating liquidity balance" module=embedded contract=liquidity epoch=0 qsrReward=500000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxaccelerat0rxxxxxxxxxxp4tk22 current-height=361 last-update-height=0
t=2001-09-09T02:46:50+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg TokenName:Zenon Coin TokenSymbol:ZNN TokenDomain:zenon.network TotalSupply:+19687200000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1znnxxxxxxxxxxxxx9z4ulx}" minted-amount=187200000000 to-address=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae
t=2001-09-09T02:46:50+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 TokenName:QuasarCoin TokenSymbol:QSR TokenDomain:zenon.network TotalSupply:+181050000000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1qsrxxxxxxxxxxxxxmrhjll}" minted-amount=500000000000 to-address=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae
t=2001-09-09T02:47:00+0000 lvl=info msg="received donation" module=embedded contract=common embedded=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae from-address=z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0 zts=zts1znnxxxxxxxxxxxxx9z4ulx amount=187200000000
t=2001-09-09T02:47:00+0000 lvl=info msg="received donation" module=embedded contract=common embedded=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae from-address=z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0 zts=zts1qsrxxxxxxxxxxxxxmrhjll amount=500000000000
`)

	// We have sporks
	activateLiquidityStep0(t, z)

	liquidityApi := embedded.NewLiquidityApi(z)

	// call from non admin
	guardians := []types.Address{g.User1.Address, g.User2.Address, g.User3.Address, g.User4.Address}
	defer z.CallContract(nominateGuardiansLiqStep(g.User4.Address, guardians)).
		Error(t, constants.ErrPermissionDenied)
	insertMomentums(z, 2)

	// one invalid address
	guardians[0] = types.ZeroAddress
	z.InsertSendBlock(nominateGuardiansLiqStep(g.User5.Address, guardians), constants.ErrForbiddenParam, mock.SkipVmChanges)
	insertMomentums(z, 2)

	// less than min
	guardians = guardians[2:]
	constants.MinGuardians = 4
	z.InsertSendBlock(nominateGuardiansLiqStep(g.User5.Address, guardians), constants.ErrInvalidGuardians, mock.SkipVmChanges)
	insertMomentums(z, 2)

	guardians = []types.Address{g.User1.Address, g.User2.Address, g.User3.Address, g.User4.Address, g.User5.Address}
	defer z.CallContract(nominateGuardiansLiqStep(g.User5.Address, guardians)).
		Error(t, nil)
	insertMomentums(z, 5)

	defer z.CallContract(nominateGuardiansLiqStep(g.User5.Address, guardians)).
		Error(t, constants.ErrTimeChallengeNotDue)
	insertMomentums(z, 30)

	common.Json(liquidityApi.GetSecurityInfo()).Equals(t, `
{
	"guardians": [],
	"guardiansVotes": [],
	"administratorDelay": 20,
	"softDelay": 10
}`)
	defer z.CallContract(nominateGuardiansLiqStep(g.User5.Address, guardians)).
		Error(t, nil)
	insertMomentums(z, 2)

	common.Json(liquidityApi.GetSecurityInfo()).Equals(t, `
{
	"guardians": [
		"z1qqaswvt0e3cc5sm7lygkyza9ra63cr8e6zre09",
		"z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx",
		"z1qraz4ermhhua89a0h0gxxan4lnzrfutgs6xxe2",
		"z1qrs2lpccnsneglhnnfwvlsj0qncnxjnwlfmjac",
		"z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz"
	],
	"guardiansVotes": [
		"z1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqsggv2f",
		"z1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqsggv2f",
		"z1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqsggv2f",
		"z1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqsggv2f",
		"z1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqsggv2f"
	],
	"administratorDelay": 20,
	"softDelay": 10
}`)
}

func TestLiquidity_ProposeAdministrator(t *testing.T) {
	z := mock.NewMockZenonWithCustomEpochDuration(t, time.Hour)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).Equals(t, `
t=2001-09-09T01:46:50+0000 lvl=dbug msg=created module=embedded contract=spork spork="&{Id:34d8229bd07586c243c6e74122a18d6d2002694c72964a7186111026a9cec6ab Name:spork-accelerator Description:activate spork for accelerator Activated:false EnforcementHeight:0}"
t=2001-09-09T01:47:00+0000 lvl=dbug msg=activated module=embedded contract=spork spork="&{Id:34d8229bd07586c243c6e74122a18d6d2002694c72964a7186111026a9cec6ab Name:spork-accelerator Description:activate spork for accelerator Activated:true EnforcementHeight:9}"
t=2001-09-09T01:50:00+0000 lvl=dbug msg=created module=embedded contract=spork spork="&{Id:46bc4121cd6a2dff8dde2d1b8a8d52d3e8e91888ded459fa2c36d8e829bc6251 Name:spork-bridge Description:activate spork for bridge Activated:false EnforcementHeight:0}"
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=0 pillar-name=TEST-pillar-1 reward="&{DelegationReward:+10363852800000 BlockReward:+8568000000000 TotalReward:+18931852800000 ProducedBlockNum:119 ExpectedBlockNum:120 Weight:+2100000000000}" total-weight=2500000000000 self-weight=2100000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=0 pillar-name=TEST-pillar-cool reward="&{DelegationReward:+995328000000 BlockReward:+8640000000000 TotalReward:+9635328000000 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2500000000000 self-weight=200000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=0 pillar-name=TEST-pillar-znn reward="&{DelegationReward:+995328000000 BlockReward:+8640000000000 TotalReward:+9635328000000 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2500000000000 self-weight=200000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=pillar epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxsentynelxxxxxxxxxxxxxwy0r2r current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating sentinel reward" module=embedded contract=sentinel epoch=0 total-znn-reward=187200000000 total-qsr-reward=500000000000 cumulated-sentinel=0 start-time=1000000000 end-time=1000003600
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=sentinel epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating stake reward" module=embedded contract=stake epoch=0 total-reward=1000000000000 cumulated-stake=0 start-time=1000000000 end-time=1000003600
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=stake epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating liquidity stake reward" module=embedded contract=liquidity epoch=0 znn-total-amount=187200000000 qsr-total-amount=500000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating liquidity balance" module=embedded contract=liquidity epoch=0 znnReward=187200000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating liquidity balance" module=embedded contract=liquidity epoch=0 qsrReward=500000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxaccelerat0rxxxxxxxxxxp4tk22 current-height=361 last-update-height=0
t=2001-09-09T02:46:50+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg TokenName:Zenon Coin TokenSymbol:ZNN TokenDomain:zenon.network TotalSupply:+19687200000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1znnxxxxxxxxxxxxx9z4ulx}" minted-amount=187200000000 to-address=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae
t=2001-09-09T02:46:50+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 TokenName:QuasarCoin TokenSymbol:QSR TokenDomain:zenon.network TotalSupply:+181050000000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1qsrxxxxxxxxxxxxxmrhjll}" minted-amount=500000000000 to-address=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae
t=2001-09-09T02:47:00+0000 lvl=info msg="received donation" module=embedded contract=common embedded=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae from-address=z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0 zts=zts1znnxxxxxxxxxxxxx9z4ulx amount=187200000000
t=2001-09-09T02:47:00+0000 lvl=info msg="received donation" module=embedded contract=common embedded=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae from-address=z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0 zts=zts1qsrxxxxxxxxxxxxxmrhjll amount=500000000000
`)

	// We have sporks
	activateLiquidityStep0(t, z)

	liquidityAPI := embedded.NewLiquidityApi(z)
	securityInfo, err := liquidityAPI.GetSecurityInfo()
	common.DealWithErr(err)

	guardians := []types.Address{g.User2.Address, g.User3.Address, g.User4.Address, g.User5.Address}
	nominateGuardiansLiq(t, z, g.User5.Address, guardians, securityInfo.AdministratorDelay)
	common.Json(liquidityAPI.GetSecurityInfo()).Equals(t, `
{
	"guardians": [
		"z1qqaswvt0e3cc5sm7lygkyza9ra63cr8e6zre09",
		"z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx",
		"z1qraz4ermhhua89a0h0gxxan4lnzrfutgs6xxe2",
		"z1qrs2lpccnsneglhnnfwvlsj0qncnxjnwlfmjac"
	],
	"guardiansVotes": [
		"z1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqsggv2f",
		"z1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqsggv2f",
		"z1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqsggv2f",
		"z1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqsggv2f"
	],
	"administratorDelay": 20,
	"softDelay": 10
}`)

	// should not work as we are not in emergency yet
	defer z.CallContract(proposeAdministratorLiq(g.User1.Address, g.User6.Address)).Error(t, constants.ErrNotEmergency)
	insertMomentums(z, 2)

	defer z.CallContract(activateEmergencyLiq(g.User5.Address)).Error(t, nil)
	insertMomentums(z, 2)

	// try to propose zero address
	z.InsertSendBlock(proposeAdministratorLiq(g.User1.Address, types.ZeroAddress),
		constants.ErrForbiddenParam, mock.SkipVmChanges)
	insertMomentums(z, 2)

	// try to propose as non guardian
	defer z.CallContract(proposeAdministratorLiq(g.User1.Address, g.User1.Address)).Error(t, constants.ErrNotGuardian)
	insertMomentums(z, 2)

	//
	defer z.CallContract(proposeAdministratorLiq(g.User2.Address, g.User1.Address)).Error(t, nil)
	insertMomentums(z, 2)
	common.Json(liquidityAPI.GetSecurityInfo()).Equals(t, `
{
	"guardians": [
		"z1qqaswvt0e3cc5sm7lygkyza9ra63cr8e6zre09",
		"z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx",
		"z1qraz4ermhhua89a0h0gxxan4lnzrfutgs6xxe2",
		"z1qrs2lpccnsneglhnnfwvlsj0qncnxjnwlfmjac"
	],
	"guardiansVotes": [
		"z1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqsggv2f",
		"z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
		"z1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqsggv2f",
		"z1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqsggv2f"
	],
	"administratorDelay": 20,
	"softDelay": 10
}`)

	// the vote should be changed
	defer z.CallContract(proposeAdministratorLiq(g.User2.Address, g.User3.Address)).Error(t, nil)
	insertMomentums(z, 2)
	common.Json(liquidityAPI.GetSecurityInfo()).Equals(t, `
{
	"guardians": [
		"z1qqaswvt0e3cc5sm7lygkyza9ra63cr8e6zre09",
		"z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx",
		"z1qraz4ermhhua89a0h0gxxan4lnzrfutgs6xxe2",
		"z1qrs2lpccnsneglhnnfwvlsj0qncnxjnwlfmjac"
	],
	"guardiansVotes": [
		"z1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqsggv2f",
		"z1qrs2lpccnsneglhnnfwvlsj0qncnxjnwlfmjac",
		"z1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqsggv2f",
		"z1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqsggv2f"
	],
	"administratorDelay": 20,
	"softDelay": 10
}`)

	defer z.CallContract(proposeAdministratorLiq(g.User3.Address, g.User3.Address)).Error(t, nil)
	insertMomentums(z, 2)
	common.Json(liquidityAPI.GetLiquidityInfo()).Equals(t, `
{
	"administrator": "z1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqsggv2f",
	"isHalted": true,
	"znnReward": "0",
	"qsrReward": "0",
	"tokenTuples": []
}`)
	defer z.CallContract(proposeAdministratorLiq(g.User4.Address, g.User3.Address)).Error(t, nil)
	insertMomentums(z, 2)

	common.Json(liquidityAPI.GetLiquidityInfo()).Equals(t, `
{
	"administrator": "z1qrs2lpccnsneglhnnfwvlsj0qncnxjnwlfmjac",
	"isHalted": true,
	"znnReward": "0",
	"qsrReward": "0",
	"tokenTuples": []
}`)
	common.Json(liquidityAPI.GetSecurityInfo()).Equals(t, `
{
	"guardians": [
		"z1qqaswvt0e3cc5sm7lygkyza9ra63cr8e6zre09",
		"z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx",
		"z1qraz4ermhhua89a0h0gxxan4lnzrfutgs6xxe2",
		"z1qrs2lpccnsneglhnnfwvlsj0qncnxjnwlfmjac"
	],
	"guardiansVotes": [
		"z1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqsggv2f",
		"z1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqsggv2f",
		"z1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqsggv2f",
		"z1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqsggv2f"
	],
	"administratorDelay": 20,
	"softDelay": 10
}`)
}

func TestLiquidity_Emergency(t *testing.T) {
	z := mock.NewMockZenonWithCustomEpochDuration(t, time.Hour)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).Equals(t, `
t=2001-09-09T01:46:50+0000 lvl=dbug msg=created module=embedded contract=spork spork="&{Id:34d8229bd07586c243c6e74122a18d6d2002694c72964a7186111026a9cec6ab Name:spork-accelerator Description:activate spork for accelerator Activated:false EnforcementHeight:0}"
t=2001-09-09T01:47:00+0000 lvl=dbug msg=activated module=embedded contract=spork spork="&{Id:34d8229bd07586c243c6e74122a18d6d2002694c72964a7186111026a9cec6ab Name:spork-accelerator Description:activate spork for accelerator Activated:true EnforcementHeight:9}"
t=2001-09-09T01:50:00+0000 lvl=dbug msg=created module=embedded contract=spork spork="&{Id:46bc4121cd6a2dff8dde2d1b8a8d52d3e8e91888ded459fa2c36d8e829bc6251 Name:spork-bridge Description:activate spork for bridge Activated:false EnforcementHeight:0}"
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=0 pillar-name=TEST-pillar-1 reward="&{DelegationReward:+10363852800000 BlockReward:+8568000000000 TotalReward:+18931852800000 ProducedBlockNum:119 ExpectedBlockNum:120 Weight:+2100000000000}" total-weight=2500000000000 self-weight=2100000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=0 pillar-name=TEST-pillar-cool reward="&{DelegationReward:+995328000000 BlockReward:+8640000000000 TotalReward:+9635328000000 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2500000000000 self-weight=200000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=0 pillar-name=TEST-pillar-znn reward="&{DelegationReward:+995328000000 BlockReward:+8640000000000 TotalReward:+9635328000000 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2500000000000 self-weight=200000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=pillar epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxsentynelxxxxxxxxxxxxxwy0r2r current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating sentinel reward" module=embedded contract=sentinel epoch=0 total-znn-reward=187200000000 total-qsr-reward=500000000000 cumulated-sentinel=0 start-time=1000000000 end-time=1000003600
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=sentinel epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating stake reward" module=embedded contract=stake epoch=0 total-reward=1000000000000 cumulated-stake=0 start-time=1000000000 end-time=1000003600
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=stake epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating liquidity stake reward" module=embedded contract=liquidity epoch=0 znn-total-amount=187200000000 qsr-total-amount=500000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating liquidity balance" module=embedded contract=liquidity epoch=0 znnReward=187200000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating liquidity balance" module=embedded contract=liquidity epoch=0 qsrReward=500000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxaccelerat0rxxxxxxxxxxp4tk22 current-height=361 last-update-height=0
t=2001-09-09T02:46:50+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg TokenName:Zenon Coin TokenSymbol:ZNN TokenDomain:zenon.network TotalSupply:+19687200000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1znnxxxxxxxxxxxxx9z4ulx}" minted-amount=187200000000 to-address=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae
t=2001-09-09T02:46:50+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 TokenName:QuasarCoin TokenSymbol:QSR TokenDomain:zenon.network TotalSupply:+181050000000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1qsrxxxxxxxxxxxxxmrhjll}" minted-amount=500000000000 to-address=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae
t=2001-09-09T02:47:00+0000 lvl=info msg="received donation" module=embedded contract=common embedded=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae from-address=z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0 zts=zts1znnxxxxxxxxxxxxx9z4ulx amount=187200000000
t=2001-09-09T02:47:00+0000 lvl=info msg="received donation" module=embedded contract=common embedded=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae from-address=z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0 zts=zts1qsrxxxxxxxxxxxxxmrhjll amount=500000000000
`)

	// We have orcInfo
	activateLiquidityStep0(t, z)

	liquidityAPI := embedded.NewLiquidityApi(z)
	securityInfo, err := liquidityAPI.GetSecurityInfo()
	common.FailIfErr(t, err)

	defer z.CallContract(activateEmergencyLiq(g.User5.Address)).Error(t, constants.ErrSecurityNotInitialized)
	insertMomentums(z, 2)

	guardians := []types.Address{g.User1.Address, g.User2.Address, g.User3.Address, g.User4.Address}
	nominateGuardiansLiq(t, z, g.User5.Address, guardians, securityInfo.AdministratorDelay)

	// activate as non admin
	defer z.CallContract(activateEmergencyLiq(g.User4.Address)).Error(t, constants.ErrPermissionDenied)
	insertMomentums(z, 2)

	common.Json(liquidityAPI.GetLiquidityInfo()).Equals(t, `
{
	"administrator": "z1qqaswvt0e3cc5sm7lygkyza9ra63cr8e6zre09",
	"isHalted": false,
	"znnReward": "0",
	"qsrReward": "0",
	"tokenTuples": []
}`)

	// halt before
	defer z.CallContract(setIsHalted(g.User5.Address, true)).Error(t, nil)
	insertMomentums(z, 2)

	defer z.CallContract(activateEmergencyLiq(g.User5.Address)).Error(t, nil)
	insertMomentums(z, 2)

	common.Json(liquidityAPI.GetLiquidityInfo()).Equals(t, `
{
	"administrator": "z1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqsggv2f",
	"isHalted": true,
	"znnReward": "0",
	"qsrReward": "0",
	"tokenTuples": []
}`)

	defer z.CallContract(activateEmergencyLiq(g.User5.Address)).Error(t, constants.ErrPermissionDenied)
	insertMomentums(z, 2)
}

func TestLiquidity_SetIsHalted(t *testing.T) {
	z := mock.NewMockZenonWithCustomEpochDuration(t, time.Hour)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).Equals(t, `
t=2001-09-09T01:46:50+0000 lvl=dbug msg=created module=embedded contract=spork spork="&{Id:34d8229bd07586c243c6e74122a18d6d2002694c72964a7186111026a9cec6ab Name:spork-accelerator Description:activate spork for accelerator Activated:false EnforcementHeight:0}"
t=2001-09-09T01:47:00+0000 lvl=dbug msg=activated module=embedded contract=spork spork="&{Id:34d8229bd07586c243c6e74122a18d6d2002694c72964a7186111026a9cec6ab Name:spork-accelerator Description:activate spork for accelerator Activated:true EnforcementHeight:9}"
t=2001-09-09T01:50:00+0000 lvl=dbug msg=created module=embedded contract=spork spork="&{Id:46bc4121cd6a2dff8dde2d1b8a8d52d3e8e91888ded459fa2c36d8e829bc6251 Name:spork-bridge Description:activate spork for bridge Activated:false EnforcementHeight:0}"
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=0 pillar-name=TEST-pillar-1 reward="&{DelegationReward:+10363852800000 BlockReward:+8568000000000 TotalReward:+18931852800000 ProducedBlockNum:119 ExpectedBlockNum:120 Weight:+2100000000000}" total-weight=2500000000000 self-weight=2100000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=0 pillar-name=TEST-pillar-cool reward="&{DelegationReward:+995328000000 BlockReward:+8640000000000 TotalReward:+9635328000000 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2500000000000 self-weight=200000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=0 pillar-name=TEST-pillar-znn reward="&{DelegationReward:+995328000000 BlockReward:+8640000000000 TotalReward:+9635328000000 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2500000000000 self-weight=200000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=pillar epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxsentynelxxxxxxxxxxxxxwy0r2r current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating sentinel reward" module=embedded contract=sentinel epoch=0 total-znn-reward=187200000000 total-qsr-reward=500000000000 cumulated-sentinel=0 start-time=1000000000 end-time=1000003600
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=sentinel epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating stake reward" module=embedded contract=stake epoch=0 total-reward=1000000000000 cumulated-stake=0 start-time=1000000000 end-time=1000003600
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=stake epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating liquidity stake reward" module=embedded contract=liquidity epoch=0 znn-total-amount=187200000000 qsr-total-amount=500000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating liquidity balance" module=embedded contract=liquidity epoch=0 znnReward=187200000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating liquidity balance" module=embedded contract=liquidity epoch=0 qsrReward=500000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxaccelerat0rxxxxxxxxxxp4tk22 current-height=361 last-update-height=0
t=2001-09-09T02:46:50+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg TokenName:Zenon Coin TokenSymbol:ZNN TokenDomain:zenon.network TotalSupply:+19687200000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1znnxxxxxxxxxxxxx9z4ulx}" minted-amount=187200000000 to-address=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae
t=2001-09-09T02:46:50+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 TokenName:QuasarCoin TokenSymbol:QSR TokenDomain:zenon.network TotalSupply:+181050000000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1qsrxxxxxxxxxxxxxmrhjll}" minted-amount=500000000000 to-address=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae
t=2001-09-09T02:47:00+0000 lvl=info msg="received donation" module=embedded contract=common embedded=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae from-address=z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0 zts=zts1znnxxxxxxxxxxxxx9z4ulx amount=187200000000
t=2001-09-09T02:47:00+0000 lvl=info msg="received donation" module=embedded contract=common embedded=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae from-address=z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0 zts=zts1qsrxxxxxxxxxxxxxmrhjll amount=500000000000
`)

	// We have orcInfo
	activateLiquidityStep0(t, z)

	liquidityAPI := embedded.NewLiquidityApi(z)
	securityInfo, err := liquidityAPI.GetSecurityInfo()
	common.FailIfErr(t, err)

	// call without guardians
	defer z.CallContract(setIsHalted(g.User5.Address, true)).Error(t, nil)
	insertMomentums(z, 2)
	common.Json(liquidityAPI.GetLiquidityInfo()).Equals(t, `
{
	"administrator": "z1qqaswvt0e3cc5sm7lygkyza9ra63cr8e6zre09",
	"isHalted": true,
	"znnReward": "0",
	"qsrReward": "0",
	"tokenTuples": []
}`)
	defer z.CallContract(setIsHalted(g.User5.Address, false)).Error(t, nil)
	insertMomentums(z, 2)
	common.Json(liquidityAPI.GetLiquidityInfo()).Equals(t, `
{
	"administrator": "z1qqaswvt0e3cc5sm7lygkyza9ra63cr8e6zre09",
	"isHalted": false,
	"znnReward": "0",
	"qsrReward": "0",
	"tokenTuples": []
}`)

	guardians := []types.Address{g.User1.Address, g.User2.Address, g.User3.Address, g.User4.Address}
	nominateGuardiansLiq(t, z, g.User5.Address, guardians, securityInfo.AdministratorDelay)

	// call with guardians
	defer z.CallContract(setIsHalted(g.User5.Address, true)).Error(t, nil)
	insertMomentums(z, 2)

	common.Json(liquidityAPI.GetLiquidityInfo()).Equals(t, `
{
	"administrator": "z1qqaswvt0e3cc5sm7lygkyza9ra63cr8e6zre09",
	"isHalted": true,
	"znnReward": "0",
	"qsrReward": "0",
	"tokenTuples": []
}`)

	// non admin
	defer z.CallContract(setIsHalted(g.User4.Address, false)).Error(t, constants.ErrPermissionDenied)
	insertMomentums(z, 2)

	defer z.CallContract(activateEmergencyLiq(g.User5.Address)).Error(t, nil)
	insertMomentums(z, 2)

	// in emergency - still non admin
	defer z.CallContract(setIsHalted(g.User5.Address, false)).Error(t, constants.ErrPermissionDenied)
	insertMomentums(z, 2)

}

func fundLiq(user types.Address, amountZnn, amountQsr *big.Int) *nom.AccountBlock {
	return &nom.AccountBlock{
		Address:   user,
		ToAddress: types.LiquidityContract,
		Data: definition.ABILiquidity.PackMethodPanic(definition.FundMethodName,
			amountZnn, // znnReward
			amountQsr, // qsrReward
		),
	}
}

func liquidityStake(user types.Address, zts types.ZenonTokenStandard, amount *big.Int, durationInMonths int64) *nom.AccountBlock {
	return &nom.AccountBlock{
		Address:   user,
		ToAddress: types.LiquidityContract,
		Data: definition.ABILiquidity.PackMethodPanic(definition.LiquidityStakeMethodName,
			durationInMonths*constants.StakeTimeMinSec),
		TokenStandard: zts,
		Amount:        amount,
	}
}

func setTokensTupleStep(administrator types.Address, customZts []string, znnPercentages, qsrPercentages []uint32, minAmounts []*big.Int) *nom.AccountBlock {
	return &nom.AccountBlock{
		Address:   administrator,
		ToAddress: types.LiquidityContract,
		Data: definition.ABILiquidity.PackMethodPanic(definition.SetTokenTupleMethodName,
			customZts,
			znnPercentages,
			qsrPercentages,
			minAmounts,
		),
	}
}

func setIsHalted(administrator types.Address, value bool) *nom.AccountBlock {
	return &nom.AccountBlock{
		Address:   administrator,
		ToAddress: types.LiquidityContract,
		Data: definition.ABILiquidity.PackMethodPanic(definition.SetIsHaltedMethodName,
			value,
		),
	}
}

func collectReward(user types.Address) *nom.AccountBlock {
	return &nom.AccountBlock{
		Address:   user,
		ToAddress: types.LiquidityContract,
		Data:      definition.ABILiquidity.PackMethodPanic(definition.CollectRewardMethodName),
	}
}

func cancelStakeLiq(user types.Address, hash types.Hash) *nom.AccountBlock {
	return &nom.AccountBlock{
		Address:   user,
		ToAddress: types.LiquidityContract,
		Data: definition.ABILiquidity.PackMethodPanic(
			definition.CancelLiquidityStakeMethodName,
			hash),
	}
}

func unlockLiquidityEntries(user types.Address, zts types.ZenonTokenStandard) *nom.AccountBlock {
	return &nom.AccountBlock{
		Address:       user,
		ToAddress:     types.LiquidityContract,
		Data:          definition.ABILiquidity.PackMethodPanic(definition.UnlockLiquidityStakeEntriesMethodName),
		TokenStandard: zts,
	}
}

func setAdditionalReward(t *testing.T, z mock.MockZenon, administrator types.Address, amountZnn, amountQsr *big.Int) {
	defer z.CallContract(setAdditionalRewardStep(administrator, amountZnn, amountQsr)).Error(t, nil)
	insertMomentums(z, 2)

	liquidityAPI := embedded.NewLiquidityApi(z)
	securityInfo, err := liquidityAPI.GetSecurityInfo()
	common.FailIfErr(t, err)

	insertMomentums(z, int(securityInfo.SoftDelay))

	defer z.CallContract(setAdditionalRewardStep(administrator, amountZnn, amountQsr)).Error(t, nil)
	insertMomentums(z, 2)
}

func setAdditionalRewardStep(administrator types.Address, amountZnn, amountQsr *big.Int) *nom.AccountBlock {
	return &nom.AccountBlock{
		Address:   administrator,
		ToAddress: types.LiquidityContract,
		Data: definition.ABILiquidity.PackMethodPanic(definition.SetAdditionalRewardMethodName,
			amountZnn,
			amountQsr,
		),
	}
}

func setTokensTuple(t *testing.T, z mock.MockZenon, administrator types.Address, customZts []string, znnPercentages, qsrPercentages []uint32, minAmounts []*big.Int) {
	defer z.CallContract(setTokensTupleStep(administrator, customZts, znnPercentages, qsrPercentages, minAmounts)).Error(t, nil)
	insertMomentums(z, 2)

	frMom, err := z.Chain().GetFrontierMomentumStore().GetFrontierMomentum()
	common.FailIfErr(t, err)
	z.InsertMomentumsTo(frMom.Height + uint64(constants.MomentumsPerEpoch))

	defer z.CallContract(setTokensTupleStep(administrator, customZts, znnPercentages, qsrPercentages, minAmounts)).Error(t, nil)
	insertMomentums(z, 2)
}

func nominateGuardiansLiqStep(administrator types.Address, guardians []types.Address) *nom.AccountBlock {
	return &nom.AccountBlock{
		Address:       administrator,
		ToAddress:     types.LiquidityContract,
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(0),
		Data: definition.ABILiquidity.PackMethodPanic(definition.NominateGuardiansMethodName,
			guardians),
	}
}

func nominateGuardiansLiq(t *testing.T, z mock.MockZenon, administrator types.Address, guardians []types.Address, delay uint64) {
	defer z.CallContract(nominateGuardiansLiqStep(administrator, guardians)).Error(t, nil)
	insertMomentums(z, 2)

	frMom, err := z.Chain().GetFrontierMomentumStore().GetFrontierMomentum()
	common.DealWithErr(err)
	z.InsertMomentumsTo(frMom.Height + delay + 2)

	defer z.CallContract(nominateGuardiansLiqStep(administrator, guardians)).Error(t, nil)
	insertMomentums(z, 2)
}

func changeAdministratorLiqStep(administrator types.Address, newAdministrator types.Address) *nom.AccountBlock {
	return &nom.AccountBlock{
		Address:       administrator,
		ToAddress:     types.LiquidityContract,
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(0),
		Data: definition.ABILiquidity.PackMethodPanic(definition.ChangeAdministratorMethodName,
			newAdministrator),
	}
}

func changeAdministratorLiq(t *testing.T, z mock.MockZenon, administrator types.Address, newAdministrator types.Address, delay uint64) {
	defer z.CallContract(changeAdministratorLiqStep(administrator, newAdministrator)).Error(t, nil)
	insertMomentums(z, 2)

	frMom, err := z.Chain().GetFrontierMomentumStore().GetFrontierMomentum()
	common.DealWithErr(err)
	z.InsertMomentumsTo(frMom.Height + delay + 2)

	defer z.CallContract(changeAdministratorLiqStep(administrator, newAdministrator)).Error(t, nil)
	insertMomentums(z, 2)
}

func proposeAdministratorLiq(guardian types.Address, proposedAdministrator types.Address) *nom.AccountBlock {
	return &nom.AccountBlock{
		Address:       guardian,
		ToAddress:     types.LiquidityContract,
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(0),
		Data: definition.ABILiquidity.PackMethodPanic(definition.ProposeAdministratorMethodName,
			proposedAdministrator),
	}
}

func activateEmergencyLiq(administrator types.Address) *nom.AccountBlock {
	return &nom.AccountBlock{
		Address:       administrator,
		ToAddress:     types.LiquidityContract,
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(0),
		Data:          definition.ABILiquidity.PackMethodPanic(definition.EmergencyMethodName),
	}
}

func donateLiq(user types.Address, zts types.ZenonTokenStandard, amount *big.Int) *nom.AccountBlock {
	return &nom.AccountBlock{
		Address:       user,
		ToAddress:     types.LiquidityContract,
		Data:          definition.ABICommon.PackMethodPanic(definition.DonateMethodName),
		TokenStandard: zts,
		Amount:        amount,
	}
}

func burnLiq(user types.Address, amount *big.Int) *nom.AccountBlock {
	return &nom.AccountBlock{
		Address:   user,
		ToAddress: types.LiquidityContract,
		Data: definition.ABILiquidity.PackMethodPanic(definition.BurnZnnMethodName,
			amount, // burnAmount
		),
	}
}

func mintForUsers(t *testing.T, z mock.MockZenon) {
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.TokenContract,
		Data:      definition.ABIToken.PackMethodPanic(definition.MintMethodName, tokens[0], big.NewInt(300*g.Zexp), g.User2.Address),
	}).Error(t, nil)
	z.InsertNewMomentum() // cemented send block
	z.InsertNewMomentum() // cemented token-receive-block
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.TokenContract,
		Data:      definition.ABIToken.PackMethodPanic(definition.MintMethodName, tokens[0], big.NewInt(200*g.Zexp), g.User3.Address),
	}).Error(t, nil)
	z.InsertNewMomentum() // cemented send block
	z.InsertNewMomentum() // cemented token-receive-block
	autoreceive(t, z, g.User2.Address)
	autoreceive(t, z, g.User3.Address)
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.TokenContract,
		Data:      definition.ABIToken.PackMethodPanic(definition.MintMethodName, tokens[0], big.NewInt(300*g.Zexp), g.User4.Address),
	}).Error(t, nil)
	z.InsertNewMomentum() // cemented send block
	z.InsertNewMomentum() // cemented token-receive-block
	autoreceive(t, z, g.User4.Address)
}
