package embedded

import (
	"github.com/inconshreveable/log15"

	"github.com/zenon-network/go-zenon/chain"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/consensus"
	"github.com/zenon-network/go-zenon/rpc/api"
	"github.com/zenon-network/go-zenon/vm/constants"
	"github.com/zenon-network/go-zenon/vm/embedded/definition"
	"github.com/zenon-network/go-zenon/zenon"
)

// TokenAPI implements the "embedded.token" JSON-RPC namespace, which
// reads the registry of ZTS tokens from the token embedded contract as
// of the frontier momentum. Each entry describes a token issued through
// the contract's IssueToken method (which costs exactly 1 ZNN), keyed
// by its ZTS identifier, with its supplies in smallest units and its
// owner-controlled flags: only the owner may mint (and only while
// IsMintable) or change the flags via UpdateToken, while IsBurnable
// true lets anyone burn the token (the owner can burn it regardless).
// Every exported method is served as
// embedded.token.<lowerCamelMethodName>.
type TokenAPI struct {
	chain chain.Chain
	z     zenon.Zenon
	cs    consensus.Consensus
	log   log15.Logger
}

// NewTokenApi returns a TokenAPI bound to the given node's chain. It is
// called by the RPC server when the "embedded" namespace is enabled; it
// is not itself an RPC method.
func NewTokenApi(z zenon.Zenon) *TokenAPI {
	return &TokenAPI{
		chain: z.Chain(),
		z:     z,
		cs:    z.Consensus(),
		log:   common.RPCLogger.New("module", "embedded_token_api"),
	}
}

// TokenList is one page of tokens. Count is the total number of tokens
// that matched the producing method's filter, not the number of entries
// in List.
type TokenList struct {
	Count int          `json:"count"`
	List  []*api.Token `json:"list"`
}

// GetAll returns one page of all ZTS tokens registered in the token
// contract at the frontier momentum, in storage (ZTS identifier) order.
// Count is the total number of registered tokens. A pageSize above 1024
// is rejected with api.ErrPageSizeParamTooBig.
//
// JSON-RPC: embedded.token.getAll
func (a *TokenAPI) GetAll(pageIndex, pageSize uint32) (*TokenList, error) {
	if pageSize > api.RpcMaxPageSize {
		return nil, api.ErrPageSizeParamTooBig
	}

	_, context, err := api.GetFrontierContext(a.chain, types.TokenContract)
	if err != nil {
		return nil, err
	}
	tokenListRaw, err := definition.GetTokenInfoList(context.Storage())
	if err != nil {
		return nil, err
	}
	tokenList := api.LedgerTokenInfosToRpc(tokenListRaw)
	start, end := api.GetRange(pageIndex, pageSize, uint32(len(tokenList)))
	return &TokenList{
		Count: len(tokenList),
		List:  tokenList[start:end],
	}, nil
}

// GetByOwner returns one page of the tokens whose current owner is the
// given address, filtered from the full registry at the frontier
// momentum and paged after filtering, preserving storage (ZTS
// identifier) order. Count is the total number of tokens owned by the
// address. A pageSize above 1024 is rejected with
// api.ErrPageSizeParamTooBig.
//
// JSON-RPC: embedded.token.getByOwner
func (a *TokenAPI) GetByOwner(owner types.Address, pageIndex, pageSize uint32) (*TokenList, error) {
	if pageSize > api.RpcMaxPageSize {
		return nil, api.ErrPageSizeParamTooBig
	}

	_, context, err := api.GetFrontierContext(a.chain, types.TokenContract)
	if err != nil {
		return nil, err
	}
	tokenListRaw, err := definition.GetTokenInfoList(context.Storage())
	if err != nil {
		return nil, err
	}
	tokenListUnfiltered := api.LedgerTokenInfosToRpc(tokenListRaw)

	tokenList := make([]*api.Token, 0)
	for _, tokenInfo := range tokenListUnfiltered {
		if tokenInfo.Owner == owner {
			tokenList = append(tokenList, tokenInfo)
		}
	}

	start, end := api.GetRange(pageIndex, pageSize, uint32(len(tokenList)))
	return &TokenList{
		Count: len(tokenList),
		List:  tokenList[start:end],
	}, nil
}

// GetByZts returns the token registered under the given ZTS identifier
// at the frontier momentum, or nil without an error when no such token
// exists.
//
// JSON-RPC: embedded.token.getByZts
func (a *TokenAPI) GetByZts(zts types.ZenonTokenStandard) (*api.Token, error) {
	_, context, err := api.GetFrontierContext(a.chain, types.TokenContract)
	if err != nil {
		return nil, err
	}
	tokenInfo, err := definition.GetTokenInfo(context.Storage(), zts)
	if err == constants.ErrDataNonExistent {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if tokenInfo != nil {
		return api.LedgerTokenInfoToRpc(tokenInfo), nil
	}
	return nil, nil
}
