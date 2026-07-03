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
	// jsonPlasma is the ABI JSON of the plasma embedded contract: the
	// Fuse and CancelFuse methods plus the per-entry fusionInfo and
	// per-beneficiary fusedAmount variables. Parsed into ABIPlasma.
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

	// FuseMethodName names the method that locks the sent QSR and
	// grants plasma to the beneficiary address passed as parameter.
	FuseMethodName = "Fuse"
	// CancelFuseMethodName names the method by which the owner of an
	// expired fusion entry (identified by its id) reclaims the QSR.
	CancelFuseMethodName = "CancelFuse"

	variableNameFusionInfo  = "fusionInfo"
	variableNameFusedAmount = "fusedAmount"
)

var (
	// ABIPlasma is the parsed ABI of the plasma embedded contract.
	ABIPlasma = abi.JSONToABIContract(strings.NewReader(jsonPlasma))

	fusionInfoKeyPrefix  = []byte{1}
	fusedAmountKeyPrefix = []byte{2}
)

// FusionInfo is one plasma fusion entry: Owner locked Amount QSR
// (smallest units) in favor of Beneficiary, who gains plasma while
// the entry exists. Id is the hash of the Fuse send block and is the
// handle CancelFuse takes; ExpirationHeight is the momentum height
// (fusion height plus constants.FuseExpiration) from which the owner
// may cancel and reclaim the QSR. Entries are stored under
// fusionInfoKeyPrefix (1) followed by the owner address bytes and the
// id, so one owner's entries share a key prefix.
type FusionInfo struct {
	Owner            types.Address `json:"owner"`
	Id               types.Hash    `json:"id"`
	Amount           *big.Int      `json:"amount"`
	ExpirationHeight uint64        `json:"withdrawHeight"`
	Beneficiary      types.Address `json:"beneficiaryAddress"`
}

// Save stores the entry under its owner+id key, returning any pack or
// put error; owner and id are recovered from the key when parsing.
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

// Delete removes the fusion entry when it is cancelled.
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

// GetFusionInfo returns the fusion entry owner created with the Fuse
// send block whose hash is id, or constants.ErrDataNonExistent if no
// such entry exists.
func GetFusionInfo(context db.DB, owner types.Address, id types.Hash) (*FusionInfo, error) {
	key := getFusionInfoKey(owner, id)
	if data, err := context.Get(key); err != nil {
		return nil, err
	} else {
		return parseFusionInfo(key, data)
	}
}

// GetFusionInfoListByOwner returns all fusion entries created by
// owner, in storage-key (id byte) order, together with the total QSR
// they lock.
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

// FusedAmount is the total QSR fused in favor of a beneficiary across
// all fusion entries; the contract keeps it in sync as entries are
// created and cancelled. The momentum store reads it (as the
// stake-beneficial amount) to derive the plasma available to the
// beneficiary's account chain. Stored under fusedAmountKeyPrefix (2)
// followed by the beneficiary address bytes.
type FusedAmount struct {
	Beneficiary types.Address
	Amount      *big.Int
}

// Save stores the total under the beneficiary key, returning any pack
// or put error; the beneficiary is recovered from the key when
// parsing.
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

// Delete removes the beneficiary's total once it reaches zero.
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

// GetFusedAmount returns the total QSR fused for beneficiary. A
// missing entry is not an error: it yields a total of zero, so
// callers never see constants.ErrDataNonExistent.
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
