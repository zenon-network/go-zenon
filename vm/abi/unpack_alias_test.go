package abi

import (
	"bytes"
	"encoding/binary"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/zenon-network/go-zenon/common"
)

const aliasABI = `[
	{"type":"function","name":"strings","inputs":[{"name":"v","type":"string[]"}]},
	{"type":"function","name":"blobs","inputs":[{"name":"v","type":"bytes[]"}]},
	{"type":"function","name":"stringsThenN","inputs":[{"name":"v","type":"string[]"},{"name":"n","type":"uint64"}]}
]`

func word(n int) []byte {
	b := make([]byte, WordSize)
	binary.BigEndian.PutUint64(b[WordSize-8:], uint64(n))
	return b
}

func padToWord(b []byte) []byte {
	padded := make([]byte, (len(b)+WordSize-1)/WordSize*WordSize)
	copy(padded, b)
	return padded
}

// aliasedArray encodes a dynamic array of n strings (or byte slices) as
// the single argument of a method, with every element offset naming the
// same payload: the head word (the array starts at 32), the element
// count, n offsets all equal to n*32 (relative to the first offset word,
// that is the word right after the offsets) and one payload, its length
// word followed by the right-padded bytes. With n == 1 this is exactly
// the canonical encoding.
func aliasedArray(n int, payload []byte) []byte {
	var enc []byte
	enc = append(enc, word(WordSize)...)
	enc = append(enc, word(n)...)
	for i := 0; i < n; i++ {
		enc = append(enc, word(n*WordSize)...)
	}
	enc = append(enc, word(len(payload))...)
	enc = append(enc, padToWord(payload)...)
	return enc
}

func callData(contract ABIContract, method string, enc []byte) []byte {
	return append(contract.Methods[method].Id(), enc...)
}

// The fixture's single-element form is byte-for-byte the packer's output,
// so the aliased forms differ from a canonical encoding only in where
// the offsets point.
func TestUnpack_AliasedFixtureMatchesPackerForOneElement(t *testing.T) {
	contract := JSONToABIContract(strings.NewReader(aliasABI))
	payload := []byte(strings.Repeat("z", 45))

	packed, err := contract.PackMethod("strings", []string{string(payload)})
	common.FailIfErr(t, err)
	if want := callData(contract, "strings", aliasedArray(1, payload)); !bytes.Equal(packed, want) {
		t.Fatalf("packer produced %x, fixture %x", packed, want)
	}

	packed, err = contract.PackMethod("blobs", [][]byte{payload})
	common.FailIfErr(t, err)
	if want := callData(contract, "blobs", aliasedArray(1, payload)); !bytes.Equal(packed, want) {
		t.Fatalf("packer produced %x, fixture %x", packed, want)
	}
}

// n offsets onto one payload of P bytes used to decode into n copies, n*P
// bytes from an input of about n*32+P (issue #269). The decoder now stops
// as soon as the lengths it has decoded exceed the input, so the aliased
// message is rejected having allocated a small multiple of its own size.
func TestUnpack_AliasedDynamicElementsAreRejected(t *testing.T) {
	contract := JSONToABIContract(strings.NewReader(aliasABI))
	const n, p = 1000, 32000
	payload := bytes.Repeat([]byte{'q'}, p)

	for _, method := range []string{"strings", "blobs"} {
		t.Run(method, func(t *testing.T) {
			data := callData(contract, method, aliasedArray(n, payload))

			var before, after runtime.MemStats
			runtime.ReadMemStats(&before)
			var decoded interface{}
			var err error
			if method == "strings" {
				var out []string
				err = contract.UnpackMethod(&out, method, data)
				decoded = out
			} else {
				var out [][]byte
				err = contract.UnpackMethod(&out, method, data)
				decoded = out
			}
			runtime.ReadMemStats(&after)

			if err == nil {
				t.Fatalf("decoded %d elements from %d bytes; want an error", reflect.ValueOf(decoded).Len(), len(data))
			}
			if !strings.Contains(err.Error(), "alias") {
				t.Fatalf("unexpected error: %v", err)
			}
			// Before the guard the strings case allocated n*p = 32 MB here.
			if allocated := after.TotalAlloc - before.TotalAlloc; allocated > 8*uint64(len(data)) {
				t.Fatalf("decoding %d bytes allocated %d bytes before failing", len(data), allocated)
			}
		})
	}
}

// The bound is the sum of the decoded lengths against the input, not a
// canonical layout: offsets may alias as long as the elements they name
// add up to no more than the array's bytes. Two elements naming one
// 96-byte payload fill the 192-byte array exactly and decode; a third
// element on the same payload exceeds it and is rejected.
func TestUnpack_AliasedDynamicElementsWithinTheInputDecode(t *testing.T) {
	contract := JSONToABIContract(strings.NewReader(aliasABI))
	payload := []byte(strings.Repeat("y", 96))

	var two []string
	common.FailIfErr(t, contract.UnpackMethod(&two, "strings", callData(contract, "strings", aliasedArray(2, payload))))
	if want := []string{string(payload), string(payload)}; !reflect.DeepEqual(two, want) {
		t.Fatalf("decoded %q, want %q", two, want)
	}

	var three []string
	if err := contract.UnpackMethod(&three, "strings", callData(contract, "strings", aliasedArray(3, payload))); err == nil {
		t.Fatalf("decoded %q from three offsets onto one payload; want an error", three)
	}
}

// Canonical encodings, which the packer produces, round-trip unchanged,
// including an empty array, empty elements and an argument that follows
// the array.
func TestUnpack_CanonicalDynamicArraysRoundTrip(t *testing.T) {
	contract := JSONToABIContract(strings.NewReader(aliasABI))

	for _, in := range [][]string{
		{},
		{""},
		{"a", strings.Repeat("b", 40), "", strings.Repeat("c", 32)},
	} {
		data, err := contract.PackMethod("strings", in)
		common.FailIfErr(t, err)
		var out []string
		common.FailIfErr(t, contract.UnpackMethod(&out, "strings", data))
		if len(in) != 0 && !reflect.DeepEqual(out, in) {
			t.Fatalf("decoded %q, want %q", out, in)
		}
		if len(out) != len(in) {
			t.Fatalf("decoded %d elements, want %d", len(out), len(in))
		}
	}

	blobs := [][]byte{{1}, bytes.Repeat([]byte{2}, 33), {}}
	data, err := contract.PackMethod("blobs", blobs)
	common.FailIfErr(t, err)
	var outBlobs [][]byte
	common.FailIfErr(t, contract.UnpackMethod(&outBlobs, "blobs", data))
	if len(outBlobs) != len(blobs) {
		t.Fatalf("decoded %d blobs, want %d", len(outBlobs), len(blobs))
	}
	for i := range blobs {
		if !bytes.Equal(outBlobs[i], blobs[i]) {
			t.Fatalf("blob %d decoded as %x, want %x", i, outBlobs[i], blobs[i])
		}
	}

	type pair struct {
		V []string
		N uint64
	}
	in := pair{V: []string{"x", strings.Repeat("y", 70)}, N: 7}
	data, err = contract.PackMethod("stringsThenN", in.V, in.N)
	common.FailIfErr(t, err)
	var out pair
	common.FailIfErr(t, contract.UnpackMethod(&out, "stringsThenN", data))
	if !reflect.DeepEqual(out, in) {
		t.Fatalf("decoded %+v, want %+v", out, in)
	}
}
