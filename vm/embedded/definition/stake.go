package definition

import (
	"math/big"
	"strings"

	"github.com/pkg/errors"

	"github.com/zenon-network/go-zenon/common/db"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/vm/abi"
	"github.com/zenon-network/go-zenon/vm/constants"
)

const (
	// jsonStake is the ABI JSON of the stake embedded contract: the
	// Stake and Cancel methods, the shared Update/CollectReward
	// methods and the stored stakeInfo variable. Parsed into ABIStake.
	jsonStake = `
	[
		{"type":"function","name":"Stake","inputs":[{"name":"durationInSec", "type":"int64"}]},
		{"type":"function","name":"Cancel","inputs":[{"name":"id","type":"hash"}]},
		{"type":"function","name":"CollectReward","inputs":[]},
		{"type":"function","name":"Update", "inputs":[]},

		{"type":"variable", "name":"stakeInfo", "inputs":[
			{"name":"amount", "type":"uint256"},
			{"name":"weightedAmount", "type":"uint256"},
			{"name":"startTime", "type":"int64"},
			{"name":"revokeTime", "type":"int64"},
			{"name":"expirationTime", "type":"int64"}
		]}
	]`

	// StakeMethodName names the method that locks the sent ZNN for a
	// duration between constants.StakeTimeMinSec and
	// constants.StakeTimeMaxSec, in constants.StakeTimeUnitSec steps.
	StakeMethodName = "Stake"
	// CancelStakeMethodName names the method that revokes an expired
	// stake entry by id and returns the locked ZNN.
	CancelStakeMethodName = "Cancel"

	stakeInfoVariableName = "stakeInfo"
)

var (
	// ABIStake is the parsed ABI of the stake embedded contract.
	ABIStake = abi.JSONToABIContract(strings.NewReader(jsonStake))

	stakeInfoPrefix = []byte{1}
)

// StakeInfo is one ZNN stake entry. Id is the hash of the Stake send
// block; Amount is the locked ZNN (smallest units) and WeightedAmount
// the duration-weighted amount the QSR rewards are computed from
// (longer durations weigh more). StartTime, ExpirationTime and
// RevokeTime are unix seconds; RevokeTime is zero until the stake is
// cancelled. Entries are stored under stakeInfoPrefix (1) followed by
// the staker address bytes and the 32-byte id, so one address's
// entries share a key prefix and iterate in id byte order, not by
// expiration.
type StakeInfo struct {
	Amount         *big.Int      `json:"amount"`
	WeightedAmount *big.Int      `json:"weightedAmount"`
	StartTime      int64         `json:"startTime"`
	RevokeTime     int64         `json:"revokeTime"`
	ExpirationTime int64         `json:"expirationTime"`
	StakeAddress   types.Address `json:"stakeAddress"`
	Id             types.Hash    `json:"id"`
}

// Save stores the entry under its address+id key, packing everything
// but the address and id (those are recovered from the key when
// parsing); packing failures panic, the put error is returned.
func (stake *StakeInfo) Save(context db.DB) error {
	return context.Put(
		getStakeInfoKey(stake.Id, stake.StakeAddress),
		ABIStake.PackVariablePanic(
			stakeInfoVariableName,
			stake.Amount,
			stake.WeightedAmount,
			stake.StartTime,
			stake.RevokeTime,
			stake.ExpirationTime,
		))
}

// Delete removes the stake entry.
func (stake *StakeInfo) Delete(context db.DB) error {
	return context.Delete(getStakeInfoKey(stake.Id, stake.StakeAddress))
}

func getStakeInfoKey(id types.Hash, address types.Address) []byte {
	return append(append(stakeInfoPrefix, address.Bytes()...), id.Bytes()...)
}
func isStakeInfoKey(key []byte) bool {
	return key[0] == stakeInfoPrefix[0]
}
func unmarshalStakeInfoKey(key []byte) (*types.Hash, *types.Address, error) {
	if !isStakeInfoKey(key) {
		return nil, nil, errors.Errorf("invalid key! Not stake info key")
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
func parseStakeInfo(key []byte, data []byte) (*StakeInfo, error) {
	if len(data) > 0 {
		entry := new(StakeInfo)
		err := ABIStake.UnpackVariable(entry, stakeInfoVariableName, data)
		if err != nil {
			return nil, err
		}

		id, address, err := unmarshalStakeInfoKey(key)
		if err != nil {
			return nil, err
		}
		entry.Id = *id
		entry.StakeAddress = *address
		return entry, err
	} else {
		return nil, constants.ErrDataNonExistent
	}
}

// GetStakeInfo returns the stake entry of address with the given id,
// or constants.ErrDataNonExistent if no such entry exists.
func GetStakeInfo(context db.DB, id types.Hash, address types.Address) (*StakeInfo, error) {
	key := getStakeInfoKey(id, address)
	if data, err := context.Get(key); err != nil {
		return nil, err
	} else {
		return parseStakeInfo(key, data)
	}
}

// IterateStakeEntries calls f for every stored stake entry, in
// storage-key (address bytes, then id bytes) order, stopping at the
// first error f returns.
func IterateStakeEntries(context db.DB, f func(*StakeInfo) error) error {
	iterator := context.NewIterator(stakeInfoPrefix)
	defer iterator.Release()

	for {
		if !iterator.Next() {
			if iterator.Error() != nil {
				return iterator.Error()
			}
			break
		}

		if stakeInfo, err := parseStakeInfo(iterator.Key(), iterator.Value()); err == nil {
			if err := f(stakeInfo); err != nil {
				return err
			}
		} else if err == constants.ErrDataNonExistent {
		} else {
			return err
		}
	}
	return nil
}

// GetStakeListByAddress returns the active (RevokeTime zero) stake
// entries of address in storage-key (id byte) order, together with
// their summed Amount and summed WeightedAmount. It scans every
// address's entries and filters in memory.
func GetStakeListByAddress(context db.DB, address types.Address) ([]*StakeInfo, *big.Int, *big.Int, error) {
	total := big.NewInt(0)
	weighted := big.NewInt(0)
	list := make([]*StakeInfo, 0)

	err := IterateStakeEntries(context, func(stakeInfo *StakeInfo) error {
		if stakeInfo.RevokeTime == 0 && stakeInfo.StakeAddress == address {
			list = append(list, stakeInfo)
			total.Add(total, stakeInfo.Amount)
			weighted.Add(weighted, stakeInfo.WeightedAmount)
		}
		return nil
	})
	if err != nil {
		return nil, nil, nil, err
	} else {
		return list, total, weighted, nil
	}
}

// StakeByExpirationTime sorts stake entries by ascending expiration
// time, breaking ties by ascending id string; the stake RPC API uses
// it to present entries in expiry order, since the storage iterates
// by id.
type StakeByExpirationTime []*StakeInfo

// Len implements sort.Interface.
func (a StakeByExpirationTime) Len() int { return len(a) }

// Swap implements sort.Interface.
func (a StakeByExpirationTime) Swap(i, j int) { a[i], a[j] = a[j], a[i] }

// Less orders by expiration time, then by id string on ties.
func (a StakeByExpirationTime) Less(i, j int) bool {
	if a[i].ExpirationTime == a[j].ExpirationTime {
		return a[i].Id.String() < a[j].Id.String()
	}
	return a[i].ExpirationTime < a[j].ExpirationTime
}
