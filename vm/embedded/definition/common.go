// Package definition declares the on-chain interface of every
// embedded contract: its ABI (a JSON contract parsed by
// abi.JSONToABIContract listing the callable functions and the stored
// variables), the method and variable name constants, the storage-key
// layout of each variable and typed Go structs with Save/Get/parse
// helpers around them. The sibling package
// vm/embedded/implementation provides the executable methods behind
// these definitions, and the RPC APIs in rpc/api/embedded read
// contract state exclusively through the getters defined here.
//
// Storage conventions: values live in the owning contract's
// key-value storage under a key built from a one-byte prefix followed
// by the bytes identifying the entry (an address, a hash, an epoch
// number or a name). Contract-specific prefixes count up from 1
// (except the accelerator's, which start at 12 — see accelerator.go —
// and the swap contract, which stores its entries under bare hashes
// with no prefix), while the prefixes of the shared ABICommon
// variables start at 128, so both sets coexist in one contract's key
// space without colliding. Token amounts are big.Int values in the token's
// smallest units, timestamps are unix seconds, heights count
// momentums and epochs count daily reward periods; multi-byte epoch
// numbers are encoded little-endian inside keys. Save methods either
// return the pack/put error (when packing with PackVariable) or panic
// through common.DealWithErr (when packing with PackVariablePanic);
// Get helpers translate a missing entry into
// constants.ErrDataNonExistent or into a zero-valued default, as
// documented on each helper.
//
// ABICommon holds the definitions shared by several contracts: reward
// bookkeeping, QSR registration deposits, periodic update markers,
// pillar voting and the time-challenge security scheme used by the
// bridge and liquidity contracts.
package definition

import (
	"encoding/binary"
	"encoding/json"
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
	// VoteYes counts the pillar in favor of the votable hash.
	VoteYes uint8 = iota
	// VoteNo counts the pillar against the votable hash.
	VoteNo
	// VoteAbstain adds the pillar to the turnout (VoteBreakdown.Total)
	// without counting toward Yes or No.
	VoteAbstain
	// VoteNotValid is the first invalid vote value; the vote methods
	// reject any vote greater than or equal to it with
	// constants.ErrForbiddenParam.
	VoteNotValid

	// jsonCommon is the ABI JSON shared by several embedded
	// contracts: the reward, deposit and update variables, the
	// Update/CollectReward/DepositQsr/WithdrawQsr/Donate methods, the
	// pillar-vote methods and variables, and the time-challenge and
	// security-info variables. Parsed into ABICommon.
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
		{"type":"variable","name":"pillarVote","inputs":[
			{"name":"id","type":"hash"},
			{"name":"name","type":"string"},
			{"name":"vote","type":"uint8"}
		]},
		{"type":"variable","name":"votableHash","inputs":[
			{"name":"exists","type":"bool"}
		]},


		{"type":"function","name":"Update", "inputs":[]},
		{"type":"function","name":"CollectReward","inputs":[]},
		{"type":"function","name":"DepositQsr", "inputs":[]},
		{"type":"function","name":"WithdrawQsr", "inputs":[]},
		{"type":"function","name":"Donate", "inputs":[]},

		{"type":"function","name":"VoteByName","inputs":[
			{"name":"id","type":"hash"},
			{"name":"name","type":"string"},
			{"name":"vote","type":"uint8"}
		]},
		{"type":"function","name":"VoteByProdAddress","inputs":[
			{"name":"id","type":"hash"},
			{"name":"vote","type":"uint8"}
		]},
		{"type":"variable","name":"timeChallengeInfo","inputs":[
			{"name":"methodName","type":"string"},
			{"name":"paramsHash","type":"hash"},
			{"name":"challengeStartHeight","type":"uint64"}
		]},
		{"type":"variable","name":"securityInfo","inputs":[
			{"name":"guardians","type":"address[]"},
			{"name":"guardiansVotes","type":"address[]"},
			{"name":"administratorDelay","type":"uint64"},
			{"name":"softDelay","type":"uint64"}
		]}
	]`

	// RewardDepositVariableName is the ABI variable holding the
	// uncollected ZNN and QSR rewards of an address.
	RewardDepositVariableName = "rewardDeposit"
	// RewardDepositHistoryVariableName is the ABI variable holding
	// the rewards credited to an address in a single epoch.
	RewardDepositHistoryVariableName = "rewardDepositHistory"
	// LastUpdateVariableName is the ABI variable holding the momentum
	// height of a contract's last Update run.
	LastUpdateVariableName = "lastUpdate"
	// QsrDepositVariableName is the ABI variable holding the QSR an
	// address has deposited toward a registration.
	QsrDepositVariableName = "qsrDeposit"
	// LastEpochUpdateVariableName is the ABI variable holding the
	// last epoch a contract has fully processed.
	LastEpochUpdateVariableName = "lastEpochUpdate"
	// PillarVoteVariableName is the ABI variable holding one pillar's
	// vote on a votable hash.
	PillarVoteVariableName = "pillarVote"
	// VotableHashVariableName is the ABI variable marking a hash as
	// open for pillar voting.
	VotableHashVariableName       = "votableHash"
	timeChallengeInfoVariableName = "timeChallengeInfo"
	securityInfoVariableName      = "securityInfo"

	// UpdateMethodName names the shared method that advances a
	// contract's reward bookkeeping; calls are throttled to once
	// every constants.UpdateMinNumMomentums momentums.
	UpdateMethodName = "Update"
	// CollectRewardMethodName names the shared method that mints a
	// caller's accumulated RewardDeposit out to its address.
	CollectRewardMethodName = "CollectReward"
	// DepositQsrMethodName names the shared method that adds the sent
	// QSR to the caller's QsrDeposit.
	DepositQsrMethodName = "DepositQsr"
	// WithdrawQsrMethodName names the shared method that refunds the
	// caller's entire QsrDeposit.
	WithdrawQsrMethodName = "WithdrawQsr"
	// DonateMethodName names the shared method that accepts any
	// tokens as a donation to the contract.
	DonateMethodName = "Donate"
	// VoteByNameMethodName names the vote method addressed by pillar
	// name; the sender must be the pillar's stake address.
	VoteByNameMethodName = "VoteByName"
	// VoteByProdAddressMethodName names the vote method where the
	// sender is identified as a pillar's block-producing address.
	VoteByProdAddressMethodName = "VoteByProdAddress"
	// ChangeAdministratorMethodName names the administrator-change
	// method declared in the bridge and liquidity ABIs; the constant
	// lives here because both contracts share it.
	ChangeAdministratorMethodName = "ChangeAdministrator"
	// EmergencyMethodName names the bridge/liquidity method by which
	// the administrator halts the contract and steps down.
	EmergencyMethodName = "Emergency"
	// NominateGuardiansMethodName names the bridge/liquidity method
	// by which the administrator appoints the guardian set.
	NominateGuardiansMethodName = "NominateGuardians"
	// ProposeAdministratorMethodName names the bridge/liquidity
	// method by which guardians vote a new administrator into place
	// while the contract is in emergency.
	ProposeAdministratorMethodName = "ProposeAdministrator"
)

var (
	// ABICommon is the parsed shared ABI; its variables are stored in
	// each participating contract's own storage under the key
	// prefixes below.
	ABICommon = abi.JSONToABIContract(strings.NewReader(jsonCommon))

	// The shared key prefixes start at 128 to stay clear of the
	// contract-specific prefixes; see the package comment.
	rewardDepositKeyPrefix        = []byte{128}
	lastUpdateKey                 = []byte{129}
	qsrDepositKeyPrefix           = []byte{130}
	lastEpochUpdateKey            = []byte{131}
	rewardDepositHistoryKeyPrefix = []byte{132}
	pillarVoteKeyPrefix           = []byte{133}
	votableHashKeyPrefix          = []byte{134}
	// TimeChallengeKeyPrefix prefixes time-challenge entries; the
	// full key appends the SHA3-256 hash of the challenged method
	// name.
	TimeChallengeKeyPrefix = []byte{135}
	// SecurityInfoKeyPrefix is, by itself, the single key under which
	// a contract's SecurityInfoVariable is stored.
	SecurityInfoKeyPrefix = []byte{136}
)

// RewardDeposit accumulates the rewards Address can collect from a
// contract: ZNN and QSR amounts in their smallest units. The Update
// methods credit it and CollectReward mints it out and deletes the
// entry. Stored under rewardDepositKeyPrefix (128) followed by the
// address bytes; only the amounts are packed, the address is
// recovered from the key.
type RewardDeposit struct {
	Address *types.Address `json:"address"`
	Znn     *big.Int       `json:"znnAmount"`
	Qsr     *big.Int       `json:"qsrAmount"`
}

// RewardDepositMarshal is the JSON form of RewardDeposit, with the
// amounts rendered as base-10 strings to survive clients that parse
// numbers as 64-bit floats.
type RewardDepositMarshal struct {
	Address *types.Address `json:"address"`
	Znn     string         `json:"znnAmount"`
	Qsr     string         `json:"qsrAmount"`
}

// ToRewardDepositMarshal converts the deposit to its JSON form with
// string-encoded amounts.
func (deposit *RewardDeposit) ToRewardDepositMarshal() *RewardDepositMarshal {
	aux := &RewardDepositMarshal{
		Address: deposit.Address,
		Znn:     deposit.Znn.String(),
		Qsr:     deposit.Qsr.String(),
	}

	return aux
}

// MarshalJSON encodes the deposit through RewardDepositMarshal.
func (deposit *RewardDeposit) MarshalJSON() ([]byte, error) {
	return json.Marshal(deposit.ToRewardDepositMarshal())
}

// UnmarshalJSON decodes the deposit from its RewardDepositMarshal
// form, parsing the string amounts back into big.Int values.
func (deposit *RewardDeposit) UnmarshalJSON(data []byte) error {
	aux := new(RewardDepositMarshal)
	if err := json.Unmarshal(data, aux); err != nil {
		return err
	}
	deposit.Address = aux.Address
	deposit.Znn = common.StringToBigInt(aux.Znn)
	deposit.Qsr = common.StringToBigInt(aux.Qsr)
	return nil
}

// Save stores the deposit under its address key, packing only the two
// amounts; packing failures panic, the put error is returned.
func (deposit *RewardDeposit) Save(context db.DB) error {
	return context.Put(
		getRewardDepositKey(deposit.Address),
		ABICommon.PackVariablePanic(
			RewardDepositVariableName,
			deposit.Znn,
			deposit.Qsr,
		))
}

// Delete removes the deposit entry of the address.
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

// GetRewardDeposit returns the uncollected ZNN and QSR rewards of
// address. A missing entry is not an error: it yields a deposit with
// both amounts zero, so callers never see
// constants.ErrDataNonExistent.
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

// LastUpdateVariable records the momentum height at which a contract
// last ran its Update method; the implementation uses it to throttle
// updates to one every constants.UpdateMinNumMomentums momentums.
// Stored under the single lastUpdateKey (129).
type LastUpdateVariable struct {
	Height uint64
}

// Save stores the height under lastUpdateKey, returning any pack or
// put error.
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

// GetLastUpdate returns the contract's last-update marker; before the
// first Update it returns a variable with Height 0 instead of an
// error.
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

// QsrDeposit holds the QSR (smallest units) an address has deposited
// toward a future registration: DepositQsr adds to it, registering a
// pillar or sentinel consumes the required amount and WithdrawQsr
// refunds the rest and deletes the entry. Stored under
// qsrDepositKeyPrefix (130) followed by the address bytes.
type QsrDeposit struct {
	Address *types.Address
	Qsr     *big.Int
}

// Save stores the deposited amount under the address key, returning
// any pack or put error; the address is recovered from the key when
// parsing.
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

// Delete removes the deposit entry of the address.
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

// GetQsrDeposit returns the QSR address has deposited toward a pillar
// or sentinel registration. A missing entry is not an error: it
// yields a deposit with amount zero, so callers never see
// constants.ErrDataNonExistent.
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

// LastEpochUpdate records the last daily reward epoch a contract has
// fully processed; the epoch-update helpers advance it one epoch at a
// time. Stored under the single lastEpochUpdateKey (131).
type LastEpochUpdate struct {
	LastEpoch int64
}

// Save stores the last processed epoch under lastEpochUpdateKey,
// returning any pack or put error.
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

// GetLastEpochUpdate returns the contract's epoch-update marker;
// before any epoch has been processed it returns LastEpoch -1 instead
// of an error.
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

// RewardDepositHistory is one epoch's reward record of an address:
// the ZNN and QSR amounts (smallest units) credited to Address for
// Epoch. Entries are stored under rewardDepositHistoryKeyPrefix (132)
// followed by the address bytes and the epoch as 8 little-endian
// bytes, so all epochs of one address share a key prefix.
type RewardDepositHistory struct {
	Epoch   uint64
	Address *types.Address `json:"address"`
	Znn     *big.Int       `json:"znnAmount"`
	Qsr     *big.Int       `json:"qsrAmount"`
}

// Save stores the entry under its address+epoch key, packing only the
// amounts and returning any pack or put error; epoch and address are
// recovered from the key when parsing.
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

// GetRewardDepositHistory returns the rewards credited to address in
// epoch. A missing entry is not an error: it yields a record with
// both amounts zero, so callers never see
// constants.ErrDataNonExistent.
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

// PillarVote is one pillar's vote on a votable hash: Id identifies
// the poll (for example an accelerator project or phase), Name the
// voting pillar and Vote one of VoteYes, VoteNo or VoteAbstain. A
// pillar voting again on the same id overwrites its previous vote.
type PillarVote struct {
	Id   types.Hash `json:"id"`
	Name string     `json:"name"`
	Vote uint8      `json:"vote"`
}

// Save stores the vote under its (id, pillar) key, panicking via
// common.DealWithErr on database errors.
func (vote *PillarVote) Save(context db.DB) {
	common.DealWithErr(context.Put(vote.Key(), vote.Data()))
}

// Delete removes the vote, panicking via common.DealWithErr on
// database errors.
func (vote *PillarVote) Delete(context db.DB) {
	common.DealWithErr(context.Delete(vote.Key()))
}

// Key is pillarVoteKeyPrefix (133) followed by the 32-byte id and the
// first 20 bytes of the SHA3-256 hash of the pillar name, giving one
// vote slot per (id, pillar) pair.
func (vote *PillarVote) Key() []byte {
	nameHash := crypto.Hash([]byte(vote.Name))[:20]
	return common.JoinBytes(pillarVoteKeyPrefix, vote.Id.Bytes(), nameHash)
}

// Data packs the full vote (id, name and choice); packing failures
// panic.
func (vote *PillarVote) Data() []byte {
	return ABICommon.PackVariablePanic(
		PillarVoteVariableName,
		vote.Id,
		vote.Name,
		vote.Vote,
	)
}

func parsePillarVote(data []byte) (*PillarVote, error) {
	if len(data) > 0 {
		pillarVote := new(PillarVote)
		ABICommon.UnpackVariablePanic(pillarVote, PillarVoteVariableName, data)
		return pillarVote, nil
	} else {
		return nil, constants.ErrDataNonExistent
	}
}

// GetAllPillarVotes returns every recorded vote for id. It scans the
// whole pillarVote prefix and filters by id in memory; database
// errors panic via common.DealWithErr.
func GetAllPillarVotes(context db.DB, id types.Hash) []*PillarVote {
	iterator := context.NewIterator(pillarVoteKeyPrefix)
	defer iterator.Release()

	pillarVoteList := make([]*PillarVote, 0)
	for {
		if !iterator.Next() {
			common.DealWithErr(iterator.Error())
			break
		}
		pillarVote, err := parsePillarVote(iterator.Value())
		if err != nil {
			continue
		}
		if pillarVote.Id == id {
			pillarVoteList = append(pillarVoteList, pillarVote)
		}
	}
	return pillarVoteList
}

// GetPillarVote returns the vote the named pillar cast on id, or
// constants.ErrDataNonExistent if it has not voted.
func GetPillarVote(context db.DB, id types.Hash, name string) (*PillarVote, error) {
	key := (&PillarVote{Id: id, Name: name}).Key()
	if data, err := context.Get(key); err != nil {
		return nil, err
	} else {
		return parsePillarVote(data)
	}
}

// VotableHash marks a hash as open for pillar voting; the accelerator
// saves one when a project or phase is submitted and the vote methods
// refuse to record votes on ids without an entry. Its presence is the
// flag: Exists merely mirrors the stored boolean when read back.
type VotableHash struct {
	Id     types.Hash
	Exists bool
}

// Save stores the marker under its id key, panicking via
// common.DealWithErr on database errors.
func (votable *VotableHash) Save(context db.DB) {
	common.DealWithErr(context.Put(votable.Key(), votable.Data()))
}

// Delete removes the marker, closing the id for voting; database
// errors panic via common.DealWithErr.
func (votable *VotableHash) Delete(context db.DB) {
	common.DealWithErr(context.Delete(votable.Key()))
}

// Key is votableHashKeyPrefix (134) followed by the 32-byte id.
func (votable *VotableHash) Key() []byte {
	return common.JoinBytes(votableHashKeyPrefix, votable.Id.Bytes())
}

// Data packs the exists flag as true unconditionally, ignoring the
// Exists field; packing failures panic.
func (votable *VotableHash) Data() []byte {
	return ABICommon.PackVariablePanic(
		VotableHashVariableName,
		true,
	)
}

func unmarshalVotableHashKey(key []byte) (*types.Hash, error) {
	id := new(types.Hash)
	if err := id.SetBytes(key[1 : types.HashSize+1]); err != nil {
		return nil, err
	}
	return id, nil
}

func parseVotableHash(data []byte, key []byte) (*VotableHash, error) {
	if len(data) > 0 {
		votableHash := new(VotableHash)
		if err := ABICommon.UnpackVariable(votableHash, VotableHashVariableName, data); err != nil {
			return nil, err
		}
		if h, err := unmarshalVotableHashKey(key); err != nil {
			return nil, err
		} else {
			votableHash.Id = *h
		}
		return votableHash, nil
	} else {
		return nil, constants.ErrDataNonExistent
	}
}

// GetVotableHash returns the marker for id, or
// constants.ErrDataNonExistent if the hash is not open for voting.
func GetVotableHash(context db.DB, id types.Hash) (*VotableHash, error) {
	key := (&VotableHash{Id: id}).Key()
	if data, err := context.Get(key); err != nil {
		return nil, err
	} else {
		return parseVotableHash(data, key)
	}
}

// VoteBreakdown tallies the recorded votes for a votable hash: Total
// counts every vote including abstentions, Yes and No the respective
// choices. The accelerator accepts an id when Total exceeds
// constants.VoteAcceptanceThreshold percent of the registered pillars
// and Yes outnumbers No.
type VoteBreakdown struct {
	Id    types.Hash `json:"id"`
	Total uint32     `json:"total"`
	Yes   uint32     `json:"yes"`
	No    uint32     `json:"no"`
}

// GetVoteBreakdown tallies all votes recorded for id via
// GetAllPillarVotes; an id nobody voted on yields all-zero counts.
func GetVoteBreakdown(context db.DB, id types.Hash) *VoteBreakdown {
	votes := GetAllPillarVotes(context, id)
	voteBreakdown := &VoteBreakdown{
		Id:    id,
		Total: 0,
		Yes:   0,
		No:    0,
	}
	for _, vote := range votes {
		voteBreakdown.Total += 1
		if vote.Vote == VoteYes {
			voteBreakdown.Yes += 1
		} else if vote.Vote == VoteNo {
			voteBreakdown.No += 1
		}
	}
	return voteBreakdown
}

// TimeChallengeInfo is the state of a time challenge: a sensitive
// bridge or liquidity action must be submitted twice with identical
// parameters, separated by a configured number of momentums (the
// security info's soft or administrator delay). ParamsHash is the
// hash of the pending parameters — zeroed once the challenge has been
// satisfied — and ChallengeStartHeight the momentum height of the
// first submission. One challenge slot exists per method name.
type TimeChallengeInfo struct {
	MethodName           string
	ParamsHash           types.Hash
	ChallengeStartHeight uint64
}

// Save stores the challenge under its method-name key, returning any
// pack or put error.
func (t *TimeChallengeInfo) Save(context db.DB) error {
	data, err := ABICommon.PackVariable(
		timeChallengeInfoVariableName,
		t.MethodName,
		t.ParamsHash,
		t.ChallengeStartHeight,
	)
	if err != nil {
		return err
	}
	return context.Put(
		t.Key(),
		data,
	)
}
func parseTimeChallengeInfoVariable(data []byte) (*TimeChallengeInfo, error) {
	if len(data) > 0 {
		timeChallengeInfo := new(TimeChallengeInfo)
		if err := ABICommon.UnpackVariable(timeChallengeInfo, timeChallengeInfoVariableName, data); err != nil {
			return nil, err
		}
		return timeChallengeInfo, nil
	} else {
		return nil, constants.ErrDataNonExistent
	}
}

func timeChallengeKey(methodName string) []byte {
	return common.JoinBytes(TimeChallengeKeyPrefix, crypto.Hash([]byte(methodName)))
}

// GetTimeChallengeInfoVariable returns the challenge state of
// methodName; when no challenge has ever been recorded it returns
// nil with a nil error, not constants.ErrDataNonExistent.
func GetTimeChallengeInfoVariable(context db.DB, methodName string) (*TimeChallengeInfo, error) {
	if data, err := context.Get(timeChallengeKey(methodName)); err != nil {
		return nil, err
	} else {
		upd, err := parseTimeChallengeInfoVariable(data)
		if err == constants.ErrDataNonExistent {
			return nil, nil
		}
		return upd, err
	}
}

// Key is TimeChallengeKeyPrefix followed by the SHA3-256 hash of the
// method name.
func (t *TimeChallengeInfo) Key() []byte {
	return common.JoinBytes(TimeChallengeKeyPrefix, crypto.Hash([]byte(t.MethodName)))
}

// Delete removes the method's challenge slot.
func (t *TimeChallengeInfo) Delete(context db.DB) error {
	return context.Delete(t.Key())
}

// SecurityInfoVariable is the security configuration of a contract
// protected by guardians and time challenges (bridge and liquidity).
// Stored as a single value under SecurityInfoKeyPrefix.
type SecurityInfoVariable struct {
	// Guardians are the addresses that may vote in a new
	// administrator once the contract is in emergency.
	Guardians []types.Address `json:"guardians"`
	// GuardiansVotes holds, per guardian, the administrator address
	// it currently votes for.
	GuardiansVotes []types.Address `json:"guardiansVotes"`
	// AdministratorDelay is the time-challenge delay, in momentums,
	// for administrator-level actions such as changing the
	// administrator or the guardians.
	AdministratorDelay uint64 `json:"administratorDelay"`
	// SoftDelay is the time-challenge delay, in momentums, for all
	// other challenged actions.
	SoftDelay uint64 `json:"softDelay"`
}

// Save stores the configuration under SecurityInfoKeyPrefix,
// returning any pack or put error.
func (s *SecurityInfoVariable) Save(context db.DB) error {
	data, err := ABICommon.PackVariable(
		securityInfoVariableName,
		s.Guardians,
		s.GuardiansVotes,
		s.AdministratorDelay,
		s.SoftDelay,
	)
	if err != nil {
		return err
	}
	return context.Put(
		SecurityInfoKeyPrefix,
		data,
	)
}
func parseSecurityInfoVariable(data []byte) (*SecurityInfoVariable, error) {
	if len(data) > 0 {
		SecurityInfo := new(SecurityInfoVariable)
		if err := ABICommon.UnpackVariable(SecurityInfo, securityInfoVariableName, data); err != nil {
			return nil, err
		}
		return SecurityInfo, nil
	} else {
		return &SecurityInfoVariable{
			Guardians:          make([]types.Address, 0),
			GuardiansVotes:     make([]types.Address, 0),
			AdministratorDelay: constants.MinAdministratorDelay,
			SoftDelay:          constants.MinSoftDelay,
		}, nil
	}
}

// GetSecurityInfoVariable returns the stored security configuration.
// When none is stored it returns defaults instead of an error: no
// guardians, no votes and the minimum delays
// (constants.MinAdministratorDelay, constants.MinSoftDelay).
func GetSecurityInfoVariable(context db.DB) (*SecurityInfoVariable, error) {
	if data, err := context.Get(SecurityInfoKeyPrefix); err != nil {
		return nil, err
	} else {
		upd, err := parseSecurityInfoVariable(data)
		return upd, err
	}
}
