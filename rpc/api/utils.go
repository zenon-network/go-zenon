package api

import (
	"github.com/pkg/errors"

	"github.com/zenon-network/go-zenon/chain"
	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/vm/vm_context"
)

const (
	RpcMaxPageSize  = 1024
	RpcMaxCountSize = 1024
)

func GetRange(index, count, listLen uint32) (uint32, uint32) {
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
