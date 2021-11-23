package definition

import (
	"encoding/binary"
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
	jsonCommon = `
	[	
		{"type":"variable","name":"lastUpdate","inputs":[{"name":"height","type":"uint64"}]},
		{"type":"variable","name":"lastEpochUpdate","inputs":[{"name":"lastEpoch", "type": "int64"}]},
		{"type":"variable","name":"rewardDeposit","inputs":[
			{"name":"znn","type":"uint256"},
			{"name":"qsr","type":"uint256"}
		]},
		{"type":"variable","name":"rewardDepositHistory","inputs":[
			{"name":"znn","type":"uint256"},
			{"name":"qsr","type":"uint256"}
		]},
		{"type":"variable","name":"qsrDeposit","inputs":[
			{"name":"qsr","type":"uint256"}
		]},

		{"type":"function","name":"Update", "inputs":[]},
		{"type":"function","name":"CollectReward","inputs":[]},
		{"type":"function","name":"DepositQsr", "inputs":[]},
		{"type":"function","name":"WithdrawQsr", "inputs":[]},
		{"type":"function","name":"Donate", "inputs":[]}
	]`

	RewardDepositVariableName        = "rewardDeposit"
	RewardDepositHistoryVariableName = "rewardDepositHistory"
	LastUpdateVariableName           = "lastUpdate"
	QsrDepositVariableName           = "qsrDeposit"
	LastEpochUpdateVariableName      = "lastEpochUpdate"

	UpdateMethodName        = "Update"
	CollectRewardMethodName = "CollectReward"
	DepositQsrMethodName    = "DepositQsr"
	WithdrawQsrMethodName   = "WithdrawQsr"
	DonateMethodName        = "Donate"
)

var (
	ABICommon = abi.JSONToABIContract(strings.NewReader(jsonCommon))

	// common key prefixes are big enough so they don't clash with embedded-specific variables
	rewardDepositKeyPrefix        = []byte{128}
	lastUpdateKey                 = []byte{129}
	qsrDepositKeyPrefix           = []byte{130}
	lastEpochUpdateKey            = []byte{131}
	rewardDepositHistoryKeyPrefix = []byte{132}
)

type RewardDeposit struct {
	Address *types.Address `json:"address"`
	Znn     *big.Int       `json:"znnAmount"`
	Qsr     *big.Int       `json:"qsrAmount"`
}

func (deposit *RewardDeposit) Save(context db.DB) error {
	return context.Put(
		getRewardDepositKey(deposit.Address),
		ABICommon.PackVariablePanic(
			RewardDepositVariableName,
			deposit.Znn,
			deposit.Qsr,
		))
}
func (deposit *RewardDeposit) Delete(context db.DB) error {
	return context.Delete(getRewardDepositKey(deposit.Address))
}

func newRewardDeposit(address *types.Address) *RewardDeposit {
	return &RewardDeposit{
		Address: address,
		Znn:     big.NewInt(0),
		Qsr:     big.NewInt(0),
	}
}

func getRewardDepositKey(address *types.Address) []byte {
	return append(rewardDepositKeyPrefix, address.Bytes()...)
}
func isRewardDepositKey(key []byte) bool {
	return key[0] == rewardDepositKeyPrefix[0]
}
func unmarshalRewardDepositKey(key []byte) (*types.Address, error) {
	if !isRewardDepositKey(key) {
		return nil, errors.Errorf("invalid key! Not reward deposit key")
	}
	addr := new(types.Address)
	if err := addr.SetBytes(key[1:]); err != nil {
		return nil, err
	}
	return addr, nil
}
func parseRewardDeposit(key []byte, data []byte) (*RewardDeposit, error) {
	if len(data) > 0 {
		deposit := new(RewardDeposit)
		if err := ABICommon.UnpackVariable(deposit, RewardDepositVariableName, data); err != nil {
			return nil, err
		}

		address, err := unmarshalRewardDepositKey(key)
		if err != nil {
			return nil, err
		}
		deposit.Address = address
		return deposit, nil
	} else {
		return nil, constants.ErrDataNonExistent
	}
}

// GetRewardDeposit returns uncollected ZNN & QSR reward.
// does not return util.ErrDataNonExistent, returns valid deposit with 0 amount.
func GetRewardDeposit(context db.DB, address *types.Address) (*RewardDeposit, error) {
	key := getRewardDepositKey(address)
	if data, err := context.Get(key); err != nil {
		return nil, err
	} else {
		deposit, err := parseRewardDeposit(key, data)
		if err == constants.ErrDataNonExistent {
			return newRewardDeposit(address), nil
		}
		return deposit, err
	}
}

type LastUpdateVariable struct {
	Height uint64
}

func (upd *LastUpdateVariable) Save(context db.DB) error {
	data, err := ABICommon.PackVariable(
		LastUpdateVariableName,
		upd.Height,
	)
	if err != nil {
		return err
	}
	return context.Put(
		lastUpdateKey,
		data,
	)
}

func parseLastUpdate(data []byte) (*LastUpdateVariable, error) {
	if len(data) > 0 {
		upd := new(LastUpdateVariable)
		if err := ABICommon.UnpackVariable(upd, LastUpdateVariableName, data); err != nil {
			return nil, err
		}
		return upd, nil
	} else {
		return nil, constants.ErrDataNonExistent
	}
}
func GetLastUpdate(context db.DB) (*LastUpdateVariable, error) {
	if data, err := context.Get(lastUpdateKey); err != nil {
		return nil, err
	} else {
		upd, err := parseLastUpdate(data)
		if err == constants.ErrDataNonExistent {
			return &LastUpdateVariable{Height: 0}, nil
		}
		return upd, err
	}
}

type QsrDeposit struct {
	Address *types.Address
	Qsr     *big.Int
}

func (deposit *QsrDeposit) Save(context db.DB) error {
	data, err := ABICommon.PackVariable(
		QsrDepositVariableName,
		deposit.Qsr,
	)
	if err != nil {
		return err
	}
	return context.Put(
		getQsrDepositKey(deposit.Address),
		data,
	)
}
func (deposit *QsrDeposit) Delete(context db.DB) error {
	return context.Delete(getQsrDepositKey(deposit.Address))
}

func newQsrDeposit(address *types.Address) *QsrDeposit {
	return &QsrDeposit{
		Address: address,
		Qsr:     big.NewInt(0),
	}
}
func getQsrDepositKey(address *types.Address) []byte {
	return append(qsrDepositKeyPrefix, address.Bytes()...)
}
func isQsrDepositKey(key []byte) bool {
	return key[0] == qsrDepositKeyPrefix[0]
}
func unmarshalQsrDepositKey(key []byte) (*types.Address, error) {
	if !isQsrDepositKey(key) {
		return nil, errors.Errorf("invalid key! Not qsr deposit key")
	}
	addr := new(types.Address)
	if err := addr.SetBytes(key[1:]); err != nil {
		return nil, err
	}
	return addr, nil
}
func parseQsrDeposit(key []byte, data []byte) (*QsrDeposit, error) {
	if len(data) > 0 {
		deposit := new(QsrDeposit)
		if err := ABICommon.UnpackVariable(deposit, QsrDepositVariableName, data); err != nil {
			return nil, err
		}

		address, err := unmarshalQsrDepositKey(key)
		if err != nil {
			return nil, err
		}
		deposit.Address = address
		return deposit, nil
	} else {
		return nil, constants.ErrDataNonExistent
	}
}

// GetQsrDeposit returns deposited QSR for sentinel/pillar.
// does not return util.ErrDataNonExistent, returns valid deposit with 0 amount.
func GetQsrDeposit(context db.DB, address *types.Address) (*QsrDeposit, error) {
	key := getQsrDepositKey(address)
	if data, err := context.Get(key); err != nil {
		return nil, err
	} else {
		deposit, err := parseQsrDeposit(key, data)
		if err == constants.ErrDataNonExistent {
			return newQsrDeposit(address), nil
		}
		return deposit, err
	}
}

type LastEpochUpdate struct {
	LastEpoch int64
}

func (epoch *LastEpochUpdate) Save(context db.DB) error {
	data, err := ABICommon.PackVariable(
		LastEpochUpdateVariableName,
		epoch.LastEpoch,
	)
	if err != nil {
		return err
	}
	return context.Put(
		lastEpochUpdateKey,
		data,
	)
}

func GetLastEpochUpdate(context db.DB) (*LastEpochUpdate, error) {
	latestData, err := context.Get(lastEpochUpdateKey)
	if err != nil {
		return nil, err
	}
	if len(latestData) == 0 {
		return &LastEpochUpdate{
			LastEpoch: -1,
		}, nil
	}

	lastEpoch := new(LastEpochUpdate)
	err = ABICommon.UnpackVariable(lastEpoch, LastEpochUpdateVariableName, latestData)
	return lastEpoch, err
}

type RewardDepositHistory struct {
	Epoch   uint64
	Address *types.Address `json:"address"`
	Znn     *big.Int       `json:"znnAmount"`
	Qsr     *big.Int       `json:"qsrAmount"`
}

func (rdh *RewardDepositHistory) Save(context db.DB) error {
	data, err := ABICommon.PackVariable(
		RewardDepositHistoryVariableName,
		rdh.Znn,
		rdh.Qsr)
	if err != nil {
		return err
	}
	return context.Put(getRewardDepositHistoryEntryKey(rdh.Epoch, rdh.Address), data)
}

func getRewardDepositHistoryPrefixKey(address *types.Address) []byte {
	return common.JoinBytes(rewardDepositHistoryKeyPrefix, address.Bytes())
}
func getRewardDepositHistoryEntryKey(epoch uint64, address *types.Address) []byte {
	epochBytes := make([]byte, 8)
	binary.LittleEndian.PutUint64(epochBytes, epoch)
	return common.JoinBytes(getRewardDepositHistoryPrefixKey(address), epochBytes)
}
func isRewardDepositHistoryEntryKey(key []byte) bool {
	return key[0] == rewardDepositHistoryKeyPrefix[0]
}
func unmarshalRewardDepositHistoryEntryKey(key []byte) (uint64, *types.Address, error) {
	if !isRewardDepositHistoryEntryKey(key) {
		return 0, nil, errors.Errorf("invalid key! Not RewardDepositHistory key")
	}
	address, err := types.BytesToAddress(key[1 : 1+types.AddressSize])
	epoch := binary.LittleEndian.Uint64(key[1+types.AddressSize : 8+1+types.AddressSize])
	if err != nil {
		return 0, nil, err
	}
	return epoch, &address, nil
}
func parseRewardDepositHistoryEntry(key, data []byte) (*RewardDepositHistory, error) {
	if len(data) > 0 {
		entry := new(RewardDepositHistory)
		if err := ABICommon.UnpackVariable(entry, RewardDepositHistoryVariableName, data); err != nil {
			return nil, err
		}
		if epoch, address, err := unmarshalRewardDepositHistoryEntryKey(key); err == nil {
			entry.Epoch = epoch
			entry.Address = address
		} else {
			return nil, err
		}
		return entry, nil
	} else {
		return nil, constants.ErrDataNonExistent
	}
}
func GetRewardDepositHistory(context db.DB, epoch uint64, address *types.Address) (*RewardDepositHistory, error) {
	key := getRewardDepositHistoryEntryKey(epoch, address)
	if data, err := context.Get(key); err != nil {
		return nil, err
	} else {
		deposit, err := parseRewardDepositHistoryEntry(key, data)
		if err == constants.ErrDataNonExistent {
			return &RewardDepositHistory{
				Epoch:   epoch,
				Address: address,
				Znn:     big.NewInt(0),
				Qsr:     big.NewInt(0),
			}, nil
		}
		return deposit, err
	}
}
