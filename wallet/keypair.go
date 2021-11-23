package wallet

import (
	"crypto/ed25519"

	"github.com/zenon-network/go-zenon/common/types"
)

type KeyPair struct {
	Public  ed25519.PublicKey
	Private ed25519.PrivateKey
	Address types.Address
}

func (kp *KeyPair) Sign(message []byte) []byte {
	return ed25519.Sign(kp.Private, message)
}
func (kp *KeyPair) Signer(data []byte) (signedData []byte, address *types.Address, pubkey []byte, err error) {
	return kp.Sign(data), &kp.Address, kp.Public, nil
}
