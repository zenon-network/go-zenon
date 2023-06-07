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
	VoteYes uint8 = iota
	VoteNo
	VoteAbstain
	VoteNotValid

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

	RewardDepositVariableName        = "rewardDeposit"
	RewardDepositHistoryVariableName = "rewardDepositHistory"
	LastUpdateVariableName           = "lastUpdate"
	QsrDepositVariableName           = "qsrDeposit"
	LastEpochUpdateVariableName      = "lastEpochUpdate"
	PillarVoteVariableName           = "pillarVote"
	VotableHashVariableName          = "votableHash"
	timeChallengeInfoVariableName    = "timeChallengeInfo"
	securityInfoVariableName         = "securityInfo"

	UpdateMethodName               = "Update"
	CollectRewardMethodName        = "CollectReward"
	DepositQsrMethodName           = "DepositQsr"
	WithdrawQsrMethodName          = "WithdrawQsr"
	DonateMethodName               = "Donate"
	VoteByNameMethodName           = "VoteByName"
	VoteByProdAddressMethodName    = "VoteByProdAddress"
	ChangeAdministratorMethodName  = "ChangeAdministrator"
	EmergencyMethodName            = "Emergency"
	NominateGuardiansMethodName    = "NominateGuardians"
	ProposeAdministratorMethodName = "ProposeAdministrator"
)

var (
	ABICommon = abi.JSONToABIContract(strings.NewReader(jsonCommon))

	// common key prefixes are big enough so they don't clash with embedded-specific variables
	rewardDepositKeyPrefix        = []byte{128}
	lastUpdateKey                 = []byte{129}
	qsrDepositKeyPrefix           = []byte{130}
	lastEpochUpdateKey            = []byte{131}
	rewardDepositHistoryKeyPrefix = []byte{132}
	pillarVoteKeyPrefix           = []byte{133}
	votableHashKeyPrefix          = []byte{134}
	TimeChallengeKeyPrefix        = []byte{135}
	SecurityInfoKeyPrefix         = []byte{136}
)

type RewardDeposit struct {
	Address *types.Address `json:"address"`
	Znn     *big.Int       `json:"znnAmount"`
	Qsr     *big.Int       `json:"qsrAmount"`
}

type RewardDepositMarshal struct {
	Address *types.Address `json:"address"`
	Znn     string         `json:"znnAmount"`
	Qsr     string         `json:"qsrAmount"`
}

func (deposit *RewardDeposit) ToRewardDepositMarshal() *RewardDepositMarshal {
	aux := &RewardDepositMarshal{
		Address: deposit.Address,
		Znn:     deposit.Znn.String(),
		Qsr:     deposit.Qsr.String(),
	}

	return aux
}

func (deposit *RewardDeposit) MarshalJSON() ([]byte, error) {
	return json.Marshal(deposit.ToRewardDepositMarshal())
}

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

type PillarVote struct {
	Id   types.Hash `json:"id"`
	Name string     `json:"name"`
	Vote uint8      `json:"vote"`
}

func (vote *PillarVote) Save(context db.DB) {
	common.DealWithErr(context.Put(vote.Key(), vote.Data()))
}
func (vote *PillarVote) Delete(context db.DB) {
	common.DealWithErr(context.Delete(vote.Key()))
}
func (vote *PillarVote) Key() []byte {
	nameHash := crypto.Hash([]byte(vote.Name))[:20]
	return common.JoinBytes(pillarVoteKeyPrefix, vote.Id.Bytes(), nameHash)
}
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

func GetPillarVote(context db.DB, id types.Hash, name string) (*PillarVote, error) {
	key := (&PillarVote{Id: id, Name: name}).Key()
	if data, err := context.Get(key); err != nil {
		return nil, err
	} else {
		return parsePillarVote(data)
	}
}

type VotableHash struct {
	Id     types.Hash
	Exists bool
}

func (votable *VotableHash) Save(context db.DB) {
	common.DealWithErr(context.Put(votable.Key(), votable.Data()))
}
func (votable *VotableHash) Delete(context db.DB) {
	common.DealWithErr(context.Delete(votable.Key()))
}
func (votable *VotableHash) Key() []byte {
	return common.JoinBytes(votableHashKeyPrefix, votable.Id.Bytes())
}
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

func GetVotableHash(context db.DB, id types.Hash) (*VotableHash, error) {
	key := (&VotableHash{Id: id}).Key()
	if data, err := context.Get(key); err != nil {
		return nil, err
	} else {
		return parseVotableHash(data, key)
	}
}

type VoteBreakdown struct {
	Id    types.Hash `json:"id"`
	Total uint32     `json:"total"`
	Yes   uint32     `json:"yes"`
	No    uint32     `json:"no"`
}

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

type TimeChallengeInfo struct {
	MethodName           string
	ParamsHash           types.Hash
	ChallengeStartHeight uint64
}

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
func (t *TimeChallengeInfo) Key() []byte {
	return common.JoinBytes(TimeChallengeKeyPrefix, crypto.Hash([]byte(t.MethodName)))
}
func (t *TimeChallengeInfo) Delete(context db.DB) error {
	return context.Delete(t.Key())
}

// SecurityInfoVariable This refers to time challenge security
type SecurityInfoVariable struct {
	// addresses that can vote for the new administrator once the bridge is in emergency
	Guardians []types.Address `json:"guardians"`
	// votes of the active guardians
	GuardiansVotes []types.Address `json:"guardiansVotes"`
	// delay upon which the new administrator or guardians will be active
	AdministratorDelay uint64 `json:"administratorDelay"`
	// delay upon which all other time challenges will expire
	SoftDelay uint64 `json:"softDelay"`
}

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
func GetSecurityInfoVariable(context db.DB) (*SecurityInfoVariable, error) {
	if data, err := context.Get(SecurityInfoKeyPrefix); err != nil {
		return nil, err
	} else {
		upd, err := parseSecurityInfoVariable(data)
		return upd, err
	}
}
