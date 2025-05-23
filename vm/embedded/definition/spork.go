package definition

import (
	"strings"

	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/db"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/vm/abi"
)

const (
	jsonSpork = `
	[
		{"type":"function","name":"CreateSpork","inputs":[{"name":"name","type":"string"},{"name":"description","type":"string"}]},
		{"type":"function","name":"ActivateSpork","inputs":[{"name":"id","type":"hash"}]},

		{"type":"variable", "name":"sporkInfo", "inputs":[
			{"name":"id", "type":"hash"},
			{"name":"name", "type":"string"},
			{"name":"description", "type":"string"},
			{"name":"activated", "type": "bool"},
			{"name":"enforcementHeight", "type": "uint64"}
		]}
	]`

	SporkCreateMethodName   = "CreateSpork"
	SporkActivateMethodName = "ActivateSpork"

	sporkInfoVariableName = "sporkInfo"
)

var (
	// ABISpork is abi definition of token contract
	ABISpork = abi.JSONToABIContract(strings.NewReader(jsonSpork))

	CommunitySporkAddressStartHeight uint64 = 10109240 // Targeting 2025-04-16 12:00:00 UTC
	CommunitySporkAddressEndHeight   uint64 = 13243712 // Targeting 2026-04-16 12:00:00 UTC
)

const (
	_ byte = iota
	sporkInfoPrefix

	SporkInfoPrefix = sporkInfoPrefix
)

type Spork struct {
	Id          types.Hash `json:"id"`
	Name        string     `json:"name"`
	Description string     `json:"description"`

	// If the spork is active, Activated = true and EnforcementHeight = activation momentum height + HeightDelay
	Activated         bool   `json:"activated"`
	EnforcementHeight uint64 `json:"enforcementHeight"`
}

func (spork *Spork) Save(context db.DB) {
	common.DealWithErr(context.Put(spork.Key(), spork.Data()))
}
func (spork *Spork) Data() []byte {
	return ABISpork.PackVariablePanic(
		sporkInfoVariableName,
		spork.Id,
		spork.Name,
		spork.Description,
		spork.Activated,
		spork.EnforcementHeight)
}
func (spork *Spork) Key() []byte {
	return common.JoinBytes([]byte{sporkInfoPrefix}, spork.Id.Bytes())
}

func ParseSporkInfo(data []byte) *Spork {
	spork := new(Spork)
	ABISpork.UnpackVariablePanic(spork, sporkInfoVariableName, data)
	return spork
}

func GetSporkInfoById(context db.DB, id types.Hash) *Spork {
	spork := new(Spork)
	spork.Id = id
	key := spork.Key()
	data, err := context.Get(key)
	common.DealWithErr(err)
	if len(data) == 0 {
		return nil
	} else {
		return ParseSporkInfo(data)
	}
}
func GetAllSporks(context db.DB) []*Spork {
	iterator := context.NewIterator([]byte{sporkInfoPrefix})
	defer iterator.Release()

	sporks := make([]*Spork, 0)
	for {
		if !iterator.Next() {
			common.DealWithErr(iterator.Error())
			break
		}
		spork := ParseSporkInfo(iterator.Value())
		sporks = append(sporks, spork)
	}
	return sporks
}
