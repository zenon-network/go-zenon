package embedded

import (
	"github.com/zenon-network/go-zenon/chain"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/rpc/api"
	"github.com/zenon-network/go-zenon/vm/embedded/definition"
	"github.com/zenon-network/go-zenon/zenon"
)

type SporkApi struct {
	chain chain.Chain
}

func NewSporkApi(z zenon.Zenon) *SporkApi {
	return &SporkApi{
		chain: z.Chain(),
	}
}

type SporkList struct {
	Count uint32              `json:"count"`
	List  []*definition.Spork `json:"list"`
}

func (a *SporkApi) GetAll(pageIndex, pageSize uint32) (*SporkList, error) {
	if pageSize > api.RpcMaxPageSize {
		return nil, api.ErrPageSizeParamTooBig
	}

	_, context, err := api.GetFrontierContext(a.chain, types.SporkContract)
	if err != nil {
		return nil, err
	}

	sporks := definition.GetAllSporks(context.Storage())

	listLen := uint32(len(sporks))
	start, end := api.GetRange(pageIndex, pageSize, listLen)
	return &SporkList{
		Count: listLen,
		List:  sporks[start:end],
	}, nil
}
