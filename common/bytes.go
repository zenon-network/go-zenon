package common

import (
	"encoding/binary"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
)

// JoinBytes concatenates the given byte slices into a newly allocated
// slice, in argument order. It is the building block for hashing
// composite structures such as account blocks and momentums. With no
// non-empty arguments it returns nil.
func JoinBytes(data ...[]byte) []byte {
	var newData []byte
	for _, d := range data {
		newData = append(newData, d...)
	}
	return newData
}

// Uint32ToBytes encodes x as exactly 4 bytes in big-endian order.
func Uint32ToBytes(x uint32) []byte {
	bytes := make([]byte, 4)
	binary.BigEndian.PutUint32(bytes, x)
	return bytes
}

// Uint64ToBytes encodes height as exactly 8 bytes in big-endian order;
// big-endian keeps numerically ordered keys byte-ordered in LevelDB.
func Uint64ToBytes(height uint64) []byte {
	bytes := make([]byte, 8)
	binary.BigEndian.PutUint64(bytes, height)
	return bytes
}

// BytesToUint64 decodes the first 8 bytes as a big-endian uint64,
// reversing Uint64ToBytes. It panics if bytes is shorter than 8.
func BytesToUint64(bytes []byte) uint64 {
	return binary.BigEndian.Uint64(bytes)
}

// BigIntToBytes encodes a non-negative big.Int as its big-endian
// absolute value left-padded with zeros to 32 bytes; nil encodes the
// same as zero. Values wider than 32 bytes are returned at their natural
// length, not truncated.
func BigIntToBytes(int *big.Int) []byte {
	if int == nil {
		return common.LeftPadBytes(Big0.Bytes(), 32)
	} else {
		return common.LeftPadBytes(int.Bytes(), 32)
	}
}

// BytesToBigInt interprets bytes as a big-endian unsigned integer,
// reversing BigIntToBytes. Empty or nil input decodes to 0, never to
// nil.
func BytesToBigInt(bytes []byte) *big.Int {
	if len(bytes) == 0 {
		return big.NewInt(0)
	} else {
		return new(big.Int).SetBytes(bytes)
	}
}

// IsHexCharacter reports whether c is a hexadecimal digit (0-9, a-f or
// A-F).
func IsHexCharacter(c byte) bool {
	return ('0' <= c && c <= '9') || ('a' <= c && c <= 'f') || ('A' <= c && c <= 'F')
}

// IsHex reports whether str consists solely of hexadecimal digits and
// has even length, i.e. encodes whole bytes. It does not accept a 0x
// prefix. The empty string is valid.
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

// StringToBigInt parses a base-10 string into a big.Int. Any input that
// does not parse — including the empty string — silently yields 0 rather
// than an error, so callers that must distinguish "0" from garbage need
// to validate the string themselves. The RPC layer relies on this when
// decoding JSON amount fields.
func StringToBigInt(str string) *big.Int {
	x := new(big.Int)
	_, ok := x.SetString(str, 10)
	if !ok {
		x.SetInt64(0)
	}
	return x
}
