package definition

import (
	"encoding/base64"
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
			{"name":"expirationTime","type":"int64"},
			{"name":"pointType","type":"uint8"},
			{"name":"pointLock","type":"bytes"}
		]},
		{"type":"function","name":"Reclaim","inputs":[
			{"name":"id","type":"hash"}
		]},
		{"type":"function","name":"Unlock","inputs":[
			{"name":"id","type":"hash"},
			{"name":"signature","type":"bytes"}
		]},
		{"type":"function","name":"ProxyUnlock","inputs":[
			{"name":"id","type":"hash"},
			{"name":"destination","type":"address"},
			{"name":"signature","type":"bytes"}
		]},
		{"type":"variable","name":"ptlcInfo","inputs":[
			{"name":"timeLocked","type":"address"},
			{"name":"tokenStandard","type":"tokenStandard"},
			{"name":"amount","type":"uint256"},
			{"name":"expirationTime", "type":"int64"},
			{"name":"pointType","type":"uint8"},
			{"name":"pointLock","type":"bytes"}
		]}
	]`

	CreatePtlcMethodName      = "Create"
	ReclaimPtlcMethodName     = "Reclaim"
	UnlockPtlcMethodName      = "Unlock"
	ProxyUnlockPtlcMethodName = "ProxyUnlock"

	variableNamePtlcInfo = "ptlcInfo"
)

const (
	PointTypeED25519 uint8 = iota
	PointTypeBIP340
)

var PointTypePubKeySizes = map[uint8]uint8{
	PointTypeED25519: 32,
	PointTypeBIP340:  32,
}

var PointTypeSignatureSizes = map[uint8]uint8{
	PointTypeED25519: 64,
	PointTypeBIP340:  64,
}

var (
	ABIPtlc = abi.JSONToABIContract(strings.NewReader(jsonPtlc))

	ptlcInfoKeyPrefix = []byte{1}
)

type CreatePtlcParam struct {
	ExpirationTime int64  `json:"expirationTime"`
	PointType      uint8  `json:"pointType"`
	PointLock      []byte `json:"pointLock"`
}

type PtlcInfo struct {
	Id             types.Hash               `json:"id"`
	TimeLocked     types.Address            `json:"timeLocked"`
	TokenStandard  types.ZenonTokenStandard `json:"tokenStandard"`
	Amount         *big.Int                 `json:"amount"`
	ExpirationTime int64                    `json:"expirationTime"`
	PointType      uint8                    `json:"pointType"`
	PointLock      []byte                   `json:"pointLock"`
}

func (p PtlcInfo) String() string {
	return fmt.Sprintf("Id:%s TimeLocked:%s TokenStandard:%s Amount:%s ExpirationTime:%d PointType:%d PointLock:%s ", p.Id, p.TimeLocked, p.TokenStandard, p.Amount, p.ExpirationTime, p.PointType, base64.StdEncoding.EncodeToString(p.PointLock))
}

type UnlockPtlcParam struct {
	Id        types.Hash
	Signature []byte
}

type ProxyUnlockPtlcParam struct {
	Id          types.Hash
	Destination types.Address
	Signature   []byte
}

func (entry *PtlcInfo) Save(context db.DB) error {
	data, err := ABIPtlc.PackVariable(
		variableNamePtlcInfo,
		entry.TimeLocked,
		entry.TokenStandard,
		entry.Amount,
		entry.ExpirationTime,
		entry.PointType,
		entry.PointLock,
	)
	if err != nil {
		return err
	}
	return context.Put(getPtlcInfoKey(entry.Id), data)
}
func (entry *PtlcInfo) Delete(context db.DB) error {
	return context.Delete(getPtlcInfoKey(entry.Id))
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
