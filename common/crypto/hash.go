package crypto

import (
	"crypto/sha256"

	"golang.org/x/crypto/sha3"
)

func Hash(data ...[]byte) []byte {
	d := sha3.New256()
	for _, item := range data {
		d.Write(item)
	}
	return d.Sum(nil)
}

func HashSHA256(data ...[]byte) []byte {
	d := sha256.New()
	for _, item := range data {
		d.Write(item)
	}
	return d.Sum(nil)
}

func Keccak256(data ...[]byte) []byte {
	d := sha3.NewLegacyKeccak256()
	for _, item := range data {
		d.Write(item)
	}

	return d.Sum(nil)
}
