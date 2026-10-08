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

// The SEC 1 prefixes of a compressed point, by the parity of its y coordinate.
const (
	compressedPointEven = byte(0x02)
	compressedPointOdd  = byte(0x03)
)

// This embedded contract locks funds to a point on a curve until a time. Two
// point types lock to a public key and are opened by an ordinary ED25519 or
// BIP-340 signature over a domain-separated unlock message; one locks to a
// secp256k1 point and is opened by the scalar behind it. Which of the two
// shapes a swap uses, and how the secret moves between chains inside an
// adaptor signature, is the swap protocol's business, not the contract's.
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

// verifyPointScalar reports whether scalar is the discrete logarithm of the
// compressed point lock. The scalar must be canonical: 32 bytes, nonzero and
// below the group order, so that one secret has exactly one encoding and a
// witness seen on chain is the same bytes everywhere.
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
		// Compressed encoding only: two encodings of one point would be two
		// locks, and a point not on the curve can never be opened.
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

	// A point lock's witness binds nothing, so the entry has to.
	if param.PointType == definition.PointTypeSecp256k1Point && param.Destination.IsZero() {
		return constants.ErrInvalidDestination
	}
	// A fixed destination is where the funds will go, with no data; an
	// embedded contract cannot take such a send, and the funds would be lost.
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

// verifyPtlcWitness checks the witness of an unlock against the stored lock:
// a signature over the unlock message for the key types, the scalar for the
// point type. Every failure is one of the contract's own errors.
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
			// Stored-state validation already checks ED25519 point-lock length;
			// keep this mapping as defense in depth for direct verifier callers.
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

// verifyPtlcSignature is the name the key-type verifier had before the point
// type existed. Kept for the tests and tools that call it directly.
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

	// An entry with a fixed destination pays nowhere else, whoever asks.
	if !ptlcInfo.Destination.IsZero() && destination != ptlcInfo.Destination {
		ptlcLog.Debug("invalid unlock - wrong destination", "id", ptlcInfo.Id, "address", sendBlock.Address, "destination", destination, "expected", ptlcInfo.Destination)
		return nil, constants.ErrPermissionDenied
	}

	momentum, err := context.GetFrontierMomentum()
	common.DealWithErr(err)

	// can only unlock before expiration time
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
