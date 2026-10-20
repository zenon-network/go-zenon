package abi

import (
	"bytes"
	"math/big"
	"reflect"
	"strings"
	"testing"

	"github.com/zenon-network/go-zenon/common"
)

// expectedU256 is the reference encoding: the value masked to 256 bits
// (two's complement for negatives) and left-padded to one word, computed
// without touching the input.
func expectedU256(n *big.Int) []byte {
	masked := new(big.Int).And(n, common.BigP256m1)
	return PaddedBigBytes(masked, WordSize)
}

func TestU256_DoesNotMutateTheCaller(t *testing.T) {
	overWide := new(big.Int).Lsh(big.NewInt(1), 256)
	overWide.Add(overWide, big.NewInt(7))
	for _, n := range []*big.Int{big.NewInt(-1), overWide, big.NewInt(0), big.NewInt(5)} {
		before := n.String()
		got := U256(n)
		if !bytes.Equal(got, expectedU256(new(big.Int).Set(n))) {
			t.Fatalf("U256(%s) = %x, want %x", before, got, expectedU256(new(big.Int).Set(n)))
		}
		if n.String() != before {
			t.Fatalf("U256 changed its argument from %s to %s", before, n.String())
		}
	}
}

const packBoundsABI = `[
	{"type":"function","name":"take","inputs":[{"name":"n","type":"uint256"}]},
	{"type":"variable","name":"held","inputs":[{"name":"n","type":"int256"}]}
]`

func TestPack_BigIntArgumentUnchanged(t *testing.T) {
	contract := JSONToABIContract(strings.NewReader(packBoundsABI))
	overWide := new(big.Int).Lsh(big.NewInt(1), 256)
	overWide.Add(overWide, big.NewInt(7))
	for _, n := range []*big.Int{big.NewInt(-1), overWide, big.NewInt(0), big.NewInt(5)} {
		before := n.String()
		want := expectedU256(new(big.Int).Set(n))

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

// A Type built by hand (not through NewType) with a width above one word
// must be refused by the decoder rather than read past the word or panic.
func TestReadFixedBytes_WidthAboveOneWordIsAnError(t *testing.T) {
	wide := Type{T: FixedBytesTy, Kind: reflect.Array, Size: 64, Type: reflect.ArrayOf(64, reflect.TypeOf(byte(0)))}

	// Backing array larger than the word: the old code copied the adjacent word.
	output := make([]byte, 96)
	for i := range output {
		output[i] = byte(i)
	}
	word := output[32:64]
	if _, err := readFixedBytes(wide, word); err == nil {
		t.Fatalf("readFixedBytes accepted a 64-byte type on a 32-byte word with spare capacity")
	}

	// Exact-capacity word: the old code panicked with a slice bound error.
	exact := make([]byte, 32)
	if _, err := readFixedBytes(wide, exact); err == nil {
		t.Fatalf("readFixedBytes accepted a 64-byte type on an exact 32-byte word")
	}
}

func TestNewType_ArrayBracketGrammar(t *testing.T) {
	for _, ok := range []string{"uint8[5]", "uint8[]", "address[2]", "bytes32[]"} {
		if _, err := NewType(ok); err != nil {
			t.Fatalf("NewType(%q): %v", ok, err)
		}
	}
	for _, bad := range []string{"uint8[-5]", "uint8[5x]", "uint8[ 5]", "uint8[5 ]", "uint8[0x5]", "uint8[1e3]"} {
		if typ, err := NewType(bad); err == nil {
			t.Fatalf("NewType(%q) accepted a malformed array suffix as size %d", bad, typ.Size)
		}
	}
}
