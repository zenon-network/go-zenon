package account

import (
	"github.com/pkg/errors"
	"github.com/syndtr/goleveldb/leveldb"

	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common/db"
	"github.com/zenon-network/go-zenon/common/types"
)

func (as *accountStore) SetFrontier(block *nom.AccountBlock) error {
	data, err := block.Serialize()
	if err != nil {
		return err
	}

	return db.SetFrontier(as.DB, block.Identifier(), data)
}

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
	return parseAccountBlock(db.GetEntryByHeight(as.DB, db.GetFrontierIdentifier(as.DB).Height))
}
func (as *accountStore) ByHash(hash types.Hash) (*nom.AccountBlock, error) {
	return parseAccountBlock(db.GetEntryByHash(as.DB, hash))
}
func (as *accountStore) ByHeight(height uint64) (*nom.AccountBlock, error) {
	return parseAccountBlock(db.GetEntryByHeight(as.DB, height))
}

func (as *accountStore) MoreByHeight(height, count uint64) ([]*nom.AccountBlock, error) {
	answer := make([]*nom.AccountBlock, 0)
	for i := 0; i < int(count); i += 1 {
		block, err := as.ByHeight(height + uint64(i))
		if err != nil {
			return nil, err
		}
		answer = append(answer, block)
	}
	return answer, nil
}
