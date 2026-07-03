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

// SporkApi implements the "embedded.spork" JSON-RPC namespace, which
// reads the protocol-upgrade records stored in the spork embedded
// contract as of the frontier momentum. A spork is created and later
// activated only by send blocks from the spork address (or, from
// momentum height 10109240 up to but excluding 13243712, the
// community spork address);
// activation sets Activated and an EnforcementHeight of the activation
// momentum's height plus 6, after which the gated feature applies.
// Every exported method is served as
// embedded.spork.<lowerCamelMethodName>.
type SporkApi struct {
	chain chain.Chain
	z     zenon.Zenon
	cs    consensus.Consensus
	log   log15.Logger
}

// NewSporkApi returns a SporkApi bound to the given node's chain. It is
// called by the RPC server when the "embedded" namespace is enabled; it
// is not itself an RPC method.
func NewSporkApi(z zenon.Zenon) *SporkApi {
	return &SporkApi{
		chain: z.Chain(),
		z:     z,
		cs:    z.Consensus(),
		log:   common.RPCLogger.New("module", "embedded_spork_api"),
	}
}

// SporkList is one page of sporks. Count is the total number of sporks
// ever created, not the number of entries in List.
type SporkList struct {
	Count uint32              `json:"count"`
	List  []*definition.Spork `json:"list"`
}

// GetAll returns one page of every spork ever created, activated or
// not, read from contract state at the frontier momentum in storage
// (spork id) order. A spork's id is the hash of the send block that
// created it. A pageSize above 1024 is rejected with
// api.ErrPageSizeParamTooBig.
//
// JSON-RPC: embedded.spork.getAll
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
