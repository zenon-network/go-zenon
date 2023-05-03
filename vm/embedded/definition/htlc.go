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

	CreateHtlcMethodName  = "Create"
	ReclaimHtlcMethodName = "Reclaim"
	UnlockHtlcMethodName  = "Unlock"

	DenyHtlcProxyUnlockMethodName  = "DenyProxyUnlock"
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
	HashTypeSHA3 uint8 = iota
	HashTypeSHA256
)

var HashTypeDigestSizes = map[uint8]uint8{
	HashTypeSHA3:   32,
	HashTypeSHA256: 32,
}

var (
	ABIHtlc = abi.JSONToABIContract(strings.NewReader(jsonHtlc))

	htlcInfoKeyPrefix            = []byte{1}
	htlcProxyUnlockInfoKeyPrefix = []byte{2}
)

type CreateHtlcParam struct {
	HashLocked     types.Address `json:"hashLocked"`
	ExpirationTime int64         `json:"expirationTime"`
	HashType       uint8         `json:"hashType"`
	KeyMaxSize     uint8         `json:"keyMaxSize"`
	HashLock       []byte        `json:"hashLock"`
}

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

func (h *HtlcInfo) String() string {
	return fmt.Sprintf("Id:%s TimeLocked:%s HashLocked:%s TokenStandard:%s Amount:%s ExpirationTime:%d HashType:%d KeyMaxSize:%d HashLock:%s", h.Id, h.TimeLocked, h.HashLocked, h.TokenStandard, h.Amount, h.ExpirationTime, h.HashType, h.KeyMaxSize, base64.StdEncoding.EncodeToString(h.HashLock))
}

type UnlockHtlcParam struct {
	Id       types.Hash
	Preimage []byte
}

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
func GetHtlcInfo(context db.DB, id types.Hash) (*HtlcInfo, error) {
	key := getHtlcInfoKey(id)
	if data, err := context.Get(key); err != nil {
		return nil, err
	} else {
		return parseHtlcInfo(key, data)
	}
}

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

func (h *HtlcInfo) MarshalJSON() ([]byte, error) {
	return json.Marshal(h.ToHtlcInfoMarshal())
}

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

type HtlcProxyUnlockInfo struct {
	Address types.Address
	Allowed bool
}

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
func GetHtlcProxyUnlockInfo(context db.DB, address types.Address) (*HtlcProxyUnlockInfo, error) {
	key := getHtlcProxyUnlockInfoKey(address)
	if data, err := context.Get(key); err != nil {
		return nil, err
	} else {
		return parseHtlcProxyUnlockInfo(key, data)
	}
}
