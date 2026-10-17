package api

import (
	"github.com/pkg/errors"

	"github.com/zenon-network/go-zenon/chain"
	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/vm/vm_context"
)

// Upper bounds on request parameters, shared by the JSON-RPC handlers
// in this package and in rpc/api/embedded: a pageSize parameter above
// RpcMaxPageSize is rejected with ErrPageSizeParamTooBig and a count
// parameter above RpcMaxCountSize with ErrCountParamTooBig.
const (
	RpcMaxPageSize  = 1024
	RpcMaxCountSize = 1024
)

// GetRange converts 0-based pagination parameters into a half-open
// [start, end) window over a list of listLen elements: page number
// index of size count, clamped to the list bounds. A page that starts
// at or past the end of the list yields the empty window
// (listLen, listLen); a page that extends past the end is truncated at
// listLen.
func GetRange(index, count, listLen uint32) (uint32, uint32) {
	// index*count must not wrap around uint32 into a valid-looking page
	if count != 0 && index > listLen/count {
		return listLen, listLen
	}
	start := index * count
	if start >= listLen {
		return listLen, listLen
	}
	end := start + count
	if end >= listLen {
		return start, listLen
	}
	return start, end
}

// GetFrontierContext returns the chain's current frontier momentum
// together with an account VM context for addr backed by the frontier
// momentum store and addr's frontier account store. That account store
// is the account pool's, so contract storage read through the context
// already reflects the contract's receive blocks that have executed but
// that no momentum has confirmed yet (the sends they answer are always
// confirmed); the embedded RPC handlers read state this way. The
// returned context has no pillar reader attached.
func GetFrontierContext(c chain.Chain, addr types.Address) (*nom.Momentum, vm_context.AccountVmContext, error) {
	store := c.GetFrontierMomentumStore()

	frontier, err := store.GetFrontierMomentum()
	if err != nil {
		return nil, nil, err
	}

	context := vm_context.NewAccountContext(
		store,
		c.GetFrontierAccountStore(addr),
		c.GetFrontierCacheStore(),
		nil,
	)
	return frontier, context, nil
}

func checkTokenIdValid(chain chain.Chain, ts *types.ZenonTokenStandard) error {
	store := chain.GetFrontierMomentumStore()
	if ts != nil && (*ts) != types.ZeroTokenStandard {
		tokenStandard, err := store.GetTokenInfoByTs(*ts)
		if err != nil {
			return err
		}
		if tokenStandard == nil {
			return errors.New("ts doesn’t exist")
		}
	}
	return nil
}
