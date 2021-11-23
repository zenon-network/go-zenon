package wallet

import "github.com/pkg/errors"

var (
	// === KeyFile content errors ===

	ErrKeyFileInvalidVersion = errors.New("unable to read KeyFile. Invalid version")
	ErrKeyFileInvalidCipher  = errors.New("unable to read KeyFile. Invalid cipherName")
	ErrKeyFileInvalidKDF     = errors.New("unable to read KeyFile. Invalid key derivation function (KDF)")

	// === keyStore errors ===

	ErrAddressNotFound = errors.New("the provided address could not be derived from the key store")
	ErrWrongPassword   = errors.New("the key store could not be decrypted with the provided password")

	// === manager errors ===

	ErrKeyStoreLocked   = errors.New("the key store is locked")
	ErrKeyStoreNotFound = errors.New("the provided key store could not be found in the data directory")

	// === derivation errors ===

	ErrInvalidPath        = errors.New("invalid derivation path")
	ErrNoPublicDerivation = errors.New("no public derivation for ed25519")
)
