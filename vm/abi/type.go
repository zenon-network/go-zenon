package abi

import (
	"reflect"
	"regexp"
	"strconv"
	"strings"

	"github.com/zenon-network/go-zenon/common/types"
)

// Type enumerator
const (
	IntTy byte = iota
	UintTy
	BoolTy
	StringTy
	SliceTy
	ArrayTy
	AddressTy
	TokenStandardTy
	FixedBytesTy
	BytesTy
	HashTy
)

// Type is the reflection of the supported argument type
type Type struct {
	Elem *Type

	Kind reflect.Kind
	Type reflect.Type
	Size int
	T    byte // Our own type checking

	stringKind string // holds the unparsed string for deriving signatures
}

var (
	// typeRegex parses the abi sub types
	typeRegex = regexp.MustCompile("([a-zA-Z]+)([0-9]+)?")
)

// NewType creates a new reflection type of abi type given in t.
func NewType(t string) (typ Type, err error) {
	if t == "uint" || t == "int" {
		// this should fail because it means that there's something wrong with
		// the abi type (the compiler should always format it to the size...always)
		return Type{}, errUnsupportedArgType(t)
	}

	// check that array brackets are equal if they exist
	if strings.Count(t, "[") != strings.Count(t, "]") || strings.Count(t, "[") > 1 {
		return Type{}, errUnsupportedArgType(t)
	}

	typ.stringKind = t

	// if there are brackets, get ready to go into slice/array mode and
	// recursively create the type
	if strings.Count(t, "[") != 0 {
		i := strings.LastIndex(t, "[")
		// recursively embed the type
		embeddedType, err := NewType(t[:i])
		if err != nil {
			return Type{}, err
		}
		// grab the last cell and create a type from there
		sliced := t[i:]
		// grab the slice size with regexp
		re := regexp.MustCompile("[0-9]+")
		intz := re.FindAllString(sliced, -1)

		if len(intz) == 0 {
			// is a slice
			typ.T = SliceTy
			typ.Kind = reflect.Slice
			typ.Elem = &embeddedType
			typ.Type = reflect.SliceOf(embeddedType.Type)
		} else if len(intz) == 1 {
			size, err := strconv.Atoi(intz[0])
			if err != nil {
				return Type{}, errParsingVariableSize(err)
			}
			if size == 0 {
				return Type{}, errInvalidZeroVariableSize
			}
			// is a array
			typ.T = ArrayTy
			typ.Kind = reflect.Array
			typ.Elem = &embeddedType
			typ.Size = size
			typ.Type = reflect.ArrayOf(typ.Size, embeddedType.Type)
		} else {
			return Type{}, errInvalidArrayTypeFormatting
		}
		return typ, err
	}
	// parse the type and size of the abi-type.
	parsedType := typeRegex.FindAllStringSubmatch(t, -1)[0]
	// varSize is the size of the variable
	var varSize int
	if len(parsedType[2]) > 0 {
		var err error
		varSize, err = strconv.Atoi(parsedType[2])
		if err != nil {
			return Type{}, errParsingVariableSize(err)
		}
		if varSize == 0 {
			return Type{}, errInvalidZeroVariableSize
		}
	}
	// varType is the parsed abi type
	switch varType := parsedType[1]; varType {
	case "int":
		typ.Kind, typ.Type = reflectIntKindAndType(false, varSize)
		typ.Size = varSize
		typ.T = IntTy
	case "uint":
		typ.Kind, typ.Type = reflectIntKindAndType(true, varSize)
		typ.Size = varSize
		typ.T = UintTy
	case "bool":
		typ.Kind = reflect.Bool
		typ.T = BoolTy
		typ.Type = reflect.TypeOf(bool(false))
	case "address":
		typ.Kind = reflect.Array
		typ.Type = addressT
		typ.Size = types.AddressSize
		typ.T = AddressTy
	case "tokenStandard":
		typ.Kind = reflect.Array
		typ.Type = tokenStandardT
		typ.Size = types.ZenonTokenStandardSize
		typ.T = TokenStandardTy
	case "string":
		typ.Kind = reflect.String
		typ.Type = reflect.TypeOf("")
		typ.T = StringTy
	case "bytes":
		if varSize == 0 {
			typ.T = BytesTy
			typ.Kind = reflect.Slice
			typ.Type = reflect.SliceOf(reflect.TypeOf(byte(0)))
		} else {
			typ.T = FixedBytesTy
			typ.Kind = reflect.Array
			typ.Size = varSize
			typ.Type = reflect.ArrayOf(varSize, reflect.TypeOf(byte(0)))
		}
	case "hash":
		typ.Kind = reflect.Array
		typ.Type = hashT
		typ.Size = types.HashSize
		typ.T = HashTy
	default:
		return Type{}, errUnsupportedArgType(t)
	}

	return
}

// String implements Stringer
func (t Type) String() (out string) {
	return t.stringKind
}

func (t Type) pack(v reflect.Value) ([]byte, error) {
	// dereference pointer first if it's a pointer
	v = indirect(v)

	if err := typeCheck(t, v); err != nil {
		return nil, err
	}
	if t.T == SliceTy || t.T == ArrayTy {
		var offsets []byte
		var packed []byte

		offset := 0
		offsetReq := t.Elem.requiresLengthPrefix()
		if offsetReq {
			offset = getTypeSize(*t.Elem) * v.Len()
		}

		for i := 0; i < v.Len(); i++ {
			val, err := t.Elem.pack(v.Index(i))
			if err != nil {
				return nil, err
			}

			if offsetReq {
				offsetPacked, err := packNum(reflect.ValueOf(offset))
				if err != nil {
					return nil, err
				}
				offsets = append(offsets, offsetPacked...)
				offset += len(val)
			}

			packed = append(packed, val...)
		}
		packed = append(offsets, packed...)
		if t.T == SliceTy {
			return packBytesSlice(packed, v.Len())
		} else if t.T == ArrayTy {
			return packed, nil
		}
	}
	return packElement(t, v)
}

// requireLengthPrefix returns whether the type requires any sort of length
// prefixing.
func (t Type) requiresLengthPrefix() bool {
	return t.T == StringTy || t.T == BytesTy || t.T == SliceTy
}

// getTypeSize returns the size that this type needs to occupy.
// We distinguish static and dynamic types. Static types are encoded in-place
// and dynamic types are encoded at a separately allocated location after the
// current block.
// So for a static variable, the size returned represents the size that the
// variable actually occupies.
// For a dynamic variable, the returned size is fixed 32 bytes, which is used
// to store the location reference for actual value storage.
func getTypeSize(t Type) int {
	if t.T == ArrayTy && !t.Elem.requiresLengthPrefix() {
		// Recursively calculate type size if it is a nested array
		if t.Elem.T == ArrayTy {
			return t.Size * getTypeSize(*t.Elem)
		}
		return t.Size * 32
	}
	return 32
}
