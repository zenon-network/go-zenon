package definition

import (
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
	jsonPlasma = `
	[
		{"type":"function","name":"Fuse", "inputs":[
			{"name":"address","type":"address"}
		]},
		{"type":"function","name":"CancelFuse","inputs":[
			{"name":"id","type":"hash"}
		]},

		{"type":"variable","name":"fusionInfo","inputs":[
			{"name":"amount","type":"uint256"},
			{"name":"expirationHeight","type":"uint64"},
			{"name":"beneficiary","type":"address"}
		]},
		{"type":"variable","name":"fusedAmount","inputs":[
			{"name":"amount","type":"uint256"}
		]}
	]`

	FuseMethodName       = "Fuse"
	CancelFuseMethodName = "CancelFuse"

	variableNameFusionInfo  = "fusionInfo"
	variableNameFusedAmount = "fusedAmount"
)

var (
	// ABIPlasma is abi definition of the plasma contract
	ABIPlasma = abi.JSONToABIContract(strings.NewReader(jsonPlasma))

	fusionInfoKeyPrefix  = []byte{1}
	fusedAmountKeyPrefix = []byte{2}

	FusedAmountKeyPrefix = fusedAmountKeyPrefix
)

type FusionInfo struct {
	Owner            types.Address `json:"owner"`
	Id               types.Hash    `json:"id"`
	Amount           *big.Int      `json:"amount"`
	ExpirationHeight uint64        `json:"withdrawHeight"`
	Beneficiary      types.Address `json:"beneficiaryAddress"`
}

func (entry *FusionInfo) Save(context db.DB) error {
	data, err := ABIPlasma.PackVariable(
		variableNameFusionInfo,
		entry.Amount,
		entry.ExpirationHeight,
		entry.Beneficiary,
	)
	if err != nil {
		return err
	}
	return context.Put(getFusionInfoKey(entry.Owner, entry.Id), data)
}
func (entry *FusionInfo) Delete(context db.DB) error {
	return context.Delete(getFusionInfoKey(entry.Owner, entry.Id))
}

func getFusionInfoKey(addr types.Address, hash types.Hash) []byte {
	return common.JoinBytes(fusionInfoKeyPrefix, addr.Bytes(), hash.Bytes())
}
func isFusionInfoKey(key []byte) bool {
	return key[0] == fusionInfoKeyPrefix[0]
}
func unmarshalFusionInfoKey(key []byte) (*types.Hash, *types.Address, error) {
	if !isFusionInfoKey(key) {
		return nil, nil, errors.Errorf("invalid key! Not fusion info key")
	}
	h := new(types.Hash)
	err := h.SetBytes(key[1+types.AddressSize:])
	if err != nil {
		return nil, nil, err
	}

	addr := new(types.Address)
	err = addr.SetBytes(key[1 : 1+types.AddressSize])
	if err != nil {
		return nil, nil, err
	}

	return h, addr, nil
}
func parseFusionInfo(key, data []byte) (*FusionInfo, error) {
	if len(data) > 0 {
		info := new(FusionInfo)
		if err := ABIPlasma.UnpackVariable(info, variableNameFusionInfo, data); err != nil {
			return nil, err
		}
		id, owner, err := unmarshalFusionInfoKey(key)
		if err != nil {
			return nil, err
		}
		info.Owner = *owner
		info.Id = *id
		return info, nil
	} else {
		return nil, constants.ErrDataNonExistent
	}
}
func GetFusionInfo(context db.DB, owner types.Address, id types.Hash) (*FusionInfo, error) {
	key := getFusionInfoKey(owner, id)
	if data, err := context.Get(key); err != nil {
		return nil, err
	} else {
		return parseFusionInfo(key, data)
	}
}
func GetFusionInfoListByOwner(context db.DB, owner types.Address) ([]*FusionInfo, *big.Int, error) {
	fusedAmount := big.NewInt(0)
	iterator := context.NewIterator(common.JoinBytes(fusionInfoKeyPrefix, owner.Bytes()))
	defer iterator.Release()
	list := make([]*FusionInfo, 0)
	for {
		if !iterator.Next() {
			if iterator.Error() != nil {
				return nil, nil, iterator.Error()
			}
			break
		}

		if fusionInfo, err := parseFusionInfo(iterator.Key(), iterator.Value()); err == nil {
			list = append(list, fusionInfo)
			fusedAmount.Add(fusedAmount, fusionInfo.Amount)
		} else if err == constants.ErrDataNonExistent {
			continue
		} else {
			return nil, nil, err
		}
	}
	return list, fusedAmount, nil
}

type FusedAmount struct {
	Beneficiary types.Address
	Amount      *big.Int
}

func (entry *FusedAmount) Save(context db.DB) error {
	data, err := ABIPlasma.PackVariable(
		variableNameFusedAmount,
		entry.Amount,
	)
	if err != nil {
		return err
	}
	return context.Put(getFusedAmountKey(entry.Beneficiary), data)
}
func (entry *FusedAmount) Delete(context db.DB) error {
	return context.Delete(getFusedAmountKey(entry.Beneficiary))
}

func getFusedAmountKey(beneficiary types.Address) []byte {
	return common.JoinBytes(fusedAmountKeyPrefix, beneficiary.Bytes())
}
func isFusedAmountKey(key []byte) bool {
	return key[0] == fusedAmountKeyPrefix[0]
}
func unmarshalFusedAmountKey(key []byte) (*types.Address, error) {
	if !isFusedAmountKey(key) {
		return nil, errors.Errorf("invalid key! Not fused amount key")
	}
	addr := new(types.Address)
	if err := addr.SetBytes(key[1:]); err != nil {
		return nil, err
	}

	return addr, nil
}
func parseFusedAmount(key, data []byte) (*FusedAmount, error) {
	if len(data) > 0 {
		info := new(FusedAmount)
		if err := ABIPlasma.UnpackVariable(info, variableNameFusedAmount, data); err != nil {
			return nil, err
		}
		beneficiary, err := unmarshalFusedAmountKey(key)
		if err != nil {
			return nil, err
		}
		info.Beneficiary = *beneficiary
		return info, nil
	} else {
		return nil, constants.ErrDataNonExistent
	}
}
func GetFusedAmount(context db.DB, beneficiary types.Address) (*FusedAmount, error) {
	key := getFusedAmountKey(beneficiary)
	if data, err := context.Get(key); err != nil {
		return nil, err
	} else {
		amount, err := parseFusedAmount(key, data)
		if err == constants.ErrDataNonExistent {
			return &FusedAmount{
				Beneficiary: beneficiary,
				Amount:      big.NewInt(0),
			}, nil
		}
		return amount, err
	}
}
