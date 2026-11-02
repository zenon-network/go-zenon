package nom

import (
	"encoding/json"
	"errors"
	"runtime"
	"strings"
	"testing"
	"time"

	"google.golang.org/protobuf/encoding/protowire"

	"github.com/zenon-network/go-zenon/common"
)

// nestedDescendantsJSON is a block with a single chain of descendants
// depth levels deep, every level a valid minimal block.
func nestedDescendantsJSON(depth int) string {
	var b strings.Builder
	for i := 0; i < depth; i++ {
		b.WriteString(`{"height":1,"nonce":"0000000000000000","descendantBlocks":[`)
	}
	b.WriteString(`{"height":1,"nonce":"0000000000000000"}`)
	for i := 0; i < depth; i++ {
		b.WriteString(`]}`)
	}
	return b.String()
}

// largestAmountDigits is the decimal length of 2^255-1, the largest
// amount the verifier admits (and constants.TokenMaxSupplyBig).
var largestAmountDigits = len(common.BigP255m1.String())

// The amount string is parsed by big.Int.SetString, whose cost grows
// faster than the string: a string longer than the largest valid
// amount is refused before parsing, at every level of the descendant
// tree, and the longest valid one still decodes.
func TestAccountBlock_UnmarshalJSON_AmountLengthIsBounded(t *testing.T) {
	longest := strings.Repeat("9", largestAmountDigits)
	block, err := decodeBlockJSON(t, `{"height":1,"nonce":"0000000000000000","amount":"`+longest+`"}`)
	common.FailIfErr(t, err)
	if block.Amount.String() != longest {
		t.Fatalf("longest valid amount decoded to %s", block.Amount)
	}

	tooLong := longest + "9"
	_, err = decodeBlockJSON(t, `{"height":1,"nonce":"0000000000000000","amount":"`+tooLong+`"}`)
	if err == nil || !strings.Contains(err.Error(), "amount") {
		t.Fatalf("%d-character amount: err = %v, want an amount error", len(tooLong), err)
	}

	_, err = decodeBlockJSON(t, `{"height":1,"nonce":"0000000000000000","descendantBlocks":[{"height":2,"nonce":"0000000000000000"},{"height":3,"nonce":"0000000000000000","amount":"`+tooLong+`"}]}`)
	if err == nil || !strings.Contains(err.Error(), "index 1") || !strings.Contains(err.Error(), "amount") {
		t.Fatalf("oversized amount in a descendant: err = %v, want an amount error naming index 1", err)
	}

	// Rejection costs a scan of the input, not a parse: 2 MiB of digits
	// took about two seconds to parse before the bound existed (and
	// about 50 ms to refuse afterwards with the race detector on).
	digits := strings.Repeat("7", 2<<20)
	start := time.Now()
	_, err = decodeBlockJSON(t, `{"height":1,"nonce":"0000000000000000","amount":"`+digits+`"}`)
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("2 MiB amount decoded without error")
	}
	if elapsed > time.Second {
		t.Fatalf("rejecting a 2 MiB amount took %v, want well under 1s", elapsed)
	}
}

// A descendant tree is decoded in one pass: before, every level handed
// its raw suffix to a nested decoder, so a chain of n descendants cost
// n parses of a shrinking tail. Eight chains of 4000 took eight seconds
// on the unbounded decoder (three minutes with the race detector on)
// and about 0.3 s afterwards; the bound is generous so a slow runner
// does not trip it. Input nested past what the decoder admits is an
// error, not a crash.
func TestAccountBlock_UnmarshalJSON_DescendantDecodeIsLinear(t *testing.T) {
	chain := nestedDescendantsJSON(4000)
	wide := `{"height":1,"nonce":"0000000000000000","descendantBlocks":[` + strings.Repeat(chain+",", 7) + chain + `]}`
	start := time.Now()
	block, err := decodeBlockJSON(t, wide)
	elapsed := time.Since(start)
	common.FailIfErr(t, err)
	if elapsed > 5*time.Second {
		t.Fatalf("decoding %d bytes of nested descendants took %v, want well under 5s", len(wide), elapsed)
	}
	if len(block.DescendantBlocks) != 8 {
		t.Fatalf("decoded %d top-level descendants, want 8", len(block.DescendantBlocks))
	}
	cur := block.DescendantBlocks[7]
	for level := 1; level < 4000; level++ {
		if len(cur.DescendantBlocks) != 1 {
			t.Fatalf("level %d has %d descendants, want 1", level, len(cur.DescendantBlocks))
		}
		cur = cur.DescendantBlocks[0]
	}
	if len(cur.DescendantBlocks) != 1 || len(cur.DescendantBlocks[0].DescendantBlocks) != 0 {
		t.Fatalf("chain does not end in a leaf at level 4000")
	}

	deeper := nestedDescendantsJSON(6000)
	if _, err := decodeBlockJSON(t, deeper); err == nil {
		t.Fatal("6000 nested descendants decoded without error")
	}
}

// nestedDescendantsWithLeafJSON is nestedDescendantsJSON with the leaf
// block replaced by leaf.
func nestedDescendantsWithLeafJSON(depth int, leaf string) string {
	var b strings.Builder
	for i := 0; i < depth; i++ {
		b.WriteString(`{"height":1,"nonce":"0000000000000000","descendantBlocks":[`)
	}
	b.WriteString(leaf)
	for i := 0; i < depth; i++ {
		b.WriteString(`]}`)
	}
	return b.String()
}

// Refusing a bad block deep in a chain must cost no more than reaching
// it: locating the failure once by its index path instead of re-wrapping
// the error at every level, which copied the growing message once per
// level (about 600 MiB of allocation for a 240 KB input before). The
// message names the path's ends and elides its middle, so it stays
// small.
func TestAccountBlock_UnmarshalJSON_DeepRejectionIsLinear(t *testing.T) {
	tooLong := strings.Repeat("9", largestAmountDigits+1)
	leaves := map[string]string{
		"oversized amount": `{"height":1,"nonce":"0000000000000000","amount":"` + tooLong + `"}`,
		"invalid nonce":    `{"height":1,"nonce":"zz"}`,
		"null entry":       `null`,
	}
	for name, leaf := range leaves {
		text := nestedDescendantsWithLeafJSON(4900, leaf)
		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)
		start := time.Now()
		_, err := decodeBlockJSON(t, text)
		elapsed := time.Since(start)
		runtime.ReadMemStats(&after)
		if err == nil {
			t.Fatalf("%s at depth 4900 decoded without error", name)
		}
		if allocated := after.TotalAlloc - before.TotalAlloc; allocated > 64<<20 {
			t.Fatalf("%s at depth 4900: rejecting %d bytes allocated %d bytes, want well under 64 MiB", name, len(text), allocated)
		}
		if elapsed > 5*time.Second {
			t.Fatalf("%s at depth 4900: rejecting %d bytes took %v, want well under 5s", name, len(text), elapsed)
		}
		if len(err.Error()) > 1024 {
			t.Fatalf("%s at depth 4900: error message is %d bytes: %.200s...", name, len(err.Error()), err.Error())
		}
		var located *DescendantError
		if !errors.As(err, &located) {
			t.Fatalf("%s at depth 4900: error is not a DescendantError: %v", name, err)
		}
		wantDepth := 4900
		if name == "null entry" {
			// The null is reported by its parent, the block at depth 4899.
			wantDepth = 4899
		}
		if len(located.Path) != wantDepth {
			t.Fatalf("%s: path has %d levels, want %d", name, len(located.Path), wantDepth)
		}
		if !strings.Contains(err.Error(), "more levels") {
			t.Fatalf("%s: long path is not elided: %.200s", name, err.Error())
		}
	}

	// A short path names every level, root first.
	_, err := decodeBlockJSON(t, `{"nonce":"0000000000000000","descendantBlocks":[{"nonce":"0000000000000000"},{"nonce":"0000000000000000","descendantBlocks":[{"nonce":"0000000000000000","amount":"`+tooLong+`"}]}]}`)
	if err == nil || !strings.HasPrefix(err.Error(), "descendant block at index 1: descendant block at index 0: amount string") {
		t.Fatalf("short path: %v", err)
	}
}

// encoding/json decodes a repeated key into the same value: a later
// descendantBlocks list reuses the earlier list's elements field by
// field. The nested decoder used to start each element afresh and so
// refused a later entry without a nonce; the one-pass decoder sees the
// merged element, which is what the JSON package defines for any struct
// in this tree. Pinned so the behaviour is deliberate, not accidental.
func TestAccountBlock_UnmarshalJSON_RepeatedDescendantKeyMerges(t *testing.T) {
	block, err := decodeBlockJSON(t, `{"nonce":"0000000000000000","descendantBlocks":[{"height":7,"nonce":"0000000000000000"}],"descendantBlocks":[{"height":8}]}`)
	common.FailIfErr(t, err)
	if len(block.DescendantBlocks) != 1 || block.DescendantBlocks[0].Height != 8 {
		t.Fatalf("later list did not win: %+v", block.DescendantBlocks)
	}
	// A later list that is longer than the earlier one gets fresh
	// elements past its end, which are checked as usual.
	_, err = decodeBlockJSON(t, `{"nonce":"0000000000000000","descendantBlocks":[{"height":7,"nonce":"0000000000000000"}],"descendantBlocks":[{"height":8},{"height":9}]}`)
	if err == nil || !strings.Contains(err.Error(), "index 1") || !strings.Contains(err.Error(), "nonce") {
		t.Fatalf("fresh element without a nonce: err = %v, want a nonce error naming index 1", err)
	}
}

// A descendant with an invalid or missing nonce was refused by the
// nested decoder; the one-pass decoder keeps refusing it, naming the
// index, while the root keeps its own nonce error.
func TestAccountBlock_UnmarshalJSON_DescendantNonceIsStillChecked(t *testing.T) {
	_, err := decodeBlockJSON(t, `{"height":1,"nonce":"0000000000000000","descendantBlocks":[{"height":2,"nonce":"zz"}]}`)
	if err == nil || !strings.Contains(err.Error(), "nonce") || !strings.Contains(err.Error(), "index 0") {
		t.Fatalf("invalid descendant nonce: err = %v, want a nonce error naming index 0", err)
	}
	_, err = decodeBlockJSON(t, `{"height":1,"nonce":"0000000000000000","descendantBlocks":[{"height":2,"nonce":"0000000000000000"},{"height":3}]}`)
	if err == nil || !strings.Contains(err.Error(), "nonce") || !strings.Contains(err.Error(), "index 1") {
		t.Fatalf("missing descendant nonce: err = %v, want a nonce error naming index 1", err)
	}
	_, err = decodeBlockJSON(t, `{"height":1,"nonce":"zz"}`)
	if err == nil || !strings.Contains(err.Error(), "nonce") {
		t.Fatalf("invalid root nonce: err = %v, want a nonce error", err)
	}
}

// The one-pass wire form round-trips a nested tree byte for byte.
func TestAccountBlock_MarshalJSON_NestedDescendantsRoundTrip(t *testing.T) {
	text := nestedDescendantsJSON(3)
	block, err := decodeBlockJSON(t, text)
	common.FailIfErr(t, err)
	encoded, err := json.Marshal(block)
	common.FailIfErr(t, err)
	again, err := decodeBlockJSON(t, string(encoded))
	common.FailIfErr(t, err)
	reencoded, err := json.Marshal(again)
	common.FailIfErr(t, err)
	if string(encoded) != string(reencoded) {
		t.Fatalf("round trip differs:\n%s\n%s", encoded, reencoded)
	}
	if again.DescendantBlocks[0].DescendantBlocks[0].DescendantBlocks[0].Height != 1 {
		t.Fatal("nested descendants lost in the round trip")
	}
}

// marshalChain builds, in memory, a wire-form block with depth levels of
// single descendants below it.
func marshalChain(depth int) *AccountBlockMarshal {
	leaf := &AccountBlockMarshal{Nonce: "0000000000000000"}
	cur := leaf
	for i := 0; i < depth; i++ {
		cur = &AccountBlockMarshal{Nonce: "0000000000000000", DescendantBlocks: []*AccountBlockMarshal{cur}}
	}
	return cur
}

// The descendant depth bound is a ceiling set to the protobuf decoder's
// recursion limit for stored blocks. encoding/json refuses deeper input
// on its own before Check sees it, so the bound is pinned on the wire
// form directly: a chain at the bound passes, one level more is refused
// with the depth named.
func TestAccountBlockMarshal_Check_DescendantDepthIsBounded(t *testing.T) {
	if err := marshalChain(MaxDescendantDepth).Check(); err != nil {
		t.Fatalf("%d levels of descendants rejected: %v", MaxDescendantDepth, err)
	}
	err := marshalChain(MaxDescendantDepth + 1).Check()
	if err == nil {
		t.Fatalf("%d levels of descendants accepted", MaxDescendantDepth+1)
	}
	if !strings.Contains(err.Error(), "nested deeper than") {
		t.Fatalf("error is not the depth bound: %v", err)
	}

	// The bound is the recursion limit the protobuf decoder applies to
	// stored blocks, and the digit bound is the length of the largest
	// valid amount; neither is a literal of its own.
	if MaxDescendantDepth != protowire.DefaultRecursionLimit {
		t.Fatalf("MaxDescendantDepth = %d, want protowire.DefaultRecursionLimit (%d)", MaxDescendantDepth, protowire.DefaultRecursionLimit)
	}
	if MaxAmountDigits != len(common.BigP255m1.String()) || MaxAmountDigits != 77 {
		t.Fatalf("MaxAmountDigits = %d, want the %d digits of 2^255-1", MaxAmountDigits, len(common.BigP255m1.String()))
	}
}

// A nil entry in DescendantBlocks is not a valid block, but encoding one
// must behave as it did before descendants were converted on the way
// out: the entry is preserved as JSON null (which the decoders then
// refuse by index), never dereferenced. Both the immediate nil and one
// below a non-nil child, and the same through FromNomMarshalJson.
func TestAccountBlock_MarshalJSON_PreservesNilDescendantEntries(t *testing.T) {
	cases := []struct {
		name  string
		block *AccountBlock
		want  string
		null  string // the index path of the null, for the decoder's error
	}{
		{
			name:  "immediate nil",
			block: &AccountBlock{Height: 1, DescendantBlocks: []*AccountBlock{nil}},
			want:  `"descendantBlocks":[null]`,
			null:  "index 0",
		},
		{
			name: "nil below a non-nil child",
			block: &AccountBlock{Height: 1, DescendantBlocks: []*AccountBlock{
				{Height: 2, DescendantBlocks: []*AccountBlock{{Height: 3}, nil}},
			}},
			want: `"height":3,`,
			null: "index 0: descendant block at index 1 is null",
		},
	}
	for _, c := range cases {
		encoded, err := json.Marshal(c.block)
		if err != nil {
			t.Fatalf("%s: marshal: %v", c.name, err)
		}
		if !strings.Contains(string(encoded), c.want) || !strings.Contains(string(encoded), "null]") {
			t.Fatalf("%s: encoded %.120s..., want it to contain %q and a null list entry", c.name, encoded, c.want)
		}
		// The wire form keeps the nil as before, and the decoder refuses
		// it by index as before.
		wire := c.block.ToNomMarshalJson()
		if len(wire.DescendantBlocks) != len(c.block.DescendantBlocks) {
			t.Fatalf("%s: wire form has %d descendants, want %d", c.name, len(wire.DescendantBlocks), len(c.block.DescendantBlocks))
		}
		if _, err := decodeBlockJSON(t, string(encoded)); err == nil || !strings.Contains(err.Error(), c.null) {
			t.Fatalf("%s: decoding the encoded form: err = %v, want a null error at %q", c.name, err, c.null)
		}
		// Converting a wire form that carries a nil keeps the nil too.
		back := wire.FromNomMarshalJson()
		if len(back.DescendantBlocks) != len(wire.DescendantBlocks) {
			t.Fatalf("%s: FromNomMarshalJson has %d descendants, want %d", c.name, len(back.DescendantBlocks), len(wire.DescendantBlocks))
		}
	}
}
