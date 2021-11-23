package abi

import (
	"math/big"
	"reflect"

	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/types"
)

const (
	// number of bits in chain big.Word
	WordBits = 32 << (uint64(^big.Word(0)) >> 63)

	// number of bytes in chain big.Word
	WordBytes = WordBits / 8

	// number of bytes in chain vm word
	WordSize = 32
)

var (
	bigT           = reflect.TypeOf(&big.Int{})
	derefbigT      = reflect.TypeOf(big.Int{})
	uint8T         = reflect.TypeOf(uint8(0))
	uint16T        = reflect.TypeOf(uint16(0))
	uint32T        = reflect.TypeOf(uint32(0))
	uint64T        = reflect.TypeOf(uint64(0))
	int8T          = reflect.TypeOf(int8(0))
	int16T         = reflect.TypeOf(int16(0))
	int32T         = reflect.TypeOf(int32(0))
	int64T         = reflect.TypeOf(int64(0))
	addressT       = reflect.TypeOf(types.Address{})
	tokenStandardT = reflect.TypeOf(types.ZenonTokenStandard{})
	hashT          = reflect.TypeOf(types.Hash{})
)

// U256 converts a big Int into a 256bit VM number.
func U256(n *big.Int) []byte {
	return PaddedBigBytes(n.And(n, common.BigP256m1), WordSize)
}

func PaddedBigBytes(bigint *big.Int, n int) []byte {
	if bigint.BitLen()/8 >= n {
		return bigint.Bytes()
	}
	ret := make([]byte, n)
	i := len(ret)
	for _, d := range bigint.Bits() {
		for j := 0; j < WordBytes && i > 0; j++ {
			i--
			ret[i] = byte(d)
			d >>= 8
		}
	}
	return ret
}
