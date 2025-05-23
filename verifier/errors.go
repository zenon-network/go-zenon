package verifier

import (
	"fmt"

	"github.com/pkg/errors"
)

func InternalError(err error) error {
	return fmt.Errorf("%w - %v", ErrVerifierInternal, err)
}

func DescendantVerifyError(err error) error {
	return fmt.Errorf("%w - %v", ErrABDescendantVerify, err)
}

var (
	ErrVerifierInternal = errors.New("internal error while verifying")

	ErrABVersionMissing            = errors.New("account-block version is missing")
	ErrABVersionInvalid            = errors.New("account-block version is invalid")
	ErrABChainIdentifierMissing    = errors.New("account-block chain-identifier is missing")
	ErrABChainIdentifierMismatch   = errors.New("account-block chain-identifier mismatch (belongs to another chain)")
	ErrABTypeInvalidExternal       = errors.New("account-block type is invalid (batched blocks should not exist as stand-alone)")
	ErrABTypeMissing               = errors.New("account-block type is missing")
	ErrABTypeMustNotBeGenesis      = errors.New("account-block type must not be genesis")
	ErrABTypeUnsupported           = errors.New("account-block type is not supported")
	ErrABTypeMustBeContract        = errors.New("account-block type is not suitable for contracts")
	ErrABTypeMustBeUser            = errors.New("account-block type is not suitable for user-blocks")
	ErrABMHeightMissing            = errors.New("account-block height must be higher than 0")
	ErrABPrevHeightExists          = errors.New("account-block prevHeight is cemented but has different hash")
	ErrABPrevHasCementedOnTop      = errors.New("account-block prevHash exists but it has a cemented block on top of it")
	ErrABPrevHashMissing           = errors.New("account-block prevHash must not be zero")
	ErrABPrevHashMustBeZero        = errors.New("account-block prevHash must be zero")
	ErrABAmountNegative            = errors.New("account-block amount can't be negative")
	ErrABAmountTooBig              = errors.New("account-block amount is too big")
	ErrABAmountMustBeZero          = errors.New("account-block amount must be zero")
	ErrABZtsMissing                = errors.New("account-block zts is missing (non-zero amount)")
	ErrABZtsMustBeZero             = errors.New("account-block zts must be zero")
	ErrABToAddressMustBeZero       = errors.New("account-block to-address must be zero")
	ErrABHashMissing               = errors.New("account-block hash must not be zero")
	ErrABHashInvalid               = errors.New("account-block hash is different than the one computed")
	ErrABDataTooBig                = errors.New("account-block data field is too big")
	ErrABPublicKeyWrongAddress     = errors.New("account-block publicKey doesn't correspond to the address")
	ErrABPublicKeyMissing          = errors.New("account-block publicKey is missing")
	ErrABPublicKeyMustBeZero       = errors.New("account-block publicKey must be zero")
	ErrABSignatureInvalid          = errors.New("account-block signature is invalid")
	ErrABSignatureMissing          = errors.New("account-block signature is missing")
	ErrABSignatureMustBeZero       = errors.New("account-block signature must be zero")
	ErrABPoWInvalid                = errors.New("account-block nonce/difficulty is invalid")
	ErrABDescendantMustBeZero      = errors.New("account-block descendant blocks must be empty")
	ErrABDescendantVerify          = errors.New("account-block descendant block failed to pass verifications")
	ErrABPreviousMissing           = errors.New("account-block previous block is missing")
	ErrABMAGap                     = errors.New("account-block momentum-acknowledged points to an older momentum than previous")
	ErrABMAMustBeTheSame           = errors.New("account-block momentum-acknowledged must have the same value for batched blocks")
	ErrABMAInvalidForAutoGenerated = errors.New("account-block momentum-acknowledged points to invalid momentum for auto-generated blocks")
	ErrABMAMissing                 = errors.New("account-block momentum-acknowledged points to missing momentum")
	ErrABMAMustNotBeZero           = errors.New("account-block momentum-acknowledged missing")
	ErrABFromBlockHashMissing      = errors.New("account-block from-block-hash is nor provided")
	ErrABFromBlockHashMustBeZero   = errors.New("account-block from-block-hash must be zero")
	ErrABFromBlockMissing          = errors.New("account-block from-block doesn't exist")
	ErrABFromBlockAlreadyReceived  = errors.New("account-block from-block already received")
	ErrABFromBlockReceiverMismatch = errors.New("account-block from-block receiver mismatch")
	ErrABSequencerNothing          = errors.New("account-block failed to pass sequencer checks. Nothing to receive")
	ErrABSequencerNotNext          = errors.New("account-block failed to pass sequencer checks. Not next in line to receive")

	ErrMVersionMissing          = errors.New("momentum version is missing")
	ErrMVersionInvalid          = errors.New("momentum version is invalid")
	ErrMChainIdentifierMissing  = errors.New("momentum chain-identifier is missing")
	ErrMChainIdentifierMismatch = errors.New("momentum chain-identifier mismatch (belongs to another chain)")
	ErrMDataMustBeZero          = errors.New("momentum data must be zero")
	ErrMChangesHashInvalid      = errors.New("momentum changes-hash is different than the one computed")
	ErrMHashInvalid             = errors.New("momentum hash is different than the one computed")
	ErrMContentTooBig           = errors.New("momentum content is too big")
	ErrMTimestampMissing        = errors.New("momentum timestamp is missing")
	ErrMTimestampInTheFuture    = errors.New("momentum timestamp is in the future (more than 10 seconds)")
	ErrMTimestampNotIncreasing  = errors.New("momentum timestamp is is lower than previous timestamp")
	ErrMSignatureMissing        = errors.New("momentum signature is missing")
	ErrMPublicKeyMissing        = errors.New("momentum publicKey is missing")
	ErrMSignatureInvalid        = errors.New("momentum signature is invalid")
	ErrMPrevHashMissing         = errors.New("momentum prevHash must not be zero")
	ErrMNotGenesis              = errors.New("momentum is not genesis-momentum")
	ErrMProducerInvalid         = errors.New("momentum producer is invalid")
	ErrMPreviousMissing         = errors.New("momentum previous momentum is missing")
)
