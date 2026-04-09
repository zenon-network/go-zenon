package definition

import (
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
	jsonAtomicSwap = `
	[
		{"type":"function","name":"CreateSwap","inputs":[
			{"name":"counterparty","type":"address"},
			{"name":"btcTxHash","type":"bytes"},
			{"name":"expirationTime","type":"int64"}
		]},
		{"type":"function","name":"ClaimSwap","inputs":[
			{"name":"swapId","type":"hash"},
			{"name":"blockHeader","type":"bytes"},
			{"name":"merkleProof","type":"bytes"},
			{"name":"txIndex","type":"uint32"}
		]},
		{"type":"function","name":"ReclaimSwap","inputs":[
			{"name":"swapId","type":"hash"}
		]},

		{"type":"variable","name":"swapInfo","inputs":[
			{"name":"creator","type":"address"},
			{"name":"counterparty","type":"address"},
			{"name":"tokenStandard","type":"tokenStandard"},
			{"name":"amount","type":"uint256"},
			{"name":"btcTxHash","type":"bytes"},
			{"name":"expirationTime","type":"int64"},
			{"name":"status","type":"uint8"}
		]}
	]`

	CreateSwapMethodName  = "CreateSwap"
	ClaimSwapMethodName   = "ClaimSwap"
	ReclaimSwapMethodName = "ReclaimSwap"

	variableNameSwapInfo = "swapInfo"

	// Swap status constants
	SwapStatusActive   uint8 = 0
	SwapStatusClaimed  uint8 = 1
	SwapStatusReclaimed uint8 = 2
)

var (
	ABIAtomicSwap = abi.JSONToABIContract(strings.NewReader(jsonAtomicSwap))

	// Key prefix {1} for swapInfo
	swapInfoKeyPrefix = []byte{1}
)

// CreateSwapParam is the parameter for the CreateSwap method.
type CreateSwapParam struct {
	Counterparty   types.Address `json:"counterparty"`
	BtcTxHash      []byte        `json:"btcTxHash"`
	ExpirationTime int64         `json:"expirationTime"`
}

// ClaimSwapParam is the parameter for the ClaimSwap method.
type ClaimSwapParam struct {
	SwapId      types.Hash `json:"swapId"`
	BlockHeader []byte     `json:"blockHeader"`
	MerkleProof []byte     `json:"merkleProof"`
	TxIndex     uint32     `json:"txIndex"`
}

// SwapInfo stores the state of an atomic swap.
type SwapInfo struct {
	Id             types.Hash               `json:"id"`
	Creator        types.Address            `json:"creator"`
	Counterparty   types.Address            `json:"counterparty"`
	TokenStandard  types.ZenonTokenStandard `json:"tokenStandard"`
	Amount         *big.Int                 `json:"amount"`
	BtcTxHash      []byte                   `json:"btcTxHash"`
	ExpirationTime int64                    `json:"expirationTime"`
	Status         uint8                    `json:"status"`
}

func (s *SwapInfo) String() string {
	return fmt.Sprintf("Id:%s Creator:%s Counterparty:%s TokenStandard:%s Amount:%s BtcTxHash:%x ExpirationTime:%d Status:%d",
		s.Id, s.Creator, s.Counterparty, s.TokenStandard, s.Amount, s.BtcTxHash, s.ExpirationTime, s.Status)
}

// --- SwapInfo storage ---

func getSwapInfoKey(id types.Hash) []byte {
	return common.JoinBytes(swapInfoKeyPrefix, id.Bytes())
}

func isSwapInfoKey(key []byte) bool {
	return key[0] == swapInfoKeyPrefix[0]
}

func unmarshalSwapInfoKey(key []byte) (*types.Hash, error) {
	if !isSwapInfoKey(key) {
		return nil, errors.Errorf("invalid key! Not swap info key")
	}
	h := new(types.Hash)
	err := h.SetBytes(key[1:])
	if err != nil {
		return nil, err
	}
	return h, nil
}

func (s *SwapInfo) Save(context db.DB) error {
	data, err := ABIAtomicSwap.PackVariable(
		variableNameSwapInfo,
		s.Creator,
		s.Counterparty,
		s.TokenStandard,
		s.Amount,
		s.BtcTxHash,
		s.ExpirationTime,
		s.Status,
	)
	if err != nil {
		return err
	}
	return context.Put(getSwapInfoKey(s.Id), data)
}

func (s *SwapInfo) Delete(context db.DB) error {
	return context.Delete(getSwapInfoKey(s.Id))
}

func parseSwapInfo(key, data []byte) (*SwapInfo, error) {
	if len(data) > 0 {
		info := new(SwapInfo)
		if err := ABIAtomicSwap.UnpackVariable(info, variableNameSwapInfo, data); err != nil {
			return nil, err
		}
		id, err := unmarshalSwapInfoKey(key)
		if err != nil {
			return nil, err
		}
		info.Id = *id
		return info, nil
	}
	return nil, constants.ErrDataNonExistent
}

func GetSwapInfo(context db.DB, id types.Hash) (*SwapInfo, error) {
	key := getSwapInfoKey(id)
	if data, err := context.Get(key); err != nil {
		return nil, err
	} else {
		return parseSwapInfo(key, data)
	}
}

// --- JSON marshaling ---

type SwapInfoMarshal struct {
	Id             types.Hash               `json:"id"`
	Creator        types.Address            `json:"creator"`
	Counterparty   types.Address            `json:"counterparty"`
	TokenStandard  types.ZenonTokenStandard `json:"tokenStandard"`
	Amount         string                   `json:"amount"`
	BtcTxHash      []byte                   `json:"btcTxHash"`
	ExpirationTime int64                    `json:"expirationTime"`
	Status         uint8                    `json:"status"`
}

func (s *SwapInfo) ToSwapInfoMarshal() *SwapInfoMarshal {
	return &SwapInfoMarshal{
		Id:             s.Id,
		Creator:        s.Creator,
		Counterparty:   s.Counterparty,
		TokenStandard:  s.TokenStandard,
		Amount:         s.Amount.String(),
		BtcTxHash:      s.BtcTxHash,
		ExpirationTime: s.ExpirationTime,
		Status:         s.Status,
	}
}

func (s *SwapInfo) MarshalJSON() ([]byte, error) {
	return json.Marshal(s.ToSwapInfoMarshal())
}

func (s *SwapInfo) UnmarshalJSON(data []byte) error {
	aux := new(SwapInfoMarshal)
	if err := json.Unmarshal(data, aux); err != nil {
		return err
	}
	s.Id = aux.Id
	s.Creator = aux.Creator
	s.Counterparty = aux.Counterparty
	s.TokenStandard = aux.TokenStandard
	s.Amount = common.StringToBigInt(aux.Amount)
	s.BtcTxHash = aux.BtcTxHash
	s.ExpirationTime = aux.ExpirationTime
	s.Status = aux.Status
	return nil
}
