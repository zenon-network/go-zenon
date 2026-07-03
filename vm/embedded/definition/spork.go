package definition

import (
	"strings"

	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/db"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/vm/abi"
)

const (
	// jsonSpork is the ABI JSON of the spork embedded contract: the
	// CreateSpork and ActivateSpork methods and the stored sporkInfo
	// variable. Parsed into ABISpork.
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

	// SporkCreateMethodName names the method that registers a new
	// spork; only types.SporkAddress and types.CommunitySporkAddress
	// may call it.
	SporkCreateMethodName = "CreateSpork"
	// SporkActivateMethodName names the method that activates an
	// existing spork by id, scheduling its enforcement; restricted to
	// the same two addresses as CreateSpork.
	SporkActivateMethodName = "ActivateSpork"

	sporkInfoVariableName = "sporkInfo"
)

var (
	// ABISpork is the parsed ABI of the spork embedded contract.
	ABISpork = abi.JSONToABIContract(strings.NewReader(jsonSpork))

	// CommunitySporkAddressStartHeight is the first momentum height at
	// which types.CommunitySporkAddress may create and activate
	// sporks; calls below it are rejected.
	CommunitySporkAddressStartHeight uint64 = 10109240 // Targeting 2025-04-16 12:00:00 UTC
	// CommunitySporkAddressEndHeight is the momentum height at which
	// the community spork address's window closes; calls at or above
	// it are rejected. types.SporkAddress is not bound by the window.
	CommunitySporkAddressEndHeight uint64 = 13243712 // Targeting 2026-04-16 12:00:00 UTC
)

const (
	_ byte = iota
	sporkInfoPrefix
)

// Spork is one stored protocol-upgrade switch. Id is the hash of the
// CreateSpork send block; Activated and EnforcementHeight stay false
// and zero until ActivateSpork sets them. Entries are stored under
// sporkInfoPrefix (1) followed by the 32-byte id.
type Spork struct {
	Id          types.Hash `json:"id"`
	Name        string     `json:"name"`
	Description string     `json:"description"`

	// Activated reports whether ActivateSpork has been called; once
	// activated, EnforcementHeight is the momentum height from which
	// the feature applies: the frontier height at activation plus
	// constants.SporkMinHeightDelay.
	Activated         bool   `json:"activated"`
	EnforcementHeight uint64 `json:"enforcementHeight"`
}

// Save stores the spork under its id key, panicking via
// common.DealWithErr on database errors.
func (spork *Spork) Save(context db.DB) {
	common.DealWithErr(context.Put(spork.Key(), spork.Data()))
}

// Data packs the full spork state; packing failures panic.
func (spork *Spork) Data() []byte {
	return ABISpork.PackVariablePanic(
		sporkInfoVariableName,
		spork.Id,
		spork.Name,
		spork.Description,
		spork.Activated,
		spork.EnforcementHeight)
}

// Key is sporkInfoPrefix (1) followed by the 32-byte id.
func (spork *Spork) Key() []byte {
	return common.JoinBytes([]byte{sporkInfoPrefix}, spork.Id.Bytes())
}

func parseSporkInfo(data []byte) *Spork {
	spork := new(Spork)
	ABISpork.UnpackVariablePanic(spork, sporkInfoVariableName, data)
	return spork
}

// GetSporkInfoById returns the spork registered under id, or nil if
// none exists; database errors panic via common.DealWithErr.
func GetSporkInfoById(context db.DB, id types.Hash) *Spork {
	spork := new(Spork)
	spork.Id = id
	key := spork.Key()
	data, err := context.Get(key)
	common.DealWithErr(err)
	if len(data) == 0 {
		return nil
	} else {
		return parseSporkInfo(data)
	}
}

// GetAllSporks returns every registered spork, in storage-key (id
// byte) order; database errors panic via common.DealWithErr.
func GetAllSporks(context db.DB) []*Spork {
	iterator := context.NewIterator([]byte{sporkInfoPrefix})
	defer iterator.Release()

	sporks := make([]*Spork, 0)
	for {
		if !iterator.Next() {
			common.DealWithErr(iterator.Error())
			break
		}
		spork := parseSporkInfo(iterator.Value())
		sporks = append(sporks, spork)
	}
	return sporks
}
