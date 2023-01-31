package embedded

import (
	"github.com/inconshreveable/log15"
	"github.com/zenon-network/go-zenon/chain"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/rpc/api"
	"github.com/zenon-network/go-zenon/vm/embedded/definition"
	"github.com/zenon-network/go-zenon/zenon"
)

type LiquidityApi struct {
	chain chain.Chain
	log   log15.Logger
}

func NewLiquidityApi(z zenon.Zenon) *LiquidityApi {
	return &LiquidityApi{
		chain: z.Chain(),
		log:   common.RPCLogger.New("module", "embedded_liquidity_api"),
	}
}

func (a *LiquidityApi) GetLiquidityInfo() (*definition.LiquidityInfo, error) {
	_, context, err := api.GetFrontierContext(a.chain, types.LiquidityContract)
	if err != nil {
		return nil, err
	}

	liquidityInfo, err := definition.GetLiquidityInfo(context.Storage())
	if err != nil {
		return nil, err
	}

	return liquidityInfo, nil
}
