package constants

import "github.com/pkg/errors"

var (
	// ErrVmRunPanic replaces any panic the supervisor recovers
	// during block execution.
	ErrVmRunPanic = errors.New("supervisor - VM panic")

	// === Common ===

	// ErrNothingToWithdraw rejects a reward-collect or refund call
	// when the address has no accumulated balance to send back.
	ErrNothingToWithdraw = errors.New("nothing to withdraw")
	// ErrNotEnoughDepositedQsr rejects a pillar or sentinel
	// registration when the address's QSR deposit doesn't cover the
	// required amount.
	ErrNotEnoughDepositedQsr = errors.New("not enough deposited Qsr")
	// ErrInvalidTokenOrAmount rejects a send whose token standard or
	// amount doesn't match what the called method expects (e.g. a
	// registration fee paid in the wrong token).
	ErrInvalidTokenOrAmount = errors.New("invalid token or amount")
	// ErrNotContractAddress signals that a send's destination is not
	// an embedded-contract address at all; the VM uses it to tell
	// plain transfers apart from embedded calls.
	ErrNotContractAddress = errors.New("not a contract address")
	// ErrContractDoesntExist signals that the destination has the
	// embedded address prefix but no contract is deployed there (or
	// the contract's spork is not yet active).
	ErrContractDoesntExist = errors.New("contract doesn't exist")
	// ErrContractMethodNotFound signals that the call data's 4-byte
	// selector matches no method of the addressed contract.
	ErrContractMethodNotFound = errors.New("method not found in the abi")
	// ErrDataNonExistent signals a miss when loading a value from an
	// embedded contract's storage.
	ErrDataNonExistent = errors.New("data non existent")
	// ErrUnpackError rejects call data whose arguments cannot be
	// ABI-decoded for the selected method.
	ErrUnpackError = errors.New("invalid unpack method data")
	// ErrInsufficientBalance rejects a send block whose amount
	// exceeds the account's balance.
	ErrInsufficientBalance = errors.New("insufficient balance for transfer")
	// ErrPermissionDenied rejects a call from an address that is not
	// allowed to invoke the method (e.g. a non-administrator calling
	// an administrator-only bridge method).
	ErrPermissionDenied = errors.New("address cannot call this method")
	// ErrInvalidArguments rejects arguments that decode correctly but
	// fail the method's semantic validation.
	ErrInvalidArguments = errors.New("invalid arguments")
	// ErrInvalidB64Decode rejects a parameter that should be base64
	// (typically a signature or public key) but doesn't decode.
	ErrInvalidB64Decode = errors.New("invalid b64 decode")
	// ErrForbiddenParam rejects a parameter value that is well-formed
	// but not allowed, such as a proof-of-work request beyond the
	// per-block cap or malformed spork parameters.
	ErrForbiddenParam = errors.New("forbidden parameter")
	// ErrNotEnoughSlots rejects a legacy pillar registration when the
	// legacy public key has no remaining pillar slots.
	ErrNotEnoughSlots = errors.New("not enough slots left")

	// === Common - update contract state ===

	// ErrUpdateTooRecent fails an UpdateEmbedded* call when fewer
	// than UpdateMinNumMomentums momentums have passed since the
	// previous update.
	ErrUpdateTooRecent = errors.New("last update was too recent")
	// ErrEpochUpdateTooRecent stops per-epoch reward processing when
	// the next epoch is not yet finished (plus RewardTimeLimit).
	ErrEpochUpdateTooRecent = errors.New("epoch update was too recent")

	// === Accelerator ===

	// ErrAcceleratorEnded rejects accelerator calls made after
	// AcceleratorDuration has elapsed since genesis.
	ErrAcceleratorEnded = errors.New("accelerator period ended")
	// ErrAcceleratorInvalidFunds rejects a project or phase whose
	// requested funds exceed ProjectZnnMaximumFunds /
	// ProjectQsrMaximumFunds or are otherwise inconsistent.
	ErrAcceleratorInvalidFunds = errors.New("invalid accelerator funds")
	// ErrInvalidDescription rejects a project or phase description
	// that is empty or longer than ProjectDescriptionLengthMax.
	ErrInvalidDescription = errors.New("invalid description")

	// === Pillar ===

	// ErrInvalidName rejects a name that is empty, too long or
	// contains characters outside the allowed set.
	ErrInvalidName = errors.New("invalid name")
	// ErrNotUnique rejects a pillar registration whose name or
	// producing address is already in use.
	ErrNotUnique = errors.New("name or producing address not unique")
	// ErrNotActive rejects an operation on a pillar that has been
	// revoked.
	ErrNotActive = errors.New("pillar is not active")

	// === Token ===

	// ErrIDNotUnique rejects a token issuance whose derived token
	// standard collides with an existing token.
	ErrIDNotUnique = errors.New("there is another token with the same id")
	// ErrTokenInvalidText rejects a token issuance with a malformed
	// name, symbol, domain or decimals declaration.
	ErrTokenInvalidText = errors.New("invalid token name/symbol/domain/decimals")
	// ErrTokenInvalidAmount rejects a token issuance or mint whose
	// total or max supply is out of range.
	ErrTokenInvalidAmount = errors.New("invalid token total/max supply")

	// === Stake ===

	// RevokeNotDue rejects a stake cancellation before the chosen
	// staking period has expired.
	RevokeNotDue = errors.New("staking period still active")
	// ErrInvalidStakingPeriod rejects a stake whose duration is not a
	// whole number of StakeTimeUnitSec units between StakeTimeMinSec
	// and StakeTimeMaxSec.
	ErrInvalidStakingPeriod = errors.New("invalid staking period")

	// === Plasma ===

	// ErrBlockPlasmaLimitReached rejects an account block whose total
	// plasma (fused + PoW) exceeds MaxPlasmaForAccountBlock.
	ErrBlockPlasmaLimitReached = errors.New("plasma limit for account-block reached")
	// ErrNotEnoughPlasma rejects an account block that commits more
	// fused plasma than the address has available.
	ErrNotEnoughPlasma = errors.New("not enough plasma on account")
	// ErrNotEnoughTotalPlasma rejects an account block whose total
	// plasma doesn't cover the block's base requirement.
	ErrNotEnoughTotalPlasma = errors.New("not enough TotalPlasma provided for account-block (PoW + Fused)")

	// === Swap ===

	// ErrInvalidSwapCode rejects a legacy swap claim whose code does
	// not verify against the legacy key.
	ErrInvalidSwapCode = errors.New("invalid swap code")
	// ErrInvalidSignature rejects a legacy swap claim with a bad
	// secp256k1 signature.
	ErrInvalidSignature = errors.New("invalid secp256k1 signature")

	// === Sentinel ===

	// ErrAlreadyRevoked rejects operations on a sentinel that has
	// already been revoked.
	ErrAlreadyRevoked = errors.New("sentinel is already revoked")
	// ErrAlreadyRegistered rejects registering a sentinel for an
	// address that already owns one.
	ErrAlreadyRegistered = errors.New("sentinel is already registered")

	// === Spork ===

	// ErrAlreadyActivated rejects activating a spork a second time.
	ErrAlreadyActivated = errors.New("spork is already activated")

	// === Htlc ===

	// ReclaimNotDue rejects the time-locked sender's reclaim of an
	// HTLC entry before its expiration time.
	ReclaimNotDue = errors.New("entry is not expired")
	// ErrInvalidHashType rejects an HTLC created with an unknown
	// hash-lock algorithm identifier.
	ErrInvalidHashType = errors.New("invalid hash type")
	// ErrInvalidHashDigest rejects an HTLC hash lock whose digest has
	// the wrong length for the chosen hash type.
	ErrInvalidHashDigest = errors.New("invalid hash digest")
	// ErrInvalidPreimage rejects an unlock attempt whose preimage is
	// over-long or doesn't hash to the entry's hash lock.
	ErrInvalidPreimage = errors.New("invalid preimage")
	// ErrInvalidExpirationTime rejects an HTLC whose expiration is
	// not in the future.
	ErrInvalidExpirationTime = errors.New("invalid expiration time")
	// ErrExpired rejects unlocking an HTLC entry whose expiration
	// time has already passed.
	ErrExpired = errors.New("expired")

	// === Bridge ===

	// ErrUnknownNetwork rejects a bridge call naming a network
	// class/chain id pair that has not been registered.
	ErrUnknownNetwork = errors.New("unknown network")
	// ErrInvalidToAddress rejects a wrap or unwrap whose destination
	// address is malformed for the target network.
	ErrInvalidToAddress = errors.New("invalid destination address")
	// ErrBridgeNotInitialized rejects bridge operations before the
	// bridge metadata has been configured.
	ErrBridgeNotInitialized = errors.New("bridge info is not initialized")
	// ErrOrchestratorNotInitialized rejects bridge operations before
	// the orchestrator metadata has been configured.
	ErrOrchestratorNotInitialized = errors.New("orchestrator info is not initialized")
	// ErrTokenNotBridgeable rejects wrapping a token pair that is not
	// flagged bridgeable on the target network.
	ErrTokenNotBridgeable = errors.New("token not bridgeable")
	// ErrNotGuardian rejects a guardian-only call (such as proposing
	// an administrator in emergency) from a non-guardian address.
	ErrNotGuardian = errors.New("sender is not a guardian")
	// ErrTokenNotRedeemable rejects unwrapping a token pair that is
	// not flagged redeemable.
	ErrTokenNotRedeemable = errors.New("token not redeemable")
	// ErrBridgeHalted rejects bridge operations while the bridge is
	// halted.
	ErrBridgeHalted = errors.New("bridge is halted")
	// ErrInvalidRedeemPeriod rejects redeeming an unwrap request
	// before its confirmation delay has elapsed.
	ErrInvalidRedeemPeriod = errors.New("invalid redeem period")
	// ErrInvalidRedeemRequest rejects redeeming an unwrap request
	// that is already redeemed or revoked.
	ErrInvalidRedeemRequest = errors.New("invalid request")
	// ErrInvalidTransactionHash rejects an unwrap request with a
	// malformed source-chain transaction hash.
	ErrInvalidTransactionHash = errors.New("invalid transaction hash")
	// ErrInvalidNetworkName rejects registering a network with an
	// empty or over-long name.
	ErrInvalidNetworkName = errors.New("invalid network name")
	// ErrInvalidContractAddress rejects a network registration with a
	// malformed counterpart contract address.
	ErrInvalidContractAddress = errors.New("invalid contract address")
	// ErrInvalidToken rejects a token pair whose Zenon token standard
	// or counterpart token address is malformed.
	ErrInvalidToken = errors.New("invalid token standard or token address")
	// ErrTokenNotFound rejects a call naming a token pair that is not
	// registered on the network.
	ErrTokenNotFound = errors.New("token not found")
	// ErrInvalidEDDSASignature rejects a bridge message with a bad
	// ed25519 signature.
	ErrInvalidEDDSASignature = errors.New("invalid ed25519 signature")
	// ErrInvalidEDDSAPubKey rejects a malformed ed25519 public key.
	ErrInvalidEDDSAPubKey = errors.New("invalid eddsa public key")
	// ErrInvalidECDSASignature rejects a TSS or orchestrator message
	// with a bad secp256k1 signature.
	ErrInvalidECDSASignature = errors.New("invalid secp256k1 signature")
	// ErrInvalidDecompressedECDSAPubKeyLength rejects a decompressed
	// secp256k1 public key that is not
	// DecompressedECDSAPubKeyLength bytes.
	ErrInvalidDecompressedECDSAPubKeyLength = errors.New("invalid decompressed secp256k1 public key length")
	// ErrInvalidCompressedECDSAPubKeyLength rejects a compressed
	// secp256k1 public key that is not CompressedECDSAPubKeyLength
	// bytes.
	ErrInvalidCompressedECDSAPubKeyLength = errors.New("invalid compressed secp256k1 public key length")
	// ErrNotAllowedToChangeTss rejects a non-administrator change of
	// the TSS public key while the bridge has key generation
	// disabled.
	ErrNotAllowedToChangeTss = errors.New("changing the tss public key is not allowed")
	// ErrInvalidJsonContent rejects metadata parameters that are not
	// valid JSON.
	ErrInvalidJsonContent = errors.New("metadata does not respect the JSON format")
	// ErrInvalidMinAmount rejects a wrap whose amount is below the
	// token pair's configured minimum.
	ErrInvalidMinAmount = errors.New("invalid min amount")
	// ErrTimeChallengeNotDue rejects finishing a time-challenged
	// administrative action before its security delay has elapsed.
	ErrTimeChallengeNotDue = errors.New("time challenge not due")
	// ErrNotEmergency rejects emergency-only calls while the contract
	// is not in emergency mode.
	ErrNotEmergency = errors.New("bridge not in emergency")
	// ErrInvalidGuardians rejects a guardian set that is too small
	// (fewer than MinGuardians). Duplicate addresses are not checked
	// and are stored as given.
	ErrInvalidGuardians = errors.New("invalid guardians")
	// ErrSecurityNotInitialized rejects security-dependent calls
	// before the contract's security info (guardians, delays) has
	// been configured.
	ErrSecurityNotInitialized = errors.New("security not initialized")
	// ErrBridgeNotHalted rejects operations that require a halted
	// bridge, such as reissuing an unhalt, while it is running.
	ErrBridgeNotHalted = errors.New("bridge not halted")

	// === Liquidity ===

	// ErrInvalidPercentages rejects per-token reward percentages that
	// do not sum to LiquidityZnnTotalPercentages /
	// LiquidityQsrTotalPercentages.
	ErrInvalidPercentages = errors.New("invalid percentages")
	// ErrInvalidRewards rejects malformed liquidity stake reward
	// parameters.
	ErrInvalidRewards = errors.New("invalid liquidity stake rewards")
)
