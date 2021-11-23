package definition

import (
	"math/big"
	"strings"

	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/db"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/vm/abi"
)

const (
	jsonSentinel = `
	[
		{"type":"function","name":"DepositQsr","inputs":[]},
		{"type":"function","name":"WithdrawQsr","inputs":[]},
		{"type":"function","name":"Register","inputs":[]},
		{"type":"function","name":"Revoke","inputs":[]},
		{"type":"function","name":"Update", "inputs":[]},
		{"type":"function","name":"CollectReward","inputs":[]},

		{"type":"variable","name":"sentinelInfo","inputs":[
			{"name":"owner","type":"address"},
			{"name":"registrationTimestamp","type":"int64"},
			{"name":"revokeTimestamp","type":"int64"},
			{"name":"znnAmount","type":"uint256"},
			{"name":"qsrAmount","type":"uint256"}]}
	]`

	RegisterSentinelMethodName = "Register"
	RevokeSentinelMethodName   = "Revoke"

	sentinelInfoVariableName = "sentinelInfo"
)

var (
	ABISentinel = abi.JSONToABIContract(strings.NewReader(jsonSentinel))
)

const (
	_ byte = iota
	sentinelInfoPrefix
)

type SentinelInfoKey struct {
	Owner types.Address `json:"owner"`
}
type SentinelInfo struct {
	SentinelInfoKey
	RegistrationTimestamp int64    `json:"registrationTimestamp"`
	RevokeTimestamp       int64    `json:"revokeTimestamp"`
	ZnnAmount             *big.Int `json:"znnAmount"`
	QsrAmount             *big.Int `json:"qsrAmount"`
}

func (sentinel *SentinelInfo) Save(context db.DB) {
	common.DealWithErr(context.Put(sentinel.Key(), sentinel.Data()))
}
func (sentinel *SentinelInfo) Delete(context db.DB) {
	common.DealWithErr(context.Delete(sentinel.Key()))
}
func (sentinel *SentinelInfo) Data() []byte {
	return ABISentinel.PackVariablePanic(
		sentinelInfoVariableName,
		sentinel.Owner,
		sentinel.RegistrationTimestamp,
		sentinel.RevokeTimestamp,
		sentinel.ZnnAmount,
		sentinel.QsrAmount)
}
func (sentinel *SentinelInfoKey) Key() []byte {
	return common.JoinBytes([]byte{sentinelInfoPrefix}, sentinel.Owner.Bytes())
}

func parseSentinelInfo(data []byte) *SentinelInfo {
	sentinel := new(SentinelInfo)
	ABISentinel.UnpackVariablePanic(sentinel, sentinelInfoVariableName, data)
	return sentinel
}
func GetSentinelInfoByOwner(context db.DB, address types.Address) *SentinelInfo {
	key := (&SentinelInfoKey{Owner: address}).Key()
	data, err := context.Get(key)
	common.DealWithErr(err)
	if len(data) == 0 {
		return nil
	} else {
		return parseSentinelInfo(data)
	}
}
func GetAllSentinelInfo(context db.DB) []*SentinelInfo {
	iterator := context.NewIterator([]byte{sentinelInfoPrefix})
	defer iterator.Release()

	sentinelInfoList := make([]*SentinelInfo, 0)
	for {
		if !iterator.Next() {
			common.DealWithErr(iterator.Error())
			break
		}
		sentinelInfoList = append(sentinelInfoList, parseSentinelInfo(iterator.Value()))
	}
	return sentinelInfoList
}
func IterateSentinelEntries(context db.DB, f func(*SentinelInfo) error) error {
	iterator := context.NewIterator([]byte{sentinelInfoPrefix})
	defer iterator.Release()

	for {
		if !iterator.Next() {
			common.DealWithErr(iterator.Error())
			break
		}

		sentinelInfo := parseSentinelInfo(iterator.Value())
		if err := f(sentinelInfo); err != nil {
			return err
		}
	}
	return nil
}
