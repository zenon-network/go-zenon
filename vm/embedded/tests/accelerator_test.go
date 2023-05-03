package tests

import (
	"fmt"
	"math/big"
	"testing"
	"time"

	g "github.com/zenon-network/go-zenon/chain/genesis/mock"
	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/rpc/api"
	"github.com/zenon-network/go-zenon/rpc/api/embedded"
	"github.com/zenon-network/go-zenon/vm/constants"
	"github.com/zenon-network/go-zenon/vm/embedded/definition"
	"github.com/zenon-network/go-zenon/zenon/mock"
)

func activateAccelerator(z mock.MockZenon) {
	sporkAPI := embedded.NewSporkApi(z)
	z.InsertSendBlock(&nom.AccountBlock{
		Address:   g.Spork.Address,
		ToAddress: types.SporkContract,
		Data: definition.ABISpork.PackMethodPanic(definition.SporkCreateMethodName,
			"spork-accelerator",              // name
			"activate spork for accelerator", // description
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
	types.AcceleratorSpork.SporkId = id
	types.ImplementedSporksMap[id] = true
	z.InsertMomentumsTo(20)
}

func TestAccelerator(t *testing.T) {
	z := mock.NewMockZenonWithCustomEpochDuration(t, time.Hour)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).Equals(t, `
t=2001-09-09T01:46:50+0000 lvl=info msg="received donation" module=embedded contract=common embedded=z1qxemdeddedxaccelerat0rxxxxxxxxxxp4tk22 from-address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz zts=zts1znnxxxxxxxxxxxxx9z4ulx amount=100
`)

	defer z.CallContract(&nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     types.AcceleratorContract,
		Data:          definition.ABICommon.PackMethodPanic(definition.DonateMethodName),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        common.Big100,
	}).Error(t, nil)

	z.InsertMomentumsTo(10)
}

func TestAccelerator_CreateProject(t *testing.T) {
	types.ImplementedSporksMap[types.AcceleratorSpork.SporkId] = true
	z := mock.NewMockZenonWithCustomEpochDuration(t, time.Hour)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).Equals(t, `
t=2001-09-09T01:46:50+0000 lvl=dbug msg=created module=embedded contract=spork spork="&{Id:34d8229bd07586c243c6e74122a18d6d2002694c72964a7186111026a9cec6ab Name:spork-accelerator Description:activate spork for accelerator Activated:false EnforcementHeight:0}"
t=2001-09-09T01:47:00+0000 lvl=dbug msg=activated module=embedded contract=spork spork="&{Id:34d8229bd07586c243c6e74122a18d6d2002694c72964a7186111026a9cec6ab Name:spork-accelerator Description:activate spork for accelerator Activated:true EnforcementHeight:9}"
t=2001-09-09T01:50:00+0000 lvl=dbug msg="successfully create project" module=embedded contract=accelerator project="&{Id:c24a5a6166c8948aba23d68aa39e206fc1410138ad218500749b75e2ae92d730 Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz Name:Test Project 1 Description:TEST DESCRIPTION Url:test.com ZnnFundsNeeded:+100 QsrFundsNeeded:+1000 CreationTimestamp:1000000200 LastUpdateTimestamp:1000000200 Status:0 PhaseIds:[]}"
`)
	activateAccelerator(z)
	acceleratorAPI := embedded.NewAcceleratorApi(z)
	defer z.CallContract(&nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     types.AcceleratorContract,
		TokenStandard: types.ZnnTokenStandard,
		Amount:        constants.ProjectCreationAmount,
		Data: definition.ABIAccelerator.PackMethodPanic(definition.CreateProjectMethodName,
			"Test Project 1",   //param.Name
			"TEST DESCRIPTION", //param.Description
			"test.com",         //param.Url
			big.NewInt(100),    //param.ZnnFundsNeeded
			big.NewInt(1000),   //param.QsrFundsNeeded
		),
	}).Error(t, nil)
	z.InsertNewMomentum() // cemented send block
	z.InsertNewMomentum() // cemented token-receive-block

	projectList, err := acceleratorAPI.GetAll(0, 10)
	common.FailIfErr(t, err)

	common.Json(projectList, err).Equals(t, `
{
	"count": 1,
	"list": [
		{
			"id": "c24a5a6166c8948aba23d68aa39e206fc1410138ad218500749b75e2ae92d730",
			"owner": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
			"name": "Test Project 1",
			"description": "TEST DESCRIPTION",
			"url": "test.com",
			"znnFundsNeeded": "100",
			"qsrFundsNeeded": "1000",
			"creationTimestamp": 1000000200,
			"lastUpdateTimestamp": 1000000200,
			"status": 0,
			"phaseIds": [],
			"votes": {
				"id": "c24a5a6166c8948aba23d68aa39e206fc1410138ad218500749b75e2ae92d730",
				"total": 0,
				"yes": 0,
				"no": 0
			},
			"phases": []
		}
	]
}`)
	z.ExpectBalance(g.User1.Address, types.ZnnTokenStandard, 1200000000000-1*constants.Decimals)
}

func TestAccelerator_CreateMultipleProject(t *testing.T) {
	z := mock.NewMockZenonWithCustomEpochDuration(t, time.Hour)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).Equals(t, `
t=2001-09-09T01:46:50+0000 lvl=dbug msg=created module=embedded contract=spork spork="&{Id:34d8229bd07586c243c6e74122a18d6d2002694c72964a7186111026a9cec6ab Name:spork-accelerator Description:activate spork for accelerator Activated:false EnforcementHeight:0}"
t=2001-09-09T01:47:00+0000 lvl=dbug msg=activated module=embedded contract=spork spork="&{Id:34d8229bd07586c243c6e74122a18d6d2002694c72964a7186111026a9cec6ab Name:spork-accelerator Description:activate spork for accelerator Activated:true EnforcementHeight:9}"
t=2001-09-09T01:50:00+0000 lvl=dbug msg="successfully create project" module=embedded contract=accelerator project="&{Id:c24a5a6166c8948aba23d68aa39e206fc1410138ad218500749b75e2ae92d730 Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz Name:Test Project 1 Description:TEST DESCRIPTION Url:test.com ZnnFundsNeeded:+100 QsrFundsNeeded:+1000 CreationTimestamp:1000000200 LastUpdateTimestamp:1000000200 Status:0 PhaseIds:[]}"
t=2001-09-09T01:50:20+0000 lvl=dbug msg="successfully create project" module=embedded contract=accelerator project="&{Id:fb056c9bdf8c08b30e8abbd17dc9406be7149878663719ceabb004440e24cdda Owner:z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx Name:Test Project 2 Description:TEST DESCRIPTION Url:test.com ZnnFundsNeeded:+100 QsrFundsNeeded:+1000 CreationTimestamp:1000000220 LastUpdateTimestamp:1000000220 Status:0 PhaseIds:[]}"
`)
	activateAccelerator(z)
	acceleratorAPI := embedded.NewAcceleratorApi(z)
	defer z.CallContract(&nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     types.AcceleratorContract,
		TokenStandard: types.ZnnTokenStandard,
		Amount:        constants.ProjectCreationAmount,
		Data: definition.ABIAccelerator.PackMethodPanic(definition.CreateProjectMethodName,
			"Test Project 1",   //param.Name
			"TEST DESCRIPTION", //param.Description
			"test.com",         //param.Url
			big.NewInt(100),    //param.ZnnFundsNeeded
			big.NewInt(1000),   //param.QsrFundsNeeded
		),
	}).Error(t, nil)
	z.InsertNewMomentum() // cemented send block
	z.InsertNewMomentum() // cemented token-receive-block

	defer z.CallContract(&nom.AccountBlock{
		Address:       g.User2.Address,
		ToAddress:     types.AcceleratorContract,
		TokenStandard: types.ZnnTokenStandard,
		Amount:        constants.ProjectCreationAmount,
		Data: definition.ABIAccelerator.PackMethodPanic(definition.CreateProjectMethodName,
			"Test Project 2",   //param.Name
			"TEST DESCRIPTION", //param.Description
			"test.com",         //param.Url
			big.NewInt(100),    //param.ZnnFundsNeeded
			big.NewInt(1000),   //param.QsrFundsNeeded
		),
	}).Error(t, nil)
	z.InsertNewMomentum() // cemented send block
	z.InsertNewMomentum() // cemented token-receive-block

	common.Json(acceleratorAPI.GetAll(0, 10)).Equals(t, `
{
	"count": 2,
	"list": [
		{
			"id": "fb056c9bdf8c08b30e8abbd17dc9406be7149878663719ceabb004440e24cdda",
			"owner": "z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx",
			"name": "Test Project 2",
			"description": "TEST DESCRIPTION",
			"url": "test.com",
			"znnFundsNeeded": "100",
			"qsrFundsNeeded": "1000",
			"creationTimestamp": 1000000220,
			"lastUpdateTimestamp": 1000000220,
			"status": 0,
			"phaseIds": [],
			"votes": {
				"id": "fb056c9bdf8c08b30e8abbd17dc9406be7149878663719ceabb004440e24cdda",
				"total": 0,
				"yes": 0,
				"no": 0
			},
			"phases": []
		},
		{
			"id": "c24a5a6166c8948aba23d68aa39e206fc1410138ad218500749b75e2ae92d730",
			"owner": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
			"name": "Test Project 1",
			"description": "TEST DESCRIPTION",
			"url": "test.com",
			"znnFundsNeeded": "100",
			"qsrFundsNeeded": "1000",
			"creationTimestamp": 1000000200,
			"lastUpdateTimestamp": 1000000200,
			"status": 0,
			"phaseIds": [],
			"votes": {
				"id": "c24a5a6166c8948aba23d68aa39e206fc1410138ad218500749b75e2ae92d730",
				"total": 0,
				"yes": 0,
				"no": 0
			},
			"phases": []
		}
	]
}`)
}

func TestAccelerator_VoteByName(t *testing.T) {
	z := mock.NewMockZenonWithCustomEpochDuration(t, time.Hour)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).Equals(t, `
t=2001-09-09T01:46:50+0000 lvl=dbug msg=created module=embedded contract=spork spork="&{Id:34d8229bd07586c243c6e74122a18d6d2002694c72964a7186111026a9cec6ab Name:spork-accelerator Description:activate spork for accelerator Activated:false EnforcementHeight:0}"
t=2001-09-09T01:47:00+0000 lvl=dbug msg=activated module=embedded contract=spork spork="&{Id:34d8229bd07586c243c6e74122a18d6d2002694c72964a7186111026a9cec6ab Name:spork-accelerator Description:activate spork for accelerator Activated:true EnforcementHeight:9}"
t=2001-09-09T01:50:00+0000 lvl=dbug msg="successfully create project" module=embedded contract=accelerator project="&{Id:c24a5a6166c8948aba23d68aa39e206fc1410138ad218500749b75e2ae92d730 Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz Name:Test Project 1 Description:TEST DESCRIPTION Url:test.com ZnnFundsNeeded:+100 QsrFundsNeeded:+1000 CreationTimestamp:1000000200 LastUpdateTimestamp:1000000200 Status:0 PhaseIds:[]}"
t=2001-09-09T01:50:20+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:c24a5a6166c8948aba23d68aa39e206fc1410138ad218500749b75e2ae92d730 Name:TEST-pillar-1 Vote:0}"
`)

	activateAccelerator(z)
	acceleratorAPI := embedded.NewAcceleratorApi(z)
	defer z.CallContract(&nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     types.AcceleratorContract,
		TokenStandard: types.ZnnTokenStandard,
		Amount:        constants.ProjectCreationAmount,
		Data: definition.ABIAccelerator.PackMethodPanic(definition.CreateProjectMethodName,
			"Test Project 1",   //param.Name
			"TEST DESCRIPTION", //param.Description
			"test.com",         //param.Url
			big.NewInt(100),    //param.ZnnFundsNeeded
			big.NewInt(1000),   //param.QsrFundsNeeded
		),
	}).Error(t, nil)
	z.InsertNewMomentum() // cemented send block
	z.InsertNewMomentum() // cemented token-receive-block

	projectList, err := acceleratorAPI.GetAll(0, 10)
	common.FailIfErr(t, err)

	defer z.CallContract(&nom.AccountBlock{
		Address:   g.Pillar1.Address,
		ToAddress: types.AcceleratorContract,
		Data: definition.ABIAccelerator.PackMethodPanic(definition.VoteByNameMethodName,
			projectList.List[0].Id,
			g.Pillar1Name,
			definition.VoteYes,
		),
	}).Error(t, nil)
	z.InsertNewMomentum() // cemented send block
	z.InsertNewMomentum() // cemented token-receive-block

	common.Json(acceleratorAPI.GetAll(0, 10)).Equals(t, `
{
	"count": 1,
	"list": [
		{
			"id": "c24a5a6166c8948aba23d68aa39e206fc1410138ad218500749b75e2ae92d730",
			"owner": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
			"name": "Test Project 1",
			"description": "TEST DESCRIPTION",
			"url": "test.com",
			"znnFundsNeeded": "100",
			"qsrFundsNeeded": "1000",
			"creationTimestamp": 1000000200,
			"lastUpdateTimestamp": 1000000200,
			"status": 0,
			"phaseIds": [],
			"votes": {
				"id": "c24a5a6166c8948aba23d68aa39e206fc1410138ad218500749b75e2ae92d730",
				"total": 1,
				"yes": 1,
				"no": 0
			},
			"phases": []
		}
	]
}`)
	common.Json(acceleratorAPI.GetPillarVotes(g.Pillar1Name, projectList.List[0].PhaseIds)).Equals(t, `[]`)
}

func TestAccelerator_VoteByProdAddress(t *testing.T) {
	z := mock.NewMockZenonWithCustomEpochDuration(t, time.Hour)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).Equals(t, `
t=2001-09-09T01:46:50+0000 lvl=dbug msg=created module=embedded contract=spork spork="&{Id:34d8229bd07586c243c6e74122a18d6d2002694c72964a7186111026a9cec6ab Name:spork-accelerator Description:activate spork for accelerator Activated:false EnforcementHeight:0}"
t=2001-09-09T01:47:00+0000 lvl=dbug msg=activated module=embedded contract=spork spork="&{Id:34d8229bd07586c243c6e74122a18d6d2002694c72964a7186111026a9cec6ab Name:spork-accelerator Description:activate spork for accelerator Activated:true EnforcementHeight:9}"
t=2001-09-09T01:50:00+0000 lvl=dbug msg="successfully create project" module=embedded contract=accelerator project="&{Id:c24a5a6166c8948aba23d68aa39e206fc1410138ad218500749b75e2ae92d730 Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz Name:Test Project 1 Description:TEST DESCRIPTION Url:test.com ZnnFundsNeeded:+100 QsrFundsNeeded:+1000 CreationTimestamp:1000000200 LastUpdateTimestamp:1000000200 Status:0 PhaseIds:[]}"
t=2001-09-09T01:50:20+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:c24a5a6166c8948aba23d68aa39e206fc1410138ad218500749b75e2ae92d730 Name:TEST-pillar-1 Vote:1}"
`)

	activateAccelerator(z)
	acceleratorAPI := embedded.NewAcceleratorApi(z)
	defer z.CallContract(&nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     types.AcceleratorContract,
		TokenStandard: types.ZnnTokenStandard,
		Amount:        constants.ProjectCreationAmount,
		Data: definition.ABIAccelerator.PackMethodPanic(definition.CreateProjectMethodName,
			"Test Project 1",   //param.Name
			"TEST DESCRIPTION", //param.Description
			"test.com",         //param.Url
			big.NewInt(100),    //param.ZnnFundsNeeded
			big.NewInt(1000),   //param.QsrFundsNeeded
		),
	}).Error(t, nil)
	z.InsertNewMomentum() // cemented send block
	z.InsertNewMomentum() // cemented token-receive-block

	projectList, err := acceleratorAPI.GetAll(0, 10)
	common.FailIfErr(t, err)

	defer z.CallContract(&nom.AccountBlock{
		Address:   g.Pillar1.Address,
		ToAddress: types.AcceleratorContract,
		Data: definition.ABIAccelerator.PackMethodPanic(definition.VoteByProdAddressMethodName,
			projectList.List[0].Id,
			definition.VoteNo,
		),
	}).Error(t, nil)
	z.InsertNewMomentum() // cemented send block
	z.InsertNewMomentum() // cemented token-receive-block

	common.Json(acceleratorAPI.GetAll(0, 10)).Equals(t, `
{
	"count": 1,
	"list": [
		{
			"id": "c24a5a6166c8948aba23d68aa39e206fc1410138ad218500749b75e2ae92d730",
			"owner": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
			"name": "Test Project 1",
			"description": "TEST DESCRIPTION",
			"url": "test.com",
			"znnFundsNeeded": "100",
			"qsrFundsNeeded": "1000",
			"creationTimestamp": 1000000200,
			"lastUpdateTimestamp": 1000000200,
			"status": 0,
			"phaseIds": [],
			"votes": {
				"id": "c24a5a6166c8948aba23d68aa39e206fc1410138ad218500749b75e2ae92d730",
				"total": 1,
				"yes": 0,
				"no": 1
			},
			"phases": []
		}
	]
}`)
}

func TestAccelerator_Update(t *testing.T) {
	z := mock.NewMockZenonWithCustomEpochDuration(t, time.Hour)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).Equals(t, `
t=2001-09-09T01:46:50+0000 lvl=dbug msg=created module=embedded contract=spork spork="&{Id:34d8229bd07586c243c6e74122a18d6d2002694c72964a7186111026a9cec6ab Name:spork-accelerator Description:activate spork for accelerator Activated:false EnforcementHeight:0}"
t=2001-09-09T01:47:00+0000 lvl=dbug msg=activated module=embedded contract=spork spork="&{Id:34d8229bd07586c243c6e74122a18d6d2002694c72964a7186111026a9cec6ab Name:spork-accelerator Description:activate spork for accelerator Activated:true EnforcementHeight:9}"
t=2001-09-09T01:50:00+0000 lvl=dbug msg="successfully create project" module=embedded contract=accelerator project="&{Id:c24a5a6166c8948aba23d68aa39e206fc1410138ad218500749b75e2ae92d730 Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz Name:Test Project 1 Description:TEST DESCRIPTION Url:test.com ZnnFundsNeeded:+100 QsrFundsNeeded:+1000 CreationTimestamp:1000000200 LastUpdateTimestamp:1000000200 Status:0 PhaseIds:[]}"
t=2001-09-09T01:50:20+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:c24a5a6166c8948aba23d68aa39e206fc1410138ad218500749b75e2ae92d730 Name:TEST-pillar-1 Vote:0}"
t=2001-09-09T01:50:40+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:c24a5a6166c8948aba23d68aa39e206fc1410138ad218500749b75e2ae92d730 Name:TEST-pillar-cool Vote:0}"
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=0 pillar-name=TEST-pillar-1 reward="&{DelegationReward:+11995123837 BlockReward:+9916666627 TotalReward:+21911790464 ProducedBlockNum:119 ExpectedBlockNum:120 Weight:+2099916666666}" total-weight=2499916666666 self-weight=2099916666666
t=2001-09-09T02:46:40+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=0 pillar-name=TEST-pillar-cool reward="&{DelegationReward:+1152038401 BlockReward:+9999999960 TotalReward:+11152038361 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2499916666666 self-weight=200000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=0 pillar-name=TEST-pillar-znn reward="&{DelegationReward:+1152038401 BlockReward:+9999999960 TotalReward:+11152038361 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2499916666666 self-weight=200000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=pillar epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxsentynelxxxxxxxxxxxxxwy0r2r current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating sentinel reward" module=embedded contract=sentinel epoch=0 total-znn-reward=187200000000 total-qsr-reward=500000000000 cumulated-sentinel=0 start-time=1000000000 end-time=1000003600
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=sentinel epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating stake reward" module=embedded contract=stake epoch=0 total-reward=1000000000000 cumulated-stake=0 start-time=1000000000 end-time=1000003600
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=stake epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating liquidity reward" module=embedded contract=liquidity epoch=0 znn-amount=187200000000 qsr-amount=500000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=liquidity epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxaccelerat0rxxxxxxxxxxp4tk22 current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="check accelerator votes" module=embedded contract=accelerator votes="&{Id:c24a5a6166c8948aba23d68aa39e206fc1410138ad218500749b75e2ae92d730 Total:2 Yes:2 No:0}" status=true
t=2001-09-09T02:46:40+0000 lvl=dbug msg="project passed voting period" module=embedded contract=accelerator project-id=c24a5a6166c8948aba23d68aa39e206fc1410138ad218500749b75e2ae92d730 passed-votes=true
t=2001-09-09T02:46:50+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg TokenName:Zenon Coin TokenSymbol:ZNN TokenDomain:zenon.network TotalSupply:+19687200000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1znnxxxxxxxxxxxxx9z4ulx}" minted-amount=187200000000 to-address=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae
t=2001-09-09T02:46:50+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 TokenName:QuasarCoin TokenSymbol:QSR TokenDomain:zenon.network TotalSupply:+181050000000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1qsrxxxxxxxxxxxxxmrhjll}" minted-amount=500000000000 to-address=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae
`)

	activateAccelerator(z)
	acceleratorAPI := embedded.NewAcceleratorApi(z)
	defer z.CallContract(&nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     types.AcceleratorContract,
		TokenStandard: types.ZnnTokenStandard,
		Amount:        constants.ProjectCreationAmount,
		Data: definition.ABIAccelerator.PackMethodPanic(definition.CreateProjectMethodName,
			"Test Project 1",   //param.Name
			"TEST DESCRIPTION", //param.Description
			"test.com",         //param.Url
			big.NewInt(100),    //param.ZnnFundsNeeded
			big.NewInt(1000),   //param.QsrFundsNeeded
		),
	}).Error(t, nil)
	z.InsertNewMomentum() // cemented send block
	z.InsertNewMomentum() // cemented token-receive-block

	projectList, err := acceleratorAPI.GetAll(0, 10)
	common.FailIfErr(t, err)

	defer z.CallContract(&nom.AccountBlock{
		Address:   g.Pillar1.Address,
		ToAddress: types.AcceleratorContract,
		Data: definition.ABIAccelerator.PackMethodPanic(definition.VoteByProdAddressMethodName,
			projectList.List[0].Id,
			definition.VoteYes,
		),
	}).Error(t, nil)
	z.InsertNewMomentum() // cemented send block
	z.InsertNewMomentum() // cemented token-receive-block

	defer z.CallContract(&nom.AccountBlock{
		Address:   g.Pillar2.Address,
		ToAddress: types.AcceleratorContract,
		Data: definition.ABIAccelerator.PackMethodPanic(definition.VoteByProdAddressMethodName,
			projectList.List[0].Id,
			definition.VoteYes,
		),
	}).Error(t, nil)
	z.InsertNewMomentum() // cemented send block
	z.InsertNewMomentum() // cemented token-receive-block

	z.InsertMomentumsTo(60*6 + 2)
	z.ExpectBalance(types.AcceleratorContract, types.ZnnTokenStandard, 100000000)

	common.Json(acceleratorAPI.GetAll(0, 10)).Equals(t, `
{
	"count": 1,
	"list": [
		{
			"id": "c24a5a6166c8948aba23d68aa39e206fc1410138ad218500749b75e2ae92d730",
			"owner": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
			"name": "Test Project 1",
			"description": "TEST DESCRIPTION",
			"url": "test.com",
			"znnFundsNeeded": "100",
			"qsrFundsNeeded": "1000",
			"creationTimestamp": 1000000200,
			"lastUpdateTimestamp": 1000003600,
			"status": 1,
			"phaseIds": [],
			"votes": {
				"id": "c24a5a6166c8948aba23d68aa39e206fc1410138ad218500749b75e2ae92d730",
				"total": 2,
				"yes": 2,
				"no": 0
			},
			"phases": []
		}
	]
}`)
}

func TestAccelerator_CreatePhase(t *testing.T) {
	z := mock.NewMockZenonWithCustomEpochDuration(t, time.Hour)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).Equals(t, `
t=2001-09-09T01:46:50+0000 lvl=dbug msg=created module=embedded contract=spork spork="&{Id:34d8229bd07586c243c6e74122a18d6d2002694c72964a7186111026a9cec6ab Name:spork-accelerator Description:activate spork for accelerator Activated:false EnforcementHeight:0}"
t=2001-09-09T01:47:00+0000 lvl=dbug msg=activated module=embedded contract=spork spork="&{Id:34d8229bd07586c243c6e74122a18d6d2002694c72964a7186111026a9cec6ab Name:spork-accelerator Description:activate spork for accelerator Activated:true EnforcementHeight:9}"
t=2001-09-09T01:50:00+0000 lvl=dbug msg="successfully create project" module=embedded contract=accelerator project="&{Id:c24a5a6166c8948aba23d68aa39e206fc1410138ad218500749b75e2ae92d730 Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz Name:Test Project 1 Description:TEST DESCRIPTION Url:test.com ZnnFundsNeeded:+100 QsrFundsNeeded:+1000 CreationTimestamp:1000000200 LastUpdateTimestamp:1000000200 Status:0 PhaseIds:[]}"
t=2001-09-09T01:50:20+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:c24a5a6166c8948aba23d68aa39e206fc1410138ad218500749b75e2ae92d730 Name:TEST-pillar-1 Vote:0}"
t=2001-09-09T01:50:40+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:c24a5a6166c8948aba23d68aa39e206fc1410138ad218500749b75e2ae92d730 Name:TEST-pillar-cool Vote:0}"
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=0 pillar-name=TEST-pillar-1 reward="&{DelegationReward:+11995123837 BlockReward:+9916666627 TotalReward:+21911790464 ProducedBlockNum:119 ExpectedBlockNum:120 Weight:+2099916666666}" total-weight=2499916666666 self-weight=2099916666666
t=2001-09-09T02:46:40+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=0 pillar-name=TEST-pillar-cool reward="&{DelegationReward:+1152038401 BlockReward:+9999999960 TotalReward:+11152038361 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2499916666666 self-weight=200000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=0 pillar-name=TEST-pillar-znn reward="&{DelegationReward:+1152038401 BlockReward:+9999999960 TotalReward:+11152038361 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2499916666666 self-weight=200000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=pillar epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxsentynelxxxxxxxxxxxxxwy0r2r current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating sentinel reward" module=embedded contract=sentinel epoch=0 total-znn-reward=187200000000 total-qsr-reward=500000000000 cumulated-sentinel=0 start-time=1000000000 end-time=1000003600
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=sentinel epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating stake reward" module=embedded contract=stake epoch=0 total-reward=1000000000000 cumulated-stake=0 start-time=1000000000 end-time=1000003600
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=stake epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating liquidity reward" module=embedded contract=liquidity epoch=0 znn-amount=187200000000 qsr-amount=500000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=liquidity epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxaccelerat0rxxxxxxxxxxp4tk22 current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="check accelerator votes" module=embedded contract=accelerator votes="&{Id:c24a5a6166c8948aba23d68aa39e206fc1410138ad218500749b75e2ae92d730 Total:2 Yes:2 No:0}" status=true
t=2001-09-09T02:46:40+0000 lvl=dbug msg="project passed voting period" module=embedded contract=accelerator project-id=c24a5a6166c8948aba23d68aa39e206fc1410138ad218500749b75e2ae92d730 passed-votes=true
t=2001-09-09T02:46:50+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg TokenName:Zenon Coin TokenSymbol:ZNN TokenDomain:zenon.network TotalSupply:+19687200000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1znnxxxxxxxxxxxxx9z4ulx}" minted-amount=187200000000 to-address=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae
t=2001-09-09T02:46:50+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 TokenName:QuasarCoin TokenSymbol:QSR TokenDomain:zenon.network TotalSupply:+181050000000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1qsrxxxxxxxxxxxxxmrhjll}" minted-amount=500000000000 to-address=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae
t=2001-09-09T02:47:00+0000 lvl=info msg="received donation" module=embedded contract=common embedded=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae from-address=z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0 zts=zts1znnxxxxxxxxxxxxx9z4ulx amount=187200000000
t=2001-09-09T02:47:00+0000 lvl=dbug msg="successfully created phase" module=embedded contract=accelerator phase="&{Id:e2b4eb834a20e51f88dba0232c856949b1462ef0f1fe2cb5049bb84063a781ab ProjectId:c24a5a6166c8948aba23d68aa39e206fc1410138ad218500749b75e2ae92d730 Name:Phase 1 Description:Description for phase 1 Url:www.phase1.com ZnnFundsNeeded:+10 QsrFundsNeeded:+10 CreationTimestamp:1000003620 AcceptedTimestamp:0 Status:0}"
t=2001-09-09T02:47:00+0000 lvl=info msg="received donation" module=embedded contract=common embedded=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae from-address=z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0 zts=zts1qsrxxxxxxxxxxxxxmrhjll amount=500000000000
`)

	activateAccelerator(z)
	acceleratorAPI := embedded.NewAcceleratorApi(z)
	defer z.CallContract(&nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     types.AcceleratorContract,
		TokenStandard: types.ZnnTokenStandard,
		Amount:        constants.ProjectCreationAmount,
		Data: definition.ABIAccelerator.PackMethodPanic(definition.CreateProjectMethodName,
			"Test Project 1",   //param.Name
			"TEST DESCRIPTION", //param.Description
			"test.com",         //param.Url
			big.NewInt(100),    //param.ZnnFundsNeeded
			big.NewInt(1000),   //param.QsrFundsNeeded
		),
	}).Error(t, nil)
	z.InsertNewMomentum() // cemented send block
	z.InsertNewMomentum() // cemented token-receive-block

	projectList, err := acceleratorAPI.GetAll(0, 10)
	common.FailIfErr(t, err)

	defer z.CallContract(&nom.AccountBlock{
		Address:   g.Pillar1.Address,
		ToAddress: types.AcceleratorContract,
		Data: definition.ABIAccelerator.PackMethodPanic(definition.VoteByProdAddressMethodName,
			projectList.List[0].Id,
			definition.VoteYes,
		),
	}).Error(t, nil)
	z.InsertNewMomentum() // cemented send block
	z.InsertNewMomentum() // cemented token-receive-block

	defer z.CallContract(&nom.AccountBlock{
		Address:   g.Pillar2.Address,
		ToAddress: types.AcceleratorContract,
		Data: definition.ABIAccelerator.PackMethodPanic(definition.VoteByProdAddressMethodName,
			projectList.List[0].Id,
			definition.VoteYes,
		),
	}).Error(t, nil)
	z.InsertNewMomentum() // cemented send block
	z.InsertNewMomentum() // cemented token-receive-block

	z.InsertMomentumsTo(60*6 + 2)

	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.AcceleratorContract,
		Data: definition.ABIAccelerator.PackMethodPanic(definition.AddPhaseMethodName,
			projectList.List[0].Id,    //param.Hash
			"Phase 1",                 //param.Name
			"Description for phase 1", //param.Description
			"www.phase1.com",          //param.Url
			big.NewInt(10),            //param.ZnnFundsNeeded
			big.NewInt(10),            //param.QsrFundsNeeded
		),
	}).Error(t, nil)
	z.InsertNewMomentum() // cemented send block
	z.InsertNewMomentum() // cemented token-receive-block

	projectId := types.HexToHashPanic("c24a5a6166c8948aba23d68aa39e206fc1410138ad218500749b75e2ae92d730")
	common.Json(acceleratorAPI.GetProjectById(projectId)).Equals(t, `
{
	"id": "c24a5a6166c8948aba23d68aa39e206fc1410138ad218500749b75e2ae92d730",
	"owner": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
	"name": "Test Project 1",
	"description": "TEST DESCRIPTION",
	"url": "test.com",
	"znnFundsNeeded": "100",
	"qsrFundsNeeded": "1000",
	"creationTimestamp": 1000000200,
	"lastUpdateTimestamp": 1000003620,
	"status": 1,
	"phaseIds": [
		"e2b4eb834a20e51f88dba0232c856949b1462ef0f1fe2cb5049bb84063a781ab"
	],
	"votes": {
		"id": "c24a5a6166c8948aba23d68aa39e206fc1410138ad218500749b75e2ae92d730",
		"total": 2,
		"yes": 2,
		"no": 0
	},
	"phases": [
		{
			"phase": {
				"id": "e2b4eb834a20e51f88dba0232c856949b1462ef0f1fe2cb5049bb84063a781ab",
				"projectID": "c24a5a6166c8948aba23d68aa39e206fc1410138ad218500749b75e2ae92d730",
				"name": "Phase 1",
				"description": "Description for phase 1",
				"url": "www.phase1.com",
				"znnFundsNeeded": "10",
				"qsrFundsNeeded": "10",
				"creationTimestamp": 1000003620,
				"acceptedTimestamp": 0,
				"status": 0
			},
			"votes": {
				"id": "e2b4eb834a20e51f88dba0232c856949b1462ef0f1fe2cb5049bb84063a781ab",
				"total": 0,
				"yes": 0,
				"no": 0
			}
		}
	]
}`)
	phaseId := types.HexToHashPanic("e2b4eb834a20e51f88dba0232c856949b1462ef0f1fe2cb5049bb84063a781ab")
	common.Json(acceleratorAPI.GetPhaseById(phaseId)).Equals(t, `
{
	"phase": {
		"id": "e2b4eb834a20e51f88dba0232c856949b1462ef0f1fe2cb5049bb84063a781ab",
		"projectID": "c24a5a6166c8948aba23d68aa39e206fc1410138ad218500749b75e2ae92d730",
		"name": "Phase 1",
		"description": "Description for phase 1",
		"url": "www.phase1.com",
		"znnFundsNeeded": "10",
		"qsrFundsNeeded": "10",
		"creationTimestamp": 1000003620,
		"acceptedTimestamp": 0,
		"status": 0
	},
	"votes": {
		"id": "e2b4eb834a20e51f88dba0232c856949b1462ef0f1fe2cb5049bb84063a781ab",
		"total": 0,
		"yes": 0,
		"no": 0
	}
}`)
}

func TestAccelerator_UpdatePhase(t *testing.T) {
	z := mock.NewMockZenonWithCustomEpochDuration(t, time.Hour)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).Equals(t, `
t=2001-09-09T01:46:50+0000 lvl=dbug msg=created module=embedded contract=spork spork="&{Id:34d8229bd07586c243c6e74122a18d6d2002694c72964a7186111026a9cec6ab Name:spork-accelerator Description:activate spork for accelerator Activated:false EnforcementHeight:0}"
t=2001-09-09T01:47:00+0000 lvl=dbug msg=activated module=embedded contract=spork spork="&{Id:34d8229bd07586c243c6e74122a18d6d2002694c72964a7186111026a9cec6ab Name:spork-accelerator Description:activate spork for accelerator Activated:true EnforcementHeight:9}"
t=2001-09-09T01:50:00+0000 lvl=dbug msg="successfully create project" module=embedded contract=accelerator project="&{Id:c24a5a6166c8948aba23d68aa39e206fc1410138ad218500749b75e2ae92d730 Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz Name:Test Project 1 Description:TEST DESCRIPTION Url:test.com ZnnFundsNeeded:+100 QsrFundsNeeded:+1000 CreationTimestamp:1000000200 LastUpdateTimestamp:1000000200 Status:0 PhaseIds:[]}"
t=2001-09-09T01:50:20+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:c24a5a6166c8948aba23d68aa39e206fc1410138ad218500749b75e2ae92d730 Name:TEST-pillar-1 Vote:0}"
t=2001-09-09T01:50:40+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:c24a5a6166c8948aba23d68aa39e206fc1410138ad218500749b75e2ae92d730 Name:TEST-pillar-cool Vote:0}"
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=0 pillar-name=TEST-pillar-1 reward="&{DelegationReward:+11995123837 BlockReward:+9916666627 TotalReward:+21911790464 ProducedBlockNum:119 ExpectedBlockNum:120 Weight:+2099916666666}" total-weight=2499916666666 self-weight=2099916666666
t=2001-09-09T02:46:40+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=0 pillar-name=TEST-pillar-cool reward="&{DelegationReward:+1152038401 BlockReward:+9999999960 TotalReward:+11152038361 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2499916666666 self-weight=200000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=0 pillar-name=TEST-pillar-znn reward="&{DelegationReward:+1152038401 BlockReward:+9999999960 TotalReward:+11152038361 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2499916666666 self-weight=200000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=pillar epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxsentynelxxxxxxxxxxxxxwy0r2r current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating sentinel reward" module=embedded contract=sentinel epoch=0 total-znn-reward=187200000000 total-qsr-reward=500000000000 cumulated-sentinel=0 start-time=1000000000 end-time=1000003600
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=sentinel epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating stake reward" module=embedded contract=stake epoch=0 total-reward=1000000000000 cumulated-stake=0 start-time=1000000000 end-time=1000003600
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=stake epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating liquidity reward" module=embedded contract=liquidity epoch=0 znn-amount=187200000000 qsr-amount=500000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=liquidity epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxaccelerat0rxxxxxxxxxxp4tk22 current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="check accelerator votes" module=embedded contract=accelerator votes="&{Id:c24a5a6166c8948aba23d68aa39e206fc1410138ad218500749b75e2ae92d730 Total:2 Yes:2 No:0}" status=true
t=2001-09-09T02:46:40+0000 lvl=dbug msg="project passed voting period" module=embedded contract=accelerator project-id=c24a5a6166c8948aba23d68aa39e206fc1410138ad218500749b75e2ae92d730 passed-votes=true
t=2001-09-09T02:46:50+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg TokenName:Zenon Coin TokenSymbol:ZNN TokenDomain:zenon.network TotalSupply:+19687200000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1znnxxxxxxxxxxxxx9z4ulx}" minted-amount=187200000000 to-address=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae
t=2001-09-09T02:46:50+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 TokenName:QuasarCoin TokenSymbol:QSR TokenDomain:zenon.network TotalSupply:+181050000000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1qsrxxxxxxxxxxxxxmrhjll}" minted-amount=500000000000 to-address=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae
t=2001-09-09T02:47:00+0000 lvl=info msg="received donation" module=embedded contract=common embedded=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae from-address=z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0 zts=zts1znnxxxxxxxxxxxxx9z4ulx amount=187200000000
t=2001-09-09T02:47:00+0000 lvl=dbug msg="successfully created phase" module=embedded contract=accelerator phase="&{Id:e2b4eb834a20e51f88dba0232c856949b1462ef0f1fe2cb5049bb84063a781ab ProjectId:c24a5a6166c8948aba23d68aa39e206fc1410138ad218500749b75e2ae92d730 Name:Phase 1 Description:Description for phase 1 Url:www.phase1.com ZnnFundsNeeded:+10 QsrFundsNeeded:+10 CreationTimestamp:1000003620 AcceptedTimestamp:0 Status:0}"
t=2001-09-09T02:47:00+0000 lvl=info msg="received donation" module=embedded contract=common embedded=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae from-address=z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0 zts=zts1qsrxxxxxxxxxxxxxmrhjll amount=500000000000
t=2001-09-09T02:47:20+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:e2b4eb834a20e51f88dba0232c856949b1462ef0f1fe2cb5049bb84063a781ab Name:TEST-pillar-cool Vote:0}"
t=2001-09-09T02:47:40+0000 lvl=dbug msg="delete pillar vote due to phase update" module=embedded contract=accelerator old-pillar-vote="&{Id:e2b4eb834a20e51f88dba0232c856949b1462ef0f1fe2cb5049bb84063a781ab Name:TEST-pillar-cool Vote:0}"
t=2001-09-09T02:47:40+0000 lvl=dbug msg="delete phase hash due to phase update" module=embedded contract=accelerator old-phase-hash=e2b4eb834a20e51f88dba0232c856949b1462ef0f1fe2cb5049bb84063a781ab
t=2001-09-09T02:47:40+0000 lvl=dbug msg="successfully updated phase" module=embedded contract=accelerator old-phase="&{Id:e2b4eb834a20e51f88dba0232c856949b1462ef0f1fe2cb5049bb84063a781ab ProjectId:c24a5a6166c8948aba23d68aa39e206fc1410138ad218500749b75e2ae92d730 Name:Phase 1 Description:Description for phase 1 Url:www.phase1.com ZnnFundsNeeded:+10 QsrFundsNeeded:+10 CreationTimestamp:1000003620 AcceptedTimestamp:0 Status:0}" new-phase="&{Id:d6c8d33db992af0d2b9f3cfa05cfbae093dd2b2deeda7565661f5d6d095be1a7 ProjectId:c24a5a6166c8948aba23d68aa39e206fc1410138ad218500749b75e2ae92d730 Name:Phase 1.1 Description:Description for phase 1.1 Url:www.phase1.com ZnnFundsNeeded:+15 QsrFundsNeeded:+15 CreationTimestamp:1000003660 AcceptedTimestamp:0 Status:0}"
`)

	activateAccelerator(z)
	acceleratorAPI := embedded.NewAcceleratorApi(z)
	defer z.CallContract(&nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     types.AcceleratorContract,
		TokenStandard: types.ZnnTokenStandard,
		Amount:        constants.ProjectCreationAmount,
		Data: definition.ABIAccelerator.PackMethodPanic(definition.CreateProjectMethodName,
			"Test Project 1",   //param.Name
			"TEST DESCRIPTION", //param.Description
			"test.com",         //param.Url
			big.NewInt(100),    //param.ZnnFundsNeeded
			big.NewInt(1000),   //param.QsrFundsNeeded
		),
	}).Error(t, nil)
	z.InsertNewMomentum() // cemented send block
	z.InsertNewMomentum() // cemented token-receive-block

	projectList, err := acceleratorAPI.GetAll(0, 10)
	common.FailIfErr(t, err)

	defer z.CallContract(&nom.AccountBlock{
		Address:   g.Pillar1.Address,
		ToAddress: types.AcceleratorContract,
		Data: definition.ABIAccelerator.PackMethodPanic(definition.VoteByProdAddressMethodName,
			projectList.List[0].Id,
			definition.VoteYes,
		),
	}).Error(t, nil)
	z.InsertNewMomentum() // cemented send block
	z.InsertNewMomentum() // cemented token-receive-block

	defer z.CallContract(&nom.AccountBlock{
		Address:   g.Pillar2.Address,
		ToAddress: types.AcceleratorContract,
		Data: definition.ABIAccelerator.PackMethodPanic(definition.VoteByProdAddressMethodName,
			projectList.List[0].Id,
			definition.VoteYes,
		),
	}).Error(t, nil)
	z.InsertNewMomentum() // cemented send block
	z.InsertNewMomentum() // cemented token-receive-block

	z.InsertMomentumsTo(60*6 + 2)

	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.AcceleratorContract,
		Data: definition.ABIAccelerator.PackMethodPanic(definition.AddPhaseMethodName,
			projectList.List[0].Id,    //param.Hash
			"Phase 1",                 //param.Name
			"Description for phase 1", //param.Description
			"www.phase1.com",          //param.Url
			big.NewInt(10),            //param.ZnnFundsNeeded
			big.NewInt(10),            //param.QsrFundsNeeded
		),
	}).Error(t, nil)
	z.InsertNewMomentum() // cemented send block
	z.InsertNewMomentum() // cemented token-receive-block

	projectList, err = acceleratorAPI.GetAll(0, 10)
	common.FailIfErr(t, err)

	defer z.CallContract(&nom.AccountBlock{
		Address:   g.Pillar2.Address,
		ToAddress: types.AcceleratorContract,
		Data: definition.ABIAccelerator.PackMethodPanic(definition.VoteByProdAddressMethodName,
			projectList.List[0].Phases[0].Phase.Id,
			definition.VoteYes,
		),
	}).Error(t, nil)
	z.InsertNewMomentum()
	z.InsertNewMomentum()

	common.Json(acceleratorAPI.GetAll(0, 10)).Equals(t, `
{
	"count": 1,
	"list": [
		{
			"id": "c24a5a6166c8948aba23d68aa39e206fc1410138ad218500749b75e2ae92d730",
			"owner": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
			"name": "Test Project 1",
			"description": "TEST DESCRIPTION",
			"url": "test.com",
			"znnFundsNeeded": "100",
			"qsrFundsNeeded": "1000",
			"creationTimestamp": 1000000200,
			"lastUpdateTimestamp": 1000003620,
			"status": 1,
			"phaseIds": [
				"e2b4eb834a20e51f88dba0232c856949b1462ef0f1fe2cb5049bb84063a781ab"
			],
			"votes": {
				"id": "c24a5a6166c8948aba23d68aa39e206fc1410138ad218500749b75e2ae92d730",
				"total": 2,
				"yes": 2,
				"no": 0
			},
			"phases": [
				{
					"phase": {
						"id": "e2b4eb834a20e51f88dba0232c856949b1462ef0f1fe2cb5049bb84063a781ab",
						"projectID": "c24a5a6166c8948aba23d68aa39e206fc1410138ad218500749b75e2ae92d730",
						"name": "Phase 1",
						"description": "Description for phase 1",
						"url": "www.phase1.com",
						"znnFundsNeeded": "10",
						"qsrFundsNeeded": "10",
						"creationTimestamp": 1000003620,
						"acceptedTimestamp": 0,
						"status": 0
					},
					"votes": {
						"id": "e2b4eb834a20e51f88dba0232c856949b1462ef0f1fe2cb5049bb84063a781ab",
						"total": 1,
						"yes": 1,
						"no": 0
					}
				}
			]
		}
	]
}`)

	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.AcceleratorContract,
		Data: definition.ABIAccelerator.PackMethodPanic(definition.UpdatePhaseMethodName,
			projectList.List[0].Id,      //param.Hash
			"Phase 1.1",                 //param.Name
			"Description for phase 1.1", //param.Description
			"www.phase1.com",            //param.Url
			big.NewInt(15),              //param.ZnnFundsNeeded
			big.NewInt(15),              //param.QsrFundsNeeded
		),
	}).Error(t, nil)
	z.InsertNewMomentum() // cemented send block
	z.InsertNewMomentum() // cemented token-receive-block
	common.Json(acceleratorAPI.GetAll(0, 10)).Equals(t, `
{
	"count": 1,
	"list": [
		{
			"id": "c24a5a6166c8948aba23d68aa39e206fc1410138ad218500749b75e2ae92d730",
			"owner": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
			"name": "Test Project 1",
			"description": "TEST DESCRIPTION",
			"url": "test.com",
			"znnFundsNeeded": "100",
			"qsrFundsNeeded": "1000",
			"creationTimestamp": 1000000200,
			"lastUpdateTimestamp": 1000003620,
			"status": 1,
			"phaseIds": [
				"d6c8d33db992af0d2b9f3cfa05cfbae093dd2b2deeda7565661f5d6d095be1a7"
			],
			"votes": {
				"id": "c24a5a6166c8948aba23d68aa39e206fc1410138ad218500749b75e2ae92d730",
				"total": 2,
				"yes": 2,
				"no": 0
			},
			"phases": [
				{
					"phase": {
						"id": "d6c8d33db992af0d2b9f3cfa05cfbae093dd2b2deeda7565661f5d6d095be1a7",
						"projectID": "c24a5a6166c8948aba23d68aa39e206fc1410138ad218500749b75e2ae92d730",
						"name": "Phase 1.1",
						"description": "Description for phase 1.1",
						"url": "www.phase1.com",
						"znnFundsNeeded": "15",
						"qsrFundsNeeded": "15",
						"creationTimestamp": 1000003660,
						"acceptedTimestamp": 0,
						"status": 0
					},
					"votes": {
						"id": "d6c8d33db992af0d2b9f3cfa05cfbae093dd2b2deeda7565661f5d6d095be1a7",
						"total": 0,
						"yes": 0,
						"no": 0
					}
				}
			]
		}
	]
}`)
}

func TestAccelerator_CreteProjectWithInvalidAmount(t *testing.T) {
	z := mock.NewMockZenonWithCustomEpochDuration(t, time.Hour)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).Equals(t, `
t=2001-09-09T01:46:50+0000 lvl=dbug msg=created module=embedded contract=spork spork="&{Id:34d8229bd07586c243c6e74122a18d6d2002694c72964a7186111026a9cec6ab Name:spork-accelerator Description:activate spork for accelerator Activated:false EnforcementHeight:0}"
t=2001-09-09T01:47:00+0000 lvl=dbug msg=activated module=embedded contract=spork spork="&{Id:34d8229bd07586c243c6e74122a18d6d2002694c72964a7186111026a9cec6ab Name:spork-accelerator Description:activate spork for accelerator Activated:true EnforcementHeight:9}"
`)

	activateAccelerator(z)
	defer z.InsertSendBlock(&nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     types.AcceleratorContract,
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(5 * constants.Decimals),
		Data: definition.ABIAccelerator.PackMethodPanic(definition.CreateProjectMethodName,
			"Test Project 1",   //param.Name
			"TEST DESCRIPTION", //param.Description
			"test.com",         //param.Url
			big.NewInt(100),    //param.ZnnFundsNeeded
			big.NewInt(1000),   //param.QsrFundsNeeded
		),
	}, constants.ErrInvalidTokenOrAmount, mock.NoVmChanges)
	z.InsertNewMomentum() // cemented send block
	z.InsertNewMomentum() // cemented token-receive-block

	defer z.InsertSendBlock(&nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     types.AcceleratorContract,
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(15 * constants.Decimals),
		Data: definition.ABIAccelerator.PackMethodPanic(definition.CreateProjectMethodName,
			"Test Project 1",   //param.Name
			"TEST DESCRIPTION", //param.Description
			"test.com",         //param.Url
			big.NewInt(100),    //param.ZnnFundsNeeded
			big.NewInt(1000),   //param.QsrFundsNeeded
		),
	}, constants.ErrInvalidTokenOrAmount, mock.NoVmChanges)
	z.InsertNewMomentum() // cemented send block
	z.InsertNewMomentum() // cemented token-receive-block

	defer z.InsertSendBlock(&nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     types.AcceleratorContract,
		TokenStandard: types.QsrTokenStandard,
		Amount:        constants.ProjectCreationAmount,
		Data: definition.ABIAccelerator.PackMethodPanic(definition.CreateProjectMethodName,
			"Test Project 1",   //param.Name
			"TEST DESCRIPTION", //param.Description
			"test.com",         //param.Url
			big.NewInt(100),    //param.ZnnFundsNeeded
			big.NewInt(1000),   //param.QsrFundsNeeded
		),
	}, constants.ErrInvalidTokenOrAmount, mock.NoVmChanges)
	z.InsertNewMomentum() // cemented send block
	z.InsertNewMomentum() // cemented token-receive-block

	z.ExpectBalance(g.User1.Address, types.ZnnTokenStandard, 1200000000000)
	z.ExpectBalance(g.User1.Address, types.QsrTokenStandard, 12000000000000)
}

func TestAccelerator_DoublePhases(t *testing.T) {
	z := mock.NewMockZenonWithCustomEpochDuration(t, time.Hour)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).Equals(t, `
t=2001-09-09T01:46:50+0000 lvl=dbug msg=created module=embedded contract=spork spork="&{Id:34d8229bd07586c243c6e74122a18d6d2002694c72964a7186111026a9cec6ab Name:spork-accelerator Description:activate spork for accelerator Activated:false EnforcementHeight:0}"
t=2001-09-09T01:47:00+0000 lvl=dbug msg=activated module=embedded contract=spork spork="&{Id:34d8229bd07586c243c6e74122a18d6d2002694c72964a7186111026a9cec6ab Name:spork-accelerator Description:activate spork for accelerator Activated:true EnforcementHeight:9}"
t=2001-09-09T01:50:00+0000 lvl=dbug msg="successfully create project" module=embedded contract=accelerator project="&{Id:3821d3a7f16d0155b476bdfbc8ccb849651d5b9c2a2bef3767675f0cb297ff4f Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz Name:Test Project 1 Description:TEST DESCRIPTION Url:test.com ZnnFundsNeeded:+100 QsrFundsNeeded:+0 CreationTimestamp:1000000200 LastUpdateTimestamp:1000000200 Status:0 PhaseIds:[]}"
t=2001-09-09T01:50:20+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:3821d3a7f16d0155b476bdfbc8ccb849651d5b9c2a2bef3767675f0cb297ff4f Name:TEST-pillar-1 Vote:0}"
t=2001-09-09T01:50:40+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:3821d3a7f16d0155b476bdfbc8ccb849651d5b9c2a2bef3767675f0cb297ff4f Name:TEST-pillar-cool Vote:0}"
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=0 pillar-name=TEST-pillar-1 reward="&{DelegationReward:+11995123837 BlockReward:+9916666627 TotalReward:+21911790464 ProducedBlockNum:119 ExpectedBlockNum:120 Weight:+2099916666666}" total-weight=2499916666666 self-weight=2099916666666
t=2001-09-09T02:46:40+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=0 pillar-name=TEST-pillar-cool reward="&{DelegationReward:+1152038401 BlockReward:+9999999960 TotalReward:+11152038361 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2499916666666 self-weight=200000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=0 pillar-name=TEST-pillar-znn reward="&{DelegationReward:+1152038401 BlockReward:+9999999960 TotalReward:+11152038361 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2499916666666 self-weight=200000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=pillar epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxsentynelxxxxxxxxxxxxxwy0r2r current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating sentinel reward" module=embedded contract=sentinel epoch=0 total-znn-reward=187200000000 total-qsr-reward=500000000000 cumulated-sentinel=0 start-time=1000000000 end-time=1000003600
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=sentinel epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating stake reward" module=embedded contract=stake epoch=0 total-reward=1000000000000 cumulated-stake=0 start-time=1000000000 end-time=1000003600
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=stake epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating liquidity reward" module=embedded contract=liquidity epoch=0 znn-amount=187200000000 qsr-amount=500000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=liquidity epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxaccelerat0rxxxxxxxxxxp4tk22 current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="check accelerator votes" module=embedded contract=accelerator votes="&{Id:3821d3a7f16d0155b476bdfbc8ccb849651d5b9c2a2bef3767675f0cb297ff4f Total:2 Yes:2 No:0}" status=true
t=2001-09-09T02:46:40+0000 lvl=dbug msg="project passed voting period" module=embedded contract=accelerator project-id=3821d3a7f16d0155b476bdfbc8ccb849651d5b9c2a2bef3767675f0cb297ff4f passed-votes=true
t=2001-09-09T02:46:50+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg TokenName:Zenon Coin TokenSymbol:ZNN TokenDomain:zenon.network TotalSupply:+19687200000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1znnxxxxxxxxxxxxx9z4ulx}" minted-amount=187200000000 to-address=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae
t=2001-09-09T02:46:50+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 TokenName:QuasarCoin TokenSymbol:QSR TokenDomain:zenon.network TotalSupply:+181050000000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1qsrxxxxxxxxxxxxxmrhjll}" minted-amount=500000000000 to-address=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae
t=2001-09-09T02:47:00+0000 lvl=info msg="received donation" module=embedded contract=common embedded=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae from-address=z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0 zts=zts1znnxxxxxxxxxxxxx9z4ulx amount=187200000000
t=2001-09-09T02:47:00+0000 lvl=dbug msg="successfully created phase" module=embedded contract=accelerator phase="&{Id:3364612a108e4490ab1c789583e0cbd451597e889a441bdcf4fcc8a6e95c705f ProjectId:3821d3a7f16d0155b476bdfbc8ccb849651d5b9c2a2bef3767675f0cb297ff4f Name:Phase 1 Description:Description for phase 1 Url:www.phase1.com ZnnFundsNeeded:+50 QsrFundsNeeded:+0 CreationTimestamp:1000003620 AcceptedTimestamp:0 Status:0}"
t=2001-09-09T02:47:00+0000 lvl=info msg="received donation" module=embedded contract=common embedded=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae from-address=z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0 zts=zts1qsrxxxxxxxxxxxxxmrhjll amount=500000000000
t=2001-09-09T02:47:20+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:3364612a108e4490ab1c789583e0cbd451597e889a441bdcf4fcc8a6e95c705f Name:TEST-pillar-1 Vote:0}"
t=2001-09-09T02:47:40+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:3364612a108e4490ab1c789583e0cbd451597e889a441bdcf4fcc8a6e95c705f Name:TEST-pillar-cool Vote:0}"
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg current-height=722 last-update-height=361
t=2001-09-09T03:46:50+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=1 pillar-name=TEST-pillar-1 reward="&{DelegationReward:+12095907836 BlockReward:+9999999960 TotalReward:+22095907796 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+2099900000000}" total-weight=2499900000000 self-weight=2099900000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=1 pillar-name=TEST-pillar-cool reward="&{DelegationReward:+1152046081 BlockReward:+9999999960 TotalReward:+11152046041 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2499900000000 self-weight=200000000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=1 pillar-name=TEST-pillar-znn reward="&{DelegationReward:+1152046081 BlockReward:+9999999960 TotalReward:+11152046041 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2499900000000 self-weight=200000000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=pillar epoch=2
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxsentynelxxxxxxxxxxxxxwy0r2r current-height=722 last-update-height=361
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating sentinel reward" module=embedded contract=sentinel epoch=1 total-znn-reward=187200000000 total-qsr-reward=500000000000 cumulated-sentinel=0 start-time=1000003600 end-time=1000007200
t=2001-09-09T03:46:50+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=sentinel epoch=2
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 current-height=722 last-update-height=361
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating stake reward" module=embedded contract=stake epoch=1 total-reward=1000000000000 cumulated-stake=0 start-time=1000003600 end-time=1000007200
t=2001-09-09T03:46:50+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=stake epoch=2
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae current-height=722 last-update-height=361
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating liquidity reward" module=embedded contract=liquidity epoch=1 znn-amount=187200000000 qsr-amount=500000000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=liquidity epoch=2
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxaccelerat0rxxxxxxxxxxp4tk22 current-height=722 last-update-height=361
t=2001-09-09T03:46:50+0000 lvl=dbug msg="check accelerator votes" module=embedded contract=accelerator votes="&{Id:3364612a108e4490ab1c789583e0cbd451597e889a441bdcf4fcc8a6e95c705f Total:2 Yes:2 No:0}" status=true
t=2001-09-09T03:46:50+0000 lvl=dbug msg="finishing and paying phase" module=embedded contract=accelerator project-id=3821d3a7f16d0155b476bdfbc8ccb849651d5b9c2a2bef3767675f0cb297ff4f phase-id=3364612a108e4490ab1c789583e0cbd451597e889a441bdcf4fcc8a6e95c705f znn-amount=50 qsr-amount=0
t=2001-09-09T03:47:00+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg TokenName:Zenon Coin TokenSymbol:ZNN TokenDomain:zenon.network TotalSupply:+19874400000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1znnxxxxxxxxxxxxx9z4ulx}" minted-amount=187200000000 to-address=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae
t=2001-09-09T03:47:00+0000 lvl=dbug msg="successfully created phase" module=embedded contract=accelerator phase="&{Id:8bf0d7d086296ea2c90224094e26f41e68a2896f8cb4d010eda01937fc1ef830 ProjectId:3821d3a7f16d0155b476bdfbc8ccb849651d5b9c2a2bef3767675f0cb297ff4f Name:Phase 2 Description:Description for phase 2 Url:www.phase1.com ZnnFundsNeeded:+50 QsrFundsNeeded:+0 CreationTimestamp:1000007220 AcceptedTimestamp:0 Status:0}"
t=2001-09-09T03:47:00+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 TokenName:QuasarCoin TokenSymbol:QSR TokenDomain:zenon.network TotalSupply:+181550000000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1qsrxxxxxxxxxxxxxmrhjll}" minted-amount=500000000000 to-address=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae
t=2001-09-09T03:47:10+0000 lvl=info msg="received donation" module=embedded contract=common embedded=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae from-address=z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0 zts=zts1znnxxxxxxxxxxxxx9z4ulx amount=187200000000
t=2001-09-09T03:47:10+0000 lvl=info msg="received donation" module=embedded contract=common embedded=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae from-address=z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0 zts=zts1qsrxxxxxxxxxxxxxmrhjll amount=500000000000
t=2001-09-09T03:47:20+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:8bf0d7d086296ea2c90224094e26f41e68a2896f8cb4d010eda01937fc1ef830 Name:TEST-pillar-1 Vote:0}"
t=2001-09-09T03:47:40+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:8bf0d7d086296ea2c90224094e26f41e68a2896f8cb4d010eda01937fc1ef830 Name:TEST-pillar-cool Vote:0}"
t=2001-09-09T04:47:00+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg current-height=1083 last-update-height=722
t=2001-09-09T04:47:00+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=2 pillar-name=TEST-pillar-1 reward="&{DelegationReward:+12095907836 BlockReward:+9999999960 TotalReward:+22095907796 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+2099900000000}" total-weight=2499900000000 self-weight=2099900000000
t=2001-09-09T04:47:00+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=2 pillar-name=TEST-pillar-cool reward="&{DelegationReward:+1152046081 BlockReward:+9999999960 TotalReward:+11152046041 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2499900000000 self-weight=200000000000
t=2001-09-09T04:47:00+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=2 pillar-name=TEST-pillar-znn reward="&{DelegationReward:+1152046081 BlockReward:+9999999960 TotalReward:+11152046041 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2499900000000 self-weight=200000000000
t=2001-09-09T04:47:00+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=pillar epoch=3
t=2001-09-09T04:47:00+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxsentynelxxxxxxxxxxxxxwy0r2r current-height=1083 last-update-height=722
t=2001-09-09T04:47:00+0000 lvl=dbug msg="updating sentinel reward" module=embedded contract=sentinel epoch=2 total-znn-reward=187200000000 total-qsr-reward=500000000000 cumulated-sentinel=0 start-time=1000007200 end-time=1000010800
t=2001-09-09T04:47:00+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=sentinel epoch=3
t=2001-09-09T04:47:00+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 current-height=1083 last-update-height=722
t=2001-09-09T04:47:00+0000 lvl=dbug msg="updating stake reward" module=embedded contract=stake epoch=2 total-reward=1000000000000 cumulated-stake=0 start-time=1000007200 end-time=1000010800
t=2001-09-09T04:47:00+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=stake epoch=3
t=2001-09-09T04:47:00+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae current-height=1083 last-update-height=722
t=2001-09-09T04:47:00+0000 lvl=dbug msg="updating liquidity reward" module=embedded contract=liquidity epoch=2 znn-amount=187200000000 qsr-amount=500000000000
t=2001-09-09T04:47:00+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=liquidity epoch=3
t=2001-09-09T04:47:00+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxaccelerat0rxxxxxxxxxxp4tk22 current-height=1083 last-update-height=722
t=2001-09-09T04:47:00+0000 lvl=dbug msg="check accelerator votes" module=embedded contract=accelerator votes="&{Id:8bf0d7d086296ea2c90224094e26f41e68a2896f8cb4d010eda01937fc1ef830 Total:2 Yes:2 No:0}" status=true
t=2001-09-09T04:47:00+0000 lvl=dbug msg="finishing and paying phase" module=embedded contract=accelerator project-id=3821d3a7f16d0155b476bdfbc8ccb849651d5b9c2a2bef3767675f0cb297ff4f phase-id=8bf0d7d086296ea2c90224094e26f41e68a2896f8cb4d010eda01937fc1ef830 znn-amount=50 qsr-amount=0
t=2001-09-09T04:47:10+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg TokenName:Zenon Coin TokenSymbol:ZNN TokenDomain:zenon.network TotalSupply:+20061600000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1znnxxxxxxxxxxxxx9z4ulx}" minted-amount=187200000000 to-address=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae
t=2001-09-09T04:47:10+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 TokenName:QuasarCoin TokenSymbol:QSR TokenDomain:zenon.network TotalSupply:+182050000000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1qsrxxxxxxxxxxxxxmrhjll}" minted-amount=500000000000 to-address=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae
`)

	activateAccelerator(z)
	acceleratorAPI := embedded.NewAcceleratorApi(z)

	defer z.CallContract(&nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     types.AcceleratorContract,
		TokenStandard: types.ZnnTokenStandard,
		Amount:        constants.ProjectCreationAmount,
		Data: definition.ABIAccelerator.PackMethodPanic(definition.CreateProjectMethodName,
			"Test Project 1",   //param.Name
			"TEST DESCRIPTION", //param.Description
			"test.com",         //param.Url
			big.NewInt(100),    //param.ZnnFundsNeeded
			big.NewInt(0),      //param.QsrFundsNeeded
		),
	}).Error(t, nil)
	z.InsertNewMomentum() // cemented send block
	z.InsertNewMomentum() // cemented token-receive-block

	projectList, err := acceleratorAPI.GetAll(0, 10)
	common.FailIfErr(t, err)

	defer z.CallContract(&nom.AccountBlock{
		Address:   g.Pillar1.Address,
		ToAddress: types.AcceleratorContract,
		Data: definition.ABIAccelerator.PackMethodPanic(definition.VoteByProdAddressMethodName,
			projectList.List[0].Id,
			definition.VoteYes,
		),
	}).Error(t, nil)
	z.InsertNewMomentum() // cemented send block
	z.InsertNewMomentum() // cemented token-receive-block

	defer z.CallContract(&nom.AccountBlock{
		Address:   g.Pillar2.Address,
		ToAddress: types.AcceleratorContract,
		Data: definition.ABIAccelerator.PackMethodPanic(definition.VoteByProdAddressMethodName,
			projectList.List[0].Id,
			definition.VoteYes,
		),
	}).Error(t, nil)
	z.InsertNewMomentum() // cemented send block
	z.InsertNewMomentum() // cemented token-receive-block

	z.InsertMomentumsTo(60*6 + 2)

	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.AcceleratorContract,
		Data: definition.ABIAccelerator.PackMethodPanic(definition.AddPhaseMethodName,
			projectList.List[0].Id,    //param.Hash
			"Phase 1",                 //param.Name
			"Description for phase 1", //param.Description
			"www.phase1.com",          //param.Url
			big.NewInt(50),            //param.ZnnFundsNeeded
			big.NewInt(0),             //param.QsrFundsNeeded
		),
	}).Error(t, nil)
	z.InsertNewMomentum() // cemented send block
	z.InsertNewMomentum() // cemented token-receive-block
	projectList, err = acceleratorAPI.GetAll(0, 10)
	common.FailIfErr(t, err)

	defer z.CallContract(&nom.AccountBlock{
		Address:   g.Pillar1.Address,
		ToAddress: types.AcceleratorContract,
		Data: definition.ABIAccelerator.PackMethodPanic(definition.VoteByProdAddressMethodName,
			projectList.List[0].PhaseIds[0],
			definition.VoteYes,
		),
	}).Error(t, nil)
	z.InsertNewMomentum() // cemented send block
	z.InsertNewMomentum() // cemented token-receive-block

	defer z.CallContract(&nom.AccountBlock{
		Address:   g.Pillar2.Address,
		ToAddress: types.AcceleratorContract,
		Data: definition.ABIAccelerator.PackMethodPanic(definition.VoteByProdAddressMethodName,
			projectList.List[0].PhaseIds[0],
			definition.VoteYes,
		),
	}).Error(t, nil)
	z.InsertNewMomentum() // cemented send block
	z.InsertNewMomentum() // cemented token-receive-block

	z.InsertMomentumsTo(60*6*2 + 2)

	z.ExpectBalance(types.AcceleratorContract, types.ZnnTokenStandard, 99999950)
	common.Json(acceleratorAPI.GetAll(0, 10)).Equals(t, `
{
	"count": 1,
	"list": [
		{
			"id": "3821d3a7f16d0155b476bdfbc8ccb849651d5b9c2a2bef3767675f0cb297ff4f",
			"owner": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
			"name": "Test Project 1",
			"description": "TEST DESCRIPTION",
			"url": "test.com",
			"znnFundsNeeded": "100",
			"qsrFundsNeeded": "0",
			"creationTimestamp": 1000000200,
			"lastUpdateTimestamp": 1000007210,
			"status": 1,
			"phaseIds": [
				"3364612a108e4490ab1c789583e0cbd451597e889a441bdcf4fcc8a6e95c705f"
			],
			"votes": {
				"id": "3821d3a7f16d0155b476bdfbc8ccb849651d5b9c2a2bef3767675f0cb297ff4f",
				"total": 2,
				"yes": 2,
				"no": 0
			},
			"phases": [
				{
					"phase": {
						"id": "3364612a108e4490ab1c789583e0cbd451597e889a441bdcf4fcc8a6e95c705f",
						"projectID": "3821d3a7f16d0155b476bdfbc8ccb849651d5b9c2a2bef3767675f0cb297ff4f",
						"name": "Phase 1",
						"description": "Description for phase 1",
						"url": "www.phase1.com",
						"znnFundsNeeded": "50",
						"qsrFundsNeeded": "0",
						"creationTimestamp": 1000003620,
						"acceptedTimestamp": 1000007210,
						"status": 2
					},
					"votes": {
						"id": "3364612a108e4490ab1c789583e0cbd451597e889a441bdcf4fcc8a6e95c705f",
						"total": 2,
						"yes": 2,
						"no": 0
					}
				}
			]
		}
	]
}`)

	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.AcceleratorContract,
		Data: definition.ABIAccelerator.PackMethodPanic(definition.AddPhaseMethodName,
			projectList.List[0].Id,    //param.Hash
			"Phase 2",                 //param.Name
			"Description for phase 2", //param.Description
			"www.phase1.com",          //param.Url
			big.NewInt(50),            //param.ZnnFundsNeeded
			big.NewInt(0),             //param.QsrFundsNeeded
		),
	}).Error(t, nil)
	z.InsertNewMomentum() // cemented send block
	z.InsertNewMomentum() // cemented token-receive-block
	projectList, err = acceleratorAPI.GetAll(0, 10)
	common.FailIfErr(t, err)

	defer z.CallContract(&nom.AccountBlock{
		Address:   g.Pillar1.Address,
		ToAddress: types.AcceleratorContract,
		Data: definition.ABIAccelerator.PackMethodPanic(definition.VoteByProdAddressMethodName,
			projectList.List[0].PhaseIds[1],
			definition.VoteYes,
		),
	}).Error(t, nil)
	z.InsertNewMomentum() // cemented send block
	z.InsertNewMomentum() // cemented token-receive-block

	defer z.CallContract(&nom.AccountBlock{
		Address:   g.Pillar2.Address,
		ToAddress: types.AcceleratorContract,
		Data: definition.ABIAccelerator.PackMethodPanic(definition.VoteByProdAddressMethodName,
			projectList.List[0].PhaseIds[1],
			definition.VoteYes,
		),
	}).Error(t, nil)
	z.InsertNewMomentum() // cemented send block
	z.InsertNewMomentum() // cemented token-receive-block

	z.InsertMomentumsTo(60*6*3 + 2*2)
	z.ExpectBalance(types.AcceleratorContract, types.ZnnTokenStandard, 99999900)
	common.Json(acceleratorAPI.GetAll(0, 10)).Equals(t, `
{
	"count": 1,
	"list": [
		{
			"id": "3821d3a7f16d0155b476bdfbc8ccb849651d5b9c2a2bef3767675f0cb297ff4f",
			"owner": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
			"name": "Test Project 1",
			"description": "TEST DESCRIPTION",
			"url": "test.com",
			"znnFundsNeeded": "100",
			"qsrFundsNeeded": "0",
			"creationTimestamp": 1000000200,
			"lastUpdateTimestamp": 1000010820,
			"status": 4,
			"phaseIds": [
				"3364612a108e4490ab1c789583e0cbd451597e889a441bdcf4fcc8a6e95c705f",
				"8bf0d7d086296ea2c90224094e26f41e68a2896f8cb4d010eda01937fc1ef830"
			],
			"votes": {
				"id": "3821d3a7f16d0155b476bdfbc8ccb849651d5b9c2a2bef3767675f0cb297ff4f",
				"total": 2,
				"yes": 2,
				"no": 0
			},
			"phases": [
				{
					"phase": {
						"id": "3364612a108e4490ab1c789583e0cbd451597e889a441bdcf4fcc8a6e95c705f",
						"projectID": "3821d3a7f16d0155b476bdfbc8ccb849651d5b9c2a2bef3767675f0cb297ff4f",
						"name": "Phase 1",
						"description": "Description for phase 1",
						"url": "www.phase1.com",
						"znnFundsNeeded": "50",
						"qsrFundsNeeded": "0",
						"creationTimestamp": 1000003620,
						"acceptedTimestamp": 1000007210,
						"status": 2
					},
					"votes": {
						"id": "3364612a108e4490ab1c789583e0cbd451597e889a441bdcf4fcc8a6e95c705f",
						"total": 2,
						"yes": 2,
						"no": 0
					}
				},
				{
					"phase": {
						"id": "8bf0d7d086296ea2c90224094e26f41e68a2896f8cb4d010eda01937fc1ef830",
						"projectID": "3821d3a7f16d0155b476bdfbc8ccb849651d5b9c2a2bef3767675f0cb297ff4f",
						"name": "Phase 2",
						"description": "Description for phase 2",
						"url": "www.phase1.com",
						"znnFundsNeeded": "50",
						"qsrFundsNeeded": "0",
						"creationTimestamp": 1000007220,
						"acceptedTimestamp": 1000010820,
						"status": 2
					},
					"votes": {
						"id": "8bf0d7d086296ea2c90224094e26f41e68a2896f8cb4d010eda01937fc1ef830",
						"total": 2,
						"yes": 2,
						"no": 0
					}
				}
			]
		}
	]
}`)
}

func TestAccelerator_CreatePhaseWithInvalidAmount(t *testing.T) {
	z := mock.NewMockZenonWithCustomEpochDuration(t, time.Hour)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).Equals(t, `
t=2001-09-09T01:46:50+0000 lvl=dbug msg=created module=embedded contract=spork spork="&{Id:34d8229bd07586c243c6e74122a18d6d2002694c72964a7186111026a9cec6ab Name:spork-accelerator Description:activate spork for accelerator Activated:false EnforcementHeight:0}"
t=2001-09-09T01:47:00+0000 lvl=dbug msg=activated module=embedded contract=spork spork="&{Id:34d8229bd07586c243c6e74122a18d6d2002694c72964a7186111026a9cec6ab Name:spork-accelerator Description:activate spork for accelerator Activated:true EnforcementHeight:9}"
t=2001-09-09T01:50:00+0000 lvl=dbug msg="successfully create project" module=embedded contract=accelerator project="&{Id:c24a5a6166c8948aba23d68aa39e206fc1410138ad218500749b75e2ae92d730 Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz Name:Test Project 1 Description:TEST DESCRIPTION Url:test.com ZnnFundsNeeded:+100 QsrFundsNeeded:+1000 CreationTimestamp:1000000200 LastUpdateTimestamp:1000000200 Status:0 PhaseIds:[]}"
t=2001-09-09T01:50:20+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:c24a5a6166c8948aba23d68aa39e206fc1410138ad218500749b75e2ae92d730 Name:TEST-pillar-1 Vote:0}"
t=2001-09-09T01:50:40+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:c24a5a6166c8948aba23d68aa39e206fc1410138ad218500749b75e2ae92d730 Name:TEST-pillar-cool Vote:0}"
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=0 pillar-name=TEST-pillar-1 reward="&{DelegationReward:+11995123837 BlockReward:+9916666627 TotalReward:+21911790464 ProducedBlockNum:119 ExpectedBlockNum:120 Weight:+2099916666666}" total-weight=2499916666666 self-weight=2099916666666
t=2001-09-09T02:46:40+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=0 pillar-name=TEST-pillar-cool reward="&{DelegationReward:+1152038401 BlockReward:+9999999960 TotalReward:+11152038361 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2499916666666 self-weight=200000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=0 pillar-name=TEST-pillar-znn reward="&{DelegationReward:+1152038401 BlockReward:+9999999960 TotalReward:+11152038361 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2499916666666 self-weight=200000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=pillar epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxsentynelxxxxxxxxxxxxxwy0r2r current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating sentinel reward" module=embedded contract=sentinel epoch=0 total-znn-reward=187200000000 total-qsr-reward=500000000000 cumulated-sentinel=0 start-time=1000000000 end-time=1000003600
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=sentinel epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating stake reward" module=embedded contract=stake epoch=0 total-reward=1000000000000 cumulated-stake=0 start-time=1000000000 end-time=1000003600
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=stake epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating liquidity reward" module=embedded contract=liquidity epoch=0 znn-amount=187200000000 qsr-amount=500000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=liquidity epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxaccelerat0rxxxxxxxxxxp4tk22 current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="check accelerator votes" module=embedded contract=accelerator votes="&{Id:c24a5a6166c8948aba23d68aa39e206fc1410138ad218500749b75e2ae92d730 Total:2 Yes:2 No:0}" status=true
t=2001-09-09T02:46:40+0000 lvl=dbug msg="project passed voting period" module=embedded contract=accelerator project-id=c24a5a6166c8948aba23d68aa39e206fc1410138ad218500749b75e2ae92d730 passed-votes=true
t=2001-09-09T02:46:50+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg TokenName:Zenon Coin TokenSymbol:ZNN TokenDomain:zenon.network TotalSupply:+19687200000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1znnxxxxxxxxxxxxx9z4ulx}" minted-amount=187200000000 to-address=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae
t=2001-09-09T02:46:50+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 TokenName:QuasarCoin TokenSymbol:QSR TokenDomain:zenon.network TotalSupply:+181050000000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1qsrxxxxxxxxxxxxxmrhjll}" minted-amount=500000000000 to-address=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae
t=2001-09-09T02:47:00+0000 lvl=info msg="received donation" module=embedded contract=common embedded=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae from-address=z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0 zts=zts1znnxxxxxxxxxxxxx9z4ulx amount=187200000000
t=2001-09-09T02:47:00+0000 lvl=info msg="received donation" module=embedded contract=common embedded=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae from-address=z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0 zts=zts1qsrxxxxxxxxxxxxxmrhjll amount=500000000000
`)

	activateAccelerator(z)
	acceleratorAPI := embedded.NewAcceleratorApi(z)
	defer z.CallContract(&nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     types.AcceleratorContract,
		TokenStandard: types.ZnnTokenStandard,
		Amount:        constants.ProjectCreationAmount,
		Data: definition.ABIAccelerator.PackMethodPanic(definition.CreateProjectMethodName,
			"Test Project 1",   //param.Name
			"TEST DESCRIPTION", //param.Description
			"test.com",         //param.Url
			big.NewInt(100),    //param.ZnnFundsNeeded
			big.NewInt(1000),   //param.QsrFundsNeeded
		),
	}).Error(t, nil)
	z.InsertNewMomentum() // cemented send block
	z.InsertNewMomentum() // cemented token-receive-block

	projectList, err := acceleratorAPI.GetAll(0, 10)
	common.FailIfErr(t, err)

	defer z.CallContract(&nom.AccountBlock{
		Address:   g.Pillar1.Address,
		ToAddress: types.AcceleratorContract,
		Data: definition.ABIAccelerator.PackMethodPanic(definition.VoteByProdAddressMethodName,
			projectList.List[0].Id,
			definition.VoteYes,
		),
	}).Error(t, nil)
	z.InsertNewMomentum() // cemented send block
	z.InsertNewMomentum() // cemented token-receive-block

	defer z.CallContract(&nom.AccountBlock{
		Address:   g.Pillar2.Address,
		ToAddress: types.AcceleratorContract,
		Data: definition.ABIAccelerator.PackMethodPanic(definition.VoteByProdAddressMethodName,
			projectList.List[0].Id,
			definition.VoteYes,
		),
	}).Error(t, nil)
	z.InsertNewMomentum() // cemented send block
	z.InsertNewMomentum() // cemented token-receive-block

	z.InsertMomentumsTo(60*6 + 2)

	defer z.InsertSendBlock(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.AcceleratorContract,
		Data: definition.ABIAccelerator.PackMethodPanic(definition.AddPhaseMethodName,
			projectList.List[0].Id,               //param.Hash
			"Phase 1",                            //param.Name
			"Description for phase 1",            //param.Description
			"www.phase1.com",                     //param.Url
			big.NewInt(5001*constants.Decimals),  //param.ZnnFundsNeeded
			big.NewInt(50001*constants.Decimals), //param.QsrFundsNeeded
		),
	}, constants.ErrAcceleratorInvalidFunds, mock.NoVmChanges)
	z.InsertNewMomentum() // cemented send block
	z.InsertNewMomentum() // cemented token-receive-block

	common.Json(acceleratorAPI.GetAll(0, 10)).Equals(t, `
{
	"count": 1,
	"list": [
		{
			"id": "c24a5a6166c8948aba23d68aa39e206fc1410138ad218500749b75e2ae92d730",
			"owner": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
			"name": "Test Project 1",
			"description": "TEST DESCRIPTION",
			"url": "test.com",
			"znnFundsNeeded": "100",
			"qsrFundsNeeded": "1000",
			"creationTimestamp": 1000000200,
			"lastUpdateTimestamp": 1000003600,
			"status": 1,
			"phaseIds": [],
			"votes": {
				"id": "c24a5a6166c8948aba23d68aa39e206fc1410138ad218500749b75e2ae92d730",
				"total": 2,
				"yes": 2,
				"no": 0
			},
			"phases": []
		}
	]
}`)
}

func TestAccelerator_InvalidVote(t *testing.T) {
	z := mock.NewMockZenonWithCustomEpochDuration(t, time.Hour)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).Equals(t, `
t=2001-09-09T01:46:50+0000 lvl=dbug msg=created module=embedded contract=spork spork="&{Id:34d8229bd07586c243c6e74122a18d6d2002694c72964a7186111026a9cec6ab Name:spork-accelerator Description:activate spork for accelerator Activated:false EnforcementHeight:0}"
t=2001-09-09T01:47:00+0000 lvl=dbug msg=activated module=embedded contract=spork spork="&{Id:34d8229bd07586c243c6e74122a18d6d2002694c72964a7186111026a9cec6ab Name:spork-accelerator Description:activate spork for accelerator Activated:true EnforcementHeight:9}"
t=2001-09-09T01:50:00+0000 lvl=dbug msg="successfully create project" module=embedded contract=accelerator project="&{Id:c24a5a6166c8948aba23d68aa39e206fc1410138ad218500749b75e2ae92d730 Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz Name:Test Project 1 Description:TEST DESCRIPTION Url:test.com ZnnFundsNeeded:+100 QsrFundsNeeded:+1000 CreationTimestamp:1000000200 LastUpdateTimestamp:1000000200 Status:0 PhaseIds:[]}"
t=2001-09-09T01:50:20+0000 lvl=dbug msg="unable to find pillar" module=embedded contract=common param="&{Id:c24a5a6166c8948aba23d68aa39e206fc1410138ad218500749b75e2ae92d730 Name:Pillar111 Vote:0}" send-block-address=z1qqq43dyrswfehx9w9td43exflqzcxrt7g6alah
t=2001-09-09T01:50:40+0000 lvl=dbug msg="unable to find pillar" module=embedded contract=common param="&{Id:c24a5a6166c8948aba23d68aa39e206fc1410138ad218500749b75e2ae92d730 Name:TEST-pillar-1 Vote:0}" send-block-address=z1qz8v73ea2vy2rrlq7skssngu8cm8mknjjkr2ju
t=2001-09-09T01:51:00+0000 lvl=dbug msg="unable to find pillar" module=embedded contract=common param="&{Id:c24a5a6166c8948aba23d68aa39e206fc1410138ad218500749b75e2ae92d730 Name: Vote:0}" send-block-address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz
`)

	activateAccelerator(z)
	acceleratorAPI := embedded.NewAcceleratorApi(z)
	defer z.CallContract(&nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     types.AcceleratorContract,
		TokenStandard: types.ZnnTokenStandard,
		Amount:        constants.ProjectCreationAmount,
		Data: definition.ABIAccelerator.PackMethodPanic(definition.CreateProjectMethodName,
			"Test Project 1",   //param.Name
			"TEST DESCRIPTION", //param.Description
			"test.com",         //param.Url
			big.NewInt(100),    //param.ZnnFundsNeeded
			big.NewInt(1000),   //param.QsrFundsNeeded
		),
	}).Error(t, nil)
	z.InsertNewMomentum() // cemented send block
	z.InsertNewMomentum() // cemented token-receive-block

	projectList, err := acceleratorAPI.GetAll(0, 10)
	common.FailIfErr(t, err)

	defer z.CallContract(&nom.AccountBlock{
		Address:   g.Pillar1.Address,
		ToAddress: types.AcceleratorContract,
		Data: definition.ABIAccelerator.PackMethodPanic(definition.VoteByNameMethodName,
			projectList.List[0].Id,
			"Pillar111",
			definition.VoteYes,
		),
	}).Error(t, constants.ErrForbiddenParam)
	z.InsertNewMomentum() // cemented send block
	z.InsertNewMomentum() // cemented token-receive-block

	defer z.CallContract(&nom.AccountBlock{
		Address:   g.Pillar2.Address,
		ToAddress: types.AcceleratorContract,
		Data: definition.ABIAccelerator.PackMethodPanic(definition.VoteByNameMethodName,
			projectList.List[0].Id,
			g.Pillar1Name,
			definition.VoteYes,
		),
	}).Error(t, constants.ErrForbiddenParam)
	z.InsertNewMomentum() // cemented send block
	z.InsertNewMomentum() // cemented token-receive-block

	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.AcceleratorContract,
		Data: definition.ABIAccelerator.PackMethodPanic(definition.VoteByProdAddressMethodName,
			projectList.List[0].Id,
			definition.VoteYes,
		),
	}).Error(t, constants.ErrForbiddenParam)
	z.InsertNewMomentum() // cemented send block
	z.InsertNewMomentum() // cemented token-receive-block

	common.Json(acceleratorAPI.GetAll(0, 10)).Equals(t, `
{
	"count": 1,
	"list": [
		{
			"id": "c24a5a6166c8948aba23d68aa39e206fc1410138ad218500749b75e2ae92d730",
			"owner": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
			"name": "Test Project 1",
			"description": "TEST DESCRIPTION",
			"url": "test.com",
			"znnFundsNeeded": "100",
			"qsrFundsNeeded": "1000",
			"creationTimestamp": 1000000200,
			"lastUpdateTimestamp": 1000000200,
			"status": 0,
			"phaseIds": [],
			"votes": {
				"id": "c24a5a6166c8948aba23d68aa39e206fc1410138ad218500749b75e2ae92d730",
				"total": 0,
				"yes": 0,
				"no": 0
			},
			"phases": []
		}
	]
}`)
}

func TestAccelerator_MultipleVoteByName(t *testing.T) {
	z := mock.NewMockZenonWithCustomEpochDuration(t, time.Hour)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).Equals(t, `
t=2001-09-09T01:46:50+0000 lvl=dbug msg=created module=embedded contract=spork spork="&{Id:34d8229bd07586c243c6e74122a18d6d2002694c72964a7186111026a9cec6ab Name:spork-accelerator Description:activate spork for accelerator Activated:false EnforcementHeight:0}"
t=2001-09-09T01:47:00+0000 lvl=dbug msg=activated module=embedded contract=spork spork="&{Id:34d8229bd07586c243c6e74122a18d6d2002694c72964a7186111026a9cec6ab Name:spork-accelerator Description:activate spork for accelerator Activated:true EnforcementHeight:9}"
t=2001-09-09T01:50:00+0000 lvl=dbug msg="successfully create project" module=embedded contract=accelerator project="&{Id:c24a5a6166c8948aba23d68aa39e206fc1410138ad218500749b75e2ae92d730 Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz Name:Test Project 1 Description:TEST DESCRIPTION Url:test.com ZnnFundsNeeded:+100 QsrFundsNeeded:+1000 CreationTimestamp:1000000200 LastUpdateTimestamp:1000000200 Status:0 PhaseIds:[]}"
t=2001-09-09T01:50:20+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:c24a5a6166c8948aba23d68aa39e206fc1410138ad218500749b75e2ae92d730 Name:TEST-pillar-1 Vote:0}"
t=2001-09-09T01:50:40+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:c24a5a6166c8948aba23d68aa39e206fc1410138ad218500749b75e2ae92d730 Name:TEST-pillar-1 Vote:1}"
t=2001-09-09T01:51:00+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:c24a5a6166c8948aba23d68aa39e206fc1410138ad218500749b75e2ae92d730 Name:TEST-pillar-cool Vote:0}"
t=2001-09-09T01:51:20+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:c24a5a6166c8948aba23d68aa39e206fc1410138ad218500749b75e2ae92d730 Name:TEST-pillar-cool Vote:1}"
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=0 pillar-name=TEST-pillar-1 reward="&{DelegationReward:+11995123837 BlockReward:+9916666627 TotalReward:+21911790464 ProducedBlockNum:119 ExpectedBlockNum:120 Weight:+2099916666666}" total-weight=2499916666666 self-weight=2099916666666
t=2001-09-09T02:46:40+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=0 pillar-name=TEST-pillar-cool reward="&{DelegationReward:+1152038401 BlockReward:+9999999960 TotalReward:+11152038361 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2499916666666 self-weight=200000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=0 pillar-name=TEST-pillar-znn reward="&{DelegationReward:+1152038401 BlockReward:+9999999960 TotalReward:+11152038361 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2499916666666 self-weight=200000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=pillar epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxsentynelxxxxxxxxxxxxxwy0r2r current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating sentinel reward" module=embedded contract=sentinel epoch=0 total-znn-reward=187200000000 total-qsr-reward=500000000000 cumulated-sentinel=0 start-time=1000000000 end-time=1000003600
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=sentinel epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating stake reward" module=embedded contract=stake epoch=0 total-reward=1000000000000 cumulated-stake=0 start-time=1000000000 end-time=1000003600
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=stake epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating liquidity reward" module=embedded contract=liquidity epoch=0 znn-amount=187200000000 qsr-amount=500000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=liquidity epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxaccelerat0rxxxxxxxxxxp4tk22 current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="check accelerator votes" module=embedded contract=accelerator votes="&{Id:c24a5a6166c8948aba23d68aa39e206fc1410138ad218500749b75e2ae92d730 Total:2 Yes:0 No:2}" status=false
t=2001-09-09T02:46:40+0000 lvl=dbug msg="project passed voting period" module=embedded contract=accelerator project-id=c24a5a6166c8948aba23d68aa39e206fc1410138ad218500749b75e2ae92d730 passed-votes=false
t=2001-09-09T02:46:50+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg TokenName:Zenon Coin TokenSymbol:ZNN TokenDomain:zenon.network TotalSupply:+19687200000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1znnxxxxxxxxxxxxx9z4ulx}" minted-amount=187200000000 to-address=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae
t=2001-09-09T02:46:50+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 TokenName:QuasarCoin TokenSymbol:QSR TokenDomain:zenon.network TotalSupply:+181050000000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1qsrxxxxxxxxxxxxxmrhjll}" minted-amount=500000000000 to-address=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae
`)

	activateAccelerator(z)
	acceleratorAPI := embedded.NewAcceleratorApi(z)
	defer z.CallContract(&nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     types.AcceleratorContract,
		TokenStandard: types.ZnnTokenStandard,
		Amount:        constants.ProjectCreationAmount,
		Data: definition.ABIAccelerator.PackMethodPanic(definition.CreateProjectMethodName,
			"Test Project 1",   //param.Name
			"TEST DESCRIPTION", //param.Description
			"test.com",         //param.Url
			big.NewInt(100),    //param.ZnnFundsNeeded
			big.NewInt(1000),   //param.QsrFundsNeeded
		),
	}).Error(t, nil)
	z.InsertNewMomentum() // cemented send block
	z.InsertNewMomentum() // cemented token-receive-block

	projectList, err := acceleratorAPI.GetAll(0, 10)
	common.FailIfErr(t, err)

	defer z.CallContract(&nom.AccountBlock{
		Address:   g.Pillar1.Address,
		ToAddress: types.AcceleratorContract,
		Data: definition.ABIAccelerator.PackMethodPanic(definition.VoteByNameMethodName,
			projectList.List[0].Id,
			g.Pillar1Name,
			definition.VoteYes,
		),
	}).Error(t, nil)
	z.InsertNewMomentum() // cemented send block
	z.InsertNewMomentum() // cemented token-receive-block

	defer z.CallContract(&nom.AccountBlock{
		Address:   g.Pillar1.Address,
		ToAddress: types.AcceleratorContract,
		Data: definition.ABIAccelerator.PackMethodPanic(definition.VoteByNameMethodName,
			projectList.List[0].Id,
			g.Pillar1Name,
			definition.VoteNo,
		),
	}).Error(t, nil)
	z.InsertNewMomentum() // cemented send block
	z.InsertNewMomentum() // cemented token-receive-block

	defer z.CallContract(&nom.AccountBlock{
		Address:   g.Pillar2.Address,
		ToAddress: types.AcceleratorContract,
		Data: definition.ABIAccelerator.PackMethodPanic(definition.VoteByNameMethodName,
			projectList.List[0].Id,
			g.Pillar2Name,
			definition.VoteYes,
		),
	}).Error(t, nil)
	z.InsertNewMomentum() // cemented send block
	z.InsertNewMomentum() // cemented token-receive-block

	defer z.CallContract(&nom.AccountBlock{
		Address:   g.Pillar2.Address,
		ToAddress: types.AcceleratorContract,
		Data: definition.ABIAccelerator.PackMethodPanic(definition.VoteByNameMethodName,
			projectList.List[0].Id,
			g.Pillar2Name,
			definition.VoteNo,
		),
	}).Error(t, nil)
	z.InsertNewMomentum() // cemented send block
	z.InsertNewMomentum() // cemented token-receive-block

	z.InsertMomentumsTo(60*6 + 2)

	common.Json(acceleratorAPI.GetAll(0, 10)).Equals(t, `
{
	"count": 1,
	"list": [
		{
			"id": "c24a5a6166c8948aba23d68aa39e206fc1410138ad218500749b75e2ae92d730",
			"owner": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
			"name": "Test Project 1",
			"description": "TEST DESCRIPTION",
			"url": "test.com",
			"znnFundsNeeded": "100",
			"qsrFundsNeeded": "1000",
			"creationTimestamp": 1000000200,
			"lastUpdateTimestamp": 1000000200,
			"status": 0,
			"phaseIds": [],
			"votes": {
				"id": "c24a5a6166c8948aba23d68aa39e206fc1410138ad218500749b75e2ae92d730",
				"total": 2,
				"yes": 0,
				"no": 2
			},
			"phases": []
		}
	]
}`)
}

func TestAccelerator_CreateMultiplePhase(t *testing.T) {
	z := mock.NewMockZenonWithCustomEpochDuration(t, time.Hour)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).Equals(t, `
t=2001-09-09T01:46:50+0000 lvl=dbug msg=created module=embedded contract=spork spork="&{Id:34d8229bd07586c243c6e74122a18d6d2002694c72964a7186111026a9cec6ab Name:spork-accelerator Description:activate spork for accelerator Activated:false EnforcementHeight:0}"
t=2001-09-09T01:47:00+0000 lvl=dbug msg=activated module=embedded contract=spork spork="&{Id:34d8229bd07586c243c6e74122a18d6d2002694c72964a7186111026a9cec6ab Name:spork-accelerator Description:activate spork for accelerator Activated:true EnforcementHeight:9}"
t=2001-09-09T01:50:00+0000 lvl=dbug msg="successfully create project" module=embedded contract=accelerator project="&{Id:c24a5a6166c8948aba23d68aa39e206fc1410138ad218500749b75e2ae92d730 Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz Name:Test Project 1 Description:TEST DESCRIPTION Url:test.com ZnnFundsNeeded:+100 QsrFundsNeeded:+1000 CreationTimestamp:1000000200 LastUpdateTimestamp:1000000200 Status:0 PhaseIds:[]}"
t=2001-09-09T01:50:20+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:c24a5a6166c8948aba23d68aa39e206fc1410138ad218500749b75e2ae92d730 Name:TEST-pillar-1 Vote:0}"
t=2001-09-09T01:50:40+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:c24a5a6166c8948aba23d68aa39e206fc1410138ad218500749b75e2ae92d730 Name:TEST-pillar-cool Vote:0}"
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=0 pillar-name=TEST-pillar-1 reward="&{DelegationReward:+11995123837 BlockReward:+9916666627 TotalReward:+21911790464 ProducedBlockNum:119 ExpectedBlockNum:120 Weight:+2099916666666}" total-weight=2499916666666 self-weight=2099916666666
t=2001-09-09T02:46:40+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=0 pillar-name=TEST-pillar-cool reward="&{DelegationReward:+1152038401 BlockReward:+9999999960 TotalReward:+11152038361 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2499916666666 self-weight=200000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=0 pillar-name=TEST-pillar-znn reward="&{DelegationReward:+1152038401 BlockReward:+9999999960 TotalReward:+11152038361 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2499916666666 self-weight=200000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=pillar epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxsentynelxxxxxxxxxxxxxwy0r2r current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating sentinel reward" module=embedded contract=sentinel epoch=0 total-znn-reward=187200000000 total-qsr-reward=500000000000 cumulated-sentinel=0 start-time=1000000000 end-time=1000003600
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=sentinel epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating stake reward" module=embedded contract=stake epoch=0 total-reward=1000000000000 cumulated-stake=0 start-time=1000000000 end-time=1000003600
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=stake epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating liquidity reward" module=embedded contract=liquidity epoch=0 znn-amount=187200000000 qsr-amount=500000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=liquidity epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxaccelerat0rxxxxxxxxxxp4tk22 current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="check accelerator votes" module=embedded contract=accelerator votes="&{Id:c24a5a6166c8948aba23d68aa39e206fc1410138ad218500749b75e2ae92d730 Total:2 Yes:2 No:0}" status=true
t=2001-09-09T02:46:40+0000 lvl=dbug msg="project passed voting period" module=embedded contract=accelerator project-id=c24a5a6166c8948aba23d68aa39e206fc1410138ad218500749b75e2ae92d730 passed-votes=true
t=2001-09-09T02:46:50+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg TokenName:Zenon Coin TokenSymbol:ZNN TokenDomain:zenon.network TotalSupply:+19687200000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1znnxxxxxxxxxxxxx9z4ulx}" minted-amount=187200000000 to-address=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae
t=2001-09-09T02:46:50+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 TokenName:QuasarCoin TokenSymbol:QSR TokenDomain:zenon.network TotalSupply:+181050000000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1qsrxxxxxxxxxxxxxmrhjll}" minted-amount=500000000000 to-address=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae
t=2001-09-09T02:47:00+0000 lvl=info msg="received donation" module=embedded contract=common embedded=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae from-address=z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0 zts=zts1znnxxxxxxxxxxxxx9z4ulx amount=187200000000
t=2001-09-09T02:47:00+0000 lvl=dbug msg="successfully created phase" module=embedded contract=accelerator phase="&{Id:e2b4eb834a20e51f88dba0232c856949b1462ef0f1fe2cb5049bb84063a781ab ProjectId:c24a5a6166c8948aba23d68aa39e206fc1410138ad218500749b75e2ae92d730 Name:Phase 1 Description:Description for phase 1 Url:www.phase1.com ZnnFundsNeeded:+10 QsrFundsNeeded:+10 CreationTimestamp:1000003620 AcceptedTimestamp:0 Status:0}"
t=2001-09-09T02:47:00+0000 lvl=info msg="received donation" module=embedded contract=common embedded=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae from-address=z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0 zts=zts1qsrxxxxxxxxxxxxxmrhjll amount=500000000000
`)

	activateAccelerator(z)
	acceleratorAPI := embedded.NewAcceleratorApi(z)
	defer z.CallContract(&nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     types.AcceleratorContract,
		TokenStandard: types.ZnnTokenStandard,
		Amount:        constants.ProjectCreationAmount,
		Data: definition.ABIAccelerator.PackMethodPanic(definition.CreateProjectMethodName,
			"Test Project 1",   //param.Name
			"TEST DESCRIPTION", //param.Description
			"test.com",         //param.Url
			big.NewInt(100),    //param.ZnnFundsNeeded
			big.NewInt(1000),   //param.QsrFundsNeeded
		),
	}).Error(t, nil)
	z.InsertNewMomentum() // cemented send block
	z.InsertNewMomentum() // cemented token-receive-block

	projectList, err := acceleratorAPI.GetAll(0, 10)
	common.FailIfErr(t, err)

	defer z.CallContract(&nom.AccountBlock{
		Address:   g.Pillar1.Address,
		ToAddress: types.AcceleratorContract,
		Data: definition.ABIAccelerator.PackMethodPanic(definition.VoteByProdAddressMethodName,
			projectList.List[0].Id,
			definition.VoteYes,
		),
	}).Error(t, nil)
	z.InsertNewMomentum() // cemented send block
	z.InsertNewMomentum() // cemented token-receive-block

	defer z.CallContract(&nom.AccountBlock{
		Address:   g.Pillar2.Address,
		ToAddress: types.AcceleratorContract,
		Data: definition.ABIAccelerator.PackMethodPanic(definition.VoteByProdAddressMethodName,
			projectList.List[0].Id,
			definition.VoteYes,
		),
	}).Error(t, nil)
	z.InsertNewMomentum() // cemented send block
	z.InsertNewMomentum() // cemented token-receive-block

	z.InsertMomentumsTo(60*6 + 2)

	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.AcceleratorContract,
		Data: definition.ABIAccelerator.PackMethodPanic(definition.AddPhaseMethodName,
			projectList.List[0].Id,    //param.Hash
			"Phase 1",                 //param.Name
			"Description for phase 1", //param.Description
			"www.phase1.com",          //param.Url
			big.NewInt(10),            //param.ZnnFundsNeeded
			big.NewInt(10),            //param.QsrFundsNeeded
		),
	}).Error(t, nil)
	z.InsertNewMomentum() // cemented send block
	z.InsertNewMomentum() // cemented token-receive-block

	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.AcceleratorContract,
		Data: definition.ABIAccelerator.PackMethodPanic(definition.AddPhaseMethodName,
			projectList.List[0].Id,    //param.Hash
			"Phase 2",                 //param.Name
			"Description for phase 2", //param.Description
			"www.phase2.com",          //param.Url
			big.NewInt(20),            //param.ZnnFundsNeeded
			big.NewInt(20),            //param.QsrFundsNeeded
		),
	}).Error(t, constants.ErrPermissionDenied)
	z.InsertNewMomentum() // cemented send block
	z.InsertNewMomentum() // cemented token-receive-block

	common.Json(acceleratorAPI.GetAll(0, 10)).Equals(t, `
{
	"count": 1,
	"list": [
		{
			"id": "c24a5a6166c8948aba23d68aa39e206fc1410138ad218500749b75e2ae92d730",
			"owner": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
			"name": "Test Project 1",
			"description": "TEST DESCRIPTION",
			"url": "test.com",
			"znnFundsNeeded": "100",
			"qsrFundsNeeded": "1000",
			"creationTimestamp": 1000000200,
			"lastUpdateTimestamp": 1000003620,
			"status": 1,
			"phaseIds": [
				"e2b4eb834a20e51f88dba0232c856949b1462ef0f1fe2cb5049bb84063a781ab"
			],
			"votes": {
				"id": "c24a5a6166c8948aba23d68aa39e206fc1410138ad218500749b75e2ae92d730",
				"total": 2,
				"yes": 2,
				"no": 0
			},
			"phases": [
				{
					"phase": {
						"id": "e2b4eb834a20e51f88dba0232c856949b1462ef0f1fe2cb5049bb84063a781ab",
						"projectID": "c24a5a6166c8948aba23d68aa39e206fc1410138ad218500749b75e2ae92d730",
						"name": "Phase 1",
						"description": "Description for phase 1",
						"url": "www.phase1.com",
						"znnFundsNeeded": "10",
						"qsrFundsNeeded": "10",
						"creationTimestamp": 1000003620,
						"acceptedTimestamp": 0,
						"status": 0
					},
					"votes": {
						"id": "e2b4eb834a20e51f88dba0232c856949b1462ef0f1fe2cb5049bb84063a781ab",
						"total": 0,
						"yes": 0,
						"no": 0
					}
				}
			]
		}
	]
}`)
}

func TestAccelerator_AcceptPhase(t *testing.T) {
	z := mock.NewMockZenonWithCustomEpochDuration(t, time.Hour)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).Equals(t, `
t=2001-09-09T01:46:50+0000 lvl=dbug msg=created module=embedded contract=spork spork="&{Id:34d8229bd07586c243c6e74122a18d6d2002694c72964a7186111026a9cec6ab Name:spork-accelerator Description:activate spork for accelerator Activated:false EnforcementHeight:0}"
t=2001-09-09T01:47:00+0000 lvl=dbug msg=activated module=embedded contract=spork spork="&{Id:34d8229bd07586c243c6e74122a18d6d2002694c72964a7186111026a9cec6ab Name:spork-accelerator Description:activate spork for accelerator Activated:true EnforcementHeight:9}"
t=2001-09-09T01:50:00+0000 lvl=dbug msg="successfully create project" module=embedded contract=accelerator project="&{Id:c24a5a6166c8948aba23d68aa39e206fc1410138ad218500749b75e2ae92d730 Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz Name:Test Project 1 Description:TEST DESCRIPTION Url:test.com ZnnFundsNeeded:+100 QsrFundsNeeded:+1000 CreationTimestamp:1000000200 LastUpdateTimestamp:1000000200 Status:0 PhaseIds:[]}"
t=2001-09-09T01:50:20+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:c24a5a6166c8948aba23d68aa39e206fc1410138ad218500749b75e2ae92d730 Name:TEST-pillar-1 Vote:0}"
t=2001-09-09T01:50:40+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:c24a5a6166c8948aba23d68aa39e206fc1410138ad218500749b75e2ae92d730 Name:TEST-pillar-cool Vote:0}"
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=0 pillar-name=TEST-pillar-1 reward="&{DelegationReward:+11995123837 BlockReward:+9916666627 TotalReward:+21911790464 ProducedBlockNum:119 ExpectedBlockNum:120 Weight:+2099916666666}" total-weight=2499916666666 self-weight=2099916666666
t=2001-09-09T02:46:40+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=0 pillar-name=TEST-pillar-cool reward="&{DelegationReward:+1152038401 BlockReward:+9999999960 TotalReward:+11152038361 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2499916666666 self-weight=200000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=0 pillar-name=TEST-pillar-znn reward="&{DelegationReward:+1152038401 BlockReward:+9999999960 TotalReward:+11152038361 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2499916666666 self-weight=200000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=pillar epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxsentynelxxxxxxxxxxxxxwy0r2r current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating sentinel reward" module=embedded contract=sentinel epoch=0 total-znn-reward=187200000000 total-qsr-reward=500000000000 cumulated-sentinel=0 start-time=1000000000 end-time=1000003600
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=sentinel epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating stake reward" module=embedded contract=stake epoch=0 total-reward=1000000000000 cumulated-stake=0 start-time=1000000000 end-time=1000003600
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=stake epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating liquidity reward" module=embedded contract=liquidity epoch=0 znn-amount=187200000000 qsr-amount=500000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=liquidity epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxaccelerat0rxxxxxxxxxxp4tk22 current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="check accelerator votes" module=embedded contract=accelerator votes="&{Id:c24a5a6166c8948aba23d68aa39e206fc1410138ad218500749b75e2ae92d730 Total:2 Yes:2 No:0}" status=true
t=2001-09-09T02:46:40+0000 lvl=dbug msg="project passed voting period" module=embedded contract=accelerator project-id=c24a5a6166c8948aba23d68aa39e206fc1410138ad218500749b75e2ae92d730 passed-votes=true
t=2001-09-09T02:46:50+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg TokenName:Zenon Coin TokenSymbol:ZNN TokenDomain:zenon.network TotalSupply:+19687200000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1znnxxxxxxxxxxxxx9z4ulx}" minted-amount=187200000000 to-address=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae
t=2001-09-09T02:46:50+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 TokenName:QuasarCoin TokenSymbol:QSR TokenDomain:zenon.network TotalSupply:+181050000000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1qsrxxxxxxxxxxxxxmrhjll}" minted-amount=500000000000 to-address=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae
t=2001-09-09T02:47:00+0000 lvl=info msg="received donation" module=embedded contract=common embedded=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae from-address=z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0 zts=zts1znnxxxxxxxxxxxxx9z4ulx amount=187200000000
t=2001-09-09T02:47:00+0000 lvl=dbug msg="successfully created phase" module=embedded contract=accelerator phase="&{Id:05f123c4e83b1cf5559638e2acfb1c2eb8575797b34b2d4b8d95490a7251f4b5 ProjectId:c24a5a6166c8948aba23d68aa39e206fc1410138ad218500749b75e2ae92d730 Name:Phase 1 Description:Description for phase 1 Url:www.phase1.com ZnnFundsNeeded:+10 QsrFundsNeeded:+0 CreationTimestamp:1000003620 AcceptedTimestamp:0 Status:0}"
t=2001-09-09T02:47:00+0000 lvl=info msg="received donation" module=embedded contract=common embedded=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae from-address=z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0 zts=zts1qsrxxxxxxxxxxxxxmrhjll amount=500000000000
t=2001-09-09T02:47:20+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:05f123c4e83b1cf5559638e2acfb1c2eb8575797b34b2d4b8d95490a7251f4b5 Name:TEST-pillar-1 Vote:0}"
t=2001-09-09T02:47:40+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:05f123c4e83b1cf5559638e2acfb1c2eb8575797b34b2d4b8d95490a7251f4b5 Name:TEST-pillar-cool Vote:0}"
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg current-height=722 last-update-height=361
t=2001-09-09T03:46:50+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=1 pillar-name=TEST-pillar-1 reward="&{DelegationReward:+12095907836 BlockReward:+9999999960 TotalReward:+22095907796 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+2099900000000}" total-weight=2499900000000 self-weight=2099900000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=1 pillar-name=TEST-pillar-cool reward="&{DelegationReward:+1152046081 BlockReward:+9999999960 TotalReward:+11152046041 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2499900000000 self-weight=200000000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=1 pillar-name=TEST-pillar-znn reward="&{DelegationReward:+1152046081 BlockReward:+9999999960 TotalReward:+11152046041 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2499900000000 self-weight=200000000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=pillar epoch=2
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxsentynelxxxxxxxxxxxxxwy0r2r current-height=722 last-update-height=361
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating sentinel reward" module=embedded contract=sentinel epoch=1 total-znn-reward=187200000000 total-qsr-reward=500000000000 cumulated-sentinel=0 start-time=1000003600 end-time=1000007200
t=2001-09-09T03:46:50+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=sentinel epoch=2
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 current-height=722 last-update-height=361
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating stake reward" module=embedded contract=stake epoch=1 total-reward=1000000000000 cumulated-stake=0 start-time=1000003600 end-time=1000007200
t=2001-09-09T03:46:50+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=stake epoch=2
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae current-height=722 last-update-height=361
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating liquidity reward" module=embedded contract=liquidity epoch=1 znn-amount=187200000000 qsr-amount=500000000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=liquidity epoch=2
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxaccelerat0rxxxxxxxxxxp4tk22 current-height=722 last-update-height=361
t=2001-09-09T03:46:50+0000 lvl=dbug msg="check accelerator votes" module=embedded contract=accelerator votes="&{Id:05f123c4e83b1cf5559638e2acfb1c2eb8575797b34b2d4b8d95490a7251f4b5 Total:2 Yes:2 No:0}" status=true
t=2001-09-09T03:46:50+0000 lvl=dbug msg="finishing and paying phase" module=embedded contract=accelerator project-id=c24a5a6166c8948aba23d68aa39e206fc1410138ad218500749b75e2ae92d730 phase-id=05f123c4e83b1cf5559638e2acfb1c2eb8575797b34b2d4b8d95490a7251f4b5 znn-amount=10 qsr-amount=0
t=2001-09-09T03:47:00+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg TokenName:Zenon Coin TokenSymbol:ZNN TokenDomain:zenon.network TotalSupply:+19874400000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1znnxxxxxxxxxxxxx9z4ulx}" minted-amount=187200000000 to-address=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae
t=2001-09-09T03:47:00+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 TokenName:QuasarCoin TokenSymbol:QSR TokenDomain:zenon.network TotalSupply:+181550000000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1qsrxxxxxxxxxxxxxmrhjll}" minted-amount=500000000000 to-address=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae
t=2001-09-09T03:47:10+0000 lvl=info msg="received donation" module=embedded contract=common embedded=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae from-address=z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0 zts=zts1znnxxxxxxxxxxxxx9z4ulx amount=187200000000
t=2001-09-09T03:47:10+0000 lvl=info msg="received donation" module=embedded contract=common embedded=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae from-address=z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0 zts=zts1qsrxxxxxxxxxxxxxmrhjll amount=500000000000
`)

	activateAccelerator(z)
	acceleratorAPI := embedded.NewAcceleratorApi(z)
	ledgerApi := api.NewLedgerApi(z)

	defer z.CallContract(&nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     types.AcceleratorContract,
		TokenStandard: types.ZnnTokenStandard,
		Amount:        constants.ProjectCreationAmount,
		Data: definition.ABIAccelerator.PackMethodPanic(definition.CreateProjectMethodName,
			"Test Project 1",   //param.Name
			"TEST DESCRIPTION", //param.Description
			"test.com",         //param.Url
			big.NewInt(100),    //param.ZnnFundsNeeded
			big.NewInt(1000),   //param.QsrFundsNeeded
		),
	}).Error(t, nil)
	z.InsertNewMomentum() // cemented send block
	z.InsertNewMomentum() // cemented token-receive-block

	projectList, err := acceleratorAPI.GetAll(0, 10)
	common.FailIfErr(t, err)

	defer z.CallContract(&nom.AccountBlock{
		Address:   g.Pillar1.Address,
		ToAddress: types.AcceleratorContract,
		Data: definition.ABIAccelerator.PackMethodPanic(definition.VoteByProdAddressMethodName,
			projectList.List[0].Id,
			definition.VoteYes,
		),
	}).Error(t, nil)
	z.InsertNewMomentum() // cemented send block
	z.InsertNewMomentum() // cemented token-receive-block

	defer z.CallContract(&nom.AccountBlock{
		Address:   g.Pillar2.Address,
		ToAddress: types.AcceleratorContract,
		Data: definition.ABIAccelerator.PackMethodPanic(definition.VoteByProdAddressMethodName,
			projectList.List[0].Id,
			definition.VoteYes,
		),
	}).Error(t, nil)
	z.InsertNewMomentum() // cemented send block
	z.InsertNewMomentum() // cemented token-receive-block

	z.InsertMomentumsTo(60*6 + 2)

	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.AcceleratorContract,
		Data: definition.ABIAccelerator.PackMethodPanic(definition.AddPhaseMethodName,
			projectList.List[0].Id,    //param.Hash
			"Phase 1",                 //param.Name
			"Description for phase 1", //param.Description
			"www.phase1.com",          //param.Url
			big.NewInt(10),            //param.ZnnFundsNeeded
			big.NewInt(0),             //param.QsrFundsNeeded
		),
	}).Error(t, nil)
	z.InsertNewMomentum() // cemented send block
	z.InsertNewMomentum() // cemented token-receive-block
	projectList, err = acceleratorAPI.GetAll(0, 10)
	common.FailIfErr(t, err)
	common.Json(acceleratorAPI.GetAll(0, 10)).Equals(t, `
{
	"count": 1,
	"list": [
		{
			"id": "c24a5a6166c8948aba23d68aa39e206fc1410138ad218500749b75e2ae92d730",
			"owner": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
			"name": "Test Project 1",
			"description": "TEST DESCRIPTION",
			"url": "test.com",
			"znnFundsNeeded": "100",
			"qsrFundsNeeded": "1000",
			"creationTimestamp": 1000000200,
			"lastUpdateTimestamp": 1000003620,
			"status": 1,
			"phaseIds": [
				"05f123c4e83b1cf5559638e2acfb1c2eb8575797b34b2d4b8d95490a7251f4b5"
			],
			"votes": {
				"id": "c24a5a6166c8948aba23d68aa39e206fc1410138ad218500749b75e2ae92d730",
				"total": 2,
				"yes": 2,
				"no": 0
			},
			"phases": [
				{
					"phase": {
						"id": "05f123c4e83b1cf5559638e2acfb1c2eb8575797b34b2d4b8d95490a7251f4b5",
						"projectID": "c24a5a6166c8948aba23d68aa39e206fc1410138ad218500749b75e2ae92d730",
						"name": "Phase 1",
						"description": "Description for phase 1",
						"url": "www.phase1.com",
						"znnFundsNeeded": "10",
						"qsrFundsNeeded": "0",
						"creationTimestamp": 1000003620,
						"acceptedTimestamp": 0,
						"status": 0
					},
					"votes": {
						"id": "05f123c4e83b1cf5559638e2acfb1c2eb8575797b34b2d4b8d95490a7251f4b5",
						"total": 0,
						"yes": 0,
						"no": 0
					}
				}
			]
		}
	]
}`)

	defer z.CallContract(&nom.AccountBlock{
		Address:   g.Pillar1.Address,
		ToAddress: types.AcceleratorContract,
		Data: definition.ABIAccelerator.PackMethodPanic(definition.VoteByProdAddressMethodName,
			projectList.List[0].PhaseIds[0],
			definition.VoteYes,
		),
	}).Error(t, nil)
	z.InsertNewMomentum() // cemented send block
	z.InsertNewMomentum() // cemented token-receive-block

	defer z.CallContract(&nom.AccountBlock{
		Address:   g.Pillar2.Address,
		ToAddress: types.AcceleratorContract,
		Data: definition.ABIAccelerator.PackMethodPanic(definition.VoteByProdAddressMethodName,
			projectList.List[0].PhaseIds[0],
			definition.VoteYes,
		),
	}).Error(t, nil)
	z.InsertNewMomentum() // cemented send block
	z.InsertNewMomentum() // cemented token-receive-block

	z.InsertMomentumsTo(60*6*2 + 2)

	z.ExpectBalance(types.AcceleratorContract, types.ZnnTokenStandard, 99999990)

	common.Json(acceleratorAPI.GetPillarVotes(g.Pillar1Name, projectList.List[0].PhaseIds)).Equals(t, `
[
	{
		"id": "05f123c4e83b1cf5559638e2acfb1c2eb8575797b34b2d4b8d95490a7251f4b5",
		"name": "TEST-pillar-1",
		"vote": 0
	}
]`)
	common.Json(acceleratorAPI.GetVoteBreakdown(projectList.List[0].Id)).Equals(t, `
{
	"id": "c24a5a6166c8948aba23d68aa39e206fc1410138ad218500749b75e2ae92d730",
	"total": 2,
	"yes": 2,
	"no": 0
}`)

	common.Json(acceleratorAPI.GetPhaseById(projectList.List[0].PhaseIds[0])).Equals(t, `
{
	"phase": {
		"id": "05f123c4e83b1cf5559638e2acfb1c2eb8575797b34b2d4b8d95490a7251f4b5",
		"projectID": "c24a5a6166c8948aba23d68aa39e206fc1410138ad218500749b75e2ae92d730",
		"name": "Phase 1",
		"description": "Description for phase 1",
		"url": "www.phase1.com",
		"znnFundsNeeded": "10",
		"qsrFundsNeeded": "0",
		"creationTimestamp": 1000003620,
		"acceptedTimestamp": 1000007210,
		"status": 2
	},
	"votes": {
		"id": "05f123c4e83b1cf5559638e2acfb1c2eb8575797b34b2d4b8d95490a7251f4b5",
		"total": 2,
		"yes": 2,
		"no": 0
	}
}`)
	z.InsertNewMomentum() // cemented send block
	z.InsertNewMomentum() // cemented token-receive-block
	common.Json(ledgerApi.GetUnreceivedBlocksByAddress(g.User1.Address, 0, 5)).Equals(t, `
{
	"list": [
		{
			"version": 1,
			"chainIdentifier": 100,
			"blockType": 4,
			"hash": "1878fc6d915eedea2c24899d75fca0a71e89e55bf5cf2d5a25b714b80eceed71",
			"previousHash": "3242f8908b0382d7c1060dcb95b884b451798b7f1067c377d8c655193a20ff24",
			"height": 8,
			"momentumAcknowledged": {
				"hash": "51df5fe96ece028a02354693e19d069f1911b5f3a695cf6997beaa40f0dad2b9",
				"height": 722
			},
			"address": "z1qxemdeddedxaccelerat0rxxxxxxxxxxp4tk22",
			"toAddress": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
			"amount": "10",
			"tokenStandard": "zts1znnxxxxxxxxxxxxx9z4ulx",
			"fromBlockHash": "0000000000000000000000000000000000000000000000000000000000000000",
			"descendantBlocks": [],
			"data": "BfEjxOg7HPVVljjirPscLrhXV5ezSy1LjZVJCnJR9LU=",
			"fusedPlasma": 0,
			"difficulty": 0,
			"nonce": "0000000000000000",
			"basePlasma": 0,
			"usedPlasma": 0,
			"changesHash": "0000000000000000000000000000000000000000000000000000000000000000",
			"publicKey": null,
			"signature": null,
			"token": {
				"name": "Zenon Coin",
				"symbol": "ZNN",
				"domain": "zenon.network",
				"totalSupply": "19874400000000",
				"decimals": 8,
				"owner": "z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg",
				"tokenStandard": "zts1znnxxxxxxxxxxxxx9z4ulx",
				"maxSupply": "4611686018427387903",
				"isBurnable": true,
				"isMintable": true,
				"isUtility": true
			},
			"confirmationDetail": {
				"numConfirmations": 2,
				"momentumHeight": 723,
				"momentumHash": "ed5609e0ab225c50c6466b377e7fe18d059eb17e4153b707527c41b63ead8d0f",
				"momentumTimestamp": 1000007220
			},
			"pairedAccountBlock": null
		},
		{
			"version": 1,
			"chainIdentifier": 100,
			"blockType": 4,
			"hash": "9ce68cd0b7caf61d522bd1b9c4f946f6027a887b50da87e2708bdb0f5e5b8cc5",
			"previousHash": "1878fc6d915eedea2c24899d75fca0a71e89e55bf5cf2d5a25b714b80eceed71",
			"height": 9,
			"momentumAcknowledged": {
				"hash": "51df5fe96ece028a02354693e19d069f1911b5f3a695cf6997beaa40f0dad2b9",
				"height": 722
			},
			"address": "z1qxemdeddedxaccelerat0rxxxxxxxxxxp4tk22",
			"toAddress": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
			"amount": "0",
			"tokenStandard": "zts1qsrxxxxxxxxxxxxxmrhjll",
			"fromBlockHash": "0000000000000000000000000000000000000000000000000000000000000000",
			"descendantBlocks": [],
			"data": "BfEjxOg7HPVVljjirPscLrhXV5ezSy1LjZVJCnJR9LU=",
			"fusedPlasma": 0,
			"difficulty": 0,
			"nonce": "0000000000000000",
			"basePlasma": 0,
			"usedPlasma": 0,
			"changesHash": "0000000000000000000000000000000000000000000000000000000000000000",
			"publicKey": null,
			"signature": null,
			"token": {
				"name": "QuasarCoin",
				"symbol": "QSR",
				"domain": "zenon.network",
				"totalSupply": "181550000000000",
				"decimals": 8,
				"owner": "z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62",
				"tokenStandard": "zts1qsrxxxxxxxxxxxxxmrhjll",
				"maxSupply": "4611686018427387903",
				"isBurnable": true,
				"isMintable": true,
				"isUtility": true
			},
			"confirmationDetail": {
				"numConfirmations": 2,
				"momentumHeight": 723,
				"momentumHash": "ed5609e0ab225c50c6466b377e7fe18d059eb17e4153b707527c41b63ead8d0f",
				"momentumTimestamp": 1000007220
			},
			"pairedAccountBlock": null
		}
	],
	"count": 2,
	"more": false
}`)
}

func TestAccelerator_AcceptPhaseWithInsufficientFunds(t *testing.T) {
	z := mock.NewMockZenonWithCustomEpochDuration(t, time.Hour)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).Equals(t, `
t=2001-09-09T01:46:50+0000 lvl=dbug msg=created module=embedded contract=spork spork="&{Id:34d8229bd07586c243c6e74122a18d6d2002694c72964a7186111026a9cec6ab Name:spork-accelerator Description:activate spork for accelerator Activated:false EnforcementHeight:0}"
t=2001-09-09T01:47:00+0000 lvl=dbug msg=activated module=embedded contract=spork spork="&{Id:34d8229bd07586c243c6e74122a18d6d2002694c72964a7186111026a9cec6ab Name:spork-accelerator Description:activate spork for accelerator Activated:true EnforcementHeight:9}"
t=2001-09-09T01:50:00+0000 lvl=dbug msg="successfully create project" module=embedded contract=accelerator project="&{Id:c24a5a6166c8948aba23d68aa39e206fc1410138ad218500749b75e2ae92d730 Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz Name:Test Project 1 Description:TEST DESCRIPTION Url:test.com ZnnFundsNeeded:+100 QsrFundsNeeded:+1000 CreationTimestamp:1000000200 LastUpdateTimestamp:1000000200 Status:0 PhaseIds:[]}"
t=2001-09-09T01:50:20+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:c24a5a6166c8948aba23d68aa39e206fc1410138ad218500749b75e2ae92d730 Name:TEST-pillar-1 Vote:0}"
t=2001-09-09T01:50:40+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:c24a5a6166c8948aba23d68aa39e206fc1410138ad218500749b75e2ae92d730 Name:TEST-pillar-cool Vote:0}"
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=0 pillar-name=TEST-pillar-1 reward="&{DelegationReward:+11995123837 BlockReward:+9916666627 TotalReward:+21911790464 ProducedBlockNum:119 ExpectedBlockNum:120 Weight:+2099916666666}" total-weight=2499916666666 self-weight=2099916666666
t=2001-09-09T02:46:40+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=0 pillar-name=TEST-pillar-cool reward="&{DelegationReward:+1152038401 BlockReward:+9999999960 TotalReward:+11152038361 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2499916666666 self-weight=200000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=0 pillar-name=TEST-pillar-znn reward="&{DelegationReward:+1152038401 BlockReward:+9999999960 TotalReward:+11152038361 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2499916666666 self-weight=200000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=pillar epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxsentynelxxxxxxxxxxxxxwy0r2r current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating sentinel reward" module=embedded contract=sentinel epoch=0 total-znn-reward=187200000000 total-qsr-reward=500000000000 cumulated-sentinel=0 start-time=1000000000 end-time=1000003600
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=sentinel epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating stake reward" module=embedded contract=stake epoch=0 total-reward=1000000000000 cumulated-stake=0 start-time=1000000000 end-time=1000003600
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=stake epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating liquidity reward" module=embedded contract=liquidity epoch=0 znn-amount=187200000000 qsr-amount=500000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=liquidity epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxaccelerat0rxxxxxxxxxxp4tk22 current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="check accelerator votes" module=embedded contract=accelerator votes="&{Id:c24a5a6166c8948aba23d68aa39e206fc1410138ad218500749b75e2ae92d730 Total:2 Yes:2 No:0}" status=true
t=2001-09-09T02:46:40+0000 lvl=dbug msg="project passed voting period" module=embedded contract=accelerator project-id=c24a5a6166c8948aba23d68aa39e206fc1410138ad218500749b75e2ae92d730 passed-votes=true
t=2001-09-09T02:46:50+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg TokenName:Zenon Coin TokenSymbol:ZNN TokenDomain:zenon.network TotalSupply:+19687200000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1znnxxxxxxxxxxxxx9z4ulx}" minted-amount=187200000000 to-address=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae
t=2001-09-09T02:46:50+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 TokenName:QuasarCoin TokenSymbol:QSR TokenDomain:zenon.network TotalSupply:+181050000000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1qsrxxxxxxxxxxxxxmrhjll}" minted-amount=500000000000 to-address=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae
t=2001-09-09T02:47:00+0000 lvl=info msg="received donation" module=embedded contract=common embedded=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae from-address=z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0 zts=zts1znnxxxxxxxxxxxxx9z4ulx amount=187200000000
t=2001-09-09T02:47:00+0000 lvl=dbug msg="successfully created phase" module=embedded contract=accelerator phase="&{Id:2d05915075b083c48aef01c59524a04e2cce7f8f1b5d08d2e0abf6ba21cc80ed ProjectId:c24a5a6166c8948aba23d68aa39e206fc1410138ad218500749b75e2ae92d730 Name:Phase 1 Description:Description for phase 1 Url:www.phase1.com ZnnFundsNeeded:+10 QsrFundsNeeded:+100 CreationTimestamp:1000003620 AcceptedTimestamp:0 Status:0}"
t=2001-09-09T02:47:00+0000 lvl=info msg="received donation" module=embedded contract=common embedded=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae from-address=z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0 zts=zts1qsrxxxxxxxxxxxxxmrhjll amount=500000000000
t=2001-09-09T02:47:20+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:2d05915075b083c48aef01c59524a04e2cce7f8f1b5d08d2e0abf6ba21cc80ed Name:TEST-pillar-1 Vote:0}"
t=2001-09-09T02:47:40+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:2d05915075b083c48aef01c59524a04e2cce7f8f1b5d08d2e0abf6ba21cc80ed Name:TEST-pillar-cool Vote:0}"
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg current-height=722 last-update-height=361
t=2001-09-09T03:46:50+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=1 pillar-name=TEST-pillar-1 reward="&{DelegationReward:+12095907836 BlockReward:+9999999960 TotalReward:+22095907796 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+2099900000000}" total-weight=2499900000000 self-weight=2099900000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=1 pillar-name=TEST-pillar-cool reward="&{DelegationReward:+1152046081 BlockReward:+9999999960 TotalReward:+11152046041 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2499900000000 self-weight=200000000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=1 pillar-name=TEST-pillar-znn reward="&{DelegationReward:+1152046081 BlockReward:+9999999960 TotalReward:+11152046041 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2499900000000 self-weight=200000000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=pillar epoch=2
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxsentynelxxxxxxxxxxxxxwy0r2r current-height=722 last-update-height=361
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating sentinel reward" module=embedded contract=sentinel epoch=1 total-znn-reward=187200000000 total-qsr-reward=500000000000 cumulated-sentinel=0 start-time=1000003600 end-time=1000007200
t=2001-09-09T03:46:50+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=sentinel epoch=2
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 current-height=722 last-update-height=361
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating stake reward" module=embedded contract=stake epoch=1 total-reward=1000000000000 cumulated-stake=0 start-time=1000003600 end-time=1000007200
t=2001-09-09T03:46:50+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=stake epoch=2
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae current-height=722 last-update-height=361
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating liquidity reward" module=embedded contract=liquidity epoch=1 znn-amount=187200000000 qsr-amount=500000000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=liquidity epoch=2
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxaccelerat0rxxxxxxxxxxp4tk22 current-height=722 last-update-height=361
t=2001-09-09T03:46:50+0000 lvl=dbug msg="check accelerator votes" module=embedded contract=accelerator votes="&{Id:2d05915075b083c48aef01c59524a04e2cce7f8f1b5d08d2e0abf6ba21cc80ed Total:2 Yes:2 No:0}" status=true
t=2001-09-09T03:47:00+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg TokenName:Zenon Coin TokenSymbol:ZNN TokenDomain:zenon.network TotalSupply:+19874400000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1znnxxxxxxxxxxxxx9z4ulx}" minted-amount=187200000000 to-address=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae
t=2001-09-09T03:47:00+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 TokenName:QuasarCoin TokenSymbol:QSR TokenDomain:zenon.network TotalSupply:+181550000000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1qsrxxxxxxxxxxxxxmrhjll}" minted-amount=500000000000 to-address=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae
t=2001-09-09T03:47:10+0000 lvl=info msg="received donation" module=embedded contract=common embedded=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae from-address=z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0 zts=zts1znnxxxxxxxxxxxxx9z4ulx amount=187200000000
t=2001-09-09T03:47:10+0000 lvl=info msg="received donation" module=embedded contract=common embedded=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae from-address=z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0 zts=zts1qsrxxxxxxxxxxxxxmrhjll amount=500000000000
`)

	activateAccelerator(z)
	acceleratorAPI := embedded.NewAcceleratorApi(z)
	ledgerApi := api.NewLedgerApi(z)

	defer z.CallContract(&nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     types.AcceleratorContract,
		TokenStandard: types.ZnnTokenStandard,
		Amount:        constants.ProjectCreationAmount,
		Data: definition.ABIAccelerator.PackMethodPanic(definition.CreateProjectMethodName,
			"Test Project 1",   //param.Name
			"TEST DESCRIPTION", //param.Description
			"test.com",         //param.Url
			big.NewInt(100),    //param.ZnnFundsNeeded
			big.NewInt(1000),   //param.QsrFundsNeeded
		),
	}).Error(t, nil)
	z.InsertNewMomentum() // cemented send block
	z.InsertNewMomentum() // cemented token-receive-block

	projectList, err := acceleratorAPI.GetAll(0, 10)
	common.FailIfErr(t, err)

	defer z.CallContract(&nom.AccountBlock{
		Address:   g.Pillar1.Address,
		ToAddress: types.AcceleratorContract,
		Data: definition.ABIAccelerator.PackMethodPanic(definition.VoteByProdAddressMethodName,
			projectList.List[0].Id,
			definition.VoteYes,
		),
	}).Error(t, nil)
	z.InsertNewMomentum() // cemented send block
	z.InsertNewMomentum() // cemented token-receive-block

	defer z.CallContract(&nom.AccountBlock{
		Address:   g.Pillar2.Address,
		ToAddress: types.AcceleratorContract,
		Data: definition.ABIAccelerator.PackMethodPanic(definition.VoteByProdAddressMethodName,
			projectList.List[0].Id,
			definition.VoteYes,
		),
	}).Error(t, nil)
	z.InsertNewMomentum() // cemented send block
	z.InsertNewMomentum() // cemented token-receive-block

	z.InsertMomentumsTo(60*6 + 2)

	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.AcceleratorContract,
		Data: definition.ABIAccelerator.PackMethodPanic(definition.AddPhaseMethodName,
			projectList.List[0].Id,    //param.Hash
			"Phase 1",                 //param.Name
			"Description for phase 1", //param.Description
			"www.phase1.com",          //param.Url
			big.NewInt(10),            //param.ZnnFundsNeeded
			big.NewInt(100),           //param.QsrFundsNeeded
		),
	}).Error(t, nil)
	z.InsertNewMomentum() // cemented send block
	z.InsertNewMomentum() // cemented token-receive-block
	projectList, err = acceleratorAPI.GetAll(0, 10)
	common.FailIfErr(t, err)

	defer z.CallContract(&nom.AccountBlock{
		Address:   g.Pillar1.Address,
		ToAddress: types.AcceleratorContract,
		Data: definition.ABIAccelerator.PackMethodPanic(definition.VoteByProdAddressMethodName,
			projectList.List[0].PhaseIds[0],
			definition.VoteYes,
		),
	}).Error(t, nil)
	z.InsertNewMomentum() // cemented send block
	z.InsertNewMomentum() // cemented token-receive-block

	defer z.CallContract(&nom.AccountBlock{
		Address:   g.Pillar2.Address,
		ToAddress: types.AcceleratorContract,
		Data: definition.ABIAccelerator.PackMethodPanic(definition.VoteByProdAddressMethodName,
			projectList.List[0].PhaseIds[0],
			definition.VoteYes,
		),
	}).Error(t, nil)
	z.InsertNewMomentum() // cemented send block
	z.InsertNewMomentum() // cemented token-receive-block

	z.InsertMomentumsTo(60*6*2 + 2)

	z.ExpectBalance(types.AcceleratorContract, types.ZnnTokenStandard, 100000000)

	common.Json(acceleratorAPI.GetPillarVotes(g.Pillar1Name, projectList.List[0].PhaseIds)).Equals(t, `
[
	{
		"id": "2d05915075b083c48aef01c59524a04e2cce7f8f1b5d08d2e0abf6ba21cc80ed",
		"name": "TEST-pillar-1",
		"vote": 0
	}
]`)
	common.Json(acceleratorAPI.GetVoteBreakdown(projectList.List[0].Id)).Equals(t, `
{
	"id": "c24a5a6166c8948aba23d68aa39e206fc1410138ad218500749b75e2ae92d730",
	"total": 2,
	"yes": 2,
	"no": 0
}`)

	common.Json(acceleratorAPI.GetPhaseById(projectList.List[0].PhaseIds[0])).Equals(t, `
{
	"phase": {
		"id": "2d05915075b083c48aef01c59524a04e2cce7f8f1b5d08d2e0abf6ba21cc80ed",
		"projectID": "c24a5a6166c8948aba23d68aa39e206fc1410138ad218500749b75e2ae92d730",
		"name": "Phase 1",
		"description": "Description for phase 1",
		"url": "www.phase1.com",
		"znnFundsNeeded": "10",
		"qsrFundsNeeded": "100",
		"creationTimestamp": 1000003620,
		"acceptedTimestamp": 0,
		"status": 0
	},
	"votes": {
		"id": "2d05915075b083c48aef01c59524a04e2cce7f8f1b5d08d2e0abf6ba21cc80ed",
		"total": 2,
		"yes": 2,
		"no": 0
	}
}`)
	z.InsertNewMomentum() // cemented send block
	z.InsertNewMomentum() // cemented token-receive-block
	common.Json(ledgerApi.GetUnreceivedBlocksByAddress(g.User1.Address, 0, 5)).Equals(t, `
{
	"list": [],
	"count": 0,
	"more": false
}`)
}

func TestAccelerator_LiquidityDonate(t *testing.T) {
	z := mock.NewMockZenonWithCustomEpochDuration(t, time.Hour)
	defer z.StopPanic()

	defer z.SaveLogs(common.EmbeddedLogger).Equals(t, `
t=2001-09-09T01:46:50+0000 lvl=info msg="received donation" module=embedded contract=common embedded=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae from-address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz zts=zts1znnxxxxxxxxxxxxx9z4ulx amount=100
t=2001-09-09T01:46:50+0000 lvl=info msg="received donation" module=embedded contract=common embedded=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae from-address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz zts=zts1qsrxxxxxxxxxxxxxmrhjll amount=100
t=2001-09-09T01:48:20+0000 lvl=dbug msg=created module=embedded contract=spork spork="&{Id:63a61c0dc3ca217ddd2f3147e59f8d0193566d3e05bfb4378d1f7b2f6eb7f9ab Name:spork-accelerator Description:activate spork for accelerator Activated:false EnforcementHeight:0}"
t=2001-09-09T01:48:30+0000 lvl=dbug msg=activated module=embedded contract=spork spork="&{Id:63a61c0dc3ca217ddd2f3147e59f8d0193566d3e05bfb4378d1f7b2f6eb7f9ab Name:spork-accelerator Description:activate spork for accelerator Activated:true EnforcementHeight:18}"
t=2001-09-09T01:50:00+0000 lvl=dbug msg="donate reward to accelerator" module=embedded contract=liquidity znn-amount=1 qsr-amount=1
t=2001-09-09T01:50:10+0000 lvl=info msg="received donation" module=embedded contract=common embedded=z1qxemdeddedxaccelerat0rxxxxxxxxxxp4tk22 from-address=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae zts=zts1znnxxxxxxxxxxxxx9z4ulx amount=1
t=2001-09-09T01:50:10+0000 lvl=info msg="received donation" module=embedded contract=common embedded=z1qxemdeddedxaccelerat0rxxxxxxxxxxp4tk22 from-address=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae zts=zts1qsrxxxxxxxxxxxxxmrhjll amount=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=0 pillar-name=TEST-pillar-1 reward="&{DelegationReward:+11995199999 BlockReward:+9916666627 TotalReward:+21911866626 ProducedBlockNum:119 ExpectedBlockNum:120 Weight:+2099999999916}" total-weight=2499999999916 self-weight=2099999999916
t=2001-09-09T02:46:40+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=0 pillar-name=TEST-pillar-cool reward="&{DelegationReward:+1152000000 BlockReward:+9999999960 TotalReward:+11151999960 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2499999999916 self-weight=200000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=0 pillar-name=TEST-pillar-znn reward="&{DelegationReward:+1152000000 BlockReward:+9999999960 TotalReward:+11151999960 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2499999999916 self-weight=200000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=pillar epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxsentynelxxxxxxxxxxxxxwy0r2r current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating sentinel reward" module=embedded contract=sentinel epoch=0 total-znn-reward=187200000000 total-qsr-reward=500000000000 cumulated-sentinel=0 start-time=1000000000 end-time=1000003600
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=sentinel epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating stake reward" module=embedded contract=stake epoch=0 total-reward=1000000000000 cumulated-stake=0 start-time=1000000000 end-time=1000003600
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=stake epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating liquidity reward" module=embedded contract=liquidity epoch=0 znn-amount=187200000000 qsr-amount=500000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=liquidity epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxaccelerat0rxxxxxxxxxxp4tk22 current-height=361 last-update-height=0
t=2001-09-09T02:46:50+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg TokenName:Zenon Coin TokenSymbol:ZNN TokenDomain:zenon.network TotalSupply:+19687200000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1znnxxxxxxxxxxxxx9z4ulx}" minted-amount=187200000000 to-address=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae
t=2001-09-09T02:46:50+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 TokenName:QuasarCoin TokenSymbol:QSR TokenDomain:zenon.network TotalSupply:+181050000000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1qsrxxxxxxxxxxxxxmrhjll}" minted-amount=500000000000 to-address=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae
t=2001-09-09T02:47:00+0000 lvl=info msg="received donation" module=embedded contract=common embedded=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae from-address=z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0 zts=zts1znnxxxxxxxxxxxxx9z4ulx amount=187200000000
t=2001-09-09T02:47:00+0000 lvl=info msg="received donation" module=embedded contract=common embedded=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae from-address=z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0 zts=zts1qsrxxxxxxxxxxxxxmrhjll amount=500000000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg current-height=722 last-update-height=361
t=2001-09-09T03:46:50+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=1 pillar-name=TEST-pillar-1 reward="&{DelegationReward:+12095999999 BlockReward:+9999999960 TotalReward:+22095999959 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+2099999999900}" total-weight=2499999999900 self-weight=2099999999900
t=2001-09-09T03:46:50+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=1 pillar-name=TEST-pillar-cool reward="&{DelegationReward:+1152000000 BlockReward:+9999999960 TotalReward:+11151999960 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2499999999900 self-weight=200000000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=1 pillar-name=TEST-pillar-znn reward="&{DelegationReward:+1152000000 BlockReward:+9999999960 TotalReward:+11151999960 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2499999999900 self-weight=200000000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=pillar epoch=2
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxsentynelxxxxxxxxxxxxxwy0r2r current-height=722 last-update-height=361
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating sentinel reward" module=embedded contract=sentinel epoch=1 total-znn-reward=187200000000 total-qsr-reward=500000000000 cumulated-sentinel=0 start-time=1000003600 end-time=1000007200
t=2001-09-09T03:46:50+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=sentinel epoch=2
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 current-height=722 last-update-height=361
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating stake reward" module=embedded contract=stake epoch=1 total-reward=1000000000000 cumulated-stake=0 start-time=1000003600 end-time=1000007200
t=2001-09-09T03:46:50+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=stake epoch=2
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae current-height=722 last-update-height=361
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating liquidity reward" module=embedded contract=liquidity epoch=1 znn-amount=187200000000 qsr-amount=500000000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=liquidity epoch=2
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxaccelerat0rxxxxxxxxxxp4tk22 current-height=722 last-update-height=361
`)

	defer z.CallContract(&nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     types.LiquidityContract,
		Data:          definition.ABICommon.PackMethodPanic(definition.DonateMethodName),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        common.Big100,
	}).Error(t, nil)
	defer z.CallContract(&nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     types.LiquidityContract,
		Data:          definition.ABICommon.PackMethodPanic(definition.DonateMethodName),
		TokenStandard: types.QsrTokenStandard,
		Amount:        common.Big100,
	}).Error(t, nil)

	z.InsertMomentumsTo(10)
	z.ExpectBalance(types.LiquidityContract, types.ZnnTokenStandard, 100)
	z.ExpectBalance(types.LiquidityContract, types.QsrTokenStandard, 100)

	activateAccelerator(z)
	z.InsertSendBlock(&nom.AccountBlock{
		Address:   g.Spork.Address,
		ToAddress: types.LiquidityContract,
		Data: definition.ABILiquidity.PackMethodPanic(definition.FundMethodName,
			common.Big1, // znnReward
			common.Big1, // qsrReward
		),
	}, nil, mock.SkipVmChanges)
	z.InsertNewMomentum()
	z.InsertNewMomentum()
	z.ExpectBalance(types.AcceleratorContract, types.ZnnTokenStandard, 1)
	z.ExpectBalance(types.AcceleratorContract, types.QsrTokenStandard, 1)
	z.ExpectBalance(types.LiquidityContract, types.ZnnTokenStandard, 99)
	z.ExpectBalance(types.LiquidityContract, types.QsrTokenStandard, 99)

	z.InsertMomentumsTo(60*6*2 + 2)
	z.ExpectBalance(types.AcceleratorContract, types.ZnnTokenStandard, 1)
	z.ExpectBalance(types.AcceleratorContract, types.QsrTokenStandard, 1)
	z.ExpectBalance(types.LiquidityContract, types.ZnnTokenStandard, 99+187200000000)
	z.ExpectBalance(types.LiquidityContract, types.QsrTokenStandard, 99+500000000000)

}

func TestAccelerator_CreateMultipleProjects(t *testing.T) {
	z := mock.NewMockZenonWithCustomEpochDuration(t, time.Hour)
	acceleratorAPI := embedded.NewAcceleratorApi(z)
	defer z.StopPanic()

	defer z.SaveLogs(common.EmbeddedLogger).Equals(t, `
t=2001-09-09T01:46:50+0000 lvl=info msg="received donation" module=embedded contract=common embedded=z1qxemdeddedxaccelerat0rxxxxxxxxxxp4tk22 from-address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz zts=zts1znnxxxxxxxxxxxxx9z4ulx amount=100
t=2001-09-09T01:47:10+0000 lvl=info msg="received donation" module=embedded contract=common embedded=z1qxemdeddedxaccelerat0rxxxxxxxxxxp4tk22 from-address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz zts=zts1qsrxxxxxxxxxxxxxmrhjll amount=100
t=2001-09-09T01:47:30+0000 lvl=dbug msg=created module=embedded contract=spork spork="&{Id:46678f209a6c9327a423dba5b34034adb3569d080778eef99e4513f1028e652e Name:spork-accelerator Description:activate spork for accelerator Activated:false EnforcementHeight:0}"
t=2001-09-09T01:47:40+0000 lvl=dbug msg=activated module=embedded contract=spork spork="&{Id:46678f209a6c9327a423dba5b34034adb3569d080778eef99e4513f1028e652e Name:spork-accelerator Description:activate spork for accelerator Activated:true EnforcementHeight:13}"
t=2001-09-09T01:50:00+0000 lvl=dbug msg="successfully create project" module=embedded contract=accelerator project="&{Id:ecaf6835810af7641900e5ec6831e37a46ad4d4937c807ea11af108701e1629e Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz Name:Test Project 1 Description:TEST DESCRIPTION Url:test.com ZnnFundsNeeded:+5 QsrFundsNeeded:+5 CreationTimestamp:1000000200 LastUpdateTimestamp:1000000200 Status:0 PhaseIds:[]}"
t=2001-09-09T01:50:20+0000 lvl=dbug msg="successfully create project" module=embedded contract=accelerator project="&{Id:01e311586f4639e25e52ffb94113e31f65de9a28612ac8b38170e39f5511727c Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz Name:Test Project 2 Description:TEST DESCRIPTION Url:test.com ZnnFundsNeeded:+5 QsrFundsNeeded:+5 CreationTimestamp:1000000220 LastUpdateTimestamp:1000000220 Status:0 PhaseIds:[]}"
t=2001-09-09T01:50:40+0000 lvl=dbug msg="successfully create project" module=embedded contract=accelerator project="&{Id:42253d277232616a64749799b53421de8a0bb6bbf577793a2c58aace77db6813 Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz Name:Test Project 3 Description:TEST DESCRIPTION Url:test.com ZnnFundsNeeded:+5 QsrFundsNeeded:+5 CreationTimestamp:1000000240 LastUpdateTimestamp:1000000240 Status:0 PhaseIds:[]}"
t=2001-09-09T01:51:00+0000 lvl=dbug msg="successfully create project" module=embedded contract=accelerator project="&{Id:0c05b7490d237b2be5c1d26ad6864bd4508f29cf6332402ca03646992dac3a3f Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz Name:Test Project 4 Description:TEST DESCRIPTION Url:test.com ZnnFundsNeeded:+5 QsrFundsNeeded:+5 CreationTimestamp:1000000260 LastUpdateTimestamp:1000000260 Status:0 PhaseIds:[]}"
t=2001-09-09T01:51:20+0000 lvl=dbug msg="successfully create project" module=embedded contract=accelerator project="&{Id:210f74710e9b98655216e80fd76d8691c824f4d9a8f899151f05e28723d1cfd4 Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz Name:Test Project 5 Description:TEST DESCRIPTION Url:test.com ZnnFundsNeeded:+5 QsrFundsNeeded:+5 CreationTimestamp:1000000280 LastUpdateTimestamp:1000000280 Status:0 PhaseIds:[]}"
t=2001-09-09T01:51:40+0000 lvl=dbug msg="successfully create project" module=embedded contract=accelerator project="&{Id:f3b56eab0e0b516660385c4a9731c3e33089ccdda4e493dc1767db98a1e4634e Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz Name:Test Project 6 Description:TEST DESCRIPTION Url:test.com ZnnFundsNeeded:+5 QsrFundsNeeded:+5 CreationTimestamp:1000000300 LastUpdateTimestamp:1000000300 Status:0 PhaseIds:[]}"
t=2001-09-09T01:52:00+0000 lvl=dbug msg="successfully create project" module=embedded contract=accelerator project="&{Id:44909794893c762f79b8ddc43e2f52f2ec3462d02a4242fa1cade2676a46ea1c Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz Name:Test Project 7 Description:TEST DESCRIPTION Url:test.com ZnnFundsNeeded:+5 QsrFundsNeeded:+5 CreationTimestamp:1000000320 LastUpdateTimestamp:1000000320 Status:0 PhaseIds:[]}"
t=2001-09-09T01:52:20+0000 lvl=dbug msg="successfully create project" module=embedded contract=accelerator project="&{Id:a61aa098c7a4e8883c7186b3b5727b67e65a4a6fb5b5b791d21077b49c34344c Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz Name:Test Project 8 Description:TEST DESCRIPTION Url:test.com ZnnFundsNeeded:+5 QsrFundsNeeded:+5 CreationTimestamp:1000000340 LastUpdateTimestamp:1000000340 Status:0 PhaseIds:[]}"
t=2001-09-09T01:52:40+0000 lvl=dbug msg="successfully create project" module=embedded contract=accelerator project="&{Id:25648e80063cf96760ad0e1f66da0fb9a5a414be57f7a9aca20a6e756a6e0686 Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz Name:Test Project 9 Description:TEST DESCRIPTION Url:test.com ZnnFundsNeeded:+5 QsrFundsNeeded:+5 CreationTimestamp:1000000360 LastUpdateTimestamp:1000000360 Status:0 PhaseIds:[]}"
t=2001-09-09T01:53:00+0000 lvl=dbug msg="successfully create project" module=embedded contract=accelerator project="&{Id:ce131e1aaa32f97e9e37ee6b6c12e51bdc311501141a78e099012f71390603f3 Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz Name:Test Project 10 Description:TEST DESCRIPTION Url:test.com ZnnFundsNeeded:+5 QsrFundsNeeded:+5 CreationTimestamp:1000000380 LastUpdateTimestamp:1000000380 Status:0 PhaseIds:[]}"
t=2001-09-09T01:53:20+0000 lvl=dbug msg="successfully create project" module=embedded contract=accelerator project="&{Id:1eee79ef32aa34b43a88e1ffd6e5ba1694e78bd69b405a9c89e7a12c993f3693 Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz Name:Test Project 11 Description:TEST DESCRIPTION Url:test.com ZnnFundsNeeded:+5 QsrFundsNeeded:+5 CreationTimestamp:1000000400 LastUpdateTimestamp:1000000400 Status:0 PhaseIds:[]}"
t=2001-09-09T01:53:40+0000 lvl=dbug msg="successfully create project" module=embedded contract=accelerator project="&{Id:0706d20fc6d6fcaac0b8b82fc8db26d74b1c195ed97c20914759c65c1c75c437 Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz Name:Test Project 12 Description:TEST DESCRIPTION Url:test.com ZnnFundsNeeded:+5 QsrFundsNeeded:+5 CreationTimestamp:1000000420 LastUpdateTimestamp:1000000420 Status:0 PhaseIds:[]}"
t=2001-09-09T01:54:00+0000 lvl=dbug msg="successfully create project" module=embedded contract=accelerator project="&{Id:c98e6ca9f0bb8b06a215984855f8b3f1ad7ebe7fcae2300348b668af5a72de22 Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz Name:Test Project 13 Description:TEST DESCRIPTION Url:test.com ZnnFundsNeeded:+5 QsrFundsNeeded:+5 CreationTimestamp:1000000440 LastUpdateTimestamp:1000000440 Status:0 PhaseIds:[]}"
t=2001-09-09T01:54:20+0000 lvl=dbug msg="successfully create project" module=embedded contract=accelerator project="&{Id:52e284cc2ed9e78bf580b37d986dc6d5bc3a937b0a40e2712f543881c7548513 Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz Name:Test Project 14 Description:TEST DESCRIPTION Url:test.com ZnnFundsNeeded:+5 QsrFundsNeeded:+5 CreationTimestamp:1000000460 LastUpdateTimestamp:1000000460 Status:0 PhaseIds:[]}"
t=2001-09-09T01:54:40+0000 lvl=dbug msg="successfully create project" module=embedded contract=accelerator project="&{Id:f96de28610e2b3b416f7639de5c71350df110b5df3f968aa36dd5c86a939e69d Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz Name:Test Project 15 Description:TEST DESCRIPTION Url:test.com ZnnFundsNeeded:+5 QsrFundsNeeded:+5 CreationTimestamp:1000000480 LastUpdateTimestamp:1000000480 Status:0 PhaseIds:[]}"
t=2001-09-09T01:55:00+0000 lvl=dbug msg="successfully create project" module=embedded contract=accelerator project="&{Id:2d0c8604e5e0f5bc39db674cbe60d5074b612e2f537bde28c86b02804d60c0d4 Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz Name:Test Project 16 Description:TEST DESCRIPTION Url:test.com ZnnFundsNeeded:+5 QsrFundsNeeded:+5 CreationTimestamp:1000000500 LastUpdateTimestamp:1000000500 Status:0 PhaseIds:[]}"
t=2001-09-09T01:55:20+0000 lvl=dbug msg="successfully create project" module=embedded contract=accelerator project="&{Id:3b7ee3ecb69f34e59d5c26abe157ed5efaf53e4892e3d87a74719bd195aabedd Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz Name:Test Project 17 Description:TEST DESCRIPTION Url:test.com ZnnFundsNeeded:+5 QsrFundsNeeded:+5 CreationTimestamp:1000000520 LastUpdateTimestamp:1000000520 Status:0 PhaseIds:[]}"
t=2001-09-09T01:55:40+0000 lvl=dbug msg="successfully create project" module=embedded contract=accelerator project="&{Id:9ae410b287268f914c99274ef0e90f1765224d6eccfba1b58dcd633fa02b191a Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz Name:Test Project 18 Description:TEST DESCRIPTION Url:test.com ZnnFundsNeeded:+5 QsrFundsNeeded:+5 CreationTimestamp:1000000540 LastUpdateTimestamp:1000000540 Status:0 PhaseIds:[]}"
t=2001-09-09T01:56:00+0000 lvl=dbug msg="successfully create project" module=embedded contract=accelerator project="&{Id:5068e9bbd0e09abc79a7675bb04cca3ce3de38259218a38ef52f77ad96ba776a Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz Name:Test Project 19 Description:TEST DESCRIPTION Url:test.com ZnnFundsNeeded:+5 QsrFundsNeeded:+5 CreationTimestamp:1000000560 LastUpdateTimestamp:1000000560 Status:0 PhaseIds:[]}"
t=2001-09-09T01:56:20+0000 lvl=dbug msg="successfully create project" module=embedded contract=accelerator project="&{Id:94771e772122504233051b9a11f2049ea3c7566fc3f1a0f36da7743e53c04619 Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz Name:Test Project 20 Description:TEST DESCRIPTION Url:test.com ZnnFundsNeeded:+5 QsrFundsNeeded:+5 CreationTimestamp:1000000580 LastUpdateTimestamp:1000000580 Status:0 PhaseIds:[]}"
t=2001-09-09T01:56:40+0000 lvl=dbug msg="successfully create project" module=embedded contract=accelerator project="&{Id:93c77e702a9da2a83ad3f2b9e662eaccdc20eacce986759b33a2de55ed0a1746 Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz Name:Test Project 21 Description:TEST DESCRIPTION Url:test.com ZnnFundsNeeded:+5 QsrFundsNeeded:+5 CreationTimestamp:1000000600 LastUpdateTimestamp:1000000600 Status:0 PhaseIds:[]}"
t=2001-09-09T01:57:00+0000 lvl=dbug msg="successfully create project" module=embedded contract=accelerator project="&{Id:00810eaa6c0be3fc06235533adc8fd8f809916efa9e13bd7761288cfa77ad7ac Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz Name:Test Project 22 Description:TEST DESCRIPTION Url:test.com ZnnFundsNeeded:+5 QsrFundsNeeded:+5 CreationTimestamp:1000000620 LastUpdateTimestamp:1000000620 Status:0 PhaseIds:[]}"
t=2001-09-09T01:57:20+0000 lvl=dbug msg="successfully create project" module=embedded contract=accelerator project="&{Id:413bd235e6186445895a2fa2e73e33d4ebd551ca6cfbbaac7541c7007037dd11 Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz Name:Test Project 23 Description:TEST DESCRIPTION Url:test.com ZnnFundsNeeded:+5 QsrFundsNeeded:+5 CreationTimestamp:1000000640 LastUpdateTimestamp:1000000640 Status:0 PhaseIds:[]}"
t=2001-09-09T01:57:40+0000 lvl=dbug msg="successfully create project" module=embedded contract=accelerator project="&{Id:2bbae9f42e844451189bd76e85eee4150c67fec86afc2532df707ab9a2e02ace Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz Name:Test Project 24 Description:TEST DESCRIPTION Url:test.com ZnnFundsNeeded:+5 QsrFundsNeeded:+5 CreationTimestamp:1000000660 LastUpdateTimestamp:1000000660 Status:0 PhaseIds:[]}"
t=2001-09-09T01:58:00+0000 lvl=dbug msg="successfully create project" module=embedded contract=accelerator project="&{Id:3af378ec6de9728caa5f7c08ea1ddb25f8e44d3801ffd9aeca0de39fbf164215 Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz Name:Test Project 25 Description:TEST DESCRIPTION Url:test.com ZnnFundsNeeded:+5 QsrFundsNeeded:+5 CreationTimestamp:1000000680 LastUpdateTimestamp:1000000680 Status:0 PhaseIds:[]}"
t=2001-09-09T01:58:20+0000 lvl=dbug msg="successfully create project" module=embedded contract=accelerator project="&{Id:1dc34af881d9f834cd6d85b0bbdc372629951b38a34e780d78885bcf4f36dad6 Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz Name:Test Project 26 Description:TEST DESCRIPTION Url:test.com ZnnFundsNeeded:+5 QsrFundsNeeded:+5 CreationTimestamp:1000000700 LastUpdateTimestamp:1000000700 Status:0 PhaseIds:[]}"
t=2001-09-09T01:58:40+0000 lvl=dbug msg="successfully create project" module=embedded contract=accelerator project="&{Id:bce40860a01f8d231f17c950e0e8f41d3cc68d2e6d22a82cd0598910000a8aac Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz Name:Test Project 27 Description:TEST DESCRIPTION Url:test.com ZnnFundsNeeded:+5 QsrFundsNeeded:+5 CreationTimestamp:1000000720 LastUpdateTimestamp:1000000720 Status:0 PhaseIds:[]}"
t=2001-09-09T01:59:00+0000 lvl=dbug msg="successfully create project" module=embedded contract=accelerator project="&{Id:0654ebf690b4256ba72a086b9c81476ab80e7646a417e02e316f393b872565ae Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz Name:Test Project 28 Description:TEST DESCRIPTION Url:test.com ZnnFundsNeeded:+5 QsrFundsNeeded:+5 CreationTimestamp:1000000740 LastUpdateTimestamp:1000000740 Status:0 PhaseIds:[]}"
t=2001-09-09T01:59:20+0000 lvl=dbug msg="successfully create project" module=embedded contract=accelerator project="&{Id:2a3996acac36858b02fbc7a5e40a1e7975befacd086e6c39a6ef01ec899eb00b Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz Name:Test Project 29 Description:TEST DESCRIPTION Url:test.com ZnnFundsNeeded:+5 QsrFundsNeeded:+5 CreationTimestamp:1000000760 LastUpdateTimestamp:1000000760 Status:0 PhaseIds:[]}"
t=2001-09-09T01:59:40+0000 lvl=dbug msg="successfully create project" module=embedded contract=accelerator project="&{Id:82ff45e10718519017cd204bfc3e0afef917600f15ef3159c334ec32338cf61b Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz Name:Test Project 30 Description:TEST DESCRIPTION Url:test.com ZnnFundsNeeded:+5 QsrFundsNeeded:+5 CreationTimestamp:1000000780 LastUpdateTimestamp:1000000780 Status:0 PhaseIds:[]}"
t=2001-09-09T02:00:00+0000 lvl=dbug msg="successfully create project" module=embedded contract=accelerator project="&{Id:cacaa36ee1dc2306c42b5f6f452cbc6ee2c179e064077463ffe701a167ad08e8 Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz Name:Test Project 31 Description:TEST DESCRIPTION Url:test.com ZnnFundsNeeded:+5 QsrFundsNeeded:+5 CreationTimestamp:1000000800 LastUpdateTimestamp:1000000800 Status:0 PhaseIds:[]}"
t=2001-09-09T02:00:20+0000 lvl=dbug msg="successfully create project" module=embedded contract=accelerator project="&{Id:9bcd977b6b114075a2464271bffebcf8e9c553e91e4d612b7461b9c84bd43e53 Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz Name:Test Project 32 Description:TEST DESCRIPTION Url:test.com ZnnFundsNeeded:+5 QsrFundsNeeded:+5 CreationTimestamp:1000000820 LastUpdateTimestamp:1000000820 Status:0 PhaseIds:[]}"
t=2001-09-09T02:00:40+0000 lvl=dbug msg="successfully create project" module=embedded contract=accelerator project="&{Id:cee73558c4cf07b6f233932f09c2ebfba6375dd3eaad46e4cc4bc1abc869e754 Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz Name:Test Project 33 Description:TEST DESCRIPTION Url:test.com ZnnFundsNeeded:+5 QsrFundsNeeded:+5 CreationTimestamp:1000000840 LastUpdateTimestamp:1000000840 Status:0 PhaseIds:[]}"
t=2001-09-09T02:01:00+0000 lvl=dbug msg="successfully create project" module=embedded contract=accelerator project="&{Id:4bd532b48d650af895d7feb6bb00b9e33d8e92984f7df122e67d7c8338d6f0ef Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz Name:Test Project 34 Description:TEST DESCRIPTION Url:test.com ZnnFundsNeeded:+5 QsrFundsNeeded:+5 CreationTimestamp:1000000860 LastUpdateTimestamp:1000000860 Status:0 PhaseIds:[]}"
t=2001-09-09T02:01:20+0000 lvl=dbug msg="successfully create project" module=embedded contract=accelerator project="&{Id:1a9a20dc93d06934e49a824018692906fd0cede5e72703b147892154f3c7e8af Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz Name:Test Project 35 Description:TEST DESCRIPTION Url:test.com ZnnFundsNeeded:+5 QsrFundsNeeded:+5 CreationTimestamp:1000000880 LastUpdateTimestamp:1000000880 Status:0 PhaseIds:[]}"
t=2001-09-09T02:01:40+0000 lvl=dbug msg="successfully create project" module=embedded contract=accelerator project="&{Id:c05df0ea4c73d268aab3ae95ea9b9394a2601a5e62cfb07e773c89c61ecc8274 Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz Name:Test Project 36 Description:TEST DESCRIPTION Url:test.com ZnnFundsNeeded:+5 QsrFundsNeeded:+5 CreationTimestamp:1000000900 LastUpdateTimestamp:1000000900 Status:0 PhaseIds:[]}"
t=2001-09-09T02:02:00+0000 lvl=dbug msg="successfully create project" module=embedded contract=accelerator project="&{Id:99308b7f33dbe0bccdb58b5e16c536c9abf9b86d48b0aa8ab461363fe9953064 Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz Name:Test Project 37 Description:TEST DESCRIPTION Url:test.com ZnnFundsNeeded:+5 QsrFundsNeeded:+5 CreationTimestamp:1000000920 LastUpdateTimestamp:1000000920 Status:0 PhaseIds:[]}"
t=2001-09-09T02:02:20+0000 lvl=dbug msg="successfully create project" module=embedded contract=accelerator project="&{Id:9aa41e368f43725a18ad591f37698cd6b0409f55cb3ee2e718df59a25df4f011 Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz Name:Test Project 38 Description:TEST DESCRIPTION Url:test.com ZnnFundsNeeded:+5 QsrFundsNeeded:+5 CreationTimestamp:1000000940 LastUpdateTimestamp:1000000940 Status:0 PhaseIds:[]}"
t=2001-09-09T02:02:40+0000 lvl=dbug msg="successfully create project" module=embedded contract=accelerator project="&{Id:f2fb30731ae535c3d275d4db489bf8aaa7f0891a94b4dea7b641197352bfccb7 Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz Name:Test Project 39 Description:TEST DESCRIPTION Url:test.com ZnnFundsNeeded:+5 QsrFundsNeeded:+5 CreationTimestamp:1000000960 LastUpdateTimestamp:1000000960 Status:0 PhaseIds:[]}"
t=2001-09-09T02:03:00+0000 lvl=dbug msg="successfully create project" module=embedded contract=accelerator project="&{Id:f94ef63940a2e93570fa423db18a74bf93f58157ca8134662187a00074a2080c Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz Name:Test Project 40 Description:TEST DESCRIPTION Url:test.com ZnnFundsNeeded:+5 QsrFundsNeeded:+5 CreationTimestamp:1000000980 LastUpdateTimestamp:1000000980 Status:0 PhaseIds:[]}"
t=2001-09-09T02:03:20+0000 lvl=dbug msg="successfully create project" module=embedded contract=accelerator project="&{Id:6657eeac715335626bf7f4d223bd79a988dad9456d58b39ededd84da61cd84be Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz Name:Test Project 41 Description:TEST DESCRIPTION Url:test.com ZnnFundsNeeded:+5 QsrFundsNeeded:+5 CreationTimestamp:1000001000 LastUpdateTimestamp:1000001000 Status:0 PhaseIds:[]}"
t=2001-09-09T02:03:40+0000 lvl=dbug msg="successfully create project" module=embedded contract=accelerator project="&{Id:22b678b98fe293586ed3cb2a4608ea8d7ddf4fddf9fc412ad9b188f1074fa463 Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz Name:Test Project 42 Description:TEST DESCRIPTION Url:test.com ZnnFundsNeeded:+5 QsrFundsNeeded:+5 CreationTimestamp:1000001020 LastUpdateTimestamp:1000001020 Status:0 PhaseIds:[]}"
t=2001-09-09T02:04:00+0000 lvl=dbug msg="successfully create project" module=embedded contract=accelerator project="&{Id:e0342441bb2213205f6fc84172a960c68dc2eff567000978407de6d056a59775 Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz Name:Test Project 43 Description:TEST DESCRIPTION Url:test.com ZnnFundsNeeded:+5 QsrFundsNeeded:+5 CreationTimestamp:1000001040 LastUpdateTimestamp:1000001040 Status:0 PhaseIds:[]}"
t=2001-09-09T02:04:20+0000 lvl=dbug msg="successfully create project" module=embedded contract=accelerator project="&{Id:9dc277609817c1e220677cb8e42ea73d63ec43b034815500a9306e7c43ca73bc Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz Name:Test Project 44 Description:TEST DESCRIPTION Url:test.com ZnnFundsNeeded:+5 QsrFundsNeeded:+5 CreationTimestamp:1000001060 LastUpdateTimestamp:1000001060 Status:0 PhaseIds:[]}"
t=2001-09-09T02:04:40+0000 lvl=dbug msg="successfully create project" module=embedded contract=accelerator project="&{Id:a4e9307b13dac391e0772f1a4b7f153786f233d68fa3f048100304feb285180a Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz Name:Test Project 45 Description:TEST DESCRIPTION Url:test.com ZnnFundsNeeded:+5 QsrFundsNeeded:+5 CreationTimestamp:1000001080 LastUpdateTimestamp:1000001080 Status:0 PhaseIds:[]}"
t=2001-09-09T02:05:00+0000 lvl=dbug msg="successfully create project" module=embedded contract=accelerator project="&{Id:d7ed623d4204c4b6b96672120349d60c8c8b8aeefb134967e02b95a29fddef70 Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz Name:Test Project 46 Description:TEST DESCRIPTION Url:test.com ZnnFundsNeeded:+5 QsrFundsNeeded:+5 CreationTimestamp:1000001100 LastUpdateTimestamp:1000001100 Status:0 PhaseIds:[]}"
t=2001-09-09T02:05:20+0000 lvl=dbug msg="successfully create project" module=embedded contract=accelerator project="&{Id:8fb32770a4b52898c5abcd054985f0d2776884759606231436ff5ae59914129b Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz Name:Test Project 47 Description:TEST DESCRIPTION Url:test.com ZnnFundsNeeded:+5 QsrFundsNeeded:+5 CreationTimestamp:1000001120 LastUpdateTimestamp:1000001120 Status:0 PhaseIds:[]}"
t=2001-09-09T02:05:40+0000 lvl=dbug msg="successfully create project" module=embedded contract=accelerator project="&{Id:0495aae6660b286625f98ef4997d7be01a96c84f1d481020fd0fc41d6c95d7da Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz Name:Test Project 48 Description:TEST DESCRIPTION Url:test.com ZnnFundsNeeded:+5 QsrFundsNeeded:+5 CreationTimestamp:1000001140 LastUpdateTimestamp:1000001140 Status:0 PhaseIds:[]}"
t=2001-09-09T02:06:00+0000 lvl=dbug msg="successfully create project" module=embedded contract=accelerator project="&{Id:7d13ab27040d4f7063e3bf7c31235296b3ee11ffcb62473131e05af4b17e195f Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz Name:Test Project 49 Description:TEST DESCRIPTION Url:test.com ZnnFundsNeeded:+5 QsrFundsNeeded:+5 CreationTimestamp:1000001160 LastUpdateTimestamp:1000001160 Status:0 PhaseIds:[]}"
t=2001-09-09T02:06:20+0000 lvl=dbug msg="successfully create project" module=embedded contract=accelerator project="&{Id:3272b8baff99aa4ed434b2c719592fefb03cc18848f55918e970d31a5630b8e9 Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz Name:Test Project 50 Description:TEST DESCRIPTION Url:test.com ZnnFundsNeeded:+5 QsrFundsNeeded:+5 CreationTimestamp:1000001180 LastUpdateTimestamp:1000001180 Status:0 PhaseIds:[]}"
t=2001-09-09T02:06:40+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:3272b8baff99aa4ed434b2c719592fefb03cc18848f55918e970d31a5630b8e9 Name:TEST-pillar-1 Vote:0}"
t=2001-09-09T02:07:00+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:3272b8baff99aa4ed434b2c719592fefb03cc18848f55918e970d31a5630b8e9 Name:TEST-pillar-cool Vote:0}"
t=2001-09-09T02:07:20+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:7d13ab27040d4f7063e3bf7c31235296b3ee11ffcb62473131e05af4b17e195f Name:TEST-pillar-1 Vote:0}"
t=2001-09-09T02:07:40+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:7d13ab27040d4f7063e3bf7c31235296b3ee11ffcb62473131e05af4b17e195f Name:TEST-pillar-cool Vote:0}"
t=2001-09-09T02:08:00+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:0495aae6660b286625f98ef4997d7be01a96c84f1d481020fd0fc41d6c95d7da Name:TEST-pillar-1 Vote:0}"
t=2001-09-09T02:08:20+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:0495aae6660b286625f98ef4997d7be01a96c84f1d481020fd0fc41d6c95d7da Name:TEST-pillar-cool Vote:0}"
t=2001-09-09T02:08:40+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:8fb32770a4b52898c5abcd054985f0d2776884759606231436ff5ae59914129b Name:TEST-pillar-1 Vote:0}"
t=2001-09-09T02:09:00+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:8fb32770a4b52898c5abcd054985f0d2776884759606231436ff5ae59914129b Name:TEST-pillar-cool Vote:0}"
t=2001-09-09T02:09:20+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:d7ed623d4204c4b6b96672120349d60c8c8b8aeefb134967e02b95a29fddef70 Name:TEST-pillar-1 Vote:0}"
t=2001-09-09T02:09:40+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:d7ed623d4204c4b6b96672120349d60c8c8b8aeefb134967e02b95a29fddef70 Name:TEST-pillar-cool Vote:0}"
t=2001-09-09T02:10:00+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:a4e9307b13dac391e0772f1a4b7f153786f233d68fa3f048100304feb285180a Name:TEST-pillar-1 Vote:0}"
t=2001-09-09T02:10:20+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:a4e9307b13dac391e0772f1a4b7f153786f233d68fa3f048100304feb285180a Name:TEST-pillar-cool Vote:0}"
t=2001-09-09T02:10:40+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:9dc277609817c1e220677cb8e42ea73d63ec43b034815500a9306e7c43ca73bc Name:TEST-pillar-1 Vote:0}"
t=2001-09-09T02:11:00+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:9dc277609817c1e220677cb8e42ea73d63ec43b034815500a9306e7c43ca73bc Name:TEST-pillar-cool Vote:0}"
t=2001-09-09T02:11:20+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:e0342441bb2213205f6fc84172a960c68dc2eff567000978407de6d056a59775 Name:TEST-pillar-1 Vote:0}"
t=2001-09-09T02:11:40+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:e0342441bb2213205f6fc84172a960c68dc2eff567000978407de6d056a59775 Name:TEST-pillar-cool Vote:0}"
t=2001-09-09T02:12:00+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:22b678b98fe293586ed3cb2a4608ea8d7ddf4fddf9fc412ad9b188f1074fa463 Name:TEST-pillar-1 Vote:0}"
t=2001-09-09T02:12:20+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:22b678b98fe293586ed3cb2a4608ea8d7ddf4fddf9fc412ad9b188f1074fa463 Name:TEST-pillar-cool Vote:0}"
t=2001-09-09T02:12:40+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:6657eeac715335626bf7f4d223bd79a988dad9456d58b39ededd84da61cd84be Name:TEST-pillar-1 Vote:0}"
t=2001-09-09T02:13:00+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:6657eeac715335626bf7f4d223bd79a988dad9456d58b39ededd84da61cd84be Name:TEST-pillar-cool Vote:0}"
t=2001-09-09T02:13:20+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:f94ef63940a2e93570fa423db18a74bf93f58157ca8134662187a00074a2080c Name:TEST-pillar-1 Vote:0}"
t=2001-09-09T02:13:40+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:f94ef63940a2e93570fa423db18a74bf93f58157ca8134662187a00074a2080c Name:TEST-pillar-cool Vote:0}"
t=2001-09-09T02:14:00+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:f2fb30731ae535c3d275d4db489bf8aaa7f0891a94b4dea7b641197352bfccb7 Name:TEST-pillar-1 Vote:0}"
t=2001-09-09T02:14:20+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:f2fb30731ae535c3d275d4db489bf8aaa7f0891a94b4dea7b641197352bfccb7 Name:TEST-pillar-cool Vote:0}"
t=2001-09-09T02:14:40+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:9aa41e368f43725a18ad591f37698cd6b0409f55cb3ee2e718df59a25df4f011 Name:TEST-pillar-1 Vote:0}"
t=2001-09-09T02:15:00+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:9aa41e368f43725a18ad591f37698cd6b0409f55cb3ee2e718df59a25df4f011 Name:TEST-pillar-cool Vote:0}"
t=2001-09-09T02:15:20+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:99308b7f33dbe0bccdb58b5e16c536c9abf9b86d48b0aa8ab461363fe9953064 Name:TEST-pillar-1 Vote:0}"
t=2001-09-09T02:15:40+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:99308b7f33dbe0bccdb58b5e16c536c9abf9b86d48b0aa8ab461363fe9953064 Name:TEST-pillar-cool Vote:0}"
t=2001-09-09T02:16:00+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:c05df0ea4c73d268aab3ae95ea9b9394a2601a5e62cfb07e773c89c61ecc8274 Name:TEST-pillar-1 Vote:0}"
t=2001-09-09T02:16:20+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:c05df0ea4c73d268aab3ae95ea9b9394a2601a5e62cfb07e773c89c61ecc8274 Name:TEST-pillar-cool Vote:0}"
t=2001-09-09T02:16:40+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:1a9a20dc93d06934e49a824018692906fd0cede5e72703b147892154f3c7e8af Name:TEST-pillar-1 Vote:0}"
t=2001-09-09T02:17:00+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:1a9a20dc93d06934e49a824018692906fd0cede5e72703b147892154f3c7e8af Name:TEST-pillar-cool Vote:0}"
t=2001-09-09T02:17:20+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:4bd532b48d650af895d7feb6bb00b9e33d8e92984f7df122e67d7c8338d6f0ef Name:TEST-pillar-1 Vote:0}"
t=2001-09-09T02:17:40+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:4bd532b48d650af895d7feb6bb00b9e33d8e92984f7df122e67d7c8338d6f0ef Name:TEST-pillar-cool Vote:0}"
t=2001-09-09T02:18:00+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:cee73558c4cf07b6f233932f09c2ebfba6375dd3eaad46e4cc4bc1abc869e754 Name:TEST-pillar-1 Vote:0}"
t=2001-09-09T02:18:20+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:cee73558c4cf07b6f233932f09c2ebfba6375dd3eaad46e4cc4bc1abc869e754 Name:TEST-pillar-cool Vote:0}"
t=2001-09-09T02:18:40+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:9bcd977b6b114075a2464271bffebcf8e9c553e91e4d612b7461b9c84bd43e53 Name:TEST-pillar-1 Vote:0}"
t=2001-09-09T02:19:00+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:9bcd977b6b114075a2464271bffebcf8e9c553e91e4d612b7461b9c84bd43e53 Name:TEST-pillar-cool Vote:0}"
t=2001-09-09T02:19:20+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:cacaa36ee1dc2306c42b5f6f452cbc6ee2c179e064077463ffe701a167ad08e8 Name:TEST-pillar-1 Vote:0}"
t=2001-09-09T02:19:40+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:cacaa36ee1dc2306c42b5f6f452cbc6ee2c179e064077463ffe701a167ad08e8 Name:TEST-pillar-cool Vote:0}"
t=2001-09-09T02:20:00+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:82ff45e10718519017cd204bfc3e0afef917600f15ef3159c334ec32338cf61b Name:TEST-pillar-1 Vote:0}"
t=2001-09-09T02:20:20+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:82ff45e10718519017cd204bfc3e0afef917600f15ef3159c334ec32338cf61b Name:TEST-pillar-cool Vote:0}"
t=2001-09-09T02:20:40+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:2a3996acac36858b02fbc7a5e40a1e7975befacd086e6c39a6ef01ec899eb00b Name:TEST-pillar-1 Vote:0}"
t=2001-09-09T02:21:00+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:2a3996acac36858b02fbc7a5e40a1e7975befacd086e6c39a6ef01ec899eb00b Name:TEST-pillar-cool Vote:0}"
t=2001-09-09T02:21:20+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:0654ebf690b4256ba72a086b9c81476ab80e7646a417e02e316f393b872565ae Name:TEST-pillar-1 Vote:0}"
t=2001-09-09T02:21:40+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:0654ebf690b4256ba72a086b9c81476ab80e7646a417e02e316f393b872565ae Name:TEST-pillar-cool Vote:0}"
t=2001-09-09T02:22:00+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:bce40860a01f8d231f17c950e0e8f41d3cc68d2e6d22a82cd0598910000a8aac Name:TEST-pillar-1 Vote:0}"
t=2001-09-09T02:22:20+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:bce40860a01f8d231f17c950e0e8f41d3cc68d2e6d22a82cd0598910000a8aac Name:TEST-pillar-cool Vote:0}"
t=2001-09-09T02:22:40+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:1dc34af881d9f834cd6d85b0bbdc372629951b38a34e780d78885bcf4f36dad6 Name:TEST-pillar-1 Vote:0}"
t=2001-09-09T02:23:00+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:1dc34af881d9f834cd6d85b0bbdc372629951b38a34e780d78885bcf4f36dad6 Name:TEST-pillar-cool Vote:0}"
t=2001-09-09T02:23:20+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:3af378ec6de9728caa5f7c08ea1ddb25f8e44d3801ffd9aeca0de39fbf164215 Name:TEST-pillar-1 Vote:0}"
t=2001-09-09T02:23:40+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:3af378ec6de9728caa5f7c08ea1ddb25f8e44d3801ffd9aeca0de39fbf164215 Name:TEST-pillar-cool Vote:0}"
t=2001-09-09T02:24:00+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:2bbae9f42e844451189bd76e85eee4150c67fec86afc2532df707ab9a2e02ace Name:TEST-pillar-1 Vote:0}"
t=2001-09-09T02:24:20+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:2bbae9f42e844451189bd76e85eee4150c67fec86afc2532df707ab9a2e02ace Name:TEST-pillar-cool Vote:0}"
t=2001-09-09T02:24:40+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:413bd235e6186445895a2fa2e73e33d4ebd551ca6cfbbaac7541c7007037dd11 Name:TEST-pillar-1 Vote:0}"
t=2001-09-09T02:25:00+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:413bd235e6186445895a2fa2e73e33d4ebd551ca6cfbbaac7541c7007037dd11 Name:TEST-pillar-cool Vote:0}"
t=2001-09-09T02:25:20+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:00810eaa6c0be3fc06235533adc8fd8f809916efa9e13bd7761288cfa77ad7ac Name:TEST-pillar-1 Vote:0}"
t=2001-09-09T02:25:40+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:00810eaa6c0be3fc06235533adc8fd8f809916efa9e13bd7761288cfa77ad7ac Name:TEST-pillar-cool Vote:0}"
t=2001-09-09T02:26:00+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:93c77e702a9da2a83ad3f2b9e662eaccdc20eacce986759b33a2de55ed0a1746 Name:TEST-pillar-1 Vote:0}"
t=2001-09-09T02:26:20+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:93c77e702a9da2a83ad3f2b9e662eaccdc20eacce986759b33a2de55ed0a1746 Name:TEST-pillar-cool Vote:0}"
t=2001-09-09T02:26:40+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:94771e772122504233051b9a11f2049ea3c7566fc3f1a0f36da7743e53c04619 Name:TEST-pillar-1 Vote:0}"
t=2001-09-09T02:27:00+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:94771e772122504233051b9a11f2049ea3c7566fc3f1a0f36da7743e53c04619 Name:TEST-pillar-cool Vote:0}"
t=2001-09-09T02:27:20+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:5068e9bbd0e09abc79a7675bb04cca3ce3de38259218a38ef52f77ad96ba776a Name:TEST-pillar-1 Vote:0}"
t=2001-09-09T02:27:40+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:5068e9bbd0e09abc79a7675bb04cca3ce3de38259218a38ef52f77ad96ba776a Name:TEST-pillar-cool Vote:0}"
t=2001-09-09T02:28:00+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:9ae410b287268f914c99274ef0e90f1765224d6eccfba1b58dcd633fa02b191a Name:TEST-pillar-1 Vote:0}"
t=2001-09-09T02:28:20+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:9ae410b287268f914c99274ef0e90f1765224d6eccfba1b58dcd633fa02b191a Name:TEST-pillar-cool Vote:0}"
t=2001-09-09T02:28:40+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:3b7ee3ecb69f34e59d5c26abe157ed5efaf53e4892e3d87a74719bd195aabedd Name:TEST-pillar-1 Vote:0}"
t=2001-09-09T02:29:00+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:3b7ee3ecb69f34e59d5c26abe157ed5efaf53e4892e3d87a74719bd195aabedd Name:TEST-pillar-cool Vote:0}"
t=2001-09-09T02:29:20+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:2d0c8604e5e0f5bc39db674cbe60d5074b612e2f537bde28c86b02804d60c0d4 Name:TEST-pillar-1 Vote:0}"
t=2001-09-09T02:29:40+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:2d0c8604e5e0f5bc39db674cbe60d5074b612e2f537bde28c86b02804d60c0d4 Name:TEST-pillar-cool Vote:0}"
t=2001-09-09T02:30:00+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:f96de28610e2b3b416f7639de5c71350df110b5df3f968aa36dd5c86a939e69d Name:TEST-pillar-1 Vote:0}"
t=2001-09-09T02:30:20+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:f96de28610e2b3b416f7639de5c71350df110b5df3f968aa36dd5c86a939e69d Name:TEST-pillar-cool Vote:0}"
t=2001-09-09T02:30:40+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:52e284cc2ed9e78bf580b37d986dc6d5bc3a937b0a40e2712f543881c7548513 Name:TEST-pillar-1 Vote:0}"
t=2001-09-09T02:31:00+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:52e284cc2ed9e78bf580b37d986dc6d5bc3a937b0a40e2712f543881c7548513 Name:TEST-pillar-cool Vote:0}"
t=2001-09-09T02:31:20+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:c98e6ca9f0bb8b06a215984855f8b3f1ad7ebe7fcae2300348b668af5a72de22 Name:TEST-pillar-1 Vote:0}"
t=2001-09-09T02:31:40+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:c98e6ca9f0bb8b06a215984855f8b3f1ad7ebe7fcae2300348b668af5a72de22 Name:TEST-pillar-cool Vote:0}"
t=2001-09-09T02:32:00+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:0706d20fc6d6fcaac0b8b82fc8db26d74b1c195ed97c20914759c65c1c75c437 Name:TEST-pillar-1 Vote:0}"
t=2001-09-09T02:32:20+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:0706d20fc6d6fcaac0b8b82fc8db26d74b1c195ed97c20914759c65c1c75c437 Name:TEST-pillar-cool Vote:0}"
t=2001-09-09T02:32:40+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:1eee79ef32aa34b43a88e1ffd6e5ba1694e78bd69b405a9c89e7a12c993f3693 Name:TEST-pillar-1 Vote:0}"
t=2001-09-09T02:33:00+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:1eee79ef32aa34b43a88e1ffd6e5ba1694e78bd69b405a9c89e7a12c993f3693 Name:TEST-pillar-cool Vote:0}"
t=2001-09-09T02:33:20+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:ce131e1aaa32f97e9e37ee6b6c12e51bdc311501141a78e099012f71390603f3 Name:TEST-pillar-1 Vote:0}"
t=2001-09-09T02:33:40+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:ce131e1aaa32f97e9e37ee6b6c12e51bdc311501141a78e099012f71390603f3 Name:TEST-pillar-cool Vote:0}"
t=2001-09-09T02:34:00+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:25648e80063cf96760ad0e1f66da0fb9a5a414be57f7a9aca20a6e756a6e0686 Name:TEST-pillar-1 Vote:0}"
t=2001-09-09T02:34:20+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:25648e80063cf96760ad0e1f66da0fb9a5a414be57f7a9aca20a6e756a6e0686 Name:TEST-pillar-cool Vote:0}"
t=2001-09-09T02:34:40+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:a61aa098c7a4e8883c7186b3b5727b67e65a4a6fb5b5b791d21077b49c34344c Name:TEST-pillar-1 Vote:0}"
t=2001-09-09T02:35:00+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:a61aa098c7a4e8883c7186b3b5727b67e65a4a6fb5b5b791d21077b49c34344c Name:TEST-pillar-cool Vote:0}"
t=2001-09-09T02:35:20+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:44909794893c762f79b8ddc43e2f52f2ec3462d02a4242fa1cade2676a46ea1c Name:TEST-pillar-1 Vote:0}"
t=2001-09-09T02:35:40+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:44909794893c762f79b8ddc43e2f52f2ec3462d02a4242fa1cade2676a46ea1c Name:TEST-pillar-cool Vote:0}"
t=2001-09-09T02:36:00+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:f3b56eab0e0b516660385c4a9731c3e33089ccdda4e493dc1767db98a1e4634e Name:TEST-pillar-1 Vote:0}"
t=2001-09-09T02:36:20+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:f3b56eab0e0b516660385c4a9731c3e33089ccdda4e493dc1767db98a1e4634e Name:TEST-pillar-cool Vote:0}"
t=2001-09-09T02:36:40+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:210f74710e9b98655216e80fd76d8691c824f4d9a8f899151f05e28723d1cfd4 Name:TEST-pillar-1 Vote:0}"
t=2001-09-09T02:37:00+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:210f74710e9b98655216e80fd76d8691c824f4d9a8f899151f05e28723d1cfd4 Name:TEST-pillar-cool Vote:0}"
t=2001-09-09T02:37:20+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:0c05b7490d237b2be5c1d26ad6864bd4508f29cf6332402ca03646992dac3a3f Name:TEST-pillar-1 Vote:0}"
t=2001-09-09T02:37:40+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:0c05b7490d237b2be5c1d26ad6864bd4508f29cf6332402ca03646992dac3a3f Name:TEST-pillar-cool Vote:0}"
t=2001-09-09T02:38:00+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:42253d277232616a64749799b53421de8a0bb6bbf577793a2c58aace77db6813 Name:TEST-pillar-1 Vote:0}"
t=2001-09-09T02:38:20+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:42253d277232616a64749799b53421de8a0bb6bbf577793a2c58aace77db6813 Name:TEST-pillar-cool Vote:0}"
t=2001-09-09T02:38:40+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:01e311586f4639e25e52ffb94113e31f65de9a28612ac8b38170e39f5511727c Name:TEST-pillar-1 Vote:0}"
t=2001-09-09T02:39:00+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:01e311586f4639e25e52ffb94113e31f65de9a28612ac8b38170e39f5511727c Name:TEST-pillar-cool Vote:0}"
t=2001-09-09T02:39:20+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:ecaf6835810af7641900e5ec6831e37a46ad4d4937c807ea11af108701e1629e Name:TEST-pillar-1 Vote:0}"
t=2001-09-09T02:39:40+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:ecaf6835810af7641900e5ec6831e37a46ad4d4937c807ea11af108701e1629e Name:TEST-pillar-cool Vote:0}"
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=0 pillar-name=TEST-pillar-1 reward="&{DelegationReward:+11992073166 BlockReward:+9916666627 TotalReward:+21908739793 ProducedBlockNum:119 ExpectedBlockNum:120 Weight:+2096583333250}" total-weight=2496583333250 self-weight=2096583333250
t=2001-09-09T02:46:40+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=0 pillar-name=TEST-pillar-cool reward="&{DelegationReward:+1153576554 BlockReward:+9999999960 TotalReward:+11153576514 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2496583333250 self-weight=200000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=0 pillar-name=TEST-pillar-znn reward="&{DelegationReward:+1153576554 BlockReward:+9999999960 TotalReward:+11153576514 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2496583333250 self-weight=200000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=pillar epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxsentynelxxxxxxxxxxxxxwy0r2r current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating sentinel reward" module=embedded contract=sentinel epoch=0 total-znn-reward=187200000000 total-qsr-reward=500000000000 cumulated-sentinel=0 start-time=1000000000 end-time=1000003600
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=sentinel epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating stake reward" module=embedded contract=stake epoch=0 total-reward=1000000000000 cumulated-stake=0 start-time=1000000000 end-time=1000003600
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=stake epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating liquidity reward" module=embedded contract=liquidity epoch=0 znn-amount=187200000000 qsr-amount=500000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=liquidity epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxaccelerat0rxxxxxxxxxxp4tk22 current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="check accelerator votes" module=embedded contract=accelerator votes="&{Id:00810eaa6c0be3fc06235533adc8fd8f809916efa9e13bd7761288cfa77ad7ac Total:2 Yes:2 No:0}" status=true
t=2001-09-09T02:46:40+0000 lvl=dbug msg="project passed voting period" module=embedded contract=accelerator project-id=00810eaa6c0be3fc06235533adc8fd8f809916efa9e13bd7761288cfa77ad7ac passed-votes=true
t=2001-09-09T02:46:40+0000 lvl=dbug msg="check accelerator votes" module=embedded contract=accelerator votes="&{Id:01e311586f4639e25e52ffb94113e31f65de9a28612ac8b38170e39f5511727c Total:2 Yes:2 No:0}" status=true
t=2001-09-09T02:46:40+0000 lvl=dbug msg="project passed voting period" module=embedded contract=accelerator project-id=01e311586f4639e25e52ffb94113e31f65de9a28612ac8b38170e39f5511727c passed-votes=true
t=2001-09-09T02:46:40+0000 lvl=dbug msg="check accelerator votes" module=embedded contract=accelerator votes="&{Id:0495aae6660b286625f98ef4997d7be01a96c84f1d481020fd0fc41d6c95d7da Total:2 Yes:2 No:0}" status=true
t=2001-09-09T02:46:40+0000 lvl=dbug msg="project passed voting period" module=embedded contract=accelerator project-id=0495aae6660b286625f98ef4997d7be01a96c84f1d481020fd0fc41d6c95d7da passed-votes=true
t=2001-09-09T02:46:40+0000 lvl=dbug msg="check accelerator votes" module=embedded contract=accelerator votes="&{Id:0654ebf690b4256ba72a086b9c81476ab80e7646a417e02e316f393b872565ae Total:2 Yes:2 No:0}" status=true
t=2001-09-09T02:46:40+0000 lvl=dbug msg="project passed voting period" module=embedded contract=accelerator project-id=0654ebf690b4256ba72a086b9c81476ab80e7646a417e02e316f393b872565ae passed-votes=true
t=2001-09-09T02:46:40+0000 lvl=dbug msg="check accelerator votes" module=embedded contract=accelerator votes="&{Id:0706d20fc6d6fcaac0b8b82fc8db26d74b1c195ed97c20914759c65c1c75c437 Total:2 Yes:2 No:0}" status=true
t=2001-09-09T02:46:40+0000 lvl=dbug msg="project passed voting period" module=embedded contract=accelerator project-id=0706d20fc6d6fcaac0b8b82fc8db26d74b1c195ed97c20914759c65c1c75c437 passed-votes=true
t=2001-09-09T02:46:40+0000 lvl=dbug msg="check accelerator votes" module=embedded contract=accelerator votes="&{Id:0c05b7490d237b2be5c1d26ad6864bd4508f29cf6332402ca03646992dac3a3f Total:2 Yes:2 No:0}" status=true
t=2001-09-09T02:46:40+0000 lvl=dbug msg="project passed voting period" module=embedded contract=accelerator project-id=0c05b7490d237b2be5c1d26ad6864bd4508f29cf6332402ca03646992dac3a3f passed-votes=true
t=2001-09-09T02:46:40+0000 lvl=dbug msg="check accelerator votes" module=embedded contract=accelerator votes="&{Id:1a9a20dc93d06934e49a824018692906fd0cede5e72703b147892154f3c7e8af Total:2 Yes:2 No:0}" status=true
t=2001-09-09T02:46:40+0000 lvl=dbug msg="project passed voting period" module=embedded contract=accelerator project-id=1a9a20dc93d06934e49a824018692906fd0cede5e72703b147892154f3c7e8af passed-votes=true
t=2001-09-09T02:46:40+0000 lvl=dbug msg="check accelerator votes" module=embedded contract=accelerator votes="&{Id:1dc34af881d9f834cd6d85b0bbdc372629951b38a34e780d78885bcf4f36dad6 Total:2 Yes:2 No:0}" status=true
t=2001-09-09T02:46:40+0000 lvl=dbug msg="project passed voting period" module=embedded contract=accelerator project-id=1dc34af881d9f834cd6d85b0bbdc372629951b38a34e780d78885bcf4f36dad6 passed-votes=true
t=2001-09-09T02:46:40+0000 lvl=dbug msg="check accelerator votes" module=embedded contract=accelerator votes="&{Id:1eee79ef32aa34b43a88e1ffd6e5ba1694e78bd69b405a9c89e7a12c993f3693 Total:2 Yes:2 No:0}" status=true
t=2001-09-09T02:46:40+0000 lvl=dbug msg="project passed voting period" module=embedded contract=accelerator project-id=1eee79ef32aa34b43a88e1ffd6e5ba1694e78bd69b405a9c89e7a12c993f3693 passed-votes=true
t=2001-09-09T02:46:40+0000 lvl=dbug msg="check accelerator votes" module=embedded contract=accelerator votes="&{Id:210f74710e9b98655216e80fd76d8691c824f4d9a8f899151f05e28723d1cfd4 Total:2 Yes:2 No:0}" status=true
t=2001-09-09T02:46:40+0000 lvl=dbug msg="project passed voting period" module=embedded contract=accelerator project-id=210f74710e9b98655216e80fd76d8691c824f4d9a8f899151f05e28723d1cfd4 passed-votes=true
t=2001-09-09T02:46:40+0000 lvl=dbug msg="check accelerator votes" module=embedded contract=accelerator votes="&{Id:22b678b98fe293586ed3cb2a4608ea8d7ddf4fddf9fc412ad9b188f1074fa463 Total:2 Yes:2 No:0}" status=true
t=2001-09-09T02:46:40+0000 lvl=dbug msg="project passed voting period" module=embedded contract=accelerator project-id=22b678b98fe293586ed3cb2a4608ea8d7ddf4fddf9fc412ad9b188f1074fa463 passed-votes=true
t=2001-09-09T02:46:40+0000 lvl=dbug msg="check accelerator votes" module=embedded contract=accelerator votes="&{Id:25648e80063cf96760ad0e1f66da0fb9a5a414be57f7a9aca20a6e756a6e0686 Total:2 Yes:2 No:0}" status=true
t=2001-09-09T02:46:40+0000 lvl=dbug msg="project passed voting period" module=embedded contract=accelerator project-id=25648e80063cf96760ad0e1f66da0fb9a5a414be57f7a9aca20a6e756a6e0686 passed-votes=true
t=2001-09-09T02:46:40+0000 lvl=dbug msg="check accelerator votes" module=embedded contract=accelerator votes="&{Id:2a3996acac36858b02fbc7a5e40a1e7975befacd086e6c39a6ef01ec899eb00b Total:2 Yes:2 No:0}" status=true
t=2001-09-09T02:46:40+0000 lvl=dbug msg="project passed voting period" module=embedded contract=accelerator project-id=2a3996acac36858b02fbc7a5e40a1e7975befacd086e6c39a6ef01ec899eb00b passed-votes=true
t=2001-09-09T02:46:40+0000 lvl=dbug msg="check accelerator votes" module=embedded contract=accelerator votes="&{Id:2bbae9f42e844451189bd76e85eee4150c67fec86afc2532df707ab9a2e02ace Total:2 Yes:2 No:0}" status=true
t=2001-09-09T02:46:40+0000 lvl=dbug msg="project passed voting period" module=embedded contract=accelerator project-id=2bbae9f42e844451189bd76e85eee4150c67fec86afc2532df707ab9a2e02ace passed-votes=true
t=2001-09-09T02:46:40+0000 lvl=dbug msg="check accelerator votes" module=embedded contract=accelerator votes="&{Id:2d0c8604e5e0f5bc39db674cbe60d5074b612e2f537bde28c86b02804d60c0d4 Total:2 Yes:2 No:0}" status=true
t=2001-09-09T02:46:40+0000 lvl=dbug msg="project passed voting period" module=embedded contract=accelerator project-id=2d0c8604e5e0f5bc39db674cbe60d5074b612e2f537bde28c86b02804d60c0d4 passed-votes=true
t=2001-09-09T02:46:40+0000 lvl=dbug msg="check accelerator votes" module=embedded contract=accelerator votes="&{Id:3272b8baff99aa4ed434b2c719592fefb03cc18848f55918e970d31a5630b8e9 Total:2 Yes:2 No:0}" status=true
t=2001-09-09T02:46:40+0000 lvl=dbug msg="project passed voting period" module=embedded contract=accelerator project-id=3272b8baff99aa4ed434b2c719592fefb03cc18848f55918e970d31a5630b8e9 passed-votes=true
t=2001-09-09T02:46:40+0000 lvl=dbug msg="check accelerator votes" module=embedded contract=accelerator votes="&{Id:3af378ec6de9728caa5f7c08ea1ddb25f8e44d3801ffd9aeca0de39fbf164215 Total:2 Yes:2 No:0}" status=true
t=2001-09-09T02:46:40+0000 lvl=dbug msg="project passed voting period" module=embedded contract=accelerator project-id=3af378ec6de9728caa5f7c08ea1ddb25f8e44d3801ffd9aeca0de39fbf164215 passed-votes=true
t=2001-09-09T02:46:40+0000 lvl=dbug msg="check accelerator votes" module=embedded contract=accelerator votes="&{Id:3b7ee3ecb69f34e59d5c26abe157ed5efaf53e4892e3d87a74719bd195aabedd Total:2 Yes:2 No:0}" status=true
t=2001-09-09T02:46:40+0000 lvl=dbug msg="project passed voting period" module=embedded contract=accelerator project-id=3b7ee3ecb69f34e59d5c26abe157ed5efaf53e4892e3d87a74719bd195aabedd passed-votes=true
t=2001-09-09T02:46:40+0000 lvl=dbug msg="check accelerator votes" module=embedded contract=accelerator votes="&{Id:413bd235e6186445895a2fa2e73e33d4ebd551ca6cfbbaac7541c7007037dd11 Total:2 Yes:2 No:0}" status=true
t=2001-09-09T02:46:40+0000 lvl=dbug msg="project passed voting period" module=embedded contract=accelerator project-id=413bd235e6186445895a2fa2e73e33d4ebd551ca6cfbbaac7541c7007037dd11 passed-votes=true
t=2001-09-09T02:46:40+0000 lvl=dbug msg="check accelerator votes" module=embedded contract=accelerator votes="&{Id:42253d277232616a64749799b53421de8a0bb6bbf577793a2c58aace77db6813 Total:2 Yes:2 No:0}" status=true
t=2001-09-09T02:46:40+0000 lvl=dbug msg="project passed voting period" module=embedded contract=accelerator project-id=42253d277232616a64749799b53421de8a0bb6bbf577793a2c58aace77db6813 passed-votes=true
t=2001-09-09T02:46:40+0000 lvl=dbug msg="check accelerator votes" module=embedded contract=accelerator votes="&{Id:44909794893c762f79b8ddc43e2f52f2ec3462d02a4242fa1cade2676a46ea1c Total:2 Yes:2 No:0}" status=true
t=2001-09-09T02:46:40+0000 lvl=dbug msg="project passed voting period" module=embedded contract=accelerator project-id=44909794893c762f79b8ddc43e2f52f2ec3462d02a4242fa1cade2676a46ea1c passed-votes=true
t=2001-09-09T02:46:40+0000 lvl=dbug msg="check accelerator votes" module=embedded contract=accelerator votes="&{Id:4bd532b48d650af895d7feb6bb00b9e33d8e92984f7df122e67d7c8338d6f0ef Total:2 Yes:2 No:0}" status=true
t=2001-09-09T02:46:40+0000 lvl=dbug msg="project passed voting period" module=embedded contract=accelerator project-id=4bd532b48d650af895d7feb6bb00b9e33d8e92984f7df122e67d7c8338d6f0ef passed-votes=true
t=2001-09-09T02:46:40+0000 lvl=dbug msg="check accelerator votes" module=embedded contract=accelerator votes="&{Id:5068e9bbd0e09abc79a7675bb04cca3ce3de38259218a38ef52f77ad96ba776a Total:2 Yes:2 No:0}" status=true
t=2001-09-09T02:46:40+0000 lvl=dbug msg="project passed voting period" module=embedded contract=accelerator project-id=5068e9bbd0e09abc79a7675bb04cca3ce3de38259218a38ef52f77ad96ba776a passed-votes=true
t=2001-09-09T02:46:40+0000 lvl=dbug msg="check accelerator votes" module=embedded contract=accelerator votes="&{Id:52e284cc2ed9e78bf580b37d986dc6d5bc3a937b0a40e2712f543881c7548513 Total:2 Yes:2 No:0}" status=true
t=2001-09-09T02:46:40+0000 lvl=dbug msg="project passed voting period" module=embedded contract=accelerator project-id=52e284cc2ed9e78bf580b37d986dc6d5bc3a937b0a40e2712f543881c7548513 passed-votes=true
t=2001-09-09T02:46:40+0000 lvl=dbug msg="check accelerator votes" module=embedded contract=accelerator votes="&{Id:6657eeac715335626bf7f4d223bd79a988dad9456d58b39ededd84da61cd84be Total:2 Yes:2 No:0}" status=true
t=2001-09-09T02:46:40+0000 lvl=dbug msg="project passed voting period" module=embedded contract=accelerator project-id=6657eeac715335626bf7f4d223bd79a988dad9456d58b39ededd84da61cd84be passed-votes=true
t=2001-09-09T02:46:40+0000 lvl=dbug msg="check accelerator votes" module=embedded contract=accelerator votes="&{Id:7d13ab27040d4f7063e3bf7c31235296b3ee11ffcb62473131e05af4b17e195f Total:2 Yes:2 No:0}" status=true
t=2001-09-09T02:46:40+0000 lvl=dbug msg="project passed voting period" module=embedded contract=accelerator project-id=7d13ab27040d4f7063e3bf7c31235296b3ee11ffcb62473131e05af4b17e195f passed-votes=true
t=2001-09-09T02:46:40+0000 lvl=dbug msg="check accelerator votes" module=embedded contract=accelerator votes="&{Id:82ff45e10718519017cd204bfc3e0afef917600f15ef3159c334ec32338cf61b Total:2 Yes:2 No:0}" status=true
t=2001-09-09T02:46:40+0000 lvl=dbug msg="project passed voting period" module=embedded contract=accelerator project-id=82ff45e10718519017cd204bfc3e0afef917600f15ef3159c334ec32338cf61b passed-votes=true
t=2001-09-09T02:46:40+0000 lvl=dbug msg="check accelerator votes" module=embedded contract=accelerator votes="&{Id:8fb32770a4b52898c5abcd054985f0d2776884759606231436ff5ae59914129b Total:2 Yes:2 No:0}" status=true
t=2001-09-09T02:46:40+0000 lvl=dbug msg="project passed voting period" module=embedded contract=accelerator project-id=8fb32770a4b52898c5abcd054985f0d2776884759606231436ff5ae59914129b passed-votes=true
t=2001-09-09T02:46:40+0000 lvl=dbug msg="check accelerator votes" module=embedded contract=accelerator votes="&{Id:93c77e702a9da2a83ad3f2b9e662eaccdc20eacce986759b33a2de55ed0a1746 Total:2 Yes:2 No:0}" status=true
t=2001-09-09T02:46:40+0000 lvl=dbug msg="project passed voting period" module=embedded contract=accelerator project-id=93c77e702a9da2a83ad3f2b9e662eaccdc20eacce986759b33a2de55ed0a1746 passed-votes=true
t=2001-09-09T02:46:40+0000 lvl=dbug msg="check accelerator votes" module=embedded contract=accelerator votes="&{Id:94771e772122504233051b9a11f2049ea3c7566fc3f1a0f36da7743e53c04619 Total:2 Yes:2 No:0}" status=true
t=2001-09-09T02:46:40+0000 lvl=dbug msg="project passed voting period" module=embedded contract=accelerator project-id=94771e772122504233051b9a11f2049ea3c7566fc3f1a0f36da7743e53c04619 passed-votes=true
t=2001-09-09T02:46:40+0000 lvl=dbug msg="check accelerator votes" module=embedded contract=accelerator votes="&{Id:99308b7f33dbe0bccdb58b5e16c536c9abf9b86d48b0aa8ab461363fe9953064 Total:2 Yes:2 No:0}" status=true
t=2001-09-09T02:46:40+0000 lvl=dbug msg="project passed voting period" module=embedded contract=accelerator project-id=99308b7f33dbe0bccdb58b5e16c536c9abf9b86d48b0aa8ab461363fe9953064 passed-votes=true
t=2001-09-09T02:46:40+0000 lvl=dbug msg="check accelerator votes" module=embedded contract=accelerator votes="&{Id:9aa41e368f43725a18ad591f37698cd6b0409f55cb3ee2e718df59a25df4f011 Total:2 Yes:2 No:0}" status=true
t=2001-09-09T02:46:40+0000 lvl=dbug msg="project passed voting period" module=embedded contract=accelerator project-id=9aa41e368f43725a18ad591f37698cd6b0409f55cb3ee2e718df59a25df4f011 passed-votes=true
t=2001-09-09T02:46:40+0000 lvl=dbug msg="check accelerator votes" module=embedded contract=accelerator votes="&{Id:9ae410b287268f914c99274ef0e90f1765224d6eccfba1b58dcd633fa02b191a Total:2 Yes:2 No:0}" status=true
t=2001-09-09T02:46:40+0000 lvl=dbug msg="project passed voting period" module=embedded contract=accelerator project-id=9ae410b287268f914c99274ef0e90f1765224d6eccfba1b58dcd633fa02b191a passed-votes=true
t=2001-09-09T02:46:40+0000 lvl=dbug msg="check accelerator votes" module=embedded contract=accelerator votes="&{Id:9bcd977b6b114075a2464271bffebcf8e9c553e91e4d612b7461b9c84bd43e53 Total:2 Yes:2 No:0}" status=true
t=2001-09-09T02:46:40+0000 lvl=dbug msg="project passed voting period" module=embedded contract=accelerator project-id=9bcd977b6b114075a2464271bffebcf8e9c553e91e4d612b7461b9c84bd43e53 passed-votes=true
t=2001-09-09T02:46:40+0000 lvl=dbug msg="check accelerator votes" module=embedded contract=accelerator votes="&{Id:9dc277609817c1e220677cb8e42ea73d63ec43b034815500a9306e7c43ca73bc Total:2 Yes:2 No:0}" status=true
t=2001-09-09T02:46:40+0000 lvl=dbug msg="project passed voting period" module=embedded contract=accelerator project-id=9dc277609817c1e220677cb8e42ea73d63ec43b034815500a9306e7c43ca73bc passed-votes=true
t=2001-09-09T02:46:40+0000 lvl=dbug msg="check accelerator votes" module=embedded contract=accelerator votes="&{Id:a4e9307b13dac391e0772f1a4b7f153786f233d68fa3f048100304feb285180a Total:2 Yes:2 No:0}" status=true
t=2001-09-09T02:46:40+0000 lvl=dbug msg="project passed voting period" module=embedded contract=accelerator project-id=a4e9307b13dac391e0772f1a4b7f153786f233d68fa3f048100304feb285180a passed-votes=true
t=2001-09-09T02:46:40+0000 lvl=dbug msg="check accelerator votes" module=embedded contract=accelerator votes="&{Id:a61aa098c7a4e8883c7186b3b5727b67e65a4a6fb5b5b791d21077b49c34344c Total:2 Yes:2 No:0}" status=true
t=2001-09-09T02:46:40+0000 lvl=dbug msg="project passed voting period" module=embedded contract=accelerator project-id=a61aa098c7a4e8883c7186b3b5727b67e65a4a6fb5b5b791d21077b49c34344c passed-votes=true
t=2001-09-09T02:46:40+0000 lvl=dbug msg="check accelerator votes" module=embedded contract=accelerator votes="&{Id:bce40860a01f8d231f17c950e0e8f41d3cc68d2e6d22a82cd0598910000a8aac Total:2 Yes:2 No:0}" status=true
t=2001-09-09T02:46:40+0000 lvl=dbug msg="project passed voting period" module=embedded contract=accelerator project-id=bce40860a01f8d231f17c950e0e8f41d3cc68d2e6d22a82cd0598910000a8aac passed-votes=true
t=2001-09-09T02:46:40+0000 lvl=dbug msg="check accelerator votes" module=embedded contract=accelerator votes="&{Id:c05df0ea4c73d268aab3ae95ea9b9394a2601a5e62cfb07e773c89c61ecc8274 Total:2 Yes:2 No:0}" status=true
t=2001-09-09T02:46:40+0000 lvl=dbug msg="project passed voting period" module=embedded contract=accelerator project-id=c05df0ea4c73d268aab3ae95ea9b9394a2601a5e62cfb07e773c89c61ecc8274 passed-votes=true
t=2001-09-09T02:46:40+0000 lvl=dbug msg="check accelerator votes" module=embedded contract=accelerator votes="&{Id:c98e6ca9f0bb8b06a215984855f8b3f1ad7ebe7fcae2300348b668af5a72de22 Total:2 Yes:2 No:0}" status=true
t=2001-09-09T02:46:40+0000 lvl=dbug msg="project passed voting period" module=embedded contract=accelerator project-id=c98e6ca9f0bb8b06a215984855f8b3f1ad7ebe7fcae2300348b668af5a72de22 passed-votes=true
t=2001-09-09T02:46:40+0000 lvl=dbug msg="check accelerator votes" module=embedded contract=accelerator votes="&{Id:cacaa36ee1dc2306c42b5f6f452cbc6ee2c179e064077463ffe701a167ad08e8 Total:2 Yes:2 No:0}" status=true
t=2001-09-09T02:46:40+0000 lvl=dbug msg="project passed voting period" module=embedded contract=accelerator project-id=cacaa36ee1dc2306c42b5f6f452cbc6ee2c179e064077463ffe701a167ad08e8 passed-votes=true
t=2001-09-09T02:46:40+0000 lvl=dbug msg="check accelerator votes" module=embedded contract=accelerator votes="&{Id:ce131e1aaa32f97e9e37ee6b6c12e51bdc311501141a78e099012f71390603f3 Total:2 Yes:2 No:0}" status=true
t=2001-09-09T02:46:40+0000 lvl=dbug msg="project passed voting period" module=embedded contract=accelerator project-id=ce131e1aaa32f97e9e37ee6b6c12e51bdc311501141a78e099012f71390603f3 passed-votes=true
t=2001-09-09T02:46:40+0000 lvl=dbug msg="check accelerator votes" module=embedded contract=accelerator votes="&{Id:cee73558c4cf07b6f233932f09c2ebfba6375dd3eaad46e4cc4bc1abc869e754 Total:2 Yes:2 No:0}" status=true
t=2001-09-09T02:46:40+0000 lvl=dbug msg="project passed voting period" module=embedded contract=accelerator project-id=cee73558c4cf07b6f233932f09c2ebfba6375dd3eaad46e4cc4bc1abc869e754 passed-votes=true
t=2001-09-09T02:46:40+0000 lvl=dbug msg="check accelerator votes" module=embedded contract=accelerator votes="&{Id:d7ed623d4204c4b6b96672120349d60c8c8b8aeefb134967e02b95a29fddef70 Total:2 Yes:2 No:0}" status=true
t=2001-09-09T02:46:40+0000 lvl=dbug msg="project passed voting period" module=embedded contract=accelerator project-id=d7ed623d4204c4b6b96672120349d60c8c8b8aeefb134967e02b95a29fddef70 passed-votes=true
t=2001-09-09T02:46:40+0000 lvl=dbug msg="check accelerator votes" module=embedded contract=accelerator votes="&{Id:e0342441bb2213205f6fc84172a960c68dc2eff567000978407de6d056a59775 Total:2 Yes:2 No:0}" status=true
t=2001-09-09T02:46:40+0000 lvl=dbug msg="project passed voting period" module=embedded contract=accelerator project-id=e0342441bb2213205f6fc84172a960c68dc2eff567000978407de6d056a59775 passed-votes=true
t=2001-09-09T02:46:40+0000 lvl=dbug msg="check accelerator votes" module=embedded contract=accelerator votes="&{Id:ecaf6835810af7641900e5ec6831e37a46ad4d4937c807ea11af108701e1629e Total:2 Yes:2 No:0}" status=true
t=2001-09-09T02:46:40+0000 lvl=dbug msg="project passed voting period" module=embedded contract=accelerator project-id=ecaf6835810af7641900e5ec6831e37a46ad4d4937c807ea11af108701e1629e passed-votes=true
t=2001-09-09T02:46:40+0000 lvl=dbug msg="check accelerator votes" module=embedded contract=accelerator votes="&{Id:f2fb30731ae535c3d275d4db489bf8aaa7f0891a94b4dea7b641197352bfccb7 Total:2 Yes:2 No:0}" status=true
t=2001-09-09T02:46:40+0000 lvl=dbug msg="project passed voting period" module=embedded contract=accelerator project-id=f2fb30731ae535c3d275d4db489bf8aaa7f0891a94b4dea7b641197352bfccb7 passed-votes=true
t=2001-09-09T02:46:40+0000 lvl=dbug msg="check accelerator votes" module=embedded contract=accelerator votes="&{Id:f3b56eab0e0b516660385c4a9731c3e33089ccdda4e493dc1767db98a1e4634e Total:2 Yes:2 No:0}" status=true
t=2001-09-09T02:46:40+0000 lvl=dbug msg="project passed voting period" module=embedded contract=accelerator project-id=f3b56eab0e0b516660385c4a9731c3e33089ccdda4e493dc1767db98a1e4634e passed-votes=true
t=2001-09-09T02:46:40+0000 lvl=dbug msg="check accelerator votes" module=embedded contract=accelerator votes="&{Id:f94ef63940a2e93570fa423db18a74bf93f58157ca8134662187a00074a2080c Total:2 Yes:2 No:0}" status=true
t=2001-09-09T02:46:40+0000 lvl=dbug msg="project passed voting period" module=embedded contract=accelerator project-id=f94ef63940a2e93570fa423db18a74bf93f58157ca8134662187a00074a2080c passed-votes=true
t=2001-09-09T02:46:40+0000 lvl=dbug msg="check accelerator votes" module=embedded contract=accelerator votes="&{Id:f96de28610e2b3b416f7639de5c71350df110b5df3f968aa36dd5c86a939e69d Total:2 Yes:2 No:0}" status=true
t=2001-09-09T02:46:40+0000 lvl=dbug msg="project passed voting period" module=embedded contract=accelerator project-id=f96de28610e2b3b416f7639de5c71350df110b5df3f968aa36dd5c86a939e69d passed-votes=true
t=2001-09-09T02:46:50+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg TokenName:Zenon Coin TokenSymbol:ZNN TokenDomain:zenon.network TotalSupply:+19687200000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1znnxxxxxxxxxxxxx9z4ulx}" minted-amount=187200000000 to-address=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae
t=2001-09-09T02:46:50+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 TokenName:QuasarCoin TokenSymbol:QSR TokenDomain:zenon.network TotalSupply:+181050000000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1qsrxxxxxxxxxxxxxmrhjll}" minted-amount=500000000000 to-address=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae
t=2001-09-09T02:47:00+0000 lvl=info msg="received donation" module=embedded contract=common embedded=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae from-address=z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0 zts=zts1znnxxxxxxxxxxxxx9z4ulx amount=187200000000
t=2001-09-09T02:47:00+0000 lvl=dbug msg="successfully created phase" module=embedded contract=accelerator phase="&{Id:7a068bac975c92d7e3935c8ecd9f5c6fb8f702a9e81b1fc2fa786942d2815738 ProjectId:3272b8baff99aa4ed434b2c719592fefb03cc18848f55918e970d31a5630b8e9 Name:Phase 1 Description:Description for phase 1 Url:www.phase1.com ZnnFundsNeeded:+1 QsrFundsNeeded:+1 CreationTimestamp:1000003620 AcceptedTimestamp:0 Status:0}"
t=2001-09-09T02:47:00+0000 lvl=info msg="received donation" module=embedded contract=common embedded=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae from-address=z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0 zts=zts1qsrxxxxxxxxxxxxxmrhjll amount=500000000000
t=2001-09-09T02:47:20+0000 lvl=dbug msg="successfully created phase" module=embedded contract=accelerator phase="&{Id:1540451bba833be4ac14ec4cb5f172464b4955ff57dd9f4c2745a3a07463ae50 ProjectId:7d13ab27040d4f7063e3bf7c31235296b3ee11ffcb62473131e05af4b17e195f Name:Phase 1 Description:Description for phase 1 Url:www.phase1.com ZnnFundsNeeded:+1 QsrFundsNeeded:+1 CreationTimestamp:1000003640 AcceptedTimestamp:0 Status:0}"
t=2001-09-09T02:47:40+0000 lvl=dbug msg="successfully created phase" module=embedded contract=accelerator phase="&{Id:0fed38b5cb19671a91cb09db4442c02e254fbe7087b2bf39b2da84449af372e5 ProjectId:0495aae6660b286625f98ef4997d7be01a96c84f1d481020fd0fc41d6c95d7da Name:Phase 1 Description:Description for phase 1 Url:www.phase1.com ZnnFundsNeeded:+1 QsrFundsNeeded:+1 CreationTimestamp:1000003660 AcceptedTimestamp:0 Status:0}"
t=2001-09-09T02:48:00+0000 lvl=dbug msg="successfully created phase" module=embedded contract=accelerator phase="&{Id:2295f48f6980bc30c2b09ea04570e70850e85c02c5d97bcb78ee3971a31484e2 ProjectId:8fb32770a4b52898c5abcd054985f0d2776884759606231436ff5ae59914129b Name:Phase 1 Description:Description for phase 1 Url:www.phase1.com ZnnFundsNeeded:+1 QsrFundsNeeded:+1 CreationTimestamp:1000003680 AcceptedTimestamp:0 Status:0}"
t=2001-09-09T02:48:20+0000 lvl=dbug msg="successfully created phase" module=embedded contract=accelerator phase="&{Id:eff87cfde9af54d4d5995e02d2eba413b0a89fd919ac941f932ad3b88e22ba43 ProjectId:d7ed623d4204c4b6b96672120349d60c8c8b8aeefb134967e02b95a29fddef70 Name:Phase 1 Description:Description for phase 1 Url:www.phase1.com ZnnFundsNeeded:+1 QsrFundsNeeded:+1 CreationTimestamp:1000003700 AcceptedTimestamp:0 Status:0}"
t=2001-09-09T02:48:40+0000 lvl=dbug msg="successfully created phase" module=embedded contract=accelerator phase="&{Id:7a26c861b7d019ebd72a82ac854281f33d54832dab8cc3b7b4d34a61d277272d ProjectId:a4e9307b13dac391e0772f1a4b7f153786f233d68fa3f048100304feb285180a Name:Phase 1 Description:Description for phase 1 Url:www.phase1.com ZnnFundsNeeded:+1 QsrFundsNeeded:+1 CreationTimestamp:1000003720 AcceptedTimestamp:0 Status:0}"
t=2001-09-09T02:49:00+0000 lvl=dbug msg="successfully created phase" module=embedded contract=accelerator phase="&{Id:2aad8476df0f80068aaa0fbe24fe455210886f6c77547645f45121c4705da9ec ProjectId:9dc277609817c1e220677cb8e42ea73d63ec43b034815500a9306e7c43ca73bc Name:Phase 1 Description:Description for phase 1 Url:www.phase1.com ZnnFundsNeeded:+1 QsrFundsNeeded:+1 CreationTimestamp:1000003740 AcceptedTimestamp:0 Status:0}"
t=2001-09-09T02:49:20+0000 lvl=dbug msg="successfully created phase" module=embedded contract=accelerator phase="&{Id:d2d312bf497a9e4d0e0b31e52188e269583ca362013646f4bedfdb5255c6d541 ProjectId:e0342441bb2213205f6fc84172a960c68dc2eff567000978407de6d056a59775 Name:Phase 1 Description:Description for phase 1 Url:www.phase1.com ZnnFundsNeeded:+1 QsrFundsNeeded:+1 CreationTimestamp:1000003760 AcceptedTimestamp:0 Status:0}"
t=2001-09-09T02:49:40+0000 lvl=dbug msg="successfully created phase" module=embedded contract=accelerator phase="&{Id:991dfcf9be207afd7eea146ec3538840559c5e9515ae396866367e58dbd5eec8 ProjectId:22b678b98fe293586ed3cb2a4608ea8d7ddf4fddf9fc412ad9b188f1074fa463 Name:Phase 1 Description:Description for phase 1 Url:www.phase1.com ZnnFundsNeeded:+1 QsrFundsNeeded:+1 CreationTimestamp:1000003780 AcceptedTimestamp:0 Status:0}"
t=2001-09-09T02:50:00+0000 lvl=dbug msg="successfully created phase" module=embedded contract=accelerator phase="&{Id:275f89121058acdb494d67abc4cb967da7875d454f6089da850445d403489b3f ProjectId:6657eeac715335626bf7f4d223bd79a988dad9456d58b39ededd84da61cd84be Name:Phase 1 Description:Description for phase 1 Url:www.phase1.com ZnnFundsNeeded:+1 QsrFundsNeeded:+1 CreationTimestamp:1000003800 AcceptedTimestamp:0 Status:0}"
t=2001-09-09T02:50:20+0000 lvl=dbug msg="successfully created phase" module=embedded contract=accelerator phase="&{Id:fb2bab9fd2687857c3ab1f1d204df6c79a33c7b068b8acdc9411289533e95930 ProjectId:f94ef63940a2e93570fa423db18a74bf93f58157ca8134662187a00074a2080c Name:Phase 1 Description:Description for phase 1 Url:www.phase1.com ZnnFundsNeeded:+1 QsrFundsNeeded:+1 CreationTimestamp:1000003820 AcceptedTimestamp:0 Status:0}"
t=2001-09-09T02:50:40+0000 lvl=dbug msg="successfully created phase" module=embedded contract=accelerator phase="&{Id:a4e92fce4b86f90cfc8058cc5696cb5c875dca8ad9a7d098c1db0cd03decfafd ProjectId:f2fb30731ae535c3d275d4db489bf8aaa7f0891a94b4dea7b641197352bfccb7 Name:Phase 1 Description:Description for phase 1 Url:www.phase1.com ZnnFundsNeeded:+1 QsrFundsNeeded:+1 CreationTimestamp:1000003840 AcceptedTimestamp:0 Status:0}"
t=2001-09-09T02:51:00+0000 lvl=dbug msg="successfully created phase" module=embedded contract=accelerator phase="&{Id:232dd7ca7f907976d61b8909a331e946b20e91b2f19df25cea4c312a2ec64039 ProjectId:9aa41e368f43725a18ad591f37698cd6b0409f55cb3ee2e718df59a25df4f011 Name:Phase 1 Description:Description for phase 1 Url:www.phase1.com ZnnFundsNeeded:+1 QsrFundsNeeded:+1 CreationTimestamp:1000003860 AcceptedTimestamp:0 Status:0}"
t=2001-09-09T02:51:20+0000 lvl=dbug msg="successfully created phase" module=embedded contract=accelerator phase="&{Id:c63cb9da0a7c27095b88463dd0586d95d0d9ac57b4d3d0c3032676bba871cea0 ProjectId:99308b7f33dbe0bccdb58b5e16c536c9abf9b86d48b0aa8ab461363fe9953064 Name:Phase 1 Description:Description for phase 1 Url:www.phase1.com ZnnFundsNeeded:+1 QsrFundsNeeded:+1 CreationTimestamp:1000003880 AcceptedTimestamp:0 Status:0}"
t=2001-09-09T02:51:40+0000 lvl=dbug msg="successfully created phase" module=embedded contract=accelerator phase="&{Id:a468c1c7fb00b79148e0dcce637e29f2030ec1585e03b4b8e56d8732c4f24115 ProjectId:c05df0ea4c73d268aab3ae95ea9b9394a2601a5e62cfb07e773c89c61ecc8274 Name:Phase 1 Description:Description for phase 1 Url:www.phase1.com ZnnFundsNeeded:+1 QsrFundsNeeded:+1 CreationTimestamp:1000003900 AcceptedTimestamp:0 Status:0}"
t=2001-09-09T02:52:00+0000 lvl=dbug msg="successfully created phase" module=embedded contract=accelerator phase="&{Id:8b7740e42c475e313771196b33af74d8956a93995bc3677d90554c64e98b01ed ProjectId:1a9a20dc93d06934e49a824018692906fd0cede5e72703b147892154f3c7e8af Name:Phase 1 Description:Description for phase 1 Url:www.phase1.com ZnnFundsNeeded:+1 QsrFundsNeeded:+1 CreationTimestamp:1000003920 AcceptedTimestamp:0 Status:0}"
t=2001-09-09T02:52:20+0000 lvl=dbug msg="successfully created phase" module=embedded contract=accelerator phase="&{Id:83ed3fc32772c9a6820d2153c142505695fb6c7dc969443d7bc33c8d835246a3 ProjectId:4bd532b48d650af895d7feb6bb00b9e33d8e92984f7df122e67d7c8338d6f0ef Name:Phase 1 Description:Description for phase 1 Url:www.phase1.com ZnnFundsNeeded:+1 QsrFundsNeeded:+1 CreationTimestamp:1000003940 AcceptedTimestamp:0 Status:0}"
t=2001-09-09T02:52:40+0000 lvl=dbug msg="successfully created phase" module=embedded contract=accelerator phase="&{Id:2e59f568cc9974dd63dff71282b74f92e8e4b7d47d7a16b947e1e56fb42cd73d ProjectId:cee73558c4cf07b6f233932f09c2ebfba6375dd3eaad46e4cc4bc1abc869e754 Name:Phase 1 Description:Description for phase 1 Url:www.phase1.com ZnnFundsNeeded:+1 QsrFundsNeeded:+1 CreationTimestamp:1000003960 AcceptedTimestamp:0 Status:0}"
t=2001-09-09T02:53:00+0000 lvl=dbug msg="successfully created phase" module=embedded contract=accelerator phase="&{Id:6bcc251da9e3886eb20098f641df8681a88655f46b2c7c851eed188217ea4dc5 ProjectId:9bcd977b6b114075a2464271bffebcf8e9c553e91e4d612b7461b9c84bd43e53 Name:Phase 1 Description:Description for phase 1 Url:www.phase1.com ZnnFundsNeeded:+1 QsrFundsNeeded:+1 CreationTimestamp:1000003980 AcceptedTimestamp:0 Status:0}"
t=2001-09-09T02:53:20+0000 lvl=dbug msg="successfully created phase" module=embedded contract=accelerator phase="&{Id:2be4082317cbdbc81a10bd0e40f3b6ba6e51693418ba110189a9595cbd21da27 ProjectId:cacaa36ee1dc2306c42b5f6f452cbc6ee2c179e064077463ffe701a167ad08e8 Name:Phase 1 Description:Description for phase 1 Url:www.phase1.com ZnnFundsNeeded:+1 QsrFundsNeeded:+1 CreationTimestamp:1000004000 AcceptedTimestamp:0 Status:0}"
t=2001-09-09T02:53:40+0000 lvl=dbug msg="successfully created phase" module=embedded contract=accelerator phase="&{Id:cf734e4813171de96ef3b78aec3625b53957500e0bd3ce1d4b89c1a1ffe6e61b ProjectId:82ff45e10718519017cd204bfc3e0afef917600f15ef3159c334ec32338cf61b Name:Phase 1 Description:Description for phase 1 Url:www.phase1.com ZnnFundsNeeded:+1 QsrFundsNeeded:+1 CreationTimestamp:1000004020 AcceptedTimestamp:0 Status:0}"
t=2001-09-09T02:54:00+0000 lvl=dbug msg="successfully created phase" module=embedded contract=accelerator phase="&{Id:f43c74177f631fbacc7c7ed077c01ea0682d62641e02730fcfea1c92b2ef3c18 ProjectId:2a3996acac36858b02fbc7a5e40a1e7975befacd086e6c39a6ef01ec899eb00b Name:Phase 1 Description:Description for phase 1 Url:www.phase1.com ZnnFundsNeeded:+1 QsrFundsNeeded:+1 CreationTimestamp:1000004040 AcceptedTimestamp:0 Status:0}"
t=2001-09-09T02:54:20+0000 lvl=dbug msg="successfully created phase" module=embedded contract=accelerator phase="&{Id:04c64e4998805f28c4b8103da6b656ea1e49e3fb13b353ba1a20c56c93501b53 ProjectId:0654ebf690b4256ba72a086b9c81476ab80e7646a417e02e316f393b872565ae Name:Phase 1 Description:Description for phase 1 Url:www.phase1.com ZnnFundsNeeded:+1 QsrFundsNeeded:+1 CreationTimestamp:1000004060 AcceptedTimestamp:0 Status:0}"
t=2001-09-09T02:54:40+0000 lvl=dbug msg="successfully created phase" module=embedded contract=accelerator phase="&{Id:9346f501d826109e9b2e62b5b54be37b6cfe6c9300396a6182b12513f581b48d ProjectId:bce40860a01f8d231f17c950e0e8f41d3cc68d2e6d22a82cd0598910000a8aac Name:Phase 1 Description:Description for phase 1 Url:www.phase1.com ZnnFundsNeeded:+1 QsrFundsNeeded:+1 CreationTimestamp:1000004080 AcceptedTimestamp:0 Status:0}"
t=2001-09-09T02:55:00+0000 lvl=dbug msg="successfully created phase" module=embedded contract=accelerator phase="&{Id:74ba31ab7cbd35dc81ec62b30dc8b4f6712d007027ebafa1befc1c17e86328e9 ProjectId:1dc34af881d9f834cd6d85b0bbdc372629951b38a34e780d78885bcf4f36dad6 Name:Phase 1 Description:Description for phase 1 Url:www.phase1.com ZnnFundsNeeded:+1 QsrFundsNeeded:+1 CreationTimestamp:1000004100 AcceptedTimestamp:0 Status:0}"
t=2001-09-09T02:55:20+0000 lvl=dbug msg="successfully created phase" module=embedded contract=accelerator phase="&{Id:aa551bc54e8af1569cba5453dd66a491bd970224f21dc6f6477060a08f817e31 ProjectId:3af378ec6de9728caa5f7c08ea1ddb25f8e44d3801ffd9aeca0de39fbf164215 Name:Phase 1 Description:Description for phase 1 Url:www.phase1.com ZnnFundsNeeded:+1 QsrFundsNeeded:+1 CreationTimestamp:1000004120 AcceptedTimestamp:0 Status:0}"
t=2001-09-09T02:55:40+0000 lvl=dbug msg="successfully created phase" module=embedded contract=accelerator phase="&{Id:5bf70de26405124ed0a765febbe8cc6f737920bbcd0e7bb046207b774de5079a ProjectId:2bbae9f42e844451189bd76e85eee4150c67fec86afc2532df707ab9a2e02ace Name:Phase 1 Description:Description for phase 1 Url:www.phase1.com ZnnFundsNeeded:+1 QsrFundsNeeded:+1 CreationTimestamp:1000004140 AcceptedTimestamp:0 Status:0}"
t=2001-09-09T02:56:00+0000 lvl=dbug msg="successfully created phase" module=embedded contract=accelerator phase="&{Id:c2215028df4d5b7f7ed8da993fcb691441055272e5e524fbdfa9105ad86f94ed ProjectId:413bd235e6186445895a2fa2e73e33d4ebd551ca6cfbbaac7541c7007037dd11 Name:Phase 1 Description:Description for phase 1 Url:www.phase1.com ZnnFundsNeeded:+1 QsrFundsNeeded:+1 CreationTimestamp:1000004160 AcceptedTimestamp:0 Status:0}"
t=2001-09-09T02:56:20+0000 lvl=dbug msg="successfully created phase" module=embedded contract=accelerator phase="&{Id:29634f24be52348c5264d6d9be375672a29bcc2a0d32862ff9bfda1fe62db9c1 ProjectId:00810eaa6c0be3fc06235533adc8fd8f809916efa9e13bd7761288cfa77ad7ac Name:Phase 1 Description:Description for phase 1 Url:www.phase1.com ZnnFundsNeeded:+1 QsrFundsNeeded:+1 CreationTimestamp:1000004180 AcceptedTimestamp:0 Status:0}"
t=2001-09-09T02:56:40+0000 lvl=dbug msg="successfully created phase" module=embedded contract=accelerator phase="&{Id:35fc522da6315ade76c61e3491fc3c8997ab902ef98f766e504b4b2a721a7162 ProjectId:93c77e702a9da2a83ad3f2b9e662eaccdc20eacce986759b33a2de55ed0a1746 Name:Phase 1 Description:Description for phase 1 Url:www.phase1.com ZnnFundsNeeded:+1 QsrFundsNeeded:+1 CreationTimestamp:1000004200 AcceptedTimestamp:0 Status:0}"
t=2001-09-09T02:57:00+0000 lvl=dbug msg="successfully created phase" module=embedded contract=accelerator phase="&{Id:f2be1901f74f601f79ff9ed1a9b423e79671ad2a28b26e52cc6b0fd6760d3206 ProjectId:94771e772122504233051b9a11f2049ea3c7566fc3f1a0f36da7743e53c04619 Name:Phase 1 Description:Description for phase 1 Url:www.phase1.com ZnnFundsNeeded:+1 QsrFundsNeeded:+1 CreationTimestamp:1000004220 AcceptedTimestamp:0 Status:0}"
t=2001-09-09T02:57:20+0000 lvl=dbug msg="successfully created phase" module=embedded contract=accelerator phase="&{Id:6e39d316d683b1516221527569e8bc918ebf8ba3fd0f7f5aa50636eac00576ed ProjectId:5068e9bbd0e09abc79a7675bb04cca3ce3de38259218a38ef52f77ad96ba776a Name:Phase 1 Description:Description for phase 1 Url:www.phase1.com ZnnFundsNeeded:+1 QsrFundsNeeded:+1 CreationTimestamp:1000004240 AcceptedTimestamp:0 Status:0}"
t=2001-09-09T02:57:40+0000 lvl=dbug msg="successfully created phase" module=embedded contract=accelerator phase="&{Id:51f6da3180b6bb5f94868d3c7ac93eb55281ece96e0a4927aee0d462278c0eab ProjectId:9ae410b287268f914c99274ef0e90f1765224d6eccfba1b58dcd633fa02b191a Name:Phase 1 Description:Description for phase 1 Url:www.phase1.com ZnnFundsNeeded:+1 QsrFundsNeeded:+1 CreationTimestamp:1000004260 AcceptedTimestamp:0 Status:0}"
t=2001-09-09T02:58:00+0000 lvl=dbug msg="successfully created phase" module=embedded contract=accelerator phase="&{Id:f4e39d4d61dba473c32a0b8e739ccb69477d6a941d8f36354a69a65e92e40820 ProjectId:3b7ee3ecb69f34e59d5c26abe157ed5efaf53e4892e3d87a74719bd195aabedd Name:Phase 1 Description:Description for phase 1 Url:www.phase1.com ZnnFundsNeeded:+1 QsrFundsNeeded:+1 CreationTimestamp:1000004280 AcceptedTimestamp:0 Status:0}"
t=2001-09-09T02:58:20+0000 lvl=dbug msg="successfully created phase" module=embedded contract=accelerator phase="&{Id:3f2108e0735bc9408e95a93ead94ddcb92ba1c87993b5423e2c728ff9b593c2c ProjectId:2d0c8604e5e0f5bc39db674cbe60d5074b612e2f537bde28c86b02804d60c0d4 Name:Phase 1 Description:Description for phase 1 Url:www.phase1.com ZnnFundsNeeded:+1 QsrFundsNeeded:+1 CreationTimestamp:1000004300 AcceptedTimestamp:0 Status:0}"
t=2001-09-09T02:58:40+0000 lvl=dbug msg="successfully created phase" module=embedded contract=accelerator phase="&{Id:32bbbad6f89fe19c99aa6605192e79fac151cede5a495567fda67166b7faf4e0 ProjectId:f96de28610e2b3b416f7639de5c71350df110b5df3f968aa36dd5c86a939e69d Name:Phase 1 Description:Description for phase 1 Url:www.phase1.com ZnnFundsNeeded:+1 QsrFundsNeeded:+1 CreationTimestamp:1000004320 AcceptedTimestamp:0 Status:0}"
t=2001-09-09T02:59:00+0000 lvl=dbug msg="successfully created phase" module=embedded contract=accelerator phase="&{Id:bf6b8facc07f5371866cb23945c0097d18cd7f917c887a28b3a439d866f5356d ProjectId:52e284cc2ed9e78bf580b37d986dc6d5bc3a937b0a40e2712f543881c7548513 Name:Phase 1 Description:Description for phase 1 Url:www.phase1.com ZnnFundsNeeded:+1 QsrFundsNeeded:+1 CreationTimestamp:1000004340 AcceptedTimestamp:0 Status:0}"
t=2001-09-09T02:59:20+0000 lvl=dbug msg="successfully created phase" module=embedded contract=accelerator phase="&{Id:139fb7da41ba6eb4b068e2ce345e9550832aee0828bc9f6b4705219203e74774 ProjectId:c98e6ca9f0bb8b06a215984855f8b3f1ad7ebe7fcae2300348b668af5a72de22 Name:Phase 1 Description:Description for phase 1 Url:www.phase1.com ZnnFundsNeeded:+1 QsrFundsNeeded:+1 CreationTimestamp:1000004360 AcceptedTimestamp:0 Status:0}"
t=2001-09-09T02:59:40+0000 lvl=dbug msg="successfully created phase" module=embedded contract=accelerator phase="&{Id:9943523a06c6f902d5614e2d2ac0807d7e1fafedd64a368fff7c38f9ad09a034 ProjectId:0706d20fc6d6fcaac0b8b82fc8db26d74b1c195ed97c20914759c65c1c75c437 Name:Phase 1 Description:Description for phase 1 Url:www.phase1.com ZnnFundsNeeded:+1 QsrFundsNeeded:+1 CreationTimestamp:1000004380 AcceptedTimestamp:0 Status:0}"
t=2001-09-09T03:00:00+0000 lvl=dbug msg="successfully created phase" module=embedded contract=accelerator phase="&{Id:60d636bc3d8733e3bac1873e2297b9d5f1fb5f54a607e047a54ce363f717890d ProjectId:1eee79ef32aa34b43a88e1ffd6e5ba1694e78bd69b405a9c89e7a12c993f3693 Name:Phase 1 Description:Description for phase 1 Url:www.phase1.com ZnnFundsNeeded:+1 QsrFundsNeeded:+1 CreationTimestamp:1000004400 AcceptedTimestamp:0 Status:0}"
t=2001-09-09T03:00:20+0000 lvl=dbug msg="successfully created phase" module=embedded contract=accelerator phase="&{Id:2e7a85177350d8806e8e2a2ac5a7791868c0185857c10c4dc6e49153e13467c9 ProjectId:ce131e1aaa32f97e9e37ee6b6c12e51bdc311501141a78e099012f71390603f3 Name:Phase 1 Description:Description for phase 1 Url:www.phase1.com ZnnFundsNeeded:+1 QsrFundsNeeded:+1 CreationTimestamp:1000004420 AcceptedTimestamp:0 Status:0}"
t=2001-09-09T03:00:40+0000 lvl=dbug msg="successfully created phase" module=embedded contract=accelerator phase="&{Id:11664d89cb881dd1493f1fffe9684e742a0e7700eeb3e892f4ccf81523d606d0 ProjectId:25648e80063cf96760ad0e1f66da0fb9a5a414be57f7a9aca20a6e756a6e0686 Name:Phase 1 Description:Description for phase 1 Url:www.phase1.com ZnnFundsNeeded:+1 QsrFundsNeeded:+1 CreationTimestamp:1000004440 AcceptedTimestamp:0 Status:0}"
t=2001-09-09T03:01:00+0000 lvl=dbug msg="successfully created phase" module=embedded contract=accelerator phase="&{Id:edaf98b25891a46f6939c2b4bf1b86fcc7da1f75eeae49ee2801aec05e783043 ProjectId:a61aa098c7a4e8883c7186b3b5727b67e65a4a6fb5b5b791d21077b49c34344c Name:Phase 1 Description:Description for phase 1 Url:www.phase1.com ZnnFundsNeeded:+1 QsrFundsNeeded:+1 CreationTimestamp:1000004460 AcceptedTimestamp:0 Status:0}"
t=2001-09-09T03:01:20+0000 lvl=dbug msg="successfully created phase" module=embedded contract=accelerator phase="&{Id:4f4906c6808c4e9a4038a630d387b547fd569a79ff4d1fd49b9b48297808b1e7 ProjectId:44909794893c762f79b8ddc43e2f52f2ec3462d02a4242fa1cade2676a46ea1c Name:Phase 1 Description:Description for phase 1 Url:www.phase1.com ZnnFundsNeeded:+1 QsrFundsNeeded:+1 CreationTimestamp:1000004480 AcceptedTimestamp:0 Status:0}"
t=2001-09-09T03:01:40+0000 lvl=dbug msg="successfully created phase" module=embedded contract=accelerator phase="&{Id:726ffe57de263a725ec3e9b5d172c9f1bf3855cb68b266b09b3ce2d27d2d20a9 ProjectId:f3b56eab0e0b516660385c4a9731c3e33089ccdda4e493dc1767db98a1e4634e Name:Phase 1 Description:Description for phase 1 Url:www.phase1.com ZnnFundsNeeded:+1 QsrFundsNeeded:+1 CreationTimestamp:1000004500 AcceptedTimestamp:0 Status:0}"
t=2001-09-09T03:02:00+0000 lvl=dbug msg="successfully created phase" module=embedded contract=accelerator phase="&{Id:ffe62d0ab121bb27a9d2681e82df849ec7188fd05f8ddd3508c0ac2060e2c93a ProjectId:210f74710e9b98655216e80fd76d8691c824f4d9a8f899151f05e28723d1cfd4 Name:Phase 1 Description:Description for phase 1 Url:www.phase1.com ZnnFundsNeeded:+1 QsrFundsNeeded:+1 CreationTimestamp:1000004520 AcceptedTimestamp:0 Status:0}"
t=2001-09-09T03:02:20+0000 lvl=dbug msg="successfully created phase" module=embedded contract=accelerator phase="&{Id:d90d630cf0d15e343e0bb654d0c6ae854c18de509a2c484c0dccaa26d4e6b522 ProjectId:0c05b7490d237b2be5c1d26ad6864bd4508f29cf6332402ca03646992dac3a3f Name:Phase 1 Description:Description for phase 1 Url:www.phase1.com ZnnFundsNeeded:+1 QsrFundsNeeded:+1 CreationTimestamp:1000004540 AcceptedTimestamp:0 Status:0}"
t=2001-09-09T03:02:40+0000 lvl=dbug msg="successfully created phase" module=embedded contract=accelerator phase="&{Id:77bb1539ef4788baa795cfd7a7c269cd1d2c27e9bb5c64dbd0da518e20d79129 ProjectId:42253d277232616a64749799b53421de8a0bb6bbf577793a2c58aace77db6813 Name:Phase 1 Description:Description for phase 1 Url:www.phase1.com ZnnFundsNeeded:+1 QsrFundsNeeded:+1 CreationTimestamp:1000004560 AcceptedTimestamp:0 Status:0}"
t=2001-09-09T03:03:00+0000 lvl=dbug msg="successfully created phase" module=embedded contract=accelerator phase="&{Id:f5f76614357d23f020419d74bce5158cb39633f2459d1bf9a834e469cac0c253 ProjectId:01e311586f4639e25e52ffb94113e31f65de9a28612ac8b38170e39f5511727c Name:Phase 1 Description:Description for phase 1 Url:www.phase1.com ZnnFundsNeeded:+1 QsrFundsNeeded:+1 CreationTimestamp:1000004580 AcceptedTimestamp:0 Status:0}"
t=2001-09-09T03:03:20+0000 lvl=dbug msg="successfully created phase" module=embedded contract=accelerator phase="&{Id:863f0170fadd4e52afd3513655f14e06ca140573b57b2022bb971dfa66766e61 ProjectId:ecaf6835810af7641900e5ec6831e37a46ad4d4937c807ea11af108701e1629e Name:Phase 1 Description:Description for phase 1 Url:www.phase1.com ZnnFundsNeeded:+1 QsrFundsNeeded:+1 CreationTimestamp:1000004600 AcceptedTimestamp:0 Status:0}"
t=2001-09-09T03:03:40+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:863f0170fadd4e52afd3513655f14e06ca140573b57b2022bb971dfa66766e61 Name:TEST-pillar-1 Vote:0}"
t=2001-09-09T03:04:00+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:863f0170fadd4e52afd3513655f14e06ca140573b57b2022bb971dfa66766e61 Name:TEST-pillar-cool Vote:0}"
t=2001-09-09T03:04:20+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:f5f76614357d23f020419d74bce5158cb39633f2459d1bf9a834e469cac0c253 Name:TEST-pillar-1 Vote:0}"
t=2001-09-09T03:04:40+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:f5f76614357d23f020419d74bce5158cb39633f2459d1bf9a834e469cac0c253 Name:TEST-pillar-cool Vote:0}"
t=2001-09-09T03:05:00+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:77bb1539ef4788baa795cfd7a7c269cd1d2c27e9bb5c64dbd0da518e20d79129 Name:TEST-pillar-1 Vote:0}"
t=2001-09-09T03:05:20+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:77bb1539ef4788baa795cfd7a7c269cd1d2c27e9bb5c64dbd0da518e20d79129 Name:TEST-pillar-cool Vote:0}"
t=2001-09-09T03:05:40+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:d90d630cf0d15e343e0bb654d0c6ae854c18de509a2c484c0dccaa26d4e6b522 Name:TEST-pillar-1 Vote:0}"
t=2001-09-09T03:06:00+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:d90d630cf0d15e343e0bb654d0c6ae854c18de509a2c484c0dccaa26d4e6b522 Name:TEST-pillar-cool Vote:0}"
t=2001-09-09T03:06:20+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:ffe62d0ab121bb27a9d2681e82df849ec7188fd05f8ddd3508c0ac2060e2c93a Name:TEST-pillar-1 Vote:0}"
t=2001-09-09T03:06:40+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:ffe62d0ab121bb27a9d2681e82df849ec7188fd05f8ddd3508c0ac2060e2c93a Name:TEST-pillar-cool Vote:0}"
t=2001-09-09T03:07:00+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:726ffe57de263a725ec3e9b5d172c9f1bf3855cb68b266b09b3ce2d27d2d20a9 Name:TEST-pillar-1 Vote:0}"
t=2001-09-09T03:07:20+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:726ffe57de263a725ec3e9b5d172c9f1bf3855cb68b266b09b3ce2d27d2d20a9 Name:TEST-pillar-cool Vote:0}"
t=2001-09-09T03:07:40+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:4f4906c6808c4e9a4038a630d387b547fd569a79ff4d1fd49b9b48297808b1e7 Name:TEST-pillar-1 Vote:0}"
t=2001-09-09T03:08:00+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:4f4906c6808c4e9a4038a630d387b547fd569a79ff4d1fd49b9b48297808b1e7 Name:TEST-pillar-cool Vote:0}"
t=2001-09-09T03:08:20+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:edaf98b25891a46f6939c2b4bf1b86fcc7da1f75eeae49ee2801aec05e783043 Name:TEST-pillar-1 Vote:0}"
t=2001-09-09T03:08:40+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:edaf98b25891a46f6939c2b4bf1b86fcc7da1f75eeae49ee2801aec05e783043 Name:TEST-pillar-cool Vote:0}"
t=2001-09-09T03:09:00+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:11664d89cb881dd1493f1fffe9684e742a0e7700eeb3e892f4ccf81523d606d0 Name:TEST-pillar-1 Vote:0}"
t=2001-09-09T03:09:20+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:11664d89cb881dd1493f1fffe9684e742a0e7700eeb3e892f4ccf81523d606d0 Name:TEST-pillar-cool Vote:0}"
t=2001-09-09T03:09:40+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:2e7a85177350d8806e8e2a2ac5a7791868c0185857c10c4dc6e49153e13467c9 Name:TEST-pillar-1 Vote:0}"
t=2001-09-09T03:10:00+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:2e7a85177350d8806e8e2a2ac5a7791868c0185857c10c4dc6e49153e13467c9 Name:TEST-pillar-cool Vote:0}"
t=2001-09-09T03:10:20+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:60d636bc3d8733e3bac1873e2297b9d5f1fb5f54a607e047a54ce363f717890d Name:TEST-pillar-1 Vote:0}"
t=2001-09-09T03:10:40+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:60d636bc3d8733e3bac1873e2297b9d5f1fb5f54a607e047a54ce363f717890d Name:TEST-pillar-cool Vote:0}"
t=2001-09-09T03:11:00+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:9943523a06c6f902d5614e2d2ac0807d7e1fafedd64a368fff7c38f9ad09a034 Name:TEST-pillar-1 Vote:0}"
t=2001-09-09T03:11:20+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:9943523a06c6f902d5614e2d2ac0807d7e1fafedd64a368fff7c38f9ad09a034 Name:TEST-pillar-cool Vote:0}"
t=2001-09-09T03:11:40+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:139fb7da41ba6eb4b068e2ce345e9550832aee0828bc9f6b4705219203e74774 Name:TEST-pillar-1 Vote:0}"
t=2001-09-09T03:12:00+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:139fb7da41ba6eb4b068e2ce345e9550832aee0828bc9f6b4705219203e74774 Name:TEST-pillar-cool Vote:0}"
t=2001-09-09T03:12:20+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:bf6b8facc07f5371866cb23945c0097d18cd7f917c887a28b3a439d866f5356d Name:TEST-pillar-1 Vote:0}"
t=2001-09-09T03:12:40+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:bf6b8facc07f5371866cb23945c0097d18cd7f917c887a28b3a439d866f5356d Name:TEST-pillar-cool Vote:0}"
t=2001-09-09T03:13:00+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:32bbbad6f89fe19c99aa6605192e79fac151cede5a495567fda67166b7faf4e0 Name:TEST-pillar-1 Vote:0}"
t=2001-09-09T03:13:20+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:32bbbad6f89fe19c99aa6605192e79fac151cede5a495567fda67166b7faf4e0 Name:TEST-pillar-cool Vote:0}"
t=2001-09-09T03:13:40+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:3f2108e0735bc9408e95a93ead94ddcb92ba1c87993b5423e2c728ff9b593c2c Name:TEST-pillar-1 Vote:0}"
t=2001-09-09T03:14:00+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:3f2108e0735bc9408e95a93ead94ddcb92ba1c87993b5423e2c728ff9b593c2c Name:TEST-pillar-cool Vote:0}"
t=2001-09-09T03:14:20+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:f4e39d4d61dba473c32a0b8e739ccb69477d6a941d8f36354a69a65e92e40820 Name:TEST-pillar-1 Vote:0}"
t=2001-09-09T03:14:40+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:f4e39d4d61dba473c32a0b8e739ccb69477d6a941d8f36354a69a65e92e40820 Name:TEST-pillar-cool Vote:0}"
t=2001-09-09T03:15:00+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:51f6da3180b6bb5f94868d3c7ac93eb55281ece96e0a4927aee0d462278c0eab Name:TEST-pillar-1 Vote:0}"
t=2001-09-09T03:15:20+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:51f6da3180b6bb5f94868d3c7ac93eb55281ece96e0a4927aee0d462278c0eab Name:TEST-pillar-cool Vote:0}"
t=2001-09-09T03:15:40+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:6e39d316d683b1516221527569e8bc918ebf8ba3fd0f7f5aa50636eac00576ed Name:TEST-pillar-1 Vote:0}"
t=2001-09-09T03:16:00+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:6e39d316d683b1516221527569e8bc918ebf8ba3fd0f7f5aa50636eac00576ed Name:TEST-pillar-cool Vote:0}"
t=2001-09-09T03:16:20+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:f2be1901f74f601f79ff9ed1a9b423e79671ad2a28b26e52cc6b0fd6760d3206 Name:TEST-pillar-1 Vote:0}"
t=2001-09-09T03:16:40+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:f2be1901f74f601f79ff9ed1a9b423e79671ad2a28b26e52cc6b0fd6760d3206 Name:TEST-pillar-cool Vote:0}"
t=2001-09-09T03:17:00+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:35fc522da6315ade76c61e3491fc3c8997ab902ef98f766e504b4b2a721a7162 Name:TEST-pillar-1 Vote:0}"
t=2001-09-09T03:17:20+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:35fc522da6315ade76c61e3491fc3c8997ab902ef98f766e504b4b2a721a7162 Name:TEST-pillar-cool Vote:0}"
t=2001-09-09T03:17:40+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:29634f24be52348c5264d6d9be375672a29bcc2a0d32862ff9bfda1fe62db9c1 Name:TEST-pillar-1 Vote:0}"
t=2001-09-09T03:18:00+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:29634f24be52348c5264d6d9be375672a29bcc2a0d32862ff9bfda1fe62db9c1 Name:TEST-pillar-cool Vote:0}"
t=2001-09-09T03:18:20+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:c2215028df4d5b7f7ed8da993fcb691441055272e5e524fbdfa9105ad86f94ed Name:TEST-pillar-1 Vote:0}"
t=2001-09-09T03:18:40+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:c2215028df4d5b7f7ed8da993fcb691441055272e5e524fbdfa9105ad86f94ed Name:TEST-pillar-cool Vote:0}"
t=2001-09-09T03:19:00+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:5bf70de26405124ed0a765febbe8cc6f737920bbcd0e7bb046207b774de5079a Name:TEST-pillar-1 Vote:0}"
t=2001-09-09T03:19:20+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:5bf70de26405124ed0a765febbe8cc6f737920bbcd0e7bb046207b774de5079a Name:TEST-pillar-cool Vote:0}"
t=2001-09-09T03:19:40+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:aa551bc54e8af1569cba5453dd66a491bd970224f21dc6f6477060a08f817e31 Name:TEST-pillar-1 Vote:0}"
t=2001-09-09T03:20:00+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:aa551bc54e8af1569cba5453dd66a491bd970224f21dc6f6477060a08f817e31 Name:TEST-pillar-cool Vote:0}"
t=2001-09-09T03:20:20+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:74ba31ab7cbd35dc81ec62b30dc8b4f6712d007027ebafa1befc1c17e86328e9 Name:TEST-pillar-1 Vote:0}"
t=2001-09-09T03:20:40+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:74ba31ab7cbd35dc81ec62b30dc8b4f6712d007027ebafa1befc1c17e86328e9 Name:TEST-pillar-cool Vote:0}"
t=2001-09-09T03:21:00+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:9346f501d826109e9b2e62b5b54be37b6cfe6c9300396a6182b12513f581b48d Name:TEST-pillar-1 Vote:0}"
t=2001-09-09T03:21:20+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:9346f501d826109e9b2e62b5b54be37b6cfe6c9300396a6182b12513f581b48d Name:TEST-pillar-cool Vote:0}"
t=2001-09-09T03:21:40+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:04c64e4998805f28c4b8103da6b656ea1e49e3fb13b353ba1a20c56c93501b53 Name:TEST-pillar-1 Vote:0}"
t=2001-09-09T03:22:00+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:04c64e4998805f28c4b8103da6b656ea1e49e3fb13b353ba1a20c56c93501b53 Name:TEST-pillar-cool Vote:0}"
t=2001-09-09T03:22:20+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:f43c74177f631fbacc7c7ed077c01ea0682d62641e02730fcfea1c92b2ef3c18 Name:TEST-pillar-1 Vote:0}"
t=2001-09-09T03:22:40+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:f43c74177f631fbacc7c7ed077c01ea0682d62641e02730fcfea1c92b2ef3c18 Name:TEST-pillar-cool Vote:0}"
t=2001-09-09T03:23:00+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:cf734e4813171de96ef3b78aec3625b53957500e0bd3ce1d4b89c1a1ffe6e61b Name:TEST-pillar-1 Vote:0}"
t=2001-09-09T03:23:20+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:cf734e4813171de96ef3b78aec3625b53957500e0bd3ce1d4b89c1a1ffe6e61b Name:TEST-pillar-cool Vote:0}"
t=2001-09-09T03:23:40+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:2be4082317cbdbc81a10bd0e40f3b6ba6e51693418ba110189a9595cbd21da27 Name:TEST-pillar-1 Vote:0}"
t=2001-09-09T03:24:00+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:2be4082317cbdbc81a10bd0e40f3b6ba6e51693418ba110189a9595cbd21da27 Name:TEST-pillar-cool Vote:0}"
t=2001-09-09T03:24:20+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:6bcc251da9e3886eb20098f641df8681a88655f46b2c7c851eed188217ea4dc5 Name:TEST-pillar-1 Vote:0}"
t=2001-09-09T03:24:40+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:6bcc251da9e3886eb20098f641df8681a88655f46b2c7c851eed188217ea4dc5 Name:TEST-pillar-cool Vote:0}"
t=2001-09-09T03:25:00+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:2e59f568cc9974dd63dff71282b74f92e8e4b7d47d7a16b947e1e56fb42cd73d Name:TEST-pillar-1 Vote:0}"
t=2001-09-09T03:25:20+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:2e59f568cc9974dd63dff71282b74f92e8e4b7d47d7a16b947e1e56fb42cd73d Name:TEST-pillar-cool Vote:0}"
t=2001-09-09T03:25:40+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:83ed3fc32772c9a6820d2153c142505695fb6c7dc969443d7bc33c8d835246a3 Name:TEST-pillar-1 Vote:0}"
t=2001-09-09T03:26:00+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:83ed3fc32772c9a6820d2153c142505695fb6c7dc969443d7bc33c8d835246a3 Name:TEST-pillar-cool Vote:0}"
t=2001-09-09T03:26:20+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:8b7740e42c475e313771196b33af74d8956a93995bc3677d90554c64e98b01ed Name:TEST-pillar-1 Vote:0}"
t=2001-09-09T03:26:40+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:8b7740e42c475e313771196b33af74d8956a93995bc3677d90554c64e98b01ed Name:TEST-pillar-cool Vote:0}"
t=2001-09-09T03:27:00+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:a468c1c7fb00b79148e0dcce637e29f2030ec1585e03b4b8e56d8732c4f24115 Name:TEST-pillar-1 Vote:0}"
t=2001-09-09T03:27:20+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:a468c1c7fb00b79148e0dcce637e29f2030ec1585e03b4b8e56d8732c4f24115 Name:TEST-pillar-cool Vote:0}"
t=2001-09-09T03:27:40+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:c63cb9da0a7c27095b88463dd0586d95d0d9ac57b4d3d0c3032676bba871cea0 Name:TEST-pillar-1 Vote:0}"
t=2001-09-09T03:28:00+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:c63cb9da0a7c27095b88463dd0586d95d0d9ac57b4d3d0c3032676bba871cea0 Name:TEST-pillar-cool Vote:0}"
t=2001-09-09T03:28:20+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:232dd7ca7f907976d61b8909a331e946b20e91b2f19df25cea4c312a2ec64039 Name:TEST-pillar-1 Vote:0}"
t=2001-09-09T03:28:40+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:232dd7ca7f907976d61b8909a331e946b20e91b2f19df25cea4c312a2ec64039 Name:TEST-pillar-cool Vote:0}"
t=2001-09-09T03:29:00+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:a4e92fce4b86f90cfc8058cc5696cb5c875dca8ad9a7d098c1db0cd03decfafd Name:TEST-pillar-1 Vote:0}"
t=2001-09-09T03:29:20+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:a4e92fce4b86f90cfc8058cc5696cb5c875dca8ad9a7d098c1db0cd03decfafd Name:TEST-pillar-cool Vote:0}"
t=2001-09-09T03:29:40+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:fb2bab9fd2687857c3ab1f1d204df6c79a33c7b068b8acdc9411289533e95930 Name:TEST-pillar-1 Vote:0}"
t=2001-09-09T03:30:00+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:fb2bab9fd2687857c3ab1f1d204df6c79a33c7b068b8acdc9411289533e95930 Name:TEST-pillar-cool Vote:0}"
t=2001-09-09T03:30:20+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:275f89121058acdb494d67abc4cb967da7875d454f6089da850445d403489b3f Name:TEST-pillar-1 Vote:0}"
t=2001-09-09T03:30:40+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:275f89121058acdb494d67abc4cb967da7875d454f6089da850445d403489b3f Name:TEST-pillar-cool Vote:0}"
t=2001-09-09T03:31:00+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:991dfcf9be207afd7eea146ec3538840559c5e9515ae396866367e58dbd5eec8 Name:TEST-pillar-1 Vote:0}"
t=2001-09-09T03:31:20+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:991dfcf9be207afd7eea146ec3538840559c5e9515ae396866367e58dbd5eec8 Name:TEST-pillar-cool Vote:0}"
t=2001-09-09T03:31:40+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:d2d312bf497a9e4d0e0b31e52188e269583ca362013646f4bedfdb5255c6d541 Name:TEST-pillar-1 Vote:0}"
t=2001-09-09T03:32:00+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:d2d312bf497a9e4d0e0b31e52188e269583ca362013646f4bedfdb5255c6d541 Name:TEST-pillar-cool Vote:0}"
t=2001-09-09T03:32:20+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:2aad8476df0f80068aaa0fbe24fe455210886f6c77547645f45121c4705da9ec Name:TEST-pillar-1 Vote:0}"
t=2001-09-09T03:32:40+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:2aad8476df0f80068aaa0fbe24fe455210886f6c77547645f45121c4705da9ec Name:TEST-pillar-cool Vote:0}"
t=2001-09-09T03:33:00+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:7a26c861b7d019ebd72a82ac854281f33d54832dab8cc3b7b4d34a61d277272d Name:TEST-pillar-1 Vote:0}"
t=2001-09-09T03:33:20+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:7a26c861b7d019ebd72a82ac854281f33d54832dab8cc3b7b4d34a61d277272d Name:TEST-pillar-cool Vote:0}"
t=2001-09-09T03:33:40+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:eff87cfde9af54d4d5995e02d2eba413b0a89fd919ac941f932ad3b88e22ba43 Name:TEST-pillar-1 Vote:0}"
t=2001-09-09T03:34:00+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:eff87cfde9af54d4d5995e02d2eba413b0a89fd919ac941f932ad3b88e22ba43 Name:TEST-pillar-cool Vote:0}"
t=2001-09-09T03:34:20+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:2295f48f6980bc30c2b09ea04570e70850e85c02c5d97bcb78ee3971a31484e2 Name:TEST-pillar-1 Vote:0}"
t=2001-09-09T03:34:40+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:2295f48f6980bc30c2b09ea04570e70850e85c02c5d97bcb78ee3971a31484e2 Name:TEST-pillar-cool Vote:0}"
t=2001-09-09T03:35:00+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:0fed38b5cb19671a91cb09db4442c02e254fbe7087b2bf39b2da84449af372e5 Name:TEST-pillar-1 Vote:0}"
t=2001-09-09T03:35:20+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:0fed38b5cb19671a91cb09db4442c02e254fbe7087b2bf39b2da84449af372e5 Name:TEST-pillar-cool Vote:0}"
t=2001-09-09T03:35:40+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:1540451bba833be4ac14ec4cb5f172464b4955ff57dd9f4c2745a3a07463ae50 Name:TEST-pillar-1 Vote:0}"
t=2001-09-09T03:36:00+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:1540451bba833be4ac14ec4cb5f172464b4955ff57dd9f4c2745a3a07463ae50 Name:TEST-pillar-cool Vote:0}"
t=2001-09-09T03:36:20+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:7a068bac975c92d7e3935c8ecd9f5c6fb8f702a9e81b1fc2fa786942d2815738 Name:TEST-pillar-1 Vote:0}"
t=2001-09-09T03:36:40+0000 lvl=dbug msg="voted for hash" module=embedded contract=common pillar-vote="&{Id:7a068bac975c92d7e3935c8ecd9f5c6fb8f702a9e81b1fc2fa786942d2815738 Name:TEST-pillar-cool Vote:0}"
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg current-height=722 last-update-height=361
t=2001-09-09T03:46:50+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=1 pillar-name=TEST-pillar-1 reward="&{DelegationReward:+12091382765 BlockReward:+9999999960 TotalReward:+22091382725 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+2094999999900}" total-weight=2494999999900 self-weight=2094999999900
t=2001-09-09T03:46:50+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=1 pillar-name=TEST-pillar-cool reward="&{DelegationReward:+1154308617 BlockReward:+9999999960 TotalReward:+11154308577 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2494999999900 self-weight=200000000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=1 pillar-name=TEST-pillar-znn reward="&{DelegationReward:+1154308617 BlockReward:+9999999960 TotalReward:+11154308577 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2494999999900 self-weight=200000000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=pillar epoch=2
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxsentynelxxxxxxxxxxxxxwy0r2r current-height=722 last-update-height=361
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating sentinel reward" module=embedded contract=sentinel epoch=1 total-znn-reward=187200000000 total-qsr-reward=500000000000 cumulated-sentinel=0 start-time=1000003600 end-time=1000007200
t=2001-09-09T03:46:50+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=sentinel epoch=2
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 current-height=722 last-update-height=361
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating stake reward" module=embedded contract=stake epoch=1 total-reward=1000000000000 cumulated-stake=0 start-time=1000003600 end-time=1000007200
t=2001-09-09T03:46:50+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=stake epoch=2
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae current-height=722 last-update-height=361
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating liquidity reward" module=embedded contract=liquidity epoch=1 znn-amount=187200000000 qsr-amount=500000000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=liquidity epoch=2
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxaccelerat0rxxxxxxxxxxp4tk22 current-height=722 last-update-height=361
t=2001-09-09T03:46:50+0000 lvl=dbug msg="check accelerator votes" module=embedded contract=accelerator votes="&{Id:7a068bac975c92d7e3935c8ecd9f5c6fb8f702a9e81b1fc2fa786942d2815738 Total:2 Yes:2 No:0}" status=true
t=2001-09-09T03:46:50+0000 lvl=dbug msg="finishing and paying phase" module=embedded contract=accelerator project-id=3272b8baff99aa4ed434b2c719592fefb03cc18848f55918e970d31a5630b8e9 phase-id=7a068bac975c92d7e3935c8ecd9f5c6fb8f702a9e81b1fc2fa786942d2815738 znn-amount=1 qsr-amount=1
t=2001-09-09T03:46:50+0000 lvl=dbug msg="check accelerator votes" module=embedded contract=accelerator votes="&{Id:1540451bba833be4ac14ec4cb5f172464b4955ff57dd9f4c2745a3a07463ae50 Total:2 Yes:2 No:0}" status=true
t=2001-09-09T03:46:50+0000 lvl=dbug msg="finishing and paying phase" module=embedded contract=accelerator project-id=7d13ab27040d4f7063e3bf7c31235296b3ee11ffcb62473131e05af4b17e195f phase-id=1540451bba833be4ac14ec4cb5f172464b4955ff57dd9f4c2745a3a07463ae50 znn-amount=1 qsr-amount=1
t=2001-09-09T03:46:50+0000 lvl=dbug msg="check accelerator votes" module=embedded contract=accelerator votes="&{Id:0fed38b5cb19671a91cb09db4442c02e254fbe7087b2bf39b2da84449af372e5 Total:2 Yes:2 No:0}" status=true
t=2001-09-09T03:46:50+0000 lvl=dbug msg="finishing and paying phase" module=embedded contract=accelerator project-id=0495aae6660b286625f98ef4997d7be01a96c84f1d481020fd0fc41d6c95d7da phase-id=0fed38b5cb19671a91cb09db4442c02e254fbe7087b2bf39b2da84449af372e5 znn-amount=1 qsr-amount=1
t=2001-09-09T03:46:50+0000 lvl=dbug msg="check accelerator votes" module=embedded contract=accelerator votes="&{Id:2295f48f6980bc30c2b09ea04570e70850e85c02c5d97bcb78ee3971a31484e2 Total:2 Yes:2 No:0}" status=true
t=2001-09-09T03:46:50+0000 lvl=dbug msg="finishing and paying phase" module=embedded contract=accelerator project-id=8fb32770a4b52898c5abcd054985f0d2776884759606231436ff5ae59914129b phase-id=2295f48f6980bc30c2b09ea04570e70850e85c02c5d97bcb78ee3971a31484e2 znn-amount=1 qsr-amount=1
t=2001-09-09T03:46:50+0000 lvl=dbug msg="check accelerator votes" module=embedded contract=accelerator votes="&{Id:eff87cfde9af54d4d5995e02d2eba413b0a89fd919ac941f932ad3b88e22ba43 Total:2 Yes:2 No:0}" status=true
t=2001-09-09T03:46:50+0000 lvl=dbug msg="finishing and paying phase" module=embedded contract=accelerator project-id=d7ed623d4204c4b6b96672120349d60c8c8b8aeefb134967e02b95a29fddef70 phase-id=eff87cfde9af54d4d5995e02d2eba413b0a89fd919ac941f932ad3b88e22ba43 znn-amount=1 qsr-amount=1
t=2001-09-09T03:46:50+0000 lvl=dbug msg="check accelerator votes" module=embedded contract=accelerator votes="&{Id:7a26c861b7d019ebd72a82ac854281f33d54832dab8cc3b7b4d34a61d277272d Total:2 Yes:2 No:0}" status=true
t=2001-09-09T03:46:50+0000 lvl=dbug msg="finishing and paying phase" module=embedded contract=accelerator project-id=a4e9307b13dac391e0772f1a4b7f153786f233d68fa3f048100304feb285180a phase-id=7a26c861b7d019ebd72a82ac854281f33d54832dab8cc3b7b4d34a61d277272d znn-amount=1 qsr-amount=1
t=2001-09-09T03:46:50+0000 lvl=dbug msg="check accelerator votes" module=embedded contract=accelerator votes="&{Id:2aad8476df0f80068aaa0fbe24fe455210886f6c77547645f45121c4705da9ec Total:2 Yes:2 No:0}" status=true
t=2001-09-09T03:46:50+0000 lvl=dbug msg="finishing and paying phase" module=embedded contract=accelerator project-id=9dc277609817c1e220677cb8e42ea73d63ec43b034815500a9306e7c43ca73bc phase-id=2aad8476df0f80068aaa0fbe24fe455210886f6c77547645f45121c4705da9ec znn-amount=1 qsr-amount=1
t=2001-09-09T03:46:50+0000 lvl=dbug msg="check accelerator votes" module=embedded contract=accelerator votes="&{Id:d2d312bf497a9e4d0e0b31e52188e269583ca362013646f4bedfdb5255c6d541 Total:2 Yes:2 No:0}" status=true
t=2001-09-09T03:46:50+0000 lvl=dbug msg="finishing and paying phase" module=embedded contract=accelerator project-id=e0342441bb2213205f6fc84172a960c68dc2eff567000978407de6d056a59775 phase-id=d2d312bf497a9e4d0e0b31e52188e269583ca362013646f4bedfdb5255c6d541 znn-amount=1 qsr-amount=1
t=2001-09-09T03:46:50+0000 lvl=dbug msg="check accelerator votes" module=embedded contract=accelerator votes="&{Id:991dfcf9be207afd7eea146ec3538840559c5e9515ae396866367e58dbd5eec8 Total:2 Yes:2 No:0}" status=true
t=2001-09-09T03:46:50+0000 lvl=dbug msg="finishing and paying phase" module=embedded contract=accelerator project-id=22b678b98fe293586ed3cb2a4608ea8d7ddf4fddf9fc412ad9b188f1074fa463 phase-id=991dfcf9be207afd7eea146ec3538840559c5e9515ae396866367e58dbd5eec8 znn-amount=1 qsr-amount=1
t=2001-09-09T03:46:50+0000 lvl=dbug msg="check accelerator votes" module=embedded contract=accelerator votes="&{Id:275f89121058acdb494d67abc4cb967da7875d454f6089da850445d403489b3f Total:2 Yes:2 No:0}" status=true
t=2001-09-09T03:46:50+0000 lvl=dbug msg="finishing and paying phase" module=embedded contract=accelerator project-id=6657eeac715335626bf7f4d223bd79a988dad9456d58b39ededd84da61cd84be phase-id=275f89121058acdb494d67abc4cb967da7875d454f6089da850445d403489b3f znn-amount=1 qsr-amount=1
t=2001-09-09T03:46:50+0000 lvl=dbug msg="check accelerator votes" module=embedded contract=accelerator votes="&{Id:fb2bab9fd2687857c3ab1f1d204df6c79a33c7b068b8acdc9411289533e95930 Total:2 Yes:2 No:0}" status=true
t=2001-09-09T03:46:50+0000 lvl=dbug msg="finishing and paying phase" module=embedded contract=accelerator project-id=f94ef63940a2e93570fa423db18a74bf93f58157ca8134662187a00074a2080c phase-id=fb2bab9fd2687857c3ab1f1d204df6c79a33c7b068b8acdc9411289533e95930 znn-amount=1 qsr-amount=1
t=2001-09-09T03:46:50+0000 lvl=dbug msg="check accelerator votes" module=embedded contract=accelerator votes="&{Id:a4e92fce4b86f90cfc8058cc5696cb5c875dca8ad9a7d098c1db0cd03decfafd Total:2 Yes:2 No:0}" status=true
t=2001-09-09T03:46:50+0000 lvl=dbug msg="finishing and paying phase" module=embedded contract=accelerator project-id=f2fb30731ae535c3d275d4db489bf8aaa7f0891a94b4dea7b641197352bfccb7 phase-id=a4e92fce4b86f90cfc8058cc5696cb5c875dca8ad9a7d098c1db0cd03decfafd znn-amount=1 qsr-amount=1
t=2001-09-09T03:46:50+0000 lvl=dbug msg="check accelerator votes" module=embedded contract=accelerator votes="&{Id:232dd7ca7f907976d61b8909a331e946b20e91b2f19df25cea4c312a2ec64039 Total:2 Yes:2 No:0}" status=true
t=2001-09-09T03:46:50+0000 lvl=dbug msg="finishing and paying phase" module=embedded contract=accelerator project-id=9aa41e368f43725a18ad591f37698cd6b0409f55cb3ee2e718df59a25df4f011 phase-id=232dd7ca7f907976d61b8909a331e946b20e91b2f19df25cea4c312a2ec64039 znn-amount=1 qsr-amount=1
t=2001-09-09T03:46:50+0000 lvl=dbug msg="check accelerator votes" module=embedded contract=accelerator votes="&{Id:c63cb9da0a7c27095b88463dd0586d95d0d9ac57b4d3d0c3032676bba871cea0 Total:2 Yes:2 No:0}" status=true
t=2001-09-09T03:46:50+0000 lvl=dbug msg="finishing and paying phase" module=embedded contract=accelerator project-id=99308b7f33dbe0bccdb58b5e16c536c9abf9b86d48b0aa8ab461363fe9953064 phase-id=c63cb9da0a7c27095b88463dd0586d95d0d9ac57b4d3d0c3032676bba871cea0 znn-amount=1 qsr-amount=1
t=2001-09-09T03:46:50+0000 lvl=dbug msg="check accelerator votes" module=embedded contract=accelerator votes="&{Id:a468c1c7fb00b79148e0dcce637e29f2030ec1585e03b4b8e56d8732c4f24115 Total:2 Yes:2 No:0}" status=true
t=2001-09-09T03:46:50+0000 lvl=dbug msg="finishing and paying phase" module=embedded contract=accelerator project-id=c05df0ea4c73d268aab3ae95ea9b9394a2601a5e62cfb07e773c89c61ecc8274 phase-id=a468c1c7fb00b79148e0dcce637e29f2030ec1585e03b4b8e56d8732c4f24115 znn-amount=1 qsr-amount=1
t=2001-09-09T03:46:50+0000 lvl=dbug msg="check accelerator votes" module=embedded contract=accelerator votes="&{Id:8b7740e42c475e313771196b33af74d8956a93995bc3677d90554c64e98b01ed Total:2 Yes:2 No:0}" status=true
t=2001-09-09T03:46:50+0000 lvl=dbug msg="finishing and paying phase" module=embedded contract=accelerator project-id=1a9a20dc93d06934e49a824018692906fd0cede5e72703b147892154f3c7e8af phase-id=8b7740e42c475e313771196b33af74d8956a93995bc3677d90554c64e98b01ed znn-amount=1 qsr-amount=1
t=2001-09-09T03:46:50+0000 lvl=dbug msg="check accelerator votes" module=embedded contract=accelerator votes="&{Id:83ed3fc32772c9a6820d2153c142505695fb6c7dc969443d7bc33c8d835246a3 Total:2 Yes:2 No:0}" status=true
t=2001-09-09T03:46:50+0000 lvl=dbug msg="finishing and paying phase" module=embedded contract=accelerator project-id=4bd532b48d650af895d7feb6bb00b9e33d8e92984f7df122e67d7c8338d6f0ef phase-id=83ed3fc32772c9a6820d2153c142505695fb6c7dc969443d7bc33c8d835246a3 znn-amount=1 qsr-amount=1
t=2001-09-09T03:46:50+0000 lvl=dbug msg="check accelerator votes" module=embedded contract=accelerator votes="&{Id:2e59f568cc9974dd63dff71282b74f92e8e4b7d47d7a16b947e1e56fb42cd73d Total:2 Yes:2 No:0}" status=true
t=2001-09-09T03:46:50+0000 lvl=dbug msg="finishing and paying phase" module=embedded contract=accelerator project-id=cee73558c4cf07b6f233932f09c2ebfba6375dd3eaad46e4cc4bc1abc869e754 phase-id=2e59f568cc9974dd63dff71282b74f92e8e4b7d47d7a16b947e1e56fb42cd73d znn-amount=1 qsr-amount=1
t=2001-09-09T03:46:50+0000 lvl=dbug msg="check accelerator votes" module=embedded contract=accelerator votes="&{Id:6bcc251da9e3886eb20098f641df8681a88655f46b2c7c851eed188217ea4dc5 Total:2 Yes:2 No:0}" status=true
t=2001-09-09T03:46:50+0000 lvl=dbug msg="finishing and paying phase" module=embedded contract=accelerator project-id=9bcd977b6b114075a2464271bffebcf8e9c553e91e4d612b7461b9c84bd43e53 phase-id=6bcc251da9e3886eb20098f641df8681a88655f46b2c7c851eed188217ea4dc5 znn-amount=1 qsr-amount=1
t=2001-09-09T03:46:50+0000 lvl=dbug msg="check accelerator votes" module=embedded contract=accelerator votes="&{Id:2be4082317cbdbc81a10bd0e40f3b6ba6e51693418ba110189a9595cbd21da27 Total:2 Yes:2 No:0}" status=true
t=2001-09-09T03:46:50+0000 lvl=dbug msg="finishing and paying phase" module=embedded contract=accelerator project-id=cacaa36ee1dc2306c42b5f6f452cbc6ee2c179e064077463ffe701a167ad08e8 phase-id=2be4082317cbdbc81a10bd0e40f3b6ba6e51693418ba110189a9595cbd21da27 znn-amount=1 qsr-amount=1
t=2001-09-09T03:46:50+0000 lvl=dbug msg="check accelerator votes" module=embedded contract=accelerator votes="&{Id:cf734e4813171de96ef3b78aec3625b53957500e0bd3ce1d4b89c1a1ffe6e61b Total:2 Yes:2 No:0}" status=true
t=2001-09-09T03:46:50+0000 lvl=dbug msg="not enough votes to finish phase" module=embedded contract=accelerator project-id=82ff45e10718519017cd204bfc3e0afef917600f15ef3159c334ec32338cf61b phase-id=cf734e4813171de96ef3b78aec3625b53957500e0bd3ce1d4b89c1a1ffe6e61b
t=2001-09-09T03:46:50+0000 lvl=dbug msg="check accelerator votes" module=embedded contract=accelerator votes="&{Id:f43c74177f631fbacc7c7ed077c01ea0682d62641e02730fcfea1c92b2ef3c18 Total:2 Yes:2 No:0}" status=true
t=2001-09-09T03:46:50+0000 lvl=dbug msg="not enough votes to finish phase" module=embedded contract=accelerator project-id=2a3996acac36858b02fbc7a5e40a1e7975befacd086e6c39a6ef01ec899eb00b phase-id=f43c74177f631fbacc7c7ed077c01ea0682d62641e02730fcfea1c92b2ef3c18
t=2001-09-09T03:46:50+0000 lvl=dbug msg="check accelerator votes" module=embedded contract=accelerator votes="&{Id:04c64e4998805f28c4b8103da6b656ea1e49e3fb13b353ba1a20c56c93501b53 Total:2 Yes:2 No:0}" status=true
t=2001-09-09T03:46:50+0000 lvl=dbug msg="not enough votes to finish phase" module=embedded contract=accelerator project-id=0654ebf690b4256ba72a086b9c81476ab80e7646a417e02e316f393b872565ae phase-id=04c64e4998805f28c4b8103da6b656ea1e49e3fb13b353ba1a20c56c93501b53
t=2001-09-09T03:46:50+0000 lvl=dbug msg="check accelerator votes" module=embedded contract=accelerator votes="&{Id:9346f501d826109e9b2e62b5b54be37b6cfe6c9300396a6182b12513f581b48d Total:2 Yes:2 No:0}" status=true
t=2001-09-09T03:46:50+0000 lvl=dbug msg="not enough votes to finish phase" module=embedded contract=accelerator project-id=bce40860a01f8d231f17c950e0e8f41d3cc68d2e6d22a82cd0598910000a8aac phase-id=9346f501d826109e9b2e62b5b54be37b6cfe6c9300396a6182b12513f581b48d
t=2001-09-09T03:46:50+0000 lvl=dbug msg="check accelerator votes" module=embedded contract=accelerator votes="&{Id:74ba31ab7cbd35dc81ec62b30dc8b4f6712d007027ebafa1befc1c17e86328e9 Total:2 Yes:2 No:0}" status=true
t=2001-09-09T03:46:50+0000 lvl=dbug msg="not enough votes to finish phase" module=embedded contract=accelerator project-id=1dc34af881d9f834cd6d85b0bbdc372629951b38a34e780d78885bcf4f36dad6 phase-id=74ba31ab7cbd35dc81ec62b30dc8b4f6712d007027ebafa1befc1c17e86328e9
t=2001-09-09T03:46:50+0000 lvl=dbug msg="check accelerator votes" module=embedded contract=accelerator votes="&{Id:aa551bc54e8af1569cba5453dd66a491bd970224f21dc6f6477060a08f817e31 Total:2 Yes:2 No:0}" status=true
t=2001-09-09T03:46:50+0000 lvl=dbug msg="not enough votes to finish phase" module=embedded contract=accelerator project-id=3af378ec6de9728caa5f7c08ea1ddb25f8e44d3801ffd9aeca0de39fbf164215 phase-id=aa551bc54e8af1569cba5453dd66a491bd970224f21dc6f6477060a08f817e31
t=2001-09-09T03:46:50+0000 lvl=dbug msg="check accelerator votes" module=embedded contract=accelerator votes="&{Id:5bf70de26405124ed0a765febbe8cc6f737920bbcd0e7bb046207b774de5079a Total:2 Yes:2 No:0}" status=true
t=2001-09-09T03:46:50+0000 lvl=dbug msg="not enough votes to finish phase" module=embedded contract=accelerator project-id=2bbae9f42e844451189bd76e85eee4150c67fec86afc2532df707ab9a2e02ace phase-id=5bf70de26405124ed0a765febbe8cc6f737920bbcd0e7bb046207b774de5079a
t=2001-09-09T03:46:50+0000 lvl=dbug msg="check accelerator votes" module=embedded contract=accelerator votes="&{Id:c2215028df4d5b7f7ed8da993fcb691441055272e5e524fbdfa9105ad86f94ed Total:2 Yes:2 No:0}" status=true
t=2001-09-09T03:46:50+0000 lvl=dbug msg="not enough votes to finish phase" module=embedded contract=accelerator project-id=413bd235e6186445895a2fa2e73e33d4ebd551ca6cfbbaac7541c7007037dd11 phase-id=c2215028df4d5b7f7ed8da993fcb691441055272e5e524fbdfa9105ad86f94ed
t=2001-09-09T03:46:50+0000 lvl=dbug msg="check accelerator votes" module=embedded contract=accelerator votes="&{Id:29634f24be52348c5264d6d9be375672a29bcc2a0d32862ff9bfda1fe62db9c1 Total:2 Yes:2 No:0}" status=true
t=2001-09-09T03:46:50+0000 lvl=dbug msg="not enough votes to finish phase" module=embedded contract=accelerator project-id=00810eaa6c0be3fc06235533adc8fd8f809916efa9e13bd7761288cfa77ad7ac phase-id=29634f24be52348c5264d6d9be375672a29bcc2a0d32862ff9bfda1fe62db9c1
t=2001-09-09T03:46:50+0000 lvl=dbug msg="check accelerator votes" module=embedded contract=accelerator votes="&{Id:35fc522da6315ade76c61e3491fc3c8997ab902ef98f766e504b4b2a721a7162 Total:2 Yes:2 No:0}" status=true
t=2001-09-09T03:46:50+0000 lvl=dbug msg="not enough votes to finish phase" module=embedded contract=accelerator project-id=93c77e702a9da2a83ad3f2b9e662eaccdc20eacce986759b33a2de55ed0a1746 phase-id=35fc522da6315ade76c61e3491fc3c8997ab902ef98f766e504b4b2a721a7162
t=2001-09-09T03:46:50+0000 lvl=dbug msg="check accelerator votes" module=embedded contract=accelerator votes="&{Id:f2be1901f74f601f79ff9ed1a9b423e79671ad2a28b26e52cc6b0fd6760d3206 Total:2 Yes:2 No:0}" status=true
t=2001-09-09T03:46:50+0000 lvl=dbug msg="not enough votes to finish phase" module=embedded contract=accelerator project-id=94771e772122504233051b9a11f2049ea3c7566fc3f1a0f36da7743e53c04619 phase-id=f2be1901f74f601f79ff9ed1a9b423e79671ad2a28b26e52cc6b0fd6760d3206
t=2001-09-09T03:46:50+0000 lvl=dbug msg="check accelerator votes" module=embedded contract=accelerator votes="&{Id:6e39d316d683b1516221527569e8bc918ebf8ba3fd0f7f5aa50636eac00576ed Total:2 Yes:2 No:0}" status=true
t=2001-09-09T03:46:50+0000 lvl=dbug msg="not enough votes to finish phase" module=embedded contract=accelerator project-id=5068e9bbd0e09abc79a7675bb04cca3ce3de38259218a38ef52f77ad96ba776a phase-id=6e39d316d683b1516221527569e8bc918ebf8ba3fd0f7f5aa50636eac00576ed
t=2001-09-09T03:46:50+0000 lvl=dbug msg="check accelerator votes" module=embedded contract=accelerator votes="&{Id:51f6da3180b6bb5f94868d3c7ac93eb55281ece96e0a4927aee0d462278c0eab Total:2 Yes:2 No:0}" status=true
t=2001-09-09T03:46:50+0000 lvl=dbug msg="not enough votes to finish phase" module=embedded contract=accelerator project-id=9ae410b287268f914c99274ef0e90f1765224d6eccfba1b58dcd633fa02b191a phase-id=51f6da3180b6bb5f94868d3c7ac93eb55281ece96e0a4927aee0d462278c0eab
t=2001-09-09T03:46:50+0000 lvl=dbug msg="check accelerator votes" module=embedded contract=accelerator votes="&{Id:f4e39d4d61dba473c32a0b8e739ccb69477d6a941d8f36354a69a65e92e40820 Total:2 Yes:2 No:0}" status=true
t=2001-09-09T03:46:50+0000 lvl=dbug msg="not enough votes to finish phase" module=embedded contract=accelerator project-id=3b7ee3ecb69f34e59d5c26abe157ed5efaf53e4892e3d87a74719bd195aabedd phase-id=f4e39d4d61dba473c32a0b8e739ccb69477d6a941d8f36354a69a65e92e40820
t=2001-09-09T03:46:50+0000 lvl=dbug msg="check accelerator votes" module=embedded contract=accelerator votes="&{Id:3f2108e0735bc9408e95a93ead94ddcb92ba1c87993b5423e2c728ff9b593c2c Total:2 Yes:2 No:0}" status=true
t=2001-09-09T03:46:50+0000 lvl=dbug msg="not enough votes to finish phase" module=embedded contract=accelerator project-id=2d0c8604e5e0f5bc39db674cbe60d5074b612e2f537bde28c86b02804d60c0d4 phase-id=3f2108e0735bc9408e95a93ead94ddcb92ba1c87993b5423e2c728ff9b593c2c
t=2001-09-09T03:46:50+0000 lvl=dbug msg="check accelerator votes" module=embedded contract=accelerator votes="&{Id:32bbbad6f89fe19c99aa6605192e79fac151cede5a495567fda67166b7faf4e0 Total:2 Yes:2 No:0}" status=true
t=2001-09-09T03:46:50+0000 lvl=dbug msg="not enough votes to finish phase" module=embedded contract=accelerator project-id=f96de28610e2b3b416f7639de5c71350df110b5df3f968aa36dd5c86a939e69d phase-id=32bbbad6f89fe19c99aa6605192e79fac151cede5a495567fda67166b7faf4e0
t=2001-09-09T03:46:50+0000 lvl=dbug msg="check accelerator votes" module=embedded contract=accelerator votes="&{Id:bf6b8facc07f5371866cb23945c0097d18cd7f917c887a28b3a439d866f5356d Total:2 Yes:2 No:0}" status=true
t=2001-09-09T03:46:50+0000 lvl=dbug msg="not enough votes to finish phase" module=embedded contract=accelerator project-id=52e284cc2ed9e78bf580b37d986dc6d5bc3a937b0a40e2712f543881c7548513 phase-id=bf6b8facc07f5371866cb23945c0097d18cd7f917c887a28b3a439d866f5356d
t=2001-09-09T03:46:50+0000 lvl=dbug msg="check accelerator votes" module=embedded contract=accelerator votes="&{Id:139fb7da41ba6eb4b068e2ce345e9550832aee0828bc9f6b4705219203e74774 Total:2 Yes:2 No:0}" status=true
t=2001-09-09T03:46:50+0000 lvl=dbug msg="not enough votes to finish phase" module=embedded contract=accelerator project-id=c98e6ca9f0bb8b06a215984855f8b3f1ad7ebe7fcae2300348b668af5a72de22 phase-id=139fb7da41ba6eb4b068e2ce345e9550832aee0828bc9f6b4705219203e74774
t=2001-09-09T03:46:50+0000 lvl=dbug msg="check accelerator votes" module=embedded contract=accelerator votes="&{Id:9943523a06c6f902d5614e2d2ac0807d7e1fafedd64a368fff7c38f9ad09a034 Total:2 Yes:2 No:0}" status=true
t=2001-09-09T03:46:50+0000 lvl=dbug msg="not enough votes to finish phase" module=embedded contract=accelerator project-id=0706d20fc6d6fcaac0b8b82fc8db26d74b1c195ed97c20914759c65c1c75c437 phase-id=9943523a06c6f902d5614e2d2ac0807d7e1fafedd64a368fff7c38f9ad09a034
t=2001-09-09T03:46:50+0000 lvl=dbug msg="check accelerator votes" module=embedded contract=accelerator votes="&{Id:60d636bc3d8733e3bac1873e2297b9d5f1fb5f54a607e047a54ce363f717890d Total:2 Yes:2 No:0}" status=true
t=2001-09-09T03:46:50+0000 lvl=dbug msg="not enough votes to finish phase" module=embedded contract=accelerator project-id=1eee79ef32aa34b43a88e1ffd6e5ba1694e78bd69b405a9c89e7a12c993f3693 phase-id=60d636bc3d8733e3bac1873e2297b9d5f1fb5f54a607e047a54ce363f717890d
t=2001-09-09T03:46:50+0000 lvl=dbug msg="check accelerator votes" module=embedded contract=accelerator votes="&{Id:2e7a85177350d8806e8e2a2ac5a7791868c0185857c10c4dc6e49153e13467c9 Total:2 Yes:2 No:0}" status=true
t=2001-09-09T03:46:50+0000 lvl=dbug msg="not enough votes to finish phase" module=embedded contract=accelerator project-id=ce131e1aaa32f97e9e37ee6b6c12e51bdc311501141a78e099012f71390603f3 phase-id=2e7a85177350d8806e8e2a2ac5a7791868c0185857c10c4dc6e49153e13467c9
t=2001-09-09T03:46:50+0000 lvl=dbug msg="check accelerator votes" module=embedded contract=accelerator votes="&{Id:11664d89cb881dd1493f1fffe9684e742a0e7700eeb3e892f4ccf81523d606d0 Total:2 Yes:2 No:0}" status=true
t=2001-09-09T03:46:50+0000 lvl=dbug msg="not enough votes to finish phase" module=embedded contract=accelerator project-id=25648e80063cf96760ad0e1f66da0fb9a5a414be57f7a9aca20a6e756a6e0686 phase-id=11664d89cb881dd1493f1fffe9684e742a0e7700eeb3e892f4ccf81523d606d0
t=2001-09-09T03:46:50+0000 lvl=dbug msg="check accelerator votes" module=embedded contract=accelerator votes="&{Id:edaf98b25891a46f6939c2b4bf1b86fcc7da1f75eeae49ee2801aec05e783043 Total:2 Yes:2 No:0}" status=true
t=2001-09-09T03:46:50+0000 lvl=dbug msg="not enough votes to finish phase" module=embedded contract=accelerator project-id=a61aa098c7a4e8883c7186b3b5727b67e65a4a6fb5b5b791d21077b49c34344c phase-id=edaf98b25891a46f6939c2b4bf1b86fcc7da1f75eeae49ee2801aec05e783043
t=2001-09-09T03:46:50+0000 lvl=dbug msg="check accelerator votes" module=embedded contract=accelerator votes="&{Id:4f4906c6808c4e9a4038a630d387b547fd569a79ff4d1fd49b9b48297808b1e7 Total:2 Yes:2 No:0}" status=true
t=2001-09-09T03:46:50+0000 lvl=dbug msg="not enough votes to finish phase" module=embedded contract=accelerator project-id=44909794893c762f79b8ddc43e2f52f2ec3462d02a4242fa1cade2676a46ea1c phase-id=4f4906c6808c4e9a4038a630d387b547fd569a79ff4d1fd49b9b48297808b1e7
t=2001-09-09T03:46:50+0000 lvl=dbug msg="check accelerator votes" module=embedded contract=accelerator votes="&{Id:726ffe57de263a725ec3e9b5d172c9f1bf3855cb68b266b09b3ce2d27d2d20a9 Total:2 Yes:2 No:0}" status=true
t=2001-09-09T03:46:50+0000 lvl=dbug msg="not enough votes to finish phase" module=embedded contract=accelerator project-id=f3b56eab0e0b516660385c4a9731c3e33089ccdda4e493dc1767db98a1e4634e phase-id=726ffe57de263a725ec3e9b5d172c9f1bf3855cb68b266b09b3ce2d27d2d20a9
t=2001-09-09T03:46:50+0000 lvl=dbug msg="check accelerator votes" module=embedded contract=accelerator votes="&{Id:ffe62d0ab121bb27a9d2681e82df849ec7188fd05f8ddd3508c0ac2060e2c93a Total:2 Yes:2 No:0}" status=true
t=2001-09-09T03:46:50+0000 lvl=dbug msg="not enough votes to finish phase" module=embedded contract=accelerator project-id=210f74710e9b98655216e80fd76d8691c824f4d9a8f899151f05e28723d1cfd4 phase-id=ffe62d0ab121bb27a9d2681e82df849ec7188fd05f8ddd3508c0ac2060e2c93a
t=2001-09-09T03:46:50+0000 lvl=dbug msg="check accelerator votes" module=embedded contract=accelerator votes="&{Id:d90d630cf0d15e343e0bb654d0c6ae854c18de509a2c484c0dccaa26d4e6b522 Total:2 Yes:2 No:0}" status=true
t=2001-09-09T03:46:50+0000 lvl=dbug msg="not enough votes to finish phase" module=embedded contract=accelerator project-id=0c05b7490d237b2be5c1d26ad6864bd4508f29cf6332402ca03646992dac3a3f phase-id=d90d630cf0d15e343e0bb654d0c6ae854c18de509a2c484c0dccaa26d4e6b522
t=2001-09-09T03:46:50+0000 lvl=dbug msg="check accelerator votes" module=embedded contract=accelerator votes="&{Id:77bb1539ef4788baa795cfd7a7c269cd1d2c27e9bb5c64dbd0da518e20d79129 Total:2 Yes:2 No:0}" status=true
t=2001-09-09T03:46:50+0000 lvl=dbug msg="not enough votes to finish phase" module=embedded contract=accelerator project-id=42253d277232616a64749799b53421de8a0bb6bbf577793a2c58aace77db6813 phase-id=77bb1539ef4788baa795cfd7a7c269cd1d2c27e9bb5c64dbd0da518e20d79129
t=2001-09-09T03:46:50+0000 lvl=dbug msg="check accelerator votes" module=embedded contract=accelerator votes="&{Id:f5f76614357d23f020419d74bce5158cb39633f2459d1bf9a834e469cac0c253 Total:2 Yes:2 No:0}" status=true
t=2001-09-09T03:46:50+0000 lvl=dbug msg="not enough votes to finish phase" module=embedded contract=accelerator project-id=01e311586f4639e25e52ffb94113e31f65de9a28612ac8b38170e39f5511727c phase-id=f5f76614357d23f020419d74bce5158cb39633f2459d1bf9a834e469cac0c253
t=2001-09-09T03:46:50+0000 lvl=dbug msg="check accelerator votes" module=embedded contract=accelerator votes="&{Id:863f0170fadd4e52afd3513655f14e06ca140573b57b2022bb971dfa66766e61 Total:2 Yes:2 No:0}" status=true
t=2001-09-09T03:46:50+0000 lvl=dbug msg="not enough votes to finish phase" module=embedded contract=accelerator project-id=ecaf6835810af7641900e5ec6831e37a46ad4d4937c807ea11af108701e1629e phase-id=863f0170fadd4e52afd3513655f14e06ca140573b57b2022bb971dfa66766e61
t=2001-09-09T03:47:00+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg TokenName:Zenon Coin TokenSymbol:ZNN TokenDomain:zenon.network TotalSupply:+19874400000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1znnxxxxxxxxxxxxx9z4ulx}" minted-amount=187200000000 to-address=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae
t=2001-09-09T03:47:00+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 TokenName:QuasarCoin TokenSymbol:QSR TokenDomain:zenon.network TotalSupply:+181550000000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1qsrxxxxxxxxxxxxxmrhjll}" minted-amount=500000000000 to-address=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae
t=2001-09-09T03:47:10+0000 lvl=info msg="received donation" module=embedded contract=common embedded=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae from-address=z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0 zts=zts1znnxxxxxxxxxxxxx9z4ulx amount=187200000000
t=2001-09-09T03:47:10+0000 lvl=info msg="received donation" module=embedded contract=common embedded=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae from-address=z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0 zts=zts1qsrxxxxxxxxxxxxxmrhjll amount=500000000000
t=2001-09-09T04:47:00+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg current-height=1083 last-update-height=722
t=2001-09-09T04:47:00+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=2 pillar-name=TEST-pillar-1 reward="&{DelegationReward:+12091382765 BlockReward:+9999999960 TotalReward:+22091382725 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+2094999999900}" total-weight=2494999999900 self-weight=2094999999900
t=2001-09-09T04:47:00+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=2 pillar-name=TEST-pillar-cool reward="&{DelegationReward:+1154308617 BlockReward:+9999999960 TotalReward:+11154308577 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2494999999900 self-weight=200000000000
t=2001-09-09T04:47:00+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=2 pillar-name=TEST-pillar-znn reward="&{DelegationReward:+1154308617 BlockReward:+9999999960 TotalReward:+11154308577 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2494999999900 self-weight=200000000000
t=2001-09-09T04:47:00+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=pillar epoch=3
t=2001-09-09T04:47:00+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxsentynelxxxxxxxxxxxxxwy0r2r current-height=1083 last-update-height=722
t=2001-09-09T04:47:00+0000 lvl=dbug msg="updating sentinel reward" module=embedded contract=sentinel epoch=2 total-znn-reward=187200000000 total-qsr-reward=500000000000 cumulated-sentinel=0 start-time=1000007200 end-time=1000010800
t=2001-09-09T04:47:00+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=sentinel epoch=3
t=2001-09-09T04:47:00+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 current-height=1083 last-update-height=722
t=2001-09-09T04:47:00+0000 lvl=dbug msg="updating stake reward" module=embedded contract=stake epoch=2 total-reward=1000000000000 cumulated-stake=0 start-time=1000007200 end-time=1000010800
t=2001-09-09T04:47:00+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=stake epoch=3
t=2001-09-09T04:47:00+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae current-height=1083 last-update-height=722
t=2001-09-09T04:47:00+0000 lvl=dbug msg="updating liquidity reward" module=embedded contract=liquidity epoch=2 znn-amount=187200000000 qsr-amount=500000000000
t=2001-09-09T04:47:00+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=liquidity epoch=3
t=2001-09-09T04:47:00+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxaccelerat0rxxxxxxxxxxp4tk22 current-height=1083 last-update-height=722
t=2001-09-09T04:47:00+0000 lvl=dbug msg="check accelerator votes" module=embedded contract=accelerator votes="&{Id:cf734e4813171de96ef3b78aec3625b53957500e0bd3ce1d4b89c1a1ffe6e61b Total:2 Yes:2 No:0}" status=true
t=2001-09-09T04:47:00+0000 lvl=dbug msg="finishing and paying phase" module=embedded contract=accelerator project-id=82ff45e10718519017cd204bfc3e0afef917600f15ef3159c334ec32338cf61b phase-id=cf734e4813171de96ef3b78aec3625b53957500e0bd3ce1d4b89c1a1ffe6e61b znn-amount=1 qsr-amount=1
t=2001-09-09T04:47:00+0000 lvl=dbug msg="check accelerator votes" module=embedded contract=accelerator votes="&{Id:f43c74177f631fbacc7c7ed077c01ea0682d62641e02730fcfea1c92b2ef3c18 Total:2 Yes:2 No:0}" status=true
t=2001-09-09T04:47:00+0000 lvl=dbug msg="finishing and paying phase" module=embedded contract=accelerator project-id=2a3996acac36858b02fbc7a5e40a1e7975befacd086e6c39a6ef01ec899eb00b phase-id=f43c74177f631fbacc7c7ed077c01ea0682d62641e02730fcfea1c92b2ef3c18 znn-amount=1 qsr-amount=1
t=2001-09-09T04:47:00+0000 lvl=dbug msg="check accelerator votes" module=embedded contract=accelerator votes="&{Id:04c64e4998805f28c4b8103da6b656ea1e49e3fb13b353ba1a20c56c93501b53 Total:2 Yes:2 No:0}" status=true
t=2001-09-09T04:47:00+0000 lvl=dbug msg="finishing and paying phase" module=embedded contract=accelerator project-id=0654ebf690b4256ba72a086b9c81476ab80e7646a417e02e316f393b872565ae phase-id=04c64e4998805f28c4b8103da6b656ea1e49e3fb13b353ba1a20c56c93501b53 znn-amount=1 qsr-amount=1
t=2001-09-09T04:47:00+0000 lvl=dbug msg="check accelerator votes" module=embedded contract=accelerator votes="&{Id:9346f501d826109e9b2e62b5b54be37b6cfe6c9300396a6182b12513f581b48d Total:2 Yes:2 No:0}" status=true
t=2001-09-09T04:47:00+0000 lvl=dbug msg="finishing and paying phase" module=embedded contract=accelerator project-id=bce40860a01f8d231f17c950e0e8f41d3cc68d2e6d22a82cd0598910000a8aac phase-id=9346f501d826109e9b2e62b5b54be37b6cfe6c9300396a6182b12513f581b48d znn-amount=1 qsr-amount=1
t=2001-09-09T04:47:00+0000 lvl=dbug msg="check accelerator votes" module=embedded contract=accelerator votes="&{Id:74ba31ab7cbd35dc81ec62b30dc8b4f6712d007027ebafa1befc1c17e86328e9 Total:2 Yes:2 No:0}" status=true
t=2001-09-09T04:47:00+0000 lvl=dbug msg="finishing and paying phase" module=embedded contract=accelerator project-id=1dc34af881d9f834cd6d85b0bbdc372629951b38a34e780d78885bcf4f36dad6 phase-id=74ba31ab7cbd35dc81ec62b30dc8b4f6712d007027ebafa1befc1c17e86328e9 znn-amount=1 qsr-amount=1
t=2001-09-09T04:47:00+0000 lvl=dbug msg="check accelerator votes" module=embedded contract=accelerator votes="&{Id:aa551bc54e8af1569cba5453dd66a491bd970224f21dc6f6477060a08f817e31 Total:2 Yes:2 No:0}" status=true
t=2001-09-09T04:47:00+0000 lvl=dbug msg="finishing and paying phase" module=embedded contract=accelerator project-id=3af378ec6de9728caa5f7c08ea1ddb25f8e44d3801ffd9aeca0de39fbf164215 phase-id=aa551bc54e8af1569cba5453dd66a491bd970224f21dc6f6477060a08f817e31 znn-amount=1 qsr-amount=1
t=2001-09-09T04:47:00+0000 lvl=dbug msg="check accelerator votes" module=embedded contract=accelerator votes="&{Id:5bf70de26405124ed0a765febbe8cc6f737920bbcd0e7bb046207b774de5079a Total:2 Yes:2 No:0}" status=true
t=2001-09-09T04:47:00+0000 lvl=dbug msg="finishing and paying phase" module=embedded contract=accelerator project-id=2bbae9f42e844451189bd76e85eee4150c67fec86afc2532df707ab9a2e02ace phase-id=5bf70de26405124ed0a765febbe8cc6f737920bbcd0e7bb046207b774de5079a znn-amount=1 qsr-amount=1
t=2001-09-09T04:47:00+0000 lvl=dbug msg="check accelerator votes" module=embedded contract=accelerator votes="&{Id:c2215028df4d5b7f7ed8da993fcb691441055272e5e524fbdfa9105ad86f94ed Total:2 Yes:2 No:0}" status=true
t=2001-09-09T04:47:00+0000 lvl=dbug msg="finishing and paying phase" module=embedded contract=accelerator project-id=413bd235e6186445895a2fa2e73e33d4ebd551ca6cfbbaac7541c7007037dd11 phase-id=c2215028df4d5b7f7ed8da993fcb691441055272e5e524fbdfa9105ad86f94ed znn-amount=1 qsr-amount=1
t=2001-09-09T04:47:00+0000 lvl=dbug msg="check accelerator votes" module=embedded contract=accelerator votes="&{Id:29634f24be52348c5264d6d9be375672a29bcc2a0d32862ff9bfda1fe62db9c1 Total:2 Yes:2 No:0}" status=true
t=2001-09-09T04:47:00+0000 lvl=dbug msg="finishing and paying phase" module=embedded contract=accelerator project-id=00810eaa6c0be3fc06235533adc8fd8f809916efa9e13bd7761288cfa77ad7ac phase-id=29634f24be52348c5264d6d9be375672a29bcc2a0d32862ff9bfda1fe62db9c1 znn-amount=1 qsr-amount=1
t=2001-09-09T04:47:00+0000 lvl=dbug msg="check accelerator votes" module=embedded contract=accelerator votes="&{Id:35fc522da6315ade76c61e3491fc3c8997ab902ef98f766e504b4b2a721a7162 Total:2 Yes:2 No:0}" status=true
t=2001-09-09T04:47:00+0000 lvl=dbug msg="finishing and paying phase" module=embedded contract=accelerator project-id=93c77e702a9da2a83ad3f2b9e662eaccdc20eacce986759b33a2de55ed0a1746 phase-id=35fc522da6315ade76c61e3491fc3c8997ab902ef98f766e504b4b2a721a7162 znn-amount=1 qsr-amount=1
t=2001-09-09T04:47:00+0000 lvl=dbug msg="check accelerator votes" module=embedded contract=accelerator votes="&{Id:f2be1901f74f601f79ff9ed1a9b423e79671ad2a28b26e52cc6b0fd6760d3206 Total:2 Yes:2 No:0}" status=true
t=2001-09-09T04:47:00+0000 lvl=dbug msg="finishing and paying phase" module=embedded contract=accelerator project-id=94771e772122504233051b9a11f2049ea3c7566fc3f1a0f36da7743e53c04619 phase-id=f2be1901f74f601f79ff9ed1a9b423e79671ad2a28b26e52cc6b0fd6760d3206 znn-amount=1 qsr-amount=1
t=2001-09-09T04:47:00+0000 lvl=dbug msg="check accelerator votes" module=embedded contract=accelerator votes="&{Id:6e39d316d683b1516221527569e8bc918ebf8ba3fd0f7f5aa50636eac00576ed Total:2 Yes:2 No:0}" status=true
t=2001-09-09T04:47:00+0000 lvl=dbug msg="finishing and paying phase" module=embedded contract=accelerator project-id=5068e9bbd0e09abc79a7675bb04cca3ce3de38259218a38ef52f77ad96ba776a phase-id=6e39d316d683b1516221527569e8bc918ebf8ba3fd0f7f5aa50636eac00576ed znn-amount=1 qsr-amount=1
t=2001-09-09T04:47:00+0000 lvl=dbug msg="check accelerator votes" module=embedded contract=accelerator votes="&{Id:51f6da3180b6bb5f94868d3c7ac93eb55281ece96e0a4927aee0d462278c0eab Total:2 Yes:2 No:0}" status=true
t=2001-09-09T04:47:00+0000 lvl=dbug msg="finishing and paying phase" module=embedded contract=accelerator project-id=9ae410b287268f914c99274ef0e90f1765224d6eccfba1b58dcd633fa02b191a phase-id=51f6da3180b6bb5f94868d3c7ac93eb55281ece96e0a4927aee0d462278c0eab znn-amount=1 qsr-amount=1
t=2001-09-09T04:47:00+0000 lvl=dbug msg="check accelerator votes" module=embedded contract=accelerator votes="&{Id:f4e39d4d61dba473c32a0b8e739ccb69477d6a941d8f36354a69a65e92e40820 Total:2 Yes:2 No:0}" status=true
t=2001-09-09T04:47:00+0000 lvl=dbug msg="finishing and paying phase" module=embedded contract=accelerator project-id=3b7ee3ecb69f34e59d5c26abe157ed5efaf53e4892e3d87a74719bd195aabedd phase-id=f4e39d4d61dba473c32a0b8e739ccb69477d6a941d8f36354a69a65e92e40820 znn-amount=1 qsr-amount=1
t=2001-09-09T04:47:00+0000 lvl=dbug msg="check accelerator votes" module=embedded contract=accelerator votes="&{Id:3f2108e0735bc9408e95a93ead94ddcb92ba1c87993b5423e2c728ff9b593c2c Total:2 Yes:2 No:0}" status=true
t=2001-09-09T04:47:00+0000 lvl=dbug msg="finishing and paying phase" module=embedded contract=accelerator project-id=2d0c8604e5e0f5bc39db674cbe60d5074b612e2f537bde28c86b02804d60c0d4 phase-id=3f2108e0735bc9408e95a93ead94ddcb92ba1c87993b5423e2c728ff9b593c2c znn-amount=1 qsr-amount=1
t=2001-09-09T04:47:00+0000 lvl=dbug msg="check accelerator votes" module=embedded contract=accelerator votes="&{Id:32bbbad6f89fe19c99aa6605192e79fac151cede5a495567fda67166b7faf4e0 Total:2 Yes:2 No:0}" status=true
t=2001-09-09T04:47:00+0000 lvl=dbug msg="finishing and paying phase" module=embedded contract=accelerator project-id=f96de28610e2b3b416f7639de5c71350df110b5df3f968aa36dd5c86a939e69d phase-id=32bbbad6f89fe19c99aa6605192e79fac151cede5a495567fda67166b7faf4e0 znn-amount=1 qsr-amount=1
t=2001-09-09T04:47:00+0000 lvl=dbug msg="check accelerator votes" module=embedded contract=accelerator votes="&{Id:bf6b8facc07f5371866cb23945c0097d18cd7f917c887a28b3a439d866f5356d Total:2 Yes:2 No:0}" status=true
t=2001-09-09T04:47:00+0000 lvl=dbug msg="finishing and paying phase" module=embedded contract=accelerator project-id=52e284cc2ed9e78bf580b37d986dc6d5bc3a937b0a40e2712f543881c7548513 phase-id=bf6b8facc07f5371866cb23945c0097d18cd7f917c887a28b3a439d866f5356d znn-amount=1 qsr-amount=1
t=2001-09-09T04:47:00+0000 lvl=dbug msg="check accelerator votes" module=embedded contract=accelerator votes="&{Id:139fb7da41ba6eb4b068e2ce345e9550832aee0828bc9f6b4705219203e74774 Total:2 Yes:2 No:0}" status=true
t=2001-09-09T04:47:00+0000 lvl=dbug msg="finishing and paying phase" module=embedded contract=accelerator project-id=c98e6ca9f0bb8b06a215984855f8b3f1ad7ebe7fcae2300348b668af5a72de22 phase-id=139fb7da41ba6eb4b068e2ce345e9550832aee0828bc9f6b4705219203e74774 znn-amount=1 qsr-amount=1
t=2001-09-09T04:47:00+0000 lvl=dbug msg="check accelerator votes" module=embedded contract=accelerator votes="&{Id:9943523a06c6f902d5614e2d2ac0807d7e1fafedd64a368fff7c38f9ad09a034 Total:2 Yes:2 No:0}" status=true
t=2001-09-09T04:47:00+0000 lvl=dbug msg="finishing and paying phase" module=embedded contract=accelerator project-id=0706d20fc6d6fcaac0b8b82fc8db26d74b1c195ed97c20914759c65c1c75c437 phase-id=9943523a06c6f902d5614e2d2ac0807d7e1fafedd64a368fff7c38f9ad09a034 znn-amount=1 qsr-amount=1
t=2001-09-09T04:47:00+0000 lvl=dbug msg="check accelerator votes" module=embedded contract=accelerator votes="&{Id:60d636bc3d8733e3bac1873e2297b9d5f1fb5f54a607e047a54ce363f717890d Total:2 Yes:2 No:0}" status=true
t=2001-09-09T04:47:00+0000 lvl=dbug msg="finishing and paying phase" module=embedded contract=accelerator project-id=1eee79ef32aa34b43a88e1ffd6e5ba1694e78bd69b405a9c89e7a12c993f3693 phase-id=60d636bc3d8733e3bac1873e2297b9d5f1fb5f54a607e047a54ce363f717890d znn-amount=1 qsr-amount=1
t=2001-09-09T04:47:00+0000 lvl=dbug msg="check accelerator votes" module=embedded contract=accelerator votes="&{Id:2e7a85177350d8806e8e2a2ac5a7791868c0185857c10c4dc6e49153e13467c9 Total:2 Yes:2 No:0}" status=true
t=2001-09-09T04:47:00+0000 lvl=dbug msg="not enough votes to finish phase" module=embedded contract=accelerator project-id=ce131e1aaa32f97e9e37ee6b6c12e51bdc311501141a78e099012f71390603f3 phase-id=2e7a85177350d8806e8e2a2ac5a7791868c0185857c10c4dc6e49153e13467c9
t=2001-09-09T04:47:00+0000 lvl=dbug msg="check accelerator votes" module=embedded contract=accelerator votes="&{Id:11664d89cb881dd1493f1fffe9684e742a0e7700eeb3e892f4ccf81523d606d0 Total:2 Yes:2 No:0}" status=true
t=2001-09-09T04:47:00+0000 lvl=dbug msg="not enough votes to finish phase" module=embedded contract=accelerator project-id=25648e80063cf96760ad0e1f66da0fb9a5a414be57f7a9aca20a6e756a6e0686 phase-id=11664d89cb881dd1493f1fffe9684e742a0e7700eeb3e892f4ccf81523d606d0
t=2001-09-09T04:47:00+0000 lvl=dbug msg="check accelerator votes" module=embedded contract=accelerator votes="&{Id:edaf98b25891a46f6939c2b4bf1b86fcc7da1f75eeae49ee2801aec05e783043 Total:2 Yes:2 No:0}" status=true
t=2001-09-09T04:47:00+0000 lvl=dbug msg="not enough votes to finish phase" module=embedded contract=accelerator project-id=a61aa098c7a4e8883c7186b3b5727b67e65a4a6fb5b5b791d21077b49c34344c phase-id=edaf98b25891a46f6939c2b4bf1b86fcc7da1f75eeae49ee2801aec05e783043
t=2001-09-09T04:47:00+0000 lvl=dbug msg="check accelerator votes" module=embedded contract=accelerator votes="&{Id:4f4906c6808c4e9a4038a630d387b547fd569a79ff4d1fd49b9b48297808b1e7 Total:2 Yes:2 No:0}" status=true
t=2001-09-09T04:47:00+0000 lvl=dbug msg="not enough votes to finish phase" module=embedded contract=accelerator project-id=44909794893c762f79b8ddc43e2f52f2ec3462d02a4242fa1cade2676a46ea1c phase-id=4f4906c6808c4e9a4038a630d387b547fd569a79ff4d1fd49b9b48297808b1e7
t=2001-09-09T04:47:00+0000 lvl=dbug msg="check accelerator votes" module=embedded contract=accelerator votes="&{Id:726ffe57de263a725ec3e9b5d172c9f1bf3855cb68b266b09b3ce2d27d2d20a9 Total:2 Yes:2 No:0}" status=true
t=2001-09-09T04:47:00+0000 lvl=dbug msg="not enough votes to finish phase" module=embedded contract=accelerator project-id=f3b56eab0e0b516660385c4a9731c3e33089ccdda4e493dc1767db98a1e4634e phase-id=726ffe57de263a725ec3e9b5d172c9f1bf3855cb68b266b09b3ce2d27d2d20a9
t=2001-09-09T04:47:00+0000 lvl=dbug msg="check accelerator votes" module=embedded contract=accelerator votes="&{Id:ffe62d0ab121bb27a9d2681e82df849ec7188fd05f8ddd3508c0ac2060e2c93a Total:2 Yes:2 No:0}" status=true
t=2001-09-09T04:47:00+0000 lvl=dbug msg="not enough votes to finish phase" module=embedded contract=accelerator project-id=210f74710e9b98655216e80fd76d8691c824f4d9a8f899151f05e28723d1cfd4 phase-id=ffe62d0ab121bb27a9d2681e82df849ec7188fd05f8ddd3508c0ac2060e2c93a
t=2001-09-09T04:47:00+0000 lvl=dbug msg="check accelerator votes" module=embedded contract=accelerator votes="&{Id:d90d630cf0d15e343e0bb654d0c6ae854c18de509a2c484c0dccaa26d4e6b522 Total:2 Yes:2 No:0}" status=true
t=2001-09-09T04:47:00+0000 lvl=dbug msg="not enough votes to finish phase" module=embedded contract=accelerator project-id=0c05b7490d237b2be5c1d26ad6864bd4508f29cf6332402ca03646992dac3a3f phase-id=d90d630cf0d15e343e0bb654d0c6ae854c18de509a2c484c0dccaa26d4e6b522
t=2001-09-09T04:47:00+0000 lvl=dbug msg="check accelerator votes" module=embedded contract=accelerator votes="&{Id:77bb1539ef4788baa795cfd7a7c269cd1d2c27e9bb5c64dbd0da518e20d79129 Total:2 Yes:2 No:0}" status=true
t=2001-09-09T04:47:00+0000 lvl=dbug msg="not enough votes to finish phase" module=embedded contract=accelerator project-id=42253d277232616a64749799b53421de8a0bb6bbf577793a2c58aace77db6813 phase-id=77bb1539ef4788baa795cfd7a7c269cd1d2c27e9bb5c64dbd0da518e20d79129
t=2001-09-09T04:47:00+0000 lvl=dbug msg="check accelerator votes" module=embedded contract=accelerator votes="&{Id:f5f76614357d23f020419d74bce5158cb39633f2459d1bf9a834e469cac0c253 Total:2 Yes:2 No:0}" status=true
t=2001-09-09T04:47:00+0000 lvl=dbug msg="not enough votes to finish phase" module=embedded contract=accelerator project-id=01e311586f4639e25e52ffb94113e31f65de9a28612ac8b38170e39f5511727c phase-id=f5f76614357d23f020419d74bce5158cb39633f2459d1bf9a834e469cac0c253
t=2001-09-09T04:47:00+0000 lvl=dbug msg="check accelerator votes" module=embedded contract=accelerator votes="&{Id:863f0170fadd4e52afd3513655f14e06ca140573b57b2022bb971dfa66766e61 Total:2 Yes:2 No:0}" status=true
t=2001-09-09T04:47:00+0000 lvl=dbug msg="not enough votes to finish phase" module=embedded contract=accelerator project-id=ecaf6835810af7641900e5ec6831e37a46ad4d4937c807ea11af108701e1629e phase-id=863f0170fadd4e52afd3513655f14e06ca140573b57b2022bb971dfa66766e61
t=2001-09-09T04:47:10+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg TokenName:Zenon Coin TokenSymbol:ZNN TokenDomain:zenon.network TotalSupply:+20061600000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1znnxxxxxxxxxxxxx9z4ulx}" minted-amount=187200000000 to-address=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae
t=2001-09-09T04:47:10+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 TokenName:QuasarCoin TokenSymbol:QSR TokenDomain:zenon.network TotalSupply:+182050000000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1qsrxxxxxxxxxxxxxmrhjll}" minted-amount=500000000000 to-address=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae
t=2001-09-09T04:47:20+0000 lvl=info msg="received donation" module=embedded contract=common embedded=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae from-address=z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0 zts=zts1znnxxxxxxxxxxxxx9z4ulx amount=187200000000
t=2001-09-09T04:47:20+0000 lvl=info msg="received donation" module=embedded contract=common embedded=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae from-address=z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0 zts=zts1qsrxxxxxxxxxxxxxmrhjll amount=500000000000
`)

	defer z.CallContract(&nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     types.AcceleratorContract,
		Data:          definition.ABICommon.PackMethodPanic(definition.DonateMethodName),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        common.Big100,
	}).Error(t, nil)
	z.InsertNewMomentum()
	z.InsertNewMomentum()
	defer z.CallContract(&nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     types.AcceleratorContract,
		Data:          definition.ABICommon.PackMethodPanic(definition.DonateMethodName),
		TokenStandard: types.QsrTokenStandard,
		Amount:        common.Big100,
	}).Error(t, nil)
	z.InsertNewMomentum()
	z.InsertNewMomentum()

	z.ExpectBalance(types.AcceleratorContract, types.ZnnTokenStandard, 100)
	z.ExpectBalance(types.AcceleratorContract, types.QsrTokenStandard, 100)

	activateAccelerator(z)

	for i := 1; i <= 50; i++ {
		z.CallContract(&nom.AccountBlock{
			Address:       g.User1.Address,
			ToAddress:     types.AcceleratorContract,
			TokenStandard: types.ZnnTokenStandard,
			Amount:        constants.ProjectCreationAmount,
			Data: definition.ABIAccelerator.PackMethodPanic(definition.CreateProjectMethodName,
				fmt.Sprintf("Test Project %d", i), //param.Name
				"TEST DESCRIPTION",                //param.Description
				"test.com",                        //param.Url
				big.NewInt(5),                     //param.ZnnFundsNeeded
				big.NewInt(5),                     //param.QsrFundsNeeded
			),
		})
		z.InsertNewMomentum() // cemented send block
		z.InsertNewMomentum() // cemented token-receive-block
	}

	projectList, err := acceleratorAPI.GetAll(0, 50)
	common.DealWithErr(err)

	for index := range projectList.List {
		z.CallContract(&nom.AccountBlock{
			Address:   g.Pillar1.Address,
			ToAddress: types.AcceleratorContract,
			Data: definition.ABIAccelerator.PackMethodPanic(definition.VoteByNameMethodName,
				projectList.List[index].Id,
				g.Pillar1Name,
				definition.VoteYes,
			),
		})
		z.InsertNewMomentum() // cemented send block
		z.InsertNewMomentum() // cemented token-receive-block

		z.CallContract(&nom.AccountBlock{
			Address:   g.Pillar2.Address,
			ToAddress: types.AcceleratorContract,
			Data: definition.ABIAccelerator.PackMethodPanic(definition.VoteByNameMethodName,
				projectList.List[index].Id,
				g.Pillar2Name,
				definition.VoteYes,
			),
		})
		z.InsertNewMomentum() // cemented send block
		z.InsertNewMomentum() // cemented token-receive-block
	}

	z.InsertMomentumsTo(60*6 + 2)

	for index := range projectList.List {
		z.CallContract(&nom.AccountBlock{
			Address:   g.User1.Address,
			ToAddress: types.AcceleratorContract,
			Data: definition.ABIAccelerator.PackMethodPanic(definition.AddPhaseMethodName,
				projectList.List[index].Id, //param.Hash
				"Phase 1",                  //param.Name
				"Description for phase 1",  //param.Description
				"www.phase1.com",           //param.Url
				big.NewInt(1),              //param.ZnnFundsNeeded
				big.NewInt(1),              //param.QsrFundsNeeded
			),
		})
		z.InsertNewMomentum() // cemented send block
		z.InsertNewMomentum() // cemented token-receive-block
	}

	projectList, err = acceleratorAPI.GetAll(0, 50)
	common.DealWithErr(err)

	for index := range projectList.List {
		z.CallContract(&nom.AccountBlock{
			Address:   g.Pillar1.Address,
			ToAddress: types.AcceleratorContract,
			Data: definition.ABIAccelerator.PackMethodPanic(definition.VoteByNameMethodName,
				projectList.List[index].PhaseIds[0],
				g.Pillar1Name,
				definition.VoteYes,
			),
		})
		z.InsertNewMomentum() // cemented send block
		z.InsertNewMomentum() // cemented token-receive-block

		z.CallContract(&nom.AccountBlock{
			Address:   g.Pillar2.Address,
			ToAddress: types.AcceleratorContract,
			Data: definition.ABIAccelerator.PackMethodPanic(definition.VoteByNameMethodName,
				projectList.List[index].PhaseIds[0],
				g.Pillar2Name,
				definition.VoteYes,
			),
		})
		z.InsertNewMomentum() // cemented send block
		z.InsertNewMomentum() // cemented token-receive-block
	}

	z.InsertMomentumsTo(60*6*2 + 2)
	z.InsertMomentumsTo(60*6*4 + 2)
}
