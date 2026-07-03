package wallet

import "github.com/pkg/errors"

var (
	// === KeyFile content errors ===

	// ErrKeyFileInvalidVersion is returned when a KeyFile declares a
	// crypto version this build does not understand.
	ErrKeyFileInvalidVersion = errors.New("unable to read KeyFile. Invalid version")
	// ErrKeyFileInvalidCipher is returned when a KeyFile names a cipher
	// other than the expected aes-256-gcm.
	ErrKeyFileInvalidCipher = errors.New("unable to read KeyFile. Invalid cipherName")
	// ErrKeyFileInvalidKDF is returned when a KeyFile names a key
	// derivation function other than the expected Argon2id.
	ErrKeyFileInvalidKDF = errors.New("unable to read KeyFile. Invalid key derivation function (KDF)")

	// === keyStore errors ===

	// ErrAddressNotFound is returned by FindAddress when the address is
	// not produced by any index within the searched range.
	ErrAddressNotFound = errors.New("the provided address could not be derived from the key store")
	// ErrWrongPassword is returned when a KeyFile cannot be decrypted
	// with the supplied password.
	ErrWrongPassword = errors.New("the key store could not be decrypted with the provided password")

	// === manager errors ===

	// ErrKeyStoreLocked is returned when an operation needs a decrypted
	// key store but the requested one is still locked.
	ErrKeyStoreLocked = errors.New("the key store is locked")
	// ErrKeyStoreNotFound is returned when no key file for the
	// requested path exists in the wallet directory.
	ErrKeyStoreNotFound = errors.New("the provided key store could not be found in the data directory")

	// === derivation errors ===

	// ErrInvalidPath is returned when a derivation path is malformed or
	// not in the expected hardened BIP-44 form.
	ErrInvalidPath = errors.New("invalid derivation path")
	// ErrNoPublicDerivation is returned for any attempt at non-hardened
	// derivation, which ed25519 does not support.
	ErrNoPublicDerivation = errors.New("no public derivation for ed25519")
)
