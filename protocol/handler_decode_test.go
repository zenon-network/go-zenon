package protocol

import (
	"testing"

	"github.com/ethereum/go-ethereum/rlp"

	"github.com/zenon-network/go-zenon/chain/nom"
)

// The BlocksMsg and NewBlockMsg handlers dereference the decoded value and its
// Momentum field. They guard against an incomplete message, and that guard is
// only reachable if the decoder can hand back a nil with a nil error.
//
// The decoder does not do that for these types: go-ethereum's makeNilPtrDecoder
// is selected only for fields tagged `rlp:"nil"`, and nom.DetailedMomentum tags
// neither itself nor Momentum that way. These tests pin that behaviour, so a
// dependency bump that changes it fails here rather than in the handlers.

func decodedElementIsComplete(d *nom.DetailedMomentum) bool {
	return d != nil && d.Momentum != nil
}

// Payloads that are structurally plausible but do not describe a whole
// DetailedMomentum. Each must be reported as an error.
var malformedMomentumPayloads = []struct {
	name    string
	payload []byte
}{
	{"empty string", []byte{0x80}},
	{"empty list", []byte{0xC0}},
	{"list holding an empty string", []byte{0xC1, 0x80}},
	{"list holding an empty list", []byte{0xC1, 0xC0}},
	{"two empty lists", []byte{0xC2, 0xC0, 0xC0}},
	{"nested empty lists", []byte{0xC3, 0xC2, 0xC0, 0xC0}},
}

func TestDecodeDetailedMomentum_NeverNilWithoutError(t *testing.T) {
	for _, test := range malformedMomentumPayloads {
		t.Run(test.name, func(t *testing.T) {
			var single *nom.DetailedMomentum
			err := rlp.DecodeBytes(test.payload, &single)
			if err == nil && !decodedElementIsComplete(single) {
				t.Fatalf("decoded an incomplete momentum with no error: %#v", single)
			}

			var batch []*nom.DetailedMomentum
			err = rlp.DecodeBytes(test.payload, &batch)
			if err == nil {
				for i, element := range batch {
					if !decodedElementIsComplete(element) {
						t.Fatalf("decoded an incomplete momentum at index %d with no error", i)
					}
				}
			}
		})
	}
}

// Guards the test above from passing vacuously. The decoder can hand back a nil
// with a nil error, but only for a field that opts in with `rlp:"nil"`. If this
// stops holding, the assertions above no longer distinguish anything.
func TestDecodeDetailedMomentum_NilTagIsWhatEnablesNil(t *testing.T) {
	type inner struct {
		Value uint64
	}
	type outer struct {
		Opted *inner `rlp:"nil"`
	}

	var opted outer
	// A list holding one empty list. The nil encoding follows the pointed-to
	// type's kind, and inner is a struct, so it is an empty list rather than an
	// empty string.
	if err := rlp.DecodeBytes([]byte{0xC1, 0xC0}, &opted); err != nil {
		t.Fatalf("a field tagged rlp:\"nil\" should accept the nil encoding: %v", err)
	}
	if opted.Opted != nil {
		t.Fatal("expected the tagged field to decode to nil")
	}

	type outerUntagged struct {
		Plain *inner
	}
	var untagged outerUntagged
	if err := rlp.DecodeBytes([]byte{0xC1, 0xC0}, &untagged); err == nil {
		t.Fatal("an untagged pointer field should reject the nil encoding, but it was accepted")
	}
}

func TestDecodeDetailedMomentum_ValidRoundTrip(t *testing.T) {
	original := []*nom.DetailedMomentum{
		{Momentum: &nom.Momentum{Height: 7}, AccountBlocks: nil},
		{Momentum: &nom.Momentum{Height: 8}, AccountBlocks: nil},
	}

	encoded, err := rlp.EncodeToBytes(original)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	var decoded []*nom.DetailedMomentum
	if err := rlp.DecodeBytes(encoded, &decoded); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(decoded) != len(original) {
		t.Fatalf("expected %d momentums, got %d", len(original), len(decoded))
	}
	for i, element := range decoded {
		if !decodedElementIsComplete(element) {
			t.Fatalf("round-tripped momentum at index %d is incomplete", i)
		}
		if element.Momentum.Height != original[i].Momentum.Height {
			t.Fatalf("index %d: height %d, want %d", i, element.Momentum.Height, original[i].Momentum.Height)
		}
	}
}
