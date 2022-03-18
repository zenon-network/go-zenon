package wallet

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ed25519"
	"crypto/rand"
	"io"

	"github.com/pkg/errors"
)

const (
	gcmAdditionData = "zenon"
)

func aesGCMEncrypt(key, inText []byte) (outText, nonce []byte, err error) {
	aesBlock, err := aes.NewCipher(key)
	if err != nil {
		return nil, nil, err
	}
	stream, err := cipher.NewGCM(aesBlock)
	if err != nil {
		return nil, nil, err
	}

	nonce = GetEntropyCSPRNG(12)

	outText = stream.Seal(nil, nonce, inText, []byte(gcmAdditionData))
	return outText, nonce, err
}
func aesGCMDecrypt(key, cipherText, nonce []byte) ([]byte, error) {
	aesBlock, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	stream, err := cipher.NewGCM(aesBlock)
	if err != nil {
		return nil, err
	}

	outText, err := stream.Open(nil, nonce, cipherText, []byte(gcmAdditionData))
	if err != nil {
		return nil, err
	}

	return outText, err
}
func GetEntropyCSPRNG(n int) []byte {
	mainBuff := make([]byte, n)
	_, err := io.ReadFull(rand.Reader, mainBuff)
	if err != nil {
		panic("reading from crypto/rand failed: " + err.Error())
	}
	return mainBuff
}

func VerifySignature(pubkey ed25519.PublicKey, message, sig []byte) (bool, error) {
	if len(pubkey) != ed25519.PublicKeySize {
		return false, errors.Errorf("ed25519: bad public key length; length=%v", len(pubkey))
	}
	return ed25519.Verify(pubkey, message, sig), nil
}
