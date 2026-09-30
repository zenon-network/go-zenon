package tests

import (
	"testing"

	g "github.com/zenon-network/go-zenon/chain/genesis/mock"
	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/rpc/api/embedded"
	"github.com/zenon-network/go-zenon/vm/constants"
	"github.com/zenon-network/go-zenon/vm/embedded/definition"
	"github.com/zenon-network/go-zenon/zenon/mock"
)

// pinCommunitySporkWindow overrides the community spork address and the
// authorization windows for the duration of the test and restores the incoming
// values once the test, including the deferred mock stop, has finished.
func pinCommunitySporkWindow(t *testing.T, address types.Address, start, end, renewalStart, renewalEnd uint64) {
	prevAddress := types.CommunitySporkAddress
	prevStart := definition.CommunitySporkAddressStartHeight
	prevEnd := definition.CommunitySporkAddressEndHeight
	prevRenewalStart := definition.CommunitySporkAddressRenewalStartHeight
	prevRenewalEnd := definition.CommunitySporkAddressRenewalEndHeight
	t.Cleanup(func() {
		types.CommunitySporkAddress = prevAddress
		definition.CommunitySporkAddressStartHeight = prevStart
		definition.CommunitySporkAddressEndHeight = prevEnd
		definition.CommunitySporkAddressRenewalStartHeight = prevRenewalStart
		definition.CommunitySporkAddressRenewalEndHeight = prevRenewalEnd
	})
	types.CommunitySporkAddress = address
	definition.CommunitySporkAddressStartHeight = start
	definition.CommunitySporkAddressEndHeight = end
	definition.CommunitySporkAddressRenewalStartHeight = renewalStart
	definition.CommunitySporkAddressRenewalEndHeight = renewalEnd
}

// markSporkImplemented marks a spork as implemented for the duration of the
// test and restores the entry's previous state afterwards. The map is written
// in place, so the entry is restored rather than the map variable.
func markSporkImplemented(t *testing.T, id types.Hash) {
	prev, existed := types.ImplementedSporksMap[id]
	t.Cleanup(func() {
		if existed {
			types.ImplementedSporksMap[id] = prev
		} else {
			delete(types.ImplementedSporksMap, id)
		}
	})
	types.ImplementedSporksMap[id] = true
}

// Test create spork
func TestSpork_CreateSpork(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()
	sporkAPI := embedded.NewSporkApi(z)
	defer z.SaveLogs(common.EmbeddedLogger).Equals(t, `
t=2001-09-09T01:46:50+0000 lvl=dbug msg=created module=embedded contract=spork spork="&{Id:eedcf4003fedfa69a0494e8b09c156f70c3e790af563642d0222514c3078966f Name:spork-1 Description:spork description Activated:false EnforcementHeight:0}"
`)

	// Create spork
	z.InsertSendBlock(&nom.AccountBlock{
		Address:   g.Spork.Address,
		ToAddress: types.SporkContract,
		Data: definition.ABISpork.PackMethodPanic(definition.SporkCreateMethodName,
			"spork-1",           // name
			"spork description", // description
		),
	}, nil, mock.SkipVmChanges)
	z.InsertNewMomentum()
	common.Json(sporkAPI.GetAll(0, 10)).Equals(t, `
{
	"count": 1,
	"list": [
		{
			"id": "eedcf4003fedfa69a0494e8b09c156f70c3e790af563642d0222514c3078966f",
			"name": "spork-1",
			"description": "spork description",
			"activated": false,
			"enforcementHeight": 0
		}
	]
}`)
}

// Test create community spork
func TestSpork_CreateCommunitySpork(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()

	// Set community spork address and validity heights
	pinCommunitySporkWindow(t, g.Pillar1.Address, 10, 15, 100, 200)

	sporkAPI := embedded.NewSporkApi(z)
	defer z.SaveLogs(common.EmbeddedLogger).Equals(t, `
t=2001-09-09T01:48:20+0000 lvl=dbug msg=created module=embedded contract=spork spork="&{Id:d1f69475c9c1d7b6ed5cecc9c6f5dbeb99380f1ea5b1516f92a8cc7c86d3cb12 Name:spork-1 Description:spork description Activated:false EnforcementHeight:0}"
`)

	// Attempt spork activation and expect error since
	// community spork address isn't valid yet
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.Pillar1.Address,
		ToAddress: types.SporkContract,
		Data: definition.ABISpork.PackMethodPanic(definition.SporkCreateMethodName,
			"spork-1",           // name
			"spork description", // description
		),
	}).Error(t, constants.ErrPermissionDenied)
	insertMomentums(z, 2)

	// Wait until spork address becomes valid
	z.InsertMomentumsTo(10)

	// Create spork after spork address has become valid
	z.InsertSendBlock(&nom.AccountBlock{
		Address:   g.Pillar1.Address,
		ToAddress: types.SporkContract,
		Data: definition.ABISpork.PackMethodPanic(definition.SporkCreateMethodName,
			"spork-1",           // name
			"spork description", // description
		),
	}, nil, mock.SkipVmChanges)
	z.InsertNewMomentum()
	common.Json(sporkAPI.GetAll(0, 10)).Equals(t, `
{
	"count": 1,
	"list": [
		{
			"id": "d1f69475c9c1d7b6ed5cecc9c6f5dbeb99380f1ea5b1516f92a8cc7c86d3cb12",
			"name": "spork-1",
			"description": "spork description",
			"activated": false,
			"enforcementHeight": 0
		}
	]
}`)

	// Wait until spork address expires
	z.InsertMomentumsTo(15)

	// Attempt spork creation and expect error
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.Pillar1.Address,
		ToAddress: types.SporkContract,
		Data: definition.ABISpork.PackMethodPanic(definition.SporkCreateMethodName,
			"spork-2",           // name
			"spork description", // description
		),
	}).Error(t, constants.ErrPermissionDenied)
	insertMomentums(z, 2)
}

// Test community spork address renewal window boundaries for both create and
// activate. The authorization check runs in the contract receive, whose
// MomentumAcknowledged is the momentum that confirmed the send, so the relevant
// height is the send's confirmation height. It is not the height at which the
// send was created, nor the frontier at the time the receive is generated. In
// this fixture every send is confirmed by the next momentum, so a send inserted
// at frontier N is evaluated at height N+1.
func TestSpork_CommunitySporkRenewalWindow(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()

	// Original window [10, 15), renewal window [20, 25)
	pinCommunitySporkWindow(t, g.Pillar1.Address, 10, 15, 20, 25)

	sporkAPI := embedded.NewSporkApi(z)
	defer z.SaveLogs(common.EmbeddedLogger).Equals(t, `
t=2001-09-09T01:48:20+0000 lvl=dbug msg=created module=embedded contract=spork spork="&{Id:d8dbf1c52335a1ad4f2d6c5ac681909c1425acd151ad60508001ce83dfeeca3f Name:spork-original Description:spork description Activated:false EnforcementHeight:0}"
t=2001-09-09T01:49:50+0000 lvl=dbug msg=created module=embedded contract=spork spork="&{Id:c333f39e1bbbdbeb42e7f04ae52f8c9a51447f606f059cd581a41c2e960dbb9a Name:spork-renewal-start Description:spork description Activated:false EnforcementHeight:0}"
t=2001-09-09T01:49:50+0000 lvl=dbug msg=activated module=embedded contract=spork spork="&{Id:d8dbf1c52335a1ad4f2d6c5ac681909c1425acd151ad60508001ce83dfeeca3f Name:spork-original Description:spork description Activated:true EnforcementHeight:26}"
t=2001-09-09T01:50:30+0000 lvl=dbug msg=created module=embedded contract=spork spork="&{Id:023262531ccb839cb58d58738d759ad4907f5923ca76aa78c7d76854b4de3dc0 Name:spork-renewal-last Description:spork description Activated:false EnforcementHeight:0}"
t=2001-09-09T01:50:30+0000 lvl=dbug msg=activated module=embedded contract=spork spork="&{Id:c333f39e1bbbdbeb42e7f04ae52f8c9a51447f606f059cd581a41c2e960dbb9a Name:spork-renewal-start Description:spork description Activated:true EnforcementHeight:30}"
`)

	createBlock := func(name string) *nom.AccountBlock {
		return &nom.AccountBlock{
			Address:   g.Pillar1.Address,
			ToAddress: types.SporkContract,
			Data: definition.ABISpork.PackMethodPanic(definition.SporkCreateMethodName,
				name,                // name
				"spork description", // description
			),
		}
	}
	sporkId := func(name string) types.Hash {
		sporkList, err := sporkAPI.GetAll(0, 10)
		common.FailIfErr(t, err)
		for _, spork := range sporkList.List {
			if spork.Name == name {
				return spork.Id
			}
		}
		t.Fatalf("spork %v not found", name)
		return types.ZeroHash
	}
	activateBlock := func(name string) *nom.AccountBlock {
		return &nom.AccountBlock{
			Address:   g.Pillar1.Address,
			ToAddress: types.SporkContract,
			Data: definition.ABISpork.PackMethodPanic(definition.SporkActivateMethodName,
				sporkId(name), // id
			),
		}
	}

	// Confirmed at 11: inside the original window
	z.InsertMomentumsTo(10)
	z.InsertSendBlock(createBlock("spork-original"), nil, mock.SkipVmChanges)
	z.InsertNewMomentum()

	// Confirmed at 15: original end height is exclusive
	z.InsertMomentumsTo(14)
	defer z.CallContract(createBlock("spork-original-end")).Error(t, constants.ErrPermissionDenied)
	defer z.CallContract(activateBlock("spork-original")).Error(t, constants.ErrPermissionDenied)
	insertMomentums(z, 2)

	// Confirmed at 19: gap between the windows
	z.InsertMomentumsTo(18)
	defer z.CallContract(createBlock("spork-gap")).Error(t, constants.ErrPermissionDenied)
	defer z.CallContract(activateBlock("spork-original")).Error(t, constants.ErrPermissionDenied)
	insertMomentums(z, 1)

	// Confirmed at 20: renewal start height is inclusive
	z.InsertSendBlock(createBlock("spork-renewal-start"), nil, mock.SkipVmChanges)
	z.InsertSendBlock(activateBlock("spork-original"), nil, mock.SkipVmChanges)
	z.InsertNewMomentum()
	markSporkImplemented(t, sporkId("spork-original"))

	// Confirmed at 24: last valid height of the renewal window
	z.InsertMomentumsTo(23)
	z.InsertSendBlock(createBlock("spork-renewal-last"), nil, mock.SkipVmChanges)
	z.InsertSendBlock(activateBlock("spork-renewal-start"), nil, mock.SkipVmChanges)
	z.InsertNewMomentum()
	markSporkImplemented(t, sporkId("spork-renewal-start"))

	// Confirmed at 25: renewal end height is exclusive
	defer z.CallContract(createBlock("spork-expired")).Error(t, constants.ErrPermissionDenied)
	defer z.CallContract(activateBlock("spork-renewal-last")).Error(t, constants.ErrPermissionDenied)
	insertMomentums(z, 2)

	common.Json(sporkAPI.GetAll(0, 10)).Equals(t, `
{
	"count": 3,
	"list": [
		{
			"id": "023262531ccb839cb58d58738d759ad4907f5923ca76aa78c7d76854b4de3dc0",
			"name": "spork-renewal-last",
			"description": "spork description",
			"activated": false,
			"enforcementHeight": 0
		},
		{
			"id": "c333f39e1bbbdbeb42e7f04ae52f8c9a51447f606f059cd581a41c2e960dbb9a",
			"name": "spork-renewal-start",
			"description": "spork description",
			"activated": true,
			"enforcementHeight": 30
		},
		{
			"id": "d8dbf1c52335a1ad4f2d6c5ac681909c1425acd151ad60508001ce83dfeeca3f",
			"name": "spork-original",
			"description": "spork description",
			"activated": true,
			"enforcementHeight": 26
		}
	]
}`)
}

// Test create spork from non-spork address
func TestSpork_CreateSporkFromNonSporkAddress(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()

	// Try to create spork using User1
	z.InsertSendBlock(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.SporkContract,
		Data: definition.ABISpork.PackMethodPanic(definition.SporkCreateMethodName,
			"spork-1",           // name
			"spork description", // description
		),
	}, constants.ErrPermissionDenied, mock.SkipVmChanges)
	z.InsertNewMomentum()
}

// Test create multiple spork
func TestSpork_CreateSporkWithSmallDelay(t *testing.T) {
	z := mock.NewMockZenon(t)
	sporkAPI := embedded.NewSporkApi(z)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).Equals(t, `
t=2001-09-09T01:46:50+0000 lvl=dbug msg=created module=embedded contract=spork spork="&{Id:eedcf4003fedfa69a0494e8b09c156f70c3e790af563642d0222514c3078966f Name:spork-1 Description:spork description Activated:false EnforcementHeight:0}"
t=2001-09-09T01:47:00+0000 lvl=dbug msg=created module=embedded contract=spork spork="&{Id:5673632bc827e8c7e12d8949d94f0dd68de7ce86452a5c27abbb75c26ef301fe Name:spork-2 Description:spork description Activated:false EnforcementHeight:0}"
`)

	z.InsertSendBlock(&nom.AccountBlock{
		Address:   g.Spork.Address,
		ToAddress: types.SporkContract,
		Data: definition.ABISpork.PackMethodPanic(definition.SporkCreateMethodName,
			"spork-1",           // name
			"spork description", // description
		),
	}, nil, mock.SkipVmChanges)
	z.InsertNewMomentum()

	z.InsertSendBlock(&nom.AccountBlock{
		Address:   g.Spork.Address,
		ToAddress: types.SporkContract,
		Data: definition.ABISpork.PackMethodPanic(definition.SporkCreateMethodName,
			"spork-2",           // name
			"spork description", // description
		),
	}, nil, mock.SkipVmChanges)
	z.InsertNewMomentum()

	common.Json(sporkAPI.GetAll(0, 5)).Equals(t, `
{
	"count": 2,
	"list": [
		{
			"id": "5673632bc827e8c7e12d8949d94f0dd68de7ce86452a5c27abbb75c26ef301fe",
			"name": "spork-2",
			"description": "spork description",
			"activated": false,
			"enforcementHeight": 0
		},
		{
			"id": "eedcf4003fedfa69a0494e8b09c156f70c3e790af563642d0222514c3078966f",
			"name": "spork-1",
			"description": "spork description",
			"activated": false,
			"enforcementHeight": 0
		}
	]
}`)
}

// Test activate spork
func TestSpork_ActivateSpork(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()
	sporkAPI := embedded.NewSporkApi(z)
	defer z.SaveLogs(common.EmbeddedLogger).Equals(t, `
t=2001-09-09T01:46:50+0000 lvl=dbug msg=created module=embedded contract=spork spork="&{Id:eedcf4003fedfa69a0494e8b09c156f70c3e790af563642d0222514c3078966f Name:spork-1 Description:spork description Activated:false EnforcementHeight:0}"
t=2001-09-09T01:47:00+0000 lvl=dbug msg=activated module=embedded contract=spork spork="&{Id:eedcf4003fedfa69a0494e8b09c156f70c3e790af563642d0222514c3078966f Name:spork-1 Description:spork description Activated:true EnforcementHeight:9}"
`)
	// Create spork
	z.InsertSendBlock(&nom.AccountBlock{
		Address:   g.Spork.Address,
		ToAddress: types.SporkContract,
		Data: definition.ABISpork.PackMethodPanic(definition.SporkCreateMethodName,
			"spork-1",           // name
			"spork description", // description
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
	common.Json(sporkAPI.GetAll(0, 5)).Equals(t, `
{
	"count": 1,
	"list": [
		{
			"id": "eedcf4003fedfa69a0494e8b09c156f70c3e790af563642d0222514c3078966f",
			"name": "spork-1",
			"description": "spork description",
			"activated": true,
			"enforcementHeight": 9
		}
	]
}`)
	markSporkImplemented(t, types.HexToHashPanic("eedcf4003fedfa69a0494e8b09c156f70c3e790af563642d0222514c3078966f"))
	z.InsertMomentumsTo(20)
}

// Test activate community spork
func TestSpork_ActivateCommunitySpork(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()

	// Set community spork address and validity heights
	pinCommunitySporkWindow(t, g.Pillar1.Address, 1, 25, 100, 200)

	sporkAPI := embedded.NewSporkApi(z)
	defer z.SaveLogs(common.EmbeddedLogger).Equals(t, `
t=2001-09-09T01:46:50+0000 lvl=dbug msg=created module=embedded contract=spork spork="&{Id:145f041e6c18cc5fecc1194636129424e4cbaffe7f22a4b711202a00be4a1158 Name:spork-1 Description:spork description Activated:false EnforcementHeight:0}"
t=2001-09-09T01:47:00+0000 lvl=dbug msg=activated module=embedded contract=spork spork="&{Id:145f041e6c18cc5fecc1194636129424e4cbaffe7f22a4b711202a00be4a1158 Name:spork-1 Description:spork description Activated:true EnforcementHeight:9}"
t=2001-09-09T01:50:00+0000 lvl=dbug msg=created module=embedded contract=spork spork="&{Id:8ba9a5508f799212fce0fc53799ae8c9d6f5b026d29e223c6bc6fec69455f15d Name:spork-2 Description:spork description Activated:false EnforcementHeight:0}"
`)
	// Create spork
	z.InsertSendBlock(&nom.AccountBlock{
		Address:   g.Pillar1.Address,
		ToAddress: types.SporkContract,
		Data: definition.ABISpork.PackMethodPanic(definition.SporkCreateMethodName,
			"spork-1",           // name
			"spork description", // description
		),
	}, nil, mock.SkipVmChanges)
	z.InsertNewMomentum()

	sporkList, _ := sporkAPI.GetAll(0, 10)
	id := sporkList.List[0].Id

	z.InsertSendBlock(&nom.AccountBlock{
		Address:   g.Pillar1.Address,
		ToAddress: types.SporkContract,
		Data: definition.ABISpork.PackMethodPanic(definition.SporkActivateMethodName,
			id, // id
		),
	}, nil, mock.SkipVmChanges)
	z.InsertNewMomentum()
	common.Json(sporkAPI.GetAll(0, 5)).Equals(t, `
{
	"count": 1,
	"list": [
		{
			"id": "145f041e6c18cc5fecc1194636129424e4cbaffe7f22a4b711202a00be4a1158",
			"name": "spork-1",
			"description": "spork description",
			"activated": true,
			"enforcementHeight": 9
		}
	]
}`)
	markSporkImplemented(t, types.HexToHashPanic("145f041e6c18cc5fecc1194636129424e4cbaffe7f22a4b711202a00be4a1158"))
	z.InsertMomentumsTo(20)

	// Create another spork
	z.InsertSendBlock(&nom.AccountBlock{
		Address:   g.Pillar1.Address,
		ToAddress: types.SporkContract,
		Data: definition.ABISpork.PackMethodPanic(definition.SporkCreateMethodName,
			"spork-2",           // name
			"spork description", // description
		),
	}, nil, mock.SkipVmChanges)
	z.InsertNewMomentum()

	// Wait until spork address expires
	z.InsertMomentumsTo(30)

	sporkList, _ = sporkAPI.GetAll(0, 10)
	id = sporkList.List[1].Id

	// Attempt spork activation and expect error
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.Pillar1.Address,
		ToAddress: types.SporkContract,
		Data: definition.ABISpork.PackMethodPanic(definition.SporkActivateMethodName,
			id, // id
		),
	}).Error(t, constants.ErrPermissionDenied)
	insertMomentums(z, 2)
}
