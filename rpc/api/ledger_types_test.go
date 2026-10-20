package api

import (
	"encoding/json"
	"fmt"
	"math/big"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common/types"
)

func TestBalanceInfo_UnmarshalJSON_NullToken(t *testing.T) {
	// "token":null must not panic; TokenInfo should be nil.
	data := `{"token":null,"balance":"100"}`
	var bi BalanceInfo
	require.NotPanics(t, func() {
		err := json.Unmarshal([]byte(data), &bi)
		require.NoError(t, err)
	})
	require.Nil(t, bi.TokenInfo)
	require.NotNil(t, bi.Balance)
}

func TestBalanceInfo_UnmarshalJSON_MissingToken(t *testing.T) {
	// Missing "token" key must not panic; TokenInfo should be nil.
	data := `{"balance":"100"}`
	var bi BalanceInfo
	require.NotPanics(t, func() {
		err := json.Unmarshal([]byte(data), &bi)
		require.NoError(t, err)
	})
	require.Nil(t, bi.TokenInfo)
}

func TestBalanceInfo_UnmarshalJSON_ValidToken(t *testing.T) {
	// A valid token object should still decode correctly.
	data := `{"token":{"name":"Zenon","symbol":"ZNN","domain":"zenon.network","totalSupply":"1000000","decimals":8,"owner":"z1qqdt06lnwz57x38rwlyutcx5wgrtl0ynkfe3kv","tokenStandard":"zts1znnxxxxxxxxxxxxx9z4ulx","maxSupply":"1000000","isBurnable":true,"isMintable":true,"isUtility":true},"balance":"100"}`
	var bi BalanceInfo
	require.NotPanics(t, func() {
		err := json.Unmarshal([]byte(data), &bi)
		require.NoError(t, err)
	})
	require.NotNil(t, bi.TokenInfo)
	require.Equal(t, "Zenon", bi.TokenInfo.TokenName)
	require.Equal(t, "ZNN", bi.TokenInfo.TokenSymbol)
}

// TestBalanceInfo_RoundTrip_NullToken verifies that a BalanceInfo decoded
// from {"token":null,...} can be re-marshalled without panicking, and that
// the null TokenInfo is preserved as null in the output.
func TestBalanceInfo_RoundTrip_NullToken(t *testing.T) {
	data := `{"token":null,"balance":"100"}`
	var bi BalanceInfo
	require.NotPanics(t, func() {
		err := json.Unmarshal([]byte(data), &bi)
		require.NoError(t, err)
	})
	require.Nil(t, bi.TokenInfo)

	// Re-marshal: must not panic and must preserve null token.
	var out []byte
	require.NotPanics(t, func() {
		var err error
		out, err = json.Marshal(&bi)
		require.NoError(t, err)
	})
	var roundTripped map[string]interface{}
	require.NoError(t, json.Unmarshal(out, &roundTripped))
	tokenVal, hasToken := roundTripped["token"]
	require.True(t, hasToken, "token key should be present in output")
	require.Nil(t, tokenVal, "token should be null in output")
}

func TestAccountBlockList_UnmarshalJSON_NullElement(t *testing.T) {
	// A null element in the list must be skipped, not panic.
	data := `{"list":[null],"count":0,"more":false}`
	var abl AccountBlockList
	require.NotPanics(t, func() {
		err := json.Unmarshal([]byte(data), &abl)
		require.NoError(t, err)
	})
	require.Len(t, abl.List, 0)
}

func TestAccountBlockList_UnmarshalJSON_MixedNullAndValid(t *testing.T) {
	// Null elements are skipped; valid elements are decoded.
	// Build a valid block JSON by marshalling a real one.
	original := &AccountBlock{
		AccountBlock: nom.AccountBlock{
			Version:         1,
			ChainIdentifier: 1,
			BlockType:       0,
			Address:         types.ParseAddressPanic("z1qqdt06lnwz57x38rwlyutcx5wgrtl0ynkfe3kv"),
			ToAddress:       types.ParseAddressPanic("z1qqdt06lnwz57x38rwlyutcx5wgrtl0ynkfe3kv"),
			Amount:          big.NewInt(0),
			TokenStandard:   types.ZnnTokenStandard,
		},
	}
	blockJSON, err := original.MarshalJSON()
	require.NoError(t, err)

	// Wrap in a list with a null element before it.
	data := fmt.Sprintf(`{"list":[null,%s],"count":1,"more":false}`, blockJSON)
	var abl AccountBlockList
	require.NotPanics(t, func() {
		err := json.Unmarshal([]byte(data), &abl)
		require.NoError(t, err)
	})
	require.Len(t, abl.List, 1)
	require.Equal(t, uint64(1), abl.List[0].Version)
}

// The RPC block form shares the ledger block's descendant list; a null
// entry is rejected when the request is decoded, before ToLedgerBlock
// copies the block for ledger.publishRawTransaction.
func TestAccountBlock_UnmarshalJSON_NullDescendantIsAnError(t *testing.T) {
	var block AccountBlock
	err := json.Unmarshal([]byte(`{"height":1,"nonce":"0000000000000000","descendantBlocks":[null]}`), &block)
	if err == nil {
		t.Fatal("descendantBlocks:[null] decoded without error")
	}
	if !strings.Contains(err.Error(), "index 0") {
		t.Fatalf("error does not name the index: %v", err)
	}

	err = json.Unmarshal([]byte(`{"height":1,"nonce":"0000000000000000","descendantBlocks":[{"height":2,"nonce":"0000000000000000","descendantBlocks":[null]}]}`), &block)
	if err == nil {
		t.Fatal("nested null descendant decoded without error")
	}

	err = json.Unmarshal([]byte(`{"height":1,"nonce":"0000000000000000","descendantBlocks":[{"height":2,"nonce":"0000000000000000"}]}`), &block)
	if err != nil {
		t.Fatal(err)
	}
	ledger, err := block.ToLedgerBlock()
	if err != nil {
		t.Fatal(err)
	}
	if len(ledger.DescendantBlocks) != 1 || ledger.DescendantBlocks[0].Height != 2 {
		t.Fatalf("descendants lost in ToLedgerBlock: %+v", ledger.DescendantBlocks)
	}

	// A missing field, an empty list and a whole-field null all decode to
	// an allocated empty list.
	for _, text := range []string{`{"height":1,"nonce":"0000000000000000"}`, `{"height":1,"nonce":"0000000000000000","descendantBlocks":[]}`, `{"height":1,"nonce":"0000000000000000","descendantBlocks":null}`} {
		var b AccountBlock
		if err := json.Unmarshal([]byte(text), &b); err != nil {
			t.Fatalf("%s: %v", text, err)
		}
		if b.DescendantBlocks == nil || len(b.DescendantBlocks) != 0 {
			t.Fatalf("%s: descendants = %#v, want an empty list", text, b.DescendantBlocks)
		}
	}
	// A null at a nonzero index names that index.
	err = json.Unmarshal([]byte(`{"height":1,"nonce":"0000000000000000","descendantBlocks":[{"height":2,"nonce":"0000000000000000"},null]}`), &block)
	if err == nil || !strings.Contains(err.Error(), "index 1") {
		t.Fatalf("second null entry: got %v", err)
	}
}

// The paired block and the elements of a block list are decoded through
// a path of their own; a null descendant there must be refused too.
func TestAccountBlock_UnmarshalJSON_NullDescendantInPairedBlockIsAnError(t *testing.T) {
	var block AccountBlock
	paired := `{"height":1,"nonce":"0000000000000000","pairedAccountBlock":{"height":2,"nonce":"0000000000000000","descendantBlocks":[null]}}`
	err := json.Unmarshal([]byte(paired), &block)
	if err == nil {
		t.Fatal("null descendant in the paired block decoded without error")
	}
	if !strings.Contains(err.Error(), "depth 1") || !strings.Contains(err.Error(), "index 0") {
		t.Fatalf("error does not locate the entry: %v", err)
	}

	nested := `{"height":1,"nonce":"0000000000000000","pairedAccountBlock":{"height":2,"nonce":"0000000000000000","pairedAccountBlock":{"height":3,"nonce":"0000000000000000","descendantBlocks":[{"height":4,"nonce":"0000000000000000"},null]}}}`
	err = json.Unmarshal([]byte(nested), &block)
	if err == nil || !strings.Contains(err.Error(), "depth 2") || !strings.Contains(err.Error(), "index 1") {
		t.Fatalf("nested paired block: got %v", err)
	}

	valid := `{"height":1,"nonce":"0000000000000000","pairedAccountBlock":{"height":2,"nonce":"0000000000000000","descendantBlocks":[{"height":3,"nonce":"0000000000000000"}]}}`
	if err := json.Unmarshal([]byte(valid), &block); err != nil {
		t.Fatal(err)
	}
	if block.PairedAccountBlock == nil || len(block.PairedAccountBlock.DescendantBlocks) != 1 {
		t.Fatalf("valid paired descendants lost: %+v", block.PairedAccountBlock)
	}
}

func TestAccountBlockList_UnmarshalJSON_NullDescendantInElementIsAnError(t *testing.T) {
	var list AccountBlockList
	data := `{"list":[null,{"height":1,"nonce":"0000000000000000","descendantBlocks":[null]}],"count":1,"more":false}`
	err := json.Unmarshal([]byte(data), &list)
	if err == nil {
		t.Fatal("null descendant inside a list element decoded without error")
	}
	if !strings.Contains(err.Error(), "list element 1") || !strings.Contains(err.Error(), "index 0") {
		t.Fatalf("error does not locate the entry: %v", err)
	}

	data = `{"list":[{"height":1,"nonce":"0000000000000000","pairedAccountBlock":{"height":2,"nonce":"0000000000000000","descendantBlocks":[null]}}],"count":1,"more":false}`
	err = json.Unmarshal([]byte(data), &list)
	if err == nil || !strings.Contains(err.Error(), "list element 0") || !strings.Contains(err.Error(), "depth 1") {
		t.Fatalf("paired block inside a list element: got %v", err)
	}

	// Null outer elements are still skipped, and valid elements copy.
	data = `{"list":[null,{"height":1,"nonce":"0000000000000000","descendantBlocks":[{"height":2,"nonce":"0000000000000000"}]}],"count":1,"more":false}`
	if err := json.Unmarshal([]byte(data), &list); err != nil {
		t.Fatal(err)
	}
	if len(list.List) != 1 {
		t.Fatalf("list has %d elements, want 1", len(list.List))
	}
	if _, err := list.List[0].ToLedgerBlock(); err != nil {
		t.Fatal(err)
	}
}
