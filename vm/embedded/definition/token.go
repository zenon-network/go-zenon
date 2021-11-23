package definition

import (
	"math/big"
	"strings"

	"github.com/pkg/errors"

	"github.com/zenon-network/go-zenon/common/db"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/vm/abi"
	"github.com/zenon-network/go-zenon/vm/constants"
)

const (
	jsonToken = `
	[
		{"type":"function","name":"IssueToken","inputs":[{"name":"tokenName","type":"string"},{"name":"tokenSymbol","type":"string"},{"name":"tokenDomain","type":"string"},{"name":"totalSupply","type":"uint256"},{"name":"maxSupply","type":"uint256"},{"name":"decimals","type":"uint8"},{"name":"isMintable","type":"bool"},{"name":"isBurnable","type":"bool"},{"name":"isUtility","type":"bool"}]},
		{"type":"function","name":"Mint","inputs":[{"name":"tokenStandard","type":"tokenStandard"},{"name":"amount","type":"uint256"},{"name":"receiveAddress","type":"address"}]},
		{"type":"function","name":"Burn","inputs":[]},
		{"type":"function","name":"UpdateToken","inputs":[{"name":"tokenStandard","type":"tokenStandard"},{"name":"owner","type":"address"},{"name":"isMintable","type":"bool"},{"name":"isBurnable","type":"bool"}]},

		{"type":"variable","name":"tokenInfo","inputs":[
			{"name":"owner","type":"address"},
			{"name":"tokenName","type":"string"},
			{"name":"tokenSymbol","type":"string"},
			{"name":"tokenDomain","type":"string"},
			{"name":"totalSupply","type":"uint256"},
			{"name":"maxSupply","type":"uint256"},
			{"name":"decimals","type":"uint8"},
			{"name":"isMintable","type":"bool"},
			{"name":"isBurnable","type":"bool"},
			{"name":"isUtility","type":"bool"}]}
	]`

	IssueMethodName       = "IssueToken"
	MintMethodName        = "Mint"
	BurnMethodName        = "Burn"
	UpdateTokenMethodName = "UpdateToken"

	tokenInfoVariableName = "tokenInfo"
)

var (
	// ABIToken is abi definition of token contract
	ABIToken = abi.JSONToABIContract(strings.NewReader(jsonToken))

	tokenInfoKeyPrefix = []byte{1}
)

type IssueParam struct {
	TokenName   string
	TokenSymbol string
	TokenDomain string
	TotalSupply *big.Int
	MaxSupply   *big.Int
	Decimals    uint8
	IsMintable  bool
	IsBurnable  bool
	IsUtility   bool
}
type MintParam struct {
	TokenStandard  types.ZenonTokenStandard
	Amount         *big.Int
	ReceiveAddress types.Address
}
type UpdateTokenParam struct {
	TokenStandard types.ZenonTokenStandard
	Owner         types.Address
	IsMintable    bool
	IsBurnable    bool
}

type TokenInfo struct {
	Owner       types.Address `json:"owner"`
	TokenName   string        `json:"tokenName"`
	TokenSymbol string        `json:"tokenSymbol"`
	TokenDomain string        `json:"tokenDomain"`
	TotalSupply *big.Int      `json:"totalSupply"`
	MaxSupply   *big.Int      `json:"maxSupply"`
	Decimals    uint8         `json:"decimals"`
	IsMintable  bool          `json:"isMintable"`
	// IsBurnable = true implies that anyone can burn the token.
	// The Owner can burn the token even if IsBurnable = false.
	IsBurnable bool `json:"isBurnable"`
	IsUtility  bool `json:"isUtility"`

	TokenStandard types.ZenonTokenStandard `json:"tokenStandard"`
}

func (token *TokenInfo) Save(context db.DB) error {
	data, err := ABIToken.PackVariable(
		tokenInfoVariableName,
		token.Owner,
		token.TokenName,
		token.TokenSymbol,
		token.TokenDomain,
		token.TotalSupply,
		token.MaxSupply,
		token.Decimals,
		token.IsMintable,
		token.IsBurnable,
		token.IsUtility,
	)
	if err != nil {
		return err
	}
	return context.Put(
		getTokenInfoKey(token.TokenStandard),
		data,
	)
}

func getTokenInfoKey(ts types.ZenonTokenStandard) []byte {
	return append(tokenInfoKeyPrefix, ts.Bytes()...)
}
func isTokenInfoKey(key []byte) bool {
	return key[0] == tokenInfoKeyPrefix[0]
}
func unmarshalTokenInfoKey(key []byte) (*types.ZenonTokenStandard, error) {
	if !isTokenInfoKey(key) {
		return nil, errors.Errorf("invalid key! Not token info key")
	}
	tokenStandard := new(types.ZenonTokenStandard)
	if err := tokenStandard.SetBytes(key[1:]); err != nil {
		return nil, err
	}
	return tokenStandard, nil
}
func parseTokenInfo(key, data []byte) (*TokenInfo, error) {
	if len(data) > 0 {
		tokenStandard, err := unmarshalTokenInfoKey(key)
		if err != nil {
			return nil, err
		}
		tokenInfo := new(TokenInfo)
		tokenInfo.TokenStandard = *tokenStandard
		if err := ABIToken.UnpackVariable(tokenInfo, tokenInfoVariableName, data); err != nil {
			return nil, err
		}
		return tokenInfo, err
	} else {
		return nil, constants.ErrDataNonExistent
	}
}
func GetTokenInfo(context db.DB, ts types.ZenonTokenStandard) (*TokenInfo, error) {
	key := getTokenInfoKey(ts)
	if data, err := context.Get(key); err != nil {
		return nil, err
	} else {
		return parseTokenInfo(key, data)
	}
}
func GetTokenInfoList(context db.DB) ([]*TokenInfo, error) {
	iterator := context.NewIterator(tokenInfoKeyPrefix)
	defer iterator.Release()
	tokenInfoList := make([]*TokenInfo, 0)
	for {
		if !iterator.Next() {
			if iterator.Error() != nil {
				return nil, iterator.Error()
			}
			break
		}

		if tokenInfo, err := parseTokenInfo(iterator.Key(), iterator.Value()); err == nil {
			tokenInfoList = append(tokenInfoList, tokenInfo)
		} else if err == constants.ErrDataNonExistent {
			continue
		} else {
			return nil, err
		}
	}
	return tokenInfoList, nil
}
