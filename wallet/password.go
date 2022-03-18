package wallet

import (
	"github.com/ethereum/go-ethereum/common/hexutil"
	"golang.org/x/crypto/argon2"
)

// passwordHash of a password
type passwordHash struct {
	password [32]byte
	salt     hexutil.Bytes
}

// Set updates the password hash to be of the provided password
func (h *passwordHash) Set(password string) error {
	h.salt = GetEntropyCSPRNG(16)
	// pw is the salted, hashed password
	pw := argon2.IDKey([]byte(password), h.salt, 1, 64*1024, 4, 32)
	copy(h.password[:], pw[:32])
	return nil
}
func (h *passwordHash) SetFromJSON(password string, params argon2Params) error {
	h.salt = params.Salt
	// pw is the salted, hashed password
	pw := argon2.IDKey([]byte(password), h.salt, 1, 64*1024, 4, 32)
	copy(h.password[:], pw[:32])
	return nil
}
