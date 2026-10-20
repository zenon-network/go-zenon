package abi

import (
	"bytes"
	"math/big"
	"reflect"
	"strings"
	"testing"

	"github.com/zenon-network/go-zenon/common"
)

// expectedU256 is an independent oracle for the one-word encoding: the
// value reduced modulo 2^256 (Euclidean, so negatives wrap to their two's
// complement) and written big-endian into exactly 32 bytes. It shares no
// code with U256 and does not touch its input.
func expectedU256(n *big.Int) []byte {
	modulus := new(big.Int).Lsh(big.NewInt(1), 256)
	reduced := new(big.Int).Mod(n, modulus) // Mod is Euclidean: the result is in [0, 2^256)
	out := make([]byte, WordSize)
	reduced.FillBytes(out)
	return out
}

// u256Cases covers zero, an ordinary value, the top of the range, the
// first over-wide value, the all-ones negative, another small negative,
// a negative that wraps to zero and one that wraps to all-ones.
func u256Cases() []*big.Int {
	two256 := new(big.Int).Lsh(big.NewInt(1), 256)
	return []*big.Int{
		big.NewInt(0),
		big.NewInt(5),
		new(big.Int).Sub(two256, big.NewInt(1)),
		new(big.Int).Set(two256),
		new(big.Int).Add(two256, big.NewInt(7)),
		big.NewInt(-1),
		big.NewInt(-2),
		new(big.Int).Neg(two256),
		new(big.Int).Neg(new(big.Int).Add(two256, big.NewInt(1))),
	}
}

func TestU256_DoesNotMutateTheCaller(t *testing.T) {
	for _, n := range u256Cases() {
		t.Run(n.String(), func(t *testing.T) {
			want := expectedU256(n)
			before := n.String()
			got := U256(n)
			if !bytes.Equal(got, want) {
				t.Fatalf("U256(%s) = %x, want %x", before, got, want)
			}
			if n.String() != before {
				t.Fatalf("U256 changed its argument from %s to %s", before, n.String())
			}
		})
	}
}

const packBoundsABI = `[
	{"type":"function","name":"take","inputs":[{"name":"n","type":"uint256"}]},
	{"type":"variable","name":"held","inputs":[{"name":"n","type":"int256"}]}
]`

func TestPack_BigIntArgumentUnchanged(t *testing.T) {
	contract := JSONToABIContract(strings.NewReader(packBoundsABI))
	for _, n := range u256Cases() {
		t.Run(n.String(), func(t *testing.T) {
			want := expectedU256(n)
			before := n.String()

			data, err := contract.PackMethod("take", n)
			common.FailIfErr(t, err)
			if !bytes.Equal(data[4:], want) {
				t.Fatalf("PackMethod(%s) encoded %x, want %x", before, data[4:], want)
			}
			if n.String() != before {
				t.Fatalf("PackMethod changed its argument from %s to %s", before, n.String())
			}

			data, err = contract.PackVariable("held", n)
			common.FailIfErr(t, err)
			if !bytes.Equal(data, want) {
				t.Fatalf("PackVariable(%s) encoded %x, want %x", before, data, want)
			}
			if n.String() != before {
				t.Fatalf("PackVariable changed its argument from %s to %s", before, n.String())
			}
		})
	}
}

func TestNewType_FixedBytesWidth(t *testing.T) {
	for _, ok := range []string{"bytes1", "bytes20", "bytes32", "bytes"} {
		if _, err := NewType(ok); err != nil {
			t.Fatalf("NewType(%q): %v", ok, err)
		}
	}
	for _, bad := range []string{"bytes0", "bytes33", "bytes64", "bytes1000"} {
		if typ, err := NewType(bad); err == nil {
			t.Fatalf("NewType(%q) accepted a fixed-byte width of %d; one ABI word holds %d", bad, typ.Size, WordSize)
		}
	}
}

func TestFixedBytes_RoundTrip(t *testing.T) {
	typ, err := NewType("bytes32")
	common.FailIfErr(t, err)
	var in [32]byte
	for i := range in {
		in[i] = byte(i + 1)
	}
	packed, err := typ.pack(reflect.ValueOf(in))
	common.FailIfErr(t, err)
	common.Expect(t, len(packed), WordSize)
	out, err := toGoType(0, typ, packed)
	common.FailIfErr(t, err)
	common.Expect(t, out.([32]byte), in)
}

func fixedBytesType(size int) Type {
	return Type{T: FixedBytesTy, Kind: reflect.Array, Size: size, Type: reflect.ArrayOf(size, reflect.TypeOf(byte(0)))}
}

// A Type built by hand (not through NewType) with a width above one word,
// or above the bytes it was handed, must be refused by the decoder rather
// than read past the word or panic.
func TestReadFixedBytes_WidthAboveOneWordIsAnError(t *testing.T) {
	output := make([]byte, 96)
	for i := range output {
		output[i] = byte(i)
	}

	t.Run("64 wide on a word with spare capacity", func(t *testing.T) {
		// The old code copied the adjacent word.
		if _, err := readFixedBytes(fixedBytesType(64), output[32:64]); err == nil {
			t.Fatal("accepted")
		}
	})
	t.Run("64 wide on an exact-capacity word", func(t *testing.T) {
		// The old code panicked with a slice bound error.
		if _, err := readFixedBytes(fixedBytesType(64), make([]byte, 32)); err == nil {
			t.Fatal("accepted")
		}
	})
	t.Run("33 wide on 64 bytes", func(t *testing.T) {
		// Above the word even though the input is long enough.
		if _, err := readFixedBytes(fixedBytesType(33), output[0:64]); err == nil {
			t.Fatal("accepted")
		}
	})
	t.Run("20 wide on 19 bytes", func(t *testing.T) {
		// Within the word but longer than the input.
		if _, err := readFixedBytes(fixedBytesType(20), output[0:19]); err == nil {
			t.Fatal("accepted")
		}
	})
	t.Run("20 wide on a word", func(t *testing.T) {
		got, err := readFixedBytes(fixedBytesType(20), output[32:64])
		common.FailIfErr(t, err)
		common.Expect(t, got.([20]byte), *(*[20]byte)(output[32:52]))
	})
}

// Short fixed bytes pad on the right to one word; dynamic bytes longer than
// a word carry a length prefix and span as many words as needed.
func TestBytes_Padding(t *testing.T) {
	short, err := NewType("bytes4")
	common.FailIfErr(t, err)
	packed, err := short.pack(reflect.ValueOf([4]byte{1, 2, 3, 4}))
	common.FailIfErr(t, err)
	want := append([]byte{1, 2, 3, 4}, make([]byte, 28)...)
	if !bytes.Equal(packed, want) {
		t.Fatalf("bytes4 packed as %x", packed)
	}

	dynamic, err := NewType("bytes")
	common.FailIfErr(t, err)
	payload := bytes.Repeat([]byte{0xab}, 40)
	packed, err = dynamic.pack(reflect.ValueOf(payload))
	common.FailIfErr(t, err)
	common.Expect(t, len(packed), WordSize+2*WordSize) // length word + 40 bytes padded to 64
	common.Expect(t, int(packed[WordSize-1]), 40)
	if !bytes.Equal(packed[WordSize:WordSize+40], payload) {
		t.Fatalf("dynamic bytes payload not preserved: %x", packed[WordSize:])
	}
}

func TestNewType_ArrayBracketGrammar(t *testing.T) {
	accepted := []struct {
		decl string
		kind byte
		size int
	}{
		{"uint8[5]", ArrayTy, 5},
		{"uint8[]", SliceTy, 0},
		{"address[2]", ArrayTy, 2},
		{"bytes32[]", SliceTy, 0},
		{"uint8[0005]", ArrayTy, 5},
		{"bytes[]", SliceTy, 0},
		{"bytes[2]", ArrayTy, 2},
	}
	for _, c := range accepted {
		typ, err := NewType(c.decl)
		if err != nil {
			t.Fatalf("NewType(%q): %v", c.decl, err)
		}
		if typ.T != c.kind || typ.Size != c.size {
			t.Fatalf("NewType(%q) = kind %d size %d, want kind %d size %d", c.decl, typ.T, typ.Size, c.kind, c.size)
		}
	}
	rejected := []string{
		"uint8[-5]", "uint8[5x]", "uint8[ 5]", "uint8[5 ]", "uint8[0x5]", "uint8[1e3]",
		"uint8[abc]", "uint8[5]trailing", "uint8[]trailing",
	}
	for _, bad := range rejected {
		if typ, err := NewType(bad); err == nil {
			t.Fatalf("NewType(%q) accepted a malformed array suffix as kind %d size %d", bad, typ.T, typ.Size)
		}
	}
}
