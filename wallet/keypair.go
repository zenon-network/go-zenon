package wallet

import (
	"crypto/ed25519"

	"github.com/zenon-network/go-zenon/common/types"
)

// KeyPair is a single derived account: an ed25519 key pair together
// with the types.Address derived from its public key. It is produced by
// the derivation helpers and used to sign account blocks.
type KeyPair struct {
	Public  ed25519.PublicKey
	Private ed25519.PrivateKey
	Address types.Address
}

// Sign returns the ed25519 signature of message under the key pair's
// private key.
func (kp *KeyPair) Sign(message []byte) []byte {
	return ed25519.Sign(kp.Private, message)
}

// Signer signs data and returns the signature along with the key pair's
// address and public key. It satisfies the generic signing callback the
// node uses to sign account blocks; the error result is always nil.
func (kp *KeyPair) Signer(data []byte) (signedData []byte, address *types.Address, pubkey []byte, err error) {
	return kp.Sign(data), &kp.Address, kp.Public, nil
}
