package momentum

import (
	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common/types"
)

// GetBlockWhichReceives returns the confirmed receive block that
// consumed the send block with the given hash, following the mapping
// recorded in the sender's mailbox; it returns nil with a nil error
// if the send block is unknown or not yet received.
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
