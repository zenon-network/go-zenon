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

type TokenAPI struct {
	chain chain.Chain
	z     zenon.Zenon
	cs    consensus.Consensus
	log   log15.Logger
}

func NewTokenApi(z zenon.Zenon) *TokenAPI {
	return &TokenAPI{
		chain: z.Chain(),
		z:     z,
		cs:    z.Consensus(),
		log:   common.RPCLogger.New("module", "embedded_token_api"),
	}
}

type TokenList struct {
	Count int          `json:"count"`
	List  []*api.Token `json:"list"`
}

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
