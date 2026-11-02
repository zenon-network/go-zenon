package abi

import (
	"bytes"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/types"
)

// Each fixed-width value is followed by a uint64 so a value that packs to
// more or less than one word would show up as a shifted second word.
const packGuardsABI = `[
	{"type":"function","name":"address","inputs":[{"name":"v","type":"address"},{"name":"n","type":"uint64"}]},
	{"type":"function","name":"tokenStandard","inputs":[{"name":"v","type":"tokenStandard"},{"name":"n","type":"uint64"}]},
	{"type":"function","name":"hash","inputs":[{"name":"v","type":"hash"},{"name":"n","type":"uint64"}]},
	{"type":"function","name":"bytes20","inputs":[{"name":"v","type":"bytes20"},{"name":"n","type":"uint64"}]},
	{"type":"function","name":"addresses","inputs":[{"name":"v","type":"address[]"},{"name":"n","type":"uint64"}]},
	{"type":"function","name":"addressPair","inputs":[{"name":"v","type":"address[2]"},{"name":"n","type":"uint64"}]}
]`

type namedByte byte

// packNoPanic runs PackMethod and turns a panic into a test failure, so a
// guard that is missing fails on the invariant rather than crashing the run.
func packNoPanic(t *testing.T, contract ABIContract, method string, args ...interface{}) (data []byte, err error) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("PackMethod(%s) panicked: %v", method, r)
		}
	}()
	return contract.PackMethod(method, args...)
}

func patterned(n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(i + 1)
	}
	return b
}

// byteArray returns a [n]byte holding patterned(n), as an interface value.
func byteArray(n int) interface{} {
	v := reflect.New(reflect.ArrayOf(n, reflect.TypeOf(byte(0)))).Elem()
	reflect.Copy(v, reflect.ValueOf(patterned(n)))
	return v.Interface()
}

var fixedWidthMethods = []struct {
	method string
	size   int
	// rightPad is true for bytesN, which pads on the right; the Zenon
	// types pad on the left.
	rightPad bool
}{
	{"address", types.AddressSize, false},
	{"tokenStandard", types.ZenonTokenStandardSize, false},
	{"hash", types.HashSize, false},
	{"bytes20", 20, true},
}

// A value for a fixed-width argument must be a byte array or byte slice of
// exactly the declared width. Anything else used to pack into a word of the
// wrong width (shifting every later argument), pack silently into a padded
// word, or panic.
func TestPack_FixedWidthValueMustMatchDeclaredSize(t *testing.T) {
	contract := JSONToABIContract(strings.NewReader(packGuardsABI))
	for _, m := range fixedWidthMethods {
		rejected := []struct {
			name  string
			value interface{}
		}{
			{"int", 7},
			{"string of the right length", strings.Repeat("a", m.size)},
			{"array one byte longer", byteArray(m.size + 1)},
			{"array one byte shorter", byteArray(m.size - 1)},
			{"array longer than a word", byteArray(WordSize + 1)},
			{"slice one byte longer", patterned(m.size + 1)},
			{"slice one byte shorter", patterned(m.size - 1)},
			{"empty slice", []byte{}},
			{"array of int8", reflect.New(reflect.ArrayOf(m.size, reflect.TypeOf(int8(0)))).Elem().Interface()},
			{"array of a named byte type", reflect.New(reflect.ArrayOf(m.size, reflect.TypeOf(namedByte(0)))).Elem().Interface()},
		}
		for _, r := range rejected {
			t.Run(fmt.Sprintf("%s/%s", m.method, r.name), func(t *testing.T) {
				data, err := packNoPanic(t, contract, m.method, r.value, uint64(7))
				if err == nil {
					t.Fatalf("packed %T into %d bytes; want an error", r.value, len(data)-4)
				}
			})
		}
	}
}

// The accepted shapes still pack to exactly one word, and the argument that
// follows sits in the second word.
func TestPack_FixedWidthValueOfDeclaredSize(t *testing.T) {
	contract := JSONToABIContract(strings.NewReader(packGuardsABI))
	nWord := make([]byte, WordSize)
	nWord[WordSize-1] = 7
	for _, m := range fixedWidthMethods {
		want := make([]byte, WordSize)
		if m.rightPad {
			copy(want, patterned(m.size))
		} else {
			copy(want[WordSize-m.size:], patterned(m.size))
		}
		want = append(want, nWord...)

		arr := byteArray(m.size)
		ptr := reflect.New(reflect.TypeOf(arr))
		ptr.Elem().Set(reflect.ValueOf(arr))
		accepted := []struct {
			name  string
			value interface{}
		}{
			{"byte array", arr},
			{"pointer to byte array", ptr.Interface()},
			{"byte slice", patterned(m.size)},
		}
		for _, a := range accepted {
			t.Run(fmt.Sprintf("%s/%s", m.method, a.name), func(t *testing.T) {
				data, err := packNoPanic(t, contract, m.method, a.value, uint64(7))
				common.FailIfErr(t, err)
				if !bytes.Equal(data[4:], want) {
					t.Fatalf("packed %x, want %x", data[4:], want)
				}
			})
		}
	}
}

// The Zenon types the contracts pass pack exactly as before and decode back
// to the same values.
func TestPack_ZenonFixedWidthTypesRoundTrip(t *testing.T) {
	contract := JSONToABIContract(strings.NewReader(packGuardsABI))
	address := types.PillarContract
	zts := types.QsrTokenStandard
	hash := types.HexToHashPanic("0102030405060708091011121314151617181920212223242526272829303132")
	for _, c := range []struct {
		method string
		value  interface{}
	}{
		{"address", address},
		{"address", &address},
		{"tokenStandard", zts},
		{"hash", hash},
		{"hash", &hash},
		{"addresses", []types.Address{types.PillarContract, types.TokenContract}},
		{"addresses", []types.Address{}},
		{"addressPair", [2]types.Address{types.PillarContract, types.TokenContract}},
	} {
		t.Run(fmt.Sprintf("%s/%T", c.method, c.value), func(t *testing.T) {
			data, err := packNoPanic(t, contract, c.method, c.value, uint64(7))
			common.FailIfErr(t, err)
			values, err := contract.Methods[c.method].Inputs.UnpackValues(data[4:])
			common.FailIfErr(t, err)
			want := reflect.Indirect(reflect.ValueOf(c.value)).Interface()
			if !reflect.DeepEqual(values[0], want) {
				t.Fatalf("round trip gave %v, want %v", values[0], want)
			}
			common.Expect(t, values[1], uint64(7))
			repacked, err := contract.Methods[c.method].Inputs.Pack(values...)
			common.FailIfErr(t, err)
			if !bytes.Equal(repacked, data[4:]) {
				t.Fatalf("repack of the decoded values gave %x, want %x", repacked, data[4:])
			}
		})
	}
}

// The width check applies to every element of an array or slice of a
// fixed-width type, not only to top-level arguments.
func TestPack_FixedWidthElementsMustMatchDeclaredSize(t *testing.T) {
	contract := JSONToABIContract(strings.NewReader(packGuardsABI))
	overlong := [types.AddressSize + 1]byte{}
	for _, c := range []struct {
		method string
		value  interface{}
	}{
		{"addresses", [][types.AddressSize + 1]byte{overlong}},
		{"addresses", [][]byte{patterned(types.AddressSize), patterned(types.AddressSize + 1)}},
		{"addressPair", [2][types.AddressSize + 1]byte{overlong, overlong}},
		{"addressPair", [2]int{1, 2}},
	} {
		t.Run(fmt.Sprintf("%s/%T", c.method, c.value), func(t *testing.T) {
			data, err := packNoPanic(t, contract, c.method, c.value, uint64(7))
			if err == nil {
				t.Fatalf("packed %T into %d bytes; want an error", c.value, len(data)-4)
			}
		})
	}
}

// sliceOfFixedArrays builds the Type of a slice whose elements are elem.
// NewType accepts one bracket pair only, so no parsed ABI has this shape;
// sliceTypeCheck must still not index into an empty value.
func sliceOfFixedArrays(t *testing.T, elem string) Type {
	inner, err := NewType(elem)
	common.FailIfErr(t, err)
	return Type{T: SliceTy, Kind: reflect.Slice, Type: reflect.SliceOf(inner.Type), Elem: &inner, stringKind: elem + "[]"}
}

func TestTypeCheck_EmptySliceOfFixedArrays(t *testing.T) {
	typ := sliceOfFixedArrays(t, "uint8[2]")
	empty := reflect.ValueOf([][2]uint8{})

	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("typeCheck panicked on an empty slice of fixed arrays: %v", r)
			}
		}()
		common.FailIfErr(t, typeCheck(typ, empty))
	}()

	packed, err := typ.pack(empty)
	common.FailIfErr(t, err)
	common.Expect(t, fmt.Sprintf("%x", packed), fmt.Sprintf("%x", make([]byte, WordSize)))

	// A non-empty value is still checked against the element size.
	common.FailIfErr(t, typeCheck(typ, reflect.ValueOf([][2]uint8{{1, 2}})))
	if err := typeCheck(typ, reflect.ValueOf([][3]uint8{{1, 2, 3}})); err == nil {
		t.Fatal("accepted [3]uint8 elements for uint8[2]")
	}
}
