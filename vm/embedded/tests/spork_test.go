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
	types.ImplementedSporksMap[types.HexToHashPanic("eedcf4003fedfa69a0494e8b09c156f70c3e790af563642d0222514c3078966f")] = true
	z.InsertMomentumsTo(20)
}
