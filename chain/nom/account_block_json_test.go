package nom

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/zenon-network/go-zenon/common"
)

func decodeBlockJSON(t *testing.T, text string) (*AccountBlock, error) {
	t.Helper()
	block := new(AccountBlock)
	err := json.Unmarshal([]byte(text), block)
	return block, err
}

// A JSON descendant list that contains null must be rejected at decode
// time with the index named; otherwise the nil reaches Copy and every
// other consumer that dereferences descendants.
func TestAccountBlock_UnmarshalJSON_NullDescendantIsAnError(t *testing.T) {
	_, err := decodeBlockJSON(t, `{"nonce":"0000000000000000","descendantBlocks":[null]}`)
	if err == nil {
		t.Fatal("descendantBlocks:[null] decoded without error")
	}
	if !strings.Contains(err.Error(), "index 0") {
		t.Fatalf("error does not name the index: %v", err)
	}

	_, err = decodeBlockJSON(t, `{"descendantBlocks":[{"height":1,"nonce":"0000000000000000"},null]}`)
	if err == nil || !strings.Contains(err.Error(), "index 1") {
		t.Fatalf("second null entry: got %v", err)
	}

	// Nested: a null inside a descendant's own list.
	_, err = decodeBlockJSON(t, `{"nonce":"0000000000000000","descendantBlocks":[{"height":1,"nonce":"0000000000000000","descendantBlocks":[null]}]}`)
	if err == nil {
		t.Fatal("nested null descendant decoded without error")
	}
	if !strings.Contains(err.Error(), "descendant block at index 0 is null") {
		t.Fatalf("nested null: error is not the descendant check: %v", err)
	}
}

func TestAccountBlock_UnmarshalJSON_DescendantsRoundTrip(t *testing.T) {
	block, err := decodeBlockJSON(t, `{"height":1,"nonce":"0000000000000000","descendantBlocks":[]}`)
	common.FailIfErr(t, err)
	common.Expect(t, len(block.DescendantBlocks), 0)

	block, err = decodeBlockJSON(t, `{"height":1,"nonce":"0000000000000000","descendantBlocks":[{"height":2,"nonce":"0000000000000000","descendantBlocks":[{"height":3,"nonce":"0000000000000000"}]}]}`)
	common.FailIfErr(t, err)
	common.Expect(t, len(block.DescendantBlocks), 1)
	common.ExpectUint64(t, block.DescendantBlocks[0].Height, 2)
	common.ExpectUint64(t, block.DescendantBlocks[0].DescendantBlocks[0].Height, 3)

	// Everything a decoded block holds can be copied.
	copied := block.Copy()
	common.ExpectUint64(t, copied.DescendantBlocks[0].DescendantBlocks[0].Height, 3)

	// A missing field is an empty list, never nil entries.
	block, err = decodeBlockJSON(t, `{"height":1,"nonce":"0000000000000000"}`)
	common.FailIfErr(t, err)
	common.Expect(t, len(block.DescendantBlocks), 0)
}
