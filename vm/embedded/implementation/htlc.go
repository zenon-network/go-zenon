package implementation

import (
	"bytes"
	"encoding/hex"

	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/crypto"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/vm/constants"
	"github.com/zenon-network/go-zenon/vm/embedded/definition"
	"github.com/zenon-network/go-zenon/vm/vm_context"
)

var (
	htlcLog = common.EmbeddedLogger.New("contract", "htlc")
)

// GetHtlcProxyUnlockStatus reports whether the address accepts proxy
// unlocks: its HtlcProxyUnlockInfo entry when one exists, the default
// of true otherwise.
//
// Neither DenyProxyUnlock nor AllowProxyUnlock deletes the entry, so
// there are really 3 states: Default, ExplicitDeny and ExplicitAllow,
// and once an address has explicitly denied/allowed proxy unlock it
// can no longer go back to using the default. This ensures that if
// the default ever changes to deny, addresses that have called the
// Allow method will still work as expected.
func GetHtlcProxyUnlockStatus(context vm_context.AccountVmContext, address types.Address) (bool, error) {
	info, err := definition.GetHtlcProxyUnlockInfo(context.Storage(), address)
	if err != nil {
		if err == constants.ErrDataNonExistent {
			// This defines the default behavior to allow proxy unlocks
			return true, nil
		}
		return false, err
	} else {
		return info.Allowed, nil
	}
}

// checkHtlc validates the static fields of a CreateHtlcParam: the
// hash type must be definition.HashTypeSHA3 or
// definition.HashTypeSHA256 (constants.ErrInvalidHashType otherwise)
// and the hash lock must be exactly the type's digest length per
// definition.HashTypeDigestSizes (constants.ErrInvalidHashDigest
// otherwise).
func checkHtlc(param definition.CreateHtlcParam) error {

	if param.HashType != definition.HashTypeSHA3 && param.HashType != definition.HashTypeSHA256 {
		return constants.ErrInvalidHashType
	}

	if len(param.HashLock) != int(definition.HashTypeDigestSizes[param.HashType]) {
		return constants.ErrInvalidHashDigest
	}

	return nil
}

// CreateHtlcMethod (Create) locks the sent tokens in a new
// hashed-timelock entry: before the expiration time they can only be
// unlocked to the hash-locked counterparty by revealing the hash
// lock's preimage; at or after it the time-locked creator can reclaim
// them. The entry id is the hash of the creating send block.
type CreateHtlcMethod struct {
	MethodName string
}

// GetPlasma quotes the EmbeddedSimple tier; creating sends no
// response block.
func (p *CreateHtlcMethod) GetPlasma(plasmaTable *constants.PlasmaTable) (uint64, error) {
	return plasmaTable.EmbeddedSimple, nil
}

// ValidateSendBlock accepts a packed definition.CreateHtlcParam
// passing the checkHtlc rules, carried by a positive amount of any
// token; a zero amount fails with constants.ErrInvalidTokenOrAmount.
func (p *CreateHtlcMethod) ValidateSendBlock(block *nom.AccountBlock) error {
	var err error

	param := new(definition.CreateHtlcParam)

	if err := definition.ABIHtlc.UnpackMethod(param, p.MethodName, block.Data); err != nil {
		return constants.ErrUnpackError
	}

	if err = checkHtlc(*param); err != nil {
		return err
	}

	// can't create empty htlcs
	if block.Amount.Sign() == 0 {
		htlcLog.Debug("invalid create - cannot create zero amount", "address", block.Address)
		return constants.ErrInvalidTokenOrAmount
	}

	block.Data, err = definition.ABIHtlc.PackMethod(p.MethodName,
		param.HashLocked,
		param.ExpirationTime,
		param.HashType,
		param.KeyMaxSize,
		param.HashLock,
	)
	return err
}

// ReceiveBlock saves the definition.HtlcInfo entry keyed by the send
// block's hash, with the sender as TimeLocked and the sent amount
// held by the contract. The expiration time must be strictly after
// the frontier momentum's timestamp, else
// constants.ErrInvalidExpirationTime. No descendant blocks are
// emitted.
func (p *CreateHtlcMethod) ReceiveBlock(context vm_context.AccountVmContext, sendBlock *nom.AccountBlock) ([]*nom.AccountBlock, error) {
	if err := p.ValidateSendBlock(sendBlock); err != nil {
		htlcLog.Debug("invalid create - syntactic validation failed", "address", sendBlock.Address, "reason", err)
		return nil, err
	}

	param := new(definition.CreateHtlcParam)
	err := definition.ABIHtlc.UnpackMethod(param, p.MethodName, sendBlock.Data)
	common.DealWithErr(err)

	momentum, err := context.GetFrontierMomentum()
	common.DealWithErr(err)

	// can't create htlc that is already expired
	if momentum.Timestamp.Unix() >= param.ExpirationTime {
		htlcLog.Debug("invalid create - cannot create already expired", "address", sendBlock.Address, "time", momentum.Timestamp.Unix(), "expiration-time", param.ExpirationTime)
		return nil, constants.ErrInvalidExpirationTime
	}

	htlcInfo := &definition.HtlcInfo{
		Id:             sendBlock.Hash,
		TimeLocked:     sendBlock.Address,
		HashLocked:     param.HashLocked,
		TokenStandard:  sendBlock.TokenStandard,
		Amount:         sendBlock.Amount,
		ExpirationTime: param.ExpirationTime,
		HashType:       param.HashType,
		KeyMaxSize:     param.KeyMaxSize,
		HashLock:       param.HashLock,
	}

	common.DealWithErr(htlcInfo.Save(context.Storage()))
	htlcLog.Debug("created", "htlcInfo", htlcInfo)
	return nil, nil
}

// ReclaimHtlcMethod (Reclaim) returns an expired entry's tokens to
// its time-locked creator, the only address allowed to call it.
type ReclaimHtlcMethod struct {
	MethodName string
}

// GetPlasma quotes the EmbeddedWWithdraw tier, covering the one
// refund block the call sends back.
func (p *ReclaimHtlcMethod) GetPlasma(plasmaTable *constants.PlasmaTable) (uint64, error) {
	return plasmaTable.EmbeddedWWithdraw, nil
}

// ValidateSendBlock accepts a packed entry id (types.Hash) carrying
// no tokens; a positive amount fails with
// constants.ErrInvalidTokenOrAmount.
func (p *ReclaimHtlcMethod) ValidateSendBlock(block *nom.AccountBlock) error {
	var err error
	param := new(types.Hash)

	if err := definition.ABIHtlc.UnpackMethod(param, p.MethodName, block.Data); err != nil {
		return constants.ErrUnpackError
	}

	if block.Amount.Sign() > 0 {
		return constants.ErrInvalidTokenOrAmount
	}

	block.Data, err = definition.ABIHtlc.PackMethod(p.MethodName, param)
	return err
}

// ReceiveBlock deletes the entry and returns one descendant send
// refunding its amount to the time-locked creator. The entry must
// exist (constants.ErrDataNonExistent), the sender must be its
// TimeLocked address (constants.ErrPermissionDenied) and the frontier
// momentum's timestamp must be at or after the expiration time, else
// constants.ReclaimNotDue.
func (p *ReclaimHtlcMethod) ReceiveBlock(context vm_context.AccountVmContext, sendBlock *nom.AccountBlock) ([]*nom.AccountBlock, error) {
	if err := p.ValidateSendBlock(sendBlock); err != nil {
		htlcLog.Debug("invalid reclaim - syntactic validation failed", "address", sendBlock.Address, "reason", err)
		return nil, err
	}

	id := new(types.Hash)
	err := definition.ABIHtlc.UnpackMethod(id, p.MethodName, sendBlock.Data)
	common.DealWithErr(err)

	htlcInfo, err := definition.GetHtlcInfo(context.Storage(), *id)
	if err == constants.ErrDataNonExistent {
		htlcLog.Debug("invalid reclaim - entry does not exist", "id", id, "address", sendBlock.Address)
		return nil, err
	}
	common.DealWithErr(err)

	// only timelocked can reclaim
	if htlcInfo.TimeLocked != sendBlock.Address {
		htlcLog.Debug("invalid reclaim - permission denied", "id", htlcInfo.Id, "address", sendBlock.Address)
		return nil, constants.ErrPermissionDenied
	}

	momentum, err := context.GetFrontierMomentum()
	common.DealWithErr(err)

	// can only reclaim after the entry is expired
	if momentum.Timestamp.Unix() < htlcInfo.ExpirationTime {
		htlcLog.Debug("invalid reclaim - entry not expired", "id", htlcInfo.Id, "address", sendBlock.Address, "time", momentum.Timestamp.Unix(), "expiration-time", htlcInfo.ExpirationTime)
		return nil, constants.ReclaimNotDue
	}

	common.DealWithErr(htlcInfo.Delete(context.Storage()))
	htlcLog.Debug("reclaimed", "htlcInfo", htlcInfo)

	return []*nom.AccountBlock{
		{
			Address:       types.HtlcContract,
			ToAddress:     htlcInfo.TimeLocked,
			BlockType:     nom.BlockTypeContractSend,
			Amount:        htlcInfo.Amount,
			TokenStandard: htlcInfo.TokenStandard,
			Data:          []byte{},
		},
	}, nil
}

// UnlockHtlcMethod (Unlock) pays out an unexpired entry by revealing
// the hash lock's preimage. The tokens always go to the entry's
// HashLocked address regardless of who calls: by default any address
// may submit the preimage (a proxy unlock), but a HashLocked address
// that has called DenyProxyUnlock must unlock its entries itself.
type UnlockHtlcMethod struct {
	MethodName string
}

// GetPlasma quotes the EmbeddedWWithdraw tier, covering the one
// payout block the call sends back.
func (p *UnlockHtlcMethod) GetPlasma(plasmaTable *constants.PlasmaTable) (uint64, error) {
	return plasmaTable.EmbeddedWWithdraw, nil
}

// ValidateSendBlock accepts a packed definition.UnlockHtlcParam
// (entry id, preimage) carrying no tokens; a positive amount fails
// with constants.ErrInvalidTokenOrAmount.
func (p *UnlockHtlcMethod) ValidateSendBlock(block *nom.AccountBlock) error {
	var err error
	param := new(definition.UnlockHtlcParam)

	if err := definition.ABIHtlc.UnpackMethod(param, p.MethodName, block.Data); err != nil {
		return constants.ErrUnpackError
	}

	if block.Amount.Sign() > 0 {
		return constants.ErrInvalidTokenOrAmount
	}

	block.Data, err = definition.ABIHtlc.PackMethod(p.MethodName, param.Id, param.Preimage)
	return err
}

// ReceiveBlock deletes the entry and returns one descendant send
// paying its amount to the HashLocked address. The entry must exist
// (constants.ErrDataNonExistent); when the HashLocked address has
// denied proxy unlocks — see GetHtlcProxyUnlockStatus — only it may
// call, else constants.ErrPermissionDenied. The frontier momentum's
// timestamp must be strictly before the expiration time
// (constants.ErrExpired at or after it) and the preimage must be at
// most KeyMaxSize bytes with its HashType digest (SHA3-256 or
// SHA-256) equal to the entry's hash lock, else
// constants.ErrInvalidPreimage.
func (p *UnlockHtlcMethod) ReceiveBlock(context vm_context.AccountVmContext, sendBlock *nom.AccountBlock) ([]*nom.AccountBlock, error) {
	if err := p.ValidateSendBlock(sendBlock); err != nil {
		htlcLog.Debug("invalid unlock - syntactic validation failed", "address", sendBlock.Address, "reason", err)
		return nil, err
	}

	param := new(definition.UnlockHtlcParam)
	err := definition.ABIHtlc.UnpackMethod(param, p.MethodName, sendBlock.Data)
	common.DealWithErr(err)

	htlcInfo, err := definition.GetHtlcInfo(context.Storage(), param.Id)
	if err == constants.ErrDataNonExistent {
		htlcLog.Debug("invalid unlock - entry does not exist", "id", param.Id, "address", sendBlock.Address)
		return nil, err
	}
	common.DealWithErr(err)

	allowProxyUnlock, err := GetHtlcProxyUnlockStatus(context, htlcInfo.HashLocked)
	common.DealWithErr(err)
	// if proxy unlock is not allowed, only hashlocked can unlock
	if !allowProxyUnlock && sendBlock.Address != htlcInfo.HashLocked {
		htlcLog.Debug("invalid unlock - permission denied", "id", htlcInfo.Id, "address", sendBlock.Address)
		return nil, constants.ErrPermissionDenied
	}

	momentum, err := context.GetFrontierMomentum()
	common.DealWithErr(err)

	// can only unlock before expiration time
	if momentum.Timestamp.Unix() >= htlcInfo.ExpirationTime {
		htlcLog.Debug("invalid unlock - entry is expired", "id", htlcInfo.Id, "address", sendBlock.Address, "time", momentum.Timestamp.Unix(), "expiration-time", htlcInfo.ExpirationTime)
		return nil, constants.ErrExpired
	}

	if len(param.Preimage) > int(htlcInfo.KeyMaxSize) {
		htlcLog.Debug("invalid unlock - preimage size greater than entry KeyMaxSize", "id", htlcInfo.Id, "address", sendBlock.Address, "preimage-size", len(param.Preimage), "max-size", htlcInfo.KeyMaxSize)
		return nil, constants.ErrInvalidPreimage
	}

	var hashedPreimage []byte
	if htlcInfo.HashType == definition.HashTypeSHA3 {
		hashedPreimage = crypto.Hash(param.Preimage)
	} else if htlcInfo.HashType == definition.HashTypeSHA256 {
		hashedPreimage = crypto.HashSHA256(param.Preimage)
	} else {
		// shouldn't get here
	}

	if !bytes.Equal(hashedPreimage, htlcInfo.HashLock) {
		htlcLog.Debug("invalid unlock - wrong preimage", "id", htlcInfo.Id, "address", sendBlock.Address, "preimage", hex.EncodeToString(param.Preimage))
		return nil, constants.ErrInvalidPreimage
	}

	common.DealWithErr(htlcInfo.Delete(context.Storage()))
	htlcLog.Debug("unlocked", "htlcInfo", htlcInfo, "preimage", hex.EncodeToString(param.Preimage))

	return []*nom.AccountBlock{
		{
			Address:       types.HtlcContract,
			ToAddress:     htlcInfo.HashLocked,
			BlockType:     nom.BlockTypeContractSend,
			Amount:        htlcInfo.Amount,
			TokenStandard: htlcInfo.TokenStandard,
			Data:          []byte{},
		},
	}, nil
}

// DenyHtlcProxyUnlockMethod (DenyProxyUnlock) opts the sender out of
// proxy unlocks: entries hash-locked to it can then only be unlocked
// by the sender itself. The setting is permanent in the sense that
// the stored entry is never deleted — see GetHtlcProxyUnlockStatus —
// though it can be flipped back with AllowProxyUnlock.
type DenyHtlcProxyUnlockMethod struct {
	MethodName string
}

// GetPlasma quotes the EmbeddedSimple tier; the call sends no
// response block.
func (p *DenyHtlcProxyUnlockMethod) GetPlasma(plasmaTable *constants.PlasmaTable) (uint64, error) {
	return plasmaTable.EmbeddedSimple, nil
}

// ValidateSendBlock accepts an argument-less call carrying no
// tokens: extra ABI arguments fail with constants.ErrUnpackError and
// a non-zero Amount with constants.ErrInvalidTokenOrAmount.
func (p *DenyHtlcProxyUnlockMethod) ValidateSendBlock(block *nom.AccountBlock) error {
	var err error

	if err := definition.ABIHtlc.UnpackEmptyMethod(p.MethodName, block.Data); err != nil {
		return constants.ErrUnpackError
	}

	if block.Amount.Sign() != 0 {
		return constants.ErrInvalidTokenOrAmount
	}

	block.Data, err = definition.ABIHtlc.PackMethod(p.MethodName)
	return err
}

// ReceiveBlock saves the sender's definition.HtlcProxyUnlockInfo with
// Allowed set to false, overwriting any earlier choice. Past
// validation only a storage failure can make it error, and it emits
// no descendant blocks.
func (p *DenyHtlcProxyUnlockMethod) ReceiveBlock(context vm_context.AccountVmContext, sendBlock *nom.AccountBlock) ([]*nom.AccountBlock, error) {
	if err := p.ValidateSendBlock(sendBlock); err != nil {
		htlcLog.Debug("invalid create - syntactic validation failed", "address", sendBlock.Address, "reason", err)
		return nil, err
	}
	info := &definition.HtlcProxyUnlockInfo{
		Address: sendBlock.Address,
		Allowed: false,
	}
	if err := info.Save(context.Storage()); err != nil {
		return nil, err
	}
	htlcLog.Debug("deny proxy unlock", "address", sendBlock.Address)
	return nil, nil
}

// AllowHtlcProxyUnlockMethod (AllowProxyUnlock) opts the sender into
// proxy unlocks, matching the current default. Calling it records an
// explicit entry that survives any future change of the default —
// see GetHtlcProxyUnlockStatus.
type AllowHtlcProxyUnlockMethod struct {
	MethodName string
}

// GetPlasma quotes the EmbeddedSimple tier; the call sends no
// response block.
func (p *AllowHtlcProxyUnlockMethod) GetPlasma(plasmaTable *constants.PlasmaTable) (uint64, error) {
	return plasmaTable.EmbeddedSimple, nil
}

// ValidateSendBlock accepts an argument-less call carrying no
// tokens: extra ABI arguments fail with constants.ErrUnpackError and
// a non-zero Amount with constants.ErrInvalidTokenOrAmount.
func (p *AllowHtlcProxyUnlockMethod) ValidateSendBlock(block *nom.AccountBlock) error {
	var err error

	if err := definition.ABIHtlc.UnpackEmptyMethod(p.MethodName, block.Data); err != nil {
		return constants.ErrUnpackError
	}

	if block.Amount.Sign() != 0 {
		return constants.ErrInvalidTokenOrAmount
	}

	block.Data, err = definition.ABIHtlc.PackMethod(p.MethodName)
	return err
}

// ReceiveBlock saves the sender's definition.HtlcProxyUnlockInfo with
// Allowed set to true, overwriting any earlier choice. Past
// validation only a storage failure can make it error, and it emits
// no descendant blocks.
func (p *AllowHtlcProxyUnlockMethod) ReceiveBlock(context vm_context.AccountVmContext, sendBlock *nom.AccountBlock) ([]*nom.AccountBlock, error) {
	if err := p.ValidateSendBlock(sendBlock); err != nil {
		htlcLog.Debug("invalid create - syntactic validation failed", "address", sendBlock.Address, "reason", err)
		return nil, err
	}
	info := &definition.HtlcProxyUnlockInfo{
		Address: sendBlock.Address,
		Allowed: true,
	}
	if err := info.Save(context.Storage()); err != nil {
		return nil, err
	}
	htlcLog.Debug("allow proxy unlock", "address", sendBlock.Address)
	return nil, nil
}
