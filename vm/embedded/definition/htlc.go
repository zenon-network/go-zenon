package definition

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
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
	// jsonHtlc is the ABI JSON of the HTLC embedded contract: entry
	// creation, reclaim and unlock, the proxy-unlock toggles, and the
	// stored entry and proxy-unlock variables. Parsed into ABIHtlc.
	jsonHtlc = `
	[
		{"type":"function","name":"Create", "inputs":[
			{"name":"hashLocked","type":"address"},
			{"name":"expirationTime","type":"int64"},
			{"name":"hashType","type":"uint8"},
			{"name":"keyMaxSize","type":"uint8"},
			{"name":"hashLock","type":"bytes"}
		]},
		{"type":"function","name":"Reclaim","inputs":[
			{"name":"id","type":"hash"}
		]},
		{"type":"function","name":"Unlock","inputs":[
			{"name":"id","type":"hash"},
			{"name":"preimage","type":"bytes"}
		]},

		{"type":"variable","name":"htlcInfo","inputs":[
			{"name":"timeLocked","type":"address"},
			{"name":"hashLocked","type":"address"},
			{"name":"tokenStandard","type":"tokenStandard"},
			{"name":"amount","type":"uint256"},
			{"name":"expirationTime", "type":"int64"},
			{"name":"hashType","type":"uint8"},
			{"name":"keyMaxSize","type":"uint8"},
			{"name":"hashLock","type":"bytes"}
		]},

		{"type":"function","name":"DenyProxyUnlock","inputs":[]},
		{"type":"function","name":"AllowProxyUnlock","inputs":[]},

		{"type":"variable","name":"htlcProxyUnlockInfo","inputs":[
			{"name":"allowed","type":"bool"}
		]}
	]`

	// CreateHtlcMethodName names the method (Create) that locks the
	// sent tokens for a hash-locked counterparty until an expiration
	// time; the entry id is the hash of the send block.
	CreateHtlcMethodName = "Create"
	// ReclaimHtlcMethodName names the method (Reclaim) by which the
	// creator recovers the funds of an expired entry.
	ReclaimHtlcMethodName = "Reclaim"
	// UnlockHtlcMethodName names the method (Unlock) that releases
	// the funds to the hash-locked address before expiration, upon a
	// preimage whose digest matches the entry's hash lock.
	UnlockHtlcMethodName = "Unlock"

	// DenyHtlcProxyUnlockMethodName names the method
	// (DenyProxyUnlock) by which an address forbids third parties to
	// unlock entries hash-locked to it.
	DenyHtlcProxyUnlockMethodName = "DenyProxyUnlock"
	// AllowHtlcProxyUnlockMethodName names the method
	// (AllowProxyUnlock) by which an address permits third parties to
	// unlock entries hash-locked to it, which is also the behavior
	// for addresses that never called either method.
	AllowHtlcProxyUnlockMethodName = "AllowProxyUnlock"

	// re: reclaim vs revoke
	// some other embedded contracts have "revoke" methods
	// indicating an action which invalidates an entry and returns funds
	// for htlcs, we invalidate unlocking via preimage as soon as soon as the expiration time arrives
	// however the funds still sit in the contract and exist as an entry, so we use "reclaim"

	variableNameHtlcInfo            = "htlcInfo"
	variableNameHtlcProxyUnlockInfo = "htlcProxyUnlockInfo"
)

const (
	// HashTypeSHA3 (0) marks a hash lock computed with SHA3-256.
	HashTypeSHA3 uint8 = iota
	// HashTypeSHA256 (1) marks a hash lock computed with SHA-256.
	HashTypeSHA256
)

// HashTypeDigestSizes maps each hash type to its digest length in
// bytes (32 for both); Create rejects hash locks of any other
// length.
var HashTypeDigestSizes = map[uint8]uint8{
	HashTypeSHA3:   32,
	HashTypeSHA256: 32,
}

var (
	// ABIHtlc is the parsed ABI of the HTLC embedded contract.
	ABIHtlc = abi.JSONToABIContract(strings.NewReader(jsonHtlc))

	htlcInfoKeyPrefix            = []byte{1}
	htlcProxyUnlockInfoKeyPrefix = []byte{2}
)

// CreateHtlcParam carries the arguments of Create: the hash-locked
// counterparty, the expiration time (unix seconds), the hash type
// (HashTypeSHA3 or HashTypeSHA256), the maximum accepted preimage
// size in bytes and the hash lock itself; the locked token and
// amount come from the send block.
type CreateHtlcParam struct {
	HashLocked     types.Address `json:"hashLocked"`
	ExpirationTime int64         `json:"expirationTime"`
	HashType       uint8         `json:"hashType"`
	KeyMaxSize     uint8         `json:"keyMaxSize"`
	HashLock       []byte        `json:"hashLock"`
}

// HtlcInfo is one stored hash time-locked contract. TimeLocked is
// the creator, who may Reclaim the funds once ExpirationTime (unix
// seconds) has passed; HashLocked is the counterparty entitled to
// Unlock them before expiration with a preimage of at most
// KeyMaxSize bytes whose HashType digest equals HashLock. Amount is
// in the token's smallest units and Id is the hash of the Create
// send block. Entries are stored under htlcInfoKeyPrefix (1)
// followed by the 32-byte id; the id is recovered from the key when
// parsing.
type HtlcInfo struct {
	Id             types.Hash               `json:"id"`
	TimeLocked     types.Address            `json:"timeLocked"`
	HashLocked     types.Address            `json:"hashLocked"`
	TokenStandard  types.ZenonTokenStandard `json:"tokenStandard"`
	Amount         *big.Int                 `json:"amount"`
	ExpirationTime int64                    `json:"expirationTime"`
	HashType       uint8                    `json:"hashType"`
	KeyMaxSize     uint8                    `json:"keyMaxSize"`
	HashLock       []byte                   `json:"hashLock"`
}

// String renders the entry on a single line, with the hash lock
// base64-encoded.
func (h *HtlcInfo) String() string {
	return fmt.Sprintf("Id:%s TimeLocked:%s HashLocked:%s TokenStandard:%s Amount:%s ExpirationTime:%d HashType:%d KeyMaxSize:%d HashLock:%s", h.Id, h.TimeLocked, h.HashLocked, h.TokenStandard, h.Amount, h.ExpirationTime, h.HashType, h.KeyMaxSize, base64.StdEncoding.EncodeToString(h.HashLock))
}

// UnlockHtlcParam carries the arguments of Unlock: the entry id and
// the preimage of its hash lock.
type UnlockHtlcParam struct {
	Id       types.Hash
	Preimage []byte
}

// Save stores the entry under its id key, packing all fields except
// the id, and returns any pack or put error.
func (h *HtlcInfo) Save(context db.DB) error {
	data, err := ABIHtlc.PackVariable(
		variableNameHtlcInfo,
		h.TimeLocked,
		h.HashLocked,
		h.TokenStandard,
		h.Amount,
		h.ExpirationTime,
		h.HashType,
		h.KeyMaxSize,
		h.HashLock,
	)
	if err != nil {
		return err
	}
	return context.Put(getHtlcInfoKey(h.Id), data)
}

// Delete removes the entry; Reclaim and Unlock call it after moving
// the funds.
func (h *HtlcInfo) Delete(context db.DB) error {
	return context.Delete(getHtlcInfoKey(h.Id))
}

func getHtlcInfoKey(hash types.Hash) []byte {
	return common.JoinBytes(htlcInfoKeyPrefix, hash.Bytes())
}
func isHtlcInfoKey(key []byte) bool {
	return key[0] == htlcInfoKeyPrefix[0]
}

func unmarshalHtlcInfoKey(key []byte) (*types.Hash, error) {
	if !isHtlcInfoKey(key) {
		return nil, errors.Errorf("invalid key! Not htlc info key")
	}
	h := new(types.Hash)
	err := h.SetBytes(key[1:])
	if err != nil {
		return nil, err
	}

	return h, nil
}

func parseHtlcInfo(key, data []byte) (*HtlcInfo, error) {
	if len(data) > 0 {
		info := new(HtlcInfo)
		if err := ABIHtlc.UnpackVariable(info, variableNameHtlcInfo, data); err != nil {
			return nil, err
		}
		id, err := unmarshalHtlcInfoKey(key)
		if err != nil {
			return nil, err
		}
		info.Id = *id
		return info, nil
	} else {
		return nil, constants.ErrDataNonExistent
	}
}

// GetHtlcInfo returns the entry stored under id, or
// constants.ErrDataNonExistent if it never existed or has already
// been reclaimed or unlocked.
func GetHtlcInfo(context db.DB, id types.Hash) (*HtlcInfo, error) {
	key := getHtlcInfoKey(id)
	if data, err := context.Get(key); err != nil {
		return nil, err
	} else {
		return parseHtlcInfo(key, data)
	}
}

// HtlcInfoMarshal is the JSON form of HtlcInfo, with the amount
// rendered as a base-10 string to survive clients that parse numbers
// as 64-bit floats.
type HtlcInfoMarshal struct {
	Id             types.Hash               `json:"id"`
	TimeLocked     types.Address            `json:"timeLocked"`
	HashLocked     types.Address            `json:"hashLocked"`
	TokenStandard  types.ZenonTokenStandard `json:"tokenStandard"`
	Amount         string                   `json:"amount"`
	ExpirationTime int64                    `json:"expirationTime"`
	HashType       uint8                    `json:"hashType"`
	KeyMaxSize     uint8                    `json:"keyMaxSize"`
	HashLock       []byte                   `json:"hashLock"`
}

// ToHtlcInfoMarshal converts the entry to its JSON form with a
// string-encoded amount.
func (h *HtlcInfo) ToHtlcInfoMarshal() *HtlcInfoMarshal {
	aux := &HtlcInfoMarshal{
		Id:             h.Id,
		TimeLocked:     h.TimeLocked,
		HashLocked:     h.HashLocked,
		TokenStandard:  h.TokenStandard,
		Amount:         h.Amount.String(),
		ExpirationTime: h.ExpirationTime,
		HashType:       h.HashType,
		KeyMaxSize:     h.KeyMaxSize,
		HashLock:       h.HashLock,
	}

	return aux
}

// MarshalJSON encodes the entry through HtlcInfoMarshal.
func (h *HtlcInfo) MarshalJSON() ([]byte, error) {
	return json.Marshal(h.ToHtlcInfoMarshal())
}

// UnmarshalJSON decodes the entry from its HtlcInfoMarshal form,
// parsing the string amount back into a big.Int.
func (h *HtlcInfo) UnmarshalJSON(data []byte) error {
	aux := new(HtlcInfoMarshal)
	if err := json.Unmarshal(data, aux); err != nil {
		return err
	}
	h.Id = aux.Id
	h.TimeLocked = aux.TimeLocked
	h.HashLocked = aux.HashLocked
	h.TimeLocked = aux.TimeLocked
	h.TokenStandard = aux.TokenStandard
	h.Amount = common.StringToBigInt(aux.Amount)
	h.ExpirationTime = aux.ExpirationTime
	h.HashType = aux.HashType
	h.KeyMaxSize = aux.KeyMaxSize
	h.TimeLocked = aux.TimeLocked
	h.HashLock = aux.HashLock
	return nil
}

// HtlcProxyUnlockInfo records whether Address permits proxy
// unlocking, that is, third parties unlocking entries hash-locked to
// it. AllowProxyUnlock and DenyProxyUnlock write the flag and
// neither deletes it, so an address that has chosen once never
// returns to the default (a missing entry, which the implementation
// treats as allowed). Entries are stored under
// htlcProxyUnlockInfoKeyPrefix (2) followed by the 20-byte address;
// only the flag is packed, the address is recovered from the key.
type HtlcProxyUnlockInfo struct {
	Address types.Address
	Allowed bool
}

// Save stores the flag under the address key, returning any pack or
// put error.
func (entry *HtlcProxyUnlockInfo) Save(context db.DB) error {
	data, err := ABIHtlc.PackVariable(
		variableNameHtlcProxyUnlockInfo,
		entry.Allowed,
	)
	if err != nil {
		return err
	}
	return context.Put(getHtlcProxyUnlockInfoKey(entry.Address), data)
}

// Delete removes the address's entry, restoring the default; no
// contract method currently does this.
func (entry *HtlcProxyUnlockInfo) Delete(context db.DB) error {
	key := getHtlcProxyUnlockInfoKey(entry.Address)
	return context.Delete(key)
}

func getHtlcProxyUnlockInfoKey(address types.Address) []byte {
	return common.JoinBytes(htlcProxyUnlockInfoKeyPrefix, address.Bytes())
}
func isHtlcProxyUnlockInfoKey(key []byte) bool {
	return key[0] == htlcProxyUnlockInfoKeyPrefix[0]
}
func unmarshalHtlcProxyUnlockInfoKey(key []byte) (*types.Address, error) {
	if !isHtlcProxyUnlockInfoKey(key) {
		return nil, errors.Errorf("invalid key! Not htlc proxy-unlock info key")
	}
	a := new(types.Address)
	err := a.SetBytes(key[1:])
	if err != nil {
		return nil, err
	}
	return a, nil
}
func parseHtlcProxyUnlockInfo(key, data []byte) (*HtlcProxyUnlockInfo, error) {
	if len(data) > 0 {
		info := new(HtlcProxyUnlockInfo)
		if err := ABIHtlc.UnpackVariable(info, variableNameHtlcProxyUnlockInfo, data); err != nil {
			return nil, err
		}
		address, err := unmarshalHtlcProxyUnlockInfoKey(key)
		if err != nil {
			return nil, err
		}
		info.Address = *address
		return info, nil
	} else {
		return nil, constants.ErrDataNonExistent
	}
}

// GetHtlcProxyUnlockInfo returns the proxy-unlock entry of address,
// or constants.ErrDataNonExistent if the address never called
// AllowProxyUnlock or DenyProxyUnlock; callers treat the missing
// entry as allowed.
func GetHtlcProxyUnlockInfo(context db.DB, address types.Address) (*HtlcProxyUnlockInfo, error) {
	key := getHtlcProxyUnlockInfoKey(address)
	if data, err := context.Get(key); err != nil {
		return nil, err
	} else {
		return parseHtlcProxyUnlockInfo(key, data)
	}
}
