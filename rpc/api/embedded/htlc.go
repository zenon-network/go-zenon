package embedded

import (
	"github.com/inconshreveable/log15"

	"github.com/zenon-network/go-zenon/chain"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/consensus"
	"github.com/zenon-network/go-zenon/rpc/api"
	"github.com/zenon-network/go-zenon/vm/embedded/definition"
	"github.com/zenon-network/go-zenon/vm/embedded/implementation"
	"github.com/zenon-network/go-zenon/zenon"
)

type HtlcApi struct {
	chain chain.Chain
	z     zenon.Zenon
	cs    consensus.Consensus
	log   log15.Logger
}

func NewHtlcApi(z zenon.Zenon) *HtlcApi {
	return &HtlcApi{
		chain: z.Chain(),
		z:     z,
		cs:    z.Consensus(),
		log:   common.RPCLogger.New("module", "embedded_htlc_api"),
	}
}

func (a *HtlcApi) GetById(id types.Hash) (*definition.HtlcInfo, error) {

	_, context, err := api.GetFrontierContext(a.chain, types.HtlcContract)
	if err != nil {
		return nil, err
	}

	htlcInfo, err := definition.GetHtlcInfo(context.Storage(), id)
	if err != nil {
		return nil, err
	}

	return htlcInfo, nil
}

func (a *HtlcApi) GetProxyUnlockStatus(address types.Address) (bool, error) {
	_, context, err := api.GetFrontierContext(a.chain, types.HtlcContract)
	if err != nil {
		return false, err
	}
	return implementation.GetHtlcProxyUnlockStatus(context, address)
}
