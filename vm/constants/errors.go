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
	ErrInvalidArguments       = errors.New("invalid arguments")
	ErrInvalidB64Decode       = errors.New("invalid b64 decode")
	ErrForbiddenParam         = errors.New("forbidden parameter")
	ErrNotEnoughSlots         = errors.New("not enough slots left")

	// Common - update contract state
	ErrUpdateTooRecent      = errors.New("last update was too recent")
	ErrEpochUpdateTooRecent = errors.New("epoch update was too recent")

	// Accelerator
	ErrAcceleratorEnded        = errors.New("accelerator period ended")
	ErrAcceleratorInvalidFunds = errors.New("invalid accelerator funds")
	ErrInvalidDescription      = errors.New("invalid description")

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

	// Htlc
	ReclaimNotDue            = errors.New("entry is not expired")
	ErrInvalidHashType       = errors.New("invalid hash type")
	ErrInvalidHashDigest     = errors.New("invalid hash digest")
	ErrInvalidPreimage       = errors.New("invalid preimage")
	ErrInvalidExpirationTime = errors.New("invalid expiration time")
	ErrExpired               = errors.New("expired")

	// Bridge
	ErrUnknownNetwork                       = errors.New("unknown network")
	ErrInvalidToAddress                     = errors.New("invalid destination address")
	ErrBridgeNotInitialized                 = errors.New("bridge info is not initialized")
	ErrOrchestratorNotInitialized           = errors.New("orchestrator info is not initialized")
	ErrTokenNotBridgeable                   = errors.New("token not bridgeable")
	ErrNotGuardian                          = errors.New("sender is not a guardian")
	ErrTokenNotRedeemable                   = errors.New("token not redeemable")
	ErrBridgeHalted                         = errors.New("bridge is halted")
	ErrInvalidRedeemPeriod                  = errors.New("invalid redeem period")
	ErrInvalidRedeemRequest                 = errors.New("invalid request")
	ErrInvalidTransactionHash               = errors.New("invalid transaction hash")
	ErrInvalidNetworkName                   = errors.New("invalid network name")
	ErrInvalidContractAddress               = errors.New("invalid contract address")
	ErrInvalidToken                         = errors.New("invalid token standard or token address")
	ErrTokenNotFound                        = errors.New("token not found")
	ErrInvalidEDDSASignature                = errors.New("invalid ed25519 signature")
	ErrInvalidEDDSAPubKey                   = errors.New("invalid eddsa public key")
	ErrInvalidECDSASignature                = errors.New("invalid secp256k1 signature")
	ErrInvalidDecompressedECDSAPubKeyLength = errors.New("invalid decompressed secp256k1 public key length")
	ErrInvalidCompressedECDSAPubKeyLength   = errors.New("invalid compressed secp256k1 public key length")
	ErrNotAllowedToChangeTss                = errors.New("changing the tss public key is not allowed")
	ErrInvalidJsonContent                   = errors.New("metadata does not respect the JSON format")
	ErrInvalidMinAmount                     = errors.New("invalid min amount")
	ErrTimeChallengeNotDue                  = errors.New("time challenge not due")
	ErrNotEmergency                         = errors.New("bridge not in emergency")
	ErrInvalidGuardians                     = errors.New("invalid guardians")
	ErrSecurityNotInitialized               = errors.New("security not initialized")
	ErrBridgeNotHalted                      = errors.New("bridge not halted")

	// Liquidity
	ErrInvalidPercentages = errors.New("invalid percentages")
	ErrInvalidRewards     = errors.New("invalid liquidity stake rewards")
)
