package implementation

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"math/big"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/btcec/v2/schnorr"
	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/crypto"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/vm/constants"
	"github.com/zenon-network/go-zenon/vm/embedded/definition"
	"github.com/zenon-network/go-zenon/vm/vm_context"
	"github.com/zenon-network/go-zenon/wallet"
)

var (
	ptlcLog = common.EmbeddedLogger.New("contract", "ptlc")
)

const (
	compressedPointEven = byte(0x02)
	compressedPointOdd  = byte(0x03)
)

func isPositiveAmount(amount *big.Int) bool {
	return amount != nil && amount.Sign() > 0
}

func isZeroAmount(amount *big.Int) bool {
	return amount != nil && amount.Sign() == 0
}

func signatureHashForLog(signature []byte) string {
	return base64.StdEncoding.EncodeToString(crypto.Hash(signature))
}

func isEmbeddedDestination(address types.Address) bool {
	for _, contract := range types.EmbeddedContracts {
		if address == contract {
			return true
		}
	}
	return false
}

func verifyBIP340Signature(message, pointLock, signature []byte) error {
	s, err := schnorr.ParseSignature(signature)
	if err != nil {
		return constants.ErrInvalidPointSignature
	}
	pk, err := schnorr.ParsePubKey(pointLock)
	if err != nil {
		return constants.ErrInvalidPointLock
	}
	if !s.Verify(message, pk) {
		return constants.ErrInvalidPointSignature
	}
	return nil
}

func verifyPointScalar(pointLock, scalar []byte) error {
	if len(scalar) != int(definition.PointTypeWitnessSizes[definition.PointTypeSecp256k1Point]) {
		return constants.ErrInvalidPointScalar
	}
	var k btcec.ModNScalar
	if overflow := k.SetByteSlice(scalar); overflow || k.IsZero() {
		return constants.ErrInvalidPointScalar
	}
	var point btcec.JacobianPoint
	btcec.ScalarBaseMultNonConst(&k, &point)
	point.ToAffine()
	if !bytes.Equal(btcec.NewPublicKey(&point.X, &point.Y).SerializeCompressed(), pointLock) {
		return constants.ErrInvalidPointScalar
	}
	return nil
}

func checkPointLock(pointType uint8, pointLock []byte) error {
	size, ok := definition.PointTypePubKeySizes[pointType]
	if !ok {
		return constants.ErrInvalidPointType
	}
	if len(pointLock) != int(size) {
		return constants.ErrInvalidPointLock
	}

	switch pointType {
	case definition.PointTypeBIP340:
		if _, err := schnorr.ParsePubKey(pointLock); err != nil {
			return constants.ErrInvalidPointLock
		}
	case definition.PointTypeSecp256k1Point:
		if pointLock[0] != compressedPointEven && pointLock[0] != compressedPointOdd {
			return constants.ErrInvalidPointLock
		}
		if _, err := btcec.ParsePubKey(pointLock); err != nil {
			return constants.ErrInvalidPointLock
		}
	}
	return nil
}

func checkPtlc(param definition.CreatePtlcParam) error {
	if err := checkPointLock(param.PointType, param.PointLock); err != nil {
		return err
	}

	if param.PointType == definition.PointTypeSecp256k1Point && param.Destination.IsZero() {
		return constants.ErrInvalidDestination
	}
	if !param.Destination.IsZero() && isEmbeddedDestination(param.Destination) {
		return constants.ErrInvalidDestination
	}

	return nil
}

func checkReclaimablePtlcInfo(ptlcInfo *definition.PtlcInfo) error {
	if ptlcInfo == nil {
		return constants.ErrDataNonExistent
	}

	if ptlcInfo.Amount == nil || ptlcInfo.Amount.Sign() <= 0 {
		return constants.ErrInvalidTokenOrAmount
	}

	if ptlcInfo.ExpirationTime <= 0 {
		return constants.ErrInvalidExpirationTime
	}

	return nil
}

func checkStoredPtlcInfo(ptlcInfo *definition.PtlcInfo) error {
	if err := checkReclaimablePtlcInfo(ptlcInfo); err != nil {
		return err
	}

	if err := checkPtlc(definition.CreatePtlcParam{
		ExpirationTime: ptlcInfo.ExpirationTime,
		PointType:      ptlcInfo.PointType,
		PointLock:      ptlcInfo.PointLock,
		Destination:    ptlcInfo.Destination,
	}); err != nil {
		return err
	}

	return nil
}

func verifyPtlcWitness(ptlcInfo *definition.PtlcInfo, chainIdentifier uint64, id types.Hash, destination types.Address, witness []byte) error {
	witnessSize, ok := definition.PointTypeWitnessSizes[ptlcInfo.PointType]
	if !ok {
		return constants.ErrInvalidPointType
	}

	if len(witness) != int(witnessSize) {
		ptlcLog.Debug("invalid unlock - witness is wrong size", "id", ptlcInfo.Id, "received-size", len(witness), "expected-size", witnessSize)
		if ptlcInfo.PointType == definition.PointTypeSecp256k1Point {
			return constants.ErrInvalidPointScalar
		}
		return constants.ErrInvalidPointSignature
	}

	switch ptlcInfo.PointType {
	case definition.PointTypeED25519:
		unlockMessage := definition.GetPtlcUnlockMessage(chainIdentifier, ptlcInfo.PointType, id, destination)
		valid, err := wallet.VerifySignature(ed25519.PublicKey(ptlcInfo.PointLock), unlockMessage, witness)
		if err != nil {
			return constants.ErrInvalidPointLock
		}
		if !valid {
			return constants.ErrInvalidPointSignature
		}
		return nil
	case definition.PointTypeBIP340:
		unlockMessage := definition.GetPtlcUnlockMessage(chainIdentifier, ptlcInfo.PointType, id, destination)
		return verifyBIP340Signature(unlockMessage, ptlcInfo.PointLock, witness)
	case definition.PointTypeSecp256k1Point:
		return verifyPointScalar(ptlcInfo.PointLock, witness)
	}

	return constants.ErrInvalidPointType
}

func verifyPtlcSignature(ptlcInfo *definition.PtlcInfo, chainIdentifier uint64, id types.Hash, destination types.Address, signature []byte) error {
	return verifyPtlcWitness(ptlcInfo, chainIdentifier, id, destination, signature)
}

type CreatePtlcMethod struct {
	MethodName string
}

func (p *CreatePtlcMethod) GetPlasma(plasmaTable *constants.PlasmaTable) (uint64, error) {
	return plasmaTable.EmbeddedSimple, nil
}
func (p *CreatePtlcMethod) ValidateSendBlock(block *nom.AccountBlock) error {
	var err error

	param := new(definition.CreatePtlcParam)

	if err := definition.ABIPtlc.UnpackMethod(param, p.MethodName, block.Data); err != nil {
		return constants.ErrUnpackError
	}

	if err = checkPtlc(*param); err != nil {
		return err
	}

	if !isPositiveAmount(block.Amount) {
		ptlcLog.Debug("invalid create - amount must be positive", "address", block.Address)
		return constants.ErrInvalidTokenOrAmount
	}

	block.Data, err = definition.ABIPtlc.PackMethod(p.MethodName,
		param.ExpirationTime,
		param.PointType,
		param.PointLock,
		param.Destination,
	)
	return err
}
func (p *CreatePtlcMethod) ReceiveBlock(context vm_context.AccountVmContext, sendBlock *nom.AccountBlock) ([]*nom.AccountBlock, error) {
	if err := p.ValidateSendBlock(sendBlock); err != nil {
		ptlcLog.Debug("invalid create - syntactic validation failed", "address", sendBlock.Address, "reason", err)
		return nil, err
	}

	param := new(definition.CreatePtlcParam)
	err := definition.ABIPtlc.UnpackMethod(param, p.MethodName, sendBlock.Data)
	common.DealWithErr(err)

	momentum, err := context.GetFrontierMomentum()
	common.DealWithErr(err)

	if momentum.Timestamp.Unix() >= param.ExpirationTime {
		ptlcLog.Debug("invalid create - cannot create already expired", "address", sendBlock.Address, "time", momentum.Timestamp.Unix(), "expiration-time", param.ExpirationTime)
		return nil, constants.ErrInvalidExpirationTime
	}

	ptlcInfo := &definition.PtlcInfo{
		Id:             sendBlock.Hash,
		TimeLocked:     sendBlock.Address,
		TokenStandard:  sendBlock.TokenStandard,
		Amount:         sendBlock.Amount,
		ExpirationTime: param.ExpirationTime,
		PointType:      param.PointType,
		PointLock:      param.PointLock,
		Destination:    param.Destination,
	}

	common.DealWithErr(ptlcInfo.Save(context.Storage()))
	ptlcLog.Debug("created", "ptlcInfo", ptlcInfo)
	return nil, nil
}

type ReclaimPtlcMethod struct {
	MethodName string
}

func (p *ReclaimPtlcMethod) GetPlasma(plasmaTable *constants.PlasmaTable) (uint64, error) {
	return plasmaTable.EmbeddedWWithdraw, nil
}
func (p *ReclaimPtlcMethod) ValidateSendBlock(block *nom.AccountBlock) error {
	var err error
	param := new(types.Hash)

	if err := definition.ABIPtlc.UnpackMethod(param, p.MethodName, block.Data); err != nil {
		return constants.ErrUnpackError
	}

	if !isZeroAmount(block.Amount) {
		return constants.ErrInvalidTokenOrAmount
	}

	block.Data, err = definition.ABIPtlc.PackMethod(p.MethodName, param)
	return err
}
func (p *ReclaimPtlcMethod) ReceiveBlock(context vm_context.AccountVmContext, sendBlock *nom.AccountBlock) ([]*nom.AccountBlock, error) {
	if err := p.ValidateSendBlock(sendBlock); err != nil {
		ptlcLog.Debug("invalid reclaim - syntactic validation failed", "address", sendBlock.Address, "reason", err)
		return nil, err
	}

	id := new(types.Hash)
	err := definition.ABIPtlc.UnpackMethod(id, p.MethodName, sendBlock.Data)
	common.DealWithErr(err)

	ptlcInfo, err := definition.GetPtlcInfo(context.Storage(), *id)
	if err == constants.ErrDataNonExistent {
		ptlcLog.Debug("invalid reclaim - entry does not exist", "id", id, "address", sendBlock.Address)
		return nil, err
	}
	common.DealWithErr(err)

	if err := checkReclaimablePtlcInfo(ptlcInfo); err != nil {
		ptlcLog.Debug("invalid reclaim - corrupt entry", "id", ptlcInfo.Id, "address", sendBlock.Address, "reason", err)
		return nil, err
	}

	if ptlcInfo.TimeLocked != sendBlock.Address {
		ptlcLog.Debug("invalid reclaim - permission denied", "id", ptlcInfo.Id, "address", sendBlock.Address)
		return nil, constants.ErrPermissionDenied
	}

	momentum, err := context.GetFrontierMomentum()
	common.DealWithErr(err)

	if momentum.Timestamp.Unix() < ptlcInfo.ExpirationTime {
		ptlcLog.Debug("invalid reclaim - entry not expired", "id", ptlcInfo.Id, "address", sendBlock.Address, "time", momentum.Timestamp.Unix(), "expiration-time", ptlcInfo.ExpirationTime)
		return nil, constants.ReclaimNotDue
	}

	common.DealWithErr(ptlcInfo.Delete(context.Storage()))
	ptlcLog.Debug("reclaimed", "ptlcInfo", ptlcInfo)

	return []*nom.AccountBlock{
		{
			Address:       types.PtlcContract,
			ToAddress:     ptlcInfo.TimeLocked,
			BlockType:     nom.BlockTypeContractSend,
			Amount:        ptlcInfo.Amount,
			TokenStandard: ptlcInfo.TokenStandard,
			Data:          []byte{},
		},
	}, nil
}

func unlockPtlc(context vm_context.AccountVmContext, sendBlock *nom.AccountBlock, id types.Hash, destination types.Address, witness []byte) ([]*nom.AccountBlock, error) {
	ptlcInfo, err := definition.GetPtlcInfo(context.Storage(), id)
	if err == constants.ErrDataNonExistent {
		ptlcLog.Debug("invalid unlock - entry does not exist", "id", id, "address", sendBlock.Address)
		return nil, err
	}
	common.DealWithErr(err)

	if err := checkStoredPtlcInfo(ptlcInfo); err != nil {
		ptlcLog.Debug("invalid unlock - corrupt entry", "id", ptlcInfo.Id, "address", sendBlock.Address, "reason", err)
		return nil, err
	}

	if !ptlcInfo.Destination.IsZero() && destination != ptlcInfo.Destination {
		ptlcLog.Debug("invalid unlock - wrong destination", "id", ptlcInfo.Id, "address", sendBlock.Address, "destination", destination, "expected", ptlcInfo.Destination)
		return nil, constants.ErrPermissionDenied
	}

	momentum, err := context.GetFrontierMomentum()
	common.DealWithErr(err)

	if momentum.Timestamp.Unix() >= ptlcInfo.ExpirationTime {
		ptlcLog.Debug("invalid unlock - entry is expired", "id", ptlcInfo.Id, "address", sendBlock.Address, "time", momentum.Timestamp.Unix(), "expiration-time", ptlcInfo.ExpirationTime)
		return nil, constants.ErrExpired
	}

	if err := verifyPtlcWitness(ptlcInfo, momentum.ChainIdentifier, id, destination, witness); err != nil {
		ptlcLog.Debug("invalid unlock - invalid witness", "id", ptlcInfo.Id, "address", sendBlock.Address, "destination", destination, "witness-hash", signatureHashForLog(witness), "reason", err)
		return nil, err
	}

	common.DealWithErr(ptlcInfo.Delete(context.Storage()))
	ptlcLog.Debug("unlocked", "ptlcInfo", ptlcInfo, "destination", destination, "witness-hash", signatureHashForLog(witness))

	return []*nom.AccountBlock{
		{
			Address:       types.PtlcContract,
			ToAddress:     destination,
			BlockType:     nom.BlockTypeContractSend,
			Amount:        ptlcInfo.Amount,
			TokenStandard: ptlcInfo.TokenStandard,
			Data:          []byte{},
		},
	}, nil
}

type UnlockPtlcMethod struct {
	MethodName string
}

func (p *UnlockPtlcMethod) GetPlasma(plasmaTable *constants.PlasmaTable) (uint64, error) {
	return plasmaTable.EmbeddedWWithdraw, nil
}
func (p *UnlockPtlcMethod) ValidateSendBlock(block *nom.AccountBlock) error {
	var err error
	param := new(definition.UnlockPtlcParam)

	if err := definition.ABIPtlc.UnpackMethod(param, p.MethodName, block.Data); err != nil {
		return constants.ErrUnpackError
	}

	if !isZeroAmount(block.Amount) {
		return constants.ErrInvalidTokenOrAmount
	}

	block.Data, err = definition.ABIPtlc.PackMethod(p.MethodName, param.Id, param.Signature)
	return err
}
func (p *UnlockPtlcMethod) ReceiveBlock(context vm_context.AccountVmContext, sendBlock *nom.AccountBlock) ([]*nom.AccountBlock, error) {
	if err := p.ValidateSendBlock(sendBlock); err != nil {
		ptlcLog.Debug("invalid unlock - syntactic validation failed", "address", sendBlock.Address, "reason", err)
		return nil, err
	}

	param := new(definition.UnlockPtlcParam)
	err := definition.ABIPtlc.UnpackMethod(param, p.MethodName, sendBlock.Data)
	common.DealWithErr(err)

	return unlockPtlc(context, sendBlock, param.Id, sendBlock.Address, param.Signature)

}

type ProxyUnlockPtlcMethod struct {
	MethodName string
}

func (p *ProxyUnlockPtlcMethod) GetPlasma(plasmaTable *constants.PlasmaTable) (uint64, error) {
	return plasmaTable.EmbeddedWWithdraw, nil
}
func (p *ProxyUnlockPtlcMethod) ValidateSendBlock(block *nom.AccountBlock) error {
	var err error
	param := new(definition.ProxyUnlockPtlcParam)

	if err := definition.ABIPtlc.UnpackMethod(param, p.MethodName, block.Data); err != nil {
		return constants.ErrUnpackError
	}

	if !isZeroAmount(block.Amount) {
		return constants.ErrInvalidTokenOrAmount
	}

	block.Data, err = definition.ABIPtlc.PackMethod(p.MethodName, param.Id, param.Destination, param.Signature)
	return err
}
func (p *ProxyUnlockPtlcMethod) ReceiveBlock(context vm_context.AccountVmContext, sendBlock *nom.AccountBlock) ([]*nom.AccountBlock, error) {
	if err := p.ValidateSendBlock(sendBlock); err != nil {
		ptlcLog.Debug("invalid unlock - syntactic validation failed", "address", sendBlock.Address, "reason", err)
		return nil, err
	}

	param := new(definition.ProxyUnlockPtlcParam)
	err := definition.ABIPtlc.UnpackMethod(param, p.MethodName, sendBlock.Data)
	common.DealWithErr(err)

	return unlockPtlc(context, sendBlock, param.Id, param.Destination, param.Signature)
}
