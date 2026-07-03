package implementation

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"reflect"
	"sort"
	"strings"

	eabi "github.com/ethereum/go-ethereum/accounts/abi"
	ecommon "github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto/secp256k1"
	"github.com/pkg/errors"
	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/crypto"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/vm/constants"
	"github.com/zenon-network/go-zenon/vm/embedded/definition"
	"github.com/zenon-network/go-zenon/vm/vm_context"
)

var (
	bridgeLog = common.EmbeddedLogger.New("contract", "bridge")
)

// CheckECDSASignature verifies a recoverable secp256k1 signature over
// message, which must be a 32-byte digest. The public key and the
// signature are base64 strings: the key must decode to the
// constants.DecompressedECDSAPubKeyLength (65) byte uncompressed form
// and the signature to constants.ECDSASignatureLength (65) bytes —
// r, s and a recovery id. The signer's key is recovered from the
// signature and compared with the given one; a mismatch fails with
// constants.ErrInvalidECDSASignature. Callers must treat any false
// result or error as an invalid signature.
func CheckECDSASignature(message []byte, pubKeyStr, signatureStr string) (bool, error) {
	pubKey, err := base64.StdEncoding.DecodeString(pubKeyStr)
	if err != nil {
		return false, constants.ErrInvalidB64Decode
	}
	if len(pubKey) != constants.DecompressedECDSAPubKeyLength {
		return false, constants.ErrInvalidDecompressedECDSAPubKeyLength
	}

	signature, err := base64.StdEncoding.DecodeString(signatureStr)
	if err != nil {
		return false, constants.ErrInvalidB64Decode
	}
	if len(signature) != constants.ECDSASignatureLength {
		return false, constants.ErrInvalidECDSASignature
	}

	recPubKey, err := secp256k1.RecoverPubkey(message, signature)
	if err != nil {
		return false, err
	}
	if !bytes.Equal(pubKey, recPubKey) {
		return false, constants.ErrInvalidECDSASignature
	}

	return true, nil
}

// CanPerformAction is the combined gate in front of every
// token-moving bridge call (WrapToken, UpdateWrapRequest, UnwrapToken
// and Redeem): the bridge must be initialized
// (CheckBridgeInitialized), security configured
// (CheckSecurityInitialized), the bridge neither halted nor inside
// the post-unhalt window (CheckBridgeHalted) and the orchestrator
// configured (CheckOrchestratorInfoInitialized). It returns the
// bridge and orchestrator state for the caller to reuse.
func CanPerformAction(context vm_context.AccountVmContext) (*definition.BridgeInfoVariable, *definition.OrchestratorInfo, error) {
	if bridgeInfo, errBridge := CheckBridgeInitialized(context); errBridge != nil {
		return nil, nil, errBridge
	} else {
		if _, errSec := CheckSecurityInitialized(context); errSec != nil {
			return nil, nil, errSec
		} else {
			if errHalt := CheckBridgeHalted(bridgeInfo, context); errHalt != nil {
				return nil, nil, errHalt
			} else {
				if orchestratorInfo, errOrc := CheckOrchestratorInfoInitialized(context); errOrc != nil {
					return nil, nil, errOrc
				} else {
					return bridgeInfo, orchestratorInfo, nil
				}
			}
		}
	}
}

// CheckBridgeInitialized returns the bridge state, failing with
// constants.ErrBridgeNotInitialized while no TSS public key has been
// installed yet or the administrator is the zero address (the
// emergency state).
func CheckBridgeInitialized(context vm_context.AccountVmContext) (*definition.BridgeInfoVariable, error) {
	bridgeInfo, err := definition.GetBridgeInfoVariable(context.Storage())
	if err != nil {
		return nil, err
	}
	if len(bridgeInfo.CompressedTssECDSAPubKey) == 0 || bridgeInfo.Administrator.IsZero() {
		return nil, constants.ErrBridgeNotInitialized
	}

	return bridgeInfo, nil
}

// CheckOrchestratorInfoInitialized returns the orchestrator
// configuration, failing with constants.ErrOrchestratorNotInitialized
// while any of its four configurable fields (window size, key-gen
// threshold, confirmations to finality, estimated momentum time) is
// still zero — that is, until SetOrchestratorInfo has been called.
func CheckOrchestratorInfoInitialized(context vm_context.AccountVmContext) (*definition.OrchestratorInfo, error) {
	orchestratorInfo, err := definition.GetOrchestratorInfoVariable(context.Storage())
	if err != nil {
		return nil, err
	}
	if orchestratorInfo.WindowSize == 0 || orchestratorInfo.KeyGenThreshold == 0 || orchestratorInfo.ConfirmationsToFinality == 0 || orchestratorInfo.EstimatedMomentumTime == 0 {
		return nil, constants.ErrOrchestratorNotInitialized
	}

	return orchestratorInfo, nil
}

// CheckBridgeHalted fails with constants.ErrBridgeHalted while the
// bridge is halted and for the whole post-unhalt window: token
// movements resume only once strictly more than
// UnhaltDurationInMomentums momentums have passed since the
// UnhaltedAt height recorded by Unhalt.
func CheckBridgeHalted(bridgeInfo *definition.BridgeInfoVariable, context vm_context.AccountVmContext) error {
	momentum, err := context.GetFrontierMomentum()
	if err != nil {
		return err
	}
	if bridgeInfo.Halted {
		return constants.ErrBridgeHalted
	} else if bridgeInfo.UnhaltedAt+bridgeInfo.UnhaltDurationInMomentums >= momentum.Height {
		return constants.ErrBridgeHalted
	}
	return nil
}

// CheckNetworkAndPairExist returns the token pair of the given
// network whose ZTS string or token-address string equals ztsOrToken:
// wrap callers pass the sent token's ZTS and read the paired token
// address from the result, unwrap callers pass the lowercased token
// address and read the paired ZTS. It fails with
// constants.ErrUnknownNetwork when no network is registered under
// networkClass and chainId and with constants.ErrTokenNotFound when
// the network has no matching pair.
func CheckNetworkAndPairExist(context vm_context.AccountVmContext, networkClass uint32, chainId uint32, ztsOrToken string) (*definition.TokenPair, error) {
	network, err := definition.GetNetworkInfoVariable(context.Storage(), networkClass, chainId)
	if err != nil {
		return nil, err
	} else if len(network.Name) == 0 {
		return nil, constants.ErrUnknownNetwork
	}

	for i := 0; i < len(network.TokenPairs); i++ {
		zts := network.TokenPairs[i].TokenStandard.String()
		token := network.TokenPairs[i].TokenAddress
		if ztsOrToken == zts || ztsOrToken == token {
			return &network.TokenPairs[i], nil
		}
	}
	return nil, constants.ErrTokenNotFound
}

// WrapTokenMethod (WrapToken) starts an outgoing transfer: the sent
// ZTS amount is taken by the bridge and a wrap request is stored for
// the orchestrator to sign (see UpdateWrapRequest), entitling the
// destination address to the amount minus the fee on the destination
// network.
type WrapTokenMethod struct {
	MethodName string
}

// GetPlasma quotes the EmbeddedSimple tier; the descendant burn of
// owned tokens is a contract send, which needs no plasma.
func (p *WrapTokenMethod) GetPlasma(plasmaTable *constants.PlasmaTable) (uint64, error) {
	return plasmaTable.EmbeddedSimple, nil
}

// ValidateSendBlock accepts a packed definition.WrapTokenParam
// (network class, chain id, destination address) carrying a positive
// amount of any token, else constants.ErrInvalidTokenOrAmount. The
// destination must be a 20-byte hex address
// (constants.ErrForbiddenParam otherwise); whether the token may be
// wrapped is only checked in ReceiveBlock.
func (p *WrapTokenMethod) ValidateSendBlock(block *nom.AccountBlock) error {
	var err error
	param := new(definition.WrapTokenParam)

	if err = definition.ABIBridge.UnpackMethod(param, p.MethodName, block.Data); err != nil {
		return constants.ErrUnpackError
	}

	if !ecommon.IsHexAddress(param.ToAddress) {
		return constants.ErrForbiddenParam
	}

	if block.Amount.Sign() <= 0 {
		return constants.ErrInvalidTokenOrAmount
	}

	block.Data, err = definition.ABIBridge.PackMethod(p.MethodName, param.NetworkClass, param.ChainId, param.ToAddress)
	return err
}

// ReceiveBlock stores a definition.WrapTokenRequest whose id is the
// send block's hash. The bridge must be fully active
// (CanPerformAction), the destination network must pair the sent
// token (CheckNetworkAndPairExist) with Bridgeable set, else
// constants.ErrTokenNotBridgeable, and the amount must reach the
// pair's minimum, else constants.ErrInvalidMinAmount.
//   - the destination address is stored lowercased, so request
//     signatures are case-insensitive
//   - the fee is FeePercentage basis points of the amount (out of
//     constants.MaximumFee, 10,000 = 100%), rounded down, and is
//     accumulated per ZTS in the contract's ZtsFeesInfo
//   - for Owned pairs one descendant burns the amount minus the fee
//     through the token contract, leaving only the fee behind; for
//     locked pairs the whole amount stays in the bridge balance and
//     no descendant blocks are emitted
func (p *WrapTokenMethod) ReceiveBlock(context vm_context.AccountVmContext, sendBlock *nom.AccountBlock) ([]*nom.AccountBlock, error) {
	if err := p.ValidateSendBlock(sendBlock); err != nil {
		return nil, err
	}
	param := new(definition.WrapTokenParam)
	err := definition.ABIBridge.UnpackMethod(param, p.MethodName, sendBlock.Data)
	if err != nil {
		return nil, err
	}

	if _, _, err := CanPerformAction(context); err != nil {
		return nil, err
	}

	tokenPair, err := CheckNetworkAndPairExist(context, param.NetworkClass, param.ChainId, sendBlock.TokenStandard.String())
	if err != nil {
		return nil, err
	}
	if !tokenPair.Bridgeable {
		return nil, constants.ErrTokenNotBridgeable
	}

	if sendBlock.Amount.Cmp(tokenPair.MinAmount) == -1 {
		return nil, constants.ErrInvalidMinAmount
	}

	frontierMomentum, err := context.GetFrontierMomentum()
	common.DealWithErr(err)
	request := new(definition.WrapTokenRequest)
	request.NetworkClass = param.NetworkClass
	request.ChainId = param.ChainId
	request.Id = sendBlock.Hash
	request.ToAddress = strings.ToLower(param.ToAddress)
	request.TokenStandard = sendBlock.TokenStandard
	request.TokenAddress = tokenPair.TokenAddress
	request.Amount = new(big.Int).Set(sendBlock.Amount)
	amount := new(big.Int).Set(sendBlock.Amount)
	fee := big.NewInt(int64(tokenPair.FeePercentage))
	amount = amount.Mul(amount, fee)
	request.Fee = amount.Div(amount, big.NewInt(int64(constants.MaximumFee)))
	request.Signature = ""
	request.CreationMomentumHeight = frontierMomentum.Height

	ztsFeesInfo, err := definition.GetZtsFeesInfoVariable(context.Storage(), sendBlock.TokenStandard)
	if err != nil {
		return nil, err
	}

	ztsFeesInfo.AccumulatedFee = ztsFeesInfo.AccumulatedFee.Add(ztsFeesInfo.AccumulatedFee, request.Fee)
	common.DealWithErr(ztsFeesInfo.Save(context.Storage()))
	common.DealWithErr(request.Save(context.Storage()))

	if tokenPair.Owned {
		return []*nom.AccountBlock{
			{
				Address:       types.BridgeContract,
				ToAddress:     types.TokenContract,
				BlockType:     nom.BlockTypeContractSend,
				Amount:        request.Amount.Sub(request.Amount, request.Fee),
				TokenStandard: request.TokenStandard,
				Data:          definition.ABIToken.PackMethodPanic(definition.BurnMethodName),
			},
		}, nil
	}
	return nil, nil
}

// GetMessageToSignEvm wraps a 32-byte digest in the Ethereum
// personal-sign envelope — the "\x19Ethereum Signed Message:\n32"
// prefix — and returns the Keccak256 hash of the result, matching
// what EVM contracts reconstruct with ecrecover; data of any other
// length is an error.
func GetMessageToSignEvm(data []byte) ([]byte, error) {
	if len(data) != 32 {
		return nil, errors.New("data len is not 32")
	}
	msg := fmt.Sprintf("\x19Ethereum Signed Message:\n32%s", data)
	return crypto.Keccak256([]byte(msg)), nil
}

// HashByNetworkClass produces the final digest the TSS key signs,
// in the form the verifying network expects: SHA3-256 for
// definition.NoMClass and the personal-sign envelope over the
// Keccak256 of data (GetMessageToSignEvm) for definition.EvmClass;
// any other class is an error.
func HashByNetworkClass(data []byte, networkClass uint32) ([]byte, error) {
	switch networkClass {
	case definition.NoMClass:
		return crypto.Hash(data), nil
	case definition.EvmClass:
		return GetMessageToSignEvm(crypto.Keccak256(data))
	default:
		return nil, errors.New("network type not supported")
	}
}

// GetWrapTokenRequestMessage builds the digest the TSS key signs for
// a wrap request: the go-ethereum ABI encoding of the network class,
// chain id, the destination network's bridge contract address, the
// request id (its 32 bytes as a uint256), the destination address,
// the token address and the amount minus the fee — what the
// destination network releases — hashed by HashByNetworkClass for
// the request's network class. The
// id makes every message unique and the contract address binds
// signatures to one deployment, so none can be replayed elsewhere.
func GetWrapTokenRequestMessage(request *definition.WrapTokenRequest, contractAddress *ecommon.Address) ([]byte, error) {
	args := eabi.Arguments{{Type: definition.Uint256Ty}, {Type: definition.Uint256Ty}, {Type: definition.AddressTy}, {Type: definition.Uint256Ty}, {Type: definition.AddressTy}, {Type: definition.AddressTy}, {Type: definition.Uint256Ty}}
	values := make([]interface{}, 0)
	amount := new(big.Int).Set(request.Amount)
	values = append(values,
		big.NewInt(0).SetUint64(uint64(request.NetworkClass)), // network type
		big.NewInt(0).SetUint64(uint64(request.ChainId)),      // network chain id
		contractAddress, // contract address so if we ever redeploy, not a single signature can be reused
		big.NewInt(0).SetBytes(request.Id.Bytes()), // id which is unique
		ecommon.HexToAddress(request.ToAddress),    // destination address
		ecommon.HexToAddress(request.TokenAddress), // token address
		amount.Sub(amount, request.Fee),            // token amount minus the fee
	)

	messageBytes, err := args.PackValues(values)
	if err != nil {
		return nil, err
	}

	//bridgeLog.Info("CheckECDSASignature", "message", message)
	return HashByNetworkClass(messageBytes, request.NetworkClass)
}

// UpdateWrapRequestMethod (UpdateWrapRequest) attaches the TSS
// signature to a stored wrap request, completing it: with the
// signature on chain, anyone can submit the transfer to the
// destination network's bridge contract. Any address may call — the
// signature check itself is the gate.
type UpdateWrapRequestMethod struct {
	MethodName string
}

// GetPlasma quotes the EmbeddedSimple tier; the call sends no
// response block.
func (p *UpdateWrapRequestMethod) GetPlasma(plasmaTable *constants.PlasmaTable) (uint64, error) {
	return plasmaTable.EmbeddedSimple, nil
}

// ValidateSendBlock accepts a packed definition.UpdateWrapRequestParam
// (request id, base64 signature) carrying no tokens; a non-zero
// amount fails with constants.ErrTokenInvalidAmount rather than the
// ErrInvalidTokenOrAmount the sibling methods use.
func (p *UpdateWrapRequestMethod) ValidateSendBlock(block *nom.AccountBlock) error {
	var err error
	param := new(definition.UpdateWrapRequestParam)

	if err := definition.ABIBridge.UnpackMethod(param, p.MethodName, block.Data); err != nil {
		return constants.ErrUnpackError
	}

	if block.Amount.Sign() != 0 {
		return constants.ErrTokenInvalidAmount
	}

	block.Data, err = definition.ABIBridge.PackMethod(p.MethodName, param.Id, param.Signature)
	return err
}

// ReceiveBlock verifies and stores the signature. The bridge must be
// fully active (CanPerformAction), the request must exist
// (constants.ErrDataNonExistent), its network must still be
// registered (constants.ErrUnknownNetwork) and must still list the
// request's exact ZTS-to-token-address pair, else
// constants.ErrInvalidToken. The signature is checked against the
// current TSS key over GetWrapTokenRequestMessage rebuilt with the
// network's current contract address
// (constants.ErrInvalidECDSASignature on failure) and then saved,
// silently replacing any signature attached earlier.
func (p *UpdateWrapRequestMethod) ReceiveBlock(context vm_context.AccountVmContext, sendBlock *nom.AccountBlock) ([]*nom.AccountBlock, error) {
	if err := p.ValidateSendBlock(sendBlock); err != nil {
		return nil, err
	}

	param := new(definition.UpdateWrapRequestParam)
	if err := definition.ABIBridge.UnpackMethod(param, p.MethodName, sendBlock.Data); err != nil {
		return nil, err
	}

	bridgeInfo, _, err := CanPerformAction(context)
	if err != nil {
		return nil, err
	}

	request, err := definition.GetWrapTokenRequestById(context.Storage(), param.Id)
	if err != nil {
		return nil, err
	}

	networkInfo, err := definition.GetNetworkInfoVariable(context.Storage(), request.NetworkClass, request.ChainId)
	if err != nil {
		return nil, err
	} else if len(networkInfo.Name) == 0 {
		return nil, constants.ErrUnknownNetwork
	}

	found := false
	for _, pair := range networkInfo.TokenPairs {
		if reflect.DeepEqual(pair.TokenStandard.Bytes(), request.TokenStandard.Bytes()) && pair.TokenAddress == request.TokenAddress {
			found = true
			break
		}
	}
	if !found {
		return nil, constants.ErrInvalidToken
	}

	contractAddress := ecommon.HexToAddress(networkInfo.ContractAddress)
	message, err := GetWrapTokenRequestMessage(request, &contractAddress)
	if err != nil {
		return nil, err
	}
	result, err := CheckECDSASignature(message, bridgeInfo.DecompressedTssECDSAPubKey, param.Signature)
	if err != nil || !result {
		return nil, constants.ErrInvalidECDSASignature
	}

	request.Signature = param.Signature
	common.DealWithErr(request.Save(context.Storage()))

	return nil, nil
}

// GetUnwrapTokenRequestMessage builds the digest the TSS key signs to
// authorize an unwrap: the go-ethereum ABI encoding of the network
// class, chain id, source transaction hash, log index, the NoM
// beneficiary address (its 20 bytes as a uint256), the token address
// and the amount, hashed by HashByNetworkClass for the source
// network's class. There is no nonce; the transaction hash and log
// index make each message unique and UnwrapToken enforces that the
// pair is registered only once.
func GetUnwrapTokenRequestMessage(param *definition.UnwrapTokenParam) ([]byte, error) {
	args := eabi.Arguments{{Type: definition.Uint256Ty}, {Type: definition.Uint256Ty}, {Type: definition.Uint256Ty}, {Type: definition.Uint256Ty}, {Type: definition.Uint256Ty}, {Type: definition.AddressTy}, {Type: definition.Uint256Ty}}
	values := make([]interface{}, 0)
	values = append(values,
		big.NewInt(0).SetUint64(uint64(param.NetworkClass)),   // network type
		big.NewInt(0).SetUint64(uint64(param.ChainId)),        // network chain id
		big.NewInt(0).SetBytes(param.TransactionHash.Bytes()), // unique tx hash
		big.NewInt(int64(param.LogIndex)),                     // unique logIndex for the tx
		big.NewInt(0).SetBytes(param.ToAddress.Bytes()),
		ecommon.HexToAddress(param.TokenAddress),
		param.Amount,
	)

	messageBytes, err := args.PackValues(values)
	if err != nil {
		return nil, err
	}

	//bridgeLog.Info("CheckECDSASignature", "message", message)

	return HashByNetworkClass(messageBytes, param.NetworkClass)
}

// checkUnwrapMetadataStatic holds UnwrapToken's static parameter
// checks: the token address must be a 20-byte hex address — rejected
// with constants.ErrInvalidToAddress, despite the error naming the
// destination field — and the amount must be positive.
func checkUnwrapMetadataStatic(param *definition.UnwrapTokenParam) error {
	if !ecommon.IsHexAddress(param.TokenAddress) {
		return constants.ErrInvalidToAddress
	}

	if param.Amount.Sign() <= 0 {
		return constants.ErrInvalidTokenOrAmount
	}

	return nil
}

// UnwrapTokenMethod (UnwrapToken) registers an incoming transfer
// observed on a source network, to be paid out by Redeem once the
// token pair's redeem delay has passed. Any address may call — the
// TSS signature carried in the parameters is the gate, so in practice
// the orchestrator submits these after observing and signing the
// source event.
type UnwrapTokenMethod struct {
	MethodName string
}

// GetPlasma quotes the EmbeddedSimple tier; the call sends no
// response block.
func (p *UnwrapTokenMethod) GetPlasma(plasmaTable *constants.PlasmaTable) (uint64, error) {
	return plasmaTable.EmbeddedSimple, nil
}

// ValidateSendBlock accepts a packed definition.UnwrapTokenParam
// carrying no tokens; the token address must be a 20-byte hex address
// and the amount positive (checkUnwrapMetadataStatic). The signature
// is only verified in ReceiveBlock.
func (p *UnwrapTokenMethod) ValidateSendBlock(block *nom.AccountBlock) error {
	var err error
	param := new(definition.UnwrapTokenParam)

	if err := definition.ABIBridge.UnpackMethod(param, p.MethodName, block.Data); err != nil {
		return constants.ErrUnpackError
	}

	err = checkUnwrapMetadataStatic(param)
	if err != nil {
		return err
	}

	if block.Amount.Sign() != 0 {
		return constants.ErrInvalidTokenOrAmount
	}

	block.Data, err = definition.ABIBridge.PackMethod(p.MethodName, param.NetworkClass, param.ChainId, param.TransactionHash, param.LogIndex, param.ToAddress, param.TokenAddress, param.Amount, param.Signature)
	return err
}

// ReceiveBlock stores a definition.UnwrapTokenRequest registered at
// the frontier momentum's height, with the redeemed and revoked
// flags clear. The bridge must be fully active (CanPerformAction)
// and:
//   - no request may exist yet for the transaction hash and log
//     index — re-registering fails with
//     constants.ErrInvalidTransactionHash, so each source event is
//     credited at most once
//   - the source network must be registered
//     (constants.ErrUnknownNetwork) and pair the lowercased token
//     address (CheckNetworkAndPairExist) with Redeemable set, else
//     constants.ErrTokenNotRedeemable; the pair supplies the ZTS the
//     beneficiary will receive
//   - the TSS signature must verify over
//     GetUnwrapTokenRequestMessage, else
//     constants.ErrInvalidECDSASignature
func (p *UnwrapTokenMethod) ReceiveBlock(context vm_context.AccountVmContext, sendBlock *nom.AccountBlock) ([]*nom.AccountBlock, error) {
	if err := p.ValidateSendBlock(sendBlock); err != nil {
		return nil, err
	}
	bridgeInfo, _, err := CanPerformAction(context)
	if err != nil {
		return nil, err
	}

	param := new(definition.UnwrapTokenParam)
	err = definition.ABIBridge.UnpackMethod(param, p.MethodName, sendBlock.Data)
	if err != nil {
		return nil, err
	}

	request, err := definition.GetUnwrapTokenRequestByTxHashAndLog(context.Storage(), param.TransactionHash, param.LogIndex)
	if err == nil {
		return nil, constants.ErrInvalidTransactionHash
	} else if err != constants.ErrDataNonExistent {
		common.DealWithErr(err)
	}

	networkInfo, err := definition.GetNetworkInfoVariable(context.Storage(), param.NetworkClass, param.ChainId)
	if err != nil {
		bridgeLog.Error("Unwrap", "error", err)
		return nil, err
	} else if len(networkInfo.Name) == 0 {
		return nil, constants.ErrUnknownNetwork
	}

	tokenPair, err := CheckNetworkAndPairExist(context, param.NetworkClass, param.ChainId, strings.ToLower(param.TokenAddress))
	if err != nil {
		return nil, err
	} else if tokenPair == nil {
		return nil, errors.New("token pair not found")
	}

	if !tokenPair.Redeemable {
		return nil, constants.ErrTokenNotRedeemable
	}

	message, err := GetUnwrapTokenRequestMessage(param)
	if err != nil {
		return nil, err
	}
	result, err := CheckECDSASignature(message, bridgeInfo.DecompressedTssECDSAPubKey, param.Signature)
	if err != nil || !result {
		bridgeLog.Error("Unwrap-ErrInvalidSignature", "error", err, "result", result, "signature", param.Signature)
		return nil, constants.ErrInvalidECDSASignature
	}

	momentum, err := context.GetFrontierMomentum()
	if err != nil {
		return nil, err
	}

	request = &definition.UnwrapTokenRequest{
		RegistrationMomentumHeight: momentum.Height,
		NetworkClass:               param.NetworkClass,
		ChainId:                    param.ChainId,
		TransactionHash:            param.TransactionHash,
		LogIndex:                   param.LogIndex,
		ToAddress:                  param.ToAddress,
		TokenAddress:               strings.ToLower(param.TokenAddress),
		TokenStandard:              tokenPair.TokenStandard,
		Amount:                     param.Amount,
		Signature:                  param.Signature,
		Redeemed:                   0,
		Revoked:                    0,
	}

	common.DealWithErr(request.Save(context.Storage()))
	return nil, nil
}

// SetNetworkMethod (SetNetwork) is the administrator method that
// registers a destination network, taking effect immediately — no
// time challenge.
type SetNetworkMethod struct {
	MethodName string
}

// GetPlasma quotes the EmbeddedSimple tier; the call sends no
// response block.
func (p *SetNetworkMethod) GetPlasma(plasmaTable *constants.PlasmaTable) (uint64, error) {
	return plasmaTable.EmbeddedSimple, nil
}

// ValidateSendBlock accepts a packed definition.NetworkInfoParam
// carrying no tokens. The name must be 3 to 32 bytes
// (constants.ErrInvalidNetworkName), the network class and chain id
// non-zero (constants.ErrForbiddenParam), the contract address a
// 20-byte hex address (constants.ErrInvalidContractAddress) and the
// metadata valid JSON (constants.ErrInvalidJsonContent).
func (p *SetNetworkMethod) ValidateSendBlock(block *nom.AccountBlock) error {
	var err error
	param := new(definition.NetworkInfoParam)

	if err := definition.ABIBridge.UnpackMethod(param, p.MethodName, block.Data); err != nil {
		return constants.ErrUnpackError
	}

	if block.Amount.Sign() != 0 {
		return constants.ErrInvalidTokenOrAmount
	}

	if len(param.Name) < 3 || len(param.Name) > 32 {
		return constants.ErrInvalidNetworkName
	}

	if param.NetworkClass < 1 || param.ChainId < 1 {
		return constants.ErrForbiddenParam
	}

	if !ecommon.IsHexAddress(param.ContractAddress) {
		return constants.ErrInvalidContractAddress
	}

	if !IsJSON(param.Metadata) {
		return constants.ErrInvalidJsonContent
	}

	block.Data, err = definition.ABIBridge.PackMethod(p.MethodName, param.NetworkClass, param.ChainId, param.Name, param.ContractAddress, param.Metadata)
	return err
}

// ReceiveBlock saves the network entry under its class and chain id.
// The sender must be the administrator, else
// constants.ErrPermissionDenied. The token-pair list is always reset
// to empty, so re-setting an existing network discards its configured
// pairs. No descendant blocks are emitted.
func (p *SetNetworkMethod) ReceiveBlock(context vm_context.AccountVmContext, sendBlock *nom.AccountBlock) ([]*nom.AccountBlock, error) {
	if err := p.ValidateSendBlock(sendBlock); err != nil {
		return nil, err
	}

	param := new(definition.NetworkInfoParam)
	err := definition.ABIBridge.UnpackMethod(param, p.MethodName, sendBlock.Data)
	if err != nil {
		return nil, err
	}

	bridgeInfo, err := definition.GetBridgeInfoVariable(context.Storage())
	if err != nil {
		return nil, err
	}

	if sendBlock.Address.String() != bridgeInfo.Administrator.String() {
		return nil, constants.ErrPermissionDenied
	}

	networkInfo, err := definition.GetNetworkInfoVariable(context.Storage(), param.NetworkClass, param.ChainId)
	if err != nil {
		return nil, err
	}

	networkInfo.NetworkClass = param.NetworkClass
	networkInfo.Id = param.ChainId
	networkInfo.Name = param.Name
	networkInfo.ContractAddress = param.ContractAddress
	networkInfo.Metadata = param.Metadata
	networkInfo.TokenPairs = make([]definition.TokenPair, 0)

	networkInfoVariable, err := definition.EncodeNetworkInfo(networkInfo)
	if err != nil {
		return nil, err
	}
	common.DealWithErr(networkInfoVariable.Save(context.Storage()))
	return nil, nil
}

// RemoveNetworkMethod (RemoveNetwork) is the administrator method
// that deletes a network entry together with its token pairs, taking
// effect immediately — no time challenge.
type RemoveNetworkMethod struct {
	MethodName string
}

// GetPlasma quotes the EmbeddedSimple tier; the call sends no
// response block.
func (p *RemoveNetworkMethod) GetPlasma(plasmaTable *constants.PlasmaTable) (uint64, error) {
	return plasmaTable.EmbeddedSimple, nil
}

// ValidateSendBlock accepts a packed definition.NetworkInfoParam
// carrying no tokens; only the network class and chain id are
// repacked, the other fields being ignored.
func (p *RemoveNetworkMethod) ValidateSendBlock(block *nom.AccountBlock) error {
	var err error
	param := new(definition.NetworkInfoParam)

	if err := definition.ABIBridge.UnpackMethod(param, p.MethodName, block.Data); err != nil {
		return constants.ErrUnpackError
	}

	if block.Amount.Sign() != 0 {
		return constants.ErrInvalidTokenOrAmount
	}

	block.Data, err = definition.ABIBridge.PackMethod(p.MethodName, param.NetworkClass, param.ChainId)
	return err
}

// ReceiveBlock deletes the network entry. The sender must be the
// administrator (constants.ErrPermissionDenied) and the network must
// exist (constants.ErrUnknownNetwork). No descendant blocks are
// emitted.
func (p *RemoveNetworkMethod) ReceiveBlock(context vm_context.AccountVmContext, sendBlock *nom.AccountBlock) ([]*nom.AccountBlock, error) {
	if err := p.ValidateSendBlock(sendBlock); err != nil {
		return nil, err
	}

	param := new(definition.NetworkInfoParam)
	err := definition.ABIBridge.UnpackMethod(param, p.MethodName, sendBlock.Data)
	if err != nil {
		return nil, err
	}

	bridgeInfo, err := definition.GetBridgeInfoVariable(context.Storage())
	if err != nil {
		return nil, err
	}

	if sendBlock.Address.String() != bridgeInfo.Administrator.String() {
		return nil, constants.ErrPermissionDenied
	}

	networkInfo, err := definition.GetNetworkInfoVariable(context.Storage(), param.NetworkClass, param.ChainId)
	if err != nil {
		return nil, err
	} else if len(networkInfo.Name) == 0 {
		return nil, constants.ErrUnknownNetwork
	}

	networkInfoVariable, err := definition.EncodeNetworkInfo(networkInfo)
	if err != nil {
		return nil, err
	}
	common.DealWithErr(networkInfoVariable.Delete(context.Storage()))
	return nil, nil
}

// SetNetworkMetadataMethod (SetNetworkMetadata) is the administrator
// method that replaces a network's metadata JSON, taking effect
// immediately — no time challenge.
type SetNetworkMetadataMethod struct {
	MethodName string
}

// GetPlasma quotes the EmbeddedSimple tier; the call sends no
// response block.
func (p *SetNetworkMetadataMethod) GetPlasma(plasmaTable *constants.PlasmaTable) (uint64, error) {
	return plasmaTable.EmbeddedSimple, nil
}

// ValidateSendBlock accepts a packed
// definition.SetNetworkMetadataParam carrying no tokens; the metadata
// must be valid JSON, else constants.ErrInvalidJsonContent.
func (p *SetNetworkMetadataMethod) ValidateSendBlock(block *nom.AccountBlock) error {
	var err error

	param := new(definition.SetNetworkMetadataParam)
	if err := definition.ABIBridge.UnpackMethod(param, p.MethodName, block.Data); err != nil {
		return constants.ErrUnpackError
	}

	if block.Amount.Sign() != 0 {
		return constants.ErrInvalidTokenOrAmount
	}

	if !IsJSON(param.Metadata) {
		return constants.ErrInvalidJsonContent
	}

	block.Data, err = definition.ABIBridge.PackMethod(p.MethodName, param.NetworkClass, param.ChainId, param.Metadata)
	return err
}

// ReceiveBlock saves the new metadata on the network entry. The
// sender must be the administrator (constants.ErrPermissionDenied)
// and the network must exist (constants.ErrUnknownNetwork). No
// descendant blocks are emitted.
func (p *SetNetworkMetadataMethod) ReceiveBlock(context vm_context.AccountVmContext, sendBlock *nom.AccountBlock) ([]*nom.AccountBlock, error) {
	if err := p.ValidateSendBlock(sendBlock); err != nil {
		return nil, err
	}

	param := new(definition.SetNetworkMetadataParam)
	err := definition.ABIBridge.UnpackMethod(param, p.MethodName, sendBlock.Data)
	if err != nil {
		return nil, err
	}

	bridgeInfo, err := definition.GetBridgeInfoVariable(context.Storage())
	if err != nil {
		return nil, err
	}

	if sendBlock.Address.String() != bridgeInfo.Administrator.String() {
		return nil, constants.ErrPermissionDenied
	}

	networkInfo, err := definition.GetNetworkInfoVariable(context.Storage(), param.NetworkClass, param.ChainId)
	if err != nil {
		return nil, err
	} else if len(networkInfo.Name) == 0 {
		return nil, constants.ErrUnknownNetwork
	}

	networkInfo.Metadata = param.Metadata
	networkInfoVariable, err := definition.EncodeNetworkInfo(networkInfo)
	if err != nil {
		return nil, err
	}
	common.DealWithErr(networkInfoVariable.Save(context.Storage()))
	return nil, nil
}

// IsJSON reports whether s parses as a single JSON value; bare
// scalars qualify as well as objects and arrays. The bridge metadata
// setters use it to keep stored metadata machine-readable.
func IsJSON(s string) bool {
	var js interface{}
	return json.Unmarshal([]byte(s), &js) == nil
}

// SetTokenPairMethod (SetTokenPair) is the administrator method that
// adds or updates a token pair on a registered network, protected by
// a soft-delay time challenge; removal through RemoveTokenPair is
// immediate by contrast.
type SetTokenPairMethod struct {
	MethodName string
}

// GetPlasma quotes the EmbeddedSimple tier; the call sends no
// response block.
func (p *SetTokenPairMethod) GetPlasma(plasmaTable *constants.PlasmaTable) (uint64, error) {
	return plasmaTable.EmbeddedSimple, nil
}

// ValidateSendBlock accepts a packed definition.TokenPairParam
// carrying no tokens. ZNN and QSR may not be marked Owned, the token
// address must be a 20-byte hex address and the ZTS non-zero (all
// constants.ErrForbiddenParam); the fee may not exceed
// constants.MaximumFee basis points, the redeem delay must be
// non-zero and the metadata must be valid JSON
// (constants.ErrInvalidJsonContent).
func (p *SetTokenPairMethod) ValidateSendBlock(block *nom.AccountBlock) error {
	var err error
	param := new(definition.TokenPairParam)

	if err := definition.ABIBridge.UnpackMethod(param, p.MethodName, block.Data); err != nil {
		return constants.ErrUnpackError
	}

	if block.Amount.Sign() != 0 {
		return constants.ErrInvalidTokenOrAmount
	}

	if (param.TokenStandard.String() == types.ZnnTokenStandard.String() || param.TokenStandard.String() == types.QsrTokenStandard.String()) &&
		param.Owned {
		return constants.ErrForbiddenParam
	}

	if !ecommon.IsHexAddress(param.TokenAddress) {
		return constants.ErrForbiddenParam
	}

	if param.TokenStandard.String() == types.ZeroTokenStandard.String() {
		return constants.ErrForbiddenParam
	}

	if param.FeePercentage > constants.MaximumFee {
		return constants.ErrForbiddenParam
	}

	if param.RedeemDelay == 0 {
		return constants.ErrForbiddenParam
	}

	if !IsJSON(param.Metadata) {
		return constants.ErrInvalidJsonContent
	}

	block.Data, err = definition.ABIBridge.PackMethod(p.MethodName, param.NetworkClass, param.ChainId, param.TokenStandard, param.TokenAddress, param.Bridgeable, param.Redeemable, param.Owned, param.MinAmount, param.FeePercentage, param.RedeemDelay, param.Metadata)
	return err
}

// ReceiveBlock writes the pair into the network's token-pair list.
// The sender must be the administrator
// (constants.ErrPermissionDenied) and the network must exist
// (constants.ErrUnknownNetwork). The change passes a TimeChallenge
// over definition.TokenPairParam.Hash with the security info's
// SoftDelay: the first call only records the challenge and the pair
// is saved when the call is repeated with identical parameters after
// the delay. The pair replaces the single existing entry matching
// its ZTS or its token address — when it would match two distinct
// entries the call fails with constants.ErrForbiddenParam, so pairs
// can never merge — and is appended when none matches; the token
// address is stored lowercased.
func (p *SetTokenPairMethod) ReceiveBlock(context vm_context.AccountVmContext, sendBlock *nom.AccountBlock) ([]*nom.AccountBlock, error) {
	if err := p.ValidateSendBlock(sendBlock); err != nil {
		return nil, err
	}

	param := new(definition.TokenPairParam)
	err := definition.ABIBridge.UnpackMethod(param, p.MethodName, sendBlock.Data)
	if err != nil {
		return nil, err
	}

	bridgeInfo, err := definition.GetBridgeInfoVariable(context.Storage())
	if err != nil {
		return nil, err
	}

	if sendBlock.Address.String() != bridgeInfo.Administrator.String() {
		return nil, constants.ErrPermissionDenied
	}

	networkInfo, err := definition.GetNetworkInfoVariable(context.Storage(), param.NetworkClass, param.ChainId)
	if err != nil {
		return nil, err
	} else if len(networkInfo.Name) == 0 {
		return nil, constants.ErrUnknownNetwork
	}

	securityInfo, err := definition.GetSecurityInfoVariable(context.Storage())
	if err != nil {
		return nil, err
	}

	if timeChallengeInfo, errTimeChallenge := TimeChallenge(context, p.MethodName, param.Hash(), securityInfo.SoftDelay); errTimeChallenge != nil {
		return nil, errTimeChallenge
	} else {
		// if paramsHash is not zero it means we had a new challenge and we can't go further to save the change into local db
		if !timeChallengeInfo.ParamsHash.IsZero() {
			return nil, nil
		}
	}

	tokenPair := definition.TokenPair{
		TokenStandard: param.TokenStandard,
		TokenAddress:  strings.ToLower(param.TokenAddress),
		Bridgeable:    param.Bridgeable,
		Redeemable:    param.Redeemable,
		Owned:         param.Owned,
		MinAmount:     param.MinAmount,
		FeePercentage: param.FeePercentage,
		RedeemDelay:   param.RedeemDelay,
		Metadata:      param.Metadata,
	}

	found := false
	for i := 0; i < len(networkInfo.TokenPairs); i++ {
		if networkInfo.TokenPairs[i].TokenStandard == tokenPair.TokenStandard || networkInfo.TokenPairs[i].TokenAddress == tokenPair.TokenAddress {
			// we do not allow duplicate zts or tokenAddress
			if found {
				return nil, constants.ErrForbiddenParam
			}
			networkInfo.TokenPairs[i] = tokenPair
			found = true
		}
	}
	if !found {
		networkInfo.TokenPairs = append(networkInfo.TokenPairs, tokenPair)
	}

	networkInfoVariable, err := definition.EncodeNetworkInfo(networkInfo)
	if err != nil {
		return nil, err
	}
	common.DealWithErr(networkInfoVariable.Save(context.Storage()))
	return nil, nil
}

// RemoveTokenPairMethod (RemoveTokenPair) is the administrator
// method that removes a token pair from a network, taking effect
// immediately — unlike SetTokenPair there is no time challenge.
type RemoveTokenPairMethod struct {
	MethodName string
}

// GetPlasma quotes the EmbeddedSimple tier; the call sends no
// response block.
func (p *RemoveTokenPairMethod) GetPlasma(plasmaTable *constants.PlasmaTable) (uint64, error) {
	return plasmaTable.EmbeddedSimple, nil
}

// ValidateSendBlock accepts a packed definition.TokenPairParam
// carrying no tokens; the token address must be a 20-byte hex
// address (constants.ErrForbiddenParam) and only the network class,
// chain id, ZTS and token address are repacked.
func (p *RemoveTokenPairMethod) ValidateSendBlock(block *nom.AccountBlock) error {
	var err error
	param := new(definition.TokenPairParam)

	if err := definition.ABIBridge.UnpackMethod(param, p.MethodName, block.Data); err != nil {
		return constants.ErrUnpackError
	}

	if block.Amount.Sign() != 0 {
		return constants.ErrInvalidTokenOrAmount
	}

	if !ecommon.IsHexAddress(param.TokenAddress) {
		return constants.ErrForbiddenParam
	}

	block.Data, err = definition.ABIBridge.PackMethod(p.MethodName, param.NetworkClass, param.ChainId, param.TokenStandard, param.TokenAddress)
	return err
}

// ReceiveBlock deletes the pair matching both the ZTS and the exact
// token-address string — pairs are stored with lowercased addresses,
// so the argument must be lowercase to match — failing with
// constants.ErrTokenNotFound otherwise. The sender must be the
// administrator (constants.ErrPermissionDenied) and the network must
// exist (constants.ErrUnknownNetwork). No descendant blocks are
// emitted.
func (p *RemoveTokenPairMethod) ReceiveBlock(context vm_context.AccountVmContext, sendBlock *nom.AccountBlock) ([]*nom.AccountBlock, error) {
	if err := p.ValidateSendBlock(sendBlock); err != nil {
		return nil, err
	}

	param := new(definition.TokenPairParam)
	err := definition.ABIBridge.UnpackMethod(param, p.MethodName, sendBlock.Data)
	if err != nil {
		return nil, err
	}

	bridgeInfo, err := definition.GetBridgeInfoVariable(context.Storage())
	if err != nil {
		return nil, err
	}

	if sendBlock.Address.String() != bridgeInfo.Administrator.String() {
		return nil, constants.ErrPermissionDenied
	}

	networkInfo, err := definition.GetNetworkInfoVariable(context.Storage(), param.NetworkClass, param.ChainId)
	if err != nil {
		return nil, err
	} else if len(networkInfo.Name) == 0 {
		return nil, constants.ErrUnknownNetwork
	}

	found := false
	for i := 0; i < len(networkInfo.TokenPairs); i++ {
		zts := networkInfo.TokenPairs[i].TokenStandard
		token := networkInfo.TokenPairs[i].TokenAddress
		if reflect.DeepEqual(param.TokenStandard.Bytes(), zts.Bytes()) && param.TokenAddress == token {
			networkInfo.TokenPairs = append(networkInfo.TokenPairs[:i], networkInfo.TokenPairs[i+1:]...)
			found = true
			break
		}
	}
	if !found {
		return nil, constants.ErrTokenNotFound
	}

	networkInfoVariable, err := definition.EncodeNetworkInfo(networkInfo)
	if err != nil {
		return nil, err
	}
	common.DealWithErr(networkInfoVariable.Save(context.Storage()))
	return nil, nil
}

// GetBasicMethodMessage builds the digest the TSS key signs for
// simple administrative actions — only Halt uses it: the go-ethereum
// ABI encoding of the method name, network class, chain id and the
// bridge's current TSS nonce, hashed by HashByNetworkClass. Embedding
// the nonce, which the contract increments when the signature is
// consumed, makes each signature single-use.
func GetBasicMethodMessage(methodName string, tssNonce uint64, networkClass uint32, chainId uint64) ([]byte, error) {
	args := eabi.Arguments{{Type: definition.StringTy}, {Type: definition.Uint256Ty}, {Type: definition.Uint256Ty}, {Type: definition.Uint256Ty}}
	values := make([]interface{}, 0)
	values = append(values,
		methodName,
		big.NewInt(0).SetUint64(uint64(networkClass)),
		big.NewInt(0).SetUint64(chainId),
		big.NewInt(0).SetUint64(tssNonce), // nonce
	)

	messageBytes, err := args.PackValues(values)
	if err != nil {
		return nil, err
	}
	//bridgeLog.Info("CheckECDSASignature", "message", message)

	return HashByNetworkClass(messageBytes, networkClass)
}

// HaltMethod (Halt) stops all token movements at once. The
// administrator halts by calling directly; any other sender must
// present a TSS signature over GetBasicMethodMessage, which lets the
// orchestrator halt the bridge without the administrator's keys.
type HaltMethod struct {
	MethodName string
}

// GetPlasma quotes the EmbeddedSimple tier; the call sends no
// response block.
func (p *HaltMethod) GetPlasma(plasmaTable *constants.PlasmaTable) (uint64, error) {
	return plasmaTable.EmbeddedSimple, nil
}

// ValidateSendBlock accepts a packed base64 signature string —
// ignored when the administrator calls — carried by no tokens; a
// non-zero amount fails with constants.ErrInvalidTokenOrAmount.
func (p *HaltMethod) ValidateSendBlock(block *nom.AccountBlock) error {
	var err error

	signature := new(string)
	if err := definition.ABIBridge.UnpackMethod(signature, p.MethodName, block.Data); err != nil {
		return constants.ErrUnpackError
	}

	if block.Amount.Sign() != 0 {
		return constants.ErrInvalidTokenOrAmount
	}

	block.Data, err = definition.ABIBridge.PackMethod(p.MethodName, *signature)
	return err
}

// ReceiveBlock sets the Halted flag. Halting twice fails with
// constants.ErrBridgeHalted; no other readiness gate applies. When
// the sender is not the administrator, the signature must verify
// against the TSS key over GetBasicMethodMessage — encoding, in
// order, the method name, definition.NoMClass, the momentum chain
// identifier and the current TssNonce
// (constants.ErrInvalidECDSASignature otherwise) — and the nonce is
// incremented so the signature cannot be replayed. No descendant
// blocks are emitted.
func (p *HaltMethod) ReceiveBlock(context vm_context.AccountVmContext, sendBlock *nom.AccountBlock) ([]*nom.AccountBlock, error) {
	if err := p.ValidateSendBlock(sendBlock); err != nil {
		return nil, err
	}

	signature := new(string)
	err := definition.ABIBridge.UnpackMethod(signature, p.MethodName, sendBlock.Data)
	if err != nil {
		return nil, err
	}

	bridgeInfo, errBridge := definition.GetBridgeInfoVariable(context.Storage())
	if errBridge != nil {
		return nil, errBridge
	}
	if bridgeInfo.Halted {
		return nil, constants.ErrBridgeHalted
	}

	if sendBlock.Address.String() != bridgeInfo.Administrator.String() {
		momentum, err := context.GetFrontierMomentum()
		common.DealWithErr(err)

		message, err := GetBasicMethodMessage(p.MethodName, bridgeInfo.TssNonce, definition.NoMClass, momentum.ChainIdentifier)
		if err != nil {
			return nil, err
		}
		result, err := CheckECDSASignature(message, bridgeInfo.DecompressedTssECDSAPubKey, *signature)
		if err != nil || !result {
			bridgeLog.Error("Halt-ErrInvalidSignature", "error", err, "result", result)
			return nil, constants.ErrInvalidECDSASignature
		}
		bridgeInfo.TssNonce += 1
	}

	bridgeInfo.Halted = true
	common.DealWithErr(bridgeInfo.Save(context.Storage()))
	return nil, nil
}

// UnhaltMethod (Unhalt) is the administrator method that lifts a
// halt; token movements stay blocked for a further
// UnhaltDurationInMomentums momentums after the call (see
// CheckBridgeHalted), giving observers time to react before the
// bridge goes live again.
type UnhaltMethod struct {
	MethodName string
}

// GetPlasma quotes the EmbeddedSimple tier; the call sends no
// response block.
func (p *UnhaltMethod) GetPlasma(plasmaTable *constants.PlasmaTable) (uint64, error) {
	return plasmaTable.EmbeddedSimple, nil
}

// ValidateSendBlock accepts an argument-less call carrying no
// tokens: extra ABI arguments fail with constants.ErrUnpackError and
// a non-zero Amount with constants.ErrInvalidTokenOrAmount.
func (p *UnhaltMethod) ValidateSendBlock(block *nom.AccountBlock) error {
	var err error
	if err := definition.ABIBridge.UnpackEmptyMethod(p.MethodName, block.Data); err != nil {
		return constants.ErrUnpackError
	}

	if block.Amount.Sign() != 0 {
		return constants.ErrInvalidTokenOrAmount
	}

	block.Data, err = definition.ABIBridge.PackMethod(p.MethodName)
	return err
}

// ReceiveBlock clears Halted and records the frontier height as
// UnhaltedAt, starting the unhalt window. The bridge must currently
// be halted — calling again fails with constants.ErrBridgeNotHalted,
// which keeps repeated calls from pushing UnhaltedAt, and with it the
// inactive window, ever forward — and the sender must be the
// administrator, else constants.ErrPermissionDenied. No descendant
// blocks are emitted.
func (p *UnhaltMethod) ReceiveBlock(context vm_context.AccountVmContext, sendBlock *nom.AccountBlock) ([]*nom.AccountBlock, error) {
	if err := p.ValidateSendBlock(sendBlock); err != nil {
		return nil, err
	}

	err := definition.ABIBridge.UnpackEmptyMethod(p.MethodName, sendBlock.Data)
	if err != nil {
		return nil, err
	}
	bridgeInfo, err := definition.GetBridgeInfoVariable(context.Storage())
	if err != nil {
		return nil, err
	}

	// we do this check, so we cannot unhalt more than one time and actually increase the duration of the halt
	if !bridgeInfo.Halted {
		return nil, constants.ErrBridgeNotHalted
	}

	if sendBlock.Address.String() != bridgeInfo.Administrator.String() {
		return nil, constants.ErrPermissionDenied
	}

	momentum, err := context.GetFrontierMomentum()
	common.DealWithErr(err)

	bridgeInfo.UnhaltedAt = momentum.Height
	bridgeInfo.Halted = false
	common.DealWithErr(bridgeInfo.Save(context.Storage()))
	return nil, nil
}

// EmergencyMethod (Emergency) is the administrator's kill switch: it
// renounces the administrator role, erases both stored TSS public
// keys and halts the bridge in a single, immediate call. Control can
// only be restored by the guardians electing a new administrator
// through ProposeAdministrator, who must then re-initialize the TSS
// key; the bridge counterpart of EmergencyLiquidity.
type EmergencyMethod struct {
	MethodName string
}

// GetPlasma quotes the EmbeddedSimple tier; the call sends no
// response block.
func (p *EmergencyMethod) GetPlasma(plasmaTable *constants.PlasmaTable) (uint64, error) {
	return plasmaTable.EmbeddedSimple, nil
}

// ValidateSendBlock accepts an argument-less call carrying no
// tokens: extra ABI arguments fail with constants.ErrUnpackError and
// a non-zero Amount with constants.ErrInvalidTokenOrAmount.
func (p *EmergencyMethod) ValidateSendBlock(block *nom.AccountBlock) error {
	var err error
	if err := definition.ABIBridge.UnpackEmptyMethod(p.MethodName, block.Data); err != nil {
		return constants.ErrUnpackError
	}

	if block.Amount.Sign() != 0 {
		return constants.ErrInvalidTokenOrAmount
	}

	block.Data, err = definition.ABIBridge.PackMethod(p.MethodName)
	return err
}

// ReceiveBlock zeroes the administrator, clears the compressed and
// decompressed TSS keys and sets Halted. Security must be
// initialized (CheckSecurityInitialized) and the sender must be the
// administrator, else constants.ErrPermissionDenied. There is no
// time challenge — the emergency takes effect at once. No descendant
// blocks are emitted.
func (p *EmergencyMethod) ReceiveBlock(context vm_context.AccountVmContext, sendBlock *nom.AccountBlock) ([]*nom.AccountBlock, error) {
	if err := p.ValidateSendBlock(sendBlock); err != nil {
		return nil, err
	}

	err := definition.ABIBridge.UnpackEmptyMethod(p.MethodName, sendBlock.Data)
	if err != nil {
		return nil, err
	}

	bridgeInfo, err := definition.GetBridgeInfoVariable(context.Storage())
	if err != nil {
		return nil, err
	}

	if _, err := CheckSecurityInitialized(context); err != nil {
		return nil, err
	}

	if sendBlock.Address.String() != bridgeInfo.Administrator.String() {
		return nil, constants.ErrPermissionDenied
	}

	if errSet := bridgeInfo.Administrator.SetBytes(types.ZeroAddress.Bytes()); errSet != nil {
		return nil, errSet
	}
	bridgeInfo.CompressedTssECDSAPubKey = ""
	bridgeInfo.DecompressedTssECDSAPubKey = ""
	bridgeInfo.Halted = true
	common.DealWithErr(bridgeInfo.Save(context.Storage()))
	return nil, nil
}

// GetChangePubKeyMessage builds the digest both the old and the new
// TSS key must sign for a non-administrator key change: the
// go-ethereum ABI encoding of the method name, network class, chain
// id, the current TSS nonce and the key material — for
// ChangeTssECDSAPubKey the 32 bytes of the compressed key after its
// parity byte; the ChangeAdministrator branch, expecting a 32-byte
// key, has no caller. Unlike the other message builders the digest
// is always SHA3-256, ignoring the network class argument.
func GetChangePubKeyMessage(methodName string, networkClass uint32, chainId, tssNonce uint64, pubKey string) ([]byte, error) {
	args := eabi.Arguments{{Type: definition.StringTy}, {Type: definition.Uint256Ty}, {Type: definition.Uint256Ty}, {Type: definition.Uint256Ty}, {Type: definition.Uint256Ty}}
	values := make([]interface{}, 0)
	values = append(values,
		methodName,
		big.NewInt(0).SetUint64(uint64(networkClass)),
		big.NewInt(0).SetUint64(chainId),
		big.NewInt(0).SetUint64(tssNonce), // nonce
	)

	pubKeyBytes, err := base64.StdEncoding.DecodeString(pubKey)
	if err != nil {
		return nil, err
	}
	if methodName == definition.ChangeTssECDSAPubKeyMethodName {
		// pubkey will always have 33 bytes as it comes compressed, we checked
		values = append(values, big.NewInt(0).SetBytes(pubKeyBytes[1:]))
	} else if methodName == definition.ChangeAdministratorMethodName {
		// pubkey will have 32 bytes
		values = append(values, big.NewInt(0).SetBytes(pubKeyBytes))
	}

	messageBytes, err := args.PackValues(values)
	if err != nil {
		return nil, err
	}

	//bridgeLog.Info("CheckECDSASignature", "message", message)

	return crypto.Hash(messageBytes), nil
}

// ChangeTssECDSAPubKeyMethod (ChangeTssECDSAPubKey) installs a new
// TSS ECDSA public key through one of two paths: the orchestrator
// proves possession of both the old and the new key after a
// key-generation ceremony, or the administrator forces the key
// through a soft-delay time challenge.
type ChangeTssECDSAPubKeyMethod struct {
	MethodName string
}

// GetPlasma quotes the EmbeddedSimple tier; the call sends no
// response block.
func (p *ChangeTssECDSAPubKeyMethod) GetPlasma(plasmaTable *constants.PlasmaTable) (uint64, error) {
	return plasmaTable.EmbeddedSimple, nil
}

// ValidateSendBlock accepts a packed definition.ChangeECDSAPubKeyParam
// carrying no tokens. The new key must base64-decode
// (constants.ErrInvalidB64Decode) to exactly
// constants.CompressedECDSAPubKeyLength (33) bytes, else
// constants.ErrInvalidCompressedECDSAPubKeyLength; the two
// signatures are only verified in ReceiveBlock, and only for
// non-administrator senders.
func (p *ChangeTssECDSAPubKeyMethod) ValidateSendBlock(block *nom.AccountBlock) error {
	var err error
	param := new(definition.ChangeECDSAPubKeyParam)
	if err = definition.ABIBridge.UnpackMethod(param, p.MethodName, block.Data); err != nil {
		return constants.ErrUnpackError
	}
	pubKey, err := base64.StdEncoding.DecodeString(param.PubKey)
	if err != nil {
		return constants.ErrInvalidB64Decode
	}
	if len(pubKey) != constants.CompressedECDSAPubKeyLength {
		return constants.ErrInvalidCompressedECDSAPubKeyLength
	}

	if block.Amount.Sign() != 0 {
		return constants.ErrInvalidTokenOrAmount
	}

	block.Data, err = definition.ABIBridge.PackMethod(p.MethodName, param.PubKey, param.OldPubKeySignature, param.NewPubKeySignature)
	return err
}

// ReceiveBlock stores the new key in both its compressed and
// decompressed forms and clears AllowKeyGen. Security and
// orchestrator info must be initialized; the halt state is not
// checked, so the key can rotate while the bridge is halted.
//   - non-administrator senders need AllowKeyGen set
//     (constants.ErrNotAllowedToChangeTss otherwise) and must carry
//     signatures over GetChangePubKeyMessage by both the current TSS
//     key and the new key (constants.ErrInvalidECDSASignature);
//     TssNonce increments on success
//   - the administrator instead passes a TimeChallenge over the hash
//     of the decompressed key with the security info's SoftDelay —
//     not the AdministratorDelay — the first call recording the
//     challenge and the repeat after the delay applying the change
func (p *ChangeTssECDSAPubKeyMethod) ReceiveBlock(context vm_context.AccountVmContext, sendBlock *nom.AccountBlock) ([]*nom.AccountBlock, error) {
	if err := p.ValidateSendBlock(sendBlock); err != nil {
		return nil, err
	}

	param := new(definition.ChangeECDSAPubKeyParam)
	err := definition.ABIBridge.UnpackMethod(param, p.MethodName, sendBlock.Data)
	if err != nil {
		return nil, err
	}

	if _, err := CheckSecurityInitialized(context); err != nil {
		return nil, err
	}
	if _, err := CheckOrchestratorInfoInitialized(context); err != nil {
		return nil, err
	}

	bridgeInfo, err := definition.GetBridgeInfoVariable(context.Storage())
	if err != nil {
		return nil, err
	}

	// we check it in the send block
	pubKey, _ := base64.StdEncoding.DecodeString(param.PubKey)

	X, Y := secp256k1.DecompressPubkey(pubKey)
	dPubKeyBytes := make([]byte, 1)
	dPubKeyBytes[0] = 4
	dPubKeyBytes = append(dPubKeyBytes, X.Bytes()...)
	dPubKeyBytes = append(dPubKeyBytes, Y.Bytes()...)
	newDecompressedPubKey := base64.StdEncoding.EncodeToString(dPubKeyBytes)

	if sendBlock.Address.String() != bridgeInfo.Administrator.String() {
		// this only applies to non administrator calls
		if !bridgeInfo.AllowKeyGen {
			return nil, constants.ErrNotAllowedToChangeTss
		}
		momentum, err := context.GetFrontierMomentum()
		common.DealWithErr(err)

		message, err := GetChangePubKeyMessage(p.MethodName, definition.NoMClass, momentum.ChainIdentifier, bridgeInfo.TssNonce, param.PubKey)
		if err != nil {
			return nil, err
		}
		result, err := CheckECDSASignature(message, bridgeInfo.DecompressedTssECDSAPubKey, param.OldPubKeySignature)
		if err != nil || !result {
			bridgeLog.Error("ChangeTssECDSAPubKey-ErrInvalidOldKeySignature", "error", err, "result", result)
			return nil, constants.ErrInvalidECDSASignature
		}

		result, err = CheckECDSASignature(message, newDecompressedPubKey, param.NewPubKeySignature)
		if err != nil || !result {
			bridgeLog.Error("ChangeTssECDSAPubKey-ErrInvalidNewKeySignature", "error", err, "result", result)
			return nil, constants.ErrInvalidECDSASignature
		}

		bridgeInfo.TssNonce += 1
	} else {
		securityInfo, err := definition.GetSecurityInfoVariable(context.Storage())
		if err != nil {
			return nil, err
		}
		paramsHash := crypto.Hash(dPubKeyBytes)
		if timeChallengeInfo, errTimeChallenge := TimeChallenge(context, p.MethodName, paramsHash, securityInfo.SoftDelay); errTimeChallenge != nil {
			return nil, errTimeChallenge
		} else {
			// if paramsHash is not zero it means we had a new challenge and we can't go further to save the change into local db
			if !timeChallengeInfo.ParamsHash.IsZero() {
				return nil, nil
			}
		}
	}

	bridgeInfo.CompressedTssECDSAPubKey = param.PubKey
	bridgeInfo.DecompressedTssECDSAPubKey = newDecompressedPubKey
	bridgeInfo.AllowKeyGen = false
	common.DealWithErr(bridgeInfo.Save(context.Storage()))
	return nil, nil
}

// ChangeAdministratorMethod (ChangeAdministrator) is the
// administrator method that hands the role to another address,
// protected by an administrator-delay time challenge; the bridge
// counterpart of ChangeAdministratorLiquidity.
type ChangeAdministratorMethod struct {
	MethodName string
}

// GetPlasma quotes the EmbeddedSimple tier; the call sends no
// response block.
func (p *ChangeAdministratorMethod) GetPlasma(plasmaTable *constants.PlasmaTable) (uint64, error) {
	return plasmaTable.EmbeddedSimple, nil
}

// ValidateSendBlock accepts a packed types.Address carrying no
// tokens. The address is re-parsed to verify its checksum, which the
// ABI alone does not, and must not be the zero address, else
// constants.ErrForbiddenParam.
func (p *ChangeAdministratorMethod) ValidateSendBlock(block *nom.AccountBlock) error {
	var err error
	address := new(types.Address)
	if err = definition.ABIBridge.UnpackMethod(address, p.MethodName, block.Data); err != nil {
		return constants.ErrUnpackError
	}

	if block.Amount.Sign() != 0 {
		return constants.ErrInvalidTokenOrAmount
	}

	// we also check with this method because in the abi the checksum is not verified
	parsedAddress, err := types.ParseAddress(address.String())
	if err != nil {
		return err
	} else if parsedAddress.IsZero() {
		return constants.ErrForbiddenParam
	}

	block.Data, err = definition.ABIBridge.PackMethod(p.MethodName, address)
	return err
}

// ReceiveBlock replaces the administrator in the bridge state.
// Security must be initialized (CheckSecurityInitialized) and the
// sender must be the current administrator, else
// constants.ErrPermissionDenied. The change passes a TimeChallenge
// over the new address with the security info's AdministratorDelay:
// the first call only records the challenge and the handover happens
// when the call is repeated with the same address after the delay.
func (p *ChangeAdministratorMethod) ReceiveBlock(context vm_context.AccountVmContext, sendBlock *nom.AccountBlock) ([]*nom.AccountBlock, error) {
	if err := p.ValidateSendBlock(sendBlock); err != nil {
		return nil, err
	}

	address := new(types.Address)
	err := definition.ABIBridge.UnpackMethod(address, p.MethodName, sendBlock.Data)
	if err != nil {
		return nil, err
	}

	bridgeInfo, err := definition.GetBridgeInfoVariable(context.Storage())
	if err != nil {
		return nil, err
	}

	if sendBlock.Address.String() != bridgeInfo.Administrator.String() {
		return nil, constants.ErrPermissionDenied
	}

	securityInfo, err := CheckSecurityInitialized(context)
	if err != nil {
		return nil, err
	}

	paramsHash := crypto.Hash(address.Bytes())
	if timeChallengeInfo, errTimeChallenge := TimeChallenge(context, p.MethodName, paramsHash, securityInfo.AdministratorDelay); errTimeChallenge != nil {
		return nil, errTimeChallenge
	} else {
		// if paramsHash is not zero it means we had a new challenge and we can't go further to save the change into local db
		if !timeChallengeInfo.ParamsHash.IsZero() {
			return nil, nil
		}
	}

	if errSet := bridgeInfo.Administrator.SetBytes(address.Bytes()); errSet != nil {
		return nil, err
	}
	common.DealWithErr(bridgeInfo.Save(context.Storage()))
	return nil, nil
}

// SetAllowKeygenMethod (SetAllowKeyGen) is the administrator method
// that toggles whether the orchestrator may run a key-generation
// ceremony — and so whether non-administrator ChangeTssECDSAPubKey
// calls are accepted. A completed key change clears the flag again.
type SetAllowKeygenMethod struct {
	MethodName string
}

// GetPlasma quotes the EmbeddedSimple tier; the call sends no
// response block.
func (p *SetAllowKeygenMethod) GetPlasma(plasmaTable *constants.PlasmaTable) (uint64, error) {
	return plasmaTable.EmbeddedSimple, nil
}

// ValidateSendBlock accepts a packed bool — the new AllowKeyGen
// state — carried by no tokens; a non-zero amount fails with
// constants.ErrInvalidTokenOrAmount.
func (p *SetAllowKeygenMethod) ValidateSendBlock(block *nom.AccountBlock) error {
	var err error

	var param bool
	if err := definition.ABIBridge.UnpackMethod(&param, p.MethodName, block.Data); err != nil {
		return constants.ErrUnpackError
	}

	if block.Amount.Sign() != 0 {
		return constants.ErrInvalidTokenOrAmount
	}

	block.Data, err = definition.ABIBridge.PackMethod(p.MethodName, param)
	return err
}

// ReceiveBlock saves the new AllowKeyGen flag. Security must be
// initialized, the bridge must not be halted (CheckBridgeHalted) and
// the orchestrator must be configured — but unlike CanPerformAction
// the bridge itself need not be initialized, so the very first key
// generation can be allowed before any TSS key exists. The sender
// must be the administrator, else constants.ErrPermissionDenied.
// When allowing, the frontier height is recorded as the orchestrator
// info's AllowKeyGenHeight. No descendant blocks are emitted.
func (p *SetAllowKeygenMethod) ReceiveBlock(context vm_context.AccountVmContext, sendBlock *nom.AccountBlock) ([]*nom.AccountBlock, error) {
	if err := p.ValidateSendBlock(sendBlock); err != nil {
		return nil, err
	}

	var param bool
	if err := definition.ABIBridge.UnpackMethod(&param, p.MethodName, sendBlock.Data); err != nil {
		return nil, constants.ErrUnpackError
	}

	bridgeInfo, errBridge := definition.GetBridgeInfoVariable(context.Storage())
	if errBridge != nil {
		return nil, errBridge
	}
	if _, errSec := CheckSecurityInitialized(context); errSec != nil {
		return nil, errSec
	} else {
		if errHalt := CheckBridgeHalted(bridgeInfo, context); errHalt != nil {
			return nil, errHalt
		}
	}
	orchestratorInfo, errOrc := CheckOrchestratorInfoInitialized(context)
	if errOrc != nil {
		return nil, errOrc
	}

	if sendBlock.Address.String() != bridgeInfo.Administrator.String() {
		return nil, constants.ErrPermissionDenied
	}

	bridgeInfo.AllowKeyGen = param
	common.DealWithErr(bridgeInfo.Save(context.Storage()))

	if param {
		momentum, err := context.GetFrontierMomentum()
		if err != nil {
			return nil, err
		}
		orchestratorInfo.AllowKeyGenHeight = momentum.Height
		common.DealWithErr(orchestratorInfo.Save(context.Storage()))
	}

	return nil, nil
}

// SetOrchestratorInfoMethod (SetOrchestratorInfo) is the
// administrator method that configures the four orchestrator
// parameters (see definition.OrchestratorInfo); until it is called
// the orchestrator-initialized gate keeps the token-moving methods
// disabled.
type SetOrchestratorInfoMethod struct {
	MethodName string
}

// GetPlasma quotes the EmbeddedSimple tier; the call sends no
// response block.
func (p *SetOrchestratorInfoMethod) GetPlasma(plasmaTable *constants.PlasmaTable) (uint64, error) {
	return plasmaTable.EmbeddedSimple, nil
}

// ValidateSendBlock accepts a packed definition.OrchestratorInfoParam
// carrying no tokens; all four fields must be non-zero, else
// constants.ErrForbiddenParam.
func (p *SetOrchestratorInfoMethod) ValidateSendBlock(block *nom.AccountBlock) error {
	var err error

	param := new(definition.OrchestratorInfoParam)
	if err := definition.ABIBridge.UnpackMethod(param, p.MethodName, block.Data); err != nil {
		return constants.ErrUnpackError
	}

	if param.KeyGenThreshold == 0 || param.ConfirmationsToFinality == 0 || param.WindowSize == 0 || param.EstimatedMomentumTime == 0 {
		return constants.ErrForbiddenParam
	}

	if block.Amount.Sign() != 0 {
		return constants.ErrInvalidTokenOrAmount
	}

	block.Data, err = definition.ABIBridge.PackMethod(p.MethodName, param.WindowSize, param.KeyGenThreshold, param.ConfirmationsToFinality, param.EstimatedMomentumTime)
	return err
}

// ReceiveBlock saves the four configurable fields, preserving
// AllowKeyGenHeight. The administrator check is the only gate — the
// bridge and security state need not be initialized, so the
// configuration can be bootstrapped — and other senders fail with
// constants.ErrPermissionDenied. No descendant blocks are emitted.
func (p *SetOrchestratorInfoMethod) ReceiveBlock(context vm_context.AccountVmContext, sendBlock *nom.AccountBlock) ([]*nom.AccountBlock, error) {
	if err := p.ValidateSendBlock(sendBlock); err != nil {
		return nil, err
	}

	param := new(definition.OrchestratorInfoParam)
	err := definition.ABIBridge.UnpackMethod(param, p.MethodName, sendBlock.Data)
	if err != nil {
		return nil, err
	}

	// the only condition is that bridge is not nil, which means the administrator pub key was set
	bridgeInfo, err := definition.GetBridgeInfoVariable(context.Storage())
	if err != nil {
		return nil, err
	}

	if sendBlock.Address.String() != bridgeInfo.Administrator.String() {
		return nil, constants.ErrPermissionDenied
	}

	orchestratorInfo, err := definition.GetOrchestratorInfoVariable(context.Storage())
	if err != nil {
		return nil, err
	}

	orchestratorInfo.WindowSize = param.WindowSize
	orchestratorInfo.KeyGenThreshold = param.KeyGenThreshold
	orchestratorInfo.ConfirmationsToFinality = param.ConfirmationsToFinality
	orchestratorInfo.EstimatedMomentumTime = param.EstimatedMomentumTime
	common.DealWithErr(orchestratorInfo.Save(context.Storage()))
	return nil, nil
}

// SetBridgeMetadataMethod (SetBridgeMetadata) is the administrator
// method that replaces the bridge's own metadata JSON, taking effect
// immediately — no time challenge.
type SetBridgeMetadataMethod struct {
	MethodName string
}

// GetPlasma quotes the EmbeddedSimple tier; the call sends no
// response block.
func (p *SetBridgeMetadataMethod) GetPlasma(plasmaTable *constants.PlasmaTable) (uint64, error) {
	return plasmaTable.EmbeddedSimple, nil
}

// ValidateSendBlock accepts a packed metadata string carrying no
// tokens; the metadata must be valid JSON, else
// constants.ErrInvalidJsonContent.
func (p *SetBridgeMetadataMethod) ValidateSendBlock(block *nom.AccountBlock) error {
	var err error

	param := new(string)
	if err := definition.ABIBridge.UnpackMethod(param, p.MethodName, block.Data); err != nil {
		return constants.ErrUnpackError
	}

	if block.Amount.Sign() != 0 {
		return constants.ErrInvalidTokenOrAmount
	}

	if !IsJSON(*param) {
		return constants.ErrInvalidJsonContent
	}

	block.Data, err = definition.ABIBridge.PackMethod(p.MethodName, param)
	return err
}

// ReceiveBlock saves the new metadata in the bridge state. The
// sender must be the administrator, else
// constants.ErrPermissionDenied. No descendant blocks are emitted.
func (p *SetBridgeMetadataMethod) ReceiveBlock(context vm_context.AccountVmContext, sendBlock *nom.AccountBlock) ([]*nom.AccountBlock, error) {
	if err := p.ValidateSendBlock(sendBlock); err != nil {
		return nil, err
	}

	param := new(string)
	err := definition.ABIBridge.UnpackMethod(param, p.MethodName, sendBlock.Data)
	if err != nil {
		return nil, err
	}

	bridgeInfo, err := definition.GetBridgeInfoVariable(context.Storage())
	if err != nil {
		return nil, err
	}

	if sendBlock.Address.String() != bridgeInfo.Administrator.String() {
		return nil, constants.ErrPermissionDenied
	}

	bridgeInfo.Metadata = *param
	common.DealWithErr(bridgeInfo.Save(context.Storage()))
	return nil, nil
}

// RevokeUnwrapRequestMethod (RevokeUnwrapRequest) is the
// administrator method that marks a registered unwrap request
// revoked so Redeem will permanently refuse it — there is no
// un-revoke. Taking effect immediately, it is the safety valve when
// a fraudulent registration is spotted during the redeem delay.
type RevokeUnwrapRequestMethod struct {
	MethodName string
}

// GetPlasma quotes the EmbeddedSimple tier; the call sends no
// response block.
func (p *RevokeUnwrapRequestMethod) GetPlasma(plasmaTable *constants.PlasmaTable) (uint64, error) {
	return plasmaTable.EmbeddedSimple, nil
}

// ValidateSendBlock accepts a packed definition.RevokeUnwrapParam
// (transaction hash, log index) carrying no tokens; a non-zero
// amount fails with constants.ErrInvalidTokenOrAmount.
func (p *RevokeUnwrapRequestMethod) ValidateSendBlock(block *nom.AccountBlock) error {
	var err error

	param := new(definition.RevokeUnwrapParam)
	if err := definition.ABIBridge.UnpackMethod(param, p.MethodName, block.Data); err != nil {
		return constants.ErrUnpackError
	}

	if block.Amount.Sign() != 0 {
		return constants.ErrInvalidTokenOrAmount
	}

	block.Data, err = definition.ABIBridge.PackMethod(p.MethodName, param.TransactionHash, param.LogIndex)
	return err
}

// ReceiveBlock sets the request's Revoked flag. The request must
// exist (constants.ErrDataNonExistent) and the sender must be the
// administrator, else constants.ErrPermissionDenied; whether it was
// already redeemed is not checked. No descendant blocks are emitted.
func (p *RevokeUnwrapRequestMethod) ReceiveBlock(context vm_context.AccountVmContext, sendBlock *nom.AccountBlock) ([]*nom.AccountBlock, error) {
	if err := p.ValidateSendBlock(sendBlock); err != nil {
		return nil, err
	}

	param := new(definition.RevokeUnwrapParam)
	err := definition.ABIBridge.UnpackMethod(param, p.MethodName, sendBlock.Data)
	if err != nil {
		return nil, err
	}

	bridgeInfo, err := definition.GetBridgeInfoVariable(context.Storage())
	if err != nil {
		return nil, err
	}

	request, err := definition.GetUnwrapTokenRequestByTxHashAndLog(context.Storage(), param.TransactionHash, param.LogIndex)
	if err != nil {
		return nil, err
	}

	if sendBlock.Address.String() != bridgeInfo.Administrator.String() {
		return nil, constants.ErrPermissionDenied
	}
	request.Revoked = 1

	common.DealWithErr(request.Save(context.Storage()))
	return nil, nil
}

// RedeemMethod (Redeem) pays a registered unwrap request out to its
// stored beneficiary. Anyone may call — the destination is fixed in
// the request — so payouts can be triggered on a user's behalf.
type RedeemMethod struct {
	MethodName string
}

// GetPlasma quotes the EmbeddedWWithdraw tier, covering the one
// payout block the call sends.
func (p *RedeemMethod) GetPlasma(plasmaTable *constants.PlasmaTable) (uint64, error) {
	return plasmaTable.EmbeddedWWithdraw, nil
}

// ValidateSendBlock accepts a packed definition.RedeemParam
// (transaction hash, log index) carrying no tokens; a non-zero
// amount fails with constants.ErrInvalidTokenOrAmount.
func (p *RedeemMethod) ValidateSendBlock(block *nom.AccountBlock) error {
	var err error
	param := new(definition.RedeemParam)

	if err := definition.ABIBridge.UnpackMethod(param, p.MethodName, block.Data); err != nil {
		return constants.ErrUnpackError
	}

	if block.Amount.Sign() != 0 {
		return constants.ErrInvalidTokenOrAmount
	}

	block.Data, err = definition.ABIBridge.PackMethod(p.MethodName, param.TransactionHash, param.LogIndex)
	return err
}

// ReceiveBlock marks the request redeemed and returns the one
// descendant block paying it out. The bridge must be fully active
// (CanPerformAction), so halts and the unhalt window block
// redemptions, and:
//   - the request must exist and be neither redeemed nor revoked,
//     else constants.ErrInvalidRedeemRequest
//   - its network must still be registered
//     (constants.ErrUnknownNetwork) and hold a pair matching the
//     request's ZTS or token address — first match wins
//     (constants.ErrTokenNotFound when none)
//   - at least the pair's RedeemDelay momentums — its current value,
//     not the one in force at registration — must have passed since
//     the request's registration height, else
//     constants.ErrInvalidRedeemPeriod
//
// For Owned pairs the descendant mints the amount to the beneficiary
// through the token contract; otherwise it transfers the amount from
// the bridge's balance, which must cover it, else
// constants.ErrInsufficientBalance.
func (p *RedeemMethod) ReceiveBlock(context vm_context.AccountVmContext, sendBlock *nom.AccountBlock) ([]*nom.AccountBlock, error) {
	if err := p.ValidateSendBlock(sendBlock); err != nil {
		return nil, err
	}

	param := new(definition.RedeemParam)
	err := definition.ABIBridge.UnpackMethod(param, p.MethodName, sendBlock.Data)
	if err != nil {
		return nil, err
	}

	if _, _, err = CanPerformAction(context); err != nil {
		return nil, err
	}

	request, err := definition.GetUnwrapTokenRequestByTxHashAndLog(context.Storage(), param.TransactionHash, param.LogIndex)
	if err != nil {
		return nil, err
	}

	if request.Redeemed > 0 || request.Revoked > 0 {
		return nil, constants.ErrInvalidRedeemRequest
	}

	network, err := definition.GetNetworkInfoVariable(context.Storage(), request.NetworkClass, request.ChainId)
	if err != nil {
		return nil, err
	} else if len(network.Name) == 0 {
		return nil, constants.ErrUnknownNetwork
	}

	foundIndex := -1
	for i := 0; i < len(network.TokenPairs); i++ {
		zts := network.TokenPairs[i].TokenStandard
		token := network.TokenPairs[i].TokenAddress
		if reflect.DeepEqual(request.TokenStandard.Bytes(), zts.Bytes()) || request.TokenAddress == token {
			foundIndex = i
			break
		}
	}
	if foundIndex == -1 {
		return nil, constants.ErrTokenNotFound
	}

	momentum, err := context.GetFrontierMomentum()
	if err != nil {
		return nil, err
	}
	if momentum.Height-request.RegistrationMomentumHeight < uint64(network.TokenPairs[foundIndex].RedeemDelay) {
		return nil, constants.ErrInvalidRedeemPeriod
	}

	request.Redeemed = 1
	common.DealWithErr(request.Save(context.Storage()))

	var block *nom.AccountBlock
	if network.TokenPairs[foundIndex].Owned {
		block = &nom.AccountBlock{
			Address:       types.BridgeContract,
			ToAddress:     types.TokenContract,
			BlockType:     nom.BlockTypeContractSend,
			Amount:        big.NewInt(0),
			TokenStandard: network.TokenPairs[foundIndex].TokenStandard,
			Data:          definition.ABIToken.PackMethodPanic(definition.MintMethodName, network.TokenPairs[foundIndex].TokenStandard, request.Amount, request.ToAddress),
		}
	} else {
		balance, err := context.GetBalance(network.TokenPairs[foundIndex].TokenStandard)
		if err != nil {
			return nil, err
		}
		if balance == nil || balance.Cmp(request.Amount) == -1 {
			return nil, constants.ErrInsufficientBalance
		}
		block = &nom.AccountBlock{
			Address:       types.BridgeContract,
			ToAddress:     request.ToAddress,
			BlockType:     nom.BlockTypeContractSend,
			Amount:        request.Amount,
			TokenStandard: network.TokenPairs[foundIndex].TokenStandard,
			Data:          []byte{},
		}
	}

	return []*nom.AccountBlock{block}, nil
}

// NominateGuardiansMethod (NominateGuardians) is the administrator
// method that installs the bridge's guardian set — the addresses
// able to elect a new administrator after an emergency — protected
// by an administrator-delay time challenge; the bridge counterpart
// of NominateGuardiansLiquidity, with identical behavior against the
// bridge contract's own security state.
type NominateGuardiansMethod struct {
	MethodName string
}

// GetPlasma quotes the EmbeddedSimple tier; the call sends no
// response block.
func (p *NominateGuardiansMethod) GetPlasma(plasmaTable *constants.PlasmaTable) (uint64, error) {
	return plasmaTable.EmbeddedSimple, nil
}

// ValidateSendBlock accepts a packed slice of at least
// constants.MinGuardians addresses (constants.ErrInvalidGuardians
// otherwise) carrying no tokens. Each address is re-parsed to verify
// its checksum, which the ABI alone does not, and must not be the
// zero address, else constants.ErrForbiddenParam. Duplicates are not
// rejected: they are stored as given and inflate the majority
// threshold of ProposeAdministrator (half the set's length) without
// granting extra votes, since a guardian's vote is recorded at its
// first matching slot only.
func (p *NominateGuardiansMethod) ValidateSendBlock(block *nom.AccountBlock) error {
	var err error

	guardians := new([]types.Address)
	if err := definition.ABIBridge.UnpackMethod(guardians, p.MethodName, block.Data); err != nil {
		return constants.ErrUnpackError
	}

	if block.Amount.Sign() != 0 {
		return constants.ErrInvalidTokenOrAmount
	}

	if len(*guardians) < constants.MinGuardians {
		return constants.ErrInvalidGuardians
	}
	for _, address := range *guardians {
		// we also check with this method because in the abi the checksum is not verified
		parsedAddress, err := types.ParseAddress(address.String())
		if err != nil {
			return err
		} else if parsedAddress.IsZero() {
			return constants.ErrForbiddenParam
		}
	}

	block.Data, err = definition.ABIBridge.PackMethod(p.MethodName, guardians)
	return err
}

// ReceiveBlock replaces the guardian set in the SecurityInfo,
// resetting all guardian votes to empty. The sender must be the
// administrator, else constants.ErrPermissionDenied. The guardians
// are sorted by address string before hashing and storing, making
// the time challenge order-insensitive; the change passes a
// TimeChallenge with the security info's AdministratorDelay and is
// applied when the call is repeated with the same set after the
// delay.
func (p *NominateGuardiansMethod) ReceiveBlock(context vm_context.AccountVmContext, sendBlock *nom.AccountBlock) ([]*nom.AccountBlock, error) {
	if err := p.ValidateSendBlock(sendBlock); err != nil {
		return nil, err
	}

	guardians := new([]types.Address)
	err := definition.ABIBridge.UnpackMethod(guardians, p.MethodName, sendBlock.Data)
	if err != nil {
		return nil, err
	}

	bridgeInfo, err := definition.GetBridgeInfoVariable(context.Storage())
	if err != nil {
		return nil, err
	}

	if sendBlock.Address.String() != bridgeInfo.Administrator.String() {
		return nil, constants.ErrPermissionDenied
	}

	securityInfo, err := definition.GetSecurityInfoVariable(context.Storage())
	if err != nil {
		return nil, err
	}

	sort.Slice(*guardians, func(i, j int) bool {
		return (*guardians)[i].String() < (*guardians)[j].String()
	})

	guardiansBytes := make([]byte, 0)
	for _, g := range *guardians {
		guardiansBytes = append(guardiansBytes, g.Bytes()...)
	}
	paramsHash := crypto.Hash(guardiansBytes)
	if timeChallengeInfo, errTimeChallenge := TimeChallenge(context, p.MethodName, paramsHash, securityInfo.AdministratorDelay); errTimeChallenge != nil {
		return nil, errTimeChallenge
	} else {
		// if paramsHash is not zero it means we had a new challenge and we can't go further to save the change into local db
		if !timeChallengeInfo.ParamsHash.IsZero() {
			return nil, nil
		}
	}

	securityInfo.Guardians = make([]types.Address, 0)
	securityInfo.GuardiansVotes = make([]types.Address, 0)
	for _, guardian := range *guardians {
		securityInfo.Guardians = append(securityInfo.Guardians, guardian)
		// append empty vote
		securityInfo.GuardiansVotes = append(securityInfo.GuardiansVotes, types.Address{})
	}

	common.DealWithErr(securityInfo.Save(context.Storage()))
	return nil, nil
}

// ProposeAdministratorMethod (ProposeAdministrator) is the guardian
// method that votes for a new administrator while the bridge is in
// emergency (administrator zeroed); the bridge counterpart of
// ProposeAdministratorLiquidity, with identical behavior against the
// bridge contract's own state.
type ProposeAdministratorMethod struct {
	MethodName string
}

// GetPlasma quotes the EmbeddedSimple tier; the call sends no
// response block.
func (p *ProposeAdministratorMethod) GetPlasma(plasmaTable *constants.PlasmaTable) (uint64, error) {
	return plasmaTable.EmbeddedSimple, nil
}

// ValidateSendBlock accepts a packed types.Address carrying no
// tokens. The address is re-parsed to verify its checksum, which the
// ABI alone does not, and must not be the zero address, else
// constants.ErrForbiddenParam.
func (p *ProposeAdministratorMethod) ValidateSendBlock(block *nom.AccountBlock) error {
	var err error

	address := new(types.Address)
	if err := definition.ABIBridge.UnpackMethod(address, p.MethodName, block.Data); err != nil {
		return constants.ErrUnpackError
	}

	if block.Amount.Sign() != 0 {
		return constants.ErrInvalidTokenOrAmount
	}

	// we also check with this method because in the abi the checksum is not verified
	parsedAddress, err := types.ParseAddress(address.String())
	if err != nil {
		return err
	} else if parsedAddress.IsZero() {
		return constants.ErrForbiddenParam
	}

	block.Data, err = definition.ABIBridge.PackMethod(p.MethodName, *address)
	return err
}

// ReceiveBlock records the guardian sender's vote, overwriting its
// previous one. The administrator must be the zero address
// (constants.ErrNotEmergency otherwise) and the sender a guardian,
// else constants.ErrNotGuardian. Once a proposed address gathers the
// votes of a strict majority of guardian slots it becomes the new
// administrator and all votes are reset; the new administrator must
// then restore the TSS key Emergency erased. No descendant blocks
// are emitted.
func (p *ProposeAdministratorMethod) ReceiveBlock(context vm_context.AccountVmContext, sendBlock *nom.AccountBlock) ([]*nom.AccountBlock, error) {
	if err := p.ValidateSendBlock(sendBlock); err != nil {
		return nil, err
	}

	proposedAddress := new(types.Address)
	if err := definition.ABIBridge.UnpackMethod(proposedAddress, p.MethodName, sendBlock.Data); err != nil {
		return nil, constants.ErrUnpackError
	}

	bridgeInfo, err := definition.GetBridgeInfoVariable(context.Storage())
	if err != nil {
		return nil, err
	}

	if !bridgeInfo.Administrator.IsZero() {
		return nil, constants.ErrNotEmergency
	}

	securityInfo, err := definition.GetSecurityInfoVariable(context.Storage())
	if err != nil {
		return nil, err
	}

	found := false
	for idx, guardian := range securityInfo.Guardians {
		if bytes.Equal(guardian.Bytes(), sendBlock.Address.Bytes()) {
			found = true
			if err := securityInfo.GuardiansVotes[idx].SetBytes(proposedAddress.Bytes()); err != nil {
				return nil, err
			}
			break
		}
	}
	if !found {
		return nil, constants.ErrNotGuardian
	}

	votes := make(map[string]uint8)

	threshold := uint8(len(securityInfo.Guardians) / 2)
	for _, vote := range securityInfo.GuardiansVotes {
		if !vote.IsZero() {
			votes[vote.String()] += 1
			// we got a majority, so we change the administrator pub key
			if votes[vote.String()] > threshold {
				votedAddress, errParse := types.ParseAddress(vote.String())
				if errParse != nil {
					return nil, errParse
				} else if votedAddress.IsZero() {
					return nil, constants.ErrForbiddenParam
				}
				if errSet := bridgeInfo.Administrator.SetBytes(votedAddress.Bytes()); errSet != nil {
					return nil, errSet
				}
				common.DealWithErr(bridgeInfo.Save(context.Storage()))
				for idx, _ := range securityInfo.GuardiansVotes {
					securityInfo.GuardiansVotes[idx] = types.Address{}
				}
				break
			}
		}
	}
	common.DealWithErr(securityInfo.Save(context.Storage()))
	return nil, nil
}
