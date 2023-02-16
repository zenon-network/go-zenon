package definition

import (
	"encoding/binary"
	"github.com/syndtr/goleveldb/leveldb"
	"github.com/zenon-network/go-zenon/common/crypto"
	"math"
	"math/big"
	"strconv"
	"strings"

	eabi "github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/db"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/vm/abi"
	"github.com/zenon-network/go-zenon/vm/constants"
)

const (
	jsonBridge = `
	[
		{"type":"function","name":"WrapToken", "inputs":[
			{"name":"networkClass","type":"uint32"},
			{"name":"chainId","type":"uint32"},
			{"name":"toAddress","type":"string"}
		]},

		{"type":"function","name":"UpdateWrapRequest", "inputs":[
			{"name":"id","type":"hash"},
			{"name":"signature","type":"string"}
		]},

		{"type":"function","name":"AddNetwork", "inputs":[
			{"name":"networkClass","type":"uint32"},
			{"name":"chainId","type":"uint32"},
			{"name":"name","type":"string"},
			{"name":"contractAddress","type":"string"},
			{"name":"metadata","type":"string"}
		]},

		{"type":"function","name":"RemoveNetwork", "inputs":[
			{"name":"networkClass","type":"uint32"},
			{"name":"chainId","type":"uint32"}
		]},

		{"type":"function","name":"SetTokenPair","inputs":[
			{"name":"networkClass","type":"uint32"},
			{"name":"chainId","type":"uint32"},
			{"name":"tokenStandard","type":"tokenStandard"},
			{"name":"tokenAddress","type":"string"},
			{"name":"bridgeable","type":"bool"},
			{"name":"redeemable","type":"bool"},
			{"name":"owned","type":"bool"},
			{"name":"minAmount","type":"uint256"},
			{"name":"feePercentage","type":"uint32"},
			{"name":"redeemDelay","type":"uint32"},
			{"name":"metadata","type":"string"}
		]},

		{"type":"function","name":"UpdateNetworkMetadata","inputs":[
			{"name":"networkClass","type":"uint32"},
			{"name":"chainId","type":"uint32"},
			{"name":"metadata","type":"string"}
		]},

		{"type":"function","name":"RemoveTokenPair","inputs":[
			{"name":"networkClass","type":"uint32"},
			{"name":"chainId","type":"uint32"},
			{"name":"tokenStandard","type":"tokenStandard"},
			{"name":"tokenAddress","type":"string"}
		]},

		{"type":"function","name":"Halt","inputs":[
			{"name":"signature","type":"string"}
		]},

		{"type":"function","name":"Unhalt","inputs":[]},
		{"type":"function","name":"Emergency","inputs":[]},
		
		{"type":"function","name":"ChangeTssECDSAPubKey","inputs":[
			{"name":"pubKey","type":"string"},
			{"name":"oldPubKeySignature","type":"string"},
			{"name":"newPubKeySignature","type":"string"},
			{"name":"keySignThreshold","type":"uint32"}
		]},

		{"type":"function","name":"ChangeAdministrator","inputs":[
			{"name":"administrator","type":"address"}
		]},
		
		{"type":"function","name":"ProposeAdministrator","inputs":[
			{"name":"address","type":"address"}
		]},

		{"type":"function","name":"AllowKeyGen","inputs":[]},

		{"type":"function","name":"SetRedeemDelay","inputs":[
			{"name":"redeemDelay","type":"uint64"}
		]},

		{"type":"function","name":"SetUnhaltDuration","inputs":[
			{"name":"unhaltDurationInMomentums","type":"uint64"}
		]},

		{"type":"function","name":"UpdateBridgeMetadata","inputs":[
			{"name":"metadata","type":"string"}
		]},

		{"type":"function","name":"UpdateOrchestratorMetadata","inputs":[
			{"name":"metadata","type":"string"}
		]},

		{"type":"function","name":"UnwrapToken","inputs":[
			{"name":"networkClass","type":"uint32"},
			{"name":"chainId","type":"uint32"},
			{"name":"transactionHash","type":"hash"},
			{"name":"logIndex","type":"uint32"},
			{"name":"toAddress","type":"address"},
			{"name":"tokenAddress","type":"string"},
			{"name":"amount","type":"uint256"},
			{"name":"signature","type":"string"}
		]},

		{"type":"function","name":"RevokeUnwrapRequest","inputs":[
			{"name":"transactionHash","type":"hash"},
			{"name":"logIndex","type":"uint32"},
			{"name":"signature","type":"string"}
		]},

		{"type":"function","name":"Redeem","inputs":[
			{"name":"transactionHash","type":"hash"},
			{"name":"logIndex","type":"uint32"}
		]},

		{"type":"function","name":"NominateGuardians","inputs":[
			{"name":"guardians","type":"string[]"}
		]},

		{"type":"function","name":"SetOrchestratorInfo","inputs":[
			{"name":"windowSize","type":"uint64"},
			{"name":"keyGenThreshold","type":"uint32"},
			{"name":"confirmationsToFinality","type":"uint32"},
			{"name":"estimatedMomentumTime","type":"uint32"}
		]},

		{"type":"variable","name":"wrapRequest","inputs":[
			{"name":"networkClass","type":"uint32"},
			{"name":"chainId", "type":"uint32"},
			{"name":"toAddress","type":"string"},
			{"name":"tokenStandard","type":"tokenStandard"},
			{"name":"tokenAddress","type":"string"},
			{"name":"amount","type":"uint256"},
			{"name":"fee","type":"uint256"},
			{"name":"signature","type":"string"},
			{"name":"creationMomentumHeight","type":"uint64"}
		]},

		{"type":"variable","name":"requestPair","inputs":[
			{"name":"creationMomentumHeight","type":"uint64"}
		]},

		{"type":"variable","name":"unwrapRequest","inputs":[
			{"name":"registrationMomentumHeight","type":"uint64"},
			{"name":"networkClass","type":"uint32"},
			{"name":"chainId", "type":"uint32"},
			{"name":"toAddress","type":"address"},
			{"name":"tokenAddress","type":"string"},
			{"name":"amount","type":"uint256"},
			{"name":"signature","type":"string"},
			{"name":"redeemed","type":"uint8"},
			{"name":"revoked","type":"uint8"}
		]},

		{"type":"variable","name":"bridgeInfo","inputs":[
			{"name":"administrator","type":"address"},
			{"name":"compressedTssECDSAPubKey","type":"string"},
			{"name":"decompressedTssECDSAPubKey","type":"string"},
			{"name":"allowKeyGen","type":"bool"},
			{"name":"halted","type":"bool"},
			{"name":"unhaltedAt","type":"uint64"},
			{"name":"unhaltDurationInMomentums","type":"uint64"},
			{"name":"tssNonce","type":"uint64"},
			{"name":"metadata","type":"string"}
		]},

		{"type":"variable","name":"orchestratorInfo","inputs":[
			{"name":"windowSize","type":"uint64"},
			{"name":"keyGenThreshold","type":"uint32"},
			{"name":"confirmationsToFinality","type":"uint32"},
			{"name":"estimatedMomentumTime","type":"uint32"},
			{"name":"allowKeyGenHeight","type":"uint64"},
			{"name":"keySignThreshold","type":"uint32"},
			{"name":"metadata","type":"string"}
		]},

		{"type":"variable","name":"securityInfo","inputs":[
			{"name":"guardians","type":"string[]"},
			{"name":"guardiansVotes","type":"string[]"},
			{"name":"administratorDelay","type":"uint64"},
			{"name":"tssDelay","type":"uint64"}
		]},

		{"type":"variable","name":"networkInfo","inputs":[
			{"name":"networkClass","type":"uint32"},
			{"name":"id","type":"uint32"},
			{"name":"name","type":"string"},
			{"name":"contractAddress","type":"string"},
			{"name":"metadata","type":"string"},
			{"name":"tokenPairs","type":"bytes[]"}
		]},

		{"type":"variable","name":"tokenPair","inputs":[
			{"name":"tokenStandard","type":"string"},
			{"name":"tokenAddress","type":"string"},
			{"name":"bridgeable","type":"bool"},
			{"name":"redeemable","type":"bool"},
			{"name":"owned","type":"bool"},
			{"name":"minAmount","type":"uint256"},
			{"name":"feePercentage","type":"uint32"},
			{"name":"redeemDelay","type":"uint32"},
			{"name":"metadata","type":"string"}
		]},

		{"type":"variable","name":"feeTokenPair","inputs":[
			{"name":"accumulatedFee","type":"uint256"}
		]},

		{"type":"variable","name":"timeChallengeInfo","inputs":[
			{"name":"methodName","type":"string"},
			{"name":"paramsHash","type":"hash"},
			{"name":"challengeStartHeight","type":"uint64"}
		]}
	]`

	WrapTokenMethodName            = "WrapToken"
	UpdateWrapRequestMethodName    = "UpdateWrapRequest"
	UnwrapTokenMethodName          = "UnwrapToken"
	RevokeUnwrapRequestMethodName  = "RevokeUnwrapRequest"
	RedeemUnwrapMethodName         = "Redeem"
	AddNetworkMethodName           = "AddNetwork"
	RemoveNetworkMethodName        = "RemoveNetwork"
	SetTokenPairMethod             = "SetTokenPair"
	RemoveTokenPairMethodName      = "RemoveTokenPair"
	HaltMethodName                 = "Halt"
	EmergencyMethodName            = "Emergency"
	NominateGuardiansMethodName    = "NominateGuardians"
	ProposeAdministratorMethodName = "ProposeAdministrator"
	UnhaltMethodName               = "Unhalt"
	AllowKeygenMethodName          = "AllowKeyGen"
	ChangeTssECDSAPubKeyMethodName = "ChangeTssECDSAPubKey"
	ChangeAdministratorMethodName  = "ChangeAdministrator"
	SetUnhaltDurationMethodName    = "SetUnhaltDuration"
	SetOrchestratorInfoMethodName  = "SetOrchestratorInfo"

	UpdateNetworkMetadataMethodName      = "UpdateNetworkMetadata"
	UpdateBridgeMetadataMethodName       = "UpdateBridgeMetadata"
	UpdateOrchestratorMetadataMethodName = "UpdateOrchestratorMetadata"

	requestPairVariableName       = "requestPair"
	wrapRequestVariableName       = "wrapRequest"
	unwrapRequestVariableName     = "unwrapRequest"
	bridgeInfoVariableName        = "bridgeInfo"
	securityInfoVariableName      = "securityInfo"
	orchestratorInfoVariableName  = "orchestratorInfo"
	networkInfoVariableName       = "networkInfo"
	feeTokenPairVariableName      = "feeTokenPair"
	tokenPairVariableName         = "tokenPair"
	timeChallengeInfoVariableName = "timeChallengeInfo"
)

var (
	ABIBridge = abi.JSONToABIContract(strings.NewReader(jsonBridge))

	wrapTokenRequestKeyPrefix   = []byte{1}
	unwrapTokenRequestKeyPrefix = []byte{2}
	BridgeInfoKeyPrefix         = []byte{3}
	OrchestratorInfoKeyPrefix   = []byte{4}
	SecurityInfoKeyPrefix       = []byte{5}
	NetworkInfoKeyPrefix        = []byte{6}
	RequestPairKeyPrefix        = []byte{7}
	FeeTokenPairKeyPrefix       = []byte{8}
	TimeChallengeKeyPrefix      = []byte{8}

	NoMClass = uint32(1)
	EvmClass = uint32(2)

	Uint256Ty, _ = eabi.NewType("uint256", "uint256", nil)
	AddressTy, _ = eabi.NewType("address", "address", nil)
	StringTy, _  = eabi.NewType("string", "string", nil)
)

type BridgeInfoVariable struct {
	// Administrator address
	Administrator types.Address `json:"administrator"`
	// ECDSA pub key generated by the orchestrator from key gen ceremony
	CompressedTssECDSAPubKey   string `json:"compressedTssECDSAPubKey"`
	DecompressedTssECDSAPubKey string `json:"decompressedTssECDSAPubKey"`
	// This specifies whether the orchestrator should key gen or not
	AllowKeyGen bool `json:"allowKeyGen"`
	// This specifies whether the bridge is halted or not
	Halted bool `json:"halted"`
	// Height at which the administrator called unhalt method, UnhaltDurationInMomentums starts from here
	UnhaltedAt uint64 `json:"unhaltedAt"`
	// After we call the unhalt embedded method, the bridge will still be halted for UnhaltDurationInMomentums momentums
	UnhaltDurationInMomentums uint64 `json:"unhaltDurationInMomentums"`
	// An incremental nonce used for signing messages
	TssNonce uint64 `json:"tssNonce"`
	// Additional metadata
	Metadata string `json:"metadata"`
}

func (b *BridgeInfoVariable) Save(context db.DB) error {
	data, err := ABIBridge.PackVariable(
		bridgeInfoVariableName,
		b.Administrator,
		b.CompressedTssECDSAPubKey,
		b.DecompressedTssECDSAPubKey,
		b.AllowKeyGen,
		b.Halted,
		b.UnhaltedAt,
		b.UnhaltDurationInMomentums,
		b.TssNonce,
		b.Metadata,
	)
	if err != nil {
		return err
	}
	return context.Put(
		BridgeInfoKeyPrefix,
		data,
	)
}
func parseBridgeInfoVariable(data []byte) (*BridgeInfoVariable, error) {
	if len(data) > 0 {
		bridgeInfo := new(BridgeInfoVariable)
		if err := ABIBridge.UnpackVariable(bridgeInfo, bridgeInfoVariableName, data); err != nil {
			return nil, err
		}
		return bridgeInfo, nil
	} else {
		return &BridgeInfoVariable{
			Administrator:              constants.InitialBridgeAdministrator,
			CompressedTssECDSAPubKey:   "",
			DecompressedTssECDSAPubKey: "",
			AllowKeyGen:                false,
			Halted:                     false,
			UnhaltDurationInMomentums:  constants.MinUnhaltDurationInMomentums,
			TssNonce:                   0,
			Metadata:                   "{}",
		}, nil
	}
}
func GetBridgeInfoVariable(context db.DB) (*BridgeInfoVariable, error) {
	if data, err := context.Get(BridgeInfoKeyPrefix); err != nil {
		return nil, err
	} else {
		upd, err := parseBridgeInfoVariable(data)
		return upd, err
	}
}

// SecurityInfoVariable This refers to time challenge security
type SecurityInfoVariable struct {
	// pub keys that can vote for the new administrator pub key once the bridge is in emergency
	Guardians []string `json:"guardians"`
	// votes of the active guardians
	GuardiansVotes []string `json:"guardiansVotes"`
	// delay upon which the new administrator pubKey will be active
	AdministratorDelay uint64 `json:"administratorDelay"`
	// delay upon which the new tss pubKey will be active when the administrator changes it
	TssDelay uint64 `json:"tssDelay"`
}

func (s *SecurityInfoVariable) Save(context db.DB) error {
	data, err := ABIBridge.PackVariable(
		securityInfoVariableName,
		s.Guardians,
		s.GuardiansVotes,
		s.AdministratorDelay,
		s.TssDelay,
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
		if err := ABIBridge.UnpackVariable(SecurityInfo, securityInfoVariableName, data); err != nil {
			return nil, err
		}
		return SecurityInfo, nil
	} else {
		return &SecurityInfoVariable{
			Guardians:          make([]string, 0),
			GuardiansVotes:     make([]string, 0),
			AdministratorDelay: constants.MinAdministratorDelay,
			TssDelay:           constants.MinTssDelay,
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

// NetworkInfoVariable One network will always be znn, so we just need the other one
type NetworkInfoVariable struct {
	NetworkClass    uint32   `json:"networkClass"`
	Id              uint32   `json:"chainId"`
	Name            string   `json:"name"`
	ContractAddress string   `json:"contractAddress"`
	Metadata        string   `json:"metadata"`
	TokenPairs      [][]byte `json:"tokenPairs"`
}

type TokenPair struct {
	TokenStandard string   `json:"tokenStandard"`
	TokenAddress  string   `json:"tokenAddress"`
	Bridgeable    bool     `json:"bridgeable"`
	Redeemable    bool     `json:"redeemable"`
	Owned         bool     `json:"owned"`
	MinAmount     *big.Int `json:"minAmount"`
	FeePercentage uint32   `json:"feePercentage"`
	RedeemDelay   uint32   `json:"redeemDelay"`
	Metadata      string   `json:"metadata"`
}

type NetworkInfo struct {
	NetworkClass    uint32      `json:"networkClass"`
	Id              uint32      `json:"chainId"`
	Name            string      `json:"name"`
	ContractAddress string      `json:"contractAddress"`
	Metadata        string      `json:"metadata"`
	TokenPairs      []TokenPair `json:"tokenPairs"`
}

type ZtsFeesInfo struct {
	TokenStandard  string   `json:"tokenStandard"`
	AccumulatedFee *big.Int `json:"accumulatedFee"`
}

func (zfi *ZtsFeesInfo) Save(context db.DB) error {
	data, err := ABIBridge.PackVariable(feeTokenPairVariableName, zfi.AccumulatedFee)
	if err != nil {
		return err
	}
	key, err := zfi.Key()
	if err != nil {
		return err
	}
	return context.Put(key, data)
}
func (zfi *ZtsFeesInfo) Key() ([]byte, error) {
	zts, err := types.ParseZTS(zfi.TokenStandard)
	if err != nil {
		return nil, err
	}
	return common.JoinBytes(FeeTokenPairKeyPrefix, zts.Bytes()), nil
}
func (zfi *ZtsFeesInfo) Delete(context db.DB) error {
	key, err := zfi.Key()
	if err != nil {
		return err
	}
	return context.Delete(key)
}
func parseZtsFeesInfoVariable(key []byte, data []byte) (*ZtsFeesInfo, error) {
	if len(data) > 0 {
		feeTokenPair := new(ZtsFeesInfo)
		if err := ABIBridge.UnpackVariable(feeTokenPair, feeTokenPairVariableName, data); err != nil {
			return nil, err
		}
		zts := new(types.ZenonTokenStandard)
		if err := zts.SetBytes(key[1:]); err != nil {
			return nil, constants.ErrInvalidTokenOrAmount
		}
		feeTokenPair.TokenStandard = zts.String()
		return feeTokenPair, nil
	} else {
		return nil, constants.ErrDataNonExistent
	}
}
func GetZtsFeesInfoVariable(context db.DB, tokenStandard string) (*ZtsFeesInfo, error) {
	feeTokenPair := &ZtsFeesInfo{
		TokenStandard: tokenStandard,
	}
	key, err := feeTokenPair.Key()
	if err != nil {
		return nil, err
	}
	if data, err := context.Get(key); err != nil {
		return nil, err
	} else {
		fee, err := parseZtsFeesInfoVariable(key, data)
		if err == constants.ErrDataNonExistent {
			return &ZtsFeesInfo{tokenStandard, big.NewInt(0)}, nil
		} else {
			return fee, err
		}
	}
}

func GetNetworkInfoKey(networkClass uint32, chainId uint32) []byte {
	networkIdBytes := make([]byte, 4)
	binary.BigEndian.PutUint32(networkIdBytes, networkClass)

	chainIdBytes := make([]byte, 4)
	binary.BigEndian.PutUint32(chainIdBytes, chainId)
	return common.JoinBytes(NetworkInfoKeyPrefix, networkIdBytes, chainIdBytes)
}

// todo remove type and id from abi variable, and parse then from the key
func (nI *NetworkInfoVariable) Save(context db.DB) error {
	data, err := ABIBridge.PackVariable(
		networkInfoVariableName,
		nI.NetworkClass,
		nI.Id,
		nI.Name,
		nI.ContractAddress,
		nI.Metadata,
		nI.TokenPairs,
	)
	if err != nil {
		return err
	}
	return context.Put(
		nI.Key(),
		data,
	)
}
func (nI *NetworkInfoVariable) Key() []byte {
	networkIdBytes := make([]byte, 4)
	binary.BigEndian.PutUint32(networkIdBytes, nI.NetworkClass)

	chainIdBytes := make([]byte, 4)
	binary.BigEndian.PutUint32(chainIdBytes, nI.Id)

	return common.JoinBytes(NetworkInfoKeyPrefix, networkIdBytes, chainIdBytes)
}
func (nI *NetworkInfoVariable) Delete(context db.DB) error {
	return context.Delete(nI.Key())
}

func parseNetworkInfoVariable(data []byte) (*NetworkInfo, error) {
	if len(data) > 0 {
		networkInfoVariable := new(NetworkInfoVariable)
		if err := ABIBridge.UnpackVariable(networkInfoVariable, networkInfoVariableName, data); err != nil {
			return nil, err
		}
		tokenPairs := make([]TokenPair, 0)
		for _, token := range networkInfoVariable.TokenPairs {
			tokenPair := new(TokenPair)
			if err := ABIBridge.UnpackVariable(tokenPair, tokenPairVariableName, token); err != nil {
				continue
			}
			tokenPairs = append(tokenPairs, *tokenPair)
		}
		networkInfo := &NetworkInfo{
			NetworkClass:    networkInfoVariable.NetworkClass,
			Id:              networkInfoVariable.Id,
			Name:            networkInfoVariable.Name,
			ContractAddress: networkInfoVariable.ContractAddress,
			Metadata:        networkInfoVariable.Metadata,
			TokenPairs:      tokenPairs,
		}

		return networkInfo, nil
	} else {
		return nil, constants.ErrDataNonExistent
	}
}
func EncodeNetworkInfo(networkInfo *NetworkInfo) (*NetworkInfoVariable, error) {
	networkInfoVariable := new(NetworkInfoVariable)
	networkInfoVariable.Id = networkInfo.Id
	networkInfoVariable.NetworkClass = networkInfo.NetworkClass
	networkInfoVariable.Name = networkInfo.Name
	networkInfoVariable.ContractAddress = networkInfo.ContractAddress
	networkInfoVariable.Metadata = networkInfo.Metadata
	tokenPairs := make([][]byte, 0)
	for _, token := range networkInfo.TokenPairs {
		if tokenPair, err := ABIBridge.PackVariable(tokenPairVariableName, token.TokenStandard,
			token.TokenAddress, token.Bridgeable, token.Redeemable, token.Owned, token.MinAmount, token.FeePercentage, token.RedeemDelay, token.Metadata); err != nil {
			return nil, err
		} else {
			tokenPairs = append(tokenPairs, tokenPair)
		}
	}
	networkInfoVariable.TokenPairs = tokenPairs
	return networkInfoVariable, nil
}
func GetNetworkInfoVariable(context db.DB, networkClass uint32, chainId uint32) (*NetworkInfo, error) {
	if data, err := context.Get(GetNetworkInfoKey(networkClass, chainId)); err != nil {
		return nil, err
	} else {
		upd, err := parseNetworkInfoVariable(data)
		if err == constants.ErrDataNonExistent {
			return &NetworkInfo{NetworkClass: 0, Id: 0, Name: "", ContractAddress: "", Metadata: "{}"}, nil
		}
		return upd, err
	}
}
func GetNetworkList(context db.DB) ([]*NetworkInfo, error) {
	iterator := context.NewIterator(NetworkInfoKeyPrefix)
	defer iterator.Release()
	networkList := make([]*NetworkInfo, 0)

	for {
		if !iterator.Next() {
			common.DealWithErr(iterator.Error())
			break
		}
		networkInfo, err := parseNetworkInfoVariable(iterator.Value())
		if err != nil {
			continue
		}
		networkList = append(networkList, networkInfo)
	}

	return networkList, nil
}

func GetTokenPairVariable(context db.DB, networkClass uint32, chainId uint32, zts types.ZenonTokenStandard) (*TokenPair, error) {
	networkInfo, err := GetNetworkInfoVariable(context, networkClass, chainId)
	if err != nil {
		return nil, err
	}
	for _, tokenPair := range networkInfo.TokenPairs {
		if tokenPair.TokenStandard == zts.String() {
			return &tokenPair, nil
		}
	}
	return nil, leveldb.ErrNotFound
}

type RequestPair struct {
	Id                     types.Hash `json:"id"`
	CreationMomentumHeight uint64     `json:"creationMomentumHeight"`
}

func (pair *RequestPair) Save(context db.DB) error {
	data, err := ABIBridge.PackVariable(
		requestPairVariableName,
		pair.CreationMomentumHeight)
	if err != nil {
		return err
	}
	return context.Put(getRequestPairKey(pair.Id), data)
}
func (pair *RequestPair) Key() []byte {
	return getRequestPairKey(pair.Id)
}
func getRequestPairKey(id types.Hash) []byte {
	return common.JoinBytes(RequestPairKeyPrefix, id[:])
}
func parseRequestPair(data, key []byte) (*RequestPair, error) {
	if len(data) > 0 {
		dataVar := new(RequestPair)
		if err := ABIBridge.UnpackVariable(dataVar, requestPairVariableName, data); err != nil {
			return nil, err
		}
		if err := dataVar.Id.SetBytes(key[1:]); err != nil {
			return nil, err
		}
		return dataVar, nil
	} else {
		return nil, constants.ErrDataNonExistent
	}
}
func GetRequestPairById(context db.DB, Id types.Hash) (*RequestPair, error) {
	key := getRequestPairKey(Id)
	if data, err := context.Get(key); err != nil {
		return nil, err
	} else {
		return parseRequestPair(data, key)
	}
}

type WrapTokenRequest struct {
	NetworkClass           uint32                   `json:"networkClass"`
	ChainId                uint32                   `json:"chainId"`
	Id                     types.Hash               `json:"id"`
	ToAddress              string                   `json:"toAddress"`
	TokenStandard          types.ZenonTokenStandard `json:"tokenStandard"`
	TokenAddress           string                   `json:"tokenAddress"`
	Amount                 *big.Int                 `json:"amount"`
	Fee                    *big.Int                 `json:"fee"`
	Signature              string                   `json:"signature"`
	CreationMomentumHeight uint64                   `json:"creationMomentumHeight"`
}

func (wrapRequest *WrapTokenRequest) Save(context db.DB) error {
	data, err := ABIBridge.PackVariable(
		wrapRequestVariableName,
		wrapRequest.NetworkClass,
		wrapRequest.ChainId,
		wrapRequest.ToAddress,
		wrapRequest.TokenStandard,
		wrapRequest.TokenAddress,
		wrapRequest.Amount,
		wrapRequest.Fee,
		wrapRequest.Signature,
		wrapRequest.CreationMomentumHeight)
	if err != nil {
		return err
	}
	pair, err := ABIBridge.PackVariable(requestPairVariableName, wrapRequest.CreationMomentumHeight)
	if err != nil {
		return err
	}
	err = context.Put(getRequestPairKey(wrapRequest.Id), pair)
	if err != nil {
		return err
	}
	return context.Put(getWrapTokenRequestKey(wrapRequest.CreationMomentumHeight, wrapRequest.Id), data)
}
func (wrapRequest *WrapTokenRequest) Key() []byte {
	return getWrapTokenRequestKey(wrapRequest.CreationMomentumHeight, wrapRequest.Id)
}
func getWrapTokenRequestKey(creationMomentumHeight uint64, id types.Hash) []byte {
	return common.JoinBytes(wrapTokenRequestKeyPrefix, []byte(strconv.FormatInt(int64(math.MaxInt64-creationMomentumHeight), 10)), id[:])
}

func parseWrapTokenRequest(data, key []byte) (*WrapTokenRequest, error) {
	if len(data) > 0 {
		dataVar := new(WrapTokenRequest)
		if err := ABIBridge.UnpackVariable(dataVar, wrapRequestVariableName, data); err != nil {
			return nil, err
		}
		if err := dataVar.Id.SetBytes(key[20:]); err != nil {
			return nil, err
		}
		return dataVar, nil
	} else {
		return nil, constants.ErrDataNonExistent
	}
}

func GetWrapTokenRequestById(context db.DB, Id types.Hash) (*WrapTokenRequest, error) {
	pair, err := GetRequestPairById(context, Id)
	if err != nil {
		return nil, err
	}
	key := getWrapTokenRequestKey(pair.CreationMomentumHeight, pair.Id)
	if data, err := context.Get(key); err != nil {
		return nil, err
	} else {
		return parseWrapTokenRequest(data, key)
	}
}

func GetWrapTokenRequests(context db.DB) ([]*WrapTokenRequest, error) {
	iterator := context.NewIterator(wrapTokenRequestKeyPrefix)
	defer iterator.Release()
	list := make([]*WrapTokenRequest, 0)

	for {
		if !iterator.Next() {
			if iterator.Error() != nil {
				return nil, iterator.Error()
			}
			break
		}
		if info, err := parseWrapTokenRequest(iterator.Value(), iterator.Key()); err == nil && info != nil {
			list = append(list, info)
		} else {
			return nil, err
		}
	}

	return list, nil
}

type UnwrapTokenRequest struct {
	RegistrationMomentumHeight uint64        `json:"registrationMomentumHeight"`
	NetworkClass               uint32        `json:"networkClass"`
	ChainId                    uint32        `json:"chainId"`
	TransactionHash            types.Hash    `json:"transactionHash"`
	LogIndex                   uint32        `json:"logIndex"`
	ToAddress                  types.Address `json:"toAddress"`
	TokenAddress               string        `json:"tokenAddress"`
	Amount                     *big.Int      `json:"amount"`
	Signature                  string        `json:"signature"`
	Redeemed                   uint8         `json:"redeemed"`
	Revoked                    uint8         `json:"revoked"`
}

func (unwrapRequest *UnwrapTokenRequest) Save(context db.DB) error {
	data, err := ABIBridge.PackVariable(
		unwrapRequestVariableName,
		unwrapRequest.RegistrationMomentumHeight,
		unwrapRequest.NetworkClass,
		unwrapRequest.ChainId,
		unwrapRequest.ToAddress,
		unwrapRequest.TokenAddress,
		unwrapRequest.Amount,
		unwrapRequest.Signature,
		unwrapRequest.Redeemed,
		unwrapRequest.Revoked)
	if err != nil {
		return err
	}
	return context.Put(unwrapRequest.Key(), data)
}
func (unwrapRequest *UnwrapTokenRequest) Key() []byte {
	return getUnwrapTokenRequestKey(unwrapRequest.TransactionHash, unwrapRequest.LogIndex)
}
func (unwrapRequest *UnwrapTokenRequest) Delete(context db.DB) error {
	return context.Delete(unwrapRequest.Key())
}

func getUnwrapTokenRequestKey(transactionHash types.Hash, logIndex uint32) []byte {
	logIndexBytes := make([]byte, 4)
	binary.BigEndian.PutUint32(logIndexBytes, logIndex)
	return common.JoinBytes(unwrapTokenRequestKeyPrefix, transactionHash[:], logIndexBytes)
}

func parseUnwrapTokenRequest(data, key []byte) (*UnwrapTokenRequest, error) {
	if len(data) > 0 {
		dataVar := new(UnwrapTokenRequest)
		if err := ABIBridge.UnpackVariable(dataVar, unwrapRequestVariableName, data); err != nil {
			return nil, err
		}
		if err := dataVar.TransactionHash.SetBytes(key[1:33]); err != nil {
			return nil, err
		}
		dataVar.LogIndex = binary.BigEndian.Uint32(key[33:37])
		return dataVar, nil
	} else {
		return nil, constants.ErrDataNonExistent
	}
}

func GetUnwrapTokenRequestByTxHashAndLog(context db.DB, txHash types.Hash, logIndex uint32) (*UnwrapTokenRequest, error) {
	key := getUnwrapTokenRequestKey(txHash, logIndex)
	if data, err := context.Get(key); err != nil {
		return nil, err
	} else {
		return parseUnwrapTokenRequest(data, key)
	}
}

func GetUnwrapTokenRequests(context db.DB) ([]*UnwrapTokenRequest, error) {
	iterator := context.NewIterator(unwrapTokenRequestKeyPrefix)
	defer iterator.Release()
	list := make([]*UnwrapTokenRequest, 0)

	for {
		if !iterator.Next() {
			if iterator.Error() != nil {
				return nil, iterator.Error()
			}
			break
		}
		if info, err := parseUnwrapTokenRequest(iterator.Value(), iterator.Key()); err == nil && info != nil {
			list = append(list, info)
		} else {
			return nil, err
		}
	}

	return list, nil
}

type OrchestratorInfoParam struct {
	WindowSize              uint64
	KeyGenThreshold         uint32
	ConfirmationsToFinality uint32
	EstimatedMomentumTime   uint32
}

type OrchestratorInfo struct {
	// Momentums period in which only one signing ceremony (wrap or unwrap) can occur in the orchestrator
	WindowSize uint64 `json:"windowSize"`
	// This variable is used in the orchestrator to wait for at least KeyGenThreshold participants for a key gen ceremony
	KeyGenThreshold uint32 `json:"keyGenThreshold"`
	// Momentums until orchestrator can process wrap requests
	ConfirmationsToFinality uint32 `json:"confirmationsToFinality"`
	// Momentum time
	EstimatedMomentumTime uint32 `json:"estimatedMomentumTime"`
	// This variable is a reference for the orchestrator to check the last 24h of momentums for producing pillars
	AllowKeyGenHeight uint64 `json:"allowKeyGenHeight"`
	// This variable is set when the CompressedTssECDSAPubKey is changed and represent k out of n (greater or equal than KeyGenThreshold) participants from the key gen ceremony
	KeySignThreshold uint32 `json:"keySignThreshold"`
	// Metadata for further variables
	Metadata string `json:"metadata"`
}

func (oI *OrchestratorInfo) Save(context db.DB) error {
	data, err := ABIBridge.PackVariable(
		orchestratorInfoVariableName,
		oI.WindowSize,
		oI.KeyGenThreshold,
		oI.ConfirmationsToFinality,
		oI.EstimatedMomentumTime,
		oI.AllowKeyGenHeight,
		oI.KeySignThreshold,
		oI.Metadata,
	)
	if err != nil {
		return err
	}
	return context.Put(
		oI.Key(),
		data,
	)
}
func parseOrchestratorInfoVariable(data []byte) (*OrchestratorInfo, error) {
	if len(data) > 0 {
		orchestratorInfo := new(OrchestratorInfo)
		if err := ABIBridge.UnpackVariable(orchestratorInfo, orchestratorInfoVariableName, data); err != nil {
			return nil, err
		}
		return orchestratorInfo, nil
	} else {
		return nil, constants.ErrDataNonExistent
	}
}
func GetOrchestratorInfoVariable(context db.DB) (*OrchestratorInfo, error) {
	if data, err := context.Get(OrchestratorInfoKeyPrefix); err != nil {
		return nil, err
	} else {
		upd, err := parseOrchestratorInfoVariable(data)
		if err == constants.ErrDataNonExistent {
			return &OrchestratorInfo{
				WindowSize:              0,
				KeyGenThreshold:         0,
				ConfirmationsToFinality: 0,
				EstimatedMomentumTime:   0,
				AllowKeyGenHeight:       0,
				KeySignThreshold:        0,
				Metadata:                "{}",
			}, nil
		}
		return upd, err
	}
}
func (oI *OrchestratorInfo) Key() []byte {
	return OrchestratorInfoKeyPrefix
}
func (oI *OrchestratorInfo) Delete(context db.DB) error {
	return context.Delete(oI.Key())
}

type TimeChallengeInfo struct {
	MethodName           string
	ParamsHash           types.Hash
	ChallengeStartHeight uint64
}

func (t *TimeChallengeInfo) Save(context db.DB) error {
	data, err := ABIBridge.PackVariable(
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
		if err := ABIBridge.UnpackVariable(timeChallengeInfo, timeChallengeInfoVariableName, data); err != nil {
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

type WrapTokenParam struct {
	NetworkClass uint32
	ChainId      uint32
	ToAddress    string
}

type UpdateWrapRequestParam struct {
	Id        types.Hash
	Signature string
}

type UnwrapTokenParam struct {
	NetworkClass    uint32
	ChainId         uint32
	TransactionHash types.Hash
	LogIndex        uint32
	ToAddress       types.Address
	TokenAddress    string
	Amount          *big.Int
	Signature       string
}

type RevokeUnwrapParam struct {
	TransactionHash types.Hash
	LogIndex        uint32
	Signature       string
}

type RedeemParam struct {
	TransactionHash types.Hash
	LogIndex        uint32
}

type TokenPairParam struct {
	NetworkClass  uint32
	ChainId       uint32
	TokenStandard types.ZenonTokenStandard
	TokenAddress  string
	Bridgeable    bool
	Redeemable    bool
	Owned         bool
	MinAmount     *big.Int
	FeePercentage uint32
	RedeemDelay   uint32
	Metadata      string
}

func (p *TokenPairParam) Hash() []byte {
	bridgeableByte := byte(0)
	if p.Bridgeable {
		bridgeableByte = 1
	}

	redeemableByte := byte(0)
	if p.Redeemable {
		redeemableByte = 1
	}

	ownedByte := byte(0)
	if p.Owned {
		ownedByte = 1
	}

	return crypto.Hash(common.JoinBytes(
		common.Uint32ToBytes(p.NetworkClass),
		common.Uint32ToBytes(p.ChainId)),
		p.TokenStandard.Bytes(),
		[]byte(strings.ToLower(p.TokenAddress)),
		[]byte{bridgeableByte, redeemableByte, ownedByte},
		common.BigIntToBytes(p.MinAmount),
		common.Uint32ToBytes(p.FeePercentage),
		common.Uint32ToBytes(p.RedeemDelay),
		crypto.Hash([]byte(p.Metadata)),
	)
}

type UpdateTokenPairParam struct {
	NetworkClass  uint32
	ChainId       uint32
	TokenStandard types.ZenonTokenStandard
	Owned         bool
	MinAmount     *big.Int
	FeePercentage uint32
	RedeemDelay   uint32
	Metadata      string
}

type NetworkInfoParam struct {
	NetworkClass    uint32
	ChainId         uint32
	Name            string
	ContractAddress string
	Metadata        string
}

// todo remove??
type UpdateNetworkMetadataParam struct {
	NetworkClass uint32
	ChainId      uint32
	Metadata     string
}

type ChangeECDSAPubKeyParam struct {
	PubKey             string
	OldPubKeySignature string
	NewPubKeySignature string
	KeySignThreshold   uint32
}
