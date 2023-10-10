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

// CheckNetworkAndPairExist for unwrapping we return the associated zts
// for wrapping we return the associated tokenAddress
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

type WrapTokenMethod struct {
	MethodName string
}

func (p *WrapTokenMethod) GetPlasma(plasmaTable *constants.PlasmaTable) (uint64, error) {
	return plasmaTable.EmbeddedSimple, nil
}
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

func GetMessageToSignEvm(data []byte) ([]byte, error) {
	if len(data) != 32 {
		return nil, errors.New("data len is not 32")
	}
	msg := fmt.Sprintf("\x19Ethereum Signed Message:\n32%s", data)
	return crypto.Keccak256([]byte(msg)), nil
}

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

type UpdateWrapRequestMethod struct {
	MethodName string
}

func (p *UpdateWrapRequestMethod) GetPlasma(plasmaTable *constants.PlasmaTable) (uint64, error) {
	return plasmaTable.EmbeddedSimple, nil
}
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

func checkUnwrapMetadataStatic(param *definition.UnwrapTokenParam) error {
	if !ecommon.IsHexAddress(param.TokenAddress) {
		return constants.ErrInvalidToAddress
	}

	if param.Amount.Sign() <= 0 {
		return constants.ErrInvalidTokenOrAmount
	}

	return nil
}

type UnwrapTokenMethod struct {
	MethodName string
}

func (p *UnwrapTokenMethod) GetPlasma(plasmaTable *constants.PlasmaTable) (uint64, error) {
	return plasmaTable.EmbeddedSimple, nil
}
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

type SetNetworkMethod struct {
	MethodName string
}

func (p *SetNetworkMethod) GetPlasma(plasmaTable *constants.PlasmaTable) (uint64, error) {
	return plasmaTable.EmbeddedSimple, nil
}
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

type RemoveNetworkMethod struct {
	MethodName string
}

func (p *RemoveNetworkMethod) GetPlasma(plasmaTable *constants.PlasmaTable) (uint64, error) {
	return plasmaTable.EmbeddedSimple, nil
}
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

type SetNetworkMetadataMethod struct {
	MethodName string
}

func (p *SetNetworkMetadataMethod) GetPlasma(plasmaTable *constants.PlasmaTable) (uint64, error) {
	return plasmaTable.EmbeddedSimple, nil
}
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

func IsJSON(s string) bool {
	var js interface{}
	return json.Unmarshal([]byte(s), &js) == nil
}

type SetTokenPairMethod struct {
	MethodName string
}

func (p *SetTokenPairMethod) GetPlasma(plasmaTable *constants.PlasmaTable) (uint64, error) {
	return plasmaTable.EmbeddedSimple, nil
}
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

type RemoveTokenPairMethod struct {
	MethodName string
}

func (p *RemoveTokenPairMethod) GetPlasma(plasmaTable *constants.PlasmaTable) (uint64, error) {
	return plasmaTable.EmbeddedSimple, nil
}
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

type HaltMethod struct {
	MethodName string
}

func (p *HaltMethod) GetPlasma(plasmaTable *constants.PlasmaTable) (uint64, error) {
	return plasmaTable.EmbeddedSimple, nil
}
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

type UnhaltMethod struct {
	MethodName string
}

func (p *UnhaltMethod) GetPlasma(plasmaTable *constants.PlasmaTable) (uint64, error) {
	return plasmaTable.EmbeddedSimple, nil
}
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

type EmergencyMethod struct {
	MethodName string
}

func (p *EmergencyMethod) GetPlasma(plasmaTable *constants.PlasmaTable) (uint64, error) {
	return plasmaTable.EmbeddedSimple, nil
}
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

type ChangeTssECDSAPubKeyMethod struct {
	MethodName string
}

func (p *ChangeTssECDSAPubKeyMethod) GetPlasma(plasmaTable *constants.PlasmaTable) (uint64, error) {
	return plasmaTable.EmbeddedSimple, nil
}
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

type ChangeAdministratorMethod struct {
	MethodName string
}

func (p *ChangeAdministratorMethod) GetPlasma(plasmaTable *constants.PlasmaTable) (uint64, error) {
	return plasmaTable.EmbeddedSimple, nil
}
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

type SetAllowKeygenMethod struct {
	MethodName string
}

func (p *SetAllowKeygenMethod) GetPlasma(plasmaTable *constants.PlasmaTable) (uint64, error) {
	return plasmaTable.EmbeddedSimple, nil
}
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

type SetOrchestratorInfoMethod struct {
	MethodName string
}

func (p *SetOrchestratorInfoMethod) GetPlasma(plasmaTable *constants.PlasmaTable) (uint64, error) {
	return plasmaTable.EmbeddedSimple, nil
}
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

type SetBridgeMetadataMethod struct {
	MethodName string
}

func (p *SetBridgeMetadataMethod) GetPlasma(plasmaTable *constants.PlasmaTable) (uint64, error) {
	return plasmaTable.EmbeddedSimple, nil
}
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

type RevokeUnwrapRequestMethod struct {
	MethodName string
}

func (p *RevokeUnwrapRequestMethod) GetPlasma(plasmaTable *constants.PlasmaTable) (uint64, error) {
	return plasmaTable.EmbeddedSimple, nil
}
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

type RedeemMethod struct {
	MethodName string
}

func (p *RedeemMethod) GetPlasma(plasmaTable *constants.PlasmaTable) (uint64, error) {
	return plasmaTable.EmbeddedWWithdraw, nil
}
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

type NominateGuardiansMethod struct {
	MethodName string
}

func (p *NominateGuardiansMethod) GetPlasma(plasmaTable *constants.PlasmaTable) (uint64, error) {
	return plasmaTable.EmbeddedSimple, nil
}
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

type ProposeAdministratorMethod struct {
	MethodName string
}

func (p *ProposeAdministratorMethod) GetPlasma(plasmaTable *constants.PlasmaTable) (uint64, error) {
	return plasmaTable.EmbeddedSimple, nil
}
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
