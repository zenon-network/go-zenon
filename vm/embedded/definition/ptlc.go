package definition

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"strings"

	"github.com/pkg/errors"

	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/crypto"
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
			{"name":"pointLock","type":"bytes"},
			{"name":"destination","type":"address"}
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
		{"type":"function","name":"CreateWithReclaimLock", "inputs":[
			{"name":"expirationTime","type":"int64"},
			{"name":"pointType","type":"uint8"},
			{"name":"pointLock","type":"bytes"},
			{"name":"destination","type":"address"},
			{"name":"reclaimPointType","type":"uint8"},
			{"name":"reclaimPointLock","type":"bytes"}
		]},
		{"type":"function","name":"ReclaimWithSignature","inputs":[
			{"name":"id","type":"hash"},
			{"name":"signature","type":"bytes"}
		]},
		{"type":"variable","name":"ptlcInfo","inputs":[
			{"name":"timeLocked","type":"address"},
			{"name":"tokenStandard","type":"tokenStandard"},
			{"name":"amount","type":"uint256"},
			{"name":"expirationTime", "type":"int64"},
			{"name":"pointType","type":"uint8"},
			{"name":"pointLock","type":"bytes"},
			{"name":"destination","type":"address"}
		]},
		{"type":"variable","name":"ptlcReclaimLock","inputs":[
			{"name":"pointType","type":"uint8"},
			{"name":"pointLock","type":"bytes"}
		]}
	]`

	// CreatePtlcMethodName names the method that locks the sent tokens
	// behind a point.
	CreatePtlcMethodName = "Create"
	// ReclaimPtlcMethodName names the method by which the creator takes the
	// tokens back after expiry.
	ReclaimPtlcMethodName = "Reclaim"
	// UnlockPtlcMethodName names the method that pays the tokens to the
	// caller against a witness for the point.
	UnlockPtlcMethodName = "Unlock"
	// ProxyUnlockPtlcMethodName names the method that pays the tokens to a
	// named destination against a witness bound to that destination.
	ProxyUnlockPtlcMethodName = "ProxyUnlock"

	CreateWithReclaimLockPtlcMethodName = "CreateWithReclaimLock"
	ReclaimWithSignaturePtlcMethodName  = "ReclaimWithSignature"

	variableNamePtlcInfo = "ptlcInfo"

	variableNamePtlcReclaimLock = "ptlcReclaimLock"

	// PtlcUnlockMessageDomain is the domain-separation prefix of the
	// message an unlock signature covers (see GetPtlcUnlockMessage).
	PtlcUnlockMessageDomain = "zenon-ptlc-unlock:v1"

	PtlcReclaimMessageDomain = "zenon-ptlc-reclaim:v1"
)

const (
	// PointTypeED25519 locks to an Ed25519 public key, unlocked by an
	// Ed25519 signature.
	PointTypeED25519 uint8 = iota
	// PointTypeBIP340 locks to a BIP-340 x-only public key, unlocked by a
	// Schnorr signature.
	PointTypeBIP340
	// PointTypeSecp256k1Point locks to a compressed secp256k1 point, unlocked
	// by revealing its scalar.
	PointTypeSecp256k1Point
)

// PointTypePubKeySizes gives the byte length of a valid point lock per
// point type.
var PointTypePubKeySizes = map[uint8]uint8{
	PointTypeED25519:        32,
	PointTypeBIP340:         32,
	PointTypeSecp256k1Point: 33,
}

// PointTypeWitnessSizes gives the byte length of a valid unlock witness
// per point type: a 64-byte signature, or the 32-byte scalar for a
// secp256k1 point.
var PointTypeWitnessSizes = map[uint8]uint8{
	PointTypeED25519:        64,
	PointTypeBIP340:         64,
	PointTypeSecp256k1Point: 32,
}

var (
	// ABIPtlc is the parsed ABI of the PTLC embedded contract.
	ABIPtlc = abi.JSONToABIContract(strings.NewReader(jsonPtlc))

	ptlcInfoKeyPrefix = []byte{1}

	ptlcReclaimLockKeyPrefix = []byte{2}
)

// CreatePtlcParam is the argument of Create: the unix time after which
// the creator may reclaim, the point type and point lock, and an
// optional fixed destination (required for secp256k1 points, since
// their witness does not commit to one).
type CreatePtlcParam struct {
	ExpirationTime int64         `json:"expirationTime"`
	PointType      uint8         `json:"pointType"`
	PointLock      []byte        `json:"pointLock"`
	Destination    types.Address `json:"destination"`
}

// PtlcInfo is one stored PTLC entry: the creator (TimeLocked), the
// locked token and amount (smallest units), the expiration time, the
// point the funds are locked to, and the destination they may be
// unlocked to (zero means any payable address). Id is the hash of the
// Create send block and is recovered from the storage key.
type PtlcInfo struct {
	Id             types.Hash               `json:"id"`
	TimeLocked     types.Address            `json:"timeLocked"`
	TokenStandard  types.ZenonTokenStandard `json:"tokenStandard"`
	Amount         *big.Int                 `json:"amount"`
	ExpirationTime int64                    `json:"expirationTime"`
	PointType      uint8                    `json:"pointType"`
	PointLock      []byte                   `json:"pointLock"`
	Destination    types.Address            `json:"destination"`
}

// PtlcInfoMarshal is the JSON wire form of PtlcInfo, with Amount as a
// base-10 string.
type PtlcInfoMarshal struct {
	Id             types.Hash               `json:"id"`
	TimeLocked     types.Address            `json:"timeLocked"`
	TokenStandard  types.ZenonTokenStandard `json:"tokenStandard"`
	Amount         string                   `json:"amount"`
	ExpirationTime int64                    `json:"expirationTime"`
	PointType      uint8                    `json:"pointType"`
	PointLock      []byte                   `json:"pointLock"`
	Destination    types.Address            `json:"destination"`
}

// ToPtlcInfoMarshal converts the entry to its JSON wire form.
func (p *PtlcInfo) ToPtlcInfoMarshal() *PtlcInfoMarshal {
	return &PtlcInfoMarshal{
		Id:             p.Id,
		TimeLocked:     p.TimeLocked,
		TokenStandard:  p.TokenStandard,
		Amount:         p.Amount.String(),
		ExpirationTime: p.ExpirationTime,
		PointType:      p.PointType,
		PointLock:      p.PointLock,
		Destination:    p.Destination,
	}
}

// MarshalJSON encodes the entry via PtlcInfoMarshal.
func (p *PtlcInfo) MarshalJSON() ([]byte, error) {
	return json.Marshal(p.ToPtlcInfoMarshal())
}

// UnmarshalJSON decodes the PtlcInfoMarshal form; an amount that is not
// a base-10 integer is an error.
func (p *PtlcInfo) UnmarshalJSON(data []byte) error {
	aux := new(PtlcInfoMarshal)
	if err := json.Unmarshal(data, aux); err != nil {
		return err
	}
	amount, ok := new(big.Int).SetString(aux.Amount, 10)
	if !ok {
		return errors.Errorf("invalid amount %q", aux.Amount)
	}
	p.Id = aux.Id
	p.TimeLocked = aux.TimeLocked
	p.TokenStandard = aux.TokenStandard
	p.Amount = amount
	p.ExpirationTime = aux.ExpirationTime
	p.PointType = aux.PointType
	p.PointLock = aux.PointLock
	p.Destination = aux.Destination
	return nil
}

// String renders the entry on one line for logs, with the point lock in
// base64.
func (p PtlcInfo) String() string {
	return fmt.Sprintf("Id:%s TimeLocked:%s TokenStandard:%s Amount:%s ExpirationTime:%d PointType:%d PointLock:%s Destination:%s ", p.Id, p.TimeLocked, p.TokenStandard, p.Amount, p.ExpirationTime, p.PointType, base64.StdEncoding.EncodeToString(p.PointLock), p.Destination)
}

// UnlockPtlcParam is the argument of Unlock: the entry id and the
// witness for its point (a signature over the unlock message bound to
// the caller, or the scalar for a secp256k1 point).
type UnlockPtlcParam struct {
	Id        types.Hash
	Signature []byte
}

// ProxyUnlockPtlcParam is the argument of ProxyUnlock: the entry id, the
// address to pay, and the witness for the entry's point (a signature
// over the unlock message bound to that address, or the scalar for a
// secp256k1 point).
type ProxyUnlockPtlcParam struct {
	Id          types.Hash
	Destination types.Address
	Signature   []byte
}

type CreateWithReclaimLockPtlcParam struct {
	ExpirationTime   int64         `json:"expirationTime"`
	PointType        uint8         `json:"pointType"`
	PointLock        []byte        `json:"pointLock"`
	Destination      types.Address `json:"destination"`
	ReclaimPointType uint8         `json:"reclaimPointType"`
	ReclaimPointLock []byte        `json:"reclaimPointLock"`
}

type ReclaimWithSignaturePtlcParam struct {
	Id        types.Hash
	Signature []byte
}

type PtlcReclaimLock struct {
	Id        types.Hash `json:"id"`
	PointType uint8      `json:"pointType"`
	PointLock []byte     `json:"pointLock"`
}

func (l PtlcReclaimLock) String() string {
	return fmt.Sprintf("Id:%s PointType:%d PointLock:%s ", l.Id, l.PointType, base64.StdEncoding.EncodeToString(l.PointLock))
}

func (l *PtlcReclaimLock) Save(context db.DB) error {
	data, err := ABIPtlc.PackVariable(
		variableNamePtlcReclaimLock,
		l.PointType,
		l.PointLock,
	)
	if err != nil {
		return err
	}
	return context.Put(getPtlcReclaimLockKey(l.Id), data)
}

func (l *PtlcReclaimLock) Delete(context db.DB) error {
	return context.Delete(getPtlcReclaimLockKey(l.Id))
}

func getPtlcReclaimLockKey(hash types.Hash) []byte {
	return common.JoinBytes(ptlcReclaimLockKeyPrefix, hash.Bytes())
}

func GetPtlcReclaimLock(context db.DB, id types.Hash) (*PtlcReclaimLock, error) {
	data, err := context.Get(getPtlcReclaimLockKey(id))
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return nil, constants.ErrDataNonExistent
	}
	lock := new(PtlcReclaimLock)
	if err := ABIPtlc.UnpackVariable(lock, variableNamePtlcReclaimLock, data); err != nil {
		return nil, err
	}
	lock.Id = id
	return lock, nil
}

func GetPtlcReclaimMessage(chainIdentifier uint64, pointType uint8, id types.Hash, timeLocked types.Address) []byte {
	return crypto.Hash(common.JoinBytes(
		[]byte(PtlcReclaimMessageDomain),
		common.Uint64ToBytes(chainIdentifier),
		types.PtlcContract.Bytes(),
		[]byte{pointType},
		id.Bytes(),
		timeLocked.Bytes(),
	))
}

// Save stores the entry under its id.
func (entry *PtlcInfo) Save(context db.DB) error {
	data, err := ABIPtlc.PackVariable(
		variableNamePtlcInfo,
		entry.TimeLocked,
		entry.TokenStandard,
		entry.Amount,
		entry.ExpirationTime,
		entry.PointType,
		entry.PointLock,
		entry.Destination,
	)
	if err != nil {
		return err
	}
	return context.Put(getPtlcInfoKey(entry.Id), data)
}

// Delete removes the entry once it has been unlocked or reclaimed.
func (entry *PtlcInfo) Delete(context db.DB) error {
	return context.Delete(getPtlcInfoKey(entry.Id))
}

func getPtlcInfoKey(hash types.Hash) []byte {
	return common.JoinBytes(ptlcInfoKeyPrefix, hash.Bytes())
}
func isPtlcInfoKey(key []byte) bool {
	return len(key) == 1+types.HashSize && key[0] == ptlcInfoKeyPrefix[0]
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

// GetPtlcUnlockMessage returns the 32-byte message a signature witness
// (Ed25519 or BIP-340) must sign: the SHA3-256 hash of the domain
// prefix, the chain identifier, the PTLC contract address, the point
// type, the entry id and the destination, so such a witness is valid
// for one entry, one destination and one chain. A secp256k1-point
// witness is the scalar itself and does not sign this message; its
// destination is enforced by the entry's fixed Destination instead.
func GetPtlcUnlockMessage(chainIdentifier uint64, pointType uint8, id types.Hash, destination types.Address) []byte {
	return crypto.Hash(common.JoinBytes(
		[]byte(PtlcUnlockMessageDomain),
		common.Uint64ToBytes(chainIdentifier),
		types.PtlcContract.Bytes(),
		[]byte{pointType},
		id.Bytes(),
		destination.Bytes(),
	))
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

// GetPtlcInfo returns the entry stored under id, or
// constants.ErrDataNonExistent if there is none.
func GetPtlcInfo(context db.DB, id types.Hash) (*PtlcInfo, error) {
	key := getPtlcInfoKey(id)
	if data, err := context.Get(key); err != nil {
		return nil, err
	} else {
		return parsePtlcInfo(key, data)
	}
}
