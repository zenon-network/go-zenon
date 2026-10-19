package api

import (
	"encoding/json"
	"fmt"
	"math/big"
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
