package implementation

import (
	"math/big"
	"testing"

	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/vm/constants"
	"github.com/zenon-network/go-zenon/vm/embedded/definition"
)

var (
	defaultToken = definition.IssueParam{
		TokenName:   "token-Nam3",
		TokenSymbol: "SYMBL",
		TokenDomain: "",
		TotalSupply: big.NewInt(0),
		MaxSupply:   big.NewInt(100),
		Decimals:    0,
		IsMintable:  true,
		IsBurnable:  true,
		IsUtility:   false,
	}
)

func TestToken_Amounts(t *testing.T) {
	token := defaultToken

	// too much total supply
	token.TotalSupply = big.NewInt(101)
	common.ExpectError(t, checkToken(token), constants.ErrTokenInvalidAmount)

	// 0 max supply
	token.TotalSupply = big.NewInt(0)
	token.MaxSupply = big.NewInt(0)
	common.ExpectError(t, checkToken(token), constants.ErrTokenInvalidAmount)

	// mintable false
	token.IsMintable = false
	token.TotalSupply = big.NewInt(9)
	token.MaxSupply = big.NewInt(10)
	common.ExpectError(t, checkToken(token), constants.ErrTokenInvalidAmount)

	// allow inputs bigger than JS's Number.MAX_SAFE_INTEGER
	token.IsMintable = true
	token.TotalSupply = common.Big0
	token.MaxSupply = big.NewInt(9007199254740991)
	common.FailIfErr(t, checkToken(token))

	token.MaxSupply = big.NewInt(9007199254740991 + 1)
	common.FailIfErr(t, checkToken(token))
}

func TestToken_URL(t *testing.T) {
	validUrls := []string{
		"zenon.network",
		"bitcoin.org",
		"www.bitcoin.org",
		"bitcoin.co.uk",
		"",
		"vereqwy.londsacxzg.domasdadain.namooooeooeoeo",
		"bi.co",
		"b-----n.co",
	}

	token := defaultToken
	for _, url := range validUrls {
		token.TokenDomain = url
		common.FailIfErr(t, checkToken(token))
	}

	invalidUrls := []string{
		"https://bitcoin.org",
		"bitcoin.org/",
		"bitcoin.o",
		".com",
		"b.com",
		"-bitcoin.com",
		"bitcoin-.com",
	}
	for _, url := range invalidUrls {
		token.TokenDomain = url
		common.ExpectError(t, checkToken(token), constants.ErrTokenInvalidText)
	}
}
