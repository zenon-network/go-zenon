package constants

import "github.com/pkg/errors"

var (
	ErrVmRunPanic = errors.New("supervisor - VM panic")

	// Common
	ErrNothingToWithdraw      = errors.New("nothing to withdraw")
	ErrNotEnoughDepositedQsr  = errors.New("not enough deposited Qsr")
	ErrInvalidTokenOrAmount   = errors.New("invalid token or amount")
	ErrNotContractAddress     = errors.New("not a contract address")
	ErrContractDoesntExist    = errors.New("contract doesn't exist")
	ErrContractMethodNotFound = errors.New("method not found in the abi")
	ErrDataNonExistent        = errors.New("data non existent")
	ErrUnpackError            = errors.New("invalid unpack method data")
	ErrInsufficientBalance    = errors.New("insufficient balance for transfer")
	ErrPermissionDenied       = errors.New("address cannot call this method")
	ErrInvalidB64Decode       = errors.New("invalid b64 decode")
	ErrForbiddenParam         = errors.New("forbidden parameter")
	ErrNotEnoughSlots         = errors.New("not enough slots left")

	// Common - update contract state
	ErrUpdateTooRecent      = errors.New("last update was too recent")
	ErrEpochUpdateTooRecent = errors.New("epoch update was too recent")

	// Pillar
	ErrInvalidName = errors.New("invalid name")
	ErrNotUnique   = errors.New("name or producing address not unique")
	ErrNotActive   = errors.New("pillar is not active")

	// Token
	ErrIDNotUnique        = errors.New("there is another token with the same id")
	ErrTokenInvalidText   = errors.New("invalid token name/symbol/domain/decimals")
	ErrTokenInvalidAmount = errors.New("invalid token total/max supply")

	// Stake
	RevokeNotDue            = errors.New("staking period still active")
	ErrInvalidStakingPeriod = errors.New("invalid staking period")

	// Plasma
	ErrBlockPlasmaLimitReached = errors.New("plasma limit for account-block reached")
	ErrNotEnoughPlasma         = errors.New("not enough plasma on account")
	ErrNotEnoughTotalPlasma    = errors.New("not enough TotalPlasma provided for account-block (PoW + Fused)")

	// Swap
	ErrInvalidSwapCode  = errors.New("invalid swap code")
	ErrInvalidSignature = errors.New("invalid secp256k1 signature")

	// Sentinel
	ErrAlreadyRevoked    = errors.New("sentinel is already revoked")
	ErrAlreadyRegistered = errors.New("sentinel is already registered")

	// Spork
	ErrAlreadyActivated = errors.New("spork is already activated")
)
