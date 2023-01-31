package implementation

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"fmt"
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
	"math"
	"math/big"
	"sort"
	"strings"
)

var (
	bridgeLog = common.EmbeddedLogger.New("contract", "bridge")
)

func GetThreshold(value uint32) (uint32, error) {
	if value == 0 {
		return 0, errors.New("invalid input")
	}
	threshold := uint32(math.Ceil(float64(value)*2.0/3.0)) - 1
	return threshold, nil
}

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

func CheckEDDSASignature(message []byte, pubKeyStr, signatureStr string) (bool, error) {
	pubKey, err := base64.StdEncoding.DecodeString(pubKeyStr)
	if err != nil {
		return false, constants.ErrInvalidB64Decode
	}
	if len(pubKey) != constants.EdDSAPubKeyLength {
		return false, constants.ErrInvalidB64Decode
	}

	signature, err := base64.StdEncoding.DecodeString(signatureStr)
	if err != nil {
		return false, constants.ErrInvalidB64Decode
	}
	if len(signature) != 64 {
		return false, constants.ErrInvalidEDDSASignature
	}

	if ed25519.Verify(pubKey, message, signature) {
		return true, nil
	}
	return false, constants.ErrInvalidEDDSASignature
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
	if len(bridgeInfo.CompressedTssECDSAPubKey) == 0 || len(bridgeInfo.AdministratorEDDSAPubKey) == 0 {
		return nil, constants.ErrBridgeNotInitialized
	}

	return bridgeInfo, nil
}

func CheckSecurityInitialized(context vm_context.AccountVmContext) (*definition.SecurityInfoVariable, error) {
	securityInfo, err := definition.GetSecurityInfoVariable(context.Storage())
	if err != nil {
		return nil, err
	}
	if len(securityInfo.Guardians) < constants.MinGuardians {
		return nil, errors.New("security not initialized")
	}

	return securityInfo, nil
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
func CheckNetworkAndPairExist(context vm_context.AccountVmContext, networkType uint32, chainId uint32, ztsOrToken string) (*definition.TokenPair, error) {
	network, err := definition.GetNetworkInfoVariable(context.Storage(), networkType, chainId)
	if err != nil {
		return nil, err
	}
	if network.Name == "" {
		return nil, constants.ErrUnknownNetwork
	}

	for i := 0; i < len(network.TokenPairs); i++ {
		zts := network.TokenPairs[i].TokenStandard
		token := network.TokenPairs[i].TokenAddress
		if ztsOrToken == zts || ztsOrToken == token {
			return &network.TokenPairs[i], nil
		}
	}
	return nil, constants.ErrTokenNotFound
}

func checkWrapMetadataStatic(param *definition.WrapTokenParam) error {
	if !ecommon.IsHexAddress(param.ToAddress) {
		return constants.ErrInvalidToAddress
	}
	return nil
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

	if err = checkWrapMetadataStatic(param); err != nil {
		return err
	}

	if block.Amount.Cmp(big.NewInt(0)) == 0 {
		return constants.ErrInvalidTokenOrAmount
	}

	block.Data, err = definition.ABIBridge.PackMethod(p.MethodName, param.NetworkType, param.ChainId, param.ToAddress)
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

	tokenPair, err := CheckNetworkAndPairExist(context, param.NetworkType, param.ChainId, sendBlock.TokenStandard.String())
	if err != nil {
		return nil, err
	}
	if tokenPair.Bridgeable == false {
		return nil, constants.ErrTokenNotBridgeable
	}

	if sendBlock.Amount.Cmp(tokenPair.MinAmount) == -1 {
		return nil, constants.ErrInvalidMinAmount
	}

	frontierMomentum, err := context.GetFrontierMomentum()
	common.DealWithErr(err)

	request := new(definition.WrapTokenRequest)
	request.NetworkType = param.NetworkType
	request.ChainId = param.ChainId
	request.Id = sendBlock.Hash
	request.ToAddress = strings.ToLower(param.ToAddress)
	request.TokenStandard = sendBlock.TokenStandard
	request.TokenAddress = strings.ToLower((*tokenPair).TokenAddress)
	request.Amount = new(big.Int).Set(sendBlock.Amount)
	amount := new(big.Int).Set(sendBlock.Amount)
	fee := big.NewInt(int64(tokenPair.FeePercentage))
	amount = amount.Mul(amount, fee)
	request.Fee = amount.Div(amount, big.NewInt(int64(constants.MaximumFee)))
	request.Signature = ""
	request.CreationMomentumHeight = frontierMomentum.Height

	ztsFeesInfo, err := definition.GetZtsFeesInfoVariable(context.Storage(), sendBlock.TokenStandard.String())
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

func hashByNetworkType(data []byte, networkType uint32) ([]byte, error) {
	switch networkType {
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
		big.NewInt(0).SetUint64(uint64(request.NetworkType)), // network type
		big.NewInt(0).SetUint64(uint64(request.ChainId)),     // network chain id
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
	return hashByNetworkType(messageBytes, request.NetworkType)
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

	networkInfo, err := definition.GetNetworkInfoVariable(context.Storage(), request.NetworkType, request.ChainId)
	if err != nil {
		return nil, err
	} else if len(networkInfo.Name) == 0 {
		return nil, constants.ErrDataNonExistent
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
		big.NewInt(0).SetUint64(uint64(param.NetworkType)),    // network type
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

	return crypto.Hash(messageBytes), nil
}

func checkUnwrapMetadataStatic(param *definition.UnwrapTokenParam) error {
	if !ecommon.IsHexAddress(param.TokenAddress) {
		return constants.ErrInvalidToAddress
	}

	if param.Amount.Sign() <= 0 {
		return constants.ErrInvalidTokenOrAmount
	}

	if param.NetworkType == 0 || param.ChainId == 0 {
		return constants.ErrForbiddenParam
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

	block.Data, err = definition.ABIBridge.PackMethod(p.MethodName, param.NetworkType, param.ChainId, param.TransactionHash, param.LogIndex, param.ToAddress, param.TokenAddress, param.Amount, param.Signature)
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

	networkInfo, err := definition.GetNetworkInfoVariable(context.Storage(), param.NetworkType, param.ChainId)
	if err != nil {
		bridgeLog.Error("Unwrap", "error", err)
		return nil, err
	} else if networkInfo.Type != param.NetworkType || networkInfo.Id != param.ChainId {
		return nil, constants.ErrForbiddenParam
	}

	tokenPair, err := CheckNetworkAndPairExist(context, param.NetworkType, param.ChainId, param.TokenAddress)
	if err != nil {
		return nil, err
	} else if tokenPair == nil {
		return nil, errors.New("token pair not found")
	}
	if tokenPair.Redeemable == false {
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
		NetworkType:                param.NetworkType,
		ChainId:                    param.ChainId,
		TransactionHash:            param.TransactionHash,
		LogIndex:                   param.LogIndex,
		ToAddress:                  param.ToAddress,
		TokenAddress:               strings.ToLower(param.TokenAddress),
		Amount:                     param.Amount,
		Signature:                  param.Signature,
		Redeemed:                   0,
		Revoked:                    0,
	}

	common.DealWithErr(request.Save(context.Storage()))
	return nil, nil
}

type AddNetworkMethod struct {
	MethodName string
}

func (p *AddNetworkMethod) GetPlasma(plasmaTable *constants.PlasmaTable) (uint64, error) {
	return plasmaTable.EmbeddedSimple, nil
}
func (p *AddNetworkMethod) ValidateSendBlock(block *nom.AccountBlock) error {
	var err error
	param := new(definition.NetworkInfoParam)

	if err := definition.ABIBridge.UnpackMethod(param, p.MethodName, block.Data); err != nil {
		return constants.ErrUnpackError
	}

	if block.Amount.Sign() != 0 {
		return constants.ErrInvalidTokenOrAmount
	}

	// todo make sure name is not longer than 32
	// check contract address as eth address

	if !IsJSON(param.Metadata) {
		return constants.ErrInvalidJsonContent
	}

	block.Data, err = definition.ABIBridge.PackMethod(p.MethodName, param.Type, param.ChainId, param.Name, param.ContractAddress, param.Metadata)
	return err
}
func (p *AddNetworkMethod) ReceiveBlock(context vm_context.AccountVmContext, sendBlock *nom.AccountBlock) ([]*nom.AccountBlock, error) {
	if err := p.ValidateSendBlock(sendBlock); err != nil {
		return nil, err
	}

	param := new(definition.NetworkInfoParam)
	err := definition.ABIBridge.UnpackMethod(param, p.MethodName, sendBlock.Data)
	if err != nil {
		return nil, err
	}

	bridgeInfo, _, err := CanPerformAction(context)
	if err != nil {
		return nil, err
	}

	senderPubKey := base64.StdEncoding.EncodeToString(sendBlock.PublicKey)
	if senderPubKey != bridgeInfo.AdministratorEDDSAPubKey {
		return nil, constants.ErrPermissionDenied
	}

	if param.Name == "" {
		return nil, constants.ErrInvalidNetworkName
	}

	if !ecommon.IsHexAddress(param.ContractAddress) {
		return nil, constants.ErrInvalidContractAddress
	}

	networkInfo, err := definition.GetNetworkInfoVariable(context.Storage(), param.Type, param.ChainId)
	if err != nil {
		return nil, err
	}

	networkInfo.Type = param.Type
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

	block.Data, err = definition.ABIBridge.PackMethod(p.MethodName, param.Type, param.ChainId)
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

	bridgeInfo, _, err := CanPerformAction(context)
	if err != nil {
		return nil, err
	}

	senderPubKey := base64.StdEncoding.EncodeToString(sendBlock.PublicKey)
	if senderPubKey != bridgeInfo.AdministratorEDDSAPubKey {
		return nil, constants.ErrPermissionDenied
	}

	networkInfo, err := definition.GetNetworkInfoVariable(context.Storage(), param.Type, param.ChainId)
	if err != nil {
		return nil, err
	}
	if networkInfo.Name == "" || networkInfo.Type != param.Type || networkInfo.Id != param.ChainId {
		// todo implement error
		return nil, constants.ErrPermissionDenied
	}
	networkInfoVariable, err := definition.EncodeNetworkInfo(networkInfo)
	if err != nil {
		return nil, err
	}
	common.DealWithErr(networkInfoVariable.Delete(context.Storage()))
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

	if !IsJSON(param.Metadata) {
		return constants.ErrInvalidJsonContent
	}

	block.Data, err = definition.ABIBridge.PackMethod(p.MethodName, param.NetworkType, param.ChainId, param.TokenStandard, param.TokenAddress, param.Bridgeable, param.Redeemable, param.Owned, param.MinAmount, param.FeePercentage, param.RedeemDelay, param.Metadata)
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

	bridgeInfo, _, err := CanPerformAction(context)
	if err != nil {
		return nil, err
	}

	if param.MinAmount.Sign() <= 0 {
		return nil, constants.ErrInvalidMinAmount
	}

	if param.FeePercentage > constants.MaximumFee {
		return nil, constants.ErrInvalidFee
	}

	if param.RedeemDelay == 0 {
		return nil, constants.ErrInvalidArguments
	}

	senderPubKey := base64.StdEncoding.EncodeToString(sendBlock.PublicKey)
	if senderPubKey != bridgeInfo.AdministratorEDDSAPubKey {
		return nil, constants.ErrPermissionDenied
	}

	networkInfo, err := definition.GetNetworkInfoVariable(context.Storage(), param.NetworkType, param.ChainId)
	if err != nil {
		return nil, err
	}
	if networkInfo.Name == "" || networkInfo.Type != param.NetworkType || networkInfo.Id != param.ChainId {
		// todo implement error
		return nil, constants.ErrInvalidArguments
	}

	tokenPair := definition.TokenPair{
		TokenStandard: param.TokenStandard.String(),
		TokenAddress:  param.TokenAddress,
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
		if networkInfo.TokenPairs[i].TokenStandard == param.TokenStandard.String() {
			networkInfo.TokenPairs[i] = tokenPair
			found = true
			break
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

	block.Data, err = definition.ABIBridge.PackMethod(p.MethodName, param.NetworkType, param.ChainId, param.TokenStandard, param.TokenAddress)
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

	bridgeInfo, _, err := CanPerformAction(context)
	if err != nil {
		return nil, err
	}

	senderPubKey := base64.StdEncoding.EncodeToString(sendBlock.PublicKey)
	if senderPubKey != bridgeInfo.AdministratorEDDSAPubKey {
		return nil, constants.ErrPermissionDenied
	}

	networkInfo, err := definition.GetNetworkInfoVariable(context.Storage(), param.NetworkType, param.ChainId)
	if err != nil {
		return nil, err
	}
	if networkInfo.Name == "" || networkInfo.Type != param.NetworkType || networkInfo.Id != param.ChainId {
		// todo implement error
		return nil, constants.ErrPermissionDenied
	}

	for i := 0; i < len(networkInfo.TokenPairs); i++ {
		zts := networkInfo.TokenPairs[i].TokenStandard
		token := networkInfo.TokenPairs[i].TokenAddress
		if param.TokenStandard.String() == zts && param.TokenAddress == token {
			networkInfo.TokenPairs[i] = networkInfo.TokenPairs[len(networkInfo.TokenPairs)-1]
			networkInfo.TokenPairs = networkInfo.TokenPairs[:len(networkInfo.TokenPairs)-1]
			break
		}
	}

	networkInfoVariable, err := definition.EncodeNetworkInfo(networkInfo)
	if err != nil {
		return nil, err
	}
	common.DealWithErr(networkInfoVariable.Save(context.Storage()))
	return nil, nil
}

type UpdateNetworkMetadataMethod struct {
	MethodName string
}

func (p *UpdateNetworkMetadataMethod) GetPlasma(plasmaTable *constants.PlasmaTable) (uint64, error) {
	return plasmaTable.EmbeddedSimple, nil
}
func (p *UpdateNetworkMetadataMethod) ValidateSendBlock(block *nom.AccountBlock) error {
	var err error

	param := new(definition.UpdateNetworkMetadataParam)
	if err := definition.ABIBridge.UnpackMethod(param, p.MethodName, block.Data); err != nil {
		return constants.ErrUnpackError
	}

	if block.Amount.Sign() != 0 {
		return constants.ErrInvalidTokenOrAmount
	}

	if !IsJSON(param.Metadata) {
		return constants.ErrInvalidJsonContent
	}

	block.Data, err = definition.ABIBridge.PackMethod(p.MethodName, param.NetworkType, param.ChainId, param.Metadata)
	return err
}
func (p *UpdateNetworkMetadataMethod) ReceiveBlock(context vm_context.AccountVmContext, sendBlock *nom.AccountBlock) ([]*nom.AccountBlock, error) {
	if err := p.ValidateSendBlock(sendBlock); err != nil {
		return nil, err
	}

	param := new(definition.UpdateNetworkMetadataParam)
	err := definition.ABIBridge.UnpackMethod(param, p.MethodName, sendBlock.Data)
	if err != nil {
		return nil, err
	}
	// todo decide whether or not we can do it when it is halted
	bridgeInfo, err := definition.GetBridgeInfoVariable(context.Storage())
	if err != nil {
		return nil, err
	}

	senderPubKey := base64.StdEncoding.EncodeToString(sendBlock.PublicKey)
	if senderPubKey != bridgeInfo.AdministratorEDDSAPubKey {
		return nil, constants.ErrPermissionDenied
	}

	networkInfo, err := definition.GetNetworkInfoVariable(context.Storage(), param.NetworkType, param.ChainId)
	if err != nil {
		return nil, err
	}
	if networkInfo.Name == "" || networkInfo.Type != param.NetworkType || networkInfo.Id != param.ChainId {
		// todo implement error
		return nil, constants.ErrPermissionDenied
	}

	networkInfo.Metadata = param.Metadata
	networkInfoVariable, err := definition.EncodeNetworkInfo(networkInfo)
	if err != nil {
		return nil, err
	}
	common.DealWithErr(networkInfoVariable.Save(context.Storage()))
	return nil, nil
}

func GetBasicMethodMessage(methodName string, tssNonce uint64, networkType, chainId uint32) ([]byte, error) {
	args := eabi.Arguments{{Type: definition.StringTy}, {Type: definition.Uint256Ty}, {Type: definition.Uint256Ty}, {Type: definition.Uint256Ty}}
	values := make([]interface{}, 0)
	values = append(values,
		methodName,
		big.NewInt(0).SetUint64(uint64(networkType)),
		big.NewInt(0).SetUint64(uint64(chainId)),
		big.NewInt(0).SetUint64(tssNonce), // nonce
	)

	messageBytes, err := args.PackValues(values)
	if err != nil {
		return nil, err
	}

	//bridgeLog.Info("CheckECDSASignature", "message", message)

	return hashByNetworkType(messageBytes, networkType)
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

	bridgeInfo, _, err := CanPerformAction(context)
	if err != nil {
		return nil, err
	}

	senderPubKey := base64.StdEncoding.EncodeToString(sendBlock.PublicKey)
	if senderPubKey != bridgeInfo.AdministratorEDDSAPubKey {
		// todo get znn chainIdentifier from variable
		message, err := GetBasicMethodMessage(p.MethodName, bridgeInfo.TssNonce, definition.NoMClass, 1)
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
	bridgeInfo, err := CheckBridgeInitialized(context)
	if err != nil {
		return nil, err
	}
	// we do this check, so we cannot unhalt more than one time and actually increase the duration of the halt
	if bridgeInfo.Halted == false {
		return nil, errors.New("bridge not halted")
	}

	senderPubKey := base64.StdEncoding.EncodeToString(sendBlock.PublicKey)
	if senderPubKey != bridgeInfo.AdministratorEDDSAPubKey {
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

	senderPubKey := base64.StdEncoding.EncodeToString(sendBlock.PublicKey)
	if senderPubKey != bridgeInfo.AdministratorEDDSAPubKey {
		return nil, constants.ErrPermissionDenied
	}

	bridgeInfo.AdministratorEDDSAPubKey = ""
	bridgeInfo.CompressedTssECDSAPubKey = ""
	bridgeInfo.DecompressedTssECDSAPubKey = ""
	bridgeInfo.Halted = true
	common.DealWithErr(bridgeInfo.Save(context.Storage()))
	return nil, nil
}

func GetChangePubKeyMessage(methodName string, networkType uint32, chainId, tssNonce uint64, pubKey string) ([]byte, error) {
	args := eabi.Arguments{{Type: definition.StringTy}, {Type: definition.Uint256Ty}, {Type: definition.Uint256Ty}, {Type: definition.Uint256Ty}, {Type: definition.Uint256Ty}}
	values := make([]interface{}, 0)
	values = append(values,
		methodName,
		big.NewInt(0).SetUint64(uint64(networkType)),
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
	} else if methodName == definition.ChangeAdministratorEDDSAPubKeyMethodName {
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
	// todo check sendBlock params for all methods

	if block.Amount.Sign() != 0 {
		return constants.ErrInvalidTokenOrAmount
	}

	block.Data, err = definition.ABIBridge.PackMethod(p.MethodName, param.PubKey, param.OldPubKeySignature, param.NewPubKeySignature, param.KeySignThreshold)
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

	orchestratorInfo, err := CheckOrchestratorInfoInitialized(context)
	if err != nil {
		return nil, err
	}
	if _, err := CheckSecurityInitialized(context); err != nil {
		return nil, err
	}
	bridgeInfo, err := definition.GetBridgeInfoVariable(context.Storage())
	if err != nil {
		return nil, err
	}

	threshold, err := GetThreshold(orchestratorInfo.KeyGenThreshold)
	if err != nil {
		return nil, err
	}
	if param.KeySignThreshold < threshold {
		return nil, constants.ErrInvalidKeySignThreshold
	}

	pubKey, err := base64.StdEncoding.DecodeString(param.PubKey)
	if err != nil {
		return nil, constants.ErrInvalidB64Decode
	} else if len(pubKey) != constants.CompressedECDSAPubKeyLength {
		return nil, constants.ErrInvalidCompressedECDSAPubKeyLength
	}
	X, Y := secp256k1.DecompressPubkey(pubKey)
	dPubKeyBytes := make([]byte, 1)
	dPubKeyBytes[0] = 4
	dPubKeyBytes = append(dPubKeyBytes, X.Bytes()...)
	dPubKeyBytes = append(dPubKeyBytes, Y.Bytes()...)
	newDecompressedPubKey := base64.StdEncoding.EncodeToString(dPubKeyBytes)

	senderPubKey := base64.StdEncoding.EncodeToString(sendBlock.PublicKey)
	if senderPubKey != bridgeInfo.AdministratorEDDSAPubKey {
		// this only applies to non administrator calls
		if !bridgeInfo.AllowKeyGen {
			return nil, constants.ErrNotAllowedToChangeSignature
		}
		// todo get zenon chainId as variable
		message, err := GetChangePubKeyMessage(p.MethodName, definition.NoMClass, 1, bridgeInfo.TssNonce, param.PubKey)
		if err != nil {
			return nil, err
		}
		result, err := CheckECDSASignature(message, bridgeInfo.DecompressedTssECDSAPubKey, param.OldPubKeySignature)
		if err != nil || !result {
			bridgeLog.Error("ChangeTssECDSAPubKey-ErrInvalidSignature", "error", err, "result", result)
			return nil, constants.ErrInvalidECDSASignature
		}

		result, err = CheckECDSASignature(message, newDecompressedPubKey, param.NewPubKeySignature)
		if err != nil || !result {
			bridgeLog.Error("ChangeTssECDSAPubKey-ErrInvalidSignature", "error", err, "result", result)
			return nil, constants.ErrInvalidECDSASignature
		}

		bridgeInfo.TssNonce += 1
	} else {
		securityInfo, err := definition.GetSecurityInfoVariable(context.Storage())
		common.DealWithErr(err)

		momentum, err := context.GetFrontierMomentum()
		common.DealWithErr(err)

		//
		if securityInfo.RequestedTssPubKey != param.PubKey {
			securityInfo.RequestedTssPubKey = param.PubKey
			securityInfo.TssChangeMomentum = momentum.Height
			common.DealWithErr(securityInfo.Save(context.Storage()))
			return nil, nil
		} else {
			// if the delay has passed, we can change the pub key
			if securityInfo.TssChangeMomentum+securityInfo.TssDelay >= momentum.Height {
				return nil, errors.New("change tss not due")
			} else {
				securityInfo.RequestedTssPubKey = ""
			}
		}
		common.DealWithErr(securityInfo.Save(context.Storage()))
	}

	bridgeInfo.CompressedTssECDSAPubKey = param.PubKey
	bridgeInfo.DecompressedTssECDSAPubKey = newDecompressedPubKey
	bridgeInfo.AllowKeyGen = false
	common.DealWithErr(bridgeInfo.Save(context.Storage()))
	orchestratorInfo.KeySignThreshold = param.KeySignThreshold
	common.DealWithErr(orchestratorInfo.Save(context.Storage()))
	return nil, nil
}

type ChangeAdministratorEDDSAPubKeyMethod struct {
	MethodName string
}

func (p *ChangeAdministratorEDDSAPubKeyMethod) GetPlasma(plasmaTable *constants.PlasmaTable) (uint64, error) {
	return plasmaTable.EmbeddedSimple, nil
}
func (p *ChangeAdministratorEDDSAPubKeyMethod) ValidateSendBlock(block *nom.AccountBlock) error {
	var err error
	param := new(definition.ChangeEDDSAPubKeyParam)
	if err = definition.ABIBridge.UnpackMethod(param, p.MethodName, block.Data); err != nil {
		return constants.ErrUnpackError
	}

	if block.Amount.Sign() != 0 {
		return constants.ErrInvalidTokenOrAmount
	}

	pubKey, err := base64.StdEncoding.DecodeString(param.PubKey)
	if err != nil {
		return constants.ErrInvalidB64Decode
	}
	if len(pubKey) != constants.EdDSAPubKeyLength {
		return constants.ErrInvalidEDDSAPubKey
	}

	signature, err := base64.StdEncoding.DecodeString(param.Signature)
	if err != nil {
		return constants.ErrInvalidB64Decode
	}
	if len(signature) != 64 {
		return constants.ErrInvalidEDDSASignature
	}

	block.Data, err = definition.ABIBridge.PackMethod(p.MethodName, param.PubKey, param.Signature)
	return err
}
func (p *ChangeAdministratorEDDSAPubKeyMethod) ReceiveBlock(context vm_context.AccountVmContext, sendBlock *nom.AccountBlock) ([]*nom.AccountBlock, error) {
	if err := p.ValidateSendBlock(sendBlock); err != nil {
		return nil, err
	}

	param := new(definition.ChangeEDDSAPubKeyParam)
	err := definition.ABIBridge.UnpackMethod(param, p.MethodName, sendBlock.Data)
	if err != nil {
		return nil, err
	}

	bridgeInfo, err := definition.GetBridgeInfoVariable(context.Storage())
	if err != nil {
		return nil, err
	}

	// todo check address instead of PubKey because it may be a contract (eg governance contract)
	senderPubKey := base64.StdEncoding.EncodeToString(sendBlock.PublicKey)
	if senderPubKey != bridgeInfo.AdministratorEDDSAPubKey {
		return nil, constants.ErrPermissionDenied
	}

	momentum, err := context.GetFrontierMomentum()
	if err != nil {
		return nil, err
	}

	// todo get chainid as variable
	// todo don't check signature for new admin public key because it may be a contract (eg governance contract)
	message, err := GetChangePubKeyMessage(p.MethodName, definition.NoMClass, 1, bridgeInfo.TssNonce, param.PubKey)
	if err != nil {
		return nil, err
	}
	result, err := CheckEDDSASignature(message, param.PubKey, param.Signature)
	if err != nil || !result {
		return nil, constants.ErrInvalidEDDSASignature
	}

	securityInfo, err := definition.GetSecurityInfoVariable(context.Storage())
	common.DealWithErr(err)

	// If we try to change the pubKey with another one than the one requested, the timer restarts
	if securityInfo.RequestedAdministratorPubKey != param.PubKey {
		securityInfo.RequestedAdministratorPubKey = param.PubKey
		securityInfo.AdministratorChangeMomentum = momentum.Height
	} else {
		// if the delay has passed, we can change the pub key
		if securityInfo.AdministratorChangeMomentum+securityInfo.AdministratorDelay < momentum.Height {
			bridgeInfo.AdministratorEDDSAPubKey = param.PubKey
			securityInfo.RequestedAdministratorPubKey = ""
			common.DealWithErr(bridgeInfo.Save(context.Storage()))
		}
	}

	common.DealWithErr(securityInfo.Save(context.Storage()))
	return nil, nil
}

type AllowKeygenMethod struct {
	MethodName string
}

func (p *AllowKeygenMethod) GetPlasma(plasmaTable *constants.PlasmaTable) (uint64, error) {
	return plasmaTable.EmbeddedSimple, nil
}
func (p *AllowKeygenMethod) ValidateSendBlock(block *nom.AccountBlock) error {
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
func (p *AllowKeygenMethod) ReceiveBlock(context vm_context.AccountVmContext, sendBlock *nom.AccountBlock) ([]*nom.AccountBlock, error) {
	if err := p.ValidateSendBlock(sendBlock); err != nil {
		return nil, err
	}

	err := definition.ABIBridge.UnpackEmptyMethod(p.MethodName, sendBlock.Data)
	if err != nil {
		return nil, err
	}

	orchestratorInfo, err := CheckOrchestratorInfoInitialized(context)
	if err != nil {
		return nil, err
	}

	bridgeInfo, err := definition.GetBridgeInfoVariable(context.Storage())
	if err != nil {
		return nil, err
	}

	senderPubKey := base64.StdEncoding.EncodeToString(sendBlock.PublicKey)

	if senderPubKey != bridgeInfo.AdministratorEDDSAPubKey {
		return nil, constants.ErrPermissionDenied
	}

	bridgeInfo.AllowKeyGen = true
	common.DealWithErr(bridgeInfo.Save(context.Storage()))

	momentum, err := context.GetFrontierMomentum()
	if err != nil {
		return nil, err
	}
	orchestratorInfo.AllowKeyGenHeight = momentum.Height
	common.DealWithErr(orchestratorInfo.Save(context.Storage()))
	return nil, nil
}

type SetUnhaltDurationMethod struct {
	MethodName string
}

func (p *SetUnhaltDurationMethod) GetPlasma(plasmaTable *constants.PlasmaTable) (uint64, error) {
	return plasmaTable.EmbeddedSimple, nil
}
func (p *SetUnhaltDurationMethod) ValidateSendBlock(block *nom.AccountBlock) error {
	var err error

	param := new(uint64)
	if err := definition.ABIBridge.UnpackMethod(param, p.MethodName, block.Data); err != nil {
		return constants.ErrUnpackError
	}

	if *param < constants.MinUnhaltDurationInMomentums {
		// todo change error
		return errors.New("not allowed")
	}

	if block.Amount.Sign() != 0 {
		return constants.ErrInvalidTokenOrAmount
	}

	block.Data, err = definition.ABIBridge.PackMethod(p.MethodName, param)
	return err
}
func (p *SetUnhaltDurationMethod) ReceiveBlock(context vm_context.AccountVmContext, sendBlock *nom.AccountBlock) ([]*nom.AccountBlock, error) {
	if err := p.ValidateSendBlock(sendBlock); err != nil {
		return nil, err
	}

	param := new(uint64)
	err := definition.ABIBridge.UnpackMethod(param, p.MethodName, sendBlock.Data)
	if err != nil {
		return nil, err
	}
	if *param < constants.MinUnhaltDurationInMomentums {
		return nil, constants.ErrForbiddenParam
	}

	_, err = CheckOrchestratorInfoInitialized(context)
	if err != nil {
		return nil, err
	}

	bridgeInfo, err := definition.GetBridgeInfoVariable(context.Storage())
	if err != nil {
		return nil, err
	}

	senderPubKey := base64.StdEncoding.EncodeToString(sendBlock.PublicKey)
	if senderPubKey != bridgeInfo.AdministratorEDDSAPubKey {
		return nil, constants.ErrPermissionDenied
	}

	bridgeInfo.UnhaltDurationInMomentums = *param
	common.DealWithErr(bridgeInfo.Save(context.Storage()))
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
		return constants.ErrInvalidArguments
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

	senderPubKey := base64.StdEncoding.EncodeToString(sendBlock.PublicKey)
	if senderPubKey != bridgeInfo.AdministratorEDDSAPubKey {
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

type UpdateBridgeMetadataMethod struct {
	MethodName string
}

func (p *UpdateBridgeMetadataMethod) GetPlasma(plasmaTable *constants.PlasmaTable) (uint64, error) {
	return plasmaTable.EmbeddedSimple, nil
}
func (p *UpdateBridgeMetadataMethod) ValidateSendBlock(block *nom.AccountBlock) error {
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
func (p *UpdateBridgeMetadataMethod) ReceiveBlock(context vm_context.AccountVmContext, sendBlock *nom.AccountBlock) ([]*nom.AccountBlock, error) {
	if err := p.ValidateSendBlock(sendBlock); err != nil {
		return nil, err
	}

	param := new(string)
	err := definition.ABIBridge.UnpackMethod(param, p.MethodName, sendBlock.Data)
	if err != nil {
		return nil, err
	}

	// todo what checks?
	bridgeInfo, err := definition.GetBridgeInfoVariable(context.Storage())
	if err != nil {
		return nil, err
	}

	senderPubKey := base64.StdEncoding.EncodeToString(sendBlock.PublicKey)
	if senderPubKey != bridgeInfo.AdministratorEDDSAPubKey {
		return nil, constants.ErrPermissionDenied
	}

	bridgeInfo.Metadata = *param
	common.DealWithErr(bridgeInfo.Save(context.Storage()))
	return nil, nil
}

func GetRevokeUnwrapMessage(param *definition.RevokeUnwrapParam, methodName string, tssNonce uint64) ([]byte, error) {
	args := eabi.Arguments{{Type: definition.StringTy}, {Type: definition.Uint256Ty}, {Type: definition.Uint256Ty}}
	values := make([]interface{}, 0)
	values = append(values,
		methodName,
		big.NewInt(0).SetUint64(tssNonce), // nonce
		big.NewInt(0).SetBytes(param.TransactionHash.Bytes()), // Tx hash
	)

	messageBytes, err := args.PackValues(values)
	if err != nil {
		return nil, err
	}

	//bridgeLog.Info("CheckECDSASignature", "message", message)

	return crypto.Hash(messageBytes), nil
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

	block.Data, err = definition.ABIBridge.PackMethod(p.MethodName, param.TransactionHash, param.Signature)
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
	// todo what checks?
	bridgeInfo, err := CheckBridgeInitialized(context)
	if err != nil {
		return nil, err
	}

	request, err := definition.GetUnwrapTokenRequestByTxHashAndLog(context.Storage(), param.TransactionHash, param.LogIndex)
	if err != nil {
		return nil, err
	}

	senderPubKey := base64.StdEncoding.EncodeToString(sendBlock.PublicKey)
	if senderPubKey != bridgeInfo.AdministratorEDDSAPubKey {
		message, err := GetRevokeUnwrapMessage(param, p.MethodName, bridgeInfo.TssNonce)
		if err != nil {
			return nil, err
		}
		result, err := CheckECDSASignature(message, bridgeInfo.DecompressedTssECDSAPubKey, param.Signature)
		if err != nil || !result {
			bridgeLog.Error("RevokeUnwrapRequest-ErrInvalidSignature", "error", err, "result", result)
			return nil, constants.ErrInvalidECDSASignature
		}

		bridgeInfo.TssNonce += 1
		common.DealWithErr(bridgeInfo.Save(context.Storage()))
	}
	request.Revoked = 1

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
	// TODO all getters should return err
	if err != nil {
		return nil, err
	}

	if request.Redeemed > 0 || request.Revoked > 0 {
		return nil, constants.ErrInvalidRedeemRequest
	}

	tokenPair, err := CheckNetworkAndPairExist(context, request.NetworkType, request.ChainId, request.TokenAddress)
	if err != nil {
		return nil, err
	} else if tokenPair == nil {
		return nil, errors.New("token pair not found")
	}

	momentum, err := context.GetFrontierMomentum()
	if err != nil {
		return nil, err
	}
	if momentum.Height-request.RegistrationMomentumHeight < uint64(tokenPair.RedeemDelay) {
		return nil, constants.ErrInvalidRedeemPeriod
	}

	request.Redeemed = 1
	common.DealWithErr(request.Save(context.Storage()))

	zts := types.ParseZTSPanic(tokenPair.TokenStandard)

	var block *nom.AccountBlock
	if tokenPair.Owned {
		block = &nom.AccountBlock{
			Address:       types.BridgeContract,
			ToAddress:     types.TokenContract,
			BlockType:     nom.BlockTypeContractSend,
			Amount:        big.NewInt(0),
			TokenStandard: zts,
			Data:          definition.ABIToken.PackMethodPanic(definition.MintMethodName, zts, request.Amount, request.ToAddress),
		}
	} else {
		balance, err := context.GetBalance(zts)
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
			TokenStandard: zts,
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

	guardians := new([]string)
	if err := definition.ABIBridge.UnpackMethod(guardians, p.MethodName, block.Data); err != nil {
		return constants.ErrUnpackError
	}

	if block.Amount.Sign() != 0 {
		return constants.ErrInvalidTokenOrAmount
	}
	// todo change
	if len(*guardians) < 4 {
		return errors.New("not enough guardians")
	}
	for _, guardian := range *guardians {
		gPubKey, err := base64.StdEncoding.DecodeString(guardian)
		if err != nil {
			return err
		}
		if len(gPubKey) != constants.EdDSAPubKeyLength {
			return constants.ErrInvalidEDDSAPubKey
		}
	}

	block.Data, err = definition.ABIBridge.PackMethod(p.MethodName, guardians)
	return err
}
func (p *NominateGuardiansMethod) ReceiveBlock(context vm_context.AccountVmContext, sendBlock *nom.AccountBlock) ([]*nom.AccountBlock, error) {
	if err := p.ValidateSendBlock(sendBlock); err != nil {
		return nil, err
	}

	guardians := new([]string)
	err := definition.ABIBridge.UnpackMethod(guardians, p.MethodName, sendBlock.Data)
	if err != nil {
		return nil, err
	}

	bridgeInfo, err := definition.GetBridgeInfoVariable(context.Storage())
	if err != nil {
		return nil, err
	}

	senderPubKey := base64.StdEncoding.EncodeToString(sendBlock.PublicKey)
	if senderPubKey != bridgeInfo.AdministratorEDDSAPubKey {
		return nil, constants.ErrPermissionDenied
	}

	securityInfo, err := definition.GetSecurityInfoVariable(context.Storage())
	common.DealWithErr(err)

	sort.Strings(*guardians)
	// todo change

	// if len is 0 or arrays have diff length, we cannot have the same nominees
	sameNominees := len(securityInfo.NominatedGuardians) >= 4 && (len(securityInfo.NominatedGuardians) == len(*guardians))
	for idx, guardian := range securityInfo.NominatedGuardians {
		if guardian != (*guardians)[idx] {
			sameNominees = false
			break
		}
	}
	currentM, err := context.GetFrontierMomentum()
	common.DealWithErr(err)

	if sameNominees {
		if securityInfo.GuardiansNominationHeight+securityInfo.AdministratorDelay < currentM.Height {
			securityInfo.Guardians = make([]string, 0)
			securityInfo.GuardiansVotes = make([]string, 0)
			securityInfo.NominatedGuardians = make([]string, 0)
			for _, guardian := range *guardians {
				securityInfo.Guardians = append(securityInfo.Guardians, guardian)
				// append empty vote
				securityInfo.GuardiansVotes = append(securityInfo.GuardiansVotes, "")
			}
		}
	} else {
		securityInfo.GuardiansNominationHeight = currentM.Height
		securityInfo.NominatedGuardians = make([]string, 0)
		for _, guardian := range *guardians {
			securityInfo.NominatedGuardians = append(securityInfo.NominatedGuardians, guardian)
		}
	}
	common.DealWithErr(securityInfo.Save(context.Storage()))
	common.DealWithErr(bridgeInfo.Save(context.Storage()))
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

	pubKey := new(string)
	if err := definition.ABIBridge.UnpackMethod(pubKey, p.MethodName, block.Data); err != nil {
		return constants.ErrUnpackError
	}
	strPubKey, err := base64.StdEncoding.DecodeString(*pubKey)
	if err != nil {
		return constants.ErrInvalidB64Decode
	}
	if len(strPubKey) != constants.EdDSAPubKeyLength {
		return constants.ErrInvalidEDDSAPubKey
	}

	if block.Amount.Sign() != 0 {
		return constants.ErrInvalidTokenOrAmount
	}

	block.Data, err = definition.ABIBridge.PackMethod(p.MethodName, *pubKey)
	return err
}
func (p *ProposeAdministratorMethod) ReceiveBlock(context vm_context.AccountVmContext, sendBlock *nom.AccountBlock) ([]*nom.AccountBlock, error) {
	if err := p.ValidateSendBlock(sendBlock); err != nil {
		return nil, err
	}

	pubKey := new(string)
	if err := definition.ABIBridge.UnpackMethod(pubKey, p.MethodName, sendBlock.Data); err != nil {
		return nil, constants.ErrUnpackError
	}

	bridgeInfo, err := definition.GetBridgeInfoVariable(context.Storage())
	if err != nil {
		return nil, err
	}

	if len(bridgeInfo.AdministratorEDDSAPubKey) > 0 {
		return nil, constants.ErrNotEmergency
	}

	securityInfo, err := definition.GetSecurityInfoVariable(context.Storage())
	common.DealWithErr(err)

	found := false
	senderPubKey := base64.StdEncoding.EncodeToString(sendBlock.PublicKey)
	for idx, guardian := range securityInfo.Guardians {
		if guardian == senderPubKey {
			found = true
			securityInfo.GuardiansVotes[idx] = *pubKey
			break
		}
	}
	if !found {
		return nil, errors.New("sender is not a guardian")
	}

	votes := make(map[string]uint8)
	// todo change discuss
	threshold := uint8(len(securityInfo.Guardians) / 2)
	for _, vote := range securityInfo.GuardiansVotes {
		if len(vote) > 0 {
			votes[vote] += 1
			// we got a majority, so we change the administrator pub key
			if votes[vote] > threshold {
				bridgeInfo.AdministratorEDDSAPubKey = vote
				common.DealWithErr(bridgeInfo.Save(context.Storage()))
				for idx, _ := range securityInfo.GuardiansVotes {
					securityInfo.GuardiansVotes[idx] = ""
				}
				break
			}
		}
	}
	common.DealWithErr(securityInfo.Save(context.Storage()))
	return nil, nil
}

// todo implement method to change security tss delay, it should be bigger than the constants.min
