package abi

import (
	"math/big"
	"reflect"

	"github.com/ethereum/go-ethereum/common"

	common2 "github.com/zenon-network/go-zenon/common"
)

// packBytesSlice packs the given bytes as [L, V] as the canonical representation
// bytes slice
func packBytesSlice(bytes []byte, l int) ([]byte, error) {
	length, err := packNum(reflect.ValueOf(l))
	if err != nil {
		return nil, err
	}
	return append(length, common.RightPadBytes(bytes, (l+WordSize-1)/WordSize*WordSize)...), nil
}

// packElement packs the given reflect value according to the abi specification in
// t.
func packElement(t Type, reflectValue reflect.Value) ([]byte, error) {
	switch t.T {
	case IntTy, UintTy:
		return packNum(reflectValue)
	case StringTy:
		return packBytesSlice([]byte(reflectValue.String()), reflectValue.Len())
	case AddressTy:
		if reflectValue.Kind() == reflect.Array {
			reflectValue = mustArrayToByteSlice(reflectValue)
		}
		return common.LeftPadBytes(reflectValue.Bytes(), WordSize), nil
	case TokenStandardTy:
		if reflectValue.Kind() == reflect.Array {
			reflectValue = mustArrayToByteSlice(reflectValue)
		}
		return common.LeftPadBytes(reflectValue.Bytes(), WordSize), nil
	case BoolTy:
		if reflectValue.Bool() {
			return PaddedBigBytes(common2.Big1, WordSize), nil
		}
		return PaddedBigBytes(common2.Big0, WordSize), nil
	case BytesTy:
		if reflectValue.Kind() == reflect.Array {
			reflectValue = mustArrayToByteSlice(reflectValue)
		}
		return packBytesSlice(reflectValue.Bytes(), reflectValue.Len())
	case FixedBytesTy:
		if reflectValue.Kind() == reflect.Array {
			reflectValue = mustArrayToByteSlice(reflectValue)
		}
		return common.RightPadBytes(reflectValue.Bytes(), WordSize), nil
	case HashTy:
		if reflectValue.Kind() == reflect.Array {
			reflectValue = mustArrayToByteSlice(reflectValue)
		}
		return common.LeftPadBytes(reflectValue.Bytes(), WordSize), nil
	default:
		return nil, errPackFailed
	}
}

// packNum packs the given number (using the reflect value) and will cast it to appropriate number representation
func packNum(value reflect.Value) ([]byte, error) {
	switch kind := value.Kind(); kind {
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return U256(new(big.Int).SetUint64(value.Uint())), nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return U256(big.NewInt(value.Int())), nil
	case reflect.Ptr:
		return U256(value.Interface().(*big.Int)), nil
	default:
		return nil, errPackFailed
	}

}
