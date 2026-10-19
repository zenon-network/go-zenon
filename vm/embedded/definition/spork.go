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
	// ABISpork is abi definition of token contract
	ABISpork = abi.JSONToABIContract(strings.NewReader(jsonSpork))

	// CommunitySporkAddressStartHeight is the first momentum height at
	// which types.CommunitySporkAddress may create and activate
	// sporks; calls below it are rejected.
	CommunitySporkAddressStartHeight uint64 = 10109240 // Targeting 2025-04-16 12:00:00 UTC
	// CommunitySporkAddressEndHeight is the momentum height at which
	// the community spork address's original window closes; calls at
	// or above it are rejected until the renewal window below opens.
	// types.SporkAddress is bound by neither window.
	CommunitySporkAddressEndHeight uint64 = 13243712 // Targeting 2026-04-16 12:00:00 UTC

	// Renewal authorization window. The start is projected from the observed
	// mainnet rate of ~11.46s per momentum (frontier 14310887 at
	// 2026-09-30 10:05:50 UTC) and sits ~30 days after the planned v0.0.9
	// release so operators have time to upgrade. The end was projected from
	// frontier 14228943 at 2026-09-19 16:57:40 UTC at ~11.35s per momentum.
	// At the nominal 10s rate the window would open around 2026-10-30 and
	// close around 2028-06-24 instead.
	//
	// Rollout: the check runs in the contract receive at the height of the
	// momentum that confirmed the send. Nodes without this window regenerate
	// that receive with ErrPermissionDenied and reject any momentum that
	// confirms a community spork send inside [RenewalStart, RenewalEnd). The
	// chain therefore diverges only when such a send is confirmed while
	// un-upgraded nodes are still producing or validating momentums. Do not
	// send community spork transactions until the network runs a release that
	// includes this window; every node should upgrade before
	// CommunitySporkAddressRenewalStartHeight so that the window can be used.
	CommunitySporkAddressRenewalStartHeight uint64 = 14567887 // Targeting 2026-11-03 12:00:00 UTC
	// CommunitySporkAddressRenewalEndHeight closes the renewal window;
	// community spork calls confirmed at or above it are rejected.
	CommunitySporkAddressRenewalEndHeight uint64 = 19791986 // Targeting 2028-09-19 12:00:00 UTC
)

const (
	_ byte = iota
	sporkInfoPrefix

	// SporkInfoPrefix is the storage prefix of spork entries, exported for
	// the chain cache, which indexes activations.
	SporkInfoPrefix = sporkInfoPrefix
)

// Spork is one stored protocol-upgrade switch. Id is the hash of the
// CreateSpork send block; Activated and EnforcementHeight stay false
// and zero until ActivateSpork sets them. Entries are stored under
// sporkInfoPrefix (1) followed by the 32-byte id.
type Spork struct {
	Id          types.Hash `json:"id"`
	Name        string     `json:"name"`
	Description string     `json:"description"`

	// If the spork is active, Activated = true and EnforcementHeight = activation momentum height + HeightDelay
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

// ParseSporkInfo decodes a stored spork entry. The data is the
// contract's own, so a malformed value panics rather than returning an
// error.
func ParseSporkInfo(data []byte) *Spork {
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
		return ParseSporkInfo(data)
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
		spork := ParseSporkInfo(iterator.Value())
		sporks = append(sporks, spork)
	}
	return sporks
}
