package tests

import (
	"encoding/base64"
	"github.com/zenon-network/go-zenon/common"
	"math/big"
	"testing"
	"time"

	g "github.com/zenon-network/go-zenon/chain/genesis/mock"
	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/rpc/api/embedded"
	"github.com/zenon-network/go-zenon/vm/constants"
	"github.com/zenon-network/go-zenon/vm/embedded/definition"
	"github.com/zenon-network/go-zenon/zenon/mock"
)

func activateGovernance(z mock.MockZenon) {
	z.InsertSendBlock(&nom.AccountBlock{
		Address:   g.Spork.Address,
		ToAddress: types.SporkContract,
		Data: definition.ABISpork.PackMethodPanic(definition.SporkCreateMethodName,
			"spork-governance",              // name
			"activate spork for governance", // description
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
	types.GovernanceSpork.SporkId = id
	types.ImplementedSporksMap[id] = true
}

// Activate spork
func activateGovernanceStep0(t *testing.T, z mock.MockZenon) {
	activateGovernance(z)
	z.InsertMomentumsTo(10)

	constants.Type1ActionVotingPeriod = 15 * 60   // 15 minutes
	constants.Type2ActionVotingPeriod = 8 * 60    // 8 minutes
	constants.Type1ActionAcceptanceThreshold = 40 // 40%
	constants.Type2ActionAcceptanceThreshold = 25 // 25%

	governanceApi := embedded.NewGovernanceApi(z)
	actionsList, err := governanceApi.GetAllActions(0, 10)

	common.Json(actionsList, err).Equals(t, `
{
	"count": 0,
	"list": []
}`)

	// Register 4th pillar for voting
	defer z.CallContract(&nom.AccountBlock{
		Address:       g.Pillar4.Address,
		ToAddress:     types.PillarContract,
		Data:          definition.ABIPillars.PackMethodPanic(definition.DepositQsrMethodName),
		TokenStandard: types.QsrTokenStandard,
		Amount:        big.NewInt(150000 * g.Zexp),
	}).Error(t, nil)
	z.InsertNewMomentum()
	// register the first normal pillar
	defer z.CallContract(&nom.AccountBlock{
		Address:       g.Pillar4.Address,
		ToAddress:     types.PillarContract,
		Data:          definition.ABIPillars.PackMethodPanic(definition.RegisterMethodName, g.Pillar4Name, g.Pillar4.Address, g.Pillar4.Address, uint8(0), uint8(100)),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        constants.PillarStakeAmount,
	}).Error(t, nil)
	z.InsertNewMomentum()

	// Register 5th pillar for voting
	defer z.CallContract(&nom.AccountBlock{
		Address:       g.Pillar5.Address,
		ToAddress:     types.PillarContract,
		Data:          definition.ABIPillars.PackMethodPanic(definition.DepositQsrMethodName),
		TokenStandard: types.QsrTokenStandard,
		Amount:        big.NewInt(160000 * g.Zexp),
	}).Error(t, nil)
	z.InsertNewMomentum()
	// register the first normal pillar
	defer z.CallContract(&nom.AccountBlock{
		Address:       g.Pillar5.Address,
		ToAddress:     types.PillarContract,
		Data:          definition.ABIPillars.PackMethodPanic(definition.RegisterMethodName, g.Pillar5Name, g.Pillar5.Address, g.Pillar5.Address, uint8(0), uint8(100)),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        constants.PillarStakeAmount,
	}).Error(t, nil)
	insertMomentums(z, 2)

	// Register 6th pillar for voting
	defer z.CallContract(&nom.AccountBlock{
		Address:       g.Pillar6.Address,
		ToAddress:     types.PillarContract,
		Data:          definition.ABIPillars.PackMethodPanic(definition.DepositQsrMethodName),
		TokenStandard: types.QsrTokenStandard,
		Amount:        big.NewInt(170000 * g.Zexp),
	}).Error(t, nil)
	insertMomentums(z, 2)

	// register the first normal pillar
	defer z.CallContract(&nom.AccountBlock{
		Address:       g.Pillar6.Address,
		ToAddress:     types.PillarContract,
		Data:          definition.ABIPillars.PackMethodPanic(definition.RegisterMethodName, g.Pillar6Name, g.Pillar6.Address, g.Pillar6.Address, uint8(0), uint8(100)),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        constants.PillarStakeAmount,
	}).Error(t, nil)
	z.InsertNewMomentum()
}

// Activate spork
// Propose action to create a spork
func activateGovernanceStep1(t *testing.T, z mock.MockZenon) {
	activateGovernanceStep0(t, z)
	insertMomentums(z, 10)

	name := "create btc-bridge spork"
	description := "this spork will implement bitcoin bridge logic"
	url := "https://qwerty.com"

	sporkName := "btc-bridge"
	sporkDescription := "btc-bridge logic"
	data, err := definition.ABISpork.PackMethod(definition.SporkCreateMethodName, sporkName, sporkDescription)
	common.FailIfErr(t, err)
	dataString := base64.StdEncoding.EncodeToString(data)

	defer z.CallContract(proposeAction(g.User1.Address, name, description, url, types.SporkContract, dataString)).
		Error(t, nil)
	insertMomentums(z, 2)

	governanceApi := embedded.NewGovernanceApi(z)
	actionsList, err := governanceApi.GetAllActions(0, 10)
	common.Json(actionsList, err).Equals(t, `
{
	"count": 1,
	"list": [
		{
			"Id": "22545375297973875f2fd10b3c4fa46789ed2256b29865accc75b76e73c4b147",
			"Owner": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
			"Name": "create btc-bridge spork",
			"Description": "this spork will implement bitcoin bridge logic",
			"Url": "https://qwerty.com",
			"Destination": "z1qxemdeddedxsp0rkxxxxxxxxxxxxxxxx956u48",
			"Data": "tgLjEQAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAABAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAIAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAACmJ0Yy1icmlkZ2UAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAABBidGMtYnJpZGdlIGxvZ2ljAAAAAAAAAAAAAAAAAAAAAA==",
			"CreationTimestamp": 1000000280,
			"Type": 1,
			"Executed": false,
			"Expired": false,
			"Votes": {
				"id": "22545375297973875f2fd10b3c4fa46789ed2256b29865accc75b76e73c4b147",
				"total": 0,
				"yes": 0,
				"no": 0
			}
		}
	]
}`)
}

// Activate spork
// Propose action to create a spork
// Vote action
func activateGovernanceStep2(t *testing.T, z mock.MockZenon) {
	activateGovernanceStep1(t, z)
	insertMomentums(z, 10)

	governanceApi := embedded.NewGovernanceApi(z)
	actionsList, err := governanceApi.GetAllActions(0, 10)
	common.FailIfErr(t, err)
	id := actionsList.List[0].Id

	defer z.CallContract(voteByName(g.Pillar1.Address, g.Pillar1Name, id, definition.VoteYes)).Error(t, nil)
	insertMomentums(z, 2)
	defer z.CallContract(voteByName(g.Pillar2.Address, g.Pillar2Name, id, definition.VoteYes)).Error(t, nil)
	insertMomentums(z, 2)
	defer z.CallContract(voteByName(g.Pillar3.Address, g.Pillar3Name, id, definition.VoteYes)).Error(t, nil)
	insertMomentums(z, 2)
	defer z.CallContract(voteByName(g.Pillar4.Address, g.Pillar4Name, id, definition.VoteNo)).Error(t, nil)
	insertMomentums(z, 2)
	defer z.CallContract(voteByName(g.Pillar5.Address, g.Pillar5Name, id, definition.VoteNo)).Error(t, nil)
	insertMomentums(z, 2)
	defer z.CallContract(voteByName(g.Pillar6.Address, g.Pillar6Name, id, definition.VoteYes)).Error(t, nil)
	insertMomentums(z, 2)

	actionsList, err = governanceApi.GetAllActions(0, 10)
	common.Json(actionsList, err).Equals(t, `
{
	"count": 1,
	"list": [
		{
			"Id": "22545375297973875f2fd10b3c4fa46789ed2256b29865accc75b76e73c4b147",
			"Owner": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
			"Name": "create btc-bridge spork",
			"Description": "this spork will implement bitcoin bridge logic",
			"Url": "https://qwerty.com",
			"Destination": "z1qxemdeddedxsp0rkxxxxxxxxxxxxxxxx956u48",
			"Data": "tgLjEQAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAABAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAIAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAACmJ0Yy1icmlkZ2UAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAABBidGMtYnJpZGdlIGxvZ2ljAAAAAAAAAAAAAAAAAAAAAA==",
			"CreationTimestamp": 1000000280,
			"Type": 1,
			"Executed": false,
			"Expired": false,
			"Votes": {
				"id": "22545375297973875f2fd10b3c4fa46789ed2256b29865accc75b76e73c4b147",
				"total": 6,
				"yes": 4,
				"no": 2
			}
		}
	]
}`)
}

// Activate spork
// Propose action to create a spork
// Vote action
// Execute action and check that the spork is created
func activateGovernanceStep3(t *testing.T, z mock.MockZenon) {
	activateGovernanceStep2(t, z)
	insertMomentums(z, 10)

	governanceApi := embedded.NewGovernanceApi(z)
	actionsList, err := governanceApi.GetAllActions(0, 10)
	common.FailIfErr(t, err)
	id := actionsList.List[0].Id

	defer z.CallContract(executeAction(g.User1.Address, id)).Error(t, nil)
	insertMomentums(z, 2)

	// Action should be executed
	action, err := governanceApi.GetActionById(id)
	common.Json(action, err).Equals(t, `
{
	"Id": "22545375297973875f2fd10b3c4fa46789ed2256b29865accc75b76e73c4b147",
	"Owner": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
	"Name": "create btc-bridge spork",
	"Description": "this spork will implement bitcoin bridge logic",
	"Url": "https://qwerty.com",
	"Destination": "z1qxemdeddedxsp0rkxxxxxxxxxxxxxxxx956u48",
	"Data": "tgLjEQAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAABAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAIAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAACmJ0Yy1icmlkZ2UAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAABBidGMtYnJpZGdlIGxvZ2ljAAAAAAAAAAAAAAAAAAAAAA==",
	"CreationTimestamp": 1000000280,
	"Type": 1,
	"Executed": true,
	"Expired": false,
	"Votes": {
		"id": "22545375297973875f2fd10b3c4fa46789ed2256b29865accc75b76e73c4b147",
		"total": 6,
		"yes": 4,
		"no": 2
	}
}`)

	// The spork should be created
	sporkApi := embedded.NewSporkApi(z)
	allSporks, err := sporkApi.GetAll(0, 10)
	common.Json(allSporks, err).Equals(t, `
{
	"count": 2,
	"list": [
		{
			"id": "195163e46afd3afd1e08aeb0119e4f74a59ccb1424f7f565052690cc90d36731",
			"name": "btc-bridge",
			"description": "btc-bridge logic",
			"activated": false,
			"enforcementHeight": 0
		},
		{
			"id": "3f45018ade795af67983e5616e42ed2e88e600afb1da73f4a2b406e74344eee6",
			"name": "spork-governance",
			"description": "activate spork for governance",
			"activated": true,
			"enforcementHeight": 9
		}
	]
}`)
}

// Activate spork
// Propose action to create a spork
// Vote action
// Execute action and check that the spork is created
// Propose action to activate spork
func activateGovernanceStep4(t *testing.T, z mock.MockZenon) {
	activateGovernanceStep3(t, z)
	insertMomentums(z, 10)

	sporkName := "btc-bridge"
	sporkId := types.ZeroHash
	sporkApi := embedded.NewSporkApi(z)
	allSporks, err := sporkApi.GetAll(0, 10)
	common.FailIfErr(t, err)
	for _, spork := range allSporks.List {
		if spork.Name == sporkName {
			sporkId = spork.Id
		}
	}

	name := "activate btc-bridge spork"
	description := "this action will activate the btc-spork"
	url := "https://qwerty.com"

	data, err := definition.ABISpork.PackMethod(definition.SporkActivateMethodName, sporkId)
	common.FailIfErr(t, err)
	dataString := base64.StdEncoding.EncodeToString(data)

	defer z.CallContract(proposeAction(g.User1.Address, name, description, url, types.SporkContract, dataString)).
		Error(t, nil)
	insertMomentums(z, 2)

	governanceApi := embedded.NewGovernanceApi(z)
	actionsList, err := governanceApi.GetAllActions(0, 10)
	common.Json(actionsList, err).Equals(t, `
{
	"count": 2,
	"list": [
		{
			"Id": "22545375297973875f2fd10b3c4fa46789ed2256b29865accc75b76e73c4b147",
			"Owner": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
			"Name": "create btc-bridge spork",
			"Description": "this spork will implement bitcoin bridge logic",
			"Url": "https://qwerty.com",
			"Destination": "z1qxemdeddedxsp0rkxxxxxxxxxxxxxxxx956u48",
			"Data": "tgLjEQAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAABAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAIAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAACmJ0Yy1icmlkZ2UAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAABBidGMtYnJpZGdlIGxvZ2ljAAAAAAAAAAAAAAAAAAAAAA==",
			"CreationTimestamp": 1000000280,
			"Type": 1,
			"Executed": true,
			"Expired": false,
			"Votes": {
				"id": "22545375297973875f2fd10b3c4fa46789ed2256b29865accc75b76e73c4b147",
				"total": 6,
				"yes": 4,
				"no": 2
			}
		},
		{
			"Id": "674b3cac7a52b70cc78da2780c3f7234715e42b1c6971f7f92345d09f5f90809",
			"Owner": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
			"Name": "activate btc-bridge spork",
			"Description": "this action will activate the btc-spork",
			"Url": "https://qwerty.com",
			"Destination": "z1qxemdeddedxsp0rkxxxxxxxxxxxxxxxx956u48",
			"Data": "JcVOlhlRY+Rq/Tr9HgiusBGeT3SlnMsUJPf1ZQUmkMyQ02cx",
			"CreationTimestamp": 1000000740,
			"Type": 1,
			"Executed": false,
			"Expired": false,
			"Votes": {
				"id": "674b3cac7a52b70cc78da2780c3f7234715e42b1c6971f7f92345d09f5f90809",
				"total": 0,
				"yes": 0,
				"no": 0
			}
		}
	]
}`)
}

// Activate spork
// Propose action to create a spork
// Vote action
// Execute action and check that the spork is created
// Propose action to activate spork
// Vote action
func activateGovernanceStep5(t *testing.T, z mock.MockZenon) {
	activateGovernanceStep4(t, z)
	insertMomentums(z, 10)

	actionName := "activate btc-bridge spork"
	actionId := types.ZeroHash
	governanceApi := embedded.NewGovernanceApi(z)
	actionsList, err := governanceApi.GetAllActions(0, 10)
	common.FailIfErr(t, err)
	for _, action := range actionsList.List {
		if action.Name == actionName {
			actionId = action.Id
		}
	}

	defer z.CallContract(voteByName(g.Pillar1.Address, g.Pillar1Name, actionId, definition.VoteYes)).Error(t, nil)
	insertMomentums(z, 2)
	defer z.CallContract(voteByName(g.Pillar2.Address, g.Pillar2Name, actionId, definition.VoteYes)).Error(t, nil)
	insertMomentums(z, 2)
	defer z.CallContract(voteByName(g.Pillar3.Address, g.Pillar3Name, actionId, definition.VoteYes)).Error(t, nil)
	insertMomentums(z, 2)
	defer z.CallContract(voteByName(g.Pillar4.Address, g.Pillar4Name, actionId, definition.VoteNo)).Error(t, nil)
	insertMomentums(z, 2)
	defer z.CallContract(voteByName(g.Pillar5.Address, g.Pillar5Name, actionId, definition.VoteYes)).Error(t, nil)
	insertMomentums(z, 2)
	defer z.CallContract(voteByName(g.Pillar6.Address, g.Pillar6Name, actionId, definition.VoteYes)).Error(t, nil)
	insertMomentums(z, 2)

	actionsList, err = governanceApi.GetAllActions(0, 10)
	common.Json(actionsList, err).Equals(t, `
{
	"count": 2,
	"list": [
		{
			"Id": "22545375297973875f2fd10b3c4fa46789ed2256b29865accc75b76e73c4b147",
			"Owner": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
			"Name": "create btc-bridge spork",
			"Description": "this spork will implement bitcoin bridge logic",
			"Url": "https://qwerty.com",
			"Destination": "z1qxemdeddedxsp0rkxxxxxxxxxxxxxxxx956u48",
			"Data": "tgLjEQAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAABAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAIAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAACmJ0Yy1icmlkZ2UAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAABBidGMtYnJpZGdlIGxvZ2ljAAAAAAAAAAAAAAAAAAAAAA==",
			"CreationTimestamp": 1000000280,
			"Type": 1,
			"Executed": true,
			"Expired": false,
			"Votes": {
				"id": "22545375297973875f2fd10b3c4fa46789ed2256b29865accc75b76e73c4b147",
				"total": 6,
				"yes": 4,
				"no": 2
			}
		},
		{
			"Id": "674b3cac7a52b70cc78da2780c3f7234715e42b1c6971f7f92345d09f5f90809",
			"Owner": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
			"Name": "activate btc-bridge spork",
			"Description": "this action will activate the btc-spork",
			"Url": "https://qwerty.com",
			"Destination": "z1qxemdeddedxsp0rkxxxxxxxxxxxxxxxx956u48",
			"Data": "JcVOlhlRY+Rq/Tr9HgiusBGeT3SlnMsUJPf1ZQUmkMyQ02cx",
			"CreationTimestamp": 1000000740,
			"Type": 1,
			"Executed": false,
			"Expired": false,
			"Votes": {
				"id": "674b3cac7a52b70cc78da2780c3f7234715e42b1c6971f7f92345d09f5f90809",
				"total": 6,
				"yes": 5,
				"no": 1
			}
		}
	]
}`)
}

// Activate spork
// Propose action to create a spork
// Vote action
// Execute action and check that the spork is created
// Propose action to activate spork
// Vote action
// Execute action and check that the spork is active
func activateGovernanceStep6(t *testing.T, z mock.MockZenon) {
	activateGovernanceStep5(t, z)
	insertMomentums(z, 10)

	actionName := "activate btc-bridge spork"
	actionId := types.ZeroHash
	governanceApi := embedded.NewGovernanceApi(z)
	actionList, err := governanceApi.GetAllActions(0, 10)
	common.FailIfErr(t, err)
	for _, action := range actionList.List {
		if action.Name == actionName {
			actionId = action.Id
		}
	}

	defer z.CallContract(executeAction(g.User1.Address, actionId)).Error(t, nil)
	insertMomentums(z, 2)

	// Action should be executed
	action, err := governanceApi.GetActionById(actionId)
	common.Json(action, err).Equals(t, `
{
	"Id": "674b3cac7a52b70cc78da2780c3f7234715e42b1c6971f7f92345d09f5f90809",
	"Owner": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
	"Name": "activate btc-bridge spork",
	"Description": "this action will activate the btc-spork",
	"Url": "https://qwerty.com",
	"Destination": "z1qxemdeddedxsp0rkxxxxxxxxxxxxxxxx956u48",
	"Data": "JcVOlhlRY+Rq/Tr9HgiusBGeT3SlnMsUJPf1ZQUmkMyQ02cx",
	"CreationTimestamp": 1000000740,
	"Type": 1,
	"Executed": true,
	"Expired": false,
	"Votes": {
		"id": "674b3cac7a52b70cc78da2780c3f7234715e42b1c6971f7f92345d09f5f90809",
		"total": 6,
		"yes": 5,
		"no": 1
	}
}`)

	// The spork should be created
	sporkApi := embedded.NewSporkApi(z)
	allSporks, err := sporkApi.GetAll(0, 10)
	common.Json(allSporks, err).Equals(t, `
{
	"count": 2,
	"list": [
		{
			"id": "195163e46afd3afd1e08aeb0119e4f74a59ccb1424f7f565052690cc90d36731",
			"name": "btc-bridge",
			"description": "btc-bridge logic",
			"activated": true,
			"enforcementHeight": 116
		},
		{
			"id": "3f45018ade795af67983e5616e42ed2e88e600afb1da73f4a2b406e74344eee6",
			"name": "spork-governance",
			"description": "activate spork for governance",
			"activated": true,
			"enforcementHeight": 9
		}
	]
}`)
}

func TestGovernance(t *testing.T) {
	z := mock.NewMockZenonWithCustomEpochDuration(t, time.Hour)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).Equals(t, `
t=2001-09-09T01:46:50+0000 lvl=dbug msg=created module=embedded contract=spork spork="&{Id:3f45018ade795af67983e5616e42ed2e88e600afb1da73f4a2b406e74344eee6 Name:spork-governance Description:activate spork for governance Activated:false EnforcementHeight:0}"
t=2001-09-09T01:47:00+0000 lvl=dbug msg=activated module=embedded contract=spork spork="&{Id:3f45018ade795af67983e5616e42ed2e88e600afb1da73f4a2b406e74344eee6 Name:spork-governance Description:activate spork for governance Activated:true EnforcementHeight:9}"
t=2001-09-09T01:48:40+0000 lvl=dbug msg="burned ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 TokenName:QuasarCoin TokenSymbol:QSR TokenDomain:zenon.network TotalSupply:+165550000000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1qsrxxxxxxxxxxxxxmrhjll}" burned-amount=15000000000000
t=2001-09-09T01:49:00+0000 lvl=dbug msg="burned ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 TokenName:QuasarCoin TokenSymbol:QSR TokenDomain:zenon.network TotalSupply:+149550000000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1qsrxxxxxxxxxxxxxmrhjll}" burned-amount=16000000000000
t=2001-09-09T01:49:40+0000 lvl=dbug msg="burned ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 TokenName:QuasarCoin TokenSymbol:QSR TokenDomain:zenon.network TotalSupply:+132550000000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1qsrxxxxxxxxxxxxxmrhjll}" burned-amount=17000000000000
t=2001-09-09T01:51:20+0000 lvl=dbug msg="successfully created action proposal" module=embedded contract=governance action="&{Id:22545375297973875f2fd10b3c4fa46789ed2256b29865accc75b76e73c4b147 Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz Name:create btc-bridge spork Description:this spork will implement bitcoin bridge logic Url:https://qwerty.com Destination:z1qxemdeddedxsp0rkxxxxxxxxxxxxxxxx956u48 Data:tgLjEQAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAABAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAIAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAACmJ0Yy1icmlkZ2UAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAABBidGMtYnJpZGdlIGxvZ2ljAAAAAAAAAAAAAAAAAAAAAA== CreationTimestamp:1000000280 Type:1 Executed:false}"
t=2001-09-09T01:53:20+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:22545375297973875f2fd10b3c4fa46789ed2256b29865accc75b76e73c4b147 Name:TEST-pillar-1 Vote:0}"
t=2001-09-09T01:53:40+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:22545375297973875f2fd10b3c4fa46789ed2256b29865accc75b76e73c4b147 Name:TEST-pillar-cool Vote:0}"
t=2001-09-09T01:54:00+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:22545375297973875f2fd10b3c4fa46789ed2256b29865accc75b76e73c4b147 Name:TEST-pillar-znn Vote:0}"
t=2001-09-09T01:54:20+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:22545375297973875f2fd10b3c4fa46789ed2256b29865accc75b76e73c4b147 Name:TEST-pillar-wewe Vote:1}"
t=2001-09-09T01:54:40+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:22545375297973875f2fd10b3c4fa46789ed2256b29865accc75b76e73c4b147 Name:TEST-pillar-zumba Vote:1}"
t=2001-09-09T01:55:00+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:22545375297973875f2fd10b3c4fa46789ed2256b29865accc75b76e73c4b147 Name:TEST-pillar-6-quasar Vote:0}"
t=2001-09-09T01:57:00+0000 lvl=dbug msg="check action votes" module=embedded contract=governance votes="&{Id:22545375297973875f2fd10b3c4fa46789ed2256b29865accc75b76e73c4b147 Total:6 Yes:4 No:2}" status=true
t=2001-09-09T01:57:00+0000 lvl=dbug msg="action passed voting and is being executed" module=embedded contract=governance action-id=22545375297973875f2fd10b3c4fa46789ed2256b29865accc75b76e73c4b147 passed-votes=true
t=2001-09-09T01:57:10+0000 lvl=dbug msg=created module=embedded contract=spork spork="&{Id:195163e46afd3afd1e08aeb0119e4f74a59ccb1424f7f565052690cc90d36731 Name:btc-bridge Description:btc-bridge logic Activated:false EnforcementHeight:0}"
t=2001-09-09T01:59:00+0000 lvl=dbug msg="successfully created action proposal" module=embedded contract=governance action="&{Id:674b3cac7a52b70cc78da2780c3f7234715e42b1c6971f7f92345d09f5f90809 Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz Name:activate btc-bridge spork Description:this action will activate the btc-spork Url:https://qwerty.com Destination:z1qxemdeddedxsp0rkxxxxxxxxxxxxxxxx956u48 Data:JcVOlhlRY+Rq/Tr9HgiusBGeT3SlnMsUJPf1ZQUmkMyQ02cx CreationTimestamp:1000000740 Type:1 Executed:false}"
t=2001-09-09T02:01:00+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:674b3cac7a52b70cc78da2780c3f7234715e42b1c6971f7f92345d09f5f90809 Name:TEST-pillar-1 Vote:0}"
t=2001-09-09T02:01:20+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:674b3cac7a52b70cc78da2780c3f7234715e42b1c6971f7f92345d09f5f90809 Name:TEST-pillar-cool Vote:0}"
t=2001-09-09T02:01:40+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:674b3cac7a52b70cc78da2780c3f7234715e42b1c6971f7f92345d09f5f90809 Name:TEST-pillar-znn Vote:0}"
t=2001-09-09T02:02:00+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:674b3cac7a52b70cc78da2780c3f7234715e42b1c6971f7f92345d09f5f90809 Name:TEST-pillar-wewe Vote:1}"
t=2001-09-09T02:02:20+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:674b3cac7a52b70cc78da2780c3f7234715e42b1c6971f7f92345d09f5f90809 Name:TEST-pillar-zumba Vote:0}"
t=2001-09-09T02:02:40+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:674b3cac7a52b70cc78da2780c3f7234715e42b1c6971f7f92345d09f5f90809 Name:TEST-pillar-6-quasar Vote:0}"
t=2001-09-09T02:04:40+0000 lvl=dbug msg="check action votes" module=embedded contract=governance votes="&{Id:674b3cac7a52b70cc78da2780c3f7234715e42b1c6971f7f92345d09f5f90809 Total:6 Yes:5 No:1}" status=true
t=2001-09-09T02:04:40+0000 lvl=dbug msg="action passed voting and is being executed" module=embedded contract=governance action-id=674b3cac7a52b70cc78da2780c3f7234715e42b1c6971f7f92345d09f5f90809 passed-votes=true
t=2001-09-09T02:04:50+0000 lvl=dbug msg=activated module=embedded contract=spork spork="&{Id:195163e46afd3afd1e08aeb0119e4f74a59ccb1424f7f565052690cc90d36731 Name:btc-bridge Description:btc-bridge logic Activated:true EnforcementHeight:116}"
`)

	activateGovernanceStep6(t, z)
}

func proposeAction(user types.Address, name, description, url string, destination types.Address, data string) *nom.AccountBlock {
	return &nom.AccountBlock{
		Address:       user,
		ToAddress:     types.GovernanceContract,
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(1 * constants.Decimals),
		Data: definition.ABIGovernance.PackMethodPanic(definition.ProposeActionMethodName,
			name, description, url, destination, data),
	}
}

func executeAction(user types.Address, id types.Hash) *nom.AccountBlock {
	return &nom.AccountBlock{
		Address:       user,
		ToAddress:     types.GovernanceContract,
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(0),
		Data:          definition.ABIGovernance.PackMethodPanic(definition.ExecuteActionMethodName, id),
	}
}

func voteByName(pillarAddress types.Address, pillarName string, id types.Hash, vote uint8) *nom.AccountBlock {
	return &nom.AccountBlock{
		Address:   pillarAddress,
		ToAddress: types.GovernanceContract,
		Data: definition.ABIGovernance.PackMethodPanic(definition.VoteByNameMethodName,
			id,
			pillarName,
			vote,
		),
	}
}
