package account

import (
	"github.com/pkg/errors"
	"github.com/syndtr/goleveldb/leveldb"

	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common/db"
	"github.com/zenon-network/go-zenon/common/types"
)

func parseAccountBlock(data []byte, err error) (*nom.AccountBlock, error) {
	if err == leveldb.ErrNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if block, err := nom.DeserializeAccountBlock(data); err != nil {
		return nil, errors.Errorf("failed to deserialize account-block; reason: %v", err.Error())
	} else {
		return block, nil
	}
}

func (as *accountStore) Frontier() (*nom.AccountBlock, error) {
	height := db.GetFrontierIdentifier(as.DB).Height
	block, err := parseAccountBlock(db.GetEntryByHeight(as.DB, height))
	if err != nil {
		return nil, errors.Wrapf(err, "account store %s: frontier entry at height %d", as.address, height)
	}
	return block, nil
}
func (as *accountStore) ByHash(hash types.Hash) (*nom.AccountBlock, error) {
	return parseAccountBlock(db.GetEntryByHash(as.DB, hash))
}
func (as *accountStore) ByHeight(height uint64) (*nom.AccountBlock, error) {
	return parseAccountBlock(db.GetEntryByHeight(as.DB, height))
}

// MoreByHeight returns the blocks from height upwards, at most count of
// them and never past the frontier, in ascending order. A height above
// the frontier yields an empty list. The range is bounded before it is
// walked, so a height near the top of the uint64 range cannot wrap
// around to the chain's first blocks.
func (as *accountStore) MoreByHeight(height, count uint64) ([]*nom.AccountBlock, error) {
	answer := make([]*nom.AccountBlock, 0)
	frontier := db.GetFrontierIdentifier(as.DB).Height
	if height == 0 || height > frontier || count == 0 {
		return answer, nil
	}
	if available := frontier - height + 1; count > available {
		count = available
	}
	for i := uint64(0); i < count; i++ {
		block, err := as.ByHeight(height + i)
		if err != nil {
			return nil, err
		}
		if block == nil {
			return nil, errors.Errorf("account-block at height %d is missing below the frontier %d", height+i, frontier)
		}
		answer = append(answer, block)
	}
	return answer, nil
}
