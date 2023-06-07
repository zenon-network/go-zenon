package common

import (
	"encoding/binary"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
)

func JoinBytes(data ...[]byte) []byte {
	var newData []byte
	for _, d := range data {
		newData = append(newData, d...)
	}
	return newData
}

func Uint32ToBytes(x uint32) []byte {
	bytes := make([]byte, 4)
	binary.BigEndian.PutUint32(bytes, x)
	return bytes
}

func Uint64ToBytes(height uint64) []byte {
	bytes := make([]byte, 8)
	binary.BigEndian.PutUint64(bytes, height)
	return bytes
}
func BytesToUint64(bytes []byte) uint64 {
	return binary.BigEndian.Uint64(bytes)
}

func BigIntToBytes(int *big.Int) []byte {
	if int == nil {
		return common.LeftPadBytes(Big0.Bytes(), 32)
	} else {
		return common.LeftPadBytes(int.Bytes(), 32)
	}
}
func BytesToBigInt(bytes []byte) *big.Int {
	if len(bytes) == 0 {
		return big.NewInt(0)
	} else {
		return new(big.Int).SetBytes(bytes)
	}
}

// IsHexCharacter returns bool of c being a valid hexadecimal.
func IsHexCharacter(c byte) bool {
	return ('0' <= c && c <= '9') || ('a' <= c && c <= 'f') || ('A' <= c && c <= 'F')
}

// IsHex validates whether each byte is valid hexadecimal string.
func IsHex(str string) bool {
	if len(str)%2 != 0 {
		return false
	}
	for _, c := range []byte(str) {
		if !IsHexCharacter(c) {
			return false
		}
	}
	return true
}

// StringToBigInt The default value is 0 when it cannot parse or the string is ""
func StringToBigInt(str string) *big.Int {
	x := new(big.Int)
	_, ok := x.SetString(str, 10)
	if !ok {
		x.SetInt64(0)
	}
	return x
}
