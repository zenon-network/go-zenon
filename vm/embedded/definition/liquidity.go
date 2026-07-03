package definition

import (
	"encoding/json"
	"github.com/pkg/errors"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/db"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/vm/constants"
	"math/big"
	"strings"

	"github.com/zenon-network/go-zenon/vm/abi"
)

const (
	// jsonLiquidity is the ABI JSON of the liquidity embedded
	// contract: liquidity staking, reward funding and configuration,
	// halting and the guardian methods, plus the stored
	// liquidity-info, token-tuple, stake-entry and security-info
	// variables. Parsed into ABILiquidity.
	jsonLiquidity = `
	[
		{"type":"function","name":"Update", "inputs":[]},
		{"type":"function","name":"Donate", "inputs":[]},
		{"type":"function","name":"Fund", "inputs":[
			{"name":"znnReward","type":"uint256"},
			{"name":"qsrReward","type":"uint256"}
		]},
		{"type":"function","name":"BurnZnn", "inputs":[
			{"name":"burnAmount","type":"uint256"}
		]},
		{"type":"function","name":"SetTokenTuple", "inputs":[
			{"name":"tokenStandards","type":"string[]"},
			{"name":"znnPercentages","type":"uint32[]"},
			{"name":"qsrPercentages","type":"uint32[]"},
			{"name":"minAmounts","type":"uint256[]"}
		]},
		{"type":"variable","name":"liquidityInfo","inputs":[
			{"name":"administrator","type":"address"},
			{"name":"isHalted","type":"bool"},
			{"name":"znnReward","type":"uint256"},
			{"name":"qsrReward","type":"uint256"},
			{"name":"tokenTuples","type":"bytes[]"}
		]},
		{"type":"variable","name":"tokenTuple","inputs":[
			{"name":"tokenStandard","type":"string"},
			{"name":"znnPercentage","type":"uint32"},
			{"name":"qsrPercentage","type":"uint32"},
			{"name":"minAmount","type":"uint256"}
		]},
		{"type":"variable", "name":"liquidityStakeEntry", "inputs":[
			{"name":"amount", "type":"uint256"},
			{"name":"tokenStandard", "type":"tokenStandard"},
			{"name":"weightedAmount", "type":"uint256"},
			{"name":"startTime", "type":"int64"},
			{"name":"revokeTime", "type":"int64"},
			{"name":"expirationTime", "type":"int64"}
		]},
		{"type":"function","name":"NominateGuardians","inputs":[
			{"name":"guardians","type":"address[]"}
		]},
		{"type":"function","name":"ProposeAdministrator","inputs":[
			{"name":"address","type":"address"}
		]},
		{"type":"function","name":"Emergency","inputs":[]},

		{"type":"variable","name":"securityInfo","inputs":[
			{"name":"guardians","type":"address[]"},
			{"name":"guardiansVotes","type":"address[]"},
			{"name":"administratorDelay","type":"uint64"},
			{"name":"softDelay","type":"uint64"}
		]},
		{"type":"function","name":"SetIsHalted","inputs":[
			{"name":"isHalted","type":"bool"}
		]},
		{"type":"function","name":"LiquidityStake","inputs":[
			{"name":"durationInSec", "type":"int64"}
		]},
		{"type":"function","name":"CancelLiquidityStake","inputs":[
			{"name":"id","type":"hash"}
		]},
		{"type":"function","name":"UnlockLiquidityStakeEntries","inputs":[]},
		{"type":"function","name":"SetAdditionalReward","inputs":[
			{"name":"znnReward", "type":"uint256"},
			{"name":"qsrReward", "type":"uint256"}
		]},
		{"type":"function","name":"CollectReward","inputs":[]},
		{"type":"function","name":"ChangeAdministrator","inputs":[
			{"name":"administrator","type":"address"}
		]}
	]`

	// FundMethodName names the spork-address method that donates
	// znnReward ZNN and qsrReward QSR from the liquidity contract's
	// balance to the accelerator.
	FundMethodName = "Fund"
	// BurnZnnMethodName names the spork-address method that burns
	// burnAmount ZNN from the liquidity contract's balance.
	BurnZnnMethodName = "BurnZnn"
	// SetTokenTupleMethodName names the administrator method,
	// protected by a soft-delay time challenge, that replaces the
	// set of reward-earning token tuples.
	SetTokenTupleMethodName = "SetTokenTuple"
	// LiquidityStakeMethodName names the method that locks the sent
	// LP tokens as a reward-earning stake for durationInSec seconds.
	LiquidityStakeMethodName = "LiquidityStake"
	// CancelLiquidityStakeMethodName names the method by which a
	// staker revokes an expired stake entry and recovers its tokens.
	CancelLiquidityStakeMethodName = "CancelLiquidityStake"
	// UnlockLiquidityStakeEntriesMethodName names the administrator
	// method that forces every stake entry of the sent block's token
	// to expire immediately, making it cancellable.
	UnlockLiquidityStakeEntriesMethodName = "UnlockLiquidityStakeEntries"
	// SetAdditionalRewardMethodName names the administrator method,
	// protected by a soft-delay time challenge, that sets the extra
	// ZNN and QSR distributed each epoch.
	SetAdditionalRewardMethodName = "SetAdditionalReward"
	// SetIsHaltedMethodName names the administrator method that
	// halts or resumes the liquidity contract.
	SetIsHaltedMethodName = "SetIsHalted"

	liquidityInfoVariableName       = "liquidityInfo"
	tokenTupleVariableName          = "tokenTuple"
	liquidityStakeEntryVariableName = "liquidityStakeEntry"
)

var (
	// ABILiquidity is the parsed ABI of the liquidity embedded
	// contract.
	ABILiquidity = abi.JSONToABIContract(strings.NewReader(jsonLiquidity))

	// LiquidityInfoKeyPrefix is, by itself, the single key under
	// which the LiquidityInfoVariable is stored.
	LiquidityInfoKeyPrefix = []byte{1}
	// LiquidityStakeEntryKeyPrefix prefixes stake entries; the full
	// key appends the 20-byte staker address and the 32-byte entry
	// id.
	LiquidityStakeEntryKeyPrefix = []byte{2}
)

// LiquidityInfoVariable is the stored form of the liquidity
// contract's global state, with the token tuples kept as
// individually ABI-packed byte slices; LiquidityInfo is the unpacked
// form callers receive. Stored as a single value under
// LiquidityInfoKeyPrefix (1).
type LiquidityInfoVariable struct {
	Administrator types.Address `json:"administrator"`
	IsHalted      bool          `json:"isHalted"`
	ZnnReward     *big.Int      `json:"znnReward"`
	QsrReward     *big.Int      `json:"qsrReward"`
	TokenTuples   [][]byte      `json:"tokenTuples"`
}

// LiquidityInfo is the liquidity contract's global state: the
// administrator address, the halted flag that suspends staking, the
// additional ZNN and QSR rewards (smallest units) distributed each
// epoch on top of the protocol emission, and the reward-earning
// token tuples.
type LiquidityInfo struct {
	Administrator types.Address `json:"administrator"`
	IsHalted      bool          `json:"isHalted"`
	ZnnReward     *big.Int      `json:"znnReward"`
	QsrReward     *big.Int      `json:"qsrReward"`
	TokenTuples   []TokenTuple  `json:"tokenTuples"`
}

// LiquidityInfoMarshal is the JSON form of LiquidityInfo, with the
// rewards rendered as base-10 strings to survive clients that parse
// numbers as 64-bit floats.
type LiquidityInfoMarshal struct {
	Administrator types.Address `json:"administrator"`
	IsHalted      bool          `json:"isHalted"`
	ZnnReward     string        `json:"znnReward"`
	QsrReward     string        `json:"qsrReward"`
	TokenTuples   []TokenTuple  `json:"tokenTuples"`
}

// ToLiquidityInfoMarshal converts the state to its JSON form with
// string-encoded rewards.
func (l *LiquidityInfo) ToLiquidityInfoMarshal() LiquidityInfoMarshal {
	aux := LiquidityInfoMarshal{
		Administrator: l.Administrator,
		IsHalted:      l.IsHalted,
		ZnnReward:     l.ZnnReward.String(),
		QsrReward:     l.QsrReward.String(),
	}

	aux.TokenTuples = make([]TokenTuple, len(l.TokenTuples))
	for idx, tuple := range l.TokenTuples {
		aux.TokenTuples[idx] = tuple
	}

	return aux
}

// MarshalJSON encodes the state through LiquidityInfoMarshal.
func (l *LiquidityInfo) MarshalJSON() ([]byte, error) {
	return json.Marshal(l.ToLiquidityInfoMarshal())
}

// UnmarshalJSON decodes the state from its LiquidityInfoMarshal
// form, parsing the string rewards back into big.Int values.
func (l *LiquidityInfo) UnmarshalJSON(data []byte) error {
	aux := new(LiquidityInfoMarshal)
	if err := json.Unmarshal(data, aux); err != nil {
		return err
	}
	l.Administrator = aux.Administrator
	l.IsHalted = aux.IsHalted
	l.ZnnReward = common.StringToBigInt(aux.ZnnReward)
	l.QsrReward = common.StringToBigInt(aux.QsrReward)
	l.TokenTuples = make([]TokenTuple, len(aux.TokenTuples))
	for idx, tuple := range aux.TokenTuples {
		l.TokenTuples[idx] = tuple
	}
	return nil
}

// Save stores the packed state under LiquidityInfoKeyPrefix,
// returning any pack or put error.
func (liq *LiquidityInfoVariable) Save(context db.DB) error {
	data, err := ABILiquidity.PackVariable(
		liquidityInfoVariableName,
		liq.Administrator,
		liq.IsHalted,
		liq.ZnnReward,
		liq.QsrReward,
		liq.TokenTuples,
	)
	if err != nil {
		return err
	}
	return context.Put(
		LiquidityInfoKeyPrefix,
		data,
	)
}
func parseLiquidityInfo(data []byte) (*LiquidityInfo, error) {
	if len(data) > 0 {
		liquidityInfoVariable := new(LiquidityInfoVariable)
		if err := ABILiquidity.UnpackVariable(liquidityInfoVariable, liquidityInfoVariableName, data); err != nil {
			return nil, err
		}
		tokenTuples := make([]TokenTuple, 0)
		for _, token := range liquidityInfoVariable.TokenTuples {
			tokenTuple := new(TokenTuple)
			if err := ABILiquidity.UnpackVariable(tokenTuple, tokenTupleVariableName, token); err != nil {
				continue
			}
			tokenTuples = append(tokenTuples, *tokenTuple)
		}
		liquidityInfo := &LiquidityInfo{
			Administrator: liquidityInfoVariable.Administrator,
			TokenTuples:   tokenTuples,
			IsHalted:      liquidityInfoVariable.IsHalted,
			ZnnReward:     liquidityInfoVariable.ZnnReward,
			QsrReward:     liquidityInfoVariable.QsrReward,
		}
		return liquidityInfo, nil
	} else {
		return &LiquidityInfo{
			Administrator: constants.InitialBridgeAdministrator,
			TokenTuples:   nil,
			IsHalted:      false,
			ZnnReward:     common.Big0,
			QsrReward:     common.Big0,
		}, nil
	}
}

// GetLiquidityInfo returns the liquidity contract's state with its
// token tuples unpacked; tuples that fail to unpack are skipped.
// When nothing is stored it returns defaults instead of an error:
// constants.InitialBridgeAdministrator as administrator, not halted,
// zero rewards and no tuples.
func GetLiquidityInfo(context db.DB) (*LiquidityInfo, error) {
	if data, err := context.Get(LiquidityInfoKeyPrefix); err != nil {
		return nil, err
	} else {
		upd, err := parseLiquidityInfo(data)
		return upd, err
	}
}

// EncodeLiquidityInfo converts a LiquidityInfo into its stored form,
// ABI-packing each token tuple into a byte slice; it returns the
// first pack error encountered.
func EncodeLiquidityInfo(liquidityInfo *LiquidityInfo) (*LiquidityInfoVariable, error) {
	liquidityInfoVariable := new(LiquidityInfoVariable)
	if err := liquidityInfoVariable.Administrator.SetBytes(liquidityInfo.Administrator.Bytes()); err != nil {
		return nil, err
	}
	liquidityInfoVariable.IsHalted = liquidityInfo.IsHalted
	liquidityInfoVariable.ZnnReward = liquidityInfo.ZnnReward
	liquidityInfoVariable.QsrReward = liquidityInfo.QsrReward
	tokenTuples := make([][]byte, 0)
	for _, token := range liquidityInfo.TokenTuples {
		if tokenTuple, err := ABILiquidity.PackVariable(tokenTupleVariableName, token.TokenStandard, token.ZnnPercentage, token.QsrPercentage, token.MinAmount); err != nil {
			return nil, err
		} else {
			tokenTuples = append(tokenTuples, tokenTuple)
		}
	}
	liquidityInfoVariable.TokenTuples = tokenTuples
	return liquidityInfoVariable, nil
}

// TokenTuple is the reward configuration of one LP token:
// TokenStandard is the ZTS in string form, ZnnPercentage and
// QsrPercentage are its shares of the epoch's liquidity ZNN and QSR
// rewards in basis points (each set must sum to
// constants.LiquidityZnnTotalPercentages and
// constants.LiquidityQsrTotalPercentages, 10,000 = 100%) and
// MinAmount (smallest units) is the smallest stake accepted.
type TokenTuple struct {
	TokenStandard string   `json:"tokenStandard"`
	ZnnPercentage uint32   `json:"znnPercentage"`
	QsrPercentage uint32   `json:"qsrPercentage"`
	MinAmount     *big.Int `json:"minAmount"`
}

// TokenTupleMarshal is the JSON form of TokenTuple, with the minimum
// amount rendered as a base-10 string to survive clients that parse
// numbers as 64-bit floats.
type TokenTupleMarshal struct {
	TokenStandard string `json:"tokenStandard"`
	ZnnPercentage uint32 `json:"znnPercentage"`
	QsrPercentage uint32 `json:"qsrPercentage"`
	MinAmount     string `json:"minAmount"`
}

// ToTokenTupleMarshal converts the tuple to its JSON form with a
// string-encoded minimum amount.
func (s *TokenTuple) ToTokenTupleMarshal() *TokenTupleMarshal {
	aux := &TokenTupleMarshal{
		TokenStandard: s.TokenStandard,
		ZnnPercentage: s.ZnnPercentage,
		QsrPercentage: s.QsrPercentage,
		MinAmount:     s.MinAmount.String(),
	}
	return aux
}

// MarshalJSON encodes the tuple through TokenTupleMarshal.
func (s *TokenTuple) MarshalJSON() ([]byte, error) {
	return json.Marshal(s.ToTokenTupleMarshal())
}

// UnmarshalJSON decodes the tuple from its TokenTupleMarshal form,
// parsing the string amount back into a big.Int.
func (s *TokenTuple) UnmarshalJSON(data []byte) error {
	aux := new(TokenTupleMarshal)
	if err := json.Unmarshal(data, aux); err != nil {
		return err
	}
	s.TokenStandard = aux.TokenStandard
	s.ZnnPercentage = aux.ZnnPercentage
	s.QsrPercentage = aux.QsrPercentage
	s.MinAmount = common.StringToBigInt(aux.MinAmount)

	return nil
}

// FundParam carries the arguments of Fund: the ZNN and QSR amounts
// (smallest units) to donate to the accelerator.
type FundParam struct {
	ZnnReward *big.Int
	QsrReward *big.Int
}

// BurnParam carries the argument of BurnZnn: the ZNN amount
// (smallest units) to burn from the contract's balance.
type BurnParam struct {
	BurnAmount *big.Int
}

// TokenTuplesParam carries the arguments of SetTokenTuple as
// parallel arrays, one element per TokenTuple.
type TokenTuplesParam struct {
	TokenStandards []string
	ZnnPercentages []uint32
	QsrPercentages []uint32
	MinAmounts     []*big.Int
}

// SetAdditionalRewardParam carries the arguments of
// SetAdditionalReward: the extra ZNN and QSR (smallest units)
// distributed each epoch on top of the protocol emission.
type SetAdditionalRewardParam struct {
	ZnnReward *big.Int
	QsrReward *big.Int
}

// LiquidityStakeEntry is one liquidity stake: Amount (smallest
// units) of TokenStandard locked from StartTime until ExpirationTime
// (unix seconds), with WeightedAmount the duration-weighted amount
// reward shares are computed from. RevokeTime is zero while the
// stake is active; CancelLiquidityStake sets it and zeroes Amount
// after returning the tokens. Id is the hash of the LiquidityStake
// send block. Entries are stored under LiquidityStakeEntryKeyPrefix
// (2) followed by the 20-byte staker address and the 32-byte id;
// both are recovered from the key when parsing.
type LiquidityStakeEntry struct {
	Amount         *big.Int                 `json:"amount"`
	TokenStandard  types.ZenonTokenStandard `json:"tokenStandard"`
	WeightedAmount *big.Int                 `json:"weightedAmount"`
	StartTime      int64                    `json:"startTime"`
	RevokeTime     int64                    `json:"revokeTime"`
	ExpirationTime int64                    `json:"expirationTime"`
	StakeAddress   types.Address            `json:"stakeAddress"`
	Id             types.Hash               `json:"id"`
}

// Save stores the entry under its address+id key, packing all fields
// except the id and stake address; packing failures panic, the put
// error is returned.
func (stake *LiquidityStakeEntry) Save(context db.DB) error {
	return context.Put(
		getLiquidityStakeEntryKey(stake.Id, stake.StakeAddress),
		ABILiquidity.PackVariablePanic(
			liquidityStakeEntryVariableName,
			stake.Amount,
			stake.TokenStandard,
			stake.WeightedAmount,
			stake.StartTime,
			stake.RevokeTime,
			stake.ExpirationTime,
		))
}

// Delete removes the stake entry.
func (stake *LiquidityStakeEntry) Delete(context db.DB) error {
	return context.Delete(getLiquidityStakeEntryKey(stake.Id, stake.StakeAddress))
}

func getLiquidityStakeEntryKey(id types.Hash, address types.Address) []byte {
	return append(append(LiquidityStakeEntryKeyPrefix, address.Bytes()...), id.Bytes()...)
}
func isLiquidityStakeEntryKey(key []byte) bool {
	return key[0] == LiquidityStakeEntryKeyPrefix[0]
}
func unmarshalLiquidityStakeEntryKey(key []byte) (*types.Hash, *types.Address, error) {
	if !isLiquidityStakeEntryKey(key) {
		return nil, nil, errors.Errorf("invalid key! Not liquidity stake entry key")
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
func parseLiquidityStakeEntry(key []byte, data []byte) (*LiquidityStakeEntry, error) {
	if len(data) > 0 {
		entry := new(LiquidityStakeEntry)
		err := ABILiquidity.UnpackVariable(entry, liquidityStakeEntryVariableName, data)
		if err != nil {
			return nil, err
		}

		id, address, err := unmarshalLiquidityStakeEntryKey(key)
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

// GetLiquidityStakeEntry returns the stake entry of id and address,
// or constants.ErrDataNonExistent if it does not exist.
func GetLiquidityStakeEntry(context db.DB, id types.Hash, address types.Address) (*LiquidityStakeEntry, error) {
	key := getLiquidityStakeEntryKey(id, address)
	if data, err := context.Get(key); err != nil {
		return nil, err
	} else {
		return parseLiquidityStakeEntry(key, data)
	}
}

// IterateLiquidityStakeEntries calls f on every stored stake entry
// in storage-key (staker address, then id) order, stopping at the
// first error f returns.
func IterateLiquidityStakeEntries(context db.DB, f func(entry *LiquidityStakeEntry) error) error {
	iterator := context.NewIterator(LiquidityStakeEntryKeyPrefix)
	defer iterator.Release()

	for {
		if !iterator.Next() {
			if iterator.Error() != nil {
				return iterator.Error()
			}
			break
		}

		if stakeEntry, err := parseLiquidityStakeEntry(iterator.Key(), iterator.Value()); err == nil {
			if err := f(stakeEntry); err != nil {
				return err
			}
		} else if err == constants.ErrDataNonExistent {
		} else {
			return err
		}
	}
	return nil
}

// LiquidityStakeEntryMarshal is the JSON form of
// LiquidityStakeEntry, with the amounts rendered as base-10 strings
// to survive clients that parse numbers as 64-bit floats.
type LiquidityStakeEntryMarshal struct {
	Amount         string                   `json:"amount"`
	TokenStandard  types.ZenonTokenStandard `json:"tokenStandard"`
	WeightedAmount string                   `json:"weightedAmount"`
	StartTime      int64                    `json:"startTime"`
	RevokeTime     int64                    `json:"revokeTime"`
	ExpirationTime int64                    `json:"expirationTime"`
	StakeAddress   types.Address            `json:"stakeAddress"`
	Id             types.Hash               `json:"id"`
}

// ToLiquidityStakeEntry converts the entry to its JSON form with
// string-encoded amounts; despite the name it returns a
// LiquidityStakeEntryMarshal.
func (stake *LiquidityStakeEntry) ToLiquidityStakeEntry() *LiquidityStakeEntryMarshal {
	aux := &LiquidityStakeEntryMarshal{
		Amount:         stake.Amount.String(),
		TokenStandard:  stake.TokenStandard,
		WeightedAmount: stake.WeightedAmount.String(),
		StartTime:      stake.StartTime,
		RevokeTime:     stake.RevokeTime,
		ExpirationTime: stake.ExpirationTime,
		StakeAddress:   stake.StakeAddress,
		Id:             stake.Id,
	}
	return aux
}

// MarshalJSON encodes the entry through LiquidityStakeEntryMarshal.
func (stake *LiquidityStakeEntry) MarshalJSON() ([]byte, error) {
	return json.Marshal(stake.ToLiquidityStakeEntry())
}

// UnmarshalJSON decodes the entry from its
// LiquidityStakeEntryMarshal form, parsing the string amounts back
// into big.Int values.
func (stake *LiquidityStakeEntry) UnmarshalJSON(data []byte) error {
	aux := new(LiquidityStakeEntryMarshal)
	if err := json.Unmarshal(data, aux); err != nil {
		return err
	}
	stake.Amount = common.StringToBigInt(aux.Amount)
	stake.TokenStandard = aux.TokenStandard
	stake.WeightedAmount = common.StringToBigInt(aux.WeightedAmount)
	stake.StartTime = aux.StartTime
	stake.RevokeTime = aux.RevokeTime
	stake.ExpirationTime = aux.ExpirationTime
	stake.StakeAddress = aux.StakeAddress
	stake.Id = aux.Id
	return nil
}

// GetLiquidityStakeListByAddress returns the active (not revoked)
// stake entries of address in storage-key order, together with their
// total staked and total weighted amounts.
func GetLiquidityStakeListByAddress(context db.DB, address types.Address) ([]*LiquidityStakeEntry, *big.Int, *big.Int, error) {
	total := big.NewInt(0)
	weighted := big.NewInt(0)
	list := make([]*LiquidityStakeEntry, 0)

	err := IterateLiquidityStakeEntries(context, func(stakeEntry *LiquidityStakeEntry) error {
		if stakeEntry.RevokeTime == 0 && stakeEntry.StakeAddress == address {
			list = append(list, stakeEntry)
			total.Add(total, stakeEntry.Amount)
			weighted.Add(weighted, stakeEntry.WeightedAmount)
		}
		return nil
	})
	if err != nil {
		return nil, nil, nil, err
	} else {
		return list, total, weighted, nil
	}
}

// GetAllLiquidityStakeEntries returns every stored stake entry,
// active or revoked, in storage-key (staker address, then id) order;
// database errors panic via common.DealWithErr.
func GetAllLiquidityStakeEntries(context db.DB) []*LiquidityStakeEntry {
	iterator := context.NewIterator(LiquidityStakeEntryKeyPrefix)
	defer iterator.Release()

	liquidityStakeEntries := make([]*LiquidityStakeEntry, 0)
	for {
		if !iterator.Next() {
			common.DealWithErr(iterator.Error())
			break
		}
		liquidityStakeEntry, err := parseLiquidityStakeEntry(iterator.Key(), iterator.Value())
		if err != nil {
			continue
		}
		liquidityStakeEntries = append(liquidityStakeEntries, liquidityStakeEntry)
	}
	return liquidityStakeEntries
}

// LiquidityStakeByExpirationTime implements sort.Interface over
// stake entries, ordering by ascending expiration time
// (soonest-expiring first) with ties broken by ascending id (hex
// string comparison, equivalent to byte order).
type LiquidityStakeByExpirationTime []*LiquidityStakeEntry

// Len implements sort.Interface.
func (a LiquidityStakeByExpirationTime) Len() int { return len(a) }

// Swap implements sort.Interface.
func (a LiquidityStakeByExpirationTime) Swap(i, j int) { a[i], a[j] = a[j], a[i] }

// Less orders by ascending expiration time, then ascending id.
func (a LiquidityStakeByExpirationTime) Less(i, j int) bool {
	if a[i].ExpirationTime == a[j].ExpirationTime {
		return a[i].Id.String() < a[j].Id.String()
	}
	return a[i].ExpirationTime < a[j].ExpirationTime
}
