package definition

import (
	"encoding/binary"
	"encoding/json"
	"math/big"
	"strings"

	"github.com/pkg/errors"

	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/db"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/vm/abi"
	"github.com/zenon-network/go-zenon/vm/constants"
)

const (
	// jsonPillars is the ABI JSON of the pillar embedded contract:
	// registration (normal and legacy), pillar updates, revocation,
	// delegation and the shared deposit/reward methods, plus the
	// stored pillar, producer-index, legacy-slot, delegation and
	// epoch-history variables. Parsed into ABIPillars.
	jsonPillars = `
	[
{"type":"function","name":"Update", "inputs":[]},
		{"type":"function","name":"Register", "inputs":[
			{"name":"name","type":"string"},
			{"name":"producerAddress","type":"address"},
			{"name":"rewardAddress","type":"address"},
			{"name":"giveBlockRewardPercentage","type":"uint8"},
			{"name":"giveDelegateRewardPercentage","type":"uint8"}
		]},
		{"type":"function","name":"RegisterLegacy", "inputs":[
			{"name":"name","type":"string"},
			{"name":"producerAddress","type":"address"},
			{"name":"rewardAddress","type":"address"},
			{"name":"giveBlockRewardPercentage","type":"uint8"},
			{"name":"giveDelegateRewardPercentage","type":"uint8"}, 
			{"name":"publicKey", "type":"string"}, 
			{"name":"signature","type":"string"}
		]},
		{"type":"function","name":"UpdatePillar", "inputs":[
			{"name":"name","type":"string"},
			{"name":"producerAddress","type":"address"},
			{"name":"rewardAddress","type":"address"},
			{"name":"giveBlockRewardPercentage","type":"uint8"},
			{"name":"giveDelegateRewardPercentage","type":"uint8"}
		]},
		{"type":"function","name":"DepositQsr", "inputs":[]},
		{"type":"function","name":"WithdrawQsr", "inputs":[]},
		{"type":"function","name":"Revoke","inputs":[{"name":"name","type":"string"}]},
		{"type":"function","name":"Delegate", "inputs":[{"name":"name","type":"string"}]},
		{"type":"function","name":"Undelegate","inputs":[]},
		{"type":"function","name":"CollectReward","inputs":[]},

		{"type":"variable","name":"pillarInfo","inputs":[
			{"name":"name","type":"string"},
			{"name":"blockProducingAddress","type":"address"},
			{"name":"rewardWithdrawAddress","type":"address"},
			{"name":"stakeAddress","type":"address"},
			{"name":"amount","type":"uint256"},
			{"name":"registrationTime","type":"int64"},
			{"name":"revokeTime","type":"int64"},
			{"name":"giveBlockRewardPercentage","type":"uint8"},
			{"name":"giveDelegateRewardPercentage","type":"uint8"},
			{"name":"pillarType","type":"uint8"}
		]},
		{"type":"variable","name":"producingPillarName","inputs":[
			{"name":"name","type":"string"}
		]},
		{"type":"variable","name":"LegacyPillarEntry","inputs":[
			{"name":"pillarCount", "type":"uint8"}
		]},
		{"type":"variable","name":"delegationInfo","inputs":[
			{"name":"name","type":"string"}
		]},
		{"type":"variable","name":"pillarEpochHistory","inputs":[
			{"name":"giveBlockRewardPercentage","type":"uint8"},
			{"name":"giveDelegateRewardPercentage","type":"uint8"},
			{"name":"producedBlockNum","type":"int32"},
			{"name":"expectedBlockNum","type":"int32"},
			{"name":"weight","type":"uint256"}
		]}
	]`

	// RegisterMethodName names the method that registers a new pillar
	// against the full ZNN stake and deposited QSR requirements.
	RegisterMethodName = "Register"
	// LegacyRegisterMethodName names the method that registers a
	// pillar against a legacy slot, proving ownership of a swap-era
	// public key with a signature.
	LegacyRegisterMethodName = "RegisterLegacy"

	// UpdatePillarMethodName names the method that changes a pillar's
	// producing address, reward address and reward-sharing
	// percentages.
	UpdatePillarMethodName = "UpdatePillar"
	// RevokeMethodName names the method that revokes a pillar and
	// returns its staked ZNN.
	RevokeMethodName = "Revoke"
	// DelegateMethodName names the method by which an address
	// delegates its ZNN balance weight to a pillar.
	DelegateMethodName = "Delegate"
	// UndelegateMethodName names the method that removes the caller's
	// delegation.
	UndelegateMethodName = "Undelegate"

	pillarInfoVariableName          = "pillarInfo"
	producingPillarNameVariableName = "producingPillarName"
	legacyPillarEntryVariableName   = "LegacyPillarEntry"
	delegationInfoVariableName      = "delegationInfo"
	pillarEpochHistoryVariableName  = "pillarEpochHistory"
)

var (
	// ABIPillars is the parsed ABI of the pillar embedded contract.
	ABIPillars = abi.JSONToABIContract(strings.NewReader(jsonPillars))

	pillarInfoKeyPrefix          = []byte{1}
	producingPillarNameKeyPrefix = []byte{2}
	legacyPillarEntryKeyPrefix   = []byte{3}
	delegationInfoKeyPrefix      = []byte{4}
	pillarEpochHistoryKeyPrefix  = []byte{5}

	// AnyPillarType matches every pillar type when filtering with
	// GetPillarsList.
	AnyPillarType = uint8(0)
	// LegacyPillarType marks a pillar registered through
	// RegisterLegacy against a legacy (swap-era) pillar slot.
	LegacyPillarType = uint8(1)
	// NormalPillarType marks a pillar registered through Register
	// with the full QSR deposit requirement.
	NormalPillarType = uint8(2)
)

// RegisterParam carries the arguments of Register and UpdatePillar:
// the pillar name, its block-producing and reward-collection
// addresses and the percentages (0-100) of its momentum and
// delegation rewards it gives away to delegators.
type RegisterParam struct {
	Name                         string
	ProducerAddress              types.Address
	RewardAddress                types.Address
	GiveBlockRewardPercentage    uint8
	GiveDelegateRewardPercentage uint8
}

// LegacyRegisterParam carries the arguments of RegisterLegacy: the
// normal registration parameters plus a base64 legacy public key and
// a signature proving ownership of the legacy pillar slot.
type LegacyRegisterParam struct {
	RegisterParam
	PublicKey string
	Signature string
}

// PillarInfo is the stored state of a pillar. Name is the unique
// identifier; BlockProducingAddress signs the momentums the pillar
// produces, RewardWithdrawAddress may collect its rewards and
// StakeAddress is the registrant that locked the ZNN deposit (Amount,
// smallest units) and controls the pillar. RegistrationTime and
// RevokeTime are unix seconds; RevokeTime is zero while the pillar is
// active. Entries are stored under pillarInfoKeyPrefix (1) followed
// by the SHA3-256 hash of the name.
type PillarInfo struct {
	Name                         string
	BlockProducingAddress        types.Address
	RewardWithdrawAddress        types.Address
	StakeAddress                 types.Address
	Amount                       *big.Int
	RegistrationTime             int64
	RevokeTime                   int64
	GiveBlockRewardPercentage    uint8
	GiveDelegateRewardPercentage uint8
	PillarType                   uint8
}

// IsActive reports whether the pillar has not been revoked
// (RevokeTime is zero).
func (pillar *PillarInfo) IsActive() bool {
	return pillar.RevokeTime == 0
}

// Save stores the full pillar state under its name key, returning any
// pack or put error.
func (pillar *PillarInfo) Save(context db.DB) error {
	data, err := ABIPillars.PackVariable(
		pillarInfoVariableName,
		pillar.Name,
		pillar.BlockProducingAddress,
		pillar.RewardWithdrawAddress,
		pillar.StakeAddress,
		pillar.Amount,
		pillar.RegistrationTime,
		pillar.RevokeTime,
		pillar.GiveBlockRewardPercentage,
		pillar.GiveDelegateRewardPercentage,
		pillar.PillarType,
	)
	if err != nil {
		return err
	}
	return context.Put(GetPillarInfoKey(pillar.Name), data)
}

// GetPillarInfoKey returns pillarInfoKeyPrefix followed by the
// SHA3-256 hash of the pillar name.
func GetPillarInfoKey(name string) []byte {
	return common.JoinBytes(pillarInfoKeyPrefix, types.NewHash([]byte(name)).Bytes())
}
func parsePillarInfo(data []byte) (*PillarInfo, error) {
	if len(data) > 0 {
		pillar := new(PillarInfo)
		if err := ABIPillars.UnpackVariable(pillar, pillarInfoVariableName, data); err != nil {
			return nil, err
		}
		return pillar, nil
	} else {
		return nil, constants.ErrDataNonExistent
	}
}

// GetPillarInfo returns the pillar registered under name, or
// constants.ErrDataNonExistent if no such pillar exists.
func GetPillarInfo(context db.DB, name string) (*PillarInfo, error) {
	key := GetPillarInfoKey(name)
	if data, err := context.Get(key); err != nil {
		return nil, err
	} else {
		return parsePillarInfo(data)
	}
}

// GetPillarsList returns the stored pillars in storage-key order
// (by hash of name, not alphabetical), optionally restricted to
// active ones and to a single pillar type; AnyPillarType matches all
// types.
func GetPillarsList(context db.DB, onlyActive bool, pillarType uint8) ([]*PillarInfo, error) {
	iterator := context.NewIterator(pillarInfoKeyPrefix)
	defer iterator.Release()
	list := make([]*PillarInfo, 0)
	for {
		if !iterator.Next() {
			if iterator.Error() != nil {
				return nil, iterator.Error()
			}
			break
		}

		if pillar, err := parsePillarInfo(iterator.Value()); err == nil {
			if (!onlyActive || pillar.RevokeTime == 0) && (pillarType == AnyPillarType || pillarType == pillar.PillarType) {
				list = append(list, pillar)
			}
		} else if err == constants.ErrDataNonExistent {
			continue
		} else {
			return nil, err
		}
	}
	return list, nil
}

// ProducingPillar is the index entry from a block-producing address
// to the pillar name using it. Entries are never deleted, so a
// producing address can only ever be reused by the same pillar; the
// implementation consults this index when validating Register and
// UpdatePillar. Keyed by the producing address.
type ProducingPillar struct {
	Producing *types.Address
	Name      string
}

// Save stores the pillar name under the producing-address key,
// returning any pack or put error; the address is recovered from the
// key when parsing.
func (ppName *ProducingPillar) Save(context db.DB) error {
	data, err := ABIPillars.PackVariable(
		producingPillarNameVariableName,
		ppName.Name,
	)
	if err != nil {
		return err
	}
	return context.Put(GetProducingPillarKey(*ppName.Producing), data)
}

// GetProducingPillarKey returns producingPillarNameKeyPrefix (2)
// followed by the producing address bytes.
func GetProducingPillarKey(producing types.Address) []byte {
	return common.JoinBytes(producingPillarNameKeyPrefix, producing.Bytes())
}
func isProducingPillarKey(key []byte) bool {
	return key[0] == producingPillarNameKeyPrefix[0]
}
func unmarshalProducingPillarKey(key []byte) (*types.Address, error) {
	if !isProducingPillarKey(key) {
		return nil, errors.Errorf("invalid key! Not producing pillar key")
	}
	addr := new(types.Address)
	if err := addr.SetBytes(key[1:]); err != nil {
		return nil, err
	}
	return addr, nil
}
func parseProducingPillar(key []byte, data []byte) (*ProducingPillar, error) {
	if len(data) > 0 {
		entry := new(ProducingPillar)
		if err := ABIPillars.UnpackVariable(entry, producingPillarNameVariableName, data); err != nil {
			return nil, err
		}

		producing, err := unmarshalProducingPillarKey(key)
		if err != nil {
			return nil, err
		}
		entry.Producing = producing
		return entry, nil
	} else {
		return nil, constants.ErrDataNonExistent
	}
}

// GetProducingPillarName returns the index entry mapping address to
// the pillar name that uses (or once used) it as block producer, or
// constants.ErrDataNonExistent if the address was never assigned.
func GetProducingPillarName(context db.DB, address types.Address) (*ProducingPillar, error) {
	key := GetProducingPillarKey(address)
	if data, err := context.Get(key); err != nil {
		return nil, err
	} else {
		return parseProducingPillar(key, data)
	}
}

// DelegationInfo records that Backer delegates its ZNN balance weight
// to the pillar Name. Stored under delegationInfoKeyPrefix (4)
// followed by the backer address bytes, so each address holds at most
// one delegation; only the name is packed as the value.
type DelegationInfo struct {
	Backer types.Address
	Name   string
}

// Save stores the delegation under the backer's key, returning any
// pack or put error.
func (delegation *DelegationInfo) Save(context db.DB) error {
	data, err := ABIPillars.PackVariable(
		delegationInfoVariableName,
		delegation.Name,
	)
	if err != nil {
		return err
	}
	return context.Put(getDelegationInfoKey(delegation.Backer), data)
}

// Delete removes the backer's delegation.
func (delegation *DelegationInfo) Delete(context db.DB) error {
	return context.Delete(getDelegationInfoKey(delegation.Backer))
}

func getDelegationInfoKey(addr types.Address) []byte {
	return common.JoinBytes(delegationInfoKeyPrefix, addr.Bytes())
}
func isDelegationInfoKey(key []byte) bool {
	return key[0] == delegationInfoKeyPrefix[0]
}
func unmarshalDelegationInfo(key []byte) (*types.Address, error) {
	if !isDelegationInfoKey(key) {
		return nil, errors.Errorf("invalid key! Not delegation info key")
	}
	addr := new(types.Address)
	if err := addr.SetBytes(key[1:]); err != nil {
		return nil, err
	}
	return addr, nil
}
func parseDelegationInfo(key, data []byte) (*DelegationInfo, error) {
	if len(data) > 0 {
		entry := new(DelegationInfo)
		if err := ABIPillars.UnpackVariable(entry, delegationInfoVariableName, data); err != nil {
			return nil, err
		}

		address, err := unmarshalDelegationInfo(key)
		if err != nil {
			return nil, err
		}
		entry.Backer = *address
		return entry, nil
	} else {
		return nil, constants.ErrDataNonExistent
	}
}

// GetDelegationInfo returns the delegation of address, or
// constants.ErrDataNonExistent if it does not delegate.
func GetDelegationInfo(context db.DB, address types.Address) (*DelegationInfo, error) {
	key := getDelegationInfoKey(address)
	if data, err := context.Get(key); err != nil {
		return nil, err
	} else {
		return parseDelegationInfo(key, data)
	}
}

// GetDelegationsList returns every stored delegation, in storage-key
// (backer address byte) order.
func GetDelegationsList(context db.DB) ([]*DelegationInfo, error) {
	iterator := context.NewIterator(delegationInfoKeyPrefix)
	defer iterator.Release()
	list := make([]*DelegationInfo, 0)
	for {
		if !iterator.Next() {
			if iterator.Error() != nil {
				return nil, iterator.Error()
			}
			break
		}

		if delegationInfo, err := parseDelegationInfo(iterator.Key(), iterator.Value()); err == nil {
			list = append(list, delegationInfo)
		} else if err == constants.ErrDataNonExistent {
			continue
		} else {
			return nil, err
		}
	}
	return list, nil
}

// LegacyPillarEntry tracks how many pillar slots remain attached to a
// legacy (swap-era) public key. KeyIdHash is the SHA-256 of the
// Bitcoin-style key id of that key and PillarCount the remaining
// registrations; RegisterLegacy decrements the count and deletes the
// entry when it reaches zero. Entries are seeded from the genesis
// configuration and stored under legacyPillarEntryKeyPrefix (3)
// followed by the 32 key-id-hash bytes.
type LegacyPillarEntry struct {
	PillarCount uint8      `json:"pillarCount"`
	KeyIdHash   types.Hash `json:"keyIdHash"`
}

// Save stores the remaining count under the legacyPillarEntryKeyPrefix
// plus key-id-hash key, returning any pack or put error; the hash is
// recovered from the key when parsing.
func (legacy *LegacyPillarEntry) Save(context db.DB) error {
	data, err := ABIPillars.PackVariable(
		legacyPillarEntryVariableName,
		legacy.PillarCount)
	if err != nil {
		return err
	}
	return context.Put(getLegacyPillarEntryKey(legacy.KeyIdHash), data)
}

// Delete removes the legacy entry once all its slots are used.
func (legacy *LegacyPillarEntry) Delete(context db.DB) error {
	return context.Delete(getLegacyPillarEntryKey(legacy.KeyIdHash))
}

func getLegacyPillarEntryKey(keyIdHash types.Hash) []byte {
	return common.JoinBytes(legacyPillarEntryKeyPrefix, keyIdHash[:])
}
func isLegacyPillarEntryKey(key []byte) bool {
	return key[0] == legacyPillarEntryKeyPrefix[0]
}
func unmarshalLegacyPillarEntryKey(key []byte) (*types.Hash, error) {
	if !isLegacyPillarEntryKey(key) {
		return nil, errors.Errorf("invalid key! Not legacy pillar key")
	}
	h := new(types.Hash)
	if err := h.SetBytes(key[1:]); err != nil {
		return nil, err
	}
	return h, nil
}
func parseLegacyPillarEntry(key, data []byte) (*LegacyPillarEntry, error) {
	if len(data) > 0 {
		dataVar := new(LegacyPillarEntry)
		if err := ABIPillars.UnpackVariable(dataVar, legacyPillarEntryVariableName, data); err != nil {
			return nil, err
		}
		if keyIdHash, err := unmarshalLegacyPillarEntryKey(key); err == nil {
			dataVar.KeyIdHash = *keyIdHash
		} else {
			return nil, err
		}
		return dataVar, nil
	} else {
		return nil, constants.ErrDataNonExistent
	}
}

// GetLegacyPillarEntry returns the legacy entry of keyIdHash, or
// constants.ErrDataNonExistent if the key has no remaining slots.
func GetLegacyPillarEntry(context db.DB, keyIdHash types.Hash) (*LegacyPillarEntry, error) {
	key := getLegacyPillarEntryKey(keyIdHash)
	if data, err := context.Get(key); err != nil {
		return nil, err
	} else {
		return parseLegacyPillarEntry(key, data)
	}
}

// GetLegacyPillarList returns every legacy entry with remaining
// slots, in storage-key (key-id-hash byte) order.
func GetLegacyPillarList(context db.DB) ([]*LegacyPillarEntry, error) {
	iterator := context.NewIterator(legacyPillarEntryKeyPrefix)
	defer iterator.Release()
	list := make([]*LegacyPillarEntry, 0)

	for {
		if !iterator.Next() {
			if iterator.Error() != nil {
				return nil, iterator.Error()
			}
			break
		}
		if len(iterator.Value()) == 0 {
			continue
		}
		if entry, err := parseLegacyPillarEntry(iterator.Key(), iterator.Value()); err == nil && entry != nil {
			list = append(list, entry)
		} else {
			return nil, err
		}
	}

	return list, nil
}

// PillarEpochHistory is the per-epoch performance record of a pillar:
// its reward-sharing percentages, the momentums it produced versus
// the number it was expected to produce, and its election weight in
// Epoch. Entries are stored under pillarEpochHistoryKeyPrefix (5)
// followed by the epoch as 8 little-endian bytes and the raw name
// bytes, so one epoch's records share a key prefix and iterate in
// name byte order.
type PillarEpochHistory struct {
	Name                         string   `json:"name"`
	Epoch                        uint64   `json:"epoch"`
	GiveBlockRewardPercentage    uint8    `json:"giveBlockRewardPercentage"`
	GiveDelegateRewardPercentage uint8    `json:"giveDelegateRewardPercentage"`
	ProducedBlockNum             int32    `json:"producedBlockNum"`
	ExpectedBlockNum             int32    `json:"expectedBlockNum"`
	Weight                       *big.Int `json:"weight"`
}

// Save stores the record under its epoch+name key, returning any pack
// or put error; epoch and name are recovered from the key when
// parsing.
func (peh *PillarEpochHistory) Save(context db.DB) error {
	data, err := ABIPillars.PackVariable(
		pillarEpochHistoryVariableName,
		peh.GiveBlockRewardPercentage,
		peh.GiveDelegateRewardPercentage,
		peh.ProducedBlockNum,
		peh.ExpectedBlockNum,
		peh.Weight)
	if err != nil {
		return err
	}
	return context.Put(getPillarEpochHistoryEntryKey(peh.Epoch, peh.Name), data)
}

func getPillarEpochHistoryPrefixKey(epoch uint64) []byte {
	epochBytes := make([]byte, 8)
	binary.LittleEndian.PutUint64(epochBytes, epoch)
	return common.JoinBytes(pillarEpochHistoryKeyPrefix, epochBytes)
}
func getPillarEpochHistoryEntryKey(epoch uint64, name string) []byte {
	return common.JoinBytes(getPillarEpochHistoryPrefixKey(epoch), []byte(name))
}
func isPillarEpochHistoryEntryKey(key []byte) bool {
	return key[0] == pillarEpochHistoryKeyPrefix[0]
}
func unmarshalPillarEpochHistoryEntryKey(key []byte) (uint64, string, error) {
	if !isPillarEpochHistoryEntryKey(key) {
		return 0, "", errors.Errorf("invalid key! Not PillarEpochHistory key")
	}
	epoch := binary.LittleEndian.Uint64(key[1:9])
	name := string(key[9:])
	return epoch, name, nil
}
func parsePillarEpochHistoryEntry(key, data []byte) (*PillarEpochHistory, error) {
	if len(data) > 0 {
		entry := new(PillarEpochHistory)
		if err := ABIPillars.UnpackVariable(entry, pillarEpochHistoryVariableName, data); err != nil {
			return nil, err
		}
		if epoch, name, err := unmarshalPillarEpochHistoryEntryKey(key); err == nil {
			entry.Epoch = epoch
			entry.Name = name
		} else {
			return nil, err
		}
		return entry, nil
	} else {
		return nil, constants.ErrDataNonExistent
	}
}

// GetPillarEpochHistoryList returns the records of every pillar for
// the given epoch, in storage-key (name byte) order; an epoch without
// records yields an empty list.
func GetPillarEpochHistoryList(context db.DB, epoch uint64) ([]*PillarEpochHistory, error) {
	iterator := context.NewIterator(getPillarEpochHistoryPrefixKey(epoch))
	defer iterator.Release()
	list := make([]*PillarEpochHistory, 0)

	for {
		if !iterator.Next() {
			if iterator.Error() != nil {
				return nil, iterator.Error()
			}
			break
		}
		if entry, err := parsePillarEpochHistoryEntry(iterator.Key(), iterator.Value()); err == nil && entry != nil {
			list = append(list, entry)
		} else {
			return nil, err
		}
	}

	return list, nil
}

// PillarEpochHistoryMarshal is the JSON form of PillarEpochHistory,
// with the weight rendered as a base-10 string to survive clients
// that parse numbers as 64-bit floats.
type PillarEpochHistoryMarshal struct {
	Name                         string `json:"name"`
	Epoch                        uint64 `json:"epoch"`
	GiveBlockRewardPercentage    uint8  `json:"giveBlockRewardPercentage"`
	GiveDelegateRewardPercentage uint8  `json:"giveDelegateRewardPercentage"`
	ProducedBlockNum             int32  `json:"producedBlockNum"`
	ExpectedBlockNum             int32  `json:"expectedBlockNum"`
	Weight                       string `json:"weight"`
}

// ToPillarEpochHistoryMarshal converts the record to its JSON form
// with a string-encoded weight.
func (g *PillarEpochHistory) ToPillarEpochHistoryMarshal() *PillarEpochHistoryMarshal {
	aux := &PillarEpochHistoryMarshal{
		Name:                         g.Name,
		Epoch:                        g.Epoch,
		GiveBlockRewardPercentage:    g.GiveBlockRewardPercentage,
		GiveDelegateRewardPercentage: g.GiveDelegateRewardPercentage,
		ProducedBlockNum:             g.ProducedBlockNum,
		ExpectedBlockNum:             g.ExpectedBlockNum,
		Weight:                       g.Weight.String(),
	}
	return aux
}

// MarshalJSON encodes the record through PillarEpochHistoryMarshal.
func (g *PillarEpochHistory) MarshalJSON() ([]byte, error) {
	return json.Marshal(g.ToPillarEpochHistoryMarshal())
}

// UnmarshalJSON decodes the record from its PillarEpochHistoryMarshal
// form, parsing the string weight back into a big.Int.
func (g *PillarEpochHistory) UnmarshalJSON(data []byte) error {
	aux := new(PillarEpochHistoryMarshal)
	if err := json.Unmarshal(data, aux); err != nil {
		return err
	}
	g.Name = aux.Name
	g.Epoch = aux.Epoch
	g.GiveBlockRewardPercentage = aux.GiveBlockRewardPercentage
	g.GiveDelegateRewardPercentage = aux.GiveDelegateRewardPercentage
	g.ProducedBlockNum = aux.ProducedBlockNum
	g.ExpectedBlockNum = aux.ExpectedBlockNum
	g.Weight = common.StringToBigInt(aux.Weight)
	return nil
}
