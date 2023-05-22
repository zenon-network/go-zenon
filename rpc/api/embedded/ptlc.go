package embedded

import (
	"github.com/inconshreveable/log15"

	"github.com/zenon-network/go-zenon/chain"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/consensus"
	"github.com/zenon-network/go-zenon/rpc/api"
	"github.com/zenon-network/go-zenon/vm/embedded/definition"
	"github.com/zenon-network/go-zenon/zenon"
)

type PtlcApi struct {
	chain chain.Chain
	z     zenon.Zenon
	cs    consensus.Consensus
	log   log15.Logger
}

func NewPtlcApi(z zenon.Zenon) *PtlcApi {
	return &PtlcApi{
		chain: z.Chain(),
		z:     z,
		cs:    z.Consensus(),
		log:   common.RPCLogger.New("module", "embedded_ptlc_api"),
	}
}

func (a *PtlcApi) GetById(id types.Hash) (*definition.PtlcInfo, error) {

	_, context, err := api.GetFrontierContext(a.chain, types.PtlcContract)
	if err != nil {
		return nil, err
	}

	ptlcInfo, err := definition.GetPtlcInfo(context.Storage(), id)
	if err != nil {
		return nil, err
	}

	return ptlcInfo, nil
}
