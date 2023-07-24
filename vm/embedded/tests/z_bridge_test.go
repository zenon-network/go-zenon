package tests

import (
	"crypto/ecdsa"
	"encoding/base64"
	eabi "github.com/ethereum/go-ethereum/accounts/abi"
	ecommon "github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/zenon-network/go-zenon/vm/embedded/implementation"
	"math/big"
	"strconv"
	"testing"
	"time"

	g "github.com/zenon-network/go-zenon/chain/genesis/mock"
	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/rpc/api/embedded"
	"github.com/zenon-network/go-zenon/vm/constants"
	"github.com/zenon-network/go-zenon/vm/embedded/definition"
	"github.com/zenon-network/go-zenon/zenon/mock"
)

func activateBridge(z mock.MockZenon) {
	z.InsertSendBlock(&nom.AccountBlock{
		Address:   g.Spork.Address,
		ToAddress: types.SporkContract,
		Data: definition.ABISpork.PackMethodPanic(definition.SporkCreateMethodName,
			"spork-bridge",              // name
			"activate spork for bridge", // description
		),
	}, nil, mock.SkipVmChanges)
	z.InsertNewMomentum()

	sporkAPI := embedded.NewSporkApi(z)
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
	types.BridgeAndLiquiditySpork.SporkId = id
	types.ImplementedSporksMap[id] = true
}

// Activate spork
func activateBridgeStep0(t *testing.T, z mock.MockZenon) {
	activateBridge(z)
	z.InsertMomentumsTo(10)

	bridgeAPI := embedded.NewBridgeApi(z)
	constants.InitialBridgeAdministrator.SetBytes(g.User5.Address.Bytes())
	constants.MinAdministratorDelay = 20
	constants.MinSoftDelay = 10
	constants.MinUnhaltDurationInMomentums = 5

	common.Json(bridgeAPI.GetBridgeInfo()).Equals(t, `
{
	"administrator": "z1qqaswvt0e3cc5sm7lygkyza9ra63cr8e6zre09",
	"compressedTssECDSAPubKey": "",
	"decompressedTssECDSAPubKey": "",
	"allowKeyGen": false,
	"halted": false,
	"unhaltedAt": 0,
	"unhaltDurationInMomentums": 5,
	"tssNonce": 0,
	"metadata": "{}"
}`)
	common.Json(bridgeAPI.GetSecurityInfo()).Equals(t, `
{
	"guardians": [],
	"guardiansVotes": [],
	"administratorDelay": 20,
	"softDelay": 10
}`)
}

// Activate spork
// Set orchestratorInfo
func activateBridgeStep1(t *testing.T, z mock.MockZenon) {
	activateBridgeStep0(t, z)
	bridgeAPI := embedded.NewBridgeApi(z)

	defer z.CallContract(setOrchestratorInfo(g.User5.Address, 6, 3, 15, 10)).
		Error(t, nil)
	insertMomentums(z, 2)

	common.Json(bridgeAPI.GetOrchestratorInfo()).Equals(t, `
{
	"windowSize": 6,
	"keyGenThreshold": 3,
	"confirmationsToFinality": 15,
	"estimatedMomentumTime": 10,
	"allowKeyGenHeight": 0
}`)
}

// Activate spork
// Set orchestratorInfo
// Sets guardians
func activateBridgeStep2(t *testing.T, z mock.MockZenon) {
	activateBridgeStep1(t, z)

	bridgeAPI := embedded.NewBridgeApi(z)
	constants.MinGuardians = 4
	securityInfo, err := bridgeAPI.GetSecurityInfo()
	common.DealWithErr(err)

	guardians := []types.Address{g.User1.Address, g.User2.Address, g.User3.Address, g.User4.Address, g.User5.Address}
	nominateGuardians(t, z, g.User5.Address, guardians, securityInfo.AdministratorDelay)

	common.Json(bridgeAPI.GetSecurityInfo()).Equals(t, `
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
	common.Json(bridgeAPI.GetTimeChallengesInfo()).Equals(t, `
{
	"count": 1,
	"list": [
		{
			"MethodName": "NominateGuardians",
			"ParamsHash": "0000000000000000000000000000000000000000000000000000000000000000",
			"ChallengeStartHeight": 13
		}
	]
}`)
}

// Activate spork
// Set orchestratorInfo
// Sets guardians
// Sets tss ecdsa public key
func activateBridgeStep3(t *testing.T, z mock.MockZenon) {
	activateBridgeStep2(t, z)

	bridgeAPI := embedded.NewBridgeApi(z)
	common.Json(bridgeAPI.GetBridgeInfo()).Equals(t, `
{
	"administrator": "z1qqaswvt0e3cc5sm7lygkyza9ra63cr8e6zre09",
	"compressedTssECDSAPubKey": "",
	"decompressedTssECDSAPubKey": "",
	"allowKeyGen": false,
	"halted": false,
	"unhaltedAt": 0,
	"unhaltDurationInMomentums": 5,
	"tssNonce": 0,
	"metadata": "{}"
}`)

	securityInfo, err := bridgeAPI.GetSecurityInfo()
	common.DealWithErr(err)
	tssPubKey := "AsAQx1M3LVXCuozDOqO5b9adj/PItYgwZFG/xTDBiZzT" // priv tuSwrTEUyJI1/3y5J8L8DSjzT/AQG2IK3JG+93qhhhI=
	changeTssWithAdministrator(t, z, g.User5.Address, tssPubKey, securityInfo.SoftDelay)

	common.Json(bridgeAPI.GetBridgeInfo()).Equals(t, `
{
	"administrator": "z1qqaswvt0e3cc5sm7lygkyza9ra63cr8e6zre09",
	"compressedTssECDSAPubKey": "AsAQx1M3LVXCuozDOqO5b9adj/PItYgwZFG/xTDBiZzT",
	"decompressedTssECDSAPubKey": "BMAQx1M3LVXCuozDOqO5b9adj/PItYgwZFG/xTDBiZzTnQAT1qOPAkuPzu6yoewss9XbnTmZmb9JQNGXmkPYtK4=",
	"allowKeyGen": false,
	"halted": false,
	"unhaltedAt": 0,
	"unhaltDurationInMomentums": 5,
	"tssNonce": 0,
	"metadata": "{}"
}`)
	common.Json(bridgeAPI.GetTimeChallengesInfo()).Equals(t, `
{
	"count": 2,
	"list": [
		{
			"MethodName": "NominateGuardians",
			"ParamsHash": "0000000000000000000000000000000000000000000000000000000000000000",
			"ChallengeStartHeight": 13
		},
		{
			"MethodName": "ChangeTssECDSAPubKey",
			"ParamsHash": "0000000000000000000000000000000000000000000000000000000000000000",
			"ChallengeStartHeight": 39
		}
	]
}`)
}

// Activate spork
// Set orchestratorInfo
// Sets guardians
// Sets tss ecdsa public key
// Adds a network
func activateBridgeStep4(t *testing.T, z mock.MockZenon) {
	activateBridgeStep3(t, z)

	bridgeAPI := embedded.NewBridgeApi(z)
	networkClass := uint32(2) // evm
	chainId := uint32(123)
	defer z.CallContract(addNetwork(g.User5.Address, networkClass, chainId, "Ethereum", "0x323b5d4c32345ced77393b3530b1eed0f346429d", "{}")).
		Error(t, nil)
	insertMomentums(z, 2)

	networkInfo, err := bridgeAPI.GetNetworkInfo(networkClass, chainId)
	common.FailIfErr(t, err)
	common.Json(networkInfo, err).Equals(t, `
{
	"networkClass": 2,
	"chainId": 123,
	"name": "Ethereum",
	"contractAddress": "0x323b5d4c32345ced77393b3530b1eed0f346429d",
	"metadata": "{}",
	"tokenPairs": []
}`)
}

// Activate spork
// Set orchestratorInfo
// Sets guardians
// Sets tss ecdsa public key
// Adds a network
// Adds two token pairs, one owned, one not owned
func activateBridgeStep5(t *testing.T, z mock.MockZenon) {
	activateBridgeStep4(t, z)

	bridgeAPI := embedded.NewBridgeApi(z)
	networkClass := uint32(2) // evm
	chainId := uint32(123)

	securityInfo, err := bridgeAPI.GetSecurityInfo()
	common.DealWithErr(err)

	// Znn - not owned
	setTokenPair(t, z, g.User5.Address, securityInfo.SoftDelay, networkClass, chainId, types.ZnnTokenStandard, "0x5fbdb2315678afecb367f032d93f642f64180aa3", true, true, false,
		big.NewInt(100), uint32(15), uint32(20), `{"APR": 15, "LockingPeriod": 100}`)

	common.Json(bridgeAPI.GetTimeChallengesInfo()).Equals(t, `
{
	"count": 3,
	"list": [
		{
			"MethodName": "NominateGuardians",
			"ParamsHash": "0000000000000000000000000000000000000000000000000000000000000000",
			"ChallengeStartHeight": 13
		},
		{
			"MethodName": "ChangeTssECDSAPubKey",
			"ParamsHash": "0000000000000000000000000000000000000000000000000000000000000000",
			"ChallengeStartHeight": 39
		},
		{
			"MethodName": "SetTokenPair",
			"ParamsHash": "0000000000000000000000000000000000000000000000000000000000000000",
			"ChallengeStartHeight": 57
		}
	]
}`)

	// New zts - owned
	newZts := createZtsOwnedByBridge(t, z)
	setTokenPair(t, z, g.User5.Address, securityInfo.SoftDelay, networkClass, chainId, newZts, "0x5aaaa2315678afecb367f032d93f642f64180aa3", true, true, true,
		big.NewInt(10), uint32(100), uint32(15), `{"APR": 20, "LockingPeriod": 50}`)

	common.Json(bridgeAPI.GetTimeChallengesInfo()).Equals(t, `
{
	"count": 3,
	"list": [
		{
			"MethodName": "NominateGuardians",
			"ParamsHash": "0000000000000000000000000000000000000000000000000000000000000000",
			"ChallengeStartHeight": 13
		},
		{
			"MethodName": "ChangeTssECDSAPubKey",
			"ParamsHash": "0000000000000000000000000000000000000000000000000000000000000000",
			"ChallengeStartHeight": 39
		},
		{
			"MethodName": "SetTokenPair",
			"ParamsHash": "0000000000000000000000000000000000000000000000000000000000000000",
			"ChallengeStartHeight": 75
		}
	]
}`)

	networkInfo, err := bridgeAPI.GetNetworkInfo(networkClass, chainId)
	common.FailIfErr(t, err)
	common.Json(networkInfo, err).Equals(t, `
{
	"networkClass": 2,
	"chainId": 123,
	"name": "Ethereum",
	"contractAddress": "0x323b5d4c32345ced77393b3530b1eed0f346429d",
	"metadata": "{}",
	"tokenPairs": [
		{
			"tokenStandard": "zts1znnxxxxxxxxxxxxx9z4ulx",
			"tokenAddress": "0x5fbdb2315678afecb367f032d93f642f64180aa3",
			"bridgeable": true,
			"redeemable": true,
			"owned": false,
			"minAmount": "100",
			"feePercentage": 15,
			"redeemDelay": 20,
			"metadata": "{\"APR\": 15, \"LockingPeriod\": 100}"
		},
		{
			"tokenStandard": "zts1qanamzukd2v0pp8j2wzx6m",
			"tokenAddress": "0x5aaaa2315678afecb367f032d93f642f64180aa3",
			"bridgeable": true,
			"redeemable": true,
			"owned": true,
			"minAmount": "10",
			"feePercentage": 100,
			"redeemDelay": 15,
			"metadata": "{\"APR\": 20, \"LockingPeriod\": 50}"
		}
	]
}`)
}

// Activate spork
// Set orchestratorInfo
// Sets guardians
// Sets tss ecdsa public key
// Adds a network
// Adds two token pairs, one owned, one not owned
// Creates 2 wraps, one for the owned token, one for the unowned token
func activateBridgeStep6(t *testing.T, z mock.MockZenon) {
	activateBridgeStep5(t, z)

	bridgeAPI := embedded.NewBridgeApi(z)
	tokenAPI := embedded.NewTokenApi(z)
	networkClass := uint32(2) // evm
	chainId := uint32(123)

	z.ExpectBalance(types.BridgeContract, types.ZnnTokenStandard, 0)
	defer z.CallContract(wrapToken(types.ZnnTokenStandard, big.NewInt(150*g.Zexp), networkClass, chainId, "0xb794f5ea0ba39494ce839613fffba74279579268")).
		Error(t, nil)
	insertMomentums(z, 2)
	z.ExpectBalance(types.BridgeContract, types.ZnnTokenStandard, 15000000000)

	tokenList, err := tokenAPI.GetByOwner(types.BridgeContract, 0, 10)
	common.FailIfErr(t, err)

	z.ExpectBalance(types.BridgeContract, tokenList.List[0].ZenonTokenStandard, 0)
	defer z.CallContract(wrapToken(tokenList.List[0].ZenonTokenStandard, big.NewInt(5000), networkClass, chainId, "0xb794f5ea0ba39494ce839613fffba74279579268")).
		Error(t, nil)
	insertMomentums(z, 2)
	z.ExpectBalance(types.BridgeContract, tokenList.List[0].ZenonTokenStandard, 50)

	// We check that requests exist
	wrapRequests, err := bridgeAPI.GetAllWrapTokenRequests(0, 5)
	common.Json(wrapRequests, err).HideHashes().Equals(t, `
{
	"count": 2,
	"list": [
		{
			"networkClass": 2,
			"chainId": 123,
			"id": "XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX",
			"toAddress": "0xb794f5ea0ba39494ce839613fffba74279579268",
			"tokenStandard": "zts1qanamzukd2v0pp8j2wzx6m",
			"tokenAddress": "0x5aaaa2315678afecb367f032d93f642f64180aa3",
			"amount": "5000",
			"fee": "50",
			"signature": "",
			"creationMomentumHeight": 91,
			"token": {
				"name": "test.tok3n_na-m3",
				"symbol": "TEST",
				"domain": "",
				"totalSupply": "95050",
				"decimals": 1,
				"owner": "z1qxemdeddedxdrydgexxxxxxxxxxxxxxxmqgr0d",
				"tokenStandard": "zts1qanamzukd2v0pp8j2wzx6m",
				"maxSupply": "1000000",
				"isBurnable": true,
				"isMintable": true,
				"isUtility": false
			},
			"confirmationsToFinality": 14
		},
		{
			"networkClass": 2,
			"chainId": 123,
			"id": "XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX",
			"toAddress": "0xb794f5ea0ba39494ce839613fffba74279579268",
			"tokenStandard": "zts1znnxxxxxxxxxxxxx9z4ulx",
			"tokenAddress": "0x5fbdb2315678afecb367f032d93f642f64180aa3",
			"amount": "15000000000",
			"fee": "22500000",
			"signature": "",
			"creationMomentumHeight": 89,
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
			"confirmationsToFinality": 12
		}
	]
}`)

	// We check that the existing fees are correct
	common.Json(bridgeAPI.GetFeeTokenPair(types.ZnnTokenStandard)).Equals(t, `
{
	"tokenStandard": "zts1znnxxxxxxxxxxxxx9z4ulx",
	"accumulatedFee": "22500000"
}`)

	common.Json(bridgeAPI.GetFeeTokenPair(tokenList.List[0].ZenonTokenStandard)).Equals(t, `
{
	"tokenStandard": "zts1qanamzukd2v0pp8j2wzx6m",
	"accumulatedFee": "50"
}`)
}

// Activate spork
// Set orchestratorInfo
// Sets guardians
// Sets tss ecdsa public key
// Adds a network
// Adds two token pairs, one owned, one not owned
// Creates 2 wraps, one for the owned token, one for the unowned token
// Create 2 unwrap token requests
func activateBridgeStep7(t *testing.T, z mock.MockZenon) {
	activateBridgeStep6(t, z)

	networkClass := uint32(2) // evm
	chainId := uint32(123)

	tokenAddress := "0x5fbdb2315678afecb367f032d93f642f64180aa3"
	hash := types.HexToHashPanic("0123456789012345678901234567890123456789012345678901234567890123")

	// unwrap znn
	signature := getUnwrapTokenSignature(t, networkClass, chainId, hash, 200, tokenAddress, big.NewInt(100*g.Zexp), networkClass)
	defer z.CallContract(unwrapToken(networkClass, chainId, hash, 200, tokenAddress, big.NewInt(100*g.Zexp), signature)).
		Error(t, nil)
	insertMomentums(z, 2)

	// unwrap token owned by bridge
	tokenAddress = "0x5aaaa2315678afecb367f032d93f642f64180aa3"
	hash = types.HexToHashPanic("0023456789012345678901234567890123456789012345678901234567890123")
	signature = getUnwrapTokenSignature(t, networkClass, chainId, hash, 200, tokenAddress, big.NewInt(800), networkClass)
	defer z.CallContract(unwrapToken(networkClass, chainId, hash, 200, tokenAddress, big.NewInt(800), signature)).
		Error(t, nil)
	insertMomentums(z, 2)

	bridgeAPI := embedded.NewBridgeApi(z)
	unwrapRequests, err := bridgeAPI.GetAllUnwrapTokenRequests(0, 10)
	common.Json(unwrapRequests, err).HideHashes().Equals(t, `
{
	"count": 2,
	"list": [
		{
			"registrationMomentumHeight": 95,
			"networkClass": 2,
			"chainId": 123,
			"transactionHash": "XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX",
			"logIndex": 200,
			"toAddress": "z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx",
			"tokenAddress": "0x5aaaa2315678afecb367f032d93f642f64180aa3",
			"tokenStandard": "zts1qanamzukd2v0pp8j2wzx6m",
			"amount": "800",
			"signature": "eCKubWhwnuqqX9vjGl9ltqxCNwE2V9Xi4bO1q404JYNCUlJ0c3h5Cq558pLxtimrS73hPStjtz281+GcfNPTyAE=",
			"redeemed": 0,
			"revoked": 0,
			"token": {
				"name": "test.tok3n_na-m3",
				"symbol": "TEST",
				"domain": "",
				"totalSupply": "95050",
				"decimals": 1,
				"owner": "z1qxemdeddedxdrydgexxxxxxxxxxxxxxxmqgr0d",
				"tokenStandard": "zts1qanamzukd2v0pp8j2wzx6m",
				"maxSupply": "1000000",
				"isBurnable": true,
				"isMintable": true,
				"isUtility": false
			},
			"redeemableIn": 14
		},
		{
			"registrationMomentumHeight": 93,
			"networkClass": 2,
			"chainId": 123,
			"transactionHash": "XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX",
			"logIndex": 200,
			"toAddress": "z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx",
			"tokenAddress": "0x5fbdb2315678afecb367f032d93f642f64180aa3",
			"tokenStandard": "zts1znnxxxxxxxxxxxxx9z4ulx",
			"amount": "10000000000",
			"signature": "YXFeAH2BIKnbHm7qd1zXt6SSReI9ovhYg5jVPGumNOkj3Bw6OfUwSZSCKSMPCw4NRZGDXT6m7Axe5UIwAno4xQE=",
			"redeemed": 0,
			"revoked": 0,
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
			"redeemableIn": 17
		}
	]
}`)
}

// Activate spork
// Set orchestratorInfo
// Sets guardians
// Sets tss ecdsa public key
// Adds a network
// Adds two token pairs, one owned, one not owned
// Creates 2 wraps, one for the owned token, one for the unowned token
// Create 2 unwrap token requests
// Update the signature of the two wraps
func activateBridgeStep8(t *testing.T, z mock.MockZenon) {
	activateBridgeStep7(t, z)

	bridgeAPI := embedded.NewBridgeApi(z)
	wrapRequests, err := bridgeAPI.GetAllWrapTokenRequests(0, 5)
	common.DealWithErr(err)
	contractAddress := ecommon.HexToAddress("0x323b5d4c32345ced77393b3530b1eed0f346429d")

	signature := getUpdateWrapTokenSignature(wrapRequests.List[0], contractAddress, "tuSwrTEUyJI1/3y5J8L8DSjzT/AQG2IK3JG+93qhhhI=")
	defer z.CallContract(updateWrapToken(wrapRequests.List[0].Id, signature)).Error(t, nil)
	insertMomentums(z, 2)
	signature = getUpdateWrapTokenSignature(wrapRequests.List[1], contractAddress, "tuSwrTEUyJI1/3y5J8L8DSjzT/AQG2IK3JG+93qhhhI=")
	defer z.CallContract(updateWrapToken(wrapRequests.List[1].Id, signature)).Error(t, nil)
	insertMomentums(z, 2)

	common.Json(bridgeAPI.GetAllWrapTokenRequests(0, 5)).HideHashes().Equals(t, `
{
	"count": 2,
	"list": [
		{
			"networkClass": 2,
			"chainId": 123,
			"id": "XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX",
			"toAddress": "0xb794f5ea0ba39494ce839613fffba74279579268",
			"tokenStandard": "zts1qanamzukd2v0pp8j2wzx6m",
			"tokenAddress": "0x5aaaa2315678afecb367f032d93f642f64180aa3",
			"amount": "5000",
			"fee": "50",
			"signature": "+i2E5GEjpFbGKIune7b6sl29+RNUCou8UniY0xXvZehRPRBCjPFuu7QukqUFEU9qzongnGqi2+6d7YcwlfTXBwE=",
			"creationMomentumHeight": 91,
			"token": {
				"name": "test.tok3n_na-m3",
				"symbol": "TEST",
				"domain": "",
				"totalSupply": "95050",
				"decimals": 1,
				"owner": "z1qxemdeddedxdrydgexxxxxxxxxxxxxxxmqgr0d",
				"tokenStandard": "zts1qanamzukd2v0pp8j2wzx6m",
				"maxSupply": "1000000",
				"isBurnable": true,
				"isMintable": true,
				"isUtility": false
			},
			"confirmationsToFinality": 6
		},
		{
			"networkClass": 2,
			"chainId": 123,
			"id": "XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX",
			"toAddress": "0xb794f5ea0ba39494ce839613fffba74279579268",
			"tokenStandard": "zts1znnxxxxxxxxxxxxx9z4ulx",
			"tokenAddress": "0x5fbdb2315678afecb367f032d93f642f64180aa3",
			"amount": "15000000000",
			"fee": "22500000",
			"signature": "TxMtWKUPCOJjdSbshzc4id58m0XSrHCDVQjI20ys6TZmazUDP6KPsNjR4Bdjx0BU7svPOSU+XM6l372X+E1aoAE=",
			"creationMomentumHeight": 89,
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
			"confirmationsToFinality": 4
		}
	]
}`)
}

// Activate spork
// Set orchestratorInfo
// Sets guardians
// Sets tss ecdsa public key
// Adds a network
// Adds two token pairs, one owned, one not owned
// Creates 2 wraps, one for the owned token, one for the unowned token
// Create 2 unwrap token requests
// Update the signature of the two wraps
// Redeem the two unwrap requests
func activateBridgeStep9(t *testing.T, z mock.MockZenon) {
	activateBridgeStep8(t, z)

	bridgeAPI := embedded.NewBridgeApi(z)
	requests, err := bridgeAPI.GetAllUnwrapTokenRequests(0, 2)
	common.DealWithErr(err)

	frMom, err := z.Chain().GetFrontierMomentumStore().GetFrontierMomentum()
	common.DealWithErr(err)
	z.InsertMomentumsTo(frMom.Height + requests.List[0].RedeemableIn)

	// owned token
	z.ExpectBalance(g.User2.Address, requests.List[0].TokenStandard, 0)
	z.ExpectBalance(types.BridgeContract, requests.List[0].TokenStandard, 50)
	defer z.CallContract(redeemUnwrap(requests.List[0].TransactionHash, requests.List[0].LogIndex)).Error(t, nil)
	insertMomentums(z, 3)
	autoreceive(t, z, g.User2.Address)
	z.InsertNewMomentum()
	z.ExpectBalance(g.User2.Address, requests.List[0].TokenStandard, 800)
	z.ExpectBalance(types.BridgeContract, requests.List[0].TokenStandard, 50)

	z.InsertMomentumsTo(frMom.Height + requests.List[1].RedeemableIn)

	// znn
	z.ExpectBalance(g.User2.Address, requests.List[1].TokenStandard, 800000000000)
	z.ExpectBalance(types.BridgeContract, requests.List[1].TokenStandard, 15000000000)
	defer z.CallContract(redeemUnwrap(requests.List[1].TransactionHash, requests.List[1].LogIndex)).Error(t, nil)
	insertMomentums(z, 3)
	autoreceive(t, z, g.User2.Address)
	z.InsertNewMomentum()
	z.ExpectBalance(g.User2.Address, requests.List[1].TokenStandard, 810000000000)
	z.ExpectBalance(types.BridgeContract, requests.List[1].TokenStandard, 5000000000)

	unwrapRequests, err := bridgeAPI.GetAllUnwrapTokenRequests(0, 10)
	common.Json(unwrapRequests, err).HideHashes().Equals(t, `
{
	"count": 2,
	"list": [
		{
			"registrationMomentumHeight": 95,
			"networkClass": 2,
			"chainId": 123,
			"transactionHash": "XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX",
			"logIndex": 200,
			"toAddress": "z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx",
			"tokenAddress": "0x5aaaa2315678afecb367f032d93f642f64180aa3",
			"tokenStandard": "zts1qanamzukd2v0pp8j2wzx6m",
			"amount": "800",
			"signature": "eCKubWhwnuqqX9vjGl9ltqxCNwE2V9Xi4bO1q404JYNCUlJ0c3h5Cq558pLxtimrS73hPStjtz281+GcfNPTyAE=",
			"redeemed": 1,
			"revoked": 0,
			"token": {
				"name": "test.tok3n_na-m3",
				"symbol": "TEST",
				"domain": "",
				"totalSupply": "95850",
				"decimals": 1,
				"owner": "z1qxemdeddedxdrydgexxxxxxxxxxxxxxxmqgr0d",
				"tokenStandard": "zts1qanamzukd2v0pp8j2wzx6m",
				"maxSupply": "1000000",
				"isBurnable": true,
				"isMintable": true,
				"isUtility": false
			},
			"redeemableIn": 0
		},
		{
			"registrationMomentumHeight": 93,
			"networkClass": 2,
			"chainId": 123,
			"transactionHash": "XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX",
			"logIndex": 200,
			"toAddress": "z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx",
			"tokenAddress": "0x5fbdb2315678afecb367f032d93f642f64180aa3",
			"tokenStandard": "zts1znnxxxxxxxxxxxxx9z4ulx",
			"amount": "10000000000",
			"signature": "YXFeAH2BIKnbHm7qd1zXt6SSReI9ovhYg5jVPGumNOkj3Bw6OfUwSZSCKSMPCw4NRZGDXT6m7Axe5UIwAno4xQE=",
			"redeemed": 1,
			"revoked": 0,
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
			"redeemableIn": 0
		}
	]
}`)
}

func TestBridge(t *testing.T) {
	z := mock.NewMockZenonWithCustomEpochDuration(t, time.Hour)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).Equals(t, `
t=2001-09-09T01:46:50+0000 lvl=dbug msg=created module=embedded contract=spork spork="&{Id:c6a597f757168bd5c9fddf52b16b3bf38e2ef781fb8edeea1bf2ae0d3225230d Name:spork-bridge Description:activate spork for bridge Activated:false EnforcementHeight:0}"
t=2001-09-09T01:47:00+0000 lvl=dbug msg=activated module=embedded contract=spork spork="&{Id:c6a597f757168bd5c9fddf52b16b3bf38e2ef781fb8edeea1bf2ae0d3225230d Name:spork-bridge Description:activate spork for bridge Activated:true EnforcementHeight:9}"
t=2001-09-09T01:58:20+0000 lvl=dbug msg="issued ZTS" module=embedded contract=token token="{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenName:test.tok3n_na-m3 TokenSymbol:TEST TokenDomain: TotalSupply:+100000 MaxSupply:+1000000 Decimals:1 IsMintable:true IsBurnable:true IsUtility:false TokenStandard:zts1qanamzukd2v0pp8j2wzx6m}"
t=2001-09-09T01:58:40+0000 lvl=dbug msg="updating token owner" module=embedded contract=token old=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz new=z1qxemdeddedxdrydgexxxxxxxxxxxxxxxmqgr0d
t=2001-09-09T01:58:40+0000 lvl=dbug msg="updated ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxdrydgexxxxxxxxxxxxxxxmqgr0d TokenName:test.tok3n_na-m3 TokenSymbol:TEST TokenDomain: TotalSupply:+100000 MaxSupply:+1000000 Decimals:1 IsMintable:true IsBurnable:true IsUtility:false TokenStandard:zts1qanamzukd2v0pp8j2wzx6m}"
t=2001-09-09T02:01:50+0000 lvl=dbug msg="burned ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxdrydgexxxxxxxxxxxxxxxmqgr0d TokenName:test.tok3n_na-m3 TokenSymbol:TEST TokenDomain: TotalSupply:+95050 MaxSupply:+1000000 Decimals:1 IsMintable:true IsBurnable:true IsUtility:false TokenStandard:zts1qanamzukd2v0pp8j2wzx6m}" burned-amount=4950
t=2001-09-09T02:05:10+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxdrydgexxxxxxxxxxxxxxxmqgr0d TokenName:test.tok3n_na-m3 TokenSymbol:TEST TokenDomain: TotalSupply:+95850 MaxSupply:+1000000 Decimals:1 IsMintable:true IsBurnable:true IsUtility:false TokenStandard:zts1qanamzukd2v0pp8j2wzx6m}" minted-amount=800 to-address=z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx
`)

	activateBridgeStep9(t, z)

	bridgeAPI := embedded.NewBridgeApi(z)
	bridgeInfo, err := bridgeAPI.GetBridgeInfo()

	common.Json(bridgeInfo, err).Equals(t, `
{
	"administrator": "z1qqaswvt0e3cc5sm7lygkyza9ra63cr8e6zre09",
	"compressedTssECDSAPubKey": "AsAQx1M3LVXCuozDOqO5b9adj/PItYgwZFG/xTDBiZzT",
	"decompressedTssECDSAPubKey": "BMAQx1M3LVXCuozDOqO5b9adj/PItYgwZFG/xTDBiZzTnQAT1qOPAkuPzu6yoewss9XbnTmZmb9JQNGXmkPYtK4=",
	"allowKeyGen": false,
	"halted": false,
	"unhaltedAt": 0,
	"unhaltDurationInMomentums": 5,
	"tssNonce": 0,
	"metadata": "{}"
}`)
}

func TestBridge_ActionsWhenInEmergency(t *testing.T) {
	z := mock.NewMockZenonWithCustomEpochDuration(t, time.Hour)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).Equals(t, `
t=2001-09-09T01:46:50+0000 lvl=dbug msg=created module=embedded contract=spork spork="&{Id:c6a597f757168bd5c9fddf52b16b3bf38e2ef781fb8edeea1bf2ae0d3225230d Name:spork-bridge Description:activate spork for bridge Activated:false EnforcementHeight:0}"
t=2001-09-09T01:47:00+0000 lvl=dbug msg=activated module=embedded contract=spork spork="&{Id:c6a597f757168bd5c9fddf52b16b3bf38e2ef781fb8edeea1bf2ae0d3225230d Name:spork-bridge Description:activate spork for bridge Activated:true EnforcementHeight:9}"
t=2001-09-09T01:58:20+0000 lvl=dbug msg="issued ZTS" module=embedded contract=token token="{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenName:test.tok3n_na-m3 TokenSymbol:TEST TokenDomain: TotalSupply:+100000 MaxSupply:+1000000 Decimals:1 IsMintable:true IsBurnable:true IsUtility:false TokenStandard:zts1qanamzukd2v0pp8j2wzx6m}"
t=2001-09-09T01:58:40+0000 lvl=dbug msg="updating token owner" module=embedded contract=token old=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz new=z1qxemdeddedxdrydgexxxxxxxxxxxxxxxmqgr0d
t=2001-09-09T01:58:40+0000 lvl=dbug msg="updated ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxdrydgexxxxxxxxxxxxxxxmqgr0d TokenName:test.tok3n_na-m3 TokenSymbol:TEST TokenDomain: TotalSupply:+100000 MaxSupply:+1000000 Decimals:1 IsMintable:true IsBurnable:true IsUtility:false TokenStandard:zts1qanamzukd2v0pp8j2wzx6m}"
t=2001-09-09T02:01:50+0000 lvl=dbug msg="burned ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxdrydgexxxxxxxxxxxxxxxmqgr0d TokenName:test.tok3n_na-m3 TokenSymbol:TEST TokenDomain: TotalSupply:+95050 MaxSupply:+1000000 Decimals:1 IsMintable:true IsBurnable:true IsUtility:false TokenStandard:zts1qanamzukd2v0pp8j2wzx6m}" burned-amount=4950
`)

	// We go to step 7 when we have valid unwraps and wraps so we can try to redeem or update them
	activateBridgeStep7(t, z)

	defer z.CallContract(activateEmergency(g.User5.Address)).Error(t, nil)
	insertMomentums(z, 2)

	bridgeAPI := embedded.NewBridgeApi(z)
	bridgeInfo, err := bridgeAPI.GetBridgeInfo()
	common.DealWithErr(err)

	// We make sure we are in emergency
	common.Json(bridgeInfo, err).Equals(t, `
{
	"administrator": "z1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqsggv2f",
	"compressedTssECDSAPubKey": "",
	"decompressedTssECDSAPubKey": "",
	"allowKeyGen": false,
	"halted": true,
	"unhaltedAt": 0,
	"unhaltDurationInMomentums": 5,
	"tssNonce": 0,
	"metadata": "{}"
}`)

	networkClass := uint32(2) // evm
	chainId := uint32(123)

	// Try to activate emergency again
	defer z.CallContract(activateEmergency(g.User5.Address)).Error(t, constants.ErrPermissionDenied)
	insertMomentums(z, 2)

	// Try wrap
	defer z.CallContract(wrapToken(types.ZnnTokenStandard, big.NewInt(15*g.Zexp), networkClass, chainId, "0xb794f5ea0ba39494ce839613fffba74279579268")).
		Error(t, constants.ErrBridgeNotInitialized)
	insertMomentums(z, 2)

	// Try updating existing wrap
	wrapRequests, err := bridgeAPI.GetAllWrapTokenRequests(0, 5)
	common.DealWithErr(err)
	contractAddress := ecommon.HexToAddress("0x323b5d4c32345ced77393b3530b1eed0f346429d")
	updateSignature := getUpdateWrapTokenSignature(wrapRequests.List[0], contractAddress, "tuSwrTEUyJI1/3y5J8L8DSjzT/AQG2IK3JG+93qhhhI=")
	defer z.CallContract(updateWrapToken(wrapRequests.List[0].Id, updateSignature)).
		Error(t, constants.ErrBridgeNotInitialized)
	insertMomentums(z, 2)

	// Try unwrap
	tokenAddress := "0x5aaaa2315678afecb367f032d93f642f64180aa3"
	hash := types.HexToHashPanic("0003456789012345678901234567890123456789012345678901234567890123")
	signature := getUnwrapTokenSignature(t, networkClass, chainId, hash, 200, tokenAddress, big.NewInt(100*g.Zexp), networkClass)
	defer z.CallContract(unwrapToken(networkClass, chainId, hash, 500, tokenAddress, big.NewInt(800), signature)).
		Error(t, constants.ErrBridgeNotInitialized)
	insertMomentums(z, 2)

	// Try redeem existing unwrap
	requests, err := bridgeAPI.GetAllUnwrapTokenRequests(0, 2)
	common.DealWithErr(err)

	frMom, err := z.Chain().GetFrontierMomentumStore().GetFrontierMomentum()
	common.DealWithErr(err)
	z.InsertMomentumsTo(frMom.Height + requests.List[0].RedeemableIn)

	//// owned token
	defer z.CallContract(redeemUnwrap(requests.List[0].TransactionHash, requests.List[0].LogIndex)).
		Error(t, constants.ErrBridgeNotInitialized)
	insertMomentums(z, 3)
	autoreceive(t, z, g.User2.Address)
	z.InsertNewMomentum()

	// Revoke unwrap token
	defer z.CallContract(revokeUnwrap(g.User5.Address, requests.List[0].TransactionHash, requests.List[0].LogIndex)).
		Error(t, constants.ErrPermissionDenied)
	insertMomentums(z, 2)

	// Try removing a network
	defer z.CallContract(removeNetwork(g.User5.Address, networkClass, chainId)).
		Error(t, constants.ErrPermissionDenied)
	insertMomentums(z, 2)

	// Try to set network metadata
	defer z.CallContract(setNetworkMetadata(g.User5.Address, networkClass, chainId, `{"APY":15}`)).
		Error(t, constants.ErrPermissionDenied)
	insertMomentums(z, 2)

	// Try adding a network
	networkClass = uint32(2) // evm
	chainId = uint32(124)
	defer z.CallContract(addNetwork(g.User5.Address, networkClass, chainId, "Ethereum", "0x323b5d4c32345ced77393b3530b1eed0f346429d", "{}")).
		Error(t, constants.ErrPermissionDenied)
	insertMomentums(z, 2)

	// Try to set a tokenPair
	//// modify
	defer z.CallContract(setTokenPairStep(g.User5.Address, networkClass, chainId, types.ZnnTokenStandard, "0x5fbdb2315678afecb367f032d93f642f64180aa3", true, true, false,
		big.NewInt(100), uint32(15), uint32(20), `{"APR": 15, "LockingPeriod": 100}`)).
		Error(t, constants.ErrPermissionDenied)
	insertMomentums(z, 2)
	//// add a new one
	defer z.CallContract(setTokenPairStep(g.User5.Address, networkClass, chainId, types.QsrTokenStandard, "0x6bbbb2315678afecb367f032d93f642f64180aa3", true, true, false,
		big.NewInt(100), uint32(15), uint32(20), `{"APR": 15, "LockingPeriod": 100}`)).
		Error(t, constants.ErrPermissionDenied)
	insertMomentums(z, 2)

	// Try to remove a tokenPair
	defer z.CallContract(removeTokenPair(g.User5.Address, networkClass, chainId, types.ZnnTokenStandard, "0x5fbdb2315678afecb367f032d93f642f64180aa3")).
		Error(t, constants.ErrPermissionDenied)
	insertMomentums(z, 2)

	// Try to halt
	defer z.CallContract(haltWithAdmin(g.User5.Address)).Error(t, constants.ErrBridgeHalted)
	insertMomentums(z, 2)
	defer z.CallContract(haltWithSignature(getHaltSignature(t, z, bridgeInfo.TssNonce))).Error(t, constants.ErrBridgeHalted)
	insertMomentums(z, 2)

	// Try to unhalt
	defer z.CallContract(unhalt(g.User5.Address)).Error(t, constants.ErrPermissionDenied)
	insertMomentums(z, 2)

	// Try to change administrator
	defer z.CallContract(changeAdministratorStep(g.User5.Address, g.User4.Address)).Error(t, constants.ErrPermissionDenied)
	insertMomentums(z, 2)

	// Try to change tss
	//// change with admin
	publicKey := "AhOiqdjx002Cj8o1jxTM5LqywbgNFZwUPJuR9ffdQwFP" // priv Sf12dS9DI7xsiKrmQfPR8zQE1HUIYkd8x0XZ6fkAxXo=
	defer z.CallContract(changeTssWithAdministratorStep(g.User5.Address, publicKey)).Error(t, constants.ErrNotAllowedToChangeTss)
	insertMomentums(z, 2)

	//// change with signatures
	message, err := implementation.GetBasicMethodMessage(definition.ChangeTssECDSAPubKeyMethodName, bridgeInfo.TssNonce, definition.NoMClass, z.Chain().ChainIdentifier())
	oldSignature, err := sign(message, "tuSwrTEUyJI1/3y5J8L8DSjzT/AQG2IK3JG+93qhhhI=")
	newSignature, err := sign(message, "Sf12dS9DI7xsiKrmQfPR8zQE1HUIYkd8x0XZ6fkAxXo=")
	defer z.CallContract(changeTssWithSignature(publicKey, oldSignature, newSignature)).Error(t, constants.ErrNotAllowedToChangeTss)
	insertMomentums(z, 2)

	// Try to nominate guardians
	guardians := []types.Address{g.User1.Address, g.User2.Address, g.User3.Address, g.User4.Address, g.User5.Address, g.User6.Address}
	defer z.CallContract(nominateGuardiansStep(g.User5.Address, guardians)).Error(t, constants.ErrPermissionDenied)
	insertMomentums(z, 2)

	// Try to setAllowKeyGen
	defer z.CallContract(setAllowKeyGen(g.User5.Address, true)).Error(t, constants.ErrBridgeHalted)
	insertMomentums(z, 2)

	// Try to set orchestratorInfo
	defer z.CallContract(setOrchestratorInfo(g.User5.Address, 6, 3, 15, 10)).
		Error(t, constants.ErrPermissionDenied)
	insertMomentums(z, 2)

	// Try to propose administrator
	defer z.CallContract(proposeAdministrator(g.User1.Address, g.User6.Address)).Error(t, nil)
	insertMomentums(z, 2)
	// Try to set bridge metadata
	defer z.CallContract(setBridgeMetadata(g.User5.Address, `{"APY":15}`)).Error(t, constants.ErrPermissionDenied)
	insertMomentums(z, 2)
}

func TestBridge_ActionsWhenHalted(t *testing.T) {
	z := mock.NewMockZenonWithCustomEpochDuration(t, time.Hour)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).Equals(t, `
t=2001-09-09T01:46:50+0000 lvl=dbug msg=created module=embedded contract=spork spork="&{Id:c6a597f757168bd5c9fddf52b16b3bf38e2ef781fb8edeea1bf2ae0d3225230d Name:spork-bridge Description:activate spork for bridge Activated:false EnforcementHeight:0}"
t=2001-09-09T01:47:00+0000 lvl=dbug msg=activated module=embedded contract=spork spork="&{Id:c6a597f757168bd5c9fddf52b16b3bf38e2ef781fb8edeea1bf2ae0d3225230d Name:spork-bridge Description:activate spork for bridge Activated:true EnforcementHeight:9}"
t=2001-09-09T01:58:20+0000 lvl=dbug msg="issued ZTS" module=embedded contract=token token="{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenName:test.tok3n_na-m3 TokenSymbol:TEST TokenDomain: TotalSupply:+100000 MaxSupply:+1000000 Decimals:1 IsMintable:true IsBurnable:true IsUtility:false TokenStandard:zts1qanamzukd2v0pp8j2wzx6m}"
t=2001-09-09T01:58:40+0000 lvl=dbug msg="updating token owner" module=embedded contract=token old=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz new=z1qxemdeddedxdrydgexxxxxxxxxxxxxxxmqgr0d
t=2001-09-09T01:58:40+0000 lvl=dbug msg="updated ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxdrydgexxxxxxxxxxxxxxxmqgr0d TokenName:test.tok3n_na-m3 TokenSymbol:TEST TokenDomain: TotalSupply:+100000 MaxSupply:+1000000 Decimals:1 IsMintable:true IsBurnable:true IsUtility:false TokenStandard:zts1qanamzukd2v0pp8j2wzx6m}"
t=2001-09-09T02:01:50+0000 lvl=dbug msg="burned ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxdrydgexxxxxxxxxxxxxxxmqgr0d TokenName:test.tok3n_na-m3 TokenSymbol:TEST TokenDomain: TotalSupply:+95050 MaxSupply:+1000000 Decimals:1 IsMintable:true IsBurnable:true IsUtility:false TokenStandard:zts1qanamzukd2v0pp8j2wzx6m}" burned-amount=4950
`)

	// We go to step 7 when we have valid unwraps and wraps so we can try to redeem or update them
	activateBridgeStep7(t, z)

	// We set allow key gen here se we can test changeTss with signature in halted mode
	defer z.CallContract(setAllowKeyGen(g.User5.Address, true)).Error(t, nil)
	insertMomentums(z, 2)

	defer z.CallContract(haltWithAdmin(g.User5.Address)).Error(t, nil)
	insertMomentums(z, 2)

	bridgeAPI := embedded.NewBridgeApi(z)
	bridgeInfo, err := bridgeAPI.GetBridgeInfo()
	common.DealWithErr(err)

	networkClass := uint32(2) // evm
	chainId := uint32(123)

	// Try wrap
	defer z.CallContract(wrapToken(types.ZnnTokenStandard, big.NewInt(15*g.Zexp), networkClass, chainId, "0xb794f5ea0ba39494ce839613fffba74279579268")).
		Error(t, constants.ErrBridgeHalted)
	insertMomentums(z, 2)

	// Try updating existing wrap
	wrapRequests, err := bridgeAPI.GetAllWrapTokenRequests(0, 5)
	common.DealWithErr(err)
	contractAddress := ecommon.HexToAddress("0x323b5d4c32345ced77393b3530b1eed0f346429d")
	updateSignature := getUpdateWrapTokenSignature(wrapRequests.List[0], contractAddress, "tuSwrTEUyJI1/3y5J8L8DSjzT/AQG2IK3JG+93qhhhI=")
	defer z.CallContract(updateWrapToken(wrapRequests.List[0].Id, updateSignature)).
		Error(t, constants.ErrBridgeHalted)
	insertMomentums(z, 2)

	// Try unwrap
	tokenAddress := "0x5aaaa2315678afecb367f032d93f642f64180aa3"
	hash := types.HexToHashPanic("0003456789012345678901234567890123456789012345678901234567890123")
	signature := getUnwrapTokenSignature(t, networkClass, chainId, hash, 200, tokenAddress, big.NewInt(100*g.Zexp), networkClass)
	defer z.CallContract(unwrapToken(networkClass, chainId, hash, 500, tokenAddress, big.NewInt(800), signature)).
		Error(t, constants.ErrBridgeHalted)
	insertMomentums(z, 2)

	// Try redeem existing unwrap
	requests, err := bridgeAPI.GetAllUnwrapTokenRequests(0, 2)
	common.DealWithErr(err)

	frMom, err := z.Chain().GetFrontierMomentumStore().GetFrontierMomentum()
	common.DealWithErr(err)
	z.InsertMomentumsTo(frMom.Height + requests.List[0].RedeemableIn)

	//// owned token
	defer z.CallContract(redeemUnwrap(requests.List[0].TransactionHash, requests.List[0].LogIndex)).
		Error(t, constants.ErrBridgeHalted)
	insertMomentums(z, 2)

	autoreceive(t, z, g.User2.Address)
	z.InsertNewMomentum()
	// Revoke unwrap token
	defer z.CallContract(revokeUnwrap(g.User5.Address, requests.List[1].TransactionHash, requests.List[1].LogIndex)).
		Error(t, nil)
	insertMomentums(z, 2)

	common.Json(bridgeAPI.GetAllUnwrapTokenRequests(0, 2)).Equals(t, `
{
	"count": 2,
	"list": [
		{
			"registrationMomentumHeight": 95,
			"networkClass": 2,
			"chainId": 123,
			"transactionHash": "0023456789012345678901234567890123456789012345678901234567890123",
			"logIndex": 200,
			"toAddress": "z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx",
			"tokenAddress": "0x5aaaa2315678afecb367f032d93f642f64180aa3",
			"tokenStandard": "zts1qanamzukd2v0pp8j2wzx6m",
			"amount": "800",
			"signature": "eCKubWhwnuqqX9vjGl9ltqxCNwE2V9Xi4bO1q404JYNCUlJ0c3h5Cq558pLxtimrS73hPStjtz281+GcfNPTyAE=",
			"redeemed": 0,
			"revoked": 0,
			"token": {
				"name": "test.tok3n_na-m3",
				"symbol": "TEST",
				"domain": "",
				"totalSupply": "95050",
				"decimals": 1,
				"owner": "z1qxemdeddedxdrydgexxxxxxxxxxxxxxxmqgr0d",
				"tokenStandard": "zts1qanamzukd2v0pp8j2wzx6m",
				"maxSupply": "1000000",
				"isBurnable": true,
				"isMintable": true,
				"isUtility": false
			},
			"redeemableIn": 0
		},
		{
			"registrationMomentumHeight": 93,
			"networkClass": 2,
			"chainId": 123,
			"transactionHash": "0123456789012345678901234567890123456789012345678901234567890123",
			"logIndex": 200,
			"toAddress": "z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx",
			"tokenAddress": "0x5fbdb2315678afecb367f032d93f642f64180aa3",
			"tokenStandard": "zts1znnxxxxxxxxxxxxx9z4ulx",
			"amount": "10000000000",
			"signature": "YXFeAH2BIKnbHm7qd1zXt6SSReI9ovhYg5jVPGumNOkj3Bw6OfUwSZSCKSMPCw4NRZGDXT6m7Axe5UIwAno4xQE=",
			"redeemed": 0,
			"revoked": 1,
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
			"redeemableIn": 0
		}
	]
}
`)

	// Try adding a network
	chainId = 124
	defer z.CallContract(addNetwork(g.User5.Address, networkClass, chainId, "Ethereum", "0x323b5d4c32345ced77393b3530b1eed0f346429d", "{}")).
		Error(t, nil)
	insertMomentums(z, 2)

	// Try to set network metadata
	defer z.CallContract(setNetworkMetadata(g.User5.Address, networkClass, chainId, `{"APYYYY":15}`)).Error(t, nil)
	insertMomentums(z, 2)

	common.Json(bridgeAPI.GetNetworkInfo(networkClass, chainId)).Equals(t, `
{
	"networkClass": 2,
	"chainId": 124,
	"name": "Ethereum",
	"contractAddress": "0x323b5d4c32345ced77393b3530b1eed0f346429d",
	"metadata": "{\"APYYYY\":15}",
	"tokenPairs": []
}`)

	// Try removing a network
	defer z.CallContract(removeNetwork(g.User5.Address, networkClass, chainId)).Error(t, nil)
	insertMomentums(z, 2)

	common.Json(bridgeAPI.GetNetworkInfo(networkClass, chainId)).Equals(t, `
{
	"networkClass": 0,
	"chainId": 0,
	"name": "",
	"contractAddress": "",
	"metadata": "{}",
	"tokenPairs": null
}`)

	// Try to set a tokenPair
	chainId = 123
	securityInfo, err := bridgeAPI.GetSecurityInfo()
	common.DealWithErr(err)
	//// modify
	setTokenPair(t, z, g.User5.Address, securityInfo.SoftDelay, networkClass, chainId, types.ZnnTokenStandard, "0x5fbdb2315678afecb367f032d93f642f64180aa3", true, true, false,
		big.NewInt(100), uint32(20), uint32(25), `{"APR": 25, "LockingPeriod": 100}`)
	//// add a new one
	setTokenPair(t, z, g.User5.Address, securityInfo.SoftDelay, networkClass, chainId, types.QsrTokenStandard, "0x6bbbb2315678afecb367f032d93f642f64180aa3", true, true, false,
		big.NewInt(100), uint32(15), uint32(20), `{"APR": 15, "LockingPeriod": 100}`)

	common.Json(bridgeAPI.GetNetworkInfo(networkClass, chainId)).Equals(t, `
{
	"networkClass": 2,
	"chainId": 123,
	"name": "Ethereum",
	"contractAddress": "0x323b5d4c32345ced77393b3530b1eed0f346429d",
	"metadata": "{}",
	"tokenPairs": [
		{
			"tokenStandard": "zts1znnxxxxxxxxxxxxx9z4ulx",
			"tokenAddress": "0x5fbdb2315678afecb367f032d93f642f64180aa3",
			"bridgeable": true,
			"redeemable": true,
			"owned": false,
			"minAmount": "100",
			"feePercentage": 20,
			"redeemDelay": 25,
			"metadata": "{\"APR\": 25, \"LockingPeriod\": 100}"
		},
		{
			"tokenStandard": "zts1qanamzukd2v0pp8j2wzx6m",
			"tokenAddress": "0x5aaaa2315678afecb367f032d93f642f64180aa3",
			"bridgeable": true,
			"redeemable": true,
			"owned": true,
			"minAmount": "10",
			"feePercentage": 100,
			"redeemDelay": 15,
			"metadata": "{\"APR\": 20, \"LockingPeriod\": 50}"
		},
		{
			"tokenStandard": "zts1qsrxxxxxxxxxxxxxmrhjll",
			"tokenAddress": "0x6bbbb2315678afecb367f032d93f642f64180aa3",
			"bridgeable": true,
			"redeemable": true,
			"owned": false,
			"minAmount": "100",
			"feePercentage": 15,
			"redeemDelay": 20,
			"metadata": "{\"APR\": 15, \"LockingPeriod\": 100}"
		}
	]
}`)

	// Try to remove a tokenPair
	defer z.CallContract(removeTokenPair(g.User5.Address, networkClass, chainId, types.ZnnTokenStandard, "0x5fbdb2315678afecb367f032d93f642f64180aa3")).
		Error(t, nil)
	insertMomentums(z, 2)

	common.Json(bridgeAPI.GetNetworkInfo(networkClass, chainId)).Equals(t, `
{
	"networkClass": 2,
	"chainId": 123,
	"name": "Ethereum",
	"contractAddress": "0x323b5d4c32345ced77393b3530b1eed0f346429d",
	"metadata": "{}",
	"tokenPairs": [
		{
			"tokenStandard": "zts1qanamzukd2v0pp8j2wzx6m",
			"tokenAddress": "0x5aaaa2315678afecb367f032d93f642f64180aa3",
			"bridgeable": true,
			"redeemable": true,
			"owned": true,
			"minAmount": "10",
			"feePercentage": 100,
			"redeemDelay": 15,
			"metadata": "{\"APR\": 20, \"LockingPeriod\": 50}"
		},
		{
			"tokenStandard": "zts1qsrxxxxxxxxxxxxxmrhjll",
			"tokenAddress": "0x6bbbb2315678afecb367f032d93f642f64180aa3",
			"bridgeable": true,
			"redeemable": true,
			"owned": false,
			"minAmount": "100",
			"feePercentage": 15,
			"redeemDelay": 20,
			"metadata": "{\"APR\": 15, \"LockingPeriod\": 100}"
		}
	]
}`)

	// Try to change administrator
	changeAdministrator(t, z, g.User5.Address, g.User5.Address, securityInfo.AdministratorDelay)
	common.Json(bridgeAPI.GetBridgeInfo()).Equals(t, `
{
	"administrator": "z1qqaswvt0e3cc5sm7lygkyza9ra63cr8e6zre09",
	"compressedTssECDSAPubKey": "AsAQx1M3LVXCuozDOqO5b9adj/PItYgwZFG/xTDBiZzT",
	"decompressedTssECDSAPubKey": "BMAQx1M3LVXCuozDOqO5b9adj/PItYgwZFG/xTDBiZzTnQAT1qOPAkuPzu6yoewss9XbnTmZmb9JQNGXmkPYtK4=",
	"allowKeyGen": true,
	"halted": true,
	"unhaltedAt": 0,
	"unhaltDurationInMomentums": 5,
	"tssNonce": 0,
	"metadata": "{}"
}`)

	// Try to setAllowKeyGen
	defer z.CallContract(setAllowKeyGen(g.User5.Address, true)).
		Error(t, constants.ErrBridgeHalted)
	insertMomentums(z, 2)

	// Try to change tss
	//// change with signatures
	bridgeInfo, err = bridgeAPI.GetBridgeInfo()
	common.DealWithErr(err)
	publicKey := "AhOiqdjx002Cj8o1jxTM5LqywbgNFZwUPJuR9ffdQwFP" // priv Sf12dS9DI7xsiKrmQfPR8zQE1HUIYkd8x0XZ6fkAxXo=
	message, err := implementation.GetChangePubKeyMessage(definition.ChangeTssECDSAPubKeyMethodName, definition.NoMClass, z.Chain().ChainIdentifier(), bridgeInfo.TssNonce, publicKey)
	common.DealWithErr(err)

	oldSignature, err := sign(message, "tuSwrTEUyJI1/3y5J8L8DSjzT/AQG2IK3JG+93qhhhI=")
	newSignature, err := sign(message, "Sf12dS9DI7xsiKrmQfPR8zQE1HUIYkd8x0XZ6fkAxXo=")
	defer z.CallContract(changeTssWithSignature(publicKey, oldSignature, newSignature)).Error(t, nil)
	insertMomentums(z, 2)

	common.Json(bridgeAPI.GetBridgeInfo()).Equals(t, `
{
	"administrator": "z1qqaswvt0e3cc5sm7lygkyza9ra63cr8e6zre09",
	"compressedTssECDSAPubKey": "AhOiqdjx002Cj8o1jxTM5LqywbgNFZwUPJuR9ffdQwFP",
	"decompressedTssECDSAPubKey": "BBOiqdjx002Cj8o1jxTM5LqywbgNFZwUPJuR9ffdQwFPC3247CxWsOED9R+qv5RTS/rOxffGZYUln3JXKEIsWSA=",
	"allowKeyGen": false,
	"halted": true,
	"unhaltedAt": 0,
	"unhaltDurationInMomentums": 5,
	"tssNonce": 1,
	"metadata": "{}"
}`)

	//// change with admin
	publicKey = "AsAQx1M3LVXCuozDOqO5b9adj/PItYgwZFG/xTDBiZzT"
	changeTssWithAdministrator(t, z, g.User5.Address, publicKey, securityInfo.SoftDelay)
	common.Json(bridgeAPI.GetBridgeInfo()).Equals(t, `
{
	"administrator": "z1qqaswvt0e3cc5sm7lygkyza9ra63cr8e6zre09",
	"compressedTssECDSAPubKey": "AsAQx1M3LVXCuozDOqO5b9adj/PItYgwZFG/xTDBiZzT",
	"decompressedTssECDSAPubKey": "BMAQx1M3LVXCuozDOqO5b9adj/PItYgwZFG/xTDBiZzTnQAT1qOPAkuPzu6yoewss9XbnTmZmb9JQNGXmkPYtK4=",
	"allowKeyGen": false,
	"halted": true,
	"unhaltedAt": 0,
	"unhaltDurationInMomentums": 5,
	"tssNonce": 1,
	"metadata": "{}"
}`)

	// Try to nominate guardians
	guardians := []types.Address{g.User1.Address, g.User2.Address, g.User3.Address, g.User4.Address, g.User5.Address, g.User6.Address}
	nominateGuardians(t, z, g.User5.Address, guardians, securityInfo.AdministratorDelay)
	common.Json(bridgeAPI.GetSecurityInfo()).Equals(t, `
{
	"guardians": [
		"z1qqaswvt0e3cc5sm7lygkyza9ra63cr8e6zre09",
		"z1qqdt06lnwz57x38rwlyutcx5wgrtl0ynkfe3kv",
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
		"z1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqsggv2f",
		"z1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqsggv2f"
	],
	"administratorDelay": 20,
	"softDelay": 10
}`)

	// Try to set orchestratorInfo
	defer z.CallContract(setOrchestratorInfo(g.User5.Address, 10, 10, 10, 15)).
		Error(t, nil)
	insertMomentums(z, 2)

	common.Json(bridgeAPI.GetOrchestratorInfo()).Equals(t, `
{
	"windowSize": 10,
	"keyGenThreshold": 10,
	"confirmationsToFinality": 10,
	"estimatedMomentumTime": 15,
	"allowKeyGenHeight": 97
}`)

	// Try to propose administrator
	defer z.CallContract(proposeAdministrator(g.User1.Address, g.User6.Address)).Error(t, constants.ErrNotEmergency)
	insertMomentums(z, 2)

	// Try to set bridge metadata
	defer z.CallContract(setBridgeMetadata(g.User5.Address, `{"APYY":15}`)).Error(t, nil)
	insertMomentums(z, 2)

	common.Json(bridgeAPI.GetBridgeInfo()).Equals(t, `
{
	"administrator": "z1qqaswvt0e3cc5sm7lygkyza9ra63cr8e6zre09",
	"compressedTssECDSAPubKey": "AsAQx1M3LVXCuozDOqO5b9adj/PItYgwZFG/xTDBiZzT",
	"decompressedTssECDSAPubKey": "BMAQx1M3LVXCuozDOqO5b9adj/PItYgwZFG/xTDBiZzTnQAT1qOPAkuPzu6yoewss9XbnTmZmb9JQNGXmkPYtK4=",
	"allowKeyGen": false,
	"halted": true,
	"unhaltedAt": 0,
	"unhaltDurationInMomentums": 5,
	"tssNonce": 1,
	"metadata": "{\"APYY\":15}"
}`)

	// Try to halt
	defer z.CallContract(haltWithAdmin(g.User5.Address)).Error(t, constants.ErrBridgeHalted)
	insertMomentums(z, 2)

	bridgeInfo, err = bridgeAPI.GetBridgeInfo()
	common.DealWithErr(err)
	defer z.CallContract(haltWithSignature(getHaltSignature(t, z, bridgeInfo.TssNonce))).Error(t, constants.ErrBridgeHalted)
	insertMomentums(z, 2)

	// Try to unhalt
	defer z.CallContract(unhalt(g.User5.Address)).Error(t, nil)
	insertMomentums(z, 2)

	frMom, err = z.Chain().GetFrontierMomentumStore().GetFrontierMomentum()
	common.DealWithErr(err)
	z.InsertMomentumsTo(frMom.Height + bridgeInfo.UnhaltDurationInMomentums + 1)

	// Try to activate emergency again
	// We also test unhalt when the bridge is not halted
	defer z.CallContract(unhalt(g.User5.Address)).Error(t, constants.ErrBridgeNotHalted)
	insertMomentums(z, 2)

	// We halt again
	defer z.CallContract(haltWithAdmin(g.User5.Address)).Error(t, nil)
	insertMomentums(z, 2)

	defer z.CallContract(activateEmergency(g.User5.Address)).Error(t, nil)
	insertMomentums(z, 2)
}

func TestBridge_WrapToken(t *testing.T) {
	z := mock.NewMockZenonWithCustomEpochDuration(t, time.Hour)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).Equals(t, `
t=2001-09-09T01:46:50+0000 lvl=dbug msg=created module=embedded contract=spork spork="&{Id:c6a597f757168bd5c9fddf52b16b3bf38e2ef781fb8edeea1bf2ae0d3225230d Name:spork-bridge Description:activate spork for bridge Activated:false EnforcementHeight:0}"
t=2001-09-09T01:47:00+0000 lvl=dbug msg=activated module=embedded contract=spork spork="&{Id:c6a597f757168bd5c9fddf52b16b3bf38e2ef781fb8edeea1bf2ae0d3225230d Name:spork-bridge Description:activate spork for bridge Activated:true EnforcementHeight:9}"
`)

	// We just have the spork and orchestratorInfo
	activateBridgeStep1(t, z)

	bridgeAPI := embedded.NewBridgeApi(z)
	securityInfo, err := bridgeAPI.GetSecurityInfo()
	common.DealWithErr(err)

	// Add a network
	networkClass := uint32(2) // evm
	chainId := uint32(123)
	defer z.CallContract(addNetwork(g.User5.Address, networkClass, chainId, "Ethereum", "0x323b5d4c32345ced77393b3530b1eed0f346429d", "{}")).
		Error(t, nil)
	insertMomentums(z, 2)

	// Znn - not owned
	feePercentage := uint32(15)
	setTokenPair(t, z, g.User5.Address, securityInfo.SoftDelay, networkClass, chainId, types.ZnnTokenStandard, "0x5fbdb2315678afecb367f032d93f642f64180aa3", true, true, false,
		big.NewInt(100), feePercentage, uint32(20), `{"APR": 15, "LockingPeriod": 100}`)

	// Try to wrap with no tss and guardians
	defer z.CallContract(wrapToken(types.ZnnTokenStandard, big.NewInt(15*g.Zexp), networkClass, chainId, "0xb794f5ea0ba39494ce839613fffba74279579268")).
		Error(t, constants.ErrBridgeNotInitialized)
	insertMomentums(z, 2)

	// nominate Guardians
	guardians := []types.Address{g.User1.Address, g.User2.Address, g.User3.Address, g.User4.Address, g.User5.Address}
	nominateGuardians(t, z, g.User5.Address, guardians, securityInfo.AdministratorDelay)

	// Try to wrap with no tss
	defer z.CallContract(wrapToken(types.ZnnTokenStandard, big.NewInt(15*g.Zexp), networkClass, chainId, "0xb794f5ea0ba39494ce839613fffba74279579268")).
		Error(t, constants.ErrBridgeNotInitialized)
	insertMomentums(z, 2)

	// Add tss pub key
	tssPubKey := "AsAQx1M3LVXCuozDOqO5b9adj/PItYgwZFG/xTDBiZzT" // priv tuSwrTEUyJI1/3y5J8L8DSjzT/AQG2IK3JG+93qhhhI=
	changeTssWithAdministrator(t, z, g.User5.Address, tssPubKey, securityInfo.SoftDelay)

	// Wrapping should pass
	z.ExpectBalance(types.BridgeContract, types.ZnnTokenStandard, 0)
	z.ExpectBalance(g.User1.Address, types.ZnnTokenStandard, 1197000000000)
	feeTokenPair, err := bridgeAPI.GetFeeTokenPair(types.ZnnTokenStandard)
	common.DealWithErr(err)
	common.Json(feeTokenPair, err).Equals(t, `
{
	"tokenStandard": "zts1znnxxxxxxxxxxxxx9z4ulx",
	"accumulatedFee": "0"
}`)
	amount := big.NewInt(15 * g.Zexp)
	defer z.CallContract(wrapToken(types.ZnnTokenStandard, amount, networkClass, chainId, "0xb794f5ea0ba39494ce839613fffba74279579268")).
		Error(t, nil)
	insertMomentums(z, 2)
	feeTokenPair, err = bridgeAPI.GetFeeTokenPair(types.ZnnTokenStandard)
	common.DealWithErr(err)
	fee := big.NewInt(int64(feePercentage))
	amount = amount.Mul(amount, fee)
	fee = amount.Div(amount, big.NewInt(int64(constants.MaximumFee)))
	// Fees should be equal
	common.String(feeTokenPair.AccumulatedFee.String()).Equals(t, fee.String())
	z.ExpectBalance(types.BridgeContract, types.ZnnTokenStandard, 15*g.Zexp)
	z.ExpectBalance(g.User1.Address, types.ZnnTokenStandard, 1195500000000)

	// We remove the network and try to wrap again
	defer z.CallContract(removeNetwork(g.User5.Address, networkClass, chainId)).Error(t, nil)
	insertMomentums(z, 2)
	defer z.CallContract(wrapToken(types.ZnnTokenStandard, big.NewInt(15*g.Zexp), networkClass, chainId, "0xb794f5ea0ba39494ce839613fffba74279579268")).
		Error(t, constants.ErrUnknownNetwork)
	insertMomentums(z, 2)

	// We add the network and try again with no token pair set
	defer z.CallContract(addNetwork(g.User5.Address, networkClass, chainId, "Ethereum", "0x323b5d4c32345ced77393b3530b1eed0f346429d", "{}")).
		Error(t, nil)
	insertMomentums(z, 2)
	defer z.CallContract(wrapToken(types.ZnnTokenStandard, big.NewInt(15*g.Zexp), networkClass, chainId, "0xb794f5ea0ba39494ce839613fffba74279579268")).
		Error(t, constants.ErrTokenNotFound)
	insertMomentums(z, 2)

	// We add the token pair
	setTokenPair(t, z, g.User5.Address, securityInfo.SoftDelay, networkClass, chainId, types.ZnnTokenStandard, "0x5fbdb2315678afecb367f032d93f642f64180aa3", true, true, false,
		big.NewInt(100), feePercentage, uint32(20), `{"APR": 15, "LockingPeriod": 100}`)

	// wrap with 0 amount - should fail on send block
	z.InsertSendBlock(wrapToken(types.ZnnTokenStandard, big.NewInt(0), networkClass, chainId, "0xb794f5ea0ba39494ce839613fffba74279579268"),
		constants.ErrInvalidTokenOrAmount, mock.SkipVmChanges)
	insertMomentums(z, 2)

	defer z.CallContract(wrapToken(types.ZnnTokenStandard, big.NewInt(15*g.Zexp), networkClass, chainId, "0xb794f5ea0ba39494ce839613fffba74279579268")).
		Error(t, nil)
	insertMomentums(z, 2)

	// Change the tokenAddress - we should only have one tokenPair
	setTokenPair(t, z, g.User5.Address, securityInfo.SoftDelay, networkClass, chainId, types.ZnnTokenStandard, "0x6fbdb2315678afecb367f032d93f642f64180aa3", true, true, false,
		big.NewInt(100), uint32(15), uint32(20), `{"APR": 15, "LockingPeriod": 100}`)
	common.Json(bridgeAPI.GetNetworkInfo(networkClass, chainId)).Equals(t, `
{
	"networkClass": 2,
	"chainId": 123,
	"name": "Ethereum",
	"contractAddress": "0x323b5d4c32345ced77393b3530b1eed0f346429d",
	"metadata": "{}",
	"tokenPairs": [
		{
			"tokenStandard": "zts1znnxxxxxxxxxxxxx9z4ulx",
			"tokenAddress": "0x6fbdb2315678afecb367f032d93f642f64180aa3",
			"bridgeable": true,
			"redeemable": true,
			"owned": false,
			"minAmount": "100",
			"feePercentage": 15,
			"redeemDelay": 20,
			"metadata": "{\"APR\": 15, \"LockingPeriod\": 100}"
		}
	]
}`)
	// Wrap should work again
	defer z.CallContract(wrapToken(types.ZnnTokenStandard, big.NewInt(15*g.Zexp), networkClass, chainId, "0xb794f5ea0ba39494ce839613fffba74279579268")).
		Error(t, nil)
	insertMomentums(z, 2)
	// contract should have the balance of 3 wraps
	z.ExpectBalance(types.BridgeContract, types.ZnnTokenStandard, 45*g.Zexp)

	// Wrap token with amount less than minAmount
	defer z.CallContract(wrapToken(types.ZnnTokenStandard, big.NewInt(10), networkClass, chainId, "0xb794f5ea0ba39494ce839613fffba74279579268")).
		Error(t, constants.ErrInvalidMinAmount)
	insertMomentums(z, 2)
	// Set the token as not bridgable
	setTokenPair(t, z, g.User5.Address, securityInfo.SoftDelay, networkClass, chainId, types.ZnnTokenStandard, "0x6fbdb2315678afecb367f032d93f642f64180aa3", false, true, false,
		big.NewInt(100), uint32(15), uint32(20), `{"APR": 15, "LockingPeriod": 100}`)
	defer z.CallContract(wrapToken(types.ZnnTokenStandard, big.NewInt(10), networkClass, chainId, "0xb794f5ea0ba39494ce839613fffba74279579268")).Error(t,
		constants.ErrTokenNotBridgeable)
	insertMomentums(z, 2)

	// Set token fee 0%
	setTokenPair(t, z, g.User5.Address, securityInfo.SoftDelay, networkClass, chainId, types.ZnnTokenStandard, "0x6fbdb2315678afecb367f032d93f642f64180aa3", true, true, false,
		big.NewInt(100), uint32(0), uint32(20), `{"APR": 15, "LockingPeriod": 100}`)
	common.Json(bridgeAPI.GetFeeTokenPair(types.ZnnTokenStandard)).Equals(t, `
{
	"tokenStandard": "zts1znnxxxxxxxxxxxxx9z4ulx",
	"accumulatedFee": "6750000"
}`)
	defer z.CallContract(wrapToken(types.ZnnTokenStandard, big.NewInt(10*g.Zexp), networkClass, chainId, "0xb794f5ea0ba39494ce839613fffba74279579268")).Error(t,
		nil)
	insertMomentums(z, 2)

	// fees should stay the same
	common.Json(bridgeAPI.GetFeeTokenPair(types.ZnnTokenStandard)).Equals(t, `
{
	"tokenStandard": "zts1znnxxxxxxxxxxxxx9z4ulx",
	"accumulatedFee": "6750000"
}`)

	// Set token fee 100%
	setTokenPair(t, z, g.User5.Address, securityInfo.SoftDelay, networkClass, chainId, types.ZnnTokenStandard, "0x6fbdb2315678afecb367f032d93f642f64180aa3", true, true, false,
		big.NewInt(100), constants.MaximumFee, uint32(20), `{"APR": 15, "LockingPeriod": 100}`)
	common.Json(bridgeAPI.GetFeeTokenPair(types.ZnnTokenStandard)).Equals(t, `
{
	"tokenStandard": "zts1znnxxxxxxxxxxxxx9z4ulx",
	"accumulatedFee": "6750000"
}`)
	defer z.CallContract(wrapToken(types.ZnnTokenStandard, big.NewInt(1*g.Zexp), networkClass, chainId, "0xb794f5ea0ba39494ce839613fffba74279579268")).Error(t,
		nil)
	insertMomentums(z, 2)

	// fees should be bigger with 1*1e8
	common.Json(bridgeAPI.GetFeeTokenPair(types.ZnnTokenStandard)).Equals(t, `
{
	"tokenStandard": "zts1znnxxxxxxxxxxxxx9z4ulx",
	"accumulatedFee": "106750000"
}`)

	// Set token minAmount to 1 so when we wrap the calculated fee value is 0
	feePercentage = 1000 // 10%
	setTokenPair(t, z, g.User5.Address, securityInfo.SoftDelay, networkClass, chainId, types.ZnnTokenStandard, "0x6fbdb2315678afecb367f032d93f642f64180aa3", true, true, false,
		big.NewInt(1), feePercentage, uint32(20), `{"APR": 15, "LockingPeriod": 100}`)

	defer z.CallContract(wrapToken(types.ZnnTokenStandard, big.NewInt(9), networkClass, chainId, "0xb794f5ea0ba39494ce839613fffba74279579268")).
		Error(t, nil)
	insertMomentums(z, 2)

	// fees accumulated should be the same
	common.Json(bridgeAPI.GetFeeTokenPair(types.ZnnTokenStandard)).Equals(t, `
{
	"tokenStandard": "zts1znnxxxxxxxxxxxxx9z4ulx",
	"accumulatedFee": "106750000"
}`)

	// wrapping 10 should increase fees with 1
	defer z.CallContract(wrapToken(types.ZnnTokenStandard, big.NewInt(10), networkClass, chainId, "0xb794f5ea0ba39494ce839613fffba74279579268")).
		Error(t, nil)
	insertMomentums(z, 2)

	// fees accumulated should be the same
	common.Json(bridgeAPI.GetFeeTokenPair(types.ZnnTokenStandard)).Equals(t, `
{
	"tokenStandard": "zts1znnxxxxxxxxxxxxx9z4ulx",
	"accumulatedFee": "106750001"
}`)
}

func TestBridge_UpdateWrapToken(t *testing.T) {
	z := mock.NewMockZenonWithCustomEpochDuration(t, time.Hour)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).Equals(t, `
t=2001-09-09T01:46:50+0000 lvl=dbug msg=created module=embedded contract=spork spork="&{Id:c6a597f757168bd5c9fddf52b16b3bf38e2ef781fb8edeea1bf2ae0d3225230d Name:spork-bridge Description:activate spork for bridge Activated:false EnforcementHeight:0}"
t=2001-09-09T01:47:00+0000 lvl=dbug msg=activated module=embedded contract=spork spork="&{Id:c6a597f757168bd5c9fddf52b16b3bf38e2ef781fb8edeea1bf2ae0d3225230d Name:spork-bridge Description:activate spork for bridge Activated:true EnforcementHeight:9}"
`)

	// We just have the spork
	activateBridgeStep0(t, z)

	bridgeAPI := embedded.NewBridgeApi(z)

	networkClass := uint32(2)
	chainId := uint32(123)

	defer z.CallContract(addNetwork(g.User5.Address, networkClass, chainId, "Ethereum", "0x323b5d4c32345ced77393b3530b1eed0f346429d", "{}")).
		Error(t, nil)
	insertMomentums(z, 2)
	common.Json(bridgeAPI.GetNetworkInfo(networkClass, chainId)).Equals(t, `
{
	"networkClass": 2,
	"chainId": 123,
	"name": "Ethereum",
	"contractAddress": "0x323b5d4c32345ced77393b3530b1eed0f346429d",
	"metadata": "{}",
	"tokenPairs": []
}`)

	// create a non existing wrap request
	tokenAddress := "0x5fbdb2315678afecb367f032d93f642f64180aa3"
	request := &embedded.WrapTokenRequest{
		WrapTokenRequest: &definition.WrapTokenRequest{
			NetworkClass:           networkClass,
			ChainId:                chainId,
			Id:                     types.HexToHashPanic("0123456789012345678901234567890123456789012345678901234567890123"),
			ToAddress:              "0x323b5d4c32345ced77393b3530b1eed0f346429d",
			TokenStandard:          types.ZnnTokenStandard,
			TokenAddress:           tokenAddress,
			Amount:                 big.NewInt(100),
			Fee:                    big.NewInt(1),
			Signature:              "",
			CreationMomentumHeight: 500,
		},
	}
	contractAddress := ecommon.HexToAddress("0x323b5d4c32345ced77393b3530b1eed0f346429d")
	signature := getUpdateWrapTokenSignature(request, contractAddress, "tuSwrTEUyJI1/3y5J8L8DSjzT/AQG2IK3JG+93qhhhI=")
	defer z.CallContract(updateWrapToken(request.Id, signature)).Error(t, constants.ErrBridgeNotInitialized)
	insertMomentums(z, 2)

	// Set orchestratorInfo
	defer z.CallContract(setOrchestratorInfo(g.User5.Address, 6, 3, 15, 10)).
		Error(t, nil)
	insertMomentums(z, 2)
	common.Json(bridgeAPI.GetOrchestratorInfo()).Equals(t, `
{
	"windowSize": 6,
	"keyGenThreshold": 3,
	"confirmationsToFinality": 15,
	"estimatedMomentumTime": 10,
	"allowKeyGenHeight": 0
}`)

	defer z.CallContract(updateWrapToken(request.Id, signature)).Error(t, constants.ErrBridgeNotInitialized)
	insertMomentums(z, 2)

	// Set guardians
	securityInfo, err := bridgeAPI.GetSecurityInfo()
	common.FailIfErr(t, err)
	guardians := []types.Address{g.User1.Address, g.User2.Address, g.User3.Address, g.User4.Address, g.User5.Address}
	nominateGuardians(t, z, g.User5.Address, guardians, securityInfo.AdministratorDelay)

	// Try to unwrap with orcInfo and guardians set
	defer z.CallContract(updateWrapToken(request.Id, signature)).Error(t, constants.ErrBridgeNotInitialized)
	insertMomentums(z, 2)

	// set tss
	tssPubKey := "AsAQx1M3LVXCuozDOqO5b9adj/PItYgwZFG/xTDBiZzT"
	changeTssWithAdministrator(t, z, g.User5.Address, tssPubKey, securityInfo.SoftDelay)

	// Try to unwrap with orcInfo, guardians and tss set
	defer z.CallContract(updateWrapToken(request.Id, signature)).Error(t, constants.ErrDataNonExistent)
	insertMomentums(z, 2)

	// Set token pair
	setTokenPair(t, z, g.User5.Address, securityInfo.SoftDelay, networkClass, chainId, types.ZnnTokenStandard, tokenAddress, true, true, false,
		big.NewInt(100), uint32(15), uint32(20), `{"APR": 15, "LockingPeriod": 100}`)

	// Try to unwrap with all set but non existing wrap
	defer z.CallContract(updateWrapToken(request.Id, signature)).Error(t, constants.ErrDataNonExistent)
	insertMomentums(z, 2)

	defer z.CallContract(wrapToken(types.ZnnTokenStandard, big.NewInt(15*g.Zexp), networkClass, chainId, "0xb794f5ea0ba39494ce839613fffba74279579268")).
		Error(t, nil)
	insertMomentums(z, 2)

	// remove token pair
	defer z.CallContract(removeTokenPair(g.User5.Address, networkClass, chainId, types.ZnnTokenStandard, "0x5fbdb2315678afecb367f032d93f642f64180aa3")).
		Error(t, nil)
	insertMomentums(z, 2)

	wrapRequests, err := bridgeAPI.GetAllWrapTokenRequests(0, 5)
	common.DealWithErr(err)

	// Try to update with non existing pair
	defer z.CallContract(updateWrapToken(wrapRequests.List[0].Id, signature)).Error(t, constants.ErrInvalidToken)
	insertMomentums(z, 2)

	// also remove the network
	defer z.CallContract(removeNetwork(g.User5.Address, networkClass, chainId)).Error(t, nil)
	insertMomentums(z, 2)

	// Try to update with non existing network
	defer z.CallContract(updateWrapToken(wrapRequests.List[0].Id, signature)).Error(t, constants.ErrUnknownNetwork)
	insertMomentums(z, 2)

	// Add network and pair
	defer z.CallContract(addNetwork(g.User5.Address, networkClass, chainId, "Ethereum", "0x323b5d4c32345ced77393b3530b1eed0f346429d", "{}")).
		Error(t, nil)
	insertMomentums(z, 2)
	// Set token pair
	setTokenPair(t, z, g.User5.Address, securityInfo.SoftDelay, networkClass, chainId, types.ZnnTokenStandard, tokenAddress, true, true, false,
		big.NewInt(100), uint32(15), uint32(20), `{"APR": 15, "LockingPeriod": 100}`)

	// Update should work
	signature = getUpdateWrapTokenSignature(wrapRequests.List[0], contractAddress, "tuSwrTEUyJI1/3y5J8L8DSjzT/AQG2IK3JG+93qhhhI=")
	defer z.CallContract(updateWrapToken(wrapRequests.List[0].Id, signature)).Error(t, nil)
	insertMomentums(z, 2)

	common.Json(bridgeAPI.GetAllWrapTokenRequests(0, 5)).Equals(t, `
{
	"count": 1,
	"list": [
		{
			"networkClass": 2,
			"chainId": 123,
			"id": "c1660dcc90ca6f94586dbb107fc5fa3c54a6093b394f159f2f619d3424d427f8",
			"toAddress": "0xb794f5ea0ba39494ce839613fffba74279579268",
			"tokenStandard": "zts1znnxxxxxxxxxxxxx9z4ulx",
			"tokenAddress": "0x5fbdb2315678afecb367f032d93f642f64180aa3",
			"amount": "1500000000",
			"fee": "2250000",
			"signature": "BeQGkmasEzOP7W9+Z+2+nUspA+LGw5kw/WJKFlL7H346PeehfbCNxxHuToXTMzrieCRze/WKIjejPUp1esQ+NgA=",
			"creationMomentumHeight": 81,
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
			"confirmationsToFinality": 0
		}
	]
}`)

	// Change tss to re update the signature
	tssPubKey = "AhOiqdjx002Cj8o1jxTM5LqywbgNFZwUPJuR9ffdQwFP" // priv Sf12dS9DI7xsiKrmQfPR8zQE1HUIYkd8x0XZ6fkAxXo=
	changeTssWithAdministrator(t, z, g.User5.Address, tssPubKey, securityInfo.SoftDelay)

	// Update should work
	signature = getUpdateWrapTokenSignature(wrapRequests.List[0], contractAddress, "Sf12dS9DI7xsiKrmQfPR8zQE1HUIYkd8x0XZ6fkAxXo=")
	defer z.CallContract(updateWrapToken(wrapRequests.List[0].Id, signature)).Error(t, nil)
	insertMomentums(z, 2)

	// try to change with back with wrong signature
	signature = getUpdateWrapTokenSignature(wrapRequests.List[0], contractAddress, "tuSwrTEUyJI1/3y5J8L8DSjzT/AQG2IK3JG+93qhhhI=")
	defer z.CallContract(updateWrapToken(wrapRequests.List[0].Id, signature)).Error(t, constants.ErrInvalidECDSASignature)
	insertMomentums(z, 2)
}

func TestBridge_UnwrapToken(t *testing.T) {
	z := mock.NewMockZenonWithCustomEpochDuration(t, time.Hour)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).Equals(t, `
t=2001-09-09T01:46:50+0000 lvl=dbug msg=created module=embedded contract=spork spork="&{Id:c6a597f757168bd5c9fddf52b16b3bf38e2ef781fb8edeea1bf2ae0d3225230d Name:spork-bridge Description:activate spork for bridge Activated:false EnforcementHeight:0}"
t=2001-09-09T01:47:00+0000 lvl=dbug msg=activated module=embedded contract=spork spork="&{Id:c6a597f757168bd5c9fddf52b16b3bf38e2ef781fb8edeea1bf2ae0d3225230d Name:spork-bridge Description:activate spork for bridge Activated:true EnforcementHeight:9}"
t=2001-09-09T02:04:20+0000 lvl=eror msg=Unwrap-ErrInvalidSignature module=embedded contract=bridge error="invalid secp256k1 signature" result=false signature="R71BUZYHOXKHlnMxPebyG5wfk4Xj9G3MN+Wq/9cTMZ1iOO8t60vwrWgiJfKyVnhkGQJENOJeXbFB/At+y6HOgwE="
t=2001-09-09T02:04:40+0000 lvl=eror msg=Unwrap-ErrInvalidSignature module=embedded contract=bridge error="invalid secp256k1 signature" result=false signature="IlCKBBhIC37fa1ntisYOBz3RV+t58n0CDNidzSuJkvA8cK6e3laUCTjHzMbmsahNgjIZei56MoplB8jwOQbHDQA="
t=2001-09-09T02:05:00+0000 lvl=eror msg=Unwrap-ErrInvalidSignature module=embedded contract=bridge error="invalid secp256k1 signature" result=false signature="8MNGAOAMeT/T7yTcgPoY1kLN4KHSD2eFopPgco0SkVMKY6y7+YVk4jKelYNY6cyDPMj2Kj9XamVOeCyj9oMI1AA="
`)

	// We just have the spork and orchestratorInfo
	activateBridgeStep1(t, z)

	bridgeAPI := embedded.NewBridgeApi(z)
	securityInfo, err := bridgeAPI.GetSecurityInfo()
	common.DealWithErr(err)

	// Add a network
	networkClass := uint32(2) // evm
	chainId := uint32(123)
	defer z.CallContract(addNetwork(g.User5.Address, networkClass, chainId, "Ethereum", "0x323b5d4c32345ced77393b3530b1eed0f346429d", "{}")).
		Error(t, nil)
	insertMomentums(z, 2)

	tokenAddress := "0x5fbdb2315678afecb367f032d93f642f64180aa3"
	setTokenPair(t, z, g.User5.Address, securityInfo.SoftDelay, networkClass, chainId, types.ZnnTokenStandard, tokenAddress, true, true, false,
		big.NewInt(100), uint32(15), uint32(20), `{"APR": 15, "LockingPeriod": 100}`)

	// Try to unwrap with no tss and guardians
	hash := types.HexToHashPanic("0123456789012345678901234567890123456789012345678901234567890123")
	signature := getUnwrapTokenSignature(t, networkClass, chainId, hash, 200, tokenAddress, big.NewInt(100*g.Zexp), networkClass)
	defer z.CallContract(unwrapToken(networkClass, chainId, hash, 200, tokenAddress, big.NewInt(100*g.Zexp), signature)).
		Error(t, constants.ErrBridgeNotInitialized)
	insertMomentums(z, 2)

	// nominate Guardians
	guardians := []types.Address{g.User1.Address, g.User2.Address, g.User3.Address, g.User4.Address, g.User5.Address}
	nominateGuardians(t, z, g.User5.Address, guardians, securityInfo.AdministratorDelay)

	// Try to unwrap with no tss
	defer z.CallContract(unwrapToken(networkClass, chainId, hash, 200, tokenAddress, big.NewInt(100*g.Zexp), signature)).
		Error(t, constants.ErrBridgeNotInitialized)
	insertMomentums(z, 2)

	// Add tss pub key
	tssPubKey := "AsAQx1M3LVXCuozDOqO5b9adj/PItYgwZFG/xTDBiZzT" // priv tuSwrTEUyJI1/3y5J8L8DSjzT/AQG2IK3JG+93qhhhI=
	changeTssWithAdministrator(t, z, g.User5.Address, tssPubKey, securityInfo.SoftDelay)

	defer z.CallContract(unwrapToken(networkClass, chainId, hash, 200, tokenAddress, big.NewInt(100*g.Zexp), signature)).
		Error(t, nil)
	insertMomentums(z, 2)
	common.Json(bridgeAPI.GetAllUnwrapTokenRequests(0, 2)).Equals(t, `
{
	"count": 1,
	"list": [
		{
			"registrationMomentumHeight": 75,
			"networkClass": 2,
			"chainId": 123,
			"transactionHash": "0123456789012345678901234567890123456789012345678901234567890123",
			"logIndex": 200,
			"toAddress": "z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx",
			"tokenAddress": "0x5fbdb2315678afecb367f032d93f642f64180aa3",
			"tokenStandard": "zts1znnxxxxxxxxxxxxx9z4ulx",
			"amount": "10000000000",
			"signature": "YXFeAH2BIKnbHm7qd1zXt6SSReI9ovhYg5jVPGumNOkj3Bw6OfUwSZSCKSMPCw4NRZGDXT6m7Axe5UIwAno4xQE=",
			"redeemed": 0,
			"revoked": 0,
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
			"redeemableIn": 19
		}
	]
}`)

	// Try to unwrap without network
	hash = types.HexToHashPanic("0023456789012345678901234567890123456789012345678901234567890123")
	defer z.CallContract(removeNetwork(g.User5.Address, networkClass, chainId)).Error(t, nil)
	insertMomentums(z, 2)

	signature = getUnwrapTokenSignature(t, networkClass, chainId, hash, 250, tokenAddress, big.NewInt(100*g.Zexp), networkClass)
	defer z.CallContract(unwrapToken(networkClass, chainId, hash, 250, tokenAddress, big.NewInt(100*g.Zexp), signature)).
		Error(t, constants.ErrUnknownNetwork)
	insertMomentums(z, 2)

	// Try to unwrap without tokenPair
	defer z.CallContract(addNetwork(g.User5.Address, networkClass, chainId, "Ethereum", "0x323b5d4c32345ced77393b3530b1eed0f346429d", "{}")).
		Error(t, nil)
	insertMomentums(z, 2)
	defer z.CallContract(unwrapToken(networkClass, chainId, hash, 250, tokenAddress, big.NewInt(100*g.Zexp), signature)).
		Error(t, constants.ErrTokenNotFound)
	insertMomentums(z, 2)

	setTokenPair(t, z, g.User5.Address, securityInfo.SoftDelay, networkClass, chainId, types.ZnnTokenStandard, tokenAddress, true, true, false,
		big.NewInt(100), uint32(15), uint32(20), `{"APR": 15, "LockingPeriod": 100}`)

	// unwrap with chain id 0
	signature = getUnwrapTokenSignature(t, networkClass, 0, hash, 250, tokenAddress, big.NewInt(100*g.Zexp), networkClass)
	defer z.CallContract(unwrapToken(networkClass, 0, hash, 250, tokenAddress, big.NewInt(100*g.Zexp), signature)).
		Error(t, constants.ErrUnknownNetwork)
	insertMomentums(z, 2)

	// unwrap with network class 0
	signature = getUnwrapTokenSignature(t, chainId, 0, hash, 250, tokenAddress, big.NewInt(100*g.Zexp), networkClass)
	defer z.CallContract(unwrapToken(0, chainId, hash, 250, tokenAddress, big.NewInt(100*g.Zexp), signature)).
		Error(t, constants.ErrUnknownNetwork)
	insertMomentums(z, 2)

	// unwrap with class and chain id 0
	signature = getUnwrapTokenSignature(t, 0, 0, hash, 250, tokenAddress, big.NewInt(100*g.Zexp), networkClass)
	defer z.CallContract(unwrapToken(0, 0, hash, 250, tokenAddress, big.NewInt(100*g.Zexp), signature)).
		Error(t, constants.ErrUnknownNetwork)
	insertMomentums(z, 2)

	// unwrap with amount 0
	z.InsertSendBlock(unwrapToken(networkClass, chainId, hash, 250, tokenAddress, big.NewInt(0), signature),
		constants.ErrInvalidTokenOrAmount, mock.SkipVmChanges)
	insertMomentums(z, 2)

	// wrong signatures
	hash = types.HexToHashPanic("0003456789012345678901234567890123456789012345678901234567890123")
	signature = getUnwrapTokenSignature(t, networkClass, chainId, hash, 250, tokenAddress, big.NewInt(100*g.Zexp), networkClass)
	defer z.CallContract(unwrapToken(networkClass, chainId, hash, 300, tokenAddress, big.NewInt(100*g.Zexp), signature)).
		Error(t, constants.ErrInvalidECDSASignature)
	insertMomentums(z, 2)

	signature = getUnwrapTokenSignature(t, networkClass, chainId, hash, 300, tokenAddress, big.NewInt(200*g.Zexp), networkClass)
	defer z.CallContract(unwrapToken(networkClass, chainId, hash, 300, tokenAddress, big.NewInt(100*g.Zexp), signature)).
		Error(t, constants.ErrInvalidECDSASignature)
	insertMomentums(z, 2)

	newTokenAddress := "0xffbdb2315678afecb367f032d93f642f64180aa3"
	signature = getUnwrapTokenSignature(t, networkClass, chainId, hash, 300, newTokenAddress, big.NewInt(100*g.Zexp), networkClass)
	defer z.CallContract(unwrapToken(networkClass, chainId, hash, 300, tokenAddress, big.NewInt(100*g.Zexp), signature)).
		Error(t, constants.ErrInvalidECDSASignature)
	insertMomentums(z, 2)

	// unwrap with token not redeemable
	setTokenPair(t, z, g.User5.Address, securityInfo.SoftDelay, networkClass, chainId, types.ZnnTokenStandard, tokenAddress, true, false, false,
		big.NewInt(100), uint32(15), uint32(20), `{"APR": 15, "LockingPeriod": 100}`)

	signature = getUnwrapTokenSignature(t, networkClass, chainId, hash, 300, tokenAddress, big.NewInt(100*g.Zexp), networkClass)
	defer z.CallContract(unwrapToken(networkClass, chainId, hash, 300, tokenAddress, big.NewInt(100*g.Zexp), signature)).
		Error(t, constants.ErrTokenNotRedeemable)
	insertMomentums(z, 2)

	/// Test revoke

	// Revoke unwrap token
	requests, err := bridgeAPI.GetAllUnwrapTokenRequests(0, 5)
	common.FailIfErr(t, err)

	// revoke as non admin
	defer z.CallContract(revokeUnwrap(g.User4.Address, requests.List[0].TransactionHash, requests.List[0].LogIndex)).
		Error(t, constants.ErrPermissionDenied)
	insertMomentums(z, 2)

	// revoke non existing unwrap
	defer z.CallContract(revokeUnwrap(g.User5.Address, requests.List[0].TransactionHash, requests.List[0].LogIndex+5)).
		Error(t, constants.ErrDataNonExistent)
	insertMomentums(z, 2)

	newHash := types.NewHash(requests.List[0].TransactionHash.Bytes())
	newHash[0] = 0
	defer z.CallContract(revokeUnwrap(g.User5.Address, newHash, requests.List[0].LogIndex)).
		Error(t, constants.ErrDataNonExistent)
	insertMomentums(z, 2)

	common.Json(bridgeAPI.GetAllUnwrapTokenRequests(0, 5)).Equals(t, `
{
	"count": 1,
	"list": [
		{
			"registrationMomentumHeight": 75,
			"networkClass": 2,
			"chainId": 123,
			"transactionHash": "0123456789012345678901234567890123456789012345678901234567890123",
			"logIndex": 200,
			"toAddress": "z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx",
			"tokenAddress": "0x5fbdb2315678afecb367f032d93f642f64180aa3",
			"tokenStandard": "zts1znnxxxxxxxxxxxxx9z4ulx",
			"amount": "10000000000",
			"signature": "YXFeAH2BIKnbHm7qd1zXt6SSReI9ovhYg5jVPGumNOkj3Bw6OfUwSZSCKSMPCw4NRZGDXT6m7Axe5UIwAno4xQE=",
			"redeemed": 0,
			"revoked": 0,
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
			"redeemableIn": 0
		}
	]
}`)
	defer z.CallContract(revokeUnwrap(g.User5.Address, requests.List[0].TransactionHash, requests.List[0].LogIndex)).
		Error(t, nil)
	insertMomentums(z, 2)
	common.Json(bridgeAPI.GetAllUnwrapTokenRequests(0, 5)).Equals(t, `
{
	"count": 1,
	"list": [
		{
			"registrationMomentumHeight": 75,
			"networkClass": 2,
			"chainId": 123,
			"transactionHash": "0123456789012345678901234567890123456789012345678901234567890123",
			"logIndex": 200,
			"toAddress": "z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx",
			"tokenAddress": "0x5fbdb2315678afecb367f032d93f642f64180aa3",
			"tokenStandard": "zts1znnxxxxxxxxxxxxx9z4ulx",
			"amount": "10000000000",
			"signature": "YXFeAH2BIKnbHm7qd1zXt6SSReI9ovhYg5jVPGumNOkj3Bw6OfUwSZSCKSMPCw4NRZGDXT6m7Axe5UIwAno4xQE=",
			"redeemed": 0,
			"revoked": 1,
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
			"redeemableIn": 0
		}
	]
}`)
}

func TestBridge_Redeem(t *testing.T) {
	z := mock.NewMockZenonWithCustomEpochDuration(t, time.Hour)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).Equals(t, `
t=2001-09-09T01:46:50+0000 lvl=dbug msg=created module=embedded contract=spork spork="&{Id:c6a597f757168bd5c9fddf52b16b3bf38e2ef781fb8edeea1bf2ae0d3225230d Name:spork-bridge Description:activate spork for bridge Activated:false EnforcementHeight:0}"
t=2001-09-09T01:47:00+0000 lvl=dbug msg=activated module=embedded contract=spork spork="&{Id:c6a597f757168bd5c9fddf52b16b3bf38e2ef781fb8edeea1bf2ae0d3225230d Name:spork-bridge Description:activate spork for bridge Activated:true EnforcementHeight:9}"
t=2001-09-09T01:58:20+0000 lvl=dbug msg="issued ZTS" module=embedded contract=token token="{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenName:test.tok3n_na-m3 TokenSymbol:TEST TokenDomain: TotalSupply:+100000 MaxSupply:+1000000 Decimals:1 IsMintable:true IsBurnable:true IsUtility:false TokenStandard:zts1qanamzukd2v0pp8j2wzx6m}"
t=2001-09-09T01:58:40+0000 lvl=dbug msg="updating token owner" module=embedded contract=token old=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz new=z1qxemdeddedxdrydgexxxxxxxxxxxxxxxmqgr0d
t=2001-09-09T01:58:40+0000 lvl=dbug msg="updated ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxdrydgexxxxxxxxxxxxxxxmqgr0d TokenName:test.tok3n_na-m3 TokenSymbol:TEST TokenDomain: TotalSupply:+100000 MaxSupply:+1000000 Decimals:1 IsMintable:true IsBurnable:true IsUtility:false TokenStandard:zts1qanamzukd2v0pp8j2wzx6m}"
t=2001-09-09T02:01:50+0000 lvl=dbug msg="burned ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxdrydgexxxxxxxxxxxxxxxmqgr0d TokenName:test.tok3n_na-m3 TokenSymbol:TEST TokenDomain: TotalSupply:+95050 MaxSupply:+1000000 Decimals:1 IsMintable:true IsBurnable:true IsUtility:false TokenStandard:zts1qanamzukd2v0pp8j2wzx6m}" burned-amount=4950
t=2001-09-09T02:05:10+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxdrydgexxxxxxxxxxxxxxxmqgr0d TokenName:test.tok3n_na-m3 TokenSymbol:TEST TokenDomain: TotalSupply:+95850 MaxSupply:+1000000 Decimals:1 IsMintable:true IsBurnable:true IsUtility:false TokenStandard:zts1qanamzukd2v0pp8j2wzx6m}" minted-amount=800 to-address=z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx
`)

	// We have 2 unwraps
	activateBridgeStep7(t, z)

	networkClass := uint32(2)
	chainId := uint32(123)

	bridgeAPI := embedded.NewBridgeApi(z)
	securityInfo, err := bridgeAPI.GetSecurityInfo()
	common.FailIfErr(t, err)

	setTokenPair(t, z, g.User5.Address, securityInfo.SoftDelay, networkClass, chainId, types.ZnnTokenStandard, "0x5fbdb2315678afecb367f032d93f642f64180aa3", false, false, false,
		big.NewInt(100), uint32(15), uint32(20), `{"APR": 15, "LockingPeriod": 100}`)

	requests, err := bridgeAPI.GetAllUnwrapTokenRequests(0, 5)
	defer z.CallContract(redeemUnwrap(requests.List[0].TransactionHash, requests.List[0].LogIndex)).Error(t, nil)
	insertMomentums(z, 3)

	setTokenPair(t, z, g.User5.Address, securityInfo.SoftDelay, networkClass, chainId, types.QsrTokenStandard, "0x5fbdb2315678afecb367f032d93f642f64180aa3", true, false, false,
		big.NewInt(100), uint32(15), uint32(20), `{"APR": 15, "LockingPeriod": 100}`)

	// Wrap first so it has balance
	defer z.CallContract(wrapToken(types.QsrTokenStandard, big.NewInt(1500*g.Zexp), networkClass, chainId, "0xb794f5ea0ba39494ce839613fffba74279579268")).
		Error(t, nil)
	insertMomentums(z, 2)

	// Redeem qsr
	z.ExpectBalance(g.User2.Address, types.QsrTokenStandard, 8000000000000)
	defer z.CallContract(redeemUnwrap(requests.List[1].TransactionHash, requests.List[1].LogIndex)).Error(t, nil)
	insertMomentums(z, 3)
	autoreceive(t, z, g.User2.Address)
	insertMomentums(z, 2)

	// We should have qsr
	z.ExpectBalance(g.User2.Address, types.QsrTokenStandard, 8010000000000)

	// Redeem twice
	defer z.CallContract(redeemUnwrap(requests.List[1].TransactionHash, requests.List[1].LogIndex)).Error(t, constants.ErrInvalidRedeemRequest)
	insertMomentums(z, 2)

	// make it redeemable
	setTokenPair(t, z, g.User5.Address, securityInfo.SoftDelay, networkClass, chainId, types.QsrTokenStandard, "0x5fbdb2315678afecb367f032d93f642f64180aa3", false, true, false,
		big.NewInt(100), uint32(15), uint32(20), `{"APR": 15, "LockingPeriod": 100}`)

	hash := types.HexToHashPanic("0000456789012345678901234567890123456789012345678901234567890123")
	signature := getUnwrapTokenSignature(t, networkClass, chainId, hash, 250, "0x5fbdb2315678afecb367f032d93f642f64180aa3", big.NewInt(100*g.Zexp), networkClass)
	defer z.CallContract(unwrapToken(networkClass, chainId, hash, 250, "0x5fbdb2315678afecb367f032d93f642f64180aa3", big.NewInt(100*g.Zexp), signature)).
		Error(t, nil)
	insertMomentums(z, 3)

	requests, err = bridgeAPI.GetAllUnwrapTokenRequests(0, 3)
	common.FailIfErr(t, err)
	common.String(strconv.Itoa(requests.Count)).Equals(t, `3`)

	// not redeemable yet
	defer z.CallContract(redeemUnwrap(requests.List[0].TransactionHash, requests.List[0].LogIndex)).Error(t, constants.ErrInvalidRedeemPeriod)
	insertMomentums(z, int(requests.List[0].RedeemableIn))

	// try to redeem a revoked unwrap
	defer z.CallContract(revokeUnwrap(g.User5.Address, requests.List[0].TransactionHash, requests.List[0].LogIndex)).
		Error(t, nil)
	insertMomentums(z, 2)
	defer z.CallContract(redeemUnwrap(requests.List[0].TransactionHash, requests.List[0].LogIndex)).
		Error(t, constants.ErrInvalidRedeemRequest)
	insertMomentums(z, 2)

	hash = types.HexToHashPanic("0000056789012345678901234567890123456789012345678901234567890123")
	signature = getUnwrapTokenSignature(t, networkClass, chainId, hash, 350, "0x5fbdb2315678afecb367f032d93f642f64180aa3", big.NewInt(100*g.Zexp), networkClass)
	defer z.CallContract(unwrapToken(networkClass, chainId, hash, 350, "0x5fbdb2315678afecb367f032d93f642f64180aa3", big.NewInt(100*g.Zexp), signature)).
		Error(t, nil)
	insertMomentums(z, 3)

	requests, err = bridgeAPI.GetAllUnwrapTokenRequests(0, 4)
	common.FailIfErr(t, err)
	common.String(strconv.Itoa(requests.Count)).Equals(t, `4`)

	// remove token pair and try to redeem
	insertMomentums(z, int(requests.List[0].RedeemableIn))
	defer z.CallContract(removeTokenPair(g.User5.Address, networkClass, chainId, types.QsrTokenStandard, "0x5fbdb2315678afecb367f032d93f642f64180aa3")).
		Error(t, nil)
	insertMomentums(z, 2)

	defer z.CallContract(redeemUnwrap(requests.List[0].TransactionHash, requests.List[0].LogIndex)).
		Error(t, constants.ErrTokenNotFound)
	insertMomentums(z, 2)

	// remove the network
	defer z.CallContract(removeNetwork(g.User5.Address, networkClass, chainId)).
		Error(t, nil)
	insertMomentums(z, 2)

	defer z.CallContract(redeemUnwrap(requests.List[0].TransactionHash, requests.List[0].LogIndex)).
		Error(t, constants.ErrUnknownNetwork)
	insertMomentums(z, 2)
}

func TestBridge_SetNetwork(t *testing.T) {
	z := mock.NewMockZenonWithCustomEpochDuration(t, time.Hour)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).Equals(t, `
t=2001-09-09T01:46:50+0000 lvl=dbug msg=created module=embedded contract=spork spork="&{Id:c6a597f757168bd5c9fddf52b16b3bf38e2ef781fb8edeea1bf2ae0d3225230d Name:spork-bridge Description:activate spork for bridge Activated:false EnforcementHeight:0}"
t=2001-09-09T01:47:00+0000 lvl=dbug msg=activated module=embedded contract=spork spork="&{Id:c6a597f757168bd5c9fddf52b16b3bf38e2ef781fb8edeea1bf2ae0d3225230d Name:spork-bridge Description:activate spork for bridge Activated:true EnforcementHeight:9}"
`)
	// We just have the spork
	activateBridgeStep0(t, z)

	bridgeAPI := embedded.NewBridgeApi(z)

	// Sets a network and its metadata and it deletes it after
	setUpdateRemoveNetwork(t, z, bridgeAPI)

	// Set orchestratorInfo
	defer z.CallContract(setOrchestratorInfo(g.User5.Address, 6, 3, 15, 10)).
		Error(t, nil)
	insertMomentums(z, 2)
	common.Json(bridgeAPI.GetOrchestratorInfo()).Equals(t, `
{
	"windowSize": 6,
	"keyGenThreshold": 3,
	"confirmationsToFinality": 15,
	"estimatedMomentumTime": 10,
	"allowKeyGenHeight": 0
}`)
	// Should pass
	setUpdateRemoveNetwork(t, z, bridgeAPI)

	// Set guardians
	securityInfo, err := bridgeAPI.GetSecurityInfo()
	common.FailIfErr(t, err)
	guardians := []types.Address{g.User1.Address, g.User2.Address, g.User3.Address, g.User4.Address, g.User5.Address}
	nominateGuardians(t, z, g.User5.Address, guardians, securityInfo.AdministratorDelay)
	common.Json(bridgeAPI.GetSecurityInfo()).Equals(t, `
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
	// Should pass
	setUpdateRemoveNetwork(t, z, bridgeAPI)

	// Set tss
	tssPubKey := "AsAQx1M3LVXCuozDOqO5b9adj/PItYgwZFG/xTDBiZzT"
	changeTssWithAdministrator(t, z, g.User5.Address, tssPubKey, securityInfo.SoftDelay)
	common.Json(bridgeAPI.GetBridgeInfo()).Equals(t, `
{
	"administrator": "z1qqaswvt0e3cc5sm7lygkyza9ra63cr8e6zre09",
	"compressedTssECDSAPubKey": "AsAQx1M3LVXCuozDOqO5b9adj/PItYgwZFG/xTDBiZzT",
	"decompressedTssECDSAPubKey": "BMAQx1M3LVXCuozDOqO5b9adj/PItYgwZFG/xTDBiZzTnQAT1qOPAkuPzu6yoewss9XbnTmZmb9JQNGXmkPYtK4=",
	"allowKeyGen": false,
	"halted": false,
	"unhaltedAt": 0,
	"unhaltDurationInMomentums": 5,
	"tssNonce": 0,
	"metadata": "{}"
}`)
	// Should pass
	setUpdateRemoveNetwork(t, z, bridgeAPI)

	// Try to add a network with invalid name
	networkClass := uint32(2) // evm
	chainId := uint32(123)
	name := ""
	z.InsertSendBlock(addNetwork(g.User5.Address, networkClass, chainId, name, "0x323b5d4c32345ced77393b3530b1eed0f346429d", "{}"),
		constants.ErrInvalidNetworkName, mock.SkipVmChanges)

	name = "x1"
	z.InsertSendBlock(addNetwork(g.User5.Address, networkClass, chainId, name, "0x323b5d4c32345ced77393b3530b1eed0f346429d", "{}"),
		constants.ErrInvalidNetworkName, mock.SkipVmChanges)

	name = "0123456789012345678901234567890123456789"
	z.InsertSendBlock(addNetwork(g.User5.Address, networkClass, chainId, name, "0x323b5d4c32345ced77393b3530b1eed0f346429d", "{}"),
		constants.ErrInvalidNetworkName, mock.SkipVmChanges)

	// invalid networkClass and chainId
	name = "Ethereum"
	newChainId := uint32(0)
	z.InsertSendBlock(addNetwork(g.User5.Address, networkClass, newChainId, name, "0x323b5d4c32345ced77393b3530b1eed0f346429d", "{}"),
		constants.ErrForbiddenParam, mock.SkipVmChanges)

	newNetworkClass := uint32(0)
	z.InsertSendBlock(addNetwork(g.User5.Address, newNetworkClass, chainId, name, "0x323b5d4c32345ced77393b3530b1eed0f346429d", "{}"),
		constants.ErrForbiddenParam, mock.SkipVmChanges)

	z.InsertSendBlock(addNetwork(g.User5.Address, newNetworkClass, newChainId, name, "0x323b5d4c32345ced77393b3530b1eed0f346429d", "{}"),
		constants.ErrForbiddenParam, mock.SkipVmChanges)

	// invalid contract address
	z.InsertSendBlock(addNetwork(g.User5.Address, networkClass, chainId, name, "j23b5d4c32345ced77393b3530b1eed0f346429d", "{}"),
		constants.ErrInvalidContractAddress, mock.SkipVmChanges)

	z.InsertSendBlock(addNetwork(g.User5.Address, networkClass, chainId, name, "0x323b5d4c32345ced77393b3530b1eed0f34642", "{}"),
		constants.ErrInvalidContractAddress, mock.SkipVmChanges)

	// json
	z.InsertSendBlock(addNetwork(g.User5.Address, networkClass, chainId, name, "0x323b5d4c32345ced77393b3530b1eed0f346429d", "{asd}"),
		constants.ErrInvalidJsonContent, mock.SkipVmChanges)

	// non admin
	defer z.CallContract(addNetwork(g.User4.Address, networkClass, chainId, "Ethereum", "0x323b5d4c32345ced77393b3530b1eed0f346429d", "{}")).
		Error(t, constants.ErrPermissionDenied)
	insertMomentums(z, 2)

	// add it
	defer z.CallContract(addNetwork(g.User5.Address, networkClass, chainId, "Ethereum", "0x323b5d4c32345ced77393b3530b1eed0f346429d", "{}")).
		Error(t, nil)
	insertMomentums(z, 2)

	// Set network metadata with invalid json
	z.InsertSendBlock(setNetworkMetadata(g.User5.Address, newNetworkClass, chainId, `{"NewApy:15}`), constants.ErrInvalidJsonContent, mock.SkipVmChanges)
	insertMomentums(z, 2)

	common.Json(bridgeAPI.GetNetworkInfo(networkClass, chainId)).Equals(t, `
{
	"networkClass": 2,
	"chainId": 123,
	"name": "Ethereum",
	"contractAddress": "0x323b5d4c32345ced77393b3530b1eed0f346429d",
	"metadata": "{}",
	"tokenPairs": []
}`)

	// remove network
	// non admin
	defer z.CallContract(removeNetwork(g.User4.Address, networkClass, chainId)).
		Error(t, constants.ErrPermissionDenied)
	insertMomentums(z, 2)

	// non existing network
	newChainId = 500
	defer z.CallContract(removeNetwork(g.User5.Address, networkClass, newChainId)).
		Error(t, constants.ErrUnknownNetwork)
	insertMomentums(z, 2)

	newNetworkClass = 5
	defer z.CallContract(removeNetwork(g.User5.Address, newNetworkClass, chainId)).
		Error(t, constants.ErrUnknownNetwork)
	insertMomentums(z, 2)

	defer z.CallContract(removeNetwork(g.User5.Address, newNetworkClass, newChainId)).
		Error(t, constants.ErrUnknownNetwork)
	insertMomentums(z, 2)

	defer z.CallContract(removeNetwork(g.User5.Address, networkClass, chainId)).
		Error(t, nil)
	insertMomentums(z, 2)
	common.Json(bridgeAPI.GetNetworkInfo(networkClass, chainId)).Equals(t, `
{
	"networkClass": 0,
	"chainId": 0,
	"name": "",
	"contractAddress": "",
	"metadata": "{}",
	"tokenPairs": null
}`)

	// remove twice
	defer z.CallContract(removeNetwork(g.User5.Address, networkClass, chainId)).
		Error(t, constants.ErrUnknownNetwork)
	insertMomentums(z, 2)

	// Try to set network metadata for removed network
	defer z.CallContract(setNetworkMetadata(g.User5.Address, networkClass, chainId, `{"NewApy":15}`)).
		Error(t, constants.ErrUnknownNetwork)
	insertMomentums(z, 2)

	// Try to set network metadata for a  network that didn't exist
	defer z.CallContract(setNetworkMetadata(g.User5.Address, newNetworkClass, chainId, `{"NewApy":15}`)).
		Error(t, constants.ErrUnknownNetwork)
	insertMomentums(z, 2)
}

func TestBridge_SetTokenPair(t *testing.T) {
	z := mock.NewMockZenonWithCustomEpochDuration(t, time.Hour)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).Equals(t, `
t=2001-09-09T01:46:50+0000 lvl=dbug msg=created module=embedded contract=spork spork="&{Id:c6a597f757168bd5c9fddf52b16b3bf38e2ef781fb8edeea1bf2ae0d3225230d Name:spork-bridge Description:activate spork for bridge Activated:false EnforcementHeight:0}"
t=2001-09-09T01:47:00+0000 lvl=dbug msg=activated module=embedded contract=spork spork="&{Id:c6a597f757168bd5c9fddf52b16b3bf38e2ef781fb8edeea1bf2ae0d3225230d Name:spork-bridge Description:activate spork for bridge Activated:true EnforcementHeight:9}"
`)
	// We just have the spork
	activateBridgeStep0(t, z)

	bridgeAPI := embedded.NewBridgeApi(z)
	networkClass := uint32(2)
	chainId := uint32(123)

	// add it
	defer z.CallContract(addNetwork(g.User5.Address, networkClass, chainId, "Ethereum", "0x323b5d4c32345ced77393b3530b1eed0f346429d", "{}")).
		Error(t, nil)
	insertMomentums(z, 2)

	// Add token pair, edit and remove it with no orchestratorInfo
	setUpdateRemoveTokenPair(t, z, bridgeAPI)

	defer z.CallContract(setOrchestratorInfo(g.User5.Address, 6, 3, 15, 10)).
		Error(t, nil)
	insertMomentums(z, 2)

	// Add token pair, edit and remove it with orchestratorInfo set
	setUpdateRemoveTokenPair(t, z, bridgeAPI)

	securityInfo, err := bridgeAPI.GetSecurityInfo()
	common.FailIfErr(t, err)
	guardians := []types.Address{g.User1.Address, g.User2.Address, g.User3.Address, g.User4.Address, g.User5.Address}
	nominateGuardians(t, z, g.User5.Address, guardians, securityInfo.AdministratorDelay)

	// Add token pair, edit and remove it with orchestratorInfo and guardians
	setUpdateRemoveTokenPair(t, z, bridgeAPI)

	tssPubKey := "AsAQx1M3LVXCuozDOqO5b9adj/PItYgwZFG/xTDBiZzT" // priv tuSwrTEUyJI1/3y5J8L8DSjzT/AQG2IK3JG+93qhhhI=
	//tssPubKeyBytes, _ := base64.StdEncoding.DecodeString(tssPubKey)
	//x, y := secp256k1.DecompressPubkey(tssPubKeyBytes)
	//dPubKey := make([]byte, 0)
	//dPubKey = append(dPubKey, 4)
	//dPubKey = append(dPubKey, x.Bytes()...)
	//dPubKey = append(dPubKey, y.Bytes()...)
	//fmt.Println(len(dPubKey))
	//fmt.Println(base64.StdEncoding.EncodeToString(dPubKey))
	changeTssWithAdministrator(t, z, g.User5.Address, tssPubKey, securityInfo.SoftDelay)

	// Add token pair, edit and remove it with orchestratorInfo, guardians and tss set
	setUpdateRemoveTokenPair(t, z, bridgeAPI)

	// Set and remove as non admin
	defer z.CallContract(setTokenPairStep(g.User4.Address, networkClass, chainId, types.ZnnTokenStandard, "0x5fbdb2315678afecb367f032d93f642f64180aa3", true, true, false,
		big.NewInt(100), uint32(15), uint32(20), `{"APR": 15, "LockingPeriod": 100}`)).
		Error(t, constants.ErrPermissionDenied)
	insertMomentums(z, 2)
	defer z.CallContract(removeTokenPair(g.User4.Address, networkClass, chainId, types.ZnnTokenStandard, "0x5fbdb2315678afecb367f032d93f642f64180aa3")).
		Error(t, constants.ErrPermissionDenied)
	insertMomentums(z, 2)

	// set token with invalid minAmount
	setTokenPair(t, z, g.User5.Address, securityInfo.SoftDelay, networkClass, chainId, types.ZnnTokenStandard, "0x5fbdb2315678afecb367f032d93f642f64180aa3", true, true, false,
		big.NewInt(-1), uint32(15), uint32(20), `{"APR": 15, "LockingPeriod": 100}`)
	common.Json(bridgeAPI.GetNetworkInfo(networkClass, chainId)).Equals(t, `
{
	"networkClass": 2,
	"chainId": 123,
	"name": "Ethereum",
	"contractAddress": "0x323b5d4c32345ced77393b3530b1eed0f346429d",
	"metadata": "{}",
	"tokenPairs": [
		{
			"tokenStandard": "zts1znnxxxxxxxxxxxxx9z4ulx",
			"tokenAddress": "0x5fbdb2315678afecb367f032d93f642f64180aa3",
			"bridgeable": true,
			"redeemable": true,
			"owned": false,
			"minAmount": "115792089237316195423570985008687907853269984665640564039457584007913129639935",
			"feePercentage": 15,
			"redeemDelay": 20,
			"metadata": "{\"APR\": 15, \"LockingPeriod\": 100}"
		}
	]
}`)

	// try to insert with invalid token address
	tokenAddress := "fbdb2315678afecb367f032d93f642f64180aa3"
	z.InsertSendBlock(setTokenPairStep(g.User5.Address, networkClass, chainId, types.ZnnTokenStandard, tokenAddress, true, true, false,
		big.NewInt(100), uint32(15), uint32(20), `{"APR": 15, "LockingPeriod": 100}`),
		constants.ErrForbiddenParam, mock.SkipVmChanges)
	tokenAddress = "0xtfbdb2315678afecb367f032d93f642f64180aa3"
	z.InsertSendBlock(setTokenPairStep(g.User5.Address, networkClass, chainId, types.ZnnTokenStandard, tokenAddress, true, true, false,
		big.NewInt(100), uint32(15), uint32(20), `{"APR": 15, "LockingPeriod": 100}`),
		constants.ErrForbiddenParam, mock.SkipVmChanges)

	// zero token standard
	tokenAddress = "0x5fbdb2315678afecb367f032d93f642f64180aa3"
	z.InsertSendBlock(setTokenPairStep(g.User5.Address, networkClass, chainId, types.ZeroTokenStandard, tokenAddress, true, true, false,
		big.NewInt(100), uint32(15), uint32(20), `{"APR": 15, "LockingPeriod": 100}`),
		constants.ErrForbiddenParam, mock.SkipVmChanges)

	// fee > maximum fee
	z.InsertSendBlock(setTokenPairStep(g.User5.Address, networkClass, chainId, types.ZnnTokenStandard, tokenAddress, true, true, false,
		big.NewInt(100), constants.MaximumFee+2, uint32(20), `{"APR": 15, "LockingPeriod": 100}`),
		constants.ErrForbiddenParam, mock.SkipVmChanges)

	// redeem delay 0
	z.InsertSendBlock(setTokenPairStep(g.User5.Address, networkClass, chainId, types.ZnnTokenStandard, tokenAddress, true, true, false,
		big.NewInt(100), uint32(15), uint32(0), `{"APR": 15, "LockingPeriod": 100}`),
		constants.ErrForbiddenParam, mock.SkipVmChanges)

	// invalid json
	z.InsertSendBlock(setTokenPairStep(g.User5.Address, networkClass, chainId, types.ZnnTokenStandard, tokenAddress, true, true, false,
		big.NewInt(100), uint32(15), uint32(20), `{"APR: 15, "LockingPeriod""": 100}`),
		constants.ErrInvalidJsonContent, mock.SkipVmChanges)

	// set for non existing network
	networkClass = 0
	defer z.CallContract(setTokenPairStep(g.User5.Address, networkClass, chainId, types.ZnnTokenStandard, "0x5fbdb2315678afecb367f032d93f642f64180aa3", true, true, false,
		big.NewInt(100), uint32(15), uint32(20), `{"APR": 15, "LockingPeriod": 100}`)).
		Error(t, constants.ErrUnknownNetwork)
	insertMomentums(z, 2)

	networkClass = 2
	chainId = 0
	defer z.CallContract(setTokenPairStep(g.User5.Address, networkClass, chainId, types.ZnnTokenStandard, "0x5fbdb2315678afecb367f032d93f642f64180aa3", true, true, false,
		big.NewInt(100), uint32(15), uint32(20), `{"APR": 15, "LockingPeriod": 100}`)).
		Error(t, constants.ErrUnknownNetwork)
	insertMomentums(z, 2)

	networkClass = 0
	defer z.CallContract(setTokenPairStep(g.User5.Address, networkClass, chainId, types.ZnnTokenStandard, "0x5fbdb2315678afecb367f032d93f642f64180aa3", true, true, false,
		big.NewInt(100), uint32(15), uint32(20), `{"APR": 15, "LockingPeriod": 100}`)).
		Error(t, constants.ErrUnknownNetwork)
	insertMomentums(z, 2)

	// set for non existing network
	networkClass = 2
	chainId = 500
	defer z.CallContract(setTokenPairStep(g.User5.Address, networkClass, chainId, types.ZnnTokenStandard, "0x5fbdb2315678afecb367f032d93f642f64180aa3", true, true, false,
		big.NewInt(100), uint32(15), uint32(20), `{"APR": 15, "LockingPeriod": 100}`)).
		Error(t, constants.ErrUnknownNetwork)
	insertMomentums(z, 2)

	// start time challenge
	chainId = 123
	defer z.CallContract(setTokenPairStep(g.User5.Address, networkClass, chainId, types.QsrTokenStandard, "0x6fbdb2315678afecb367f032d93f642f64180aa3", true, true, false,
		big.NewInt(100), uint32(15), uint32(20), `{"APR": 15, "LockingPeriod": 100}`)).
		Error(t, nil)
	insertMomentums(z, 2)

	// try to set before time challenge expires
	insertMomentums(z, 5)
	defer z.CallContract(setTokenPairStep(g.User5.Address, networkClass, chainId, types.QsrTokenStandard, "0x6fbdb2315678afecb367f032d93f642f64180aa3", true, true, false,
		big.NewInt(100), uint32(15), uint32(20), `{"APR": 15, "LockingPeriod": 100}`)).
		Error(t, constants.ErrTimeChallengeNotDue)
	insertMomentums(z, 2)

	// add it
	insertMomentums(z, 6)
	defer z.CallContract(setTokenPairStep(g.User5.Address, networkClass, chainId, types.QsrTokenStandard, "0x6fbdb2315678afecb367f032d93f642f64180aa3", true, true, false,
		big.NewInt(100), uint32(15), uint32(20), `{"APR": 15, "LockingPeriod": 100}`)).
		Error(t, nil)
	insertMomentums(z, 2)

	// remove and add network and add it
	defer z.CallContract(removeNetwork(g.User5.Address, networkClass, chainId)).
		Error(t, nil)
	insertMomentums(z, 2)
	defer z.CallContract(addNetwork(g.User5.Address, networkClass, chainId, "Ethereum", "0x323b5d4c32345ced77393b3530b1eed0f346429d", "{}")).
		Error(t, nil)
	insertMomentums(z, 2)
	common.Json(bridgeAPI.GetNetworkInfo(networkClass, chainId)).Equals(t, `
{
	"networkClass": 2,
	"chainId": 123,
	"name": "Ethereum",
	"contractAddress": "0x323b5d4c32345ced77393b3530b1eed0f346429d",
	"metadata": "{}",
	"tokenPairs": []
}`)

	setTokenPair(t, z, g.User5.Address, securityInfo.SoftDelay, networkClass, chainId, types.ZnnTokenStandard, "0x5fbdb2315678afecb367f032d93f642f64180aa3", true, true, false,
		big.NewInt(100), uint32(15), uint32(20), `{"APR": 15, "LockingPeriod": 100}`)

	// try to remove with different zts
	defer z.CallContract(removeTokenPair(g.User5.Address, networkClass, chainId, types.QsrTokenStandard, "0x5fbdb2315678afecb367f032d93f642f64180aa3")).
		Error(t, constants.ErrTokenNotFound)
	insertMomentums(z, 2)

	// try to remove with different token address
	defer z.CallContract(removeTokenPair(g.User5.Address, networkClass, chainId, types.ZnnTokenStandard, "0x6fbdb2315678afecb367f032d93f642f64180aa3")).
		Error(t, constants.ErrTokenNotFound)
	insertMomentums(z, 2)

	// try to remove with non existing token pair
	defer z.CallContract(removeTokenPair(g.User5.Address, networkClass, chainId, types.QsrTokenStandard, "0x6fbdb2315678afecb367f032d93f642f64180aa3")).
		Error(t, constants.ErrTokenNotFound)
	insertMomentums(z, 2)

	// remove twice
	defer z.CallContract(removeTokenPair(g.User5.Address, networkClass, chainId, types.ZnnTokenStandard, "0x5fbdb2315678afecb367f032d93f642f64180aa3")).
		Error(t, nil)
	insertMomentums(z, 2)
	defer z.CallContract(removeTokenPair(g.User5.Address, networkClass, chainId, types.ZnnTokenStandard, "0x5fbdb2315678afecb367f032d93f642f64180aa3")).
		Error(t, constants.ErrTokenNotFound)
	insertMomentums(z, 2)

	z.InsertSendBlock(setTokenPairStep(g.User5.Address, networkClass, chainId, types.ZnnTokenStandard, tokenAddress, true, true, true,
		big.NewInt(100), uint32(15), uint32(20), `{"APR": 15, "LockingPeriod": 100}`),
		constants.ErrForbiddenParam, mock.SkipVmChanges)
	insertMomentums(z, 2)

	common.Json(bridgeAPI.GetTimeChallengesInfo()).Equals(t, `
{
	"count": 3,
	"list": [
		{
			"MethodName": "NominateGuardians",
			"ParamsHash": "0000000000000000000000000000000000000000000000000000000000000000",
			"ChallengeStartHeight": 75
		},
		{
			"MethodName": "ChangeTssECDSAPubKey",
			"ParamsHash": "0000000000000000000000000000000000000000000000000000000000000000",
			"ChallengeStartHeight": 131
		},
		{
			"MethodName": "SetTokenPair",
			"ParamsHash": "0000000000000000000000000000000000000000000000000000000000000000",
			"ChallengeStartHeight": 224
		}
	]
}`)

	z.InsertSendBlock(setTokenPairStep(g.User5.Address, networkClass, chainId, types.QsrTokenStandard, tokenAddress, true, true, true,
		big.NewInt(100), uint32(15), uint32(20), `{"APR": 15, "LockingPeriod": 100}`),
		constants.ErrForbiddenParam, mock.SkipVmChanges)
	insertMomentums(z, 2)

	common.Json(bridgeAPI.GetTimeChallengesInfo()).Equals(t, `
{
	"count": 3,
	"list": [
		{
			"MethodName": "NominateGuardians",
			"ParamsHash": "0000000000000000000000000000000000000000000000000000000000000000",
			"ChallengeStartHeight": 75
		},
		{
			"MethodName": "ChangeTssECDSAPubKey",
			"ParamsHash": "0000000000000000000000000000000000000000000000000000000000000000",
			"ChallengeStartHeight": 131
		},
		{
			"MethodName": "SetTokenPair",
			"ParamsHash": "0000000000000000000000000000000000000000000000000000000000000000",
			"ChallengeStartHeight": 224
		}
	]
}`)

	common.Json(bridgeAPI.GetNetworkInfo(networkClass, chainId)).Equals(t, `
{
	"networkClass": 2,
	"chainId": 123,
	"name": "Ethereum",
	"contractAddress": "0x323b5d4c32345ced77393b3530b1eed0f346429d",
	"metadata": "{}",
	"tokenPairs": []
}`)
}

func TestBridge_Halt(t *testing.T) {
	z := mock.NewMockZenonWithCustomEpochDuration(t, time.Hour)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).Equals(t, `
t=2001-09-09T01:46:50+0000 lvl=dbug msg=created module=embedded contract=spork spork="&{Id:c6a597f757168bd5c9fddf52b16b3bf38e2ef781fb8edeea1bf2ae0d3225230d Name:spork-bridge Description:activate spork for bridge Activated:false EnforcementHeight:0}"
t=2001-09-09T01:47:00+0000 lvl=dbug msg=activated module=embedded contract=spork spork="&{Id:c6a597f757168bd5c9fddf52b16b3bf38e2ef781fb8edeea1bf2ae0d3225230d Name:spork-bridge Description:activate spork for bridge Activated:true EnforcementHeight:9}"
t=2001-09-09T01:53:00+0000 lvl=eror msg=Halt-ErrInvalidSignature module=embedded contract=bridge error="invalid decompressed secp256k1 public key length" result=false
t=2001-09-09T01:59:20+0000 lvl=eror msg=Halt-ErrInvalidSignature module=embedded contract=bridge error="invalid secp256k1 signature" result=false
t=2001-09-09T01:59:40+0000 lvl=eror msg=Halt-ErrInvalidSignature module=embedded contract=bridge error="invalid secp256k1 signature" result=false
`)

	// We have orcInfo and guardians
	activateBridgeStep2(t, z)

	bridgeAPI := embedded.NewBridgeApi(z)
	bridgeInfo, err := bridgeAPI.GetBridgeInfo()
	common.FailIfErr(t, err)

	// try to halt with signature with no tss set
	defer z.CallContract(haltWithSignature(getHaltSignature(t, z, bridgeInfo.TssNonce))).
		Error(t, constants.ErrInvalidECDSASignature)
	insertMomentums(z, 2)

	// admin should pass
	defer z.CallContract(haltWithAdmin(g.User5.Address)).Error(t, nil)
	insertMomentums(z, 2)

	defer z.CallContract(unhalt(g.User5.Address)).Error(t, nil)
	insertMomentums(z, int(2+bridgeInfo.UnhaltDurationInMomentums))

	// add tss
	tssPubKey := "AsAQx1M3LVXCuozDOqO5b9adj/PItYgwZFG/xTDBiZzT" // priv tuSwrTEUyJI1/3y5J8L8DSjzT/AQG2IK3JG+93qhhhI=
	securityInfo, err := bridgeAPI.GetSecurityInfo()
	common.DealWithErr(err)
	changeTssWithAdministrator(t, z, g.User5.Address, tssPubKey, securityInfo.SoftDelay)

	// halt with sig should work
	defer z.CallContract(haltWithSignature(getHaltSignature(t, z, bridgeInfo.TssNonce))).
		Error(t, nil)
	insertMomentums(z, 2)

	// try to unhalt with non admin
	defer z.CallContract(unhalt(g.User4.Address)).Error(t, constants.ErrPermissionDenied)
	insertMomentums(z, 2)

	defer z.CallContract(unhalt(g.User5.Address)).Error(t, nil)
	insertMomentums(z, int(2+bridgeInfo.UnhaltDurationInMomentums))

	// using the same tss nonce should fail
	defer z.CallContract(haltWithSignature(getHaltSignature(t, z, bridgeInfo.TssNonce))).
		Error(t, constants.ErrInvalidECDSASignature)
	insertMomentums(z, 2)

	// using random tss nonce
	defer z.CallContract(haltWithSignature(getHaltSignature(t, z, uint64(100)))).
		Error(t, constants.ErrInvalidECDSASignature)
	insertMomentums(z, 2)
}

func TestBridge_Emergency(t *testing.T) {
	z := mock.NewMockZenonWithCustomEpochDuration(t, time.Hour)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).Equals(t, `
t=2001-09-09T01:46:50+0000 lvl=dbug msg=created module=embedded contract=spork spork="&{Id:c6a597f757168bd5c9fddf52b16b3bf38e2ef781fb8edeea1bf2ae0d3225230d Name:spork-bridge Description:activate spork for bridge Activated:false EnforcementHeight:0}"
t=2001-09-09T01:47:00+0000 lvl=dbug msg=activated module=embedded contract=spork spork="&{Id:c6a597f757168bd5c9fddf52b16b3bf38e2ef781fb8edeea1bf2ae0d3225230d Name:spork-bridge Description:activate spork for bridge Activated:true EnforcementHeight:9}"
`)

	// We have orcInfo
	activateBridgeStep1(t, z)

	bridgeAPI := embedded.NewBridgeApi(z)
	securityInfo, err := bridgeAPI.GetSecurityInfo()
	common.FailIfErr(t, err)

	defer z.CallContract(activateEmergency(g.User5.Address)).Error(t, constants.ErrSecurityNotInitialized)
	insertMomentums(z, 2)

	guardians := []types.Address{g.User1.Address, g.User2.Address, g.User3.Address, g.User4.Address, g.User5.Address}
	nominateGuardians(t, z, g.User5.Address, guardians, securityInfo.AdministratorDelay)

	// activate as non admin
	defer z.CallContract(activateEmergency(g.User4.Address)).Error(t, constants.ErrPermissionDenied)
	insertMomentums(z, 2)

	// activate with no tss set
	defer z.CallContract(activateEmergency(g.User5.Address)).Error(t, nil)
	insertMomentums(z, 2)

	common.Json(bridgeAPI.GetBridgeInfo()).Equals(t, `
{
	"administrator": "z1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqsggv2f",
	"compressedTssECDSAPubKey": "",
	"decompressedTssECDSAPubKey": "",
	"allowKeyGen": false,
	"halted": true,
	"unhaltedAt": 0,
	"unhaltDurationInMomentums": 5,
	"tssNonce": 0,
	"metadata": "{}"
}`)
}

func TestBridge_ChangeAdministrator(t *testing.T) {
	z := mock.NewMockZenonWithCustomEpochDuration(t, time.Hour)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).Equals(t, `
t=2001-09-09T01:46:50+0000 lvl=dbug msg=created module=embedded contract=spork spork="&{Id:c6a597f757168bd5c9fddf52b16b3bf38e2ef781fb8edeea1bf2ae0d3225230d Name:spork-bridge Description:activate spork for bridge Activated:false EnforcementHeight:0}"
t=2001-09-09T01:47:00+0000 lvl=dbug msg=activated module=embedded contract=spork spork="&{Id:c6a597f757168bd5c9fddf52b16b3bf38e2ef781fb8edeea1bf2ae0d3225230d Name:spork-bridge Description:activate spork for bridge Activated:true EnforcementHeight:9}"
`)

	// We have orcInfo
	activateBridgeStep1(t, z)

	defer z.CallContract(changeAdministratorStep(g.User5.Address, g.User4.Address)).
		Error(t, constants.ErrSecurityNotInitialized)
	insertMomentums(z, 2)

	// add guardians
	bridgeAPI := embedded.NewBridgeApi(z)
	securityInfo, err := bridgeAPI.GetSecurityInfo()
	common.DealWithErr(err)
	guardians := []types.Address{g.User1.Address, g.User2.Address, g.User3.Address, g.User4.Address, g.User5.Address}
	nominateGuardians(t, z, g.User5.Address, guardians, securityInfo.AdministratorDelay)

	// change it no tss set
	changeAdministrator(t, z, g.User5.Address, g.User4.Address, securityInfo.AdministratorDelay)

	// call with non admin
	defer z.CallContract(changeAdministratorStep(g.User5.Address, g.User4.Address)).
		Error(t, constants.ErrPermissionDenied)
	insertMomentums(z, 2)

	// set tss
	tssPubKey := "AsAQx1M3LVXCuozDOqO5b9adj/PItYgwZFG/xTDBiZzT" // priv tuSwrTEUyJI1/3y5J8L8DSjzT/AQG2IK3JG+93qhhhI=
	changeTssWithAdministrator(t, z, g.User4.Address, tssPubKey, securityInfo.AdministratorDelay)

	defer z.CallContract(changeAdministratorStep(g.User4.Address, g.User5.Address)).
		Error(t, nil)
	insertMomentums(z, 7)
	common.Json(bridgeAPI.GetTimeChallengesInfo()).Equals(t, `
{
	"count": 3,
	"list": [
		{
			"MethodName": "NominateGuardians",
			"ParamsHash": "0000000000000000000000000000000000000000000000000000000000000000",
			"ChallengeStartHeight": 15
		},
		{
			"MethodName": "ChangeTssECDSAPubKey",
			"ParamsHash": "0000000000000000000000000000000000000000000000000000000000000000",
			"ChallengeStartHeight": 69
		},
		{
			"MethodName": "ChangeAdministrator",
			"ParamsHash": "88938fa41176c43b559724b6e24cb16b8c70ea84dcc886326e34c3ac6a4b9c06",
			"ChallengeStartHeight": 95
		}
	]
}`)

	// should fail due to time challenge
	defer z.CallContract(changeAdministratorStep(g.User4.Address, g.User5.Address)).
		Error(t, constants.ErrTimeChallengeNotDue)
	insertMomentums(z, 2)

	// try to change admin with zero address
	z.InsertSendBlock(changeAdministratorStep(g.User4.Address, types.ZeroAddress), constants.ErrForbiddenParam, mock.SkipVmChanges)
	insertMomentums(z, 2)

	newAddress := types.Address{}
	common.FailIfErr(t, newAddress.SetBytes([]byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 0, 1, 2, 3, 4, 5, 6, 7, 8, 9}))
	z.InsertSendBlock(changeAdministratorStep(g.User4.Address, newAddress), nil, mock.SkipVmChanges)
	insertMomentums(z, 2)

	common.Json(bridgeAPI.GetTimeChallengesInfo()).Equals(t, `
{
	"count": 3,
	"list": [
		{
			"MethodName": "NominateGuardians",
			"ParamsHash": "0000000000000000000000000000000000000000000000000000000000000000",
			"ChallengeStartHeight": 15
		},
		{
			"MethodName": "ChangeTssECDSAPubKey",
			"ParamsHash": "0000000000000000000000000000000000000000000000000000000000000000",
			"ChallengeStartHeight": 69
		},
		{
			"MethodName": "ChangeAdministrator",
			"ParamsHash": "b741e8bbc09b26cc15bba162c564fa01c8fd22e37747b2d1219def00ad5ccd5e",
			"ChallengeStartHeight": 106
		}
	]
}`)
	insertMomentums(z, 32)
	defer z.CallContract(changeAdministratorStep(g.User4.Address, newAddress)).
		Error(t, nil)
	insertMomentums(z, 2)

	common.Json(bridgeAPI.GetTimeChallengesInfo()).Equals(t, `
{
	"count": 3,
	"list": [
		{
			"MethodName": "NominateGuardians",
			"ParamsHash": "0000000000000000000000000000000000000000000000000000000000000000",
			"ChallengeStartHeight": 15
		},
		{
			"MethodName": "ChangeTssECDSAPubKey",
			"ParamsHash": "0000000000000000000000000000000000000000000000000000000000000000",
			"ChallengeStartHeight": 69
		},
		{
			"MethodName": "ChangeAdministrator",
			"ParamsHash": "0000000000000000000000000000000000000000000000000000000000000000",
			"ChallengeStartHeight": 106
		}
	]
}`)
	common.Json(bridgeAPI.GetBridgeInfo()).Equals(t, `
{
	"administrator": "z1qqqsyqcyq5rqwzqfqqqsyqcyq5rqwzqf4xq84c",
	"compressedTssECDSAPubKey": "AsAQx1M3LVXCuozDOqO5b9adj/PItYgwZFG/xTDBiZzT",
	"decompressedTssECDSAPubKey": "BMAQx1M3LVXCuozDOqO5b9adj/PItYgwZFG/xTDBiZzTnQAT1qOPAkuPzu6yoewss9XbnTmZmb9JQNGXmkPYtK4=",
	"allowKeyGen": false,
	"halted": false,
	"unhaltedAt": 0,
	"unhaltDurationInMomentums": 5,
	"tssNonce": 0,
	"metadata": "{}"
}`)

}

func TestBridge_ChangeTss(t *testing.T) {
	z := mock.NewMockZenonWithCustomEpochDuration(t, time.Hour)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).Equals(t, `
t=2001-09-09T01:46:50+0000 lvl=dbug msg=created module=embedded contract=spork spork="&{Id:c6a597f757168bd5c9fddf52b16b3bf38e2ef781fb8edeea1bf2ae0d3225230d Name:spork-bridge Description:activate spork for bridge Activated:false EnforcementHeight:0}"
t=2001-09-09T01:47:00+0000 lvl=dbug msg=activated module=embedded contract=spork spork="&{Id:c6a597f757168bd5c9fddf52b16b3bf38e2ef781fb8edeea1bf2ae0d3225230d Name:spork-bridge Description:activate spork for bridge Activated:true EnforcementHeight:9}"
t=2001-09-09T01:55:40+0000 lvl=eror msg=ChangeTssECDSAPubKey-ErrInvalidOldKeySignature module=embedded contract=bridge error="invalid decompressed secp256k1 public key length" result=false
t=2001-09-09T01:59:30+0000 lvl=eror msg=ChangeTssECDSAPubKey-ErrInvalidOldKeySignature module=embedded contract=bridge error="invalid secp256k1 signature" result=false
t=2001-09-09T01:59:50+0000 lvl=eror msg=ChangeTssECDSAPubKey-ErrInvalidOldKeySignature module=embedded contract=bridge error="invalid secp256k1 signature" result=false
t=2001-09-09T02:00:10+0000 lvl=eror msg=ChangeTssECDSAPubKey-ErrInvalidNewKeySignature module=embedded contract=bridge error="invalid secp256k1 signature" result=false
`)

	// We have spork
	activateBridgeStep0(t, z)

	bridgeAPI := embedded.NewBridgeApi(z)
	bridgeInfo, err := bridgeAPI.GetBridgeInfo()
	common.FailIfErr(t, err)
	securityInfo, err := bridgeAPI.GetSecurityInfo()
	common.FailIfErr(t, err)

	publicKey := "AhOiqdjx002Cj8o1jxTM5LqywbgNFZwUPJuR9ffdQwFP" // priv Sf12dS9DI7xsiKrmQfPR8zQE1HUIYkd8x0XZ6fkAxXo=
	tssPubKey := "AsAQx1M3LVXCuozDOqO5b9adj/PItYgwZFG/xTDBiZzT" // priv tuSwrTEUyJI1/3y5J8L8DSjzT/AQG2IK3JG+93qhhhI=
	message, err := implementation.GetChangePubKeyMessage(definition.ChangeTssECDSAPubKeyMethodName, definition.NoMClass, z.Chain().ChainIdentifier(), bridgeInfo.TssNonce, tssPubKey)
	common.FailIfErr(t, err)
	oldSignature, err := sign(message, "Sf12dS9DI7xsiKrmQfPR8zQE1HUIYkd8x0XZ6fkAxXo=")
	newSignature, err := sign(message, "tuSwrTEUyJI1/3y5J8L8DSjzT/AQG2IK3JG+93qhhhI=")

	defer z.CallContract(changeTssWithSignature(publicKey, oldSignature, newSignature)).Error(t, constants.ErrSecurityNotInitialized)
	insertMomentums(z, 2)
	defer z.CallContract(changeTssWithAdministratorStep(g.User5.Address, publicKey)).Error(t, constants.ErrSecurityNotInitialized)
	insertMomentums(z, 2)
	// also test allowKeyGen
	defer z.CallContract(setAllowKeyGen(g.User5.Address, true)).Error(t, constants.ErrSecurityNotInitialized)
	insertMomentums(z, 2)

	// set orcInfo
	defer z.CallContract(setOrchestratorInfo(g.User5.Address, 6, 3, 15, 10)).
		Error(t, nil)
	insertMomentums(z, 2)

	defer z.CallContract(changeTssWithSignature(publicKey, oldSignature, newSignature)).Error(t, constants.ErrSecurityNotInitialized)
	insertMomentums(z, 2)
	defer z.CallContract(changeTssWithAdministratorStep(g.User5.Address, publicKey)).Error(t, constants.ErrSecurityNotInitialized)
	insertMomentums(z, 2)
	defer z.CallContract(setAllowKeyGen(g.User5.Address, true)).Error(t, constants.ErrSecurityNotInitialized)
	insertMomentums(z, 2)

	// add guardians
	guardians := []types.Address{g.User1.Address, g.User2.Address, g.User3.Address, g.User4.Address, g.User5.Address}
	nominateGuardians(t, z, g.User5.Address, guardians, securityInfo.AdministratorDelay)

	// should fail with allow keyGen false
	defer z.CallContract(changeTssWithSignature(publicKey, oldSignature, newSignature)).Error(t, constants.ErrNotAllowedToChangeTss)
	insertMomentums(z, 2)

	// SetAllowKeyGen to true
	defer z.CallContract(setAllowKeyGen(g.User5.Address, true)).Error(t, nil)
	insertMomentums(z, 2)

	defer z.CallContract(changeTssWithSignature(publicKey, oldSignature, newSignature)).Error(t, constants.ErrInvalidECDSASignature)
	insertMomentums(z, 2)

	defer z.CallContract(changeTssWithAdministratorStep(g.User5.Address, publicKey)).Error(t, nil)
	insertMomentums(z, 8)

	// time challenge not due
	defer z.CallContract(changeTssWithAdministratorStep(g.User5.Address, publicKey)).Error(t, constants.ErrTimeChallengeNotDue)
	insertMomentums(z, 5)

	// should still work with admin for allowKeyGen false
	defer z.CallContract(setAllowKeyGen(g.User5.Address, false)).Error(t, nil)
	insertMomentums(z, 2)

	defer z.CallContract(changeTssWithAdministratorStep(g.User5.Address, publicKey)).Error(t, nil)
	insertMomentums(z, 2)

	common.Json(bridgeAPI.GetBridgeInfo()).Equals(t, `
{
	"administrator": "z1qqaswvt0e3cc5sm7lygkyza9ra63cr8e6zre09",
	"compressedTssECDSAPubKey": "AhOiqdjx002Cj8o1jxTM5LqywbgNFZwUPJuR9ffdQwFP",
	"decompressedTssECDSAPubKey": "BBOiqdjx002Cj8o1jxTM5LqywbgNFZwUPJuR9ffdQwFPC3247CxWsOED9R+qv5RTS/rOxffGZYUln3JXKEIsWSA=",
	"allowKeyGen": false,
	"halted": false,
	"unhaltedAt": 0,
	"unhaltDurationInMomentums": 5,
	"tssNonce": 0,
	"metadata": "{}"
}`)

	// non admin
	defer z.CallContract(setAllowKeyGen(g.User4.Address, true)).Error(t, constants.ErrPermissionDenied)
	insertMomentums(z, 2)

	defer z.CallContract(setAllowKeyGen(g.User5.Address, true)).Error(t, nil)
	insertMomentums(z, 2)

	bridgeInfo, err = bridgeAPI.GetBridgeInfo()
	common.FailIfErr(t, err)
	// random nonce
	randomNonceMessage, err := implementation.GetChangePubKeyMessage(definition.ChangeTssECDSAPubKeyMethodName, definition.NoMClass, z.Chain().ChainIdentifier(), bridgeInfo.TssNonce+100, tssPubKey)
	common.FailIfErr(t, err)
	oldSignature, err = sign(randomNonceMessage, "Sf12dS9DI7xsiKrmQfPR8zQE1HUIYkd8x0XZ6fkAxXo=")
	newSignature, err = sign(randomNonceMessage, "tuSwrTEUyJI1/3y5J8L8DSjzT/AQG2IK3JG+93qhhhI=")

	defer z.CallContract(changeTssWithSignature(tssPubKey, oldSignature, newSignature)).Error(t, constants.ErrInvalidECDSASignature)
	insertMomentums(z, 2)

	// wrong old signature
	oldSignature, err = sign(randomNonceMessage, "Sf12dS9DI7xsiKrmQfPR8zQE1HUIYkd8x0XZ6fkAxXo=")
	newSignature, err = sign(message, "tuSwrTEUyJI1/3y5J8L8DSjzT/AQG2IK3JG+93qhhhI=")
	defer z.CallContract(changeTssWithSignature(tssPubKey, oldSignature, newSignature)).Error(t, constants.ErrInvalidECDSASignature)
	insertMomentums(z, 2)

	// wrong new signature
	oldSignature, err = sign(message, "Sf12dS9DI7xsiKrmQfPR8zQE1HUIYkd8x0XZ6fkAxXo=")
	newSignature, err = sign(randomNonceMessage, "tuSwrTEUyJI1/3y5J8L8DSjzT/AQG2IK3JG+93qhhhI=")
	defer z.CallContract(changeTssWithSignature(tssPubKey, oldSignature, newSignature)).Error(t, constants.ErrInvalidECDSASignature)
	insertMomentums(z, 2)

	newSignature, err = sign(message, "tuSwrTEUyJI1/3y5J8L8DSjzT/AQG2IK3JG+93qhhhI=")
	defer z.CallContract(changeTssWithSignature(tssPubKey, oldSignature, newSignature)).Error(t, nil)
	insertMomentums(z, 2)

	common.Json(bridgeAPI.GetBridgeInfo()).Equals(t, `
{
	"administrator": "z1qqaswvt0e3cc5sm7lygkyza9ra63cr8e6zre09",
	"compressedTssECDSAPubKey": "AsAQx1M3LVXCuozDOqO5b9adj/PItYgwZFG/xTDBiZzT",
	"decompressedTssECDSAPubKey": "BMAQx1M3LVXCuozDOqO5b9adj/PItYgwZFG/xTDBiZzTnQAT1qOPAkuPzu6yoewss9XbnTmZmb9JQNGXmkPYtK4=",
	"allowKeyGen": false,
	"halted": false,
	"unhaltedAt": 0,
	"unhaltDurationInMomentums": 5,
	"tssNonce": 1,
	"metadata": "{}"
}`)
}

func TestBridge_SetOrchestratorInfo(t *testing.T) {
	z := mock.NewMockZenonWithCustomEpochDuration(t, time.Hour)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).Equals(t, `
t=2001-09-09T01:46:50+0000 lvl=dbug msg=created module=embedded contract=spork spork="&{Id:c6a597f757168bd5c9fddf52b16b3bf38e2ef781fb8edeea1bf2ae0d3225230d Name:spork-bridge Description:activate spork for bridge Activated:false EnforcementHeight:0}"
t=2001-09-09T01:47:00+0000 lvl=dbug msg=activated module=embedded contract=spork spork="&{Id:c6a597f757168bd5c9fddf52b16b3bf38e2ef781fb8edeea1bf2ae0d3225230d Name:spork-bridge Description:activate spork for bridge Activated:true EnforcementHeight:9}"
`)

	// We have spork
	activateBridgeStep0(t, z)

	// non admin
	defer z.CallContract(setOrchestratorInfo(g.User4.Address, 6, 3, 15, 10)).
		Error(t, constants.ErrPermissionDenied)
	insertMomentums(z, 2)

	// try with any of the params with value 0
	z.InsertSendBlock(setOrchestratorInfo(g.User5.Address, 0, 3, 15, 10),
		constants.ErrForbiddenParam, mock.SkipVmChanges)
	insertMomentums(z, 2)

	z.InsertSendBlock(setOrchestratorInfo(g.User5.Address, 3, 0, 15, 10),
		constants.ErrForbiddenParam, mock.SkipVmChanges)
	insertMomentums(z, 2)

	z.InsertSendBlock(setOrchestratorInfo(g.User5.Address, 3, 3, 0, 10),
		constants.ErrForbiddenParam, mock.SkipVmChanges)
	insertMomentums(z, 2)

	z.InsertSendBlock(setOrchestratorInfo(g.User5.Address, 3, 3, 15, 0),
		constants.ErrForbiddenParam, mock.SkipVmChanges)
	insertMomentums(z, 2)
}

func TestBridge_SetBridgeMetadata(t *testing.T) {
	z := mock.NewMockZenonWithCustomEpochDuration(t, time.Hour)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).Equals(t, `
t=2001-09-09T01:46:50+0000 lvl=dbug msg=created module=embedded contract=spork spork="&{Id:c6a597f757168bd5c9fddf52b16b3bf38e2ef781fb8edeea1bf2ae0d3225230d Name:spork-bridge Description:activate spork for bridge Activated:false EnforcementHeight:0}"
t=2001-09-09T01:47:00+0000 lvl=dbug msg=activated module=embedded contract=spork spork="&{Id:c6a597f757168bd5c9fddf52b16b3bf38e2ef781fb8edeea1bf2ae0d3225230d Name:spork-bridge Description:activate spork for bridge Activated:true EnforcementHeight:9}"
`)

	// We have spork
	activateBridgeStep0(t, z)

	// non admin
	defer z.CallContract(setBridgeMetadata(g.User4.Address, `{"APY": 15}`)).
		Error(t, constants.ErrPermissionDenied)
	insertMomentums(z, 2)

	// invalid json
	z.InsertSendBlock(setBridgeMetadata(g.User5.Address, `{"APY: "15"}`), constants.ErrInvalidJsonContent, mock.SkipVmChanges)
	insertMomentums(z, 2)

	defer z.CallContract(setBridgeMetadata(g.User5.Address, `{"APYY":15}`)).
		Error(t, nil)
	insertMomentums(z, 2)

	bridgeAPI := embedded.NewBridgeApi(z)
	common.Json(bridgeAPI.GetBridgeInfo()).Equals(t, `
{
	"administrator": "z1qqaswvt0e3cc5sm7lygkyza9ra63cr8e6zre09",
	"compressedTssECDSAPubKey": "",
	"decompressedTssECDSAPubKey": "",
	"allowKeyGen": false,
	"halted": false,
	"unhaltedAt": 0,
	"unhaltDurationInMomentums": 5,
	"tssNonce": 0,
	"metadata": "{\"APYY\":15}"
}`)
}

func TestBridge_NominateGuardians(t *testing.T) {
	z := mock.NewMockZenonWithCustomEpochDuration(t, time.Hour)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).Equals(t, `
t=2001-09-09T01:46:50+0000 lvl=dbug msg=created module=embedded contract=spork spork="&{Id:c6a597f757168bd5c9fddf52b16b3bf38e2ef781fb8edeea1bf2ae0d3225230d Name:spork-bridge Description:activate spork for bridge Activated:false EnforcementHeight:0}"
t=2001-09-09T01:47:00+0000 lvl=dbug msg=activated module=embedded contract=spork spork="&{Id:c6a597f757168bd5c9fddf52b16b3bf38e2ef781fb8edeea1bf2ae0d3225230d Name:spork-bridge Description:activate spork for bridge Activated:true EnforcementHeight:9}"
`)

	// We have spork and orcInfo
	activateBridgeStep1(t, z)

	bridgeAPI := embedded.NewBridgeApi(z)

	guardians := []types.Address{g.User1.Address, g.User2.Address, g.User3.Address, g.User4.Address, g.User5.Address}
	defer z.CallContract(nominateGuardiansStep(g.User4.Address, guardians)).
		Error(t, constants.ErrPermissionDenied)
	insertMomentums(z, 2)

	// one invalid address
	guardians[0] = types.ZeroAddress
	z.InsertSendBlock(nominateGuardiansStep(g.User5.Address, guardians), constants.ErrForbiddenParam, mock.SkipVmChanges)
	insertMomentums(z, 2)

	// less than min
	guardians = guardians[2:]
	constants.MinGuardians = 4
	z.InsertSendBlock(nominateGuardiansStep(g.User5.Address, guardians), constants.ErrInvalidGuardians, mock.SkipVmChanges)
	insertMomentums(z, 2)

	guardians = []types.Address{g.User1.Address, g.User2.Address, g.User3.Address, g.User4.Address, g.User5.Address}
	defer z.CallContract(nominateGuardiansStep(g.User5.Address, guardians)).
		Error(t, nil)
	insertMomentums(z, 5)

	defer z.CallContract(nominateGuardiansStep(g.User5.Address, guardians)).
		Error(t, constants.ErrTimeChallengeNotDue)
	insertMomentums(z, 30)

	common.Json(bridgeAPI.GetSecurityInfo()).Equals(t, `
{
	"guardians": [],
	"guardiansVotes": [],
	"administratorDelay": 20,
	"softDelay": 10
}`)
	defer z.CallContract(nominateGuardiansStep(g.User5.Address, guardians)).
		Error(t, nil)
	insertMomentums(z, 2)

	common.Json(bridgeAPI.GetSecurityInfo()).Equals(t, `
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

func TestBridge_ProposeAdministrator(t *testing.T) {
	z := mock.NewMockZenonWithCustomEpochDuration(t, time.Hour)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).Equals(t, `
t=2001-09-09T01:46:50+0000 lvl=dbug msg=created module=embedded contract=spork spork="&{Id:c6a597f757168bd5c9fddf52b16b3bf38e2ef781fb8edeea1bf2ae0d3225230d Name:spork-bridge Description:activate spork for bridge Activated:false EnforcementHeight:0}"
t=2001-09-09T01:47:00+0000 lvl=dbug msg=activated module=embedded contract=spork spork="&{Id:c6a597f757168bd5c9fddf52b16b3bf38e2ef781fb8edeea1bf2ae0d3225230d Name:spork-bridge Description:activate spork for bridge Activated:true EnforcementHeight:9}"
`)

	// We have orc info
	activateBridgeStep1(t, z)

	bridgeAPI := embedded.NewBridgeApi(z)
	securityInfo, err := bridgeAPI.GetSecurityInfo()
	common.DealWithErr(err)

	guardians := []types.Address{g.User2.Address, g.User3.Address, g.User4.Address, g.User5.Address}
	nominateGuardians(t, z, g.User5.Address, guardians, securityInfo.AdministratorDelay)

	// should not work as we are not in emergency yet
	defer z.CallContract(proposeAdministrator(g.User1.Address, g.User6.Address)).Error(t, constants.ErrNotEmergency)
	insertMomentums(z, 2)

	defer z.CallContract(activateEmergency(g.User5.Address)).Error(t, nil)
	insertMomentums(z, 2)

	// try to propose zero address
	z.InsertSendBlock(proposeAdministrator(g.User1.Address, types.ZeroAddress),
		constants.ErrForbiddenParam, mock.SkipVmChanges)
	insertMomentums(z, 2)

	// try to propose as non guardian
	defer z.CallContract(proposeAdministrator(g.User1.Address, g.User1.Address)).Error(t, constants.ErrNotGuardian)
	insertMomentums(z, 2)

	//
	defer z.CallContract(proposeAdministrator(g.User2.Address, g.User1.Address)).Error(t, nil)
	insertMomentums(z, 2)
	common.Json(bridgeAPI.GetSecurityInfo()).Equals(t, `
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
	defer z.CallContract(proposeAdministrator(g.User2.Address, g.User3.Address)).Error(t, nil)
	insertMomentums(z, 2)
	common.Json(bridgeAPI.GetSecurityInfo()).Equals(t, `
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

	defer z.CallContract(proposeAdministrator(g.User3.Address, g.User3.Address)).Error(t, nil)
	insertMomentums(z, 2)
	defer z.CallContract(proposeAdministrator(g.User4.Address, g.User3.Address)).Error(t, nil)
	insertMomentums(z, 2)

	common.Json(bridgeAPI.GetBridgeInfo()).Equals(t, `
{
	"administrator": "z1qrs2lpccnsneglhnnfwvlsj0qncnxjnwlfmjac",
	"compressedTssECDSAPubKey": "",
	"decompressedTssECDSAPubKey": "",
	"allowKeyGen": false,
	"halted": true,
	"unhaltedAt": 0,
	"unhaltDurationInMomentums": 5,
	"tssNonce": 0,
	"metadata": "{}"
}`)
	common.Json(bridgeAPI.GetSecurityInfo()).Equals(t, `
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

func proposeAdministrator(guardian types.Address, proposedAdministrator types.Address) *nom.AccountBlock {
	return &nom.AccountBlock{
		Address:       guardian,
		ToAddress:     types.BridgeContract,
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(0),
		Data: definition.ABIBridge.PackMethodPanic(definition.ProposeAdministratorMethodName,
			proposedAdministrator),
	}
}

func setAllowKeyGen(administrator types.Address, allowKeyGen bool) *nom.AccountBlock {
	return &nom.AccountBlock{
		Address:       administrator,
		ToAddress:     types.BridgeContract,
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(0),
		Data: definition.ABIBridge.PackMethodPanic(definition.SetAllowKeygenMethodName,
			allowKeyGen),
	}
}

func setNetworkMetadata(administrator types.Address, networkClass, chainId uint32, metadata string) *nom.AccountBlock {
	return &nom.AccountBlock{
		Address:       administrator,
		ToAddress:     types.BridgeContract,
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(0),
		Data: definition.ABIBridge.PackMethodPanic(definition.SetNetworkMetadataMethodName,
			networkClass, chainId, metadata),
	}
}

func setBridgeMetadata(administrator types.Address, metadata string) *nom.AccountBlock {
	return &nom.AccountBlock{
		Address:       administrator,
		ToAddress:     types.BridgeContract,
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(0),
		Data: definition.ABIBridge.PackMethodPanic(definition.SetBridgeMetadataMethodName,
			metadata),
	}
}

func setOrchestratorInfo(administrator types.Address, windowSize uint64, keyGenThreshold, confirmationsToFinality, estimatedMomentumTime uint32) *nom.AccountBlock {
	return &nom.AccountBlock{
		Address:       administrator,
		ToAddress:     types.BridgeContract,
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(0),
		Data: definition.ABIBridge.PackMethodPanic(definition.SetOrchestratorInfoMethodName,
			windowSize, keyGenThreshold, confirmationsToFinality, estimatedMomentumTime),
	}
}

func nominateGuardiansStep(administrator types.Address, guardians []types.Address) *nom.AccountBlock {
	return &nom.AccountBlock{
		Address:       administrator,
		ToAddress:     types.BridgeContract,
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(0),
		Data: definition.ABIBridge.PackMethodPanic(definition.NominateGuardiansMethodName,
			guardians),
	}
}

func nominateGuardians(t *testing.T, z mock.MockZenon, administrator types.Address, guardians []types.Address, delay uint64) {
	defer z.CallContract(nominateGuardiansStep(administrator, guardians)).Error(t, nil)
	insertMomentums(z, 2)

	frMom, err := z.Chain().GetFrontierMomentumStore().GetFrontierMomentum()
	common.DealWithErr(err)
	z.InsertMomentumsTo(frMom.Height + delay + 2)

	defer z.CallContract(nominateGuardiansStep(administrator, guardians)).Error(t, nil)
	insertMomentums(z, 2)
}

func changeTssWithAdministratorStep(administrator types.Address, newTssPublicKey string) *nom.AccountBlock {
	return &nom.AccountBlock{
		Address:       administrator,
		ToAddress:     types.BridgeContract,
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(0),
		Data: definition.ABIBridge.PackMethodPanic(definition.ChangeTssECDSAPubKeyMethodName,
			newTssPublicKey, "", ""),
	}
}

func changeTssWithAdministrator(t *testing.T, z mock.MockZenon, administrator types.Address, newTssPublicKey string, delay uint64) {
	defer z.CallContract(changeTssWithAdministratorStep(administrator, newTssPublicKey)).Error(t, nil)
	insertMomentums(z, 2)

	frMom, err := z.Chain().GetFrontierMomentumStore().GetFrontierMomentum()
	common.DealWithErr(err)
	z.InsertMomentumsTo(frMom.Height + delay + 2)

	defer z.CallContract(changeTssWithAdministratorStep(administrator, newTssPublicKey)).Error(t, nil)
	insertMomentums(z, 2)
}

func changeTssWithSignature(newPubKey, oldPubKeySignature, newPubKeySignature string) *nom.AccountBlock {
	return &nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     types.BridgeContract,
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(0),
		Data: definition.ABIBridge.PackMethodPanic(definition.ChangeTssECDSAPubKeyMethodName,
			newPubKey, oldPubKeySignature, newPubKeySignature),
	}
}

func changeAdministratorStep(administrator types.Address, newAdministrator types.Address) *nom.AccountBlock {
	return &nom.AccountBlock{
		Address:       administrator,
		ToAddress:     types.BridgeContract,
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(0),
		Data: definition.ABIBridge.PackMethodPanic(definition.ChangeAdministratorMethodName,
			newAdministrator),
	}
}

func changeAdministrator(t *testing.T, z mock.MockZenon, administrator types.Address, newAdministrator types.Address, delay uint64) {
	defer z.CallContract(changeAdministratorStep(administrator, newAdministrator)).Error(t, nil)
	insertMomentums(z, 2)

	frMom, err := z.Chain().GetFrontierMomentumStore().GetFrontierMomentum()
	common.DealWithErr(err)
	z.InsertMomentumsTo(frMom.Height + delay + 2)

	defer z.CallContract(changeAdministratorStep(administrator, newAdministrator)).Error(t, nil)
	insertMomentums(z, 2)
}

func activateEmergency(administrator types.Address) *nom.AccountBlock {
	return &nom.AccountBlock{
		Address:       administrator,
		ToAddress:     types.BridgeContract,
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(0),
		Data:          definition.ABIBridge.PackMethodPanic(definition.EmergencyMethodName),
	}
}

func haltWithAdmin(administrator types.Address) *nom.AccountBlock {
	return &nom.AccountBlock{
		Address:       administrator,
		ToAddress:     types.BridgeContract,
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(0),
		Data:          definition.ABIBridge.PackMethodPanic(definition.HaltMethodName, ""),
	}
}

func haltWithSignature(signature string) *nom.AccountBlock {
	return &nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     types.BridgeContract,
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(0),
		Data: definition.ABIBridge.PackMethodPanic(definition.HaltMethodName,
			signature),
	}
}

func unhalt(administrator types.Address) *nom.AccountBlock {
	return &nom.AccountBlock{
		Address:       administrator,
		ToAddress:     types.BridgeContract,
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(0),
		Data:          definition.ABIBridge.PackMethodPanic(definition.UnhaltMethodName),
	}
}

func wrapToken(zts types.ZenonTokenStandard, amount *big.Int, networkClass, chainId uint32, toAddress string) *nom.AccountBlock {
	return &nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     types.BridgeContract,
		TokenStandard: zts,
		Amount:        amount,
		Data: definition.ABIBridge.PackMethodPanic(definition.WrapTokenMethodName,
			networkClass,
			chainId,
			toAddress,
		),
	}
}

func unwrapToken(networkClass, chainId uint32, txHash types.Hash, logIndex uint32, tokenAddress string, amount *big.Int, signature string) *nom.AccountBlock {
	return &nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     types.BridgeContract,
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(0),
		Data: definition.ABIBridge.PackMethodPanic(definition.UnwrapTokenMethodName,
			networkClass,
			chainId,
			txHash,          // TransactionHash
			logIndex,        // LogIndex
			g.User2.Address, // ToAddress
			tokenAddress,    // TokenAddress
			amount,          // Amount
			signature,
		),
	}
}

func redeemUnwrap(hash types.Hash, logIndex uint32) *nom.AccountBlock {
	return &nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     types.BridgeContract,
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(0),
		Data: definition.ABIBridge.PackMethodPanic(definition.RedeemUnwrapMethodName,
			hash,
			logIndex,
		),
	}
}

func revokeUnwrap(administrator types.Address, hash types.Hash, logIndex uint32) *nom.AccountBlock {
	return &nom.AccountBlock{
		Address:       administrator,
		ToAddress:     types.BridgeContract,
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(0),
		Data: definition.ABIBridge.PackMethodPanic(definition.RevokeUnwrapRequestMethodName,
			hash,
			logIndex,
		),
	}
}

func getUpdateWrapTokenSignature(request *embedded.WrapTokenRequest, contractAddress ecommon.Address, privateKey string) string {
	message, err := implementation.GetWrapTokenRequestMessage(request.WrapTokenRequest, &contractAddress)
	common.DealWithErr(err)
	signature, err := sign(message, privateKey)
	common.DealWithErr(err)
	return signature
}

func updateWrapToken(id types.Hash, signature string) *nom.AccountBlock {
	return &nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     types.BridgeContract,
		TokenStandard: types.ZnnTokenStandard,
		Amount:        common.Big0,
		Data: definition.ABIBridge.PackMethodPanic(definition.UpdateWrapRequestMethodName,
			id,
			signature,
		),
	}
}

func addNetwork(administrator types.Address, networkClass, chainId uint32, name, contractAddress, metadata string) *nom.AccountBlock {
	return &nom.AccountBlock{
		Address:       administrator,
		ToAddress:     types.BridgeContract,
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(0),
		Data: definition.ABIBridge.PackMethodPanic(definition.SetNetworkMethodName,
			networkClass, // evm
			chainId,
			name,            //Network name
			contractAddress, // Contract address
			metadata,
		),
	}
}

func removeNetwork(administrator types.Address, networkClass, chainId uint32) *nom.AccountBlock {
	return &nom.AccountBlock{
		Address:       administrator,
		ToAddress:     types.BridgeContract,
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(0),
		Data: definition.ABIBridge.PackMethodPanic(definition.RemoveNetworkMethodName,
			networkClass,
			chainId,
		),
	}
}

func removeTokenPair(administrator types.Address, networkClass, chainId uint32, zts types.ZenonTokenStandard, tokenAddress string) *nom.AccountBlock {
	return &nom.AccountBlock{
		Address:       administrator,
		ToAddress:     types.BridgeContract,
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(0),
		Data: definition.ABIBridge.PackMethodPanic(definition.RemoveTokenPairMethodName,
			networkClass,
			chainId,
			zts,
			tokenAddress,
		),
	}
}

func setTokenPairStep(administrator types.Address, networkClass, chainId uint32, zts types.ZenonTokenStandard, tokenAddress string, bridgeable, redeemable, owned bool, minAmount *big.Int, feePercentage, redeemDelay uint32, metadata string) *nom.AccountBlock {
	return &nom.AccountBlock{
		Address:       administrator,
		ToAddress:     types.BridgeContract,
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(0),
		Data: definition.ABIBridge.PackMethodPanic(definition.SetTokenPairMethod,
			networkClass,
			chainId,
			zts,
			tokenAddress,
			bridgeable,
			redeemable,
			owned,
			minAmount,
			feePercentage,
			redeemDelay,
			metadata,
		),
	}
}

func setTokenPair(t *testing.T, z mock.MockZenon, administrator types.Address, delay uint64, networkClass, chainId uint32, zts types.ZenonTokenStandard, tokenAddress string, bridgeable, redeemable, owned bool, minAmount *big.Int, feePercentage, redeemDelay uint32, metadata string) {
	defer z.CallContract(setTokenPairStep(administrator, networkClass, chainId, zts, tokenAddress, bridgeable, redeemable, owned,
		minAmount, feePercentage, redeemDelay, metadata)).Error(t, nil)
	insertMomentums(z, 2)

	frMom, err := z.Chain().GetFrontierMomentumStore().GetFrontierMomentum()
	common.FailIfErr(t, err)
	z.InsertMomentumsTo(frMom.Height + delay)

	defer z.CallContract(setTokenPairStep(administrator, networkClass, chainId, zts, tokenAddress, bridgeable, redeemable, owned,
		minAmount, feePercentage, redeemDelay, metadata)).Error(t, nil)
	insertMomentums(z, 2)
}

func createZtsOwnedByBridge(t *testing.T, z mock.MockZenon) types.ZenonTokenStandard {
	tokenAPI := embedded.NewTokenApi(z)

	defer z.CallContract(&nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     types.TokenContract,
		TokenStandard: types.ZnnTokenStandard,
		Amount:        constants.TokenIssueAmount,
		Data: definition.ABIToken.PackMethodPanic(definition.IssueMethodName,
			"test.tok3n_na-m3",  //param.TokenName
			"TEST",              //param.TokenSymbol
			"",                  //param.TokenDomain
			big.NewInt(100000),  //param.TotalSupply
			big.NewInt(1000000), //param.MaxSupply
			uint8(1),            //param.Decimals
			true,                //param.IsMintable
			true,                //param.IsBurnable
			false,               //param.IsUtility
		),
	}).Error(t, nil)
	z.InsertNewMomentum()
	z.InsertNewMomentum()

	tokenList, err := tokenAPI.GetByOwner(g.User1.Address, 0, 10)
	common.FailIfErr(t, err)

	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.TokenContract,
		Data:      definition.ABIToken.PackMethodPanic(definition.UpdateTokenMethodName, tokenList.List[0].ZenonTokenStandard, types.BridgeContract, true, true),
	}).Error(t, nil)
	z.InsertNewMomentum()
	z.InsertNewMomentum()

	autoreceive(t, z, g.User1.Address)
	z.ExpectBalance(g.User1.Address, tokenList.List[0].ZenonTokenStandard, 100000)
	return tokenList.List[0].ZenonTokenStandard
}

// hashNetworkClass is used to determine what hash to use despite the param networkClass
func getUnwrapTokenRequestMessage(param *definition.UnwrapTokenParam, hashNetworkClass uint32) ([]byte, error) {
	args := eabi.Arguments{{Type: definition.Uint256Ty}, {Type: definition.Uint256Ty}, {Type: definition.Uint256Ty}, {Type: definition.Uint256Ty}, {Type: definition.Uint256Ty}, {Type: definition.AddressTy}, {Type: definition.Uint256Ty}}
	values := make([]interface{}, 0)
	values = append(values,
		big.NewInt(0).SetUint64(uint64(param.NetworkClass)),   // network type
		big.NewInt(0).SetUint64(uint64(param.ChainId)),        // network chain id
		big.NewInt(0).SetBytes(param.TransactionHash.Bytes()), // unique tx hash
		big.NewInt(int64(param.LogIndex)),                     // unique logIndex for the tx
		big.NewInt(0).SetBytes(param.ToAddress.Bytes()),
		ecommon.HexToAddress(param.TokenAddress),
		param.Amount,
	)

	messageBytes, err := args.PackValues(values)
	if err != nil {
		return nil, err
	}

	//bridgeLog.Info("CheckECDSASignature", "message", message)

	return implementation.HashByNetworkClass(messageBytes, hashNetworkClass)
}

func getUnwrapTokenSignature(t *testing.T, networkClass, chainId uint32, txHash types.Hash, logIndex uint32, tokenAddress string, amount *big.Int, hashNetworkClass uint32) string {
	unwrapVar := &definition.UnwrapTokenParam{
		NetworkClass:    networkClass,
		ChainId:         chainId,
		TransactionHash: txHash,
		LogIndex:        logIndex,
		ToAddress:       g.User2.Address,
		TokenAddress:    tokenAddress,
		Amount:          amount,
	}
	message, err := getUnwrapTokenRequestMessage(unwrapVar, hashNetworkClass)
	common.FailIfErr(t, err)
	signature, err := sign(message, "tuSwrTEUyJI1/3y5J8L8DSjzT/AQG2IK3JG+93qhhhI=")
	common.FailIfErr(t, err)
	return signature
}

func getHaltSignature(t *testing.T, z mock.MockZenon, tssNonce uint64) string {
	message, err := implementation.GetBasicMethodMessage(definition.HaltMethodName, tssNonce, definition.NoMClass, z.Chain().ChainIdentifier())
	common.FailIfErr(t, err)
	signature, err := sign(message, "tuSwrTEUyJI1/3y5J8L8DSjzT/AQG2IK3JG+93qhhhI=")
	common.FailIfErr(t, err)
	return signature
}

func setUpdateRemoveNetwork(t *testing.T, z mock.MockZenon, bridgeAPI *embedded.BridgeApi) {
	// Add a network
	networkClass := uint32(2) // evm
	chainId := uint32(123)
	defer z.CallContract(addNetwork(g.User5.Address, networkClass, chainId, "Ethereum", "0x323b5d4c32345ced77393b3530b1eed0f346429d", "{}")).
		Error(t, nil)
	insertMomentums(z, 2)

	common.Json(bridgeAPI.GetNetworkInfo(networkClass, chainId)).Equals(t, `
{
	"networkClass": 2,
	"chainId": 123,
	"name": "Ethereum",
	"contractAddress": "0x323b5d4c32345ced77393b3530b1eed0f346429d",
	"metadata": "{}",
	"tokenPairs": []
}`)

	// Try to set network metadata
	defer z.CallContract(setNetworkMetadata(g.User5.Address, networkClass, chainId, `{"NewApy":15}`)).
		Error(t, nil)
	insertMomentums(z, 2)
	common.Json(bridgeAPI.GetNetworkInfo(networkClass, chainId)).Equals(t, `
{
	"networkClass": 2,
	"chainId": 123,
	"name": "Ethereum",
	"contractAddress": "0x323b5d4c32345ced77393b3530b1eed0f346429d",
	"metadata": "{\"NewApy\":15}",
	"tokenPairs": []
}`)

	// Remove this network
	defer z.CallContract(removeNetwork(g.User5.Address, networkClass, chainId)).
		Error(t, nil)
	insertMomentums(z, 2)
	common.Json(bridgeAPI.GetNetworkInfo(networkClass, chainId)).Equals(t, `
{
	"networkClass": 0,
	"chainId": 0,
	"name": "",
	"contractAddress": "",
	"metadata": "{}",
	"tokenPairs": null
}`)
}

func setUpdateRemoveTokenPair(t *testing.T, z mock.MockZenon, bridgeAPI *embedded.BridgeApi) {
	networkClass := uint32(2)
	chainId := uint32(123)
	securityInfo, err := bridgeAPI.GetSecurityInfo()
	common.FailIfErr(t, err)
	setTokenPair(t, z, g.User5.Address, securityInfo.SoftDelay, networkClass, chainId, types.ZnnTokenStandard, "0x5fbdb2315678afecb367f032d93f642f64180aa3", true, true, false,
		big.NewInt(100), uint32(15), uint32(20), `{"APR": 15, "LockingPeriod": 100}`)

	setTokenPair(t, z, g.User5.Address, securityInfo.SoftDelay, networkClass, chainId, types.QsrTokenStandard, "0x5fbdb2315678afecb367f032d93f642f64180aa3", true, true, false,
		big.NewInt(100), uint32(15), uint32(20), `{"APR": 15, "LockingPeriod": 100}`)

	defer z.CallContract(removeTokenPair(g.User5.Address, networkClass, chainId, types.QsrTokenStandard, "0x5fbdb2315678afecb367f032d93f642f64180aa3")).Error(t, nil)
	insertMomentums(z, 2)
	common.Json(bridgeAPI.GetNetworkInfo(networkClass, chainId)).Equals(t, `
{
	"networkClass": 2,
	"chainId": 123,
	"name": "Ethereum",
	"contractAddress": "0x323b5d4c32345ced77393b3530b1eed0f346429d",
	"metadata": "{}",
	"tokenPairs": []
}`)
}

func sign(hash []byte, privateKey string) (string, error) {
	var key *ecdsa.PrivateKey
	var bytes []byte

	if b, err := base64.StdEncoding.DecodeString(privateKey); err != nil {
		return "", err
	} else {
		bytes = b
	}
	if pk, err := crypto.ToECDSA(bytes); err != nil {
		return "", err
	} else {
		key = pk
	}

	if sig, err := crypto.Sign(hash, key); err != nil {
		return "", err
	} else {
		return base64.StdEncoding.EncodeToString(sig), nil
	}
}

func insertMomentums(z mock.MockZenon, target int) {
	for i := 0; i < target; i++ {
		z.InsertNewMomentum()
	}
}
