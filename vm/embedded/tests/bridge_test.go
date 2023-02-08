package tests

import (
	"crypto/ecdsa"
	"encoding/base64"
	"fmt"
	ecommon "github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/assert"
	"github.com/zenon-network/go-zenon/rpc/api"
	"github.com/zenon-network/go-zenon/vm/abi"
	"github.com/zenon-network/go-zenon/vm/embedded/implementation"
	"golang.org/x/crypto/ed25519"
	"math/big"
	"math/rand"
	"strconv"
	"strings"
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

func activateBridge(t *testing.T, z mock.MockZenon) {
	sporkAPI := embedded.NewSporkApi(z)
	z.InsertSendBlock(&nom.AccountBlock{
		Address:   g.Spork.Address,
		ToAddress: types.SporkContract,
		Data: definition.ABISpork.PackMethodPanic(definition.SporkCreateMethodName,
			"spork-bridge",              // name
			"activate spork for bridge", // description
		),
	}, nil, mock.SkipVmChanges)
	z.InsertNewMomentum()

	sporkList, _ := sporkAPI.GetAll(0, 10)
	id := sporkList.List[0].Id
	constants.MinUnhaltDurationInMomentums = 30
	constants.MinAdministratorDelay = 30
	z.InsertSendBlock(&nom.AccountBlock{
		Address:   g.Spork.Address,
		ToAddress: types.SporkContract,
		Data: definition.ABISpork.PackMethodPanic(definition.SporkActivateMethodName,
			id, // id
		),
	}, nil, mock.SkipVmChanges)
	z.InsertNewMomentum()
	types.BridgeSpork.SporkId = id
	types.ImplementedSporksMap[id] = true
	z.InsertMomentumsTo(10)

	bridgeAPI := embedded.NewBridgeApi(z)

	message, err := implementation.GetChangePubKeyMessage(definition.ChangeAdministratorEDDSAPubKeyMethodName, definition.NoMClass, 1, 0, base64.StdEncoding.EncodeToString(g.User5.Public))
	common.FailIfErr(t, err)
	signatureEDDSA := ed25519.Sign(g.User5.Private, message)
	signatureStr := base64.StdEncoding.EncodeToString(signatureEDDSA)
	constants.InitialBridgeAdministratorPubKey = base64.StdEncoding.EncodeToString(g.User5.Public)
	z.InsertSendBlock(&nom.AccountBlock{
		Address:   g.User5.Address,
		ToAddress: types.BridgeContract,
		Data: definition.ABIBridge.PackMethodPanic(definition.ChangeAdministratorEDDSAPubKeyMethodName,
			base64.StdEncoding.EncodeToString(g.User5.Public),
			signatureStr,
		),
	}, nil, mock.SkipVmChanges)
	z.InsertNewMomentum()
	z.InsertNewMomentum()

	secInfo, err := bridgeAPI.GetSecurityInfo()
	common.FailIfErr(t, err)
	common.Json(secInfo, err).Equals(t, `
{
	"requestedAdministratorPubKey": "CyJWr3uy4niOIG1BDGjmurWOo/czaiErqb0EpZLrMAQ=",
	"administratorChangeMomentum": 11,
	"administratorDelay": 30,
	"requestedTssPubKey": "",
	"tssChangeMomentum": 0,
	"tssDelay": 21,
	"guardians": [],
	"guardiansVotes": [],
	"nominatedGuardians": [],
	"guardiansNominationHeight": 0
}`)

	bridgeInfo, err := bridgeAPI.GetBridgeInfo()
	common.FailIfErr(t, err)
	common.Json(bridgeInfo, err).Equals(t, `
{
	"administratorEDDSAPubKey": "CyJWr3uy4niOIG1BDGjmurWOo/czaiErqb0EpZLrMAQ=",
	"compressedTssECDSAPubKey": "",
	"decompressedTssECDSAPubKey": "",
	"allowKeyGen": false,
	"halted": false,
	"unhaltedAt": 0,
	"unhaltDurationInMomentums": 30,
	"tssNonce": 0,
	"metadata": "{}"
}`)

	z.InsertSendBlock(&nom.AccountBlock{
		Address:   g.User5.Address,
		ToAddress: types.BridgeContract,
		Data: definition.ABIBridge.PackMethodPanic(definition.SetOrchestratorInfoMethodName,
			uint64(6),  // windowSize
			uint32(3),  // keyGenThreshold
			uint32(15), // confirmationsToFinality
			uint32(10), // estimatedMomTime
		),
	}, nil, mock.SkipVmChanges)
	z.InsertNewMomentum()
	z.InsertNewMomentum()

	orchestratorInfo, err := bridgeAPI.GetOrchestratorInfo()
	common.FailIfErr(t, err)
	common.Json(orchestratorInfo, err).Equals(t, `
{
	"windowSize": 6,
	"keyGenThreshold": 3,
	"confirmationsToFinality": 15,
	"estimatedMomentumTime": 10,
	"allowKeyGenHeight": 0,
	"keySignThreshold": 0,
	"metadata": "{}"
}`)
	guardians := []string{
		base64.StdEncoding.EncodeToString(g.User1.Public),
		base64.StdEncoding.EncodeToString(g.User2.Public),
		base64.StdEncoding.EncodeToString(g.User3.Public),
		base64.StdEncoding.EncodeToString(g.User4.Public),
		base64.StdEncoding.EncodeToString(g.User5.Public),
	}
	z.InsertSendBlock(&nom.AccountBlock{
		Address:   g.User5.Address,
		ToAddress: types.BridgeContract,
		Data: definition.ABIBridge.PackMethodPanic(definition.NominateGuardiansMethodName,
			guardians,
		),
	}, nil, mock.SkipVmChanges)
	z.InsertNewMomentum()
	z.InsertNewMomentum()

	securityInfo, err := bridgeAPI.GetSecurityInfo()
	common.FailIfErr(t, err)
	common.Json(securityInfo, err).Equals(t, `
{
	"requestedAdministratorPubKey": "CyJWr3uy4niOIG1BDGjmurWOo/czaiErqb0EpZLrMAQ=",
	"administratorChangeMomentum": 11,
	"administratorDelay": 30,
	"requestedTssPubKey": "",
	"tssChangeMomentum": 0,
	"tssDelay": 21,
	"guardians": [],
	"guardiansVotes": [],
	"nominatedGuardians": [
		"B8uNQ2UGKIG1aMMYo1keh3DBMH0rrlBa4e9+maM8w2E=",
		"CyJWr3uy4niOIG1BDGjmurWOo/czaiErqb0EpZLrMAQ=",
		"GYyn77OXTL31zPbDBCe/eKir+VCF3hv+LxiOUF3XcJY=",
		"i0rZDoLUpl3vCI+5eMbFih7vh3VTWm3u+y2KXqO6Qh0=",
		"tUJu3P7Drp25XP662lIjyFlFpvj8bWUpyC+0y5YTzXM="
	],
	"guardiansNominationHeight": 15
}`)
	z.InsertMomentumsTo(42)
	z.InsertSendBlock(&nom.AccountBlock{
		Address:   g.User5.Address,
		ToAddress: types.BridgeContract,
		Data: definition.ABIBridge.PackMethodPanic(definition.ChangeAdministratorEDDSAPubKeyMethodName,
			base64.StdEncoding.EncodeToString(g.User5.Public),
			signatureStr,
		),
	}, nil, mock.SkipVmChanges)
	z.InsertNewMomentum()
	z.InsertNewMomentum()

	securityInfo, err = bridgeAPI.GetSecurityInfo()
	common.FailIfErr(t, err)
	common.Json(securityInfo, err).Equals(t, `
{
	"requestedAdministratorPubKey": "",
	"administratorChangeMomentum": 11,
	"administratorDelay": 30,
	"requestedTssPubKey": "",
	"tssChangeMomentum": 0,
	"tssDelay": 21,
	"guardians": [],
	"guardiansVotes": [],
	"nominatedGuardians": [
		"B8uNQ2UGKIG1aMMYo1keh3DBMH0rrlBa4e9+maM8w2E=",
		"CyJWr3uy4niOIG1BDGjmurWOo/czaiErqb0EpZLrMAQ=",
		"GYyn77OXTL31zPbDBCe/eKir+VCF3hv+LxiOUF3XcJY=",
		"i0rZDoLUpl3vCI+5eMbFih7vh3VTWm3u+y2KXqO6Qh0=",
		"tUJu3P7Drp25XP662lIjyFlFpvj8bWUpyC+0y5YTzXM="
	],
	"guardiansNominationHeight": 15
}`)

	z.InsertMomentumsTo(65)
	z.InsertSendBlock(&nom.AccountBlock{
		Address:   g.User5.Address,
		ToAddress: types.BridgeContract,
		Data: definition.ABIBridge.PackMethodPanic(definition.NominateGuardiansMethodName,
			guardians,
		),
	}, nil, mock.SkipVmChanges)
	z.InsertNewMomentum()
	z.InsertNewMomentum()

	securityInfo, err = bridgeAPI.GetSecurityInfo()
	common.FailIfErr(t, err)
	common.Json(securityInfo, err).Equals(t, `
{
	"requestedAdministratorPubKey": "",
	"administratorChangeMomentum": 11,
	"administratorDelay": 30,
	"requestedTssPubKey": "",
	"tssChangeMomentum": 0,
	"tssDelay": 21,
	"guardians": [
		"B8uNQ2UGKIG1aMMYo1keh3DBMH0rrlBa4e9+maM8w2E=",
		"CyJWr3uy4niOIG1BDGjmurWOo/czaiErqb0EpZLrMAQ=",
		"GYyn77OXTL31zPbDBCe/eKir+VCF3hv+LxiOUF3XcJY=",
		"i0rZDoLUpl3vCI+5eMbFih7vh3VTWm3u+y2KXqO6Qh0=",
		"tUJu3P7Drp25XP662lIjyFlFpvj8bWUpyC+0y5YTzXM="
	],
	"guardiansVotes": [
		"",
		"",
		"",
		"",
		""
	],
	"nominatedGuardians": [],
	"guardiansNominationHeight": 15
}`)

	common.FailIfErr(t, err)
	z.InsertSendBlock(&nom.AccountBlock{
		Address:   g.User5.Address,
		ToAddress: types.BridgeContract,
		Data:      definition.ABIBridge.PackMethodPanic(definition.SetUnhaltDurationMethodName, uint64(35)),
	}, nil, mock.SkipVmChanges)
	z.InsertNewMomentum()
	z.InsertNewMomentum()

	bridgeInfo, err = bridgeAPI.GetBridgeInfo()
	common.FailIfErr(t, err)
	common.Json(bridgeInfo, err).Equals(t, `
{
	"administratorEDDSAPubKey": "CyJWr3uy4niOIG1BDGjmurWOo/czaiErqb0EpZLrMAQ=",
	"compressedTssECDSAPubKey": "",
	"decompressedTssECDSAPubKey": "",
	"allowKeyGen": false,
	"halted": false,
	"unhaltedAt": 0,
	"unhaltDurationInMomentums": 35,
	"tssNonce": 0,
	"metadata": "{}"
}`)
	tssPubKey := "AsAQx1M3LVXCuozDOqO5b9adj/PItYgwZFG/xTDBiZzT" // priv tuSwrTEUyJI1/3y5J8L8DSjzT/AQG2IK3JG+93qhhhI=
	//tssPubKeyBytes, _ := base64.StdEncoding.DecodeString(tssPubKey)
	//x, y := secp256k1.DecompressPubkey(tssPubKeyBytes)
	//dPubKey := make([]byte, 0)
	//dPubKey = append(dPubKey, 4)
	//dPubKey = append(dPubKey, x.Bytes()...)
	//dPubKey = append(dPubKey, y.Bytes()...)
	//fmt.Println(len(dPubKey))
	//fmt.Println(base64.StdEncoding.EncodeToString(dPubKey))
	z.InsertSendBlock(&nom.AccountBlock{
		Address:   g.User5.Address,
		ToAddress: types.BridgeContract,
		Data: definition.ABIBridge.PackMethodPanic(definition.ChangeTssECDSAPubKeyMethodName,
			tssPubKey,
			"",
			"",
			uint32(3),
		),
	}, nil, mock.SkipVmChanges)
	z.InsertNewMomentum()
	z.InsertNewMomentum()

	securityInfo, err = bridgeAPI.GetSecurityInfo()
	common.FailIfErr(t, err)
	common.Json(securityInfo, err).Equals(t, `
{
	"requestedAdministratorPubKey": "",
	"administratorChangeMomentum": 11,
	"administratorDelay": 30,
	"requestedTssPubKey": "AsAQx1M3LVXCuozDOqO5b9adj/PItYgwZFG/xTDBiZzT",
	"tssChangeMomentum": 70,
	"tssDelay": 21,
	"guardians": [
		"B8uNQ2UGKIG1aMMYo1keh3DBMH0rrlBa4e9+maM8w2E=",
		"CyJWr3uy4niOIG1BDGjmurWOo/czaiErqb0EpZLrMAQ=",
		"GYyn77OXTL31zPbDBCe/eKir+VCF3hv+LxiOUF3XcJY=",
		"i0rZDoLUpl3vCI+5eMbFih7vh3VTWm3u+y2KXqO6Qh0=",
		"tUJu3P7Drp25XP662lIjyFlFpvj8bWUpyC+0y5YTzXM="
	],
	"guardiansVotes": [
		"",
		"",
		"",
		"",
		""
	],
	"nominatedGuardians": [],
	"guardiansNominationHeight": 15
}`)

	z.InsertMomentumsTo(92)
	z.InsertSendBlock(&nom.AccountBlock{
		Address:   g.User5.Address,
		ToAddress: types.BridgeContract,
		Data: definition.ABIBridge.PackMethodPanic(definition.ChangeTssECDSAPubKeyMethodName,
			tssPubKey,
			"",
			"",
			uint32(3),
		),
	}, nil, mock.SkipVmChanges)
	z.InsertNewMomentum()
	z.InsertNewMomentum()

	securityInfo, err = bridgeAPI.GetSecurityInfo()
	common.FailIfErr(t, err)
	common.Json(securityInfo, err).Equals(t, `
{
	"requestedAdministratorPubKey": "",
	"administratorChangeMomentum": 11,
	"administratorDelay": 30,
	"requestedTssPubKey": "",
	"tssChangeMomentum": 70,
	"tssDelay": 21,
	"guardians": [
		"B8uNQ2UGKIG1aMMYo1keh3DBMH0rrlBa4e9+maM8w2E=",
		"CyJWr3uy4niOIG1BDGjmurWOo/czaiErqb0EpZLrMAQ=",
		"GYyn77OXTL31zPbDBCe/eKir+VCF3hv+LxiOUF3XcJY=",
		"i0rZDoLUpl3vCI+5eMbFih7vh3VTWm3u+y2KXqO6Qh0=",
		"tUJu3P7Drp25XP662lIjyFlFpvj8bWUpyC+0y5YTzXM="
	],
	"guardiansVotes": [
		"",
		"",
		"",
		"",
		""
	],
	"nominatedGuardians": [],
	"guardiansNominationHeight": 15
}`)

	bridgeInfo, err = bridgeAPI.GetBridgeInfo()
	common.FailIfErr(t, err)
	common.Json(bridgeInfo, err).Equals(t, `
{
	"administratorEDDSAPubKey": "CyJWr3uy4niOIG1BDGjmurWOo/czaiErqb0EpZLrMAQ=",
	"compressedTssECDSAPubKey": "AsAQx1M3LVXCuozDOqO5b9adj/PItYgwZFG/xTDBiZzT",
	"decompressedTssECDSAPubKey": "BMAQx1M3LVXCuozDOqO5b9adj/PItYgwZFG/xTDBiZzTnQAT1qOPAkuPzu6yoewss9XbnTmZmb9JQNGXmkPYtK4=",
	"allowKeyGen": false,
	"halted": false,
	"unhaltedAt": 0,
	"unhaltDurationInMomentums": 35,
	"tssNonce": 0,
	"metadata": "{}"
}`)
}

func TestBridge(t *testing.T) {
	z := mock.NewMockZenonWithCustomEpochDuration(t, time.Hour)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).Equals(t, `
t=2001-09-09T01:46:50+0000 lvl=dbug msg=created module=embedded contract=spork spork="&{Id:c6a597f757168bd5c9fddf52b16b3bf38e2ef781fb8edeea1bf2ae0d3225230d Name:spork-bridge Description:activate spork for bridge Activated:false EnforcementHeight:0}"
t=2001-09-09T01:47:00+0000 lvl=dbug msg=activated module=embedded contract=spork spork="&{Id:c6a597f757168bd5c9fddf52b16b3bf38e2ef781fb8edeea1bf2ae0d3225230d Name:spork-bridge Description:activate spork for bridge Activated:true EnforcementHeight:9}"
`)
	activateBridge(t, z)
	bridgeAPI := embedded.NewBridgeApi(z)
	bridgeInfo, err := bridgeAPI.GetBridgeInfo()

	common.Json(bridgeInfo, err).Equals(t, `
{
	"administratorEDDSAPubKey": "CyJWr3uy4niOIG1BDGjmurWOo/czaiErqb0EpZLrMAQ=",
	"compressedTssECDSAPubKey": "AsAQx1M3LVXCuozDOqO5b9adj/PItYgwZFG/xTDBiZzT",
	"decompressedTssECDSAPubKey": "BMAQx1M3LVXCuozDOqO5b9adj/PItYgwZFG/xTDBiZzTnQAT1qOPAkuPzu6yoewss9XbnTmZmb9JQNGXmkPYtK4=",
	"allowKeyGen": false,
	"halted": false,
	"unhaltedAt": 0,
	"unhaltDurationInMomentums": 35,
	"tssNonce": 0,
	"metadata": "{}"
}`)
}

func addNetwork(t *testing.T, z mock.MockZenon, networkClass, chainId uint32, name, contractAddress, metadata string) {
	bridgeAPI := embedded.NewBridgeApi(z)
	defer z.CallContract(&nom.AccountBlock{
		Address:       g.User5.Address,
		ToAddress:     types.BridgeContract,
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(0),
		Data: definition.ABIBridge.PackMethodPanic(definition.AddNetworkMethodName,
			networkClass, // evm
			chainId,
			name,            //Network name
			contractAddress, // Contract address
			metadata,
		),
	}).Error(t, nil)
	z.InsertNewMomentum() // cemented send block
	z.InsertNewMomentum() // cemented token-receive-block
	networkInfo, err := bridgeAPI.GetNetworkInfo(networkClass, chainId)
	common.FailIfErr(t, err)
	common.Json(networkInfo.Id, err).Equals(t, strconv.Itoa(int(chainId)))
	common.Json(networkInfo.Class, err).Equals(t, strconv.Itoa(int(networkClass)))
	common.ExpectString(t, networkInfo.Name, name)
	common.ExpectString(t, networkInfo.Metadata, metadata)
}
func addTokenPair(t *testing.T, z mock.MockZenon, networkClass, chainId uint32, zts types.ZenonTokenStandard, tokenAddress string, bridgeable, redeemable, owned bool, minAmount *big.Int, feePercentage, redeemDelay uint32, metadata string) {
	defer z.CallContract(&nom.AccountBlock{
		Address:       g.User5.Address,
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
	}).Error(t, nil)
	z.InsertNewMomentum() // cemented send block
	z.InsertNewMomentum() // cemented token-receive-block
}
func addNetworkInfo(t *testing.T, z mock.MockZenon) {
	networkClass := uint32(2) // evm
	chainId := uint32(31337)
	networkName := "Ethereum"
	tokenAddress := "0x5FbDB2315678afecb367f032d93F642f64180aa3"
	contractAddress := "0x323b5d4c32345ced77393b3530b1eed0f346429d"

	addNetwork(t, z, networkClass, chainId, networkName, contractAddress, "{}")
	addTokenPair(t, z, networkClass, chainId, types.ZnnTokenStandard, tokenAddress, true, true, true,
		big.NewInt(1000), uint32(10), uint32(40), `{}`)
}

func TestBridge_AddNetwork(t *testing.T) {
	z := mock.NewMockZenonWithCustomEpochDuration(t, time.Hour)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).Equals(t, `
t=2001-09-09T01:46:50+0000 lvl=dbug msg=created module=embedded contract=spork spork="&{Id:c6a597f757168bd5c9fddf52b16b3bf38e2ef781fb8edeea1bf2ae0d3225230d Name:spork-bridge Description:activate spork for bridge Activated:false EnforcementHeight:0}"
t=2001-09-09T01:47:00+0000 lvl=dbug msg=activated module=embedded contract=spork spork="&{Id:c6a597f757168bd5c9fddf52b16b3bf38e2ef781fb8edeea1bf2ae0d3225230d Name:spork-bridge Description:activate spork for bridge Activated:true EnforcementHeight:9}"
`)
	activateBridge(t, z)
	bridgeAPI := embedded.NewBridgeApi(z)

	networkClass := uint32(2) // evm
	chainId := uint32(123)
	addNetwork(t, z, networkClass, chainId, "Ethereum", "0x323b5d4c32345ced77393b3530b1eed0f346429d", "{}")

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

func TestBridge_AddAndRemoveNetworkAndTokenPair(t *testing.T) {
	z := mock.NewMockZenonWithCustomEpochDuration(t, time.Hour)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).Equals(t, `
t=2001-09-09T01:46:50+0000 lvl=dbug msg=created module=embedded contract=spork spork="&{Id:c6a597f757168bd5c9fddf52b16b3bf38e2ef781fb8edeea1bf2ae0d3225230d Name:spork-bridge Description:activate spork for bridge Activated:false EnforcementHeight:0}"
t=2001-09-09T01:47:00+0000 lvl=dbug msg=activated module=embedded contract=spork spork="&{Id:c6a597f757168bd5c9fddf52b16b3bf38e2ef781fb8edeea1bf2ae0d3225230d Name:spork-bridge Description:activate spork for bridge Activated:true EnforcementHeight:9}"
`)
	activateBridge(t, z)
	bridgeAPI := embedded.NewBridgeApi(z)

	networkClass := uint32(2) // evm
	chainId := uint32(123)
	networkName := "Ethereum"
	contractAddress := "0x323b5d4c32345ced77393b3530b1eed0f346428d"
	addNetwork(t, z, networkClass, chainId, networkName, contractAddress, "{}")

	networkInfo, err := bridgeAPI.GetNetworkInfo(networkClass, chainId)
	common.FailIfErr(t, err)

	common.Json(networkInfo, err).Equals(t, `
{
	"networkClass": 2,
	"chainId": 123,
	"name": "Ethereum",
	"contractAddress": "0x323b5d4c32345ced77393b3530b1eed0f346428d",
	"metadata": "{}",
	"tokenPairs": []
}`)

	addTokenPair(t, z, networkClass, chainId, types.ZnnTokenStandard, "0x5FbDB2315678afecb367f032d93F642f64180aa3", true, true, true,
		big.NewInt(100), uint32(15), uint32(20), `{"APR": 15, "LockingPeriod": 100}`)

	networkInfo, err = bridgeAPI.GetNetworkInfo(networkClass, chainId)
	common.FailIfErr(t, err)
	common.Json(networkInfo, err).Equals(t, `
{
	"networkClass": 2,
	"chainId": 123,
	"name": "Ethereum",
	"contractAddress": "0x323b5d4c32345ced77393b3530b1eed0f346428d",
	"metadata": "{}",
	"tokenPairs": [
		{
			"tokenStandard": "zts1znnxxxxxxxxxxxxx9z4ulx",
			"tokenAddress": "0x5FbDB2315678afecb367f032d93F642f64180aa3",
			"bridgeable": true,
			"redeemable": true,
			"owned": true,
			"minAmount": 100,
			"feePercentage": 15,
			"redeemDelay": 20,
			"metadata": "{\"APR\": 15, \"LockingPeriod\": 100}"
		}
	]
}`)

	defer z.CallContract(&nom.AccountBlock{
		Address:       g.User5.Address,
		ToAddress:     types.BridgeContract,
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(0),
		Data: definition.ABIBridge.PackMethodPanic(definition.RemoveTokenPairMethodName,
			networkClass,
			chainId,
			types.ZnnTokenStandard,
			"0x5FbDB2315678afecb367f032d93F642f64180aa3",
		),
	}).Error(t, nil)
	z.InsertNewMomentum() // cemented send block
	z.InsertNewMomentum() // cemented token-receive-block

	networkInfo, err = bridgeAPI.GetNetworkInfo(networkClass, chainId)
	common.FailIfErr(t, err)
	common.Json(networkInfo, err).Equals(t, `
{
	"networkClass": 2,
	"chainId": 123,
	"name": "Ethereum",
	"contractAddress": "0x323b5d4c32345ced77393b3530b1eed0f346428d",
	"metadata": "{}",
	"tokenPairs": []
}`)

	defer z.CallContract(&nom.AccountBlock{
		Address:       g.User5.Address,
		ToAddress:     types.BridgeContract,
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(0),
		Data: definition.ABIBridge.PackMethodPanic(definition.RemoveNetworkMethodName,
			networkClass,
			chainId,
		),
	}).Error(t, nil)
	z.InsertNewMomentum() // cemented send block
	z.InsertNewMomentum() // cemented token-receive-block

	networkInfo, err = bridgeAPI.GetNetworkInfo(networkClass, chainId)
	common.FailIfErr(t, err)

	common.Json(networkInfo, err).Equals(t, `
{
	"networkClass": 0,
	"chainId": 0,
	"name": "",
	"contractAddress": "",
	"metadata": "{}",
	"tokenPairs": null
}`)
}

func TestBridge_AddAndRemoveTokenPair(t *testing.T) {
	z := mock.NewMockZenonWithCustomEpochDuration(t, time.Hour)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).Equals(t, `
t=2001-09-09T01:46:50+0000 lvl=dbug msg=created module=embedded contract=spork spork="&{Id:c6a597f757168bd5c9fddf52b16b3bf38e2ef781fb8edeea1bf2ae0d3225230d Name:spork-bridge Description:activate spork for bridge Activated:false EnforcementHeight:0}"
t=2001-09-09T01:47:00+0000 lvl=dbug msg=activated module=embedded contract=spork spork="&{Id:c6a597f757168bd5c9fddf52b16b3bf38e2ef781fb8edeea1bf2ae0d3225230d Name:spork-bridge Description:activate spork for bridge Activated:true EnforcementHeight:9}"
`)
	activateBridge(t, z)
	bridgeAPI := embedded.NewBridgeApi(z)

	networkClass := uint32(2) // evm
	chainId := uint32(123)
	networkName := "Ethereum"
	contractAddress := "0x323b5d4c32345ced77393b3530b1eed0f346427d"

	addNetwork(t, z, networkClass, chainId, networkName, contractAddress, "{}")
	networkInfo, err := bridgeAPI.GetNetworkInfo(networkClass, chainId)
	common.FailIfErr(t, err)

	common.Json(networkInfo, err).Equals(t, `
{
	"networkClass": 2,
	"chainId": 123,
	"name": "Ethereum",
	"contractAddress": "0x323b5d4c32345ced77393b3530b1eed0f346427d",
	"metadata": "{}",
	"tokenPairs": []
}`)

	addTokenPair(t, z, networkClass, chainId, types.ZnnTokenStandard, "0x5FbDB2315678afecb367f032d93F642f64180aa3", true, true, true,
		big.NewInt(200), uint32(25), uint32(20), `{"decimals": 8}`)

	networkInfo, err = bridgeAPI.GetNetworkInfo(networkClass, chainId)
	common.FailIfErr(t, err)

	common.Json(networkInfo, err).Equals(t, `
{
	"networkClass": 2,
	"chainId": 123,
	"name": "Ethereum",
	"contractAddress": "0x323b5d4c32345ced77393b3530b1eed0f346427d",
	"metadata": "{}",
	"tokenPairs": [
		{
			"tokenStandard": "zts1znnxxxxxxxxxxxxx9z4ulx",
			"tokenAddress": "0x5FbDB2315678afecb367f032d93F642f64180aa3",
			"bridgeable": true,
			"redeemable": true,
			"owned": true,
			"minAmount": 200,
			"feePercentage": 25,
			"redeemDelay": 20,
			"metadata": "{\"decimals\": 8}"
		}
	]
}`)

	addTokenPair(t, z, networkClass, chainId, types.QsrTokenStandard, "0x6AbDB2315678afecb367f032d93F642f64180ab4", true, true, true,
		big.NewInt(200), uint32(25), uint32(15), `{"decimals": 8}`)

	networkInfo, err = bridgeAPI.GetNetworkInfo(networkClass, chainId)
	common.FailIfErr(t, err)

	common.Json(networkInfo, err).Equals(t, `
{
	"networkClass": 2,
	"chainId": 123,
	"name": "Ethereum",
	"contractAddress": "0x323b5d4c32345ced77393b3530b1eed0f346427d",
	"metadata": "{}",
	"tokenPairs": [
		{
			"tokenStandard": "zts1znnxxxxxxxxxxxxx9z4ulx",
			"tokenAddress": "0x5FbDB2315678afecb367f032d93F642f64180aa3",
			"bridgeable": true,
			"redeemable": true,
			"owned": true,
			"minAmount": 200,
			"feePercentage": 25,
			"redeemDelay": 20,
			"metadata": "{\"decimals\": 8}"
		},
		{
			"tokenStandard": "zts1qsrxxxxxxxxxxxxxmrhjll",
			"tokenAddress": "0x6AbDB2315678afecb367f032d93F642f64180ab4",
			"bridgeable": true,
			"redeemable": true,
			"owned": true,
			"minAmount": 200,
			"feePercentage": 25,
			"redeemDelay": 15,
			"metadata": "{\"decimals\": 8}"
		}
	]
}`)

	defer z.CallContract(&nom.AccountBlock{
		Address:       g.User5.Address,
		ToAddress:     types.BridgeContract,
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(0),
		Data: definition.ABIBridge.PackMethodPanic(definition.RemoveTokenPairMethodName,
			networkClass,
			chainId,
			types.ZnnTokenStandard,
			"0x5FbDB2315678afecb367f032d93F642f64180aa3",
		),
	}).Error(t, nil)
	z.InsertNewMomentum() // cemented send block
	z.InsertNewMomentum() // cemented token-receive-block

	networkInfo, err = bridgeAPI.GetNetworkInfo(networkClass, chainId)
	common.FailIfErr(t, err)

	common.Json(networkInfo, err).Equals(t, `
{
	"networkClass": 2,
	"chainId": 123,
	"name": "Ethereum",
	"contractAddress": "0x323b5d4c32345ced77393b3530b1eed0f346427d",
	"metadata": "{}",
	"tokenPairs": [
		{
			"tokenStandard": "zts1qsrxxxxxxxxxxxxxmrhjll",
			"tokenAddress": "0x6AbDB2315678afecb367f032d93F642f64180ab4",
			"bridgeable": true,
			"redeemable": true,
			"owned": true,
			"minAmount": 200,
			"feePercentage": 25,
			"redeemDelay": 15,
			"metadata": "{\"decimals\": 8}"
		}
	]
}`)
}

func TestBridge_AddAndUpdateTokenPair(t *testing.T) {
	z := mock.NewMockZenonWithCustomEpochDuration(t, time.Hour)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).Equals(t, `
t=2001-09-09T01:46:50+0000 lvl=dbug msg=created module=embedded contract=spork spork="&{Id:c6a597f757168bd5c9fddf52b16b3bf38e2ef781fb8edeea1bf2ae0d3225230d Name:spork-bridge Description:activate spork for bridge Activated:false EnforcementHeight:0}"
t=2001-09-09T01:47:00+0000 lvl=dbug msg=activated module=embedded contract=spork spork="&{Id:c6a597f757168bd5c9fddf52b16b3bf38e2ef781fb8edeea1bf2ae0d3225230d Name:spork-bridge Description:activate spork for bridge Activated:true EnforcementHeight:9}"
`)
	activateBridge(t, z)
	bridgeAPI := embedded.NewBridgeApi(z)

	networkClass := uint32(2) // evm
	chainId := uint32(123)
	networkName := "Ethereum"
	contractAddress := "0x323b5d4c32345ced77393b3530b1eed0f346426d"

	addNetwork(t, z, networkClass, chainId, networkName, contractAddress, "{}")
	networkInfo, err := bridgeAPI.GetNetworkInfo(networkClass, chainId)
	common.FailIfErr(t, err)

	common.Json(networkInfo, err).Equals(t, `
{
	"networkClass": 2,
	"chainId": 123,
	"name": "Ethereum",
	"contractAddress": "0x323b5d4c32345ced77393b3530b1eed0f346426d",
	"metadata": "{}",
	"tokenPairs": []
}`)

	addTokenPair(t, z, networkClass, chainId, types.ZnnTokenStandard, "0x5FbDB2315678afecb367f032d93F642f64180aa3", true, true, true,
		big.NewInt(200), uint32(25), uint32(15), `{"decimals": 8}`)

	networkInfo, err = bridgeAPI.GetNetworkInfo(networkClass, chainId)
	common.FailIfErr(t, err)

	common.Json(networkInfo, err).Equals(t, `
{
	"networkClass": 2,
	"chainId": 123,
	"name": "Ethereum",
	"contractAddress": "0x323b5d4c32345ced77393b3530b1eed0f346426d",
	"metadata": "{}",
	"tokenPairs": [
		{
			"tokenStandard": "zts1znnxxxxxxxxxxxxx9z4ulx",
			"tokenAddress": "0x5FbDB2315678afecb367f032d93F642f64180aa3",
			"bridgeable": true,
			"redeemable": true,
			"owned": true,
			"minAmount": 200,
			"feePercentage": 25,
			"redeemDelay": 15,
			"metadata": "{\"decimals\": 8}"
		}
	]
}`)
	common.FailIfErr(t, err)

	networkInfo, err = bridgeAPI.GetNetworkInfo(networkClass, chainId)
	common.FailIfErr(t, err)

	common.Json(networkInfo, err).Equals(t, `
{
	"networkClass": 2,
	"chainId": 123,
	"name": "Ethereum",
	"contractAddress": "0x323b5d4c32345ced77393b3530b1eed0f346426d",
	"metadata": "{}",
	"tokenPairs": [
		{
			"tokenStandard": "zts1znnxxxxxxxxxxxxx9z4ulx",
			"tokenAddress": "0x5FbDB2315678afecb367f032d93F642f64180aa3",
			"bridgeable": true,
			"redeemable": true,
			"owned": true,
			"minAmount": 200,
			"feePercentage": 25,
			"redeemDelay": 15,
			"metadata": "{\"decimals\": 8}"
		}
	]
}`)
}

// todo This method is now called setTokenPair and edits and entry if it does not exist, this test should check that the token stays the same after change
func TestBridge_AddDuplicateTokenStandards(t *testing.T) {
	z := mock.NewMockZenonWithCustomEpochDuration(t, time.Hour)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).Equals(t, `
t=2001-09-09T01:46:50+0000 lvl=dbug msg=created module=embedded contract=spork spork="&{Id:c6a597f757168bd5c9fddf52b16b3bf38e2ef781fb8edeea1bf2ae0d3225230d Name:spork-bridge Description:activate spork for bridge Activated:false EnforcementHeight:0}"
t=2001-09-09T01:47:00+0000 lvl=dbug msg=activated module=embedded contract=spork spork="&{Id:c6a597f757168bd5c9fddf52b16b3bf38e2ef781fb8edeea1bf2ae0d3225230d Name:spork-bridge Description:activate spork for bridge Activated:true EnforcementHeight:9}"
`)
	activateBridge(t, z)
	bridgeAPI := embedded.NewBridgeApi(z)
	networkClass := uint32(2) // evm
	chainId := uint32(123)
	networkName := "Ethereum"
	// todo add method for testing that adds a network instead of adding it in every test
	contractAddress := "0x323b5d4c32345ced77393b3530b1eed0f346429d"

	addNetwork(t, z, networkClass, chainId, networkName, contractAddress, "{}")
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

	addTokenPair(t, z, networkClass, chainId, types.ZnnTokenStandard, "0x5FbDB2315678afecb367f032d93F642f64180aa3", true, true, true,
		big.NewInt(98745), uint32(85), uint32(40), "{}")

	//addTokenPair(t, z, networkClass, chainId, types.ZnnTokenStandard, "0x5FbDB2315678afecb367f032d93F642f64180aa4", true, true, false,
	//	big.NewInt(456), uint32(75), uint32(17), "{}")
	defer z.CallContract(&nom.AccountBlock{
		Address:       g.User5.Address,
		ToAddress:     types.BridgeContract,
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(0),
		Data: definition.ABIBridge.PackMethodPanic(definition.SetTokenPairMethod,
			networkClass,
			chainId,
			types.ZnnTokenStandard,
			"0x5FbDB2315678afecb367f032d93F642f64180aa4",
			true,
			true,
			false,
			big.NewInt(456),
			uint32(75),
			uint32(17),
			"{}",
		),
	}).Error(t, nil)
	z.InsertNewMomentum() // cemented send block
	z.InsertNewMomentum() // cemented token-receive-block

	networkInfo, err = bridgeAPI.GetNetworkInfo(networkClass, chainId)
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
			"tokenAddress": "0x5FbDB2315678afecb367f032d93F642f64180aa4",
			"bridgeable": true,
			"redeemable": true,
			"owned": false,
			"minAmount": 456,
			"feePercentage": 75,
			"redeemDelay": 17,
			"metadata": "{}"
		}
	]
}`)
}

func TestBridge_AddMultipleNetworks(t *testing.T) {
	z := mock.NewMockZenonWithCustomEpochDuration(t, time.Hour)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).Equals(t, `
t=2001-09-09T01:46:50+0000 lvl=dbug msg=created module=embedded contract=spork spork="&{Id:c6a597f757168bd5c9fddf52b16b3bf38e2ef781fb8edeea1bf2ae0d3225230d Name:spork-bridge Description:activate spork for bridge Activated:false EnforcementHeight:0}"
t=2001-09-09T01:47:00+0000 lvl=dbug msg=activated module=embedded contract=spork spork="&{Id:c6a597f757168bd5c9fddf52b16b3bf38e2ef781fb8edeea1bf2ae0d3225230d Name:spork-bridge Description:activate spork for bridge Activated:true EnforcementHeight:9}"
`)
	activateBridge(t, z)
	bridgeAPI := embedded.NewBridgeApi(z)
	networkClass := uint32(2) // evm
	chainId := uint32(123)
	networkName := "Ethereum"
	contractAddress := "0x323b5d4c32345ced77393b3530b1eed0f346429d"

	addNetwork(t, z, networkClass, chainId, networkName, contractAddress, "{}")

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

	networkClass2 := uint32(1) // znn
	chainId2 := uint32(1234)
	networkName2 := "ZnnTestnet"
	contractAddress2 := "0x423b5d4c32345ced77393b3530b1eed0f346429d"

	addNetwork(t, z, networkClass2, chainId2, networkName2, contractAddress2, "{}")

	networkInfo, err = bridgeAPI.GetNetworkInfo(networkClass2, chainId2)
	common.FailIfErr(t, err)
	common.Json(networkInfo, err).Equals(t, `
{
	"networkClass": 1,
	"chainId": 1234,
	"name": "ZnnTestnet",
	"contractAddress": "0x423b5d4c32345ced77393b3530b1eed0f346429d",
	"metadata": "{}",
	"tokenPairs": []
}`)

	networks, err := bridgeAPI.GetAllNetworks(0, 5)
	common.FailIfErr(t, err)
	common.Json(networks, err).Equals(t, `
{
	"count": 2,
	"list": [
		{
			"networkClass": 1,
			"chainId": 1234,
			"name": "ZnnTestnet",
			"contractAddress": "0x423b5d4c32345ced77393b3530b1eed0f346429d",
			"metadata": "{}",
			"tokenPairs": []
		},
		{
			"networkClass": 2,
			"chainId": 123,
			"name": "Ethereum",
			"contractAddress": "0x323b5d4c32345ced77393b3530b1eed0f346429d",
			"metadata": "{}",
			"tokenPairs": []
		}
	]
}`)
}

func TestBridge_AddRemoveAddNetwork(t *testing.T) {
	z := mock.NewMockZenonWithCustomEpochDuration(t, time.Hour)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).Equals(t, `
t=2001-09-09T01:46:50+0000 lvl=dbug msg=created module=embedded contract=spork spork="&{Id:c6a597f757168bd5c9fddf52b16b3bf38e2ef781fb8edeea1bf2ae0d3225230d Name:spork-bridge Description:activate spork for bridge Activated:false EnforcementHeight:0}"
t=2001-09-09T01:47:00+0000 lvl=dbug msg=activated module=embedded contract=spork spork="&{Id:c6a597f757168bd5c9fddf52b16b3bf38e2ef781fb8edeea1bf2ae0d3225230d Name:spork-bridge Description:activate spork for bridge Activated:true EnforcementHeight:9}"
`)
	activateBridge(t, z)
	bridgeAPI := embedded.NewBridgeApi(z)
	networkClass := uint32(2) // evm
	chainId := uint32(123)
	networkName := "Ethereum"
	contractAddress := "0x323b5d4c32345ced77393b3530b1eed0f346425d"

	addNetwork(t, z, networkClass, chainId, networkName, contractAddress, "{}")

	networkInfo, err := bridgeAPI.GetNetworkInfo(networkClass, chainId)
	common.FailIfErr(t, err)
	common.Json(networkInfo, err).Equals(t, `
{
	"networkClass": 2,
	"chainId": 123,
	"name": "Ethereum",
	"contractAddress": "0x323b5d4c32345ced77393b3530b1eed0f346425d",
	"metadata": "{}",
	"tokenPairs": []
}`)

	addTokenPair(t, z, networkClass, chainId, types.ZnnTokenStandard, "0x5FbDB2315678afecb367f032d93F642f64180aa3", true, true, true,
		big.NewInt(5789), uint32(24), uint32(10), "{}")

	networkInfo, err = bridgeAPI.GetNetworkInfo(networkClass, chainId)
	common.FailIfErr(t, err)

	common.Json(networkInfo, err).Equals(t, `
{
	"networkClass": 2,
	"chainId": 123,
	"name": "Ethereum",
	"contractAddress": "0x323b5d4c32345ced77393b3530b1eed0f346425d",
	"metadata": "{}",
	"tokenPairs": [
		{
			"tokenStandard": "zts1znnxxxxxxxxxxxxx9z4ulx",
			"tokenAddress": "0x5FbDB2315678afecb367f032d93F642f64180aa3",
			"bridgeable": true,
			"redeemable": true,
			"owned": true,
			"minAmount": 5789,
			"feePercentage": 24,
			"redeemDelay": 10,
			"metadata": "{}"
		}
	]
}`)

	defer z.CallContract(&nom.AccountBlock{
		Address:       g.User5.Address,
		ToAddress:     types.BridgeContract,
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(0),
		Data: definition.ABIBridge.PackMethodPanic(definition.RemoveNetworkMethodName,
			networkClass,
			chainId,
		),
	}).Error(t, nil)
	z.InsertNewMomentum() // cemented send block
	z.InsertNewMomentum() // cemented token-receive-block

	networkInfo, err = bridgeAPI.GetNetworkInfo(networkClass, chainId)
	common.FailIfErr(t, err)

	common.Json(networkInfo, err).Equals(t, `
{
	"networkClass": 0,
	"chainId": 0,
	"name": "",
	"contractAddress": "",
	"metadata": "{}",
	"tokenPairs": null
}`)

	addNetwork(t, z, networkClass, chainId, networkName, contractAddress, "{}")

	networkInfo, err = bridgeAPI.GetNetworkInfo(networkClass, chainId)
	common.FailIfErr(t, err)
	common.Json(networkInfo, err).Equals(t, `
{
	"networkClass": 2,
	"chainId": 123,
	"name": "Ethereum",
	"contractAddress": "0x323b5d4c32345ced77393b3530b1eed0f346425d",
	"metadata": "{}",
	"tokenPairs": []
}`)
}

func unwrapToken(z mock.MockZenon, t *testing.T) {
	networkClass := uint32(2) // evm
	chainId := uint32(31337)
	tokenAddress := "0x5FbDB2315678afecb367f032d93F642f64180aa3"

	amount := big.NewInt(100 * 1e8)
	hash := types.HexToHashPanic("0123456789012345678901234567890123456789012345678901234567890123")
	unwrapVar := &definition.UnwrapTokenParam{
		NetworkClass:    networkClass,
		ChainId:         chainId,
		TransactionHash: hash,
		LogIndex:        5,
		ToAddress:       g.User2.Address,
		TokenAddress:    tokenAddress,
		Amount:          amount,
	}
	message, err := implementation.GetUnwrapTokenRequestMessage(unwrapVar)
	common.FailIfErr(t, err)
	signature, err := Sign(message, "tuSwrTEUyJI1/3y5J8L8DSjzT/AQG2IK3JG+93qhhhI=")
	common.FailIfErr(t, err)

	defer z.CallContract(&nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     types.BridgeContract,
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(0),
		Data: definition.ABIBridge.PackMethodPanic(definition.UnwrapTokenMethodName,
			networkClass,
			chainId,
			hash,            // TransactionHash
			uint32(5),       // LogIndex
			g.User2.Address, // ToAddress
			tokenAddress,    // TokenAddress
			amount,          // Amount
			signature,
		),
	}).Error(t, nil)

	z.InsertNewMomentum() // cemented send block
	z.InsertNewMomentum() // cemented token-receive-block

	bridgeAPI := embedded.NewBridgeApi(z)
	unwrapRequests, err := bridgeAPI.GetAllUnwrapTokenRequests(0, 10)
	common.Json(unwrapRequests, err).HideHashes().Equals(t, `
{
	"count": 1,
	"list": [
		{
			"registrationMomentumHeight": 99,
			"networkClass": 2,
			"chainId": 31337,
			"transactionHash": "XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX",
			"logIndex": 5,
			"toAddress": "z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx",
			"tokenAddress": "0x5FbDB2315678afecb367f032d93F642f64180aa3",
			"amount": 10000000000,
			"signature": "wzYSqwcLr8EUIgaKu/2NGxg8uSLQrVIyjtc/HxplRFkKYhwRNDiM9/8g3ErRE8ulLQFXgSjo4ByV++t+NJkihwA=",
			"redeemed": 0,
			"revoked": 0,
			"token": {
				"name": "Zenon Coin",
				"symbol": "ZNN",
				"domain": "zenon.network",
				"totalSupply": 19500000000000,
				"decimals": 8,
				"owner": "z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg",
				"tokenStandard": "zts1znnxxxxxxxxxxxxx9z4ulx",
				"maxSupply": 4611686018427387903,
				"isBurnable": true,
				"isMintable": true,
				"isUtility": true
			},
			"redeemableIn": 39
		}
	]
}`)
}

func generateKeysAndAddress() (string, string, []byte) {
	privateKey, err := crypto.GenerateKey()
	common.DealWithErr(err)
	privateKeyBytes := crypto.FromECDSA(privateKey)
	publicKey := privateKey.Public()
	publicKeyECDSA, _ := publicKey.(*ecdsa.PublicKey)
	publicKeyBytes := crypto.FromECDSAPub(publicKeyECDSA)
	address := crypto.PubkeyToAddress(*publicKeyECDSA).Bytes()
	return hexutil.Encode(privateKeyBytes), base64.StdEncoding.EncodeToString(publicKeyBytes), address
}

func wrapToken(z mock.MockZenon, t *testing.T) {
	bridgeAPI := embedded.NewBridgeApi(z)
	//address := []byte{43, 80, 192, 173, 21, 180, 157, 231, 112, 3, 20, 93, 189, 154, 20, 167, 142, 66, 99, 32}
	networkClass := uint32(2) // evm
	chainId := uint32(31337)

	defer z.CallContract(&nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     types.BridgeContract,
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(150 * 1e8),
		Data: definition.ABIBridge.PackMethodPanic(definition.WrapTokenMethodName,
			networkClass,
			chainId,
			"0xb794f5ea0ba39494ce839613fffba74279579268", // ToAddress
		),
	}).Error(t, nil)
	z.InsertNewMomentum() // cemented send block
	z.InsertNewMomentum() // cemented token-receive-block
	wrapRequests, err := bridgeAPI.GetAllWrapTokenRequests(0, 5)
	common.Json(wrapRequests, err).HideHashes().Equals(t, `
{
	"count": 1,
	"list": [
		{
			"networkClass": 2,
			"chainId": 31337,
			"id": "XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX",
			"toAddress": "0xb794f5ea0ba39494ce839613fffba74279579268",
			"tokenStandard": "zts1znnxxxxxxxxxxxxx9z4ulx",
			"tokenAddress": "0x5FbDB2315678afecb367f032d93F642f64180aa3",
			"amount": 15000000000,
			"fee": 15000000,
			"signature": "",
			"creationMomentumHeight": 101,
			"token": {
				"name": "Zenon Coin",
				"symbol": "ZNN",
				"domain": "zenon.network",
				"totalSupply": 19485015000000,
				"decimals": 8,
				"owner": "z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg",
				"tokenStandard": "zts1znnxxxxxxxxxxxxx9z4ulx",
				"maxSupply": 4611686018427387903,
				"isBurnable": true,
				"isMintable": true,
				"isUtility": true
			},
			"confirmationsToFinality": 14
		}
	]
}`)
	z.InsertNewMomentum()
	common.Json(bridgeAPI.GetAllWrapTokenRequests(0, 5)).HideHashes().Equals(t, `
{
	"count": 1,
	"list": [
		{
			"networkClass": 2,
			"chainId": 31337,
			"id": "XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX",
			"toAddress": "0xb794f5ea0ba39494ce839613fffba74279579268",
			"tokenStandard": "zts1znnxxxxxxxxxxxxx9z4ulx",
			"tokenAddress": "0x5FbDB2315678afecb367f032d93F642f64180aa3",
			"amount": 15000000000,
			"fee": 15000000,
			"signature": "",
			"creationMomentumHeight": 101,
			"token": {
				"name": "Zenon Coin",
				"symbol": "ZNN",
				"domain": "zenon.network",
				"totalSupply": 19485015000000,
				"decimals": 8,
				"owner": "z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg",
				"tokenStandard": "zts1znnxxxxxxxxxxxxx9z4ulx",
				"maxSupply": 4611686018427387903,
				"isBurnable": true,
				"isMintable": true,
				"isUtility": true
			},
			"confirmationsToFinality": 13
		}
	]
}`)
	common.Json(bridgeAPI.GetFeeTokenPair(types.ZnnTokenStandard)).Equals(t, `
{
	"tokenStandard": "zts1znnxxxxxxxxxxxxx9z4ulx",
	"accumulatedFee": 15000000
}`)
}

func updateWrap(z mock.MockZenon, t *testing.T) {
	bridgeAPI := embedded.NewBridgeApi(z)
	//address := []byte{43, 80, 192, 173, 21, 180, 157, 231, 112, 3, 20, 93, 189, 154, 20, 167, 142, 66, 99, 32}

	defer z.CallContract(&nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     types.BridgeContract,
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(50),
		Data: definition.ABIBridge.PackMethodPanic(definition.WrapTokenMethodName,
			"Ethereum", // Network name
			"b794f5ea0ba39494ce839613fffba74279579268", // ToAddress
		),
	}).Error(t, nil)
	z.InsertNewMomentum() // cemented send block
	z.InsertNewMomentum() // cemented token-receive-block
	wrapRequests, err := bridgeAPI.GetAllWrapTokenRequests(0, 5)
	common.Json(wrapRequests, err).Equals(t, `
{
	"count": 1,
	"list": [
		{
			"id": "cbe71cb113d7cbb02587266103e889853c2e7de0de0a03250de2e9a0f49215c8",
			"network": "Ethereum",
			"toAddress": "b794f5ea0ba39494ce839613fffba74279579268",
			"tokenAddress": "0x5FbDB2315678afecb367f032d93F642f64180aa3",
			"amount": 50,
			"signature": ""
		}
	]
}`)
}

func TestBridge_UnwrapToken(t *testing.T) {
	z := mock.NewMockZenonWithCustomEpochDuration(t, time.Hour)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).Equals(t, `
t=2001-09-09T01:46:50+0000 lvl=dbug msg=created module=embedded contract=spork spork="&{Id:c6a597f757168bd5c9fddf52b16b3bf38e2ef781fb8edeea1bf2ae0d3225230d Name:spork-bridge Description:activate spork for bridge Activated:false EnforcementHeight:0}"
t=2001-09-09T01:47:00+0000 lvl=dbug msg=activated module=embedded contract=spork spork="&{Id:c6a597f757168bd5c9fddf52b16b3bf38e2ef781fb8edeea1bf2ae0d3225230d Name:spork-bridge Description:activate spork for bridge Activated:true EnforcementHeight:9}"
`)
	activateBridge(t, z)
	addNetworkInfo(t, z)
	unwrapToken(z, t)
	bridgeAPI := embedded.NewBridgeApi(z)

	requests, err := bridgeAPI.GetAllUnwrapTokenRequests(0, 5)
	common.Json(requests, err).Error(t, nil)
	common.Json(requests, err).HideHashes().Equals(t, `
{
	"count": 1,
	"list": [
		{
			"registrationMomentumHeight": 99,
			"networkClass": 2,
			"chainId": 31337,
			"transactionHash": "XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX",
			"logIndex": 5,
			"toAddress": "z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx",
			"tokenAddress": "0x5FbDB2315678afecb367f032d93F642f64180aa3",
			"amount": 10000000000,
			"signature": "wzYSqwcLr8EUIgaKu/2NGxg8uSLQrVIyjtc/HxplRFkKYhwRNDiM9/8g3ErRE8ulLQFXgSjo4ByV++t+NJkihwA=",
			"redeemed": 0,
			"revoked": 0,
			"token": {
				"name": "Zenon Coin",
				"symbol": "ZNN",
				"domain": "zenon.network",
				"totalSupply": 19500000000000,
				"decimals": 8,
				"owner": "z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg",
				"tokenStandard": "zts1znnxxxxxxxxxxxxx9z4ulx",
				"maxSupply": 4611686018427387903,
				"isBurnable": true,
				"isMintable": true,
				"isUtility": true
			},
			"redeemableIn": 39
		}
	]
}`)
}

func TestBridge_UnwrapAndWrapToken(t *testing.T) {
	z := mock.NewMockZenonWithCustomEpochDuration(t, time.Hour)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).Equals(t, `
t=2001-09-09T01:46:50+0000 lvl=dbug msg=created module=embedded contract=spork spork="&{Id:c6a597f757168bd5c9fddf52b16b3bf38e2ef781fb8edeea1bf2ae0d3225230d Name:spork-bridge Description:activate spork for bridge Activated:false EnforcementHeight:0}"
t=2001-09-09T01:47:00+0000 lvl=dbug msg=activated module=embedded contract=spork spork="&{Id:c6a597f757168bd5c9fddf52b16b3bf38e2ef781fb8edeea1bf2ae0d3225230d Name:spork-bridge Description:activate spork for bridge Activated:true EnforcementHeight:9}"
t=2001-09-09T02:03:30+0000 lvl=dbug msg="burned ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg TokenName:Zenon Coin TokenSymbol:ZNN TokenDomain:zenon.network TotalSupply:+19485015000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1znnxxxxxxxxxxxxx9z4ulx}" burned-amount=14985000000
`)
	activateBridge(t, z)
	addNetworkInfo(t, z)
	unwrapToken(z, t)
	wrapToken(z, t)
	updateWrapToken(z, t)
}

func updateWrapToken(z mock.MockZenon, t *testing.T) {
	contractAddress := ecommon.HexToAddress("0x323b5d4c32345ced77393b3530b1eed0f346429d")
	bridgeAPI := embedded.NewBridgeApi(z)
	wrapRequests, err := bridgeAPI.GetAllWrapTokenRequests(0, 5)
	common.DealWithErr(err)

	wrapReqVar := &definition.WrapTokenRequest{
		NetworkClass:  wrapRequests.List[0].NetworkClass,
		ChainId:       wrapRequests.List[0].ChainId,
		Id:            wrapRequests.List[0].Id,
		ToAddress:     wrapRequests.List[0].ToAddress,
		TokenStandard: wrapRequests.List[0].TokenStandard,
		TokenAddress:  wrapRequests.List[0].TokenAddress,
		Amount:        wrapRequests.List[0].Amount,
		Fee:           wrapRequests.List[0].Fee,
	}
	message, err := implementation.GetWrapTokenRequestMessage(wrapReqVar, &contractAddress)
	common.FailIfErr(t, err)
	signature, err := Sign(message, "tuSwrTEUyJI1/3y5J8L8DSjzT/AQG2IK3JG+93qhhhI=")
	common.FailIfErr(t, err)

	defer z.CallContract(&nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     types.BridgeContract,
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(50),
		Data: definition.ABIBridge.PackMethodPanic(definition.UpdateWrapRequestMethodName,
			wrapRequests.List[0].Id,
			signature,
		),
	}).Error(t, nil)
	z.InsertNewMomentum() // cemented send block
	z.InsertNewMomentum() // cemented token-receive-block

	common.Json(bridgeAPI.GetAllWrapTokenRequests(0, 5)).HideHashes().Equals(t, `
{
	"count": 1,
	"list": [
		{
			"networkClass": 2,
			"chainId": 31337,
			"id": "XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX",
			"toAddress": "0xb794f5ea0ba39494ce839613fffba74279579268",
			"tokenStandard": "zts1znnxxxxxxxxxxxxx9z4ulx",
			"tokenAddress": "0x5FbDB2315678afecb367f032d93F642f64180aa3",
			"amount": 15000000000,
			"fee": 15000000,
			"signature": "7Z8mo4Sit1B9SigFcYY85mQWe9757EkdEzmdXPy3Xf16L0cwg/78dp1IKCTTiFLqJ4VBdEJEo8QnbK1QPNJ1fgA=",
			"creationMomentumHeight": 101,
			"token": {
				"name": "Zenon Coin",
				"symbol": "ZNN",
				"domain": "zenon.network",
				"totalSupply": 19485015000000,
				"decimals": 8,
				"owner": "z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg",
				"tokenStandard": "zts1znnxxxxxxxxxxxxx9z4ulx",
				"maxSupply": 4611686018427387903,
				"isBurnable": true,
				"isMintable": true,
				"isUtility": true
			},
			"confirmationsToFinality": 11
		}
	]
}`)
}

func signMessage() []byte {
	key, err := crypto.GenerateKey()
	fmt.Println("Key: ", key)
	common.DealWithErr(err)

	fmt.Println("PubKey x: ", base64.StdEncoding.EncodeToString(key.PublicKey.X.Bytes()))
	newKeyBytes := make([]byte, 0)
	if key.PublicKey.Y.Mod(key.PublicKey.Y, big.NewInt(2)).Cmp(big.NewInt(0)) == 0 {
		newKeyBytes = append(newKeyBytes, 2)
	} else {
		newKeyBytes = append(newKeyBytes, 3)
	}
	fmt.Println(len(newKeyBytes))
	newKeyBytes = append(newKeyBytes, key.PublicKey.X.Bytes()...)
	fmt.Println(len(newKeyBytes))
	fmt.Println("PubKey: ", base64.StdEncoding.EncodeToString(newKeyBytes))
	d := key.D.Bytes()
	prvKey := base64.StdEncoding.EncodeToString(d)
	fmt.Println("PrivKey: ", prvKey)

	bytes, err := hexutil.Decode("0xc590f9f6ad6e751b2ca7ca89558e7804876a9c5cc56eb8309d62725cd2ef585c")
	common.DealWithErr(err)

	seconKey, err := crypto.ToECDSA(bytes)
	fmt.Println(seconKey)

	fmt.Println(seconKey.PublicKey)
	//prvKeyBytes := privateKey.D.Bytes()
	//privateKeyECDSA, err := crypto.HexToECDSA(prvKey)
	//fmt.Println(privateKeyECDSA)
	//fmt.Println("Error: ", err)
	//fmt.Println("PrivateKey: ", prvKey)
	return nil
}

// strip0x remove the 0x prefix, which is not important to us
func strip0x(s string) string {
	if strings.HasPrefix(s, "0x") {
		s = s[2:]
	}
	return s
}

// ---- Main functions ----

func Sign(hash []byte, privateKey string) (string, error) {
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

func TestBridge_UpdateWrapToken(t *testing.T) {
	z := mock.NewMockZenonWithCustomEpochDuration(t, time.Hour)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).Equals(t, `
t=2001-09-09T01:46:50+0000 lvl=dbug msg=created module=embedded contract=spork spork="&{Id:c6a597f757168bd5c9fddf52b16b3bf38e2ef781fb8edeea1bf2ae0d3225230d Name:spork-bridge Description:activate spork for bridge Activated:false EnforcementHeight:0}"
t=2001-09-09T01:47:00+0000 lvl=dbug msg=activated module=embedded contract=spork spork="&{Id:c6a597f757168bd5c9fddf52b16b3bf38e2ef781fb8edeea1bf2ae0d3225230d Name:spork-bridge Description:activate spork for bridge Activated:true EnforcementHeight:9}"
t=2001-09-09T02:03:30+0000 lvl=dbug msg="burned ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg TokenName:Zenon Coin TokenSymbol:ZNN TokenDomain:zenon.network TotalSupply:+19485015000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1znnxxxxxxxxxxxxx9z4ulx}" burned-amount=14985000000
t=2001-09-09T02:10:00+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg TokenName:Zenon Coin TokenSymbol:ZNN TokenDomain:zenon.network TotalSupply:+19495015000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1znnxxxxxxxxxxxxx9z4ulx}" minted-amount=10000000000 to-address=z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx
`)
	bridgeAPI := embedded.NewBridgeApi(z)
	activateBridge(t, z)
	addNetworkInfo(t, z)
	unwrapToken(z, t)
	wrapToken(z, t)
	hash, err := types.HexToHash("0123456789012345678901234567890123456789012345678901234567890123")
	common.DealWithErr(err)
	unwrapToken, err := bridgeAPI.GetUnwrapTokenRequestByHashAndLog(hash, 0)
	common.DealWithErr(err)
	ledgerApi := api.NewLedgerApi(z)
	frontierMomentum, err := ledgerApi.GetFrontierMomentum()
	common.FailIfErr(t, err)
	defer z.CallContract(&nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     types.BridgeContract,
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(0),
		Data: definition.ABIBridge.PackMethodPanic(definition.RedeemUnwrapMethodName,
			hash,
			uint32(0),
		),
	}).Error(t, constants.ErrInvalidRedeemPeriod)
	z.InsertNewMomentum() // cemented send block
	z.InsertNewMomentum() // cemented token-receive-block

	z.InsertMomentumsTo(frontierMomentum.Height + unwrapToken.RedeemableIn)
	z.ExpectBalance(g.User2.Address, types.ZnnTokenStandard, 800000000000)
	defer z.CallContract(&nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     types.BridgeContract,
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(0),
		Data: definition.ABIBridge.PackMethodPanic(definition.RedeemUnwrapMethodName,
			hash,
			uint32(0),
		),
	}).Error(t, nil)
	z.InsertNewMomentum() // cemented send block
	z.InsertNewMomentum() // cemented token-receive-block
	autoreceive(t, z, g.User2.Address)
	z.ExpectBalance(g.User2.Address, types.ZnnTokenStandard, 800000000000)
	z.InsertNewMomentum() // cemented send block
	z.InsertNewMomentum() // cemented token-receive-block
	autoreceive(t, z, g.User2.Address)
	z.ExpectBalance(g.User2.Address, types.ZnnTokenStandard, 810000000000)
	unwrapRequests, err := bridgeAPI.GetAllUnwrapTokenRequests(0, 10)
	common.Json(unwrapRequests, err).Equals(t, `
{
	"count": 1,
	"list": [
		{
			"registrationMomentumHeight": 99,
			"networkClass": 2,
			"chainId": 31337,
			"transactionHash": "0123456789012345678901234567890123456789012345678901234567890123",
			"logIndex": 0,
			"toAddress": "z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx",
			"tokenAddress": "0x5FbDB2315678afecb367f032d93F642f64180aa3",
			"amount": 10000000000,
			"signature": "wzYSqwcLr8EUIgaKu/2NGxg8uSLQrVIyjtc/HxplRFkKYhwRNDiM9/8g3ErRE8ulLQFXgSjo4ByV++t+NJkihwA=",
			"redeemed": 1,
			"revoked": 0,
			"token": {
				"name": "Zenon Coin",
				"symbol": "ZNN",
				"domain": "zenon.network",
				"totalSupply": 19495015000000,
				"decimals": 8,
				"owner": "z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg",
				"tokenStandard": "zts1znnxxxxxxxxxxxxx9z4ulx",
				"maxSupply": 4611686018427387903,
				"isBurnable": true,
				"isMintable": true,
				"isUtility": true
			},
			"redeemableIn": 0
		}
	]
}`)
}

func multipleWrapToken(z mock.MockZenon, t *testing.T) {
	networkClass := uint32(2) // evm
	chainId := uint32(31337)
	name := "Ethereum"
	contractAddress := "0x323b5d4c32345ced77393b3530b1eed0f346429d"

	addNetwork(t, z, networkClass, chainId, name, contractAddress, "{}")

	addTokenPair(t, z, networkClass, chainId, types.ZnnTokenStandard, "0x5FbDB2315678afecb367f032d93F642f64180aa3", true, true, true,
		big.NewInt(9876), uint32(20), uint32(40), "{}")

	for i := 1; i < 20; i++ {
		defer z.CallContract(&nom.AccountBlock{
			Address:       g.User1.Address,
			ToAddress:     types.BridgeContract,
			TokenStandard: types.ZnnTokenStandard,
			Amount:        big.NewInt(int64(i * 1e8)),
			Data: definition.ABIBridge.PackMethodPanic(definition.WrapTokenMethodName,
				networkClass,
				chainId,
				"0xb794f5ea0ba39494ce839613fffba74279579268", // ToAddress
			),
		}).Error(t, nil)
		z.InsertNewMomentum() // cemented send block
		z.InsertNewMomentum() // cemented token-receive-block
	}
}

func TestBridge_MultipleWrapToken(t *testing.T) {
	z := mock.NewMockZenonWithCustomEpochDuration(t, time.Hour)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).Equals(t, `
t=2001-09-09T01:46:50+0000 lvl=dbug msg=created module=embedded contract=spork spork="&{Id:c6a597f757168bd5c9fddf52b16b3bf38e2ef781fb8edeea1bf2ae0d3225230d Name:spork-bridge Description:activate spork for bridge Activated:false EnforcementHeight:0}"
t=2001-09-09T01:47:00+0000 lvl=dbug msg=activated module=embedded contract=spork spork="&{Id:c6a597f757168bd5c9fddf52b16b3bf38e2ef781fb8edeea1bf2ae0d3225230d Name:spork-bridge Description:activate spork for bridge Activated:true EnforcementHeight:9}"
t=2001-09-09T02:03:10+0000 lvl=dbug msg="burned ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg TokenName:Zenon Coin TokenSymbol:ZNN TokenDomain:zenon.network TotalSupply:+19499900200000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1znnxxxxxxxxxxxxx9z4ulx}" burned-amount=99800000
t=2001-09-09T02:03:30+0000 lvl=dbug msg="burned ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg TokenName:Zenon Coin TokenSymbol:ZNN TokenDomain:zenon.network TotalSupply:+19499700600000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1znnxxxxxxxxxxxxx9z4ulx}" burned-amount=199600000
t=2001-09-09T02:03:50+0000 lvl=dbug msg="burned ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg TokenName:Zenon Coin TokenSymbol:ZNN TokenDomain:zenon.network TotalSupply:+19499401200000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1znnxxxxxxxxxxxxx9z4ulx}" burned-amount=299400000
t=2001-09-09T02:04:10+0000 lvl=dbug msg="burned ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg TokenName:Zenon Coin TokenSymbol:ZNN TokenDomain:zenon.network TotalSupply:+19499002000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1znnxxxxxxxxxxxxx9z4ulx}" burned-amount=399200000
t=2001-09-09T02:04:30+0000 lvl=dbug msg="burned ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg TokenName:Zenon Coin TokenSymbol:ZNN TokenDomain:zenon.network TotalSupply:+19498503000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1znnxxxxxxxxxxxxx9z4ulx}" burned-amount=499000000
t=2001-09-09T02:04:50+0000 lvl=dbug msg="burned ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg TokenName:Zenon Coin TokenSymbol:ZNN TokenDomain:zenon.network TotalSupply:+19497904200000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1znnxxxxxxxxxxxxx9z4ulx}" burned-amount=598800000
t=2001-09-09T02:05:10+0000 lvl=dbug msg="burned ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg TokenName:Zenon Coin TokenSymbol:ZNN TokenDomain:zenon.network TotalSupply:+19497205600000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1znnxxxxxxxxxxxxx9z4ulx}" burned-amount=698600000
t=2001-09-09T02:05:30+0000 lvl=dbug msg="burned ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg TokenName:Zenon Coin TokenSymbol:ZNN TokenDomain:zenon.network TotalSupply:+19496407200000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1znnxxxxxxxxxxxxx9z4ulx}" burned-amount=798400000
t=2001-09-09T02:05:50+0000 lvl=dbug msg="burned ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg TokenName:Zenon Coin TokenSymbol:ZNN TokenDomain:zenon.network TotalSupply:+19495509000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1znnxxxxxxxxxxxxx9z4ulx}" burned-amount=898200000
t=2001-09-09T02:06:10+0000 lvl=dbug msg="burned ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg TokenName:Zenon Coin TokenSymbol:ZNN TokenDomain:zenon.network TotalSupply:+19494511000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1znnxxxxxxxxxxxxx9z4ulx}" burned-amount=998000000
t=2001-09-09T02:06:30+0000 lvl=dbug msg="burned ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg TokenName:Zenon Coin TokenSymbol:ZNN TokenDomain:zenon.network TotalSupply:+19493413200000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1znnxxxxxxxxxxxxx9z4ulx}" burned-amount=1097800000
t=2001-09-09T02:06:50+0000 lvl=dbug msg="burned ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg TokenName:Zenon Coin TokenSymbol:ZNN TokenDomain:zenon.network TotalSupply:+19492215600000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1znnxxxxxxxxxxxxx9z4ulx}" burned-amount=1197600000
t=2001-09-09T02:07:10+0000 lvl=dbug msg="burned ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg TokenName:Zenon Coin TokenSymbol:ZNN TokenDomain:zenon.network TotalSupply:+19490918200000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1znnxxxxxxxxxxxxx9z4ulx}" burned-amount=1297400000
t=2001-09-09T02:07:30+0000 lvl=dbug msg="burned ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg TokenName:Zenon Coin TokenSymbol:ZNN TokenDomain:zenon.network TotalSupply:+19489521000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1znnxxxxxxxxxxxxx9z4ulx}" burned-amount=1397200000
t=2001-09-09T02:07:50+0000 lvl=dbug msg="burned ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg TokenName:Zenon Coin TokenSymbol:ZNN TokenDomain:zenon.network TotalSupply:+19488024000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1znnxxxxxxxxxxxxx9z4ulx}" burned-amount=1497000000
t=2001-09-09T02:08:10+0000 lvl=dbug msg="burned ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg TokenName:Zenon Coin TokenSymbol:ZNN TokenDomain:zenon.network TotalSupply:+19486427200000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1znnxxxxxxxxxxxxx9z4ulx}" burned-amount=1596800000
t=2001-09-09T02:08:30+0000 lvl=dbug msg="burned ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg TokenName:Zenon Coin TokenSymbol:ZNN TokenDomain:zenon.network TotalSupply:+19484730600000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1znnxxxxxxxxxxxxx9z4ulx}" burned-amount=1696600000
t=2001-09-09T02:08:50+0000 lvl=dbug msg="burned ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg TokenName:Zenon Coin TokenSymbol:ZNN TokenDomain:zenon.network TotalSupply:+19482934200000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1znnxxxxxxxxxxxxx9z4ulx}" burned-amount=1796400000
t=2001-09-09T02:09:10+0000 lvl=dbug msg="burned ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg TokenName:Zenon Coin TokenSymbol:ZNN TokenDomain:zenon.network TotalSupply:+19481038000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1znnxxxxxxxxxxxxx9z4ulx}" burned-amount=1896200000
`)
	activateBridge(t, z)
	multipleWrapToken(z, t)
	bridgeAPI := embedded.NewBridgeApi(z)

	requests, err := bridgeAPI.GetAllWrapTokenRequests(0, 3)
	common.Json(requests, err).Error(t, nil)
	common.Json(requests, err).HideHashes().Equals(t, `
{
	"count": 19,
	"list": [
		{
			"networkClass": 2,
			"chainId": 31337,
			"id": "XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX",
			"toAddress": "0xb794f5ea0ba39494ce839613fffba74279579268",
			"tokenStandard": "zts1znnxxxxxxxxxxxxx9z4ulx",
			"tokenAddress": "0x5FbDB2315678afecb367f032d93F642f64180aa3",
			"amount": 1900000000,
			"fee": 3800000,
			"signature": "",
			"creationMomentumHeight": 135,
			"token": {
				"name": "Zenon Coin",
				"symbol": "ZNN",
				"domain": "zenon.network",
				"totalSupply": 19481038000000,
				"decimals": 8,
				"owner": "z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg",
				"tokenStandard": "zts1znnxxxxxxxxxxxxx9z4ulx",
				"maxSupply": 4611686018427387903,
				"isBurnable": true,
				"isMintable": true,
				"isUtility": true
			},
			"confirmationsToFinality": 14
		},
		{
			"networkClass": 2,
			"chainId": 31337,
			"id": "XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX",
			"toAddress": "0xb794f5ea0ba39494ce839613fffba74279579268",
			"tokenStandard": "zts1znnxxxxxxxxxxxxx9z4ulx",
			"tokenAddress": "0x5FbDB2315678afecb367f032d93F642f64180aa3",
			"amount": 1800000000,
			"fee": 3600000,
			"signature": "",
			"creationMomentumHeight": 133,
			"token": {
				"name": "Zenon Coin",
				"symbol": "ZNN",
				"domain": "zenon.network",
				"totalSupply": 19481038000000,
				"decimals": 8,
				"owner": "z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg",
				"tokenStandard": "zts1znnxxxxxxxxxxxxx9z4ulx",
				"maxSupply": 4611686018427387903,
				"isBurnable": true,
				"isMintable": true,
				"isUtility": true
			},
			"confirmationsToFinality": 12
		},
		{
			"networkClass": 2,
			"chainId": 31337,
			"id": "XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX",
			"toAddress": "0xb794f5ea0ba39494ce839613fffba74279579268",
			"tokenStandard": "zts1znnxxxxxxxxxxxxx9z4ulx",
			"tokenAddress": "0x5FbDB2315678afecb367f032d93F642f64180aa3",
			"amount": 1700000000,
			"fee": 3400000,
			"signature": "",
			"creationMomentumHeight": 131,
			"token": {
				"name": "Zenon Coin",
				"symbol": "ZNN",
				"domain": "zenon.network",
				"totalSupply": 19481038000000,
				"decimals": 8,
				"owner": "z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg",
				"tokenStandard": "zts1znnxxxxxxxxxxxxx9z4ulx",
				"maxSupply": 4611686018427387903,
				"isBurnable": true,
				"isMintable": true,
				"isUtility": true
			},
			"confirmationsToFinality": 10
		}
	]
}`)
	requests, err = bridgeAPI.GetAllUnsignedWrapTokenRequests(5, 3)
	common.Json(requests, err).Error(t, nil)
	common.Json(requests, err).HideHashes().Equals(t, `
{
	"count": 19,
	"list": [
		{
			"networkClass": 2,
			"chainId": 31337,
			"id": "XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX",
			"toAddress": "0xb794f5ea0ba39494ce839613fffba74279579268",
			"tokenStandard": "zts1znnxxxxxxxxxxxxx9z4ulx",
			"tokenAddress": "0x5FbDB2315678afecb367f032d93F642f64180aa3",
			"amount": 1600000000,
			"fee": 3200000,
			"signature": "",
			"creationMomentumHeight": 129,
			"token": {
				"name": "Zenon Coin",
				"symbol": "ZNN",
				"domain": "zenon.network",
				"totalSupply": 19481038000000,
				"decimals": 8,
				"owner": "z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg",
				"tokenStandard": "zts1znnxxxxxxxxxxxxx9z4ulx",
				"maxSupply": 4611686018427387903,
				"isBurnable": true,
				"isMintable": true,
				"isUtility": true
			},
			"confirmationsToFinality": 8
		},
		{
			"networkClass": 2,
			"chainId": 31337,
			"id": "XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX",
			"toAddress": "0xb794f5ea0ba39494ce839613fffba74279579268",
			"tokenStandard": "zts1znnxxxxxxxxxxxxx9z4ulx",
			"tokenAddress": "0x5FbDB2315678afecb367f032d93F642f64180aa3",
			"amount": 1700000000,
			"fee": 3400000,
			"signature": "",
			"creationMomentumHeight": 131,
			"token": {
				"name": "Zenon Coin",
				"symbol": "ZNN",
				"domain": "zenon.network",
				"totalSupply": 19481038000000,
				"decimals": 8,
				"owner": "z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg",
				"tokenStandard": "zts1znnxxxxxxxxxxxxx9z4ulx",
				"maxSupply": 4611686018427387903,
				"isBurnable": true,
				"isMintable": true,
				"isUtility": true
			},
			"confirmationsToFinality": 10
		},
		{
			"networkClass": 2,
			"chainId": 31337,
			"id": "XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX",
			"toAddress": "0xb794f5ea0ba39494ce839613fffba74279579268",
			"tokenStandard": "zts1znnxxxxxxxxxxxxx9z4ulx",
			"tokenAddress": "0x5FbDB2315678afecb367f032d93F642f64180aa3",
			"amount": 1800000000,
			"fee": 3600000,
			"signature": "",
			"creationMomentumHeight": 133,
			"token": {
				"name": "Zenon Coin",
				"symbol": "ZNN",
				"domain": "zenon.network",
				"totalSupply": 19481038000000,
				"decimals": 8,
				"owner": "z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg",
				"tokenStandard": "zts1znnxxxxxxxxxxxxx9z4ulx",
				"maxSupply": 4611686018427387903,
				"isBurnable": true,
				"isMintable": true,
				"isUtility": true
			},
			"confirmationsToFinality": 12
		}
	]
}`)
	common.Json(bridgeAPI.GetFeeTokenPair(types.ZnnTokenStandard)).Equals(t, `
{
	"tokenStandard": "zts1znnxxxxxxxxxxxxx9z4ulx",
	"accumulatedFee": 38000000
}`)
}

func init() {
	rand.Seed(0)
}

var letterRunes = []rune("abcdef0123456789")

func RandStringRunes(n int) string {
	b := make([]rune, n)
	for i := range b {
		b[i] = letterRunes[rand.Intn(len(letterRunes))]
	}
	return string(b)
}

func multipleUnwrapToken(z mock.MockZenon, t *testing.T) {
	networkClass := uint32(2) // evm
	chainId := uint32(31337)
	name := "Ethereum"
	contractAddress := "0x323b5d4c32345ced77393b3530b1eed0f346429d"
	tokenAddress := "0x5FbDB2315678afecb367f032d93F642f64180aa3"
	addNetwork(t, z, networkClass, chainId, name, contractAddress, "{}")

	addTokenPair(t, z, networkClass, chainId, types.ZnnTokenStandard, tokenAddress, true, true, false,
		big.NewInt(1478), uint32(96), uint32(10), "{}")

	rand.Seed(123456)
	for i := 1; i < 20; i++ {
		amount := big.NewInt(int64(i * 1e8))
		hash := types.HexToHashPanic(RandStringRunes(64))
		unwrapVar := &definition.UnwrapTokenParam{
			NetworkClass:    networkClass,
			ChainId:         chainId,
			TransactionHash: hash,
			LogIndex:        uint32(i),
			ToAddress:       g.User2.Address,
			TokenAddress:    tokenAddress,
			Amount:          amount,
		}

		message, err := implementation.GetUnwrapTokenRequestMessage(unwrapVar)
		common.FailIfErr(t, err)
		singature, err := Sign(message, "tuSwrTEUyJI1/3y5J8L8DSjzT/AQG2IK3JG+93qhhhI=")
		common.FailIfErr(t, err)

		defer z.CallContract(&nom.AccountBlock{
			Address:       g.User1.Address,
			ToAddress:     types.BridgeContract,
			TokenStandard: types.ZnnTokenStandard,
			Amount:        big.NewInt(0),
			Data: definition.ABIBridge.PackMethodPanic(definition.UnwrapTokenMethodName,
				networkClass,
				chainId,
				unwrapVar.TransactionHash, // TransactionHash
				unwrapVar.LogIndex,
				g.User2.Address,        // ToAddress
				unwrapVar.TokenAddress, // TokenAddress
				unwrapVar.Amount,       // Amount
				singature,
			),
		}).Error(t, nil)

		z.InsertNewMomentum() // cemented send block
		z.InsertNewMomentum() // cemented token-receive-block
	}
}

func TestBridge_MultipleUnwrapToken(t *testing.T) {
	z := mock.NewMockZenonWithCustomEpochDuration(t, time.Hour)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).Equals(t, `
t=2001-09-09T01:46:50+0000 lvl=dbug msg=created module=embedded contract=spork spork="&{Id:c6a597f757168bd5c9fddf52b16b3bf38e2ef781fb8edeea1bf2ae0d3225230d Name:spork-bridge Description:activate spork for bridge Activated:false EnforcementHeight:0}"
t=2001-09-09T01:47:00+0000 lvl=dbug msg=activated module=embedded contract=spork spork="&{Id:c6a597f757168bd5c9fddf52b16b3bf38e2ef781fb8edeea1bf2ae0d3225230d Name:spork-bridge Description:activate spork for bridge Activated:true EnforcementHeight:9}"
`)
	activateBridge(t, z)
	multipleUnwrapToken(z, t)
	bridgeAPI := embedded.NewBridgeApi(z)

	requests, err := bridgeAPI.GetAllUnwrapTokenRequests(0, 3)
	common.Json(requests, err).Error(t, nil)
	common.Json(requests, err).HideHashes().Equals(t, `        
{
	"count": 19,
	"list": [
		{
			"registrationMomentumHeight": 135,
			"networkClass": 2,
			"chainId": 31337,
			"transactionHash": "XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX",
			"logIndex": 19,
			"toAddress": "z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx",
			"tokenAddress": "0x5FbDB2315678afecb367f032d93F642f64180aa3",
			"amount": 1900000000,
			"signature": "8w2voLF6YNhkYy07wqFswXXJkVx7NTTVeOHH9fJJct1CSX1otk8XX7SsP74hK9ZRsESn0QCqMxc2j3biNHW2zQA=",
			"redeemed": 0,
			"revoked": 0,
			"token": {
				"name": "Zenon Coin",
				"symbol": "ZNN",
				"domain": "zenon.network",
				"totalSupply": 19500000000000,
				"decimals": 8,
				"owner": "z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg",
				"tokenStandard": "zts1znnxxxxxxxxxxxxx9z4ulx",
				"maxSupply": 4611686018427387903,
				"isBurnable": true,
				"isMintable": true,
				"isUtility": true
			},
			"redeemableIn": 9
		},
		{
			"registrationMomentumHeight": 133,
			"networkClass": 2,
			"chainId": 31337,
			"transactionHash": "XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX",
			"logIndex": 18,
			"toAddress": "z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx",
			"tokenAddress": "0x5FbDB2315678afecb367f032d93F642f64180aa3",
			"amount": 1800000000,
			"signature": "1cxpcON6OxidSngt3kiM7uvJdJYtAGK+S2SGyGbw33wbBu+nvrJRvuMTqz3FstO8OyUge3TRLQ53pTJuutskrgE=",
			"redeemed": 0,
			"revoked": 0,
			"token": {
				"name": "Zenon Coin",
				"symbol": "ZNN",
				"domain": "zenon.network",
				"totalSupply": 19500000000000,
				"decimals": 8,
				"owner": "z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg",
				"tokenStandard": "zts1znnxxxxxxxxxxxxx9z4ulx",
				"maxSupply": 4611686018427387903,
				"isBurnable": true,
				"isMintable": true,
				"isUtility": true
			},
			"redeemableIn": 7
		},
		{
			"registrationMomentumHeight": 131,
			"networkClass": 2,
			"chainId": 31337,
			"transactionHash": "XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX",
			"logIndex": 17,
			"toAddress": "z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx",
			"tokenAddress": "0x5FbDB2315678afecb367f032d93F642f64180aa3",
			"amount": 1700000000,
			"signature": "XLVmPpzATtLH2WqmJuEpirRLixx+sx4uoJtCps8e3NBUXmPQCen1jIn5tZ0tu2vTNUMNCiFqSuGDtBDouzBsgwA=",
			"redeemed": 0,
			"revoked": 0,
			"token": {
				"name": "Zenon Coin",
				"symbol": "ZNN",
				"domain": "zenon.network",
				"totalSupply": 19500000000000,
				"decimals": 8,
				"owner": "z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg",
				"tokenStandard": "zts1znnxxxxxxxxxxxxx9z4ulx",
				"maxSupply": 4611686018427387903,
				"isBurnable": true,
				"isMintable": true,
				"isUtility": true
			},
			"redeemableIn": 5
		}
	]
}`)

	requests, err = bridgeAPI.GetAllUnwrapTokenRequests(5, 3)
	common.Json(requests, err).Error(t, nil)
	common.Json(requests, err).HideHashes().Equals(t, `
{
	"count": 19,
	"list": [
		{
			"registrationMomentumHeight": 105,
			"networkClass": 2,
			"chainId": 31337,
			"transactionHash": "XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX",
			"logIndex": 0,
			"toAddress": "z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx",
			"tokenAddress": "0x5FbDB2315678afecb367f032d93F642f64180aa3",
			"amount": 400000000,
			"signature": "cC8aY3BI8jt4Jh+jBtCtcoNhiI4U6WVp1vCKsY632f9LP6gIV40gIdn0dyRo3FvLEPl0d5l/YiZgi9QFdThyjwE=",
			"redeemed": 0,
			"revoked": 0,
			"token": {
				"name": "Zenon Coin",
				"symbol": "ZNN",
				"domain": "zenon.network",
				"totalSupply": 19500000000000,
				"decimals": 8,
				"owner": "z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg",
				"tokenStandard": "zts1znnxxxxxxxxxxxxx9z4ulx",
				"maxSupply": 4611686018427387903,
				"isBurnable": true,
				"isMintable": true,
				"isUtility": true
			},
			"redeemableIn": 0
		},
		{
			"registrationMomentumHeight": 103,
			"networkClass": 2,
			"chainId": 31337,
			"transactionHash": "XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX",
			"logIndex": 0,
			"toAddress": "z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx",
			"tokenAddress": "0x5FbDB2315678afecb367f032d93F642f64180aa3",
			"amount": 300000000,
			"signature": "dZwto03/t5HlfwmRDKng8CKvFQrA87baAoXkX/mIPExjZLdLLe4ouF/UW7aIvXl1pLGkYd8JM7vFCGmoxivvoQE=",
			"redeemed": 0,
			"revoked": 0,
			"token": {
				"name": "Zenon Coin",
				"symbol": "ZNN",
				"domain": "zenon.network",
				"totalSupply": 19500000000000,
				"decimals": 8,
				"owner": "z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg",
				"tokenStandard": "zts1znnxxxxxxxxxxxxx9z4ulx",
				"maxSupply": 4611686018427387903,
				"isBurnable": true,
				"isMintable": true,
				"isUtility": true
			},
			"redeemableIn": 0
		},
		{
			"registrationMomentumHeight": 101,
			"networkClass": 2,
			"chainId": 31337,
			"transactionHash": "XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX",
			"logIndex": 0,
			"toAddress": "z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx",
			"tokenAddress": "0x5FbDB2315678afecb367f032d93F642f64180aa3",
			"amount": 200000000,
			"signature": "gS1OvdW7uL7S2yZ+iANszF6srBVUiSn0B1lB4OPlbsRe2+ly5gA08UA76Ao4CTPrLkQyxa1X36JPc+/XmdlydAE=",
			"redeemed": 0,
			"revoked": 0,
			"token": {
				"name": "Zenon Coin",
				"symbol": "ZNN",
				"domain": "zenon.network",
				"totalSupply": 19500000000000,
				"decimals": 8,
				"owner": "z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg",
				"tokenStandard": "zts1znnxxxxxxxxxxxxx9z4ulx",
				"maxSupply": 4611686018427387903,
				"isBurnable": true,
				"isMintable": true,
				"isUtility": true
			},
			"redeemableIn": 0
		}
	]
}`)
}

func TestBridge_IsJson(t *testing.T) {
	content := `{"contractAddress":"0x0834eFec9672e5953fbda13B36339A726fBcaA8F","contractDeploymentHeight":7790553,"estimatedBlockTime":12,"confirmationsToFinality":5}`
	assert.True(t, implementation.IsJSON(content))
}

func TestBridge_UpdateNetworkMetadata(t *testing.T) {
	z := mock.NewMockZenonWithCustomEpochDuration(t, time.Hour)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).Equals(t, `
t=2001-09-09T01:46:50+0000 lvl=dbug msg=created module=embedded contract=spork spork="&{Id:c6a597f757168bd5c9fddf52b16b3bf38e2ef781fb8edeea1bf2ae0d3225230d Name:spork-bridge Description:activate spork for bridge Activated:false EnforcementHeight:0}"
t=2001-09-09T01:47:00+0000 lvl=dbug msg=activated module=embedded contract=spork spork="&{Id:c6a597f757168bd5c9fddf52b16b3bf38e2ef781fb8edeea1bf2ae0d3225230d Name:spork-bridge Description:activate spork for bridge Activated:true EnforcementHeight:9}"
`)
	activateBridge(t, z)
	bridgeAPI := embedded.NewBridgeApi(z)

	networkClass := uint32(2) // evm
	chainId := uint32(123)
	name := "Ethereum"
	contractAddress := "0x323b5d4c32345ced77393b3530b1eed0f346429d"

	addNetwork(t, z, networkClass, chainId, name, contractAddress, "{}")

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

	defer z.CallContract(&nom.AccountBlock{
		Address:       g.User5.Address,
		ToAddress:     types.BridgeContract,
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(0),
		Data: definition.ABIBridge.PackMethodPanic(definition.UpdateNetworkMetadataMethodName,
			networkClass, // evm
			chainId,
			`{"contractAddress":"0x0834eFec9672e5953fbda13B36339A726fBcaA8F","contractDeploymentHeight":7790553,"estimatedBlockTime":12,"confirmationsToFinality":5}`,
		),
	}).Error(t, nil)
	z.InsertNewMomentum() // cemented send block
	z.InsertNewMomentum() // cemented token-receive-block
	common.Json(bridgeAPI.GetNetworkInfo(networkClass, chainId)).Equals(t, `
{
	"networkClass": 2,
	"chainId": 123,
	"name": "Ethereum",
	"contractAddress": "0x323b5d4c32345ced77393b3530b1eed0f346429d",
	"metadata": "{\"contractAddress\":\"0x0834eFec9672e5953fbda13B36339A726fBcaA8F\",\"contractDeploymentHeight\":7790553,\"estimatedBlockTime\":12,\"confirmationsToFinality\":5}",
	"tokenPairs": []
}`)

}

func TestBridge_Halt(t *testing.T) {
	z := mock.NewMockZenonWithCustomEpochDuration(t, time.Hour)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).Equals(t, `
t=2001-09-09T01:46:50+0000 lvl=dbug msg=created module=embedded contract=spork spork="&{Id:c6a597f757168bd5c9fddf52b16b3bf38e2ef781fb8edeea1bf2ae0d3225230d Name:spork-bridge Description:activate spork for bridge Activated:false EnforcementHeight:0}"
t=2001-09-09T01:47:00+0000 lvl=dbug msg=activated module=embedded contract=spork spork="&{Id:c6a597f757168bd5c9fddf52b16b3bf38e2ef781fb8edeea1bf2ae0d3225230d Name:spork-bridge Description:activate spork for bridge Activated:true EnforcementHeight:9}"
`)
	activateBridge(t, z)
	networkClass := uint32(2) // evm
	chainId := uint32(31337)

	defer z.CallContract(&nom.AccountBlock{
		Address:       g.User5.Address,
		ToAddress:     types.BridgeContract,
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(0),
		Data: definition.ABIBridge.PackMethodPanic(definition.HaltMethodName,
			""),
	}).Error(t, nil)
	z.InsertNewMomentum() // cemented send block
	z.InsertNewMomentum() // cemented token-receive-block
	bridgeAPI := embedded.NewBridgeApi(z)

	common.Json(bridgeAPI.GetBridgeInfo()).Equals(t, `
{
	"administratorEDDSAPubKey": "CyJWr3uy4niOIG1BDGjmurWOo/czaiErqb0EpZLrMAQ=",
	"compressedTssECDSAPubKey": "AsAQx1M3LVXCuozDOqO5b9adj/PItYgwZFG/xTDBiZzT",
	"decompressedTssECDSAPubKey": "BMAQx1M3LVXCuozDOqO5b9adj/PItYgwZFG/xTDBiZzTnQAT1qOPAkuPzu6yoewss9XbnTmZmb9JQNGXmkPYtK4=",
	"allowKeyGen": false,
	"halted": true,
	"unhaltedAt": 0,
	"unhaltDurationInMomentums": 35,
	"tssNonce": 0,
	"metadata": "{}"
}`)
	defer z.CallContract(&nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     types.BridgeContract,
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(150 * 1e8),
		Data: definition.ABIBridge.PackMethodPanic(definition.WrapTokenMethodName,
			networkClass,
			chainId,
			"0xb794f5ea0ba39494ce839613fffba74279579268", // ToAddress
		),
	}).Error(t, constants.ErrBridgeHalted)
	z.InsertNewMomentum() // cemented send block
	z.InsertNewMomentum() // cemented token-receive-block
}

type NetworkInfoParam struct {
	Type       uint32   `json:"networkClass"`
	Id         uint32   `json:"chainId"`
	Name       string   `json:"name"`
	Metadata   string   `json:"metadata"`
	TokenPairs [][]byte `json:"tokenPairs"`
}

func TestBridge_CheckABI(t *testing.T) {
	json := `[
		{"type":"variable","name":"networkInfo","inputs":[
			{"name":"type","type":"uint32"},
			{"name":"id","type":"uint32"},
			{"name":"name","type":"string"},
			{"name":"metadata","type":"string"},
			{"name":"tokenPairs","type":"bytes[]"}
		]}
	]`
	testAbi := abi.JSONToABIContract(strings.NewReader(json))
	name := "networkInfo"
	pairs := make([][]byte, 1)
	bridgeInfo := &definition.BridgeInfoVariable{
		AllowKeyGen:              true,
		CompressedTssECDSAPubKey: "ECDSA",
		AdministratorEDDSAPubKey: constants.InitialBridgeAdministratorPubKey,
		TssNonce:                 3,
		Metadata:                 "{}",
		Halted:                   true,
	}

	bridgeInfoBytes, err := definition.ABIBridge.PackVariable("bridgeInfo",
		bridgeInfo.AllowKeyGen,
		bridgeInfo.UnhaltedAt,
		bridgeInfo.CompressedTssECDSAPubKey,
		bridgeInfo.AdministratorEDDSAPubKey,
		bridgeInfo.TssNonce,
		bridgeInfo.UnhaltDurationInMomentums,
		bridgeInfo.Metadata,
		bridgeInfo.Halted)
	fmt.Println(bridgeInfoBytes, err)
	pairs[0] = bridgeInfoBytes
	fmt.Println(pairs)
	packedData := testAbi.PackVariablePanic(
		name,
		uint32(2),
		uint32(3),
		"test",
		"{}",
		pairs,
	)
	newVariable := new(NetworkInfoParam)
	if err := testAbi.UnpackVariable(newVariable, "networkInfo", packedData); err != nil {
		fmt.Println(err)
	} else {
		fmt.Println(newVariable)
	}

	bridge := new(definition.BridgeInfoVariable)
	if err := definition.ABIBridge.UnpackVariable(bridge, "bridgeInfo", newVariable.TokenPairs[0]); err != nil {
		fmt.Println(err)
	} else {
		fmt.Println(bridge)
	}
}

func TestBridge_WrapOwnedToken(t *testing.T) {
	z := mock.NewMockZenonWithCustomEpochDuration(t, time.Hour)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).Equals(t, `
t=2001-09-09T01:46:50+0000 lvl=dbug msg=created module=embedded contract=spork spork="&{Id:c6a597f757168bd5c9fddf52b16b3bf38e2ef781fb8edeea1bf2ae0d3225230d Name:spork-bridge Description:activate spork for bridge Activated:false EnforcementHeight:0}"
t=2001-09-09T01:47:00+0000 lvl=dbug msg=activated module=embedded contract=spork spork="&{Id:c6a597f757168bd5c9fddf52b16b3bf38e2ef781fb8edeea1bf2ae0d3225230d Name:spork-bridge Description:activate spork for bridge Activated:true EnforcementHeight:9}"
t=2001-09-09T02:03:10+0000 lvl=dbug msg="burned ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg TokenName:Zenon Coin TokenSymbol:ZNN TokenDomain:zenon.network TotalSupply:+19485022500000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1znnxxxxxxxxxxxxx9z4ulx}" burned-amount=14977500000
`)
	activateBridge(t, z)
	tokenAPI := embedded.NewTokenApi(z)

	networkClass := uint32(2) // evm
	chainId := uint32(123)
	name := "Ethereum"
	contractAddress := "0x323b5d4c32345ced77393b3530b1eed0f346429d"
	addNetwork(t, z, networkClass, chainId, name, contractAddress, "{}")
	addTokenPair(t, z, networkClass, chainId, types.ZnnTokenStandard, "0x5FbDB2315678afecb367f032d93F642f64180aa3", true, true, true,
		big.NewInt(100), uint32(15), uint32(20), `{"APR": 15, "LockingPeriod": 100}`)

	z.ExpectBalance(g.User1.Address, types.ZnnTokenStandard, 1200000000000)
	z.ExpectBalance(g.User5.Address, types.ZnnTokenStandard, 50000000000)
	z.ExpectBalance(types.BridgeContract, types.ZnnTokenStandard, 0)

	common.Json(tokenAPI.GetByZts(types.ZnnTokenStandard)).Equals(t, `
{
	"name": "Zenon Coin",
	"symbol": "ZNN",
	"domain": "zenon.network",
	"totalSupply": 19500000000000,
	"decimals": 8,
	"owner": "z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg",
	"tokenStandard": "zts1znnxxxxxxxxxxxxx9z4ulx",
	"maxSupply": 4611686018427387903,
	"isBurnable": true,
	"isMintable": true,
	"isUtility": true
}`)

	defer z.CallContract(&nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     types.BridgeContract,
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(150 * 1e8),
		Data: definition.ABIBridge.PackMethodPanic(definition.WrapTokenMethodName,
			networkClass,
			chainId,
			"0xb794f5ea0ba39494ce839613fffba74279579268", // ToAddress
		),
	}).Error(t, nil)
	z.InsertNewMomentum() // cemented send block
	z.InsertNewMomentum() // cemented token-receive-block

	z.ExpectBalance(g.User1.Address, types.ZnnTokenStandard, 1185000000000)
	z.ExpectBalance(g.User5.Address, types.ZnnTokenStandard, 50000000000)
	// todo check that bridge balance is still 0, or actually the same as before, maybe someone donated or smth
	common.Json(tokenAPI.GetByZts(types.ZnnTokenStandard)).Equals(t, `
{
	"name": "Zenon Coin",
	"symbol": "ZNN",
	"domain": "zenon.network",
	"totalSupply": 19485022500000,
	"decimals": 8,
	"owner": "z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg",
	"tokenStandard": "zts1znnxxxxxxxxxxxxx9z4ulx",
	"maxSupply": 4611686018427387903,
	"isBurnable": true,
	"isMintable": true,
	"isUtility": true
}`)
}

func TestBridge_WrapNonOwnedToken(t *testing.T) {
	z := mock.NewMockZenonWithCustomEpochDuration(t, time.Hour)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).Equals(t, `
t=2001-09-09T01:46:50+0000 lvl=dbug msg=created module=embedded contract=spork spork="&{Id:c6a597f757168bd5c9fddf52b16b3bf38e2ef781fb8edeea1bf2ae0d3225230d Name:spork-bridge Description:activate spork for bridge Activated:false EnforcementHeight:0}"
t=2001-09-09T01:47:00+0000 lvl=dbug msg=activated module=embedded contract=spork spork="&{Id:c6a597f757168bd5c9fddf52b16b3bf38e2ef781fb8edeea1bf2ae0d3225230d Name:spork-bridge Description:activate spork for bridge Activated:true EnforcementHeight:9}"
`)
	activateBridge(t, z)
	tokenAPI := embedded.NewTokenApi(z)

	networkClass := uint32(2) // evm
	chainId := uint32(123)
	name := "Ethereum"
	contractAddress := "0x323b5d4c32345ced77393b3530b1eed0f346429d"
	addNetwork(t, z, networkClass, chainId, name, contractAddress, "{}")
	addTokenPair(t, z, networkClass, chainId, types.ZnnTokenStandard, "0x5FbDB2315678afecb367f032d93F642f64180aa3", true, true, false,
		big.NewInt(100), uint32(15), uint32(20), `{"APR": 15, "LockingPeriod": 100}`)

	z.ExpectBalance(g.User1.Address, types.ZnnTokenStandard, 1200000000000)
	z.ExpectBalance(g.User5.Address, types.ZnnTokenStandard, 50000000000)
	z.ExpectBalance(types.BridgeContract, types.ZnnTokenStandard, 0)

	common.Json(tokenAPI.GetByZts(types.ZnnTokenStandard)).Equals(t, `
{
	"name": "Zenon Coin",
	"symbol": "ZNN",
	"domain": "zenon.network",
	"totalSupply": 19500000000000,
	"decimals": 8,
	"owner": "z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg",
	"tokenStandard": "zts1znnxxxxxxxxxxxxx9z4ulx",
	"maxSupply": 4611686018427387903,
	"isBurnable": true,
	"isMintable": true,
	"isUtility": true
}`)

	defer z.CallContract(&nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     types.BridgeContract,
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(150 * 1e8),
		Data: definition.ABIBridge.PackMethodPanic(definition.WrapTokenMethodName,
			networkClass,
			chainId,
			"0xb794f5ea0ba39494ce839613fffba74279579268", // ToAddress
		),
	}).Error(t, nil)
	z.InsertNewMomentum() // cemented send block
	z.InsertNewMomentum() // cemented token-receive-block

	z.ExpectBalance(g.User1.Address, types.ZnnTokenStandard, 1185000000000)
	z.ExpectBalance(g.User5.Address, types.ZnnTokenStandard, 50000000000)
	// todo check that bridge balance of this zts is the amount the user wrapped
	common.Json(tokenAPI.GetByZts(types.ZnnTokenStandard)).Equals(t, `
{
	"name": "Zenon Coin",
	"symbol": "ZNN",
	"domain": "zenon.network",
	"totalSupply": 19500000000000,
	"decimals": 8,
	"owner": "z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg",
	"tokenStandard": "zts1znnxxxxxxxxxxxxx9z4ulx",
	"maxSupply": 4611686018427387903,
	"isBurnable": true,
	"isMintable": true,
	"isUtility": true
}`)
}

func TestBridge_RedeemNonOwnedToken(t *testing.T) {
	z := mock.NewMockZenonWithCustomEpochDuration(t, time.Hour)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).Equals(t, `
t=2001-09-09T01:46:50+0000 lvl=dbug msg=created module=embedded contract=spork spork="&{Id:c6a597f757168bd5c9fddf52b16b3bf38e2ef781fb8edeea1bf2ae0d3225230d Name:spork-bridge Description:activate spork for bridge Activated:false EnforcementHeight:0}"
t=2001-09-09T01:47:00+0000 lvl=dbug msg=activated module=embedded contract=spork spork="&{Id:c6a597f757168bd5c9fddf52b16b3bf38e2ef781fb8edeea1bf2ae0d3225230d Name:spork-bridge Description:activate spork for bridge Activated:true EnforcementHeight:9}"
`)
	bridgeAPI := embedded.NewBridgeApi(z)
	activateBridge(t, z)
	networkClass := uint32(2) // evm
	chainId := uint32(31337)
	networkName := "Ethereum"
	tokenAddress := "0x5FbDB2315678afecb367f032d93F642f64180aa3"
	contractAddress := "0x323b5d4c32345ced77393b3530b1eed0f346429d"

	addNetwork(t, z, networkClass, chainId, networkName, contractAddress, "{}")
	addTokenPair(t, z, networkClass, chainId, types.ZnnTokenStandard, tokenAddress, true, true, false,
		big.NewInt(1000), uint32(10), uint32(40), `{ "contractAddress":"aaa", "contractDeploymentHeight": 100, "estimatedBlockTime": 200, "confirmationsToFinality": 300, "decimals": 8 }`)

	unwrapToken(z, t)
	defer z.CallContract(&nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     types.BridgeContract,
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(150 * 1e8),
		Data: definition.ABIBridge.PackMethodPanic(definition.WrapTokenMethodName,
			networkClass,
			chainId,
			"0xb794f5ea0ba39494ce839613fffba74279579268", // ToAddress
		),
	}).Error(t, nil)
	z.InsertNewMomentum() // cemented send block
	z.InsertNewMomentum() // cemented token-receive-block
	hash, err := types.HexToHash("0123456789012345678901234567890123456789012345678901234567890123")
	common.DealWithErr(err)
	unwrapToken, err := bridgeAPI.GetUnwrapTokenRequestByHashAndLog(hash, 0)
	common.DealWithErr(err)
	ledgerApi := api.NewLedgerApi(z)
	frontierMomentum, err := ledgerApi.GetFrontierMomentum()
	common.FailIfErr(t, err)
	z.InsertMomentumsTo(frontierMomentum.Height + unwrapToken.RedeemableIn)
	z.ExpectBalance(g.User2.Address, types.ZnnTokenStandard, 800000000000)
	z.ExpectBalance(types.BridgeContract, types.ZnnTokenStandard, 15000000000)
	defer z.CallContract(&nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     types.BridgeContract,
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(0),
		Data: definition.ABIBridge.PackMethodPanic(definition.RedeemUnwrapMethodName,
			hash,
			uint32(0),
		),
	}).Error(t, nil)
	z.InsertNewMomentum() // cemented send block
	z.InsertNewMomentum() // cemented token-receive-block
	autoreceive(t, z, g.User2.Address)
	z.ExpectBalance(g.User2.Address, types.ZnnTokenStandard, 810000000000)
	z.ExpectBalance(types.BridgeContract, types.ZnnTokenStandard, 5000000000)
	unwrapRequests, err := bridgeAPI.GetAllUnwrapTokenRequests(0, 10)
	common.Json(unwrapRequests, err).Equals(t, `
{
	"count": 1,
	"list": [
		{
			"registrationMomentumHeight": 99,
			"networkClass": 2,
			"chainId": 31337,
			"transactionHash": "0123456789012345678901234567890123456789012345678901234567890123",
			"logIndex": 0,
			"toAddress": "z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx",
			"tokenAddress": "0x5FbDB2315678afecb367f032d93F642f64180aa3",
			"amount": 10000000000,
			"signature": "wzYSqwcLr8EUIgaKu/2NGxg8uSLQrVIyjtc/HxplRFkKYhwRNDiM9/8g3ErRE8ulLQFXgSjo4ByV++t+NJkihwA=",
			"redeemed": 1,
			"revoked": 0,
			"token": {
				"name": "Zenon Coin",
				"symbol": "ZNN",
				"domain": "zenon.network",
				"totalSupply": 19500000000000,
				"decimals": 8,
				"owner": "z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg",
				"tokenStandard": "zts1znnxxxxxxxxxxxxx9z4ulx",
				"maxSupply": 4611686018427387903,
				"isBurnable": true,
				"isMintable": true,
				"isUtility": true
			},
			"redeemableIn": 0
		}
	]
}`)
}

func TestBridge_RedeemOwnedToken(t *testing.T) {
	z := mock.NewMockZenonWithCustomEpochDuration(t, time.Hour)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).Equals(t, `
t=2001-09-09T01:46:50+0000 lvl=dbug msg=created module=embedded contract=spork spork="&{Id:c6a597f757168bd5c9fddf52b16b3bf38e2ef781fb8edeea1bf2ae0d3225230d Name:spork-bridge Description:activate spork for bridge Activated:false EnforcementHeight:0}"
t=2001-09-09T01:47:00+0000 lvl=dbug msg=activated module=embedded contract=spork spork="&{Id:c6a597f757168bd5c9fddf52b16b3bf38e2ef781fb8edeea1bf2ae0d3225230d Name:spork-bridge Description:activate spork for bridge Activated:true EnforcementHeight:9}"
t=2001-09-09T02:02:20+0000 lvl=dbug msg="issued ZTS" module=embedded contract=token token="{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenName:test.tok3n_na-m3 TokenSymbol:TEST TokenDomain: TotalSupply:+1000 MaxSupply:+10000 Decimals:1 IsMintable:true IsBurnable:true IsUtility:false TokenStandard:zts10u47l27k874s2lec33m6ww}"
t=2001-09-09T02:02:40+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenName:test.tok3n_na-m3 TokenSymbol:TEST TokenDomain: TotalSupply:+1100 MaxSupply:+10000 Decimals:1 IsMintable:true IsBurnable:true IsUtility:false TokenStandard:zts10u47l27k874s2lec33m6ww}" minted-amount=100 to-address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz
`)
	bridgeAPI := embedded.NewBridgeApi(z)
	tokenAPI := embedded.NewTokenApi(z)
	activateBridge(t, z)
	networkClass := uint32(2) // evm
	chainId := uint32(31337)
	networkName := "Ethereum"
	tokenAddress := "0x5FbDB2315678afecb367f032d93F642f64180aa3"
	contractAddress := "0x323b5d4c32345ced77393b3530b1eed0f346429d"

	defer z.CallContract(&nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     types.TokenContract,
		TokenStandard: types.ZnnTokenStandard,
		Amount:        constants.TokenIssueAmount,
		Data: definition.ABIToken.PackMethodPanic(definition.IssueMethodName,
			"test.tok3n_na-m3", //param.TokenName
			"TEST",             //param.TokenSymbol
			"",                 //param.TokenDomain
			big.NewInt(1000),   //param.TotalSupply
			big.NewInt(10000),  //param.MaxSupply
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

	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.TokenContract,
		Data:      definition.ABIToken.PackMethodPanic(definition.MintMethodName, tokenList.List[0].ZenonTokenStandard, big.NewInt(100), g.User1.Address),
	}).Error(t, nil)
	z.InsertNewMomentum()
	z.InsertNewMomentum()

	autoreceive(t, z, g.User1.Address)
	z.ExpectBalance(g.User1.Address, tokenList.List[0].ZenonTokenStandard, 1100)

	addNetwork(t, z, networkClass, chainId, networkName, contractAddress, "{}")
	addTokenPair(t, z, networkClass, chainId, tokenList.List[0].ZenonTokenStandard, tokenAddress, true, true, false,
		big.NewInt(1), uint32(10), uint32(40), `{ "contractAddress":"aaa", "contractDeploymentHeight": 100, "estimatedBlockTime": 200, "confirmationsToFinality": 300, "decimals": 1 }`)

	amount := big.NewInt(3)
	hash := types.HexToHashPanic("0123456789012345678901234567890123456789012345678901234567890123")
	unwrapVar := &definition.UnwrapTokenParam{
		NetworkClass:    networkClass,
		ChainId:         chainId,
		TransactionHash: hash,
		ToAddress:       g.User2.Address,
		TokenAddress:    tokenAddress,
		Amount:          amount,
	}
	message, err := implementation.GetUnwrapTokenRequestMessage(unwrapVar)
	common.FailIfErr(t, err)
	signature, err := Sign(message, "tuSwrTEUyJI1/3y5J8L8DSjzT/AQG2IK3JG+93qhhhI=")
	common.FailIfErr(t, err)

	defer z.CallContract(&nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     types.BridgeContract,
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(0),
		Data: definition.ABIBridge.PackMethodPanic(definition.UnwrapTokenMethodName,
			networkClass,
			chainId,
			hash, // TransactionHash
			uint32(0),
			g.User2.Address, // ToAddress
			tokenAddress,    // TokenAddress
			amount,          // Amount
			signature,
		),
	}).Error(t, nil)

	z.InsertNewMomentum() // cemented send block
	z.InsertNewMomentum() // cemented token-receive-block

	unwrapRequests, err := bridgeAPI.GetAllUnwrapTokenRequests(0, 10)
	common.Json(unwrapRequests, err).HideHashes().Equals(t, `
{
	"count": 1,
	"list": [
		{
			"registrationMomentumHeight": 103,
			"networkClass": 2,
			"chainId": 31337,
			"transactionHash": "XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX",
			"logIndex": 0,
			"toAddress": "z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx",
			"tokenAddress": "0x5FbDB2315678afecb367f032d93F642f64180aa3",
			"amount": 3,
			"signature": "+HlcXaZDwMRvCVxqcKdGdfg1ZHkqPcK1M4CAAt2Sh59ghlTUxAE64mXHoJZjuHgNGzKJ8ZJswkaKLBknqjz1hwA=",
			"redeemed": 0,
			"revoked": 0,
			"token": {
				"name": "test.tok3n_na-m3",
				"symbol": "TEST",
				"domain": "",
				"totalSupply": 1100,
				"decimals": 1,
				"owner": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
				"tokenStandard": "zts10u47l27k874s2lec33m6ww",
				"maxSupply": 10000,
				"isBurnable": true,
				"isMintable": true,
				"isUtility": false
			},
			"redeemableIn": 39
		}
	]
}`)
	z.ExpectBalance(g.User1.Address, tokenList.List[0].ZenonTokenStandard, 1100)
	defer z.CallContract(&nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     types.BridgeContract,
		TokenStandard: tokenList.List[0].ZenonTokenStandard,
		Amount:        big.NewInt(3),
		Data: definition.ABIBridge.PackMethodPanic(definition.WrapTokenMethodName,
			networkClass,
			chainId,
			"0xb794f5ea0ba39494ce839613fffba74279579268", // ToAddress
		),
	}).Error(t, nil)
	z.InsertNewMomentum() // cemented send block
	z.InsertNewMomentum() // cemented token-receive-block
	txHash, err := types.HexToHash("0123456789012345678901234567890123456789012345678901234567890123")
	common.DealWithErr(err)
	unwrapToken, err := bridgeAPI.GetUnwrapTokenRequestByHashAndLog(txHash, 0)
	common.DealWithErr(err)
	ledgerApi := api.NewLedgerApi(z)
	frontierMomentum, err := ledgerApi.GetFrontierMomentum()
	common.FailIfErr(t, err)
	z.InsertMomentumsTo(frontierMomentum.Height + unwrapToken.RedeemableIn)
	z.ExpectBalance(g.User2.Address, tokenList.List[0].ZenonTokenStandard, 0)
	z.ExpectBalance(types.BridgeContract, tokenList.List[0].ZenonTokenStandard, 3)
	defer z.CallContract(&nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     types.BridgeContract,
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(0),
		Data: definition.ABIBridge.PackMethodPanic(definition.RedeemUnwrapMethodName,
			hash,
			uint32(0),
		),
	}).Error(t, nil)
	z.InsertNewMomentum() // cemented send block
	z.InsertNewMomentum() // cemented token-receive-block
	autoreceive(t, z, g.User2.Address)
	z.ExpectBalance(g.User2.Address, tokenList.List[0].ZenonTokenStandard, 3)
	z.ExpectBalance(types.BridgeContract, tokenList.List[0].ZenonTokenStandard, 0)
	unwrapRequests, err = bridgeAPI.GetAllUnwrapTokenRequests(0, 10)
	common.Json(unwrapRequests, err).Equals(t, `
{
	"count": 1,
	"list": [
		{
			"registrationMomentumHeight": 103,
			"networkClass": 2,
			"chainId": 31337,
			"transactionHash": "0123456789012345678901234567890123456789012345678901234567890123",
			"logIndex": 0,
			"toAddress": "z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx",
			"tokenAddress": "0x5FbDB2315678afecb367f032d93F642f64180aa3",
			"amount": 3,
			"signature": "+HlcXaZDwMRvCVxqcKdGdfg1ZHkqPcK1M4CAAt2Sh59ghlTUxAE64mXHoJZjuHgNGzKJ8ZJswkaKLBknqjz1hwA=",
			"redeemed": 1,
			"revoked": 0,
			"token": {
				"name": "test.tok3n_na-m3",
				"symbol": "TEST",
				"domain": "",
				"totalSupply": 1100,
				"decimals": 1,
				"owner": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
				"tokenStandard": "zts10u47l27k874s2lec33m6ww",
				"maxSupply": 10000,
				"isBurnable": true,
				"isMintable": true,
				"isUtility": false
			},
			"redeemableIn": 0
		}
	]
}`)
}

func TestBridge_ActivateEmergency(t *testing.T) {
	z := mock.NewMockZenonWithCustomEpochDuration(t, time.Hour)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).Equals(t, `
t=2001-09-09T01:46:50+0000 lvl=dbug msg=created module=embedded contract=spork spork="&{Id:c6a597f757168bd5c9fddf52b16b3bf38e2ef781fb8edeea1bf2ae0d3225230d Name:spork-bridge Description:activate spork for bridge Activated:false EnforcementHeight:0}"
t=2001-09-09T01:47:00+0000 lvl=dbug msg=activated module=embedded contract=spork spork="&{Id:c6a597f757168bd5c9fddf52b16b3bf38e2ef781fb8edeea1bf2ae0d3225230d Name:spork-bridge Description:activate spork for bridge Activated:true EnforcementHeight:9}"
`)
	bridgeAPI := embedded.NewBridgeApi(z)
	activateBridge(t, z)

	defer z.CallContract(&nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     types.BridgeContract,
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(0),
		Data:          definition.ABIBridge.PackMethodPanic(definition.EmergencyMethodName),
	}).Error(t, constants.ErrPermissionDenied)
	z.InsertNewMomentum()
	z.InsertNewMomentum()
	common.Json(bridgeAPI.GetBridgeInfo()).Equals(t, `
{
	"administratorEDDSAPubKey": "CyJWr3uy4niOIG1BDGjmurWOo/czaiErqb0EpZLrMAQ=",
	"compressedTssECDSAPubKey": "AsAQx1M3LVXCuozDOqO5b9adj/PItYgwZFG/xTDBiZzT",
	"decompressedTssECDSAPubKey": "BMAQx1M3LVXCuozDOqO5b9adj/PItYgwZFG/xTDBiZzTnQAT1qOPAkuPzu6yoewss9XbnTmZmb9JQNGXmkPYtK4=",
	"allowKeyGen": false,
	"halted": false,
	"unhaltedAt": 0,
	"unhaltDurationInMomentums": 35,
	"tssNonce": 0,
	"metadata": "{}"
}`)

	defer z.CallContract(&nom.AccountBlock{
		Address:       g.User5.Address,
		ToAddress:     types.BridgeContract,
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(0),
		Data:          definition.ABIBridge.PackMethodPanic(definition.EmergencyMethodName),
	}).Error(t, nil)
	z.InsertNewMomentum()
	z.InsertNewMomentum()

	common.Json(bridgeAPI.GetBridgeInfo()).Equals(t, `
{
	"administratorEDDSAPubKey": "",
	"compressedTssECDSAPubKey": "",
	"decompressedTssECDSAPubKey": "",
	"allowKeyGen": false,
	"halted": true,
	"unhaltedAt": 0,
	"unhaltDurationInMomentums": 35,
	"tssNonce": 0,
	"metadata": "{}"
}`)
}

func TestBridge_ActivateEmergencyAndCheckAdministrator(t *testing.T) {
	z := mock.NewMockZenonWithCustomEpochDuration(t, time.Hour)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).Equals(t, `
t=2001-09-09T01:46:50+0000 lvl=dbug msg=created module=embedded contract=spork spork="&{Id:c6a597f757168bd5c9fddf52b16b3bf38e2ef781fb8edeea1bf2ae0d3225230d Name:spork-bridge Description:activate spork for bridge Activated:false EnforcementHeight:0}"
t=2001-09-09T01:47:00+0000 lvl=dbug msg=activated module=embedded contract=spork spork="&{Id:c6a597f757168bd5c9fddf52b16b3bf38e2ef781fb8edeea1bf2ae0d3225230d Name:spork-bridge Description:activate spork for bridge Activated:true EnforcementHeight:9}"
`)
	activateBridge(t, z)

	defer z.CallContract(&nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     types.BridgeContract,
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(0),
		Data:          definition.ABIBridge.PackMethodPanic(definition.EmergencyMethodName),
	}).Error(t, constants.ErrPermissionDenied)
	z.InsertNewMomentum()
	z.InsertNewMomentum()

	defer z.CallContract(&nom.AccountBlock{
		Address:       g.User5.Address,
		ToAddress:     types.BridgeContract,
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(0),
		Data:          definition.ABIBridge.PackMethodPanic(definition.EmergencyMethodName),
	}).Error(t, nil)
	z.InsertNewMomentum()
	z.InsertNewMomentum()

	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User5.Address,
		ToAddress: types.BridgeContract,
		Data:      definition.ABIBridge.PackMethodPanic(definition.SetUnhaltDurationMethodName, uint64(35)),
	}).Error(t, constants.ErrPermissionDenied)
	z.InsertNewMomentum()
	z.InsertNewMomentum()

	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User5.Address,
		ToAddress: types.BridgeContract,
		Data:      definition.ABIBridge.PackMethodPanic(definition.SetUnhaltDurationMethodName, uint64(35)),
	}).Error(t, constants.ErrPermissionDenied)
	z.InsertNewMomentum()
	z.InsertNewMomentum()

	defer z.CallContract(&nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     types.BridgeContract,
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(150 * 1e8),
		Data: definition.ABIBridge.PackMethodPanic(definition.WrapTokenMethodName,
			uint32(2), // evm
			uint32(31337),
			"0xb794f5ea0ba39494ce839613fffba74279579268", // ToAddress
		),
	}).Error(t, constants.ErrBridgeNotInitialized)
	z.InsertNewMomentum() // cemented send block
	z.InsertNewMomentum() // cemented token-receive-block
}

func TestBridge_ProposeAdministrator(t *testing.T) {
	z := mock.NewMockZenonWithCustomEpochDuration(t, time.Hour)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).Equals(t, `
t=2001-09-09T01:46:50+0000 lvl=dbug msg=created module=embedded contract=spork spork="&{Id:c6a597f757168bd5c9fddf52b16b3bf38e2ef781fb8edeea1bf2ae0d3225230d Name:spork-bridge Description:activate spork for bridge Activated:false EnforcementHeight:0}"
t=2001-09-09T01:47:00+0000 lvl=dbug msg=activated module=embedded contract=spork spork="&{Id:c6a597f757168bd5c9fddf52b16b3bf38e2ef781fb8edeea1bf2ae0d3225230d Name:spork-bridge Description:activate spork for bridge Activated:true EnforcementHeight:9}"
`)
	bridgeAPI := embedded.NewBridgeApi(z)
	activateBridge(t, z)

	defer z.CallContract(&nom.AccountBlock{
		Address:       g.User5.Address,
		ToAddress:     types.BridgeContract,
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(0),
		Data: definition.ABIBridge.PackMethodPanic(definition.ProposeAdministratorMethodName,
			base64.StdEncoding.EncodeToString(g.User5.Public),
		),
	}).Error(t, constants.ErrNotEmergency)
	z.InsertNewMomentum()
	z.InsertNewMomentum()

	defer z.CallContract(&nom.AccountBlock{
		Address:       g.User5.Address,
		ToAddress:     types.BridgeContract,
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(0),
		Data:          definition.ABIBridge.PackMethodPanic(definition.EmergencyMethodName),
	}).Error(t, nil)
	z.InsertNewMomentum()
	z.InsertNewMomentum()

	defer z.CallContract(&nom.AccountBlock{
		Address:       g.User5.Address,
		ToAddress:     types.BridgeContract,
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(0),
		Data: definition.ABIBridge.PackMethodPanic(definition.ProposeAdministratorMethodName,
			base64.StdEncoding.EncodeToString(g.User5.Public),
		),
	}).Error(t, nil)
	z.InsertNewMomentum()
	z.InsertNewMomentum()

	common.Json(bridgeAPI.GetSecurityInfo()).Equals(t, `
{
	"requestedAdministratorPubKey": "",
	"administratorChangeMomentum": 11,
	"administratorDelay": 30,
	"requestedTssPubKey": "",
	"tssChangeMomentum": 70,
	"tssDelay": 21,
	"guardians": [
		"B8uNQ2UGKIG1aMMYo1keh3DBMH0rrlBa4e9+maM8w2E=",
		"CyJWr3uy4niOIG1BDGjmurWOo/czaiErqb0EpZLrMAQ=",
		"GYyn77OXTL31zPbDBCe/eKir+VCF3hv+LxiOUF3XcJY=",
		"i0rZDoLUpl3vCI+5eMbFih7vh3VTWm3u+y2KXqO6Qh0=",
		"tUJu3P7Drp25XP662lIjyFlFpvj8bWUpyC+0y5YTzXM="
	],
	"guardiansVotes": [
		"",
		"CyJWr3uy4niOIG1BDGjmurWOo/czaiErqb0EpZLrMAQ=",
		"",
		"",
		""
	],
	"nominatedGuardians": [],
	"guardiansNominationHeight": 15
}`)
}

func TestBridge_ProposeMultipleAdministrators(t *testing.T) {
	z := mock.NewMockZenonWithCustomEpochDuration(t, time.Hour)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).Equals(t, `
t=2001-09-09T01:46:50+0000 lvl=dbug msg=created module=embedded contract=spork spork="&{Id:c6a597f757168bd5c9fddf52b16b3bf38e2ef781fb8edeea1bf2ae0d3225230d Name:spork-bridge Description:activate spork for bridge Activated:false EnforcementHeight:0}"
t=2001-09-09T01:47:00+0000 lvl=dbug msg=activated module=embedded contract=spork spork="&{Id:c6a597f757168bd5c9fddf52b16b3bf38e2ef781fb8edeea1bf2ae0d3225230d Name:spork-bridge Description:activate spork for bridge Activated:true EnforcementHeight:9}"
`)
	bridgeAPI := embedded.NewBridgeApi(z)
	activateBridge(t, z)

	defer z.CallContract(&nom.AccountBlock{
		Address:       g.User5.Address,
		ToAddress:     types.BridgeContract,
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(0),
		Data: definition.ABIBridge.PackMethodPanic(definition.ProposeAdministratorMethodName,
			base64.StdEncoding.EncodeToString(g.User5.Public),
		),
	}).Error(t, constants.ErrNotEmergency)
	z.InsertNewMomentum()
	z.InsertNewMomentum()

	defer z.CallContract(&nom.AccountBlock{
		Address:       g.User5.Address,
		ToAddress:     types.BridgeContract,
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(0),
		Data:          definition.ABIBridge.PackMethodPanic(definition.EmergencyMethodName),
	}).Error(t, nil)
	z.InsertNewMomentum()
	z.InsertNewMomentum()

	defer z.CallContract(&nom.AccountBlock{
		Address:       g.User5.Address,
		ToAddress:     types.BridgeContract,
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(0),
		Data: definition.ABIBridge.PackMethodPanic(definition.ProposeAdministratorMethodName,
			base64.StdEncoding.EncodeToString(g.User5.Public),
		),
	}).Error(t, nil)
	z.InsertNewMomentum()
	z.InsertNewMomentum()

	common.Json(bridgeAPI.GetSecurityInfo()).Equals(t, `
{
	"requestedAdministratorPubKey": "",
	"administratorChangeMomentum": 11,
	"administratorDelay": 30,
	"requestedTssPubKey": "",
	"tssChangeMomentum": 70,
	"tssDelay": 21,
	"guardians": [
		"B8uNQ2UGKIG1aMMYo1keh3DBMH0rrlBa4e9+maM8w2E=",
		"CyJWr3uy4niOIG1BDGjmurWOo/czaiErqb0EpZLrMAQ=",
		"GYyn77OXTL31zPbDBCe/eKir+VCF3hv+LxiOUF3XcJY=",
		"i0rZDoLUpl3vCI+5eMbFih7vh3VTWm3u+y2KXqO6Qh0=",
		"tUJu3P7Drp25XP662lIjyFlFpvj8bWUpyC+0y5YTzXM="
	],
	"guardiansVotes": [
		"",
		"CyJWr3uy4niOIG1BDGjmurWOo/czaiErqb0EpZLrMAQ=",
		"",
		"",
		""
	],
	"nominatedGuardians": [],
	"guardiansNominationHeight": 15
}`)

	defer z.CallContract(&nom.AccountBlock{
		Address:       g.User5.Address,
		ToAddress:     types.BridgeContract,
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(0),
		Data: definition.ABIBridge.PackMethodPanic(definition.ProposeAdministratorMethodName,
			base64.StdEncoding.EncodeToString(g.User1.Public),
		),
	}).Error(t, nil)
	z.InsertNewMomentum()
	z.InsertNewMomentum()

	common.Json(bridgeAPI.GetSecurityInfo()).Equals(t, `
{
	"requestedAdministratorPubKey": "",
	"administratorChangeMomentum": 11,
	"administratorDelay": 30,
	"requestedTssPubKey": "",
	"tssChangeMomentum": 70,
	"tssDelay": 21,
	"guardians": [
		"B8uNQ2UGKIG1aMMYo1keh3DBMH0rrlBa4e9+maM8w2E=",
		"CyJWr3uy4niOIG1BDGjmurWOo/czaiErqb0EpZLrMAQ=",
		"GYyn77OXTL31zPbDBCe/eKir+VCF3hv+LxiOUF3XcJY=",
		"i0rZDoLUpl3vCI+5eMbFih7vh3VTWm3u+y2KXqO6Qh0=",
		"tUJu3P7Drp25XP662lIjyFlFpvj8bWUpyC+0y5YTzXM="
	],
	"guardiansVotes": [
		"",
		"GYyn77OXTL31zPbDBCe/eKir+VCF3hv+LxiOUF3XcJY=",
		"",
		"",
		""
	],
	"nominatedGuardians": [],
	"guardiansNominationHeight": 15
}`)

	defer z.CallContract(&nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     types.BridgeContract,
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(0),
		Data: definition.ABIBridge.PackMethodPanic(definition.ProposeAdministratorMethodName,
			base64.StdEncoding.EncodeToString(g.User5.Public),
		),
	}).Error(t, nil)
	z.InsertNewMomentum()
	z.InsertNewMomentum()

	defer z.CallContract(&nom.AccountBlock{
		Address:       g.User2.Address,
		ToAddress:     types.BridgeContract,
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(0),
		Data: definition.ABIBridge.PackMethodPanic(definition.ProposeAdministratorMethodName,
			base64.StdEncoding.EncodeToString(g.User3.Public),
		),
	}).Error(t, nil)
	z.InsertNewMomentum()
	z.InsertNewMomentum()

	defer z.CallContract(&nom.AccountBlock{
		Address:       g.User3.Address,
		ToAddress:     types.BridgeContract,
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(0),
		Data: definition.ABIBridge.PackMethodPanic(definition.ProposeAdministratorMethodName,
			base64.StdEncoding.EncodeToString(g.User1.Public),
		),
	}).Error(t, nil)
	z.InsertNewMomentum()
	z.InsertNewMomentum()

	common.Json(bridgeAPI.GetSecurityInfo()).Equals(t, `
{
	"requestedAdministratorPubKey": "",
	"administratorChangeMomentum": 11,
	"administratorDelay": 30,
	"requestedTssPubKey": "",
	"tssChangeMomentum": 70,
	"tssDelay": 21,
	"guardians": [
		"B8uNQ2UGKIG1aMMYo1keh3DBMH0rrlBa4e9+maM8w2E=",
		"CyJWr3uy4niOIG1BDGjmurWOo/czaiErqb0EpZLrMAQ=",
		"GYyn77OXTL31zPbDBCe/eKir+VCF3hv+LxiOUF3XcJY=",
		"i0rZDoLUpl3vCI+5eMbFih7vh3VTWm3u+y2KXqO6Qh0=",
		"tUJu3P7Drp25XP662lIjyFlFpvj8bWUpyC+0y5YTzXM="
	],
	"guardiansVotes": [
		"",
		"GYyn77OXTL31zPbDBCe/eKir+VCF3hv+LxiOUF3XcJY=",
		"CyJWr3uy4niOIG1BDGjmurWOo/czaiErqb0EpZLrMAQ=",
		"GYyn77OXTL31zPbDBCe/eKir+VCF3hv+LxiOUF3XcJY=",
		"i0rZDoLUpl3vCI+5eMbFih7vh3VTWm3u+y2KXqO6Qh0="
	],
	"nominatedGuardians": [],
	"guardiansNominationHeight": 15
}`)
}

func TestBridge_ChooseAdministrator(t *testing.T) {
	z := mock.NewMockZenonWithCustomEpochDuration(t, time.Hour)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).Equals(t, `
t=2001-09-09T01:46:50+0000 lvl=dbug msg=created module=embedded contract=spork spork="&{Id:c6a597f757168bd5c9fddf52b16b3bf38e2ef781fb8edeea1bf2ae0d3225230d Name:spork-bridge Description:activate spork for bridge Activated:false EnforcementHeight:0}"
t=2001-09-09T01:47:00+0000 lvl=dbug msg=activated module=embedded contract=spork spork="&{Id:c6a597f757168bd5c9fddf52b16b3bf38e2ef781fb8edeea1bf2ae0d3225230d Name:spork-bridge Description:activate spork for bridge Activated:true EnforcementHeight:9}"
`)
	bridgeAPI := embedded.NewBridgeApi(z)
	activateBridge(t, z)

	defer z.CallContract(&nom.AccountBlock{
		Address:       g.User5.Address,
		ToAddress:     types.BridgeContract,
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(0),
		Data: definition.ABIBridge.PackMethodPanic(definition.ProposeAdministratorMethodName,
			base64.StdEncoding.EncodeToString(g.User5.Public),
		),
	}).Error(t, constants.ErrNotEmergency)
	z.InsertNewMomentum()
	z.InsertNewMomentum()

	defer z.CallContract(&nom.AccountBlock{
		Address:       g.User5.Address,
		ToAddress:     types.BridgeContract,
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(0),
		Data:          definition.ABIBridge.PackMethodPanic(definition.EmergencyMethodName),
	}).Error(t, nil)
	z.InsertNewMomentum()
	z.InsertNewMomentum()

	defer z.CallContract(&nom.AccountBlock{
		Address:       g.User5.Address,
		ToAddress:     types.BridgeContract,
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(0),
		Data: definition.ABIBridge.PackMethodPanic(definition.ProposeAdministratorMethodName,
			base64.StdEncoding.EncodeToString(g.User1.Public),
		),
	}).Error(t, nil)
	z.InsertNewMomentum()
	z.InsertNewMomentum()

	defer z.CallContract(&nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     types.BridgeContract,
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(0),
		Data: definition.ABIBridge.PackMethodPanic(definition.ProposeAdministratorMethodName,
			base64.StdEncoding.EncodeToString(g.User1.Public),
		),
	}).Error(t, nil)
	z.InsertNewMomentum()
	z.InsertNewMomentum()

	common.Json(bridgeAPI.GetSecurityInfo()).Equals(t, `
{
	"requestedAdministratorPubKey": "",
	"administratorChangeMomentum": 11,
	"administratorDelay": 30,
	"requestedTssPubKey": "",
	"tssChangeMomentum": 70,
	"tssDelay": 21,
	"guardians": [
		"B8uNQ2UGKIG1aMMYo1keh3DBMH0rrlBa4e9+maM8w2E=",
		"CyJWr3uy4niOIG1BDGjmurWOo/czaiErqb0EpZLrMAQ=",
		"GYyn77OXTL31zPbDBCe/eKir+VCF3hv+LxiOUF3XcJY=",
		"i0rZDoLUpl3vCI+5eMbFih7vh3VTWm3u+y2KXqO6Qh0=",
		"tUJu3P7Drp25XP662lIjyFlFpvj8bWUpyC+0y5YTzXM="
	],
	"guardiansVotes": [
		"",
		"GYyn77OXTL31zPbDBCe/eKir+VCF3hv+LxiOUF3XcJY=",
		"GYyn77OXTL31zPbDBCe/eKir+VCF3hv+LxiOUF3XcJY=",
		"",
		""
	],
	"nominatedGuardians": [],
	"guardiansNominationHeight": 15
}`)

	defer z.CallContract(&nom.AccountBlock{
		Address:       g.User3.Address,
		ToAddress:     types.BridgeContract,
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(0),
		Data: definition.ABIBridge.PackMethodPanic(definition.ProposeAdministratorMethodName,
			base64.StdEncoding.EncodeToString(g.User1.Public),
		),
	}).Error(t, nil)
	z.InsertNewMomentum()
	z.InsertNewMomentum()

	common.Json(bridgeAPI.GetSecurityInfo()).Equals(t, `
{
	"requestedAdministratorPubKey": "",
	"administratorChangeMomentum": 11,
	"administratorDelay": 30,
	"requestedTssPubKey": "",
	"tssChangeMomentum": 70,
	"tssDelay": 21,
	"guardians": [
		"B8uNQ2UGKIG1aMMYo1keh3DBMH0rrlBa4e9+maM8w2E=",
		"CyJWr3uy4niOIG1BDGjmurWOo/czaiErqb0EpZLrMAQ=",
		"GYyn77OXTL31zPbDBCe/eKir+VCF3hv+LxiOUF3XcJY=",
		"i0rZDoLUpl3vCI+5eMbFih7vh3VTWm3u+y2KXqO6Qh0=",
		"tUJu3P7Drp25XP662lIjyFlFpvj8bWUpyC+0y5YTzXM="
	],
	"guardiansVotes": [
		"",
		"",
		"",
		"",
		""
	],
	"nominatedGuardians": [],
	"guardiansNominationHeight": 15
}`)
	common.Json(bridgeAPI.GetBridgeInfo()).Equals(t, `
{
	"administratorEDDSAPubKey": "GYyn77OXTL31zPbDBCe/eKir+VCF3hv+LxiOUF3XcJY=",
	"compressedTssECDSAPubKey": "",
	"decompressedTssECDSAPubKey": "",
	"allowKeyGen": false,
	"halted": true,
	"unhaltedAt": 0,
	"unhaltDurationInMomentums": 35,
	"tssNonce": 0,
	"metadata": "{}"
}`)

	defer z.CallContract(&nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     types.BridgeContract,
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(0),
		Data:          definition.ABIBridge.PackMethodPanic(definition.UnhaltMethodName),
	}).Error(t, constants.ErrBridgeNotInitialized)
	z.InsertNewMomentum()
	z.InsertNewMomentum()

	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.BridgeContract,
		Data:      definition.ABIBridge.PackMethodPanic(definition.AllowKeygenMethodName),
	}).Error(t, nil)
	z.InsertNewMomentum()
	z.InsertNewMomentum()
	common.Json(bridgeAPI.GetBridgeInfo()).Equals(t, `
{
	"administratorEDDSAPubKey": "GYyn77OXTL31zPbDBCe/eKir+VCF3hv+LxiOUF3XcJY=",
	"compressedTssECDSAPubKey": "",
	"decompressedTssECDSAPubKey": "",
	"allowKeyGen": true,
	"halted": true,
	"unhaltedAt": 0,
	"unhaltDurationInMomentums": 35,
	"tssNonce": 0,
	"metadata": "{}"
}`)

	tssPubKey := "AsAQx1M3LVXCuozDOqO5b9adj/PItYgwZFG/xTDBiZzT" // priv tuSwrTEUyJI1/3y5J8L8DSjzT/AQG2IK3JG+93qhhhI=
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.BridgeContract,
		Data: definition.ABIBridge.PackMethodPanic(definition.ChangeTssECDSAPubKeyMethodName,
			tssPubKey,
			"",
			"",
			uint32(3),
		),
	}).Error(t, nil)
	z.InsertNewMomentum()
	z.InsertNewMomentum()
	common.Json(bridgeAPI.GetSecurityInfo()).Equals(t, `
{
	"requestedAdministratorPubKey": "",
	"administratorChangeMomentum": 11,
	"administratorDelay": 30,
	"requestedTssPubKey": "AsAQx1M3LVXCuozDOqO5b9adj/PItYgwZFG/xTDBiZzT",
	"tssChangeMomentum": 109,
	"tssDelay": 21,
	"guardians": [
		"B8uNQ2UGKIG1aMMYo1keh3DBMH0rrlBa4e9+maM8w2E=",
		"CyJWr3uy4niOIG1BDGjmurWOo/czaiErqb0EpZLrMAQ=",
		"GYyn77OXTL31zPbDBCe/eKir+VCF3hv+LxiOUF3XcJY=",
		"i0rZDoLUpl3vCI+5eMbFih7vh3VTWm3u+y2KXqO6Qh0=",
		"tUJu3P7Drp25XP662lIjyFlFpvj8bWUpyC+0y5YTzXM="
	],
	"guardiansVotes": [
		"",
		"",
		"",
		"",
		""
	],
	"nominatedGuardians": [],
	"guardiansNominationHeight": 15
}`)

	z.InsertMomentumsTo(200)

	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.BridgeContract,
		Data: definition.ABIBridge.PackMethodPanic(definition.ChangeTssECDSAPubKeyMethodName,
			tssPubKey,
			"",
			"",
			uint32(3),
		),
	}).Error(t, nil)
	z.InsertNewMomentum()
	z.InsertNewMomentum()
	common.Json(bridgeAPI.GetSecurityInfo()).Equals(t, `
{
	"requestedAdministratorPubKey": "",
	"administratorChangeMomentum": 11,
	"administratorDelay": 30,
	"requestedTssPubKey": "",
	"tssChangeMomentum": 109,
	"tssDelay": 21,
	"guardians": [
		"B8uNQ2UGKIG1aMMYo1keh3DBMH0rrlBa4e9+maM8w2E=",
		"CyJWr3uy4niOIG1BDGjmurWOo/czaiErqb0EpZLrMAQ=",
		"GYyn77OXTL31zPbDBCe/eKir+VCF3hv+LxiOUF3XcJY=",
		"i0rZDoLUpl3vCI+5eMbFih7vh3VTWm3u+y2KXqO6Qh0=",
		"tUJu3P7Drp25XP662lIjyFlFpvj8bWUpyC+0y5YTzXM="
	],
	"guardiansVotes": [
		"",
		"",
		"",
		"",
		""
	],
	"nominatedGuardians": [],
	"guardiansNominationHeight": 15
}`)

	defer z.CallContract(&nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     types.BridgeContract,
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(0),
		Data:          definition.ABIBridge.PackMethodPanic(definition.UnhaltMethodName),
	}).Error(t, nil)
	z.InsertNewMomentum()
	z.InsertNewMomentum()

	common.Json(bridgeAPI.GetBridgeInfo()).Equals(t, `
{
	"administratorEDDSAPubKey": "GYyn77OXTL31zPbDBCe/eKir+VCF3hv+LxiOUF3XcJY=",
	"compressedTssECDSAPubKey": "AsAQx1M3LVXCuozDOqO5b9adj/PItYgwZFG/xTDBiZzT",
	"decompressedTssECDSAPubKey": "BMAQx1M3LVXCuozDOqO5b9adj/PItYgwZFG/xTDBiZzTnQAT1qOPAkuPzu6yoewss9XbnTmZmb9JQNGXmkPYtK4=",
	"allowKeyGen": false,
	"halted": false,
	"unhaltedAt": 203,
	"unhaltDurationInMomentums": 35,
	"tssNonce": 0,
	"metadata": "{}"
}`)
}

func TestBridge_NominateGuardians(t *testing.T) {
	z := mock.NewMockZenonWithCustomEpochDuration(t, time.Hour)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).Equals(t, `
t=2001-09-09T01:46:50+0000 lvl=dbug msg=created module=embedded contract=spork spork="&{Id:c6a597f757168bd5c9fddf52b16b3bf38e2ef781fb8edeea1bf2ae0d3225230d Name:spork-bridge Description:activate spork for bridge Activated:false EnforcementHeight:0}"
t=2001-09-09T01:47:00+0000 lvl=dbug msg=activated module=embedded contract=spork spork="&{Id:c6a597f757168bd5c9fddf52b16b3bf38e2ef781fb8edeea1bf2ae0d3225230d Name:spork-bridge Description:activate spork for bridge Activated:true EnforcementHeight:9}"
`)
	bridgeAPI := embedded.NewBridgeApi(z)
	activateBridge(t, z)

	guardians := []string{
		base64.StdEncoding.EncodeToString(g.User6.Public),
		base64.StdEncoding.EncodeToString(g.User7.Public),
		base64.StdEncoding.EncodeToString(g.User8.Public),
		base64.StdEncoding.EncodeToString(g.User9.Public),
		base64.StdEncoding.EncodeToString(g.User10.Public),
	}
	z.InsertSendBlock(&nom.AccountBlock{
		Address:   g.User5.Address,
		ToAddress: types.BridgeContract,
		Data: definition.ABIBridge.PackMethodPanic(definition.NominateGuardiansMethodName,
			guardians,
		),
	}, nil, mock.SkipVmChanges)
	z.InsertNewMomentum()
	z.InsertNewMomentum()

	common.Json(bridgeAPI.GetSecurityInfo()).Equals(t, `
{
	"requestedAdministratorPubKey": "",
	"administratorChangeMomentum": 11,
	"administratorDelay": 30,
	"requestedTssPubKey": "",
	"tssChangeMomentum": 70,
	"tssDelay": 21,
	"guardians": [
		"B8uNQ2UGKIG1aMMYo1keh3DBMH0rrlBa4e9+maM8w2E=",
		"CyJWr3uy4niOIG1BDGjmurWOo/czaiErqb0EpZLrMAQ=",
		"GYyn77OXTL31zPbDBCe/eKir+VCF3hv+LxiOUF3XcJY=",
		"i0rZDoLUpl3vCI+5eMbFih7vh3VTWm3u+y2KXqO6Qh0=",
		"tUJu3P7Drp25XP662lIjyFlFpvj8bWUpyC+0y5YTzXM="
	],
	"guardiansVotes": [
		"",
		"",
		"",
		"",
		""
	],
	"nominatedGuardians": [
		"DQ38l1x6yvRLv8wESsC/q7drQw6hF+W2bDAM8C1r90U=",
		"KM4WeGe153tqcw4MqfknhHoD09lv+8sCn9a44FYbEVk=",
		"M5q3OIrEfg8lLZnf99uw/KtMXFYFA1EnumhFOUeawVA=",
		"U5SG0cvWTUkKzGSssz9qhuQqvpXTdXiv3nY6par7QBk=",
		"d7V1bTzlSu1Y5pBDBcK1Dzr1YGgt69RaRRGdpdho8wU="
	],
	"guardiansNominationHeight": 95
}`)
}
