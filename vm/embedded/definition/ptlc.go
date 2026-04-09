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
	jsonPtlc = `
	[
		{"type":"function","name":"Create", "inputs":[
			{"name":"pointLocked","type":"address"},
			{"name":"expirationTime","type":"int64"},
			{"name":"pointLock","type":"bytes"}
		]},
		{"type":"function","name":"Reclaim","inputs":[
			{"name":"id","type":"hash"}
		]},
		{"type":"function","name":"Unlock","inputs":[
			{"name":"id","type":"hash"},
			{"name":"scalar","type":"bytes"}
		]},

		{"type":"variable","name":"ptlcInfo","inputs":[
			{"name":"timeLocked","type":"address"},
			{"name":"pointLocked","type":"address"},
			{"name":"tokenStandard","type":"tokenStandard"},
			{"name":"amount","type":"uint256"},
			{"name":"expirationTime", "type":"int64"},
			{"name":"pointLock","type":"bytes"}
		]},

		{"type":"function","name":"DenyProxyUnlock","inputs":[]},
		{"type":"function","name":"AllowProxyUnlock","inputs":[]},

		{"type":"variable","name":"ptlcProxyUnlockInfo","inputs":[
			{"name":"allowed","type":"bool"}
		]},

		{"type":"function","name":"CreateAdaptor","inputs":[
			{"name":"pointLocked","type":"address"},
			{"name":"expirationTime","type":"int64"},
			{"name":"pointLock","type":"bytes"},
			{"name":"hashType","type":"uint8"},
			{"name":"hashLock","type":"bytes"}
		]},
		{"type":"function","name":"UnlockAdaptor","inputs":[
			{"name":"id","type":"hash"},
			{"name":"scalar","type":"bytes"},
			{"name":"preimage","type":"bytes"}
		]},
		{"type":"function","name":"ReclaimAdaptor","inputs":[
			{"name":"id","type":"hash"}
		]},

		{"type":"variable","name":"adaptorPtlcInfo","inputs":[
			{"name":"timeLocked","type":"address"},
			{"name":"pointLocked","type":"address"},
			{"name":"tokenStandard","type":"tokenStandard"},
			{"name":"amount","type":"uint256"},
			{"name":"expirationTime","type":"int64"},
			{"name":"pointLock","type":"bytes"},
			{"name":"hashType","type":"uint8"},
			{"name":"hashLock","type":"bytes"}
		]}
	]`

	CreatePtlcMethodName  = "Create"
	ReclaimPtlcMethodName = "Reclaim"
	UnlockPtlcMethodName  = "Unlock"

	DenyPtlcProxyUnlockMethodName  = "DenyProxyUnlock"
	AllowPtlcProxyUnlockMethodName = "AllowProxyUnlock"

	CreateAdaptorPtlcMethodName  = "CreateAdaptor"
	UnlockAdaptorPtlcMethodName  = "UnlockAdaptor"
	ReclaimAdaptorPtlcMethodName = "ReclaimAdaptor"

	variableNamePtlcInfo            = "ptlcInfo"
	variableNamePtlcProxyUnlockInfo = "ptlcProxyUnlockInfo"
	variableNameAdaptorPtlcInfo     = "adaptorPtlcInfo"
)

var (
	ABIPtlc = abi.JSONToABIContract(strings.NewReader(jsonPtlc))

	ptlcInfoKeyPrefix            = []byte{1}
	ptlcProxyUnlockInfoKeyPrefix = []byte{2}
	adaptorPtlcInfoKeyPrefix     = []byte{3}
)

type CreatePtlcParam struct {
	PointLocked    types.Address `json:"pointLocked"`
	ExpirationTime int64         `json:"expirationTime"`
	PointLock      []byte        `json:"pointLock"`
}

type PtlcInfo struct {
	Id             types.Hash               `json:"id"`
	TimeLocked     types.Address            `json:"timeLocked"`
	PointLocked    types.Address            `json:"pointLocked"`
	TokenStandard  types.ZenonTokenStandard `json:"tokenStandard"`
	Amount         *big.Int                 `json:"amount"`
	ExpirationTime int64                    `json:"expirationTime"`
	PointLock      []byte                   `json:"pointLock"`
}

func (p *PtlcInfo) String() string {
	return fmt.Sprintf("Id:%s TimeLocked:%s PointLocked:%s TokenStandard:%s Amount:%s ExpirationTime:%d PointLock:%s", p.Id, p.TimeLocked, p.PointLocked, p.TokenStandard, p.Amount, p.ExpirationTime, base64.StdEncoding.EncodeToString(p.PointLock))
}

type UnlockPtlcParam struct {
	Id     types.Hash
	Scalar []byte
}

func (p *PtlcInfo) Save(context db.DB) error {
	data, err := ABIPtlc.PackVariable(
		variableNamePtlcInfo,
		p.TimeLocked,
		p.PointLocked,
		p.TokenStandard,
		p.Amount,
		p.ExpirationTime,
		p.PointLock,
	)
	if err != nil {
		return err
	}
	return context.Put(getPtlcInfoKey(p.Id), data)
}
func (p *PtlcInfo) Delete(context db.DB) error {
	return context.Delete(getPtlcInfoKey(p.Id))
}

func getPtlcInfoKey(hash types.Hash) []byte {
	return common.JoinBytes(ptlcInfoKeyPrefix, hash.Bytes())
}
func isPtlcInfoKey(key []byte) bool {
	return key[0] == ptlcInfoKeyPrefix[0]
}

func unmarshalPtlcInfoKey(key []byte) (*types.Hash, error) {
	if !isPtlcInfoKey(key) {
		return nil, errors.Errorf("invalid key! Not ptlc info key")
	}
	h := new(types.Hash)
	err := h.SetBytes(key[1:])
	if err != nil {
		return nil, err
	}

	return h, nil
}

func parsePtlcInfo(key, data []byte) (*PtlcInfo, error) {
	if len(data) > 0 {
		info := new(PtlcInfo)
		if err := ABIPtlc.UnpackVariable(info, variableNamePtlcInfo, data); err != nil {
			return nil, err
		}
		id, err := unmarshalPtlcInfoKey(key)
		if err != nil {
			return nil, err
		}
		info.Id = *id
		return info, nil
	} else {
		return nil, constants.ErrDataNonExistent
	}
}
func GetPtlcInfo(context db.DB, id types.Hash) (*PtlcInfo, error) {
	key := getPtlcInfoKey(id)
	if data, err := context.Get(key); err != nil {
		return nil, err
	} else {
		return parsePtlcInfo(key, data)
	}
}

type PtlcInfoMarshal struct {
	Id             types.Hash               `json:"id"`
	TimeLocked     types.Address            `json:"timeLocked"`
	PointLocked    types.Address            `json:"pointLocked"`
	TokenStandard  types.ZenonTokenStandard `json:"tokenStandard"`
	Amount         string                   `json:"amount"`
	ExpirationTime int64                    `json:"expirationTime"`
	PointLock      []byte                   `json:"pointLock"`
}

func (p *PtlcInfo) ToPtlcInfoMarshal() *PtlcInfoMarshal {
	aux := &PtlcInfoMarshal{
		Id:             p.Id,
		TimeLocked:     p.TimeLocked,
		PointLocked:    p.PointLocked,
		TokenStandard:  p.TokenStandard,
		Amount:         p.Amount.String(),
		ExpirationTime: p.ExpirationTime,
		PointLock:      p.PointLock,
	}

	return aux
}

func (p *PtlcInfo) MarshalJSON() ([]byte, error) {
	return json.Marshal(p.ToPtlcInfoMarshal())
}

func (p *PtlcInfo) UnmarshalJSON(data []byte) error {
	aux := new(PtlcInfoMarshal)
	if err := json.Unmarshal(data, aux); err != nil {
		return err
	}
	p.Id = aux.Id
	p.TimeLocked = aux.TimeLocked
	p.PointLocked = aux.PointLocked
	p.TokenStandard = aux.TokenStandard
	p.Amount = common.StringToBigInt(aux.Amount)
	p.ExpirationTime = aux.ExpirationTime
	p.PointLock = aux.PointLock
	return nil
}

type PtlcProxyUnlockInfo struct {
	Address types.Address
	Allowed bool
}

func (entry *PtlcProxyUnlockInfo) Save(context db.DB) error {
	data, err := ABIPtlc.PackVariable(
		variableNamePtlcProxyUnlockInfo,
		entry.Allowed,
	)
	if err != nil {
		return err
	}
	return context.Put(getPtlcProxyUnlockInfoKey(entry.Address), data)
}
func (entry *PtlcProxyUnlockInfo) Delete(context db.DB) error {
	key := getPtlcProxyUnlockInfoKey(entry.Address)
	return context.Delete(key)
}

func getPtlcProxyUnlockInfoKey(address types.Address) []byte {
	return common.JoinBytes(ptlcProxyUnlockInfoKeyPrefix, address.Bytes())
}
func isPtlcProxyUnlockInfoKey(key []byte) bool {
	return key[0] == ptlcProxyUnlockInfoKeyPrefix[0]
}
func unmarshalPtlcProxyUnlockInfoKey(key []byte) (*types.Address, error) {
	if !isPtlcProxyUnlockInfoKey(key) {
		return nil, errors.Errorf("invalid key! Not ptlc proxy-unlock info key")
	}
	a := new(types.Address)
	err := a.SetBytes(key[1:])
	if err != nil {
		return nil, err
	}
	return a, nil
}
func parsePtlcProxyUnlockInfo(key, data []byte) (*PtlcProxyUnlockInfo, error) {
	if len(data) > 0 {
		info := new(PtlcProxyUnlockInfo)
		if err := ABIPtlc.UnpackVariable(info, variableNamePtlcProxyUnlockInfo, data); err != nil {
			return nil, err
		}
		address, err := unmarshalPtlcProxyUnlockInfoKey(key)
		if err != nil {
			return nil, err
		}
		info.Address = *address
		return info, nil
	} else {
		return nil, constants.ErrDataNonExistent
	}
}
func GetPtlcProxyUnlockInfo(context db.DB, address types.Address) (*PtlcProxyUnlockInfo, error) {
	key := getPtlcProxyUnlockInfoKey(address)
	if data, err := context.Get(key); err != nil {
		return nil, err
	} else {
		return parsePtlcProxyUnlockInfo(key, data)
	}
}

// ============================================================
// Adaptor PTLC — dual-lock (point lock + hash lock)
// ============================================================

type CreateAdaptorPtlcParam struct {
	PointLocked    types.Address `json:"pointLocked"`
	ExpirationTime int64         `json:"expirationTime"`
	PointLock      []byte        `json:"pointLock"`
	HashType       uint8         `json:"hashType"`
	HashLock       []byte        `json:"hashLock"`
}

type UnlockAdaptorPtlcParam struct {
	Id       types.Hash
	Scalar   []byte
	Preimage []byte
}

type AdaptorPtlcInfo struct {
	Id             types.Hash               `json:"id"`
	TimeLocked     types.Address            `json:"timeLocked"`
	PointLocked    types.Address            `json:"pointLocked"`
	TokenStandard  types.ZenonTokenStandard `json:"tokenStandard"`
	Amount         *big.Int                 `json:"amount"`
	ExpirationTime int64                    `json:"expirationTime"`
	PointLock      []byte                   `json:"pointLock"`
	HashType       uint8                    `json:"hashType"`
	HashLock       []byte                   `json:"hashLock"`
}

func (a *AdaptorPtlcInfo) String() string {
	return fmt.Sprintf("Id:%s TimeLocked:%s PointLocked:%s TokenStandard:%s Amount:%s ExpirationTime:%d PointLock:%s HashType:%d HashLock:%s",
		a.Id, a.TimeLocked, a.PointLocked, a.TokenStandard, a.Amount, a.ExpirationTime,
		base64.StdEncoding.EncodeToString(a.PointLock), a.HashType,
		base64.StdEncoding.EncodeToString(a.HashLock))
}

func (a *AdaptorPtlcInfo) Save(context db.DB) error {
	data, err := ABIPtlc.PackVariable(
		variableNameAdaptorPtlcInfo,
		a.TimeLocked,
		a.PointLocked,
		a.TokenStandard,
		a.Amount,
		a.ExpirationTime,
		a.PointLock,
		a.HashType,
		a.HashLock,
	)
	if err != nil {
		return err
	}
	return context.Put(getAdaptorPtlcInfoKey(a.Id), data)
}
func (a *AdaptorPtlcInfo) Delete(context db.DB) error {
	return context.Delete(getAdaptorPtlcInfoKey(a.Id))
}

func getAdaptorPtlcInfoKey(hash types.Hash) []byte {
	return common.JoinBytes(adaptorPtlcInfoKeyPrefix, hash.Bytes())
}
func isAdaptorPtlcInfoKey(key []byte) bool {
	return key[0] == adaptorPtlcInfoKeyPrefix[0]
}

func unmarshalAdaptorPtlcInfoKey(key []byte) (*types.Hash, error) {
	if !isAdaptorPtlcInfoKey(key) {
		return nil, errors.Errorf("invalid key! Not adaptor ptlc info key")
	}
	h := new(types.Hash)
	err := h.SetBytes(key[1:])
	if err != nil {
		return nil, err
	}
	return h, nil
}

func parseAdaptorPtlcInfo(key, data []byte) (*AdaptorPtlcInfo, error) {
	if len(data) > 0 {
		info := new(AdaptorPtlcInfo)
		if err := ABIPtlc.UnpackVariable(info, variableNameAdaptorPtlcInfo, data); err != nil {
			return nil, err
		}
		id, err := unmarshalAdaptorPtlcInfoKey(key)
		if err != nil {
			return nil, err
		}
		info.Id = *id
		return info, nil
	} else {
		return nil, constants.ErrDataNonExistent
	}
}
func GetAdaptorPtlcInfo(context db.DB, id types.Hash) (*AdaptorPtlcInfo, error) {
	key := getAdaptorPtlcInfoKey(id)
	if data, err := context.Get(key); err != nil {
		return nil, err
	} else {
		return parseAdaptorPtlcInfo(key, data)
	}
}

type AdaptorPtlcInfoMarshal struct {
	Id             types.Hash               `json:"id"`
	TimeLocked     types.Address            `json:"timeLocked"`
	PointLocked    types.Address            `json:"pointLocked"`
	TokenStandard  types.ZenonTokenStandard `json:"tokenStandard"`
	Amount         string                   `json:"amount"`
	ExpirationTime int64                    `json:"expirationTime"`
	PointLock      []byte                   `json:"pointLock"`
	HashType       uint8                    `json:"hashType"`
	HashLock       []byte                   `json:"hashLock"`
}

func (a *AdaptorPtlcInfo) ToAdaptorPtlcInfoMarshal() *AdaptorPtlcInfoMarshal {
	return &AdaptorPtlcInfoMarshal{
		Id:             a.Id,
		TimeLocked:     a.TimeLocked,
		PointLocked:    a.PointLocked,
		TokenStandard:  a.TokenStandard,
		Amount:         a.Amount.String(),
		ExpirationTime: a.ExpirationTime,
		PointLock:      a.PointLock,
		HashType:       a.HashType,
		HashLock:       a.HashLock,
	}
}

func (a *AdaptorPtlcInfo) MarshalJSON() ([]byte, error) {
	return json.Marshal(a.ToAdaptorPtlcInfoMarshal())
}

func (a *AdaptorPtlcInfo) UnmarshalJSON(data []byte) error {
	aux := new(AdaptorPtlcInfoMarshal)
	if err := json.Unmarshal(data, aux); err != nil {
		return err
	}
	a.Id = aux.Id
	a.TimeLocked = aux.TimeLocked
	a.PointLocked = aux.PointLocked
	a.TokenStandard = aux.TokenStandard
	a.Amount = common.StringToBigInt(aux.Amount)
	a.ExpirationTime = aux.ExpirationTime
	a.PointLock = aux.PointLock
	a.HashType = aux.HashType
	a.HashLock = aux.HashLock
	return nil
}
