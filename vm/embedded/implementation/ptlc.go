package implementation

import (
	"crypto/ed25519"
	"encoding/base64"

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

func checkPtlc(param definition.CreatePtlcParam) error {

	if param.PointType != definition.PointTypeED25519 && param.PointType != definition.PointTypeBIP340 {
		return constants.ErrInvalidPointType
	}

	if len(param.PointLock) != int(definition.PointTypePubKeySizes[param.PointType]) {
		return constants.ErrInvalidPointLock
	}

	return nil
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

	// can't create empty ptlcs
	if block.Amount.Sign() == 0 {
		ptlcLog.Debug("invalid create - cannot create zero amount", "address", block.Address)
		return constants.ErrInvalidTokenOrAmount
	}

	block.Data, err = definition.ABIPtlc.PackMethod(p.MethodName,
		param.ExpirationTime,
		param.PointType,
		param.PointLock,
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

	// can't create ptlc that is already expired
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

	if block.Amount.Sign() > 0 {
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

	// only timelocked can reclaim
	if ptlcInfo.TimeLocked != sendBlock.Address {
		ptlcLog.Debug("invalid reclaim - permission denied", "id", ptlcInfo.Id, "address", sendBlock.Address)
		return nil, constants.ErrPermissionDenied
	}

	momentum, err := context.GetFrontierMomentum()
	common.DealWithErr(err)

	// can only reclaim after the entry is expired
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

// helper for Unlock and ProxyUnlock
func unlockPtlc(context vm_context.AccountVmContext, sendBlock *nom.AccountBlock, id types.Hash, destination types.Address, signature []byte) ([]*nom.AccountBlock, error) {
	ptlcInfo, err := definition.GetPtlcInfo(context.Storage(), id)
	if err == constants.ErrDataNonExistent {
		ptlcLog.Debug("invalid unlock - entry does not exist", "id", id, "address", sendBlock.Address)
		return nil, err
	}
	common.DealWithErr(err)

	momentum, err := context.GetFrontierMomentum()
	common.DealWithErr(err)

	// can only unlock before expiration time
	if momentum.Timestamp.Unix() >= ptlcInfo.ExpirationTime {
		ptlcLog.Debug("invalid unlock - entry is expired", "id", ptlcInfo.Id, "address", sendBlock.Address, "time", momentum.Timestamp.Unix(), "expiration-time", ptlcInfo.ExpirationTime)
		return nil, constants.ErrExpired
	}

	// signature must be right size for the type
	if len(signature) != int(definition.PointTypeSignatureSizes[ptlcInfo.PointType]) {
		ptlcLog.Debug("invalid unlock - signature is wrong size", "id", ptlcInfo.Id, "address", sendBlock.Address, "received-size", len(signature), "expected-size", definition.PointTypeSignatureSizes[ptlcInfo.PointType])
		return nil, constants.ErrInvalidPointSignature
	}

	unlockMessage := crypto.Hash(common.JoinBytes(id.Bytes(), destination.Bytes()))
	if ptlcInfo.PointType == definition.PointTypeED25519 {
		valid, err := wallet.VerifySignature(ed25519.PublicKey(ptlcInfo.PointLock), unlockMessage, signature)
		if err != nil {
			return nil, err
		}
		if !valid {
			ptlcLog.Debug("invalid unlock - invalid signature", "id", ptlcInfo.Id, "address", sendBlock.Address, "destination", destination, "signature", base64.StdEncoding.EncodeToString(signature))
			return nil, constants.ErrInvalidPointSignature
		}
	} else if ptlcInfo.PointType == definition.PointTypeBIP340 {
		s, err := schnorr.ParseSignature(signature)
		if err != nil {
			return nil, err
		}
		pk, err := schnorr.ParsePubKey(ptlcInfo.PointLock)
		if err != nil {
			return nil, err
		}
		valid := s.Verify(unlockMessage, pk)
		if !valid {
			ptlcLog.Debug("invalid unlock - invalid signature", "id", ptlcInfo.Id, "address", sendBlock.Address, "destination", destination, "signature", base64.StdEncoding.EncodeToString(signature))
			return nil, constants.ErrInvalidPointSignature
		}
	} else {
		// shouldn't get here
	}

	common.DealWithErr(ptlcInfo.Delete(context.Storage()))
	ptlcLog.Debug("unlocked", "ptlcInfo", ptlcInfo, "destination", destination, "signature", base64.StdEncoding.EncodeToString(signature))

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

	if block.Amount.Sign() > 0 {
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

// exact same as unlock but takes in an extra Destination param
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

	if block.Amount.Sign() > 0 {
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
