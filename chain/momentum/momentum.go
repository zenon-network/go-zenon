package momentum

import (
	"fmt"

	"github.com/syndtr/goleveldb/leveldb"

	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common/db"
	"github.com/zenon-network/go-zenon/common/types"
)

func (ms *momentumStore) SetFrontier(momentum *nom.Momentum) error {
	data, err := momentum.Serialize()
	if err != nil {
		return err
	}

	return db.SetFrontier(ms.DB, momentum.Identifier(), data)
}

func parseMomentum(data []byte, err error) (*nom.Momentum, error) {
	if err == leveldb.ErrNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return nom.DeserializeMomentum(data)
}

func (ms *momentumStore) GetFrontierMomentum() (*nom.Momentum, error) {
	return parseMomentum(db.GetEntryByHeight(ms.DB, db.GetFrontierIdentifier(ms.DB).Height))
}
func (ms *momentumStore) GetMomentumByHash(hash types.Hash) (*nom.Momentum, error) {
	return parseMomentum(db.GetEntryByHash(ms.DB, hash))
}
func (ms *momentumStore) GetMomentumsByHash(blockHash types.Hash, higher bool, count uint64) ([]*nom.Momentum, error) {
	momentum, err := ms.GetMomentumByHash(blockHash)
	if err != nil {
		return nil, err
	}
	return ms.GetMomentumsByHeight(momentum.Height, higher, count)
}
func (ms *momentumStore) GetMomentumByHeight(height uint64) (*nom.Momentum, error) {
	return parseMomentum(db.GetEntryByHeight(ms.DB, height))
}
func (ms *momentumStore) GetMomentumsByHeight(height uint64, higher bool, count uint64) ([]*nom.Momentum, error) {
	var to, from uint64
	if higher {
		from = height
		to = height + count
	} else {
		if height+1 <= count {
			from = 1
		} else {
			from = height + 1 - count
		}
		to = height + 1
	}
	return ms.getMomentumsByRange(from, to)
}

func (ms *momentumStore) PrefetchMomentum(momentum *nom.Momentum) (*nom.DetailedMomentum, error) {
	accountBlocks := make([]*nom.AccountBlock, len(momentum.Content))
	for index := range momentum.Content {
		var err error
		accountBlocks[index], err = ms.GetAccountBlock(*momentum.Content[index])
		if err != nil {
			return nil, fmt.Errorf("error while prefetching account-blocks for insert-momentum event. %w", err)
		}
	}

	return &nom.DetailedMomentum{
		Momentum:      momentum,
		AccountBlocks: accountBlocks,
	}, nil
}

func (ms *momentumStore) getMomentumsByRange(from, to uint64) ([]*nom.Momentum, error) {
	list := make([]*nom.Momentum, 0, to-from)
	for i := from; i < to; i += 1 {
		momentum, err := ms.GetMomentumByHeight(i)
		if err != nil {
			return nil, err
		}
		list = append(list, momentum)
	}
	return list, nil
}
