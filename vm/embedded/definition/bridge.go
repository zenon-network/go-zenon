package definition

import (
	"encoding/binary"
	"encoding/json"
	"github.com/syndtr/goleveldb/leveldb"
	"github.com/zenon-network/go-zenon/common/crypto"
	"math"
	"math/big"
	"reflect"
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
	// jsonBridge is the ABI JSON of the bridge embedded contract: the
	// wrap/unwrap/redeem token flow, network and token-pair
	// administration, halting, TSS key management and the guardian
	// methods, plus the stored request, bridge-info,
	// orchestrator-info, network and fee variables. Parsed into
	// ABIBridge.
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

		{"type":"function","name":"SetNetwork", "inputs":[
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

		{"type":"function","name":"SetNetworkMetadata","inputs":[
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
			{"name":"newPubKeySignature","type":"string"}
		]},

		{"type":"function","name":"ChangeAdministrator","inputs":[
			{"name":"administrator","type":"address"}
		]},
		
		{"type":"function","name":"ProposeAdministrator","inputs":[
			{"name":"address","type":"address"}
		]},

		{"type":"function","name":"SetAllowKeyGen","inputs":[
			{"name":"allowKeyGen","type":"bool"}
		]},

		{"type":"function","name":"SetRedeemDelay","inputs":[
			{"name":"redeemDelay","type":"uint64"}
		]},

		{"type":"function","name":"SetBridgeMetadata","inputs":[
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
			{"name":"logIndex","type":"uint32"}
		]},

		{"type":"function","name":"Redeem","inputs":[
			{"name":"transactionHash","type":"hash"},
			{"name":"logIndex","type":"uint32"}
		]},

		{"type":"function","name":"NominateGuardians","inputs":[
			{"name":"guardians","type":"address[]"}
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
			{"name":"tokenStandard","type":"tokenStandard"},
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
			{"name":"allowKeyGenHeight","type":"uint64"}
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

		{"type":"variable","name":"feeTokenPair","inputs":[
			{"name":"accumulatedFee","type":"uint256"}
		]}
	]`

	// WrapTokenMethodName names the method that locks the sent ZTS
	// tokens for release on another network; the request id is the
	// hash of the send block and the fee is deducted from the amount.
	WrapTokenMethodName = "WrapToken"
	// UpdateWrapRequestMethodName names the method that attaches the
	// TSS signature to a wrap request; on-chain the gate is the
	// signature check itself, not the caller's identity.
	UpdateWrapRequestMethodName = "UpdateWrapRequest"
	// UnwrapTokenMethodName names the method that registers a
	// TSS-signed incoming transfer from another network, redeemable
	// after the token pair's redeem delay.
	UnwrapTokenMethodName = "UnwrapToken"
	// RevokeUnwrapRequestMethodName names the administrator method
	// that marks an unwrap request revoked so it can never be
	// redeemed.
	RevokeUnwrapRequestMethodName = "RevokeUnwrapRequest"
	// RedeemUnwrapMethodName names the method (Redeem) that pays a
	// registered unwrap request out to its destination address once
	// the token pair's redeem delay has passed.
	RedeemUnwrapMethodName = "Redeem"
	// SetNetworkMethodName names the administrator method that
	// registers a destination network.
	SetNetworkMethodName = "SetNetwork"
	// RemoveNetworkMethodName names the administrator method that
	// deletes a network entry.
	RemoveNetworkMethodName = "RemoveNetwork"
	// SetTokenPairMethod names the administrator method, protected by
	// a soft-delay time challenge over TokenPairParam.Hash, that adds
	// or updates a token pair of a network; unlike its siblings the
	// constant name carries no Name suffix.
	SetTokenPairMethod = "SetTokenPair"
	// RemoveTokenPairMethodName names the administrator method that
	// removes a token pair from a network.
	RemoveTokenPairMethodName = "RemoveTokenPair"
	// HaltMethodName names the method that halts the bridge: the
	// administrator calls it directly, anyone else must present a
	// valid TSS signature over the current nonce.
	HaltMethodName = "Halt"
	// UnhaltMethodName names the administrator method that lifts the
	// halt; the bridge stays inactive for another
	// UnhaltDurationInMomentums momentums after the call.
	UnhaltMethodName = "Unhalt"
	// SetAllowKeygenMethodName names the administrator method that
	// toggles whether the orchestrator may run a key-generation
	// ceremony; allowing it also records AllowKeyGenHeight.
	SetAllowKeygenMethodName = "SetAllowKeyGen"
	// ChangeTssECDSAPubKeyMethodName names the method that installs a
	// new TSS ECDSA public key: non-administrator callers must prove
	// it with signatures from both the old and the new key while key
	// generation is allowed, the administrator instead passes a
	// soft-delay time challenge.
	ChangeTssECDSAPubKeyMethodName = "ChangeTssECDSAPubKey"
	// SetOrchestratorInfoMethodName names the administrator method
	// that configures the orchestrator parameters.
	SetOrchestratorInfoMethodName = "SetOrchestratorInfo"

	// SetNetworkMetadataMethodName names the administrator method
	// that replaces a network's metadata JSON.
	SetNetworkMetadataMethodName = "SetNetworkMetadata"
	// SetBridgeMetadataMethodName names the administrator method that
	// replaces the bridge's own metadata JSON.
	SetBridgeMetadataMethodName = "SetBridgeMetadata"

	requestPairVariableName   = "requestPair"
	wrapRequestVariableName   = "wrapRequest"
	unwrapRequestVariableName = "unwrapRequest"
	bridgeInfoVariableName    = "bridgeInfo"

	orchestratorInfoVariableName = "orchestratorInfo"
	networkInfoVariableName      = "networkInfo"
	feeTokenPairVariableName     = "feeTokenPair"
	tokenPairVariableName        = "tokenPair"
)

var (
	// ABIBridge is the parsed ABI of the bridge embedded contract.
	ABIBridge = abi.JSONToABIContract(strings.NewReader(jsonBridge))

	wrapTokenRequestKeyPrefix   = []byte{1}
	unwrapTokenRequestKeyPrefix = []byte{2}
	// BridgeInfoKeyPrefix is, by itself, the single key under which
	// the BridgeInfoVariable is stored.
	BridgeInfoKeyPrefix = []byte{3}
	// OrchestratorInfoKeyPrefix is, by itself, the single key under
	// which the OrchestratorInfo is stored.
	OrchestratorInfoKeyPrefix = []byte{4}
	// NetworkInfoKeyPrefix prefixes network entries; the full key
	// appends the network class and the chain id, each as 4
	// big-endian bytes.
	NetworkInfoKeyPrefix = []byte{5}
	// RequestPairKeyPrefix prefixes the id-to-creation-height index
	// entries of wrap requests; the full key appends the 32-byte id.
	RequestPairKeyPrefix = []byte{6}
	// FeeTokenPairKeyPrefix prefixes the accumulated-fee entries; the
	// full key appends the 10 token-standard bytes.
	FeeTokenPairKeyPrefix = []byte{7}

	// NoMClass (1) is the network class of Zenon itself; the messages
	// the TSS key signs for NoM-side actions embed it.
	NoMClass = uint32(1)
	// EvmClass (2) is the network class of EVM-compatible chains.
	EvmClass = uint32(2)

	// Uint256Ty is the go-ethereum ABI uint256 type; together with
	// AddressTy and StringTy it encodes the messages whose hashes the
	// TSS key signs.
	Uint256Ty, _ = eabi.NewType("uint256", "uint256", nil)
	// AddressTy is the go-ethereum ABI address type; see Uint256Ty.
	AddressTy, _ = eabi.NewType("address", "address", nil)
	// StringTy is the go-ethereum ABI string type; see Uint256Ty.
	StringTy, _ = eabi.NewType("string", "string", nil)
)

// BridgeInfoVariable is the global state of the bridge contract,
// stored as a single value under BridgeInfoKeyPrefix (3).
type BridgeInfoVariable struct {
	// Administrator is the address allowed to call the
	// administrator-gated bridge methods; Emergency zeroes it and the
	// guardians vote a replacement in through ProposeAdministrator.
	Administrator types.Address `json:"administrator"`
	// CompressedTssECDSAPubKey is the base64 compressed form of the
	// ECDSA public key produced by the orchestrator's key-generation
	// ceremony.
	CompressedTssECDSAPubKey string `json:"compressedTssECDSAPubKey"`
	// DecompressedTssECDSAPubKey is the base64 decompressed form of
	// the same key; signatures are verified against it.
	DecompressedTssECDSAPubKey string `json:"decompressedTssECDSAPubKey"`
	// AllowKeyGen reports whether the orchestrator may run a
	// key-generation ceremony; SetAllowKeyGen toggles it and a
	// completed key change clears it.
	AllowKeyGen bool `json:"allowKeyGen"`
	// Halted reports whether the bridge is halted; while it is true,
	// and during the unhalt window below, all token movements are
	// rejected.
	Halted bool `json:"halted"`
	// UnhaltedAt is the momentum height of the administrator's last
	// Unhalt call; the bridge stays inactive until
	// UnhaltDurationInMomentums momentums have passed since it.
	UnhaltedAt uint64 `json:"unhaltedAt"`
	// UnhaltDurationInMomentums is the length of the post-Unhalt
	// waiting window, at least
	// constants.MinUnhaltDurationInMomentums.
	UnhaltDurationInMomentums uint64 `json:"unhaltDurationInMomentums"`
	// TssNonce is the incremental nonce embedded in the messages the
	// TSS key signs, preventing replays.
	TssNonce uint64 `json:"tssNonce"`
	// Metadata is a free-form JSON string.
	Metadata string `json:"metadata"`
}

// Save stores the bridge state under BridgeInfoKeyPrefix, returning
// any pack or put error.
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

// GetBridgeInfoVariable returns the stored bridge state. When none
// is stored yet it returns defaults instead of an error:
// constants.InitialBridgeAdministrator as administrator, empty TSS
// keys, key generation disallowed, not halted, the minimum unhalt
// duration and metadata "{}".
func GetBridgeInfoVariable(context db.DB) (*BridgeInfoVariable, error) {
	if data, err := context.Get(BridgeInfoKeyPrefix); err != nil {
		return nil, err
	} else {
		upd, err := parseBridgeInfoVariable(data)
		return upd, err
	}
}

// NetworkInfoVariable is the stored form of a network entry, with
// the token pairs kept as individually ABI-packed byte slices;
// NetworkInfo is the unpacked form callers receive. One side of
// every pairing is always the NoM network itself, so a single entry
// describes the other side. Entries are stored under
// NetworkInfoKeyPrefix (5) followed by the network class and the
// chain id, each as 4 big-endian bytes.
type NetworkInfoVariable struct {
	NetworkClass    uint32   `json:"networkClass"`
	Id              uint32   `json:"chainId"`
	Name            string   `json:"name"`
	ContractAddress string   `json:"contractAddress"`
	Metadata        string   `json:"metadata"`
	TokenPairs      [][]byte `json:"tokenPairs"`
}

// TokenPair links a ZTS token with its counterpart token contract on
// a paired network. Bridgeable gates outgoing wraps and Redeemable
// incoming redemptions; Owned marks tokens the bridge mints and
// burns rather than holding a locked balance. MinAmount (smallest
// units) is the smallest wrap accepted, FeePercentage the wrap fee
// in basis points of constants.MaximumFee (10,000 = 100%) and
// RedeemDelay the number of momentums between an unwrap request's
// registration and its earliest redemption.
type TokenPair struct {
	TokenStandard types.ZenonTokenStandard `json:"tokenStandard"`
	TokenAddress  string                   `json:"tokenAddress"`
	Bridgeable    bool                     `json:"bridgeable"`
	Redeemable    bool                     `json:"redeemable"`
	Owned         bool                     `json:"owned"`
	MinAmount     *big.Int                 `json:"minAmount"`
	FeePercentage uint32                   `json:"feePercentage"`
	RedeemDelay   uint32                   `json:"redeemDelay"`
	Metadata      string                   `json:"metadata"`
}

// TokenPairMarshall is the JSON form of TokenPair, with the minimum
// amount rendered as a base-10 string to survive clients that parse
// numbers as 64-bit floats.
type TokenPairMarshall struct {
	TokenStandard types.ZenonTokenStandard `json:"tokenStandard"`
	TokenAddress  string                   `json:"tokenAddress"`
	Bridgeable    bool                     `json:"bridgeable"`
	Redeemable    bool                     `json:"redeemable"`
	Owned         bool                     `json:"owned"`
	MinAmount     string                   `json:"minAmount"`
	FeePercentage uint32                   `json:"feePercentage"`
	RedeemDelay   uint32                   `json:"redeemDelay"`
	Metadata      string                   `json:"metadata"`
}

// ToMarshalJson converts the pair to its JSON form with a
// string-encoded minimum amount.
func (t *TokenPair) ToMarshalJson() *TokenPairMarshall {
	aux := &TokenPairMarshall{
		TokenStandard: t.TokenStandard,
		TokenAddress:  t.TokenAddress,
		Bridgeable:    t.Bridgeable,
		Redeemable:    t.Redeemable,
		Owned:         t.Owned,
		MinAmount:     t.MinAmount.String(),
		FeePercentage: t.FeePercentage,
		RedeemDelay:   t.RedeemDelay,
		Metadata:      t.Metadata,
	}
	return aux
}

// MarshalJSON encodes the pair through TokenPairMarshall.
func (t *TokenPair) MarshalJSON() ([]byte, error) {
	return json.Marshal(t.ToMarshalJson())
}

// UnmarshalJSON decodes the pair from its TokenPairMarshall form,
// parsing the string amount back into a big.Int.
func (t *TokenPair) UnmarshalJSON(data []byte) error {
	aux := new(TokenPairMarshall)
	if err := json.Unmarshal(data, aux); err != nil {
		return err
	}
	t.TokenStandard = aux.TokenStandard
	t.TokenAddress = aux.TokenAddress
	t.Bridgeable = aux.Bridgeable
	t.Redeemable = aux.Redeemable
	t.Owned = aux.Owned
	t.MinAmount = common.StringToBigInt(aux.MinAmount)
	t.FeePercentage = aux.FeePercentage
	t.RedeemDelay = aux.RedeemDelay
	t.Metadata = aux.Metadata

	return nil
}

// NetworkInfo is a network entry with its token pairs unpacked:
// NetworkClass and Id identify the paired network (its chain id),
// ContractAddress is the bridge contract on that network and
// Metadata a free-form JSON string. GetNetworkInfoVariable and
// GetNetworkList return this form.
type NetworkInfo struct {
	NetworkClass    uint32      `json:"networkClass"`
	Id              uint32      `json:"chainId"`
	Name            string      `json:"name"`
	ContractAddress string      `json:"contractAddress"`
	Metadata        string      `json:"metadata"`
	TokenPairs      []TokenPair `json:"tokenPairs"`
}

// ZtsFeesInfo accumulates the wrap fees collected in one token;
// AccumulatedFee is in the token's smallest units. Entries are
// stored under FeeTokenPairKeyPrefix (7) followed by the 10
// token-standard bytes; only the fee is packed, the token standard
// is recovered from the key.
type ZtsFeesInfo struct {
	TokenStandard  types.ZenonTokenStandard `json:"tokenStandard"`
	AccumulatedFee *big.Int                 `json:"accumulatedFee"`
}

// ZtsFeesInfoMarshal is the JSON form of ZtsFeesInfo, with the fee
// rendered as a base-10 string to survive clients that parse numbers
// as 64-bit floats.
type ZtsFeesInfoMarshal struct {
	TokenStandard  types.ZenonTokenStandard `json:"tokenStandard"`
	AccumulatedFee string                   `json:"accumulatedFee"`
}

// ToZtsFeesInfoMarshal converts the entry to its JSON form with a
// string-encoded fee.
func (zfi *ZtsFeesInfo) ToZtsFeesInfoMarshal() *ZtsFeesInfoMarshal {
	aux := &ZtsFeesInfoMarshal{
		TokenStandard:  zfi.TokenStandard,
		AccumulatedFee: zfi.AccumulatedFee.String(),
	}
	return aux
}

// MarshalJSON encodes the entry through ZtsFeesInfoMarshal.
func (zfi *ZtsFeesInfo) MarshalJSON() ([]byte, error) {
	return json.Marshal(zfi.ToZtsFeesInfoMarshal())
}

// UnmarshalJSON decodes the entry from its ZtsFeesInfoMarshal form,
// parsing the string fee back into a big.Int.
func (zfi *ZtsFeesInfo) UnmarshalJSON(data []byte) error {
	aux := new(ZtsFeesInfoMarshal)
	if err := json.Unmarshal(data, aux); err != nil {
		return err
	}
	zfi.TokenStandard = aux.TokenStandard
	zfi.AccumulatedFee = common.StringToBigInt(aux.AccumulatedFee)
	return nil
}

// Save stores the accumulated fee under the token's key, returning
// any pack or put error.
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

// Key returns FeeTokenPairKeyPrefix (7) followed by the 10
// token-standard bytes; the returned error is always nil.
func (zfi *ZtsFeesInfo) Key() ([]byte, error) {
	return common.JoinBytes(FeeTokenPairKeyPrefix, zfi.TokenStandard.Bytes()), nil
}

// Delete removes the token's fee entry.
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
		if err := feeTokenPair.TokenStandard.SetBytes(key[1:]); err != nil {
			return nil, constants.ErrInvalidTokenOrAmount
		}

		return feeTokenPair, nil
	} else {
		return nil, constants.ErrDataNonExistent
	}
}

// GetZtsFeesInfoVariable returns the fees accumulated in
// tokenStandard. A missing entry is not an error: it yields an entry
// with fee zero, so callers never see constants.ErrDataNonExistent.
func GetZtsFeesInfoVariable(context db.DB, tokenStandard types.ZenonTokenStandard) (*ZtsFeesInfo, error) {
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

// GetNetworkInfoKey returns NetworkInfoKeyPrefix (5) followed by the
// network class and the chain id, each as 4 big-endian bytes.
func GetNetworkInfoKey(networkClass uint32, chainId uint32) []byte {
	networkIdBytes := make([]byte, 4)
	binary.BigEndian.PutUint32(networkIdBytes, networkClass)

	chainIdBytes := make([]byte, 4)
	binary.BigEndian.PutUint32(chainIdBytes, chainId)
	return common.JoinBytes(NetworkInfoKeyPrefix, networkIdBytes, chainIdBytes)
}

// Save stores the packed network entry under its class+chain-id key,
// returning any pack or put error.
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

// Key is NetworkInfoKeyPrefix (5) followed by the network class and
// the chain id, each as 4 big-endian bytes.
func (nI *NetworkInfoVariable) Key() []byte {
	networkClassBytes := make([]byte, 4)
	binary.BigEndian.PutUint32(networkClassBytes, nI.NetworkClass)

	chainIdBytes := make([]byte, 4)
	binary.BigEndian.PutUint32(chainIdBytes, nI.Id)

	return common.JoinBytes(NetworkInfoKeyPrefix, networkClassBytes, chainIdBytes)
}

// Delete removes the network entry.
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

// EncodeNetworkInfo converts a NetworkInfo into its stored form,
// ABI-packing each token pair into a byte slice; it returns the
// first pack error encountered.
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

// GetNetworkInfoVariable returns the network registered under
// networkClass and chainId, with its token pairs unpacked; pairs
// that fail to unpack are skipped. A missing network is not an
// error: it yields an all-zero entry with metadata "{}", so callers
// never see constants.ErrDataNonExistent.
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

// GetNetworkList returns every stored network in storage-key order
// (network class, then chain id, big-endian); entries that fail to
// parse are skipped and database errors panic via
// common.DealWithErr, so the returned error is always nil.
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

// GetTokenPairVariable returns the token pair of zts on the given
// network. When the network has no pair for the token — including
// when the network itself does not exist, since that reads as an
// entry without pairs — it returns leveldb.ErrNotFound, not
// constants.ErrDataNonExistent.
func GetTokenPairVariable(context db.DB, networkClass uint32, chainId uint32, zts types.ZenonTokenStandard) (*TokenPair, error) {
	networkInfo, err := GetNetworkInfoVariable(context, networkClass, chainId)
	if err != nil {
		return nil, err
	}
	for _, tokenPair := range networkInfo.TokenPairs {
		if reflect.DeepEqual(tokenPair.TokenStandard.Bytes(), zts.Bytes()) {
			return &tokenPair, nil
		}
	}
	return nil, leveldb.ErrNotFound
}

// RequestPair is the index entry from a wrap request id to its
// creation momentum height. Because wrap request keys embed the
// height (see WrapTokenRequest), this side index lets
// GetWrapTokenRequestById rebuild the full key from the id alone.
// Entries are stored under RequestPairKeyPrefix (6) followed by the
// 32-byte id; only the height is packed, the id is recovered from
// the key.
type RequestPair struct {
	Id                     types.Hash `json:"id"`
	CreationMomentumHeight uint64     `json:"creationMomentumHeight"`
}

// Save stores the creation height under the request's id key,
// returning any pack or put error.
func (pair *RequestPair) Save(context db.DB) error {
	data, err := ABIBridge.PackVariable(
		requestPairVariableName,
		pair.CreationMomentumHeight)
	if err != nil {
		return err
	}
	return context.Put(getRequestPairKey(pair.Id), data)
}

// Key is RequestPairKeyPrefix (6) followed by the 32-byte id.
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

// GetRequestPairById returns the index entry of Id, or
// constants.ErrDataNonExistent if no wrap request with that id
// exists.
func GetRequestPairById(context db.DB, Id types.Hash) (*RequestPair, error) {
	key := getRequestPairKey(Id)
	if data, err := context.Get(key); err != nil {
		return nil, err
	} else {
		return parseRequestPair(data, key)
	}
}

// WrapTokenRequest is one stored wrap (outgoing transfer). Id is the
// hash of the WrapToken send block, ToAddress the destination
// address on the target network, Amount the full sent amount and Fee
// the part the bridge keeps (both smallest units; the fee is
// FeePercentage basis points of the amount). Signature holds the
// base64 TSS signature attached later by UpdateWrapRequest. Entries
// are stored under wrapTokenRequestKeyPrefix (1) followed by the
// ASCII decimal digits of math.MaxInt64 minus the creation height (a
// fixed 19-digit string for any realistic height) and the 32-byte
// id, so iterating in key order visits requests newest first; the
// RequestPair index maps the id back to the height.
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

// Save stores the request under its height-embedding key and writes
// the RequestPair index entry for its id, returning any pack or put
// error; the id is recovered from the key when parsing.
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

// Key is wrapTokenRequestKeyPrefix (1) followed by the ASCII decimal
// digits of math.MaxInt64 minus the creation height and the 32-byte
// id; the complement makes newer requests sort first.
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

// GetWrapTokenRequestById returns the wrap request whose id is Id,
// resolving the creation height through its RequestPair entry, or
// constants.ErrDataNonExistent if no such request exists.
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

// GetWrapTokenRequests returns every stored wrap request, newest
// first (descending creation momentum height, ties by ascending id
// bytes).
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

// WrapTokenRequestMarshal is the JSON form of WrapTokenRequest, with
// the amount and fee rendered as base-10 strings to survive clients
// that parse numbers as 64-bit floats.
type WrapTokenRequestMarshal struct {
	NetworkClass           uint32                   `json:"networkClass"`
	ChainId                uint32                   `json:"chainId"`
	Id                     types.Hash               `json:"id"`
	ToAddress              string                   `json:"toAddress"`
	TokenStandard          types.ZenonTokenStandard `json:"tokenStandard"`
	TokenAddress           string                   `json:"tokenAddress"`
	Amount                 string                   `json:"amount"`
	Fee                    string                   `json:"fee"`
	Signature              string                   `json:"signature"`
	CreationMomentumHeight uint64                   `json:"creationMomentumHeight"`
}

// ToMarshalJson converts the request to its JSON form with
// string-encoded amounts.
func (wrapRequest *WrapTokenRequest) ToMarshalJson() *WrapTokenRequestMarshal {
	aux := &WrapTokenRequestMarshal{
		NetworkClass:           wrapRequest.NetworkClass,
		ChainId:                wrapRequest.ChainId,
		Id:                     wrapRequest.Id,
		ToAddress:              wrapRequest.ToAddress,
		TokenStandard:          wrapRequest.TokenStandard,
		TokenAddress:           wrapRequest.TokenAddress,
		Amount:                 wrapRequest.Amount.String(),
		Fee:                    wrapRequest.Fee.String(),
		Signature:              wrapRequest.Signature,
		CreationMomentumHeight: wrapRequest.CreationMomentumHeight,
	}
	return aux
}

// MarshalJSON encodes the request through WrapTokenRequestMarshal.
func (wrapRequest *WrapTokenRequest) MarshalJSON() ([]byte, error) {
	return json.Marshal(wrapRequest.ToMarshalJson())
}

// UnmarshalJSON decodes the request from its WrapTokenRequestMarshal
// form, parsing the string amounts back into big.Int values.
func (wrapRequest *WrapTokenRequest) UnmarshalJSON(data []byte) error {
	aux := new(WrapTokenRequestMarshal)
	if err := json.Unmarshal(data, aux); err != nil {
		return err
	}

	wrapRequest.NetworkClass = aux.NetworkClass
	wrapRequest.ChainId = aux.ChainId
	wrapRequest.Id = aux.Id
	wrapRequest.ToAddress = aux.ToAddress
	wrapRequest.TokenStandard = aux.TokenStandard
	wrapRequest.TokenAddress = aux.TokenAddress
	wrapRequest.Amount = common.StringToBigInt(aux.Amount)
	wrapRequest.Fee = common.StringToBigInt(aux.Fee)
	wrapRequest.Signature = aux.Signature
	wrapRequest.CreationMomentumHeight = aux.CreationMomentumHeight
	return nil
}

// UnwrapTokenRequest is one registered incoming transfer.
// TransactionHash and LogIndex identify the source-network event,
// ToAddress is the NoM beneficiary and Amount (smallest units) what
// Redeem pays out once the token pair's redeem delay has passed
// since RegistrationMomentumHeight. Redeemed and Revoked are 0/1
// flags. Entries are stored under unwrapTokenRequestKeyPrefix (2)
// followed by the 32-byte transaction hash and the log index as 4
// big-endian bytes; both are recovered from the key when parsing.
type UnwrapTokenRequest struct {
	RegistrationMomentumHeight uint64                   `json:"registrationMomentumHeight"`
	NetworkClass               uint32                   `json:"networkClass"`
	ChainId                    uint32                   `json:"chainId"`
	TransactionHash            types.Hash               `json:"transactionHash"`
	LogIndex                   uint32                   `json:"logIndex"`
	ToAddress                  types.Address            `json:"toAddress"`
	TokenAddress               string                   `json:"tokenAddress"`
	TokenStandard              types.ZenonTokenStandard `json:"tokenStandard"`
	Amount                     *big.Int                 `json:"amount"`
	Signature                  string                   `json:"signature"`
	Redeemed                   uint8                    `json:"redeemed"`
	Revoked                    uint8                    `json:"revoked"`
}

// Save stores the request under its hash+log-index key, packing all
// fields except the two recovered from the key, and returns any pack
// or put error.
func (unwrapRequest *UnwrapTokenRequest) Save(context db.DB) error {
	data, err := ABIBridge.PackVariable(
		unwrapRequestVariableName,
		unwrapRequest.RegistrationMomentumHeight,
		unwrapRequest.NetworkClass,
		unwrapRequest.ChainId,
		unwrapRequest.ToAddress,
		unwrapRequest.TokenAddress,
		unwrapRequest.TokenStandard,
		unwrapRequest.Amount,
		unwrapRequest.Signature,
		unwrapRequest.Redeemed,
		unwrapRequest.Revoked)
	if err != nil {
		return err
	}
	return context.Put(unwrapRequest.Key(), data)
}

// Key is unwrapTokenRequestKeyPrefix (2) followed by the 32-byte
// transaction hash and the log index as 4 big-endian bytes.
func (unwrapRequest *UnwrapTokenRequest) Key() []byte {
	return getUnwrapTokenRequestKey(unwrapRequest.TransactionHash, unwrapRequest.LogIndex)
}

// Delete removes the unwrap request.
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

// GetUnwrapTokenRequestByTxHashAndLog returns the unwrap request
// registered for the source transaction hash and log index, or
// constants.ErrDataNonExistent if none exists.
func GetUnwrapTokenRequestByTxHashAndLog(context db.DB, txHash types.Hash, logIndex uint32) (*UnwrapTokenRequest, error) {
	key := getUnwrapTokenRequestKey(txHash, logIndex)
	if data, err := context.Get(key); err != nil {
		return nil, err
	} else {
		return parseUnwrapTokenRequest(data, key)
	}
}

// GetUnwrapTokenRequests returns every stored unwrap request, in
// storage-key (transaction hash, then log index) order.
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

// UnwrapTokenRequestMarshal is the JSON form of UnwrapTokenRequest,
// with the amount rendered as a base-10 string to survive clients
// that parse numbers as 64-bit floats.
type UnwrapTokenRequestMarshal struct {
	RegistrationMomentumHeight uint64                   `json:"registrationMomentumHeight"`
	NetworkClass               uint32                   `json:"networkClass"`
	ChainId                    uint32                   `json:"chainId"`
	TransactionHash            types.Hash               `json:"transactionHash"`
	LogIndex                   uint32                   `json:"logIndex"`
	ToAddress                  types.Address            `json:"toAddress"`
	TokenAddress               string                   `json:"tokenAddress"`
	TokenStandard              types.ZenonTokenStandard `json:"tokenStandard"`
	Amount                     string                   `json:"amount"`
	Signature                  string                   `json:"signature"`
	Redeemed                   uint8                    `json:"redeemed"`
	Revoked                    uint8                    `json:"revoked"`
}

// ToMarshalJson converts the request to its JSON form with a
// string-encoded amount.
func (unwrapRequest *UnwrapTokenRequest) ToMarshalJson() *UnwrapTokenRequestMarshal {
	aux := &UnwrapTokenRequestMarshal{
		RegistrationMomentumHeight: unwrapRequest.RegistrationMomentumHeight,
		NetworkClass:               unwrapRequest.NetworkClass,
		ChainId:                    unwrapRequest.ChainId,
		TransactionHash:            unwrapRequest.TransactionHash,
		LogIndex:                   unwrapRequest.LogIndex,
		ToAddress:                  unwrapRequest.ToAddress,
		TokenAddress:               unwrapRequest.TokenAddress,
		TokenStandard:              unwrapRequest.TokenStandard,
		Amount:                     unwrapRequest.Amount.String(),
		Signature:                  unwrapRequest.Signature,
		Redeemed:                   unwrapRequest.Redeemed,
		Revoked:                    unwrapRequest.Revoked,
	}
	return aux
}

// MarshalJSON encodes the request through UnwrapTokenRequestMarshal.
func (unwrapRequest *UnwrapTokenRequest) MarshalJSON() ([]byte, error) {
	return json.Marshal(unwrapRequest.ToMarshalJson())
}

// UnmarshalJSON decodes the request from its
// UnwrapTokenRequestMarshal form, parsing the string amount back
// into a big.Int.
func (unwrapRequest *UnwrapTokenRequest) UnmarshalJSON(data []byte) error {
	aux := new(UnwrapTokenRequestMarshal)
	if err := json.Unmarshal(data, aux); err != nil {
		return err
	}

	unwrapRequest.RegistrationMomentumHeight = aux.RegistrationMomentumHeight
	unwrapRequest.NetworkClass = aux.NetworkClass
	unwrapRequest.ChainId = aux.ChainId
	unwrapRequest.TransactionHash = aux.TransactionHash
	unwrapRequest.LogIndex = aux.LogIndex
	unwrapRequest.ToAddress = aux.ToAddress
	unwrapRequest.TokenAddress = aux.TokenAddress
	unwrapRequest.TokenStandard = aux.TokenStandard
	unwrapRequest.Amount = common.StringToBigInt(aux.Amount)
	unwrapRequest.Signature = aux.Signature
	unwrapRequest.Redeemed = aux.Redeemed
	unwrapRequest.Revoked = aux.Revoked
	return nil
}

// OrchestratorInfoParam carries the arguments of
// SetOrchestratorInfo: the OrchestratorInfo fields except
// AllowKeyGenHeight, which SetAllowKeyGen records on its own.
type OrchestratorInfoParam struct {
	WindowSize              uint64
	KeyGenThreshold         uint32
	ConfirmationsToFinality uint32
	EstimatedMomentumTime   uint32
}

// OrchestratorInfo is the configuration the orchestrator network
// reads from the contract. Stored as a single value under
// OrchestratorInfoKeyPrefix (4). The node only stores and serves
// these values — on-chain validation requires merely that the four
// configurable fields are nonzero — so the per-field descriptions
// below state the off-chain orchestrator's intended use of them, not
// behavior this codebase enforces.
type OrchestratorInfo struct {
	// WindowSize is the length, in momentums, of the windows in
	// which the orchestrator performs at most one signing ceremony
	// (wrap or unwrap).
	WindowSize uint64 `json:"windowSize"`
	// KeyGenThreshold is the minimum number of participants the
	// orchestrator waits for before a key-generation ceremony.
	KeyGenThreshold uint32 `json:"keyGenThreshold"`
	// ConfirmationsToFinality is the number of momentums after which
	// the orchestrator treats a wrap request as final and signs it.
	ConfirmationsToFinality uint32 `json:"confirmationsToFinality"`
	// EstimatedMomentumTime is the expected seconds per momentum.
	EstimatedMomentumTime uint32 `json:"estimatedMomentumTime"`
	// AllowKeyGenHeight is the momentum height of the last
	// SetAllowKeyGen call that allowed key generation; the
	// orchestrator inspects the producing pillars of the day before
	// it.
	AllowKeyGenHeight uint64 `json:"allowKeyGenHeight"`
}

// Save stores the configuration under OrchestratorInfoKeyPrefix,
// returning any pack or put error.
func (oI *OrchestratorInfo) Save(context db.DB) error {
	data, err := ABIBridge.PackVariable(
		orchestratorInfoVariableName,
		oI.WindowSize,
		oI.KeyGenThreshold,
		oI.ConfirmationsToFinality,
		oI.EstimatedMomentumTime,
		oI.AllowKeyGenHeight,
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

// GetOrchestratorInfoVariable returns the stored orchestrator
// configuration; when none is stored it returns an all-zero
// configuration instead of an error.
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
			}, nil
		}
		return upd, err
	}
}

// Key is OrchestratorInfoKeyPrefix (4) by itself; the configuration
// is a single value.
func (oI *OrchestratorInfo) Key() []byte {
	return OrchestratorInfoKeyPrefix
}

// Delete removes the stored configuration.
func (oI *OrchestratorInfo) Delete(context db.DB) error {
	return context.Delete(oI.Key())
}

// WrapTokenParam carries the arguments of WrapToken: the destination
// network and the destination address on it; the wrapped token and
// amount come from the send block itself.
type WrapTokenParam struct {
	NetworkClass uint32
	ChainId      uint32
	ToAddress    string
}

// UpdateWrapRequestParam carries the arguments of UpdateWrapRequest:
// the wrap request id and the base64 TSS signature to attach.
type UpdateWrapRequestParam struct {
	Id        types.Hash
	Signature string
}

// UnwrapTokenParam carries the arguments of UnwrapToken: the source
// network, the transaction hash and log index of the source event,
// the NoM beneficiary, the source token address, the amount
// (smallest units) and the TSS signature authorizing the
// registration.
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

// RevokeUnwrapParam carries the arguments of RevokeUnwrapRequest:
// the transaction hash and log index identifying the unwrap request.
type RevokeUnwrapParam struct {
	TransactionHash types.Hash
	LogIndex        uint32
}

// RedeemParam carries the arguments of Redeem: the transaction hash
// and log index identifying the unwrap request to pay out.
type RedeemParam struct {
	TransactionHash types.Hash
	LogIndex        uint32
}

// TokenPairParam carries the arguments of SetTokenPair and
// RemoveTokenPair: the network and the full token-pair
// configuration (see TokenPair).
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

// Hash returns the SHA3-256 digest of a canonical encoding of the
// parameters (token address lowercased, booleans as single bytes,
// metadata hashed first); SetTokenPair uses it as the
// time-challenge parameter hash.
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

// SetTokenPairParam mirrors most of SetTokenPair's arguments; the
// implementation unpacks into TokenPairParam instead and does not
// reference this type.
type SetTokenPairParam struct {
	NetworkClass  uint32
	ChainId       uint32
	TokenStandard types.ZenonTokenStandard
	Owned         bool
	MinAmount     *big.Int
	FeePercentage uint32
	RedeemDelay   uint32
	Metadata      string
}

// NetworkInfoParam carries the arguments of SetNetwork and
// RemoveNetwork: the class, chain id, name, contract address and
// metadata of the network (RemoveNetwork uses only the first two).
type NetworkInfoParam struct {
	NetworkClass    uint32
	ChainId         uint32
	Name            string
	ContractAddress string
	Metadata        string
}

// SetNetworkMetadataParam carries the arguments of
// SetNetworkMetadata: the network identifiers and the replacement
// metadata JSON.
type SetNetworkMetadataParam struct {
	NetworkClass uint32
	ChainId      uint32
	Metadata     string
}

// ChangeECDSAPubKeyParam carries the arguments of
// ChangeTssECDSAPubKey: the new compressed public key and the
// signatures produced with the old and the new key, all base64.
type ChangeECDSAPubKeyParam struct {
	PubKey             string
	OldPubKeySignature string
	NewPubKeySignature string
}
