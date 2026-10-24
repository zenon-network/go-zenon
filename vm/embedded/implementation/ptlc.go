package implementation

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"math/big"

	"filippo.io/edwards25519"
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

func isPayableDestination(address types.Address) bool {
	return address[0] == types.UserAddrByte && !address.IsZero()
}

func isPtlcWitnessSize(size int) bool {
	for _, witnessSize := range definition.PointTypeWitnessSizes {
		if size == int(witnessSize) {
			return true
		}
	}
	return false
}

func checkED25519PointLock(pointLock []byte) error {
	point, err := new(edwards25519.Point).SetBytes(pointLock)
	if err != nil {
		return constants.ErrInvalidPointLock
	}
	if point.MultByCofactor(point).Equal(edwards25519.NewIdentityPoint()) == 1 {
		return constants.ErrInvalidPointLock
	}
	return nil
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
	case definition.PointTypeED25519:
		if err := checkED25519PointLock(pointLock); err != nil {
			return err
		}
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
	if !param.Destination.IsZero() && !isPayableDestination(param.Destination) {
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
	case definition.PointTypeED25519, definition.PointTypeBIP340:
		unlockMessage := definition.GetPtlcUnlockMessage(chainIdentifier, ptlcInfo.PointType, id, destination)
		return verifyPointSignature(ptlcInfo.PointType, ptlcInfo.PointLock, unlockMessage, witness)
	case definition.PointTypeSecp256k1Point:
		return verifyPointScalar(ptlcInfo.PointLock, witness)
	}

	return constants.ErrInvalidPointType
}

func verifyPointSignature(pointType uint8, pointLock, message, signature []byte) error {
	switch pointType {
	case definition.PointTypeED25519:
		valid, err := wallet.VerifySignature(ed25519.PublicKey(pointLock), message, signature)
		if err != nil {
			return constants.ErrInvalidPointLock
		}
		if !valid {
			return constants.ErrInvalidPointSignature
		}
		return nil
	case definition.PointTypeBIP340:
		return verifyBIP340Signature(message, pointLock, signature)
	}
	return constants.ErrInvalidPointType
}

// isReclaimPointType reports whether a point type can be a reclaim lock:
// the signature types only, since a reclaim must carry a signature.
func isReclaimPointType(pointType uint8) bool {
	return pointType == definition.PointTypeED25519 || pointType == definition.PointTypeBIP340
}

// checkReclaimLock checks a reclaim lock is a signature point type and a
// valid point of that type.
func checkReclaimLock(pointType uint8, pointLock []byte) error {
	if !isReclaimPointType(pointType) {
		return constants.ErrInvalidPointType
	}
	return checkPointLock(pointType, pointLock)
}

// verifyPtlcReclaimSignature checks a reclaim signature against the
// entry's reclaim lock, re-validating the lock first.
func verifyPtlcReclaimSignature(ptlcInfo *definition.PtlcInfo, reclaimLock *definition.PtlcReclaimLock, chainIdentifier uint64, signature []byte) error {
	if err := checkReclaimLock(reclaimLock.PointType, reclaimLock.PointLock); err != nil {
		return err
	}
	if len(signature) != int(definition.PointTypeWitnessSizes[reclaimLock.PointType]) {
		return constants.ErrInvalidPointSignature
	}
	reclaimMessage := definition.GetPtlcReclaimMessage(chainIdentifier, reclaimLock.PointType, ptlcInfo)
	return verifyPointSignature(reclaimLock.PointType, reclaimLock.PointLock, reclaimMessage, signature)
}

// getPtlcReclaimLock returns the entry's reclaim lock, or nil if it has
// none.
func getPtlcReclaimLock(context vm_context.AccountVmContext, id types.Hash) *definition.PtlcReclaimLock {
	reclaimLock, err := definition.GetPtlcReclaimLock(context.Storage(), id)
	if err == constants.ErrDataNonExistent {
		return nil
	}
	common.DealWithErr(err)
	return reclaimLock
}

// deletePtlcReclaimLock removes the entry's reclaim lock if it has one.
func deletePtlcReclaimLock(context vm_context.AccountVmContext, id types.Hash) {
	if reclaimLock := getPtlcReclaimLock(context, id); reclaimLock != nil {
		common.DealWithErr(reclaimLock.Delete(context.Storage()))
	}
}

func verifyPtlcSignature(ptlcInfo *definition.PtlcInfo, chainIdentifier uint64, id types.Hash, destination types.Address, signature []byte) error {
	return verifyPtlcWitness(ptlcInfo, chainIdentifier, id, destination, signature)
}

// CreatePtlcMethod implements Create: it locks the tokens sent with the
// call behind a point until an expiration time, recording the entry
// under the send block's hash.
type CreatePtlcMethod struct {
	MethodName string
}

// GetPlasma prices the call as a simple embedded call.
func (p *CreatePtlcMethod) GetPlasma(plasmaTable *constants.PlasmaTable) (uint64, error) {
	return plasmaTable.EmbeddedSimple, nil
}

// ValidateSendBlock checks the point lock is a valid point of its type,
// the destination is payable when given and present for secp256k1
// points, and a positive amount is attached; it then repacks the data
// in canonical form.
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

// ReceiveBlock re-validates the send, requires the expiration time to be
// in the future of the confirming momentum, and stores the entry.
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

// ReclaimPtlcMethod implements Reclaim: the creator takes the locked
// tokens back once the entry has expired.
type ReclaimPtlcMethod struct {
	MethodName string
}

// GetPlasma prices the call as an embedded withdrawal.
func (p *ReclaimPtlcMethod) GetPlasma(plasmaTable *constants.PlasmaTable) (uint64, error) {
	return plasmaTable.EmbeddedWWithdraw, nil
}

// ValidateSendBlock checks the data is an entry id and no tokens are
// attached.
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

// ReceiveBlock requires the caller to be the entry's creator and the
// confirming momentum to be at or past the expiration time, then
// deletes the entry and sends the tokens back to the creator.
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

	if getPtlcReclaimLock(context, *id) != nil {
		ptlcLog.Debug("invalid reclaim - entry has a reclaim lock", "id", ptlcInfo.Id, "address", sendBlock.Address)
		return nil, constants.ErrReclaimSignatureRequired
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

	if !isPayableDestination(destination) {
		ptlcLog.Debug("invalid unlock - destination cannot be paid", "id", ptlcInfo.Id, "address", sendBlock.Address, "destination", destination)
		return nil, constants.ErrInvalidDestination
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
	deletePtlcReclaimLock(context, id)
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

// UnlockPtlcMethod implements Unlock: the caller claims the locked
// tokens by presenting a witness for the entry's point — a signature
// over the unlock message bound to its own address, or the scalar for a
// secp256k1 point.
type UnlockPtlcMethod struct {
	MethodName string
}

// GetPlasma prices the call as an embedded withdrawal.
func (p *UnlockPtlcMethod) GetPlasma(plasmaTable *constants.PlasmaTable) (uint64, error) {
	return plasmaTable.EmbeddedWWithdraw, nil
}

// ValidateSendBlock checks the data unpacks, no tokens are attached and
// the witness has a valid size.
func (p *UnlockPtlcMethod) ValidateSendBlock(block *nom.AccountBlock) error {
	var err error
	param := new(definition.UnlockPtlcParam)

	if err := definition.ABIPtlc.UnpackMethod(param, p.MethodName, block.Data); err != nil {
		return constants.ErrUnpackError
	}

	if !isZeroAmount(block.Amount) {
		return constants.ErrInvalidTokenOrAmount
	}

	if !isPtlcWitnessSize(len(param.Signature)) {
		return constants.ErrInvalidPointSignature
	}

	block.Data, err = definition.ABIPtlc.PackMethod(p.MethodName, param.Id, param.Signature)
	return err
}

// ReceiveBlock pays the entry to the caller if the entry has not
// expired, the caller matches a fixed destination when the entry has
// one, and the witness verifies for the entry's point type.
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

// ProxyUnlockPtlcMethod implements ProxyUnlock: anyone may claim the
// locked tokens for a named destination by presenting a witness for
// the entry's point (a signature over the unlock message bound to that
// destination, or the scalar for a secp256k1 point, whose entry must
// then name that destination), so the holder of the secret need not
// pay plasma themselves.
type ProxyUnlockPtlcMethod struct {
	MethodName string
}

// GetPlasma prices the call as an embedded withdrawal.
func (p *ProxyUnlockPtlcMethod) GetPlasma(plasmaTable *constants.PlasmaTable) (uint64, error) {
	return plasmaTable.EmbeddedWWithdraw, nil
}

// ValidateSendBlock checks the data unpacks, no tokens are attached,
// the destination is payable and the witness has a valid size.
func (p *ProxyUnlockPtlcMethod) ValidateSendBlock(block *nom.AccountBlock) error {
	var err error
	param := new(definition.ProxyUnlockPtlcParam)

	if err := definition.ABIPtlc.UnpackMethod(param, p.MethodName, block.Data); err != nil {
		return constants.ErrUnpackError
	}

	if !isZeroAmount(block.Amount) {
		return constants.ErrInvalidTokenOrAmount
	}

	if !isPayableDestination(param.Destination) {
		return constants.ErrInvalidDestination
	}

	if !isPtlcWitnessSize(len(param.Signature)) {
		return constants.ErrInvalidPointSignature
	}

	block.Data, err = definition.ABIPtlc.PackMethod(p.MethodName, param.Id, param.Destination, param.Signature)
	return err
}

// ReceiveBlock pays the entry to the given destination under the same
// rules as Unlock, with the witness verified against that destination.
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

// CreateWithReclaimLockPtlcMethod implements CreateWithReclaimLock: it
// locks the tokens like Create, and stores a reclaim lock beside the
// entry, so that the creator can reclaim only with a signature under it.
type CreateWithReclaimLockPtlcMethod struct {
	MethodName string
}

// GetPlasma prices the call as a simple embedded call, like Create.
func (p *CreateWithReclaimLockPtlcMethod) GetPlasma(plasmaTable *constants.PlasmaTable) (uint64, error) {
	return plasmaTable.EmbeddedSimple, nil
}

// ValidateSendBlock checks the entry as Create does, checks the reclaim
// lock is an Ed25519 or BIP-340 key, and repacks the data in canonical
// form.
func (p *CreateWithReclaimLockPtlcMethod) ValidateSendBlock(block *nom.AccountBlock) error {
	var err error

	param := new(definition.CreateWithReclaimLockPtlcParam)

	if err := definition.ABIPtlc.UnpackMethod(param, p.MethodName, block.Data); err != nil {
		return constants.ErrUnpackError
	}

	if err = checkPtlc(definition.CreatePtlcParam{
		ExpirationTime: param.ExpirationTime,
		PointType:      param.PointType,
		PointLock:      param.PointLock,
		Destination:    param.Destination,
	}); err != nil {
		return err
	}

	if err = checkReclaimLock(param.ReclaimPointType, param.ReclaimPointLock); err != nil {
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
		param.ReclaimPointType,
		param.ReclaimPointLock,
	)
	return err
}

// ReceiveBlock re-validates the send, requires the expiration time to be
// in the future of the confirming momentum, and stores the entry and its
// reclaim lock.
func (p *CreateWithReclaimLockPtlcMethod) ReceiveBlock(context vm_context.AccountVmContext, sendBlock *nom.AccountBlock) ([]*nom.AccountBlock, error) {
	if err := p.ValidateSendBlock(sendBlock); err != nil {
		ptlcLog.Debug("invalid create - syntactic validation failed", "address", sendBlock.Address, "reason", err)
		return nil, err
	}

	param := new(definition.CreateWithReclaimLockPtlcParam)
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
	reclaimLock := &definition.PtlcReclaimLock{
		Id:        sendBlock.Hash,
		PointType: param.ReclaimPointType,
		PointLock: param.ReclaimPointLock,
	}

	common.DealWithErr(ptlcInfo.Save(context.Storage()))
	common.DealWithErr(reclaimLock.Save(context.Storage()))
	ptlcLog.Debug("created", "ptlcInfo", ptlcInfo, "reclaimLock", reclaimLock)
	return nil, nil
}

// ReclaimWithSignaturePtlcMethod implements ReclaimWithSignature: the
// creator takes the locked tokens of an entry with a reclaim lock back once
// it has expired, publishing a signature under that lock over the entry's
// reclaim message.
type ReclaimWithSignaturePtlcMethod struct {
	MethodName string
}

// GetPlasma prices the call as an embedded withdrawal, like Reclaim.
func (p *ReclaimWithSignaturePtlcMethod) GetPlasma(plasmaTable *constants.PlasmaTable) (uint64, error) {
	return plasmaTable.EmbeddedWWithdraw, nil
}

// ValidateSendBlock checks no tokens are attached and the signature has the
// reclaim signature size, and repacks the data in canonical form.
func (p *ReclaimWithSignaturePtlcMethod) ValidateSendBlock(block *nom.AccountBlock) error {
	var err error
	param := new(definition.ReclaimWithSignaturePtlcParam)

	if err := definition.ABIPtlc.UnpackMethod(param, p.MethodName, block.Data); err != nil {
		return constants.ErrUnpackError
	}

	if !isZeroAmount(block.Amount) {
		return constants.ErrInvalidTokenOrAmount
	}

	if len(param.Signature) != definition.PtlcReclaimSignatureSize {
		return constants.ErrInvalidPointSignature
	}

	block.Data, err = definition.ABIPtlc.PackMethod(p.MethodName, param.Id, param.Signature)
	return err
}

// ReceiveBlock re-validates the send and pays the tokens back to the
// creator when the entry exists, is sane, was created by the caller, has a
// reclaim lock and has expired at the confirming momentum, and the
// signature verifies; the entry and its lock are deleted first.
func (p *ReclaimWithSignaturePtlcMethod) ReceiveBlock(context vm_context.AccountVmContext, sendBlock *nom.AccountBlock) ([]*nom.AccountBlock, error) {
	if err := p.ValidateSendBlock(sendBlock); err != nil {
		ptlcLog.Debug("invalid reclaim - syntactic validation failed", "address", sendBlock.Address, "reason", err)
		return nil, err
	}

	param := new(definition.ReclaimWithSignaturePtlcParam)
	err := definition.ABIPtlc.UnpackMethod(param, p.MethodName, sendBlock.Data)
	common.DealWithErr(err)

	ptlcInfo, err := definition.GetPtlcInfo(context.Storage(), param.Id)
	if err == constants.ErrDataNonExistent {
		ptlcLog.Debug("invalid reclaim - entry does not exist", "id", param.Id, "address", sendBlock.Address)
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

	reclaimLock := getPtlcReclaimLock(context, param.Id)
	if reclaimLock == nil {
		ptlcLog.Debug("invalid reclaim - entry has no reclaim lock", "id", ptlcInfo.Id, "address", sendBlock.Address)
		return nil, constants.ErrNoReclaimLock
	}

	momentum, err := context.GetFrontierMomentum()
	common.DealWithErr(err)

	if momentum.Timestamp.Unix() < ptlcInfo.ExpirationTime {
		ptlcLog.Debug("invalid reclaim - entry not expired", "id", ptlcInfo.Id, "address", sendBlock.Address, "time", momentum.Timestamp.Unix(), "expiration-time", ptlcInfo.ExpirationTime)
		return nil, constants.ReclaimNotDue
	}

	if err := verifyPtlcReclaimSignature(ptlcInfo, reclaimLock, momentum.ChainIdentifier, param.Signature); err != nil {
		ptlcLog.Debug("invalid reclaim - invalid signature", "id", ptlcInfo.Id, "address", sendBlock.Address, "signature-hash", signatureHashForLog(param.Signature), "reason", err)
		return nil, err
	}

	common.DealWithErr(ptlcInfo.Delete(context.Storage()))
	common.DealWithErr(reclaimLock.Delete(context.Storage()))
	ptlcLog.Debug("reclaimed", "ptlcInfo", ptlcInfo, "reclaimLock", reclaimLock, "signature-hash", signatureHashForLog(param.Signature))

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
