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
	// jsonSentinel is the ABI JSON of the sentinel embedded contract:
	// registration and revocation plus the shared deposit/reward
	// methods, and the stored sentinelInfo variable. Parsed into
	// ABISentinel.
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

	// RegisterSentinelMethodName names the method that registers a
	// sentinel, locking the sent ZNN and the deposited QSR; the Go
	// constant carries the Sentinel suffix to avoid clashing with the
	// pillar contract's Register constant.
	RegisterSentinelMethodName = "Register"
	// RevokeSentinelMethodName names the method that revokes the
	// caller's sentinel and returns the locked ZNN and QSR.
	RevokeSentinelMethodName = "Revoke"

	sentinelInfoVariableName = "sentinelInfo"
)

var (
	// ABISentinel is the parsed ABI of the sentinel embedded
	// contract.
	ABISentinel = abi.JSONToABIContract(strings.NewReader(jsonSentinel))
)

const (
	_ byte = iota
	// sentinelInfoPrefix (1) prefixes sentinelInfo entries; the full
	// key appends the owner address bytes.
	sentinelInfoPrefix
)

// SentinelInfoKey identifies a sentinel by Owner, the address that
// registered it; each address can run at most one sentinel.
type SentinelInfoKey struct {
	Owner types.Address `json:"owner"`
}

// SentinelInfo is the stored state of a sentinel: the registration
// and revocation timestamps in unix seconds (RevokeTimestamp is zero
// while the sentinel is active) and the ZNN and QSR amounts (smallest
// units) locked at registration and returned on revocation.
type SentinelInfo struct {
	SentinelInfoKey
	RegistrationTimestamp int64    `json:"registrationTimestamp"`
	RevokeTimestamp       int64    `json:"revokeTimestamp"`
	ZnnAmount             *big.Int `json:"znnAmount"`
	QsrAmount             *big.Int `json:"qsrAmount"`
}

// Save stores the sentinel under its owner key, panicking via
// common.DealWithErr on database errors.
func (sentinel *SentinelInfo) Save(context db.DB) {
	common.DealWithErr(context.Put(sentinel.Key(), sentinel.Data()))
}

// Delete removes the sentinel entry, panicking via common.DealWithErr
// on database errors.
func (sentinel *SentinelInfo) Delete(context db.DB) {
	common.DealWithErr(context.Delete(sentinel.Key()))
}

// Data packs the full sentinel state, owner included; packing
// failures panic.
func (sentinel *SentinelInfo) Data() []byte {
	return ABISentinel.PackVariablePanic(
		sentinelInfoVariableName,
		sentinel.Owner,
		sentinel.RegistrationTimestamp,
		sentinel.RevokeTimestamp,
		sentinel.ZnnAmount,
		sentinel.QsrAmount)
}

// Key is sentinelInfoPrefix followed by the owner address bytes.
func (sentinel *SentinelInfoKey) Key() []byte {
	return common.JoinBytes([]byte{sentinelInfoPrefix}, sentinel.Owner.Bytes())
}

func parseSentinelInfo(data []byte) *SentinelInfo {
	sentinel := new(SentinelInfo)
	ABISentinel.UnpackVariablePanic(sentinel, sentinelInfoVariableName, data)
	return sentinel
}

// GetSentinelInfoByOwner returns the sentinel registered by address,
// or nil if it has none; database errors panic via
// common.DealWithErr.
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

// GetAllSentinelInfo returns every stored sentinel, in storage-key
// (owner address byte) order; database errors panic via
// common.DealWithErr.
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

// IterateSentinelEntries calls f for each stored sentinel in
// storage-key order, stopping early and returning f's error if it
// fails; database errors panic via common.DealWithErr.
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
