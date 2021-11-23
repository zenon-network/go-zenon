package momentum

import (
	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common/types"
)

func (ms *momentumStore) GetBlockWhichReceives(hash types.Hash) (*nom.AccountBlock, error) {
	block, err := ms.GetAccountBlockByHash(hash)
	if err != nil || block == nil {
		return nil, err
	}

	fromHeader := ms.GetAccountMailbox(block.Address).GetBlockWhichReceives(hash)
	if fromHeader == nil {
		return nil, nil
	}
	return ms.GetAccountBlock(*fromHeader)
}
